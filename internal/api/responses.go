package api

import (
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
)

// PaginatedResponse represents a paginated API response
type PaginatedResponse struct {
	Data       interface{}      `json:"data"`
	Pagination PaginationMeta   `json:"pagination"`
	Filters    map[string]interface{} `json:"filters,omitempty"`
}

// PaginationMeta holds pagination metadata
type PaginationMeta struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// PaginationParams holds pagination parameters from request
type PaginationParams struct {
	Page    int
	PerPage int
	Offset  int
}

// DefaultPaginationParams returns default pagination parameters
func DefaultPaginationParams() *PaginationParams {
	return &PaginationParams{
		Page:    1,
		PerPage: 50,
		Offset:  0,
	}
}

// ParsePaginationParams extracts pagination parameters from query string
func ParsePaginationParams(c echo.Context) *PaginationParams {
	params := DefaultPaginationParams()

	if page := c.QueryParam("page"); page != "" {
		if p, err := strconv.Atoi(page); err == nil && p > 0 {
			params.Page = p
		}
	}

	if perPage := c.QueryParam("per_page"); perPage != "" {
		if pp, err := strconv.Atoi(perPage); err == nil && pp > 0 && pp <= 100 {
			params.PerPage = pp
		}
	}

	params.Offset = (params.Page - 1) * params.PerPage
	return params
}

// CalculatePagination calculates pagination metadata
func CalculatePagination(page, perPage, total int) PaginationMeta {
	totalPages := total / perPage
	if total%perPage > 0 {
		totalPages++
	}

	return PaginationMeta{
		Page:       page,
		PerPage:    perPage,
		Total:      total,
		TotalPages: totalPages,
	}
}

// SuccessPaginated returns a paginated success response
func SuccessPaginated(c echo.Context, data interface{}, pagination PaginationMeta, filters map[string]interface{}) error {
	return c.JSON(http.StatusOK, &PaginatedResponse{
		Data:       data,
		Pagination: pagination,
		Filters:    filters,
	})
}

// SuccessCreated returns a 201 Created response
func SuccessCreated(c echo.Context, data interface{}) error {
	return c.JSON(http.StatusCreated, data)
}

// SuccessOK returns a 200 OK response
func SuccessOK(c echo.Context, data interface{}) error {
	return c.JSON(http.StatusOK, data)
}

// SuccessNoContent returns a 204 No Content response
func SuccessNoContent(c echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}
