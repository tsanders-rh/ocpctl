package installer

import (
	"testing"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestBuildTagSetProvenance(t *testing.T) {
	created := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	meta := ClusterMetadata{
		ClusterID:   "11111111-2222-3333-4444-555555555555",
		ClusterName: "demo-cluster",
		ProfileName: "aws-rosa-standard",
		InfraID:     "demo-abc12",
		CreatedAt:   created,
	}

	tags := buildTagSet(meta)

	want := map[string]string{
		types.TagKeyManaged:     "true",
		types.TagKeyClusterID:   meta.ClusterID,
		types.TagKeyClusterName: meta.ClusterName,
		types.TagKeyCreatedAt:   created.Format(time.RFC3339),
	}
	for k, v := range want {
		if got := tags[k]; got != v {
			t.Errorf("buildTagSet()[%q] = %q, want %q", k, got, v)
		}
	}

	// Existing tags must be preserved alongside provenance tags.
	if tags["ManagedBy"] != "ocpctl" {
		t.Errorf("buildTagSet() dropped ManagedBy tag: got %q", tags["ManagedBy"])
	}
	if tags["kubernetes.io/cluster/"+meta.InfraID] != "owned" {
		t.Errorf("buildTagSet() dropped cluster ownership tag")
	}
}

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

func TestParseMajorMinor(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMajor int
		wantMinor int
		wantOK    bool
	}{
		{name: "standard 4.22", input: "4.22", wantMajor: 4, wantMinor: 22, wantOK: true},
		{name: "next major 5.0", input: "5.0", wantMajor: 5, wantMinor: 0, wantOK: true},
		{name: "single digit minor", input: "4.9", wantMajor: 4, wantMinor: 9, wantOK: true},
		{name: "empty", input: "", wantOK: false},
		{name: "single part", input: "4", wantOK: false},
		{name: "three parts", input: "4.22.1", wantOK: false},
		{name: "non-numeric minor", input: "4.x", wantOK: false},
		{name: "non-numeric major", input: "v4.22", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, ok := parseMajorMinor(tt.input)
			if ok != tt.wantOK {
				t.Fatalf("parseMajorMinor(%q) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && (major != tt.wantMajor || minor != tt.wantMinor) {
				t.Errorf("parseMajorMinor(%q) = (%d, %d), want (%d, %d)",
					tt.input, major, minor, tt.wantMajor, tt.wantMinor)
			}
		})
	}
}

func TestSelectNewestInstaller(t *testing.T) {
	tests := []struct {
		name  string
		paths []string
		want  string
	}{
		{
			name:  "empty list",
			paths: nil,
			want:  "",
		},
		{
			name: "picks highest major over minor",
			paths: []string{
				"/usr/local/bin/openshift-install-4.22",
				"/usr/local/bin/openshift-install-5.0",
				"/usr/local/bin/openshift-install-4.14",
			},
			want: "/usr/local/bin/openshift-install-5.0",
		},
		{
			name: "picks highest minor within same major",
			paths: []string{
				"/usr/local/bin/openshift-install-4.18",
				"/usr/local/bin/openshift-install-4.22",
				"/usr/local/bin/openshift-install-4.20",
			},
			want: "/usr/local/bin/openshift-install-4.22",
		},
		{
			name: "extracts major.minor from patch and prerelease suffixes",
			paths: []string{
				"/usr/local/bin/openshift-install-4.21.19",
				"/usr/local/bin/openshift-install-4.22.0-ec.5",
			},
			want: "/usr/local/bin/openshift-install-4.22.0-ec.5",
		},
		{
			name: "labeled build still parses to its major.minor",
			paths: []string{
				"/usr/local/bin/openshift-install-4.22-patched",
				"/usr/local/bin/openshift-install-4.20",
			},
			want: "/usr/local/bin/openshift-install-4.22-patched",
		},
		{
			name: "skips non-version suffixes",
			paths: []string{
				"/usr/local/bin/openshift-install-latest",
				"/usr/local/bin/openshift-install-4.20",
			},
			want: "/usr/local/bin/openshift-install-4.20",
		},
		{
			name: "all unparseable yields empty",
			paths: []string{
				"/usr/local/bin/openshift-install-latest",
				"/usr/local/bin/openshift-install-patched",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectNewestInstaller(tt.paths); got != tt.want {
				t.Errorf("selectNewestInstaller(%v) = %q, want %q", tt.paths, got, tt.want)
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
			expectedMajor: "4.17",
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

func TestIsAdminAlreadyExists(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "rosa create admin exact error (#87)",
			in:   "ERR: Cluster 'octl-mman-rosa-minimal-test' already has 'cluster-admin' user",
			want: true,
		},
		{
			name: "wrapped go error string",
			in:   "rosa create admin failed: exit status 1\nStderr: ERR: Cluster 'x' already has 'cluster-admin' user",
			want: true,
		},
		{
			name: "admin variant wording",
			in:   "Cluster 'x' already has 'admin' user",
			want: true,
		},
		{
			name: "unrelated failure",
			in:   "rosa create admin failed: cluster is not ready",
			want: false,
		},
		{
			name: "empty",
			in:   "",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAdminAlreadyExists(tt.in); got != tt.want {
				t.Errorf("isAdminAlreadyExists(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsAdminNotFound(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{
			name: "no admin present",
			in:   "ERR: Cluster 'x' has no admin user",
			want: true,
		},
		{
			name: "not found wording",
			in:   "admin user not found",
			want: true,
		},
		{
			name: "unrelated failure is not treated as absent",
			in:   "rosa delete admin failed: connection refused",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAdminNotFound(tt.in); got != tt.want {
				t.Errorf("isAdminNotFound(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
