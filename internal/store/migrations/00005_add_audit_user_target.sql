-- Migration: Add target_user_id to audit_events for user management audit logging
-- Created: 2026-03-01

-- Add target_user_id column to audit_events table
ALTER TABLE audit_events
ADD COLUMN IF NOT EXISTS target_user_id UUID REFERENCES users(id);

-- Create index on target_user_id for efficient querying
CREATE INDEX IF NOT EXISTS idx_audit_events_target_user_id
ON audit_events(target_user_id);

-- Add comment
COMMENT ON COLUMN audit_events.target_user_id IS 'User ID for user management actions (create/update/delete user)';
