# Team Admin RBAC - Testing Guide

This document describes the comprehensive test suite for the Team Admin RBAC feature.

## Test Coverage

### Store Layer Tests (Database Operations)

#### `internal/store/teams_test.go`
**Coverage**: Team CRUD operations

- ✅ Create team successfully
- ✅ Reject duplicate team names
- ✅ Create team without created_by
- ✅ List all teams
- ✅ Return empty list when no teams
- ✅ Get team by name
- ✅ Return ErrNotFound for nonexistent team
- ✅ Update team description
- ✅ Delete team when no clusters exist
- ✅ Prevent deletion when clusters exist
- ✅ Get teams with cluster counts

**Test Count**: 12 tests

#### `internal/store/team_admins_test.go`
**Coverage**: Team admin privilege management

- ✅ Grant privilege successfully
- ✅ Reject user without TEAM_ADMIN role
- ✅ Prevent duplicate grants
- ✅ Return ErrNotFound for nonexistent user
- ✅ Revoke privilege successfully
- ✅ Return ErrNotFound when mapping doesn't exist
- ✅ Get managed teams for user
- ✅ Return empty slice when no teams managed
- ✅ List team admins with user details
- ✅ Return empty slice when no admins for team
- ✅ Check if user is team admin
- ✅ Return false for non-admin
- ✅ Revoke all privileges for user
- ✅ Succeed when user has no privileges
- ✅ Cascade delete mappings when user is deleted

**Test Count**: 15 tests

#### `internal/store/clusters_teamadmin_test.go`
**Coverage**: Team-scoped cluster filtering and access control

- ✅ Team admin sees owned clusters and managed team clusters
- ✅ Team admin with multiple managed teams sees all relevant clusters
- ✅ Team admin with no managed teams sees only owned clusters
- ✅ Team filter with team admin access validation
- ✅ Regular user sees only owned clusters
- ✅ Platform admin sees all clusters
- ✅ Efficient SQL execution with OR condition
- ✅ Pagination works correctly with team admin filter

**Test Count**: 8 tests

**SQL Query Performance Validation**:
- Verifies `owner_id = $1 OR team = ANY($2)` executes in single query
- Tests with 100 clusters across 5 teams
- Validates correct result set without N+1 queries

### API Layer Tests (HTTP Handlers)

#### `internal/api/handler_teams_test.go`
**Coverage**: Team management API endpoints

- ✅ List teams returns all teams for admin
- ✅ List teams requires admin role
- ✅ Create team successfully
- ✅ Reject invalid team name
- ✅ Reject duplicate team name
- ✅ Grant team admin privilege successfully
- ✅ Reject user without TEAM_ADMIN role
- ✅ Revoke team admin privilege successfully
- ✅ Return 404 for nonexistent mapping
- ✅ Delete team when no clusters exist
- ✅ Prevent deletion when clusters exist

**Test Count**: 11 tests

**API Endpoints Tested**:
- `GET /admin/teams` - List teams
- `POST /admin/teams` - Create team
- `GET /admin/teams/:name` - Get team
- `PATCH /admin/teams/:name` - Update team
- `DELETE /admin/teams/:name` - Delete team
- `GET /admin/teams/:name/admins` - List team admins
- `POST /admin/teams/:name/admins` - Grant privilege
- `DELETE /admin/teams/:name/admins/:user_id` - Revoke privilege

#### `internal/api/handler_clusters_teamadmin_test.go`
**Coverage**: Cluster access control with team admin role

- ✅ Team admin sees owned and managed team clusters
- ✅ Team admin with multiple teams sees all relevant clusters
- ✅ Team admin filtering by specific team
- ✅ Team admin cannot filter by non-managed team
- ✅ Team admin can access owned cluster
- ✅ Team admin can access managed team cluster
- ✅ Team admin cannot access non-managed team cluster
- ✅ Regular user sees only owned clusters
- ✅ Platform admin sees all clusters

