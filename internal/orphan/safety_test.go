package orphan

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

type mockLookup struct {
	byID   map[string]types.ClusterStatus
	byName map[string]types.ClusterStatus
	err    error
}

func (m mockLookup) ClusterStatusByID(_ context.Context, id string) (types.ClusterStatus, bool, error) {
	if m.err != nil {
		return "", false, m.err
	}
	s, ok := m.byID[id]
	return s, ok, nil
}

func (m mockLookup) MostRecentClusterStatusByName(_ context.Context, name string) (types.ClusterStatus, bool, error) {
	if m.err != nil {
		return "", false, m.err
	}
	s, ok := m.byName[name]
	return s, ok, nil
}

type mockInspector struct {
	counts VPCLiveCounts
	err    error
}

func (m mockInspector) InspectVPC(_ context.Context, _, _ string) (VPCLiveCounts, error) {
	return m.counts, m.err
}

func strptr(s string) *string { return &s }

func TestIsLiveStatus(t *testing.T) {
	live := []types.ClusterStatus{
		types.ClusterStatusPending, types.ClusterStatusCreating, types.ClusterStatusReady,
		types.ClusterStatusHibernating, types.ClusterStatusHibernated, types.ClusterStatusResuming,
		types.ClusterStatusDestroying, types.ClusterStatusDestroyVerifying,
	}
	for _, s := range live {
		if !IsLiveStatus(s) {
			t.Errorf("status %s should be live", s)
		}
	}
	notLive := []types.ClusterStatus{
		types.ClusterStatusDestroyed, types.ClusterStatusDestroyFailed, types.ClusterStatusFailed,
	}
	for _, s := range notLive {
		if IsLiveStatus(s) {
			t.Errorf("status %s should not be live", s)
		}
	}
	// Unknown status defaults to live (fail-safe).
	if !IsLiveStatus(types.ClusterStatus("SOME_FUTURE_STATUS")) {
		t.Error("unknown status should be treated as live")
	}
}

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	cfg := Config{MinAge: 24 * time.Hour, MinDetections: 2, CheckVPCEmpty: true}

	// A resource that passes every check by default; individual cases override.
	base := func() *types.OrphanedResource {
		return &types.OrphanedResource{
			ID:              "orph-1",
			ResourceType:    types.OrphanedResourceTypeEBSVolume,
			ResourceID:      "vol-abc",
			Region:          "us-east-1",
			ClusterName:     "tsanders-test",
			ClusterID:       strptr("cl-1"),
			Status:          types.OrphanedResourceStatusActive,
			FirstDetectedAt: now.Add(-48 * time.Hour),
			DetectionCount:  5,
		}
	}

	tests := []struct {
		name       string
		mutate     func(*types.OrphanedResource)
		lookup     ClusterLookup
		inspector  VPCInspector
		wantSafe   bool
		wantReason string // substring expected in a block reason (when !wantSafe)
	}{
		{
			name:     "all clear (destroyed source)",
			lookup:   mockLookup{byID: map[string]types.ClusterStatus{"cl-1": types.ClusterStatusDestroyed}},
			wantSafe: true,
		},
		{
			name:       "live source cluster blocks",
			lookup:     mockLookup{byID: map[string]types.ClusterStatus{"cl-1": types.ClusterStatusReady}},
			wantSafe:   false,
			wantReason: "source cluster is live",
		},
		{
			name: "no cluster_id, live by name blocks",
			mutate: func(r *types.OrphanedResource) {
				r.ClusterID = nil
			},
			lookup:     mockLookup{byName: map[string]types.ClusterStatus{"tsanders-test": types.ClusterStatusCreating}},
			wantSafe:   false,
			wantReason: "source cluster is live",
		},
		{
			name: "no source found at all is allowed",
			mutate: func(r *types.OrphanedResource) {
				r.ClusterID = nil
			},
			lookup:   mockLookup{},
			wantSafe: true,
		},
		{
			name: "within grace period blocks",
			mutate: func(r *types.OrphanedResource) {
				r.FirstDetectedAt = now.Add(-1 * time.Hour)
			},
			lookup:     mockLookup{byID: map[string]types.ClusterStatus{"cl-1": types.ClusterStatusDestroyed}},
			wantSafe:   false,
			wantReason: "within grace period",
		},
		{
			name: "too few detections blocks",
			mutate: func(r *types.OrphanedResource) {
				r.DetectionCount = 1
			},
			lookup:     mockLookup{byID: map[string]types.ClusterStatus{"cl-1": types.ClusterStatusDestroyed}},
			wantSafe:   false,
			wantReason: "detected only 1 time",
		},
		{
			name: "non-active status blocks",
			mutate: func(r *types.OrphanedResource) {
				r.Status = types.OrphanedResourceStatusIgnored
			},
			lookup:     mockLookup{byID: map[string]types.ClusterStatus{"cl-1": types.ClusterStatusDestroyed}},
			wantSafe:   false,
			wantReason: "not ACTIVE",
		},
		{
			name: "VPC with live instances blocks",
			mutate: func(r *types.OrphanedResource) {
				r.ResourceType = types.OrphanedResourceTypeVPC
				r.ResourceID = "vpc-xyz"
			},
			lookup:     mockLookup{byID: map[string]types.ClusterStatus{"cl-1": types.ClusterStatusDestroyed}},
			inspector:  mockInspector{counts: VPCLiveCounts{RunningInstances: 3}},
			wantSafe:   false,
			wantReason: "VPC still hosts live resources",
		},
		{
			name: "empty VPC is allowed",
			mutate: func(r *types.OrphanedResource) {
				r.ResourceType = types.OrphanedResourceTypeVPC
				r.ResourceID = "vpc-xyz"
			},
			lookup:    mockLookup{byID: map[string]types.ClusterStatus{"cl-1": types.ClusterStatusDestroyed}},
			inspector: mockInspector{counts: VPCLiveCounts{}},
			wantSafe:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := base()
			if tt.mutate != nil {
				tt.mutate(res)
			}
			v := Evaluate(context.Background(), res, cfg, now, tt.lookup, tt.inspector)
			if v.Safe != tt.wantSafe {
				t.Fatalf("Safe = %v, want %v (reasons: %v)", v.Safe, tt.wantSafe, v.BlockReasons)
			}
			if !tt.wantSafe {
				found := false
				for _, r := range v.BlockReasons {
					if strings.Contains(r, tt.wantReason) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected a block reason containing %q, got %v", tt.wantReason, v.BlockReasons)
				}
			}
		})
	}
}

func TestEvaluateLookupErrorWarns(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	cfg := DefaultConfig()
	res := &types.OrphanedResource{
		ResourceType:    types.OrphanedResourceTypeEBSVolume,
		ClusterID:       strptr("cl-err"),
		Status:          types.OrphanedResourceStatusActive,
		FirstDetectedAt: now.Add(-72 * time.Hour),
		DetectionCount:  9,
	}
	v := Evaluate(context.Background(), res, cfg, now, mockLookup{err: errors.New("db down")}, nil)
	// A lookup error must not block deletion, but should warn.
	if !v.Safe {
		t.Errorf("lookup error should not block, got reasons %v", v.BlockReasons)
	}
	if len(v.Warnings) == 0 {
		t.Error("expected a warning about the failed lookup")
	}
}
