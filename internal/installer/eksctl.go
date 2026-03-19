package installer

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// EKSInstaller wraps the eksctl CLI for EKS cluster operations
type EKSInstaller struct {
	binaryPath string
	timeout    time.Duration
}

// EKSClusterConfig represents an eksctl cluster configuration
type EKSClusterConfig struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   EKSMetadata            `yaml:"metadata"`
	IAM        *EKSIAM                `yaml:"iam,omitempty"`
	NodeGroups []EKSNodeGroup         `yaml:"nodeGroups"`
	VPC        *EKSVPC                `yaml:"vpc,omitempty"`
	Tags       map[string]string      `yaml:"tags,omitempty"`
}

// EKSMetadata represents cluster metadata
type EKSMetadata struct {
	Name    string `yaml:"name"`
	Region  string `yaml:"region"`
	Version string `yaml:"version"`
}

// EKSIAM represents IAM configuration
type EKSIAM struct {
	WithOIDC bool `yaml:"withOIDC"`
}

// EKSNodeGroup represents a node group configuration
type EKSNodeGroup struct {
	Name            string            `yaml:"name"`
	InstanceType    string            `yaml:"instanceType"`
	DesiredCapacity int               `yaml:"desiredCapacity"`
	MinSize         int               `yaml:"minSize"`
	MaxSize         int               `yaml:"maxSize"`
	VolumeSize      int               `yaml:"volumeSize,omitempty"`
	VolumeType      string            `yaml:"volumeType,omitempty"`
	SSH             *EKSNodeGroupSSH  `yaml:"ssh,omitempty"`
	Tags            map[string]string `yaml:"tags,omitempty"`
}

// EKSNodeGroupSSH represents SSH configuration for nodes
type EKSNodeGroupSSH struct {
	Allow         bool   `yaml:"allow"`
	PublicKeyPath string `yaml:"publicKeyPath,omitempty"`
}

// EKSVPC represents VPC configuration
type EKSVPC struct {
	CIDR string `yaml:"cidr,omitempty"`
	NAT  *EKSNAT `yaml:"nat,omitempty"`
}

// EKSNAT represents NAT gateway configuration
type EKSNAT struct {
	Gateway string `yaml:"gateway"` // "Single" or "HighlyAvailable"
}

// NewEKSInstaller creates a new EKS installer instance
func NewEKSInstaller() *EKSInstaller {
	binaryPath := os.Getenv("EKSCTL_BINARY")
	if binaryPath == "" {
		binaryPath = "/usr/local/bin/eksctl"
	}

	return &EKSInstaller{
		binaryPath: binaryPath,
		timeout:    60 * time.Minute, // EKS clusters typically take 15-20 minutes
	}
}

// CreateCluster creates an EKS cluster using eksctl
func (e *EKSInstaller) CreateCluster(ctx context.Context, configPath string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.binaryPath, "create", "cluster", "-f", configPath, "--verbose", "4")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("eksctl create cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// DestroyCluster destroys an EKS cluster using eksctl
func (e *EKSInstaller) DestroyCluster(ctx context.Context, clusterName, region string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, e.binaryPath, "delete", "cluster",
		"--name", clusterName,
		"--region", region,
		"--wait")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stderr.String(), fmt.Errorf("eksctl delete cluster failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}

// WriteConfig writes an eksctl configuration file
func (e *EKSInstaller) WriteConfig(workDir string, config *EKSClusterConfig) error {
	configPath := filepath.Join(workDir, "eksctl-config.yaml")

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// GetKubeconfig retrieves the kubeconfig for an EKS cluster
func (e *EKSInstaller) GetKubeconfig(ctx context.Context, clusterName, region, outputPath string) error {
	cmd := exec.CommandContext(ctx, e.binaryPath, "utils", "write-kubeconfig",
		"--cluster", clusterName,
		"--region", region,
		"--kubeconfig", outputPath)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("get kubeconfig: %w\nStderr: %s", err, stderr.String())
	}

	return nil
}

// Version returns the eksctl version
func (e *EKSInstaller) Version() (string, error) {
	cmd := exec.Command(e.binaryPath, "version")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("eksctl version failed: %w\nStderr: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
