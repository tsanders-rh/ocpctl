#!/bin/bash
set -e

# Sync profile versions from YAML to database
# This updates the database to match the standardized version format (x.y for GA, x.y.z-suffix for pre-release)

echo "Syncing profile versions to database..."

# Get database URL from environment or config
if [ -z "$DATABASE_URL" ]; then
    if [ -f "/etc/ocpctl/api.env" ]; then
        export $(grep DATABASE_URL /etc/ocpctl/api.env | xargs)
    elif [ -f "config/api.env" ]; then
        export $(grep DATABASE_URL config/api.env | xargs)
    else
        echo "ERROR: DATABASE_URL not found. Please set DATABASE_URL environment variable."
        exit 1
    fi
fi

echo "Updating profiles in database..."

# Update each profile with consolidated versions
# GA profiles: use x.y format
# Pre-release profiles: keep x.y.z-suffix format

# AWS profiles
psql "$DATABASE_URL" <<'SQL'
-- aws-minimal-ga
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.21"'::jsonb
)
WHERE name = 'aws-minimal-ga';

-- aws-sno-ga
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.21"'::jsonb
)
WHERE name = 'aws-sno-ga';

-- aws-standard-ga
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.21"'::jsonb
)
WHERE name = 'aws-standard-ga';

-- aws-virtualization-ga
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.21"'::jsonb
)
WHERE name = 'aws-virtualization-ga';

-- aws-virt-windows-minimal-ga
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.21"'::jsonb
)
WHERE name = 'aws-virt-windows-minimal-ga';

-- aws-rosa-minimal
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.21"'::jsonb
)
WHERE name = 'aws-rosa-minimal';

-- aws-rosa-standard
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.21"'::jsonb
)
WHERE name = 'aws-rosa-standard';

-- aws-rhwa-lab
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.21"'::jsonb
)
WHERE name = 'aws-rhwa-lab';

-- Mixed GA/pre-release profiles
-- aws-minimal-test
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21", "4.22.0-ec.5"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'aws-minimal-test';

-- aws-sno-test
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21", "4.22.0-ec.5"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'aws-sno-test';

-- aws-sno-shared-vpc
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21", "4.22.0-ec.5"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'aws-sno-shared-vpc';

-- aws-sno-shared-vpc-custom
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21", "4.22.0-ec.5"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'aws-sno-shared-vpc-custom';

-- aws-standard
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21", "4.22.0-ec.5"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'aws-standard';

-- aws-virtualization
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21", "4.22.0-ec.5"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'aws-virtualization';

-- aws-virt-windows-minimal
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21", "4.22.0-ec.5"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'aws-virt-windows-minimal';

-- GCP profiles
-- gcp-sno-ga
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'gcp-sno-ga';

-- gcp-standard
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20", "4.21"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'gcp-standard';

-- IBM Cloud profiles
-- ibmcloud-minimal-test
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'ibmcloud-minimal-test';

-- ibmcloud-standard
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.18", "4.19", "4.20"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'ibmcloud-standard';

-- Azure ARO profile (special case - has specific patch versions)
-- azure-aro-standard
UPDATE profiles SET profile_data = jsonb_set(
  jsonb_set(
    profile_data,
    '{OpenshiftVersions,allowed}',
    '["4.16", "4.17", "4.18", "4.19", "4.20"]'::jsonb
  ),
  '{OpenshiftVersions,default}',
  '"4.20"'::jsonb
)
WHERE name = 'azure-aro-standard';

SQL

echo "✓ Database profiles updated"
echo ""
echo "Verification:"
echo "  psql \$DATABASE_URL -c \"SELECT name, profile_data->'OpenshiftVersions'->'allowed' as versions FROM profiles WHERE name LIKE '%ga%' LIMIT 5;\""
