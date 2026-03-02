package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"
)

// Installer wraps the openshift-install CLI
type Installer struct {
	binaryPath  string
	ccoCtlPath  string
	timeout     time.Duration
	useSTSCreds bool // true if using temporary STS/IMDS credentials
}

// CredentialType represents the type of AWS credentials in use
type CredentialType int

const (
	CredentialTypeStatic CredentialType = iota
	CredentialTypeSTSIMDS
)

// NewInstaller creates a new installer instance
func NewInstaller() *Installer {
	// Get binary path from environment or use default
	binaryPath := os.Getenv("OPENSHIFT_INSTALL_BINARY")
	if binaryPath == "" {
		binaryPath = "openshift-install" // Assumes it's in PATH
	}

	ccoCtlPath := os.Getenv("CCOCTL_BINARY")
	if ccoCtlPath == "" {
		ccoCtlPath = "ccoctl" // Assumes it's in PATH
	}

	// Detect if we're using STS/IMDS credentials
	useSTSCreds := detectSTSCredentials()

	return &Installer{
		binaryPath:  binaryPath,
		ccoCtlPath:  ccoCtlPath,
		timeout:     120 * time.Minute, // Default 120 minute timeout for cluster installations
		useSTSCreds: useSTSCreds,
	}
}

// detectSTSCredentials checks if we're using temporary STS credentials (IMDS or explicit session token)
func detectSTSCredentials() bool {
	// Check for explicit AWS_SESSION_TOKEN (indicates STS credentials)
	if os.Getenv("AWS_SESSION_TOKEN") != "" {
		return true
	}

	// Check if running on EC2 with instance profile (IMDS available)
	// Try to fetch from IMDS - if successful, we're using instance profile
	creds, err := getAWSCredentialsFromIMDS()
	if err == nil && creds.Token != "" {
		// IMDS credentials include a session token, so they're STS-based
		return true
	}

	// No STS credentials detected
	return false
}

// CreateCluster runs openshift-install create cluster
// If using STS/IMDS credentials, uses Manual mode with ccoctl workflow
func (i *Installer) CreateCluster(ctx context.Context, workDir string) (string, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	// If using STS credentials, we must use Manual mode with ccoctl workflow
	if i.useSTSCreds {
		return i.createClusterManualMode(ctx, workDir)
	}

	// Static credentials - direct cluster creation
	return i.createClusterDirect(ctx, workDir)
}

