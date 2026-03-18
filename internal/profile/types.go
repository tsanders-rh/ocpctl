package profile

import "github.com/tsanders-rh/ocpctl/pkg/types"

// Profile represents a complete cluster profile loaded from YAML
type Profile struct {
	Name               string              `yaml:"name" validate:"required"`
	DisplayName        string              `yaml:"displayName" validate:"required"`
	Description        string              `yaml:"description" validate:"required"`
	Platform           types.Platform      `yaml:"platform" validate:"required,oneof=aws ibmcloud"`
	Enabled            bool                `yaml:"enabled"`
	OpenshiftVersions  VersionConfig       `yaml:"openshiftVersions" validate:"required"`
	Regions            RegionConfig        `yaml:"regions" validate:"required"`
	BaseDomains        BaseDomainConfig    `yaml:"baseDomains" validate:"required"`
	Compute            ComputeConfig       `yaml:"compute" validate:"required"`
	Lifecycle          LifecycleConfig     `yaml:"lifecycle" validate:"required"`
	Networking         *NetworkingConfig   `yaml:"networking,omitempty"`
	Tags               TagsConfig          `yaml:"tags" validate:"required"`
	Features           FeaturesConfig      `yaml:"features"`
	CostControls       CostControlsConfig  `yaml:"costControls"`
	PlatformConfig     PlatformConfig      `yaml:"platformConfig"`
	PostDeployment     *PostDeploymentConfig `yaml:"postDeployment,omitempty"`
}

// VersionConfig defines OpenShift version constraints
type VersionConfig struct {
	Allowlist []string `yaml:"allowlist" json:"allowed" validate:"required,min=1"`
	Default   string   `yaml:"default" json:"default" validate:"required"`
}

// RegionConfig defines region constraints
type RegionConfig struct {
	Allowlist []string `yaml:"allowlist" json:"allowed" validate:"required,min=1"`
	Default   string   `yaml:"default" json:"default" validate:"required"`
}

// BaseDomainConfig defines base domain constraints
type BaseDomainConfig struct {
	Allowlist []string `yaml:"allowlist" json:"allowed" validate:"required,min=1"`
	Default   string   `yaml:"default" json:"default" validate:"required"`
}

// ComputeConfig defines compute resource configuration
type ComputeConfig struct {
	ControlPlane ControlPlaneConfig `yaml:"controlPlane" json:"control_plane" validate:"required"`
	Workers      WorkersConfig      `yaml:"workers" json:"workers" validate:"required"`
}

// ControlPlaneConfig defines control plane node configuration
type ControlPlaneConfig struct {
	Replicas     int    `yaml:"replicas" json:"replicas" validate:"required,min=1,odd"`
	InstanceType string `yaml:"instanceType" json:"instance_type" validate:"required"`
	Schedulable  bool   `yaml:"schedulable" json:"schedulable"`
}

// WorkersConfig defines worker node configuration
type WorkersConfig struct {
	Replicas     int    `yaml:"replicas" json:"replicas" validate:"min=0"`
	MinReplicas  int    `yaml:"minReplicas" json:"min_replicas" validate:"min=0"`
	MaxReplicas  int    `yaml:"maxReplicas" json:"max_replicas" validate:"gtefield=MinReplicas"`
	InstanceType string `yaml:"instanceType" json:"instance_type" validate:"required"`
	Autoscaling  bool   `yaml:"autoscaling" json:"autoscaling"`
}

// LifecycleConfig defines cluster lifecycle policies
type LifecycleConfig struct {
	MaxTTLHours              int  `yaml:"maxTTLHours" json:"max_ttl_hours" validate:"required,min=1"`
	DefaultTTLHours          int  `yaml:"defaultTTLHours" json:"default_ttl_hours" validate:"required,min=1,ltefield=MaxTTLHours"`
	AllowCustomTTL           bool `yaml:"allowCustomTTL" json:"allow_custom_ttl"`
	WarnBeforeDestroyHours   int  `yaml:"warnBeforeDestroyHours" json:"warn_before_destroy_hours" validate:"min=0"`
}

// NetworkingConfig defines networking configuration
type NetworkingConfig struct {
	NetworkType     string                 `yaml:"networkType"`
	ClusterNetworks []ClusterNetworkConfig `yaml:"clusterNetworks"`
	ServiceNetwork  []string               `yaml:"serviceNetwork"`
	MachineNetwork  []MachineNetworkConfig `yaml:"machineNetwork"`
}

// ClusterNetworkConfig defines cluster network CIDR
type ClusterNetworkConfig struct {
	CIDR       string `yaml:"cidr"`
	HostPrefix int    `yaml:"hostPrefix"`
}

// MachineNetworkConfig defines machine network CIDR
type MachineNetworkConfig struct {
	CIDR string `yaml:"cidr"`
}

// TagsConfig defines tag requirements
type TagsConfig struct {
	Required       map[string]string `yaml:"required" json:"required"`
	Defaults       map[string]string `yaml:"defaults" json:"defaults"`
	AllowUserTags  bool              `yaml:"allowUserTags" json:"allow_user_tags"`
}

