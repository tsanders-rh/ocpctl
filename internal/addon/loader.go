package addon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-playground/validator/v10"
	"gopkg.in/yaml.v3"
)

type Loader struct {
	addonsDir string
	validate  *validator.Validate
}

func NewLoader(addonsDir string) *Loader {
	return &Loader{
		addonsDir: addonsDir,
		validate:  validator.New(),
	}
}

// Load reads and validates a single add-on YAML file
func (l *Loader) Load(id string) (*AddonDefinition, error) {
	filename := filepath.Join(l.addonsDir, id+".yaml")

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read addon file %s: %w", filename, err)
	}

	var addon AddonDefinition
	if err := yaml.Unmarshal(data, &addon); err != nil {
		return nil, fmt.Errorf("parse addon YAML %s: %w", filename, err)
	}

	if err := l.Validate(&addon); err != nil {
		return nil, fmt.Errorf("validate addon %s: %w", id, err)
	}

	return &addon, nil
}

// LoadAll reads all add-on YAML files from the definitions directory
func (l *Loader) LoadAll() ([]*AddonDefinition, error) {
	entries, err := os.ReadDir(l.addonsDir)
	if err != nil {
		return nil, fmt.Errorf("read addons directory: %w", err)
	}

	addons := []*AddonDefinition{}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "SCHEMA.md" {
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".yaml") && !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}

		id := strings.TrimSuffix(entry.Name(), ".yaml")
		id = strings.TrimSuffix(id, ".yml")

		addon, err := l.Load(id)
		if err != nil {
			return nil, fmt.Errorf("load addon %s: %w", id, err)
		}

		addons = append(addons, addon)
	}

	if len(addons) == 0 {
		return nil, fmt.Errorf("no addons found in %s", l.addonsDir)
	}

	return addons, nil
}

// Validate ensures the add-on definition is valid
func (l *Loader) Validate(addon *AddonDefinition) error {
	if err := l.validate.Struct(addon); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Ensure exactly one default version
	defaultCount := 0
	channels := make(map[string]bool)

	for _, v := range addon.Versions {
		if v.IsDefault {
			defaultCount++
		}
		if channels[v.Channel] {
			return fmt.Errorf("duplicate channel: %s", v.Channel)
		}
		channels[v.Channel] = true
	}

	if defaultCount == 0 {
		return fmt.Errorf("no default version specified")
	}
	if defaultCount > 1 {
		return fmt.Errorf("multiple default versions specified (%d)", defaultCount)
	}

	return nil
}
