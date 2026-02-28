# Phase 2: Authentication & Authorization - Design Document

**Date**: February 28, 2026
**Status**: Planning
**Target**: JWT-based auth for Next.js web frontend

## Overview

Phase 2 adds authentication and authorization to ocpctl, enabling multi-user access with role-based permissions. Designed specifically to work seamlessly with a Next.js frontend.

## Architecture

### Auth Strategy: JWT (JSON Web Tokens)

**Why JWT?**
- Stateless (scales horizontally)
- Works perfectly with Next.js (both SSR and client-side)
- Industry standard for API authentication
- Can be stored in httpOnly cookies (secure) or localStorage

**Token Types:**
1. **Access Token** (short-lived, 15 minutes)
   - Used for API requests
   - Contains: user_id, email, role, permissions
   - Stored in httpOnly cookie or Authorization header

2. **Refresh Token** (long-lived, 7 days)
   - Used to get new access tokens
   - Stored in database for revocation
   - Stored in httpOnly cookie

### User Model

```go
type User struct {
    ID           string    // UUID
    Email        string    // Unique, used for login
    Username     string    // Display name
    PasswordHash string    // bcrypt hash
    Role         UserRole  // ADMIN, USER, VIEWER
    Active       bool      // Can be disabled
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type UserRole string
const (
    RoleAdmin  UserRole = "ADMIN"
    RoleUser   UserRole = "USER"
    RoleViewer UserRole = "VIEWER"
)
```

### Roles & Permissions

| Role | Permissions |
|------|-------------|
| **ADMIN** | All operations, user management, view all clusters |
| **USER** | Create/delete own clusters, extend own clusters, view own clusters |
| **VIEWER** | View own clusters only (read-only) |

### Database Schema

**New Tables:**

```sql
-- Users table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    username VARCHAR(100) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL DEFAULT 'USER',
    active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_active ON users(active);

-- Refresh tokens (for revocation)
CREATE TABLE refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL UNIQUE,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMP
);

CREATE INDEX idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens(expires_at);
```

**Schema Changes:**

```sql
-- Add owner to clusters table
ALTER TABLE clusters ADD COLUMN owner_id UUID REFERENCES users(id);
CREATE INDEX idx_clusters_owner_id ON clusters(owner_id);

-- Migrate existing clusters to a default admin user
-- (handled in migration script)
```

## API Changes

### New Auth Endpoints

```
POST   /api/v1/auth/register      # Create new user (admin only)
POST   /api/v1/auth/login         # Login with email/password
POST   /api/v1/auth/logout        # Logout (revoke refresh token)
POST   /api/v1/auth/refresh       # Get new access token
GET    /api/v1/auth/me            # Get current user info
PATCH  /api/v1/auth/me            # Update current user
PATCH  /api/v1/auth/password      # Change password

# Admin only
GET    /api/v1/users              # List users
POST   /api/v1/users              # Create user
GET    /api/v1/users/:id          # Get user
PATCH  /api/v1/users/:id          # Update user
DELETE /api/v1/users/:id          # Delete user
```

### Updated Cluster Endpoints (with auth)

```
POST   /api/v1/clusters           # Auth required, creates cluster owned by user
GET    /api/v1/clusters           # Auth required, users see own clusters, admins see all
GET    /api/v1/clusters/:id       # Auth required, must own cluster or be admin
DELETE /api/v1/clusters/:id       # Auth required, must own cluster or be admin
PATCH  /api/v1/clusters/:id/extend # Auth required, must own cluster or be admin

# Existing endpoints remain unchanged (profiles, jobs are public or admin-only)
```

### Request/Response Examples

**Login:**
```bash
POST /api/v1/auth/login
{
  "email": "user@example.com",
  "password": "password123"
}

Response 200:
{
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "username": "John Doe",
    "role": "USER"
  },
  "access_token": "eyJhbGc...",
  "expires_in": 900
}

# Sets httpOnly cookie: refresh_token=...
```

**Create Cluster (authenticated):**
```bash
POST /api/v1/clusters
Authorization: Bearer eyJhbGc...

{
  "name": "my-cluster",
  "profile": "aws-dev-small",
  ...
}

# Cluster automatically assigned to authenticated user
```

## Implementation Components

### 1. Auth Package (`internal/auth/`)

**`auth.go`** - Core auth logic
```go
type Auth struct {
    jwtSecret     []byte
    accessTTL     time.Duration  // 15 minutes
    refreshTTL    time.Duration  // 7 days
}

func (a *Auth) GenerateAccessToken(user *types.User) (string, error)
func (a *Auth) GenerateRefreshToken(user *types.User) (string, error)
func (a *Auth) ValidateAccessToken(token string) (*Claims, error)
func (a *Auth) ValidateRefreshToken(token string) (*Claims, error)
```

