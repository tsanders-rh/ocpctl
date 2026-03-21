-- +goose Up
-- Migration: Add job retry history tracking
-- This table tracks each retry attempt for a job with the error that caused the retry
-- Allows debugging why jobs are retrying and how often retries occur

-- Create job_retry_history table
CREATE TABLE IF NOT EXISTS job_retry_history (
    id BIGSERIAL PRIMARY KEY,
    job_id VARCHAR(64) NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    attempt INT NOT NULL,
    error_code VARCHAR(100),
    error_message TEXT,
    failed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE(job_id, attempt)
);

-- Indexes for efficient querying
CREATE INDEX idx_job_retry_history_job_id ON job_retry_history(job_id);
CREATE INDEX idx_job_retry_history_failed_at ON job_retry_history(failed_at);
CREATE INDEX idx_job_retry_history_error_code ON job_retry_history(error_code) WHERE error_code IS NOT NULL;

-- Comment on table
COMMENT ON TABLE job_retry_history IS 'Tracks retry attempts for jobs with error details';
COMMENT ON COLUMN job_retry_history.attempt IS 'The attempt number that failed (1-indexed)';
COMMENT ON COLUMN job_retry_history.error_code IS 'Short error code for categorization';
COMMENT ON COLUMN job_retry_history.error_message IS 'Full error message from the failed attempt';
COMMENT ON COLUMN job_retry_history.failed_at IS 'When this attempt failed';

-- +goose Down
DROP TABLE IF EXISTS job_retry_history;
