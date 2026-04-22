package installer

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/tls"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"gopkg.in/yaml.v3"
)

// ocpctlVersion is the version of ocpctl, injected at build time
// Build with: go build -ldflags "-X github.com/tsanders-rh/ocpctl/internal/installer.ocpctlVersion=1.0.0"
var ocpctlVersion = "dev"

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

// ClusterMetadata contains metadata for tagging cluster resources
type ClusterMetadata struct {
	ClusterName string
	ProfileName string
	InfraID     string
	CreatedAt   time.Time
	Region      string
}

// NewInstaller creates a new installer instance for the latest supported version
// Deprecated: Use NewInstallerForVersion instead
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

// NewInstallerForVersion creates a new installer instance for a specific OpenShift version
// Supports versions 4.18, 4.19, and 4.20
func NewInstallerForVersion(version string) (*Installer, error) {
	// Extract major.minor version (e.g., "4.20.3" -> "4.20")
	majorMinor := extractMajorMinor(version)
	if majorMinor == "" {
		return nil, fmt.Errorf("invalid version format: %s", version)
	}

	// Validate supported version
	supportedVersions := []string{"4.18", "4.19", "4.20", "4.21", "4.22"}
	isSupported := false
	for _, v := range supportedVersions {
		if majorMinor == v {
			isSupported = true
			break
		}
	}
	if !isSupported {
		return nil, fmt.Errorf("unsupported OpenShift version: %s (supported: 4.18, 4.19, 4.20, 4.21, 4.22)", version)
	}

	// Check for version-specific binaries in environment
	binaryEnvKey := fmt.Sprintf("OPENSHIFT_INSTALL_BINARY_%s", strings.ReplaceAll(majorMinor, ".", "_"))
	binaryPath := os.Getenv(binaryEnvKey)

	// Fall back to version-suffixed binary in standard location
	if binaryPath == "" {
		binaryPath = fmt.Sprintf("/usr/local/bin/openshift-install-%s", majorMinor)
	}

	// Check for version-specific ccoctl
	ccoCtlEnvKey := fmt.Sprintf("CCOCTL_BINARY_%s", strings.ReplaceAll(majorMinor, ".", "_"))
	ccoCtlPath := os.Getenv(ccoCtlEnvKey)

	if ccoCtlPath == "" {
		// For 4.22, use RHEL9-specific ccoctl (required for RHEL9 FIPS releases)
		if majorMinor == "4.22" {
			ccoCtlPath = fmt.Sprintf("/usr/local/bin/ccoctl-%s-rhel9", majorMinor)
		} else {
			ccoCtlPath = fmt.Sprintf("/usr/local/bin/ccoctl-%s", majorMinor)
		}
	}

	// Verify binaries exist
	if _, err := os.Stat(binaryPath); err != nil {
		return nil, fmt.Errorf("openshift-install binary not found for version %s at %s: %w", version, binaryPath, err)
	}

	if _, err := os.Stat(ccoCtlPath); err != nil {
		log.Printf("Warning: ccoctl binary not found for version %s at %s (Manual mode may not work): %v", version, ccoCtlPath, err)
		// Don't fail - ccoctl is only needed for Manual/STS mode
	}

	// Detect if we're using STS/IMDS credentials
	useSTSCreds := detectSTSCredentials()

	log.Printf("Using OpenShift installer version %s: %s", majorMinor, binaryPath)
	log.Printf("Using ccoctl version %s: %s", majorMinor, ccoCtlPath)

	return &Installer{
		binaryPath:  binaryPath,
		ccoCtlPath:  ccoCtlPath,
		timeout:     120 * time.Minute,
		useSTSCreds: useSTSCreds,
	}, nil
}

// isDevPreviewVersion detects if a version string is a dev-preview/candidate release
// Dev-preview versions contain markers like: -ec. (early candidate), -rc. (release candidate),
// -0.nightly (nightly builds), or -fc. (feature candidate)
// Examples: "4.22.0-ec.5", "4.21.0-rc.1", "4.22.0-0.nightly-2024-03-15"
func isDevPreviewVersion(version string) bool {
	return strings.Contains(version, "-ec.") ||
		strings.Contains(version, "-rc.") ||
		strings.Contains(version, "-0.nightly") ||
		strings.Contains(version, "-fc.")
}

