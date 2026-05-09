package types

import "time"

// Team represents an organizational team for cluster grouping and access control
type Team struct {
	ID          string     `json:"id" db:"id"`
	Name        string     `json:"name" db:"name"`
	Description *string    `json:"description,omitempty" db:"description"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" db:"updated_at"`
	CreatedBy   *string    `json:"created_by,omitempty" db:"created_by"`
}

// TeamAdminMapping represents the assignment of team admin privileges to a user for a specific team
type TeamAdminMapping struct {
	ID        string     `json:"id" db:"id"`
	UserID    string     `json:"user_id" db:"user_id"`
	Team      string     `json:"team" db:"team"`
	GrantedBy *string    `json:"granted_by,omitempty" db:"granted_by"`
	GrantedAt time.Time  `json:"granted_at" db:"granted_at"`
	Notes     *string    `json:"notes,omitempty" db:"notes"`
}

// CreateTeamRequest represents a request to create a new team
type CreateTeamRequest struct {
	Name        string  `json:"name" validate:"required,min=2,max=255"`
	Description *string `json:"description,omitempty"`
}

// UpdateTeamRequest represents a request to update team metadata
type UpdateTeamRequest struct {
	Description *string `json:"description,omitempty"`
}

// GrantTeamAdminRequest represents a request to grant team admin privileges
type GrantTeamAdminRequest struct {
	UserID string  `json:"user_id" validate:"required,uuid"`
	Notes  *string `json:"notes,omitempty"`
}

// TeamAdminResponse represents a team admin with additional user information
type TeamAdminResponse struct {
	TeamAdminMapping
	Username string `json:"username"`
	Email    string `json:"email"`
}

// UserTeamMembership represents a user's membership in a team
type UserTeamMembership struct {
	ID      string     `json:"id" db:"id"`
	UserID  string     `json:"user_id" db:"user_id"`
	Team    string     `json:"team" db:"team"`
	AddedBy *string    `json:"added_by,omitempty" db:"added_by"`
	AddedAt time.Time  `json:"added_at" db:"added_at"`
	Notes   *string    `json:"notes,omitempty" db:"notes"`
}

// AddUserToTeamRequest represents a request to add a user to a team
type AddUserToTeamRequest struct {
	UserID string  `json:"user_id" validate:"required,uuid"`
	Notes  *string `json:"notes,omitempty"`
}
