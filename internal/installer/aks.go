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

// AKSInstaller wraps the Azure CLI for AKS cluster operations
type AKSInstaller struct {
	binaryPath string
	timeout    time.Duration
}

// AKSClusterConfig represents an AKS cluster configuration
type AKSClusterConfig struct {
	Name              string
	SubscriptionID    string
	ResourceGroup     string
	Region            string
	KubernetesVersion string
	NetworkPlugin     string
	NetworkPolicy     string
	NodePools         []AKSNodePoolConfig
	Tags              map[string]string
}

// AKSNodePoolConfig represents an AKS node pool configuration
type AKSNodePoolConfig struct {
	Name            string
	VMSize          string
	Count           int
	MinCount        int
	MaxCount        int
	EnableAutoScale bool
	OSDiskSizeGB    int
}

// AKSClusterInfo represents cluster information from Azure
type AKSClusterInfo struct {
	Name              string            `json:"name"`
	ProvisioningState string            `json:"provisioningState"`
	FQDN              string            `json:"fqdn"`
	KubernetesVersion string            `json:"kubernetesVersion"`
	Tags              map[string]string `json:"tags"`
}

// NewAKSInstaller creates a new AKS installer instance
func NewAKSInstaller() *AKSInstaller {
	binaryPath := os.Getenv("AZ_BINARY")
	if binaryPath == "" {
		binaryPath = "az"
	}

	return &AKSInstaller{
		binaryPath: binaryPath,
		timeout:    60 * time.Minute, // AKS clusters take 10-15 minutes
	}
}

// CreateCluster creates an AKS cluster using az aks create
func (a *AKSInstaller) CreateCluster(ctx context.Context, config *AKSClusterConfig, logFile string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Get first node pool (system pool)
	if len(config.NodePools) == 0 {
		return "", fmt.Errorf("at least one node pool required")
	}
	systemPool := config.NodePools[0]

	// Build az aks create command
	args := []string{
		"aks", "create",
		"--resource-group", config.ResourceGroup,
		"--name", config.Name,
		"--kubernetes-version", config.KubernetesVersion,
		"--node-count", fmt.Sprintf("%d", systemPool.Count),
		"--node-vm-size", systemPool.VMSize,
		"--network-plugin", config.NetworkPlugin,
		"--network-policy", config.NetworkPolicy,
		"--generate-ssh-keys",
	}

	// Add autoscaling for system pool
	if systemPool.EnableAutoScale {
		args = append(args,
			"--enable-cluster-autoscaler",
			"--min-count", fmt.Sprintf("%d", systemPool.MinCount),
			"--max-count", fmt.Sprintf("%d", systemPool.MaxCount))
	}

	// Add OS disk size
	if systemPool.OSDiskSizeGB > 0 {
		args = append(args, "--node-osdisk-size", fmt.Sprintf("%d", systemPool.OSDiskSizeGB))
	}

	// Add tags
	if len(config.Tags) > 0 {
		tagPairs := []string{}
		for k, v := range config.Tags {
			tagPairs = append(tagPairs, fmt.Sprintf("%s=%s", k, v))
		}
		args = append(args, "--tags", strings.Join(tagPairs, " "))
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
		return string(logData), fmt.Errorf("az aks create failed: %w", err)
	}

	// Create additional node pools
	for _, pool := range config.NodePools[1:] {
		if err := a.CreateNodePool(ctx, config.ResourceGroup, config.Name, &pool, logFile); err != nil {
			return "", fmt.Errorf("create node pool %s: %w", pool.Name, err)
		}
	}

	logData, _ := os.ReadFile(logFile)
	return string(logData), nil
}

// CreateNodePool creates an additional node pool
func (a *AKSInstaller) CreateNodePool(ctx context.Context, resourceGroup, clusterName string, pool *AKSNodePoolConfig, logFile string) error {
	args := []string{
		"aks", "nodepool", "add",
		"--resource-group", resourceGroup,
		"--cluster-name", clusterName,
		"--name", pool.Name,
		"--node-count", fmt.Sprintf("%d", pool.Count),
		"--node-vm-size", pool.VMSize,
	}

	if pool.EnableAutoScale {
		args = append(args,
			"--enable-cluster-autoscaler",
			"--min-count", fmt.Sprintf("%d", pool.MinCount),
			"--max-count", fmt.Sprintf("%d", pool.MaxCount))
	}

	if pool.OSDiskSizeGB > 0 {
		args = append(args, "--node-osdisk-size", fmt.Sprintf("%d", pool.OSDiskSizeGB))
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	cmd.Stdout = f
	cmd.Stderr = f

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("az aks nodepool add failed: %w", err)
	}

	return nil
}

// DestroyCluster destroys an AKS cluster
func (a *AKSInstaller) DestroyCluster(ctx context.Context, resourceGroup, clusterName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	args := []string{
		"aks", "delete",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--yes",
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("az aks delete failed: %w", err)
	}

	return stdout.String(), nil
}

// GetClusterInfo retrieves AKS cluster details
func (a *AKSInstaller) GetClusterInfo(ctx context.Context, resourceGroup, clusterName string) (*AKSClusterInfo, error) {
	args := []string{
		"aks", "show",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--output", "json",
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("az aks show failed: %w", err)
	}

	var info AKSClusterInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("parse cluster info: %w", err)
	}

	return &info, nil
}

// GetKubeconfig retrieves the kubeconfig for an AKS cluster
func (a *AKSInstaller) GetKubeconfig(ctx context.Context, resourceGroup, clusterName, outputPath string) error {
	args := []string{
		"aks", "get-credentials",
		"--resource-group", resourceGroup,
		"--name", clusterName,
		"--file", outputPath,
		"--admin",
		"--overwrite-existing",
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

// ListNodePools lists all node pools for an AKS cluster
func (a *AKSInstaller) ListNodePools(ctx context.Context, resourceGroup, clusterName string) ([]string, error) {
	args := []string{
		"aks", "nodepool", "list",
		"--resource-group", resourceGroup,
		"--cluster-name", clusterName,
		"--query", "[].{name:name,count:count}",
		"--output", "json",
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("az aks nodepool list failed: %w", err)
	}

	var pools []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	if err := json.Unmarshal(stdout.Bytes(), &pools); err != nil {
		return nil, fmt.Errorf("parse node pools: %w", err)
	}

	var names []string
	for _, pool := range pools {
		names = append(names, pool.Name)
	}

	return names, nil
}

// ScaleNodePool scales an AKS node pool to a specific size
func (a *AKSInstaller) ScaleNodePool(ctx context.Context, resourceGroup, clusterName, poolName string, count int) error {
	args := []string{
		"aks", "nodepool", "scale",
		"--resource-group", resourceGroup,
		"--cluster-name", clusterName,
		"--name", poolName,
		"--node-count", fmt.Sprintf("%d", count),
	}

	cmd := exec.CommandContext(ctx, a.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("az aks nodepool scale failed: %w", err)
	}

	return nil
}

// Version returns the Azure CLI version
func (a *AKSInstaller) Version() (string, error) {
	cmd := exec.Command(a.binaryPath, "version", "--output", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("az version failed: %w", err)
	}

	return stdout.String(), nil
}
