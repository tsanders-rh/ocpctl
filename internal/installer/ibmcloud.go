package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// IKSInstaller wraps the ibmcloud CLI for IKS cluster operations
type IKSInstaller struct {
	binaryPath string
	timeout    time.Duration
}

// IKSClusterCreateOptions represents options for creating an IKS cluster
type IKSClusterCreateOptions struct {
	Name             string
	Zone             string
	MachineType      string
	Workers          int
	KubeVersion      string
	PublicVLAN       string
	PrivateVLAN      string
	PublicServiceEndpoint  bool
	PrivateServiceEndpoint bool
}

// IKSClusterInfo represents cluster information from ibmcloud
type IKSClusterInfo struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	State             string `json:"state"`
	CreatedDate       string `json:"createdDate"`
	MasterKubeVersion string `json:"masterKubeVersion"`
	MasterURL         string `json:"masterURL"`
	Ingress           struct {
		Hostname string `json:"hostname"`
	} `json:"ingress"`
}

// IKSWorkerPoolInfo represents worker pool information
type IKSWorkerPoolInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SizePerZone int    `json:"sizePerZone"`
	MachineType string `json:"machineType"`
	State       string `json:"state"`
	Zones       []struct {
		ID          string `json:"id"`
		WorkerCount int    `json:"workerCount"`
	} `json:"zones"`
}

// NewIKSInstaller creates a new IKS installer instance
func NewIKSInstaller() *IKSInstaller {
	binaryPath := os.Getenv("IBMCLOUD_BINARY")
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/ibmcloud"
	}

	return &IKSInstaller{
		binaryPath: binaryPath,
		timeout:    60 * time.Minute, // IKS clusters typically take 20-30 minutes
	}
}

// Login authenticates with IBM Cloud
// If resourceGroup is empty, login without specifying a resource group
func (i *IKSInstaller) Login(ctx context.Context, apiKey, region, resourceGroup string) error {
	var cmd *exec.Cmd

	if resourceGroup == "" {
		fmt.Printf("Logging into IBM Cloud (region: %s, no resource group specified)...\n", region)
		// Login without resource group - IBM Cloud will use account default
		cmd = exec.CommandContext(ctx, i.binaryPath, "login",
			"--apikey", apiKey,
			"-r", region)
	} else {
		fmt.Printf("Logging into IBM Cloud (region: %s, resource group: %s)...\n", region, resourceGroup)
		// Login with specified resource group
		cmd = exec.CommandContext(ctx, i.binaryPath, "login",
			"--apikey", apiKey,
			"-r", region,
			"-g", resourceGroup)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ibmcloud login failed: %w\nStderr: %s", err, stderr.String())
	}

	fmt.Printf("✓ IBM Cloud login successful\n")
	return nil
}

// ResolvedVLANs contains resolved VLAN IDs for a zone
type ResolvedVLANs struct {
	PublicVLAN  string
	PrivateVLAN string
}

// ResolveClassicVLANs queries available VLANs for a zone and returns the first public/private pair
func (i *IKSInstaller) ResolveClassicVLANs(ctx context.Context, zone string) (*ResolvedVLANs, error) {
	cmd := exec.CommandContext(ctx, i.binaryPath, "ks", "vlan", "ls", "--zone", zone, "--output", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("list VLANs: %w\nStderr: %s", err, stderr.String())
	}

	// Parse VLAN list
	var vlans []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &vlans); err != nil {
		return nil, fmt.Errorf("parse VLAN list: %w", err)
	}

	resolved := &ResolvedVLANs{}
	for _, vlan := range vlans {
		if vlan.Type == "public" && resolved.PublicVLAN == "" {
			resolved.PublicVLAN = vlan.ID
		}
		if vlan.Type == "private" && resolved.PrivateVLAN == "" {
			resolved.PrivateVLAN = vlan.ID
		}
	}

	if resolved.PublicVLAN == "" {
		return nil, fmt.Errorf("no public VLAN found in zone %s", zone)
	}
	if resolved.PrivateVLAN == "" {
		return nil, fmt.Errorf("no private VLAN found in zone %s", zone)
	}

	return resolved, nil
}

