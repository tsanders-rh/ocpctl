package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ProfileUpdateHandler handles profile version update operations
type ProfileUpdateHandler struct {
	registry       *profile.Registry
	versionChecker *profile.VersionChecker
	updater        *profile.ProfileUpdater
	store          *store.Store
	profilesDir    string
}

// NewProfileUpdateHandler creates a new profile update handler
func NewProfileUpdateHandler(registry *profile.Registry, store *store.Store, profilesDir string) *ProfileUpdateHandler {
	return &ProfileUpdateHandler{
		registry:       registry,
		versionChecker: profile.NewVersionChecker(),
		updater:        profile.NewProfileUpdater(profilesDir),
		store:          store,
		profilesDir:    profilesDir,
	}
}

// CheckVersionsResponse represents the response for version check
type CheckVersionsResponse struct {
	ProfilesWithUpdates []profile.ProfileVersionStatus `json:"profiles_with_updates"`
	TotalProfiles       int                            `json:"total_profiles"`
	UpdatesAvailable    int                            `json:"updates_available"`
	CacheAge            string                         `json:"cache_age"`
	LastChecked         time.Time                      `json:"last_checked"`
}

// UpdateVersionsRequest represents the request to update profile versions
type UpdateVersionsRequest struct {
	OpenshiftVersions  []string `json:"openshift_versions,omitempty"`
	KubernetesVersions []string `json:"kubernetes_versions,omitempty"`
	DryRun             bool     `json:"dry_run,omitempty"`
}

