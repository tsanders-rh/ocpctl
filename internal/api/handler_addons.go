package api

import (
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// AddonsHandler handles HTTP requests for post-config add-ons
type AddonsHandler struct {
	store    *store.Store
	registry *profile.Registry
}

// NewAddonsHandler creates a new add-ons handler
func NewAddonsHandler(store *store.Store, registry *profile.Registry) *AddonsHandler {
	return &AddonsHandler{
		store:    store,
		registry: registry,
	}
}

// AddonWithVersions represents an add-on with all its versions
type AddonWithVersions struct {
	ID                 string       `json:"id" example:"oadp"`
	Name               string       `json:"name" example:"OpenShift API for Data Protection (OADP)"`
	Description        string       `json:"description" example:"Backup and restore OpenShift clusters and applications"`
	Category           string       `json:"category" example:"backup"`
	SupportedPlatforms []string     `json:"supportedPlatforms" example:"openshift"`
	Enabled            bool         `json:"enabled" example:"true"`
	Versions           VersionsInfo `json:"versions"`
}

// VersionsInfo contains version information
type VersionsInfo struct {
	Allowed []VersionOption `json:"allowed"`
	Default string          `json:"default" example:"stable"`
}

// VersionOption represents a single version option
type VersionOption struct {
	Channel     string `json:"channel" example:"stable"`
	DisplayName string `json:"displayName" example:"OADP 1.5 (Stable)"`
}

// AddonsListResponse represents the response from the list addons endpoint
type AddonsListResponse struct {
	Addons     []AddonWithVersions            `json:"addons"`
	Categories map[string][]AddonWithVersions `json:"categories"`
	Total      int                            `json:"total" example:"3"`
}

// List returns all enabled add-ons, optionally filtered by category, platform, profile capabilities, and search query
//
//	@Summary		List add-ons
//	@Description	Lists all enabled post-config add-ons with version information. Supports filtering by category, platform, profile capabilities, and search query. Each add-on includes multiple versions with one marked as default.
//	@Tags			post-config
//	@Produce		json
//	@Param			category	query		string	false	"Filter by category (backup, migration, cicd, monitoring, security, storage, networking, virtualization)"
//	@Param			platform	query		string	false	"Filter by supported platform (openshift, eks, iks)"
//	@Param			profile		query		string	false	"Filter by profile capabilities (e.g., aws-minimal)"
//	@Param			search		query		string	false	"Search in name and description"
//	@Success		200			{object}	AddonsListResponse
//	@Failure		401			{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/addons [get]
func (h *AddonsHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	// Get query parameters
	category := c.QueryParam("category")
	platform := c.QueryParam("platform")
	profileName := c.QueryParam("profile")
	search := c.QueryParam("search")

	var categoryPtr *string
	var platformPtr *string

	if category != "" {
		categoryPtr = &category
	}

	if platform != "" {
		platformPtr = &platform
	}

	// Retrieve add-ons from database
	addons, err := h.store.PostConfigAddons.List(ctx, categoryPtr, platformPtr)
	if err != nil {
		log.Printf("Error listing add-ons: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	// Filter by profile capabilities if profile parameter is provided
	if profileName != "" {
		prof, err := h.registry.GetAny(profileName)
		if err != nil {
			log.Printf("Warning: failed to get profile %s: %v", profileName, err)
			// Don't fail the request, just skip capability filtering
		} else {
			// Filter addons based on profile capabilities
			// If profile has no metadata or no capabilities, treat as empty capability list
			filteredAddons := make([]types.PostConfigAddon, 0)
			var profileCapabilities []string
			if prof.Metadata != nil {
				profileCapabilities = prof.Metadata.Capabilities
			}

			for _, addon := range addons {
				// If addon has no metadata or no requirements, include it
				if addon.Metadata == nil {
					filteredAddons = append(filteredAddons, addon)
					continue
				}

				// Check if addon requires bare metal
				if addon.Metadata.RequiresBareMetal {
					// Profile must have "bare-metal" capability
					if !hasCapability(profileCapabilities, "bare-metal") {
						log.Printf("Filtering out addon %s: requires bare-metal but profile %s doesn't have it", addon.AddonID, profileName)
						continue
					}
				}

				// Check if addon has specific required capabilities
				if len(addon.Metadata.RequiredCapabilities) > 0 {
					hasAllCapabilities := true
					for _, requiredCap := range addon.Metadata.RequiredCapabilities {
						if !hasCapability(profileCapabilities, requiredCap) {
							hasAllCapabilities = false
							break
						}
					}
					if !hasAllCapabilities {
						log.Printf("Filtering out addon %s: missing required capabilities for profile %s", addon.AddonID, profileName)
						continue
					}
				}

				// Addon meets all requirements
				filteredAddons = append(filteredAddons, addon)
			}
			addons = filteredAddons
		}
	}

	// Client-side search filtering if search parameter is provided
	if search != "" {
		filteredAddons := make([]types.PostConfigAddon, 0)
		searchLower := toLower(search)
		for _, addon := range addons {
			if contains(toLower(addon.Name), searchLower) || contains(toLower(addon.Description), searchLower) {
				filteredAddons = append(filteredAddons, addon)
			}
		}
		addons = filteredAddons
	}

	// Group by addon_id and collect versions
	grouped := make(map[string]*AddonWithVersions)
	for _, addon := range addons {
		if _, exists := grouped[addon.AddonID]; !exists {
			grouped[addon.AddonID] = &AddonWithVersions{
				ID:                 addon.AddonID,
				Name:               addon.Name,
				Description:        addon.Description,
				Category:           addon.Category,
				SupportedPlatforms: addon.SupportedPlatforms,
				Enabled:            addon.Enabled,
				Versions: VersionsInfo{
					Allowed: []VersionOption{},
				},
			}
		}

		grouped[addon.AddonID].Versions.Allowed = append(
			grouped[addon.AddonID].Versions.Allowed,
			VersionOption{
				Channel:     addon.Version,
				DisplayName: addon.DisplayName,
			},
		)

		if addon.IsDefault {
			grouped[addon.AddonID].Versions.Default = addon.Version
		}
	}

	// Convert to array
	result := make([]AddonWithVersions, 0, len(grouped))
	for _, addon := range grouped {
		result = append(result, *addon)
	}

	// Group by category for UI display
	categories := make(map[string][]AddonWithVersions)
	for _, addon := range result {
		categories[addon.Category] = append(categories[addon.Category], addon)
	}

	return c.JSON(200, map[string]interface{}{
		"addons":     result,
		"categories": categories,
		"total":      len(result),
	})
}

// Helper functions for search
func toLower(s string) string {
	result := ""
	for _, c := range s {
		if c >= 'A' && c <= 'Z' {
			result += string(c + 32)
		} else {
			result += string(c)
		}
	}
	return result
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// hasCapability checks if a capability exists in the capabilities list
func hasCapability(capabilities []string, capability string) bool {
	for _, cap := range capabilities {
		if cap == capability {
			return true
		}
	}
	return false
}

// GetByID returns a specific addon by its database ID
//
//	@Summary		Get addon by ID
//	@Description	Retrieves a specific addon by its database ID. System addons are accessible to all users. User addons are only accessible to their creator or admins.
//	@Tags			post-config
//	@Produce		json
//	@Param			id	path		string	true	"Addon database ID (UUID)"
//	@Success		200	{object}	types.PostConfigAddon
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/addons/{id} [get]
func (h *AddonsHandler) GetByID(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	addon, err := h.store.PostConfigAddons.GetByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrorNotFound(c, "Addon not found")
		}
		log.Printf("Error retrieving addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	// Check access: system addons are public, user addons require ownership or admin
	if addon.AddonSource == "user" {
		userID, err := auth.GetUserID(c)
		if err != nil {
			return err
		}

		// Allow access if user is the creator or is an admin
		if addon.CreatedByUserID == nil || *addon.CreatedByUserID != userID {
			if !auth.IsAdmin(c) {
				return ErrorForbidden(c, "You do not have access to this addon")
			}
		}
	}

	return c.JSON(200, addon)
}

// CreateAddonRequest represents the request to create a new user addon
type CreateAddonRequest struct {
	AddonID            string              `json:"addonId" validate:"required,min=1,max=100"`
	Name               string              `json:"name" validate:"required,min=1,max=200"`
	Description        string              `json:"description" validate:"required"`
	Category           string              `json:"category" validate:"required,oneof=backup migration cicd monitoring security storage networking virtualization"`
	Config             types.CustomPostConfig `json:"config" validate:"required"`
	SupportedPlatforms []string            `json:"supportedPlatforms" validate:"required,min=1"`
	Version            string              `json:"version" validate:"required"`
	DisplayName        string              `json:"displayName" validate:"required"`
	IsDefault          bool                `json:"isDefault"`
	Metadata           *types.AddonMetadata `json:"metadata,omitempty"`
}

// Create creates a new user addon
//
//	@Summary		Create user addon
//	@Description	Creates a new user-defined addon. The addon is created as a draft (unpublished) and can be edited.
//	@Tags			post-config
//	@Accept			json
//	@Produce		json
//	@Param			addon	body		CreateAddonRequest	true	"Addon creation request"
//	@Success		201		{object}	types.PostConfigAddon
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/addons [post]
func (h *AddonsHandler) Create(c echo.Context) error {
	ctx := c.Request().Context()

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	var req CreateAddonRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Create addon object
	addon := &types.PostConfigAddon{
		ID:                 uuid.New().String(),
		AddonID:            req.AddonID,
		Name:               req.Name,
		Description:        req.Description,
		Category:           req.Category,
		Config:             req.Config,
		SupportedPlatforms: req.SupportedPlatforms,
		Enabled:            true,
		Version:            req.Version,
		DisplayName:        req.DisplayName,
		IsDefault:          req.IsDefault,
		Metadata:           req.Metadata,
		AddonSource:        "user",
		CreatedByUserID:    &userID,
		IsPublished:        false,
		VersionNumber:      1,
		IsImmutable:        false,
	}

	if err := h.store.PostConfigAddons.Create(ctx, addon); err != nil {
		log.Printf("Error creating addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(201, addon)
}

// UpdateAddonRequest represents the request to update an addon
type UpdateAddonRequest struct {
	Name               *string              `json:"name,omitempty" validate:"omitempty,min=1,max=200"`
	Description        *string              `json:"description,omitempty"`
	Category           *string              `json:"category,omitempty" validate:"omitempty,oneof=backup migration cicd monitoring security storage networking virtualization"`
	Config             *types.CustomPostConfig `json:"config,omitempty"`
	SupportedPlatforms []string             `json:"supportedPlatforms,omitempty" validate:"omitempty,min=1"`
	Version            *string              `json:"version,omitempty"`
	DisplayName        *string              `json:"displayName,omitempty"`
	IsDefault          *bool                `json:"isDefault,omitempty"`
	Metadata           *types.AddonMetadata `json:"metadata,omitempty"`
}

// Update updates an existing user addon (draft only)
//
//	@Summary		Update user addon
//	@Description	Updates a user addon. Only unpublished (draft) addons can be updated. Published addons are immutable.
//	@Tags			post-config
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Addon database ID"
//	@Param			addon	body		UpdateAddonRequest	true	"Addon update request"
//	@Success		200		{object}	types.PostConfigAddon
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/addons/{id} [put]
func (h *AddonsHandler) Update(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Get existing addon
	existing, err := h.store.PostConfigAddons.GetByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrorNotFound(c, "Addon not found")
		}
		log.Printf("Error retrieving addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	// Check ownership
	if existing.AddonSource != "user" {
		return ErrorForbidden(c, "Cannot update system addons")
	}

	if existing.CreatedByUserID == nil || *existing.CreatedByUserID != userID {
		if !auth.IsAdmin(c) {
			return ErrorForbidden(c, "You do not have access to this addon")
		}
	}

	// Check if addon is published (immutable)
	if existing.IsPublished {
		return ErrorBadRequest(c, "Cannot update published addons. Clone the addon to create a new version.")
	}

	var req UpdateAddonRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Apply updates
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.Category != nil {
		existing.Category = *req.Category
	}
	if req.Config != nil {
		existing.Config = *req.Config
	}
	if req.SupportedPlatforms != nil {
		existing.SupportedPlatforms = req.SupportedPlatforms
	}
	if req.Version != nil {
		existing.Version = *req.Version
	}
	if req.DisplayName != nil {
		existing.DisplayName = *req.DisplayName
	}
	if req.IsDefault != nil {
		existing.IsDefault = *req.IsDefault
	}
	if req.Metadata != nil {
		existing.Metadata = req.Metadata
	}

	if err := h.store.PostConfigAddons.Update(ctx, id, existing); err != nil {
		log.Printf("Error updating addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(200, existing)
}

// Delete deletes a user addon
//
//	@Summary		Delete user addon
//	@Description	Deletes a user addon. Only the creator or admins can delete an addon.
//	@Tags			post-config
//	@Produce		json
//	@Param			id	path		string	true	"Addon database ID"
//	@Success		204	"No Content"
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/addons/{id} [delete]
func (h *AddonsHandler) Delete(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Get existing addon
	existing, err := h.store.PostConfigAddons.GetByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrorNotFound(c, "Addon not found")
		}
		log.Printf("Error retrieving addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	// Check ownership
	if existing.AddonSource != "user" {
		return ErrorForbidden(c, "Cannot delete system addons")
	}

	if existing.CreatedByUserID == nil || *existing.CreatedByUserID != userID {
		if !auth.IsAdmin(c) {
			return ErrorForbidden(c, "You do not have access to this addon")
		}
	}

	if err := h.store.PostConfigAddons.Delete(ctx, id); err != nil {
		log.Printf("Error deleting addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	return c.NoContent(204)
}

// Publish publishes a user addon, making it immutable
//
//	@Summary		Publish user addon
//	@Description	Publishes a user addon, making it immutable. Once published, the addon cannot be edited.
//	@Tags			post-config
//	@Produce		json
//	@Param			id	path		string	true	"Addon database ID"
//	@Success		200	{object}	types.PostConfigAddon
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/addons/{id}/publish [post]
func (h *AddonsHandler) Publish(c echo.Context) error {
	ctx := c.Request().Context()
	id := c.Param("id")

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Get existing addon
	existing, err := h.store.PostConfigAddons.GetByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrorNotFound(c, "Addon not found")
		}
		log.Printf("Error retrieving addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	// Check ownership
	if existing.AddonSource != "user" {
		return ErrorForbidden(c, "Cannot publish system addons")
	}

	if existing.CreatedByUserID == nil || *existing.CreatedByUserID != userID {
		if !auth.IsAdmin(c) {
			return ErrorForbidden(c, "You do not have access to this addon")
		}
	}

	if existing.IsPublished {
		return ErrorBadRequest(c, "Addon is already published")
	}

	if err := h.store.PostConfigAddons.PublishAddon(ctx, id); err != nil {
		log.Printf("Error publishing addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	// Retrieve updated addon
	updated, err := h.store.PostConfigAddons.GetByID(ctx, id)
	if err != nil {
		log.Printf("Error retrieving updated addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(200, updated)
}

// Clone clones an addon to create a new version
//
//	@Summary		Clone addon
//	@Description	Clones an existing addon to create a new draft version. Increments version number and creates a version lineage link.
//	@Tags			post-config
//	@Produce		json
//	@Param			id	path		string	true	"Addon database ID to clone from"
//	@Success		201	{object}	types.PostConfigAddon
//	@Failure		401	{object}	ErrorResponse
//	@Failure		403	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/addons/{id}/clone [post]
func (h *AddonsHandler) Clone(c echo.Context) error {
	ctx := c.Request().Context()
	parentID := c.Param("id")

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Verify parent exists
	parent, err := h.store.PostConfigAddons.GetByID(ctx, parentID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrorNotFound(c, "Parent addon not found")
		}
		log.Printf("Error retrieving parent addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	// Check access to parent
	if parent.AddonSource == "user" {
		if parent.CreatedByUserID == nil || *parent.CreatedByUserID != userID {
			if !auth.IsAdmin(c) {
				return ErrorForbidden(c, "You do not have access to this addon")
			}
		}
	}

	cloned, err := h.store.PostConfigAddons.CloneAddon(ctx, parentID, userID)
	if err != nil {
		log.Printf("Error cloning addon: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(201, cloned)
}

// ListUserAddons returns all addons created by the current user
//
//	@Summary		List user's custom addons
//	@Description	Returns all addons created by the current user, including drafts and published versions.
//	@Tags			post-config
//	@Produce		json
//	@Success		200	{array}		types.PostConfigAddon
//	@Failure		401	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/addons/my [get]
func (h *AddonsHandler) ListUserAddons(c echo.Context) error {
	ctx := c.Request().Context()

	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	addons, err := h.store.PostConfigAddons.GetUserAddons(ctx, userID)
	if err != nil {
		log.Printf("Error listing user addons: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(200, addons)
}
