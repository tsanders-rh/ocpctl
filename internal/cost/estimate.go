package cost

import (
	"time"

	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// EffectiveHourlyCost returns the effective hourly cost for a cluster based on
// its current state. Hibernated clusters cost significantly less than running
// clusters, and the reduction depends on the cluster type.
//
// This is the shared implementation used by both the per-team costs endpoint and
// the platform-wide usage report so cost figures stay consistent across surfaces.
func EffectiveHourlyCost(cluster *types.Cluster, prof *profile.Profile) float64 {
	baseCost := prof.CostControls.EstimatedHourlyCost

	// If cluster is hibernated, calculate reduced cost based on cluster type
	if cluster.Status == types.ClusterStatusHibernated {
		switch cluster.ClusterType {
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
			// GKE: Node pools scaled to 0, no control plane cost, only persistent disks (~3%)
			return baseCost * 0.03
		default:
			// Unknown cluster type, use conservative estimate
			return baseCost * 0.10
		}
	}

	// For all other states (READY, CREATING, etc.), use full cost
	return baseCost
}

// PeriodCost returns the estimated cost and runtime hours for a cluster within
// the window [periodStart, periodEnd]. It intersects the cluster's lifetime
// (CreatedAt .. DestroyedAt/now) with the window; a cluster that was not active
// during the window contributes zero cost and zero hours.
func PeriodCost(cluster *types.Cluster, effectiveCost float64, periodStart, periodEnd time.Time) (float64, float64) {
	// Determine cluster's active period within the date range
	clusterStart := cluster.CreatedAt
	clusterEnd := periodEnd // Assume still running

	// If cluster is destroyed, use destroyed_at as the end time
	if cluster.Status == types.ClusterStatusDestroyed && cluster.DestroyedAt != nil {
		clusterEnd = *cluster.DestroyedAt
	}

	// Calculate intersection of cluster lifetime and period
	activeStart := clusterStart
	if periodStart.After(clusterStart) {
		activeStart = periodStart
	}

	activeEnd := clusterEnd
	if periodEnd.Before(clusterEnd) {
		activeEnd = periodEnd
	}

	// If cluster wasn't active during this period, return 0
	if activeStart.After(activeEnd) || activeStart.After(periodEnd) || activeEnd.Before(periodStart) {
		return 0.0, 0.0
	}

	// Calculate runtime hours
	runtimeHours := activeEnd.Sub(activeStart).Hours()
	if runtimeHours < 0 {
		runtimeHours = 0
	}

	// Calculate total cost
	totalCost := runtimeHours * effectiveCost

	return totalCost, runtimeHours
}
