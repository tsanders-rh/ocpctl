package types

import "time"

// IAMPrincipalMapping represents a mapping between an AWS IAM principal and an internal user
type IAMPrincipalMapping struct {
	ID              string    `json:"id"`
	IAMPrincipalARN string    `json:"iam_principal_arn"`
	UserID          string    `json:"user_id"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// CreateIAMMapping is used when creating a new IAM principal mapping
type CreateIAMMapping struct {
	IAMPrincipalARN string `json:"iam_principal_arn" validate:"required"`
	UserID          string `json:"user_id" validate:"required"`
	Enabled         bool   `json:"enabled"`
}

// UpdateIAMMapping is used when updating an existing IAM principal mapping
type UpdateIAMMapping struct {
	Enabled *bool `json:"enabled,omitempty"`
}
