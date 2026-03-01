package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	store *store.Store
	auth  *auth.Auth
}

// NewAuthHandler creates a new auth handler
func NewAuthHandler(st *store.Store, authService *auth.Auth) *AuthHandler {
	return &AuthHandler{
		store: st,
		auth:  authService,
	}
}

// Login handles user login
// POST /api/v1/auth/login
func (h *AuthHandler) Login(c echo.Context) error {
	var req types.LoginRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Get user by email
	user, err := h.store.Users.GetByEmail(c.Request().Context(), req.Email)
	if err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid email or password")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to authenticate")
	}

	// Check if user is active
	if !user.Active {
		return echo.NewHTTPError(http.StatusForbidden, "account is disabled")
	}

	// Check password
	if err := auth.CheckPassword(req.Password, user.PasswordHash); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid email or password")
	}

	// Generate access token
	accessToken, err := h.auth.GenerateAccessToken(user)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate access token")
	}

	// Generate refresh token
	refreshToken, err := h.auth.GenerateRefreshToken()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate refresh token")
	}

	// Hash refresh token for storage
	hasher := sha256.New()
	hasher.Write([]byte(refreshToken))
	tokenHash := hex.EncodeToString(hasher.Sum(nil))

	// Store refresh token
	refreshTokenRecord := &types.RefreshToken{
		ID:        uuid.New().String(),
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(h.auth.GetRefreshTTL()),
		CreatedAt: time.Now(),
	}

	if err := h.store.RefreshTokens.Create(c.Request().Context(), refreshTokenRecord); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create session")
	}

	// Set refresh token as httpOnly cookie
	cookie := &http.Cookie{
		Name:     "refresh_token",
		Value:    refreshToken,
		Path:     "/api/v1/auth",
		HttpOnly: true,
		Secure:   c.Request().TLS != nil, // Only secure in HTTPS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(h.auth.GetRefreshTTL().Seconds()),
	}
	c.SetCookie(cookie)

	// Return response
	response := &types.LoginResponse{
		User:        user.ToResponse(),
		AccessToken: accessToken,
		ExpiresIn:   int(h.auth.GetAccessTTL().Seconds()),
	}

	return c.JSON(http.StatusOK, response)
}

// Logout handles user logout
// POST /api/v1/auth/logout
func (h *AuthHandler) Logout(c echo.Context) error {
	// Get refresh token from cookie
	cookie, err := c.Cookie("refresh_token")
	if err == nil && cookie.Value != "" {
		// Hash token
		hasher := sha256.New()
		hasher.Write([]byte(cookie.Value))
		tokenHash := hex.EncodeToString(hasher.Sum(nil))

		// Revoke token
		_ = h.store.RefreshTokens.Revoke(c.Request().Context(), tokenHash)
	}

	// Clear cookie
	cookie = &http.Cookie{
		Name:     "refresh_token",
		Value:    "",
		Path:     "/api/v1/auth",
		HttpOnly: true,
		MaxAge:   -1,
	}
	c.SetCookie(cookie)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "logged out successfully",
	})
}

// Refresh handles access token refresh
// POST /api/v1/auth/refresh
func (h *AuthHandler) Refresh(c echo.Context) error {
	// Get refresh token from cookie
	cookie, err := c.Cookie("refresh_token")
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "refresh token not found")
	}

	// Hash token
	hasher := sha256.New()
	hasher.Write([]byte(cookie.Value))
	tokenHash := hex.EncodeToString(hasher.Sum(nil))

	// Get token from database
	tokenRecord, err := h.store.RefreshTokens.GetByTokenHash(c.Request().Context(), tokenHash)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid or expired refresh token")
	}

	// Get user
	user, err := h.store.Users.GetByID(c.Request().Context(), tokenRecord.UserID)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "user not found")
	}

	// Check if user is active
	if !user.Active {
		return echo.NewHTTPError(http.StatusForbidden, "account is disabled")
	}

	// Generate new access token
	accessToken, err := h.auth.GenerateAccessToken(user)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate access token")
	}

	// Return response
	response := &types.LoginResponse{
		User:        user.ToResponse(),
		AccessToken: accessToken,
		ExpiresIn:   int(h.auth.GetAccessTTL().Seconds()),
	}

	return c.JSON(http.StatusOK, response)
}

// GetMe returns current user info
// GET /api/v1/auth/me
func (h *AuthHandler) GetMe(c echo.Context) error {
	// Get user ID from context (set by auth middleware)
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Get user from database
	user, err := h.store.Users.GetByID(c.Request().Context(), userID)
	if err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user")
	}

	return c.JSON(http.StatusOK, user.ToResponse())
}

// UpdateMe updates current user profile
// PATCH /api/v1/auth/me
func (h *AuthHandler) UpdateMe(c echo.Context) error {
	// Get user ID from context
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	var req types.UpdateMeRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Build update map
	updates := make(map[string]interface{})
	if req.Username != nil {
		updates["username"] = *req.Username
	}

	if len(updates) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "no fields to update")
	}

	// Update user
	if err := h.store.Users.UpdatePartial(c.Request().Context(), userID, updates); err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update user")
	}

	// Get updated user
	user, err := h.store.Users.GetByID(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get updated user")
	}

	return c.JSON(http.StatusOK, user.ToResponse())
}

// ChangePassword handles password change
// POST /api/v1/auth/password
func (h *AuthHandler) ChangePassword(c echo.Context) error {
	// Get user ID from context
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	var req types.ChangePasswordRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Get user
	user, err := h.store.Users.GetByID(c.Request().Context(), userID)
	if err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user")
	}

	// Check current password
	if err := auth.CheckPassword(req.CurrentPassword, user.PasswordHash); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "current password is incorrect")
	}

	// Validate new password strength
	if err := auth.ValidatePasswordStrength(req.NewPassword); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Hash new password
	newHash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to hash password")
	}

	// Update password
	user.PasswordHash = newHash
	if err := h.store.Users.Update(c.Request().Context(), user); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update password")
	}

	// Revoke all refresh tokens (logout from all sessions)
	_ = h.store.RefreshTokens.RevokeAllForUser(c.Request().Context(), userID)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "password changed successfully, please log in again",
	})
}
