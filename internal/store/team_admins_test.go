package store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TestTeamAdminStore_GrantTeamAdmin verifies granting team admin privileges
func TestTeamAdminStore_GrantTeamAdmin(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("grants privilege successfully", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create users and team
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Grant privilege
		notes := "Team lead for engineering"
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, &notes)
		require.NoError(t, err)

		// Verify privilege was granted
		isAdmin, err := s.TeamAdmins.IsTeamAdmin(ctx, teamAdmin.ID, "engineering")
		require.NoError(t, err)
		assert.True(t, isAdmin)

		// Verify managed teams includes engineering
		teams, err := s.TeamAdmins.GetManagedTeams(ctx, teamAdmin.ID)
		require.NoError(t, err)
		assert.Contains(t, teams, "engineering")
	})

	t.Run("rejects user without TEAM_ADMIN role", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create users and team
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		regularUser := createTestUser(t, s, "user@example.com", types.RoleUser)
		team := &types.Team{Name: "sales", Description: stringPtr("Sales")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Attempt to grant privilege to regular user
		err = s.TeamAdmins.GrantTeamAdmin(ctx, regularUser.ID, "sales", admin.ID, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "must have TEAM_ADMIN or ADMIN role")
	})

	t.Run("prevents duplicate grants", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create users and team
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		team := &types.Team{Name: "product", Description: stringPtr("Product")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Grant privilege first time
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "product", admin.ID, nil)
		require.NoError(t, err)

		// Attempt to grant again
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "product", admin.ID, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already has team admin privileges")
	})

	t.Run("returns ErrNotFound for nonexistent user", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create team
		team := &types.Team{Name: "marketing", Description: stringPtr("Marketing")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Attempt to grant to nonexistent user
		err = s.TeamAdmins.GrantTeamAdmin(ctx, "nonexistent-user-id", "marketing", "admin-id", nil)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

// TestTeamAdminStore_RevokeTeamAdmin verifies revoking team admin privileges
func TestTeamAdminStore_RevokeTeamAdmin(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("revokes privilege successfully", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create users and team
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Grant privilege
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)

		// Revoke privilege
		err = s.TeamAdmins.RevokeTeamAdmin(ctx, teamAdmin.ID, "engineering")
		require.NoError(t, err)

		// Verify privilege was revoked
		isAdmin, err := s.TeamAdmins.IsTeamAdmin(ctx, teamAdmin.ID, "engineering")
		require.NoError(t, err)
		assert.False(t, isAdmin)

		// Verify managed teams no longer includes engineering
		teams, err := s.TeamAdmins.GetManagedTeams(ctx, teamAdmin.ID)
		require.NoError(t, err)
		assert.NotContains(t, teams, "engineering")
	})

	t.Run("returns ErrNotFound when mapping doesn't exist", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create user
		user := createTestUser(t, s, "user@example.com", types.RoleTeamAdmin)

		// Attempt to revoke non-existent privilege
		err := s.TeamAdmins.RevokeTeamAdmin(ctx, user.ID, "nonexistent-team")
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

// TestTeamAdminStore_GetManagedTeams verifies retrieval of managed teams
func TestTeamAdminStore_GetManagedTeams(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("returns all managed teams", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create users and teams
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)

		teams := []string{"engineering", "product", "sales"}
		for _, teamName := range teams {
			team := &types.Team{Name: teamName, Description: stringPtr(teamName)}
			err := s.Teams.Create(ctx, team)
			require.NoError(t, err)

			err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, teamName, admin.ID, nil)
			require.NoError(t, err)
		}

		// Get managed teams
		managedTeams, err := s.TeamAdmins.GetManagedTeams(ctx, teamAdmin.ID)
		require.NoError(t, err)
		assert.ElementsMatch(t, teams, managedTeams)
	})

	t.Run("returns empty slice when no teams managed", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create user with no team admin privileges
		user := createTestUser(t, s, "user@example.com", types.RoleTeamAdmin)

		managedTeams, err := s.TeamAdmins.GetManagedTeams(ctx, user.ID)
		require.NoError(t, err)
		assert.Empty(t, managedTeams)
	})
}

