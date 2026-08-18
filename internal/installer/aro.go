package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AROInstaller wraps the Azure CLI for ARO cluster operations
type AROInstaller struct {
	binaryPath string
	timeout    time.Duration
}

// AROClusterConfig represents an ARO cluster configuration
type AROClusterConfig struct {
	Name             string
	SubscriptionID   string
	ResourceGroup    string
	Region           string
	MasterVMSize     string
	WorkerVMSize     string
	WorkerCount      int
	OpenShiftVersion string
	PullSecret       string
	Tags             map[string]string
	// ClientID/ClientSecret are the cluster service principal passed to
	// az aro create (Red Hat recommended BYO-SP pattern). Providing an existing
	// SP prevents az from creating a new app registration in Entra ID, which
	// would require Microsoft Graph write permissions the worker does not have.
	ClientID     string
	ClientSecret string
}

// AROClusterInfo represents cluster information from Azure
type AROClusterInfo struct {
	Name              string `json:"name"`
	ProvisioningState string `json:"provisioningState"`
	// az aro show returns the URLs nested under consoleProfile/apiserverProfile.
	// Go's encoding/json does not treat dotted json tags as nested-path access,
	// so these must be modeled as nested structs and flattened after unmarshal.
	ConsoleProfile struct {
		URL string `json:"url"`
	} `json:"consoleProfile"`
	APIServerProfile struct {
		URL string `json:"url"`
	} `json:"apiserverProfile"`
	Tags map[string]string `json:"tags"`

	// ConsoleURL/APIURL are flattened from the nested profiles above in
	// GetClusterInfo so callers keep a stable, simple accessor.
	ConsoleURL string `json:"-"`
	APIURL     string `json:"-"`
}

// NewAROInstaller creates a new ARO installer instance
func NewAROInstaller() *AROInstaller {
	binaryPath := os.Getenv("AZ_BINARY")
	if binaryPath == "" {
		binaryPath = "az"
	}

	return &AROInstaller{
		binaryPath: binaryPath,
		timeout:    90 * time.Minute, // ARO clusters take 30-40 minutes
	}
}

// VerifyAuthentication checks if Azure CLI is authenticated
func (a *AROInstaller) VerifyAuthentication(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, a.binaryPath, "account", "show", "--output", "json")

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Azure CLI not authenticated: %w\nRun: az login", err)
	}

	return nil
}

// CreateVNet creates an Azure Virtual Network for ARO
func (a *AROInstaller) CreateVNet(ctx context.Context, resourceGroup, vnetName, region string) error {
	args := []string{
		"network", "vnet", "create",
		"--resource-group", resourceGroup,
		"--name", vnetName,
		"--location", region,
		"--address-prefixes", "10.0.0.0/16",
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create vnet: %w: %s", err, stderr.String())
	}

	return nil
}

// CreateSubnet creates a subnet within a VNet
func (a *AROInstaller) CreateSubnet(ctx context.Context, resourceGroup, vnetName, subnetName, addressPrefix string, serviceEndpoints bool) error {
	args := []string{
		"network", "vnet", "subnet", "create",
		"--resource-group", resourceGroup,
		"--vnet-name", vnetName,
		"--name", subnetName,
		"--address-prefixes", addressPrefix,
	}

	// ARO master and worker subnets need service endpoints
	if serviceEndpoints {
		args = append(args, "--service-endpoints", "Microsoft.ContainerRegistry")
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create subnet %s: %w: %s", subnetName, err, stderr.String())
	}

	return nil
}

// ResolveVersion turns a MAJOR.MINOR OpenShift version (e.g. "4.20") into the
// highest available patch release (e.g. "4.20.15") for the given Azure region.
// `az aro create --version` rejects a bare minor with "--version is invalid",
// but ocpctl profiles declare minor versions by convention. This bridges that
// gap, mirroring the ROSA ResolveVersion path.
//
// Already fully-qualified versions (X.Y.Z) and empty versions are returned
// unchanged (empty lets az aro create pick its own default).
func (a *AROInstaller) ResolveVersion(ctx context.Context, region, version string) (string, error) {
	if version == "" || fullPatchVersionRe.MatchString(version) {
		return version, nil
	}
	if !minorVersionRe.MatchString(version) {
		return "", fmt.Errorf("unrecognized OpenShift version %q: expected MAJOR.MINOR or MAJOR.MINOR.PATCH", version)
	}

	listCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(listCtx, a.binaryPath, "aro", "get-versions", "--location", region, "--output", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("az aro get-versions: %w: %s", err, stderr.String())
	}

	// az aro get-versions returns a JSON array of patch version strings.
	var versions []string
	if err := json.Unmarshal(stdout.Bytes(), &versions); err != nil {
		return "", fmt.Errorf("parse aro versions: %w", err)
	}

	// Pick the highest patch matching the requested minor.
	prefix := version + "."
	best := ""
	bestPatch := -1
	for _, v := range versions {
		if !strings.HasPrefix(v, prefix) {
			continue
		}
		patch, ok := patchNumber(v)
		if !ok {
			continue
		}
		if patch > bestPatch {
			bestPatch = patch
			best = v
		}
	}

	if best == "" {
		return "", fmt.Errorf("no ARO-supported patch version found for OpenShift %s in region %s", version, region)
	}
	return best, nil
}

