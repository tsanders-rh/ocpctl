package worker

import (
	"testing"

	"github.com/tsanders-rh/ocpctl/internal/profile"
)

func azureTestProfile(fallback []profile.AzureCandidate) *profile.Profile {
	return &profile.Profile{
		Name: "azure-standard",
		Regions: profile.RegionConfig{
			Allowlist: []string{"eastus", "eastus2", "centralus"},
			Default:   "eastus",
		},
		Compute: profile.ComputeConfig{
			ControlPlane: &profile.ControlPlaneConfig{Replicas: 3, InstanceType: "Standard_D8s_v5"},
			Workers:      &profile.WorkersConfig{Replicas: 3, InstanceType: "Standard_D4s_v5"},
		},
		PlatformConfig: profile.PlatformConfig{
			Azure: &profile.AzureConfig{
				CapacityFallback: fallback,
			},
		},
	}
}

func TestBuildAzureCandidates_RequestedRegionMovedFirst(t *testing.T) {
	prof := azureTestProfile([]profile.AzureCandidate{
		{Region: "eastus", Zones: []string{"1", "2"}, ControlPlaneSku: "Standard_D8s_v4", ComputeSku: "Standard_D4s_v4"},
		{Region: "eastus2", Zones: []string{"1", "3"}},
		{Region: "centralus", Zones: []string{"1", "2", "3"}},
	})

	got := buildAzureCandidates(prof, "centralus")
	if len(got) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(got))
	}
	if got[0].Region != "centralus" {
		t.Fatalf("expected requested region centralus first, got %s", got[0].Region)
	}

	// A candidate with no SKUs inherits the profile defaults.
	if got[0].ControlPlaneSku != "Standard_D8s_v5" || got[0].ComputeSku != "Standard_D4s_v5" {
		t.Fatalf("expected profile default SKUs, got cp=%s worker=%s", got[0].ControlPlaneSku, got[0].ComputeSku)
	}

	// An explicit candidate SKU is preserved.
	var eastus *azureCandidate
	for i := range got {
		if got[i].Region == "eastus" {
			eastus = &got[i]
		}
	}
	if eastus == nil || eastus.ControlPlaneSku != "Standard_D8s_v4" {
		t.Fatalf("expected eastus D8s_v4 override preserved, got %+v", eastus)
	}
}

func TestIsAzureCapacityFailure(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"skunotavailable", "RESPONSE 409: SkuNotAvailable: The requested VM size ...", true},
		{"capacity restrictions", "failed for Capacity Restrictions", true},
		{"zonal allocation", "ZonalAllocationFailed: Allocation failed", true},
		{"installer timeout only", "failed to provision control-plane machines within 20m0s", false},
		{"unrelated", "some other install failure", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isAzureCapacityFailure(tc.in); got != tc.want {
				t.Fatalf("isAzureCapacityFailure(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestBuildAzureCandidates_NoMatrixSingleCandidate(t *testing.T) {
	prof := azureTestProfile(nil)
	got := buildAzureCandidates(prof, "eastus2")
	if len(got) != 1 {
		t.Fatalf("expected 1 fallback candidate, got %d", len(got))
	}
	if got[0].Region != "eastus2" {
		t.Fatalf("expected requested region eastus2, got %s", got[0].Region)
	}
	if len(got[0].Zones) != 0 {
		t.Fatalf("expected no zone pinning without a matrix, got %v", got[0].Zones)
	}
	if got[0].ControlPlaneSku != "Standard_D8s_v5" {
		t.Fatalf("expected profile default SKU, got %s", got[0].ControlPlaneSku)
	}
}
