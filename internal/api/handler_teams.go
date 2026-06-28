package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TeamHandler handles team management endpoints (admin only)
type TeamHandler struct {
	store    *store.Store
	registry *profile.Registry
}

// NewTeamHandler creates a new team handler
func NewTeamHandler(st *store.Store, r *profile.Registry) *TeamHandler {
	return &TeamHandler{
		store:    st,
		registry: r,
	}
}

// ListTeams returns all teams
//
//	@Summary		List teams
//	@Description	Returns a list of all teams (admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}	"Returns teams array"
//	@Failure		500	{object}	map[string]string		"Failed to list teams"
//	@Security		BearerAuth
//	@Router			/admin/teams [get]
func (h *TeamHandler) ListTeams(c echo.Context) error {
	ctx := c.Request().Context()

	teams, err := h.store.Teams.List(ctx)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"teams": teams,
	})
}

// CreateTeam creates a new team
//
//	@Summary		Create team
//	@Description	Creates a new team (admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			body	body		types.CreateTeamRequest	true	"Team creation request"
//	@Success		201		{object}	types.Team
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		409		{object}	map[string]string	"Team name already exists"
//	@Failure		500		{object}	map[string]string	"Failed to create team"
//	@Security		BearerAuth
//	@Router			/admin/teams [post]
func (h *TeamHandler) CreateTeam(c echo.Context) error {
	ctx := c.Request().Context()

	var req types.CreateTeamRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Get current user ID for created_by field
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	team := &types.Team{
		Name:        req.Name,
		Description: req.Description,
		CreatedBy:   &userID,
	}

	if err := h.store.Teams.Create(ctx, team); err != nil {
		if err.Error() == "team with name '"+req.Name+"' already exists" {
			return ErrorConflict(c, err.Error())
		}
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusCreated, team)
}

// GetTeam retrieves a team by name
//
//	@Summary		Get team
//	@Description	Retrieves team details by name (admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Team name"
//	@Success		200		{object}	types.Team
//	@Failure		404		{object}	map[string]string	"Team not found"
//	@Failure		500		{object}	map[string]string	"Failed to get team"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name} [get]
func (h *TeamHandler) GetTeam(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	team, err := h.store.Teams.Get(ctx, teamName)
	if err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "team not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, team)
}

// UpdateTeam updates team metadata
//
//	@Summary		Update team
//	@Description	Updates team description (admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string						true	"Team name"
//	@Param			body	body		types.UpdateTeamRequest		true	"Team update fields"
//	@Success		200		{object}	types.Team
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		404		{object}	map[string]string	"Team not found"
//	@Failure		500		{object}	map[string]string	"Failed to update team"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name} [patch]
func (h *TeamHandler) UpdateTeam(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	var req types.UpdateTeamRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "invalid request body")
	}

	// Build updates map
	updates := make(map[string]interface{})
	if req.Description != nil {
		updates["description"] = *req.Description
	}

	if len(updates) == 0 {
		return ErrorBadRequest(c, "no fields to update")
	}

	// Update team
	if err := h.store.Teams.Update(ctx, teamName, updates); err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "team not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Get updated team
	team, err := h.store.Teams.Get(ctx, teamName)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, team)
}

// DeleteTeam deletes a team
//
//	@Summary		Delete team
//	@Description	Deletes a team (admin only). Only allowed if no clusters reference it.
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Team name"
//	@Success		200		{object}	map[string]string
//	@Failure		400		{object}	map[string]string	"Team has clusters"
//	@Failure		404		{object}	map[string]string	"Team not found"
//	@Failure		500		{object}	map[string]string	"Failed to delete team"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name} [delete]
func (h *TeamHandler) DeleteTeam(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	if err := h.store.Teams.Delete(ctx, teamName); err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "team not found")
		}
		// Check if error is about clusters referencing this team
		if err.Error() == "cannot delete team '"+teamName+"': 1 cluster(s) still reference it" {
			return ErrorBadRequest(c, err.Error())
		}
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "team deleted successfully",
	})
}

