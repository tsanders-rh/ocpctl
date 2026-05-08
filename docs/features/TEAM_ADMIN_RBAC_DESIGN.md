# Team Admin RBAC Design

**Purpose:** Design document for implementing Team Admin role to enable team-scoped cluster management.

**Issue:** #32 - Add Team Admin role for managing team-scoped clusters

**Date:** 2026-05-08

---

## Problem Statement

Currently, ocpctl has three roles:
- **ADMIN** - Full platform access to all clusters
- **USER** - Can create and manage only their own clusters
- **VIEWER** - Read-only access to own clusters

There is no middle-tier role for team leads who need to manage all clusters within their team without having full platform admin privileges.

**Use Case:**
- Team lead for "engineering" team needs to see and manage all clusters created by team members
- Should not have access to "finance" team clusters
- Should not have platform-wide admin capabilities (creating users, managing IAM mappings, etc.)

---

## Proposed Solution

### New Role: Team Admin

**Capabilities:**
- View and manage all clusters within assigned team(s)
- Cannot create/delete users
- Cannot manage platform-wide settings
- Cannot access clusters from other teams
- Cannot grant team admin privileges (only platform admins can)

**Scope:** Team-scoped permissions based on `cluster.team` field

---

## Database Schema Changes

### Migration: 00042_add_team_admin_role.sql

#### 1. Update users table role constraint

```sql
-- Add TEAM_ADMIN to allowed roles
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('ADMIN', 'USER', 'VIEWER', 'TEAM_ADMIN'));
```

#### 2. Create teams table (optional but recommended)

```sql
-- Teams registry for referential integrity and team metadata
CREATE TABLE IF NOT EXISTS teams (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) UNIQUE NOT NULL,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX idx_teams_name ON teams(name);

COMMENT ON TABLE teams IS 'Team registry for organizational structure and access control';
COMMENT ON COLUMN teams.name IS 'Team identifier (matches cluster.team field)';
```

#### 3. Create user_team_admin_mappings table

```sql
-- Maps users with TEAM_ADMIN role to specific teams they can manage
CREATE TABLE IF NOT EXISTS user_team_admin_mappings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    team VARCHAR(255) NOT NULL,
    granted_by UUID REFERENCES users(id),
    granted_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    notes TEXT,
    UNIQUE(user_id, team)
);

CREATE INDEX idx_user_team_admin_user ON user_team_admin_mappings(user_id);
CREATE INDEX idx_user_team_admin_team ON user_team_admin_mappings(team);

COMMENT ON TABLE user_team_admin_mappings IS 'Grants team admin privileges to users for specific teams';
COMMENT ON COLUMN user_team_admin_mappings.user_id IS 'User being granted team admin privileges';
COMMENT ON COLUMN user_team_admin_mappings.team IS 'Team name that user can administer';
COMMENT ON COLUMN user_team_admin_mappings.granted_by IS 'Platform admin who granted this privilege';
```

#### 4. Add foreign key from clusters to teams (optional)

```sql
-- Optional: Add FK constraint if teams table is created
-- ALTER TABLE clusters ADD CONSTRAINT fk_clusters_team
--   FOREIGN KEY (team) REFERENCES teams(name) ON UPDATE CASCADE;
```

---

## Code Changes

### 1. Add RoleTeamAdmin constant

**File:** `pkg/types/user.go`

```go
const (
    RoleAdmin     UserRole = "ADMIN"
    RoleUser      UserRole = "USER"
    RoleViewer    UserRole = "VIEWER"
    RoleTeamAdmin UserRole = "TEAM_ADMIN"  // NEW
)

func (r UserRole) IsValid() bool {
    switch r {
    case RoleAdmin, RoleUser, RoleViewer, RoleTeamAdmin:
        return true
    default:
        return false
    }
}
```

### 2. Add User.ManagedTeams field

**File:** `pkg/types/user.go`

```go
type User struct {
    // ... existing fields ...
    ManagedTeams []string `json:"managed_teams,omitempty"` // Teams user can administer (for TEAM_ADMIN role)
}
```

### 3. Create store methods for team admin operations

**File:** `internal/store/team_admins.go` (NEW)

```go
package store

type TeamAdminStore struct {
    db *pgxpool.Pool
}

// GetManagedTeams returns list of teams a user can administer
func (s *TeamAdminStore) GetManagedTeams(ctx context.Context, userID string) ([]string, error)

// GrantTeamAdmin adds team admin privilege for a user
func (s *TeamAdminStore) GrantTeamAdmin(ctx context.Context, userID, team, grantedBy string, notes *string) error

// RevokeTeamAdmin removes team admin privilege
func (s *TeamAdminStore) RevokeTeamAdmin(ctx context.Context, userID, team string) error

// ListTeamAdmins returns all users who can administer a given team
func (s *TeamAdminStore) ListTeamAdmins(ctx context.Context, team string) ([]*TeamAdminMapping, error)

// IsTeamAdmin checks if user is admin for specific team
func (s *TeamAdminStore) IsTeamAdmin(ctx context.Context, userID, team string) (bool, error)
```

