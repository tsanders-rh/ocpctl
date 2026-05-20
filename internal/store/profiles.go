package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// GetProfile retrieves a profile by name from the database
func (s *Store) GetProfile(ctx context.Context, name string) (*profile.Profile, error) {
	query := `
		SELECT name, display_name, description, platform, cluster_type, track, enabled, profile_data
		FROM profiles
		WHERE name = $1
	`

	var p profile.Profile
	var profileDataJSON []byte

	err := s.pool.QueryRow(ctx, query, name).Scan(
		&p.Name,
		&p.DisplayName,
		&p.Description,
		&p.Platform,
		&p.ClusterType,
		&p.Track,
		&p.Enabled,
		&profileDataJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get profile: %w", err)
	}

	// Unmarshal the full profile data
	if err := json.Unmarshal(profileDataJSON, &p); err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile data: %w", err)
	}

	return &p, nil
}

// ListProfiles retrieves all profiles from the database with optional filters
func (s *Store) ListProfiles(ctx context.Context, platform *types.Platform, track *string, enabledOnly bool) ([]*profile.Profile, error) {
	query := `
		SELECT name, display_name, description, platform, cluster_type, track, enabled, profile_data
		FROM profiles
		WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1

	if platform != nil {
		query += fmt.Sprintf(" AND platform = $%d", argIdx)
		args = append(args, *platform)
		argIdx++
	}

	if track != nil {
		query += fmt.Sprintf(" AND track = $%d", argIdx)
		args = append(args, *track)
		argIdx++
	}

	if enabledOnly {
		query += " AND enabled = true"
	}

	query += " ORDER BY display_name ASC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list profiles: %w", err)
	}
	defer rows.Close()

	profiles := []*profile.Profile{}
	for rows.Next() {
		var p profile.Profile
		var profileDataJSON []byte

		err := rows.Scan(
			&p.Name,
			&p.DisplayName,
			&p.Description,
			&p.Platform,
			&p.ClusterType,
			&p.Track,
			&p.Enabled,
			&profileDataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan profile: %w", err)
		}

		// Unmarshal the full profile data
		if err := json.Unmarshal(profileDataJSON, &p); err != nil {
			return nil, fmt.Errorf("failed to unmarshal profile data: %w", err)
		}

		profiles = append(profiles, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating profiles: %w", err)
	}

	return profiles, nil
}

// UpsertProfile inserts or updates a profile in the database
func (s *Store) UpsertProfile(ctx context.Context, p *profile.Profile) error {
	// Marshal the entire profile to JSON
	profileDataJSON, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshal profile: %w", err)
	}

	query := `
		INSERT INTO profiles (name, display_name, description, platform, cluster_type, track, enabled, profile_data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (name) DO UPDATE SET
			display_name = EXCLUDED.display_name,
			description = EXCLUDED.description,
			platform = EXCLUDED.platform,
			cluster_type = EXCLUDED.cluster_type,
			track = EXCLUDED.track,
			enabled = EXCLUDED.enabled,
			profile_data = EXCLUDED.profile_data,
			updated_at = NOW()
	`

	_, err = s.pool.Exec(ctx, query,
		p.Name,
		p.DisplayName,
		p.Description,
		p.Platform,
		p.ClusterType,
		p.Track,
		p.Enabled,
		profileDataJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to upsert profile: %w", err)
	}

	return nil
}

// DeleteProfile deletes a profile from the database
func (s *Store) DeleteProfile(ctx context.Context, name string) error {
	query := `DELETE FROM profiles WHERE name = $1`

	result, err := s.pool.Exec(ctx, query, name)
	if err != nil {
		return fmt.Errorf("failed to delete profile: %w", err)
	}

	if result.RowsAffected() == 0 {
		return fmt.Errorf("profile not found: %s", name)
	}

	return nil
}

// UpdateProfileVersions updates the version allowlist and default for a profile
func (s *Store) UpdateProfileVersions(ctx context.Context, name string, addVersions []string, removeVersions []string, newDefault *string) error {
	// Get current profile
	p, err := s.GetProfile(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to get profile: %w", err)
	}

	// Determine which version config to update
	var versionConfig *profile.VersionConfig
	if p.OpenshiftVersions != nil {
		versionConfig = p.OpenshiftVersions
	} else if p.KubernetesVersions != nil {
		versionConfig = p.KubernetesVersions
	} else {
		return fmt.Errorf("profile has no version configuration")
	}

	// Remove versions
	if len(removeVersions) > 0 {
		allowlist := []string{}
		removeMap := make(map[string]bool)
		for _, v := range removeVersions {
			removeMap[v] = true
		}
		for _, v := range versionConfig.Allowlist {
			if !removeMap[v] {
				allowlist = append(allowlist, v)
			}
		}
		versionConfig.Allowlist = allowlist
	}

	// Add versions
	if len(addVersions) > 0 {
		existingMap := make(map[string]bool)
		for _, v := range versionConfig.Allowlist {
			existingMap[v] = true
		}
		for _, v := range addVersions {
			if !existingMap[v] {
				versionConfig.Allowlist = append(versionConfig.Allowlist, v)
			}
		}
	}

	// Update default
	if newDefault != nil {
		versionConfig.Default = *newDefault
	}

	// Save the updated profile
	return s.UpsertProfile(ctx, p)
}
