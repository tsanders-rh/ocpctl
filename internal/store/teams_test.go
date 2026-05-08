package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TestTeamStore_Create verifies team creation
func TestTeamStore_Create(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("creates team successfully", func(t *testing.T) {
		// Setup test database
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create test user
		user := createTestUser(t, s, "admin@example.com", types.RoleAdmin)

		// Create team
		team := &types.Team{
			Name:        "engineering",
			Description: stringPtr("Engineering team"),
			CreatedBy:   &user.ID,
		}

		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)
		assert.NotEmpty(t, team.ID)
		assert.False(t, team.CreatedAt.IsZero())
		assert.False(t, team.UpdatedAt.IsZero())

		// Verify team was created
		retrieved, err := s.Teams.Get(ctx, "engineering")
		require.NoError(t, err)
		assert.Equal(t, "engineering", retrieved.Name)
		assert.Equal(t, "Engineering team", *retrieved.Description)
		assert.Equal(t, user.ID, *retrieved.CreatedBy)
	})

	t.Run("rejects duplicate team name", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create first team
		team1 := &types.Team{
			Name:        "sales",
			Description: stringPtr("Sales team"),
		}
		err := s.Teams.Create(ctx, team1)
		require.NoError(t, err)

		// Attempt to create duplicate
		team2 := &types.Team{
			Name:        "sales",
			Description: stringPtr("Another sales team"),
		}
		err = s.Teams.Create(ctx, team2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("creates team without created_by", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		team := &types.Team{
			Name:        "auto-created",
			Description: stringPtr("Auto-created from migration"),
		}

		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)
		assert.NotEmpty(t, team.ID)
		assert.Nil(t, team.CreatedBy)
	})
}

// TestTeamStore_List verifies team listing
func TestTeamStore_List(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("lists all teams", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create multiple teams
		teams := []*types.Team{
			{Name: "team-a", Description: stringPtr("Team A")},
			{Name: "team-b", Description: stringPtr("Team B")},
			{Name: "team-c", Description: stringPtr("Team C")},
		}

		for _, team := range teams {
			err := s.Teams.Create(ctx, team)
			require.NoError(t, err)
		}

		// List teams
		retrieved, err := s.Teams.List(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(retrieved), 3)

		// Verify team names exist
		teamNames := make(map[string]bool)
		for _, team := range retrieved {
			teamNames[team.Name] = true
		}
		assert.True(t, teamNames["team-a"])
		assert.True(t, teamNames["team-b"])
		assert.True(t, teamNames["team-c"])
	})

	t.Run("returns empty list when no teams", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		teams, err := s.Teams.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, teams)
	})
}

