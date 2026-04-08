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

// APIKeyHandler handles API key management endpoints
type APIKeyHandler struct {
	store *store.Store
}

// NewAPIKeyHandler creates a new API key handler
func NewAPIKeyHandler(st *store.Store) *APIKeyHandler {
	return &APIKeyHandler{
		store: st,
	}
}

// List retrieves all API keys for the current user
//
//	@Summary		List API keys
//	@Description	Returns all API keys for the authenticated user. Plaintext keys are never returned.
//	@Tags			API Keys
//	@Accept			json
//	@Produce		json
//	@Success		200	{array}		types.APIKeyResponse
//	@Failure		401	{object}	map[string]string	"Unauthorized"
//	@Failure		500	{object}	map[string]string	"Failed to list API keys"
//	@Security		BearerAuth
//	@Router			/api-keys [get]
func (h *APIKeyHandler) List(c echo.Context) error {
	// Get user ID from context
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Get API keys
	apiKeys, err := h.store.APIKeys.ListByUserID(c.Request().Context(), userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list API keys")
	}

	// Convert to response format
	responses := make([]*types.APIKeyResponse, len(apiKeys))
	for i, key := range apiKeys {
		responses[i] = key.ToResponse()
	}

	return c.JSON(http.StatusOK, responses)
}

// Create creates a new API key
//
//	@Summary		Create API key
//	@Description	Generates a new API key for the authenticated user. The plaintext key is only returned once on creation.
//	@Tags			API Keys
//	@Accept			json
//	@Produce		json
//	@Param			body	body		types.CreateAPIKeyRequest	true	"API key creation request"
//	@Success		201		{object}	types.CreateAPIKeyResponse
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		401		{object}	map[string]string	"Unauthorized"
//	@Failure		500		{object}	map[string]string	"Failed to create API key"
//	@Security		BearerAuth
//	@Router			/api-keys [post]
func (h *APIKeyHandler) Create(c echo.Context) error {
	// Get user ID from context
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	var req types.CreateAPIKeyRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Validate scope
	if !req.Scope.IsValid() {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid scope")
	}

	// Generate API key
	plainKey, keyPrefix, keyHash, err := auth.GenerateAPIKey()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to generate API key")
	}

	// Create API key record
	apiKey := &types.APIKey{
		ID:        uuid.New().String(),
		UserID:    userID,
		Name:      req.Name,
		KeyPrefix: keyPrefix,
		KeyHash:   keyHash,
		Scope:     req.Scope,
		ExpiresAt: req.ExpiresAt,
		CreatedAt: time.Now(),
	}

	if err := h.store.APIKeys.Create(c.Request().Context(), apiKey); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to create API key")
	}

	// Return response with plaintext key (only time it's shown)
	response := &types.CreateAPIKeyResponse{
		APIKey:   apiKey.ToResponse(),
		PlainKey: plainKey,
	}

	return c.JSON(http.StatusCreated, response)
}

// Update updates an API key (only name can be updated)
//
//	@Summary		Update API key
//	@Description	Updates the name of an API key. Only the key owner can update it.
//	@Tags			API Keys
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string						true	"API Key ID"
//	@Param			body	body		types.UpdateAPIKeyRequest	true	"API key update request"
//	@Success		200		{object}	types.APIKeyResponse
//	@Failure		400		{object}	map[string]string	"Invalid request or validation error"
//	@Failure		401		{object}	map[string]string	"Unauthorized"
//	@Failure		403		{object}	map[string]string	"Not your API key"
//	@Failure		404		{object}	map[string]string	"API key not found"
//	@Failure		500		{object}	map[string]string	"Failed to update API key"
//	@Security		BearerAuth
//	@Router			/api-keys/{id} [patch]
func (h *APIKeyHandler) Update(c echo.Context) error {
	keyID := c.Param("id")
	if keyID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "API key ID required")
	}

	// Get user ID from context
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	var req types.UpdateAPIKeyRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	// Validate request
	if err := c.Validate(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	// Get API key to verify ownership
	apiKey, err := h.store.APIKeys.GetByID(c.Request().Context(), keyID)
	if err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "API key not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get API key")
	}

	// Verify ownership
	if apiKey.UserID != userID {
		return echo.NewHTTPError(http.StatusForbidden, "not your API key")
	}

	// Update name if provided
	if req.Name != nil {
		if err := h.store.APIKeys.UpdateName(c.Request().Context(), keyID, *req.Name); err != nil {
			if err == store.ErrNotFound {
				return echo.NewHTTPError(http.StatusNotFound, "API key not found")
			}
			return echo.NewHTTPError(http.StatusInternalServerError, "failed to update API key")
		}
	}

	// Get updated API key
	apiKey, err = h.store.APIKeys.GetByID(c.Request().Context(), keyID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get updated API key")
	}

	return c.JSON(http.StatusOK, apiKey.ToResponse())
}

