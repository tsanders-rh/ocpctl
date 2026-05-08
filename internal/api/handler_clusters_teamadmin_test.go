package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/api"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TestClusterHandler_List_TeamAdminAccess verifies team-scoped cluster access in API
func TestClusterHandler_List_TeamAdminAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("team admin sees owned and managed team clusters", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		// Create policy engine and registry (required by ClusterHandler)
		policyEngine := policy.NewEngine(nil) // Mock profiles
		registry := profile.NewRegistry()

		handler := api.NewClusterHandler(s, policyEngine, registry)

		// Create users and teams
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		otherUser := createTestUser(t, s, "other@example.com", types.RoleUser)

		teamEng := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		teamSales := &types.Team{Name: "sales", Description: stringPtr("Sales")}
		err := s.Teams.Create(ctx, teamEng)
		require.NoError(t, err)
		err = s.Teams.Create(ctx, teamSales)
		require.NoError(t, err)

		// Grant team admin privilege for engineering
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)

		// Reload user to get managed teams
		teamAdmin, err = s.Users.GetByID(ctx, teamAdmin.ID)
		require.NoError(t, err)
		require.Contains(t, teamAdmin.ManagedTeams, "engineering")

		// Create clusters
		clusterOwned := createTestCluster(t, s, teamAdmin.ID, "engineering")      // Should see
		clusterTeam := createTestCluster(t, s, otherUser.ID, "engineering")       // Should see
		clusterSales := createTestCluster(t, s, otherUser.ID, "sales")            // Should NOT see
		clusterOwnedSales := createTestCluster(t, s, teamAdmin.ID, "sales")       // Should see (owned)

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set team admin context
		setAuthContext(c, teamAdmin)

		// Execute
		err = handler.List(c)
		require.NoError(t, err)

		// Assert
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		data, ok := response["data"].([]interface{})
		require.True(t, ok)
		assert.Equal(t, 3, len(data)) // Should see 3 clusters

		// Verify correct clusters returned
		clusterIDs := make(map[string]bool)
		for _, item := range data {
			cluster := item.(map[string]interface{})
			clusterIDs[cluster["id"].(string)] = true
		}

		assert.True(t, clusterIDs[clusterOwned.ID])
		assert.True(t, clusterIDs[clusterTeam.ID])
		assert.False(t, clusterIDs[clusterSales.ID])
		assert.True(t, clusterIDs[clusterOwnedSales.ID])
	})

	t.Run("team admin with multiple teams sees all relevant clusters", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		policyEngine := policy.NewEngine(nil)
		registry := profile.NewRegistry()
		handler := api.NewClusterHandler(s, policyEngine, registry)

		// Create users and teams
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		otherUser := createTestUser(t, s, "other@example.com", types.RoleUser)

		teams := []string{"engineering", "product", "sales"}
		for _, teamName := range teams {
			team := &types.Team{Name: teamName, Description: stringPtr(teamName)}
			err := s.Teams.Create(ctx, team)
			require.NoError(t, err)
		}

		// Grant privileges for engineering and product
		err := s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "product", admin.ID, nil)
		require.NoError(t, err)

		// Reload user
		teamAdmin, err = s.Users.GetByID(ctx, teamAdmin.ID)
		require.NoError(t, err)

		// Create clusters
		createTestCluster(t, s, otherUser.ID, "engineering") // Should see
		createTestCluster(t, s, otherUser.ID, "product")     // Should see
		createTestCluster(t, s, otherUser.ID, "sales")       // Should NOT see

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set team admin context
		setAuthContext(c, teamAdmin)

		// Execute
		err = handler.List(c)
		require.NoError(t, err)

		// Assert
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		pagination := response["pagination"].(map[string]interface{})
		assert.Equal(t, float64(2), pagination["total"])
	})

	t.Run("team admin filtering by specific team", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		policyEngine := policy.NewEngine(nil)
		registry := profile.NewRegistry()
		handler := api.NewClusterHandler(s, policyEngine, registry)

		// Create users and teams
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		otherUser := createTestUser(t, s, "other@example.com", types.RoleUser)

		teamEng := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		teamProd := &types.Team{Name: "product", Description: stringPtr("Product")}
		err := s.Teams.Create(ctx, teamEng)
		require.NoError(t, err)
		err = s.Teams.Create(ctx, teamProd)
		require.NoError(t, err)

		// Grant both teams
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "product", admin.ID, nil)
		require.NoError(t, err)

		// Reload user
		teamAdmin, err = s.Users.GetByID(ctx, teamAdmin.ID)
		require.NoError(t, err)

		// Create clusters
		createTestCluster(t, s, otherUser.ID, "engineering")
		createTestCluster(t, s, otherUser.ID, "product")

		// Create request with team filter
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters?team=engineering", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set team admin context
		setAuthContext(c, teamAdmin)

		// Execute
		err = handler.List(c)
		require.NoError(t, err)

		// Assert - should only see engineering clusters
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		pagination := response["pagination"].(map[string]interface{})
		assert.Equal(t, float64(1), pagination["total"])

		data := response["data"].([]interface{})
		cluster := data[0].(map[string]interface{})
		assert.Equal(t, "engineering", cluster["team"])
	})

	t.Run("team admin cannot filter by non-managed team", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		policyEngine := policy.NewEngine(nil)
		registry := profile.NewRegistry()
		handler := api.NewClusterHandler(s, policyEngine, registry)

		// Create users and teams
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		otherUser := createTestUser(t, s, "other@example.com", types.RoleUser)

		teamEng := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		teamSales := &types.Team{Name: "sales", Description: stringPtr("Sales")}
		err := s.Teams.Create(ctx, teamEng)
		require.NoError(t, err)
		err = s.Teams.Create(ctx, teamSales)
		require.NoError(t, err)

		// Only grant engineering
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)

		// Reload user
		teamAdmin, err = s.Users.GetByID(ctx, teamAdmin.ID)
		require.NoError(t, err)

		// Create clusters in both teams
		clusterOwned := createTestCluster(t, s, teamAdmin.ID, "engineering") // Should see (owned)
		createTestCluster(t, s, otherUser.ID, "sales")                        // Should NOT see

		// Create request filtering by non-managed team (sales)
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters?team=sales", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set team admin context
		setAuthContext(c, teamAdmin)

		// Execute
		err = handler.List(c)
		require.NoError(t, err)

		// Assert - should fall back to showing only owned clusters
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Should only see owned cluster (not in sales team)
		pagination := response["pagination"].(map[string]interface{})
		assert.Equal(t, float64(0), pagination["total"])
	})
}

