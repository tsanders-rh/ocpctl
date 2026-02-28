package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// DestroyHandler handles cluster destruction jobs
type DestroyHandler struct {
	config    *Config
	store     *store.Store
	installer *installer.Installer
}

// NewDestroyHandler creates a new destroy handler
func NewDestroyHandler(config *Config, st *store.Store) *DestroyHandler {
	return &DestroyHandler{
		config:    config,
		store:     st,
		installer: installer.NewInstaller(),
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

	// Check if work directory exists
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		log.Printf("Warning: work directory %s not found, cluster may already be destroyed", workDir)

		// Mark cluster as destroyed anyway
		if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
			return fmt.Errorf("mark cluster destroyed: %w", err)
		}

		return nil
	}

	// Run openshift-install destroy cluster
	log.Printf("Running openshift-install destroy cluster for %s", cluster.Name)

	output, err := h.installer.DestroyCluster(ctx, workDir)
	if err != nil {
		// Store error logs
		logPath := filepath.Join(workDir, ".openshift_install.log")
		if logData, readErr := os.ReadFile(logPath); readErr == nil {
			log.Printf("Destroy failed, logs:\n%s", string(logData))
		}

		// Don't fail the job if destroy encounters errors - infrastructure might already be gone
		log.Printf("Warning: openshift-install destroy cluster returned error: %v\nOutput: %s", err, output)
	} else {
		log.Printf("Cluster %s destroyed successfully", cluster.Name)
	}

	// Store destroy log as artifact
	if err := h.storeDestroyLog(ctx, workDir, cluster.ID); err != nil {
		log.Printf("Warning: failed to store destroy log: %v", err)
	}

	// Clean up work directory
	if err := os.RemoveAll(workDir); err != nil {
		log.Printf("Warning: failed to clean up work directory %s: %v", workDir, err)
	}

	// Mark cluster as destroyed in database
	if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
		return fmt.Errorf("mark cluster destroyed: %w", err)
	}

	log.Printf("Cluster %s is now DESTROYED", cluster.Name)

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
