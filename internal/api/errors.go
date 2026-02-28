package api

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/policy"
)

// ErrorResponse represents a standard API error response
type ErrorResponse struct {
	Error   string                   `json:"error"`
	Message string                   `json:"message,omitempty"`
	Details []map[string]interface{} `json:"details,omitempty"`
}

// NewErrorResponse creates a new error response
func NewErrorResponse(error, message string) *ErrorResponse {
	return &ErrorResponse{
		Error:   error,
		Message: message,
	}
}

// WithDetails adds details to an error response
func (e *ErrorResponse) WithDetails(details []map[string]interface{}) *ErrorResponse {
	e.Details = details
	return e
}

// ErrorBadRequest returns a 400 Bad Request error
func ErrorBadRequest(c echo.Context, message string) error {
	return c.JSON(http.StatusBadRequest, NewErrorResponse("bad_request", message))
}

// ErrorUnauthorized returns a 401 Unauthorized error
func ErrorUnauthorized(c echo.Context, message string) error {
	return c.JSON(http.StatusUnauthorized, NewErrorResponse("unauthorized", message))
}

// ErrorForbidden returns a 403 Forbidden error
func ErrorForbidden(c echo.Context, message string) error {
	return c.JSON(http.StatusForbidden, NewErrorResponse("forbidden", message))
}

// ErrorNotFound returns a 404 Not Found error
func ErrorNotFound(c echo.Context, message string) error {
	return c.JSON(http.StatusNotFound, NewErrorResponse("not_found", message))
}

// ErrorConflict returns a 409 Conflict error
func ErrorConflict(c echo.Context, message string) error {
	return c.JSON(http.StatusConflict, NewErrorResponse("conflict", message))
}

// ErrorValidation returns a 422 Unprocessable Entity error with validation details
func ErrorValidation(c echo.Context, result *policy.ValidationResult) error {
	details := make([]map[string]interface{}, len(result.Errors))
	for i, err := range result.Errors {
		details[i] = map[string]interface{}{
			"field":   err.Field,
			"message": err.Message,
		}
	}

	return c.JSON(http.StatusUnprocessableEntity, NewErrorResponse(
		"validation_failed",
		"Request validation failed",
	).WithDetails(details))
}

// ErrorInternal returns a 500 Internal Server Error
func ErrorInternal(c echo.Context, message string) error {
	return c.JSON(http.StatusInternalServerError, NewErrorResponse("internal_error", message))
}

// ErrorServiceUnavailable returns a 503 Service Unavailable error
func ErrorServiceUnavailable(c echo.Context, message string) error {
	return c.JSON(http.StatusServiceUnavailable, NewErrorResponse("service_unavailable", message))
}