// UpdateVersionsResponse represents the response after updating versions
type UpdateVersionsResponse struct {
	Success       bool      `json:"success"`
	ProfileName   string    `json:"profile_name"`
	BackupPath    string    `json:"backup_path,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
	AuditEventID  string    `json:"audit_event_id,omitempty"`
	DryRun        bool      `json:"dry_run,omitempty"`
	PreviewProfile *profile.Profile `json:"preview_profile,omitempty"`
}

// ReloadProfilesResponse represents the response after reloading profiles
type ReloadProfilesResponse struct {
	Success        bool      `json:"success"`
	ProfilesLoaded int       `json:"profiles_loaded"`
	ReloadedAt     time.Time `json:"reloaded_at"`
}

// HandleCheckVersions checks all profiles for available version updates
// GET /api/v1/admin/profiles/version-check
func (h *ProfileUpdateHandler) HandleCheckVersions(c echo.Context) error {
	ctx := c.Request().Context()

	// Optional: force cache refresh
	refresh := c.QueryParam("refresh") == "true"
	if refresh {
		if err := h.versionChecker.RefreshCache(ctx); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to refresh version cache: %v", err))
		}
	}

	// Get all enabled profiles
	profiles := h.registry.List()

	// Check each profile for updates in parallel
	type result struct {
		status *profile.ProfileVersionStatus
		err    error
	}

	results := make(chan result, len(profiles))

	for _, prof := range profiles {
		go func(p *profile.Profile) {
			status, err := h.versionChecker.CheckProfileUpdates(ctx, p)
			results <- result{status: status, err: err}
		}(prof)
	}

	// Collect results
	profilesWithUpdates := []profile.ProfileVersionStatus{}
	totalProfiles := len(profiles)
	updatesAvailable := 0

	for i := 0; i < len(profiles); i++ {
		res := <-results
		if res.err != nil {
			// Log error but continue with other profiles
			fmt.Printf("Warning: failed to check updates for profile: %v\n", res.err)
			continue
		}

		if res.status != nil && res.status.UpdateCount > 0 {
			profilesWithUpdates = append(profilesWithUpdates, *res.status)
			updatesAvailable++
		}
	}

	response := CheckVersionsResponse{
		ProfilesWithUpdates: profilesWithUpdates,
		TotalProfiles:       totalProfiles,
		UpdatesAvailable:    updatesAvailable,
		CacheAge:            h.versionChecker.GetCacheAge().String(),
		LastChecked:         time.Now(),
	}

	return c.JSON(http.StatusOK, response)
}

// HandleUpdateVersions updates version allowlists for a specific profile
// POST /api/v1/admin/profiles/:name/update-versions
func (h *ProfileUpdateHandler) HandleUpdateVersions(c echo.Context) error {
	ctx := c.Request().Context()
	profileName := c.Param("name")

	// Parse request
	var req UpdateVersionsRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Invalid request: %v", err))
	}

	// Validate that at least one version list is provided
	if len(req.OpenshiftVersions) == 0 && len(req.KubernetesVersions) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "At least one version list must be provided")
	}

	// Get current user for audit logging
	user, err := h.getCurrentUser(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Failed to get current user")
	}

	// Prepare version update
	updates := &profile.VersionUpdate{
		OpenshiftVersions:  req.OpenshiftVersions,
		KubernetesVersions: req.KubernetesVersions,
	}

	// Dry run mode - validate without writing
	if req.DryRun {
		previewProfile, err := h.updater.DryRun(profileName, updates)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Dry run validation failed: %v", err))
		}

		return c.JSON(http.StatusOK, UpdateVersionsResponse{
			Success:        true,
			ProfileName:    profileName,
			UpdatedAt:      time.Now(),
			DryRun:         true,
			PreviewProfile: previewProfile,
		})
	}

	// Perform actual update
	backupPath, err := h.updater.UpdateVersions(profileName, updates)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to update profile: %v", err))
	}

	// Reload profile registry to pick up changes
	if err := h.registry.Reload(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Profile updated but registry reload failed: %v", err))
	}

	// Create audit event
	auditEvent := &types.AuditEvent{
		Actor:  user.ID,
		Action: fmt.Sprintf("profile_update:%s", profileName),
		Status: types.AuditEventStatusSuccess,
		Metadata: types.JobMetadata{
			"openshift_versions":  req.OpenshiftVersions,
			"kubernetes_versions": req.KubernetesVersions,
			"backup_path":         backupPath,
		},
	}

	if err := h.store.Audit.Log(ctx, auditEvent); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Warning: failed to create audit event: %v\n", err)
	}

	// Sync to S3 if configured (for multi-node deployment)
	go h.syncProfileToS3(profileName)

	response := UpdateVersionsResponse{
		Success:      true,
		ProfileName:  profileName,
		BackupPath:   backupPath,
		UpdatedAt:    time.Now(),
		AuditEventID: auditEvent.ID,
		DryRun:       false,
	}

	return c.JSON(http.StatusOK, response)
}

// HandleReloadProfiles forces a reload of the profile registry
// POST /api/v1/admin/profiles/reload
func (h *ProfileUpdateHandler) HandleReloadProfiles(c echo.Context) error {
	ctx := c.Request().Context()

	// Reload profile registry
	if err := h.registry.Reload(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to reload profiles: %v", err))
	}

	// Get count of loaded profiles
	profiles := h.registry.List()
	profileCount := len(profiles)

	// Get current user for audit logging
	user, err := h.getCurrentUser(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Failed to get current user")
	}

	// Create audit event
	auditEvent := &types.AuditEvent{
		Actor:  user.ID,
		Action: "profile_reload",
		Status: types.AuditEventStatusSuccess,
		Metadata: types.JobMetadata{
			"profiles_loaded": profileCount,
		},
	}

	if err := h.store.Audit.Log(ctx, auditEvent); err != nil {
		// Log error but don't fail the request
		fmt.Printf("Warning: failed to create audit event: %v\n", err)
	}

	response := ReloadProfilesResponse{
		Success:        true,
		ProfilesLoaded: profileCount,
		ReloadedAt:     time.Now(),
	}

	return c.JSON(http.StatusOK, response)
}

// HandleRollbackProfile rolls back a profile to the latest backup
// POST /api/v1/admin/profiles/:name/rollback
func (h *ProfileUpdateHandler) HandleRollbackProfile(c echo.Context) error {
	ctx := c.Request().Context()
	profileName := c.Param("name")

	// Get current user for audit logging
	user, err := h.getCurrentUser(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Failed to get current user")
	}

	// Perform rollback
	if err := h.updater.Rollback(profileName); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to rollback profile: %v", err))
	}

	// Reload profile registry
	if err := h.registry.Reload(); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Profile rolled back but registry reload failed: %v", err))
	}

	// Create audit event
	auditEvent := &types.AuditEvent{
		Actor:  user.ID,
		Action: fmt.Sprintf("profile_rollback:%s", profileName),
		Status: types.AuditEventStatusSuccess,
		Metadata: types.JobMetadata{
			"rolled_back_at": time.Now(),
		},
	}

	if err := h.store.Audit.Log(ctx, auditEvent); err != nil {
		fmt.Printf("Warning: failed to create audit event: %v\n", err)
	}

	// Sync to S3
	go h.syncProfileToS3(profileName)

	return c.JSON(http.StatusOK, map[string]interface{}{
		"success":      true,
		"profile_name": profileName,
		"rolled_back_at": time.Now(),
	})
}

// getCurrentUser extracts the current user from the context
func (h *ProfileUpdateHandler) getCurrentUser(c echo.Context) (*types.User, error) {
	user, ok := c.Get("user").(*types.User)
	if !ok || user == nil {
		return nil, fmt.Errorf("user not found in context")
	}
	return user, nil
}

// syncProfileToS3 syncs a profile to S3 for multi-node deployment
func (h *ProfileUpdateHandler) syncProfileToS3(profileName string) {
	// TODO: Implement S3 sync
	// This should upload the updated profile YAML to s3://ocpctl-binaries/profiles/
	// For now, this is a placeholder that logs the intent
	fmt.Printf("TODO: Sync profile %s to S3 for worker propagation\n", profileName)
}
