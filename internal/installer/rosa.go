package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ROSAInstaller wraps the rosa CLI for ROSA cluster operations
type ROSAInstaller struct {
	binaryPath string
	timeout    time.Duration
}

// ROSAMachinePool represents a ROSA machine pool
type ROSAMachinePool struct {
	ID           string `json:"id"`
	InstanceType string `json:"instance_type"`
	Replicas     int    `json:"replicas"`
	Autoscaling  struct {
		Enabled    bool `json:"enabled"`
		MinReplicas int  `json:"min_replicas"`
		MaxReplicas int  `json:"max_replicas"`
	} `json:"autoscaling"`
	AvailabilityZones []string          `json:"availability_zones"`
	Subnets           []string          `json:"subnets"`
	Labels            map[string]string `json:"labels"`
	Taints            []ROSATaint       `json:"taints"`
}

// ROSATaint represents a node taint
type ROSATaint struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Effect string `json:"effect"`
}

// ROSAClusterInfo represents basic ROSA cluster information
type ROSAClusterInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	API   struct {
		URL string `json:"url"`
	} `json:"api"`
	Console struct {
		URL string `json:"url"`
	} `json:"console"`
	Region struct {
		ID string `json:"id"`
	} `json:"region"`
	OpenshiftVersion string `json:"openshift_version"`
}

// APIURL returns the cluster API URL
func (r *ROSAClusterInfo) APIURL() string {
	return r.API.URL
}

// ConsoleURL returns the cluster console URL
func (r *ROSAClusterInfo) ConsoleURL() string {
	return r.Console.URL
}

// RegionID returns the cluster region ID
func (r *ROSAClusterInfo) RegionID() string {
	return r.Region.ID
}

// NewROSAInstaller creates a new ROSA installer instance
func NewROSAInstaller() *ROSAInstaller {
	binaryPath := os.Getenv("ROSA_BINARY")
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/rosa"
	}

	return &ROSAInstaller{
		binaryPath: binaryPath,
		timeout:    60 * time.Minute, // ROSA clusters typically take 30-40 minutes
	}
}

// ensureLoggedIn ensures rosa CLI is authenticated with OCM
// Uses OCM_TOKEN environment variable for authentication
func (r *ROSAInstaller) ensureLoggedIn(ctx context.Context) error {
	// Use a fresh context with short timeout for login operations
	// This prevents login from failing when parent context is about to expire (e.g., during long WaitForClusterReady calls)
	loginCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Check if already logged in
	cmd := exec.CommandContext(loginCtx, r.binaryPath, "whoami")
	if err := cmd.Run(); err == nil {
		// Already logged in
		return nil
	}

	// Get token from environment
	token := os.Getenv("OCM_TOKEN")
	if token == "" {
		token = os.Getenv("ROSA_TOKEN")
	}
	if token == "" {
		return fmt.Errorf("not logged in to OCM: set OCM_TOKEN or ROSA_TOKEN environment variable")
	}

	// Login using token
	cmd = exec.CommandContext(loginCtx, r.binaryPath, "login", "--token="+token)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rosa login failed: %w: %s", err, stderr.String())
	}

	return nil
}

// ensureAccountRoles ensures ROSA account-level IAM roles exist
// These are one-time roles required for any ROSA cluster in the AWS account
func (r *ROSAInstaller) ensureAccountRoles(ctx context.Context) error {
	// Check if account roles exist
	cmd := exec.CommandContext(ctx, r.binaryPath, "list", "account-roles")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("check account roles: %w: %s", err, stderr.String())
	}

	// If output contains role ARNs, roles exist
	output := stdout.String()
	if strings.Contains(output, "ManagedOpenShift-Installer-Role") &&
		strings.Contains(output, "ManagedOpenShift-ControlPlane-Role") &&
		strings.Contains(output, "ManagedOpenShift-Worker-Role") &&
		strings.Contains(output, "ManagedOpenShift-Support-Role") {
		// Account roles already exist
		return nil
	}

	// Create account roles
	cmd = exec.CommandContext(ctx, r.binaryPath, "create", "account-roles", "--mode", "auto", "--yes")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create account roles: %w: %s", err, stderr.String())
	}

	return nil
}

