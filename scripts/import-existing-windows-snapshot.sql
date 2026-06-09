-- Import existing Windows snapshot from SSM Parameter Store into database
-- This is a one-time migration for the snapshot created before the management system
-- EXECUTED: 2026-06-09 16:12:45 UTC
-- STATUS: Successfully imported snap-0070e39897d0e0928 (us-east-1, v1.0)

INSERT INTO windows_snapshots (
    id,
    region,
    version,
    ebs_snapshot_id,
    status,
    ssm_parameter_path,
    validated_at,
    validation_vm_booted
) VALUES (
    gen_random_uuid(),
    'us-east-1',
    '1.0',
    'snap-0070e39897d0e0928',
    'ready',
    '/ocpctl/windows-snapshots/1.0/us-east-1',
    '2026-06-08 16:18:36',
    true
)
ON CONFLICT (region, version) DO NOTHING;
