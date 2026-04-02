package api

import (
	"log"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// AddonsHandler handles HTTP requests for post-config add-ons
type AddonsHandler struct {
	store *store.Store
}

// NewAddonsHandler creates a new add-ons handler
func NewAddonsHandler(store *store.Store) *AddonsHandler {
	return &AddonsHandler{store: store}
}

// AddonWithVersions represents an add-on with all its versions
type AddonWithVersions struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	Description        string       `json:"description"`
	Category           string       `json:"category"`
	SupportedPlatforms []string     `json:"supportedPlatforms"`
	Enabled            bool         `json:"enabled"`
	Versions           VersionsInfo `json:"versions"`
}

// VersionsInfo contains version information
type VersionsInfo struct {
	Allowed []VersionOption `json:"allowed"`
	Default string          `json:"default"`
}

// VersionOption represents a single version option
type VersionOption struct {
	Channel     string `json:"channel"`
	DisplayName string `json:"displayName"`
}

// List returns all enabled add-ons, optionally filtered by category, platform, and search query
//
//	@Summary		List add-ons
//	@Description	Lists all enabled post-config add-ons with optional filtering by category, platform, and search
//	@Tags			post-config
//	@Produce		json
//	@Param			category	query		string	false	"Filter by category (backup, migration, cicd, monitoring, security, storage, networking)"
//	@Param			platform	query		string	false	"Filter by supported platform (openshift, eks, iks)"
//	@Param			search		query		string	false	"Search in name and description"
//	@Success		200			{object}	map[string]interface{}
//	@Failure		401			{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/addons [get]
func (h *AddonsHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	// Get query parameters
	category := c.QueryParam("category")
	platform := c.QueryParam("platform")
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