// ListTeamAdmins returns all team admins for a specific team
//
//	@Summary		List team admins
//	@Description	Returns all users who can administer a given team (admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Team name"
//	@Success		200		{object}	map[string]interface{}	"Returns team and admins array"
//	@Failure		500		{object}	map[string]string		"Failed to list team admins"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name}/admins [get]
func (h *TeamHandler) ListTeamAdmins(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	admins, err := h.store.TeamAdmins.ListTeamAdmins(ctx, teamName)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"team":   teamName,
		"admins": admins,
	})
}

// GrantTeamAdmin grants team admin privilege to a user
//
//	@Summary		Grant team admin privilege
//	@Description	Grants team admin privilege to a user for a specific team (platform admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string							true	"Team name"
//	@Param			body	body		types.GrantTeamAdminRequest		true	"Grant request"
//	@Success		200		{object}	map[string]string
//	@Failure		400		{object}	map[string]string	"Invalid request or user doesn't have TEAM_ADMIN role"
//	@Failure		404		{object}	map[string]string	"User not found"
//	@Failure		500		{object}	map[string]string	"Failed to grant privilege"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name}/admins [post]
func (h *TeamHandler) GrantTeamAdmin(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	var req types.GrantTeamAdminRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Get current user ID (who is granting the privilege)
	grantedBy, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Grant team admin privilege
	if err := h.store.TeamAdmins.GrantTeamAdmin(ctx, req.UserID, teamName, grantedBy, req.Notes); err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "user not found")
		}
		if err.Error() == "user must have TEAM_ADMIN or ADMIN role to be granted team admin privileges" {
			return ErrorBadRequest(c, err.Error())
		}
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "team admin privilege granted successfully",
	})
}

// RevokeTeamAdmin revokes team admin privilege from a user
//
//	@Summary		Revoke team admin privilege
//	@Description	Revokes team admin privilege from a user for a specific team (platform admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name		path		string	true	"Team name"
//	@Param			user_id		path		string	true	"User ID"
//	@Success		200			{object}	map[string]string
//	@Failure		404			{object}	map[string]string	"Mapping not found"
//	@Failure		500			{object}	map[string]string	"Failed to revoke privilege"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name}/admins/{user_id} [delete]
func (h *TeamHandler) RevokeTeamAdmin(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")
	userID := c.Param("user_id")

	if err := h.store.TeamAdmins.RevokeTeamAdmin(ctx, userID, teamName); err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "team admin mapping not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "team admin privilege revoked successfully",
	})
}

// ListTeamMembers returns all users who are members of a given team
//
//	@Summary		List team members
//	@Description	Returns all users who belong to a given team (platform admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Team name"
//	@Success		200		{object}	map[string]interface{}	"Returns team and members array"
//	@Failure		500		{object}	map[string]string		"Failed to list team members"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name}/members [get]
func (h *TeamHandler) ListTeamMembers(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	members, err := h.store.TeamMemberships.GetTeamMembers(ctx, teamName)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	// Collect user IDs from members
	userIDs := make([]string, len(members))
	for i, member := range members {
		userIDs[i] = member.UserID
	}

	// Fetch all users in batch
	usersMap, err := h.store.Users.GetByIDs(ctx, userIDs)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	// Enrich member data with user details
	type MemberWithUser struct {
		*types.UserTeamMembership
		User *types.UserResponse `json:"user"`
	}

	enrichedMembers := make([]MemberWithUser, 0, len(members))
	for _, member := range members {
		enriched := MemberWithUser{
			UserTeamMembership: member,
		}
		if user, ok := usersMap[member.UserID]; ok {
			enriched.User = user.ToResponse()
		}
		enrichedMembers = append(enrichedMembers, enriched)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"team":    teamName,
		"members": enrichedMembers,
	})
}

