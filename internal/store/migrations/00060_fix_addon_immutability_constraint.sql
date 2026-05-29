-- +goose Up
-- Migration 00060: Fix Addon Immutability Constraint
-- Remove incorrect system_addons_immutable constraint that was preventing system addons from being updated

-- Drop the incorrect constraint that forced system addons to be immutable
ALTER TABLE post_config_addons
  DROP CONSTRAINT IF EXISTS system_addons_immutable;

-- The correct constraint (published_addons_immutable) already exists from migration 00058
-- It allows:
-- - System addons to be updated when YAML changes (is_immutable can be false)
-- - User addon drafts to be edited (is_published=false, is_immutable=false)
-- - Published user addons to be immutable (is_published=true requires is_immutable=true)

-- +goose Down
-- Recreate the old (incorrect) constraint if rolling back
ALTER TABLE post_config_addons
  ADD CONSTRAINT system_addons_immutable CHECK (
    (addon_source = 'system' AND is_immutable = true) OR (addon_source = 'user')
  );