// TestTeamAdminStore_ListTeamAdmins verifies listing admins for a team
func TestTeamAdminStore_ListTeamAdmins(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("returns all team admins with user details", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create users and team
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin1 := createTestUser(t, s, "teamadmin1@example.com", types.RoleTeamAdmin)
		teamAdmin2 := createTestUser(t, s, "teamadmin2@example.com", types.RoleTeamAdmin)
		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Grant privileges to both users
		notes1 := "Lead engineer"
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin1.ID, "engineering", admin.ID, &notes1)
		require.NoError(t, err)

		notes2 := "Senior engineer"
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin2.ID, "engineering", admin.ID, &notes2)
		require.NoError(t, err)

		// List team admins
		admins, err := s.TeamAdmins.ListTeamAdmins(ctx, "engineering")
		require.NoError(t, err)
		assert.Len(t, admins, 2)

		// Verify user details are included
		for _, adminResp := range admins {
			assert.NotEmpty(t, adminResp.UserID)
			assert.NotEmpty(t, adminResp.Username)
			assert.NotEmpty(t, adminResp.Email)
			assert.Equal(t, types.RoleTeamAdmin, adminResp.Role)
			assert.Equal(t, "engineering", adminResp.Team)
		}
	})

	t.Run("returns empty slice when no admins for team", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create team with no admins
		team := &types.Team{Name: "sales", Description: stringPtr("Sales")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		admins, err := s.TeamAdmins.ListTeamAdmins(ctx, "sales")
		require.NoError(t, err)
		assert.Empty(t, admins)
	})
}

// TestTeamAdminStore_IsTeamAdmin verifies team admin check
func TestTeamAdminStore_IsTeamAdmin(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("returns true for team admin", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create users and team
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Grant privilege
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)

		// Check privilege
		isAdmin, err := s.TeamAdmins.IsTeamAdmin(ctx, teamAdmin.ID, "engineering")
		require.NoError(t, err)
		assert.True(t, isAdmin)
	})

	t.Run("returns false for non-admin", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create user (no privilege granted)
		user := createTestUser(t, s, "user@example.com", types.RoleTeamAdmin)

		// Check privilege
		isAdmin, err := s.TeamAdmins.IsTeamAdmin(ctx, user.ID, "engineering")
		require.NoError(t, err)
		assert.False(t, isAdmin)
	})
}

// TestTeamAdminStore_RevokeAllForUser verifies bulk revocation
func TestTeamAdminStore_RevokeAllForUser(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("revokes all privileges for user", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create users and teams
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)

		teams := []string{"engineering", "product", "sales"}
		for _, teamName := range teams {
			team := &types.Team{Name: teamName, Description: stringPtr(teamName)}
			err := s.Teams.Create(ctx, team)
			require.NoError(t, err)

			err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, teamName, admin.ID, nil)
			require.NoError(t, err)
		}

		// Verify all privileges granted
		managedTeams, err := s.TeamAdmins.GetManagedTeams(ctx, teamAdmin.ID)
		require.NoError(t, err)
		assert.Len(t, managedTeams, 3)

		// Revoke all privileges
		err = s.TeamAdmins.RevokeAllForUser(ctx, teamAdmin.ID)
		require.NoError(t, err)

		// Verify all privileges revoked
		managedTeams, err = s.TeamAdmins.GetManagedTeams(ctx, teamAdmin.ID)
		require.NoError(t, err)
		assert.Empty(t, managedTeams)
	})

	t.Run("succeeds when user has no privileges", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create user with no privileges
		user := createTestUser(t, s, "user@example.com", types.RoleTeamAdmin)

		// Revoke all (should not error)
		err := s.TeamAdmins.RevokeAllForUser(ctx, user.ID)
		require.NoError(t, err)
	})
}

// Test integration with user deletion
func TestTeamAdminStore_CascadeDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("deletes mappings when user is deleted", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create users and team
		admin := createTestUser(t, s, "admin@example.com", types.RoleAdmin)
		teamAdmin := createTestUser(t, s, "teamadmin@example.com", types.RoleTeamAdmin)
		team := &types.Team{Name: "engineering", Description: stringPtr("Engineering")}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Grant privilege
		err = s.TeamAdmins.GrantTeamAdmin(ctx, teamAdmin.ID, "engineering", admin.ID, nil)
		require.NoError(t, err)

		// Delete user
		err = s.Users.Delete(ctx, teamAdmin.ID)
		require.NoError(t, err)

		// Verify mappings were cascade deleted
		// (ListTeamAdmins should not return the deleted user)
		admins, err := s.TeamAdmins.ListTeamAdmins(ctx, "engineering")
		require.NoError(t, err)
		assert.Empty(t, admins)
	})
}