// GetEligibleUsers returns users who can be added to a team
//
//	@Summary		Get eligible users for team
//	@Description	Returns users who are not yet members of the specified team (excludes ADMIN role users who have access to all teams)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Team name"
//	@Success		200		{object}	map[string]interface{}	"Returns eligible users array"
//	@Failure		500		{object}	map[string]string		"Failed to get eligible users"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name}/eligible-users [get]
func (h *TeamHandler) GetEligibleUsers(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	// Get all users
	allUsers, err := h.store.Users.List(ctx)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	// Get current team members
	members, err := h.store.TeamMemberships.GetTeamMembers(ctx, teamName)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	// Build set of existing member user IDs
	memberIDs := make(map[string]bool)
	for _, member := range members {
		memberIDs[member.UserID] = true
	}

	// Filter out users who are already members or are admins (admins have access to all teams)
	eligibleUsers := make([]*types.UserResponse, 0)
	for _, user := range allUsers {
		if !memberIDs[user.ID] && user.Role != types.RoleAdmin {
			eligibleUsers = append(eligibleUsers, user.ToResponse())
		}
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"users": eligibleUsers,
	})
}

// AddUserToTeam adds a user to a team
//
//	@Summary		Add user to team
//	@Description	Adds a user to a team for general membership (platform admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string							true	"Team name"
//	@Param			body	body		types.AddUserToTeamRequest		true	"Add user request"
//	@Success		200		{object}	map[string]string
//	@Failure		400		{object}	map[string]string	"Invalid request"
//	@Failure		404		{object}	map[string]string	"User not found"
//	@Failure		500		{object}	map[string]string	"Failed to add user"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name}/members [post]
func (h *TeamHandler) AddUserToTeam(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	var req types.AddUserToTeamRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Get current user ID (who is adding the user to team)
	addedBy, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Add user to team
	if err := h.store.TeamMemberships.AddUserToTeam(ctx, req.UserID, teamName, addedBy, req.Notes); err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "user added to team successfully",
	})
}

// RemoveUserFromTeam removes a user from a team
//
//	@Summary		Remove user from team
//	@Description	Removes a user from a team (platform admin only)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name		path		string	true	"Team name"
//	@Param			user_id		path		string	true	"User ID"
//	@Success		200			{object}	map[string]string
//	@Failure		404			{object}	map[string]string	"Membership not found"
//	@Failure		500			{object}	map[string]string	"Failed to remove user"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name}/members/{user_id} [delete]
func (h *TeamHandler) RemoveUserFromTeam(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")
	userID := c.Param("user_id")

	if err := h.store.TeamMemberships.RemoveUserFromTeam(ctx, userID, teamName); err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "team membership not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "user removed from team successfully",
	})
}

// GetAllowedProfiles returns the allowed profiles for a team
//
//	@Summary		Get team allowed profiles
//	@Description	Returns the list of profiles allowed for this team (null/empty = all profiles allowed)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Team name"
//	@Success		200		{object}	map[string]interface{}	"Returns allowed_profiles array"
//	@Failure		404		{object}	map[string]string		"Team not found"
//	@Failure		500		{object}	map[string]string		"Failed to get allowed profiles"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name}/allowed-profiles [get]
func (h *TeamHandler) GetAllowedProfiles(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	team, err := h.store.Teams.Get(ctx, teamName)
	if err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "team not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"team":             teamName,
		"allowed_profiles": team.AllowedProfiles,
	})
}