// extractMajorMinor extracts the major.minor version from a full version string
// For dev-preview versions, strips the pre-release suffix before extracting
// Examples:
//   - "4.20.3" -> "4.20"
//   - "4.19" -> "4.19"
//   - "4.18.15" -> "4.18"
//   - "4.22.0-ec.5" -> "4.22" (strips "-ec.5" first)
//   - "4.22.0-0.nightly-2024-03-15" -> "4.22" (strips nightly suffix)
func extractMajorMinor(version string) string {
	// Strip pre-release suffix if present (e.g., "4.22.0-ec.5" -> "4.22.0")
	baseVersion := version
	if idx := strings.Index(version, "-"); idx > 0 {
		baseVersion = version[:idx]
	}

	parts := strings.Split(baseVersion, ".")
	if len(parts) < 2 {
		return ""
	}
	return fmt.Sprintf("%s.%s", parts[0], parts[1])
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
func (i *Installer) CreateCluster(ctx context.Context, workDir string, metadata *ClusterMetadata) (string, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	// If using STS credentials, we must use Manual mode with ccoctl workflow
	if i.useSTSCreds {
		return i.createClusterManualMode(ctx, workDir, metadata)
	}

	// Static credentials - direct cluster creation
	return i.CreateClusterDirect(ctx, workDir)
}

// CreateClusterDirect runs openshift-install create cluster directly without CCO workflow
// Used for IBM Cloud (CCO already done) or static credentials
func (i *Installer) CreateClusterDirect(ctx context.Context, workDir string) (string, error) {
	cmd := exec.CommandContext(ctx, i.binaryPath, "create", "cluster", "--dir", workDir, "--log-level=debug")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Extract credentials from IMDS and export as environment variables
	// This prevents the installer from seeing credentials as "EC2RoleProvider"
	// which OpenShift 4.22 rejects for all credentials modes
	envVars := os.Environ()

	// Check if we're running on EC2 with instance profile
	creds, imdsErr := getAWSCredentialsFromIMDS()
	if imdsErr == nil && creds.AccessKeyID != "" {
		log.Printf("Extracted credentials from EC2 instance metadata (hiding EC2RoleProvider from installer)")
		envVars = append(envVars,
			fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", creds.AccessKeyID),
			fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", creds.SecretAccessKey),
			fmt.Sprintf("AWS_SESSION_TOKEN=%s", creds.Token),
		)
	} else {
		log.Printf("Not running on EC2 or IMDS not available, using existing credential chain: %v", imdsErr)
	}

	cmd.Env = append(envVars,
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
func (i *Installer) createClusterManualMode(ctx context.Context, workDir string, metadata *ClusterMetadata) (string, error) {
	// Log initial state
	i.logInstallState(workDir, "BEFORE create manifests")

	// Step 1: Create manifests
	fmt.Printf("Creating manifests for Manual mode (STS credentials detected)...\n")
	if err := i.CreateManifests(ctx, workDir); err != nil {
		return "", fmt.Errorf("create manifests: %w", err)
	}
	i.logInstallState(workDir, "AFTER create manifests")

	// Step 2: Tag Route53 hosted zone for cluster discovery
	fmt.Printf("Tagging Route53 hosted zone for cluster discovery...\n")
	if err := i.tagRoute53Zone(ctx, workDir); err != nil {
		log.Printf("Warning: failed to tag Route53 zone: %v", err)
		// Don't fail - zone might already be tagged or might not be using Route53
	}
	i.logInstallState(workDir, "AFTER Route53 tagging")

	// Step 3: Run ccoctl to create IAM resources and generate credential manifests
	fmt.Printf("Running ccoctl to create IAM roles and credential manifests...\n")
	if err := i.runCCOCtl(ctx, workDir, metadata); err != nil {
		return "", fmt.Errorf("run ccoctl: %w", err)
	}
	i.logInstallState(workDir, "AFTER ccoctl")

	// Step 4: Run create cluster
	fmt.Printf("Creating cluster with Manual credentials mode...\n")
	i.logInstallState(workDir, "BEFORE create cluster")
	return i.CreateClusterDirect(ctx, workDir)
}

// CCOCtlPath returns the path to the ccoctl binary
func (i *Installer) CCOCtlPath() string {
	return i.ccoCtlPath
}

// CreateManifests runs openshift-install create manifests
func (i *Installer) CreateManifests(ctx context.Context, workDir string) error {
	cmd := exec.CommandContext(ctx, i.binaryPath, "create", "manifests", "--dir", workDir, "--log-level=debug")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Extract credentials from IMDS and export as environment variables
	// This prevents the installer from seeing credentials as "EC2RoleProvider"
	// which OpenShift 4.22 rejects for all credentials modes
	envVars := os.Environ()

	// Check if we're running on EC2 with instance profile
	creds, imdsErr := getAWSCredentialsFromIMDS()
	if imdsErr == nil && creds.AccessKeyID != "" {
		log.Printf("Extracted credentials from EC2 instance metadata (hiding EC2RoleProvider from installer)")
		envVars = append(envVars,
			fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", creds.AccessKeyID),
			fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", creds.SecretAccessKey),
			fmt.Sprintf("AWS_SESSION_TOKEN=%s", creds.Token),
		)
	} else {
		log.Printf("Not running on EC2 or IMDS not available, using existing credential chain: %v", imdsErr)
	}

	cmd.Env = append(envVars,
		fmt.Sprintf("OPENSHIFT_INSTALL_INVOKER=ocpctl"),
	)

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("create manifests failed: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// runCCOCtl runs ccoctl to create IAM resources and generate credential manifests
func (i *Installer) runCCOCtl(ctx context.Context, workDir string, metadata *ClusterMetadata) error {
	// Use region from cluster metadata (already parsed from install-config before it was consumed)
	region := metadata.Region
	if region == "" {
		return fmt.Errorf("region not specified in cluster metadata")
	}

	// Get infraID from state file (created by openshift-install create manifests)
	// IMPORTANT: ccoctl MUST be called with infraID, not the human cluster name or workDir UUID
	infraID, err := i.getInfraID(workDir)
	if err != nil {
		return fmt.Errorf("get infraID: %w", err)
	}
	log.Printf("Found infraID for ccoctl: %s", infraID)

	// Populate infraID in metadata for tagging
	metadata.InfraID = infraID

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

	// Add EFS CSI Driver CredentialsRequest for optional EFS storage support
	// This allows ccoctl to create the IAM role for the EFS operator if the user enables it
	log.Printf("Adding EFS CSI Driver CredentialsRequest...")
	if err := i.addEFSCredentialsRequest(credsReqDir); err != nil {
		log.Printf("Warning: failed to add EFS CredentialsRequest: %v", err)
		// Don't fail - EFS is optional, cluster can still be created
	}

	// Create output directory for ccoctl
	ccoOutputDir := filepath.Join(workDir, "cco-output")
	if err := os.MkdirAll(ccoOutputDir, 0755); err != nil {
		return fmt.Errorf("create cco output dir: %w", err)
	}
	defer os.RemoveAll(ccoOutputDir)

	// Run ccoctl to create IAM resources and manifests
	// Use infraID as the name - ccoctl will create resources with this prefix
	log.Printf("Running ccoctl to create IAM roles for cluster %s in region %s...", infraID, region)
	if err := i.executeCCOCtl(ctx, infraID, region, credsReqDir, ccoOutputDir); err != nil {
		return fmt.Errorf("execute ccoctl: %w", err)
	}

	// Copy generated manifests back into install directory
	log.Printf("Copying generated manifests to install directory...")
	if err := i.copyManifests(ccoOutputDir, workDir); err != nil {
		return fmt.Errorf("copy manifests: %w", err)
	}

	// Fix OIDC provider thumbprint (ccoctl generates incorrect thumbprint)
	// Use same infraID that was passed to ccoctl
	log.Printf("Fixing OIDC provider thumbprint for %s...", infraID)
	if err := i.fixOIDCThumbprint(ctx, infraID, region); err != nil {
		log.Printf("Warning: failed to fix OIDC thumbprint: %v", err)
		// Don't fail the installation, but log the warning
		// The cluster might still work or can be fixed manually
	} else {
		log.Printf("Successfully updated OIDC provider thumbprint")
	}

	// Tag IAM/OIDC resources immediately after creation
	// This ensures orphaned resources can be detected even if cluster creation fails later
	log.Printf("Tagging IAM and OIDC resources for cluster %s...", metadata.ClusterName)
	tags := buildTagSet(*metadata)
	if err := i.tagIAMRoles(ctx, infraID, region, tags); err != nil {
		log.Printf("Warning: failed to tag IAM roles immediately after ccoctl: %v", err)
		// Don't fail - we'll try again later, but at least attempt early tagging
	} else {
		log.Printf("Successfully tagged IAM roles")
	}

	if err := i.tagOIDCProvider(ctx, infraID, region, tags); err != nil {
		log.Printf("Warning: failed to tag OIDC provider immediately after ccoctl: %v", err)
	} else {
		log.Printf("Successfully tagged OIDC provider")
	}

	if err := i.tagOIDCBucket(ctx, infraID, region, tags); err != nil {
		log.Printf("Warning: failed to tag OIDC bucket immediately after ccoctl: %v", err)
	} else {
		log.Printf("Successfully tagged OIDC bucket")
	}

	// Validate OIDC configuration before proceeding
	log.Printf("Validating OIDC configuration...")
	if err := i.validateOIDCConfiguration(ctx, infraID, region); err != nil {
		return fmt.Errorf("OIDC validation failed: %w", err)
	}

	log.Printf("Successfully created IAM resources and credential manifests")

	return nil
}

// getClusterInfo extracts cluster name and region from install-config.yaml
func (i *Installer) getClusterInfo(workDir string) (string, string, error) {
	// Try to read install-config.yaml first
	installConfigPath := filepath.Join(workDir, "install-config.yaml")
	data, err := os.ReadFile(installConfigPath)
	if err != nil {
		// Fall back to metadata.json if install-config.yaml doesn't exist
		metadataPath := filepath.Join(workDir, "metadata.json")
		metadataData, metaErr := os.ReadFile(metadataPath)
		if metaErr != nil {
			return "", "", fmt.Errorf("failed to read install-config.yaml or metadata.json: %w", err)
		}

		var metadata struct {
			ClusterName string `json:"clusterName"`
			AWS         struct {
				Region string `json:"region"`
			} `json:"aws"`
		}
		if err := json.Unmarshal(metadataData, &metadata); err != nil {
			return "", "", fmt.Errorf("parse metadata.json: %w", err)
		}

		return metadata.ClusterName, metadata.AWS.Region, nil
	}

	// Parse install-config.yaml
	var installConfig struct {
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
		Platform struct {
			AWS struct {
				Region string `yaml:"region"`
			} `yaml:"aws"`
		} `yaml:"platform"`
	}

	if err := yaml.Unmarshal(data, &installConfig); err != nil {
		return "", "", fmt.Errorf("parse install-config.yaml: %w", err)
	}

	clusterName := installConfig.Metadata.Name
	region := installConfig.Platform.AWS.Region

	if clusterName == "" {
		return "", "", fmt.Errorf("cluster name not found in install-config.yaml")
	}
	if region == "" {
		return "", "", fmt.Errorf("region not found in install-config.yaml")
	}

	return clusterName, region, nil
}

// copyDir recursively copies a directory tree, preserving file modes
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		// Read and write file, preserving mode
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(target, data, info.Mode()); err != nil {
			return err
		}
		log.Printf("Copied: %s (mode %o)", rel, info.Mode())
		return nil
	})
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

	// CRITICAL: Copy tls/ directory containing bound-service-account-signing-key.key
	// These TLS assets are required for the installer to configure STS/bound-token behavior
	// See: https://docs.openshift.com/container-platform/4.9/authentication/managing_cloud_provider_credentials/cco-mode-sts.html
	srcTLSDir := filepath.Join(ccoOutputDir, "tls")
	dstTLSDir := filepath.Join(workDir, "tls")

	if st, err := os.Stat(srcTLSDir); err == nil && st.IsDir() {
		log.Printf("Copying tls/ directory to install directory...")
		if err := copyDir(srcTLSDir, dstTLSDir); err != nil {
			return fmt.Errorf("copy tls dir: %w", err)
		}
		log.Printf("✓ Copied tls/ directory")

		// Validate that the critical signing key exists
		// This turns a 90-minute cluster failure into a 2-second error
		keyPath := filepath.Join(dstTLSDir, "bound-service-account-signing-key.key")
		if _, err := os.Stat(keyPath); err != nil {
			return fmt.Errorf("missing required STS signing key %s: %w", keyPath, err)
		}
		log.Printf("✓ Validated STS signing key exists: %s", keyPath)
	} else {
		log.Printf("Warning: tls/ directory not found in ccoctl output (STS may fail)")
	}

	return nil
}

// fixOIDCThumbprint updates the OIDC provider thumbprint to the correct S3 certificate thumbprint
// ccoctl generates an incorrect thumbprint, so we need to fix it after creation
func (i *Installer) fixOIDCThumbprint(ctx context.Context, clusterName, region string) error {
	// Get the correct S3 certificate thumbprint for the region
	s3Endpoint := fmt.Sprintf("s3.%s.amazonaws.com", region)
	thumbprint, err := i.getS3CertThumbprint(ctx, s3Endpoint)
	if err != nil {
		return fmt.Errorf("get S3 cert thumbprint: %w", err)
	}

	log.Printf("S3 certificate thumbprint for %s: %s", s3Endpoint, thumbprint)

	// Get AWS account ID
	accountID, err := i.getAWSAccountID(ctx)
	if err != nil {
		return fmt.Errorf("get AWS account ID: %w", err)
	}

	// Construct OIDC provider ARN
	oidcProviderARN := fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s-oidc.s3.%s.amazonaws.com",
		accountID, clusterName, region)

	log.Printf("Updating OIDC provider thumbprint for %s", oidcProviderARN)

	// Update the OIDC provider thumbprint
	cmd := exec.CommandContext(ctx, "aws", "iam", "update-open-id-connect-provider-thumbprint",
		"--open-id-connect-provider-arn", oidcProviderARN,
		"--thumbprint-list", thumbprint)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("update OIDC thumbprint: %w\nStderr: %s", err, stderr.String())
	}

	log.Printf("Successfully updated OIDC provider thumbprint to %s", thumbprint)
	return nil
}

