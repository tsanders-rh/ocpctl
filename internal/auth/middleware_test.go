package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func testAuth() *Auth { return NewAuth("test-secret", time.Hour, 24*time.Hour) }

func newEchoCtx(authHeader string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	return e.NewContext(req, rec), rec
}

func tokenFor(t *testing.T, a *Auth, role types.UserRole) string {
	t.Helper()
	tok, err := a.GenerateAccessToken(&types.User{ID: "u1", Email: "u1@x", Role: role})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return tok
}

func httpCode(err error) int {
	if he, ok := err.(*echo.HTTPError); ok {
		return he.Code
	}
	return 0
}

func pass(c echo.Context) error { return c.String(http.StatusOK, "ok") }

func TestRequireAuth(t *testing.T) {
	a := testAuth()

	t.Run("missing header", func(t *testing.T) {
		c, _ := newEchoCtx("")
		if code := httpCode(RequireAuth(a)(pass)(c)); code != http.StatusUnauthorized {
			t.Errorf("got %d want 401", code)
		}
	})
	t.Run("bad format", func(t *testing.T) {
		c, _ := newEchoCtx("Basic abc")
		if code := httpCode(RequireAuth(a)(pass)(c)); code != http.StatusUnauthorized {
			t.Errorf("got %d want 401", code)
		}
	})
	t.Run("invalid token", func(t *testing.T) {
		c, _ := newEchoCtx("Bearer not-a-jwt")
		if code := httpCode(RequireAuth(a)(pass)(c)); code != http.StatusUnauthorized {
			t.Errorf("got %d want 401", code)
		}
	})
	t.Run("valid token sets claims", func(t *testing.T) {
		c, _ := newEchoCtx("Bearer " + tokenFor(t, a, types.RoleUser))
		if err := RequireAuth(a)(pass)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := c.Get(string(ClaimsContextKey)).(*Claims); !ok {
			t.Error("expected claims stored in context")
		}
	})
}