// createClusterDirect runs openshift-install create cluster directly (for static credentials)
func (i *Installer) createClusterDirect(ctx context.Context, workDir string) (string, error) {
	cmd := exec.CommandContext(ctx, i.binaryPath, "create", "cluster", "--dir", workDir, "--log-level=debug")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("OPENSHIFT_INSTALL_INVOKER=ocpctl"),
	)

	err := cmd.Run()
	if err != nil {
		return stderr.String(), fmt.Errorf("openshift-install create cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// createClusterManualMode implements the Manual credentials mode workflow with ccoctl
// This is required when using STS/IMDS credentials which cannot be used with Mint/Passthrough modes
func (i *Installer) createClusterManualMode(ctx context.Context, workDir string) (string, error) {
	// Step 1: Create manifests
	fmt.Printf("Creating manifests for Manual mode (STS credentials detected)...\n")
	if err := i.createManifests(ctx, workDir); err != nil {
		return "", fmt.Errorf("create manifests: %w", err)
	}

	// Step 2: Run ccoctl to create IAM resources and generate credential manifests
	fmt.Printf("Running ccoctl to create IAM roles and credential manifests...\n")
	if err := i.runCCOCtl(ctx, workDir); err != nil {
		return "", fmt.Errorf("run ccoctl: %w", err)
	}

	// Step 3: Run create cluster
	fmt.Printf("Creating cluster with Manual credentials mode...\n")
	return i.createClusterDirect(ctx, workDir)
}

// createManifests runs openshift-install create manifests
func (i *Installer) createManifests(ctx context.Context, workDir string) error {
	cmd := exec.CommandContext(ctx, i.binaryPath, "create", "manifests", "--dir", workDir, "--log-level=debug")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("OPENSHIFT_INSTALL_INVOKER=ocpctl"),
	)

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("create manifests failed: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// runCCOCtl runs ccoctl to create IAM resources and generate credential manifests
func (i *Installer) runCCOCtl(ctx context.Context, workDir string) error {
	// TODO: Implement ccoctl workflow
	// For Phase 1, return an error with clear instructions
	return fmt.Errorf("Manual credentials mode with ccoctl is not yet implemented.\n\n" +
		"You are using STS/IMDS credentials (EC2 instance profile), which require Manual mode.\n" +
		"Manual mode requires running 'ccoctl aws create-all' to provision IAM roles.\n\n" +
		"To proceed:\n" +
		"1. Install ccoctl (https://docs.openshift.com/container-platform/latest/installing/installing_aws/manually-creating-iam.html)\n" +
		"2. Run: openshift-install create manifests --dir %s\n" +
		"3. Extract CredentialsRequests: oc adm release extract --credentials-requests --cloud=aws\n" +
		"4. Run: ccoctl aws create-all --name=<cluster> --region=<region> --credentials-requests-dir=<dir>\n" +
		"5. Copy generated manifests to: %s/manifests/ and %s/openshift/\n" +
		"6. Run: openshift-install create cluster --dir %s\n\n" +
		"Full automation with ccoctl coming in a future update.", workDir, workDir, workDir, workDir)
}

// DestroyCluster runs openshift-install destroy cluster
func (i *Installer) DestroyCluster(ctx context.Context, workDir string) (string, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, i.binaryPath, "destroy", "cluster", "--dir", workDir, "--log-level=debug")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Set environment variables
	// Note: For Passthrough mode, let AWS SDK discover everything from IMDS naturally
	// Do not set AWS_REGION or other AWS env vars - let it discover from instance metadata
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("OPENSHIFT_INSTALL_INVOKER=ocpctl"),
	)

	err := cmd.Run()
	if err != nil {
		return stderr.String(), fmt.Errorf("openshift-install destroy cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// Version returns the openshift-install version
func (i *Installer) Version() (string, error) {
	cmd := exec.Command(i.binaryPath, "version")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("openshift-install version failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// IMDSCredentials represents credentials from EC2 instance metadata
type IMDSCredentials struct {
	AccessKeyID     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	Token           string `json:"Token"`
}

// getAWSCredentialsFromIMDS fetches AWS credentials from EC2 instance metadata service
func getAWSCredentialsFromIMDS() (*IMDSCredentials, error) {
	const imdsTokenURL = "http://169.254.169.254/latest/api/token"
	const imdsRoleURL = "http://169.254.169.254/latest/meta-data/iam/security-credentials/"

	client := &http.Client{Timeout: 5 * time.Second}

	// Get IMDSv2 token
	tokenReq, err := http.NewRequest("PUT", imdsTokenURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	tokenReq.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")

	tokenResp, err := client.Do(tokenReq)
	if err != nil {
		return nil, fmt.Errorf("fetch IMDS token: %w", err)
	}
	defer tokenResp.Body.Close()

	tokenBytes, err := io.ReadAll(tokenResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token: %w", err)
	}
	token := string(tokenBytes)

	// Get role name
	roleReq, err := http.NewRequest("GET", imdsRoleURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create role request: %w", err)
	}
	roleReq.Header.Set("X-aws-ec2-metadata-token", token)

	roleResp, err := client.Do(roleReq)
	if err != nil {
		return nil, fmt.Errorf("fetch role name: %w", err)
	}
	defer roleResp.Body.Close()

	roleBytes, err := io.ReadAll(roleResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read role name: %w", err)
	}
	roleName := string(roleBytes)

	// Get credentials
	credsURL := imdsRoleURL + roleName
	credsReq, err := http.NewRequest("GET", credsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create credentials request: %w", err)
	}
	credsReq.Header.Set("X-aws-ec2-metadata-token", token)

	credsResp, err := client.Do(credsReq)
	if err != nil {
		return nil, fmt.Errorf("fetch credentials: %w", err)
	}
	defer credsResp.Body.Close()

	var creds IMDSCredentials
	if err := json.NewDecoder(credsResp.Body).Decode(&creds); err != nil {
		return nil, fmt.Errorf("decode credentials: %w", err)
	}

	return &creds, nil
}