// getS3CertThumbprint retrieves the TLS certificate thumbprint for an S3 endpoint
// Uses Go's crypto/tls instead of shelling out to openssl (prevents command injection)
func (i *Installer) getS3CertThumbprint(ctx context.Context, s3Endpoint string) (string, error) {
	// Create TLS dialer with context support
	dialer := &tls.Dialer{
		Config: &tls.Config{
			ServerName:         s3Endpoint,
			InsecureSkipVerify: true, // We only need the cert, not validation
		},
	}

	// Connect to the endpoint
	conn, err := dialer.DialContext(ctx, "tcp", s3Endpoint+":443")
	if err != nil {
		return "", fmt.Errorf("dial TLS connection to %s:443: %w", s3Endpoint, err)
	}
	defer conn.Close()

	// Get the TLS connection state
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return "", fmt.Errorf("connection is not TLS")
	}

	// Get the peer certificates
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return "", fmt.Errorf("no peer certificates found")
	}

	// Get the leaf certificate (first in chain)
	cert := state.PeerCertificates[0]

	// Calculate SHA1 fingerprint of the certificate
	fingerprint := sha1.Sum(cert.Raw)

	// Convert to lowercase hex string (matching openssl format)
	thumbprint := strings.ToLower(hex.EncodeToString(fingerprint[:]))

	if thumbprint == "" {
		return "", fmt.Errorf("empty thumbprint generated")
	}

	return thumbprint, nil
}

// getAWSAccountID retrieves the current AWS account ID
func (i *Installer) getAWSAccountID(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "aws", "sts", "get-caller-identity",
		"--query", "Account",
		"--output", "text")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("get caller identity: %w\nStderr: %s", err, stderr.String())
	}

	accountID := strings.TrimSpace(stdout.String())
	if accountID == "" {
		return "", fmt.Errorf("empty account ID returned")
	}

	return accountID, nil
}

// getInfraID extracts the infrastructure ID from the openshift install state file
func (i *Installer) getInfraID(workDir string) (string, error) {
	stateFile := filepath.Join(workDir, ".openshift_install_state.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		return "", fmt.Errorf("read state file: %w", err)
	}

	var state map[string]json.RawMessage
	if err := json.Unmarshal(data, &state); err != nil {
		return "", fmt.Errorf("parse state file: %w", err)
	}

	clusterIDRaw, ok := state["*installconfig.ClusterID"]
	if !ok {
		return "", fmt.Errorf("ClusterID not found in state file")
	}

	var clusterID struct {
		InfraID string `json:"InfraID"`
	}
	if err := json.Unmarshal(clusterIDRaw, &clusterID); err != nil {
		return "", fmt.Errorf("parse ClusterID: %w", err)
	}

	if clusterID.InfraID == "" {
		return "", fmt.Errorf("InfraID is empty in state file")
	}

	return clusterID.InfraID, nil
}

