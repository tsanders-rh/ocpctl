package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// DestroyHandler handles cluster destruction jobs
type DestroyHandler struct {
	config *Config
	store  *store.Store
}

// NewDestroyHandler creates a new destroy handler
func NewDestroyHandler(config *Config, st *store.Store) *DestroyHandler {
	return &DestroyHandler{
		config: config,
		store:  st,
	}
}

// Handle handles a cluster destruction job
func (h *DestroyHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Destroying cluster %s (platform=%s)", cluster.Name, cluster.Platform)

	// Work directory should still exist from creation
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)

	// Check if work directory exists locally, if not try downloading from S3
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		log.Printf("Work directory %s not found locally, attempting to download from S3", workDir)

		// Try to download artifacts from S3
		artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
		if err != nil {
			log.Printf("Warning: failed to create artifact storage: %v", err)
			// Mark cluster as destroyed since we can't access artifacts
			if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
				return fmt.Errorf("mark cluster destroyed: %w", err)
			}
			return nil
		}

		if err := artifactStorage.DownloadClusterArtifacts(ctx, cluster.ID, workDir); err != nil {
			log.Printf("Warning: failed to download artifacts from S3: %v", err)
			// Mark cluster as destroyed since artifacts are not available
			if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
				return fmt.Errorf("mark cluster destroyed: %w", err)
			}
			return nil
		}

		log.Printf("Successfully downloaded artifacts from S3 for cluster %s", cluster.Name)
	}

	// Start log streaming before running openshift-install destroy
	logPath := filepath.Join(workDir, ".openshift_install.log")
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logPath)

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		log.Printf("Warning: failed to start log streaming: %v", err)
	}

	// Create version-specific installer for this cluster
	log.Printf("Creating installer for OpenShift version %s", cluster.Version)
	inst, err := installer.NewInstallerForVersion(cluster.Version)
	if err != nil {
		return fmt.Errorf("create installer for version %s: %w", cluster.Version, err)
	}

	// Run openshift-install destroy cluster with explicit timeout
	// Use 45-minute timeout to ensure destroy completes or fails definitively
	log.Printf("Running openshift-install destroy cluster for %s (version %s, timeout: 45m)", cluster.Name, cluster.Version)
	destroyCtx, destroyCancel := context.WithTimeout(ctx, 45*time.Minute)
	defer destroyCancel()

	output, err := inst.DestroyCluster(destroyCtx, workDir)

	// Stop log streaming after installer completes
	streamCancel()
	time.Sleep(500 * time.Millisecond) // Allow final batch to flush
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	// Handle destroy errors
	destroyFailed := false
	if err != nil {
		// Logs are already streamed to database
		if logData, readErr := os.ReadFile(logPath); readErr == nil {
			log.Printf("Destroy failed or timed out, logs:\n%s", string(logData))
		}

		// Check if this was a timeout
		if destroyCtx.Err() == context.DeadlineExceeded {
			log.Printf("ERROR: openshift-install destroy timed out after 45 minutes for %s", cluster.Name)
			log.Printf("This likely indicates AWS has too many IAM resources (500+) causing infinite scanning loop")
			log.Printf("Will mark cluster as destroyed and create orphan detection records")
			destroyFailed = true
		} else {
			// Don't fail the job if destroy encounters errors - infrastructure might already be gone
			log.Printf("Warning: openshift-install destroy cluster returned error: %v\nOutput: %s", err, output)
		}
	} else {
		log.Printf("Cluster %s destroyed successfully", cluster.Name)
	}

	// Store destroy log as artifact
	if err := h.storeDestroyLog(ctx, workDir, cluster.ID); err != nil {
		log.Printf("Warning: failed to store destroy log: %v", err)
	}

	// Platform-specific post-destruction cleanup
	if cluster.Platform == types.PlatformIBMCloud {
		// IBM Cloud requires CCO cleanup - delete service IDs created during installation
		log.Printf("Running IBM Cloud post-destruction cleanup (CCO service IDs)...")
		if err := h.HandleIBMCloudDestroy(ctx, cluster, inst, workDir); err != nil {
			// Don't fail the job - just log the warning
			log.Printf("Warning: IBM Cloud cleanup encountered issues: %v", err)
		}
	} else if cluster.Platform == types.PlatformAWS {
		// AWS requires CCO cleanup - delete IAM roles and OIDC provider created during installation
		log.Printf("Running AWS post-destruction cleanup (CCO IAM roles and OIDC provider)...")
		if err := h.HandleAWSDestroy(ctx, cluster, inst, workDir); err != nil {
			// Don't fail the job - just log the warning
			log.Printf("Warning: AWS cleanup encountered issues: %v", err)
		}
	}

	// Clean up work directory
	if err := os.RemoveAll(workDir); err != nil {
		log.Printf("Warning: failed to clean up work directory %s: %v", workDir, err)
	}

	// Clean up artifacts from S3
	log.Printf("Cleaning up artifacts from S3 for cluster %s", cluster.Name)
	artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
	if err != nil {
		log.Printf("Warning: failed to create artifact storage for cleanup: %v", err)
	} else {
		if err := artifactStorage.DeleteClusterArtifacts(ctx, cluster.ID); err != nil {
			log.Printf("Warning: failed to delete artifacts from S3: %v", err)
		} else {
			log.Printf("Successfully deleted artifacts from S3 for cluster %s", cluster.Name)
		}
	}

	// Clean up temporary files created by openshift-install
	h.cleanupTempFiles(cluster.ID)

	// Mark cluster as destroyed in database
	if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
		return fmt.Errorf("mark cluster destroyed: %w", err)
	}

	log.Printf("Cluster %s is now DESTROYED", cluster.Name)

	// Return error if destroy timed out - job should be marked as FAILED
	if destroyFailed {
		return fmt.Errorf("destroy operation timed out after 45 minutes - likely due to AWS IAM resource scanning issues. Cluster marked as DESTROYED but manual cleanup may be required for orphaned IAM roles and OIDC provider")
	}

	return nil
}

