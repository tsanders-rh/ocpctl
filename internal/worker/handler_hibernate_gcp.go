package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path"

	compute "cloud.google.com/go/compute/apiv1"
	"cloud.google.com/go/compute/apiv1/computepb"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
	"google.golang.org/api/iterator"
)

// hibernateGKE hibernates a GKE cluster by scaling all node pools to 0
func (h *HibernateHandler) hibernateGKE(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Hibernating GKE cluster %s by scaling node pools to 0", cluster.Name)

	// Get profile to extract GCP project
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	project := getGCPProject(prof)
	if project == "" {
		return fmt.Errorf("GCP project ID not found in profile or environment")
	}

	// Verify GCP authentication
	if err := VerifyGCPAuthentication(ctx, project); err != nil {
		return fmt.Errorf("GCP authentication failed: %w", err)
	}

	// Create GKE installer
	gkeInstaller := installer.NewGKEInstaller()

	// List all node pools for this cluster
	nodePools, err := gkeInstaller.ListNodePools(ctx, cluster.Name, project, cluster.Region, "")
	if err != nil {
		return fmt.Errorf("list node pools: %w", err)
	}

	if len(nodePools) == 0 {
		log.Printf("Warning: no node pools found for cluster %s, may already be hibernated", cluster.Name)
		// Update cluster status to HIBERNATED anyway
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
	}

	// Store original node pool sizes in job metadata
	nodePoolSizes := make(map[string]int)

	for _, poolName := range nodePools {
		// Get current node pool info
		// Note: We don't have a GetNodePoolInfo method yet, so we'll scale and record
		// For now, we'll assume pools have nodes and scale to 0
		log.Printf("Scaling node pool %s to 0...", poolName)

		// Scale node pool to 0
		if err := gkeInstaller.ScaleNodePool(ctx, cluster.Name, poolName, project, cluster.Region, "", 0); err != nil {
			log.Printf("Warning: failed to scale node pool %s to 0: %v", poolName, err)
			continue
		}

		// TODO: Store original size - for now we'll use the profile default
		// This will be improved when we add GetNodePoolInfo to the GKE installer
		nodePoolSizes[poolName] = 3 // Default from profile

		log.Printf("Scaled node pool %s to 0", poolName)
	}

	// Store pool sizes in job metadata for resume
	if job.Metadata == nil {
		job.Metadata = make(types.JobMetadata)
	}
	sizesJSON, err := json.Marshal(nodePoolSizes)
	if err != nil {
		log.Printf("Warning: failed to marshal node pool sizes: %v", err)
	} else {
		job.Metadata["node_pool_sizes"] = string(sizesJSON)
		log.Printf("Stored node pool sizes in job metadata for resume")
	}

	// Update cluster status to HIBERNATED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("GKE cluster %s hibernated successfully (node pools scaled to 0, control plane still running)", cluster.Name)
	return nil
}

// hibernateGCPOpenShift hibernates an OpenShift cluster on GCP by stopping all VM instances
func (h *HibernateHandler) hibernateGCPOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Hibernating OpenShift on GCP cluster %s by stopping VM instances", cluster.Name)

	// Get profile to extract GCP project
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	project := getGCPProject(prof)
	if project == "" {
		return fmt.Errorf("GCP project ID not found in profile or environment")
	}

	// Verify GCP authentication
	if err := VerifyGCPAuthentication(ctx, project); err != nil {
		return fmt.Errorf("GCP authentication failed: %w", err)
	}

	// Ensure artifacts are available locally to get infraID
	if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
		return fmt.Errorf("ensure artifacts available: %w", err)
	}

	// Get infraID from metadata.json
	infraID, err := h.getInfraID(cluster)
	if err != nil {
		return fmt.Errorf("get infrastructure ID: %w", err)
	}

	log.Printf("Found infrastructure ID: %s", infraID)

	// Create GCP compute client
	instancesClient, err := compute.NewInstancesRESTClient(ctx)
	if err != nil {
		return fmt.Errorf("create GCP instances client: %w", err)
	}
	defer instancesClient.Close()

	// Find all instances with label kubernetes-io-cluster-{infraID}=owned
	// Note: GCP labels use underscores instead of dots/slashes
	labelFilter := fmt.Sprintf("labels.kubernetes-io-cluster-%s=owned", infraID)

	// List instances across all zones in the region
	aggregatedListReq := &computepb.AggregatedListInstancesRequest{
		Project: project,
		Filter:  &labelFilter,
	}

	it := instancesClient.AggregatedList(ctx, aggregatedListReq)

	var instanceIDs []string
	var instanceZones []string

	for {
		pair, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("iterate instances: %w", err)
		}

		// Check if instances exist in this zone
		if pair.Value == nil || pair.Value.Instances == nil {
			continue
		}

		for _, instance := range pair.Value.Instances {
			if instance.Name == nil || instance.Zone == nil {
				continue
			}

			// Only stop running instances
			if instance.Status != nil && *instance.Status == "RUNNING" {
				instanceIDs = append(instanceIDs, *instance.Name)
				// Extract zone name from full zone path (e.g., "https://www.googleapis.com/.../zones/us-central1-a" -> "us-central1-a")
				zoneName := path.Base(*instance.Zone)
				instanceZones = append(instanceZones, zoneName)
			}
		}
	}

	if len(instanceIDs) == 0 {
		log.Printf("Warning: no running instances found for cluster %s, may already be stopped", cluster.Name)
		// Update cluster status to HIBERNATED anyway
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
	}

	log.Printf("Found %d running instances to stop: %v", len(instanceIDs), instanceIDs)

	// Stop each instance and track successes/failures
	var stoppedCount int
	var failedInstances []string

	for i, instanceName := range instanceIDs {
		zone := instanceZones[i]
		log.Printf("Stopping instance %s in zone %s...", instanceName, zone)

		stopReq := &computepb.StopInstanceRequest{
			Project:  project,
			Zone:     zone,
			Instance: instanceName,
		}

		op, err := instancesClient.Stop(ctx, stopReq)
		if err != nil {
			log.Printf("Warning: failed to stop instance %s: %v", instanceName, err)
			failedInstances = append(failedInstances, instanceName)
			continue
		}

		// Wait for operation to complete
		if err := op.Wait(ctx); err != nil {
			log.Printf("Warning: failed to wait for stop operation on instance %s: %v", instanceName, err)
			failedInstances = append(failedInstances, instanceName)
			continue
		}

		log.Printf("Instance %s stopped successfully", instanceName)
		stoppedCount++
	}

	// If all instances failed to stop, return error
	if stoppedCount == 0 {
		return fmt.Errorf("failed to stop any instances (0/%d succeeded)", len(instanceIDs))
	}

	// If some instances failed, log warning but continue
	if len(failedInstances) > 0 {
		log.Printf("Warning: failed to stop %d/%d instances: %v", len(failedInstances), len(instanceIDs), failedInstances)
	}

	// Store instance info in job metadata for resume
	if job.Metadata == nil {
		job.Metadata = make(types.JobMetadata)
	}
	job.Metadata["instance_count"] = fmt.Sprintf("%d", stoppedCount)
	job.Metadata["total_instances"] = fmt.Sprintf("%d", len(instanceIDs))
	job.Metadata["infra_id"] = infraID
	if len(failedInstances) > 0 {
		job.Metadata["failed_instances"] = fmt.Sprintf("%v", failedInstances)
	}

	// Update cluster status to HIBERNATED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("OpenShift on GCP cluster %s hibernated successfully (%d/%d instances stopped)", cluster.Name, stoppedCount, len(instanceIDs))
	return nil
}
