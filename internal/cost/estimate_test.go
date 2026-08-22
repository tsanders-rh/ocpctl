package cost

import (
	"testing"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func prof(hourly float64) *profile.Profile {
	return &profile.Profile{
		CostControls: profile.CostControlsConfig{EstimatedHourlyCost: hourly},
	}
}

func TestEffectiveHourlyCost(t *testing.T) {
	tests := []struct {
		name   string
		status types.ClusterStatus
		ctype  types.ClusterType
		base   float64
		want   float64
	}{
		{"running full cost", types.ClusterStatusReady, types.ClusterTypeOpenShift, 1.0, 1.0},
		{"hibernated openshift 10pct", types.ClusterStatusHibernated, types.ClusterTypeOpenShift, 1.0, 0.10},
		{"hibernated rosa fixed", types.ClusterStatusHibernated, types.ClusterTypeROSA, 1.0, 0.03},
		{"hibernated eks fixed", types.ClusterStatusHibernated, types.ClusterTypeEKS, 1.0, 0.10},
		{"hibernated gke 3pct", types.ClusterStatusHibernated, types.ClusterTypeGKE, 1.0, 0.03},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := &types.Cluster{Status: tc.status, ClusterType: tc.ctype}
			got := EffectiveHourlyCost(cl, prof(tc.base))
			if got != tc.want {
				t.Fatalf("EffectiveHourlyCost = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPeriodCost(t *testing.T) {
	winStart := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)

	t.Run("lifetime fully inside window", func(t *testing.T) {
		created := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
		destroyed := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)
		cl := &types.Cluster{
			Status:      types.ClusterStatusDestroyed,
			CreatedAt:   created,
			DestroyedAt: &destroyed,
		}
		cost, hours := PeriodCost(cl, 2.0, winStart, winEnd)
		if hours != 48 {
			t.Fatalf("hours = %v, want 48", hours)
		}
		if cost != 96 {
			t.Fatalf("cost = %v, want 96", cost)
		}
	})

	t.Run("created before window, still running clamps to window", func(t *testing.T) {
		created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		cl := &types.Cluster{Status: types.ClusterStatusReady, CreatedAt: created}
		_, hours := PeriodCost(cl, 1.0, winStart, winEnd)
		wantHours := winEnd.Sub(winStart).Hours() // full 10 days = 240h
		if hours != wantHours {
			t.Fatalf("hours = %v, want %v", hours, wantHours)
		}
	})

	t.Run("destroyed before window returns zero", func(t *testing.T) {
		created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		destroyed := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
		cl := &types.Cluster{
			Status:      types.ClusterStatusDestroyed,
			CreatedAt:   created,
			DestroyedAt: &destroyed,
		}
		cost, hours := PeriodCost(cl, 1.0, winStart, winEnd)
		if cost != 0 || hours != 0 {
			t.Fatalf("cost/hours = %v/%v, want 0/0", cost, hours)
		}
	})

	t.Run("created after window returns zero", func(t *testing.T) {
		created := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
		cl := &types.Cluster{Status: types.ClusterStatusReady, CreatedAt: created}
		cost, hours := PeriodCost(cl, 1.0, winStart, winEnd)
		if cost != 0 || hours != 0 {
			t.Fatalf("cost/hours = %v/%v, want 0/0", cost, hours)
		}
	})

	// A DESTROYED cluster with no destroyed_at must not be projected as
	// "still running" through the window end (the legacy zombie-row bug that
	// inflated usage reports).
	t.Run("destroyed without timestamp returns zero", func(t *testing.T) {
		created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) // created before window
		cl := &types.Cluster{
			Status:      types.ClusterStatusDestroyed,
			CreatedAt:   created,
			DestroyedAt: nil,
		}
		cost, hours := PeriodCost(cl, 1.0, winStart, winEnd)
		if cost != 0 || hours != 0 {
			t.Fatalf("cost/hours = %v/%v, want 0/0", cost, hours)
		}
	})
}
