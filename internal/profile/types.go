package profile

import "github.com/tsanders-rh/ocpctl/pkg/types"

// Profile represents a complete cluster profile loaded from YAML
type Profile struct {
	Name               string                 `yaml:"name" validate:"required"`
	DisplayName        string                 `yaml:"displayName" validate:"required"`
	Description        string                 `yaml:"description" validate:"required"`
	Platform           types.Platform         `yaml:"platform" validate:"required,oneof=aws ibmcloud gcp azure"`
	ClusterType        types.ClusterType      `yaml:"clusterType,omitempty"`
	Track              string                 `yaml:"track,omitempty" validate:"omitempty,oneof=ga prerelease kube"` // ga, prerelease, or kube
	Enabled            bool                   `yaml:"enabled"`
	CredentialsMode    string                 `yaml:"credentialsMode,omitempty"`                                // Default credentials mode (Mint, Passthrough, Manual, Static)
	OpenshiftVersions  *VersionConfig         `yaml:"openshiftVersions,omitempty"`
	KubernetesVersions *VersionConfig         `yaml:"kubernetesVersions,omitempty"`
	Regions            RegionConfig           `yaml:"regions" validate:"required"`
	Zones              *ZoneConfig            `yaml:"zones,omitempty"`
	BaseDomains        *BaseDomainConfig      `yaml:"baseDomains,omitempty"`
	Compute            ComputeConfig          `yaml:"compute" validate:"required"`
	Lifecycle          LifecycleConfig        `yaml:"lifecycle" validate:"required"`
	Networking         *NetworkingConfig      `yaml:"networking,omitempty"`
	Tags               TagsConfig             `yaml:"tags" validate:"required"`
	Features           FeaturesConfig         `yaml:"features"`
	CostControls       CostControlsConfig     `yaml:"costControls"`
	PlatformConfig     PlatformConfig         `yaml:"platformConfig"`
	PostDeployment     *PostDeploymentConfig  `yaml:"postDeployment,omitempty"`
	Metadata           *MetadataConfig        `yaml:"metadata,omitempty"`
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

// ZoneConfig defines zone constraints (for IKS)
type ZoneConfig struct {
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
	ControlPlane       *ControlPlaneConfig `yaml:"controlPlane,omitempty" json:"control_plane,omitempty"`
	Workers            *WorkersConfig      `yaml:"workers,omitempty" json:"workers,omitempty"`
	NodeGroups         []NodeGroupConfig   `yaml:"nodeGroups,omitempty" json:"node_groups,omitempty"`                 // For EKS (unmanaged)
	ManagedNodeGroups  []NodeGroupConfig   `yaml:"managedNodeGroups,omitempty" json:"managed_node_groups,omitempty"` // For EKS (managed)
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
	InstanceType string `yaml:"instanceType,omitempty" json:"instance_type,omitempty"`
	Autoscaling  bool   `yaml:"autoscaling" json:"autoscaling"`
	// IKS-specific fields
	MachineType string `yaml:"machineType,omitempty" json:"machine_type,omitempty"`
	Count       int    `yaml:"count,omitempty" json:"count,omitempty"`
	PublicVLAN  string `yaml:"publicVLAN,omitempty" json:"public_vlan,omitempty"`
	PrivateVLAN string `yaml:"privateVLAN,omitempty" json:"private_vlan,omitempty"`
}

// NodeGroupConfig defines EKS node group configuration
type NodeGroupConfig struct {
	Name            string `yaml:"name" json:"name" validate:"required"`
	InstanceType    string `yaml:"instanceType" json:"instance_type" validate:"required"`
	DesiredCapacity int    `yaml:"desiredCapacity" json:"desired_capacity" validate:"required,min=1"`
	MinSize         int    `yaml:"minSize" json:"min_size" validate:"required,min=0"`
	MaxSize         int    `yaml:"maxSize" json:"max_size" validate:"required,gtefield=DesiredCapacity"`
	VolumeSize      int    `yaml:"volumeSize,omitempty" json:"volume_size,omitempty"`
	VolumeType      string `yaml:"volumeType,omitempty" json:"volume_type,omitempty"`
	AMIFamily       string `yaml:"amiFamily,omitempty" json:"ami_family,omitempty"` // For managed node groups (AmazonLinux2023, AmazonLinux2, etc.)
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
	NetworkType     string                 `yaml:"networkType,omitempty"`
	ClusterNetworks []ClusterNetworkConfig `yaml:"clusterNetworks,omitempty"`
	ServiceNetwork  []string               `yaml:"serviceNetwork,omitempty"`
	MachineNetwork  []MachineNetworkConfig `yaml:"machineNetwork,omitempty"`
	// EKS-specific fields
	VpcCIDR    string `yaml:"vpcCIDR,omitempty" json:"vpc_cidr,omitempty"`
	NatGateway string `yaml:"natGateway,omitempty" json:"nat_gateway,omitempty"` // "Single" or "HighlyAvailable"
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
	// EKS-specific
	OidcProvider bool `yaml:"oidcProvider,omitempty" json:"oidc_provider,omitempty"`
	// IKS-specific
	PublicServiceEndpoint  bool `yaml:"publicServiceEndpoint,omitempty" json:"public_service_endpoint,omitempty"`
	PrivateServiceEndpoint bool `yaml:"privateServiceEndpoint,omitempty" json:"private_service_endpoint,omitempty"`
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
	EKS      *EKSConfig      `yaml:"eks,omitempty"`
	GCP      *GCPConfig      `yaml:"gcp,omitempty"`
	GKE      *GKEConfig      `yaml:"gke,omitempty"`
	ROSA     *ROSAConfig     `yaml:"rosa,omitempty"`
	Azure    *AzureConfig    `yaml:"azure,omitempty"`
	ARO      *AROConfig      `yaml:"aro,omitempty"`
	AKS      *AKSConfig      `yaml:"aks,omitempty"`
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

// ROSAConfig contains ROSA-specific settings
type ROSAConfig struct {
	STSEnabled   bool   `yaml:"stsEnabled" json:"sts_enabled"`               // STS authentication (required for ROSA)
	ComputeNodes int    `yaml:"computeNodes" json:"compute_nodes"`           // Number of compute nodes
	MachineType  string `yaml:"machineType" json:"machine_type"`             // Compute instance type
	MultiAZ      bool   `yaml:"multiAZ,omitempty" json:"multi_az,omitempty"` // Multi-AZ deployment
}

// IBMCloudConfig contains IBM Cloud-specific settings
type IBMCloudConfig struct {
	ResourceGroup         string `yaml:"resourceGroup"`
	VPCName               string `yaml:"vpcName"`
	ClassicInfrastructure bool   `yaml:"classicInfrastructure,omitempty"`
	DataCenter            string `yaml:"dataCenter,omitempty"`
}

// EKSConfig contains EKS-specific settings
type EKSConfig struct {
	EnabledClusterLogTypes []string `yaml:"enabledClusterLogTypes,omitempty" json:"enabled_cluster_log_types,omitempty"`
	PublicAccess           bool     `yaml:"publicAccess,omitempty" json:"public_access,omitempty"`
	PrivateAccess          bool     `yaml:"privateAccess,omitempty" json:"private_access,omitempty"`
}

// GCPConfig contains GCP-specific settings for OpenShift on GCP
type GCPConfig struct {
	Project              string              `yaml:"project" json:"project"`
	Network              string              `yaml:"network,omitempty" json:"network,omitempty"`
	Subnetwork           string              `yaml:"subnetwork,omitempty" json:"subnetwork,omitempty"`
	ControlPlane         *GCPMachineConfig   `yaml:"controlPlane,omitempty" json:"control_plane,omitempty"`
	Compute              *GCPMachineConfig   `yaml:"compute,omitempty" json:"compute,omitempty"`
	ServiceAccount       string              `yaml:"serviceAccount,omitempty" json:"service_account,omitempty"`
	SecureBootPolicy     string              `yaml:"secureBootPolicy,omitempty" json:"secure_boot_policy,omitempty"`
}

// GCPMachineConfig defines GCP machine configuration
type GCPMachineConfig struct {
	MachineType          string  `yaml:"machineType" json:"machine_type"`
	DiskSizeGB           int     `yaml:"diskSizeGB" json:"disk_size_gb"`
	DiskType             string  `yaml:"diskType,omitempty" json:"disk_type,omitempty"`
}

// GKEConfig contains GKE-specific settings
type GKEConfig struct {
	EnabledClusterLogTypes []string          `yaml:"enabledClusterLogTypes,omitempty" json:"enabled_cluster_log_types,omitempty"`
	PublicAccess           bool              `yaml:"publicAccess,omitempty" json:"public_access,omitempty"`
	PrivateAccess          bool              `yaml:"privateAccess,omitempty" json:"private_access,omitempty"`
	EnableWorkloadIdentity bool              `yaml:"enableWorkloadIdentity,omitempty" json:"enable_workload_identity,omitempty"`
	ReleaseChannel         string            `yaml:"releaseChannel,omitempty" json:"release_channel,omitempty"` // "rapid", "regular", "stable"
	NodePools              []GKENodePoolConfig `yaml:"nodePools,omitempty" json:"node_pools,omitempty"`
}

// GKENodePoolConfig defines a GKE node pool configuration
type GKENodePoolConfig struct {
	Name           string `yaml:"name" json:"name"`
	MachineType    string `yaml:"machineType" json:"machine_type"`
	DiskSizeGB     int    `yaml:"diskSizeGB" json:"disk_size_gb"`
	DiskType       string `yaml:"diskType,omitempty" json:"disk_type,omitempty"`
	NodeCount      int    `yaml:"nodeCount" json:"node_count"`
	MinNodeCount   int    `yaml:"minNodeCount,omitempty" json:"min_node_count,omitempty"`
	MaxNodeCount   int    `yaml:"maxNodeCount,omitempty" json:"max_node_count,omitempty"`
	EnableAutoScale bool  `yaml:"enableAutoScale,omitempty" json:"enable_auto_scale,omitempty"`
}

// AzureConfig contains Azure-specific settings for self-managed OpenShift
type AzureConfig struct {
	SubscriptionID            string              `yaml:"subscriptionId" json:"subscription_id"`
	BaseDomainResourceGroup   string              `yaml:"baseDomainResourceGroup" json:"base_domain_resource_group"`
	ResourceGroupPrefix       string              `yaml:"resourceGroupPrefix,omitempty" json:"resource_group_prefix,omitempty"`
	Network                   string              `yaml:"network,omitempty" json:"network,omitempty"`
	Subnetwork                string              `yaml:"subnetwork,omitempty" json:"subnetwork,omitempty"`
	ControlPlane              *AzureMachineConfig `yaml:"controlPlane,omitempty" json:"control_plane,omitempty"`
	Compute                   *AzureMachineConfig `yaml:"compute,omitempty" json:"compute,omitempty"`
}

// AzureMachineConfig defines Azure VM configuration
type AzureMachineConfig struct {
	VMSize       string `yaml:"vmSize" json:"vm_size"`                                      // e.g., "Standard_D8s_v3"
	OSDiskSizeGB int    `yaml:"osDiskSizeGB" json:"os_disk_size_gb"`
	OSDiskType   string `yaml:"osDiskType,omitempty" json:"os_disk_type,omitempty"` // StandardSSD_LRS, Premium_LRS
}

// AROConfig contains ARO-specific settings
type AROConfig struct {
	MasterVMSize     string `yaml:"masterVMSize" json:"master_vm_size"`              // e.g., "Standard_D8s_v3"
	WorkerVMSize     string `yaml:"workerVMSize" json:"worker_vm_size"`              // e.g., "Standard_D4s_v3"
	WorkerCount      int    `yaml:"workerCount" json:"worker_count"`
	OpenShiftVersion string `yaml:"openshiftVersion,omitempty" json:"openshift_version,omitempty"`
	PullSecretPath   string `yaml:"pullSecretPath,omitempty" json:"pull_secret_path,omitempty"`
}

// AKSConfig contains AKS-specific settings
type AKSConfig struct {
	KubernetesVersion string              `yaml:"kubernetesVersion" json:"kubernetes_version"`
	NetworkPlugin     string              `yaml:"networkPlugin,omitempty" json:"network_plugin,omitempty"` // azure, kubenet
	NetworkPolicy     string              `yaml:"networkPolicy,omitempty" json:"network_policy,omitempty"` // azure, calico
	NodePools         []AKSNodePoolConfig `yaml:"nodePools,omitempty" json:"node_pools,omitempty"`
}

// AKSNodePoolConfig defines an AKS node pool configuration
type AKSNodePoolConfig struct {
	Name            string `yaml:"name" json:"name"`
	VMSize          string `yaml:"vmSize" json:"vm_size"`                                    // e.g., "Standard_D4s_v3"
	Count           int    `yaml:"count" json:"count"`
	MinCount        int    `yaml:"minCount,omitempty" json:"min_count,omitempty"`
	MaxCount        int    `yaml:"maxCount,omitempty" json:"max_count,omitempty"`
	EnableAutoScale bool   `yaml:"enableAutoScale,omitempty" json:"enable_auto_scale,omitempty"`
	OSDiskSizeGB    int    `yaml:"osDiskSizeGB,omitempty" json:"os_disk_size_gb,omitempty"`
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
	Source         string                 `yaml:"source,omitempty" json:"source,omitempty"` // e.g. "redhat-operators" (optional - OLM will search all catalogs if omitted)
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
	Path        string `yaml:"path,omitempty" json:"path,omitempty"`           // Local file path
	URL         string `yaml:"url,omitempty" json:"url,omitempty"`             // Remote URL (e.g. GitHub raw URL)
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Namespace   string `yaml:"namespace,omitempty" json:"namespace,omitempty"` // Target namespace for the manifest
}

// HelmChartConfig defines a Helm chart to install post-deployment
type HelmChartConfig struct {
	Name   string                 `yaml:"name" json:"name" validate:"required"`
	Repo   string                 `yaml:"repo" json:"repo" validate:"required"`
	Chart  string                 `yaml:"chart" json:"chart" validate:"required"`
	Values map[string]interface{} `yaml:"values,omitempty" json:"values,omitempty"`
}

// MetadataConfig contains profile metadata for documentation
type MetadataConfig struct {
	Capabilities []string          `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Capacity     map[string]interface{} `yaml:"capacity,omitempty" json:"capacity,omitempty"`
	Notes        []string          `yaml:"notes,omitempty" json:"notes,omitempty"`
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
