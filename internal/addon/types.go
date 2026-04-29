package addon

import "github.com/tsanders-rh/ocpctl/pkg/types"

// AddonDefinition represents the YAML structure for an add-on
type AddonDefinition struct {
	ID                 string               `yaml:"id" validate:"required"`
	Name               string               `yaml:"name" validate:"required"`
	Description        string               `yaml:"description" validate:"required"`
	Category           string               `yaml:"category" validate:"required,oneof=backup migration cicd monitoring security storage networking virtualization"`
	Enabled            bool                 `yaml:"enabled"`
	SupportedPlatforms []string             `yaml:"supportedPlatforms" validate:"required,min=1"`
	Versions           []AddonVersionConfig `yaml:"versions" validate:"required,min=1,dive"`
	Metadata           *AddonMetadata       `yaml:"metadata,omitempty"`
}

// AddonMetadata contains additional metadata about addon requirements and notes
type AddonMetadata struct {
	RequiresBareMetal bool     `yaml:"requiresBareMetal,omitempty" json:"requiresBareMetal,omitempty"`
	RequiredCapabilities []string `yaml:"requiredCapabilities,omitempty" json:"requiredCapabilities,omitempty"`
	Notes             []string `yaml:"notes,omitempty" json:"notes,omitempty"`
	Warnings          []string `yaml:"warnings,omitempty" json:"warnings,omitempty"`
}

// AddonVersionConfig defines a specific version of an add-on
type AddonVersionConfig struct {
	Channel     string                 `yaml:"channel" validate:"required"`
	DisplayName string                 `yaml:"displayName" validate:"required"`
	IsDefault   bool                   `yaml:"isDefault"`
	Config      types.CustomPostConfig `yaml:"config" validate:"required"`
}

// GetDefaultVersion returns the default version configuration
func (a *AddonDefinition) GetDefaultVersion() *AddonVersionConfig {
	for i := range a.Versions {
		if a.Versions[i].IsDefault {
			return &a.Versions[i]
		}
	}
	return nil
}
