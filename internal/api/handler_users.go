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

// List returns all users
// GET /api/v1/users
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
// POST /api/v1/users
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

	// Create user
	user := &types.User{
		ID:           uuid.New().String(),
		Email:        req.Email,
		Username:     req.Username,
		PasswordHash: passwordHash,
		Role:         req.Role,
		Active:       true,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := h.store.Users.Create(c.Request().Context(), user); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create user")
	}

	return c.JSON(http.StatusCreated, user.ToResponse())
}

// Get retrieves a user by ID
// GET /api/v1/users/:id
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
// PATCH /api/v1/users/:id
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

// Delete deletes a user
// DELETE /api/v1/users/:id
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
			return echo.NewHTTPError(http.StatusNotFound, "user not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete user")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "user deleted successfully",
	})
}
