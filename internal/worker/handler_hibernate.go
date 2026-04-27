package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// HibernateHandler handles cluster hibernation jobs
type HibernateHandler struct {
	config   *Config
	store    *store.Store
	registry *profile.Registry
}

// NewHibernateHandler creates a new hibernate handler
func NewHibernateHandler(cfg *Config, st *store.Store) *HibernateHandler {
	// Load profile registry
	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "internal/profile/definitions"
	}

	loader := profile.NewLoader(profilesDir)
	registry, err := profile.NewRegistry(loader)
	if err != nil {
		log.Fatalf("Failed to load profile registry: %v", err)
	}

	return &HibernateHandler{
		config:   cfg,
		store:    st,
		registry: registry,
	}
}

// Handle handles a cluster hibernation job
func (h *HibernateHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Hibernating cluster %s (platform=%s, cluster_type=%s)", cluster.Name, cluster.Platform, cluster.ClusterType)

	// Route by cluster type
	switch cluster.ClusterType {
	case types.ClusterTypeOpenShift:
		return h.hibernateOpenShift(ctx, cluster, job)
	case types.ClusterTypeEKS:
		return h.hibernateEKS(ctx, cluster, job)
	case types.ClusterTypeIKS:
		return h.hibernateIKS(ctx, cluster, job)
	case types.ClusterTypeGKE:
		return h.hibernateGKE(ctx, cluster, job)
	default:
		return fmt.Errorf("unsupported cluster type for hibernation: %s", cluster.ClusterType)
	}
}

// hibernateOpenShift hibernates an OpenShift cluster (AWS, IBMCloud, or GCP)
func (h *HibernateHandler) hibernateOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	switch cluster.Platform {
	case types.PlatformAWS:
		return h.hibernateAWS(ctx, cluster, job)
	case types.PlatformIBMCloud:
		return h.fallbackToDestroy(ctx, cluster, job)
	case types.PlatformGCP:
		return h.hibernateGCPOpenShift(ctx, cluster, job)
	default:
		return fmt.Errorf("unsupported platform for OpenShift hibernation: %s", cluster.Platform)
	}
}

// hibernateAWS hibernates an AWS cluster by stopping all EC2 instances
func (h *HibernateHandler) hibernateAWS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Hibernating AWS cluster %s by stopping EC2 instances", cluster.Name)

	// Ensure artifacts are available locally
	if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
		return fmt.Errorf("ensure artifacts available: %w", err)
	}

	// Get infraID from metadata.json (now available after ensureArtifactsAvailable)
	infraID, err := h.getInfraID(cluster)
	if err != nil {
		return fmt.Errorf("get infrastructure ID: %w", err)
	}

	log.Printf("Found infrastructure ID: %s", infraID)

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// Find all instances with tag kubernetes.io/cluster/{infraID}=owned
	tagKey := fmt.Sprintf("kubernetes.io/cluster/%s", infraID)
	describeInput := &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   strPtr("tag-key"),
				Values: []string{tagKey},
			},
			{
				Name:   strPtr("instance-state-name"),
				Values: []string{"running"},
			},
		},
	}

	result, err := ec2Client.DescribeInstances(ctx, describeInput)
	if err != nil {
		return fmt.Errorf("describe instances: %w", err)
	}

	// Collect instance IDs
	var instanceIDs []string
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceId != nil {
				instanceIDs = append(instanceIDs, *instance.InstanceId)
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

	// Store instance IDs in job metadata for resume
	if job.Metadata == nil {
		job.Metadata = make(types.JobMetadata)
	}
	job.Metadata["instance_ids"] = instanceIDs
	job.Metadata["infra_id"] = infraID

	// Stop instances
	stopInput := &ec2.StopInstancesInput{
		InstanceIds: instanceIDs,
	}

	stopResult, err := ec2Client.StopInstances(ctx, stopInput)
	if err != nil {
		return fmt.Errorf("stop instances: %w", err)
	}

	log.Printf("Successfully initiated stop for %d instances", len(stopResult.StoppingInstances))

	// Wait for instances to be fully stopped before marking as HIBERNATED
	// This is especially important for bare metal instances (m5zn.metal, m6i.metal) which can take 20+ minutes
	log.Printf("Waiting for all instances to fully stop...")
	stoppedWaiter := ec2.NewInstanceStoppedWaiter(ec2Client)
	waitInput := &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	}

	waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Minute)
	defer waitCancel()

	if err := stoppedWaiter.Wait(waitCtx, waitInput, 30*time.Minute); err != nil {
		return fmt.Errorf("instances did not stop within 30 minutes: %w", err)
	}

	log.Printf("All instances are now fully stopped")

	// Update cluster status to HIBERNATED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("Cluster %s is now HIBERNATED", cluster.Name)

	return nil
}

