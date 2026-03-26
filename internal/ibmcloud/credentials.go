package ibmcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// CredentialSource represents where credentials are sourced from
type CredentialSource string

const (
	// CredentialSourceAPIKey indicates credentials from IC_API_KEY environment variable
	CredentialSourceAPIKey CredentialSource = "api_key"
	// CredentialSourceInstanceMetadata indicates credentials from instance metadata service
	CredentialSourceInstanceMetadata CredentialSource = "instance_metadata"
)

// Credentials holds IBM Cloud authentication credentials
type Credentials struct {
	APIKey        string
	AccountID     string
	Region        string
	ResourceGroup string
	Source        CredentialSource
}

// DetectCredentials attempts to detect IBM Cloud credentials from environment or instance metadata
func DetectCredentials() (*Credentials, error) {
	// Try environment variables first (explicit configuration)
	if apiKey := os.Getenv("IC_API_KEY"); apiKey != "" {
		log.Printf("Using IBM Cloud credentials from environment variables")
		return &Credentials{
			APIKey:        apiKey,
			AccountID:     os.Getenv("IC_ACCOUNT_ID"),
			Region:        getRegionFromEnv(),
			ResourceGroup: os.Getenv("IC_RESOURCE_GROUP"),
			Source:        CredentialSourceAPIKey,
		}, nil
	}

	// Try instance metadata service (when running on IBM Cloud VPC instance)
	if creds, err := getCredentialsFromInstanceMetadata(); err == nil {
		log.Printf("Using IBM Cloud credentials from instance metadata service")
		return creds, nil
	}

	return nil, fmt.Errorf("no IBM Cloud credentials found: set IC_API_KEY environment variable")
}

// getRegionFromEnv gets the region from environment with fallback to default
func getRegionFromEnv() string {
	region := os.Getenv("IC_REGION")
	if region == "" {
		region = "us-south" // Default region
	}
	return region
}

// getCredentialsFromInstanceMetadata attempts to retrieve credentials from IBM Cloud instance metadata service
func getCredentialsFromInstanceMetadata() (*Credentials, error) {
	// IBM Cloud VPC instances have a metadata service similar to AWS IMDS
	// Endpoint: http://169.254.169.254/metadata/v1/
	const metadataBaseURL = "http://169.254.169.254/metadata/v1"

	// Create HTTP client with short timeout for metadata service
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	// 1. Check if metadata service is accessible and get instance identity token
	tokenURL := metadataBaseURL + "/instance_identity/token"
	req, err := http.NewRequest("PUT", tokenURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create metadata request: %w", err)
	}

	// IBM Cloud metadata service requires this header
	req.Header.Set("Metadata-Flavor", "ibm")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metadata service not accessible: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata service returned status %d", resp.StatusCode)
	}

	// Read instance identity token
	tokenBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read instance identity token: %w", err)
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.Unmarshal(tokenBytes, &tokenResponse); err != nil {
		return nil, fmt.Errorf("parse instance identity token: %w", err)
	}

	if tokenResponse.AccessToken == "" {
		return nil, fmt.Errorf("empty access token from metadata service")
	}

	// 2. Get instance metadata to retrieve account ID and region
	instanceURL := metadataBaseURL + "/instance"
	req, err = http.NewRequest("GET", instanceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create instance metadata request: %w", err)
	}
	req.Header.Set("Metadata-Flavor", "ibm")

	resp, err = client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get instance metadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("instance metadata returned status %d", resp.StatusCode)
	}

	instanceBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read instance metadata: %w", err)
	}

	var instanceMetadata struct {
		VPC struct {
			ID     string `json:"id"`
			CRN    string `json:"crn"`
			Region string `json:"region"`
		} `json:"vpc"`
		Zone      string `json:"zone"`
		AccountID string `json:"account_id"`
	}

	if err := json.Unmarshal(instanceBytes, &instanceMetadata); err != nil {
		return nil, fmt.Errorf("parse instance metadata: %w", err)
	}

	// Extract region from zone (e.g., "us-south-1" -> "us-south")
	region := instanceMetadata.VPC.Region
	if region == "" && instanceMetadata.Zone != "" {
		// Fallback: extract region from zone name
		region = extractRegionFromZone(instanceMetadata.Zone)
	}

	// 3. The access token from metadata service can be used directly for IBM Cloud API calls
	// Store it as the "API key" - IBM Cloud SDK accepts IAM tokens in place of API keys
	return &Credentials{
		APIKey:        tokenResponse.AccessToken,
		AccountID:     instanceMetadata.AccountID,
		Region:        region,
		ResourceGroup: "", // Will be retrieved from environment or defaulted
		Source:        CredentialSourceInstanceMetadata,
	}, nil
}

