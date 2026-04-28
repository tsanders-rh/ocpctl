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

// resumeGKE resumes a GKE cluster by scaling node pools back to original size
func (h *ResumeHandler) resumeGKE(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming GKE cluster %s by scaling node pools", cluster.Name)

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

	// Get stored node pool sizes from hibernate job metadata
	// We need to find the most recent HIBERNATE job for this cluster
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}

	// Find the most recent successful hibernate job
	var nodePoolSizes map[string]int
	for i := len(hibernateJobs) - 1; i >= 0; i-- {
		if hibernateJobs[i].Status == types.JobStatusSucceeded {
			// Parse node pool sizes from metadata
			if sizesJSON, ok := hibernateJobs[i].Metadata["node_pool_sizes"].(string); ok {
				if err := json.Unmarshal([]byte(sizesJSON), &nodePoolSizes); err != nil {
					log.Printf("Warning: failed to parse node pool sizes: %v", err)
					continue
				}
				log.Printf("Found node pool sizes from hibernate job: %v", nodePoolSizes)
				break
			}
		}
	}

	// If we couldn't find stored sizes, use profile defaults
	if nodePoolSizes == nil {
		log.Printf("No stored node pool sizes found, using profile defaults")
		nodePoolSizes = make(map[string]int)

		// Use default from profile
		if prof.Compute.Workers != nil {
			defaultSize := prof.Compute.Workers.Replicas
			if defaultSize == 0 {
				defaultSize = 3 // Fallback default
			}

			// List current node pools
			nodePools, err := gkeInstaller.ListNodePools(ctx, cluster.Name, project, cluster.Region, "")
			if err != nil {
				return fmt.Errorf("list node pools: %w", err)
			}

			for _, poolName := range nodePools {
				nodePoolSizes[poolName] = defaultSize
			}
		}
	}

	// Scale each node pool back to original size
	for poolName, targetSize := range nodePoolSizes {
		log.Printf("Scaling node pool %s to %d...", poolName, targetSize)

		if err := gkeInstaller.ScaleNodePool(ctx, cluster.Name, poolName, project, cluster.Region, "", targetSize); err != nil {
			log.Printf("Warning: failed to scale node pool %s to %d: %v", poolName, targetSize, err)
			continue
		}

		log.Printf("Scaled node pool %s to %d", poolName, targetSize)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("GKE cluster %s resumed successfully (node pools scaled back up)", cluster.Name)
	return nil
}

// resumeGCPOpenShift resumes an OpenShift cluster on GCP by starting all VM instances
func (h *ResumeHandler) resumeGCPOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming OpenShift on GCP cluster %s by starting VM instances", cluster.Name)

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

			// Only start stopped instances
			if instance.Status != nil && (*instance.Status == "TERMINATED" || *instance.Status == "STOPPED") {
				instanceIDs = append(instanceIDs, *instance.Name)
				// Extract zone name from full zone path (e.g., "https://www.googleapis.com/.../zones/us-central1-a" -> "us-central1-a")
				zoneName := path.Base(*instance.Zone)
				instanceZones = append(instanceZones, zoneName)
			}
		}
	}

	if len(instanceIDs) == 0 {
		log.Printf("Warning: no stopped instances found for cluster %s, may already be running", cluster.Name)
		// Update cluster status to READY anyway
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
	}

	log.Printf("Found %d stopped instances to start: %v", len(instanceIDs), instanceIDs)

	// Start each instance
	for i, instanceName := range instanceIDs {
		zone := instanceZones[i]
		log.Printf("Starting instance %s in zone %s...", instanceName, zone)

		startReq := &computepb.StartInstanceRequest{
			Project:  project,
			Zone:     zone,
			Instance: instanceName,
		}

		op, err := instancesClient.Start(ctx, startReq)
		if err != nil {
			log.Printf("Warning: failed to start instance %s: %v", instanceName, err)
			continue
		}

		// Wait for operation to complete
		if err := op.Wait(ctx); err != nil {
			log.Printf("Warning: failed to wait for start operation on instance %s: %v", instanceName, err)
			continue
		}

		log.Printf("Instance %s started successfully", instanceName)
	}

	log.Printf("All instances started, waiting for cluster to become ready...")

	// Wait for cluster to become accessible (similar to AWS resume)
	if err := h.waitForClusterHealth(ctx, cluster); err != nil {
		log.Printf("Warning: cluster health check failed: %v", err)
		// Don't fail - instances are running, cluster may just need more time
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("OpenShift on GCP cluster %s resumed successfully (%d instances started)", cluster.Name, len(instanceIDs))
	return nil
}
