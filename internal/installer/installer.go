package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	// Step 2: Tag Route53 hosted zone for cluster discovery
	fmt.Printf("Tagging Route53 hosted zone for cluster discovery...\n")
	if err := i.tagRoute53Zone(ctx, workDir); err != nil {
		log.Printf("Warning: failed to tag Route53 zone: %v", err)
		// Don't fail - zone might already be tagged or might not be using Route53
	}

	// Step 3: Run ccoctl to create IAM resources and generate credential manifests
	fmt.Printf("Running ccoctl to create IAM roles and credential manifests...\n")
	if err := i.runCCOCtl(ctx, workDir); err != nil {
		return "", fmt.Errorf("run ccoctl: %w", err)
	}

	// Step 4: Run create cluster
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
	// Read cluster name and region from install-config for ccoctl parameters
	clusterName, region, err := i.getClusterInfo(workDir)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	// Create temporary directory for CredentialsRequests
	credsReqDir := filepath.Join(workDir, "credentialsrequests")
	if err := os.MkdirAll(credsReqDir, 0755); err != nil {
		return fmt.Errorf("create credentials requests dir: %w", err)
	}
	defer os.RemoveAll(credsReqDir)

	// Extract CredentialsRequests from release image
	log.Printf("Extracting CredentialsRequests from manifests...")
	if err := i.extractCredentialRequests(ctx, workDir, credsReqDir); err != nil {
		return fmt.Errorf("extract credential requests: %w", err)
	}

	// Create output directory for ccoctl
	ccoOutputDir := filepath.Join(workDir, "cco-output")
	if err := os.MkdirAll(ccoOutputDir, 0755); err != nil {
		return fmt.Errorf("create cco output dir: %w", err)
	}
	defer os.RemoveAll(ccoOutputDir)

	// Run ccoctl to create IAM resources and manifests
	log.Printf("Running ccoctl to create IAM roles for cluster %s in region %s...", clusterName, region)
	if err := i.executeCCOCtl(ctx, clusterName, region, credsReqDir, ccoOutputDir); err != nil {
		return fmt.Errorf("execute ccoctl: %w", err)
	}

	// Copy generated manifests back into install directory
	log.Printf("Copying generated manifests to install directory...")
	if err := i.copyManifests(ccoOutputDir, workDir); err != nil {
		return fmt.Errorf("copy manifests: %w", err)
	}

	log.Printf("Successfully created IAM resources and credential manifests")
	return nil
}

// getClusterInfo extracts cluster name and region from install-config.yaml
func (i *Installer) getClusterInfo(workDir string) (string, string, error) {
	// For now, default to reading from environment or standard AWS region
	// TODO: Parse install-config.yaml to get actual cluster name and region
	clusterName := filepath.Base(workDir) // Use work dir name as cluster name
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1" // Default region
	}

	return clusterName, region, nil
}

// extractCredentialRequests extracts CredentialsRequest manifests from the release image
// using oc adm release extract (required for Manual credentials mode)
func (i *Installer) extractCredentialRequests(ctx context.Context, workDir, outputDir string) error {
	// Read the release image from install-config or detect from openshift-install
	releaseImage, err := i.getReleaseImage(ctx)
	if err != nil {
		return fmt.Errorf("get release image: %w", err)
	}

	log.Printf("Extracting CredentialsRequests from release image: %s", releaseImage)

	// Get pull secret from environment
	pullSecret := os.Getenv("OPENSHIFT_PULL_SECRET")
	if pullSecret == "" {
		return fmt.Errorf("OPENSHIFT_PULL_SECRET environment variable not set")
	}

	// Write pull secret to temporary file
	pullSecretFile := filepath.Join(workDir, ".dockerconfigjson")
	if err := os.WriteFile(pullSecretFile, []byte(pullSecret), 0600); err != nil {
		return fmt.Errorf("write pull secret: %w", err)
	}
	defer os.Remove(pullSecretFile)

	// Use oc adm release extract to get CredentialsRequests from the release image
	cmd := exec.CommandContext(ctx, "oc", "adm", "release", "extract",
		"--credentials-requests",
		"--cloud=aws",
		"--to="+outputDir,
		"--from="+releaseImage,
		"--registry-config="+pullSecretFile,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("Running: oc adm release extract --credentials-requests --cloud=aws --to=%s --from=%s", outputDir, releaseImage)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oc adm release extract failed: %w\nStdout: %s\nStderr: %s", err, stdout.String(), stderr.String())
	}

	log.Printf("oc output:\n%s", stdout.String())

	// Verify that CredentialsRequest files were extracted
	files, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("read output dir: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no CredentialsRequest manifests extracted from release image")
	}

	log.Printf("Successfully extracted %d CredentialsRequest manifests", len(files))
	return nil
}

// getReleaseImage gets the OpenShift release image version
func (i *Installer) getReleaseImage(ctx context.Context) (string, error) {
	// Run openshift-install version to get release image
	cmd := exec.CommandContext(ctx, i.binaryPath, "version")

	var stdout bytes.Buffer
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("get version: %w", err)
	}

	// Parse output to find release image
	// Output format: "release image quay.io/openshift-release-dev/ocp-release@sha256:..."
	output := stdout.String()
	lines := bytes.Split([]byte(output), []byte("\n"))
	for _, line := range lines {
		if bytes.Contains(line, []byte("release image")) {
			parts := bytes.Fields(line)
			if len(parts) >= 3 {
				return string(parts[2]), nil
			}
		}
	}

	return "", fmt.Errorf("could not parse release image from version output")
}

