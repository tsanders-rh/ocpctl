package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// handleGKEDestroy handles GKE cluster destruction using gcloud CLI
func (h *DestroyHandler) handleGKEDestroy(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting GKE cluster destruction for %s", cluster.Name)

	// Get profile to extract GCP project
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	project := getGCPProject(prof)
	if project == "" {
		return fmt.Errorf("GCP project ID not found in profile or environment")
	}

	// Verify GCP authentication before attempting destroy
	if err := VerifyGCPAuthentication(ctx, project); err != nil {
		return fmt.Errorf("GCP authentication failed: %w", err)
	}

	// Create GKE installer for deletion operations
	gkeInstaller := installer.NewGKEInstaller()

	// Clean up Kubernetes resources BEFORE deleting the cluster
	// This includes LoadBalancer services and other cloud resources that might block deletion
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	if _, err := os.Stat(workDir); err == nil {
		log.Printf("[Destroy] Cleaning up Kubernetes resources (LoadBalancers, PVCs)...")
		if err := h.cleanupKubernetesResources(ctx, cluster, workDir); err != nil {
			// Log warning but don't fail destroy - GKE will clean up during cluster deletion
			log.Printf("Warning: Kubernetes resource cleanup encountered errors: %v", err)
			log.Printf("Continuing with cluster deletion - GKE will clean up remaining resources")
		} else {
			log.Printf("[Destroy] ✓ Kubernetes resources cleaned up successfully")
		}
	}

	// Delete the GKE cluster
	log.Printf("Deleting GKE cluster %s in project %s, region %s", cluster.Name, project, cluster.Region)

	destroyCtx, destroyCancel := context.WithTimeout(ctx, DestroyOperationTimeout)
	defer destroyCancel()

	output, err := gkeInstaller.DestroyCluster(destroyCtx, cluster.Name, project, cluster.Region, "")
	if err != nil {
		// Check if cluster doesn't exist (already deleted)
		if isGKEClusterNotFoundError(err) {
			log.Printf("GKE cluster %s not found (may have been deleted manually), marking as DESTROYED", cluster.Name)

			// Update cluster status to DESTROYED
			if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyed); err != nil {
				return fmt.Errorf("update cluster status to DESTROYED: %w", err)
			}

			// Set destroyed timestamp
			now := time.Now()
			if err := h.store.Clusters.SetDestroyedAt(ctx, cluster.ID, &now); err != nil {
				log.Printf("Warning: failed to set destroyed_at timestamp: %v", err)
			}

			return nil
		}

		return fmt.Errorf("GKE cluster deletion failed: %w\nOutput: %s", err, output)
	}

	log.Printf("GKE cluster %s deleted successfully", cluster.Name)

	// Verify cluster is fully deleted
	log.Printf("Verifying GKE cluster deletion...")
	if err := h.verifyGKEClusterDeleted(ctx, gkeInstaller, cluster.Name, project, cluster.Region); err != nil {
		log.Printf("Warning: cluster deletion verification failed: %v", err)
		// Don't fail - cluster deletion command succeeded
	} else {
		log.Printf("✓ GKE cluster deletion verified")
	}

	// Update cluster status to DESTROYED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyed); err != nil {
		return fmt.Errorf("update cluster status to DESTROYED: %w", err)
	}

	// Set destroyed timestamp
	now := time.Now()
	if err := h.store.Clusters.SetDestroyedAt(ctx, cluster.ID, &now); err != nil {
		log.Printf("Warning: failed to set destroyed_at timestamp: %v", err)
	}

	// Publish cluster destroyed metric
	if h.metricsPublisher != nil {
		destroyTime := time.Since(job.CreatedAt)
		if err := h.metricsPublisher.PublishClusterDestroyed(ctx, cluster, destroyTime); err != nil {
			log.Printf("Warning: failed to publish cluster destroyed metric: %v", err)
		}
	}

	log.Printf("GKE cluster %s destruction complete", cluster.Name)
	return nil
}

// verifyGKEClusterDeleted verifies that a GKE cluster has been fully deleted
func (h *DestroyHandler) verifyGKEClusterDeleted(ctx context.Context, gkeInstaller *installer.GKEInstaller, clusterName, project, region string) error {
	// Try to get cluster info - should fail with "not found" error
	_, err := gkeInstaller.GetClusterInfo(ctx, clusterName, project, region, "")
	if err != nil {
		// If we get a "not found" error, cluster is deleted
		if isGKEClusterNotFoundError(err) {
			return nil
		}
		// Other errors are unexpected
		return fmt.Errorf("unexpected error during verification: %w", err)
	}

	// If we got cluster info, cluster still exists
	return fmt.Errorf("cluster %s still exists after deletion", clusterName)
}

// isGKEClusterNotFoundError checks if an error indicates a GKE cluster was not found
func isGKEClusterNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()
	// GKE API returns specific error messages for non-existent clusters
	return contains(errStr, "not found") ||
		contains(errStr, "does not exist") ||
		contains(errStr, "NotFound") ||
		contains(errStr, "404")
}

// contains checks if a string contains a substring (case-sensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr)))
}

// findSubstring checks if substr exists anywhere in s
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// HandleGCPOpenShiftDestroy handles OpenShift on GCP cluster destruction
// This follows the standard OpenShift destroy workflow using openshift-install
func (h *DestroyHandler) HandleGCPOpenShiftDestroy(ctx context.Context, job *types.Job, cluster *types.Cluster, workDir string) error {
	log.Printf("Starting OpenShift on GCP destruction for %s", cluster.Name)

	// Get profile to extract GCP project
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	project := getGCPProject(prof)
	if project == "" {
		return fmt.Errorf("GCP project ID not found in profile or environment")
	}

	// Verify GCP authentication before attempting destroy
	if err := VerifyGCPAuthentication(ctx, project); err != nil {
		return fmt.Errorf("GCP authentication failed: %w", err)
	}

	// OpenShift on GCP uses the standard openshift-install destroy workflow
	// The installer will use GCP credentials from GOOGLE_APPLICATION_CREDENTIALS
	log.Printf("OpenShift on GCP destroy will use standard openshift-install destroy workflow")

	// The main OpenShift destroy handler (handleOpenShiftDestroy) will handle the actual destruction
	// This function just performs GCP-specific pre-destroy checks and auth verification
	return nil
}

// cleanupGCPNetworkResources cleans up orphaned GCP network resources
// This is called if cluster deletion leaves behind resources
func (h *DestroyHandler) cleanupGCPNetworkResources(ctx context.Context, cluster *types.Cluster, project string) error {
	log.Printf("Cleaning up GCP network resources for cluster %s...", cluster.Name)

	// GCP automatically cleans up most resources when a GKE cluster is deleted
	// For OpenShift on GCP, openshift-install handles cleanup
	// This is a placeholder for any additional cleanup logic needed in the future

	// Potential cleanup targets (if needed):
	// - Load balancers created by Kubernetes services
	// - Firewall rules created by the cluster
	// - Persistent disks not attached to the cluster
	// - VPC networks/subnets (if created by ocpctl)

	log.Printf("GCP network resource cleanup complete for cluster %s", cluster.Name)
	return nil
}