// TestClusterHandler_Get_TeamAdminAccess verifies cluster access control
func TestClusterHandler_Get_TeamAdminAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("team admin can access owned cluster", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		policyEngine := policy.NewEngine(nil)
		registry := profile.NewRegistry()
		handler := api.NewClusterHandler(s, policyEngine, registry)

		// Create users and team
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)

		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)

		// Reload user
		teamAdmin, err = s.Users.GetByID(ctx, teamAdmin.ID)
		require.NoError(t, err)

		// Create owned cluster
		cluster := createTestCluster(t, s, teamAdmin.ID, "engineering")

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/"+cluster.ID, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/clusters/:id")
		c.SetParamNames("id")
		c.SetParamValues(cluster.ID)

		// Set team admin context
		setAuthContext(c, teamAdmin)

		// Execute
		err = handler.Get(c)
		require.NoError(t, err)

		// Assert
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		clusterData := response["cluster"].(map[string]interface{})
		assert.Equal(t, cluster.ID, clusterData["id"])
	})

	t.Run("team admin can access managed team cluster", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		policyEngine := policy.NewEngine(nil)
		registry := profile.NewRegistry()
		handler := api.NewClusterHandler(s, policyEngine, registry)

		// Create users and team
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		otherUser := createTestUser(t, s, "other@example.com", types.RoleUser)

		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)

		// Reload user
		teamAdmin, err = s.Users.GetByID(ctx, teamAdmin.ID)
		require.NoError(t, err)

		// Create cluster owned by other user in managed team
		cluster := createTestCluster(t, s, otherUser.ID, "engineering")

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/"+cluster.ID, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/clusters/:id")
		c.SetParamNames("id")
		c.SetParamValues(cluster.ID)

		// Set team admin context
		setAuthContext(c, teamAdmin)

		// Execute
		err = handler.Get(c)
		require.NoError(t, err)

		// Assert
		assert.Equal(t, http.StatusOK, rec.Code)
	})

	t.Run("team admin cannot access non-managed team cluster", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		policyEngine := policy.NewEngine(nil)
		registry := profile.NewRegistry()
		handler := api.NewClusterHandler(s, policyEngine, registry)

		// Create users and teams
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		otherUser := createTestUser(t, s, "other@example.com", types.RoleUser)

		teamEng := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		teamSales := &types.Team{Name: "sales", Description: stringPtr("Sales")}
		err := s.Teams.Create(ctx, teamEng)
		require.NoError(t, err)
		err = s.Teams.Create(ctx, teamSales)
		require.NoError(t, err)

		// Only grant engineering
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)

		// Reload user
		teamAdmin, err = s.Users.GetByID(ctx, teamAdmin.ID)
		require.NoError(t, err)

		// Create cluster in non-managed team
		cluster := createTestCluster(t, s, otherUser.ID, "sales")

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters/"+cluster.ID, nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)
		c.SetPath("/api/v1/clusters/:id")
		c.SetParamNames("id")
		c.SetParamValues(cluster.ID)

		// Set team admin context
		setAuthContext(c, teamAdmin)

		// Execute
		err = handler.Get(c)

		// Assert - should be forbidden
		assert.Error(t, err)
		httpErr, ok := err.(*echo.HTTPError)
		require.True(t, ok)
		assert.Equal(t, http.StatusForbidden, httpErr.Code)
	})
}

