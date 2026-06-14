package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/k8s"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PoolCleanHandler handles cluster cleaning/sanitization for pools
type PoolCleanHandler struct {
	config *Config
	store  *store.Store
}

// NewPoolCleanHandler creates a new pool clean handler
func NewPoolCleanHandler(config *Config, st *store.Store) *PoolCleanHandler {
	return &PoolCleanHandler{
		config: config,
		store:  st,
	}
}

// Handle processes a pool cluster cleaning job
func (h *PoolCleanHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("failed to get cluster: %w", err)
	}

	log.Printf("Cleaning cluster %s (pool_id=%v, pool_state=%v)",
		cluster.Name, cluster.PoolID, cluster.PoolState)

	// Verify cluster is in CLEANING state
	if cluster.PoolState == nil || *cluster.PoolState != types.PoolStateCleaning {
		return fmt.Errorf("cluster %s is not in CLEANING state (current: %v)", cluster.Name, cluster.PoolState)
	}

	// Verify cluster status is READY
	if cluster.Status != types.ClusterStatusReady {
		log.Printf("Cluster %s is not in READY status (current: %s), marking as EXPIRED", cluster.Name, cluster.Status)
		// If cluster is not healthy, mark as EXPIRED instead of READY
		expiredState := types.PoolStateExpired
		if err := h.store.Clusters.UpdatePoolState(ctx, cluster.ID, expiredState); err != nil {
			return fmt.Errorf("failed to update pool state: %w", err)
		}
		return nil
	}

	log.Printf("Starting cluster cleanup for %s", cluster.Name)

	// Get cluster outputs to retrieve kubeconfig
	outputs, err := h.store.ClusterOutputs.GetByClusterID(ctx, cluster.ID)
	if err != nil {
		log.Printf("Warning: Could not get cluster outputs for %s: %v", cluster.Name, err)
		// Continue without cleanup - mark as EXPIRED instead of READY
		return h.markClusterExpired(ctx, cluster, "missing cluster outputs")
	}

	// Get kubeconfig path
	kubeconfigPath, err := h.getKubeconfigPath(outputs)
	if err != nil {
		log.Printf("Warning: Could not get kubeconfig path for %s: %v", cluster.Name, err)
		return h.markClusterExpired(ctx, cluster, "missing kubeconfig")
	}

	// Verify kubeconfig exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		log.Printf("Warning: Kubeconfig not found at %s for cluster %s", kubeconfigPath, cluster.Name)
		return h.markClusterExpired(ctx, cluster, "kubeconfig not found")
	}

	log.Printf("Using kubeconfig: %s", kubeconfigPath)

	// Perform cluster cleanup
	if err := h.cleanupCluster(ctx, cluster, kubeconfigPath); err != nil {
		log.Printf("Error cleaning cluster %s: %v", cluster.Name, err)
		// Don't mark as EXPIRED - retry the cleanup job
		return fmt.Errorf("cleanup failed: %w", err)
	}

	log.Printf("Cluster %s cleanup completed successfully", cluster.Name)

	// Recreate ServiceAccount credentials for pool clusters
	if cluster.PoolID != nil {
		if err := h.recreateServiceAccount(ctx, cluster, kubeconfigPath, outputs); err != nil {
			log.Printf("Warning: Failed to recreate ServiceAccount for cluster %s: %v", cluster.Name, err)
			// Don't fail the cleanup job - cluster can still be used, just without SA credentials
		}
	}

	// Reset cluster metadata and mark as READY
	now := time.Now()
	updates := map[string]interface{}{
		"pool_state":       types.PoolStateReady,
		"leased_by":        nil,
		"leased_at":        nil,
		"lease_expires_at": nil,
		"lease_metadata":   types.JobMetadata{},
		"pool_generation":  cluster.PoolGeneration + 1, // Increment generation
		"last_cleaned_at":  &now,
	}

	if err := h.store.Clusters.Update(ctx, cluster.ID, updates); err != nil {
		return fmt.Errorf("failed to update cluster state: %w", err)
	}

	log.Printf("Successfully cleaned cluster %s, marked as READY for pool (generation=%d)",
		cluster.Name, cluster.PoolGeneration+1)

	return nil
}