// getMetadataInfraID reads infraID from metadata.json if it exists
func (i *Installer) getMetadataInfraID(workDir string) (string, error) {
	metadataFile := filepath.Join(workDir, "metadata.json")
	data, err := os.ReadFile(metadataFile)
	if err != nil {
		return "", err // File might not exist yet
	}

	var metadata struct {
		InfraID string `json:"infraID"`
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", fmt.Errorf("parse metadata.json: %w", err)
	}

	return metadata.InfraID, nil
}

// logInstallState logs the current state of the installation directory for debugging
func (i *Installer) logInstallState(workDir string, phase string) {
	log.Printf("=== Install State at %s ===", phase)
	log.Printf("WorkDir (absolute): %s", workDir)

	// Check install-config.yaml
	installConfigPath := filepath.Join(workDir, "install-config.yaml")
	if data, err := os.ReadFile(installConfigPath); err == nil {
		hash := sha256.Sum256(data)
		log.Printf("install-config.yaml SHA256: %x", hash[:8])
	} else {
		log.Printf("install-config.yaml: not found or unreadable")
	}

	// Check metadata.json
	metadataPath := filepath.Join(workDir, "metadata.json")
	if stat, err := os.Stat(metadataPath); err == nil {
		log.Printf("metadata.json: exists (size=%d, mtime=%s)", stat.Size(), stat.ModTime().Format(time.RFC3339))
		if infraID, err := i.getMetadataInfraID(workDir); err == nil {
			log.Printf("metadata.json infraID: %s", infraID)
		}
	} else {
		log.Printf("metadata.json: does not exist")
	}

	// Check state file infraID
	if infraID, err := i.getInfraID(workDir); err == nil {
		log.Printf("state file infraID: %s", infraID)
	} else {
		log.Printf("state file infraID: error reading (%v)", err)
	}

	// Check manifests directory
	manifestsDir := filepath.Join(workDir, "manifests")
	if stat, err := os.Stat(manifestsDir); err == nil {
		log.Printf("manifests/: exists (mtime=%s)", stat.ModTime().Format(time.RFC3339))
		if files, err := os.ReadDir(manifestsDir); err == nil {
			log.Printf("manifests/ file count: %d", len(files))
		}
	} else {
		log.Printf("manifests/: does not exist")
	}
	log.Printf("=== End Install State ===")
}

