package cost

import (
	"sort"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// RunningHourlyCost returns the full (running) hourly rate for a cluster's
// profile — the rate that applies whenever the cluster is not hibernated.
func RunningHourlyCost(prof *profile.Profile) float64 {
	return prof.CostControls.EstimatedHourlyCost
}

// HibernatedHourlyCost returns the reduced hourly rate that applies while a
// cluster of the given type is hibernated. Hibernated clusters cost
// significantly less than running clusters, and the reduction depends on the
// cluster type.
func HibernatedHourlyCost(clusterType types.ClusterType, prof *profile.Profile) float64 {
	baseCost := prof.CostControls.EstimatedHourlyCost

	switch clusterType {
	case types.ClusterTypeOpenShift:
		// OpenShift: All instances stopped, only persistent storage remains (~10%)
		return baseCost * 0.10
	case types.ClusterTypeROSA:
		// ROSA: Machine pools scaled to 0, but control plane runs at fixed $0.03/hr
		return 0.03
	case types.ClusterTypeEKS:
		// EKS: Node groups scaled to 0, control plane at $0.10/hr
		return 0.10
	case types.ClusterTypeIKS:
		// IKS: Workers scaled to 0, minimal cost (~5%)
		return baseCost * 0.05
	case types.ClusterTypeGKE:
		// GKE Standard: node pools scaled to 0, but the $0.10/hr cluster
		// management fee continues (plus a little for persistent disks).
		return 0.10
	default:
		// Unknown cluster type, use conservative estimate
		return baseCost * 0.10
	}
}

// EffectiveHourlyCost returns the effective hourly cost for a cluster based on
// its current state. Hibernated clusters cost significantly less than running
// clusters, and the reduction depends on the cluster type.
//
// This is the shared implementation used by both the per-team costs endpoint and
// the platform-wide usage report so cost figures stay consistent across surfaces.
//
// It uses a single instantaneous status snapshot, so it cannot account for state
// changes during a reporting window. Prefer PeriodCostWithHistory when the
// cluster's hibernate/resume history is available.
func EffectiveHourlyCost(cluster *types.Cluster, prof *profile.Profile) float64 {
	if cluster.Status == types.ClusterStatusHibernated {
		return HibernatedHourlyCost(cluster.ClusterType, prof)
	}
	// For all other states (READY, CREATING, etc.), use full cost
	return RunningHourlyCost(prof)
}

// activeWindow intersects a cluster's lifetime (CreatedAt .. DestroyedAt/window
// end) with [periodStart, periodEnd], returning the billable [start, end] and
// whether any overlap exists.
//
// A destroyed cluster with no destroyed_at timestamp cannot be proven active
// during any window, so it is reported as not overlapping (ok=false). Treating
// it as "still running" would project it through the window end and bill it
// forever; this guards against legacy rows whose destroy path failed to stamp
// destroyed_at.
func activeWindow(cluster *types.Cluster, periodStart, periodEnd time.Time) (start, end time.Time, ok bool) {
	if cluster.Status == types.ClusterStatusDestroyed && cluster.DestroyedAt == nil {
		return time.Time{}, time.Time{}, false
	}

	// Determine cluster's active period within the date range.
	clusterStart := cluster.CreatedAt
	clusterEnd := periodEnd // Assume still running
	if cluster.Status == types.ClusterStatusDestroyed && cluster.DestroyedAt != nil {
		clusterEnd = *cluster.DestroyedAt
	}

	activeStart := clusterStart
	if periodStart.After(clusterStart) {
		activeStart = periodStart
	}
	activeEnd := clusterEnd
	if periodEnd.Before(clusterEnd) {
		activeEnd = periodEnd
	}

	// If cluster wasn't active during this period, there is no overlap.
	if activeStart.After(activeEnd) || activeStart.After(periodEnd) || activeEnd.Before(periodStart) {
		return time.Time{}, time.Time{}, false
	}

	return activeStart, activeEnd, true
}

// PeriodCost returns the estimated cost and runtime hours for a cluster within
// the window [periodStart, periodEnd] using a single flat effectiveCost. It
// intersects the cluster's lifetime (CreatedAt .. DestroyedAt/now) with the
// window; a cluster that was not active during the window contributes zero cost
// and zero hours.
//
// Because it applies one flat rate to the whole window, it cannot distinguish
// running from hibernated time. Prefer PeriodCostWithHistory when the cluster's
// hibernate/resume history is available.
func PeriodCost(cluster *types.Cluster, effectiveCost float64, periodStart, periodEnd time.Time) (float64, float64) {
	activeStart, activeEnd, ok := activeWindow(cluster, periodStart, periodEnd)
	if !ok {
		return 0.0, 0.0
	}

	runtimeHours := activeEnd.Sub(activeStart).Hours()
	if runtimeHours < 0 {
		runtimeHours = 0
	}

	return runtimeHours * effectiveCost, runtimeHours
}

// StateTransition marks a point in time where a cluster changed billing state
// (running <-> hibernated), reconstructed from its hibernate/resume job history.
type StateTransition struct {
	// At is the moment the transition took effect (typically a hibernate/resume
	// job's completion time).
	At time.Time
	// ToHibernated is true when the cluster entered the hibernated state at this
	// transition, false when it returned to running.
	ToHibernated bool
}

// PeriodCostWithHistory returns the estimated cost and runtime hours for a
// cluster within [periodStart, periodEnd], billing each sub-interval at the rate
// that actually applied — running hours at the running rate, hibernated hours at
// the reduced rate — rather than a single flat rate keyed on the cluster's
// instantaneous status.
//
// transitions is the cluster's hibernate/resume history (in any order; it is
// sorted internally). Transitions before the window establish the billing state
// at the window start. When transitions is empty the function falls back to the
// flat-rate model keyed on the cluster's current status, so clusters with no
// recorded state changes bill exactly as they did before.
//
// runtimeHours counts all active wall-clock hours in the window (running plus
// hibernated), matching PeriodCost; only the cost is state-aware.
func PeriodCostWithHistory(cluster *types.Cluster, prof *profile.Profile, transitions []StateTransition, periodStart, periodEnd time.Time) (float64, float64) {
	activeStart, activeEnd, ok := activeWindow(cluster, periodStart, periodEnd)
	if !ok {
		return 0.0, 0.0
	}

	totalHours := activeEnd.Sub(activeStart).Hours()
	if totalHours <= 0 {
		return 0.0, 0.0
	}

	// No recorded state changes: preserve the legacy single-rate behavior so
	// currently-hibernated clusters (and clusters with no job history) bill as
	// they did before.
	if len(transitions) == 0 {
		return totalHours * EffectiveHourlyCost(cluster, prof), totalHours
	}

	runningRate := RunningHourlyCost(prof)
	hibernatedRate := HibernatedHourlyCost(cluster.ClusterType, prof)
	rateFor := func(hibernated bool) float64 {
		if hibernated {
			return hibernatedRate
		}
		return runningRate
	}

	// Sort a copy so the caller's slice is left untouched.
	sorted := make([]StateTransition, len(transitions))
	copy(sorted, transitions)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })

	// Determine the billing state at activeStart: the most recent transition at
	// or before activeStart wins; with none, the cluster starts running.
	hibernated := false
	for _, tr := range sorted {
		if tr.At.After(activeStart) {
			break
		}
		hibernated = tr.ToHibernated
	}

	// Walk the segments between activeStart and activeEnd, billing each at the
	// rate that applied during it.
	var totalCost float64
	segStart := activeStart
	for _, tr := range sorted {
		if !tr.At.After(activeStart) {
			continue // already folded into the initial state
		}
		if !tr.At.Before(activeEnd) {
			break // at or beyond the window end
		}
		if tr.ToHibernated == hibernated {
			continue // redundant transition, no state change
		}
		if h := tr.At.Sub(segStart).Hours(); h > 0 {
			totalCost += h * rateFor(hibernated)
		}
		segStart = tr.At
		hibernated = tr.ToHibernated
	}
	// Final segment runs from the last transition to the window end.
	if h := activeEnd.Sub(segStart).Hours(); h > 0 {
		totalCost += h * rateFor(hibernated)
	}

	return totalCost, totalHours
}
