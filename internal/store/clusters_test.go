package store_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// This is a sample test demonstrating the testing pattern
// Full integration tests would use testcontainers for real PostgreSQL

func TestClusterStore_Create(t *testing.T) {
	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	// In real tests, you would:
	// 1. Start a PostgreSQL container with testcontainers
	// 2. Run migrations
	// 3. Create store instance
	//
	// For now, this is a template showing the test structure

	t.Run("creates cluster successfully", func(t *testing.T) {
		// Setup
		// pool := setupTestDB(t)
		// defer pool.Close()
		// s := store.New(pool)

		// Example cluster for testing:
		// cluster := &types.Cluster{
		//	ID:          types.GenerateClusterID(),
		//	Name:        "test-cluster-01",
		//	Platform:    types.PlatformAWS,
		//	Version:     "4.20.3",
		//	Profile:     "aws-minimal-test",
		//	Region:      "us-east-1",
		//	BaseDomain:  "labs.example.com",
		//	Owner:       "test-user",
		//	Team:        "test-team",
		//	CostCenter:  "test-cost-center",
		//	Status:      types.ClusterStatusPending,
		//	RequestedBy: "arn:aws:iam::123456789012:user/test-user",
		//	TTLHours:    24,
		// }

		// Execute
		// err := s.Clusters.Create(ctx, cluster)

		// Assert
		// require.NoError(t, err)

		// Verify
		// retrieved, err := s.Clusters.GetByID(ctx, cluster.ID)
		// require.NoError(t, err)
		// assert.Equal(t, cluster.Name, retrieved.Name)
		// assert.Equal(t, cluster.Platform, retrieved.Platform)

		t.Log("Test template - implement with testcontainers")
	})

	t.Run("rejects duplicate active cluster name", func(t *testing.T) {
		t.Log("Test template - verify unique constraint works")
	})
}

func TestClusterStore_GetByID(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("returns cluster when exists", func(t *testing.T) {
		t.Log("Test template - implement with testcontainers")
	})

	t.Run("returns ErrNotFound when cluster doesn't exist", func(t *testing.T) {
		t.Log("Test template - implement with testcontainers")
	})
}

func TestClusterStore_UpdateStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("updates status successfully", func(t *testing.T) {
		t.Log("Test template - implement with testcontainers")
	})
}

func TestClusterStore_GetExpiredClusters(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	t.Run("returns clusters past TTL", func(t *testing.T) {
		t.Log("Test template - implement with testcontainers")
		// Create cluster with destroy_at in the past
		// Verify it's returned by GetExpiredClusters
	})
}

// setupTestDB returns a pgx pool connected to a real PostgreSQL test database.
// It is not yet wired up (a testcontainers-based harness is the intended
// implementation), so it skips the calling test rather than returning a nil pool.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	t.Skip("integration test: requires a PostgreSQL test database (not configured)")
	return nil
}
