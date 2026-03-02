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
	PullSecret    string
	SSHKey        string
	Platform      string
	Region        string

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

	// Tags
	UserTags map[string]string

	// AWS-specific
	AWSRootVolumeType string
	AWSRootVolumeSize int
	AWSRootVolumeIOPS int

	// IBM Cloud-specific
	IBMResourceGroup string
	IBMVPCName       string
}

// RenderInstallConfig generates an install-config.yaml file
func (r *Renderer) RenderInstallConfig(req *types.CreateClusterRequest, pullSecret string, mergedTags map[string]string) ([]byte, error) {
	// Get profile
	prof, err := r.registry.Get(req.Profile)
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}

	// Build template data
	data := InstallConfigData{
		ClusterName:          req.Name,
		BaseDomain:           req.BaseDomain,
		PullSecret:           pullSecret,
		SSHKey:               stringValue(req.SSHPublicKey),
		Platform:             req.Platform,
		Region:               req.Region,
		ControlPlaneReplicas: prof.Compute.ControlPlane.Replicas,
		ControlPlaneType:     prof.Compute.ControlPlane.InstanceType,
		WorkerReplicas:       prof.Compute.Workers.Replicas,
		WorkerType:           prof.Compute.Workers.InstanceType,
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
	}

	if prof.Platform == "ibmcloud" && prof.PlatformConfig.IBMCloud != nil {
		data.IBMResourceGroup = prof.PlatformConfig.IBMCloud.ResourceGroup
		data.IBMVPCName = prof.PlatformConfig.IBMCloud.VPCName
	}

	// Select appropriate template
	var tmplStr string
	switch prof.Platform {
	case "aws":
		tmplStr = awsInstallConfigTemplate
	case "ibmcloud":
		tmplStr = ibmCloudInstallConfigTemplate
	default:
		return nil, fmt.Errorf("unsupported platform: %s", prof.Platform)
	}

	// Render template
	tmpl, err := template.New("install-config").Parse(tmplStr)
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

// awsInstallConfigTemplate is the template for AWS install-config.yaml
const awsInstallConfigTemplate = `apiVersion: v1
baseDomain: {{.BaseDomain}}
metadata:
  name: {{.ClusterName}}
platform:
  aws:
    region: {{.Region}}
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
const ibmCloudInstallConfigTemplate = `apiVersion: v1
baseDomain: {{.BaseDomain}}
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
