-- +goose Up
-- Seed default add-ons for Phase 2

-- OADP (OpenShift API for Data Protection)
INSERT INTO post_config_addons (addon_id, name, description, category, config, supported_platforms, enabled)
VALUES (
  'oadp',
  'OpenShift API for Data Protection (OADP)',
  'Backup and restore OpenShift clusters and applications using Velero. Provides disaster recovery capabilities and data protection.',
  'backup',
  '{"operators": [{"name": "oadp-operator", "namespace": "openshift-adp", "source": "redhat-operators", "channel": "stable-1.4"}]}'::jsonb,
  ARRAY['openshift'],
  TRUE
);

-- MTC (Migration Toolkit for Containers)
INSERT INTO post_config_addons (addon_id, name, description, category, config, supported_platforms, enabled)
VALUES (
  'mtc',
  'Migration Toolkit for Containers (MTC)',
  'Migrate applications between OpenShift clusters. Enables cluster-to-cluster migration for workloads, PVs, and configurations.',
  'migration',
  '{"operators": [{"name": "mtc-operator", "namespace": "openshift-migration", "source": "redhat-operators", "channel": "release-v1.8"}]}'::jsonb,
  ARRAY['openshift'],
  TRUE
);

-- MTA (Migration Toolkit for Applications)
INSERT INTO post_config_addons (addon_id, name, description, category, config, supported_platforms, enabled)
VALUES (
  'mta',
  'Migration Toolkit for Applications (MTA)',
  'Modernize and migrate applications to containers. Analyze applications, identify migration issues, and provide transformation guidance.',
  'migration',
  '{"operators": [{"name": "mta-operator", "namespace": "openshift-mta", "source": "redhat-operators", "channel": "stable-v7.0"}]}'::jsonb,
  ARRAY['openshift'],
  TRUE
);

-- Add comments
COMMENT ON TABLE post_config_addons IS 'Pre-defined add-on configurations seeded with OADP, MTC, and MTA';

-- +goose Down
DELETE FROM post_config_addons WHERE addon_id IN ('oadp', 'mtc', 'mta');
