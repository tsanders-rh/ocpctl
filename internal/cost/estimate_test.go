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
		{"hibernated gke fixed mgmt fee", types.ClusterStatusHibernated, types.ClusterTypeGKE, 1.0, 0.10},
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

func TestPeriodCostWithHistory(t *testing.T) {
	winStart := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC)
	winEnd := time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC)
	// OpenShift hibernates to 10% of the running rate: running $1/hr, hibernated $0.10/hr.
	p := prof(1.0)

	const (
		runRate = 1.0
		hibRate = 0.10
	)

	t.Run("no history falls back to flat running rate", func(t *testing.T) {
		created := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
		destroyed := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC) // 48h
		cl := &types.Cluster{
			Status:      types.ClusterStatusDestroyed,
			ClusterType: types.ClusterTypeOpenShift,
			CreatedAt:   created,
			DestroyedAt: &destroyed,
		}
		cost, hours := PeriodCostWithHistory(cl, p, nil, winStart, winEnd)
		if hours != 48 || cost != 48*runRate {
			t.Fatalf("cost/hours = %v/%v, want %v/48", cost, hours, 48*runRate)
		}
	})

	t.Run("no history for hibernated cluster falls back to flat reduced rate", func(t *testing.T) {
		created := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC) // 8 days to winEnd = 192h
		cl := &types.Cluster{
			Status:      types.ClusterStatusHibernated,
			ClusterType: types.ClusterTypeOpenShift,
			CreatedAt:   created,
		}
		cost, hours := PeriodCostWithHistory(cl, p, nil, winStart, winEnd)
		wantHours := winEnd.Sub(created).Hours()
		if hours != wantHours || cost != wantHours*hibRate {
			t.Fatalf("cost/hours = %v/%v, want %v/%v", cost, hours, wantHours*hibRate, wantHours)
		}
	})

	// Hibernation mid-window: ran 24h, hibernated the next 24h, then destroyed.
	t.Run("hibernated mid-window bills each interval at its own rate", func(t *testing.T) {
		created := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
		hibAt := time.Date(2026, 1, 13, 0, 0, 0, 0, time.UTC)     // +24h running
		destroyed := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC) // +24h hibernated
		cl := &types.Cluster{
			Status:      types.ClusterStatusDestroyed,
			ClusterType: types.ClusterTypeOpenShift,
			CreatedAt:   created,
			DestroyedAt: &destroyed,
		}
		trans := []StateTransition{{At: hibAt, ToHibernated: true}}
		cost, hours := PeriodCostWithHistory(cl, p, trans, winStart, winEnd)
		wantCost := 24*runRate + 24*hibRate
		if hours != 48 {
			t.Fatalf("hours = %v, want 48", hours)
		}
		if cost != wantCost {
			t.Fatalf("cost = %v, want %v", cost, wantCost)
		}
	})

	// Currently-hibernated cluster with prior running time: transition before
	// window end, so tail of the window bills at the reduced rate.
	t.Run("currently hibernated with prior running time", func(t *testing.T) {
		created := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
		hibAt := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC) // 96h running, then hibernated to winEnd (120h)
		cl := &types.Cluster{
			Status:      types.ClusterStatusHibernated,
			ClusterType: types.ClusterTypeOpenShift,
			CreatedAt:   created,
		}
		trans := []StateTransition{{At: hibAt, ToHibernated: true}}
		cost, hours := PeriodCostWithHistory(cl, p, trans, winStart, winEnd)
		runH := hibAt.Sub(created).Hours() // 96
		hibH := winEnd.Sub(hibAt).Hours()  // 120
		wantCost := runH*runRate + hibH*hibRate
		if hours != runH+hibH {
			t.Fatalf("hours = %v, want %v", hours, runH+hibH)
		}
		if cost != wantCost {
			t.Fatalf("cost = %v, want %v", cost, wantCost)
		}
	})

	// Destroyed hibernated cluster: hibernated before the window opened (prior
	// transition sets the initial state), stayed hibernated until destroyed.
	t.Run("destroyed hibernated cluster bills entire window at reduced rate", func(t *testing.T) {
		created := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
		hibAt := time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC)      // before window
		destroyed := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC) // 96h into window
		cl := &types.Cluster{
			Status:      types.ClusterStatusDestroyed,
			ClusterType: types.ClusterTypeOpenShift,
			CreatedAt:   created,
			DestroyedAt: &destroyed,
		}
		trans := []StateTransition{{At: hibAt, ToHibernated: true}}
		cost, hours := PeriodCostWithHistory(cl, p, trans, winStart, winEnd)
		wantHours := destroyed.Sub(winStart).Hours() // 96
		if hours != wantHours {
			t.Fatalf("hours = %v, want %v", hours, wantHours)
		}
		if cost != wantHours*hibRate {
			t.Fatalf("cost = %v, want %v", cost, wantHours*hibRate)
		}
	})

	// Resume after hibernation: hibernate then resume, both within the window.
	t.Run("hibernate then resume within window", func(t *testing.T) {
		created := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
		hibAt := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)     // +24h running
		resumeAt := time.Date(2026, 1, 13, 0, 0, 0, 0, time.UTC)  // +24h hibernated
		destroyed := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC) // +24h running
		cl := &types.Cluster{
			Status:      types.ClusterStatusDestroyed,
			ClusterType: types.ClusterTypeOpenShift,
			CreatedAt:   created,
			DestroyedAt: &destroyed,
		}
		trans := []StateTransition{
			{At: hibAt, ToHibernated: true},
			{At: resumeAt, ToHibernated: false},
		}
		cost, hours := PeriodCostWithHistory(cl, p, trans, winStart, winEnd)
		wantCost := 24*runRate + 24*hibRate + 24*runRate
		if hours != 72 {
			t.Fatalf("hours = %v, want 72", hours)
		}
		if cost != wantCost {
			t.Fatalf("cost = %v, want %v", cost, wantCost)
		}
	})

	// Cluster not active in the window contributes nothing.
	t.Run("outside window returns zero", func(t *testing.T) {
		created := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
		cl := &types.Cluster{
			Status:      types.ClusterStatusReady,
			ClusterType: types.ClusterTypeOpenShift,
			CreatedAt:   created,
		}
		cost, hours := PeriodCostWithHistory(cl, p, nil, winStart, winEnd)
		if cost != 0 || hours != 0 {
			t.Fatalf("cost/hours = %v/%v, want 0/0", cost, hours)
		}
	})
}
