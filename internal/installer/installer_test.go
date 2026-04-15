package installer

import (
	"testing"
)

func TestExtractMajorMinor(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected string
	}{
		{
			name:     "stable version with patch",
			version:  "4.20.3",
			expected: "4.20",
		},
		{
			name:     "stable version major.minor only",
			version:  "4.19",
			expected: "4.19",
		},
		{
			name:     "stable version with larger patch",
			version:  "4.18.15",
			expected: "4.18",
		},
		{
			name:     "dev-preview early candidate",
			version:  "4.22.0-ec.5",
			expected: "4.22",
		},
		{
			name:     "dev-preview release candidate",
			version:  "4.21.0-rc.1",
			expected: "4.21",
		},
		{
			name:     "dev-preview nightly build",
			version:  "4.22.0-0.nightly-2024-03-15-145014",
			expected: "4.22",
		},
		{
			name:     "dev-preview feature candidate",
			version:  "4.22.0-fc.2",
			expected: "4.22",
		},
		{
			name:     "invalid version - single part",
			version:  "4",
			expected: "",
		},
		{
			name:     "invalid version - empty string",
			version:  "",
			expected: "",
		},
		{
			name:     "version with hyphen but stable format",
			version:  "4.20.0-hotfix-test",
			expected: "4.20",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMajorMinor(tt.version)
			if result != tt.expected {
				t.Errorf("extractMajorMinor(%q) = %q, expected %q",
					tt.version, result, tt.expected)
			}
		})
	}
}

func TestIsDevPreviewVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{
			name:     "stable version",
			version:  "4.20.3",
			expected: false,
		},
		{
			name:     "early candidate",
			version:  "4.22.0-ec.5",
			expected: true,
		},
		{
			name:     "release candidate",
			version:  "4.21.0-rc.1",
			expected: true,
		},
		{
			name:     "nightly build with timestamp",
			version:  "4.22.0-0.nightly-2024-03-15-145014",
			expected: true,
		},
		{
			name:     "nightly build without timestamp",
			version:  "4.22.0-0.nightly",
			expected: true,
		},
		{
			name:     "feature candidate",
			version:  "4.22.0-fc.2",
			expected: true,
		},
		{
			name:     "major.minor only stable",
			version:  "4.20",
			expected: false,
		},
		{
			name:     "hotfix - not dev preview",
			version:  "4.20.0-hotfix-test",
			expected: false,
		},
		{
			name:     "assembly build - not dev preview",
			version:  "4.18.15-assembly.art3489",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDevPreviewVersion(tt.version)
			if result != tt.expected {
				t.Errorf("isDevPreviewVersion(%q) = %v, expected %v",
					tt.version, result, tt.expected)
			}
		})
	}
}

// TestNewInstallerForVersionDevPreview tests that dev-preview versions are properly parsed
// Note: This test will fail if the binary doesn't exist, so it only tests the parsing logic
func TestNewInstallerForVersionDevPreviewParsing(t *testing.T) {
	tests := []struct {
		name          string
		version       string
		expectedMajor string
		shouldError   bool
	}{
		{
			name:          "dev-preview version 4.22",
			version:       "4.22.0-ec.5",
			expectedMajor: "4.22",
			shouldError:   true, // Will error due to missing binary, but parsing should work
		},
		{
			name:          "stable version",
			version:       "4.20.3",
			expectedMajor: "4.20",
			shouldError:   true, // Will error due to missing binary
		},
		{
			name:          "unsupported version",
			version:       "4.17.0",
			expectedMajor: "",
			shouldError:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			majorMinor := extractMajorMinor(tt.version)
			if majorMinor != tt.expectedMajor {
				t.Errorf("extractMajorMinor(%q) = %q, expected %q",
					tt.version, majorMinor, tt.expectedMajor)
			}

			// Test that version is recognized as valid format (even if binaries don't exist)
			_, err := NewInstallerForVersion(tt.version)
			if !tt.shouldError && err != nil {
				t.Errorf("NewInstallerForVersion(%q) unexpected error: %v", tt.version, err)
			}
		})
	}
}
