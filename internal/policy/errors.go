package policy

import "fmt"

// ValidationError represents a policy validation failure
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface
func (e ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationResult contains the outcome of policy validation
type ValidationResult struct {
	Valid       bool
	Errors      []ValidationError
	MergedTags  map[string]string
	DestroyAt   string // ISO8601 timestamp
}

// AddError adds a validation error
func (r *ValidationResult) AddError(field, message string) {
	r.Valid = false
	r.Errors = append(r.Errors, ValidationError{
		Field:   field,
		Message: message,
	})
}

// HasErrors returns true if there are validation errors
func (r *ValidationResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// FirstError returns the first validation error message
func (r *ValidationResult) FirstError() string {
	if len(r.Errors) == 0 {
		return ""
	}
	return r.Errors[0].Error()
}
