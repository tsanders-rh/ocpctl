package profile_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

func TestGCPProfiles_Load(t *testing.T) {
	loader := profile.NewLoader("definitions")

	t.Run("loads gcp-gke-standard profile", func(t *testing.T) {
		prof, err := loader.Load("gcp-gke-standard")
		require.NoError(t, err)
		require.NotNil(t, prof)

		assert.Equal(t, "gcp-gke-standard", prof.Name)
		assert.Equal(t, "gcp", string(prof.Platform))
		assert.Equal(t, "gke", string(prof.ClusterType))
		assert.True(t, prof.Enabled)
		assert.Equal(t, "kube", prof.Track)

		// Verify Kubernetes versions
		require.NotNil(t, prof.KubernetesVersions)
		assert.Contains(t, prof.KubernetesVersions.Allowlist, "1.34")
		assert.Equal(t, "1.34", prof.KubernetesVersions.Default)

		// Verify regions
		assert.Contains(t, prof.Regions.Allowlist, "us-central1")
		assert.Equal(t, "us-central1", prof.Regions.Default)

		// Verify compute config
		require.NotNil(t, prof.Compute.Workers)
		assert.Equal(t, 3, prof.Compute.Workers.Replicas)
		assert.Equal(t, 1, prof.Compute.Workers.MinReplicas)
		assert.Equal(t, 10, prof.Compute.Workers.MaxReplicas)
		assert.True(t, prof.Compute.Workers.Autoscaling)

		// Verify cost controls
		require.NotNil(t, prof.CostControls)
		assert.Equal(t, 0.05, prof.CostControls.EstimatedHourlyCost)
		assert.Contains(t, prof.CostControls.WarningMessage, "no control plane cost")

		// Verify lifecycle
		assert.Equal(t, 168, prof.Lifecycle.MaxTTLHours)
		assert.Equal(t, 48, prof.Lifecycle.DefaultTTLHours)

		// Verify features
		assert.True(t, prof.Features.OffHoursScaling)

		// Verify platform config
		require.NotNil(t, prof.PlatformConfig)
		require.NotNil(t, prof.PlatformConfig.GKE)
		assert.True(t, prof.PlatformConfig.GKE.PublicAccess)
		assert.True(t, prof.PlatformConfig.GKE.PrivateAccess)
		assert.True(t, prof.PlatformConfig.GKE.EnableWorkloadIdentity)
		assert.Equal(t, "regular", prof.PlatformConfig.GKE.ReleaseChannel)

		// Verify post-deployment
		require.NotNil(t, prof.PostDeployment)
		assert.True(t, prof.PostDeployment.Enabled)
		assert.NotEmpty(t, prof.PostDeployment.Manifests)
	})

	t.Run("loads gcp-standard profile (OpenShift)", func(t *testing.T) {
		prof, err := loader.Load("gcp-standard")
		require.NoError(t, err)
		require.NotNil(t, prof)

		assert.Equal(t, "gcp-standard", prof.Name)
		assert.Equal(t, "gcp", string(prof.Platform))
		assert.Equal(t, "openshift", string(prof.ClusterType))
		assert.True(t, prof.Enabled)
		assert.Equal(t, "ga", prof.Track)

		// Verify OpenShift versions
		require.NotNil(t, prof.OpenshiftVersions)
		assert.Contains(t, prof.OpenshiftVersions.Allowlist, "4.20.3")
		assert.Equal(t, "4.20.3", prof.OpenshiftVersions.Default)

		// Verify base domains
		require.NotNil(t, prof.BaseDomains)
		assert.NotEmpty(t, prof.BaseDomains.Allowlist)
		assert.NotEmpty(t, prof.BaseDomains.Default)

		// Verify compute config
		require.NotNil(t, prof.Compute.ControlPlane)
		assert.Equal(t, 3, prof.Compute.ControlPlane.Replicas)
		assert.Equal(t, "n2-standard-4", prof.Compute.ControlPlane.InstanceType)
		assert.False(t, prof.Compute.ControlPlane.Schedulable)

		require.NotNil(t, prof.Compute.Workers)
		assert.Equal(t, 3, prof.Compute.Workers.Replicas)
		assert.Equal(t, "n2-standard-8", prof.Compute.Workers.InstanceType)
		assert.False(t, prof.Compute.Workers.Autoscaling)

		// Verify networking
		require.NotNil(t, prof.Networking)
		assert.Equal(t, "OVNKubernetes", prof.Networking.NetworkType)

		// Verify cost controls
		require.NotNil(t, prof.CostControls)
		assert.Equal(t, 1.20, prof.CostControls.EstimatedHourlyCost)
		assert.Contains(t, prof.CostControls.WarningMessage, "GCE VMs")

		// Verify platform config
		require.NotNil(t, prof.PlatformConfig)
		require.NotNil(t, prof.PlatformConfig.GCP)
		assert.NotNil(t, prof.PlatformConfig.GCP.ControlPlane)
		assert.NotNil(t, prof.PlatformConfig.GCP.Compute)
	})
}

