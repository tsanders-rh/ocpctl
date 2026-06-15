package api

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/labstack/echo/v4"
	"gopkg.in/yaml.v3"
)

// InstalledVersionsHandler handles checking installed binary versions
type InstalledVersionsHandler struct{}

// NewInstalledVersionsHandler creates a new installed versions handler
func NewInstalledVersionsHandler() *InstalledVersionsHandler {
	return &InstalledVersionsHandler{}
}

// InstalledVersionsResponse represents the response for installed versions
type InstalledVersionsResponse struct {
	OpenShiftVersions  map[string]InstalledVersion `json:"openshift_versions"`
	TotalInstalled     int                         `json:"total_installed"`
	BinariesPath       string                      `json:"binaries_path"`
}

// InstalledVersion represents details about an installed version
type InstalledVersion struct {
	MajorMinor        string   `json:"major_minor"`    // e.g., "4.20"
	ExactVersion      string   `json:"exact_version"`  // e.g., "4.20.17"
	BinaryPath        string   `json:"binary_path"`    // e.g., "/usr/local/bin/openshift-install-4.20"
	CcoctlPath        string   `json:"ccoctl_path,omitempty"` // e.g., "/usr/local/bin/ccoctl-4.20"
	ProfileReferences []string `json:"profile_references,omitempty"` // e.g., ["4.20", "4.20.3", "4.20.4"]
}

//	@Summary		Get installed OpenShift versions
//	@Description	Returns all OpenShift installer versions currently installed on the server by checking /usr/local/bin/openshift-install-* binaries
//	@Tags			admin
//	@Produce		json
//	@Success		200	{object}	InstalledVersionsResponse
//	@Failure		401	{object}	ErrorResponse	"Unauthorized"
//	@Failure		403	{object}	ErrorResponse	"Forbidden - Admin access required"
//	@Failure		500	{object}	ErrorResponse	"Internal server error"
//	@Security		BearerAuth
//	@Router			/admin/installed-versions [get]
func (h *InstalledVersionsHandler) HandleGetInstalledVersions(c echo.Context) error {
	versions := make(map[string]InstalledVersion)
	binariesPath := "/usr/local/bin"
	profilesPath := "/opt/ocpctl/profiles"

	// Scan profile references to build map of major.minor -> version references
	profileReferences := scanProfileReferences(profilesPath)

	// Pattern to match openshift-install binaries
	// Matches: openshift-install-4.20, openshift-install-4.22-standard, etc.
	pattern := filepath.Join(binariesPath, "openshift-install-*")

	// Find all matching binaries
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to glob binaries: %v", err))
	}

	// Regex to extract version from filename
	// Matches: openshift-install-4.20, openshift-install-4.21.19, etc.
	// Only match major.minor or major.minor.patch format (no suffixes like -standard, -patched, -ec, -rhel9)
	filenameRegex := regexp.MustCompile(`openshift-install-(4\.\d+(?:\.\d+)?(?:-rc\.\d+|-ec\.\d+)?)$`)

	for _, binaryPath := range matches {
		// Extract version from filename
		filename := filepath.Base(binaryPath)

		// Skip non-standard variants (rhel9, standard, patched, etc.)
		if strings.Contains(filename, "-rhel9") ||
		   strings.Contains(filename, "-standard") ||
		   strings.Contains(filename, "-patched") {
			continue
		}

		filenameMatches := filenameRegex.FindStringSubmatch(filename)
		if len(filenameMatches) < 2 {
			continue
		}
		versionFromFilename := filenameMatches[1]

		// Extract major.minor (e.g., "4.21.19" -> "4.21", "4.22.0-ec.5" -> "4.22")
		majorMinor := versionFromFilename
		parts := strings.Split(strings.Split(versionFromFilename, "-")[0], ".")
		if len(parts) >= 2 {
			majorMinor = parts[0] + "." + parts[1]
		}

		// Get exact version by running the binary
		exactVersion, err := getExactVersion(binaryPath)
		if err != nil {
			// If we can't get exact version, log warning but continue
			fmt.Printf("Warning: failed to get version for %s: %v\n", binaryPath, err)
			// Use major.minor as fallback
			exactVersion = majorMinor
		}

		// Check for corresponding ccoctl binary
		ccoctlPath := filepath.Join(binariesPath, "ccoctl-"+majorMinor)
		ccoctlExists := fileExists(ccoctlPath)

		// Get profile references for this major.minor version
		profileRefs := profileReferences[majorMinor]

		// If we already have an entry for this major.minor, prefer the newer/higher patch version
		if existing, exists := versions[majorMinor]; exists {
			// Compare versions - prefer the higher one
			if shouldPreferExisting(existing.ExactVersion, exactVersion) {
				continue
			}
		}

		versions[majorMinor] = InstalledVersion{
			MajorMinor:        majorMinor,
			ExactVersion:      exactVersion,
			BinaryPath:        binaryPath,
			CcoctlPath:        func() string {
				if ccoctlExists {
					return ccoctlPath
				}
				return ""
			}(),
			ProfileReferences: profileRefs,
		}
	}

	response := InstalledVersionsResponse{
		OpenShiftVersions: versions,
		TotalInstalled:    len(versions),
		BinariesPath:      binariesPath,
	}

	return c.JSON(http.StatusOK, response)
}

