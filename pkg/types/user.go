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
	ID               string    `json:"id"`
	Email            string    `json:"email"`
	Username         string    `json:"username"`
	PasswordHash     string    `json:"-"` // Never expose in JSON
	Role             UserRole  `json:"role"`
	Timezone         string    `json:"timezone"`
	WorkHoursEnabled bool      `json:"work_hours_enabled"`
	WorkHoursStart   time.Time `json:"work_hours_start"` // Only time component used
	WorkHoursEnd     time.Time `json:"work_hours_end"`   // Only time component used
	WorkDays         int16     `json:"work_days"`        // Bitmask: bit 0=Sun, 1=Mon, ..., 6=Sat
	Active           bool      `json:"active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// WorkHoursSchedule represents a work hours configuration
type WorkHoursSchedule struct {
	StartTime string   `json:"start_time"` // "09:00" format
	EndTime   string   `json:"end_time"`   // "17:00" format
	WorkDays  []string `json:"work_days"`  // ["Monday", "Tuesday", ...]
}

// UserResponse is the public user representation (safe for API responses)
type UserResponse struct {
	ID               string             `json:"id"`
	Email            string             `json:"email"`
	Username         string             `json:"username"`
	Role             UserRole           `json:"role"`
	Timezone         string             `json:"timezone"`
	WorkHoursEnabled bool               `json:"work_hours_enabled"`
	WorkHours        *WorkHoursSchedule `json:"work_hours,omitempty"`
	Active           bool               `json:"active"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

// ToResponse converts a User to a UserResponse
func (u *User) ToResponse() *UserResponse {
	resp := &UserResponse{
		ID:               u.ID,
		Email:            u.Email,
		Username:         u.Username,
		Role:             u.Role,
		Timezone:         u.Timezone,
		WorkHoursEnabled: u.WorkHoursEnabled,
		Active:           u.Active,
		CreatedAt:        u.CreatedAt,
		UpdatedAt:        u.UpdatedAt,
	}

	// Include work hours if enabled
	if u.WorkHoursEnabled {
		resp.WorkHours = &WorkHoursSchedule{
			StartTime: u.WorkHoursStart.Format("15:04"),
			EndTime:   u.WorkHoursEnd.Format("15:04"),
			WorkDays:  WorkDaysToStrings(u.WorkDays),
		}
	}

	return resp
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
	Username    *string   `json:"username,omitempty" validate:"omitempty,min=2,max=100"`
	Role        *UserRole `json:"role,omitempty"`
	Active      *bool     `json:"active,omitempty"`
	NewPassword *string   `json:"new_password,omitempty" validate:"omitempty,min=8"`
}

// UpdateMeRequest represents a request for a user to update their own profile
type UpdateMeRequest struct {
	Username         *string            `json:"username,omitempty" validate:"omitempty,min=2,max=100"`
	Timezone         *string            `json:"timezone,omitempty"`
	WorkHoursEnabled *bool              `json:"work_hours_enabled,omitempty"`
	WorkHours        *WorkHoursSchedule `json:"work_hours,omitempty"`
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

// WorkDaysFromStrings converts day names to bitmask
// Input: ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"]
// Output: 62 (binary: 0111110)
func WorkDaysFromStrings(days []string) int16 {
	dayMap := map[string]int{
		"Sunday": 0, "Monday": 1, "Tuesday": 2, "Wednesday": 3,
		"Thursday": 4, "Friday": 5, "Saturday": 6,
	}
	var mask int16
	for _, day := range days {
		if bit, ok := dayMap[day]; ok {
			mask |= (1 << bit)
		}
	}
	return mask
}

// WorkDaysToStrings converts bitmask to day names
func WorkDaysToStrings(mask int16) []string {
	dayNames := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	var days []string
	for i := 0; i < 7; i++ {
		if mask&(1<<i) != 0 {
			days = append(days, dayNames[i])
		}
	}
	return days
}

// IsWorkDay checks if a given weekday is a work day
func IsWorkDay(mask int16, weekday time.Weekday) bool {
	return mask&(1<<int(weekday)) != 0
}
