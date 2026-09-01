package profile

import (
	"bytes"
	"fmt"
	"log"
	"sort"
	"strings"
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
	ClusterName     string
	BaseDomain      string
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

	// Azure-specific
	AzureBaseDomainResourceGroup string
	AzureZones                   []string // Availability zones to pin control-plane/worker machines to
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

	if prof.Platform == "azure" && prof.PlatformConfig.Azure != nil {
		data.AzureBaseDomainResourceGroup = prof.PlatformConfig.Azure.BaseDomainResourceGroup
		// Azure tag keys forbid ':' and the installer caps userTags at 10, so
		// sanitize keys and trim to an Azure-compliant subset (provenance kept).
		data.UserTags = azureUserTags(mergedTags)

		// Capacity pre-flight overrides (set by the worker after probing a
		// candidate from AzureConfig.CapacityFallback). These pin the create to a
		// region/zone/SKU combination whose zones actually have allocation
		// capacity. When unset (e.g. API-side validation render), the profile
		// defaults above stand.
		if len(req.AzureZones) > 0 {
			data.AzureZones = req.AzureZones
		}
		if req.AzureControlPlaneType != "" {
			data.ControlPlaneType = req.AzureControlPlaneType
		}
		if req.AzureWorkerType != "" {
			data.WorkerType = req.AzureWorkerType
		}
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
	case "azure":
		tmplStr = azureInstallConfigTemplate
	default:
		return nil, fmt.Errorf("unsupported platform: %s", prof.Platform)
	}

	// Render template with custom functions
	funcMap := template.FuncMap{
		"gcpLabelKey":   gcpLabelKey,   // Convert tag keys to GCP-compliant format
		"gcpLabelValue": gcpLabelValue, // Convert tag values to GCP-compliant format
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

// gcpLabelValue converts tag values to GCP-compliant label values
// GCP label values must:
// - Contain only lowercase letters, numeric characters, and _-
// - Have a maximum of 63 characters
// - Cannot be empty
func gcpLabelValue(value string) string {
	if value == "" {
		return "none"
	}

	var result []rune
	for _, r := range value {
		// Convert uppercase to lowercase
		if r >= 'A' && r <= 'Z' {
			result = append(result, r+32) // Convert to lowercase
		} else if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			// Keep lowercase letters, numbers, hyphens, and underscores
			result = append(result, r)
		} else {
			// Replace any other character with hyphen
			// But avoid consecutive hyphens
			if len(result) > 0 && result[len(result)-1] != '-' {
				result = append(result, '-')
			}
		}
	}

	// Remove trailing hyphens
	for len(result) > 0 && result[len(result)-1] == '-' {
		result = result[:len(result)-1]
	}

	// Truncate to 63 characters if needed
	if len(result) > 63 {
		result = result[:63]
		// Remove trailing hyphen after truncation
		for len(result) > 0 && result[len(result)-1] == '-' {
			result = result[:len(result)-1]
		}
	}

	// Return "none" if empty after sanitization
	if len(result) == 0 {
		return "none"
	}

	return string(result)
}

// azureMaxUserTags is the maximum number of userTags the OpenShift installer
// accepts for the Azure platform (platform.azure.userTags).
const azureMaxUserTags = 10

// azureRedundantTagKeys are request tag keys that duplicate information already
// carried by the ocpctl provenance tags (or are otherwise trivially inferable),
// so they are the first to be dropped when trimming to Azure's 10-tag limit.
var azureRedundantTagKeys = map[string]bool{
	"ClusterName": true, // duplicates ocpctl:cluster-name
	"Platform":    true, // always the cluster's platform
	"ManagedBy":   true, // always "ocpctl"; duplicates ocpctl:managed
}

// azureUserTags returns an Azure-compliant subset of tags for
// platform.azure.userTags. Azure tag keys forbid ':' (used by the "ocpctl:"
// provenance prefix) and the installer rejects more than 10 userTags, so this:
//   - sanitizes every key (see sanitizeAzureTagKey),
//   - always retains the 4 ocpctl provenance tags (needed for orphan cleanup),
//   - fills the remaining slots with request tags up to azureMaxUserTags,
//     dropping redundant keys first and then alphabetically (deterministic).
//
// Dropped keys are logged so the trimming is visible in worker logs.
func azureUserTags(merged map[string]string) map[string]string {
	provenanceKeys := map[string]bool{
		types.TagKeyManaged:     true,
		types.TagKeyClusterID:   true,
		types.TagKeyClusterName: true,
		types.TagKeyCreatedAt:   true,
	}

	result := make(map[string]string, azureMaxUserTags)

	// Always keep provenance tags (with sanitized keys).
	for k, v := range merged {
		if provenanceKeys[k] {
			if sk := sanitizeAzureTagKey(k); sk != "" {
				result[sk] = v
			}
		}
	}

	// Order request (non-provenance) keys so non-redundant keys come first
	// (alphabetically) and redundant keys last; this makes redundant keys the
	// first to be dropped when we exceed the limit.
	var requestKeys []string
	for k := range merged {
		if !provenanceKeys[k] {
			requestKeys = append(requestKeys, k)
		}
	}
	sort.Slice(requestKeys, func(i, j int) bool {
		ri, rj := azureRedundantTagKeys[requestKeys[i]], azureRedundantTagKeys[requestKeys[j]]
		if ri != rj {
			return !ri // non-redundant sorts before redundant
		}
		return requestKeys[i] < requestKeys[j]
	})

	var dropped []string
	for _, k := range requestKeys {
		sk := sanitizeAzureTagKey(k)
		if sk == "" || len(result) >= azureMaxUserTags {
			dropped = append(dropped, k)
			continue
		}
		if _, exists := result[sk]; exists {
			// Sanitized key collides with a provenance/earlier key; skip.
			dropped = append(dropped, k)
			continue
		}
		result[sk] = merged[k]
	}

	if len(dropped) > 0 {
		log.Printf("Azure userTags: kept %d of %d tags (installer limit %d); dropped: %v",
			len(result), len(merged), azureMaxUserTags, dropped)
	}

	return result
}

// sanitizeAzureTagKey converts a tag key to an Azure-compliant tag key. Azure
// keys may contain only alphanumerics and '_', '.', '-' (notably no ':'), must
// begin with a letter, must end with a letter, number or underscore, and are at
// most 128 characters. Invalid characters (e.g. the ':' in "ocpctl:managed")
// are replaced with '_'. Returns "" if nothing valid remains.
func sanitizeAzureTagKey(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	// Must begin with a letter and end with a letter, number or underscore.
	s := strings.TrimLeft(b.String(), "_.-0123456789")
	s = strings.TrimRight(s, ".-")
	if len(s) > 128 {
		s = strings.TrimRight(s[:128], ".-")
	}
	return s
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
publish: {{.PublishStrategy}}
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
      value: "{{gcpLabelValue $value}}"
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

const azureInstallConfigTemplate = `apiVersion: v1
baseDomain: {{.BaseDomain}}
metadata:
  name: {{.ClusterName}}
publish: {{.PublishStrategy}}
platform:
  azure:
    region: {{.Region}}
    baseDomainResourceGroupName: {{.AzureBaseDomainResourceGroup}}
    defaultMachinePlatform:
      identity:
        type: None
{{- if .UserTags}}
    userTags:
{{- range $key, $value := .UserTags}}
      {{$key}}: {{$value}}
{{- end}}
{{- end}}
controlPlane:
  name: master
  replicas: {{.ControlPlaneReplicas}}
  platform:
    azure:
      type: {{.ControlPlaneType}}
{{- if .AzureZones}}
      zones:
{{- range .AzureZones}}
      - "{{.}}"
{{- end}}
{{- end}}
compute:
- name: worker
  replicas: {{.WorkerReplicas}}
  platform:
    azure:
      type: {{.WorkerType}}
{{- if .AzureZones}}
      zones:
{{- range .AzureZones}}
      - "{{.}}"
{{- end}}
{{- end}}
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
