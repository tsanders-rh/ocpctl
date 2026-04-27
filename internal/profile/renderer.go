package profile

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/tsanders-rh/ocpctl/pkg/types"
	"gopkg.in/yaml.v3"
)

// Renderer generates install-config.yaml from profiles and requests
type Renderer struct {
	registry *Registry
}

// NewRenderer creates a new install-config renderer
func NewRenderer(registry *Registry) *Renderer {
	return &Renderer{
		registry: registry,
	}
}

// InstallConfigData holds data for rendering install-config.yaml
type InstallConfigData struct {
	ClusterName   string
	BaseDomain    string
	PullSecret      string
	SSHKey          string
	Platform        string
	Region          string
	CredentialsMode string

	// Compute
	ControlPlaneReplicas int
	ControlPlaneType     string
	WorkerReplicas       int
	WorkerType           string

	// Networking
	NetworkType     string
	ClusterCIDR     string
	ClusterPrefix   int
	ServiceCIDR     string
	MachineCIDR     string
	PublishStrategy string // "External" or "Internal"

	// Tags
	UserTags map[string]string

	// AWS-specific
	AWSRootVolumeType string
	AWSRootVolumeSize int
	AWSRootVolumeIOPS int
	AWSSubnets        []string

	// IBM Cloud-specific
	IBMResourceGroup string
	IBMVPCName       string

	// GCP-specific
	GCPProject    string
	GCPNetwork    string
	GCPSubnetwork string
}

// RenderInstallConfig generates an install-config.yaml file
func (r *Renderer) RenderInstallConfig(req *types.CreateClusterRequest, pullSecret string, mergedTags map[string]string) ([]byte, error) {
	// Get profile
	prof, err := r.registry.Get(req.Profile)
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}

	// Determine publish strategy based on privateCluster setting
	publishStrategy := "External" // Default to External (public API)
	if prof.Features.PrivateCluster {
		publishStrategy = "Internal" // Private clusters use internal-only API
	}

	// Use credentials mode from request, or fall back to profile default
	// Both "Static" and "Mint" work with permanent IAM credentials from environment
	credentialsMode := stringValue(req.CredentialsMode)
	if credentialsMode == "" && prof.CredentialsMode != "" {
		credentialsMode = prof.CredentialsMode
	}

	// "Auto" means omit credentialsMode from install-config.yaml
	// This allows the installer to auto-detect (Mint or Passthrough)
	if credentialsMode == "Auto" {
		credentialsMode = ""
	}

	// Build template data
	data := InstallConfigData{
		ClusterName:          req.Name,
		BaseDomain:           req.BaseDomain,
		PullSecret:           pullSecret,
		SSHKey:               stringValue(req.SSHPublicKey),
		Platform:             req.Platform,
		Region:               req.Region,
		CredentialsMode:      credentialsMode,
		ControlPlaneReplicas: prof.Compute.ControlPlane.Replicas,
		ControlPlaneType:     prof.Compute.ControlPlane.InstanceType,
		WorkerReplicas:       prof.Compute.Workers.Replicas,
		WorkerType:           prof.Compute.Workers.InstanceType,
		PublishStrategy:      publishStrategy,
		UserTags:             mergedTags,
	}

	// Set networking defaults
	if prof.Networking != nil {
		data.NetworkType = prof.Networking.NetworkType
		if len(prof.Networking.ClusterNetworks) > 0 {
			data.ClusterCIDR = prof.Networking.ClusterNetworks[0].CIDR
			data.ClusterPrefix = prof.Networking.ClusterNetworks[0].HostPrefix
		}
		if len(prof.Networking.ServiceNetwork) > 0 {
			data.ServiceCIDR = prof.Networking.ServiceNetwork[0]
		}
		if len(prof.Networking.MachineNetwork) > 0 {
			data.MachineCIDR = prof.Networking.MachineNetwork[0].CIDR
		}
	} else {
		// OpenShift defaults
		data.NetworkType = "OVNKubernetes"
		data.ClusterCIDR = "10.128.0.0/14"
		data.ClusterPrefix = 23
		data.ServiceCIDR = "172.30.0.0/16"
		data.MachineCIDR = "10.0.0.0/16"
	}

	// Platform-specific configuration
	if prof.Platform == "aws" && prof.PlatformConfig.AWS != nil {
		if prof.PlatformConfig.AWS.RootVolume != nil {
			data.AWSRootVolumeType = prof.PlatformConfig.AWS.RootVolume.Type
			data.AWSRootVolumeSize = prof.PlatformConfig.AWS.RootVolume.Size
			data.AWSRootVolumeIOPS = prof.PlatformConfig.AWS.RootVolume.IOPS
		}
		data.AWSSubnets = prof.PlatformConfig.AWS.Subnets
	}

	if prof.Platform == "ibmcloud" && prof.PlatformConfig.IBMCloud != nil {
		data.IBMResourceGroup = prof.PlatformConfig.IBMCloud.ResourceGroup
		data.IBMVPCName = prof.PlatformConfig.IBMCloud.VPCName
	}

	if prof.Platform == "gcp" && prof.PlatformConfig.GCP != nil {
		data.GCPProject = prof.PlatformConfig.GCP.Project
		data.GCPNetwork = prof.PlatformConfig.GCP.Network
		data.GCPSubnetwork = prof.PlatformConfig.GCP.Subnetwork
	}

	// Select appropriate template
	var tmplStr string
	switch prof.Platform {
	case "aws":
		tmplStr = awsInstallConfigTemplate
	case "ibmcloud":
		tmplStr = ibmCloudInstallConfigTemplate
	case "gcp":
		tmplStr = gcpInstallConfigTemplate
	default:
		return nil, fmt.Errorf("unsupported platform: %s", prof.Platform)
	}

	// Render template with custom functions
	funcMap := template.FuncMap{
		"gcpLabelKey": gcpLabelKey, // Convert tag keys to GCP-compliant format
	}
	tmpl, err := template.New("install-config").Funcs(funcMap).Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}

	// Parse YAML to validate
	var installConfig map[string]interface{}
	if err := yaml.Unmarshal(buf.Bytes(), &installConfig); err != nil {
		return nil, fmt.Errorf("validate generated YAML: %w", err)
	}

	return buf.Bytes(), nil
}

