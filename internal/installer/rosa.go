package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// fullPatchVersionRe matches a fully-qualified OpenShift version (X.Y.Z, with an
// optional pre-release/build suffix). minorVersionRe matches a minor-only version
// (X.Y), which is what ocpctl profiles declare by convention.
var (
	fullPatchVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+`)
	minorVersionRe     = regexp.MustCompile(`^\d+\.\d+$`)
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

// ensureAccountRoles ensures the default ManagedOpenShift account-level IAM roles
// exist AND are at a version >= the cluster's OpenShift version.
//
// ROSA requires account roles to be equal to or newer than the cluster version;
// otherwise `rosa create cluster` cannot auto-detect suitable roles and falls
// back to an interactive ARN prompt (which fails with "Expected a valid ARN: EOF"
// in our non-interactive context). Checking only that the role names exist is not
// enough — a 4.21 set of roles will not satisfy a 4.22 cluster.
//
// version may be a minor ("4.22") or full patch ("4.22.8"); only MAJOR.MINOR is
// used. When roles are missing or too old, they are created/upgraded with an
// explicit --version, because `rosa create account-roles` otherwise targets the
// channel default (which can lag the requested version, e.g. 4.21 default while
// provisioning 4.22).
func (r *ROSAInstaller) ensureAccountRoles(ctx context.Context, version string) error {
	target, ok := majorMinor(version)
	if !ok {
		return fmt.Errorf("invalid OpenShift version %q for account role check", version)
	}

	// Check existing account roles and their versions.
	cmd := exec.CommandContext(ctx, r.binaryPath, "list", "account-roles", "-o", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("check account roles: %w: %s", err, stderr.String())
	}

	var roles []struct {
		RoleName string `json:"RoleName"`
		Version  string `json:"Version"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &roles); err != nil {
		return fmt.Errorf("parse account roles: %w", err)
	}

	// All four default account roles must be present and at a version >= target.
	present := map[string]bool{
		"ManagedOpenShift-Installer-Role":    false,
		"ManagedOpenShift-ControlPlane-Role": false,
		"ManagedOpenShift-Worker-Role":       false,
		"ManagedOpenShift-Support-Role":      false,
	}
	suitable := true
	for _, role := range roles {
		if _, required := present[role.RoleName]; !required {
			continue
		}
		present[role.RoleName] = true
		if !versionAtLeast(role.Version, target) {
			suitable = false
		}
	}
	allPresent := true
	for _, ok := range present {
		if !ok {
			allPresent = false
			break
		}
	}
	if allPresent && suitable {
		return nil
	}

	// Create or upgrade the account roles to the cluster's version.
	cmd = exec.CommandContext(ctx, r.binaryPath, "create", "account-roles",
		"--mode", "auto", "--yes", "--version", target)
	stdout.Reset()
	stderr.Reset()
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create account roles for %s: %w: %s", target, err, stderr.String())
	}

	return nil
}

