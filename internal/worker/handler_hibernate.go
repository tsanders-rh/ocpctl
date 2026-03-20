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
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// HibernateHandler handles cluster hibernation jobs
type HibernateHandler struct {
	config *Config
	store  *store.Store
}

// NewHibernateHandler creates a new hibernate handler
func NewHibernateHandler(cfg *Config, st *store.Store) *HibernateHandler {
	return &HibernateHandler{
		config: cfg,
		store:  st,
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
	default:
		return fmt.Errorf("unsupported cluster type for hibernation: %s", cluster.ClusterType)
	}
}

// hibernateOpenShift hibernates an OpenShift cluster (AWS or IBMCloud)
func (h *HibernateHandler) hibernateOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	switch cluster.Platform {
	case types.PlatformAWS:
		return h.hibernateAWS(ctx, cluster, job)
	case types.PlatformIBMCloud:
		return h.fallbackToDestroy(ctx, cluster, job)
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
		log.Printf("No node groups found for EKS cluster %s", cluster.Name)
		// Update cluster status to HIBERNATED even if no node groups
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
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

	// Create IKS installer
	iksInstaller := installer.NewIKSInstaller()

	// Get IBM Cloud API key from environment
	apiKey := os.Getenv("IBMCLOUD_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("IBMCLOUD_API_KEY environment variable not set")
	}

	// Login to IBM Cloud
	if err := iksInstaller.Login(ctx, apiKey, cluster.Region); err != nil {
		return fmt.Errorf("IBM Cloud login: %w", err)
	}

	// Get cluster info to find current worker count
	info, err := iksInstaller.GetClusterInfo(ctx, cluster.Name)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	// TODO: Get actual worker count from IKS API
	// For now, store a placeholder - this would need the actual IBM Cloud SDK
	// to query worker pools and get current worker counts

	log.Printf("Warning: IKS worker pool scaling requires IBM Cloud Kubernetes Service API")
	log.Printf("Current implementation limitation: Cannot programmatically scale IKS workers to 0")
	log.Printf("Cluster info: %+v", info)

	// Store cluster ID in job metadata for resume
	// Note: Metadata will be saved when the job completes via MarkSucceeded
	if job.Metadata == nil {
		job.Metadata = make(types.JobMetadata)
	}
	job.Metadata["cluster_id"] = info.ID
	job.Metadata["original_worker_count"] = "2" // Placeholder - should query actual count
	log.Printf("Stored cluster info in job metadata for resume")

	// Update cluster status to HIBERNATED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	log.Printf("IKS cluster %s marked as hibernated (Note: actual worker scaling not yet implemented)", cluster.Name)
	return nil
}

// int32Ptr returns a pointer to an int32
func int32Ptr(i int32) *int32 {
	return &i
}
