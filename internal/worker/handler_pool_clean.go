package worker

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/k8s"
	"github.com/tsanders-rh/ocpctl/internal/s3"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
	"golang.org/x/crypto/bcrypt"
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
		// A transient store error is not evidence the cluster is unhealthy. Fail
		// the job so it retries rather than marking a healthy cluster EXPIRED.
		return fmt.Errorf("get cluster outputs for %s: %w", cluster.Name, err)
	}

	// A cluster with no kubeconfig reference at all cannot be sanitized and must
	// not be returned to the pool. Mark it EXPIRED; the scheduler will destroy and
	// replace it (see checkExpiredClustersForDestroy).
	if outputs.KubeconfigS3URI == nil || *outputs.KubeconfigS3URI == "" {
		return h.markClusterExpired(ctx, cluster, "missing kubeconfig URI")
	}

	// Resolve the kubeconfig to a local file, downloading from S3 when needed.
	kubeconfigPath, cleanupKubeconfig, err := h.resolveKubeconfig(ctx, *outputs.KubeconfigS3URI)
	if err != nil {
		// Fetching the kubeconfig failed (transient S3 error, or the artifact is
		// only on another worker's local disk). This is an infra/placement issue,
		// not evidence the cluster is unhealthy — fail the job so it retries
		// instead of destroying a healthy cluster. Previously this path marked the
		// cluster EXPIRED, which silently drained pools on autoscale workers where
		// cleanup runs on a host that lacks the local file:// kubeconfig and the
		// s3:// case was rejected outright.
		return fmt.Errorf("resolve kubeconfig for cluster %s: %w", cluster.Name, err)
	}
	defer cleanupKubeconfig()

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

		// Rotate kubeadmin password for security (prevents previous leasees from accessing)
		if err := h.rotateKubeadminPassword(ctx, cluster, kubeconfigPath, outputs); err != nil {
			log.Printf("Warning: Failed to rotate kubeadmin password for cluster %s: %v", cluster.Name, err)
			// Don't fail the cleanup job - cluster can still be used with old password
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

// resolveKubeconfig resolves a kubeconfig URI to a readable local file path.
//
// s3:// URIs are downloaded to a temporary file (the common case for pool
// clusters, whose kubeconfig is uploaded to S3 so it can be served from any
// host — see storeArtifacts in handler_create.go). file:// URIs and bare paths
// are used in place. The returned cleanup func removes any temp file and is
// always safe to call (no-op when nothing was downloaded).
func (h *PoolCleanHandler) resolveKubeconfig(ctx context.Context, kubeconfigURI string) (string, func(), error) {
	noop := func() {}

	switch {
	case strings.HasPrefix(kubeconfigURI, "s3://"):
		bucket, key, err := s3.ParseS3URI(kubeconfigURI)
		if err != nil {
			return "", noop, fmt.Errorf("invalid kubeconfig S3 URI %q: %w", kubeconfigURI, err)
		}
		data, err := s3.DownloadFile(ctx, bucket, key)
		if err != nil {
			return "", noop, fmt.Errorf("download kubeconfig from %s: %w", kubeconfigURI, err)
		}
		tmp, err := os.CreateTemp("", "pool-clean-kubeconfig-*.yaml")
		if err != nil {
			return "", noop, fmt.Errorf("create temp kubeconfig: %w", err)
		}
		cleanup := func() { _ = os.Remove(tmp.Name()) }
		if _, err := tmp.Write(data); err != nil {
			_ = tmp.Close()
			cleanup()
			return "", noop, fmt.Errorf("write temp kubeconfig: %w", err)
		}
		if err := tmp.Close(); err != nil {
			cleanup()
			return "", noop, fmt.Errorf("close temp kubeconfig: %w", err)
		}
		return tmp.Name(), cleanup, nil

	case strings.HasPrefix(kubeconfigURI, "file://"):
		path := strings.TrimPrefix(kubeconfigURI, "file://")
		if _, err := os.Stat(path); err != nil {
			return "", noop, fmt.Errorf("local kubeconfig %s not accessible: %w", path, err)
		}
		return path, noop, nil

	default:
		// Assume it's a direct file path.
		if _, err := os.Stat(kubeconfigURI); err != nil {
			return "", noop, fmt.Errorf("kubeconfig %s not accessible: %w", kubeconfigURI, err)
		}
		return kubeconfigURI, noop, nil
	}
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

	// Update existing outputs record with new ServiceAccount credentials
	// Use all fields from existing record to avoid nulling out other fields
	updatedOutputs := &types.ClusterOutputs{
		ID:                 outputs.ID, // Preserve existing ID for upsert
		ClusterID:          cluster.ID,
		APIURL:             outputs.APIURL,
		ConsoleURL:         outputs.ConsoleURL,
		KubeconfigS3URI:    outputs.KubeconfigS3URI,
		KubeadminSecretRef: outputs.KubeadminSecretRef,
		MetadataS3URI:      outputs.MetadataS3URI,
		DashboardToken:     outputs.DashboardToken,
		SAName:             &creds.SAName,
		SANamespace:        &creds.SANamespace,
		SAToken:            &creds.Token,
		SATokenExpiresAt:   &creds.TokenExpiresAt,
		OcLoginCommand:     &ocLoginCmd,
	}

	// Upsert to update existing record
	if err := h.store.ClusterOutputs.Upsert(ctx, updatedOutputs); err != nil {
		return fmt.Errorf("store ServiceAccount credentials: %w", err)
	}

	log.Printf("ServiceAccount recreated and credentials stored: sa_name=%s, expires_at=%s", creds.SAName, creds.TokenExpiresAt)
	return nil
}

// rotateKubeadminPassword generates a new kubeadmin password and updates the cluster
func (h *PoolCleanHandler) rotateKubeadminPassword(ctx context.Context, cluster *types.Cluster, kubeconfigPath string, outputs *types.ClusterOutputs) error {
	log.Printf("Rotating kubeadmin password for pool cluster %s", cluster.Name)

	// Generate new random password (23 characters, alphanumeric)
	newPassword, err := generateRandomPassword(23)
	if err != nil {
		return fmt.Errorf("generate random password: %w", err)
	}

	// Hash password with bcrypt for OpenShift
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// Determine which CLI to use based on cluster type
	cli := "kubectl"
	if cluster.ClusterType == types.ClusterTypeOpenShift {
		cli = "oc"
	}

	// Update kubeadmin secret in kube-system namespace
	// The secret contains the bcrypt-hashed password
	updateCmd := exec.CommandContext(ctx, cli, "--kubeconfig", kubeconfigPath,
		"patch", "secret", "kubeadmin", "-n", "kube-system",
		"--type=json",
		"-p", fmt.Sprintf(`[{"op":"replace","path":"/data/kubeadmin","value":"%s"}]`, base64.StdEncoding.EncodeToString(hashedPassword)))

	if output, err := updateCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("update kubeadmin secret: %w (output: %s)", err, string(output))
	}

	log.Printf("Updated kubeadmin secret in cluster %s", cluster.Name)

	// Persist the new password to wherever KubeadminSecretRef points so the API
	// (which reads it back to display console credentials) doesn't serve a stale
	// value. For OpenShift IPI this is now an s3:// URI; legacy clusters may still
	// carry a worker-local file:// path. A failure here is non-fatal: the
	// in-cluster secret is already rotated.
	if outputs.KubeadminSecretRef != nil && *outputs.KubeadminSecretRef != "" {
		ref := *outputs.KubeadminSecretRef
		switch {
		case strings.HasPrefix(ref, "s3://"):
			bucket, key, err := s3.ParseS3URI(ref)
			if err != nil {
				log.Printf("Warning: invalid kubeadmin s3 uri %q: %v", ref, err)
			} else if client, err := s3.NewClient(ctx); err != nil {
				log.Printf("Warning: failed to create s3 client for kubeadmin update: %v", err)
			} else if err := client.UploadFile(ctx, bucket, key, []byte(newPassword)); err != nil {
				log.Printf("Warning: failed to update kubeadmin-password in s3: %v", err)
			} else {
				log.Printf("Updated kubeadmin-password in s3 at %s", ref)
			}
		case strings.HasPrefix(ref, "file://") || strings.HasPrefix(ref, "/"):
			passwordPath := strings.TrimPrefix(ref, "file://")
			if err := os.WriteFile(passwordPath, []byte(newPassword), 0600); err != nil {
				log.Printf("Warning: Failed to update kubeadmin-password file: %v", err)
			} else {
				log.Printf("Updated kubeadmin-password file at %s", passwordPath)
			}
		}
	}

	log.Printf("Successfully rotated kubeadmin password for cluster %s", cluster.Name)
	return nil
}

// generateRandomPassword generates a random alphanumeric password of the specified length
func generateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = charset[int(b[i])%len(charset)]
	}
	return string(b), nil
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
