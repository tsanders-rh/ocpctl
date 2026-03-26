package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

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

// ResumeHandler handles cluster resume jobs
type ResumeHandler struct {
	config   *Config
	store    *store.Store
	registry *profile.Registry
}

// NewResumeHandler creates a new resume handler
func NewResumeHandler(cfg *Config, st *store.Store) *ResumeHandler {
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

	return &ResumeHandler{
		config:   cfg,
		store:    st,
		registry: registry,
	}
}

// Handle handles a cluster resume job by starting stopped instances or scaling node groups back up.
// Routes to the appropriate platform-specific resume handler based on cluster type.
// Supports OpenShift (AWS), EKS, and IKS cluster types.
func (h *ResumeHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Resuming cluster %s (platform=%s, cluster_type=%s)", cluster.Name, cluster.Platform, cluster.ClusterType)

	// Route by cluster type
	switch cluster.ClusterType {
	case types.ClusterTypeOpenShift:
		return h.resumeOpenShift(ctx, cluster, job)
	case types.ClusterTypeEKS:
		return h.resumeEKS(ctx, cluster, job)
	case types.ClusterTypeIKS:
		return h.resumeIKS(ctx, cluster, job)
	default:
		return fmt.Errorf("unsupported cluster type for resume: %s", cluster.ClusterType)
	}
}

// resumeOpenShift resumes an OpenShift cluster (AWS or IBMCloud)
func (h *ResumeHandler) resumeOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	switch cluster.Platform {
	case types.PlatformAWS:
		return h.resumeAWS(ctx, cluster, job)
	case types.PlatformIBMCloud:
		return fmt.Errorf("resume not supported for platform %s - cluster was destroyed", cluster.Platform)
	default:
		return fmt.Errorf("unsupported platform for OpenShift resume: %s", cluster.Platform)
	}
}

// resumeAWS resumes an AWS cluster by starting all EC2 instances
func (h *ResumeHandler) resumeAWS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming AWS cluster %s by starting EC2 instances", cluster.Name)

	// Ensure artifacts are available locally
	if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
		return fmt.Errorf("ensure artifacts available: %w", err)
	}

	// Get instance IDs from the last HIBERNATE job metadata
	var instanceIDs []string
	var infraID string

	// Try to get instance IDs from the most recent successful HIBERNATE job
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}

	// Find the most recent successful hibernate job
	var lastHibernateJob *types.Job
	for _, hJob := range hibernateJobs {
		if hJob.Status == types.JobStatusSucceeded {
			if lastHibernateJob == nil || hJob.CreatedAt.After(lastHibernateJob.CreatedAt) {
				lastHibernateJob = hJob
			}
		}
	}

	if lastHibernateJob != nil && lastHibernateJob.Metadata != nil {
		if ids, ok := lastHibernateJob.Metadata["instance_ids"].([]interface{}); ok {
			for _, id := range ids {
				if strID, ok := id.(string); ok {
					instanceIDs = append(instanceIDs, strID)
				}
			}
		}
		if id, ok := lastHibernateJob.Metadata["infra_id"].(string); ok {
			infraID = id
		}
	}

	// If we don't have instance IDs, try to discover them
	if len(instanceIDs) == 0 {
		log.Printf("No instance IDs found in hibernate job metadata, discovering instances")

		// Get infraID from metadata.json (now available after ensureArtifactsAvailable)
		if infraID == "" {
			infraID, err = h.getInfraID(cluster)
			if err != nil {
				return fmt.Errorf("get infrastructure ID: %w", err)
			}
		}

		log.Printf("Using infrastructure ID: %s", infraID)

		// Load AWS config
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
		if err != nil {
			return fmt.Errorf("load AWS config: %w", err)
		}

		ec2Client := ec2.NewFromConfig(cfg)

		// Find all stopped instances with tag kubernetes.io/cluster/{infraID}=owned
		tagKey := fmt.Sprintf("kubernetes.io/cluster/%s", infraID)
		describeInput := &ec2.DescribeInstancesInput{
			Filters: []ec2types.Filter{
				{
					Name:   strPtr("tag-key"),
					Values: []string{tagKey},
				},
				{
					Name:   strPtr("instance-state-name"),
					Values: []string{"stopped"},
				},
			},
		}

		result, err := ec2Client.DescribeInstances(ctx, describeInput)
		if err != nil {
			return fmt.Errorf("describe instances: %w", err)
		}

		// Collect instance IDs
		for _, reservation := range result.Reservations {
			for _, instance := range reservation.Instances {
				if instance.InstanceId != nil {
					instanceIDs = append(instanceIDs, *instance.InstanceId)
				}
			}
		}
	}

	if len(instanceIDs) == 0 {
		log.Printf("Warning: no stopped instances found for cluster %s", cluster.Name)
		// Update cluster status to READY anyway (maybe instances are already running)
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
	}

	log.Printf("Found %d stopped instances to start: %v", len(instanceIDs), instanceIDs)

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	// Start instances
	startInput := &ec2.StartInstancesInput{
		InstanceIds: instanceIDs,
	}

	startResult, err := ec2Client.StartInstances(ctx, startInput)
	if err != nil {
		return fmt.Errorf("start instances: %w", err)
	}

	log.Printf("Successfully initiated start for %d instances", len(startResult.StartingInstances))

	// Wait for instances to reach running state (with timeout)
	log.Printf("Waiting for instances to reach running state...")
	waiter := ec2.NewInstanceRunningWaiter(ec2Client)
	waitInput := &ec2.DescribeInstancesInput{
		InstanceIds: instanceIDs,
	}

	waitCtx, cancel := context.WithTimeout(ctx, ClusterStatusCheckTimeout)
	defer cancel()

	if err := waiter.Wait(waitCtx, waitInput, ClusterStatusCheckTimeout); err != nil {
		log.Printf("Warning: instances may not be fully running yet: %v", err)
		// Don't fail the job, just log the warning
	} else {
		log.Printf("All instances are now running")
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("Cluster %s is now READY", cluster.Name)

	return nil
}

