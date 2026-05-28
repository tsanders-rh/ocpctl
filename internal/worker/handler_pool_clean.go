package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

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

	log.Printf("Cluster %s cleanup completed successfully", cluster.Name)
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

// markClusterExpired marks a cluster as EXPIRED when cleanup cannot be performed
func (h *PoolCleanHandler) markClusterExpired(ctx context.Context, cluster *types.Cluster, reason string) error {
	log.Printf("Marking cluster %s as EXPIRED: %s", cluster.Name, reason)

	expiredState := types.PoolStateExpired
	if err := h.store.Clusters.UpdatePoolState(ctx, cluster.ID, expiredState); err != nil {
		return fmt.Errorf("failed to update pool state to EXPIRED: %w", err)
	}

	return nil
}
