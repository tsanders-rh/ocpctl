# GCP Deployment Notes

## Issues Encountered and Fixed

### 1. Database Platform Constraint
**Issue:** Creating GCP clusters failed with:
```
ERROR: new row for relation "clusters" violates check constraint "clusters_platform_check"
```

**Root Cause:** The `clusters` table had a check constraint that only allowed `'aws'` and `'ibmcloud'` platforms.

**Fix:**
- Applied migration `00035_add_gcp_platform_support.sql`
- Updated constraint to allow `'aws'`, `'ibmcloud'`, and `'gcp'`

**SQL Applied:**
```sql
ALTER TABLE clusters DROP CONSTRAINT clusters_platform_check;
ALTER TABLE clusters ADD CONSTRAINT clusters_platform_check
  CHECK (platform IN ('aws', 'ibmcloud', 'gcp'));
```

### 2. Worker Environment Variables
**Issue:** GCP cluster creation failed with:
```
credentials: could not find default credentials
```

**Root Cause:** The production worker service uses `EnvironmentFile=/etc/ocpctl/worker.env`, but the `configure-gcp-credentials.sh` script only added GCP variables to the systemd service file directly (using `Environment=` directives), not to the `/etc/ocpctl/worker.env` file.

**Fix:**
Added GCP environment variables to `/etc/ocpctl/worker.env`:
```bash
# GCP Configuration
GOOGLE_APPLICATION_CREDENTIALS=/opt/ocpctl/gcp-credentials.json
GCP_PROJECT=migration-eng
```

Then restarted the worker: `sudo systemctl restart ocpctl-worker`

**Files Updated:**
- `/etc/ocpctl/worker.env` (on production server)
- `s3://ocpctl-binaries/config/worker.env` (already had GCP vars from configure script)

### 3. Autoscale Workers
**Status:** ✅ Configured correctly

The `configure-gcp-credentials.sh` script already:
- Uploaded GCP credentials to S3 at `s3://ocpctl-binaries/config/gcp-credentials.json`
- Updated `config/worker.env` with GCP variables and uploaded to S3
- Modified `bootstrap-worker.sh` to download GCP credentials from S3
- Uploaded updated bootstrap script to S3

So autoscale workers will automatically get GCP support when they launch.

## Current Status

All components are now configured for GCP support:

✅ **Database:** Platform constraint allows 'gcp'
✅ **API Server:** GCP profiles API filter includes 'gcp'
✅ **Frontend:** GCP platform button added
✅ **Main Worker:** Environment variables configured, can authenticate with GCP
✅ **Autoscale Workers:** Bootstrap script downloads credentials and env vars from S3
✅ **GCP Credentials:** Service account JSON uploaded and accessible
✅ **GCP Authentication:** Verified working with `migration-eng` project

## Testing

To verify GCP cluster creation works:
1. Go to https://ocpctl.mg.dog8code.com/clusters/new
2. Select Platform: **GCP**
3. Select Profile: **gcp-gke-standard** or **gcp-standard**
4. Fill in cluster details and create

Expected: Cluster creation should proceed without credential errors.

## Files Modified

- `internal/api/handler_profiles.go` - Added GCP to platform filter validation
- `web/app/(dashboard)/profiles/page.tsx` - Added GCP platform button
- `internal/store/migrations/00035_add_gcp_platform_support.sql` - Database constraint
- `scripts/configure-gcp-credentials.sh` - GCP credential setup automation
- `scripts/bootstrap-worker.sh` - Downloads GCP credentials from S3
- `/etc/ocpctl/worker.env` - Added GCP environment variables (production server)

## Lessons Learned

When adding environment variables to workers:
1. Check if the service uses `EnvironmentFile=` or `Environment=` directives
2. If using `EnvironmentFile`, update the env file itself, not just the service file
3. For autoscale workers, update both the S3 config and the bootstrap script
4. Verify environment variables are loaded by checking `/proc/<pid>/environ`
