package api

import (
	"regexp"

	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// CustomValidator wraps the validator instance
type CustomValidator struct {
	validator *validator.Validate
}

// Cluster name must follow DNS-1123 subdomain format:
// - contain only lowercase alphanumeric characters or '-'
// - start with an alphanumeric character
// - end with an alphanumeric character
const clusterNamePattern = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`

// NewValidator creates a new custom validator
func NewValidator() *CustomValidator {
	v := validator.New()

	// Register custom validation for cluster names
	v.RegisterValidation("cluster_name", func(fl validator.FieldLevel) bool {
		name := fl.Field().String()
		matched, _ := regexp.MatchString(clusterNamePattern, name)
		return matched
	})

	return &CustomValidator{
		validator: v,
	}
}

// Validate validates a struct
func (cv *CustomValidator) Validate(i interface{}) error {
	if err := cv.validator.Struct(i); err != nil {
		// Provide user-friendly error messages for custom validators
		if validationErrors, ok := err.(validator.ValidationErrors); ok {
			for _, e := range validationErrors {
				if e.Tag() == "cluster_name" {
					return echo.NewHTTPError(422, "Cluster name must contain only lowercase letters, numbers, and hyphens, and must start and end with an alphanumeric character")
				}
			}
		}
		return echo.NewHTTPError(422, err.Error())
	}
	return nil
}
