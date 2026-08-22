package api

import (
	"testing"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestBuildLifecycleStats(t *testing.T) {
	stats := &types.JobStats{Counts: map[string]map[string]int{
		string(types.JobTypeCreate):         {"SUCCEEDED": 8, "FAILED": 2},
		string(types.JobTypeDestroy):        {"SUCCEEDED": 3},
		string(types.JobTypeJanitorDestroy): {"SUCCEEDED": 2},
		string(types.JobTypeHibernate):      {"SUCCEEDED": 4},
	}}

	clusters := []*types.Cluster{
		{Platform: types.PlatformAWS, ClusterType: types.ClusterTypeOpenShift, Status: types.ClusterStatusReady},
		{Platform: types.PlatformAWS, ClusterType: types.ClusterTypeEKS, Status: types.ClusterStatusDestroyed},
		{Platform: types.PlatformGCP, ClusterType: types.ClusterTypeGKE, Status: types.ClusterStatusReady},
	}

	ls := buildLifecycleStats(stats, clusters)

	if ls.Created != 8 {
		t.Errorf("Created = %d, want 8", ls.Created)
	}
	if ls.Destroyed != 5 { // 3 destroy + 2 janitor destroy
		t.Errorf("Destroyed = %d, want 5", ls.Destroyed)
	}
	if ls.Hibernated != 4 {
		t.Errorf("Hibernated = %d, want 4", ls.Hibernated)
	}
	if ls.CreateSuccess != 8 || ls.CreateFailure != 2 {
		t.Errorf("CreateSuccess/Failure = %d/%d, want 8/2", ls.CreateSuccess, ls.CreateFailure)
	}
	if ls.CreateSuccessRate != 0.8 {
		t.Errorf("CreateSuccessRate = %v, want 0.8", ls.CreateSuccessRate)
	}
	if ls.ByPlatform["aws"] != 2 || ls.ByPlatform["gcp"] != 1 {
		t.Errorf("ByPlatform = %v", ls.ByPlatform)
	}
	if ls.ByClusterType["openshift"] != 1 || ls.ByClusterType["eks"] != 1 || ls.ByClusterType["gke"] != 1 {
		t.Errorf("ByClusterType = %v", ls.ByClusterType)
	}
	if ls.ByStatus["READY"] != 2 || ls.ByStatus["DESTROYED"] != 1 {
		t.Errorf("ByStatus = %v", ls.ByStatus)
	}
}

func TestBuildLifecycleStats_NoCreateJobs(t *testing.T) {
	stats := &types.JobStats{Counts: map[string]map[string]int{}}
	ls := buildLifecycleStats(stats, nil)
	if ls.CreateSuccessRate != 0 {
		t.Errorf("CreateSuccessRate = %v, want 0 (no completed create jobs)", ls.CreateSuccessRate)
	}
}