// TestClusterHandler_RegularUserAccess verifies regular users only see owned clusters
func TestClusterHandler_RegularUserAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("regular user sees only owned clusters", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		policyEngine := policy.NewEngine(nil)
		registry := profile.NewRegistry()
		handler := api.NewClusterHandler(s, policyEngine, registry)

		// Create users and team
		user1 := createTestUser(t, s, "user1@example.com", types.RoleUser)
		user2 := createTestUser(t, s, "user2@example.com", types.RoleUser)

		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Create clusters
		clusterOwned := createTestCluster(t, s, user1.ID, "engineering")
		createTestCluster(t, s, user2.ID, "engineering") // Same team, different owner

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set user1 context
		setAuthContext(c, user1)

		// Execute
		err = handler.List(c)
		require.NoError(t, err)

		// Assert - should only see owned cluster
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		pagination := response["pagination"].(map[string]interface{})
		assert.Equal(t, float64(1), pagination["total"])

		data := response["data"].([]interface{})
		cluster := data[0].(map[string]interface{})
		assert.Equal(t, clusterOwned.ID, cluster["id"])
	})
}

// TestClusterHandler_PlatformAdminAccess verifies platform admins see all clusters
func TestClusterHandler_PlatformAdminAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("platform admin sees all clusters", func(t *testing.T) {
		// Setup
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)

		policyEngine := policy.NewEngine(nil)
		registry := profile.NewRegistry()
		handler := api.NewClusterHandler(s, policyEngine, registry)

		// Create users and teams
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		user1 := createTestUser(t, s, "user1@example.com", types.RoleUser)
		user2 := createTestUser(t, s, "user2@example.com", types.RoleUser)

		teamEng := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		teamSales := &types.Team{Name: "sales", Description: stringPtr("Sales")}
		err := s.Teams.Create(ctx, teamEng)
		require.NoError(t, err)
		err = s.Teams.Create(ctx, teamSales)
		require.NoError(t, err)

		// Create clusters across different teams
		createTestCluster(t, s, admin.ID, "engineering")
		createTestCluster(t, s, user1.ID, "engineering")
		createTestCluster(t, s, user2.ID, "sales")

		// Create request
		e := echo.New()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/clusters", nil)
		rec := httptest.NewRecorder()
		c := e.NewContext(req, rec)

		// Set admin context
		setAuthContext(c, admin)

		// Execute
		err = handler.List(c)
		require.NoError(t, err)

		// Assert - should see all clusters
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		pagination := response["pagination"].(map[string]interface{})
		assert.Equal(t, float64(3), pagination["total"])
	})
}
