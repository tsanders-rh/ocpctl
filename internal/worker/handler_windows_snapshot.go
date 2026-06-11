package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// WindowsSnapshotHandler handles CREATE_WINDOWS_SNAPSHOT jobs
type WindowsSnapshotHandler struct {
	config         *Config
	store          *store.Store
	destroyHandler *DestroyHandler
}

// NewWindowsSnapshotHandler creates a new Windows snapshot handler
func NewWindowsSnapshotHandler(config *Config, st *store.Store) *WindowsSnapshotHandler {
	return &WindowsSnapshotHandler{
		config:         config,
		store:          st,
		destroyHandler: NewDestroyHandler(config, st),
	}
}

// Handle processes a CREATE_WINDOWS_SNAPSHOT job by either copying an existing snapshot
// or creating a new one from scratch via temporary cluster.
func (h *WindowsSnapshotHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get snapshot ID from metadata
	snapshotIDRaw, ok := job.Metadata["snapshot_id"]
	if !ok {
		return fmt.Errorf("snapshot_id not found in job metadata")
	}
	snapshotID, ok := snapshotIDRaw.(string)
	if !ok {
		return fmt.Errorf("snapshot_id is not a string")
	}

	// Get snapshot record
	snapshot, err := h.store.GetWindowsSnapshot(ctx, snapshotID)
	if err != nil {
		return fmt.Errorf("failed to get snapshot: %w", err)
	}

	region := snapshot.Region
	version := snapshot.Version

	// Determine creation method from job metadata
	creationMethod := "regenerate" // default
	if methodRaw, ok := job.Metadata["creation_method"]; ok {
		if method, ok := methodRaw.(string); ok && method != "" {
			creationMethod = method
		}
	}

	fmt.Printf("Creating Windows snapshot: region=%s version=%s method=%s\n", region, version, creationMethod)

	// Branch based on creation method
	if creationMethod == "copy" {
		return h.handleCopySnapshot(ctx, job, snapshot)
	}

	// Default: regenerate from S3
	return h.handleRegenerateSnapshot(ctx, job, snapshot)
}

// handleCopySnapshot copies an existing EBS snapshot to a new region
func (h *WindowsSnapshotHandler) handleCopySnapshot(ctx context.Context, job *types.Job, snapshot *types.WindowsSnapshot) error {
	snapshotID := snapshot.ID
	region := snapshot.Region
	version := snapshot.Version

	// Get source snapshot details from job metadata
	sourceSnapshotIDRaw, ok := job.Metadata["source_snapshot_id"]
	if !ok {
		errMsg := "source_snapshot_id not found in job metadata for copy method"
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf(errMsg)
	}
	sourceSnapshotID, ok := sourceSnapshotIDRaw.(string)
	if !ok || sourceSnapshotID == "" {
		errMsg := "source_snapshot_id is invalid"
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf(errMsg)
	}

	sourceRegionRaw, ok := job.Metadata["source_region"]
	if !ok {
		errMsg := "source_region not found in job metadata for copy method"
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf(errMsg)
	}
	sourceRegion, ok := sourceRegionRaw.(string)
	if !ok || sourceRegion == "" {
		errMsg := "source_region is invalid"
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf(errMsg)
	}

	fmt.Printf("Copying snapshot %s from %s to %s\n", sourceSnapshotID, sourceRegion, region)

	// Update status to creating
	if err := h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusCreating, nil); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Copy snapshot using AWS CLI
	newEBSSnapshotID, err := h.copyEBSSnapshot(ctx, sourceSnapshotID, sourceRegion, region, version)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to copy EBS snapshot: %v", err)
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf("copy EBS snapshot: %w", err)
	}

	fmt.Printf("✓ Snapshot copied: %s\n", newEBSSnapshotID)

	// Update status to validating (we skip VM boot validation for copies)
	if err := h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusValidating, nil); err != nil {
		return fmt.Errorf("failed to update status to validating: %w", err)
	}

	// Publish to SSM Parameter Store
	ssmPath := fmt.Sprintf("/ocpctl/windows-snapshots/%s/%s", version, region)
	if err := h.publishToSSM(ctx, region, ssmPath, newEBSSnapshotID); err != nil {
		errMsg := fmt.Sprintf("Failed to publish to SSM: %v", err)
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf("publish to SSM: %w", err)
	}

	fmt.Printf("✓ Published to SSM: %s\n", ssmPath)

	// Update snapshot record with success (validated=true since source was validated)
	if err := h.store.UpdateWindowsSnapshotValidation(ctx, snapshotID, true, ssmPath); err != nil {
		return fmt.Errorf("failed to update snapshot record: %w", err)
	}

	// Update EBS snapshot ID
	updateQuery := `UPDATE windows_snapshots SET ebs_snapshot_id = $1 WHERE id = $2`
	if _, err := h.store.DB().Exec(ctx, updateQuery, newEBSSnapshotID, snapshotID); err != nil {
		return fmt.Errorf("failed to update EBS snapshot ID: %w", err)
	}

	fmt.Println("✓ Windows snapshot copy completed successfully!")
	return nil
}

