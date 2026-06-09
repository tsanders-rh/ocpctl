package worker

import (
	"context"
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
	createHandler  *CreateHandler
	destroyHandler *DestroyHandler
}

// NewWindowsSnapshotHandler creates a new Windows snapshot handler
func NewWindowsSnapshotHandler(config *Config, st *store.Store) *WindowsSnapshotHandler {
	return &WindowsSnapshotHandler{
		config:         config,
		store:          st,
		createHandler:  NewCreateHandler(config, st),
		destroyHandler: NewDestroyHandler(config, st),
	}
}

// Handle processes a CREATE_WINDOWS_SNAPSHOT job by creating a temporary cluster,
// running the snapshot creation script, and cleaning up.
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
	s3SourceURL := "s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2"
	if snapshot.S3SourceURL != nil {
		s3SourceURL = *snapshot.S3SourceURL
	}

	fmt.Printf("Creating Windows snapshot: region=%s version=%s\n", region, version)

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

	// Step 3: Install CNV
	fmt.Println("Step 3: Installing OpenShift Virtualization...")
	if err := h.installCNV(ctx, tempClusterID); err != nil {
		errMsg := fmt.Sprintf("Failed to install CNV: %v", err)
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf("install CNV: %w", err)
	}

	// Step 4: Wait for CNV to be ready
	fmt.Println("Step 4: Waiting for CNV installation to complete...")
	if err := h.waitForCNVReady(ctx, tempClusterID, 30*time.Minute); err != nil {
		errMsg := fmt.Sprintf("CNV installation failed: %v", err)
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf("wait for CNV: %w", err)
	}

	// Step 5: Run snapshot creation script
	fmt.Println("Step 5: Running snapshot creation script...")
	if err := h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusValidating, nil); err != nil {
		return fmt.Errorf("failed to update status to validating: %w", err)
	}

	ebsSnapshotID, ssmPath, err := h.runSnapshotCreationScript(ctx, tempClusterID, region, version, s3SourceURL)
	if err != nil {
		errMsg := fmt.Sprintf("Snapshot creation script failed: %v", err)
		_ = h.store.UpdateWindowsSnapshotStatus(ctx, snapshotID, types.WindowsSnapshotStatusFailed, &errMsg)
		return fmt.Errorf("run snapshot creation script: %w", err)
	}

	// Step 6: Update snapshot record with success
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

	// Create cluster record
	cluster := &types.Cluster{
		ID:               clusterID,
		Name:             clusterName,
		Platform:         types.PlatformAWS,
		ClusterType:      types.ClusterTypeOpenShift,
		Region:           region,
		Profile:          "aws-virtualization-ga", // Use virtualization profile for CNV support
		Version:          "4.20",                  // Use stable version
		Status:           types.ClusterStatusPending,
		OwnerID:          "",       // System-managed cluster, no owner
		TTLHours:         2,        // Short TTL - 2 hours max
		SelectedAddonIDs: []string{}, // No addons for snapshot creation cluster
	}

	if err := h.store.Clusters.Create(ctx, cluster); err != nil {
		return "", fmt.Errorf("create cluster record: %w", err)
	}

	// Create cluster creation job
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

	// Execute cluster creation
	if err := h.createHandler.Handle(ctx, createJob); err != nil {
		return "", fmt.Errorf("execute cluster creation: %w", err)
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

// installCNV triggers CNV installation via POST_CONFIGURE
func (h *WindowsSnapshotHandler) installCNV(ctx context.Context, clusterID string) error {
	// Create POST_CONFIGURE job with CNV addon
	postConfigJobID := uuid.New().String()
	postConfigJob := &types.Job{
		ID:          postConfigJobID,
		ClusterID:   clusterID,
		JobType:     types.JobTypePostConfigure,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 3,
		Metadata: types.JobMetadata{
			"addons": []map[string]interface{}{
				{
					"name":    "cnv",
					"version": "4.20", // Match cluster version
				},
			},
		},
	}

	if err := h.store.Jobs.Create(ctx, nil, postConfigJob); err != nil {
		return fmt.Errorf("create POST_CONFIGURE job: %w", err)
	}

	// Wait for job to complete
	return h.waitForJobCompletion(ctx, postConfigJobID, 30*time.Minute)
}

// waitForCNVReady waits for CNV to be fully operational
func (h *WindowsSnapshotHandler) waitForCNVReady(ctx context.Context, clusterID string, timeout time.Duration) error {
	// Get kubeconfig
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	kubeconfigPath := filepath.Join(h.config.WorkDir, "clusters", cluster.Name, "auth", "kubeconfig")

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Check if CDI API is ready
		cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
			"get", "endpoints", "cdi-api", "-n", "openshift-cnv",
			"-o", "jsonpath={.subsets[*].addresses[*].ip}")

		output, err := cmd.Output()
		if err == nil && len(output) > 0 {
			fmt.Println("✓ CNV is ready")
			return nil
		}

		fmt.Println("  Waiting for CNV to be ready...")
		time.Sleep(30 * time.Second)
	}

	return fmt.Errorf("timeout waiting for CNV to be ready")
}

// runSnapshotCreationScript executes the snapshot creation script and parses output
func (h *WindowsSnapshotHandler) runSnapshotCreationScript(ctx context.Context, clusterID, region, version, s3SourceURL string) (ebsSnapshotID string, ssmPath string, err error) {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, clusterID)
	if err != nil {
		return "", "", fmt.Errorf("get cluster: %w", err)
	}

	// Get kubeconfig path
	kubeconfigPath := filepath.Join(h.config.WorkDir, "clusters", cluster.Name, "auth", "kubeconfig")

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
