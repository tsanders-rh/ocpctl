package auth

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ContextKey is the type for context keys
type ContextKey string

const (
	// UserContextKey is the key for storing user in context
	UserContextKey ContextKey = "user"
	// ClaimsContextKey is the key for storing claims in context
	ClaimsContextKey ContextKey = "claims"
)

// RequireAuth is middleware that requires authentication
func RequireAuth(auth *Auth) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Extract token from Authorization header
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
			}

			// Check Bearer prefix
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid authorization header format")
			}

			tokenString := parts[1]

			// Validate token
			claims, err := auth.ValidateAccessToken(tokenString)
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired token")
			}

			// Store claims in context
			c.Set(string(ClaimsContextKey), claims)

			return next(c)
		}
	}
}

// RequireRole is middleware that requires specific role(s)
func RequireRole(roles ...types.UserRole) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Get claims from context (set by RequireAuth)
			claims, ok := c.Get(string(ClaimsContextKey)).(*Claims)
			if !ok {
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}

			// Check if user has required role
			userRole := types.UserRole(claims.Role)
			hasRole := false
			for _, role := range roles {
				if userRole == role {
					hasRole = true
					break
				}
			}

			if !hasRole {
				return echo.NewHTTPError(http.StatusForbidden, "insufficient permissions")
			}

			return next(c)
		}
	}
}

// RequireAdmin is middleware that requires admin role
func RequireAdmin() echo.MiddlewareFunc {
	return RequireRole(types.RoleAdmin)
}

// OptionalAuth is middleware that optionally authenticates (doesn't fail if no token)
func OptionalAuth(auth *Auth) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Extract token from Authorization header
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				// No auth header, continue without authentication
				return next(c)
			}

			// Check Bearer prefix
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				// Invalid format, continue without authentication
				return next(c)
			}

			tokenString := parts[1]

			// Validate token
			claims, err := auth.ValidateAccessToken(tokenString)
			if err != nil {
				// Invalid token, continue without authentication
				return next(c)
			}

			// Store claims in context
			c.Set(string(ClaimsContextKey), claims)

			return next(c)
		}
	}
}

// GetClaims retrieves claims from echo context
func GetClaims(c echo.Context) (*Claims, error) {
	claims, ok := c.Get(string(ClaimsContextKey)).(*Claims)
	if !ok {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
	}
	return claims, nil
}

// GetUser retrieves the user object from context (set by RequireAuthDual)
func GetUser(c echo.Context) (*types.User, error) {
	user, ok := c.Get(string(UserContextKey)).(*types.User)
	if ok && user != nil {
		return user, nil
	}
	// Fallback: try to construct from claims (for JWT-only auth)
	return nil, echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
}

// GetUserID retrieves the current user ID from context
func GetUserID(c echo.Context) (string, error) {
	// Try to get from user object first (dual auth)
	user, err := GetUser(c)
	if err == nil {
		return user.ID, nil
	}

	// Fallback to claims (JWT-only auth)
	claims, err := GetClaims(c)
	if err != nil {
		return "", err
	}
	return claims.UserID, nil
}

// GetUserRole retrieves the current user role from context
func GetUserRole(c echo.Context) (types.UserRole, error) {
	// Try to get from user object first (dual auth)
	user, err := GetUser(c)
	if err == nil {
		return user.Role, nil
	}

	// Fallback to claims (JWT-only auth)
	claims, err := GetClaims(c)
	if err != nil {
		return "", err
	}
	return types.UserRole(claims.Role), nil
}

// IsAdmin checks if the current user is an admin
func IsAdmin(c echo.Context) bool {
	role, err := GetUserRole(c)
	if err != nil {
		return false
	}
	return role == types.RoleAdmin
}

// RequireAuthDual is middleware that supports both JWT and IAM authentication
func RequireAuthDual(auth *Auth, iamAuth *IAMAuthenticator) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authHeader := c.Request().Header.Get("Authorization")
			if authHeader == "" {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing authorization header")
			}

			// Detect auth type
			if IsIAMRequest(c.Request()) {
				// IAM authentication
				if iamAuth == nil || !iamAuth.enabledIAMAuth {
					return echo.NewHTTPError(http.StatusUnauthorized, "IAM authentication not enabled")
				}

				user, err := iamAuth.ValidateIAMRequest(c.Request().Context(), c.Request())
				if err != nil {
					return echo.NewHTTPError(http.StatusUnauthorized, "invalid IAM credentials: "+err.Error())
				}

				// Store user in context
				c.Set(string(UserContextKey), user)
				return next(c)
			}

			// JWT authentication (existing flow)
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid authorization header format")
			}

			tokenString := parts[1]
			claims, err := auth.ValidateAccessToken(tokenString)
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired token")
			}

			// Store claims in context (for backward compatibility)
			c.Set(string(ClaimsContextKey), claims)

			// Also create a user object from claims for consistency
			user := &types.User{
				ID:       claims.UserID,
				Email:    claims.Email,
				Role:     types.UserRole(claims.Role),
			}
			c.Set(string(UserContextKey), user)

			return next(c)
		}
	}
}
