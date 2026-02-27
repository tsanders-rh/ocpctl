package types

import "time"

// UsageSample represents a point-in-time cost tracking sample
type UsageSample struct {
	ID                   string    `db:"id"`
	ClusterID            string    `db:"cluster_id"`
	SampleTime           time.Time `db:"sample_time"`
	State                string    `db:"state"`
	ControlPlaneReplicas int       `db:"control_plane_replicas"`
	WorkerReplicas       int       `db:"worker_replicas"`
	EstimatedHourlyCost  *float64  `db:"estimated_hourly_cost"`
	Metadata             JobMetadata `db:"metadata"`
}