// handleRegenerateSnapshot creates a new snapshot from scratch using a temporary cluster
func (h *WindowsSnapshotHandler) handleRegenerateSnapshot(ctx context.Context, job *types.Job, snapshot *types.WindowsSnapshot) error {
	snapshotID := snapshot.ID
	region := snapshot.Region
	version := snapshot.Version
	s3SourceURL := "s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2"
	if snapshot.S3SourceURL != nil {
		s3SourceURL = *snapshot.S3SourceURL
	}

	fmt.Printf("Regenerating Windows snapshot from S3: region=%s version=%s\n", region, version)

	// Update status to creating
	if err := h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusCreating, nil); err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Step 1: Create temporary cluster
	fmt.Println("Step 1: Creating temporary cluster for snapshot creation...")
	tempClusterID, err := h.createTemporaryCluster(ctx, region, version)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to create temporary cluster: %v", err)
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf("create temporary cluster: %w", err)
	}

	// Ensure cleanup happens even if we fail later
	defer func() {
		fmt.Println("Cleaning up temporary cluster...")
		if cleanupErr := h.destroyTemporaryCluster(ctx, tempClusterID); cleanupErr != nil {
			fmt.Printf("Warning: Failed to cleanup temporary cluster %s: %v\n", tempClusterID, cleanupErr)
		}
	}()

	// Step 2: Wait for cluster to be ready
	fmt.Println("Step 2: Waiting for temporary cluster to be ready...")
	if err := h.waitForClusterReady(ctx, tempClusterID, 60*time.Minute); err != nil {
		errMsg := fmt.Sprintf("Temporary cluster failed to become ready: %v", err)
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf("wait for cluster ready: %w", err)
	}

	// Step 3: Wait for CNV operators to be ready
	fmt.Println("Step 3: Waiting for CNV operators to be ready...")
	if err := h.waitForCNVReady(ctx, tempClusterID, 15*time.Minute); err != nil {
		errMsg := fmt.Sprintf("CNV installation failed: %v", err)
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf("wait for CNV installation: %w", err)
	}

	// Step 4: Run snapshot creation script
	fmt.Println("Step 4: Running snapshot creation script...")
	if err := h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusValidating, nil); err != nil {
		return fmt.Errorf("failed to update status to validating: %w", err)
	}

	ebsSnapshotID, ssmPath, err := h.runSnapshotCreationScript(ctx, tempClusterID, region, version, s3SourceURL)
	if err != nil {
		errMsg := fmt.Sprintf("Snapshot creation script failed: %v", err)
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf("run snapshot creation script: %w", err)
	}

	// Step 5: Update snapshot record with success
	fmt.Printf("✓ Snapshot created successfully: %s\n", ebsSnapshotID)
	if err := h.store.UpdateWindowsSnapshotValidation(ctx, snapshotID, true, ssmPath); err != nil {
		return fmt.Errorf("failed to update snapshot record: %w", err)
	}

	// Also update EBS snapshot ID
	updateQuery := `UPDATE windows_snapshots SET ebs_snapshot_id = $1 WHERE id = $2`
	if _, err := h.store.DB().Exec(ctx, updateQuery, ebsSnapshotID, snapshotID); err != nil {
		return fmt.Errorf("failed to update EBS snapshot ID: %w", err)
	}

	fmt.Println("✓ Windows snapshot creation completed successfully!")
	return nil
}