// getExactVersion runs the binary to get its exact version
func getExactVersion(binaryPath string) (string, error) {
	cmd := exec.Command(binaryPath, "version")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to run version command: %w", err)
	}

	// Parse output: "/usr/local/bin/openshift-install-4.20 4.20.17"
	// We want just the version number
	outputStr := strings.TrimSpace(string(output))
	parts := strings.Fields(outputStr)
	if len(parts) >= 2 {
		return parts[1], nil
	}

	return "", fmt.Errorf("unexpected version output format: %s", outputStr)
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	cmd := exec.Command("test", "-f", path)
	err := cmd.Run()
	return err == nil
}

// shouldPreferExisting compares two version strings and returns true if existingVersion
// should be preferred over newVersion (i.e., existing is newer or equal)
func shouldPreferExisting(existingVersion, newVersion string) bool {
	// Simple string comparison for now
	// GA releases (4.22.1) should be preferred over dev previews (4.22.0-ec.5)
	// Higher patch versions should be preferred (4.21.19 > 4.21.10)

	// If new version contains -ec. or -rc., prefer existing if it doesn't
	newIsDev := strings.Contains(newVersion, "-ec.") || strings.Contains(newVersion, "-rc.")
	existingIsDev := strings.Contains(existingVersion, "-ec.") || strings.Contains(existingVersion, "-rc.")

	if !existingIsDev && newIsDev {
		return true // Keep existing GA version over dev preview
	}
	if existingIsDev && !newIsDev {
		return false // Prefer new GA version over existing dev preview
	}

	// Both are same type (both GA or both dev), prefer higher version
	// Simple lexicographic comparison works for most cases
	return existingVersion >= newVersion
}

// profileYAML represents minimal profile structure for version extraction
type profileYAML struct {
	ClusterType       string `yaml:"clusterType"`
	OpenShiftVersions struct {
		Allowlist []string `yaml:"allowlist"`
		Default   string   `yaml:"default"`
	} `yaml:"openshiftVersions"`
}

// scanProfileReferences scans all profiles and builds a map of version -> major.minor
// Returns map[majorMinor][]versionReferences
func scanProfileReferences(profilesDir string) map[string][]string {
	references := make(map[string]map[string]bool) // majorMinor -> set of version references

	// Find all YAML files in profiles directory
	matches, err := filepath.Glob(filepath.Join(profilesDir, "*.yaml"))
	if err != nil {
		fmt.Printf("Warning: failed to glob profiles: %v\n", err)
		return convertReferencesToSlices(references)
	}

	for _, profilePath := range matches {
		// Read profile file
		data, err := ioutil.ReadFile(profilePath)
		if err != nil {
			fmt.Printf("Warning: failed to read profile %s: %v\n", profilePath, err)
			continue
		}

		// Parse YAML
		var profile profileYAML
		if err := yaml.Unmarshal(data, &profile); err != nil {
			fmt.Printf("Warning: failed to parse profile %s: %v\n", profilePath, err)
			continue
		}

		// Skip non-OpenShift profiles
		if profile.ClusterType != "openshift" {
			continue
		}

		// Extract versions from allowlist
		for _, version := range profile.OpenShiftVersions.Allowlist {
			if !strings.HasPrefix(version, "4.") {
				continue
			}
			majorMinor := extractMajorMinor(version)
			if references[majorMinor] == nil {
				references[majorMinor] = make(map[string]bool)
			}
			references[majorMinor][version] = true
		}

		// Extract default version
		if profile.OpenShiftVersions.Default != "" && strings.HasPrefix(profile.OpenShiftVersions.Default, "4.") {
			version := profile.OpenShiftVersions.Default
			majorMinor := extractMajorMinor(version)
			if references[majorMinor] == nil {
				references[majorMinor] = make(map[string]bool)
			}
			references[majorMinor][version] = true
		}
	}

	return convertReferencesToSlices(references)
}

// extractMajorMinor extracts major.minor from a version string
// e.g., "4.21.19" -> "4.21", "4.22.0-ec.5" -> "4.22"
func extractMajorMinor(version string) string {
	// Remove any -rc.X or -ec.X suffix
	version = strings.Split(version, "-")[0]

	// Split by dots and take first two parts
	parts := strings.Split(version, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return version
}

// convertReferencesToSlices converts map of sets to map of sorted slices
func convertReferencesToSlices(references map[string]map[string]bool) map[string][]string {
	result := make(map[string][]string)
	for majorMinor, versionSet := range references {
		versions := make([]string, 0, len(versionSet))
		for version := range versionSet {
			versions = append(versions, version)
		}
		// Sort versions
		sort.Slice(versions, func(i, j int) bool {
			return compareVersionStrings(versions[i], versions[j])
		})
		result[majorMinor] = versions
	}
	return result
}

// compareVersionStrings compares two version strings for sorting
func compareVersionStrings(v1, v2 string) bool {
	// Simple lexicographic comparison
	return v1 < v2
}
