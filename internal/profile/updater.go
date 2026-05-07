package profile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// VersionUpdate represents requested version updates for a profile
type VersionUpdate struct {
	OpenshiftVersions   []string `json:"openshift_versions,omitempty"`
	KubernetesVersions  []string `json:"kubernetes_versions,omitempty"`
}

// ProfileUpdater updates profile YAML files
type ProfileUpdater struct {
	profilesDir string
	maxBackups  int
}

// NewProfileUpdater creates a new profile updater
func NewProfileUpdater(profilesDir string) *ProfileUpdater {
	return &ProfileUpdater{
		profilesDir: profilesDir,
		maxBackups:  5, // Keep last 5 backups
	}
}

// UpdateVersions updates version allowlists in a profile YAML file
func (pu *ProfileUpdater) UpdateVersions(profileName string, updates *VersionUpdate) (string, error) {
	profilePath := filepath.Join(pu.profilesDir, profileName+".yaml")

	// Check if profile exists
	if _, err := os.Stat(profilePath); os.IsNotExist(err) {
		return "", fmt.Errorf("profile not found: %s", profileName)
	}

	// Create backup
	backupPath, err := pu.CreateBackup(profilePath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	// Read original file
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read profile: %w", err)
	}

	// Parse YAML with Node API to preserve comments
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return "", fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Update the YAML node tree
	if err := pu.updateYAMLNode(&node, updates); err != nil {
		return "", fmt.Errorf("failed to update YAML: %w", err)
	}

	// Marshal back to YAML
	updatedData, err := yaml.Marshal(&node)
	if err != nil {
		return "", fmt.Errorf("failed to marshal YAML: %w", err)
	}

	// Write updated file
	if err := os.WriteFile(profilePath, updatedData, 0644); err != nil {
		return "", fmt.Errorf("failed to write updated profile: %w", err)
	}

	// Validate the updated profile
	loader := NewLoader(pu.profilesDir)
	profile, err := loader.Load(profileName)
	if err != nil {
		// Rollback on validation error
		if restoreErr := pu.restoreFromBackup(profilePath, backupPath); restoreErr != nil {
			return "", fmt.Errorf("validation failed and rollback failed: %w, rollback error: %v", err, restoreErr)
		}
		return "", fmt.Errorf("validation failed, rolled back: %w", err)
	}

	if err := pu.ValidateUpdate(profile); err != nil {
		// Rollback on validation error
		if restoreErr := pu.restoreFromBackup(profilePath, backupPath); restoreErr != nil {
			return "", fmt.Errorf("validation failed and rollback failed: %w, rollback error: %v", err, restoreErr)
		}
		return "", fmt.Errorf("validation failed, rolled back: %w", err)
	}

	// Clean up old backups
	pu.cleanupOldBackups(profileName)

	return backupPath, nil
}

// updateYAMLNode updates version allowlists in the YAML node tree
func (pu *ProfileUpdater) updateYAMLNode(node *yaml.Node, updates *VersionUpdate) error {
	// Navigate to the root document node
	if node.Kind != yaml.DocumentNode {
		return fmt.Errorf("expected document node, got %v", node.Kind)
	}

	if len(node.Content) == 0 {
		return fmt.Errorf("empty document")
	}

	rootMap := node.Content[0]
	if rootMap.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping node at root, got %v", rootMap.Kind)
	}

	// Find openshiftVersions or kubernetesVersions nodes
	for i := 0; i < len(rootMap.Content); i += 2 {
		keyNode := rootMap.Content[i]
		valueNode := rootMap.Content[i+1]

		if keyNode.Value == "openshiftVersions" && len(updates.OpenshiftVersions) > 0 {
			if err := pu.updateVersionsNode(valueNode, updates.OpenshiftVersions); err != nil {
				return fmt.Errorf("failed to update openshiftVersions: %w", err)
			}
		} else if keyNode.Value == "kubernetesVersions" && len(updates.KubernetesVersions) > 0 {
			if err := pu.updateVersionsNode(valueNode, updates.KubernetesVersions); err != nil {
				return fmt.Errorf("failed to update kubernetesVersions: %w", err)
			}
		}
	}

	return nil
}

