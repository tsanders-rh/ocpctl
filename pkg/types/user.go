package types

import "time"

// UserRole represents a user's role in the system
type UserRole string

const (
	RoleAdmin  UserRole = "ADMIN"
	RoleUser   UserRole = "USER"
	RoleViewer UserRole = "VIEWER"
)

// IsValid checks if the role is valid
func (r UserRole) IsValid() bool {
	switch r {
	case RoleAdmin, RoleUser, RoleViewer:
		return true
	default:
		return false
	}
}

// User represents a system user
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"` // Never expose in JSON
	Role         UserRole  `json:"role"`
	Active       bool      `json:"active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// UserResponse is the public user representation (safe for API responses)
type UserResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	Role      UserRole  `json:"role"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ToResponse converts a User to a UserResponse
func (u *User) ToResponse() *UserResponse {
	return &UserResponse{
		ID:        u.ID,
		Email:     u.Email,
		Username:  u.Username,
		Role:      u.Role,
		Active:    u.Active,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

// CreateUserRequest represents a request to create a new user
type CreateUserRequest struct {
	Email    string   `json:"email" validate:"required,email"`
	Username string   `json:"username" validate:"required,min=2,max=100"`
	Password string   `json:"password" validate:"required,min=8"`
	Role     UserRole `json:"role" validate:"required"`
}

// UpdateUserRequest represents a request to update a user
type UpdateUserRequest struct {
	Username *string   `json:"username,omitempty" validate:"omitempty,min=2,max=100"`
	Role     *UserRole `json:"role,omitempty"`
	Active   *bool     `json:"active,omitempty"`
}

// UpdateMeRequest represents a request for a user to update their own profile
type UpdateMeRequest struct {
	Username *string `json:"username,omitempty" validate:"omitempty,min=2,max=100"`
}

// ChangePasswordRequest represents a password change request
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" validate:"required"`
	NewPassword     string `json:"new_password" validate:"required,min=8"`
}

// LoginRequest represents a login request
type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// LoginResponse represents a successful login response
type LoginResponse struct {
	User        *UserResponse `json:"user"`
	AccessToken string        `json:"access_token"`
	ExpiresIn   int           `json:"expires_in"` // seconds
}

// RefreshToken represents a refresh token in the database
type RefreshToken struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	TokenHash string     `json:"-"` // Never expose
	ExpiresAt time.Time  `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}