// getInfraID extracts the infrastructure ID from metadata.json
// Reuses the same implementation as HibernateHandler
func (h *ResumeHandler) getInfraID(cluster *types.Cluster) (string, error) {
	hibernateHandler := NewHibernateHandler(h.config, h.store)
	return hibernateHandler.getInfraID(cluster)
}

// ensureArtifactsAvailable downloads cluster artifacts from S3 if they don't exist locally
func (h *ResumeHandler) ensureArtifactsAvailable(ctx context.Context, clusterID string) error {
	workDir := filepath.Join(h.config.WorkDir, clusterID)
	metadataPath := filepath.Join(workDir, "metadata.json")

	// Check if metadata.json already exists
	if _, err := os.Stat(metadataPath); err == nil {
		log.Printf("[ResumeHandler] Artifacts already available locally for cluster %s", clusterID)
		return nil
	}

	// Download artifacts from S3
	log.Printf("[ResumeHandler] Downloading artifacts from S3 for cluster %s", clusterID)
	artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
	if err != nil {
		return fmt.Errorf("create artifact storage: %w", err)
	}

	if err := artifactStorage.DownloadClusterArtifacts(ctx, clusterID, workDir); err != nil {
		return fmt.Errorf("download artifacts: %w", err)
	}

	log.Printf("[ResumeHandler] Successfully downloaded artifacts for cluster %s", clusterID)
	return nil
}