// createTemporaryCluster creates a virtualization cluster with bare metal for snapshot creation
func (h *WindowsSnapshotHandler) createTemporaryCluster(ctx context.Context, region, version string) (string, error) {
	clusterID := uuid.New().String()
	clusterName := fmt.Sprintf("win-snap-%s", clusterID[:8])

	// Required for OpenShift installer
	baseDomain := "mg.dog8code.com"

	// Create cluster record
	cluster := &types.Cluster{
		ID:               clusterID,
		Name:             clusterName,
		Platform:         types.PlatformAWS,
		ClusterType:      types.ClusterTypeOpenShift,
		Region:           region,
		BaseDomain:       &baseDomain,             // Required for OpenShift installer
		Profile:          "aws-virtualization-ga", // Use virtualization profile for CNV support
		Version:          "4.21.10",               // Use profile default version
		Status:           types.ClusterStatusPending,
		Owner:            "system",
		OwnerID:          "a0000000-0000-0000-0000-000000000001", // Default admin user for system clusters
		TTLHours:         2, // Short TTL - 2 hours max
		SelectedAddonIDs: []string{
			"cnv", // CNV addon (will use default version with Windows VM support)
		},
	}

	if err := h.store.Clusters.Create(ctx, cluster); err != nil {
		return "", fmt.Errorf("create cluster record: %w", err)
	}

	// Create cluster creation job (will be picked up by worker automatically)
	createJobID := uuid.New().String()
	createJob := &types.Job{
		ID:          createJobID,
		ClusterID:   clusterID,
		JobType:     types.JobTypeCreate,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 1, // No retries for snapshot creation
	}

	if err := h.store.Jobs.Create(ctx, nil, createJob); err != nil {
		return "", fmt.Errorf("create job: %w", err)
	}

	// Wait for CREATE job to complete (async polling instead of direct handler call)
	if err := h.waitForJobCompletion(ctx, createJobID, 60*time.Minute); err != nil {
		return "", fmt.Errorf("cluster creation job failed: %w", err)
	}

	return clusterID, nil
}

// waitForClusterReady polls until cluster reaches READY status
func (h *WindowsSnapshotHandler) waitForClusterReady(ctx context.Context, clusterID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
		if err != nil {
			return fmt.Errorf("get cluster: %w", err)
		}

		switch cluster.Status {
		case types.ClusterStatusReady:
			return nil
		case types.ClusterStatusFailed:
			return fmt.Errorf("cluster creation failed")
		case types.ClusterStatusCreating:
			fmt.Printf("  Cluster status: %s, waiting...\n", cluster.Status)
			time.Sleep(30 * time.Second)
		default:
			fmt.Printf("  Cluster status: %s\n", cluster.Status)
			time.Sleep(10 * time.Second)
		}
	}

	return fmt.Errorf("timeout waiting for cluster to be ready")
}

// waitForCNVReady polls until CNV operators are ready by checking the cluster directly
func (h *WindowsSnapshotHandler) waitForCNVReady(ctx context.Context, clusterID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	// Work directory for cluster artifacts
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

	// Verify kubeconfig exists
	if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
		return fmt.Errorf("kubeconfig not found at %s", kubeconfigPath)
	}

	for time.Now().Before(deadline) {
		// Check if openshift-cnv namespace exists and CNV operator is ready
		cmd := exec.CommandContext(ctx, "oc", "get", "deployment", "-n", "openshift-cnv", "virt-operator", "-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
		cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))

		output, err := cmd.Output()
		if err == nil && string(output) == "True" {
			fmt.Println("  ✓ CNV virt-operator is available")

			// Also check if CDI operator is ready (needed for Windows VM imports)
			cmd = exec.CommandContext(ctx, "oc", "get", "deployment", "-n", "openshift-cnv", "cdi-operator", "-o", "jsonpath={.status.conditions[?(@.type=='Available')].status}")
			cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath))

			output, err = cmd.Output()
			if err == nil && string(output) == "True" {
				fmt.Println("  ✓ CNV cdi-operator is available")
				return nil
			}
		}

		fmt.Printf("  Waiting for CNV operators to be ready...\n")
		time.Sleep(15 * time.Second)
	}

	return fmt.Errorf("timeout waiting for CNV operators to be ready")
}

