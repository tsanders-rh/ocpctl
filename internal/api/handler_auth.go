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
//
//	@Summary		User login
//	@Description	Authenticates user with email and password. Returns JWT access token and sets httpOnly refresh token cookie.
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Param			body	body		types.LoginRequest	true	"Login credentials"
//	@Success		200		{object}	types.LoginResponse
//	@Failure		400		{object}	map[string]string	"Invalid request body"
//	@Failure		401		{object}	map[string]string	"Invalid email or password"
//	@Failure		403		{object}	map[string]string	"Account is disabled"
//	@Failure		500		{object}	map[string]string	"Internal server error"
//	@Router			/auth/login [post]
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
		Path:     "/",
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
//
//	@Summary		User logout
//	@Description	Revokes refresh token and clears authentication cookies
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Router			/auth/logout [post]
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
//
//	@Summary		Refresh access token
//	@Description	Generates a new access token using the refresh token from httpOnly cookie
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	types.LoginResponse
//	@Failure		401	{object}	map[string]string	"Invalid or expired refresh token"
//	@Failure		403	{object}	map[string]string	"Account is disabled"
//	@Failure		500	{object}	map[string]string	"Failed to generate token"
//	@Router			/auth/refresh [post]
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
//
//	@Summary		Get current user
//	@Description	Returns profile information for the authenticated user
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	types.UserResponse
//	@Failure		401	{object}	map[string]string	"Unauthorized"
//	@Failure		404	{object}	map[string]string	"User not found"
//	@Failure		500	{object}	map[string]string	"Internal server error"
//	@Security		BearerAuth
//	@Router			/auth/me [get]
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
//
//	@Summary		Update current user profile
//	@Description	Updates profile information for the authenticated user (username, timezone, work hours)
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Param			body	body		types.UpdateMeRequest	true	"Profile update fields"
//	@Success		200		{object}	types.UserResponse
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		401		{object}	map[string]string	"Unauthorized"
//	@Failure		404		{object}	map[string]string	"User not found"
//	@Failure		500		{object}	map[string]string	"Failed to update user"
//	@Security		BearerAuth
//	@Router			/auth/me [patch]
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
	if req.Timezone != nil {
		updates["timezone"] = *req.Timezone
	}
	if req.WorkHoursEnabled != nil {
		updates["work_hours_enabled"] = *req.WorkHoursEnabled
	}
	if req.WorkHours != nil {
		// Parse "09:00" format to time.Time
		startTime, err := time.Parse("15:04", req.WorkHours.StartTime)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid start time format, use HH:MM")
		}
		endTime, err := time.Parse("15:04", req.WorkHours.EndTime)
		if err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "invalid end time format, use HH:MM")
		}

		// Convert day names to bitmask
		workDaysMask := types.WorkDaysFromStrings(req.WorkHours.WorkDays)
		if workDaysMask == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "at least one work day must be selected")
		}

		updates["work_hours_start"] = startTime
		updates["work_hours_end"] = endTime
		updates["work_days"] = workDaysMask
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
//
//	@Summary		Change password
//	@Description	Changes the authenticated user's password. Requires current password. Invalidates all sessions.
//	@Tags			Authentication
//	@Accept			json
//	@Produce		json
//	@Param			body	body		types.ChangePasswordRequest	true	"Password change request"
//	@Success		200		{object}	map[string]string
//	@Failure		400		{object}	map[string]string	"Invalid request or weak password"
//	@Failure		401		{object}	map[string]string	"Current password is incorrect"
//	@Failure		404		{object}	map[string]string	"User not found"
//	@Failure		500		{object}	map[string]string	"Failed to update password"
//	@Security		BearerAuth
//	@Router			/auth/password [post]
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
