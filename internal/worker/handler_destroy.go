package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	log.Printf("Destroying cluster %s (platform=%s, cluster_type=%s)", cluster.Name, cluster.Platform, cluster.ClusterType)

	// Route to appropriate handler based on cluster type
	switch cluster.ClusterType {
	case types.ClusterTypeOpenShift:
		return h.handleOpenShiftDestroy(ctx, job, cluster)
	case types.ClusterTypeEKS:
		return h.handleEKSDestroy(ctx, job, cluster)
	case types.ClusterTypeIKS:
		return h.handleIKSDestroy(ctx, job, cluster)
	default:
		return fmt.Errorf("unsupported cluster type: %s", cluster.ClusterType)
	}
}

// handleOpenShiftDestroy handles OpenShift cluster destruction
func (h *DestroyHandler) handleOpenShiftDestroy(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting OpenShift cluster destruction for %s", cluster.Name)

	// Work directory should still exist from creation
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)

	// Check if work directory exists locally, if not try downloading from S3
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		log.Printf("Work directory %s not found locally, attempting to download from S3", workDir)

		// Try to download artifacts from S3
		artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
		if err != nil {
			log.Printf("ERROR: failed to create artifact storage: %v", err)
			// Cannot proceed with destroy without install directory - mark as DESTROY_FAILED
			// Resources may still exist in AWS and require manual cleanup
			if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyFailed); err != nil {
				return fmt.Errorf("mark cluster destroy failed: %w", err)
			}
			return fmt.Errorf("cannot destroy cluster without install directory: %w", err)
		}

		if err := artifactStorage.DownloadClusterArtifacts(ctx, cluster.ID, workDir); err != nil {
			log.Printf("ERROR: failed to download artifacts from S3: %v", err)
			// Cannot proceed with destroy without install directory - mark as DESTROY_FAILED
			// Resources may still exist in AWS and require manual cleanup
			if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyFailed); err != nil {
				return fmt.Errorf("mark cluster destroy failed: %w", err)
			}
			return fmt.Errorf("cannot destroy cluster without install directory: %w", err)
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
	destroySucceeded := true
	if err != nil {
		// Logs are already streamed to database
		if logData, readErr := os.ReadFile(logPath); readErr == nil {
			log.Printf("Destroy failed or timed out, logs:\n%s", string(logData))
		}

		// Check if this was a timeout
		if destroyCtx.Err() == context.DeadlineExceeded {
			log.Printf("ERROR: openshift-install destroy timed out after 45 minutes for %s", cluster.Name)
			log.Printf("This likely indicates AWS has too many IAM resources (500+) causing infinite scanning loop")
			log.Printf("Will mark cluster as DESTROY_FAILED - verification required before marking DESTROYED")
			destroySucceeded = false
		} else {
			// openshift-install failed with error - cannot confirm destruction
			log.Printf("ERROR: openshift-install destroy cluster failed: %v\nOutput: %s", err, output)
			destroySucceeded = false
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

	// Mark cluster status based on destroy result
	if !destroySucceeded {
		// Destroy failed or timed out - mark as DESTROY_FAILED
		// Resources may still exist in AWS and require manual verification
		log.Printf("Marking cluster %s as DESTROY_FAILED - destroy operation did not complete successfully", cluster.Name)
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyFailed); err != nil {
			return fmt.Errorf("mark cluster destroy failed: %w", err)
		}
		return fmt.Errorf("destroy operation failed - resources may still exist in AWS and require manual cleanup")
	}

	// Destroy succeeded - mark as DESTROYED
	// TODO: Add verification for OpenShift similar to EKS (check CloudFormation stacks, tagged resources)
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

// handleEKSDestroy handles EKS cluster destruction using reconciliation-based approach
func (h *DestroyHandler) handleEKSDestroy(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting EKS cluster destruction for %s using reconciliation", cluster.Name)

	// Create destroy audit record
	auditID := fmt.Sprintf("%s-destroy-%d", cluster.ID, time.Now().Unix())
	workerID := os.Getenv("HOSTNAME")
	if workerID == "" {
		workerID = "unknown"
	}

	destroyAudit := &types.DestroyAudit{
		ID:               auditID,
		ClusterID:        cluster.ID,
		JobID:            job.ID,
		WorkerID:         workerID,
		DestroyStartedAt: time.Now(),
		CreatedAt:        time.Now(),
	}

	log.Printf("[Destroy Audit] Created audit record: %s", auditID)

	// Create reconciler
	reconciler, err := NewEKSDestroyReconciler(ctx, h.store, cluster)
	if err != nil {
		terminalReason := fmt.Sprintf("Failed to create reconciler: %v", err)
		destroyAudit.TerminalReason = &terminalReason
		destroyAudit.CompletedAt = ptrTime(time.Now())
		// Best effort audit save
		_ = h.saveDestroyAudit(ctx, destroyAudit)
		return fmt.Errorf("create destroy reconciler: %w", err)
	}

	// Run reconciliation loop
	// This will continuously discover state and delete resources until nothing remains
	log.Printf("[Destroy] Running reconciliation loop for cluster %s", cluster.Name)
	if err := reconciler.ReconcileLoop(ctx); err != nil {
		terminalReason := fmt.Sprintf("Reconciliation failed: %v", err)
		destroyAudit.TerminalReason = &terminalReason
		destroyAudit.CompletedAt = ptrTime(time.Now())
		_ = h.saveDestroyAudit(ctx, destroyAudit)

		// Update cluster to DESTROY_FAILED
		if updateErr := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyFailed); updateErr != nil {
			log.Printf("Failed to update cluster status to DESTROY_FAILED: %v", updateErr)
		}

		return fmt.Errorf("reconcile destroy: %w", err)
	}

	log.Printf("[Destroy] Reconciliation loop completed, entering verification phase")

	// CRITICAL: Verify all resources are actually deleted before marking as DESTROYED
	// This is the invariant: Never mark DESTROYED unless AWS confirms absence
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyVerifying); err != nil {
		log.Printf("Warning: failed to update status to DESTROY_VERIFYING: %v", err)
	}

	verifyResult, err := reconciler.VerifyDestroyed(ctx)
	if err != nil {
		terminalReason := fmt.Sprintf("Verification error: %v", err)
		destroyAudit.TerminalReason = &terminalReason
		destroyAudit.CompletedAt = ptrTime(time.Now())
		_ = h.saveDestroyAudit(ctx, destroyAudit)

		// Update cluster to DESTROY_FAILED
		if updateErr := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyFailed); updateErr != nil {
			log.Printf("Failed to update cluster status to DESTROY_FAILED: %v", updateErr)
		}

		return fmt.Errorf("destroy verification: %w", err)
	}

	// Update audit with verification results
	now := time.Now()
	destroyAudit.LastVerifiedAt = &now
	destroyAudit.VerificationPassed = &verifyResult.Passed

	// Store verification snapshot
	verifySnapshot := types.JobMetadata{
		"cluster_exists":              verifyResult.ClusterExists,
		"managed_nodegroups_count":    verifyResult.ManagedNodegroupsCount,
		"fargate_profiles_count":      verifyResult.FargateProfilesCount,
		"cloudformation_stacks_count": verifyResult.CloudFormationStacksCount,
		"load_balancers_count":        verifyResult.LoadBalancersCount,
		"target_groups_count":         verifyResult.TargetGroupsCount,
		"security_groups_count":       verifyResult.SecurityGroupsCount,
		"network_interfaces_count":    verifyResult.NetworkInterfacesCount,
		"instances_count":             verifyResult.InstancesCount,
		"vpc_id":                      verifyResult.VPCID,
		"remaining_resources":         verifyResult.RemainingResources,
	}
	destroyAudit.VerificationSnapshot = verifySnapshot

	// If verification failed, capture the first remaining resource
	if !verifyResult.Passed {
		for resourceType, resources := range verifyResult.RemainingResources {
			if len(resources) > 0 {
				lastResource := fmt.Sprintf("%s: %v", resourceType, resources)
				destroyAudit.LastResourcePresent = &lastResource
				break
			}
		}

		terminalReason := "Verification failed: resources still exist"
		destroyAudit.TerminalReason = &terminalReason
		destroyAudit.CompletedAt = ptrTime(time.Now())
		_ = h.saveDestroyAudit(ctx, destroyAudit)

		// Update cluster to DESTROY_FAILED instead of DESTROYED
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyFailed); err != nil {
			return fmt.Errorf("mark cluster destroy failed: %w", err)
		}

		log.Printf("Cluster %s marked as DESTROY_FAILED - verification found remaining resources", cluster.Name)
		return fmt.Errorf("destroy verification failed - resources still exist: %v", verifyResult.RemainingResources)
	}

	// Verification passed - safe to clean up local resources and mark as DESTROYED
	log.Printf("[Destroy] ✓ Verification passed - all AWS resources deleted")

	// Clean up local work directory
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	if err := os.RemoveAll(workDir); err != nil {
		log.Printf("Warning: failed to remove work directory: %v", err)
	}

	// Mark cluster as destroyed (ONLY after verification passes)
	if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
		return fmt.Errorf("mark cluster destroyed: %w", err)
	}

	// Complete audit record
	terminalReason := "Destroyed successfully - verification passed"
	destroyAudit.TerminalReason = &terminalReason
	destroyAudit.CompletedAt = ptrTime(time.Now())
	if err := h.saveDestroyAudit(ctx, destroyAudit); err != nil {
		log.Printf("Warning: failed to save final destroy audit: %v", err)
	}

	log.Printf("Cluster %s marked as DESTROYED", cluster.Name)
	log.Printf("[Destroy Audit] Completed audit record: %s", auditID)
	return nil
}

