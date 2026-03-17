package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/auth"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// UserHandler handles user management endpoints (admin only)
type UserHandler struct {
	store *store.Store
}

// NewUserHandler creates a new user handler
func NewUserHandler(st *store.Store) *UserHandler {
	return &UserHandler{
		store: st,
	}
}

// logAudit logs an audit event for user management actions
func (h *UserHandler) logAudit(c echo.Context, action string, targetUserID string, status types.AuditEventStatus, metadata map[string]interface{}) {
	// Get actor (current user)
	actorID, _ := auth.GetUserID(c)

	// Get request metadata
	ip := c.RealIP()
	userAgent := c.Request().UserAgent()

	event := &types.AuditEvent{
		ID:           uuid.New().String(),
		Actor:        actorID,
		Action:       action,
		TargetUserID: &targetUserID,
		Status:       status,
		Metadata:     metadata,
		IPAddress:    &ip,
		UserAgent:    &userAgent,
		CreatedAt:    time.Now(),
	}

	// Log audit event (fire and forget, don't fail the request if audit fails)
	_ = h.store.Audit.Log(c.Request().Context(), event)
}

// List returns all users
// GET /api/v1/users
// List lists all users
//
//	@Summary		List users
//	@Description	Returns a list of all users (admin only). Password hashes are excluded from response.
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	map[string]interface{}
//	@Failure		500	{object}	map[string]string	"Failed to list users"
//	@Security		BearerAuth
//	@Router			/users [get]
func (h *UserHandler) List(c echo.Context) error {
	users, err := h.store.Users.List(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list users")
	}

	// Convert to response format (exclude password hashes)
	responses := make([]*types.UserResponse, len(users))
	for i, user := range users {
		responses[i] = user.ToResponse()
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"users": responses,
		"total": len(responses),
	})
}

// Create creates a new user
//
//	@Summary		Create user
//	@Description	Creates a new user with specified email, username, role, and password (admin only)
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Param			body	body		types.CreateUserRequest	true	"User creation request"
//	@Success		201		{object}	types.UserResponse
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		409		{object}	map[string]string	"Email already exists"
//	@Failure		500		{object}	map[string]string	"Failed to create user"
//	@Security		BearerAuth
//	@Router			/users [post]
func (h *UserHandler) Create(c echo.Context) error {
	var req types.CreateUserRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Validate email format
	if err := auth.ValidateEmail(req.Email); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Check if email already exists
	exists, err := h.store.Users.EmailExists(c.Request().Context(), req.Email)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to check email")
	}
	if exists {
		return echo.NewHTTPError(http.StatusConflict, "email already exists")
	}

	// Validate role
	if !req.Role.IsValid() {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid role")
	}

	// Validate password strength
	if err := auth.ValidatePasswordStrength(req.Password); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Hash password
	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to hash password")
	}

	// Parse default work hours times
	defaultStartTime, _ := time.Parse("15:04", "09:00")
	defaultEndTime, _ := time.Parse("15:04", "17:00")

	// Create user
	user := &types.User{
		ID:               uuid.New().String(),
		Email:            req.Email,
		Username:         req.Username,
		PasswordHash:     passwordHash,
		Role:             req.Role,
		Timezone:         "UTC", // Default timezone
		WorkHoursEnabled: false,
		WorkHoursStart:   defaultStartTime,
		WorkHoursEnd:     defaultEndTime,
		WorkDays:         62, // Monday-Friday (binary: 0111110)
		Active:           true,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if err := h.store.Users.Create(c.Request().Context(), user); err != nil {
		h.logAudit(c, "user.create", user.ID, types.AuditEventStatusFailure, map[string]interface{}{
			"email": user.Email,
			"role":  user.Role,
		})
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create user")
	}

	// Log successful user creation
	h.logAudit(c, "user.create", user.ID, types.AuditEventStatusSuccess, map[string]interface{}{
		"email": user.Email,
		"role":  user.Role,
	})

	return c.JSON(http.StatusCreated, user.ToResponse())
}

