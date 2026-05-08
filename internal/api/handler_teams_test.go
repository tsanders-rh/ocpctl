package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/api"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TestTeamHandler_ListTeams verifies team listing endpoint
func TestTeamHandler_ListTeams(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("returns all teams for admin", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create test teams
		teams := []*types.Team{
			{Name: "engineering", Description: stringPtr("Engineering")},
			{Name: "sales", Description: stringPtr("Sales")},
		}
		for _, team := range teams {
			err := s.Teams.Create(ctx, team)
			require.NoError(t, err)
		}

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/admin/teams", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set admin context
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		setAuthContext(c, admin)

		// Execute
		err := handler.ListTeams(c)
		require.NoError(t, err)

		// Assert
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		teamsData, ok := response["teams"].([]interface{})
		require.True(t, ok)
		assert.GreaterOrEqual(t, len(teamsData), 2)
	})

	t.Run("requires admin role", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/admin/teams", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set non-admin context
		user := createTestUser(t, s, "user@example.com", types.RoleUser)
		setAuthContext(c, user)

		// Execute with admin middleware
		err := api.RequireAdmin()(handler.ListTeams)(c)

		// Assert - should be forbidden
		assert.Error(t, err)
		httpErr, ok := err.(*echo.HTTPError)
		require.True(t, ok)
		assert.Equal(t, http.StatusForbidden, httpErr.Code)
	})
}

// TestTeamHandler_CreateTeam verifies team creation endpoint
func TestTeamHandler_CreateTeam(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("creates team successfully", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create request
		e := echo.New()
		e.Validator = api.NewValidator()
		reqBody := `{"name":"product","description":"Product team"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/teams", strings.NewReader(reqBody))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set admin context
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		setAuthContext(c, admin)

		// Execute
		err := handler.CreateTeam(c)
		require.NoError(t, err)

		// Assert
		assert.Equal(t, http.StatusCreated, rec.Code)

		var team types.Team
		err = json.Unmarshal(rec.Body.Bytes(), &team)
		require.NoError(t, err)
		assert.Equal(t, "product", team.Name)
		assert.Equal(t, "Product team", *team.Description)
		assert.NotEmpty(t, team.ID)
	})

	t.Run("rejects invalid team name", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create request with invalid name
		e := echo.New()
		e.Validator = api.NewValidator()
		reqBody := `{"name":"","description":"Invalid"}` // Empty name
		req := httptest.NewRequest(http.MethodPost, "/admin/teams", strings.NewReader(reqBody))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set admin context
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		setAuthContext(c, admin)

		// Execute
		err := handler.CreateTeam(c)

		// Assert - should fail validation
		assert.Error(t, err)
	})

	t.Run("rejects duplicate team name", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create first team
		team1 := &types.Team{Name: "duplicate", Description: stringPtr("First")}
		err := s.Teams.Create(ctx, team1)
		require.NoError(t, err)

		// Attempt to create duplicate
		e := echo.New()
		e.Validator = api.NewValidator()
		reqBody := `{"name":"duplicate","description":"Second"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/teams", strings.NewReader(reqBody))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set admin context
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		setAuthContext(c, admin)

		// Execute
		err = handler.CreateTeam(c)

		// Assert - should fail with conflict
		assert.Error(t, err)
		httpErr, ok := err.(*echo.HTTPError)
		require.True(t, ok)
		assert.Equal(t, http.StatusConflict, httpErr.Code)
	})
}