**Test Count**: 9 tests

**Access Control Scenarios**:
1. **Team Admin with single managed team**:
   - ✅ Can view/manage owned clusters (any team)
   - ✅ Can view/manage all clusters in managed team
   - ❌ Cannot access clusters in non-managed teams

2. **Team Admin with multiple managed teams**:
   - ✅ Can view/manage owned clusters (any team)
   - ✅ Can view/manage clusters in all managed teams
   - ❌ Cannot access clusters in non-managed teams

3. **Team Admin with no managed teams**:
   - ✅ Can view/manage only owned clusters
   - ❌ Cannot access other users' clusters (same team or different)

4. **Regular User**:
   - ✅ Can view/manage only owned clusters
   - ❌ Cannot access team clusters

5. **Platform Admin**:
   - ✅ Can view/manage all clusters (no restrictions)

## Total Test Coverage

**Total Tests**: 55 comprehensive integration tests

**Lines of Test Code**: ~2000+ lines

**Coverage Areas**:
- ✅ Database schema and migrations
- ✅ CRUD operations for teams and team admins
- ✅ SQL query correctness and performance
- ✅ Authorization logic for all user roles
- ✅ API endpoint security and validation
- ✅ Edge cases and error handling
- ✅ Cascade deletion and referential integrity
- ✅ Pagination with team-scoped filtering

## Running Tests

### Prerequisites

Tests require a PostgreSQL database. The test suite uses testcontainers to spin up temporary PostgreSQL instances.

```bash
# Install testcontainers dependency
go get github.com/testcontainers/testcontainers-go
go get github.com/testcontainers/testcontainers-go/modules/postgres
```

### Run All Tests

```bash
# Run all tests (including integration tests)
go test ./internal/store/... ./internal/api/... -v

# Run with race detection
go test ./internal/store/... ./internal/api/... -race -v
```

### Run Specific Test Suites

```bash
# Store layer tests only
go test ./internal/store -v

# Team admin store tests only
go test ./internal/store -run TestTeamAdminStore -v

# Cluster filtering tests only
go test ./internal/store -run TestClusterStore_List_TeamAdminAccess -v

# API handler tests only
go test ./internal/api -v

# Team management API tests only
go test ./internal/api -run TestTeamHandler -v

# Cluster access control API tests only
go test ./internal/api -run TestClusterHandler.*TeamAdmin -v
```

### Skip Integration Tests

```bash
# Run only unit tests (skip integration tests)
go test ./internal/store/... ./internal/api/... -short -v
```

### Run with Coverage

```bash
# Generate coverage report
go test ./internal/store/... ./internal/api/... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html

# View coverage in browser
open coverage.html
```

## Test Database Setup

Integration tests use testcontainers to automatically:
1. Spin up a PostgreSQL 15 container
2. Run all migrations (00001-00043)
3. Create test data
4. Clean up after tests

**No manual database setup required!**

## Manual Testing Checklist

After running automated tests, verify the following manually:

### 1. Team Admin User Flow
- [ ] Create team admin user
- [ ] Grant team admin privilege for specific team
- [ ] Login as team admin
- [ ] Verify can see owned clusters + team clusters
- [ ] Verify cannot see other team clusters
- [ ] Revoke team admin privilege
- [ ] Verify falls back to seeing only owned clusters

### 2. Platform Admin Operations
- [ ] Create multiple teams
- [ ] Grant team admin privileges to different users
- [ ] Verify team admins list shows correct users
- [ ] Revoke team admin privileges
- [ ] Delete teams (should fail if clusters exist)
- [ ] Delete teams (should succeed if no clusters)

### 3. API Authorization
- [ ] Test `/admin/teams` endpoints require admin role
- [ ] Test team admin can access managed team clusters via `/api/v1/clusters`
- [ ] Test team admin cannot access non-managed team clusters
- [ ] Test regular user still only sees owned clusters