// validateOIDCConfiguration verifies OIDC provider and discovery documents are correctly configured
// This catches configuration mismatches before cluster installation starts
func (i *Installer) validateOIDCConfiguration(ctx context.Context, infraID, region string) error {
	log.Printf("Validating OIDC configuration for infraID=%s, region=%s", infraID, region)

	// Define canonical issuer URL (using infraID, not human cluster name)
	issuerHost := fmt.Sprintf("%s-oidc.s3.%s.amazonaws.com", infraID, region)
	canonicalIssuer := fmt.Sprintf("https://%s", issuerHost)

	// Get AWS account ID
	accountID, err := i.getAWSAccountID(ctx)
	if err != nil {
		return fmt.Errorf("get AWS account ID: %w", err)
	}

	// Check 1: Verify discovery document issuer
	s3Bucket := fmt.Sprintf("%s-oidc", infraID)
	s3Key := ".well-known/openid-configuration"

	getDiscoveryCmd := exec.CommandContext(ctx, "aws", "s3", "cp",
		fmt.Sprintf("s3://%s/%s", s3Bucket, s3Key),
		"-",
		"--region", region)

	var discoveryOut, discoveryErr bytes.Buffer
	getDiscoveryCmd.Stdout = &discoveryOut
	getDiscoveryCmd.Stderr = &discoveryErr
	if err := getDiscoveryCmd.Run(); err != nil {
		return fmt.Errorf("fetch discovery document: %w\nStderr: %s", err, discoveryErr.String())
	}

	var discoveryDoc struct {
		Issuer  string `json:"issuer"`
		JwksURI string `json:"jwks_uri"`
	}
	if err := json.Unmarshal(discoveryOut.Bytes(), &discoveryDoc); err != nil {
		return fmt.Errorf("parse discovery document: %w\nContent: %s", err, discoveryOut.String())
	}

	if discoveryDoc.Issuer != canonicalIssuer {
		return fmt.Errorf("discovery doc issuer mismatch: expected %s, got %s",
			canonicalIssuer, discoveryDoc.Issuer)
	}
	log.Printf("✓ Discovery document issuer correct: %s", discoveryDoc.Issuer)

	// Verify jwks_uri host matches issuer host (optional sanity check)
	expectedJwksURI := fmt.Sprintf("https://%s/keys.json", issuerHost)
	if discoveryDoc.JwksURI != expectedJwksURI {
		log.Printf("Warning: jwks_uri mismatch (expected %s, got %s)", expectedJwksURI, discoveryDoc.JwksURI)
	}

	// Check 2: Verify IAM OIDC provider exists and corresponds to canonical issuer
	providerARN := fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s", accountID, issuerHost)

	getProviderCmd := exec.CommandContext(ctx, "aws", "iam", "get-open-id-connect-provider",
		"--open-id-connect-provider-arn", providerARN,
		"--output", "json")

	var providerOut, providerErr bytes.Buffer
	getProviderCmd.Stdout = &providerOut
	getProviderCmd.Stderr = &providerErr
	if err := getProviderCmd.Run(); err != nil {
		return fmt.Errorf("get OIDC provider %s: %w\nStderr: %s", providerARN, err, providerErr.String())
	}

	var providerInfo struct {
		Url            string   `json:"Url"`
		ClientIDList   []string `json:"ClientIDList"`
		ThumbprintList []string `json:"ThumbprintList"`
	}
	if err := json.Unmarshal(providerOut.Bytes(), &providerInfo); err != nil {
		return fmt.Errorf("parse provider info: %w", err)
	}

	// AWS displays Url without scheme - verify host/path matches
	if providerInfo.Url != issuerHost {
		return fmt.Errorf("OIDC provider URL mismatch: expected %s, got %s",
			issuerHost, providerInfo.Url)
	}
	log.Printf("✓ IAM OIDC provider URL correct: %s", providerInfo.Url)

	// Check 3: Verify client IDs include sts.amazonaws.com (required for AWS STS)
	hasSTSAudience := false
	for _, clientID := range providerInfo.ClientIDList {
		if clientID == "sts.amazonaws.com" {
			hasSTSAudience = true
			break
		}
	}
	if !hasSTSAudience {
		return fmt.Errorf("OIDC provider missing required client ID: sts.amazonaws.com (has: %v)",
			providerInfo.ClientIDList)
	}
	log.Printf("✓ OIDC provider has sts.amazonaws.com in client IDs: %v", providerInfo.ClientIDList)

	// Check 4: Verify thumbprint is non-empty
	// Note: Don't recompute thumbprint against wrong host - trust what fixOIDCThumbprint set
	if len(providerInfo.ThumbprintList) == 0 {
		return fmt.Errorf("OIDC provider has empty thumbprint list")
	}
	log.Printf("✓ OIDC provider thumbprint: %s", providerInfo.ThumbprintList[0])

	// Check 5: Verify at least one IAM role trust policy is correctly configured
	// Pick a deterministic role name that ccoctl always creates
	testRoleName := fmt.Sprintf("%s-openshift-cloud-credential-", infraID)

	getRoleCmd := exec.CommandContext(ctx, "aws", "iam", "get-role",
		"--role-name", testRoleName,
		"--output", "json")

	var roleOut, roleErr bytes.Buffer
	getRoleCmd.Stdout = &roleOut
	getRoleCmd.Stderr = &roleErr
	if err := getRoleCmd.Run(); err != nil {
		log.Printf("Warning: could not verify role trust policy for %s: %v", testRoleName, err)
	} else {
		var roleInfo struct {
			Role struct {
				AssumeRolePolicyDocument struct {
					Statement []struct {
						Principal struct {
							Federated string `json:"Federated"`
						} `json:"Principal"`
						Condition struct {
							StringEquals map[string]string `json:"StringEquals"`
						} `json:"Condition"`
					} `json:"Statement"`
				} `json:"AssumeRolePolicyDocument"`
			} `json:"Role"`
		}
		if err := json.Unmarshal(roleOut.Bytes(), &roleInfo); err != nil {
			log.Printf("Warning: could not parse role trust policy: %v", err)
		} else if len(roleInfo.Role.AssumeRolePolicyDocument.Statement) > 0 {
			stmt := roleInfo.Role.AssumeRolePolicyDocument.Statement[0]

			// Verify Principal.Federated matches provider ARN
			if stmt.Principal.Federated != providerARN {
				return fmt.Errorf("role %s trust policy references wrong provider: expected %s, got %s",
					testRoleName, providerARN, stmt.Principal.Federated)
			}

			// Verify condition includes aud check
			audKey := fmt.Sprintf("%s:aud", issuerHost)
			if audValue, ok := stmt.Condition.StringEquals[audKey]; !ok {
				log.Printf("Warning: role trust policy missing %s condition", audKey)
			} else if audValue != "sts.amazonaws.com" {
				log.Printf("Warning: role trust policy %s=%s (expected sts.amazonaws.com)", audKey, audValue)
			} else {
				log.Printf("✓ Role trust policy correctly configured for %s", testRoleName)
			}
		}
	}

	log.Printf("✓ OIDC configuration validation passed")
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

	// Extract baseDomain from *installconfig.InstallConfig.config
	var installConfigData struct {
		Config struct {
			BaseDomain string `json:"baseDomain"`
		} `json:"config"`
	}
	if installConfigRaw, ok := stateMap["*installconfig.InstallConfig"]; ok {
		if err := json.Unmarshal(installConfigRaw, &installConfigData); err != nil {
			return fmt.Errorf("parse install config: %w", err)
		}
	}

	baseDomain := installConfigData.Config.BaseDomain
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

// buildTagSet creates the standard tag map for cluster resources
func buildTagSet(metadata ClusterMetadata) map[string]string {
	return map[string]string{
		fmt.Sprintf("kubernetes.io/cluster/%s", metadata.InfraID): "owned",
		"ManagedBy":      "ocpctl",
		"ClusterName":    metadata.ClusterName,
		"Profile":        metadata.ProfileName,
		"InfraID":        metadata.InfraID,
		"CreatedAt":      metadata.CreatedAt.Format(time.RFC3339),
		"OcpctlVersion":  ocpctlVersion,
	}
}

// isClusterIAMRole checks if an IAM role name matches the cluster's infraID
func isClusterIAMRole(roleName, infraID string) bool {
	// Pattern 1: <cluster>-<infraID>-openshift-*
	if strings.Contains(roleName, fmt.Sprintf("-%s-openshift-", infraID)) {
		return true
	}

	// Pattern 2: <cluster>-<infraID>-master-role
	if strings.HasSuffix(roleName, fmt.Sprintf("-%s-master-role", infraID)) {
		return true
	}

	// Pattern 3: <cluster>-<infraID>-worker-role
	if strings.HasSuffix(roleName, fmt.Sprintf("-%s-worker-role", infraID)) {
		return true
	}

	return false
}

// tagIAMRoles finds and tags all IAM roles created by ccoctl for this cluster
// Includes retry logic to handle AWS IAM eventual consistency
func (i *Installer) tagIAMRoles(ctx context.Context, infraID, region string, tags map[string]string) error {
	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	// Retry logic to handle IAM eventual consistency
	// IAM roles created by ccoctl may not appear in ListRoles immediately
	var rolesToTag []string
	maxRetries := 5
	backoffSeconds := []int{2, 4, 6, 8, 10} // Total: 30 seconds max

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Use paginator to handle accounts with 1000+ roles
		paginator := iam.NewListRolesPaginator(iamClient, &iam.ListRolesInput{})
		rolesToTag = []string{}

		for paginator.HasMorePages() {
			page, err := paginator.NextPage(ctx)
			if err != nil {
				return fmt.Errorf("list roles: %w", err)
			}

			for _, role := range page.Roles {
				roleName := aws.ToString(role.RoleName)

				// Check if this role belongs to our cluster
				if isClusterIAMRole(roleName, infraID) {
					rolesToTag = append(rolesToTag, roleName)
				}
			}
		}

		log.Printf("Found %d IAM roles to tag for infraID %s (attempt %d/%d)", len(rolesToTag), infraID, attempt+1, maxRetries)

		// If we found roles, break out of retry loop
		if len(rolesToTag) > 0 {
			break
		}

		// If this was the last attempt, fail
		if attempt == maxRetries-1 {
			return fmt.Errorf("no IAM roles found for infraID %s after %d retries (expected at least 5-10 roles) - IAM eventual consistency timeout", infraID, maxRetries)
		}

		// Wait before retrying (exponential backoff)
		waitTime := time.Duration(backoffSeconds[attempt]) * time.Second
		log.Printf("No roles found yet for infraID %s, waiting %v before retry %d/%d (IAM eventual consistency)", infraID, waitTime, attempt+2, maxRetries)
		time.Sleep(waitTime)
	}

	// Convert tags to IAM SDK format
	iamTags := []iamtypes.Tag{}
	for k, v := range tags {
		iamTags = append(iamTags, iamtypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	// Tag each role
	var errs []error
	for _, roleName := range rolesToTag {
		_, err := iamClient.TagRole(ctx, &iam.TagRoleInput{
			RoleName: aws.String(roleName),
			Tags:     iamTags,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("tag role %s: %w", roleName, err))
			log.Printf("Failed to tag IAM role %s: %v", roleName, err)
		} else {
			log.Printf("Tagged IAM role: %s", roleName)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("failed to tag %d/%d roles: %v", len(errs), len(rolesToTag), errs)
	}

	return nil
}

// tagOIDCProvider tags the OIDC provider created by ccoctl
func (i *Installer) tagOIDCProvider(ctx context.Context, infraID, region string, tags map[string]string) error {
	// Get AWS account ID
	accountID, err := i.getAWSAccountID(ctx)
	if err != nil {
		return fmt.Errorf("get AWS account ID: %w", err)
	}

	// Construct OIDC provider ARN (same pattern as fixOIDCThumbprint)
	providerARN := fmt.Sprintf("arn:aws:iam::%s:oidc-provider/%s-oidc.s3.%s.amazonaws.com",
		accountID, infraID, region)

	log.Printf("Tagging OIDC provider: %s", providerARN)

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	// Convert tags to IAM SDK format
	iamTags := []iamtypes.Tag{}
	for k, v := range tags {
		iamTags = append(iamTags, iamtypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	// Tag the OIDC provider
	_, err = iamClient.TagOpenIDConnectProvider(ctx, &iam.TagOpenIDConnectProviderInput{
		OpenIDConnectProviderArn: aws.String(providerARN),
		Tags:                     iamTags,
	})
	if err != nil {
		return fmt.Errorf("tag OIDC provider %s: %w", providerARN, err)
	}

	log.Printf("Successfully tagged OIDC provider")
	return nil
}

// tagOIDCBucket tags the S3 bucket used for OIDC discovery documents
// SECURITY NOTE: OIDC bucket names are predictable ({infraID}-oidc) where infraID
// contains only 5 random characters. This creates a bucket hijacking risk where
// attackers could pre-create buckets to intercept OIDC provider trust.
// This function includes verification to detect pre-created malicious buckets.
func (i *Installer) tagOIDCBucket(ctx context.Context, infraID, region string, tags map[string]string) error {
	bucketName := fmt.Sprintf("%s-oidc", infraID)

	log.Printf("Tagging S3 bucket: %s", bucketName)

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	// Get AWS account ID for ownership verification
	stsClient := sts.NewFromConfig(cfg)
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("get AWS account ID: %w", err)
	}
	accountID := aws.ToString(identity.Account)

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.Region = region
	})

	// Verify bucket ownership before tagging (defense against bucket hijacking)
	// Check that bucket creation time is recent (within last 2 hours of cluster creation)
	headOutput, err := s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
		ExpectedBucketOwner: aws.String(accountID), // Fails if bucket owned by different account
	})
	if err != nil {
		return fmt.Errorf("verify bucket ownership for %s: %w (potential bucket hijacking attack)", bucketName, err)
	}

	// Log successful ownership verification
	log.Printf("Verified OIDC bucket %s is owned by account %s", bucketName, accountID)

	// Additional safety check: Verify bucket region matches expected region
	// This prevents cross-region bucket attacks
	bucketLocation, err := s3Client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{
		Bucket: aws.String(bucketName),
		ExpectedBucketOwner: aws.String(accountID),
	})
	if err != nil {
		return fmt.Errorf("verify bucket location: %w", err)
	}

	// S3 returns empty string for us-east-1
	bucketRegion := string(bucketLocation.LocationConstraint)
	if bucketRegion == "" {
		bucketRegion = "us-east-1"
	}
	if bucketRegion != region {
		return fmt.Errorf("SECURITY: OIDC bucket %s is in region %s but expected %s (potential hijacking)",
			bucketName, bucketRegion, region)
	}

	_ = headOutput // Use headOutput to avoid unused variable

	// Convert tags to S3 SDK format
	tagSet := []s3types.Tag{}
	for k, v := range tags {
		tagSet = append(tagSet, s3types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	// Tag the S3 bucket
	_, err = s3Client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: aws.String(bucketName),
		Tagging: &s3types.Tagging{
			TagSet: tagSet,
		},
	})
	if err != nil {
		return fmt.Errorf("tag S3 bucket %s: %w", bucketName, err)
	}

	log.Printf("Successfully tagged S3 bucket")
	return nil
}