**`password.go`** - Password hashing
```go
func HashPassword(password string) (string, error)
func CheckPassword(password, hash string) error
func ValidatePasswordStrength(password string) error
```

**`middleware.go`** - HTTP middleware
```go
func RequireAuth() echo.MiddlewareFunc
func RequireRole(roles ...UserRole) echo.MiddlewareFunc
func RequireAdmin() echo.MiddlewareFunc
```

**`claims.go`** - JWT claims
```go
type Claims struct {
    UserID   string   `json:"user_id"`
    Email    string   `json:"email"`
    Role     string   `json:"role"`
    jwt.RegisteredClaims
}
```

### 2. Store Updates (`internal/store/`)

**`users.go`** - User store
```go
type UserStore struct { pool *pgxpool.Pool }

func (s *UserStore) Create(ctx context.Context, user *types.User) error
func (s *UserStore) GetByID(ctx context.Context, id string) (*types.User, error)
func (s *UserStore) GetByEmail(ctx context.Context, email string) (*types.User, error)
func (s *UserStore) Update(ctx context.Context, user *types.User) error
func (s *UserStore) Delete(ctx context.Context, id string) error
func (s *UserStore) List(ctx context.Context) ([]*types.User, error)
```

**`refresh_tokens.go`** - Refresh token store
```go
type RefreshTokenStore struct { pool *pgxpool.Pool }

func (s *RefreshTokenStore) Create(ctx context.Context, token *types.RefreshToken) error
func (s *RefreshTokenStore) GetByToken(ctx context.Context, tokenHash string) (*types.RefreshToken, error)
func (s *RefreshTokenStore) Revoke(ctx context.Context, tokenHash string) error
func (s *RefreshTokenStore) RevokeAllForUser(ctx context.Context, userID string) error
func (s *RefreshTokenStore) CleanupExpired(ctx context.Context) error
```

**`clusters.go`** - Update existing methods
```go
// Add owner_id parameter
func (s *ClusterStore) Create(ctx context.Context, cluster *types.Cluster, ownerID string) error

// Add filtering by owner
func (s *ClusterStore) ListByOwner(ctx context.Context, ownerID string) ([]*types.Cluster, error)
```

### 3. API Handlers (`internal/api/`)

**`handler_auth.go`** - Auth endpoints
```go
func (h *AuthHandler) Login(c echo.Context) error
func (h *AuthHandler) Logout(c echo.Context) error
func (h *AuthHandler) Refresh(c echo.Context) error
func (h *AuthHandler) GetMe(c echo.Context) error
func (h *AuthHandler) UpdateMe(c echo.Context) error
func (h *AuthHandler) ChangePassword(c echo.Context) error
```

**`handler_users.go`** - User management (admin only)
```go
func (h *UserHandler) List(c echo.Context) error
func (h *UserHandler) Create(c echo.Context) error
func (h *UserHandler) Get(c echo.Context) error
func (h *UserHandler) Update(c echo.Context) error
func (h *UserHandler) Delete(c echo.Context) error
```

**Update `handler_clusters.go`**
- Add auth middleware
- Filter clusters by owner (unless admin)
- Set owner on creation

### 4. Types (`pkg/types/`)

**`user.go`**
```go
type User struct {
    ID           string
    Email        string
    Username     string
    PasswordHash string
    Role         UserRole
    Active       bool
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type UserRole string
const (
    RoleAdmin  UserRole = "ADMIN"
    RoleUser   UserRole = "USER"
    RoleViewer UserRole = "VIEWER"
)
```

**`refresh_token.go`**
```go
type RefreshToken struct {
    ID        string
    UserID    string
    TokenHash string
    ExpiresAt time.Time
    CreatedAt time.Time
    RevokedAt *time.Time
}
```

## Security Considerations

### Password Security
- **bcrypt** for password hashing (cost factor: 12)
- Minimum password length: 8 characters
- Require: uppercase, lowercase, number, special char

### JWT Security
- Secret key stored in environment variable (32+ bytes)
- Access tokens expire in 15 minutes
- Refresh tokens expire in 7 days
- Refresh tokens stored in database for revocation

### Cookie Security (for Next.js)
```go
cookie := &http.Cookie{
    Name:     "refresh_token",
    Value:    token,
    HttpOnly: true,      // Prevent XSS
    Secure:   true,      // HTTPS only (production)
    SameSite: http.SameSiteStrictMode,
    MaxAge:   7 * 24 * 60 * 60, // 7 days
    Path:     "/api/v1/auth",
}
```

