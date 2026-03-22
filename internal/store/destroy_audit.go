package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// DestroyAuditStore handles destroy audit operations
type DestroyAuditStore struct {
	pool *pgxpool.Pool
}

// Create creates a new destroy audit record
func (s *DestroyAuditStore) Create(ctx context.Context, audit *types.DestroyAudit) error {
	query := `
		INSERT INTO destroy_audit (
			id, cluster_id, job_id, worker_id, destroy_started_at,
			last_verified_at, verification_passed, last_resource_present,
			terminal_reason, resources_snapshot, verification_snapshot,
			created_at, completed_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
	`

	_, err := s.pool.Exec(ctx, query,
		audit.ID,
		audit.ClusterID,
		audit.JobID,
		audit.WorkerID,
		audit.DestroyStartedAt,
		audit.LastVerifiedAt,
		audit.VerificationPassed,
		audit.LastResourcePresent,
		audit.TerminalReason,
		audit.ResourcesSnapshot,
		audit.VerificationSnapshot,
		audit.CreatedAt,
		audit.CompletedAt,
	)

	if err != nil {
		return fmt.Errorf("insert destroy audit: %w", err)
	}

	return nil
}

// Update updates an existing destroy audit record
func (s *DestroyAuditStore) Update(ctx context.Context, audit *types.DestroyAudit) error {
	query := `
		UPDATE destroy_audit
		SET
			last_verified_at = $2,
			verification_passed = $3,
			last_resource_present = $4,
			terminal_reason = $5,
			resources_snapshot = $6,
			verification_snapshot = $7,
			completed_at = $8
		WHERE id = $1
	`

	result, err := s.pool.Exec(ctx, query,
		audit.ID,
		audit.LastVerifiedAt,
		audit.VerificationPassed,
		audit.LastResourcePresent,
		audit.TerminalReason,
		audit.ResourcesSnapshot,
		audit.VerificationSnapshot,
		audit.CompletedAt,
	)

	if err != nil {
		return fmt.Errorf("update destroy audit: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("destroy audit record not found: %s", audit.ID)
	}

	return nil
}

// GetByCluster retrieves all destroy audit records for a cluster
func (s *DestroyAuditStore) GetByCluster(ctx context.Context, clusterID string) ([]*types.DestroyAudit, error) {
	query := `
		SELECT id, cluster_id, job_id, worker_id, destroy_started_at,
			last_verified_at, verification_passed, last_resource_present,
			terminal_reason, resources_snapshot, verification_snapshot,
			created_at, completed_at
		FROM destroy_audit
		WHERE cluster_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query, clusterID)
	if err != nil {
		return nil, fmt.Errorf("query destroy audit by cluster: %w", err)
	}
	defer rows.Close()

	audits := []*types.DestroyAudit{}
	for rows.Next() {
		var audit types.DestroyAudit
		err := rows.Scan(
			&audit.ID,
			&audit.ClusterID,
			&audit.JobID,
			&audit.WorkerID,
			&audit.DestroyStartedAt,
			&audit.LastVerifiedAt,
			&audit.VerificationPassed,
			&audit.LastResourcePresent,
			&audit.TerminalReason,
			&audit.ResourcesSnapshot,
			&audit.VerificationSnapshot,
			&audit.CreatedAt,
			&audit.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan destroy audit: %w", err)
		}
		audits = append(audits, &audit)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate destroy audits: %w", err)
	}

	return audits, nil
}

// GetByJob retrieves the destroy audit record for a specific job
func (s *DestroyAuditStore) GetByJob(ctx context.Context, jobID string) (*types.DestroyAudit, error) {
	query := `
		SELECT id, cluster_id, job_id, worker_id, destroy_started_at,
			last_verified_at, verification_passed, last_resource_present,
			terminal_reason, resources_snapshot, verification_snapshot,
			created_at, completed_at
		FROM destroy_audit
		WHERE job_id = $1
	`

	var audit types.DestroyAudit
	err := s.pool.QueryRow(ctx, query, jobID).Scan(
		&audit.ID,
		&audit.ClusterID,
		&audit.JobID,
		&audit.WorkerID,
		&audit.DestroyStartedAt,
		&audit.LastVerifiedAt,
		&audit.VerificationPassed,
		&audit.LastResourcePresent,
		&audit.TerminalReason,
		&audit.ResourcesSnapshot,
		&audit.VerificationSnapshot,
		&audit.CreatedAt,
		&audit.CompletedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("query destroy audit by job: %w", err)
	}

	return &audit, nil
}

// GetIncomplete retrieves destroy audits that haven't completed (for reconciliation)
func (s *DestroyAuditStore) GetIncomplete(ctx context.Context) ([]*types.DestroyAudit, error) {
	query := `
		SELECT id, cluster_id, job_id, worker_id, destroy_started_at,
			last_verified_at, verification_passed, last_resource_present,
			terminal_reason, resources_snapshot, verification_snapshot,
			created_at, completed_at
		FROM destroy_audit
		WHERE completed_at IS NULL
		ORDER BY created_at DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query incomplete destroy audits: %w", err)
	}
	defer rows.Close()

	audits := []*types.DestroyAudit{}
	for rows.Next() {
		var audit types.DestroyAudit
		err := rows.Scan(
			&audit.ID,
			&audit.ClusterID,
			&audit.JobID,
			&audit.WorkerID,
			&audit.DestroyStartedAt,
			&audit.LastVerifiedAt,
			&audit.VerificationPassed,
			&audit.LastResourcePresent,
			&audit.TerminalReason,
			&audit.ResourcesSnapshot,
			&audit.VerificationSnapshot,
			&audit.CreatedAt,
			&audit.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan destroy audit: %w", err)
		}
		audits = append(audits, &audit)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incomplete destroy audits: %w", err)
	}

	return audits, nil
}

// GetFailedVerifications retrieves destroy audits where verification failed (for drift detection)
func (s *DestroyAuditStore) GetFailedVerifications(ctx context.Context) ([]*types.DestroyAudit, error) {
	query := `
		SELECT id, cluster_id, job_id, worker_id, destroy_started_at,
			last_verified_at, verification_passed, last_resource_present,
			terminal_reason, resources_snapshot, verification_snapshot,
			created_at, completed_at
		FROM destroy_audit
		WHERE verification_passed = false
		ORDER BY last_verified_at DESC
	`

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query failed verification destroy audits: %w", err)
	}
	defer rows.Close()

	audits := []*types.DestroyAudit{}
	for rows.Next() {
		var audit types.DestroyAudit
		err := rows.Scan(
			&audit.ID,
			&audit.ClusterID,
			&audit.JobID,
			&audit.WorkerID,
			&audit.DestroyStartedAt,
			&audit.LastVerifiedAt,
			&audit.VerificationPassed,
			&audit.LastResourcePresent,
			&audit.TerminalReason,
			&audit.ResourcesSnapshot,
			&audit.VerificationSnapshot,
			&audit.CreatedAt,
			&audit.CompletedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan destroy audit: %w", err)
		}
		audits = append(audits, &audit)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate failed verification destroy audits: %w", err)
	}

	return audits, nil
}