// saveDestroyAudit saves destroy audit record (best effort, doesn't fail destroy)
func (h *DestroyHandler) saveDestroyAudit(ctx context.Context, audit *types.DestroyAudit) error {
	// Try to create the audit record if it doesn't exist yet (initial save)
	if audit.CompletedAt == nil {
		if err := h.store.DestroyAudit.Create(ctx, audit); err != nil {
			// Log but don't fail - audit is best-effort
			log.Printf("[Destroy Audit] Warning: failed to create audit record: %v", err)
			// Try update instead in case it already exists
			if updateErr := h.store.DestroyAudit.Update(ctx, audit); updateErr != nil {
				log.Printf("[Destroy Audit] Warning: failed to update audit record: %v", updateErr)
			}
		}
	} else {
		// Update existing audit record with completion data
		if err := h.store.DestroyAudit.Update(ctx, audit); err != nil {
			// Log but don't fail - audit is best-effort
			log.Printf("[Destroy Audit] Warning: failed to update audit record: %v", err)
		}
	}

	log.Printf("[Destroy Audit] Saved: cluster=%s job=%s worker=%s started=%v verified=%v passed=%v resources=%v",
		audit.ClusterID, audit.JobID, audit.WorkerID, audit.DestroyStartedAt,
		audit.LastVerifiedAt, audit.VerificationPassed, audit.LastResourcePresent)
	return nil
}