// fallbackToDestroy logs a warning for unsupported platforms
func (h *HibernateHandler) fallbackToDestroy(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("WARNING: Hibernation not supported for platform %s (cluster %s)", cluster.Platform, cluster.Name)
	log.Printf("Cluster will remain in HIBERNATING state - manual intervention required")

	// Mark cluster status back to READY since we can't hibernate
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	return fmt.Errorf("hibernation not supported for platform %s", cluster.Platform)
}

// getInfraID extracts the infrastructure ID from metadata.json
func (h *HibernateHandler) getInfraID(cluster *types.Cluster) (string, error) {
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	metadataPath := filepath.Join(workDir, "metadata.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return "", fmt.Errorf("read metadata.json: %w", err)
	}

	var metadata struct {
		InfraID string `json:"infraID"`
	}

	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("parse metadata.json: %w", err)
	}

	if metadata.InfraID == "" {
		return "", fmt.Errorf("infraID not found in metadata.json")
	}

	return metadata.InfraID, nil
}

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}

// ensureArtifactsAvailable downloads cluster artifacts from S3 if they don't exist locally
func (h *HibernateHandler) ensureArtifactsAvailable(ctx context.Context, clusterID string) error {
	workDir := filepath.Join(h.config.WorkDir, clusterID)
	metadataPath := filepath.Join(workDir, "metadata.json")

	// Check if metadata.json already exists
	if _, err := os.Stat(metadataPath); err == nil {
		log.Printf("[HibernateHandler] Artifacts already available locally for cluster %s", clusterID)
		return nil
	}

	// Download artifacts from S3
	log.Printf("[HibernateHandler] Downloading artifacts from S3 for cluster %s", clusterID)
	artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
	if err != nil {
		return fmt.Errorf("create artifact storage: %w", err)
	}

	if err := artifactStorage.DownloadClusterArtifacts(ctx, clusterID, workDir); err != nil {
		return fmt.Errorf("download artifacts: %w", err)
	}

	log.Printf("[HibernateHandler] Successfully downloaded artifacts for cluster %s", clusterID)
	return nil
}

// hibernateEKS hibernates an EKS cluster by scaling all node groups to 0
func (h *HibernateHandler) hibernateEKS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Hibernating EKS cluster %s by scaling node groups to 0", cluster.Name)

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	eksClient := eks.NewFromConfig(cfg)

	// List all node groups for this cluster
	listNgInput := &eks.ListNodegroupsInput{
		ClusterName: &cluster.Name,
	}

	listNgOutput, err := eksClient.ListNodegroups(ctx, listNgInput)
	if err != nil {
		return fmt.Errorf("list node groups: %w", err)
	}

	if len(listNgOutput.Nodegroups) == 0 {
		// No managed nodegroups found - this could mean:
		// 1. Cluster was created with unmanaged nodeGroups (CloudFormation-based)
		// 2. Cluster has no nodegroups at all
		// Either way, hibernate/resume workflow requires managed nodegroups
		return fmt.Errorf("cluster %s has no managed nodegroups in EKS API - hibernate/resume only supports EKS-managed nodegroups. This cluster may use unmanaged nodegroups (created via CloudFormation) which are not visible to the EKS API. To use hibernate/resume, the cluster must be recreated with managedNodeGroups in the profile", cluster.Name)
	}

	// Store original node group capacities in job metadata
	nodeGroupCapacities := make(map[string]int)

	for _, ngName := range listNgOutput.Nodegroups {
		// Get current node group configuration
		describeInput := &eks.DescribeNodegroupInput{
			ClusterName:   &cluster.Name,
			NodegroupName: &ngName,
		}

		describeOutput, err := eksClient.DescribeNodegroup(ctx, describeInput)
		if err != nil {
			log.Printf("Warning: failed to describe node group %s: %v", ngName, err)
			continue
		}

		// Store original desired capacity
		if describeOutput.Nodegroup.ScalingConfig != nil && describeOutput.Nodegroup.ScalingConfig.DesiredSize != nil {
			originalCapacity := int(*describeOutput.Nodegroup.ScalingConfig.DesiredSize)
			nodeGroupCapacities[ngName] = originalCapacity
			log.Printf("Node group %s original capacity: %d", ngName, originalCapacity)

			// Scale to 0
			updateInput := &eks.UpdateNodegroupConfigInput{
				ClusterName:   &cluster.Name,
				NodegroupName: &ngName,
				ScalingConfig: &ekstypes.NodegroupScalingConfig{
					DesiredSize: int32Ptr(0),
					MinSize:     int32Ptr(0),
					MaxSize:     describeOutput.Nodegroup.ScalingConfig.MaxSize,
				},
			}

			_, err = eksClient.UpdateNodegroupConfig(ctx, updateInput)
			if err != nil {
				log.Printf("Warning: failed to scale node group %s to 0: %v", ngName, err)
				continue
			}

			log.Printf("Scaled node group %s to 0 (original: %d)", ngName, originalCapacity)
		}
	}

	// Store capacities in job metadata for resume
	// Note: Metadata will be saved when the job completes via MarkSucceeded
	if job.Metadata == nil {
		job.Metadata = make(types.JobMetadata)
	}
	capacitiesJSON, err := json.Marshal(nodeGroupCapacities)
	if err != nil {
		log.Printf("Warning: failed to marshal node group capacities: %v", err)
	} else {
		job.Metadata["node_group_capacities"] = string(capacitiesJSON)
		log.Printf("Stored node group capacities in job metadata for resume")
	}

	// Update cluster status to HIBERNATED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("EKS cluster %s hibernated successfully (node groups scaled to 0, control plane still running at $0.10/hr)", cluster.Name)
	return nil
}