// executeCCOCtl runs the ccoctl binary to create IAM resources
func (i *Installer) executeCCOCtl(ctx context.Context, clusterName, region, credsReqDir, outputDir string) error {
	// ccoctl aws create-all --name=<name> --region=<region> --credentials-requests-dir=<dir> --output-dir=<dir>
	cmd := exec.CommandContext(ctx, i.ccoCtlPath,
		"aws", "create-all",
		"--name="+clusterName,
		"--region="+region,
		"--credentials-requests-dir="+credsReqDir,
		"--output-dir="+outputDir,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Inherit environment for AWS credentials
	cmd.Env = os.Environ()

	log.Printf("Executing: %s", cmd.String())
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("ccoctl failed: %w\nStdout: %s\nStderr: %s", err, stdout.String(), stderr.String())
	}

	log.Printf("ccoctl output:\n%s", stdout.String())
	return nil
}

// copyManifests copies generated manifests from ccoctl output to install directory
func (i *Installer) copyManifests(ccoOutputDir, workDir string) error {
	// ccoctl generates manifests in outputDir/manifests/
	srcManifestsDir := filepath.Join(ccoOutputDir, "manifests")
	dstManifestsDir := filepath.Join(workDir, "manifests")

	// Copy all files from src to dst
	files, err := os.ReadDir(srcManifestsDir)
	if err != nil {
		return fmt.Errorf("read cco manifests dir: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		srcFile := filepath.Join(srcManifestsDir, file.Name())
		dstFile := filepath.Join(dstManifestsDir, file.Name())

		data, err := os.ReadFile(srcFile)
		if err != nil {
			return fmt.Errorf("read %s: %w", srcFile, err)
		}

		if err := os.WriteFile(dstFile, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", dstFile, err)
		}

		log.Printf("Copied manifest: %s", file.Name())
	}

	return nil
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

// tagRoute53Zone tags the Route53 hosted zone for cluster discovery by the ingress operator
func (i *Installer) tagRoute53Zone(ctx context.Context, workDir string) error {
	// Read .openshift_install_state.json to get the cluster infrastructure ID
	// This file is created during manifest generation and contains the infraID
	stateFilePath := filepath.Join(workDir, ".openshift_install_state.json")
	stateFileBytes, err := os.ReadFile(stateFilePath)
	if err != nil {
		return fmt.Errorf("read .openshift_install_state.json: %w", err)
	}

	// Parse the state file to extract infraID and baseDomain
	var state struct {
		ClusterID string `json:"*installconfig.ClusterID"`
		Config    struct {
			BaseDomain string `json:"baseDomain"`
			Metadata   struct {
				Name string `json:"name"`
			} `json:"metadata"`
		} `json:"*installconfig.InstallConfig"`
	}

	// The state file is a JSON object with keys like "*installconfig.ClusterID"
	// We need to parse it as a map first
	var stateMap map[string]json.RawMessage
	if err := json.Unmarshal(stateFileBytes, &stateMap); err != nil {
		return fmt.Errorf("parse state file: %w", err)
	}

	// Extract infraID from *installconfig.ClusterID
	var clusterIDData struct {
		InfraID string `json:"InfraID"`
	}
	if clusterIDRaw, ok := stateMap["*installconfig.ClusterID"]; ok {
		if err := json.Unmarshal(clusterIDRaw, &clusterIDData); err != nil {
			return fmt.Errorf("parse cluster ID: %w", err)
		}
	}

	infraID := clusterIDData.InfraID
	if infraID == "" {
		return fmt.Errorf("no infraID found in state file")
	}

	log.Printf("Found cluster infrastructure ID: %s", infraID)

	// Extract baseDomain from *installconfig.InstallConfig
	var installConfigData struct {
		BaseDomain string `json:"baseDomain"`
	}
	if installConfigRaw, ok := stateMap["*installconfig.InstallConfig"]; ok {
		if err := json.Unmarshal(installConfigRaw, &installConfigData); err != nil {
			return fmt.Errorf("parse install config: %w", err)
		}
	}

	baseDomain := installConfigData.BaseDomain
	if baseDomain == "" {
		return fmt.Errorf("no baseDomain found in state file")
	}

	log.Printf("Found base domain: %s", baseDomain)

	// Use AWS CLI to find and tag the hosted zone
	// First, find the hosted zone ID for the base domain
	cmd := exec.CommandContext(ctx, "aws", "route53", "list-hosted-zones-by-name",
		"--dns-name", baseDomain,
		"--max-items", "1",
		"--query", "HostedZones[0].Id",
		"--output", "text")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("list hosted zones: %w\nStderr: %s", err, stderr.String())
	}

	hostedZoneID := strings.TrimSpace(stdout.String())
	if hostedZoneID == "" || hostedZoneID == "None" {
		return fmt.Errorf("no hosted zone found for domain %s", baseDomain)
	}

	// Extract just the zone ID (remove /hostedzone/ prefix if present)
	hostedZoneID = strings.TrimPrefix(hostedZoneID, "/hostedzone/")

	log.Printf("Found hosted zone ID: %s for domain %s", hostedZoneID, baseDomain)

	// Tag the hosted zone with cluster ownership tag
	clusterTag := fmt.Sprintf("kubernetes.io/cluster/%s", infraID)
	cmd = exec.CommandContext(ctx, "aws", "route53", "change-tags-for-resource",
		"--resource-type", "hostedzone",
		"--resource-id", hostedZoneID,
		"--add-tags", fmt.Sprintf("Key=%s,Value=owned", clusterTag))

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	stdout.Reset()
	stderr.Reset()

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("tag hosted zone: %w\nStderr: %s", err, stderr.String())
	}

	log.Printf("Successfully tagged hosted zone %s with %s=owned", hostedZoneID, clusterTag)

	return nil
}
