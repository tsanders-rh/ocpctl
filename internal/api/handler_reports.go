package api

import (
	"sort"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/cost"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// dateFormat is the query-param date layout for report ranges.
const dateFormat = "2006-01-02"

// ReportHandler serves platform-wide usage/cost reports (admin only).
type ReportHandler struct {
	store    *store.Store
	registry *profile.Registry
}

// NewReportHandler creates a new report handler.
func NewReportHandler(st *store.Store, r *profile.Registry) *ReportHandler {
	return &ReportHandler{store: st, registry: r}
}

// GetUsageReport returns an adhoc, date-ranged usage report.
//
//	@Summary		Get usage report
//	@Description	Returns a platform-wide usage/cost report for an adhoc date range: estimated cost, most used profiles, most active users, and cluster lifecycle stats.
//	@Tags			Reports
//	@Accept			json
//	@Produce		json
//	@Param			start_date	query		string	false	"Start date (YYYY-MM-DD), inclusive. Defaults to 30 days ago."
//	@Param			end_date	query		string	false	"End date (YYYY-MM-DD), inclusive. Defaults to today."
//	@Success		200			{object}	types.UsageReport
//	@Failure		400			{object}	map[string]string	"Invalid date range"
//	@Failure		500			{object}	map[string]string	"Failed to build report"
//	@Security		BearerAuth
//	@Router			/admin/reports/usage [get]
func (h *ReportHandler) GetUsageReport(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse the requested window, defaulting to the last 30 days.
	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month(), now.Day(), 23, 59, 59, 0, time.UTC)
	start := end.AddDate(0, 0, -30)

	if v := c.QueryParam("start_date"); v != "" {
		t, err := time.Parse(dateFormat, v)
		if err != nil {
			return ErrorBadRequest(c, "invalid start_date, expected YYYY-MM-DD")
		}
		start = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	if v := c.QueryParam("end_date"); v != "" {
		t, err := time.Parse(dateFormat, v)
		if err != nil {
			return ErrorBadRequest(c, "invalid end_date, expected YYYY-MM-DD")
		}
		end = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)
	}
	if start.After(end) {
		return ErrorBadRequest(c, "start_date must be on or before end_date")
	}

	clusters, err := h.store.Reports.GetClustersActiveInRange(ctx, start, end)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	// Resolve owner emails in one batch to avoid N+1 lookups.
	ownerIDs := make([]string, 0, len(clusters))
	seen := map[string]bool{}
	for _, cl := range clusters {
		if cl.OwnerID != "" && !seen[cl.OwnerID] {
			seen[cl.OwnerID] = true
			ownerIDs = append(ownerIDs, cl.OwnerID)
		}
	}
	usersByID := map[string]*types.User{}
	if len(ownerIDs) > 0 {
		if usersByID, err = h.store.Users.GetByIDs(ctx, ownerIDs); err != nil {
			// Non-fatal: fall back to owner IDs / emails on the cluster record.
			LogWarning(c, "failed to resolve owner emails for usage report", "error", err.Error())
			usersByID = map[string]*types.User{}
		}
	}

	// Aggregate cost / profiles / users over the window.
	profileAgg := map[string]*types.ProfileUsage{}
	userAgg := map[string]*types.UserUsage{}
	var totalCost, totalHours float64
	var lifetimeSum float64
	var lifetimeCount int

	for _, cl := range clusters {
		// Use GetAny so disabled/removed profiles still get costed.
		prof, perr := h.registry.GetAny(cl.Profile)
		var effective float64
		var clusterCost, clusterHours float64
		if perr == nil && prof != nil {
			effective = cost.EffectiveHourlyCost(cl, prof)
			clusterCost, clusterHours = cost.PeriodCost(cl, effective, start, end)
		}

		totalCost += clusterCost
		totalHours += clusterHours

		// Profile aggregation
		pu := profileAgg[cl.Profile]
		if pu == nil {
			pu = &types.ProfileUsage{Profile: cl.Profile}
			profileAgg[cl.Profile] = pu
		}
		pu.ClusterCount++
		pu.RuntimeHours += clusterHours
		pu.EstimatedCost += clusterCost

		// User aggregation
		ownerKey := cl.Owner
		if ownerKey == "" {
			ownerKey = cl.OwnerID
		}
		if u := usersByID[cl.OwnerID]; u != nil && u.Email != "" {
			ownerKey = u.Email
		}

		// Per-cluster drill-down detail behind the profile aggregate.
		pu.Clusters = append(pu.Clusters, types.ClusterUsage{
			Name:          cl.Name,
			Owner:         ownerKey,
			Region:        cl.Region,
			Status:        string(cl.Status),
			ClusterType:   string(cl.ClusterType),
			CreatedAt:     cl.CreatedAt,
			DestroyedAt:   cl.DestroyedAt,
			RuntimeHours:  clusterHours,
			EstimatedCost: clusterCost,
		})

		uu := userAgg[ownerKey]
		if uu == nil {
			uu = &types.UserUsage{Owner: ownerKey}
			userAgg[ownerKey] = uu
		}
		uu.ClusterCount++
		uu.RuntimeHours += clusterHours
		uu.EstimatedCost += clusterCost

		// Average lifetime (over clusters active in-window). Cap still-running
		// clusters at the current time, not the window end: the window end may be
		// in the future (end-of-day today) or, for a past-dated range, long before
		// now — either way it does not reflect a cluster's real age.
		lifeEnd := now
		if cl.Status == types.ClusterStatusDestroyed && cl.DestroyedAt != nil {
			lifeEnd = *cl.DestroyedAt
		}
		if lifeEnd.After(cl.CreatedAt) {
			lifetimeSum += lifeEnd.Sub(cl.CreatedAt).Hours()
			lifetimeCount++
		}
	}

	// Job-derived lifecycle stats.
	jobStats, err := h.store.Reports.GetJobStatsInRange(ctx, start, end)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}
	lifecycle := buildLifecycleStats(jobStats, clusters)
	if lifetimeCount > 0 {
		lifecycle.AvgLifetimeHours = lifetimeSum / float64(lifetimeCount)
	}

	// Sort profiles by cluster count desc, users by estimated cost desc.
	profiles := make([]types.ProfileUsage, 0, len(profileAgg))
	for _, pu := range profileAgg {
		sort.Slice(pu.Clusters, func(i, j int) bool {
			return pu.Clusters[i].EstimatedCost > pu.Clusters[j].EstimatedCost
		})
		profiles = append(profiles, *pu)
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].ClusterCount != profiles[j].ClusterCount {
			return profiles[i].ClusterCount > profiles[j].ClusterCount
		}
		return profiles[i].EstimatedCost > profiles[j].EstimatedCost
	})

	users := make([]types.UserUsage, 0, len(userAgg))
	for _, uu := range userAgg {
		users = append(users, *uu)
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].EstimatedCost != users[j].EstimatedCost {
			return users[i].EstimatedCost > users[j].EstimatedCost
		}
		return users[i].ClusterCount > users[j].ClusterCount
	})

	// Prior-period comparison: an equally-sized window immediately preceding the
	// current one. Queried separately so the current-window aggregations above
	// (counts, breakdowns) only ever reflect clusters active in-window.
	duration := end.Sub(start)
	priorStart := start.Add(-duration)
	priorEnd := start
	var priorComparison *types.PeriodComparison
	if priorClusters, perr := h.store.Reports.GetClustersActiveInRange(ctx, priorStart, priorEnd); perr != nil {
		// Non-fatal: the report is still useful without the comparison.
		LogWarning(c, "failed to compute prior-period cost for usage report", "error", perr.Error())
	} else {
		priorTotal := h.sumWindowCost(priorClusters, priorStart, priorEnd)
		percentChange := 0.0
		if priorTotal > 0 {
			percentChange = ((totalCost - priorTotal) / priorTotal) * 100
		}
		priorComparison = &types.PeriodComparison{
			CurrentPeriod:  totalCost,
			PreviousPeriod: priorTotal,
			PercentChange:  percentChange,
			StartDate:      priorStart.Format(dateFormat),
			EndDate:        priorEnd.Format(dateFormat),
		}
	}

	report := &types.UsageReport{
		StartDate:   start.Format(dateFormat),
		EndDate:     end.Format(dateFormat),
		GeneratedAt: now,
		Cost: types.UsageCostSummary{
			TotalCost:             totalCost,
			TotalRuntimeHours:     totalHours,
			ClustersActive:        len(clusters),
			PriorPeriodComparison: priorComparison,
		},
		Profiles:  profiles,
		Users:     users,
		Lifecycle: lifecycle,
	}

	return SuccessOK(c, report)
}