// CreateCluster creates an ARO cluster using az aro create
func (a *AROInstaller) CreateCluster(ctx context.Context, config *AROClusterConfig, logFile string, vnetName, masterSubnetName, workerSubnetName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Build az aro create command
	args := []string{
		"aro", "create",
		"--resource-group", config.ResourceGroup,
		"--name", config.Name,
		"--vnet", vnetName,
		"--master-subnet", masterSubnetName,
		"--worker-subnet", workerSubnetName,
		"--master-vm-size", config.MasterVMSize,
		"--worker-vm-size", config.WorkerVMSize,
		"--worker-count", fmt.Sprintf("%d", config.WorkerCount),
	}

	// Add version if specified
	if config.OpenShiftVersion != "" {
		args = append(args, "--version", config.OpenShiftVersion)
	}

	// BYO service principal (Red Hat recommended pattern). Passing an existing SP
	// stops az aro create from creating a new app registration in Entra ID, which
	// requires Microsoft Graph write permissions the worker SP lacks.
	if config.ClientID != "" && config.ClientSecret != "" {
		// Pass the client secret via Azure CLI's @<file> convention instead of as a
		// literal argument, so the secret never appears in the process argument list
		// (readable by any local user via `ps`) (#81). os.CreateTemp makes the file
		// 0600; it is removed when this function returns.
		secretFile, err := os.CreateTemp("", "aro-client-secret-*")
		if err != nil {
			return "", fmt.Errorf("create client-secret temp file: %w", err)
		}
		defer os.Remove(secretFile.Name())
		if _, err := secretFile.WriteString(config.ClientSecret); err != nil {
			secretFile.Close()
			return "", fmt.Errorf("write client-secret temp file: %w", err)
		}
		if err := secretFile.Close(); err != nil {
			return "", fmt.Errorf("close client-secret temp file: %w", err)
		}
		args = append(args, "--client-id", config.ClientID, "--client-secret", "@"+secretFile.Name())
	}

	// Add pull secret
	if config.PullSecret != "" {
		args = append(args, "--pull-secret", config.PullSecret)
	}

	// Add tags. Each key=value pair must be a separate argument to --tags; joining
	// them into a single space-delimited string makes az treat the whole string as
	// one tag value, which trips Azure's 256-char tag-value limit.
	if len(config.Tags) > 0 {
		args = append(args, "--tags")
		for k, v := range config.Tags {
			args = append(args, fmt.Sprintf("%s=%s", k, v))
		}
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	// Open log file for appending
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return "", fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	cmd.Stdout = f
	cmd.Stderr = f

	if err := cmd.Run(); err != nil {
		logData, _ := os.ReadFile(logFile)
		return string(logData), fmt.Errorf("az aro create failed: %w", err)
	}

	logData, _ := os.ReadFile(logFile)
	return string(logData), nil
}

// DestroyCluster destroys an ARO cluster using az aro delete
func (a *AROInstaller) DestroyCluster(ctx context.Context, resourceGroup, clusterName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	args := []string{
		"aro", "delete",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--yes", // Skip confirmation
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("az aro delete failed: %w", err)
	}

	return stdout.String(), nil
}

// GetClusterInfo retrieves ARO cluster details
func (a *AROInstaller) GetClusterInfo(ctx context.Context, resourceGroup, clusterName string) (*AROClusterInfo, error) {
	args := []string{
		"aro", "show",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--output", "json",
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("az aro show failed: %w", err)
	}

	var info AROClusterInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("parse cluster info: %w", err)
	}

	// Flatten the nested profile URLs into the simple accessors.
	info.ConsoleURL = info.ConsoleProfile.URL
	info.APIURL = info.APIServerProfile.URL

	return &info, nil
}

// GetKubeconfig retrieves the kubeconfig for an ARO cluster
func (a *AROInstaller) GetKubeconfig(ctx context.Context, resourceGroup, clusterName, outputPath string) error {
	args := []string{
		"aro", "get-admin-kubeconfig",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--file", outputPath,
	}

	// Create auth directory if it doesn't exist
	authDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(authDir, 0755); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}

	return nil
}

// CreateResourceGroup creates an Azure resource group
func (a *AROInstaller) CreateResourceGroup(ctx context.Context, name, region string, tags map[string]string) error {
	args := []string{
		"group", "create",
		"--name", name,
		"--location", region,
	}

	// Add tags. Each key=value pair must be a separate argument to --tags; joining
	// them into a single space-delimited string makes az treat the whole string as
	// one tag value, which trips Azure's 256-char tag-value limit.
	if len(tags) > 0 {
		args = append(args, "--tags")
		for k, v := range tags {
			args = append(args, fmt.Sprintf("%s=%s", k, v))
		}
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("create resource group: %w: %s", err, stderr.String())
	}

	return nil
}

// DeleteResourceGroup deletes an Azure resource group and all contained resources
func (a *AROInstaller) DeleteResourceGroup(ctx context.Context, name string) error {
	args := []string{
		"group", "delete",
		"--name", name,
		"--yes", // Skip confirmation
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("delete resource group: %w: %s", err, stderr.String())
	}

	return nil
}

// Version returns the Azure CLI version
func (a *AROInstaller) Version() (string, error) {
	cmd := exec.Command(a.binaryPath, "version", "--output", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("az version failed: %w", err)
	}

	return stdout.String(), nil
}

// SaveMetadata saves cluster metadata to a JSON file
func (a *AROInstaller) SaveMetadata(workDir string, metadata map[string]string) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(workDir, "metadata.json"), data, 0644)
}

// LoadMetadata loads cluster metadata from a JSON file
func (a *AROInstaller) LoadMetadata(workDir string) (map[string]string, error) {
	data, err := os.ReadFile(filepath.Join(workDir, "metadata.json"))
	if err != nil {
		return nil, err
	}

	var metadata map[string]string
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, err
	}

	return metadata, nil
}