func TestGCPProfiles_Validation(t *testing.T) {
	loader := profile.NewLoader("definitions")

	t.Run("gcp-gke-standard validates correctly", func(t *testing.T) {
		prof, err := loader.Load("gcp-gke-standard")
		require.NoError(t, err)

		// Verify required fields
		assert.NotEmpty(t, prof.Name)
		assert.NotEmpty(t, prof.Platform)
		assert.NotEmpty(t, prof.Regions.Allowlist)
		assert.NotEmpty(t, prof.Regions.Default)

		// Verify GKE-specific requirements
		require.NotNil(t, prof.KubernetesVersions)
		assert.NotEmpty(t, prof.KubernetesVersions.Allowlist)
		assert.NotEmpty(t, prof.KubernetesVersions.Default)

		// Verify workers config (GKE uses workers, not control plane)
		require.NotNil(t, prof.Compute.Workers)
		assert.Greater(t, prof.Compute.Workers.Replicas, 0)
		assert.Greater(t, prof.Compute.Workers.MaxReplicas, prof.Compute.Workers.MinReplicas)
	})

	t.Run("gcp-standard validates correctly", func(t *testing.T) {
		prof, err := loader.Load("gcp-standard")
		require.NoError(t, err)

		// Verify required fields
		assert.NotEmpty(t, prof.Name)
		assert.NotEmpty(t, prof.Platform)

		// Verify OpenShift-specific requirements
		require.NotNil(t, prof.OpenshiftVersions)
		assert.NotEmpty(t, prof.OpenshiftVersions.Allowlist)
		assert.NotEmpty(t, prof.OpenshiftVersions.Default)

		require.NotNil(t, prof.BaseDomains)
		assert.NotEmpty(t, prof.BaseDomains.Allowlist)

		// Verify control plane has odd number of replicas
		require.NotNil(t, prof.Compute.ControlPlane)
		assert.Equal(t, 1, prof.Compute.ControlPlane.Replicas%2, "control plane replicas must be odd")

		// Verify networking
		require.NotNil(t, prof.Networking)
		assert.NotEmpty(t, prof.Networking.NetworkType)
	})
}

func TestGCPProfiles_CostEstimates(t *testing.T) {
	loader := profile.NewLoader("definitions")

	t.Run("gcp-gke-standard cost estimates", func(t *testing.T) {
		prof, err := loader.Load("gcp-gke-standard")
		require.NoError(t, err)

		assert.Equal(t, 0.05, prof.CostControls.EstimatedHourlyCost)
		assert.Equal(t, float64(40), prof.CostControls.MaxMonthlyCost)

		// Verify monthly cost calculation
		hoursPerMonth := 24 * 30 // 720 hours
		estimatedMonthlyCost := prof.CostControls.EstimatedHourlyCost * float64(hoursPerMonth)
		assert.InDelta(t, 36.0, estimatedMonthlyCost, 5.0)
	})

	t.Run("gcp-standard cost estimates", func(t *testing.T) {
		prof, err := loader.Load("gcp-standard")
		require.NoError(t, err)

		assert.Equal(t, 1.20, prof.CostControls.EstimatedHourlyCost)
		assert.Equal(t, float64(900), prof.CostControls.MaxMonthlyCost)

		// Verify monthly cost calculation
		hoursPerMonth := 24 * 30 // 720 hours
		estimatedMonthlyCost := prof.CostControls.EstimatedHourlyCost * float64(hoursPerMonth)
		assert.InDelta(t, 864.0, estimatedMonthlyCost, 50.0)
	})
}

