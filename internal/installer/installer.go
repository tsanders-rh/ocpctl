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
	binaryPath string
	timeout    time.Duration
}

// NewInstaller creates a new installer instance
func NewInstaller() *Installer {
	// Get binary path from environment or use default
	binaryPath := os.Getenv("OPENSHIFT_INSTALL_BINARY")
	if binaryPath == "" {
		binaryPath = "openshift-install" // Assumes it's in PATH
	}

	return &Installer{
		binaryPath: binaryPath,
		timeout:    120 * time.Minute, // Default 120 minute timeout for cluster installations
	}
}

// CreateCluster runs openshift-install create cluster
func (i *Installer) CreateCluster(ctx context.Context, workDir string) (string, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, i.binaryPath, "create", "cluster", "--dir", workDir, "--log-level=debug")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Fetch AWS credentials from IMDS and export as environment variables
	// This works around openshift-install not reliably using EC2 instance metadata
	creds, err := getAWSCredentialsFromIMDS()
	if err != nil {
		return "", fmt.Errorf("fetch AWS credentials from IMDS: %w", err)
	}

	// Set environment variables with explicit AWS credentials
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("OPENSHIFT_INSTALL_INVOKER=ocpctl"),
		fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", creds.AccessKeyID),
		fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", creds.SecretAccessKey),
		fmt.Sprintf("AWS_SESSION_TOKEN=%s", creds.Token),
		"AWS_REGION=us-east-1",
		"AWS_STS_REGIONAL_ENDPOINTS=regional",
	)

	err = cmd.Run()
	if err != nil {
		return stderr.String(), fmt.Errorf("openshift-install create cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
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

	// Fetch AWS credentials from IMDS and export as environment variables
	creds, err := getAWSCredentialsFromIMDS()
	if err != nil {
		return "", fmt.Errorf("fetch AWS credentials from IMDS: %w", err)
	}

	// Set environment variables with explicit AWS credentials
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("OPENSHIFT_INSTALL_INVOKER=ocpctl"),
		fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", creds.AccessKeyID),
		fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", creds.SecretAccessKey),
		fmt.Sprintf("AWS_SESSION_TOKEN=%s", creds.Token),
		"AWS_REGION=us-east-1",
		"AWS_STS_REGIONAL_ENDPOINTS=regional",
	)

	err = cmd.Run()
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
