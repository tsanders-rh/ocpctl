package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TeamHandler handles team management endpoints (admin only)
type TeamHandler struct {
	store *store.Store
}

// NewTeamHandler creates a new team handler
func NewTeamHandler(st *store.Store) *TeamHandler {
	return &TeamHandler{
		store: st,
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