// getKubeconfigPath extracts the kubeconfig file path from cluster outputs
func (h *PoolCleanHandler) getKubeconfigPath(outputs *types.ClusterOutputs) (string, error) {
	if outputs.KubeconfigS3URI == nil || *outputs.KubeconfigS3URI == "" {
		return "", fmt.Errorf("kubeconfig URI not set")
	}

	kubeconfigURI := *outputs.KubeconfigS3URI

	// Handle file:// URIs (local filesystem)
	if strings.HasPrefix(kubeconfigURI, "file://") {
		return kubeconfigURI[7:], nil // Remove "file://" prefix
	}

	// Handle s3:// URIs (future enhancement - download from S3)
	if strings.HasPrefix(kubeconfigURI, "s3://") {
		return "", fmt.Errorf("s3:// URIs not yet supported for cleanup")
	}

	// Assume it's a direct file path
	return kubeconfigURI, nil
}

// cleanupCluster performs the actual cluster cleanup operations
func (h *PoolCleanHandler) cleanupCluster(ctx context.Context, cluster *types.Cluster, kubeconfigPath string) error {
	log.Printf("Cleaning cluster %s using kubeconfig %s", cluster.Name, kubeconfigPath)

	// Delete ServiceAccount for pool clusters (before other cleanup)
	if cluster.PoolID != nil {
		log.Printf("Deleting ServiceAccount for pool cluster %s", cluster.Name)
		saManager, err := k8s.NewServiceAccountManager(kubeconfigPath)
		if err != nil {
			log.Printf("Warning: Failed to init ServiceAccount manager for cleanup: %v", err)
			// Continue with cluster cleanup anyway
		} else {
			if err := saManager.DeletePoolLeaseServiceAccount(ctx, cluster.Name); err != nil {
				log.Printf("Warning: Failed to delete ServiceAccount: %v", err)
				// Continue with cluster cleanup anyway
			} else {
				log.Printf("Successfully deleted ServiceAccount for cluster %s", cluster.Name)
			}
		}
	}

	// Determine which CLI to use based on cluster type
	cli := "kubectl"
	if cluster.ClusterType == types.ClusterTypeOpenShift {
		cli = "oc"
	}

	// 1. Delete user-created namespaces (excluding system namespaces)
	if err := h.deleteUserNamespaces(ctx, cli, kubeconfigPath, cluster); err != nil {
		return fmt.Errorf("failed to delete user namespaces: %w", err)
	}

	// 2. Clean up resources in default namespace
	if err := h.cleanDefaultNamespace(ctx, cli, kubeconfigPath, cluster); err != nil {
		return fmt.Errorf("failed to clean default namespace: %w", err)
	}

	// 3. Clean up resources in openshift namespace (OpenShift only)
	if cluster.ClusterType == types.ClusterTypeOpenShift {
		if err := h.cleanOpenshiftNamespace(ctx, cli, kubeconfigPath, cluster); err != nil {
			log.Printf("Warning: Failed to clean openshift namespace: %v", err)
			// Don't fail cleanup if this fails
		}
	}

	return nil
}

// deleteUserNamespaces deletes all user-created namespaces
func (h *PoolCleanHandler) deleteUserNamespaces(ctx context.Context, cli, kubeconfigPath string, cluster *types.Cluster) error {
	log.Printf("Deleting user-created namespaces from cluster %s", cluster.Name)

	// Get all namespaces
	cmd := exec.CommandContext(ctx, cli, "--kubeconfig", kubeconfigPath, "get", "namespaces", "-o", "jsonpath={.items[*].metadata.name}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %w (output: %s)", err, string(output))
	}

	namespaces := strings.Fields(string(output))
	deletedCount := 0

	for _, ns := range namespaces {
		// Skip system namespaces:
		// - default, kube-* (Kubernetes core)
		// - openshift-* (OpenShift platform and operators)
		if ns == "default" || strings.HasPrefix(ns, "kube-") || strings.HasPrefix(ns, "openshift-") {
			continue
		}

		log.Printf("Deleting namespace: %s", ns)
		deleteCmd := exec.CommandContext(ctx, cli, "--kubeconfig", kubeconfigPath, "delete", "namespace", ns, "--timeout=60s")
		if output, err := deleteCmd.CombinedOutput(); err != nil {
			log.Printf("Warning: Failed to delete namespace %s: %v (output: %s)", ns, err, string(output))
			// Continue with other namespaces
		} else {
			deletedCount++
		}
	}

	log.Printf("Deleted %d user-created namespace(s) from cluster %s", deletedCount, cluster.Name)
	return nil
}