// CreateCluster creates an IKS cluster
// If logPath is provided, outputs are written to the log file for streaming
func (i *IKSInstaller) CreateCluster(ctx context.Context, opts *IKSClusterCreateOptions, logPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	args := []string{"ks", "cluster", "create", "classic",
		"--name", opts.Name,
		"--zone", opts.Zone,
		"--machine-type", opts.MachineType,
		"--workers", fmt.Sprintf("%d", opts.Workers),
	}

	if opts.KubeVersion != "" {
		args = append(args, "--version", opts.KubeVersion)
	}

	if opts.PublicVLAN != "" {
		args = append(args, "--public-vlan", opts.PublicVLAN)
	}

	if opts.PrivateVLAN != "" {
		args = append(args, "--private-vlan", opts.PrivateVLAN)
	}

	if opts.PublicServiceEndpoint {
		args = append(args, "--public-service-endpoint")
	}

	if opts.PrivateServiceEndpoint {
		args = append(args, "--private-service-endpoint")
	}

	cmd := exec.CommandContext(ctx, i.binaryPath, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// If logPath is provided, also write to log file
	var logFile *os.File
	if logPath != "" {
		var err error
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return "", fmt.Errorf("open log file: %w", err)
		}
		defer logFile.Close()

		// Write command being executed to log
		fmt.Fprintf(logFile, "$ ibmcloud %s\n", joinArgs(args))

		// Write to both buffer and log file
		cmd.Stdout = io.MultiWriter(&stdout, logFile)
		cmd.Stderr = io.MultiWriter(&stderr, logFile)
	}

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("ibmcloud ks cluster create failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// joinArgs joins command arguments for logging
func joinArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.Contains(arg, " ") {
			quoted[i] = fmt.Sprintf("\"%s\"", arg)
		} else {
			quoted[i] = arg
		}
	}
	return strings.Join(quoted, " ")
}