// storeDestroyLog stores the destroy operation log
func (h *DestroyHandler) storeDestroyLog(ctx context.Context, workDir, clusterID string) error {
	logPath := filepath.Join(workDir, ".openshift_install.log")

	stat, err := os.Stat(logPath)
	if err != nil {
		return err
	}

	size := stat.Size()
	artifact := &types.ClusterArtifact{
		ID:           fmt.Sprintf("%s-destroy-log", clusterID),
		ClusterID:    clusterID,
		ArtifactType: types.ArtifactTypeDestroyLog,
		S3URI:        fmt.Sprintf("file://%s", logPath),
		SizeBytes:    &size,
	}

	return h.store.Artifacts.Create(ctx, artifact)
}

// cleanupTempFiles removes temporary files created by openshift-install
func (h *DestroyHandler) cleanupTempFiles(clusterID string) {
	tmpDir := os.Getenv("TMPDIR")
	if tmpDir == "" {
		tmpDir = "/tmp"
	}

	// Pattern: openshift-install creates files like:
	// - /tmp/openshift-install-bootstrap-*.ign
	// - /tmp/openshift-cluster-api-system-components*
	// We can't match by cluster ID, so we clean up old temp files (>1 hour old)

	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		log.Printf("Warning: failed to read temp directory %s: %v", tmpDir, err)
		return
	}

	now := time.Now()
	for _, entry := range entries {
		// Only clean openshift-related temp files
		if !entry.IsDir() && !filepath.HasPrefix(entry.Name(), "openshift-") {
			continue
		}
		if entry.IsDir() && !filepath.HasPrefix(entry.Name(), "openshift-") {
			continue
		}

		fullPath := filepath.Join(tmpDir, entry.Name())
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		// Remove files/dirs older than 1 hour
		if now.Sub(info.ModTime()) > 1*time.Hour {
			if err := os.RemoveAll(fullPath); err != nil {
				log.Printf("Warning: failed to remove temp file %s: %v", fullPath, err)
			} else {
				log.Printf("Cleaned up old temp file: %s", fullPath)
			}
		}
	}
}
