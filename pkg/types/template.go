package types

import "time"

// PostConfigTemplate represents a reusable post-configuration template
type PostConfigTemplate struct {
	ID          string            `db:"id" json:"id"`
	Name        string            `db:"name" json:"name"`
	Description string            `db:"description" json:"description"`
	Config      CustomPostConfig  `db:"config" json:"config"`
	OwnerID     string            `db:"owner_id" json:"ownerId"`
	IsPublic    bool              `db:"is_public" json:"isPublic"` // If true, visible to all users
	Tags        []string          `db:"tags" json:"tags"`
	CreatedAt   time.Time         `db:"created_at" json:"createdAt"`
	UpdatedAt   time.Time         `db:"updated_at" json:"updatedAt"`
}