// ResolveVersion resolves a possibly minor-only OpenShift version (e.g. "4.21")
// to the latest fully-qualified patch version supported by ROSA (e.g. "4.21.27").
//
// The rosa CLI's `create cluster --version` flag rejects minor-only versions and
// demands an exact patch from its supported list. ocpctl profiles, however,
// declare minor versions by convention (matching the IPI path, where
// openshift-install resolves the minor internally). This method bridges that gap
// so a profile default like "4.21" provisions successfully on ROSA.
//
// Already fully-qualified versions (X.Y.Z) are returned unchanged.
func (r *ROSAInstaller) ResolveVersion(ctx context.Context, version string) (string, error) {
	// Already fully qualified — nothing to resolve.
	if fullPatchVersionRe.MatchString(version) {
		return version, nil
	}
	if !minorVersionRe.MatchString(version) {
		return "", fmt.Errorf("unrecognized OpenShift version %q: expected MAJOR.MINOR or MAJOR.MINOR.PATCH", version)
	}

	// Listing versions requires OCM authentication.
	if err := r.ensureLoggedIn(ctx); err != nil {
		return "", err
	}

	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(listCtx, r.binaryPath, "list", "versions", "--output", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("rosa list versions: %w: %s", err, stderr.String())
	}

	var versions []struct {
		RawID   string `json:"raw_id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &versions); err != nil {
		return "", fmt.Errorf("parse rosa versions: %w", err)
	}

	// Pick the highest enabled patch matching the requested minor.
	prefix := version + "."
	best := ""
	bestPatch := -1
	for _, v := range versions {
		if !v.Enabled || !strings.HasPrefix(v.RawID, prefix) {
			continue
		}
		patch, ok := patchNumber(v.RawID)
		if !ok {
			continue
		}
		if patch > bestPatch {
			bestPatch = patch
			best = v.RawID
		}
	}

	if best == "" {
		return "", fmt.Errorf("no ROSA-supported patch version found for OpenShift %s", version)
	}
	return best, nil
}

// majorMinor returns the "MAJOR.MINOR" prefix of a version string, accepting
// either "4.22" or "4.22.8", and false if it can't be parsed.
func majorMinor(version string) (string, bool) {
	maj, min, ok := splitMajorMinor(version)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%d.%d", maj, min), true
}

// versionAtLeast reports whether MAJOR.MINOR version have is >= want. Both are
// compared numerically; unparseable input compares as false (treated as too old).
func versionAtLeast(have, want string) bool {
	hMaj, hMin, ok := splitMajorMinor(have)
	if !ok {
		return false
	}
	wMaj, wMin, ok := splitMajorMinor(want)
	if !ok {
		return false
	}
	if hMaj != wMaj {
		return hMaj > wMaj
	}
	return hMin >= wMin
}

// splitMajorMinor parses the major and minor components of a version string.
func splitMajorMinor(v string) (int, int, bool) {
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	maj, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	min, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return maj, min, true
}

// patchNumber extracts the numeric patch component from a version string like
// "4.21.27" (or "4.21.27-candidate"), returning false if it can't be parsed.
func patchNumber(rawID string) (int, bool) {
	parts := strings.Split(rawID, ".")
	if len(parts) < 3 {
		return 0, false
	}
	patchStr := parts[2]
	if i := strings.IndexAny(patchStr, "-+"); i >= 0 {
		patchStr = patchStr[:i]
	}
	n, err := strconv.Atoi(patchStr)
	if err != nil {
		return 0, false
	}
	return n, true
}

// CreateCluster creates a ROSA cluster using rosa CLI
// Returns cluster ID and error
func (r *ROSAInstaller) CreateCluster(ctx context.Context, version string, args []string, logFile string) (string, string, error) {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return "", "", err
	}

	// Ensure account-level IAM roles exist and are new enough for this version.
	if err := r.ensureAccountRoles(ctx, version); err != nil {
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

	// Use a fresh context with timeout for the describe operation
	// This prevents the command from failing when parent context is about to expire
	describeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// rosa describe cluster --cluster <name> -o json
	cmd := exec.CommandContext(describeCtx, r.binaryPath, "describe", "cluster",
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

// ROSAAdminCredentials holds the admin username and password for console access
type ROSAAdminCredentials struct {
	Username string
	Password string
}

// GetKubeconfig retrieves the kubeconfig for a ROSA cluster and returns admin credentials
// The admin credentials expire after 72 hours and can be used for console login
func (r *ROSAInstaller) GetKubeconfig(ctx context.Context, clusterName, outputPath string) (*ROSAAdminCredentials, error) {
	// ROSA doesn't have a direct kubeconfig command, need to use 'oc login' approach
	// or AWS credentials. For now, we'll use oc to get the kubeconfig.
	// This requires the cluster to be ready and oc to be installed.

	// The cluster API URL is not always published the instant ROSA reports the
	// cluster ready. Poll `rosa describe cluster` until .api.url appears before
	// creating the admin user, so `rosa create admin` doesn't fail spuriously
	// and leave the cluster READY without usable credentials (issue #75).
	const (
		apiURLPollTimeout  = 5 * time.Minute
		apiURLPollInterval = 15 * time.Second
	)
	deadline := time.Now().Add(apiURLPollTimeout)
	for {
		info, err := r.DescribeCluster(ctx, clusterName)
		if err != nil {
			return nil, fmt.Errorf("get cluster info: %w", err)
		}
		if info.APIURL() != "" {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("cluster API URL not available after %s", apiURLPollTimeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(apiURLPollInterval):
		}
	}

	// Create admin user and get login command
	// rosa create admin --cluster <name>
	cmd := exec.CommandContext(ctx, r.binaryPath, "create", "admin",
		"--cluster", clusterName)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rosa create admin failed: %w\nStderr: %s", err, stderr.String())
	}

	// Output contains multiple INFO lines, need to extract just the "oc login" command
	// Example output:
	//   INFO: Admin account has been added...
	//   INFO: To login, run the following command:
	//
	//      oc login https://api.xxx --username cluster-admin --password xxx
	//
	//   INFO: It may take several minutes...
	output := stdout.String()

	// Find the line containing "oc login"
	var loginCmd string
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "oc login") {
			loginCmd = trimmed
			break
		}
	}

	if loginCmd == "" {
		return nil, fmt.Errorf("unexpected admin create output format: no 'oc login' command found")
	}

	// Parse oc login command to extract credentials
	// Format: "oc login <api-url> --username <user> --password <pass>"
	parts := strings.Fields(loginCmd)
	if len(parts) < 7 || parts[0] != "oc" || parts[1] != "login" {
		return nil, fmt.Errorf("unexpected admin create output format: %s", loginCmd)
	}

	apiURL := parts[2]
	username := parts[4]
	password := parts[6]

	// Run oc login with kubeconfig output (with retry logic)
	// The cluster API may not be immediately accessible after cluster becomes ready
	var lastErr error
	maxRetries := 5
	retryDelay := 10 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		ocCmd := exec.CommandContext(ctx, "oc", "login", apiURL,
			"--username", username,
			"--password", password,
			"--kubeconfig", outputPath,
			"--insecure-skip-tls-verify")

		var ocStderr bytes.Buffer
		ocCmd.Stderr = &ocStderr

		if err := ocCmd.Run(); err != nil {
			lastErr = fmt.Errorf("oc login failed (attempt %d/%d): %w\nStderr: %s", attempt, maxRetries, err, ocStderr.String())
			if attempt < maxRetries {
				// Wait before retrying
				time.Sleep(retryDelay)
				continue
			}
			// Final attempt failed
			return nil, lastErr
		}

		// Success
		break
	}

	// Return admin credentials for storage
	return &ROSAAdminCredentials{
		Username: username,
		Password: password,
	}, nil
}

// ListMachinePools lists all machine pools for a ROSA cluster
func (r *ROSAInstaller) ListMachinePools(ctx context.Context, clusterName string) ([]ROSAMachinePool, error) {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return nil, err
	}

	// Use a fresh context with timeout for the list operation
	listCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// rosa list machinepools --cluster <name> -o json
	cmd := exec.CommandContext(listCtx, r.binaryPath, "list", "machinepools",
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

// ROSAOperatorRole represents a ROSA operator IAM role
type ROSAOperatorRole struct {
	ID          string `json:"id"`
	RoleName    string `json:"role_name"`
	RoleARN     string `json:"role_arn"`
	ServiceAccount string `json:"service_account"`
}

// DeleteOperatorRoles deletes operator IAM roles for a ROSA cluster
// Returns the list of deleted role ARNs and any error encountered
func (r *ROSAInstaller) DeleteOperatorRoles(ctx context.Context, clusterID string) ([]string, error) {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return nil, err
	}

	// rosa delete operator-roles --cluster <id> --yes --mode auto
	cmd := exec.CommandContext(ctx, r.binaryPath, "delete", "operator-roles",
		"--cluster", clusterID,
		"--yes",
		"--mode", "auto")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("rosa delete operator-roles failed: %w\nStderr: %s", err, stderr.String())
	}

	// Parse output to extract deleted role ARNs
	deletedRoles := []string{}
	output := stdout.String()
	for _, line := range strings.Split(output, "\n") {
		// Look for lines containing role ARNs
		if strings.Contains(line, "arn:aws:iam::") && strings.Contains(line, ":role/") {
			// Extract ARN from line
			start := strings.Index(line, "arn:aws:iam::")
			if start != -1 {
				// Find end of ARN (usually ends with whitespace or newline)
				end := strings.IndexAny(line[start:], " \t\n")
				if end == -1 {
					deletedRoles = append(deletedRoles, strings.TrimSpace(line[start:]))
				} else {
					deletedRoles = append(deletedRoles, strings.TrimSpace(line[start:start+end]))
				}
			}
		}
	}

	return deletedRoles, nil
}

// DeleteOIDCProvider deletes the OIDC provider for a ROSA cluster
func (r *ROSAInstaller) DeleteOIDCProvider(ctx context.Context, clusterID string) error {
	// Ensure rosa is authenticated
	if err := r.ensureLoggedIn(ctx); err != nil {
		return err
	}

	// rosa delete oidc-provider --cluster <id> --yes --mode auto
	cmd := exec.CommandContext(ctx, r.binaryPath, "delete", "oidc-provider",
		"--cluster", clusterID,
		"--yes",
		"--mode", "auto")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// OIDC provider may not exist (cluster created before STS, or already deleted)
		// Don't fail if it's just a "not found" error
		stderrStr := stderr.String()
		if strings.Contains(stderrStr, "not found") || strings.Contains(stderrStr, "does not exist") {
			return nil
		}
		return fmt.Errorf("rosa delete oidc-provider failed: %w\nStderr: %s", err, stderrStr)
	}

	return nil
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
