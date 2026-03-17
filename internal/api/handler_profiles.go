package api

import (
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ProfileHandler handles profile-related API endpoints
type ProfileHandler struct {
	registry *profile.Registry
}

// NewProfileHandler creates a new profile handler
func NewProfileHandler(registry *profile.Registry) *ProfileHandler {
	return &ProfileHandler{
		registry: registry,
	}
}

// ProfileResponse represents a profile in API responses
type ProfileResponse struct {
	Name               string                       `json:"name"`
	DisplayName        string                       `json:"display_name"`
	Description        string                       `json:"description"`
	Platform           string                       `json:"platform"`
	Enabled            bool                         `json:"enabled"`
	OpenshiftVersions  profile.VersionConfig        `json:"openshift_versions"`
	Regions            profile.RegionConfig         `json:"regions"`
	BaseDomains        profile.BaseDomainConfig     `json:"base_domains"`
	Compute            profile.ComputeConfig        `json:"compute"`
	Lifecycle          profile.LifecycleConfig      `json:"lifecycle"`
	Networking         *profile.NetworkingConfig    `json:"networking,omitempty"`
	Tags               profile.TagsConfig           `json:"tags"`
	Features           profile.FeaturesConfig       `json:"features"`
	CostControls       *profile.CostControlsConfig  `json:"cost_controls,omitempty"`
	PostDeployment     *profile.PostDeploymentConfig `json:"post_deployment,omitempty"`
}

// toProfileResponse converts a profile to API response format
func toProfileResponse(p *profile.Profile) *ProfileResponse {
	return &ProfileResponse{
		Name:              p.Name,
		DisplayName:       p.DisplayName,
		Description:       p.Description,
		Platform:          string(p.Platform),
		Enabled:           p.Enabled,
		OpenshiftVersions: p.OpenshiftVersions,
		Regions:           p.Regions,
		BaseDomains:       p.BaseDomains,
		Compute:           p.Compute,
		Lifecycle:         p.Lifecycle,
		Networking:        p.Networking,
		Tags:              p.Tags,
		Features:          p.Features,
		CostControls:      &p.CostControls,
		PostDeployment:    p.PostDeployment,
	}
}

// List handles GET /api/v1/profiles
//
//	@Summary		List cluster profiles
//	@Description	Returns all available cluster profiles. Can be filtered by platform (aws or ibmcloud).
//	@Tags			Profiles
//	@Accept			json
//	@Produce		json
//	@Param			platform	query		string	false	"Filter by platform (aws, ibmcloud)"
//	@Success		200			{array}		ProfileResponse
//	@Failure		400			{object}	map[string]string	"Invalid platform parameter"
//	@Security		BearerAuth
//	@Router			/profiles [get]
func (h *ProfileHandler) List(c echo.Context) error {
	// Parse platform filter
	platformParam := c.QueryParam("platform")

	var profiles []*profile.Profile

	if platformParam != "" {
		// Validate platform
		var platform types.Platform
		switch platformParam {
		case "aws":
			platform = types.PlatformAWS
		case "ibmcloud":
			platform = types.PlatformIBMCloud
		default:
			return ErrorBadRequest(c, "Invalid platform. Must be 'aws' or 'ibmcloud'")
		}

		profiles = h.registry.ListByPlatform(platform)
	} else {
		profiles = h.registry.List()
	}

	// Convert to response format
	response := make([]*ProfileResponse, len(profiles))
	for i, p := range profiles {
		response[i] = toProfileResponse(p)
	}

	return SuccessOK(c, response)
}

// Get handles GET /api/v1/profiles/:name
//
//	@Summary		Get cluster profile
//	@Description	Returns details of a specific cluster profile by name
//	@Tags			Profiles
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Profile name"
//	@Success		200		{object}	ProfileResponse
//	@Failure		404		{object}	map[string]string	"Profile not found"
//	@Security		BearerAuth
//	@Router			/profiles/{name} [get]
func (h *ProfileHandler) Get(c echo.Context) error {
	name := c.Param("name")

	prof, err := h.registry.Get(name)
	if err != nil {
		return ErrorNotFound(c, "Profile not found: "+err.Error())
	}

	return SuccessOK(c, toProfileResponse(prof))
}
