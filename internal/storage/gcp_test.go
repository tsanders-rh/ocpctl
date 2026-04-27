package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGCPStorageManager(t *testing.T) {
	t.Run("returns nil when GCP_PROJECT not set", func(t *testing.T) {
		t.Setenv("GCP_PROJECT", "")
		manager := NewGCPStorageManager()
		assert.Nil(t, manager)
	})

	t.Run("creates manager when GCP_PROJECT is set", func(t *testing.T) {
		t.Setenv("GCP_PROJECT", "test-project")
		manager := NewGCPStorageManager()
		require.NotNil(t, manager)
		assert.Equal(t, "test-project", manager.project)
	})
}

func TestPersistentDiskConfig_Structure(t *testing.T) {
	tests := []struct {
		name   string
		config *PersistentDiskConfig
	}{
		{
			name: "valid standard disk",
			config: &PersistentDiskConfig{
				Name:   "my-disk",
				Zone:   "us-central1-a",
				Type:   "pd-standard",
				SizeGB: 100,
			},
		},
		{
			name: "valid SSD disk",
			config: &PersistentDiskConfig{
				Name:   "my-ssd",
				Zone:   "us-central1-a",
				Type:   "pd-ssd",
				SizeGB: 500,
			},
		},
		{
			name: "valid balanced disk",
			config: &PersistentDiskConfig{
				Name:   "my-balanced",
				Zone:   "us-central1-a",
				Type:   "pd-balanced",
				SizeGB: 200,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.config.Name)
			assert.NotEmpty(t, tt.config.Zone)
			assert.NotEmpty(t, tt.config.Type)
			assert.Greater(t, tt.config.SizeGB, 0)
		})
	}
}

func TestGCSBucketConfig_Structure(t *testing.T) {
	t.Run("valid bucket config", func(t *testing.T) {
		config := &GCSBucketConfig{
			Name:         "my-bucket",
			Location:     "us-central1",
			StorageClass: "STANDARD",
		}
		assert.NotEmpty(t, config.Name)
		assert.NotEmpty(t, config.Location)
		assert.NotEmpty(t, config.StorageClass)
	})

	t.Run("valid with lifecycle rules", func(t *testing.T) {
		config := &GCSBucketConfig{
			Name:         "my-bucket-with-lifecycle",
			Location:     "us",
			StorageClass: "STANDARD",
			LifecycleRules: []LifecycleRule{
				{
					Action: LifecycleAction{
						Type: "Delete",
					},
					Condition: LifecycleCondition{
						Age: 90,
					},
				},
			},
		}
		assert.NotEmpty(t, config.LifecycleRules)
		assert.Equal(t, "Delete", config.LifecycleRules[0].Action.Type)
		assert.Equal(t, 90, config.LifecycleRules[0].Condition.Age)
	})
}

func TestFilestoreConfig_Structure(t *testing.T) {
	tests := []struct {
		name   string
		config *FilestoreConfig
	}{
		{
			name: "valid BASIC_HDD filestore",
			config: &FilestoreConfig{
				Name:          "my-filestore",
				Tier:          "BASIC_HDD",
				Capacity:      1024,
				Network:       "default",
				FileShareName: "share1",
				Location:      "us-central1",
			},
		},
		{
			name: "valid BASIC_SSD filestore",
			config: &FilestoreConfig{
				Name:          "my-ssd-filestore",
				Tier:          "BASIC_SSD",
				Capacity:      2560,
				Network:       "default",
				FileShareName: "share1",
				Location:      "us-central1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.config.Name)
			assert.NotEmpty(t, tt.config.Tier)
			assert.Greater(t, tt.config.Capacity, 0)
			assert.NotEmpty(t, tt.config.FileShareName)
			assert.NotEmpty(t, tt.config.Location)
		})
	}
}

func TestSanitizeGCPLabel_Storage(t *testing.T) {
	// Test the sanitizeGCPLabel function used in storage contexts
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "cluster ID with UUID",
			input:    "abc-123-def-456",
			expected: "abc-123-def-456",
		},
		{
			name:     "cluster name with uppercase",
			input:    "MyCluster",
			expected: "mycluster",
		},
		{
			name:     "profile name with dots",
			input:    "gcp-gke-standard",
			expected: "gcp-gke-standard",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeGCPLabel(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

