package api

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/orphan"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// AutoRemediationResponse is the payload for GET
// /admin/orphaned-resources/auto-remediation.
//
// Settings is the console-configured override (nil when the console has never
// set it -- in that case the worker's ORPHAN_AUTO_DELETE* env bootstrap default
// is in force, which the API cannot read since it runs on a different host).
// Status is the janitor's last-cycle telemetry; its Mode field reflects the
// EFFECTIVE mode the janitor actually ran with, so the console shows reality even
// when Settings is nil.
type AutoRemediationResponse struct {
	Settings   *types.OrphanAutoRemediationSettings `json:"settings"`
	Configured bool                                 `json:"configured"`
	Status     *types.OrphanAutoRemediationStatus   `json:"status"`
}

// UpdateAutoRemediationRequest is the body for PUT
// /admin/orphaned-resources/auto-remediation.
type UpdateAutoRemediationRequest struct {
	Mode        string `json:"mode"`
	MaxPerCycle int    `json:"maxPerCycle"`
}

// maxAutoRemediationPerCycle bounds the per-cycle deletion cap an admin can set,
// keeping the blast radius sane even under a fat-finger.
const maxAutoRemediationPerCycle = 500

// GetAutoRemediation handles GET /api/v1/admin/orphaned-resources/auto-remediation
//
//	@Summary		Get orphaned-resource auto-remediation config and status
//	@Description	Returns the console-configured auto-remediation settings (if any) and the janitor's last-cycle telemetry. Admin only.
//	@Tags			Orphaned Resources
//	@Produce		json
//	@Success		200	{object}	AutoRemediationResponse
//	@Failure		500	{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/admin/orphaned-resources/auto-remediation [get]
func (h *OrphanedResourceHandler) GetAutoRemediation(c echo.Context) error {
	ctx := c.Request().Context()

	resp := AutoRemediationResponse{}

	rawCfg, err := h.store.SystemSettings.Get(ctx, types.SettingOrphanAutoRemediation)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to read auto-remediation setting: %w", err))
	}
	if rawCfg != nil {
		var cfg types.OrphanAutoRemediationSettings
		if err := json.Unmarshal(rawCfg, &cfg); err != nil {
			return LogAndReturnGenericError(c, fmt.Errorf("failed to parse auto-remediation setting: %w", err))
		}
		resp.Settings = &cfg
		resp.Configured = true
	}

	rawStatus, err := h.store.SystemSettings.Get(ctx, types.SettingOrphanAutoRemediationStatus)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to read auto-remediation status: %w", err))
	}
	if rawStatus != nil {
		var status types.OrphanAutoRemediationStatus
		if err := json.Unmarshal(rawStatus, &status); err != nil {
			return LogAndReturnGenericError(c, fmt.Errorf("failed to parse auto-remediation status: %w", err))
		}
		resp.Status = &status
	}

	return c.JSON(200, resp)
}

// UpdateAutoRemediation handles PUT /api/v1/admin/orphaned-resources/auto-remediation
//
//	@Summary		Update orphaned-resource auto-remediation config
//	@Description	Sets the janitor's auto-remediation mode (off|dryrun|on) and per-cycle deletion cap. The DB value overrides the worker's env bootstrap and takes effect within one janitor cycle (no restart). Admin only.
//	@Tags			Orphaned Resources
//	@Accept			json
//	@Produce		json
//	@Param			body	body		UpdateAutoRemediationRequest	true	"New settings"
//	@Success		200		{object}	AutoRemediationResponse
//	@Failure		400		{object}	map[string]string
//	@Failure		500		{object}	map[string]string
//	@Security		BearerAuth
//	@Router			/admin/orphaned-resources/auto-remediation [put]
func (h *OrphanedResourceHandler) UpdateAutoRemediation(c echo.Context) error {
	ctx := c.Request().Context()

	var req UpdateAutoRemediationRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	// Validate mode strictly to the canonical set (no aliases persisted).
	mode, ok := orphan.ParseAutoDeleteMode(req.Mode)
	if !ok || (req.Mode != string(orphan.AutoDeleteOff) &&
		req.Mode != string(orphan.AutoDeleteDryRun) &&
		req.Mode != string(orphan.AutoDeleteOn)) {
		return ErrorBadRequest(c, "mode must be one of: off, dryrun, on")
	}
	if req.MaxPerCycle <= 0 || req.MaxPerCycle > maxAutoRemediationPerCycle {
		return ErrorBadRequest(c, fmt.Sprintf("maxPerCycle must be between 1 and %d", maxAutoRemediationPerCycle))
	}

	userEmail := "unknown"
	if user := c.Get("user"); user != nil {
		if u, ok := user.(*types.User); ok {
			userEmail = u.Email
		}
	}

	// Capture the previous value for the audit trail (best-effort).
	var previous *types.OrphanAutoRemediationSettings
	if rawOld, err := h.store.SystemSettings.Get(ctx, types.SettingOrphanAutoRemediation); err == nil && rawOld != nil {
		var old types.OrphanAutoRemediationSettings
		if json.Unmarshal(rawOld, &old) == nil {
			previous = &old
		}
	}

	newSettings := types.OrphanAutoRemediationSettings{
		Mode:        string(mode),
		MaxPerCycle: req.MaxPerCycle,
	}
	raw, err := json.Marshal(newSettings)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to encode auto-remediation setting: %w", err))
	}
	if err := h.store.SystemSettings.Upsert(ctx, types.SettingOrphanAutoRemediation, raw, userEmail); err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to save auto-remediation setting: %w", err))
	}

	h.auditAutoRemediationChange(c, userEmail, previous, newSettings)

	// Return the fresh state (config + last-known status).
	return h.GetAutoRemediation(c)
}

// auditAutoRemediationChange records an audit event for an auto-remediation
// config change. Fire-and-forget: audit failures never fail the request.
func (h *OrphanedResourceHandler) auditAutoRemediationChange(c echo.Context, actor string, previous *types.OrphanAutoRemediationSettings, next types.OrphanAutoRemediationSettings) {
	ip := c.RealIP()
	userAgent := c.Request().UserAgent()

	metadata := types.JobMetadata{
		"new_mode":          next.Mode,
		"new_max_per_cycle": next.MaxPerCycle,
	}
	if previous != nil {
		metadata["old_mode"] = previous.Mode
		metadata["old_max_per_cycle"] = previous.MaxPerCycle
	}

	event := &types.AuditEvent{
		ID:        uuid.New().String(),
		Actor:     actor,
		Action:    "orphaned_resource.auto_remediation_config_changed",
		Status:    types.AuditEventStatusSuccess,
		Metadata:  metadata,
		IPAddress: &ip,
		UserAgent: &userAgent,
		CreatedAt: time.Now(),
	}

	_ = h.store.Audit.Log(c.Request().Context(), event)
}