**File:** `internal/store/teams.go` (NEW)

```go
package store

type TeamStore struct {
    db *pgxpool.Pool
}

// List returns all teams
func (s *TeamStore) List(ctx context.Context) ([]*Team, error)

// Create creates a new team
func (s *TeamStore) Create(ctx context.Context, team *Team) error

// Get retrieves a team by name
func (s *TeamStore) Get(ctx context.Context, name string) (*Team, error)

// Update updates team metadata
func (s *TeamStore) Update(ctx context.Context, name string, updates map[string]interface{}) error

// Delete removes a team (only if no clusters reference it)
func (s *TeamStore) Delete(ctx context.Context, name string) error
```

### 4. Update auth middleware

**File:** `internal/auth/middleware.go`

Add new helper functions:

```go
// IsTeamAdmin checks if the current user is a team admin
func IsTeamAdmin(c echo.Context) bool {
    role, err := GetUserRole(c)
    if err != nil {
        return false
    }
    return role == types.RoleTeamAdmin || role == types.RoleAdmin
}

// GetManagedTeams retrieves teams the current user can administer
func GetManagedTeams(c echo.Context) ([]string, error) {
    user, err := GetUser(c)
    if err != nil {
        return nil, err
    }
    return user.ManagedTeams, nil
}

// RequireTeamAdmin is middleware that requires team admin or platform admin role
func RequireTeamAdmin() echo.MiddlewareFunc {
    return RequireRole(types.RoleTeamAdmin, types.RoleAdmin)
}
```

### 5. Update cluster handler authorization

**File:** `internal/api/handler_clusters.go`

**Update `checkClusterAccess` function:**

```go
// checkClusterAccess verifies the user has access to the cluster
// Returns nil if user is owner, team admin for cluster's team, or platform admin
func (h *ClusterHandler) checkClusterAccess(c echo.Context, cluster *types.Cluster) error {
    // Platform admins can access all clusters
    if auth.IsAdmin(c) {
        return nil
    }

    // Get current user
    userID, err := auth.GetUserID(c)
    if err != nil {
        return err
    }

    // Check if user owns this cluster
    if cluster.OwnerID == userID {
        return nil
    }

    // Check if user is team admin for this cluster's team
    user, err := auth.GetUser(c)
    if err == nil && user.Role == types.RoleTeamAdmin {
        for _, managedTeam := range user.ManagedTeams {
            if managedTeam == cluster.Team {
                return nil
            }
        }
    }

    return ErrorForbidden(c, "You do not have access to this cluster")
}
```

**Update `List` function:**

```go
func (h *ClusterHandler) List(c echo.Context) error {
    ctx := c.Request().Context()

    // Get authenticated user
    userID, err := auth.GetUserID(c)
    if err != nil {
        return err
    }

    user, err := auth.GetUser(c)
    if err != nil {
        return err
    }

    // Parse pagination and filters
    pagination := ParsePaginationParams(c)
    filters := &ListClustersFilters{
        Platform:   c.QueryParam("platform"),
        Profile:    c.QueryParam("profile"),
        Owner:      c.QueryParam("owner"),
        Team:       c.QueryParam("team"),
        CostCenter: c.QueryParam("cost_center"),
        Status:     c.QueryParam("status"),
    }

    // Build list filters
    listFilters := store.ListFilters{
        Limit:  pagination.PerPage,
        Offset: pagination.Offset,
    }

    // Apply role-based filtering
    switch user.Role {
    case types.RoleAdmin:
        // Admins see all clusters, respect all filters
        if filters.Owner != "" {
            listFilters.Owner = &filters.Owner
        }
        if filters.Team != "" {
            listFilters.Team = &filters.Team
        }

    case types.RoleTeamAdmin:
        // Team admins see:
        // 1. Clusters they own
        // 2. Clusters from teams they manage
        if filters.Team != "" {
            // If filtering by team, ensure it's one they manage
            isManaged := false
            for _, team := range user.ManagedTeams {
                if team == filters.Team {
                    isManaged = true
                    break
                }
            }
            if isManaged {
                listFilters.Team = &filters.Team
            } else {
                // Can't filter by team they don't manage, show only owned clusters
                listFilters.OwnerID = &userID
            }
        } else {
            // No team filter: show owned clusters OR managed team clusters
            listFilters.OwnerIDOrTeams = &store.OwnerIDOrTeamsFilter{
                OwnerID: userID,
                Teams:   user.ManagedTeams,
            }
        }

    default:
        // Regular users see only their own clusters
        listFilters.OwnerID = &userID
    }

    // Apply platform/profile/status filters (all roles)
    if filters.Platform != "" {
        platform := types.Platform(filters.Platform)
        listFilters.Platform = &platform
    }
    if filters.Profile != "" {
        listFilters.Profile = &filters.Profile
    }
    if filters.Status != "" {
        status := types.ClusterStatus(filters.Status)
        listFilters.Status = &status
    }

    // Get clusters with total count
    clusters, total, err := h.store.Clusters.List(ctx, listFilters)
    if err != nil {
        return LogAndReturnGenericError(c, fmt.Errorf("failed to list clusters: %w", err))
    }

    paginationMeta := CalculatePagination(pagination.Page, pagination.PerPage, total)
    return SuccessPaginated(c, clusters, paginationMeta, nil)
}
```