// TestTeamHandler_GrantTeamAdmin verifies granting team admin privileges
func TestTeamHandler_GrantTeamAdmin(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("grants privilege successfully", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create team and users
		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)

		// Create request
		e := echo.New()
		e.Validator = api.NewValidator()
		reqBody := `{"user_id":"` + teamAdmin.ID + `","notes":"Engineering lead"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/teams/engineering/admins", strings.NewReader(reqBody))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/teams/:name/admins")
		c.SetParamNames("name")
		c.SetParamValues("engineering")

		// Set admin context
		setAuthContext(c, admin)

		// Execute
		err = handler.GrantTeamAdmin(c)
		require.NoError(t, err)

		// Assert
		assert.Equal(t, http.StatusOK, rec.Code)

		// Verify privilege was granted
		isAdmin, err := s.TeamAdmins.IsTeamAdmin(ctx, teamAdmin.ID, "engineering")
		require.NoError(t, err)
		assert.True(t, isAdmin)
	})

	t.Run("rejects user without TEAM_ADMIN role", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create team and users
		team := &types.Team{Name: "sales", Description: stringPtr("Sales")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		regularUser := createTestUser(t, s, "user@example.com", types.RoleUser)

		// Create request
		e := echo.New()
		e.Validator = api.NewValidator()
		reqBody := `{"user_id":"` + regularUser.ID + `","notes":"Invalid"}`
		req := httptest.NewRequest(http.MethodPost, "/admin/teams/sales/admins", strings.NewReader(reqBody))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/teams/:name/admins")
		c.SetParamNames("name")
		c.SetParamValues("sales")

		// Set admin context
		setAuthContext(c, admin)

		// Execute
		err = handler.GrantTeamAdmin(c)

		// Assert - should fail
		assert.Error(t, err)
		httpErr, ok := err.(*echo.HTTPError)
		require.True(t, ok)
		assert.Equal(t, http.StatusBadRequest, httpErr.Code)
	})
}

// TestTeamHandler_RevokeTeamAdmin verifies revoking team admin privileges
func TestTeamHandler_RevokeTeamAdmin(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("revokes privilege successfully", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create team and users
		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)

		// Grant privilege first
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodDelete, "/admin/teams/engineering/admins/"+teamAdmin.ID, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/teams/:name/admins/:user_id")
		c.SetParamNames("name", "user_id")
		c.SetParamValues("engineering", teamAdmin.ID)

		// Set admin context
		setAuthContext(c, admin)

		// Execute
		err = handler.RevokeTeamAdmin(c)
		require.NoError(t, err)

		// Assert
		assert.Equal(t, http.StatusOK, rec.Code)

		// Verify privilege was revoked
		isAdmin, err := s.TeamAdmins.IsTeamAdmin(ctx, teamAdmin.ID, "engineering")
		require.NoError(t, err)
		assert.False(t, isAdmin)
	})

	t.Run("returns 404 for nonexistent mapping", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create admin
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodDelete, "/admin/teams/engineering/admins/nonexistent", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/teams/:name/admins/:user_id")
		c.SetParamNames("name", "user_id")
		c.SetParamValues("engineering", "nonexistent-user-id")

		// Set admin context
		setAuthContext(c, admin)

		// Execute
		err := handler.RevokeTeamAdmin(c)

		// Assert - should return 404
		assert.Error(t, err)
		httpErr, ok := err.(*echo.HTTPError)
		require.True(t, ok)
		assert.Equal(t, http.StatusNotFound, httpErr.Code)
	})
}

// TestTeamHandler_DeleteTeam verifies team deletion with cluster checks
func TestTeamHandler_DeleteTeam(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("deletes team when no clusters exist", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create team
		team := &types.Team{Name: "temp-team", Description: stringPtr("Temporary")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodDelete, "/admin/teams/temp-team", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/teams/:name")
		c.SetParamNames("name")
		c.SetParamValues("temp-team")

		// Set admin context
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		setAuthContext(c, admin)

		// Execute
		err = handler.DeleteTeam(c)
		require.NoError(t, err)

		// Assert
		assert.Equal(t, http.StatusOK, rec.Code)

		// Verify deleted
		_, err = s.Teams.Get(ctx, "temp-team")
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("prevents deletion when clusters exist", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		handler := api.NewTeamHandler(s)

		// Create team and cluster
		team := &types.Team{Name: "active-team", Description: stringPtr("Active")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		user := createTestUser(t, s, "user@example.com", types.RoleUser)
		createTestCluster(t, s, user.ID, "active-team")

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodDelete, "/admin/teams/active-team", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/admin/teams/:name")
		c.SetParamNames("name")
		c.SetParamValues("active-team")

		// Set admin context
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		setAuthContext(c, admin)

		// Execute
		err = handler.DeleteTeam(c)

		// Assert - should fail
		assert.Error(t, err)
		httpErr, ok := err.(*echo.HTTPError)
		require.True(t, ok)
		assert.Equal(t, http.StatusBadRequest, httpErr.Code)
	})
}

// Helper functions for API tests

func setAuthContext(c echo.Context, user *types.User) {
	c.Set("user", user)
}