// UpdateAllowedProfiles updates the allowed profiles for a team
//
//	@Summary		Update team allowed profiles
//	@Description	Updates the list of profiles allowed for this team. Empty array = no profiles allowed. Null = all profiles allowed.
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string							true	"Team name"
//	@Param			body	body		types.UpdateAllowedProfilesRequest	true	"Allowed profiles list"
//	@Success		200		{object}	types.Team
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		404		{object}	map[string]string	"Team not found"
//	@Failure		500		{object}	map[string]string	"Failed to update allowed profiles"
//	@Security		BearerAuth
//	@Router			/admin/teams/{name}/allowed-profiles [patch]
func (h *TeamHandler) UpdateAllowedProfiles(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	var req types.UpdateAllowedProfilesRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Build updates map
	updates := make(map[string]interface{})
	updates["allowed_profiles"] = req.AllowedProfiles

	// Update team
	if err := h.store.Teams.Update(ctx, teamName, updates); err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "team not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Get updated team
	team, err := h.store.Teams.Get(ctx, teamName)
	if err != nil {
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(http.StatusOK, team)
}

// GetTeamCosts returns cost summary for a team
//
//	@Summary		Get team costs
//	@Description	Returns cost summary for a team (current month and last 30 days)
//	@Tags			Teams
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string	true	"Team name"
//	@Success		200		{object}	types.TeamCostSummary
//	@Failure		403		{object}	map[string]string	"Not authorized to view this team"
//	@Failure		404		{object}	map[string]string	"Team not found"
//	@Failure		500		{object}	map[string]string	"Failed to get team costs"
//	@Security		BearerAuth
//	@Router			/teams/{name}/costs [get]
func (h *TeamHandler) GetTeamCosts(c echo.Context) error {
	ctx := c.Request().Context()
	teamName := c.Param("name")

	// Check authorization - user must be team admin for this team or platform admin
	if !auth.CanManageTeam(c, teamName) {
		return ErrorForbidden(c, "You do not have permission to view costs for this team")
	}

	// Verify team exists
	team, err := h.store.Teams.Get(ctx, teamName)
	if err != nil {
		if err == store.ErrNotFound {
			return ErrorNotFound(c, "team not found")
		}
		return LogAndReturnGenericError(c, err)
	}

	// Get all active clusters for the team
	clusters, err := h.store.Teams.GetTeamClusters(ctx, team.Name)
	if err != nil {
		return LogAndReturnGenericError(c, fmt.Errorf("failed to get team clusters: %w", err))
	}

	// Calculate date ranges
	now := time.Now().UTC()
	currentMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	currentMonthEnd := now
	last30DaysStart := now.AddDate(0, 0, -30)
	last30DaysEnd := now

	// Calculate costs for each cluster
	clusterDetails := []*types.ClusterCostDetail{}
	var currentMonthTotal float64
	var last30DaysTotal float64

	for _, cluster := range clusters {
		// Get profile for cost calculation
		prof, _ := h.registry.Get(cluster.Profile)
		if prof == nil {
			LogWarning(c, "profile not found for cluster, skipping cost calculation",
				"cluster", cluster.Name,
				"profile", cluster.Profile)
			continue
		}

		// Calculate effective hourly cost based on cluster state
		effectiveCost := h.calculateEffectiveCost(cluster, prof)

		// Calculate current month costs
		currentMonthCost, currentMonthHours := h.calculatePeriodCost(
			cluster, effectiveCost, currentMonthStart, currentMonthEnd)

		// Calculate last 30 days costs
		last30DaysCost, last30DaysHours := h.calculatePeriodCost(
			cluster, effectiveCost, last30DaysStart, last30DaysEnd)

		// Get owner email for display
		owner, err := h.store.Users.GetByID(ctx, cluster.OwnerID)
		ownerEmail := cluster.OwnerID
		if err == nil && owner != nil {
			ownerEmail = owner.Email
		}

		detail := &types.ClusterCostDetail{
			ID:                       cluster.ID,
			Name:                     cluster.Name,
			Profile:                  cluster.Profile,
			Status:                   string(cluster.Status),
			Owner:                    ownerEmail,
			CreatedAt:                cluster.CreatedAt,
			EstimatedHourlyCost:      effectiveCost,
			CurrentMonthCost:         currentMonthCost,
			Last30DaysCost:           last30DaysCost,
			RuntimeHoursCurrentMonth: currentMonthHours,
			RuntimeHoursLast30Days:   last30DaysHours,
		}

		clusterDetails = append(clusterDetails, detail)
		currentMonthTotal += currentMonthCost
		last30DaysTotal += last30DaysCost
	}

	// Calculate estimated full month cost (prorated)
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	daysElapsed := now.Day()
	var estimatedFullMonth float64
	if daysElapsed > 0 {
		estimatedFullMonth = currentMonthTotal * float64(daysInMonth) / float64(daysElapsed)
	}

	// Build response
	summary := &types.TeamCostSummary{
		Team: team.Name,
		CurrentMonth: &types.PeriodCostSummary{
			StartDate:          currentMonthStart.Format("2006-01-02"),
			EndDate:            currentMonthEnd.Format("2006-01-02"),
			TotalCost:          currentMonthTotal,
			EstimatedFullMonth: estimatedFullMonth,
		},
		Last30Days: &types.PeriodCostSummary{
			StartDate: last30DaysStart.Format("2006-01-02"),
			EndDate:   last30DaysEnd.Format("2006-01-02"),
			TotalCost: last30DaysTotal,
		},
		Clusters: clusterDetails,
	}

	return c.JSON(http.StatusOK, summary)
}

// calculateEffectiveCost calculates the effective hourly cost based on cluster state
// Hibernated clusters cost significantly less than running clusters
func (h *TeamHandler) calculateEffectiveCost(cluster *types.Cluster, prof *profile.Profile) float64 {
	baseCost := prof.CostControls.EstimatedHourlyCost

	// If cluster is hibernated, calculate reduced cost based on cluster type
	if cluster.Status == types.ClusterStatusHibernated {
		switch cluster.ClusterType {
		case types.ClusterTypeOpenShift:
			// OpenShift: All instances stopped, only persistent storage remains (~10%)
			return baseCost * 0.10
		case types.ClusterTypeROSA:
			// ROSA: Machine pools scaled to 0, but control plane runs at fixed $0.03/hr
			return 0.03
		case types.ClusterTypeEKS:
			// EKS: Node groups scaled to 0, control plane at $0.10/hr
			return 0.10
		case types.ClusterTypeIKS:
			// IKS: Workers scaled to 0, minimal cost (~5%)
			return baseCost * 0.05
		case types.ClusterTypeGKE:
			// GKE: Node pools scaled to 0, no control plane cost, only persistent disks (~3%)
			return baseCost * 0.03
		default:
			// Unknown cluster type, use conservative estimate
			return baseCost * 0.10
		}
	}

	// For all other states (READY, CREATING, etc.), use full cost
	return baseCost
}

// calculatePeriodCost calculates cost and runtime hours for a specific period
func (h *TeamHandler) calculatePeriodCost(cluster *types.Cluster, effectiveCost float64, periodStart, periodEnd time.Time) (float64, float64) {
	// Determine cluster's active period within the date range
	clusterStart := cluster.CreatedAt
	clusterEnd := periodEnd // Assume still running

	// If cluster is destroyed, use destroyed_at as the end time
	if cluster.Status == types.ClusterStatusDestroyed && cluster.DestroyedAt != nil {
		clusterEnd = *cluster.DestroyedAt
	}

	// Calculate intersection of cluster lifetime and period
	activeStart := clusterStart
	if periodStart.After(clusterStart) {
		activeStart = periodStart
	}

	activeEnd := clusterEnd
	if periodEnd.Before(clusterEnd) {
		activeEnd = periodEnd
	}

	// If cluster wasn't active during this period, return 0
	if activeStart.After(activeEnd) || activeStart.After(periodEnd) || activeEnd.Before(periodStart) {
		return 0.0, 0.0
	}

	// Calculate runtime hours
	runtimeHours := activeEnd.Sub(activeStart).Hours()
	if runtimeHours < 0 {
		runtimeHours = 0
	}

	// Calculate total cost
	totalCost := runtimeHours * effectiveCost

	return totalCost, runtimeHours
}