// TestTeamStore_Get verifies team retrieval by name
func TestTeamStore_Get(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("returns team when exists", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create team
		team := &types.Team{
			Name:        "product",
			Description: stringPtr("Product team"),
		}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Retrieve team
		retrieved, err := s.Teams.Get(ctx, "product")
		require.NoError(t, err)
		assert.Equal(t, "product", retrieved.Name)
		assert.Equal(t, "Product team", *retrieved.Description)
	})

	t.Run("returns ErrNotFound when team doesn't exist", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		_, err := s.Teams.Get(ctx, "nonexistent")
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

// TestTeamStore_Update verifies team metadata updates
func TestTeamStore_Update(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("updates description successfully", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create team
		team := &types.Team{
			Name:        "marketing",
			Description: stringPtr("Old description"),
		}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Update description
		updates := map[string]interface{}{
			"description": "New description",
		}
		err = s.Teams.Update(ctx, "marketing", updates)
		require.NoError(t, err)

		// Verify update
		retrieved, err := s.Teams.Get(ctx, "marketing")
		require.NoError(t, err)
		assert.Equal(t, "New description", *retrieved.Description)
		assert.True(t, retrieved.UpdatedAt.After(retrieved.CreatedAt))
	})

	t.Run("returns ErrNotFound for nonexistent team", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		updates := map[string]interface{}{
			"description": "Test",
		}
		err := s.Teams.Update(ctx, "nonexistent", updates)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

// TestTeamStore_Delete verifies team deletion
func TestTeamStore_Delete(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("deletes team successfully when no clusters", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create team
		team := &types.Team{
			Name:        "temp-team",
			Description: stringPtr("Temporary team"),
		}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Delete team
		err = s.Teams.Delete(ctx, "temp-team")
		require.NoError(t, err)

		// Verify deleted
		_, err = s.Teams.Get(ctx, "temp-team")
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("prevents deletion when clusters exist", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create team
		team := &types.Team{
			Name:        "active-team",
			Description: stringPtr("Active team"),
		}
		err := s.Teams.Create(ctx, team)
		require.NoError(t, err)

		// Create cluster referencing team
		user := createTestUser(t, s, "user@example.com", types.RoleUser)
		cluster := createTestCluster(t, s, user.ID, "active-team")

		// Attempt to delete team
		err = s.Teams.Delete(ctx, "active-team")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "still reference it")

		// Mark cluster as destroyed and try again
		err = s.Clusters.MarkDestroyed(ctx, cluster.ID)
		require.NoError(t, err)

		// Should still prevent deletion (DESTROYED clusters still reference team)
		err = s.Teams.Delete(ctx, "active-team")
		assert.Error(t, err)
	})

	t.Run("returns ErrNotFound for nonexistent team", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		err := s.Teams.Delete(ctx, "nonexistent")
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

// TestTeamStore_GetTeamsWithClusterCounts verifies cluster count aggregation
func TestTeamStore_GetTeamsWithClusterCounts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("returns correct cluster counts", func(t *testing.T) {
		pool := setupTestDB(t)
		defer pool.Close()
		s := store.New(pool)
		ctx := context.Background()

		// Create teams
		teamA := &types.Team{Name: "team-alpha", Description: stringPtr("Team Alpha")}
		teamB := &types.Team{Name: "team-beta", Description: stringPtr("Team Beta")}
		err := s.Teams.Create(ctx, teamA)
		require.NoError(t, err)
		err = s.Teams.Create(ctx, teamB)
		require.NoError(t, err)

		// Create user and clusters
		user := createTestUser(t, s, "owner@example.com", types.RoleUser)
		createTestCluster(t, s, user.ID, "team-alpha")
		createTestCluster(t, s, user.ID, "team-alpha")
		createTestCluster(t, s, user.ID, "team-beta")

		// Get teams with counts
		teams, err := s.Teams.GetTeamsWithClusterCounts(ctx)
		require.NoError(t, err)

		// Verify counts
		counts := make(map[string]int)
		for _, team := range teams {
			counts[team.Name] = team.ClusterCount
		}

		assert.Equal(t, 2, counts["team-alpha"])
		assert.Equal(t, 1, counts["team-beta"])
	})
}

// Helper functions

func stringPtr(s string) *string {
	return &s
}

func createTestUser(t *testing.T, s *store.Store, email string, role types.UserRole) *types.User {
	ctx := context.Background()
	user := &types.User{
		Username: email,
		Email:    email,
		Role:     role,
	}
	err := s.Users.Create(ctx, user)
	require.NoError(t, err)
	return user
}

func createTestCluster(t *testing.T, s *store.Store, ownerID, team string) *types.Cluster {
	ctx := context.Background()
	cluster := &types.Cluster{
		Name:        "test-cluster-" + time.Now().Format("20060102150405"),
		Platform:    types.PlatformAWS,
		ClusterType: types.ClusterTypeOpenShift,
		Version:     "4.20",
		Profile:     "aws-sno-ga",
		Region:      "us-east-1",
		Owner:       "test@example.com",
		OwnerID:     ownerID,
		Team:        team,
		CostCenter:  "test",
		Status:      types.ClusterStatusReady,
		TTLHours:    72,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	err := s.Clusters.Create(ctx, cluster)
	require.NoError(t, err)
	return cluster
}
