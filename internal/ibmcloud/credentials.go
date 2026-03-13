package ibmcloud

import (
	"context"
	"fmt"
	"log"
	"os"
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

	// TODO: Implement instance metadata service credential retrieval
	// This requires:
	// 1. Check if metadata service is accessible
	// 2. Retrieve instance identity token
	// 3. Exchange for IAM token
	// 4. Use IAM token for API authentication

	return nil, fmt.Errorf("instance metadata service not implemented yet")
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
	_, err := client.VPCService().ListVpcs(&client.VPCService().NewListVpcsOptions())
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
