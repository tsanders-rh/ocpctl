package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/internal/cost"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ReportStore provides aggregate read queries backing the usage report.
type ReportStore struct {
	pool *pgxpool.Pool
}

// GetClustersActiveInRange returns every cluster whose lifetime overlaps the
// window [start, end]. A cluster is considered active if it was created on or
// before the end of the window and was either not yet destroyed or destroyed on
// or after the start of the window.
//
// A cluster whose status is DESTROYED but whose destroyed_at is NULL is excluded:
// it is gone, we just cannot prove when, so it must not be treated as an
// indefinitely-running cluster that overlaps every window. (See the destroy-path
// fast paths that historically set status without stamping destroyed_at.)
//
// Only the columns needed by the usage report are selected; the rest of the
// Cluster struct is left zero-valued.
func (s *ReportStore) GetClustersActiveInRange(ctx context.Context, start, end time.Time) ([]*types.Cluster, error) {
	query := `
		SELECT id, name, platform, cluster_type, version, profile, region,
			owner, owner_id, team, status, created_at, destroyed_at,
			selected_addon_ids
		FROM clusters
		WHERE created_at <= $2
		  AND (
			destroyed_at >= $1
			OR (destroyed_at IS NULL AND status <> 'DESTROYED')
		  )
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, start, end)
	if err != nil {
		return nil, fmt.Errorf("query clusters active in range: %w", err)
	}
	defer rows.Close()

	clusters := []*types.Cluster{}
	for rows.Next() {
		var c types.Cluster
		if err := rows.Scan(
			&c.ID,
			&c.Name,
			&c.Platform,
			&c.ClusterType,
			&c.Version,
			&c.Profile,
			&c.Region,
			&c.Owner,
			&c.OwnerID,
			&c.Team,
			&c.Status,
			&c.CreatedAt,
			&c.DestroyedAt,
			&c.SelectedAddonIDs,
		); err != nil {
			return nil, fmt.Errorf("scan cluster: %w", err)
		}
		clusters = append(clusters, &c)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate clusters: %w", err)
	}

	return clusters, nil
}

// GetHibernationTransitions returns the running<->hibernated transition history
// for the given clusters, reconstructed from SUCCEEDED hibernate/resume jobs.
//
// The full history is returned (not just transitions within a report window) so
// callers can determine a cluster's billing state at any point in a window from
// the most recent prior transition. The transition time is the job's completion
// time (falling back to updated_at/created_at for legacy rows that never stamped
// ended_at). Results are keyed by cluster ID and sorted ascending by time;
// clusters with no hibernate/resume history are absent from the map.
func (s *ReportStore) GetHibernationTransitions(ctx context.Context, clusterIDs []string) (map[string][]cost.StateTransition, error) {
	out := map[string][]cost.StateTransition{}
	if len(clusterIDs) == 0 {
		return out, nil
	}

	query := `
		SELECT cluster_id, job_type, COALESCE(ended_at, updated_at, created_at)
		FROM jobs
		WHERE cluster_id = ANY($1)
		  AND job_type IN ('HIBERNATE', 'RESUME')
		  AND status = 'SUCCEEDED'
		ORDER BY cluster_id, COALESCE(ended_at, updated_at, created_at) ASC
	`

	rows, err := s.pool.Query(ctx, query, clusterIDs)
	if err != nil {
		return nil, fmt.Errorf("query hibernation transitions: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var clusterID, jobType string
		var at time.Time
		if err := rows.Scan(&clusterID, &jobType, &at); err != nil {
			return nil, fmt.Errorf("scan hibernation transition: %w", err)
		}
		out[clusterID] = append(out[clusterID], cost.StateTransition{
			At:           at,
			ToHibernated: jobType == string(types.JobTypeHibernate),
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hibernation transitions: %w", err)
	}

	return out, nil
}

// GetJobStatsInRange returns job counts grouped by job type and status for jobs
// created within the window [start, end]. The handler folds these into the
// report's lifecycle section (created/destroyed/hibernated counts, create
// success rate).
func (s *ReportStore) GetJobStatsInRange(ctx context.Context, start, end time.Time) (*types.JobStats, error) {
	query := `
		SELECT job_type, status, COUNT(*)
		FROM jobs
		WHERE created_at >= $1 AND created_at <= $2
		GROUP BY job_type, status
	`

	rows, err := s.pool.Query(ctx, query, start, end)
	if err != nil {
		return nil, fmt.Errorf("query job stats in range: %w", err)
	}
	defer rows.Close()

	stats := &types.JobStats{Counts: map[string]map[string]int{}}
	for rows.Next() {
		var jobType, status string
		var count int
		if err := rows.Scan(&jobType, &status, &count); err != nil {
			return nil, fmt.Errorf("scan job stats: %w", err)
		}
		if stats.Counts[jobType] == nil {
			stats.Counts[jobType] = map[string]int{}
		}
		stats.Counts[jobType][status] = count
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job stats: %w", err)
	}

	return stats, nil
}
