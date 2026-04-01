package api

import (
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/internal/validation"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TemplateHandler handles template-related API endpoints
type TemplateHandler struct {
	store *store.Store
}

// NewTemplateHandler creates a new template handler
func NewTemplateHandler(s *store.Store) *TemplateHandler {
	return &TemplateHandler{store: s}
}

// CreateTemplateRequest represents a request to create a template
type CreateTemplateRequest struct {
	Name        string                  `json:"name" validate:"required,min=3,max=100"`
	Description string                  `json:"description" validate:"max=500"`
	Config      *types.CustomPostConfig `json:"config" validate:"required"`
	IsPublic    bool                    `json:"isPublic"`
	Tags        []string                `json:"tags"`
}

// UpdateTemplateRequest represents a request to update a template
type UpdateTemplateRequest struct {
	Name        string                  `json:"name" validate:"required,min=3,max=100"`
	Description string                  `json:"description" validate:"max=500"`
	Config      *types.CustomPostConfig `json:"config" validate:"required"`
	IsPublic    bool                    `json:"isPublic"`
	Tags        []string                `json:"tags"`
}

// Create handles POST /api/v1/templates
//
//	@Summary		Create template
//	@Description	Creates a new post-configuration template for reuse
//	@Tags			templates
//	@Accept			json
//	@Produce		json
//	@Param			request	body		CreateTemplateRequest	true	"Template configuration"
//	@Success		201		{object}	types.PostConfigTemplate
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/templates [post]
func (h *TemplateHandler) Create(c echo.Context) error {
	ctx := c.Request().Context()

	var req CreateTemplateRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	if err := c.Validate(req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Validate the custom post-config
	if errs := validation.ValidateCustomPostConfig(req.Config); len(errs) > 0 {
		return ErrorBadRequest(c, errs[0].Error())
	}

	// Get authenticated user
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Create template
	template := &types.PostConfigTemplate{
		ID:          uuid.New().String(),
		Name:        req.Name,
		Description: req.Description,
		Config:      *req.Config,
		OwnerID:     userID,
		IsPublic:    req.IsPublic,
		Tags:        req.Tags,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := h.store.PostConfigTemplates.Create(ctx, template); err != nil {
		return LogAndReturnGenericError(c, err)
	}

	LogInfo(c, "template created",
		"template_id", template.ID,
		"template_name", template.Name,
		"user_id", userID)

	return SuccessCreated(c, template)
}

// List handles GET /api/v1/templates
//
//	@Summary		List templates
//	@Description	Lists post-configuration templates visible to the authenticated user
//	@Tags			templates
//	@Produce		json
//	@Param			public	query		bool	false	"Only show public templates"
//	@Param			tags	query		string	false	"Filter by tags (comma-separated)"
//	@Success		200		{object}	map[string]interface{}
//	@Failure		401		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/templates [get]
func (h *TemplateHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	// Get authenticated user
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	isAdmin := auth.IsAdmin(c)
	onlyPublic := c.QueryParam("public") == "true"

	var templates []*types.PostConfigTemplate

	// Check if filtering by tags
	tagsParam := c.QueryParam("tags")
	if tagsParam != "" {
		// Split tags by comma
		tags := []string{}
		for _, tag := range splitByComma(tagsParam) {
			if tag != "" {
				tags = append(tags, tag)
			}
		}
		templates, err = h.store.PostConfigTemplates.SearchByTags(ctx, tags, userID, isAdmin)
	} else {
		templates, err = h.store.PostConfigTemplates.List(ctx, userID, isAdmin, onlyPublic)
	}

	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return SuccessOK(c, map[string]interface{}{
		"templates": templates,
	})
}

// Get handles GET /api/v1/templates/:id
//
//	@Summary		Get template
//	@Description	Retrieves a specific template by ID
//	@Tags			templates
//	@Produce		json
//	@Param			id	path		string	true	"Template ID"
//	@Success		200	{object}	types.PostConfigTemplate
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/templates/{id} [get]
func (h *TemplateHandler) Get(c echo.Context) error {
	ctx := c.Request().Context()

	id := c.Param("id")

	// Get authenticated user
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	isAdmin := auth.IsAdmin(c)

	template, err := h.store.PostConfigTemplates.GetByID(ctx, id, userID, isAdmin)
	if err != nil {
		return ErrorNotFound(c, "Template not found")
	}

	return SuccessOK(c, template)
}

// Update handles PATCH /api/v1/templates/:id
//
//	@Summary		Update template
//	@Description	Updates a template (only owner can update)
//	@Tags			templates
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string					true	"Template ID"
//	@Param			request	body		UpdateTemplateRequest	true	"Updated template"
//	@Success		200		{object}	types.PostConfigTemplate
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/templates/{id} [patch]
func (h *TemplateHandler) Update(c echo.Context) error {
	ctx := c.Request().Context()

	id := c.Param("id")

	var req UpdateTemplateRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	if err := c.Validate(req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Validate the custom post-config
	if errs := validation.ValidateCustomPostConfig(req.Config); len(errs) > 0 {
		return ErrorBadRequest(c, errs[0].Error())
	}

	// Get authenticated user
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	isAdmin := auth.IsAdmin(c)

	// Get existing template to verify ownership
	existing, err := h.store.PostConfigTemplates.GetByID(ctx, id, userID, isAdmin)
	if err != nil {
		return ErrorNotFound(c, "Template not found")
	}

	// Only owner can update
	if existing.OwnerID != userID && !isAdmin {
		return ErrorForbidden(c, "Only the template owner can update it")
	}

	// Update template
	template := &types.PostConfigTemplate{
		ID:          id,
		Name:        req.Name,
		Description: req.Description,
		Config:      *req.Config,
		OwnerID:     existing.OwnerID,
		IsPublic:    req.IsPublic,
		Tags:        req.Tags,
		CreatedAt:   existing.CreatedAt,
		UpdatedAt:   time.Now(),
	}

	if err := h.store.PostConfigTemplates.Update(ctx, template); err != nil {
		return LogAndReturnGenericError(c, err)
	}

	LogInfo(c, "template updated",
		"template_id", template.ID,
		"template_name", template.Name,
		"user_id", userID)

	return SuccessOK(c, template)
}

// Delete handles DELETE /api/v1/templates/:id
//
//	@Summary		Delete template
//	@Description	Deletes a template (only owner or admin can delete)
//	@Tags			templates
//	@Produce		json
//	@Param			id	path		string	true	"Template ID"
//	@Success		200	{object}	map[string]string
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/templates/{id} [delete]
func (h *TemplateHandler) Delete(c echo.Context) error {
	ctx := c.Request().Context()

	id := c.Param("id")

	// Get authenticated user
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	isAdmin := auth.IsAdmin(c)

	if err := h.store.PostConfigTemplates.Delete(ctx, id, userID, isAdmin); err != nil {
		return ErrorNotFound(c, "Template not found or access denied")
	}

	LogInfo(c, "template deleted",
		"template_id", id,
		"user_id", userID)

	return SuccessOK(c, map[string]string{
		"message": "Template deleted successfully",
	})
}

// Helper function to split comma-separated string
func splitByComma(s string) []string {
	if s == "" {
		return []string{}
	}
	result := []string{}
	for _, part := range splitString(s, ',') {
		trimmed := trimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitString(s string, sep rune) []string {
	parts := []string{}
	current := ""
	for _, c := range s {
		if c == sep {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	parts = append(parts, current)
	return parts
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
