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
	SupportedPlatforms []string            `db:"supported_platforms" json:"supportedPlatforms"`
	Enabled            bool                `db:"enabled" json:"enabled"`
	CreatedAt          time.Time           `db:"created_at" json:"createdAt"`
	UpdatedAt          time.Time           `db:"updated_at" json:"updatedAt"`
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