// CreateCluster creates a ROSA cluster using rosa CLI
// Returns cluster ID and error
func (r *ROSAInstaller) CreateCluster(ctx context.Context, args []string, logFile string) (string, string, error) {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return "", "", err
	}

	// Ensure account-level IAM roles exist (one-time setup)
	if err := r.ensureAccountRoles(ctx); err != nil {
		return "", "", fmt.Errorf("ensure account roles: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// Build command: rosa create cluster [args...]
	cmdArgs := append([]string{"create", "cluster"}, args...)
	cmd := exec.CommandContext(ctx, r.binaryPath, cmdArgs...)

	// Open log file for writing
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", "", fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	// Write both stdout and stderr to log file
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Write error output to log file
		f.WriteString(stderr.String())
		return "", stderr.String(), fmt.Errorf("rosa create cluster failed: %w", err)
	}

	// Write successful output to log file
	f.WriteString(stdout.String())

	// Extract cluster ID from output
	// ROSA outputs: "I: Cluster '<cluster-id>' is now creating."
	output := stdout.String()
	clusterID := extractClusterID(output)

	return clusterID, output, nil
}

// DestroyCluster destroys a ROSA cluster using rosa CLI
func (r *ROSAInstaller) DestroyCluster(ctx context.Context, clusterName string) (string, error) {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	// rosa delete cluster --cluster <name> --yes
	cmd := exec.CommandContext(ctx, r.binaryPath, "delete", "cluster",
		"--cluster", clusterName,
		"--yes")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("rosa delete cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// WaitForClusterReady waits for the ROSA cluster to reach ready state
func (r *ROSAInstaller) WaitForClusterReady(ctx context.Context, clusterName string, pollInterval time.Duration) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			info, err := r.DescribeCluster(ctx, clusterName)
			if err != nil {
				return fmt.Errorf("describe cluster: %w", err)
			}

			if info.State == "ready" {
				return nil
			}

			if info.State == "error" || info.State == "uninstalling" {
				return fmt.Errorf("cluster entered error state: %s", info.State)
			}
		}
	}
}

// DescribeCluster gets cluster information using rosa CLI
func (r *ROSAInstaller) DescribeCluster(ctx context.Context, clusterName string) (*ROSAClusterInfo, error) {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return nil, err
	}

	// rosa describe cluster --cluster <name> -o json
	cmd := exec.CommandContext(ctx, r.binaryPath, "describe", "cluster",
		"--cluster", clusterName,
		"-o", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rosa describe cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	var info ROSAClusterInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("parse cluster info: %w", err)
	}

	return &info, nil
}

// GetKubeconfig retrieves the kubeconfig for a ROSA cluster
func (r *ROSAInstaller) GetKubeconfig(ctx context.Context, clusterName, outputPath string) error {
	// ROSA doesn't have a direct kubeconfig command, need to use 'oc login' approach
	// or AWS credentials. For now, we'll use oc to get the kubeconfig.
	// This requires the cluster to be ready and oc to be installed.

	// First get cluster API URL
	info, err := r.DescribeCluster(ctx, clusterName)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	if info.APIURL() == "" {
		return fmt.Errorf("cluster API URL not available")
	}

	// Create admin user and get login command
	// rosa create admin --cluster <name>
	cmd := exec.CommandContext(ctx, r.binaryPath, "create", "admin",
		"--cluster", clusterName)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rosa create admin failed: %w\nStderr: %s", err, stderr.String())
	}

	// Output contains: oc login https://api.xxx --username cluster-admin --password xxx
	// Extract and execute the oc login command with --kubeconfig flag
	loginCmd := strings.TrimSpace(stdout.String())

	// Parse oc login command to extract credentials
	// Format: "oc login <api-url> --username <user> --password <pass>"
	parts := strings.Fields(loginCmd)
	if len(parts) < 7 || parts[0] != "oc" || parts[1] != "login" {
		return fmt.Errorf("unexpected admin create output format")
	}

	apiURL := parts[2]
	username := parts[4]
	password := parts[6]

	// Run oc login with kubeconfig output
	ocCmd := exec.CommandContext(ctx, "oc", "login", apiURL,
		"--username", username,
		"--password", password,
		"--kubeconfig", outputPath,
		"--insecure-skip-tls-verify")

	var ocStderr bytes.Buffer
	ocCmd.Stderr = &ocStderr

	if err := ocCmd.Run(); err != nil {
		return fmt.Errorf("oc login failed: %w\nStderr: %s", err, ocStderr.String())
	}

	return nil
}