// resumeEKS resumes an EKS cluster by scaling node groups back to original capacity
func (h *ResumeHandler) resumeEKS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming EKS cluster %s by scaling node groups to original capacity", cluster.Name)

	// Get the last hibernate job to retrieve original capacities
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}

	// Find the most recent successful hibernate job
	var lastHibernateJob *types.Job
	for _, hJob := range hibernateJobs {
		if hJob.Status == types.JobStatusSucceeded {
			if lastHibernateJob == nil || hJob.CreatedAt.After(lastHibernateJob.CreatedAt) {
				lastHibernateJob = hJob
			}
		}
	}

	if lastHibernateJob == nil {
		return fmt.Errorf("no successful hibernate job found for cluster %s", cluster.ID)
	}

	// Get original node group capacities from job metadata
	capacitiesJSON, ok := lastHibernateJob.Metadata["node_group_capacities"]
	if !ok {
		return fmt.Errorf("node_group_capacities not found in hibernate job metadata")
	}

	capacitiesStr, ok := capacitiesJSON.(string)
	if !ok {
		return fmt.Errorf("node_group_capacities is not a string")
	}

	var nodeGroupCapacities map[string]int
	if err := json.Unmarshal([]byte(capacitiesStr), &nodeGroupCapacities); err != nil {
		return fmt.Errorf("unmarshal node group capacities: %w", err)
	}

	log.Printf("Restoring node group capacities: %+v", nodeGroupCapacities)

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(cluster.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	eksClient := eks.NewFromConfig(cfg)

	// Scale each node group back to original capacity
	for ngName, originalCapacity := range nodeGroupCapacities {
		log.Printf("Scaling node group %s to %d", ngName, originalCapacity)

		// Get current node group config to preserve max size
		describeInput := &eks.DescribeNodegroupInput{
			ClusterName:   &cluster.Name,
			NodegroupName: &ngName,
		}

		describeOutput, err := eksClient.DescribeNodegroup(ctx, describeInput)
		if err != nil {
			log.Printf("Warning: failed to describe node group %s: %v", ngName, err)
			continue
		}

		// Scale back to original capacity
		updateInput := &eks.UpdateNodegroupConfigInput{
			ClusterName:   &cluster.Name,
			NodegroupName: &ngName,
			ScalingConfig: &ekstypes.NodegroupScalingConfig{
				DesiredSize: int32Ptr(int32(originalCapacity)),
				MinSize:     int32Ptr(0), // Keep min at 0 for future hibernation
				MaxSize:     describeOutput.Nodegroup.ScalingConfig.MaxSize,
			},
		}

		_, err = eksClient.UpdateNodegroupConfig(ctx, updateInput)
		if err != nil {
			log.Printf("Warning: failed to scale node group %s: %v", ngName, err)
			continue
		}

		log.Printf("Successfully scaled node group %s to %d", ngName, originalCapacity)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("EKS cluster %s resumed successfully", cluster.Name)
	return nil
}

// resumeIKS resumes an IKS cluster by scaling workers back to original count
func (h *ResumeHandler) resumeIKS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming IKS cluster %s by scaling workers to original count", cluster.Name)

	// Get the last hibernate job to retrieve original worker count
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}

	// Find the most recent successful hibernate job
	var lastHibernateJob *types.Job
	for _, hJob := range hibernateJobs {
		if hJob.Status == types.JobStatusSucceeded {
			if lastHibernateJob == nil || hJob.CreatedAt.After(lastHibernateJob.CreatedAt) {
				lastHibernateJob = hJob
			}
		}
	}

	if lastHibernateJob == nil {
		return fmt.Errorf("no successful hibernate job found for cluster %s", cluster.ID)
	}

	// Get original worker pool sizes from job metadata
	poolSizesVal, ok := lastHibernateJob.Metadata["worker_pool_sizes"]
	if !ok {
		// Fallback to legacy metadata format if new format not available
		return fmt.Errorf("worker_pool_sizes not found in hibernate job metadata (cluster may have been hibernated before this feature was implemented)")
	}

	poolSizesStr, ok := poolSizesVal.(string)
	if !ok {
		return fmt.Errorf("worker_pool_sizes is not a string")
	}

	// Parse the worker pool sizes map
	var originalPoolSizes map[string]int
	if err := json.Unmarshal([]byte(poolSizesStr), &originalPoolSizes); err != nil {
		return fmt.Errorf("parse worker pool sizes: %w", err)
	}

	// Calculate total worker count for logging
	totalWorkers := 0
	for _, size := range originalPoolSizes {
		totalWorkers += size
	}

	log.Printf("Restoring IKS cluster to original configuration (%d worker pools, %d total workers)", len(originalPoolSizes), totalWorkers)

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

	// Restore each worker pool to its original size
	log.Printf("Restoring worker pools to original sizes...")
	restoredCount := 0
	for poolName, originalSize := range originalPoolSizes {
		if originalSize == 0 {
			log.Printf("Skipping pool '%s' (original size was 0)", poolName)
			continue
		}

		log.Printf("Restoring worker pool '%s' to %d workers per zone", poolName, originalSize)
		if err := iksInstaller.ResizeWorkerPool(ctx, cluster.Name, poolName, originalSize); err != nil {
			return fmt.Errorf("restore worker pool %s: %w", poolName, err)
		}

		restoredCount++
	}

	log.Printf("Successfully restored %d worker pools to original sizes", restoredCount)

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("IKS cluster %s resumed successfully (%d workers restored)", cluster.Name, totalWorkers)
	return nil
}
