package profile

import (
	"fmt"
	"sync"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Registry provides fast in-memory access to cluster profiles
type Registry struct {
	mu       sync.RWMutex
	profiles map[string]*Profile // keyed by profile name
	loader   *Loader
}

// NewRegistry creates a new profile registry and loads all profiles
func NewRegistry(loader *Loader) (*Registry, error) {
	r := &Registry{
		profiles: make(map[string]*Profile),
		loader:   loader,
	}

	if err := r.Reload(); err != nil {
		return nil, fmt.Errorf("initial profile load: %w", err)
	}

	return r, nil
}

// Get retrieves a profile by name
func (r *Registry) Get(name string) (*Profile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profile, exists := r.profiles[name]
	if !exists {
		return nil, fmt.Errorf("profile not found: %s", name)
	}

	if !profile.Enabled {
		return nil, fmt.Errorf("profile disabled: %s", name)
	}

	return profile, nil
}

// List returns all enabled profiles
func (r *Registry) List() []*Profile {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profiles := make([]*Profile, 0, len(r.profiles))
	for _, profile := range r.profiles {
		if profile.Enabled {
			profiles = append(profiles, profile)
		}
	}

	return profiles
}

// ListAll returns all profiles including disabled ones
func (r *Registry) ListAll() []*Profile {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profiles := make([]*Profile, 0, len(r.profiles))
	for _, profile := range r.profiles {
		profiles = append(profiles, profile)
	}

	return profiles
}

// ListByPlatform returns all enabled profiles for a platform
func (r *Registry) ListByPlatform(platform types.Platform) []*Profile {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profiles := make([]*Profile, 0)
	for _, profile := range r.profiles {
		if profile.Enabled && profile.Platform == platform {
			profiles = append(profiles, profile)
		}
	}

	return profiles
}

// Exists checks if a profile exists and is enabled
func (r *Registry) Exists(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profile, exists := r.profiles[name]
	return exists && profile.Enabled
}

// Reload reloads all profiles from disk
func (r *Registry) Reload() error {
	profiles, err := r.loader.LoadAll()
	if err != nil {
		return fmt.Errorf("load profiles: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Clear existing profiles
	r.profiles = make(map[string]*Profile)

	// Add newly loaded profiles
	for _, profile := range profiles {
		r.profiles[profile.Name] = profile
	}

	return nil
}

// Count returns the total number of profiles (including disabled)
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.profiles)
}

// CountEnabled returns the number of enabled profiles
func (r *Registry) CountEnabled() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, profile := range r.profiles {
		if profile.Enabled {
			count++
		}
	}

	return count
}
