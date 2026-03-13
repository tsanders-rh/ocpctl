package ibmcloud

import (
	"os"
	"testing"
)

func TestDetectCredentials(t *testing.T) {
	// Save original environment
	originalAPIKey := os.Getenv("IC_API_KEY")
	originalAccountID := os.Getenv("IC_ACCOUNT_ID")
	originalRegion := os.Getenv("IC_REGION")
	originalResourceGroup := os.Getenv("IC_RESOURCE_GROUP")

	defer func() {
		// Restore original environment
		os.Setenv("IC_API_KEY", originalAPIKey)
		os.Setenv("IC_ACCOUNT_ID", originalAccountID)
		os.Setenv("IC_REGION", originalRegion)
		os.Setenv("IC_RESOURCE_GROUP", originalResourceGroup)
	}()

	tests := []struct {
		name          string
		apiKey        string
		accountID     string
		region        string
		resourceGroup string
		wantErr       bool
		wantSource    CredentialSource
	}{
		{
			name:          "valid environment variables",
			apiKey:        "test-api-key-123",
			accountID:     "test-account-id",
			region:        "us-south",
			resourceGroup: "default",
			wantErr:       false,
			wantSource:    CredentialSourceAPIKey,
		},
		{
			name:          "minimal environment variables",
			apiKey:        "test-api-key-456",
			accountID:     "",
			region:        "",
			resourceGroup: "",
			wantErr:       false,
			wantSource:    CredentialSourceAPIKey,
		},
		{
			name:    "missing API key",
			apiKey:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment
			if tt.apiKey != "" {
				os.Setenv("IC_API_KEY", tt.apiKey)
			} else {
				os.Unsetenv("IC_API_KEY")
			}
			os.Setenv("IC_ACCOUNT_ID", tt.accountID)
			os.Setenv("IC_REGION", tt.region)
			os.Setenv("IC_RESOURCE_GROUP", tt.resourceGroup)

			// Test credential detection
			creds, err := DetectCredentials()

			if (err != nil) != tt.wantErr {
				t.Errorf("DetectCredentials() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if creds.APIKey != tt.apiKey {
					t.Errorf("DetectCredentials() APIKey = %v, want %v", creds.APIKey, tt.apiKey)
				}
				if creds.Source != tt.wantSource {
					t.Errorf("DetectCredentials() Source = %v, want %v", creds.Source, tt.wantSource)
				}
				if tt.accountID != "" && creds.AccountID != tt.accountID {
					t.Errorf("DetectCredentials() AccountID = %v, want %v", creds.AccountID, tt.accountID)
				}
			}
		})
	}
}

func TestValidateRegion(t *testing.T) {
	tests := []struct {
		name    string
		region  string
		wantErr bool
	}{
		{
			name:    "valid region us-south",
			region:  "us-south",
			wantErr: false,
		},
		{
			name:    "valid region eu-de",
			region:  "eu-de",
			wantErr: false,
		},
		{
			name:    "valid region jp-tok",
			region:  "jp-tok",
			wantErr: false,
		},
		{
			name:    "invalid region",
			region:  "invalid-region",
			wantErr: true,
		},
		{
			name:    "empty region",
			region:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRegion(tt.region)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRegion() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetRegionFromEnv(t *testing.T) {
	// Save original environment
	originalRegion := os.Getenv("IC_REGION")
	defer func() {
		os.Setenv("IC_REGION", originalRegion)
	}()

	tests := []struct {
		name       string
		envValue   string
		wantRegion string
	}{
		{
			name:       "region from environment",
			envValue:   "us-south",
			wantRegion: "us-south",
		},
		{
			name:       "no region set - defaults to us-south",
			envValue:   "",
			wantRegion: "us-south",
		},
		{
			name:       "invalid region",
			envValue:   "invalid",
			wantRegion: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				os.Setenv("IC_REGION", tt.envValue)
			} else {
				os.Unsetenv("IC_REGION")
			}

			got := getRegionFromEnv()
			if got != tt.wantRegion {
				t.Errorf("getRegionFromEnv() = %v, want %v", got, tt.wantRegion)
			}
		})
	}
}
