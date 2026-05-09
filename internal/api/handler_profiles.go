package api

import (
	"log"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ProfileHandler handles profile-related API endpoints
type ProfileHandler struct {
	registry *profile.Registry
	store    *store.Store
}

// NewProfileHandler creates a new profile handler
func NewProfileHandler(registry *profile.Registry, st *store.Store) *ProfileHandler {
	return &ProfileHandler{
		registry: registry,
		store:    st,
	}
}

// ProfileResponse represents a profile in API responses
type ProfileResponse struct {
	Name               string                            `json:"name"`
	DisplayName        string                            `json:"display_name"`
	Description        string                            `json:"description"`
	Platform           string                            `json:"platform"`
	Track              string                            `json:"track,omitempty"`
	Enabled            bool                              `json:"enabled"`
	CredentialsMode    string                            `json:"credentials_mode,omitempty"`
	OpenshiftVersions  *profile.VersionConfig            `json:"openshift_versions,omitempty"`
	KubernetesVersions *profile.VersionConfig            `json:"kubernetes_versions,omitempty"`
	Regions            profile.RegionConfig              `json:"regions"`
	BaseDomains        *profile.BaseDomainConfig         `json:"base_domains,omitempty"`
	Compute            profile.ComputeConfig             `json:"compute"`
	Lifecycle          profile.LifecycleConfig           `json:"lifecycle"`
	Networking         *profile.NetworkingConfig         `json:"networking,omitempty"`
	Tags               profile.TagsConfig                `json:"tags"`
	Features           profile.FeaturesConfig            `json:"features"`
	CostControls       *profile.CostControlsConfig       `json:"cost_controls,omitempty"`
	PostDeployment     *profile.PostDeploymentConfig     `json:"post_deployment,omitempty"`
	DeploymentMetrics  *types.ProfileDeploymentMetrics   `json:"deployment_metrics,omitempty"`
}

// toProfileResponse converts a profile to API response format
func toProfileResponse(p *profile.Profile) *ProfileResponse {
	return &ProfileResponse{
		Name:               p.Name,
		DisplayName:        p.DisplayName,
		Description:        p.Description,
		Platform:           string(p.Platform),
		Track:              p.Track,
		Enabled:            p.Enabled,
		CredentialsMode:    p.CredentialsMode,
		OpenshiftVersions:  p.OpenshiftVersions,
		KubernetesVersions: p.KubernetesVersions,
		Regions:            p.Regions,
		BaseDomains:        p.BaseDomains,
		Compute:            p.Compute,
		Lifecycle:          p.Lifecycle,
		Networking:         p.Networking,
		Tags:               p.Tags,
		Features:           p.Features,
		CostControls:       &p.CostControls,
		PostDeployment:     p.PostDeployment,
	}
}

// List handles GET /api/v1/profiles
//
//	@Summary		List cluster profiles
//	@Description	Returns all available cluster profiles. Can be filtered by platform (aws, ibmcloud, or gcp) and track (ga or prerelease).
//	@Tags			Profiles
//	@Accept			json
//	@Produce		json
//	@Param			platform	query		string	false	"Filter by platform (aws, ibmcloud, gcp)"
//	@Param			track		query		string	false	"Filter by track (ga, prerelease)"
//	@Success		200			{array}		ProfileResponse
//	@Failure		400			{object}	map[string]string	"Invalid platform or track parameter"
//	@Security		BearerAuth
//	@Router			/profiles [get]
func (h *ProfileHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	// Parse filters
	platformParam := c.QueryParam("platform")
	trackParam := c.QueryParam("track")

	var profiles []*profile.Profile

	// Start with platform filter if specified
	if platformParam != "" {
		// Validate platform
		var platform types.Platform
		switch platformParam {
		case "aws":
			platform = types.PlatformAWS
		case "ibmcloud":
			platform = types.PlatformIBMCloud
		case "gcp":
			platform = types.PlatformGCP
		default:
			return ErrorBadRequest(c, "Invalid platform. Must be 'aws', 'ibmcloud', or 'gcp'")
		}

		profiles = h.registry.ListByPlatform(platform)
	} else {
		profiles = h.registry.List()
	}

	// Apply track filter if specified
	if trackParam != "" {
		// Validate track
		if trackParam != "ga" && trackParam != "prerelease" && trackParam != "kube" {
			return ErrorBadRequest(c, "Invalid track. Must be 'ga', 'prerelease', or 'kube'")
		}

		// Filter profiles by track
		var filtered []*profile.Profile
		for _, p := range profiles {
			if p.Track == trackParam {
				filtered = append(filtered, p)
			}
		}
		profiles = filtered
	}

	// Apply team-based filtering (non-admin users only)
	user, err := auth.GetUser(c)
	if err == nil && user.Role != types.RoleAdmin {
		// Get user's teams
		userTeams := user.Teams
		if len(userTeams) > 0 {
			// Collect allowed profiles from all teams
			allowedProfilesSet := make(map[string]bool)
			hasUnrestrictedTeam := false

			for _, teamName := range userTeams {
				team, err := h.store.Teams.Get(ctx, teamName)
				if err != nil {
					// Skip team if we can't fetch it
					continue
				}

				// If any team has no restrictions (null or empty), user sees all profiles
				if team.AllowedProfiles == nil || len(team.AllowedProfiles) == 0 {
					hasUnrestrictedTeam = true
					break
				}

				// Add this team's allowed profiles to the set
				for _, profileName := range team.AllowedProfiles {
					allowedProfilesSet[profileName] = true
				}
			}

			// Filter profiles if there are restrictions
			if !hasUnrestrictedTeam && len(allowedProfilesSet) > 0 {
				var filtered []*profile.Profile
				for _, p := range profiles {
					if allowedProfilesSet[p.Name] {
						filtered = append(filtered, p)
					}
				}
				profiles = filtered
			}
		}
	}

	// Load deployment metrics for all profiles
	metrics, err := h.store.ProfileDeploymentMetrics.GetAll(ctx)
	if err != nil {
		log.Printf("Failed to load deployment metrics: %v", err)
		// Continue without metrics (non-critical)
	}

	// Map metrics by profile name for quick lookup
	metricsMap := make(map[string]*types.ProfileDeploymentMetrics)
	for _, m := range metrics {
		metricsMap[m.Profile] = m
	}

	// Convert to response format and attach metrics
	response := make([]*ProfileResponse, len(profiles))
	for i, p := range profiles {
		response[i] = toProfileResponse(p)
		// Attach deployment metrics if available
		if metric, ok := metricsMap[p.Name]; ok {
			response[i].DeploymentMetrics = metric
		}
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
	ctx := c.Request().Context()
	name := c.Param("name")

	prof, err := h.registry.Get(name)
	if err != nil {
		return ErrorNotFound(c, "Profile not found: "+err.Error())
	}

	response := toProfileResponse(prof)

	// Load deployment metrics for this profile
	metrics, err := h.store.ProfileDeploymentMetrics.GetByProfile(ctx, name)
	if err != nil {
		log.Printf("Failed to load deployment metrics for profile %s: %v", name, err)
		// Continue without metrics (non-critical)
	} else {
		response.DeploymentMetrics = metrics
	}

	return SuccessOK(c, response)
}