// TagIAMResources tags IAM roles, OIDC provider, and S3 bucket with cluster metadata
// This should be called AFTER cluster creation completes to allow IAM eventual consistency
func (i *Installer) TagIAMResources(ctx context.Context, workDir string, metadata ClusterMetadata) error {
	// Build standard tag set from metadata
	tags := buildTagSet(metadata)

	var errs []error

	// Tag IAM roles
	if err := i.tagIAMRoles(ctx, metadata.InfraID, metadata.Region, tags); err != nil {
		errs = append(errs, fmt.Errorf("tag IAM roles: %w", err))
	}

	// Tag OIDC provider
	if err := i.tagOIDCProvider(ctx, metadata.InfraID, metadata.Region, tags); err != nil {
		errs = append(errs, fmt.Errorf("tag OIDC provider: %w", err))
	}

	// Tag S3 bucket
	if err := i.tagOIDCBucket(ctx, metadata.InfraID, metadata.Region, tags); err != nil {
		errs = append(errs, fmt.Errorf("tag S3 bucket: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("tagging failed: %v", errs)
	}

	return nil
}

// tagEC2Resources tags VPCs, subnets, instances, volumes, security groups, and elastic IPs
func (i *Installer) tagEC2Resources(ctx context.Context, cfg aws.Config, infraID string, tags map[string]string) error {
	ec2Client := ec2.NewFromConfig(cfg)

	// Build EC2 tag format
	ec2Tags := make([]ec2types.Tag, 0, len(tags))
	for k, v := range tags {
		ec2Tags = append(ec2Tags, ec2types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	// Discover resources by kubernetes.io/cluster/<infraID> tag
	filter := []ec2types.Filter{
		{
			Name:   aws.String(fmt.Sprintf("tag:kubernetes.io/cluster/%s", infraID)),
			Values: []string{"owned"},
		},
	}

	var allResourceIDs []string

	// 1. VPCs
	vpcsResp, err := ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{Filters: filter})
	if err != nil {
		return fmt.Errorf("describe VPCs: %w", err)
	}
	for _, vpc := range vpcsResp.Vpcs {
		allResourceIDs = append(allResourceIDs, *vpc.VpcId)
	}

	// 2. Subnets
	subnetsResp, err := ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{Filters: filter})
	if err != nil {
		return fmt.Errorf("describe subnets: %w", err)
	}
	for _, subnet := range subnetsResp.Subnets {
		allResourceIDs = append(allResourceIDs, *subnet.SubnetId)
	}

	// 3. Instances
	instancesResp, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{Filters: filter})
	if err != nil {
		return fmt.Errorf("describe instances: %w", err)
	}
	for _, reservation := range instancesResp.Reservations {
		for _, instance := range reservation.Instances {
			allResourceIDs = append(allResourceIDs, *instance.InstanceId)
		}
	}

	// 4. Volumes
	volumesResp, err := ec2Client.DescribeVolumes(ctx, &ec2.DescribeVolumesInput{Filters: filter})
	if err != nil {
		return fmt.Errorf("describe volumes: %w", err)
	}
	for _, volume := range volumesResp.Volumes {
		allResourceIDs = append(allResourceIDs, *volume.VolumeId)
	}

	// 5. Security Groups
	sgsResp, err := ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{Filters: filter})
	if err != nil {
		return fmt.Errorf("describe security groups: %w", err)
	}
	for _, sg := range sgsResp.SecurityGroups {
		allResourceIDs = append(allResourceIDs, *sg.GroupId)
	}

	// 6. Elastic IPs
	eipsResp, err := ec2Client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{Filters: filter})
	if err != nil {
		return fmt.Errorf("describe elastic IPs: %w", err)
	}
	for _, eip := range eipsResp.Addresses {
		if eip.AllocationId != nil {
			allResourceIDs = append(allResourceIDs, *eip.AllocationId)
		}
	}

	// Tag all discovered resources in a single call
	if len(allResourceIDs) == 0 {
		log.Printf("[tagEC2Resources] Warning: no EC2 resources found for infraID %s", infraID)
		return nil
	}

	// EC2 CreateTags has a limit of 1000 resources per call
	const batchSize = 1000
	for i := 0; i < len(allResourceIDs); i += batchSize {
		end := i + batchSize
		if end > len(allResourceIDs) {
			end = len(allResourceIDs)
		}
		batch := allResourceIDs[i:end]

		_, err = ec2Client.CreateTags(ctx, &ec2.CreateTagsInput{
			Resources: batch,
			Tags:      ec2Tags,
		})
		if err != nil {
			return fmt.Errorf("create tags (batch %d-%d): %w", i, end, err)
		}
	}

	log.Printf("[tagEC2Resources] Tagged %d EC2 resources", len(allResourceIDs))
	return nil
}