func TestRequireRoleAndAdmin(t *testing.T) {
	setClaims := func(role types.UserRole) echo.Context {
		c, _ := newEchoCtx("")
		c.Set(string(ClaimsContextKey), &Claims{UserID: "u1", Role: string(role)})
		return c
	}

	t.Run("no claims", func(t *testing.T) {
		c, _ := newEchoCtx("")
		if code := httpCode(RequireRole(types.RoleAdmin)(pass)(c)); code != http.StatusUnauthorized {
			t.Errorf("got %d want 401", code)
		}
	})
	t.Run("wrong role", func(t *testing.T) {
		if code := httpCode(RequireRole(types.RoleAdmin)(pass)(setClaims(types.RoleUser))); code != http.StatusForbidden {
			t.Errorf("got %d want 403", code)
		}
	})
	t.Run("right role", func(t *testing.T) {
		if err := RequireRole(types.RoleUser)(pass)(setClaims(types.RoleUser)); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("RequireAdmin allows admin", func(t *testing.T) {
		if err := RequireAdmin()(pass)(setClaims(types.RoleAdmin)); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("RequireTeamAdmin allows team admin and admin", func(t *testing.T) {
		if err := RequireTeamAdmin()(pass)(setClaims(types.RoleTeamAdmin)); err != nil {
			t.Errorf("team admin: %v", err)
		}
		if err := RequireTeamAdmin()(pass)(setClaims(types.RoleAdmin)); err != nil {
			t.Errorf("admin: %v", err)
		}
		if code := httpCode(RequireTeamAdmin()(pass)(setClaims(types.RoleUser))); code != http.StatusForbidden {
			t.Errorf("user got %d want 403", code)
		}
	})
}

func TestOptionalAuth(t *testing.T) {
	a := testAuth()
	cases := []struct {
		name       string
		header     string
		wantClaims bool
	}{
		{"no header", "", false},
		{"bad format", "Basic abc", false},
		{"invalid token", "Bearer nope", false},
		{"valid token", "Bearer " + tokenFor(t, a, types.RoleUser), true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newEchoCtx(tt.header)
			if err := OptionalAuth(a)(pass)(c); err != nil {
				t.Fatalf("OptionalAuth must never fail, got %v", err)
			}
			_, ok := c.Get(string(ClaimsContextKey)).(*Claims)
			if ok != tt.wantClaims {
				t.Errorf("claims present = %v, want %v", ok, tt.wantClaims)
			}
		})
	}
}

func TestContextGetters(t *testing.T) {
	t.Run("claims present", func(t *testing.T) {
		c, _ := newEchoCtx("")
		c.Set(string(ClaimsContextKey), &Claims{UserID: "u1", Role: string(types.RoleAdmin)})
		claims, err := GetClaims(c)
		if err != nil || claims.UserID != "u1" {
			t.Errorf("GetClaims: %v %+v", err, claims)
		}
		if id, _ := GetUserID(c); id != "u1" {
			t.Errorf("GetUserID from claims = %q", id)
		}
		if role, _ := GetUserRole(c); role != types.RoleAdmin {
			t.Errorf("GetUserRole from claims = %q", role)
		}
		if !IsAdmin(c) {
			t.Error("IsAdmin should be true")
		}
		if !IsTeamAdmin(c) {
			t.Error("IsTeamAdmin should be true for admin")
		}
	})

	t.Run("claims absent", func(t *testing.T) {
		c, _ := newEchoCtx("")
		if _, err := GetClaims(c); err == nil {
			t.Error("expected error for missing claims")
		}
		if _, err := GetUser(c); err == nil {
			t.Error("expected error for missing user")
		}
		if _, err := GetUserID(c); err == nil {
			t.Error("expected error for missing user id")
		}
		if _, err := GetUserRole(c); err == nil {
			t.Error("expected error for missing role")
		}
		if IsAdmin(c) || IsTeamAdmin(c) {
			t.Error("IsAdmin/IsTeamAdmin should be false without claims")
		}
		if teams := GetManagedTeams(c); len(teams) != 0 {
			t.Errorf("expected no managed teams, got %v", teams)
		}
	})

	t.Run("user object present", func(t *testing.T) {
		c, _ := newEchoCtx("")
		c.Set(string(UserContextKey), &types.User{ID: "u9", Role: types.RoleTeamAdmin, ManagedTeams: []string{"team-a"}})
		if u, err := GetUser(c); err != nil || u.ID != "u9" {
			t.Errorf("GetUser: %v %+v", err, u)
		}
		if id, _ := GetUserID(c); id != "u9" {
			t.Errorf("GetUserID from user = %q", id)
		}
		if role, _ := GetUserRole(c); role != types.RoleTeamAdmin {
			t.Errorf("GetUserRole from user = %q", role)
		}
		if teams := GetManagedTeams(c); len(teams) != 1 || teams[0] != "team-a" {
			t.Errorf("GetManagedTeams = %v", teams)
		}
	})
}

func TestCanManageTeam(t *testing.T) {
	t.Run("admin manages any team", func(t *testing.T) {
		c, _ := newEchoCtx("")
		c.Set(string(UserContextKey), &types.User{ID: "a", Role: types.RoleAdmin})
		if !CanManageTeam(c, "anything") {
			t.Error("admin should manage any team")
		}
	})
	t.Run("team admin manages only assigned teams", func(t *testing.T) {
		c, _ := newEchoCtx("")
		c.Set(string(UserContextKey), &types.User{ID: "t", Role: types.RoleTeamAdmin, ManagedTeams: []string{"team-a"}})
		if !CanManageTeam(c, "team-a") {
			t.Error("should manage assigned team")
		}
		if CanManageTeam(c, "team-b") {
			t.Error("should not manage unassigned team")
		}
	})
	t.Run("regular user manages nothing", func(t *testing.T) {
		c, _ := newEchoCtx("")
		c.Set(string(UserContextKey), &types.User{ID: "u", Role: types.RoleUser})
		if CanManageTeam(c, "team-a") {
			t.Error("regular user should not manage teams")
		}
	})
	t.Run("no user in context", func(t *testing.T) {
		c, _ := newEchoCtx("")
		if CanManageTeam(c, "team-a") {
			t.Error("no user should not manage teams")
		}
	})
}

func TestRequireAuthDual(t *testing.T) {
	a := testAuth()
	disabledIAM, _ := NewIAMAuthenticator(nil, nil, false, "")

	t.Run("missing header", func(t *testing.T) {
		c, _ := newEchoCtx("")
		if code := httpCode(RequireAuthDual(a, disabledIAM)(pass)(c)); code != http.StatusUnauthorized {
			t.Errorf("got %d want 401", code)
		}
	})

	t.Run("valid JWT sets user", func(t *testing.T) {
		c, _ := newEchoCtx("Bearer " + tokenFor(t, a, types.RoleUser))
		if err := RequireAuthDual(a, disabledIAM)(pass)(c); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if u, ok := c.Get(string(UserContextKey)).(*types.User); !ok || u.ID != "u1" {
			t.Errorf("expected user set from JWT, got %+v", c.Get(string(UserContextKey)))
		}
	})

	t.Run("bad bearer format", func(t *testing.T) {
		c, _ := newEchoCtx("Bearer")
		if code := httpCode(RequireAuthDual(a, disabledIAM)(pass)(c)); code != http.StatusUnauthorized {
			t.Errorf("got %d want 401", code)
		}
	})

	t.Run("API key without store errors 500", func(t *testing.T) {
		c, _ := newEchoCtx("Bearer " + APIKeyPrefix + "abc123")
		if code := httpCode(RequireAuthDual(a, disabledIAM)(pass)(c)); code != http.StatusInternalServerError {
			t.Errorf("got %d want 500", code)
		}
	})

	t.Run("API key with wrong store type is unauthorized", func(t *testing.T) {
		c, _ := newEchoCtx("Bearer " + APIKeyPrefix + "abc123")
		c.Set("store", "not-a-store")
		if code := httpCode(RequireAuthDual(a, disabledIAM)(pass)(c)); code != http.StatusUnauthorized {
			t.Errorf("got %d want 401", code)
		}
	})

	t.Run("IAM request rejected when IAM disabled", func(t *testing.T) {
		c, _ := newEchoCtx("AWS4-HMAC-SHA256 Credential=...")
		if code := httpCode(RequireAuthDual(a, disabledIAM)(pass)(c)); code != http.StatusUnauthorized {
			t.Errorf("got %d want 401", code)
		}
	})
}