// extractRegionFromZone extracts region from zone name (e.g., "us-south-1" -> "us-south")
func extractRegionFromZone(zone string) string {
	// Zone format is typically "<region>-<number>"
	if len(zone) < 3 {
		return ""
	}

	// Find last dash and strip the zone number
	for i := len(zone) - 1; i >= 0; i-- {
		if zone[i] == '-' {
			return zone[:i]
		}
	}

	return zone
}

// ValidateIAMPermissions validates that the credentials have required IAM permissions
func ValidateIAMPermissions(ctx context.Context, client *Client) error {
	// Required permissions for OpenShift cluster deployment:
	requiredPermissions := []string{
		"VPC Infrastructure Services: Administrator",
		"Cloud Object Storage: Manager",
		"IAM Identity Service: Operator",
		"Resource Group: Viewer",
	}

	log.Printf("Validating IAM permissions for IBM Cloud credentials")
	log.Printf("Required permissions: %v", requiredPermissions)

	// Test VPC access (Administrator on VPC Infrastructure Services)
	if err := testVPCAccess(ctx, client); err != nil {
		return fmt.Errorf("insufficient VPC permissions: %w", err)
	}

	// Test IAM access (Operator on IAM Identity Service)
	if err := testIAMAccess(ctx, client); err != nil {
		return fmt.Errorf("insufficient IAM permissions: %w", err)
	}

	// Test Resource Manager access (Viewer on Resource Group)
	if err := testResourceManagerAccess(ctx, client); err != nil {
		return fmt.Errorf("insufficient Resource Manager permissions: %w", err)
	}

	log.Printf("IAM permissions validated successfully")
	return nil
}

// testVPCAccess tests if credentials have VPC access
func testVPCAccess(ctx context.Context, client *Client) error {
	// Try to list VPCs
	listOptions := client.VPCService().NewListVpcsOptions()
	_, _, err := client.VPCService().ListVpcs(listOptions)
	if err != nil {
		return fmt.Errorf("cannot list VPCs: %w", err)
	}
	return nil
}

// testIAMAccess tests if credentials have IAM access
func testIAMAccess(ctx context.Context, client *Client) error {
	// Try to list service IDs (requires IAM Operator permissions)
	accountID := os.Getenv("IC_ACCOUNT_ID")
	if accountID == "" {
		return fmt.Errorf("IC_ACCOUNT_ID environment variable required for IAM validation")
	}

	listOptions := client.IAMService().NewListServiceIdsOptions()
	listOptions.SetAccountID(accountID)
	listOptions.SetPagesize(1) // Just need to verify we can call this

	_, _, err := client.IAMService().ListServiceIds(listOptions)
	if err != nil {
		return fmt.Errorf("cannot list service IDs: %w", err)
	}
	return nil
}

// testResourceManagerAccess tests if credentials have Resource Manager access
func testResourceManagerAccess(ctx context.Context, client *Client) error {
	// Try to list resource groups
	listOptions := client.ResourceManagerService().NewListResourceGroupsOptions()
	_, _, err := client.ResourceManagerService().ListResourceGroups(listOptions)
	if err != nil {
		return fmt.Errorf("cannot list resource groups: %w", err)
	}
	return nil
}

// GetDefaultResourceGroup gets the default resource group or the one specified in environment
func GetDefaultResourceGroup(ctx context.Context, client *Client) (string, error) {
	// Check if resource group is specified in environment
	if rg := os.Getenv("IC_RESOURCE_GROUP"); rg != "" {
		// Validate it exists
		_, err := client.GetResourceGroup(ctx, rg)
		if err != nil {
			return "", fmt.Errorf("resource group %q not found: %w", rg, err)
		}
		return rg, nil
	}

	// Use "default" resource group if not specified
	return "default", nil
}

// ValidateRegion validates that the specified region is valid
func ValidateRegion(region string) error {
	validRegions := map[string]bool{
		"us-south":   true,
		"us-east":    true,
		"eu-de":      true,
		"eu-gb":      true,
		"jp-tok":     true,
		"jp-osa":     true,
		"au-syd":     true,
		"ca-tor":     true,
		"br-sao":     true,
	}

	if !validRegions[region] {
		return fmt.Errorf("invalid region %q: must be one of us-south, us-east, eu-de, eu-gb, jp-tok, jp-osa, au-syd, ca-tor, br-sao", region)
	}

	return nil
}
