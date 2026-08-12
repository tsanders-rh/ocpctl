package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ClusterTemplateHandler handles per-user cluster-creation template endpoints
type ClusterTemplateHandler struct {
	store *store.Store
}

// NewClusterTemplateHandler creates a new cluster template handler
func NewClusterTemplateHandler(s *store.Store) *ClusterTemplateHandler {
	return &ClusterTemplateHandler{store: s}
}

// ClusterTemplateRequest is the body for creating or updating a cluster template.
// Config is a partial create-cluster payload captured from the creation form.
type ClusterTemplateRequest struct {
	Name   string          `json:"name" validate:"required,min=3,max=100"`
	Config json.RawMessage `json:"config" validate:"required"`
}

// sanitizeTemplateConfig ensures the config is a JSON object and strips any
// cluster "name" so a template can never carry a cluster name.
func sanitizeTemplateConfig(raw json.RawMessage) (json.RawMessage, bool) {
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, false
	}
	delete(obj, "name")

	cleaned, err := json.Marshal(obj)
	if err != nil {
		return nil, false
	}
	return cleaned, true
}

// Create handles POST /api/v1/cluster-templates
func (h *ClusterTemplateHandler) Create(c echo.Context) error {
	ctx := c.Request().Context()

	var req ClusterTemplateRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}
	if err := c.Validate(req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	config, ok := sanitizeTemplateConfig(req.Config)
	if !ok {
		return ErrorBadRequest(c, "config must be a JSON object")
	}

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	template := &types.ClusterTemplate{
		ID:        uuid.New().String(),
		Name:      req.Name,
		OwnerID:   userID,
		Config:    config,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := h.store.ClusterTemplates.Create(ctx, template); err != nil {
		if errors.Is(err, store.ErrTemplateLimitReached) {
			return ErrorBadRequest(c, fmt.Sprintf("You can save at most %d templates. Overwrite an existing template instead.", store.MaxTemplatesPerUser))
		}
		return LogAndReturnGenericError(c, err)
	}

	LogInfo(c, "cluster template created",
		"template_id", template.ID,
		"template_name", template.Name,
		"user_id", userID)

	return SuccessCreated(c, template)
}

// List handles GET /api/v1/cluster-templates
func (h *ClusterTemplateHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	templates, err := h.store.ClusterTemplates.List(ctx, userID)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, map[string]interface{}{
		"templates": templates,
	})
}

// Get handles GET /api/v1/cluster-templates/:id
func (h *ClusterTemplateHandler) Get(c echo.Context) error {
	ctx := c.Request().Context()

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	template, err := h.store.ClusterTemplates.GetByID(ctx, c.Param("id"), userID)
	if err != nil {
		return ErrorNotFound(c, "Template not found")
	}

	return SuccessOK(c, template)
}

// Update handles PATCH /api/v1/cluster-templates/:id
func (h *ClusterTemplateHandler) Update(c echo.Context) error {
	ctx := c.Request().Context()

	id := c.Param("id")

	var req ClusterTemplateRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}
	if err := c.Validate(req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	config, ok := sanitizeTemplateConfig(req.Config)
	if !ok {
		return ErrorBadRequest(c, "config must be a JSON object")
	}

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	existing, err := h.store.ClusterTemplates.GetByID(ctx, id, userID)
	if err != nil {
		return ErrorNotFound(c, "Template not found")
	}

	template := &types.ClusterTemplate{
		ID:        id,
		Name:      req.Name,
		OwnerID:   existing.OwnerID,
		Config:    config,
		CreatedAt: existing.CreatedAt,
		UpdatedAt: time.Now(),
	}

	if err := h.store.ClusterTemplates.Update(ctx, template); err != nil {
		return LogAndReturnGenericError(c, err)
	}

	LogInfo(c, "cluster template updated",
		"template_id", template.ID,
		"template_name", template.Name,
		"user_id", userID)

	return SuccessOK(c, template)
}

// Delete handles DELETE /api/v1/cluster-templates/:id
func (h *ClusterTemplateHandler) Delete(c echo.Context) error {
	ctx := c.Request().Context()

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	if err := h.store.ClusterTemplates.Delete(ctx, c.Param("id"), userID); err != nil {
		return ErrorNotFound(c, "Template not found or access denied")
	}

	LogInfo(c, "cluster template deleted",
		"template_id", c.Param("id"),
		"user_id", userID)

	return SuccessOK(c, map[string]string{
		"message": "Template deleted successfully",
	})
}