// hibernateIKS hibernates an IKS cluster by scaling workers to 0
func (h *HibernateHandler) hibernateIKS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Hibernating IKS cluster %s by scaling workers to 0", cluster.Name)

	// Get profile to extract configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Extract resource group from profile (if specified)
	resourceGroup := ""
	if prof.PlatformConfig.IBMCloud != nil {
		resourceGroup = prof.PlatformConfig.IBMCloud.ResourceGroup
	}

	// Create IKS installer
	iksInstaller := installer.NewIKSInstaller()

	// Get IBM Cloud API key from environment
	apiKey := os.Getenv("IBMCLOUD_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("IBMCLOUD_API_KEY environment variable not set")
	}

	// Login to IBM Cloud (will query for resource groups if not specified)
	if err := iksInstaller.Login(ctx, apiKey, cluster.Region, resourceGroup); err != nil {
		return fmt.Errorf("IBM Cloud login: %w", err)
	}

	// Get cluster info to find cluster ID
	info, err := iksInstaller.GetClusterInfo(ctx, cluster.Name)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	// Get current worker pool configurations before scaling down
	log.Printf("Retrieving worker pool configurations for cluster %s...", cluster.Name)
	originalSizes, err := iksInstaller.ScaleAllWorkerPools(ctx, cluster.Name, 0)
	if err != nil {
		return fmt.Errorf("scale worker pools to 0: %w", err)
	}

	// Calculate total original worker count
	totalOriginalCount := 0
	for _, size := range originalSizes {
		totalOriginalCount += size
	}

	log.Printf("Scaled down %d worker pools (total %d workers) for cluster %s", len(originalSizes), totalOriginalCount, cluster.Name)

	// Store cluster ID and worker pool sizes in job metadata for resume
	// Note: Metadata will be saved when the job completes via MarkSucceeded
	if job.Metadata == nil {
		job.Metadata = make(types.JobMetadata)
	}
	job.Metadata["cluster_id"] = info.ID
	job.Metadata["total_worker_count"] = fmt.Sprintf("%d", totalOriginalCount)

	// Store each pool's original size for restoration
	poolSizesJSON, err := json.Marshal(originalSizes)
	if err != nil {
		return fmt.Errorf("marshal pool sizes: %w", err)
	}
	job.Metadata["worker_pool_sizes"] = string(poolSizesJSON)

	log.Printf("Stored cluster info and worker pool configurations in job metadata for resume")

	// Update cluster status to HIBERNATED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("IKS cluster %s hibernated successfully (scaled %d workers to 0)", cluster.Name, totalOriginalCount)
	return nil
}

// int32Ptr returns a pointer to an int32
func int32Ptr(i int32) *int32 {
	return &i
}
