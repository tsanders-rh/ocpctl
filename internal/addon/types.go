package addon

import "github.com/tsanders-rh/ocpctl/pkg/types"

// AddonDefinition represents the YAML structure for an add-on
type AddonDefinition struct {
	ID                 string               `yaml:"id" validate:"required"`
	Name               string               `yaml:"name" validate:"required"`
	Description        string               `yaml:"description" validate:"required"`
	Category           string               `yaml:"category" validate:"required,oneof=backup migration cicd monitoring security storage networking"`
	Enabled            bool                 `yaml:"enabled"`
	SupportedPlatforms []string             `yaml:"supportedPlatforms" validate:"required,min=1"`
	Versions           []AddonVersionConfig `yaml:"versions" validate:"required,min=1,dive"`
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
