package api

import (
	"log"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/store"
)

// AddonsHandler handles HTTP requests for post-config add-ons
type AddonsHandler struct {
	store *store.Store
}

// NewAddonsHandler creates a new add-ons handler
func NewAddonsHandler(store *store.Store) *AddonsHandler {
	return &AddonsHandler{store: store}
}

// List returns all enabled add-ons, optionally filtered by category and platform
// GET /api/v1/post-config/addons?category=backup&platform=openshift
func (h *AddonsHandler) List(c echo.Context) error {
	ctx := c.Request().Context()

	// Get query parameters
	category := c.QueryParam("category")
	platform := c.QueryParam("platform")

	var categoryPtr *string
	var platformPtr *string

	if category != "" {
		categoryPtr = &category
	}

	if platform != "" {
		platformPtr = &platform
	}

	// Retrieve add-ons from database
	addons, err := h.store.PostConfigAddons.List(ctx, categoryPtr, platformPtr)
	if err != nil {
		log.Printf("Error listing add-ons: %v", err)
		return LogAndReturnGenericError(c, err)
	}

	return c.JSON(200, map[string]interface{}{
		"addons": addons,
	})
}
