package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ResumeHandler handles cluster resume jobs
type ResumeHandler struct {
	config *Config
	store  *store.Store
}

// NewResumeHandler creates a new resume handler
func NewResumeHandler(cfg *Config, st *store.Store) *ResumeHandler {
	return &ResumeHandler{
		config: cfg,
		store:  st,
	}
}

// Handle handles a cluster resume job
func (h *ResumeHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Resuming cluster %s (platform=%s)", cluster.Name, cluster.Platform)

	switch cluster.Platform {
	case types.PlatformAWS:
		return h.resumeAWS(ctx, cluster, job)
	case types.PlatformIBMCloud:
		return fmt.Errorf("resume not supported for platform %s - cluster was destroyed", cluster.Platform)
	default:
		return fmt.Errorf("unsupported platform for resume: %s", cluster.Platform)
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

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	if err := waiter.Wait(waitCtx, waitInput, 10*time.Minute); err != nil {
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