### CORS Configuration
```go
e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
    AllowOrigins:     []string{"http://localhost:3000"}, // Next.js dev
    AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete},
    AllowHeaders:     []string{echo.HeaderAuthorization, echo.HeaderContentType},
    AllowCredentials: true, // For cookies
}))
```

## Migration Strategy

### 1. Create Default Admin User
```sql
-- Migration creates default admin
INSERT INTO users (id, email, username, password_hash, role, active)
VALUES (
    gen_random_uuid(),
    'admin@localhost',
    'Admin User',
    '$2a$12$...', -- Hash of 'changeme'
    'ADMIN',
    true
);
```

### 2. Migrate Existing Clusters
```sql
-- Assign existing clusters to admin user
UPDATE clusters
SET owner_id = (SELECT id FROM users WHERE role = 'ADMIN' LIMIT 1)
WHERE owner_id IS NULL;
```

### 3. Bootstrap Script
```bash
# Create initial admin user
./bin/ocpctl-admin create-user \
  --email admin@example.com \
  --username "Admin" \
  --password "secure-password" \
  --role ADMIN
```

## Next.js Integration

### Auth Context (client-side)
```typescript
// context/AuthContext.tsx
const { user, login, logout, loading } = useAuth();

// Usage in components
if (!user) return <LoginPage />;
```

### API Client (with auth)
```typescript
// lib/api.ts
const api = axios.create({
  baseURL: '/api/v1',
  withCredentials: true, // Send cookies
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('access_token');
  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

api.interceptors.response.use(
  (response) => response,
  async (error) => {
    if (error.response?.status === 401) {
      // Try refresh
      await refreshToken();
      // Retry request
    }
    return Promise.reject(error);
  }
);
```

### Protected Routes
```typescript
// middleware.ts (Next.js 13+)
export function middleware(request: NextRequest) {
  const token = request.cookies.get('access_token');
  if (!token) {
    return NextResponse.redirect(new URL('/login', request.url));
  }
}

export const config = {
  matcher: ['/dashboard/:path*', '/clusters/:path*'],
};
```

## Testing Strategy

### Unit Tests
- Password hashing/validation
- JWT generation/validation
- User CRUD operations
- Token refresh flow

### Integration Tests
- Login flow (email/password → tokens)
- Protected endpoint access
- Role-based authorization
- Cluster ownership filtering

### E2E Tests
- User registration → login → create cluster → logout
- Admin: create user → assign role → view all clusters
- Token refresh on expiration

## Environment Variables

```bash
# JWT Configuration
JWT_SECRET=your-super-secret-key-min-32-chars
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=168h  # 7 days

# Password Requirements
MIN_PASSWORD_LENGTH=8
REQUIRE_SPECIAL_CHARS=true

# CORS (for Next.js)
CORS_ALLOWED_ORIGINS=http://localhost:3000,https://ocpctl.example.com

# Default Admin
DEFAULT_ADMIN_EMAIL=admin@localhost
DEFAULT_ADMIN_PASSWORD=changeme
```

## Implementation Order

1. **Database Migration** - Add users, refresh_tokens tables
2. **Types & Models** - User, RefreshToken, updated Cluster
3. **Auth Package** - JWT, password hashing, middleware
4. **Store Layer** - UserStore, RefreshTokenStore
5. **Auth Handlers** - Login, logout, refresh, me
6. **User Handlers** - Admin user management
7. **Update Cluster Handlers** - Add auth, ownership
8. **Testing** - Unit + integration tests
9. **Documentation** - API docs, Next.js integration guide

## Files to Create

```
internal/auth/
├── auth.go          (~200 LOC)
├── password.go      (~100 LOC)
├── middleware.go    (~150 LOC)
└── claims.go        (~50 LOC)

internal/store/
├── users.go         (~250 LOC)
├── refresh_tokens.go (~150 LOC)

internal/api/
├── handler_auth.go  (~400 LOC)
├── handler_users.go (~300 LOC)

pkg/types/
├── user.go          (~50 LOC)
├── refresh_token.go (~30 LOC)

internal/store/migrations/
└── 00002_add_auth.sql (~100 LOC)

Total: ~8 new files, ~1,780 LOC
```

## Success Criteria

- [x] Users can register and login
- [x] JWT tokens issued and validated
- [x] Refresh token flow works
- [x] Role-based access control enforced
- [x] Users can only see/manage their own clusters
- [x] Admins can see/manage all clusters
- [x] Passwords securely hashed
- [x] CORS configured for Next.js
- [x] All existing functionality preserved

## Timeline Estimate

- Database migration: 1 hour
- Auth package: 3-4 hours
- Store layer: 2-3 hours
- API handlers: 4-5 hours
- Testing: 2-3 hours
- Documentation: 1 hour

**Total: 1-2 days**

---

Next: Start implementation with database migration and types.
