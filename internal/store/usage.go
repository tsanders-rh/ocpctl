package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// UsageStore handles usage sample operations
type UsageStore struct {
	pool *pgxpool.Pool
}

// Record inserts a usage sample
func (s *UsageStore) Record(ctx context.Context, sample *types.UsageSample) error {
	query := `
		INSERT INTO usage_samples (
			id, cluster_id, sample_time, state, control_plane_replicas,
			worker_replicas, estimated_hourly_cost, metadata
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
		ON CONFLICT (cluster_id, sample_time) DO UPDATE
		SET state = EXCLUDED.state,
			control_plane_replicas = EXCLUDED.control_plane_replicas,
			worker_replicas = EXCLUDED.worker_replicas,
			estimated_hourly_cost = EXCLUDED.estimated_hourly_cost,
			metadata = EXCLUDED.metadata
	`

	_, err := s.pool.Exec(ctx, query,
		sample.ID,
		sample.ClusterID,
		sample.SampleTime,
		sample.State,
		sample.ControlPlaneReplicas,
		sample.WorkerReplicas,
		sample.EstimatedHourlyCost,
		sample.Metadata,
	)

	if err != nil {
		return fmt.Errorf("record usage sample: %w", err)
	}

	return nil
}