// FeaturesConfig defines feature flags
type FeaturesConfig struct {
	OffHoursScaling bool `yaml:"offHoursScaling" json:"off_hours_scaling"`
	FIPSMode        bool `yaml:"fipsMode" json:"fips_mode"`
	PrivateCluster  bool `yaml:"privateCluster" json:"private_cluster"`
}

// CostControlsConfig defines cost management settings
type CostControlsConfig struct {
	EstimatedHourlyCost  float64 `yaml:"estimatedHourlyCost" json:"estimated_hourly_cost"`
	MaxMonthlyCost       float64 `yaml:"maxMonthlyCost" json:"max_monthly_cost"`
	BudgetAlertThreshold float64 `yaml:"budgetAlertThreshold" json:"budget_alert_threshold" validate:"min=0,max=1"`
	WarningMessage       string  `yaml:"warningMessage,omitempty" json:"warning_message,omitempty"`
}

// PlatformConfig contains platform-specific configuration
type PlatformConfig struct {
	AWS      *AWSConfig      `yaml:"aws,omitempty"`
	IBMCloud *IBMCloudConfig `yaml:"ibmcloud,omitempty"`
}

// AWSConfig contains AWS-specific settings
type AWSConfig struct {
	InstanceMetadataService string          `yaml:"instanceMetadataService"`
	RootVolume              *AWSRootVolume  `yaml:"rootVolume"`
	Subnets                 []string        `yaml:"subnets,omitempty"`
}

// AWSRootVolume defines root volume configuration
type AWSRootVolume struct {
	Type string `yaml:"type"`
	Size int    `yaml:"size"`
	IOPS int    `yaml:"iops,omitempty"`
}

// IBMCloudConfig contains IBM Cloud-specific settings
type IBMCloudConfig struct {
	ResourceGroup string `yaml:"resourceGroup"`
	VPCName       string `yaml:"vpcName"`
}

// PostDeploymentConfig defines automated post-deployment configuration
type PostDeploymentConfig struct {
	Enabled    bool                 `yaml:"enabled" json:"enabled"`
	Timeout    string               `yaml:"timeout,omitempty" json:"timeout,omitempty"` // Duration string, e.g. "30m"
	Operators  []OperatorConfig     `yaml:"operators,omitempty" json:"operators,omitempty"`
	Scripts    []ScriptConfig       `yaml:"scripts,omitempty" json:"scripts,omitempty"`
	Manifests  []ManifestConfig     `yaml:"manifests,omitempty" json:"manifests,omitempty"`
	HelmCharts []HelmChartConfig    `yaml:"helmCharts,omitempty" json:"helm_charts,omitempty"`
}

// OperatorConfig defines an operator to install post-deployment
type OperatorConfig struct {
	Name           string                 `yaml:"name" json:"name" validate:"required"`
	Namespace      string                 `yaml:"namespace" json:"namespace" validate:"required"`
	Source         string                 `yaml:"source" json:"source" validate:"required"` // e.g. "redhat-operators"
	Channel        string                 `yaml:"channel" json:"channel" validate:"required"`
	CustomResource *CustomResourceConfig  `yaml:"customResource,omitempty" json:"custom_resource,omitempty"`
}

// CustomResourceConfig defines a custom resource to create after operator installation
type CustomResourceConfig struct {
	APIVersion string                 `yaml:"apiVersion" json:"api_version" validate:"required"`
	Kind       string                 `yaml:"kind" json:"kind" validate:"required"`
	Name       string                 `yaml:"name" json:"name" validate:"required"`
	Namespace  string                 `yaml:"namespace,omitempty" json:"namespace,omitempty"`
	Spec       map[string]interface{} `yaml:"spec,omitempty" json:"spec,omitempty"`
}

// ScriptConfig defines a script to execute post-deployment
type ScriptConfig struct {
	Name        string            `yaml:"name" json:"name" validate:"required"`
	Path        string            `yaml:"path" json:"path" validate:"required"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Env         map[string]string `yaml:"env,omitempty" json:"env,omitempty"` // Additional environment variables
}

// ManifestConfig defines a manifest file to apply post-deployment
type ManifestConfig struct {
	Name        string `yaml:"name" json:"name" validate:"required"`
	Path        string `yaml:"path" json:"path" validate:"required"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// HelmChartConfig defines a Helm chart to install post-deployment
type HelmChartConfig struct {
	Name   string                 `yaml:"name" json:"name" validate:"required"`
	Repo   string                 `yaml:"repo" json:"repo" validate:"required"`
	Chart  string                 `yaml:"chart" json:"chart" validate:"required"`
	Values map[string]interface{} `yaml:"values,omitempty" json:"values,omitempty"`
}

// ReservedTagKeys are tag keys that cannot be overridden by users
var ReservedTagKeys = []string{
	"ManagedBy",
	"ClusterId",
	"ClusterName",
	"Owner",
	"Team",
	"CostCenter",
	"Environment",
	"TTLExpiry",
	"RequestId",
	"Profile",
	"Platform",
}

// IsReservedTagKey checks if a tag key is reserved
func IsReservedTagKey(key string) bool {
	for _, reserved := range ReservedTagKeys {
		if key == reserved {
			return true
		}
	}
	return false
}
