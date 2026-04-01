package api

import (
	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/postconfig"
	"github.com/tsanders-rh/ocpctl/internal/validation"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// PostConfigHandler handles post-configuration API endpoints
type PostConfigHandler struct{}

// NewPostConfigHandler creates a new post-config handler
func NewPostConfigHandler() *PostConfigHandler {
	return &PostConfigHandler{}
}

// ValidateRequest represents a validation request
type ValidateRequest struct {
	Config *types.CustomPostConfig `json:"config" validate:"required"`
}

// ValidateResponse represents a validation response
type ValidateResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors,omitempty"`
	DAG    *DAGInfo `json:"dag,omitempty"`
}

// DAGInfo provides information about the execution DAG
type DAGInfo struct {
	ExecutionOrder []string          `json:"executionOrder"`
	TaskCount      int               `json:"taskCount"`
	Dependencies   map[string][]string `json:"dependencies"`
}

// Validate handles POST /api/v1/post-config/validate
//
//	@Summary		Validate custom post-config
//	@Description	Validates custom post-deployment configuration without creating a cluster. Checks resource limits, dependencies, conditions, and generates execution DAG.
//	@Tags			post-config
//	@Accept			json
//	@Produce		json
//	@Param			request	body		ValidateRequest	true	"Validation request"
//	@Success		200		{object}	ValidateResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/post-config/validate [post]
func (h *PostConfigHandler) Validate(c echo.Context) error {
	var req ValidateRequest
	if err := c.Bind(&req); err != nil {
		return ErrorBadRequest(c, "Invalid request body")
	}

	if err := c.Validate(req); err != nil {
		return ErrorBadRequest(c, err.Error())
	}

	// Validate custom post-config
	validationErrors := validation.ValidateCustomPostConfig(req.Config)
	if len(validationErrors) > 0 {
		errorMessages := make([]string, len(validationErrors))
		for i, err := range validationErrors {
			errorMessages[i] = err.Error()
		}
		return SuccessOK(c, ValidateResponse{
			Valid:  false,
			Errors: errorMessages,
		})
	}

	// Build execution DAG to validate dependencies
	dag, err := postconfig.BuildExecutionDAG(req.Config)
	if err != nil {
		return SuccessOK(c, ValidateResponse{
			Valid:  false,
			Errors: []string{err.Error()},
		})
	}

	// Validation passed - return DAG info
	dagInfo := &DAGInfo{
		ExecutionOrder: dag.ExecutionOrder,
		TaskCount:      len(dag.Nodes),
		Dependencies:   dag.AdjacencyList,
	}

	return SuccessOK(c, ValidateResponse{
		Valid: true,
		DAG:   dagInfo,
	})
}
