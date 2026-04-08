package types

import "time"

// APIKeyScope represents the permission scope of an API key
type APIKeyScope string

const (
	APIKeyScopeReadOnly   APIKeyScope = "read_only"
	APIKeyScopeFullAccess APIKeyScope = "full_access"
)

// IsValid checks if the API key scope is valid
func (s APIKeyScope) IsValid() bool {
	switch s {
	case APIKeyScopeReadOnly, APIKeyScopeFullAccess:
		return true
	default:
		return false
	}
}

// APIKey represents an API key in the database
type APIKey struct {
	ID         string       `json:"id"`
	UserID     string       `json:"user_id"`
	Name       string       `json:"name"`
	KeyPrefix  string       `json:"key_prefix"` // First 8 chars for display
	KeyHash    string       `json:"-"`          // Never expose hash
	Scope      APIKeyScope  `json:"scope"`
	LastUsedAt *time.Time   `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time   `json:"expires_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	RevokedAt  *time.Time   `json:"revoked_at,omitempty"`
}

// APIKeyResponse is the safe public representation of an API key
type APIKeyResponse struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	KeyPrefix  string       `json:"key_prefix"`
	Scope      APIKeyScope  `json:"scope"`
	LastUsedAt *time.Time   `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time   `json:"expires_at,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
	RevokedAt  *time.Time   `json:"revoked_at,omitempty"`
	IsActive   bool         `json:"is_active"`
	IsExpired  bool         `json:"is_expired"`
}

// ToResponse converts an APIKey to APIKeyResponse
func (k *APIKey) ToResponse() *APIKeyResponse {
	now := time.Now()
	isActive := k.RevokedAt == nil && (k.ExpiresAt == nil || k.ExpiresAt.After(now))
	isExpired := k.ExpiresAt != nil && k.ExpiresAt.Before(now)

	return &APIKeyResponse{
		ID:         k.ID,
		Name:       k.Name,
		KeyPrefix:  k.KeyPrefix,
		Scope:      k.Scope,
		LastUsedAt: k.LastUsedAt,
		ExpiresAt:  k.ExpiresAt,
		CreatedAt:  k.CreatedAt,
		RevokedAt:  k.RevokedAt,
		IsActive:   isActive,
		IsExpired:  isExpired,
	}
}

// CreateAPIKeyRequest represents a request to create a new API key
type CreateAPIKeyRequest struct {
	Name      string       `json:"name" validate:"required,min=3,max=255"`
	Scope     APIKeyScope  `json:"scope" validate:"required"`
	ExpiresAt *time.Time   `json:"expires_at,omitempty"`
}

// CreateAPIKeyResponse includes the full plaintext key (only returned once)
type CreateAPIKeyResponse struct {
	APIKey    *APIKeyResponse `json:"api_key"`
	PlainKey  string          `json:"plain_key"` // Full key, only shown on creation
}

// UpdateAPIKeyRequest represents a request to update an API key
type UpdateAPIKeyRequest struct {
	Name *string `json:"name,omitempty" validate:"omitempty,min=3,max=255"`
}
