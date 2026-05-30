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
	ConfigJSON         []byte              `db:"-" json:"-"`          // For database storage
	SupportedPlatforms []string            `db:"supported_platforms" json:"supportedPlatforms"`
	Enabled            bool                `db:"enabled" json:"enabled"`
	Version            string              `db:"version" json:"version"`
	DisplayName        string              `db:"display_name" json:"displayName"`
	IsDefault          bool                `db:"is_default" json:"isDefault"`
	Metadata           *AddonMetadata      `db:"metadata" json:"metadata,omitempty"`
	MetadataJSON       []byte              `db:"-" json:"-"`          // For database storage

	// Addon versioning and source tracking
	AddonSource        string              `db:"addon_source" json:"addonSource"`              // 'system' or 'user'
	CreatedByUserID    *string             `db:"created_by_user_id" json:"createdByUserId,omitempty"`
	IsPublished        bool                `db:"is_published" json:"isPublished"`
	PublishedAt        *time.Time          `db:"published_at" json:"publishedAt,omitempty"`
	ParentVersionID    *string             `db:"parent_version_id" json:"parentVersionId,omitempty"`  // Version lineage
	VersionNumber      int                 `db:"version_number" json:"versionNumber"`
	IsImmutable        bool                `db:"is_immutable" json:"isImmutable"`

	CreatedAt          time.Time           `db:"created_at" json:"createdAt"`
	UpdatedAt          time.Time           `db:"updated_at" json:"updatedAt"`
}

// AddonMetadata contains hardware requirements and additional addon information
type AddonMetadata struct {
	RequiresBareMetal    bool     `json:"requiresBareMetal,omitempty"`
	RequiredCapabilities []string `json:"requiredCapabilities,omitempty"`
	ConflictsWith        []string `json:"conflictsWith,omitempty"`        // List of addon IDs that conflict with this addon
	Notes                []string `json:"notes,omitempty"`
	Warnings             []string `json:"warnings,omitempty"`
}

// AddonSelection represents a user's selection of an add-on with a specific version
type AddonSelection struct {
	ID      string `json:"id" validate:"required" example:"oadp"`
	Version string `json:"version" validate:"required" example:"stable"`
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