// ptrTime returns a pointer to a time.Time
func ptrTime(t time.Time) *time.Time {
	return &t
}

// handleIKSDestroy handles IKS cluster destruction
func (h *DestroyHandler) handleIKSDestroy(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting IKS cluster destruction for %s", cluster.Name)

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

	// Run ibmcloud ks cluster rm
	log.Printf("Running ibmcloud ks cluster rm for %s", cluster.Name)
	destroyCtx, destroyCancel := context.WithTimeout(ctx, 45*time.Minute)
	defer destroyCancel()

	output, err := iksInstaller.DestroyCluster(destroyCtx, cluster.Name)
	if err != nil {
		log.Printf("IKS cluster destruction failed: %v\nOutput: %s", err, output)
		// Mark as DESTROY_FAILED - cluster resources may be partially destroyed and require manual cleanup
		if updateErr := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyFailed); updateErr != nil {
			log.Printf("Failed to update cluster status to DESTROY_FAILED: %v", updateErr)
		}
		return fmt.Errorf("ibmcloud ks cluster rm: %w", err)
	}

	log.Printf("IKS cluster %s destroyed successfully", cluster.Name)

	// Mark cluster as destroyed
	// TODO: Add verification for IKS similar to EKS (verify cluster no longer exists via ibmcloud API)
	if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
		return fmt.Errorf("mark cluster destroyed: %w", err)
	}

	// Clean up work directory
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	if err := os.RemoveAll(workDir); err != nil {
		log.Printf("Warning: failed to remove work directory: %v", err)
	}

	log.Printf("Cluster %s marked as DESTROYED", cluster.Name)
	return nil
}