// stringValue safely dereferences a string pointer
func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// gcpLabelKey converts tag keys to GCP-compliant label keys
// GCP label keys must:
// - Begin with a lowercase letter
// - Contain only lowercase letters, numeric characters, and _-
// - Have a maximum of 63 characters
func gcpLabelKey(key string) string {
	if key == "" {
		return ""
	}

	var result []rune
	for i, r := range key {
		// Convert to lowercase
		if r >= 'A' && r <= 'Z' {
			// Add hyphen before uppercase letters (except first character)
			if i > 0 && len(result) > 0 {
				result = append(result, '-')
			}
			result = append(result, r+32) // Convert to lowercase
		} else if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			// Keep lowercase letters, numbers, hyphens, and underscores
			result = append(result, r)
		}
		// Skip any other characters (spaces, special chars, etc.)
	}

	// Ensure it starts with a lowercase letter
	for len(result) > 0 && !(result[0] >= 'a' && result[0] <= 'z') {
		result = result[1:]
	}

	// Truncate to 63 characters if needed
	if len(result) > 63 {
		result = result[:63]
	}

	return string(result)
}

// awsInstallConfigTemplate is the template for AWS install-config.yaml
// credentialsMode is optional and defaults to installer auto-detection (Mint or Passthrough)
const awsInstallConfigTemplate = `apiVersion: v1
baseDomain: {{.BaseDomain}}
{{- if .CredentialsMode}}
credentialsMode: {{.CredentialsMode}}
{{- end}}
metadata:
  name: {{.ClusterName}}
publish: {{.PublishStrategy}}
platform:
  aws:
    region: {{.Region}}
{{- if .AWSSubnets}}
    subnets:
{{- range .AWSSubnets}}
      - {{.}}
{{- end}}
{{- end}}
{{- if .UserTags}}
    userTags:
{{- range $key, $value := .UserTags}}
      {{$key}}: {{$value}}
{{- end}}
{{- end}}
{{- if .AWSRootVolumeType}}
    defaultMachinePlatform:
      rootVolume:
        type: {{.AWSRootVolumeType}}
        size: {{.AWSRootVolumeSize}}
{{- if .AWSRootVolumeIOPS}}
        iops: {{.AWSRootVolumeIOPS}}
{{- end}}
{{- end}}
controlPlane:
  name: master
  replicas: {{.ControlPlaneReplicas}}
  platform:
    aws:
      type: {{.ControlPlaneType}}
compute:
- name: worker
  replicas: {{.WorkerReplicas}}
  platform:
    aws:
      type: {{.WorkerType}}
networking:
  networkType: {{.NetworkType}}
  clusterNetwork:
  - cidr: {{.ClusterCIDR}}
    hostPrefix: {{.ClusterPrefix}}
  serviceNetwork:
  - {{.ServiceCIDR}}
  machineNetwork:
  - cidr: {{.MachineCIDR}}
pullSecret: '{{.PullSecret}}'
{{- if .SSHKey}}
sshKey: '{{.SSHKey}}'
{{- end}}
`