### 6. Update cluster store List method

**File:** `internal/store/clusters.go`

```go
type OwnerIDOrTeamsFilter struct {
    OwnerID string
    Teams   []string
}

type ListFilters struct {
    Limit           int
    Offset          int
    OwnerID         *string
    Owner           *string
    Team            *string
    OwnerIDOrTeams  *OwnerIDOrTeamsFilter  // NEW: For team admin filtering
    Platform        *types.Platform
    Profile         *string
    Status          *types.ClusterStatus
}

func (s *ClusterStore) List(ctx context.Context, filters ListFilters) ([]*types.Cluster, int, error) {
    // Build WHERE clause
    conditions := []string{}
    args := []interface{}{}
    argIdx := 1

    // Handle team admin filtering (owner_id OR team IN (...))
    if filters.OwnerIDOrTeams != nil {
        teamPlaceholders := []string{}
        conditions = append(conditions, fmt.Sprintf("(owner_id = $%d OR team = ANY($%d))", argIdx, argIdx+1))
        args = append(args, filters.OwnerIDOrTeams.OwnerID, pq.Array(filters.OwnerIDOrTeams.Teams))
        argIdx += 2
    } else if filters.OwnerID != nil {
        conditions = append(conditions, fmt.Sprintf("owner_id = $%d", argIdx))
        args = append(args, *filters.OwnerID)
        argIdx++
    }

    // ... rest of filtering logic
}
```

---

## API Endpoints

### Team Management (Admin Only)

#### GET /api/v1/admin/teams

List all teams.

**Response:**
```json
{
  "teams": [
    {
      "id": "uuid",
      "name": "engineering",
      "description": "Engineering team",
      "created_at": "2026-01-01T00:00:00Z",
      "updated_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```

#### POST /api/v1/admin/teams

Create a new team.

**Request:**
```json
{
  "name": "engineering",
  "description": "Engineering team"
}
```

#### GET /api/v1/admin/teams/:name/admins

List all team admins for a specific team.

**Response:**
```json
{
  "team": "engineering",
  "admins": [
    {
      "user_id": "uuid",
      "user_email": "lead@example.com",
      "granted_by": "admin@example.com",
      "granted_at": "2026-01-01T00:00:00Z",
      "notes": "Team lead"
    }
  ]
}
```

#### POST /api/v1/admin/teams/:name/admins

Grant team admin privilege to a user.

**Request:**
```json
{
  "user_id": "uuid-of-user",
  "notes": "Promoted to team lead"
}
```

**Requirements:**
- User must have TEAM_ADMIN role set first (via PATCH /api/v1/users/:id)
- Only platform admins can grant team admin privileges

#### DELETE /api/v1/admin/teams/:name/admins/:user_id

Revoke team admin privilege.

### User Information (Authenticated)

#### GET /api/v1/auth/me

**Updated Response** (includes managed_teams for TEAM_ADMIN role):
```json
{
  "id": "uuid",
  "email": "lead@example.com",
  "role": "TEAM_ADMIN",
  "managed_teams": ["engineering", "devops"]
}
```

---

## User Store Changes

**File:** `internal/store/users.go`

Update `GetByID` and `GetByEmail` to populate `ManagedTeams`:

