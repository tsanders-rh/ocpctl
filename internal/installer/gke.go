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

// GKEInstaller wraps the gcloud CLI for GKE cluster operations
type GKEInstaller struct {
	binaryPath string
	timeout    time.Duration
}

// GKEClusterConfig represents a GKE cluster configuration
// This mirrors the structure that can be created via gcloud commands
type GKEClusterConfig struct {
	Name                   string                `json:"name"`
	Project                string                `json:"project"`
	Zone                   string                `json:"zone,omitempty"`
	Region                 string                `json:"region,omitempty"`
	ClusterVersion         string                `json:"cluster_version"`
	ReleaseChannel         string                `json:"release_channel,omitempty"` // rapid, regular, stable
	Network                string                `json:"network,omitempty"`
	Subnetwork             string                `json:"subnetwork,omitempty"`
	EnableWorkloadIdentity bool                  `json:"enable_workload_identity,omitempty"`
	EnableAutoscaling      bool                  `json:"enable_autoscaling,omitempty"`
	MinNodes               int                   `json:"min_nodes,omitempty"`
	MaxNodes               int                   `json:"max_nodes,omitempty"`
	NumNodes               int                   `json:"num_nodes,omitempty"`
	MachineType            string                `json:"machine_type,omitempty"`
	DiskType               string                `json:"disk_type,omitempty"`
	DiskSize               int                   `json:"disk_size_gb,omitempty"`
	Labels                 map[string]string     `json:"labels,omitempty"`
	Tags                   []string              `json:"tags,omitempty"`
	NodePools              []GKENodePoolConfig   `json:"node_pools,omitempty"`
	EnableClusterLogging   []string              `json:"enable_logging,omitempty"`   // SYSTEM_COMPONENTS, WORKLOADS
	EnableClusterMonitoring []string             `json:"enable_monitoring,omitempty"` // SYSTEM_COMPONENTS, WORKLOADS
	PublicAccess           bool                  `json:"public_access,omitempty"`
	PrivateAccess          bool                  `json:"private_access,omitempty"`
}