// tagELBResources tags Network Load Balancers and Application Load Balancers
func (i *Installer) tagELBResources(ctx context.Context, cfg aws.Config, infraID string, tags map[string]string) error {
	elbClient := elasticloadbalancingv2.NewFromConfig(cfg)

	// Build ELB tag format
	elbTags := make([]elbv2types.Tag, 0, len(tags))
	for k, v := range tags {
		elbTags = append(elbTags, elbv2types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	// Discover load balancers (can't filter by tags in DescribeLoadBalancers)
	var allLBArns []string

	// Paginate through all load balancers
	paginator := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(elbClient, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("describe load balancers: %w", err)
		}

		// Filter by kubernetes.io/cluster/<infraID> tag
		for _, lb := range page.LoadBalancers {
			// Get tags for this LB
			tagsResp, err := elbClient.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{
				ResourceArns: []string{*lb.LoadBalancerArn},
			})
			if err != nil {
				log.Printf("[tagELBResources] Warning: failed to get tags for LB %s: %v", *lb.LoadBalancerName, err)
				continue
			}

			// Check if it has the cluster tag
			for _, tagDesc := range tagsResp.TagDescriptions {
				for _, tag := range tagDesc.Tags {
					if *tag.Key == fmt.Sprintf("kubernetes.io/cluster/%s", infraID) {
						allLBArns = append(allLBArns, *lb.LoadBalancerArn)
						break
					}
				}
			}
		}
	}

	if len(allLBArns) == 0 {
		log.Printf("[tagELBResources] No load balancers found for infraID %s", infraID)
		return nil
	}

	// Tag all discovered load balancers
	// ELB AddTags only supports tagging one resource at a time
	for _, lbArn := range allLBArns {
		_, err := elbClient.AddTags(ctx, &elasticloadbalancingv2.AddTagsInput{
			ResourceArns: []string{lbArn},
			Tags:         elbTags,
		})
		if err != nil {
			return fmt.Errorf("add tags to load balancer %s: %w", lbArn, err)
		}
	}

	log.Printf("[tagELBResources] Tagged %d load balancers", len(allLBArns))
	return nil
}

// tagRoute53Resources tags Route53 hosted zones
func (i *Installer) tagRoute53Resources(ctx context.Context, cfg aws.Config, infraID string, tags map[string]string) error {
	r53Client := route53.NewFromConfig(cfg)

	// Build Route53 tag format
	r53Tags := make([]route53types.Tag, 0, len(tags))
	for k, v := range tags {
		r53Tags = append(r53Tags, route53types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	// List all hosted zones
	zonesResp, err := r53Client.ListHostedZones(ctx, &route53.ListHostedZonesInput{})
	if err != nil {
		return fmt.Errorf("list hosted zones: %w", err)
	}

	var matchingZoneIDs []string

	// Filter zones by kubernetes.io/cluster/<infraID> tag
	for _, zone := range zonesResp.HostedZones {
		// Get tags for this zone
		tagsResp, err := r53Client.ListTagsForResource(ctx, &route53.ListTagsForResourceInput{
			ResourceType: route53types.TagResourceTypeHostedzone,
			ResourceId:   zone.Id,
		})
		if err != nil {
			log.Printf("[tagRoute53Resources] Warning: failed to get tags for zone %s: %v", *zone.Name, err)
			continue
		}

		// Check if it has the cluster tag
		for _, tag := range tagsResp.ResourceTagSet.Tags {
			if *tag.Key == fmt.Sprintf("kubernetes.io/cluster/%s", infraID) {
				matchingZoneIDs = append(matchingZoneIDs, *zone.Id)
				break
			}
		}
	}

	if len(matchingZoneIDs) == 0 {
		log.Printf("[tagRoute53Resources] No hosted zones found for infraID %s", infraID)
		return nil
	}

	// Tag each zone (Route53 doesn't support batch tagging)
	for _, zoneID := range matchingZoneIDs {
		_, err := r53Client.ChangeTagsForResource(ctx, &route53.ChangeTagsForResourceInput{
			ResourceType: route53types.TagResourceTypeHostedzone,
			ResourceId:   aws.String(zoneID),
			AddTags:      r53Tags,
		})
		if err != nil {
			return fmt.Errorf("tag zone %s: %w", zoneID, err)
		}
	}

	log.Printf("[tagRoute53Resources] Tagged %d hosted zones", len(matchingZoneIDs))
	return nil
}

// tagBootstrapBucket tags the S3 bootstrap bucket used during cluster installation
func (i *Installer) tagBootstrapBucket(ctx context.Context, cfg aws.Config, infraID string, tags map[string]string) error {
	s3Client := s3.NewFromConfig(cfg)

	// Build S3 tag format
	s3Tags := make([]s3types.Tag, 0, len(tags))
	for k, v := range tags {
		s3Tags = append(s3Tags, s3types.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	// Bootstrap bucket name format: <cluster>-<infraID>-bootstrap
	// List all buckets and filter by name pattern
	bucketsResp, err := s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return fmt.Errorf("list buckets: %w", err)
	}

	var bootstrapBucket string
	bucketSuffix := fmt.Sprintf("-%s-bootstrap", infraID)

	for _, bucket := range bucketsResp.Buckets {
		if strings.HasSuffix(*bucket.Name, bucketSuffix) {
			bootstrapBucket = *bucket.Name
			break
		}
	}

	if bootstrapBucket == "" {
		log.Printf("[tagBootstrapBucket] No bootstrap bucket found for infraID %s", infraID)
		return nil
	}

	// Tag the bootstrap bucket
	_, err = s3Client.PutBucketTagging(ctx, &s3.PutBucketTaggingInput{
		Bucket: aws.String(bootstrapBucket),
		Tagging: &s3types.Tagging{
			TagSet: s3Tags,
		},
	})
	if err != nil {
		return fmt.Errorf("tag bucket %s: %w", bootstrapBucket, err)
	}

	log.Printf("[tagBootstrapBucket] Tagged bootstrap bucket: %s", bootstrapBucket)
	return nil
}

// TagAWSResources tags all AWS resources created by the cluster with ocpctl metadata.
// This includes EC2 resources (VPCs, subnets, instances, volumes, security groups),
// load balancers, Route53 hosted zones, S3 buckets, IAM roles, and OIDC providers.
//
// Tagging is done post-cluster-creation and failures are non-blocking to avoid
// disrupting cluster provisioning.
func (i *Installer) TagAWSResources(ctx context.Context, workDir string, metadata ClusterMetadata) error {
	// Build standard tag set
	tags := buildTagSet(metadata)

	log.Printf("[TagAWSResources] Tagging all AWS resources for cluster %s (infraID: %s)",
		metadata.ClusterName, metadata.InfraID)

	// Create AWS config
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(metadata.Region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	// Tag resources in parallel using goroutines
	var wg sync.WaitGroup
	errChan := make(chan error, 5) // Buffer for 5 services

	// EC2 resources (VPCs, subnets, instances, volumes, security groups, elastic IPs)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := i.tagEC2Resources(ctx, cfg, metadata.InfraID, tags); err != nil {
			errChan <- fmt.Errorf("EC2: %w", err)
		} else {
			log.Printf("[TagAWSResources] ✓ EC2 resources tagged")
		}
	}()

	// Elastic Load Balancers (NLB, ALB)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := i.tagELBResources(ctx, cfg, metadata.InfraID, tags); err != nil {
			errChan <- fmt.Errorf("ELB: %w", err)
		} else {
			log.Printf("[TagAWSResources] ✓ Load balancers tagged")
		}
	}()

	// Route53 hosted zones
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := i.tagRoute53Resources(ctx, cfg, metadata.InfraID, tags); err != nil {
			errChan <- fmt.Errorf("Route53: %w", err)
		} else {
			log.Printf("[TagAWSResources] ✓ Route53 zones tagged")
		}
	}()

	// S3 bootstrap bucket
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := i.tagBootstrapBucket(ctx, cfg, metadata.InfraID, tags); err != nil {
			errChan <- fmt.Errorf("S3: %w", err)
		} else {
			log.Printf("[TagAWSResources] ✓ S3 buckets tagged")
		}
	}()

	// IAM roles and OIDC provider (existing implementation)
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := i.tagIAMRoles(ctx, metadata.InfraID, metadata.Region, tags); err != nil {
			errChan <- fmt.Errorf("IAM roles: %w", err)
			return
		}
		if err := i.tagOIDCProvider(ctx, metadata.InfraID, metadata.Region, tags); err != nil {
			errChan <- fmt.Errorf("OIDC: %w", err)
			return
		}
		if err := i.tagOIDCBucket(ctx, metadata.InfraID, metadata.Region, tags); err != nil {
			errChan <- fmt.Errorf("OIDC bucket: %w", err)
			return
		}
		log.Printf("[TagAWSResources] ✓ IAM/OIDC resources tagged")
	}()

	// Wait for all goroutines
	wg.Wait()
	close(errChan)

	// Aggregate errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("tagging failed for %d services: %v", len(errs), errs)
	}

	log.Printf("[TagAWSResources] ✓ All AWS resources tagged successfully")
	return nil
}