// runSnapshotCreationScript executes the snapshot creation script and parses output
func (h *WindowsSnapshotHandler) runSnapshotCreationScript(ctx context.Context, clusterID, region, version, s3SourceURL string) (ebsSnapshotID string, ssmPath string, err error) {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		return "", "", fmt.Errorf("get cluster: %w", err)
	}

	// Work directory for cluster artifacts
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)

	// Check if work directory exists locally, if not download from S3
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		fmt.Printf("Work directory %s not found locally, attempting to download from S3\n", workDir)

		artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
		if err != nil {
			return "", "", fmt.Errorf("create artifact storage: %w", err)
		}

		if err := artifactStorage.DownloadClusterArtifacts(ctx, cluster.ID, workDir); err != nil {
			return "", "", fmt.Errorf("download artifacts from S3: %w", err)
		}

		fmt.Printf("✓ Downloaded cluster artifacts from S3 to %s\n", workDir)
	}

	// Get kubeconfig path
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

	// Get script path
	scriptPath := filepath.Join(h.config.WorkDir, "..", "..", "manifests", "windows-vm", "create-regional-snapshot.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		// Try alternative path (deployed location)
		scriptPath = "/opt/ocpctl/manifests/windows-vm/create-regional-snapshot.sh"
	}

	// Execute script
	cmd := exec.CommandContext(ctx, "/bin/bash", scriptPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("KUBECONFIG=%s", kubeconfigPath),
		fmt.Sprintf("REGION=%s", region),
		fmt.Sprintf("SNAPSHOT_VERSION=%s", version),
		fmt.Sprintf("S3_SOURCE_URL=%s", s3SourceURL),
	)

	// Capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("script execution failed: %w\nOutput: %s", err, string(output))
	}

	// Parse output
	fmt.Println("Script output:")
	fmt.Println(string(output))

	// Extract EBS snapshot ID and SSM path from output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "EBS_SNAPSHOT_ID=") {
			ebsSnapshotID = strings.TrimPrefix(line, "EBS_SNAPSHOT_ID=")
		}
		if strings.HasPrefix(line, "SSM_PARAMETER_PATH=") {
			ssmPath = strings.TrimPrefix(line, "SSM_PARAMETER_PATH=")
		}
	}

	if ebsSnapshotID == "" {
		return "", "", fmt.Errorf("failed to extract EBS snapshot ID from script output")
	}
	if ssmPath == "" {
		return "", "", fmt.Errorf("failed to extract SSM parameter path from script output")
	}

	return ebsSnapshotID, ssmPath, nil
}

// destroyTemporaryCluster destroys the temporary cluster
func (h *WindowsSnapshotHandler) destroyTemporaryCluster(ctx context.Context, clusterID string) error {
	// Create destroy job
	destroyJobID := uuid.New().String()
	destroyJob := &types.Job{
		ID:          destroyJobID,
		ClusterID:   clusterID,
		JobType:     types.JobTypeDestroy,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 2,
	}

	if err := h.store.Jobs.Create(ctx, nil, destroyJob); err != nil {
		return fmt.Errorf("create destroy job: %w", err)
	}

	// Execute destroy
	if err := h.destroyHandler.Handle(ctx, destroyJob); err != nil {
		return fmt.Errorf("execute destroy: %w", err)
	}

	return nil
}

// waitForJobCompletion polls until a job reaches terminal status
func (h *WindowsSnapshotHandler) waitForJobCompletion(ctx context.Context, jobID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		job, err := h.store.Jobs.GetByID(ctx, jobID)
		if err != nil {
			return fmt.Errorf("get job: %w", err)
		}

		switch job.Status {
		case types.JobStatusSucceeded:
			return nil
		case types.JobStatusFailed:
			errMsg := "job failed"
			if job.ErrorMessage != nil {
				errMsg = *job.ErrorMessage
			}
			return fmt.Errorf("job failed: %s", errMsg)
		case types.JobStatusRunning, types.JobStatusPending, types.JobStatusRetrying:
			fmt.Printf("  Job status: %s, waiting...\n", job.Status)
			time.Sleep(15 * time.Second)
		default:
			fmt.Printf("  Job status: %s\n", job.Status)
			time.Sleep(10 * time.Second)
		}
	}

	return fmt.Errorf("timeout waiting for job to complete")
}