// cleanupKubernetesResources deletes all Kubernetes-created resources that could block VPC deletion
// This includes LoadBalancer services, Ingresses, and the Dashboard namespace
func (h *DestroyHandler) cleanupKubernetesResources(ctx context.Context, cluster *types.Cluster, workDir string) error {
	// Get kubeconfig
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

	// Check if kubeconfig exists - if not, try to fetch it
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		log.Printf("Kubeconfig not found locally, attempting to fetch from EKS")
		if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0755); err != nil {
			return fmt.Errorf("create auth directory: %w", err)
		}

		eksInstaller := installer.NewEKSInstaller()
		if err := eksInstaller.GetKubeconfig(ctx, cluster.Name, cluster.Region, kubeconfigPath); err != nil {
			return fmt.Errorf("get kubeconfig: %w", err)
		}
	}

	// Delete all LoadBalancer-type services across all namespaces
	// These create ELBs/NLBs that block VPC deletion
	log.Printf("Finding and deleting all LoadBalancer services...")
	lbCmd := exec.CommandContext(ctx, "kubectl", "get", "svc", "-A",
		"-o", "jsonpath={range .items[?(@.spec.type==\"LoadBalancer\")]}{.metadata.namespace}{\" \"}{.metadata.name}{\"\\n\"}{end}",
		"--kubeconfig", kubeconfigPath)
	lbOutput, err := lbCmd.CombinedOutput()
	if err != nil {
		log.Printf("Warning: failed to list LoadBalancer services: %v", err)
	} else {
		lbServices := string(lbOutput)
		if lbServices != "" {
			log.Printf("Found LoadBalancer services:\n%s", lbServices)
			// Parse and delete each service
			lines := strings.Split(strings.TrimSpace(lbServices), "\n")
			for _, line := range lines {
				if line == "" {
					continue
				}
				parts := strings.Fields(line)
				if len(parts) == 2 {
					namespace, name := parts[0], parts[1]
					log.Printf("Deleting LoadBalancer service %s/%s", namespace, name)
					delCmd := exec.CommandContext(ctx, "kubectl", "delete", "svc", name,
						"-n", namespace, "--ignore-not-found=true", "--timeout=120s", "--kubeconfig", kubeconfigPath)
					if delOutput, delErr := delCmd.CombinedOutput(); delErr != nil {
						log.Printf("Warning: failed to delete service %s/%s: %v\nOutput: %s", namespace, name, delErr, string(delOutput))
					}
				}
			}
		}
	}

	// Delete all Ingress resources (can create ALBs)
	log.Printf("Deleting all Ingress resources...")
	ingressCmd := exec.CommandContext(ctx, "kubectl", "delete", "ingress", "--all", "-A",
		"--ignore-not-found=true", "--timeout=120s", "--kubeconfig", kubeconfigPath)
	if ingressOutput, ingressErr := ingressCmd.CombinedOutput(); ingressErr != nil {
		log.Printf("Warning: failed to delete ingresses: %v\nOutput: %s", ingressErr, string(ingressOutput))
	}

	// Delete the kubernetes-dashboard namespace (created by POST_CONFIGURE)
	log.Printf("Deleting kubernetes-dashboard namespace...")
	dashCmd := exec.CommandContext(ctx, "kubectl", "delete", "namespace", "kubernetes-dashboard",
		"--ignore-not-found=true", "--timeout=120s", "--kubeconfig", kubeconfigPath)
	if dashOutput, dashErr := dashCmd.CombinedOutput(); dashErr != nil {
		log.Printf("Warning: failed to delete dashboard namespace: %v\nOutput: %s", dashErr, string(dashOutput))
	} else {
		log.Printf("Dashboard namespace deleted: %s", string(dashOutput))
	}

	// Wait for AWS to clean up ELB/NLB resources and ENIs
	log.Printf("Waiting 60s for AWS to clean up LoadBalancer resources and ENIs...")
	time.Sleep(60 * time.Second)

	return nil
}

// isClusterNotFoundError checks if the error message indicates the cluster doesn't exist
func isClusterNotFoundError(output string) bool {
	return stringContains(output, "ResourceNotFoundException") ||
		stringContains(output, "No cluster found for name") ||
		stringContains(output, "cluster not found")
}

// stringContains is a helper function to check if a string contains a substring
func stringContains(s, substr string) bool {
	// Simple contains implementation without importing strings package
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
