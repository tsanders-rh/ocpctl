package types

import "time"

// PostConfigAddon represents a pre-defined add-on configuration
type PostConfigAddon struct {
	ID                 string              `db:"id" json:"id"`
	AddonID            string              `db:"addon_id" json:"addonId"`
	Name               string              `db:"name" json:"name"`
	Description        string              `db:"description" json:"description"`
	Category           string              `db:"category" json:"category"`
	Config             CustomPostConfig    `db:"config" json:"config"`
	ConfigJSON         []byte              `db:"-" json:"-"` // For database storage
	SupportedPlatforms []string            `db:"supported_platforms" json:"supportedPlatforms"`
	Enabled            bool                `db:"enabled" json:"enabled"`
	Version            string              `db:"version" json:"version"`
	DisplayName        string              `db:"display_name" json:"displayName"`
	IsDefault          bool                `db:"is_default" json:"isDefault"`
	CreatedAt          time.Time           `db:"created_at" json:"createdAt"`
	UpdatedAt          time.Time           `db:"updated_at" json:"updatedAt"`
}

// AddonSelection represents a user's selection of an add-on with a specific version
type AddonSelection struct {
	ID      string `json:"id" validate:"required"`
	Version string `json:"version" validate:"required"`
}

// AddonCategory represents the category of an add-on
type AddonCategory string

const (
	AddonCategoryBackup    AddonCategory = "backup"
	AddonCategoryMigration AddonCategory = "migration"
	AddonCategoryCICD      AddonCategory = "cicd"
	AddonCategoryMonitoring AddonCategory = "monitoring"
	AddonCategorySecurity  AddonCategory = "security"
	AddonCategoryStorage   AddonCategory = "storage"
	AddonCategoryNetworking AddonCategory = "networking"
)
