package api

import (
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// CustomValidator wraps the validator instance
type CustomValidator struct {
	validator *validator.Validate
}

// NewValidator creates a new custom validator
func NewValidator() *CustomValidator {
	return &CustomValidator{
		validator: validator.New(),
	}
}

// Validate validates a struct
func (cv *CustomValidator) Validate(i interface{}) error {
	if err := cv.validator.Struct(i); err != nil {
		return echo.NewHTTPError(422, err.Error())
	}
	return nil
}
