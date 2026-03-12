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
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
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

	log.Printf("Hibernating cluster %s (platform=%s)", cluster.Name, cluster.Platform)

	switch cluster.Platform {
	case types.PlatformAWS:
		return h.hibernateAWS(ctx, cluster, job)
	case types.PlatformIBMCloud:
		return h.fallbackToDestroy(ctx, cluster, job)
	default:
		return fmt.Errorf("unsupported platform for hibernation: %s", cluster.Platform)
	}
}

// hibernateAWS hibernates an AWS cluster by stopping all EC2 instances
func (h *HibernateHandler) hibernateAWS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Hibernating AWS cluster %s by stopping EC2 instances", cluster.Name)

	// Get infraID from metadata.json
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
