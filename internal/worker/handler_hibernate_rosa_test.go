package worker

import (
	"testing"

	"github.com/tsanders-rh/ocpctl/internal/installer"
)

func TestRosaMinReplicasForPool(t *testing.T) {
	single := installer.ROSAMachinePool{AvailabilityZones: []string{"us-east-1a"}}
	if got := rosaMinReplicasForPool(single); got != 2 {
		t.Errorf("single-zone min = %d, want 2", got)
	}
	multi := installer.ROSAMachinePool{AvailabilityZones: []string{"us-east-1a", "us-east-1b", "us-east-1c"}}
	if got := rosaMinReplicasForPool(multi); got != 3 {
		t.Errorf("multi-zone min = %d, want 3", got)
	}
	// No AZ info reported -> treat as single-zone minimum.
	if got := rosaMinReplicasForPool(installer.ROSAMachinePool{}); got != 2 {
		t.Errorf("no-AZ min = %d, want 2", got)
	}
}

func TestIsWorkloadCapablePool(t *testing.T) {
	if !isWorkloadCapablePool(installer.ROSAMachinePool{ID: "worker"}) {
		t.Error("untainted pool should be workload-capable")
	}
	tainted := installer.ROSAMachinePool{
		ID:     "portworx",
		Taints: []installer.ROSATaint{{Key: "dedicated", Value: "portworx", Effect: "NoSchedule"}},
	}
	if isWorkloadCapablePool(tainted) {
		t.Error("tainted pool should not be workload-capable")
	}
}

func TestSelectROSAPoolToKeep(t *testing.T) {
	taint := []installer.ROSATaint{{Key: "dedicated", Value: "x", Effect: "NoSchedule"}}

	t.Run("prefers the default worker pool", func(t *testing.T) {
		pools := []installer.ROSAMachinePool{
			{ID: "portworx", Taints: taint},
			{ID: "extra"},
			{ID: "worker"},
		}
		if got := selectROSAPoolToKeep(pools); got != 2 {
			t.Errorf("keep index = %d, want 2 (worker)", got)
		}
	})

	t.Run("falls back to first untainted pool when no worker/default", func(t *testing.T) {
		pools := []installer.ROSAMachinePool{
			{ID: "portworx", Taints: taint},
			{ID: "compute-a"},
			{ID: "compute-b"},
		}
		if got := selectROSAPoolToKeep(pools); got != 1 {
			t.Errorf("keep index = %d, want 1 (first untainted)", got)
		}
	})

	t.Run("falls back to index 0 when every pool is tainted", func(t *testing.T) {
		pools := []installer.ROSAMachinePool{
			{ID: "a", Taints: taint},
			{ID: "b", Taints: taint},
		}
		if got := selectROSAPoolToKeep(pools); got != 0 {
			t.Errorf("keep index = %d, want 0 (fallback)", got)
		}
	})
}