// ListMachinePools lists all machine pools for a ROSA cluster
func (r *ROSAInstaller) ListMachinePools(ctx context.Context, clusterName string) ([]ROSAMachinePool, error) {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return nil, err
	}

	// rosa list machinepools --cluster <name> -o json
	cmd := exec.CommandContext(ctx, r.binaryPath, "list", "machinepools",
		"--cluster", clusterName,
		"-o", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rosa list machinepools failed: %w\nStderr: %s", err, stderr.String())
	}

	var pools []ROSAMachinePool
	if err := json.Unmarshal(stdout.Bytes(), &pools); err != nil {
		return nil, fmt.Errorf("parse machine pools: %w", err)
	}

	return pools, nil
}

// ScaleMachinePool scales a ROSA machine pool to a specific replica count
// Used for hibernation (scale to 0) and resume (scale back to original)
func (r *ROSAInstaller) ScaleMachinePool(ctx context.Context, clusterName, poolID string, replicas int) error {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return err
	}

	// rosa edit machinepool --cluster <name> --replicas <count> <pool-id>
	cmd := exec.CommandContext(ctx, r.binaryPath, "edit", "machinepool",
		"--cluster", clusterName,
		"--replicas", fmt.Sprintf("%d", replicas),
		poolID)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rosa edit machinepool failed: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// CreateMachinePool creates a new machine pool for a ROSA cluster
func (r *ROSAInstaller) CreateMachinePool(ctx context.Context, clusterName, poolName, instanceType string, replicas int, labels map[string]string) error {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return err
	}

	// Build command: rosa create machinepool --cluster <name> --name <pool> --instance-type <type> --replicas <count>
	args := []string{"create", "machinepool",
		"--cluster", clusterName,
		"--name", poolName,
		"--instance-type", instanceType,
		"--replicas", fmt.Sprintf("%d", replicas),
	}

	// Add labels if provided
	if len(labels) > 0 {
		labelStrs := make([]string, 0, len(labels))
		for k, v := range labels {
			labelStrs = append(labelStrs, fmt.Sprintf("%s=%s", k, v))
		}
		args = append(args, "--labels", strings.Join(labelStrs, ","))
	}

	cmd := exec.CommandContext(ctx, r.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rosa create machinepool failed: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// DeleteMachinePool deletes a machine pool from a ROSA cluster
func (r *ROSAInstaller) DeleteMachinePool(ctx context.Context, clusterName, poolID string) error {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return err
	}

	// rosa delete machinepool --cluster <name> --yes <pool-id>
	cmd := exec.CommandContext(ctx, r.binaryPath, "delete", "machinepool",
		"--cluster", clusterName,
		"--yes",
		poolID)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rosa delete machinepool failed: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// StreamInstallLogs streams ROSA installation logs to a file
// This runs 'rosa logs install -c <cluster> --watch' and writes to logFile
func (r *ROSAInstaller) StreamInstallLogs(ctx context.Context, clusterName, logFile string) error {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return err
	}

	// Open log file for writing
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	// rosa logs install -c <cluster> --watch
	cmd := exec.CommandContext(ctx, r.binaryPath, "logs", "install",
		"-c", clusterName,
		"--watch")

	// Write both stdout and stderr to log file
	cmd.Stdout = f
	cmd.Stderr = f

	// Run command (blocks until context cancelled or logs complete)
	if err := cmd.Run(); err != nil {
		// Context cancellation is expected
		if ctx.Err() == context.Canceled {
			return nil
		}
		return fmt.Errorf("rosa logs install failed: %w", err)
	}

	return nil
}

// Version returns the rosa CLI version
func (r *ROSAInstaller) Version() (string, error) {
	cmd := exec.Command(r.binaryPath, "version")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rosa version failed: %w\nStderr: %s", err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

// extractClusterID extracts the cluster ID from rosa create cluster output
func extractClusterID(output string) string {
	// Look for pattern: "Cluster '<id>' is now creating"
	// or "I: Cluster '<id>' has been created"
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Cluster '") && (strings.Contains(line, "creating") || strings.Contains(line, "created")) {
			start := strings.Index(line, "'")
			if start == -1 {
				continue
			}
			end := strings.Index(line[start+1:], "'")
			if end == -1 {
				continue
			}
			return line[start+1 : start+1+end]
		}
	}
	return ""
}