// GKENodePoolConfig represents a GKE node pool configuration
type GKENodePoolConfig struct {
	Name              string            `json:"name"`
	MachineType       string            `json:"machine_type"`
	DiskType          string            `json:"disk_type,omitempty"`
	DiskSize          int               `json:"disk_size_gb,omitempty"`
	NumNodes          int               `json:"num_nodes,omitempty"`
	MinNodes          int               `json:"min_nodes,omitempty"`
	MaxNodes          int               `json:"max_nodes,omitempty"`
	EnableAutoscaling bool              `json:"enable_autoscaling,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
}

// GKEClusterInfo represents cluster information retrieved from GCP
type GKEClusterInfo struct {
	Name              string    `json:"name"`
	Status            string    `json:"status"`
	Endpoint          string    `json:"endpoint"`
	MasterVersion     string    `json:"currentMasterVersion"`
	NodeVersion       string    `json:"currentNodeVersion"`
	Location          string    `json:"location"`
	CreateTime        string    `json:"createTime"`
	Labels            map[string]string `json:"resourceLabels,omitempty"`
}

// NewGKEInstaller creates a new GKE installer instance
func NewGKEInstaller() *GKEInstaller {
	binaryPath := os.Getenv("GCLOUD_BINARY")
	if binaryPath == "" {
		binaryPath = "gcloud" // Use PATH to find gcloud binary
	}

	return &GKEInstaller{
		binaryPath: binaryPath,
		timeout:    60 * time.Minute, // GKE clusters typically take 10-15 minutes
	}
}

// CreateNetwork creates a VPC network if it doesn't exist
func (g *GKEInstaller) CreateNetwork(ctx context.Context, networkName, project string) error {
	// Check if network already exists
	checkCmd := exec.CommandContext(ctx, g.binaryPath,
		"compute", "networks", "describe", networkName,
		"--project", project,
		"--format", "json",
	)

	if err := checkCmd.Run(); err == nil {
		// Network already exists
		return nil
	}

	// Create auto-mode VPC network
	createCmd := exec.CommandContext(ctx, g.binaryPath,
		"compute", "networks", "create", networkName,
		"--project", project,
		"--subnet-mode", "auto",
		"--bgp-routing-mode", "regional",
	)

	var stderr bytes.Buffer
	createCmd.Stderr = &stderr

	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("create network failed: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// CreateCluster creates a GKE cluster using gcloud
func (g *GKEInstaller) CreateCluster(ctx context.Context, config *GKEClusterConfig, logFile string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	// Create VPC network if not specified
	if config.Network == "" {
		networkName := config.Name + "-vpc"
		if err := g.CreateNetwork(ctx, networkName, config.Project); err != nil {
			return "", fmt.Errorf("create VPC network: %w", err)
		}
		config.Network = networkName
	}

	// Build gcloud command arguments
	args := []string{
		"container", "clusters", "create", config.Name,
		"--project", config.Project,
	}

	// Add region or zone
	if config.Region != "" {
		args = append(args, "--region", config.Region)
	} else if config.Zone != "" {
		args = append(args, "--zone", config.Zone)
	} else {
		return "", fmt.Errorf("either region or zone must be specified")
	}

	// Add cluster version
	if config.ClusterVersion != "" {
		args = append(args, "--cluster-version", config.ClusterVersion)
	}

	// Add release channel
	if config.ReleaseChannel != "" {
		args = append(args, "--release-channel", config.ReleaseChannel)
	}

	// Add network configuration
	args = append(args, "--enable-ip-alias")
	args = append(args, "--network", config.Network)

	// Auto-create subnetwork within the VPC
	if config.Subnetwork == "" {
		args = append(args, "--create-subnetwork", "name="+config.Name+"-subnet")
	} else {
		args = append(args, "--subnetwork", config.Subnetwork)
	}

	// Add Workload Identity
	if config.EnableWorkloadIdentity {
		args = append(args, "--workload-pool="+config.Project+".svc.id.goog")
	}

	// Add node configuration (for default node pool)
	if config.MachineType != "" {
		args = append(args, "--machine-type", config.MachineType)
	}
	if config.DiskType != "" {
		args = append(args, "--disk-type", config.DiskType)
	}
	if config.DiskSize > 0 {
		args = append(args, "--disk-size", fmt.Sprintf("%d", config.DiskSize))
	}

	// Add autoscaling
	if config.EnableAutoscaling {
		args = append(args, "--enable-autoscaling")
		if config.MinNodes > 0 {
			args = append(args, "--min-nodes", fmt.Sprintf("%d", config.MinNodes))
		}
		if config.MaxNodes > 0 {
			args = append(args, "--max-nodes", fmt.Sprintf("%d", config.MaxNodes))
		}
	} else if config.NumNodes > 0 {
		args = append(args, "--num-nodes", fmt.Sprintf("%d", config.NumNodes))
	}

	// Add labels
	if len(config.Labels) > 0 {
		labelPairs := []string{}
		for k, v := range config.Labels {
			labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", k, v))
		}
		args = append(args, "--labels", strings.Join(labelPairs, ","))
	}

	// Add tags
	if len(config.Tags) > 0 {
		args = append(args, "--tags", strings.Join(config.Tags, ","))
	}

	// Add logging (use modern --logging flag, default SYSTEM is enabled automatically)
	// Skip deprecated --enable-cloud-logging flag
	if len(config.EnableClusterLogging) > 0 {
		// Map old values to new: SYSTEM_COMPONENTS -> SYSTEM, WORKLOADS -> WORKLOAD
		modernValues := []string{}
		for _, v := range config.EnableClusterLogging {
			if v == "SYSTEM_COMPONENTS" {
				modernValues = append(modernValues, "SYSTEM")
			} else if v == "WORKLOADS" {
				modernValues = append(modernValues, "WORKLOAD")
			} else {
				modernValues = append(modernValues, v)
			}
		}
		if len(modernValues) > 0 {
			args = append(args, "--logging="+strings.Join(modernValues, ","))
		}
	}

	// Add monitoring (use modern --monitoring flag, default SYSTEM is enabled automatically)
	// Skip deprecated --enable-cloud-monitoring flag
	if len(config.EnableClusterMonitoring) > 0 {
		// Map old values to new: SYSTEM_COMPONENTS -> SYSTEM, WORKLOADS -> WORKLOAD
		modernValues := []string{}
		for _, v := range config.EnableClusterMonitoring {
			if v == "SYSTEM_COMPONENTS" {
				modernValues = append(modernValues, "SYSTEM")
			} else if v == "WORKLOADS" {
				modernValues = append(modernValues, "WORKLOAD")
			} else {
				modernValues = append(modernValues, v)
			}
		}
		if len(modernValues) > 0 {
			args = append(args, "--monitoring="+strings.Join(modernValues, ","))
		}
	}

	// Add access configuration
	if config.PrivateAccess {
		args = append(args, "--enable-master-authorized-networks")
		args = append(args, "--enable-ip-alias")
		args = append(args, "--enable-private-nodes")
	}

	cmd := exec.CommandContext(ctx, g.binaryPath, args...)

	// Open log file for appending (not truncating)
	// Using O_APPEND instead of O_TRUNC to preserve logs from log streamer
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return "", fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	// Write both stdout and stderr to log file
	cmd.Stdout = f
	cmd.Stderr = f

	if err := cmd.Run(); err != nil {
		// Read log file for error context
		logData, _ := os.ReadFile(logFile)
		return string(logData), fmt.Errorf("gcloud create cluster failed: %w", err)
	}

	// Create additional node pools if specified
	if len(config.NodePools) > 0 {
		for _, pool := range config.NodePools {
			if err := g.CreateNodePool(ctx, config.Name, config.Project, config.Region, config.Zone, &pool, logFile); err != nil {
				return "", fmt.Errorf("create node pool %s: %w", pool.Name, err)
			}
		}
	}

	// Read log file for output
	logData, _ := os.ReadFile(logFile)
	return string(logData), nil
}

// CreateNodePool creates an additional node pool in an existing GKE cluster
func (g *GKEInstaller) CreateNodePool(ctx context.Context, clusterName, project, region, zone string, pool *GKENodePoolConfig, logFile string) error {
	args := []string{
		"container", "node-pools", "create", pool.Name,
		"--cluster", clusterName,
		"--project", project,
	}

	// Add region or zone
	if region != "" {
		args = append(args, "--region", region)
	} else if zone != "" {
		args = append(args, "--zone", zone)
	}

	// Add node configuration
	if pool.MachineType != "" {
		args = append(args, "--machine-type", pool.MachineType)
	}
	if pool.DiskType != "" {
		args = append(args, "--disk-type", pool.DiskType)
	}
	if pool.DiskSize > 0 {
		args = append(args, "--disk-size", fmt.Sprintf("%d", pool.DiskSize))
	}

	// Add autoscaling
	if pool.EnableAutoscaling {
		args = append(args, "--enable-autoscaling")
		if pool.MinNodes > 0 {
			args = append(args, "--min-nodes", fmt.Sprintf("%d", pool.MinNodes))
		}
		if pool.MaxNodes > 0 {
			args = append(args, "--max-nodes", fmt.Sprintf("%d", pool.MaxNodes))
		}
	} else if pool.NumNodes > 0 {
		args = append(args, "--num-nodes", fmt.Sprintf("%d", pool.NumNodes))
	}

	// Add labels
	if len(pool.Labels) > 0 {
		labelPairs := []string{}
		for k, v := range pool.Labels {
			labelPairs = append(labelPairs, fmt.Sprintf("%s=%s", k, v))
		}
		args = append(args, "--node-labels", strings.Join(labelPairs, ","))
	}

	// Add tags
	if len(pool.Tags) > 0 {
		args = append(args, "--tags", strings.Join(pool.Tags, ","))
	}

	cmd := exec.CommandContext(ctx, g.binaryPath, args...)

	// Append to log file
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	cmd.Stdout = f
	cmd.Stderr = f

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gcloud create node pool failed: %w", err)
	}

	return nil
}

// DestroyCluster destroys a GKE cluster using gcloud
func (g *GKEInstaller) DestroyCluster(ctx context.Context, clusterName, project, region, zone string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	args := []string{
		"container", "clusters", "delete", clusterName,
		"--project", project,
		"--quiet", // Skip confirmation prompt
	}

	// Add region or zone
	if region != "" {
		args = append(args, "--region", region)
	} else if zone != "" {
		args = append(args, "--zone", zone)
	} else {
		return "", fmt.Errorf("either region or zone must be specified")
	}

	cmd := exec.CommandContext(ctx, g.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("gcloud delete cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// GetClusterInfo retrieves information about a GKE cluster
func (g *GKEInstaller) GetClusterInfo(ctx context.Context, clusterName, project, region, zone string) (*GKEClusterInfo, error) {
	args := []string{
		"container", "clusters", "describe", clusterName,
		"--project", project,
		"--format", "json",
	}

	// Add region or zone
	if region != "" {
		args = append(args, "--region", region)
	} else if zone != "" {
		args = append(args, "--zone", zone)
	} else {
		return nil, fmt.Errorf("either region or zone must be specified")
	}

	cmd := exec.CommandContext(ctx, g.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gcloud describe cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	var info GKEClusterInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("parse cluster info: %w", err)
	}

	return &info, nil
}

// GetKubeconfig retrieves the kubeconfig for a GKE cluster
func (g *GKEInstaller) GetKubeconfig(ctx context.Context, clusterName, project, region, zone, outputPath string) error {
	args := []string{
		"container", "clusters", "get-credentials", clusterName,
		"--project", project,
	}

	// Add region or zone
	if region != "" {
		args = append(args, "--region", region)
	} else if zone != "" {
		args = append(args, "--zone", zone)
	} else {
		return fmt.Errorf("either region or zone must be specified")
	}

	// Create auth directory if it doesn't exist
	authDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(authDir, 0755); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	// Set KUBECONFIG environment variable to the desired output path
	cmd := exec.CommandContext(ctx, g.binaryPath, args...)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+outputPath)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("get kubeconfig: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// ListNodePools lists all node pools for a cluster
func (g *GKEInstaller) ListNodePools(ctx context.Context, clusterName, project, region, zone string) ([]string, error) {
	args := []string{
		"container", "node-pools", "list",
		"--cluster", clusterName,
		"--project", project,
		"--format", "json",
	}

	// Add region or zone
	if region != "" {
		args = append(args, "--region", region)
	} else if zone != "" {
		args = append(args, "--zone", zone)
	}

	cmd := exec.CommandContext(ctx, g.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gcloud list node pools failed: %w\nStderr: %s", err, stderr.String())
	}

	// Parse node pool names from JSON output
	var pools []map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &pools); err != nil {
		return nil, fmt.Errorf("parse node pools: %w", err)
	}

	var names []string
	for _, pool := range pools {
		if name, ok := pool["name"].(string); ok {
			names = append(names, name)
		}
	}

	return names, nil
}

// DeleteNodePool deletes a specific node pool from a cluster
func (g *GKEInstaller) DeleteNodePool(ctx context.Context, clusterName, poolName, project, region, zone string) (string, error) {
	args := []string{
		"container", "node-pools", "delete", poolName,
		"--cluster", clusterName,
		"--project", project,
		"--quiet",
	}

	// Add region or zone
	if region != "" {
		args = append(args, "--region", region)
	} else if zone != "" {
		args = append(args, "--zone", zone)
	}

	cmd := exec.CommandContext(ctx, g.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("gcloud delete node pool failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// ScaleNodePool scales a node pool to a specific size (used for hibernate/resume)
func (g *GKEInstaller) ScaleNodePool(ctx context.Context, clusterName, poolName, project, region, zone string, numNodes int) error {
	args := []string{
		"container", "clusters", "resize", clusterName,
		"--node-pool", poolName,
		"--project", project,
		"--num-nodes", fmt.Sprintf("%d", numNodes),
		"--quiet",
	}

	// Add region or zone
	if region != "" {
		args = append(args, "--region", region)
	} else if zone != "" {
		args = append(args, "--zone", zone)
	}

	cmd := exec.CommandContext(ctx, g.binaryPath, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gcloud resize cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// Version returns the gcloud version
func (g *GKEInstaller) Version() (string, error) {
	cmd := exec.Command(g.binaryPath, "version", "--format", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gcloud version failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// VerifyAuthentication checks if gcloud is properly authenticated
func (g *GKEInstaller) VerifyAuthentication(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, g.binaryPath, "auth", "list", "--format", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gcloud auth list failed: %w\nStderr: %s", err, stderr.String())
	}

	// Check if we have at least one authenticated account
	var accounts []map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &accounts); err != nil {
		return fmt.Errorf("parse auth accounts: %w", err)
	}

	if len(accounts) == 0 {
		return fmt.Errorf("no authenticated accounts found. Run: gcloud auth login")
	}

	return nil
}