// copyEBSSnapshot copies an EBS snapshot from one region to another
func (h *WindowsSnapshotHandler) copyEBSSnapshot(ctx context.Context, sourceSnapshotID, sourceRegion, destRegion, version string) (string, error) {
	description := fmt.Sprintf("Windows 10 OADP v%s (copied from %s)", version, sourceRegion)

	// Build AWS CLI command to copy snapshot
	cmd := exec.CommandContext(ctx, "aws", "ec2", "copy-snapshot",
		"--source-region", sourceRegion,
		"--source-snapshot-id", sourceSnapshotID,
		"--destination-region", destRegion,
		"--description", description,
		"--region", destRegion,
		"--output", "json",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("aws ec2 copy-snapshot failed: %w\nOutput: %s", err, string(output))
	}

	// Parse snapshot ID from response
	var result struct {
		SnapshotID string `json:"SnapshotId"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("parse copy-snapshot response: %w", err)
	}

	newSnapshotID := result.SnapshotID
	fmt.Printf("  Snapshot copy initiated: %s (waiting for completion...)\n", newSnapshotID)

	// Wait for snapshot to complete (can take 60+ minutes for cross-region)
	deadline := time.Now().Add(90 * time.Minute)
	for time.Now().Before(deadline) {
		// Check snapshot status
		statusCmd := exec.CommandContext(ctx, "aws", "ec2", "describe-snapshots",
			"--snapshot-ids", newSnapshotID,
			"--region", destRegion,
			"--query", "Snapshots[0].[Progress,State]",
			"--output", "json",
		)

		statusOutput, err := statusCmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("describe-snapshots failed: %w\nOutput: %s", err, string(statusOutput))
		}

		var status [2]string
		if err := json.Unmarshal(statusOutput, &status); err != nil {
			return "", fmt.Errorf("parse snapshot status: %w", err)
		}

		progress, state := status[0], status[1]
		fmt.Printf("  Snapshot copy progress: %s (%s)\n", progress, state)

		if state == "completed" {
			fmt.Printf("  ✓ Snapshot copy completed: %s\n", newSnapshotID)

			// Add ocpctl tags to the copied snapshot
			if err := h.tagCopiedSnapshot(ctx, newSnapshotID, destRegion, version); err != nil {
				return "", fmt.Errorf("tag snapshot: %w", err)
			}

			return newSnapshotID, nil
		} else if state == "error" {
			return "", fmt.Errorf("snapshot copy failed with error state")
		}

		time.Sleep(2 * time.Minute)
	}

	return "", fmt.Errorf("timeout waiting for snapshot copy to complete")
}

// tagCopiedSnapshot adds ocpctl metadata tags to a copied EBS snapshot
func (h *WindowsSnapshotHandler) tagCopiedSnapshot(ctx context.Context, snapshotID, region, version string) error {
	tags := []string{
		"Key=Name,Value=ocpctl-windows-10-oadp-v" + version,
		"Key=ocpctl:managed,Value=true",
		"Key=ocpctl:image-version,Value=" + version,
		"Key=ocpctl:region,Value=" + region,
		"Key=ocpctl:validated,Value=true",
		"Key=ocpctl:creation-method,Value=copy",
	}

	args := []string{"ec2", "create-tags",
		"--resources", snapshotID,
		"--region", region,
		"--tags"}
	args = append(args, tags...)

	cmd := exec.CommandContext(ctx, "aws", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create-tags failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("  ✓ Tagged snapshot %s\n", snapshotID)
	return nil
}

// publishToSSM publishes the snapshot ID to SSM Parameter Store
func (h *WindowsSnapshotHandler) publishToSSM(ctx context.Context, region, parameterName, snapshotID string) error {
	cmd := exec.CommandContext(ctx, "aws", "ssm", "put-parameter",
		"--name", parameterName,
		"--value", snapshotID,
		"--type", "String",
		"--region", region,
		"--overwrite",
		"--output", "json",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("put-parameter failed: %w\nOutput: %s", err, string(output))
	}

	return nil
}
