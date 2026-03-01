package profile

import "github.com/tsanders-rh/ocpctl/pkg/types"

// Profile represents a complete cluster profile loaded from YAML
type Profile struct {
	Name               string             `yaml:"name" validate:"required"`
	DisplayName        string             `yaml:"displayName" validate:"required"`
	Description        string             `yaml:"description" validate:"required"`
	Platform           types.Platform     `yaml:"platform" validate:"required,oneof=aws ibmcloud"`
	Enabled            bool               `yaml:"enabled"`
	OpenshiftVersions  VersionConfig      `yaml:"openshiftVersions" validate:"required"`
	Regions            RegionConfig       `yaml:"regions" validate:"required"`
	BaseDomains        BaseDomainConfig   `yaml:"baseDomains" validate:"required"`
	Compute            ComputeConfig      `yaml:"compute" validate:"required"`
	Lifecycle          LifecycleConfig    `yaml:"lifecycle" validate:"required"`
	Networking         *NetworkingConfig  `yaml:"networking,omitempty"`
	Tags               TagsConfig         `yaml:"tags" validate:"required"`
	Features           FeaturesConfig     `yaml:"features"`
	CostControls       CostControlsConfig `yaml:"costControls"`
	PlatformConfig     PlatformConfig     `yaml:"platformConfig"`
}

// VersionConfig defines OpenShift version constraints
type VersionConfig struct {
	Allowlist []string `yaml:"allowlist" validate:"required,min=1"`
	Default   string   `yaml:"default" validate:"required"`
}

// RegionConfig defines region constraints
type RegionConfig struct {
	Allowlist []string `yaml:"allowlist" validate:"required,min=1"`
	Default   string   `yaml:"default" validate:"required"`
}

// BaseDomainConfig defines base domain constraints
type BaseDomainConfig struct {
	Allowlist []string `yaml:"allowlist" validate:"required,min=1"`
	Default   string   `yaml:"default" validate:"required"`
}

// ComputeConfig defines compute resource configuration
type ComputeConfig struct {
	ControlPlane ControlPlaneConfig `yaml:"controlPlane" validate:"required"`
	Workers      WorkersConfig      `yaml:"workers" validate:"required"`
}

// ControlPlaneConfig defines control plane node configuration
type ControlPlaneConfig struct {
	Replicas     int    `yaml:"replicas" validate:"required,min=1,odd"`
	InstanceType string `yaml:"instanceType" validate:"required"`
	Schedulable  bool   `yaml:"schedulable"`
}

// WorkersConfig defines worker node configuration
type WorkersConfig struct {
	Replicas     int    `yaml:"replicas" validate:"min=0"`
	MinReplicas  int    `yaml:"minReplicas" validate:"min=0"`
	MaxReplicas  int    `yaml:"maxReplicas" validate:"gtefield=MinReplicas"`
	InstanceType string `yaml:"instanceType" validate:"required"`
	Autoscaling  bool   `yaml:"autoscaling"`
}

// LifecycleConfig defines cluster lifecycle policies
type LifecycleConfig struct {
	MaxTTLHours              int  `yaml:"maxTTLHours" validate:"required,min=1"`
	DefaultTTLHours          int  `yaml:"defaultTTLHours" validate:"required,min=1,ltefield=MaxTTLHours"`
	AllowCustomTTL           bool `yaml:"allowCustomTTL"`
	WarnBeforeDestroyHours   int  `yaml:"warnBeforeDestroyHours" validate:"min=0"`
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
	Required       map[string]string `yaml:"required"`
	Defaults       map[string]string `yaml:"defaults"`
	AllowUserTags  bool              `yaml:"allowUserTags"`
}

// FeaturesConfig defines feature flags
type FeaturesConfig struct {
	OffHoursScaling bool `yaml:"offHoursScaling"`
	FIPSMode        bool `yaml:"fipsMode"`
	PrivateCluster  bool `yaml:"privateCluster"`
}

// CostControlsConfig defines cost management settings
type CostControlsConfig struct {
	EstimatedHourlyCost  float64 `yaml:"estimatedHourlyCost" json:"estimated_hourly_cost"`
	MaxMonthlyCost       float64 `yaml:"maxMonthlyCost" json:"max_monthly_cost"`
	BudgetAlertThreshold float64 `yaml:"budgetAlertThreshold" json:"budget_alert_threshold" validate:"min=0,max=1"`
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