// sumWindowCost returns the total estimated cost for the given clusters over the
// window [start, end], using the same cost model as the main aggregation.
func (h *ReportHandler) sumWindowCost(clusters []*types.Cluster, start, end time.Time) float64 {
	var total float64
	for _, cl := range clusters {
		prof, err := h.registry.GetAny(cl.Profile)
		if err != nil || prof == nil {
			continue
		}
		eff := cost.EffectiveHourlyCost(cl, prof)
		c, _ := cost.PeriodCost(cl, eff, start, end)
		total += c
	}
	return total
}

// buildLifecycleStats folds job counts and cluster breakdowns into the report's
// lifecycle section.
func buildLifecycleStats(jobStats *types.JobStats, clusters []*types.Cluster) types.LifecycleStats {
	ls := types.LifecycleStats{
		ByPlatform:    map[string]int{},
		ByClusterType: map[string]int{},
		ByStatus:      map[string]int{},
	}

	succeeded := string(types.JobStatusSucceeded)
	failed := string(types.JobStatusFailed)

	count := func(jobType, status string) int {
		if m := jobStats.Counts[jobType]; m != nil {
			return m[status]
		}
		return 0
	}

	ls.Created = count(string(types.JobTypeCreate), succeeded)
	ls.Destroyed = count(string(types.JobTypeDestroy), succeeded) +
		count(string(types.JobTypeJanitorDestroy), succeeded)
	ls.Hibernated = count(string(types.JobTypeHibernate), succeeded)

	ls.CreateSuccess = count(string(types.JobTypeCreate), succeeded)
	ls.CreateFailure = count(string(types.JobTypeCreate), failed)
	if completed := ls.CreateSuccess + ls.CreateFailure; completed > 0 {
		ls.CreateSuccessRate = float64(ls.CreateSuccess) / float64(completed)
	}

	for _, cl := range clusters {
		ls.ByPlatform[string(cl.Platform)]++
		ls.ByClusterType[string(cl.ClusterType)]++
		ls.ByStatus[string(cl.Status)]++
	}

	return ls
}