```go
func (s *UserStore) GetByID(ctx context.Context, userID string) (*types.User, error) {
    user := &types.User{}

    // ... existing query ...

    // If user is team admin, load managed teams
    if user.Role == types.RoleTeamAdmin {
        managedTeams, err := s.teamAdminStore.GetManagedTeams(ctx, userID)
        if err != nil {
            return nil, fmt.Errorf("failed to load managed teams: %w", err)
        }
        user.ManagedTeams = managedTeams
    }

    return user, nil
}
```

---

## Authorization Flow

### Example: Team Admin accessing cluster

1. User authenticates (JWT or IAM)
2. Middleware loads user object with `Role = TEAM_ADMIN` and `ManagedTeams = ["engineering"]`
3. User requests GET /api/v1/clusters/:id
4. Handler calls `checkClusterAccess(cluster)`
5. Access check logic:
   ```
   IF user.role == ADMIN -> allow
   ELSE IF cluster.owner_id == user.id -> allow
   ELSE IF user.role == TEAM_ADMIN AND cluster.team IN user.managed_teams -> allow
   ELSE -> deny (403 Forbidden)
   ```

### Example: Team Admin listing clusters

1. User with `Role = TEAM_ADMIN`, `ManagedTeams = ["engineering", "devops"]`
2. Requests GET /api/v1/clusters
3. Store query:
   ```sql
   SELECT * FROM clusters
   WHERE owner_id = 'user-uuid'
      OR team IN ('engineering', 'devops')
   ORDER BY created_at DESC
   LIMIT 50 OFFSET 0;
   ```
4. Returns clusters owned by user OR in managed teams

---

## UI Changes

### Cluster List Page

**For TEAM_ADMIN role:**
- Show "My Clusters" and "Team Clusters" tabs
- "My Clusters" tab: owned clusters
- "Team Clusters" tab: clusters from managed teams (grouped by team)
- Add team filter dropdown (shows only managed teams)

### User Profile Page

**For TEAM_ADMIN role:**
- Display "Managed Teams" section showing list of teams
- Non-editable (only platform admins can modify)

### Admin Panel (ADMIN role only)

**New "Team Management" section:**
- List all teams
- Create new team
- Assign/revoke team admin privileges
- View team admins per team

---

## Security Considerations

1. **Privilege Escalation Prevention:**
   - Team admins cannot grant team admin privileges to others
   - Only platform admins can manage team admin mappings
   - Team admins cannot modify their own team assignments

2. **Audit Trail:**
   - All team admin grants/revokes logged to `audit_events` table
   - `granted_by` field tracks who granted privilege
   - `granted_at` timestamp for temporal tracking

3. **Role Validation:**
   - User must have `role = TEAM_ADMIN` before team assignments
   - Changing user role from TEAM_ADMIN automatically removes team mappings

4. **Team Boundary Enforcement:**
   - All cluster operations validate team membership
   - Team admins cannot create clusters for teams they don't manage
   - Cluster updates cannot change team to one admin doesn't manage

---

## Migration Strategy

### Phase 1: Database Schema
1. Create migration 00042_add_team_admin_role.sql
2. Add teams table
3. Add user_team_admin_mappings table
4. Update users role constraint

### Phase 2: Backend Implementation
1. Add RoleTeamAdmin constant
2. Create TeamAdminStore and TeamStore
3. Update auth middleware helpers
4. Update cluster handler authorization logic
5. Create team management API handlers

### Phase 3: API Testing
1. Test team admin cluster access
2. Test team admin cluster listing
3. Test team admin boundaries (cannot access other teams)
4. Test admin team management endpoints

### Phase 4: UI Implementation
1. Update user profile to show managed teams
2. Add team management admin panel
3. Update cluster list filtering for team admins
4. Add team-scoped cluster views

---

## Testing Checklist

- [ ] Team admin can see own clusters
- [ ] Team admin can see clusters from managed teams
- [ ] Team admin cannot see clusters from other teams
- [ ] Team admin can hibernate/resume/extend managed team clusters
- [ ] Team admin cannot delete users
- [ ] Team admin cannot access admin-only endpoints
- [ ] Platform admin can grant team admin privileges
- [ ] Platform admin can revoke team admin privileges
- [ ] Audit events logged for team admin operations
- [ ] Team filtering works correctly in cluster list
- [ ] Team admin role appears correctly in UI
- [ ] Changing user role from TEAM_ADMIN clears team mappings

---

## Future Enhancements

1. **Team Quotas:** Limit number of clusters per team
2. **Team Budgets:** Cost tracking and limits per team
3. **Team Policies:** Custom TTL limits, allowed profiles per team
4. **Sub-Teams:** Hierarchical team structure
5. **Team Delegated Auth:** Team admins can create users for their team

---

**Status:** Design Complete - Ready for Implementation
**Next Step:** Create database migration (Task #3)