// DestroyCluster destroys an IKS cluster
func (i *IKSInstaller) DestroyCluster(ctx context.Context, clusterNameOrID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, i.binaryPath, "ks", "cluster", "rm",
		"--cluster", clusterNameOrID,
		"-f") // Force without confirmation

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("ibmcloud ks cluster rm failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// GetClusterInfo retrieves cluster information
func (i *IKSInstaller) GetClusterInfo(ctx context.Context, clusterNameOrID string) (*IKSClusterInfo, error) {
	cmd := exec.CommandContext(ctx, i.binaryPath, "ks", "cluster", "get",
		"--cluster", clusterNameOrID,
		"--output", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("get cluster info: %w\nStderr: %s", err, stderr.String())
	}

	var info IKSClusterInfo
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil {
		return nil, fmt.Errorf("parse cluster info: %w", err)
	}

	return &info, nil
}

// GetKubeconfig retrieves the kubeconfig for an IKS cluster
func (i *IKSInstaller) GetKubeconfig(ctx context.Context, clusterNameOrID, outputPath string) error {
	// Create a temporary directory for the config
	tempDir := filepath.Dir(outputPath)

	// Set KUBECONFIG environment variable to output path
	cmd := exec.CommandContext(ctx, i.binaryPath, "ks", "cluster", "config",
		"--cluster", clusterNameOrID,
		"--admin")

	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", outputPath))

	// Create directory structure for auth
	authDir := filepath.Join(tempDir, "auth")
	if err := os.MkdirAll(authDir, 0755); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("get kubeconfig: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// WaitForCluster waits for a cluster to reach the desired state
// If logPath is provided, status updates are written to the log file
func (i *IKSInstaller) WaitForCluster(ctx context.Context, clusterNameOrID, desiredState string, timeout time.Duration, logPath string) error {
	deadline := time.Now().Add(timeout)

	var logFile *os.File
	if logPath != "" {
		var err error
		logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		defer logFile.Close()

		fmt.Fprintf(logFile, "\nWaiting for cluster %s to reach state '%s' (timeout: %v)...\n", clusterNameOrID, desiredState, timeout)
	}

	for time.Now().Before(deadline) {
		info, err := i.GetClusterInfo(ctx, clusterNameOrID)
		if err != nil {
			return fmt.Errorf("get cluster info: %w", err)
		}

		if logFile != nil {
			fmt.Fprintf(logFile, "[%s] Cluster state: %s (target: %s)\n", time.Now().Format("15:04:05"), info.State, desiredState)
		}

		if info.State == desiredState {
			if logFile != nil {
				fmt.Fprintf(logFile, "[%s] Cluster reached desired state: %s\n", time.Now().Format("15:04:05"), desiredState)
			}
			return nil
		}

		// Wait 30 seconds before checking again
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Second):
			continue
		}
	}

	return fmt.Errorf("timeout waiting for cluster to reach state %s", desiredState)
}

// Version returns the ibmcloud CLI version
func (i *IKSInstaller) Version() (string, error) {
	cmd := exec.Command(i.binaryPath, "version")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ibmcloud version failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// ListWorkerPools lists all worker pools for a cluster
func (i *IKSInstaller) ListWorkerPools(ctx context.Context, clusterNameOrID string) ([]IKSWorkerPoolInfo, error) {
	cmd := exec.CommandContext(ctx, i.binaryPath, "ks", "worker-pool", "ls",
		"--cluster", clusterNameOrID,
		"--output", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("list worker pools: %w\nStderr: %s", err, stderr.String())
	}

	var pools []IKSWorkerPoolInfo
	if err := json.Unmarshal(stdout.Bytes(), &pools); err != nil {
		return nil, fmt.Errorf("parse worker pools: %w", err)
	}

	return pools, nil
}

// GetWorkerPool retrieves details about a specific worker pool
func (i *IKSInstaller) GetWorkerPool(ctx context.Context, clusterNameOrID, poolNameOrID string) (*IKSWorkerPoolInfo, error) {
	cmd := exec.CommandContext(ctx, i.binaryPath, "ks", "worker-pool", "get",
		"--cluster", clusterNameOrID,
		"--worker-pool", poolNameOrID,
		"--output", "json")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("get worker pool: %w\nStderr: %s", err, stderr.String())
	}

	var pool IKSWorkerPoolInfo
	if err := json.Unmarshal(stdout.Bytes(), &pool); err != nil {
		return nil, fmt.Errorf("parse worker pool: %w", err)
	}

	return &pool, nil
}

// ResizeWorkerPool resizes a worker pool to the specified size per zone
func (i *IKSInstaller) ResizeWorkerPool(ctx context.Context, clusterNameOrID, poolNameOrID string, sizePerZone int) error {
	cmd := exec.CommandContext(ctx, i.binaryPath, "ks", "worker-pool", "resize",
		"--cluster", clusterNameOrID,
		"--worker-pool", poolNameOrID,
		"--size-per-zone", fmt.Sprintf("%d", sizePerZone))

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("resize worker pool: %w\nStderr: %s\nStdout: %s", err, stderr.String(), stdout.String())
	}

	return nil
}

// GetTotalWorkerCount calculates the total worker count across all pools
func (i *IKSInstaller) GetTotalWorkerCount(ctx context.Context, clusterNameOrID string) (int, error) {
	pools, err := i.ListWorkerPools(ctx, clusterNameOrID)
	if err != nil {
		return 0, fmt.Errorf("list worker pools: %w", err)
	}

	totalCount := 0
	for _, pool := range pools {
		// Sum up worker count across all zones in the pool
		for _, zone := range pool.Zones {
			totalCount += zone.WorkerCount
		}
	}

	return totalCount, nil
}

// ScaleAllWorkerPools scales all worker pools to the specified size per zone
// Returns a map of pool name to original size for later restoration
func (i *IKSInstaller) ScaleAllWorkerPools(ctx context.Context, clusterNameOrID string, sizePerZone int) (map[string]int, error) {
	pools, err := i.ListWorkerPools(ctx, clusterNameOrID)
	if err != nil {
		return nil, fmt.Errorf("list worker pools: %w", err)
	}

	originalSizes := make(map[string]int)
	for _, pool := range pools {
		// Store original size
		originalSizes[pool.Name] = pool.SizePerZone

		// Skip if already at target size
		if pool.SizePerZone == sizePerZone {
			continue
		}

		// Resize the pool
		if err := i.ResizeWorkerPool(ctx, clusterNameOrID, pool.Name, sizePerZone); err != nil {
			return originalSizes, fmt.Errorf("resize worker pool %s: %w", pool.Name, err)
		}

		fmt.Printf("Scaled worker pool '%s' from %d to %d workers per zone\n", pool.Name, pool.SizePerZone, sizePerZone)
	}

	return originalSizes, nil
}