// updateVersionsNode updates the allowlist array in a versions node
func (pu *ProfileUpdater) updateVersionsNode(versionsNode *yaml.Node, newVersions []string) error {
	if versionsNode.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping node for versions, got %v", versionsNode.Kind)
	}

	// Sort versions before updating
	sortedVersions := make([]string, len(newVersions))
	copy(sortedVersions, newVersions)
	sort.Strings(sortedVersions)

	// Find allowlist node
	for i := 0; i < len(versionsNode.Content); i += 2 {
		keyNode := versionsNode.Content[i]
		valueNode := versionsNode.Content[i+1]

		if keyNode.Value == "allowlist" {
			// Update allowlist array
			if valueNode.Kind != yaml.SequenceNode {
				return fmt.Errorf("expected sequence node for allowlist, got %v", valueNode.Kind)
			}

			// Clear existing content
			valueNode.Content = make([]*yaml.Node, 0, len(sortedVersions))

			// Add new versions
			for _, v := range sortedVersions {
				versionNode := &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: v,
				}
				valueNode.Content = append(valueNode.Content, versionNode)
			}

			return nil
		}
	}

	return fmt.Errorf("allowlist field not found in versions node")
}

// CreateBackup creates a timestamped backup of the profile file
func (pu *ProfileUpdater) CreateBackup(profilePath string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.backup.%s", profilePath, timestamp)

	// Read original file
	data, err := os.ReadFile(profilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	// Write backup
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	return backupPath, nil
}

// restoreFromBackup restores a profile from a backup file
func (pu *ProfileUpdater) restoreFromBackup(profilePath, backupPath string) error {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	if err := os.WriteFile(profilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to restore from backup: %w", err)
	}

	return nil
}

// Rollback restores a profile from the latest backup
func (pu *ProfileUpdater) Rollback(profileName string) error {
	profilePath := filepath.Join(pu.profilesDir, profileName+".yaml")

	// Find latest backup
	backups, err := pu.findBackups(profileName)
	if err != nil {
		return fmt.Errorf("failed to find backups: %w", err)
	}

	if len(backups) == 0 {
		return fmt.Errorf("no backups found for profile: %s", profileName)
	}

	// Use the most recent backup
	latestBackup := backups[len(backups)-1]

	// Restore from backup
	if err := pu.restoreFromBackup(profilePath, latestBackup); err != nil {
		return fmt.Errorf("failed to rollback: %w", err)
	}

	return nil
}

// findBackups finds all backup files for a profile
func (pu *ProfileUpdater) findBackups(profileName string) ([]string, error) {
	pattern := filepath.Join(pu.profilesDir, profileName+".yaml.backup.*")
	backups, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	// Sort by timestamp (filename)
	sort.Strings(backups)

	return backups, nil
}

// cleanupOldBackups removes old backups beyond the maxBackups limit
func (pu *ProfileUpdater) cleanupOldBackups(profileName string) error {
	backups, err := pu.findBackups(profileName)
	if err != nil {
		return err
	}

	// Remove oldest backups if we exceed maxBackups
	if len(backups) > pu.maxBackups {
		toRemove := backups[:len(backups)-pu.maxBackups]
		for _, backup := range toRemove {
			if err := os.Remove(backup); err != nil {
				// Log error but don't fail
				fmt.Printf("Warning: failed to remove old backup %s: %v\n", backup, err)
			}
		}
	}

	return nil
}

// ValidateUpdate validates an updated profile
func (pu *ProfileUpdater) ValidateUpdate(profile *Profile) error {
	// 1. Required fields present
	if profile.Name == "" {
		return fmt.Errorf("profile name is required")
	}
	if profile.Platform == "" {
		return fmt.Errorf("platform is required")
	}
	if profile.ClusterType == "" {
		return fmt.Errorf("clusterType is required")
	}

	// 2. Version format validation
	if profile.OpenshiftVersions != nil {
		if err := pu.validateVersionFormats(profile.OpenshiftVersions.Allowlist); err != nil {
			return fmt.Errorf("invalid openshiftVersions: %w", err)
		}
		if err := pu.validateNoDuplicates(profile.OpenshiftVersions.Allowlist); err != nil {
			return fmt.Errorf("duplicate openshiftVersions: %w", err)
		}
	}

	if profile.KubernetesVersions != nil {
		if err := pu.validateVersionFormats(profile.KubernetesVersions.Allowlist); err != nil {
			return fmt.Errorf("invalid kubernetesVersions: %w", err)
		}
		if err := pu.validateNoDuplicates(profile.KubernetesVersions.Allowlist); err != nil {
			return fmt.Errorf("duplicate kubernetesVersions: %w", err)
		}
	}

	// 3. Region config intact
	if len(profile.Regions.Allowlist) == 0 {
		return fmt.Errorf("regions.allowlist is required")
	}

	// 4. Compute config intact (always present as it's not a pointer)
	// No need for nil check

	return nil
}

// validateVersionFormats validates version string formats
func (pu *ProfileUpdater) validateVersionFormats(versions []string) error {
	// Support both semantic versions (4.20.3) and minor versions (1.30)
	semverRegex := `^\d+\.\d+(\.\d+)?$`

	for _, v := range versions {
		matched, err := filepath.Match(semverRegex, v)
		if err != nil {
			return fmt.Errorf("regex error: %w", err)
		}
		if !matched {
			// Try simple string validation
			parts := strings.Split(v, ".")
			if len(parts) < 2 || len(parts) > 3 {
				return fmt.Errorf("invalid version format: %s (expected X.Y or X.Y.Z)", v)
			}
		}
	}

	return nil
}

// validateNoDuplicates ensures no duplicate versions in allowlist
func (pu *ProfileUpdater) validateNoDuplicates(versions []string) error {
	seen := make(map[string]bool)
	for _, v := range versions {
		if seen[v] {
			return fmt.Errorf("duplicate version: %s", v)
		}
		seen[v] = true
	}
	return nil
}

// DryRun performs validation without writing files
func (pu *ProfileUpdater) DryRun(profileName string, updates *VersionUpdate) (*Profile, error) {
	profilePath := filepath.Join(pu.profilesDir, profileName+".yaml")

	// Check if profile exists
	if _, err := os.Stat(profilePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("profile not found: %s", profileName)
	}

	// Create temporary file for dry-run
	tmpFile, err := os.CreateTemp("", "profile-dryrun-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Copy original to temp
	src, err := os.Open(profilePath)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	if _, err := io.Copy(tmpFile, src); err != nil {
		return nil, err
	}
	tmpFile.Close()

	// Read and update temp file
	data, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return nil, err
	}

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if err := pu.updateYAMLNode(&node, updates); err != nil {
		return nil, fmt.Errorf("failed to update YAML: %w", err)
	}

	updatedData, err := yaml.Marshal(&node)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(tmpFile.Name(), updatedData, 0644); err != nil {
		return nil, err
	}

	// Load and validate using a temporary loader that points to temp directory
	// Create temporary profile file with correct name for validation
	tempDir := filepath.Dir(tmpFile.Name())
	tempProfilePath := filepath.Join(tempDir, profileName+".yaml")
	if err := os.Rename(tmpFile.Name(), tempProfilePath); err != nil {
		return nil, err
	}
	defer os.Remove(tempProfilePath)

	loader := NewLoader(tempDir)
	profile, err := loader.Load(profileName)
	if err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	if err := pu.ValidateUpdate(profile); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return profile, nil
}