// Get retrieves a user by ID
//
//	@Summary		Get user
//	@Description	Retrieves user details by ID (admin only)
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"User ID"
//	@Success		200	{object}	types.UserResponse
//	@Failure		400	{object}	map[string]string	"User ID required"
//	@Failure		404	{object}	map[string]string	"User not found"
//	@Failure		500	{object}	map[string]string	"Failed to get user"
//	@Security		BearerAuth
//	@Router			/users/{id} [get]
func (h *UserHandler) Get(c echo.Context) error {
	userID := c.Param("id")
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "user ID required")
	}

	user, err := h.store.Users.GetByID(c.Request().Context(), userID)
	if err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get user")
	}

	return c.JSON(http.StatusOK, user.ToResponse())
}

// Update updates a user
//
//	@Summary		Update user
//	@Description	Updates user details including username, role, timezone, work hours, or active status (admin only)
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"User ID"
//	@Param			body	body		types.UpdateUserRequest		true	"User update fields"
//	@Success		200		{object}	types.UserResponse
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		404		{object}	map[string]string	"User not found"
//	@Failure		500		{object}	map[string]string	"Failed to update user"
//	@Security		BearerAuth
//	@Router			/users/{id} [patch]
func (h *UserHandler) Update(c echo.Context) error {
	userID := c.Param("id")
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "user ID required")
	}

	var req types.UpdateUserRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Validate role if provided
	if req.Role != nil && !req.Role.IsValid() {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid role")
	}

	// Validate and hash password if provided
	if req.NewPassword != nil {
		if err := auth.ValidatePasswordStrength(*req.NewPassword); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	// Build update map
	updates := make(map[string]interface{})
	if req.Username != nil {
		updates["username"] = *req.Username
	}
	if req.Role != nil {
		updates["role"] = *req.Role
	}
	if req.Active != nil {
		updates["active"] = *req.Active
	}
	if req.NewPassword != nil {
		passwordHash, err := auth.HashPassword(*req.NewPassword)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to hash password")
		}
		updates["password_hash"] = passwordHash
	}

	if len(updates) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "no fields to update")
	}

	// Update user
	if err := h.store.Users.UpdatePartial(c.Request().Context(), userID, updates); err != nil {
		if err == store.ErrNotFound {
			h.logAudit(c, "user.update", userID, types.AuditEventStatusFailure, map[string]interface{}{
				"error": "user not found",
			})
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		h.logAudit(c, "user.update", userID, types.AuditEventStatusFailure, updates)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update user")
	}

	// Get updated user
	user, err := h.store.Users.GetByID(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get updated user")
	}

	// Log successful user update
	h.logAudit(c, "user.update", userID, types.AuditEventStatusSuccess, updates)

	return c.JSON(http.StatusOK, user.ToResponse())
}

// Delete deletes a user
//
//	@Summary		Delete user
//	@Description	Soft deletes a user by marking as inactive (admin only). Also revokes all refresh tokens.
//	@Tags			Users
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"User ID"
//	@Success		200	{object}	map[string]string
//	@Failure		400	{object}	map[string]string	"User ID required or cannot delete self"
//	@Failure		404	{object}	map[string]string	"User not found"
//	@Failure		500	{object}	map[string]string	"Failed to delete user"
//	@Security		BearerAuth
//	@Router			/users/{id} [delete]
func (h *UserHandler) Delete(c echo.Context) error {
	userID := c.Param("id")
	if userID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "user ID required")
	}

	// Prevent deleting yourself
	currentUserID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}
	if currentUserID == userID {
		return echo.NewHTTPError(http.StatusBadRequest, "cannot delete your own account")
	}

	// Delete user (cascades to refresh tokens)
	if err := h.store.Users.Delete(c.Request().Context(), userID); err != nil {
		if err == store.ErrNotFound {
			h.logAudit(c, "user.delete", userID, types.AuditEventStatusFailure, map[string]interface{}{
				"error": "user not found",
			})
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		h.logAudit(c, "user.delete", userID, types.AuditEventStatusFailure, nil)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete user")
	}

	// Log successful user deletion
	h.logAudit(c, "user.delete", userID, types.AuditEventStatusSuccess, nil)

	return c.JSON(http.StatusOK, map[string]string{
		"message": "user deleted successfully",
	})
}