func TestGCPProfiles_RegionSupport(t *testing.T) {
	loader := profile.NewLoader("definitions")

	commonRegions := []string{
		"us-central1",
		"us-east1",
		"us-west1",
		"europe-west1",
		"asia-east1",
	}

	t.Run("gcp-gke-standard regions", func(t *testing.T) {
		prof, err := loader.Load("gcp-gke-standard")
		require.NoError(t, err)

		for _, region := range commonRegions {
			assert.Contains(t, prof.Regions.Allowlist, region)
		}
	})

	t.Run("gcp-standard regions", func(t *testing.T) {
		prof, err := loader.Load("gcp-standard")
		require.NoError(t, err)

		for _, region := range commonRegions {
			assert.Contains(t, prof.Regions.Allowlist, region)
		}
	})
}

func TestGCPProfiles_NodePoolConfiguration(t *testing.T) {
	loader := profile.NewLoader("definitions")

	t.Run("gcp-gke-standard node pool", func(t *testing.T) {
		prof, err := loader.Load("gcp-gke-standard")
		require.NoError(t, err)

		require.NotNil(t, prof.PlatformConfig)
		require.NotNil(t, prof.PlatformConfig.GKE)
		require.NotEmpty(t, prof.PlatformConfig.GKE.NodePools)

		nodePool := prof.PlatformConfig.GKE.NodePools[0]
		assert.Equal(t, "default-pool", nodePool.Name)
		assert.Equal(t, "e2-medium", nodePool.MachineType)
		assert.Equal(t, 100, nodePool.DiskSizeGB)
		assert.Equal(t, 3, nodePool.NodeCount)
		assert.Equal(t, 1, nodePool.MinNodeCount)
		assert.Equal(t, 10, nodePool.MaxNodeCount)
		assert.True(t, nodePool.EnableAutoScale)
	})
}

func TestGCPProfiles_LifecycleConfiguration(t *testing.T) {
	loader := profile.NewLoader("definitions")

	tests := []struct {
		name               string
		profileName        string
		expectedMaxTTL     int
		expectedDefaultTTL int
	}{
		{
			name:               "gcp-gke-standard",
			profileName:        "gcp-gke-standard",
			expectedMaxTTL:     168, // 1 week
			expectedDefaultTTL: 48,  // 2 days
		},
		{
			name:               "gcp-standard",
			profileName:        "gcp-standard",
			expectedMaxTTL:     168, // 1 week
			expectedDefaultTTL: 72,  // 3 days
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prof, err := loader.Load(tt.profileName)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedMaxTTL, prof.Lifecycle.MaxTTLHours)
			assert.Equal(t, tt.expectedDefaultTTL, prof.Lifecycle.DefaultTTLHours)
			assert.True(t, prof.Lifecycle.AllowCustomTTL)
		})
	}
}

func TestGCPProfiles_Tags(t *testing.T) {
	loader := profile.NewLoader("definitions")

	expectedTags := map[string]string{
		"ManagedBy": "cluster-control-plane",
	}

	t.Run("gcp-gke-standard tags", func(t *testing.T) {
		prof, err := loader.Load("gcp-gke-standard")
		require.NoError(t, err)

		assert.True(t, prof.Tags.AllowUserTags)
		assert.Contains(t, prof.Tags.Defaults, "ManagedBy")
		assert.Equal(t, expectedTags["ManagedBy"], prof.Tags.Defaults["ManagedBy"])
	})

	t.Run("gcp-standard tags", func(t *testing.T) {
		prof, err := loader.Load("gcp-standard")
		require.NoError(t, err)

		assert.True(t, prof.Tags.AllowUserTags)
		assert.Contains(t, prof.Tags.Defaults, "ManagedBy")
		assert.Equal(t, expectedTags["ManagedBy"], prof.Tags.Defaults["ManagedBy"])
	})
}
