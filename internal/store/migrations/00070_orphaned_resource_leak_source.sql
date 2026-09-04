-- +goose Up
-- Migration: Add leak-source back-references to orphaned_resources
-- Records which cluster (and originating lifecycle job) leaked an orphaned
-- resource, so operators can trace an orphan back to its source. Matching is
-- best-effort by cluster name (there is no infra_id column) and intentionally
-- includes DESTROYED clusters, since the leaking cluster is almost always
-- already torn down.
-- Created: 2026-09-04

-- +goose StatementBegin

ALTER TABLE orphaned_resources
    ADD COLUMN cluster_id VARCHAR(64) REFERENCES clusters(id) ON DELETE SET NULL,
    ADD COLUMN job_id VARCHAR(64) REFERENCES jobs(id) ON DELETE SET NULL;

-- ON DELETE SET NULL: the janitor purges old DESTROYED clusters (and their jobs
-- cascade away with them). The orphaned_resource row must survive that purge --
-- the back-reference simply becomes NULL rather than blocking the delete.

CREATE INDEX IF NOT EXISTS idx_orphaned_resources_cluster_id ON orphaned_resources(cluster_id);

COMMENT ON COLUMN orphaned_resources.cluster_id IS 'Best-effort back-reference to the cluster (most-recent name match, incl. DESTROYED) that leaked this resource; NULL if unresolved or the cluster record was later purged';
COMMENT ON COLUMN orphaned_resources.job_id IS 'Best-effort back-reference to the originating lifecycle job (latest FAILED, else latest, CREATE/DESTROY/JANITOR_DESTROY for cluster_id) that leaked this resource; NULL if unresolved or purged';

-- +goose StatementEnd

-- +goose Down

-- +goose StatementBegin

DROP INDEX IF EXISTS idx_orphaned_resources_cluster_id;

ALTER TABLE orphaned_resources
    DROP COLUMN cluster_id,
    DROP COLUMN job_id;

-- +goose StatementEnd