// cleanDefaultNamespace cleans up resources in the default namespace
func (h *PoolCleanHandler) cleanDefaultNamespace(ctx context.Context, cli, kubeconfigPath string, cluster *types.Cluster) error {
	log.Printf("Cleaning default namespace in cluster %s", cluster.Name)

	// Resource types to clean up
	resourceTypes := []string{
		"pods",
		"deployments",
		"statefulsets",
		"daemonsets",
		"replicasets",
		"jobs",
		"cronjobs",
		"services",
		"configmaps",
		"secrets",
		"persistentvolumeclaims",
		"ingresses",
		"routes", // OpenShift
	}

	for _, resourceType := range resourceTypes {
		// Delete all resources of this type (except system resources)
		cmd := exec.CommandContext(ctx, cli, "--kubeconfig", kubeconfigPath, "delete", resourceType, "--all", "-n", "default", "--timeout=60s")
		if output, err := cmd.CombinedOutput(); err != nil {
			// Some resource types may not exist (like routes on non-OpenShift)
			if !strings.Contains(string(output), "not found") && !strings.Contains(string(output), "no resources found") {
				log.Printf("Warning: Failed to delete %s in default namespace: %v (output: %s)", resourceType, err, string(output))
			}
		} else {
			log.Printf("Cleaned %s from default namespace", resourceType)
		}
	}

	return nil
}

// cleanOpenshiftNamespace cleans up resources in the openshift namespace (OpenShift only)
func (h *PoolCleanHandler) cleanOpenshiftNamespace(ctx context.Context, cli, kubeconfigPath string, cluster *types.Cluster) error {
	log.Printf("Cleaning openshift namespace in cluster %s", cluster.Name)

	// Only clean up user-created resources, not system resources
	// For now, we'll skip this as it's risky to delete things from the openshift namespace
	// Most user workloads should be in other namespaces anyway

	return nil
}

// recreateServiceAccount recreates ServiceAccount credentials after cleaning
func (h *PoolCleanHandler) recreateServiceAccount(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, outputs *types.ClusterOutputs) error {
	log.Printf("Recreating ServiceAccount for pool cluster %s", cluster.Name)

	// Get pool to determine default lease duration
	pool, err := h.store.Pools.GetByID(ctx, *cluster.PoolID)
	if err != nil {
		return fmt.Errorf("get pool for SA creation: %w", err)
	}

	// Initialize ServiceAccount manager
	saManager, err := k8s.NewServiceAccountManager(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("init ServiceAccount manager: %w", err)
	}

	// Create ServiceAccount with time-bound token
	// Token will be valid for the default lease duration
	creds, err := saManager.CreatePoolLeaseServiceAccount(ctx, cluster.Name, pool.DefaultLeaseDurationHours)
	if err != nil {
		return fmt.Errorf("create pool lease ServiceAccount: %w", err)
	}

	// Get API URL from existing outputs (should already exist)
	apiURL := ""
	if outputs.APIURL != nil {
		apiURL = *outputs.APIURL
	} else {
		return fmt.Errorf("API URL not found in cluster outputs")
	}

	// Generate oc login command
	ocLoginCmd := fmt.Sprintf("oc login %s --token=%s", apiURL, creds.Token)

	// Update cluster outputs with ServiceAccount credentials
	updatedOutputs := &types.ClusterOutputs{
		ClusterID:        cluster.ID,
		SAName:           &creds.SAName,
		SANamespace:      &creds.SANamespace,
		SAToken:          &creds.Token,
		SATokenExpiresAt: &creds.TokenExpiresAt,
		OcLoginCommand:   &ocLoginCmd,
	}

	// Upsert to update existing record
	if err := h.store.ClusterOutputs.Upsert(ctx, updatedOutputs); err != nil {
		return fmt.Errorf("store ServiceAccount credentials: %w", err)
	}

	log.Printf("ServiceAccount recreated and credentials stored: sa_name=%s, expires_at=%s", creds.SAName, creds.TokenExpiresAt)
	return nil
}

// markClusterExpired marks a cluster as EXPIRED when cleanup cannot be performed
func (h *PoolCleanHandler) markClusterExpired(ctx context.Context, cluster *types.Cluster, reason string) error {
	log.Printf("Marking cluster %s as EXPIRED: %s", cluster.Name, reason)

	expiredState := types.PoolStateExpired
	if err := h.store.Clusters.UpdatePoolState(ctx, cluster.ID, expiredState); err != nil {
		return fmt.Errorf("failed to update pool state to EXPIRED: %w", err)
	}

	return nil
}
