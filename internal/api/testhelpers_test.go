package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

var ctx = context.Background()

// setupTestDB returns a pgx pool connected to a real PostgreSQL test database.
// It is not yet wired up (a testcontainers-based harness is the intended
// implementation), so it skips the calling test rather than returning a nil pool.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	t.Skip("integration test: requires a PostgreSQL test database (not configured)")
	return nil
}

func createTestUser(t *testing.T, s *store.Store, email string, role types.UserRole) *types.User {
	t.Helper()
	user := &types.User{
		Email:    email,
		Username: email,
		Role:     role,
		Active:   true,
	}
	if err := s.Users.Create(ctx, user); err != nil {
		t.Fatalf("create test user %s: %v", email, err)
	}
	return user
}

func createTestCluster(t *testing.T, s *store.Store, ownerID, team string) *types.Cluster {
	t.Helper()
	cluster := &types.Cluster{
		Name:        "test-cluster-" + time.Now().Format("20060102150405.000000000"),
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
	if err := s.Clusters.Create(ctx, cluster); err != nil {
		t.Fatalf("create test cluster: %v", err)
	}
	return cluster
}

func stringPtr(s string) *string {
	return &s
}