// Revoke revokes an API key
//
//	@Summary		Revoke API key
//	@Description	Revokes an API key, preventing further use. Only the key owner can revoke it.
//	@Tags			API Keys
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"API Key ID"
//	@Success		200	{object}	map[string]string
//	@Failure		400	{object}	map[string]string	"API key ID required"
//	@Failure		401	{object}	map[string]string	"Unauthorized"
//	@Failure		403	{object}	map[string]string	"Not your API key"
//	@Failure		404	{object}	map[string]string	"API key not found"
//	@Failure		500	{object}	map[string]string	"Failed to revoke API key"
//	@Security		BearerAuth
//	@Router			/api-keys/{id}/revoke [post]
func (h *APIKeyHandler) Revoke(c echo.Context) error {
	keyID := c.Param("id")
	if keyID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "API key ID required")
	}

	// Get user ID from context
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Get API key to verify ownership
	apiKey, err := h.store.APIKeys.GetByID(c.Request().Context(), keyID)
	if err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "API key not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get API key")
	}

	// Verify ownership
	if apiKey.UserID != userID {
		return echo.NewHTTPError(http.StatusForbidden, "not your API key")
	}

	// Revoke API key
	if err := h.store.APIKeys.Revoke(c.Request().Context(), keyID); err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "API key not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to revoke API key")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "API key revoked successfully",
	})
}

// Delete permanently deletes an API key
//
//	@Summary		Delete API key
//	@Description	Permanently deletes an API key. Only the key owner can delete it.
//	@Tags			API Keys
//	@Accept			json
//	@Produce		json
//	@Param			id	path		string	true	"API Key ID"
//	@Success		200	{object}	map[string]string
//	@Failure		400	{object}	map[string]string	"API key ID required"
//	@Failure		401	{object}	map[string]string	"Unauthorized"
//	@Failure		403	{object}	map[string]string	"Not your API key"
//	@Failure		404	{object}	map[string]string	"API key not found"
//	@Failure		500	{object}	map[string]string	"Failed to delete API key"
//	@Security		BearerAuth
//	@Router			/api-keys/{id} [delete]
func (h *APIKeyHandler) Delete(c echo.Context) error {
	keyID := c.Param("id")
	if keyID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "API key ID required")
	}

	// Get user ID from context
	userID, err := auth.GetUserID(c)
	if err != nil {
		return err
	}

	// Get API key to verify ownership
	apiKey, err := h.store.APIKeys.GetByID(c.Request().Context(), keyID)
	if err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "API key not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get API key")
	}

	// Verify ownership
	if apiKey.UserID != userID {
		return echo.NewHTTPError(http.StatusForbidden, "not your API key")
	}

	// Delete API key
	if err := h.store.APIKeys.Delete(c.Request().Context(), keyID); err != nil {
		if err == store.ErrNotFound {
			return echo.NewHTTPError(http.StatusNotFound, "API key not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete API key")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"message": "API key deleted successfully",
	})
}