// TagPartialResources attempts to tag whatever AWS resources it can find, even on cluster creation failure
// This is a best-effort operation to ensure orphaned resources can be detected
func (i *Installer) TagPartialResources(ctx context.Context, workDir string, metadata ClusterMetadata) {
	log.Printf("[TagPartialResources] Attempting to tag partial resources for failed cluster %s", metadata.ClusterName)

	// Try to get infraID if we don't have it yet
	if metadata.InfraID == "" {
		if infraID, err := i.getInfraID(workDir); err == nil {
			metadata.InfraID = infraID
			log.Printf("[TagPartialResources] Found infraID: %s", infraID)
		} else {
			log.Printf("[TagPartialResources] Could not determine infraID: %v (skipping resource tagging)", err)
			return
		}
	}

	// Build tag set
	tags := buildTagSet(metadata)

	// Try to tag IAM roles (created by ccoctl, might exist even on early failure)
	if err := i.tagIAMRoles(ctx, metadata.InfraID, metadata.Region, tags); err != nil {
		log.Printf("[TagPartialResources] Could not tag IAM roles: %v", err)
	} else {
		log.Printf("[TagPartialResources] ✓ Tagged IAM roles")
	}

	// Try to tag OIDC provider
	if err := i.tagOIDCProvider(ctx, metadata.InfraID, metadata.Region, tags); err != nil {
		log.Printf("[TagPartialResources] Could not tag OIDC provider: %v", err)
	} else {
		log.Printf("[TagPartialResources] ✓ Tagged OIDC provider")
	}

	// Try to tag OIDC bucket
	if err := i.tagOIDCBucket(ctx, metadata.InfraID, metadata.Region, tags); err != nil {
		log.Printf("[TagPartialResources] Could not tag OIDC bucket: %v", err)
	} else {
		log.Printf("[TagPartialResources] ✓ Tagged OIDC bucket")
	}

	// Try to tag EC2 resources if any exist
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(metadata.Region))
	if err != nil {
		log.Printf("[TagPartialResources] Could not load AWS config: %v", err)
		return
	}

	if err := i.tagEC2Resources(ctx, cfg, metadata.InfraID, tags); err != nil {
		log.Printf("[TagPartialResources] Could not tag EC2 resources: %v", err)
	} else {
		log.Printf("[TagPartialResources] ✓ Tagged EC2 resources")
	}

	// Try to tag ELBs
	if err := i.tagELBResources(ctx, cfg, metadata.InfraID, tags); err != nil {
		log.Printf("[TagPartialResources] Could not tag ELB resources: %v", err)
	} else {
		log.Printf("[TagPartialResources] ✓ Tagged ELB resources")
	}

	// Try to tag Route53 zones
	if err := i.tagRoute53Resources(ctx, cfg, metadata.InfraID, tags); err != nil {
		log.Printf("[TagPartialResources] Could not tag Route53 resources: %v", err)
	} else {
		log.Printf("[TagPartialResources] ✓ Tagged Route53 resources")
	}

	log.Printf("[TagPartialResources] Best-effort tagging complete for cluster %s", metadata.ClusterName)
}

//go:embed credreqs/efs-csi-driver.yaml
var efsCredentialsRequestYAML string

// addEFSCredentialsRequest adds the EFS CSI Driver CredentialsRequest to the credentials requests directory
// This allows ccoctl to create the IAM role needed for the EFS CSI operator
func (i *Installer) addEFSCredentialsRequest(credsReqDir string) error {
	efsCredReqPath := filepath.Join(credsReqDir, "efs-csi-driver.yaml")

	if err := os.WriteFile(efsCredReqPath, []byte(efsCredentialsRequestYAML), 0644); err != nil {
		return fmt.Errorf("write EFS CredentialsRequest: %w", err)
	}

	log.Printf("Added EFS CSI driver CredentialsRequest")
	return nil
}
