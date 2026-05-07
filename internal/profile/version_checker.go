package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"sync"
	"time"

	"github.com/hashicorp/go-version"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// VersionCache stores cached version data
type VersionCache struct {
	OpenShiftVersions []string
	ROSAVersions      []string
	EKSVersions       []string
	GKEVersions       []string
	LastUpdated       time.Time
	mu                sync.RWMutex
}

// ProfileVersionStatus represents version update availability for a profile
type ProfileVersionStatus struct {
	ProfileName        string   `json:"profile_name"`
	CurrentVersions    []string `json:"current_versions"`
	AvailableVersions  []string `json:"available_versions"`
	NewVersions        []string `json:"new_versions"`        // Versions not in current list
	UpdateCount        int      `json:"update_count"`
	LastChecked        time.Time `json:"last_checked"`
}

// VersionChecker checks for profile version updates
type VersionChecker struct {
	cache       *VersionCache
	cacheTTL    time.Duration
	httpClient  *http.Client
}

// NewVersionChecker creates a new version checker
func NewVersionChecker() *VersionChecker {
	return &VersionChecker{
		cache: &VersionCache{},
		cacheTTL: 6 * time.Hour,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// CheckProfileUpdates checks if a profile has available version updates
func (vc *VersionChecker) CheckProfileUpdates(ctx context.Context, prof *Profile) (*ProfileVersionStatus, error) {
	status := &ProfileVersionStatus{
		ProfileName:    prof.Name,
		CurrentVersions: []string{},
		AvailableVersions: []string{},
		NewVersions:    []string{},
		LastChecked:    time.Now(),
	}

	// Determine which version source to use based on cluster type
	var availableVersions []string
	var err error

	switch prof.ClusterType {
	case types.ClusterTypeOpenShift:
		// Get OpenShift versions
		availableVersions, err = vc.GetOpenShiftVersions(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get OpenShift versions: %w", err)
		}

		// Extract current versions from profile
		if prof.OpenshiftVersions != nil && len(prof.OpenshiftVersions.Allowlist) > 0 {
			status.CurrentVersions = prof.OpenshiftVersions.Allowlist
		}

	case "rosa":
		// Get ROSA versions
		availableVersions, err = vc.GetROSAVersions(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get ROSA versions: %w", err)
		}

		if prof.OpenshiftVersions != nil && len(prof.OpenshiftVersions.Allowlist) > 0 {
			status.CurrentVersions = prof.OpenshiftVersions.Allowlist
		}

	case types.ClusterTypeEKS:
		// Get EKS/Kubernetes versions
		availableVersions, err = vc.GetKubernetesVersions(ctx, "eks")
		if err != nil {
			return nil, fmt.Errorf("failed to get EKS versions: %w", err)
		}

		if prof.KubernetesVersions != nil && len(prof.KubernetesVersions.Allowlist) > 0 {
			status.CurrentVersions = prof.KubernetesVersions.Allowlist
		}

	case types.ClusterTypeGKE:
		// Get GKE/Kubernetes versions
		availableVersions, err = vc.GetKubernetesVersions(ctx, "gke")
		if err != nil {
			return nil, fmt.Errorf("failed to get GKE versions: %w", err)
		}

		if prof.KubernetesVersions != nil && len(prof.KubernetesVersions.Allowlist) > 0 {
			status.CurrentVersions = prof.KubernetesVersions.Allowlist
		}

	default:
		return status, nil // No version checking for other cluster types
	}

	status.AvailableVersions = availableVersions

	// Find new versions not in current list
	currentMap := make(map[string]bool)
	for _, v := range status.CurrentVersions {
		currentMap[v] = true
	}

	for _, v := range availableVersions {
		if !currentMap[v] {
			status.NewVersions = append(status.NewVersions, v)
		}
	}

	status.UpdateCount = len(status.NewVersions)

	return status, nil
}

// GetOpenShiftVersions fetches available OpenShift versions from mirror
func (vc *VersionChecker) GetOpenShiftVersions(ctx context.Context) ([]string, error) {
	// Check cache first
	vc.cache.mu.RLock()
	if time.Since(vc.cache.LastUpdated) < vc.cacheTTL && len(vc.cache.OpenShiftVersions) > 0 {
		versions := vc.cache.OpenShiftVersions
		vc.cache.mu.RUnlock()
		return versions, nil
	}
	vc.cache.mu.RUnlock()

	// Fetch from OpenShift mirror
	url := "https://mirror.openshift.com/pub/openshift-v4/clients/ocp/"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := vc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OpenShift mirror: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse HTML for version directories
	versionRegex := regexp.MustCompile(`href="(4\.\d+\.\d+)/"`)
	matches := versionRegex.FindAllStringSubmatch(string(body), -1)

	versionsMap := make(map[string]bool)
	for _, match := range matches {
		if len(match) > 1 {
			versionsMap[match[1]] = true
		}
	}

	// Convert to slice and sort
	versions := make([]string, 0, len(versionsMap))
	for v := range versionsMap {
		versions = append(versions, v)
	}

	// Sort versions using semantic versioning
	sortVersions(versions)

	// Cache the results
	vc.cache.mu.Lock()
	vc.cache.OpenShiftVersions = versions
	vc.cache.LastUpdated = time.Now()
	vc.cache.mu.Unlock()

	return versions, nil
}

// GetROSAVersions fetches available ROSA versions via CLI
func (vc *VersionChecker) GetROSAVersions(ctx context.Context) ([]string, error) {
	// Check cache first
	vc.cache.mu.RLock()
	if time.Since(vc.cache.LastUpdated) < vc.cacheTTL && len(vc.cache.ROSAVersions) > 0 {
		versions := vc.cache.ROSAVersions
		vc.cache.mu.RUnlock()
		return versions, nil
	}
	vc.cache.mu.RUnlock()

	// Execute rosa list versions --output json
	cmd := exec.CommandContext(ctx, "rosa", "list", "versions", "--output", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute rosa CLI: %w", err)
	}

	// Parse JSON output
	var rosaVersions []struct {
		RawID string `json:"raw_id"`
	}
	if err := json.Unmarshal(output, &rosaVersions); err != nil {
		return nil, fmt.Errorf("failed to parse rosa output: %w", err)
	}

	versions := make([]string, 0, len(rosaVersions))
	for _, rv := range rosaVersions {
		// Extract version (e.g., "4.20.3" from "openshift-v4.20.3")
		versionRegex := regexp.MustCompile(`(\d+\.\d+\.\d+)`)
		if match := versionRegex.FindString(rv.RawID); match != "" {
			versions = append(versions, match)
		}
	}

	// Sort versions
	sortVersions(versions)

	// Cache the results
	vc.cache.mu.Lock()
	vc.cache.ROSAVersions = versions
	vc.cache.LastUpdated = time.Now()
	vc.cache.mu.Unlock()

	return versions, nil
}

// GetKubernetesVersions fetches available Kubernetes versions (EKS or GKE)
func (vc *VersionChecker) GetKubernetesVersions(ctx context.Context, platform string) ([]string, error) {
	switch platform {
	case "eks":
		return vc.getEKSVersions(ctx)
	case "gke":
		return vc.getGKEVersions(ctx)
	default:
		return nil, fmt.Errorf("unsupported platform: %s", platform)
	}
}

// getEKSVersions fetches EKS versions
func (vc *VersionChecker) getEKSVersions(ctx context.Context) ([]string, error) {
	// Check cache first
	vc.cache.mu.RLock()
	if time.Since(vc.cache.LastUpdated) < vc.cacheTTL && len(vc.cache.EKSVersions) > 0 {
		versions := vc.cache.EKSVersions
		vc.cache.mu.RUnlock()
		return versions, nil
	}
	vc.cache.mu.RUnlock()

	// For now, return hardcoded supported versions
	// TODO: Implement AWS EKS API call to get available versions dynamically
	// Could use: aws eks describe-addon-versions or eksctl CLI
	versions := []string{"1.30", "1.31", "1.32", "1.33", "1.34", "1.35"}

	// Cache the results
	vc.cache.mu.Lock()
	vc.cache.EKSVersions = versions
	vc.cache.LastUpdated = time.Now()
	vc.cache.mu.Unlock()

	return versions, nil
}

// getGKEVersions fetches GKE versions
func (vc *VersionChecker) getGKEVersions(ctx context.Context) ([]string, error) {
	// Check cache first
	vc.cache.mu.RLock()
	if time.Since(vc.cache.LastUpdated) < vc.cacheTTL && len(vc.cache.GKEVersions) > 0 {
		versions := vc.cache.GKEVersions
		vc.cache.mu.RUnlock()
		return versions, nil
	}
	vc.cache.mu.RUnlock()

	// Execute gcloud container get-server-config to get GKE versions
	// For now, return hardcoded supported versions
	// TODO: Implement GCP API call to get available versions
	versions := []string{"1.30", "1.31", "1.32", "1.33", "1.34"}

	// Cache the results
	vc.cache.mu.Lock()
	vc.cache.GKEVersions = versions
	vc.cache.LastUpdated = time.Now()
	vc.cache.mu.Unlock()

	return versions, nil
}

// RefreshCache forces a cache refresh for all version sources
func (vc *VersionChecker) RefreshCache(ctx context.Context) error {
	// Clear cache
	vc.cache.mu.Lock()
	vc.cache.LastUpdated = time.Time{}
	vc.cache.mu.Unlock()

	// Refresh all sources in parallel
	var wg sync.WaitGroup
	errors := make(chan error, 4)

	wg.Add(4)

	go func() {
		defer wg.Done()
		if _, err := vc.GetOpenShiftVersions(ctx); err != nil {
			errors <- fmt.Errorf("OpenShift: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		if _, err := vc.GetROSAVersions(ctx); err != nil {
			errors <- fmt.Errorf("ROSA: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		if _, err := vc.getEKSVersions(ctx); err != nil {
			errors <- fmt.Errorf("EKS: %w", err)
		}
	}()

	go func() {
		defer wg.Done()
		if _, err := vc.getGKEVersions(ctx); err != nil {
			errors <- fmt.Errorf("GKE: %w", err)
		}
	}()

	wg.Wait()
	close(errors)

	// Collect errors
	var errs []error
	for err := range errors {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("cache refresh errors: %v", errs)
	}

	return nil
}

// sortVersions sorts a slice of version strings using semantic versioning
func sortVersions(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		v1, err1 := version.NewVersion(versions[i])
		v2, err2 := version.NewVersion(versions[j])

		if err1 != nil || err2 != nil {
			// Fallback to string comparison if parsing fails
			return versions[i] < versions[j]
		}

		return v1.LessThan(v2)
	})
}

// GetCacheAge returns how old the cache is
func (vc *VersionChecker) GetCacheAge() time.Duration {
	vc.cache.mu.RLock()
	defer vc.cache.mu.RUnlock()
	return time.Since(vc.cache.LastUpdated)
}