### 4. Database Integrity
- [ ] Verify foreign key constraints work
- [ ] Verify cascade delete on user deletion
- [ ] Verify unique constraints on team names
- [ ] Verify team deletion prevention when clusters exist

## Performance Testing

### SQL Query Performance

Test queries under load to ensure efficient execution:

```sql
-- Test OwnerIDOrTeams filter with 10,000 clusters
EXPLAIN ANALYZE
SELECT * FROM clusters
WHERE (owner_id = 'user-uuid' OR team = ANY(ARRAY['team-a', 'team-b', 'team-c']))
ORDER BY created_at DESC
LIMIT 50 OFFSET 0;
```

**Expected Performance**:
- Query execution time: < 50ms for 10,000 clusters
- Index usage: Both `idx_clusters_owner_id` and `idx_clusters_team` should be used
- No sequential scans on large tables

### API Response Times

Test API endpoints under load:

```bash
# Test cluster list endpoint with team admin filter
ab -n 1000 -c 10 -H "Authorization: Bearer $JWT_TOKEN" \
  http://localhost:8080/api/v1/clusters

# Test team list endpoint
ab -n 1000 -c 10 -H "Authorization: Bearer $JWT_TOKEN" \
  http://localhost:8080/admin/teams
```

**Expected Performance**:
- `/api/v1/clusters`: < 100ms p95, < 200ms p99
- `/admin/teams`: < 50ms p95, < 100ms p99

## Test Data Fixtures

Tests use the following helper functions to create test data:

### User Fixtures
```go
createTestUser(t, s, email, role) *types.User
```

Creates a user with specified email and role:
- `admin@example.com` → `RoleAdmin`
- `teamadmin@example.com` → `RoleTeamAdmin`
- `user@example.com` → `RoleUser`

### Cluster Fixtures
```go
createTestCluster(t, s, ownerID, team) *types.Cluster
```

Creates a cluster with:
- Name: `test-cluster-TIMESTAMP`
- Platform: AWS
- ClusterType: OpenShift
- Status: READY
- TTL: 72 hours

### Team Fixtures
```go
team := &types.Team{
    Name: "engineering",
    Description: stringPtr("Engineering team"),
}
s.Teams.Create(ctx, team)
```

## Debugging Failed Tests

### Enable Debug Logging

```bash
# Run tests with verbose output
go test ./internal/store -v -run TestTeamAdminStore_GrantTeamAdmin

# Run with database query logging (if implemented)
DEBUG_SQL=true go test ./internal/store -v
```

### Common Issues

1. **Database connection errors**: Ensure Docker is running for testcontainers
2. **Migration failures**: Check migration files in `internal/store/migrations/`
3. **Foreign key violations**: Verify test cleanup order
4. **Race conditions**: Run tests with `-race` flag to detect

### Test Isolation

Each test runs in a fresh database:
- ✅ No state shared between tests
- ✅ Parallel test execution supported
- ✅ Deterministic results

## CI/CD Integration

Add to GitHub Actions workflow:

```yaml
- name: Run Integration Tests
  run: |
    docker pull postgres:15
    go test ./internal/store/... ./internal/api/... -v -coverprofile=coverage.out
    go tool cover -html=coverage.out -o coverage.html

- name: Upload Coverage
  uses: actions/upload-artifact@v3
  with:
    name: coverage-report
    path: coverage.html
```

## Next Steps

After all tests pass:

1. **Deploy to staging**: Run full regression suite
2. **Test with production data volume**: Benchmark with 100k+ clusters
3. **Load testing**: Verify API endpoints under concurrent load
4. **Security audit**: Verify no privilege escalation vulnerabilities
5. **Documentation review**: Ensure API docs reflect new team admin endpoints

## Test Maintenance

**Review and update tests when**:
- Adding new team admin features
- Modifying authorization logic
- Changing database schema
- Updating API endpoints
- Performance optimizations

**Goal**: Maintain 80%+ test coverage for all RBAC code paths.
