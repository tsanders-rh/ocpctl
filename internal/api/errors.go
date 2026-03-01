package api

import (
	"fmt"
	"log"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/policy"
)

// GetRequestID extracts the request ID from the echo context
func GetRequestID(c echo.Context) string {
	requestID := c.Response().Header().Get(echo.HeaderXRequestID)
	if requestID == "" {
		requestID = "unknown"
	}
	return requestID
}

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
// DEPRECATED: Use ErrorInternalWithLog instead to avoid exposing internal details
func ErrorInternal(c echo.Context, message string) error {
	return c.JSON(http.StatusInternalServerError, NewErrorResponse("internal_error", message))
}

// ErrorInternalWithLog logs the detailed error server-side and returns a generic message to the client
func ErrorInternalWithLog(c echo.Context, userMessage string, internalError error) error {
	// Log the detailed error server-side with request context
	requestID := GetRequestID(c)
	log.Printf("[ERROR] request_id=%s method=%s path=%s error=%v",
		requestID, c.Request().Method, c.Request().URL.Path, internalError)

	// Return generic message to client
	return c.JSON(http.StatusInternalServerError, NewErrorResponse("internal_error", userMessage))
}

// LogAndReturnGenericError logs detailed error and returns a generic internal error
func LogAndReturnGenericError(c echo.Context, internalError error) error {
	return ErrorInternalWithLog(c, "An internal error occurred. Please try again later.", internalError)
}

// ErrorServiceUnavailable returns a 503 Service Unavailable error
func ErrorServiceUnavailable(c echo.Context, message string) error {
	return c.JSON(http.StatusServiceUnavailable, NewErrorResponse("service_unavailable", message))
}

// LogInfo logs an informational message with request context
func LogInfo(c echo.Context, message string, keyvals ...interface{}) {
	requestID := GetRequestID(c)

	// Build key-value pairs string
	kvString := ""
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			if i > 0 {
				kvString += " "
			}
			kvString += formatKeyVal(keyvals[i], keyvals[i+1])
		}
	}

	log.Printf("[INFO] request_id=%s method=%s path=%s message=\"%s\" %s",
		requestID, c.Request().Method, c.Request().URL.Path, message, kvString)
}

// LogWarning logs a warning message with request context
func LogWarning(c echo.Context, message string, keyvals ...interface{}) {
	requestID := GetRequestID(c)

	// Build key-value pairs string
	kvString := ""
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			if i > 0 {
				kvString += " "
			}
			kvString += formatKeyVal(keyvals[i], keyvals[i+1])
		}
	}

	log.Printf("[WARN] request_id=%s method=%s path=%s message=\"%s\" %s",
		requestID, c.Request().Method, c.Request().URL.Path, message, kvString)
}

// formatKeyVal formats a key-value pair for logging
func formatKeyVal(key, val interface{}) string {
	return fmt.Sprintf("%v=%v", key, val)
}
