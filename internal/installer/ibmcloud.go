package installer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
func (i *IKSInstaller) CreateCluster(ctx context.Context, opts *IKSClusterCreateOptions) (string, error) {
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

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("ibmcloud ks cluster create failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
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
func (i *IKSInstaller) WaitForCluster(ctx context.Context, clusterNameOrID, desiredState string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		info, err := i.GetClusterInfo(ctx, clusterNameOrID)
		if err != nil {
			return fmt.Errorf("get cluster info: %w", err)
		}

		if info.State == desiredState {
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