// ibmCloudInstallConfigTemplate is the template for IBM Cloud install-config.yaml
// IBM Cloud typically requires Manual credentialsMode with ccoctl
const ibmCloudInstallConfigTemplate = `apiVersion: v1
baseDomain: {{.BaseDomain}}
{{- if .CredentialsMode}}
credentialsMode: {{.CredentialsMode}}
{{- else}}
credentialsMode: Manual
{{- end}}
metadata:
  name: {{.ClusterName}}
platform:
  ibmcloud:
    region: {{.Region}}
{{- if .IBMResourceGroup}}
    resourceGroupName: {{.IBMResourceGroup}}
{{- end}}
{{- if .IBMVPCName}}
    vpcName: {{.IBMVPCName}}
{{- end}}
controlPlane:
  name: master
  replicas: {{.ControlPlaneReplicas}}
  platform:
    ibmcloud:
      type: {{.ControlPlaneType}}
compute:
- name: worker
  replicas: {{.WorkerReplicas}}
  platform:
    ibmcloud:
      type: {{.WorkerType}}
networking:
  networkType: {{.NetworkType}}
  clusterNetwork:
  - cidr: {{.ClusterCIDR}}
    hostPrefix: {{.ClusterPrefix}}
  serviceNetwork:
  - {{.ServiceCIDR}}
  machineNetwork:
  - cidr: {{.MachineCIDR}}
pullSecret: '{{.PullSecret}}'
{{- if .SSHKey}}
sshKey: '{{.SSHKey}}'
{{- end}}
`

// gcpInstallConfigTemplate is the template for GCP install-config.yaml
const gcpInstallConfigTemplate = `apiVersion: v1
baseDomain: {{.BaseDomain}}
{{- if .CredentialsMode}}
credentialsMode: {{.CredentialsMode}}
{{- end}}
metadata:
  name: {{.ClusterName}}
publish: {{.PublishStrategy}}
platform:
  gcp:
    projectID: {{.GCPProject}}
    region: {{.Region}}
{{- if .GCPNetwork}}
    network: {{.GCPNetwork}}
{{- end}}
{{- if .GCPSubnetwork}}
    computeSubnet: {{.GCPSubnetwork}}
    controlPlaneSubnet: {{.GCPSubnetwork}}
{{- end}}
{{- if .UserTags}}
    userLabels:
{{- range $key, $value := .UserTags}}
    - key: {{gcpLabelKey $key}}
      value: "{{$value}}"
{{- end}}
{{- end}}
controlPlane:
  name: master
  replicas: {{.ControlPlaneReplicas}}
  platform:
    gcp:
      type: {{.ControlPlaneType}}
compute:
- name: worker
  replicas: {{.WorkerReplicas}}
  platform:
    gcp:
      type: {{.WorkerType}}
networking:
  networkType: {{.NetworkType}}
  clusterNetwork:
  - cidr: {{.ClusterCIDR}}
    hostPrefix: {{.ClusterPrefix}}
  serviceNetwork:
  - {{.ServiceCIDR}}
  machineNetwork:
  - cidr: {{.MachineCIDR}}
pullSecret: '{{.PullSecret}}'
{{- if .SSHKey}}
sshKey: '{{.SSHKey}}'
{{- end}}
`
