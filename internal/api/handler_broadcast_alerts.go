package api

import (
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// BroadcastAlertHandler handles admin broadcast alert endpoints and the
// per-user active/acknowledge endpoints.
type BroadcastAlertHandler struct {
	store *store.Store
}

// NewBroadcastAlertHandler creates a new broadcast alert handler.
func NewBroadcastAlertHandler(s *store.Store) *BroadcastAlertHandler {
	return &BroadcastAlertHandler{store: s}
}

// CreateBroadcastAlertRequest is the body for creating a broadcast alert.
type CreateBroadcastAlertRequest struct {
	Title     string                       `json:"title" validate:"required,min=1,max=200"`
	Body      string                       `json:"body" validate:"required,min=1"`
	Severity  types.BroadcastAlertSeverity `json:"severity" validate:"required,oneof=info warning critical"`
	ExpiresAt *time.Time                   `json:"expiresAt,omitempty"`
}

// Create handles POST /api/v1/admin/broadcast-alerts (admin only)
//
//	@Summary		Create broadcast alert
//	@Description	Creates a broadcast alert shown to all users until acknowledged. Critical alerts render as a blocking modal; info/warning render as a dismissible banner.
//	@Tags			admin
//	@Accept			json
//	@Produce		json
//	@Param			alert	body		CreateBroadcastAlertRequest	true	"Alert to create"
//	@Success		201		{object}	types.BroadcastAlert
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/admin/broadcast-alerts [post]
func (h *BroadcastAlertHandler) Create(c echo.Context) error {
	ctx := c.Request().Context()

	var req CreateBroadcastAlertRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}
	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	alert := &types.BroadcastAlert{
		Title:     req.Title,
		Body:      req.Body,
		Severity:  req.Severity,
		CreatedBy: &userID,
		Active:    true,
		ExpiresAt: req.ExpiresAt,
	}

	if err := h.store.BroadcastAlerts.Create(ctx, alert); err != nil {
		return LogAndReturnGenericError(c, err)
	}

	LogInfo(c, "broadcast alert created",
		"alert_id", alert.ID,
		"severity", string(alert.Severity),
		"user_id", userID)

	return SuccessCreated(c, alert)
}

// ListAll handles GET /api/v1/admin/broadcast-alerts (admin only)
//
//	@Summary		List broadcast alerts
//	@Description	Returns all broadcast alerts (active and past) with per-alert acknowledgment counts.
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/admin/broadcast-alerts [get]
func (h *BroadcastAlertHandler) ListAll(c echo.Context) error {
	ctx := c.Request().Context()

	alerts, err := h.store.BroadcastAlerts.ListAll(ctx)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, map[string]interface{}{"alerts": alerts})
}

// Deactivate handles DELETE /api/v1/admin/broadcast-alerts/:id (admin only)
//
//	@Summary		Deactivate broadcast alert
//	@Description	Marks a broadcast alert inactive so it stops showing to users. Acknowledgment history is retained.
//	@Tags			admin
//	@Produce		json
//	@Param			id	path		string	true	"Alert ID"
//	@Success		200	{object}	map[string]string
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/admin/broadcast-alerts/{id} [delete]
func (h *BroadcastAlertHandler) Deactivate(c echo.Context) error {
	ctx := c.Request().Context()

	if err := h.store.BroadcastAlerts.Deactivate(ctx, c.Param("id")); err != nil {
		return ErrorNotFound(c, "Alert not found")
	}

	LogInfo(c, "broadcast alert deactivated", "alert_id", c.Param("id"))

	return SuccessOK(c, map[string]string{"message": "Alert deactivated"})
}

// ListActive handles GET /api/v1/broadcast-alerts/active (any authenticated user)
//
//	@Summary		List active alerts for current user
//	@Description	Returns active, non-expired broadcast alerts the current user has not yet acknowledged, most severe first.
//	@Tags			broadcast-alerts
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/broadcast-alerts/active [get]
func (h *BroadcastAlertHandler) ListActive(c echo.Context) error {
	ctx := c.Request().Context()

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	alerts, err := h.store.BroadcastAlerts.ListActiveForUser(ctx, userID)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, map[string]interface{}{"alerts": alerts})
}

// Acknowledge handles POST /api/v1/broadcast-alerts/:id/ack (any authenticated user)
//
//	@Summary		Acknowledge an alert
//	@Description	Records that the current user has acknowledged (dismissed) an alert. Idempotent.
//	@Tags			broadcast-alerts
//	@Produce		json
//	@Param			id	path		string	true	"Alert ID"
//	@Success		200	{object}	map[string]string
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/broadcast-alerts/{id}/ack [post]
func (h *BroadcastAlertHandler) Acknowledge(c echo.Context) error {
	ctx := c.Request().Context()

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	if err := h.store.BroadcastAlerts.Acknowledge(ctx, c.Param("id"), userID); err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, map[string]string{"message": "Acknowledged"})
}
