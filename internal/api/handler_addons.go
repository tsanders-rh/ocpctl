package api

import (
	"log"

	"github.com/labstack/echo/v4"
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
