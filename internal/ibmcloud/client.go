package ibmcloud

import (
	"context"
	"fmt"
	"os"

	"github.com/IBM/go-sdk-core/v5/core"
	"github.com/IBM/platform-services-go-sdk/iamidentityv1"
	"github.com/IBM/platform-services-go-sdk/resourcemanagerv2"
	"github.com/IBM/vpc-go-sdk/vpcv1"
)

// Client wraps IBM Cloud SDK clients for VPC and IAM operations
type Client struct {
	apiKey   string
	region   string
	vpcSvc   *vpcv1.VpcV1
	iamSvc   *iamidentityv1.IamIdentityV1
	rmSvc    *resourcemanagerv2.ResourceManagerV2
	authenticator *core.IamAuthenticator
}

// Config holds IBM Cloud client configuration
type Config struct {
	APIKey        string
	Region        string
	ResourceGroup string
}

// NewClient creates a new IBM Cloud client
func NewClient(cfg *Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("IBM Cloud API key is required")
	}

	if cfg.Region == "" {
		cfg.Region = "us-south" // Default region
	}

	// Create IAM authenticator
	authenticator := &core.IamAuthenticator{
		ApiKey: cfg.APIKey,
	}

	client := &Client{
		apiKey:        cfg.APIKey,
		region:        cfg.Region,
		authenticator: authenticator,
	}

	// Initialize VPC service
	if err := client.initVPCService(); err != nil {
		return nil, fmt.Errorf("initialize VPC service: %w", err)
	}

	// Initialize IAM service
	if err := client.initIAMService(); err != nil {
		return nil, fmt.Errorf("initialize IAM service: %w", err)
	}

	// Initialize Resource Manager service
	if err := client.initResourceManagerService(); err != nil {
		return nil, fmt.Errorf("initialize Resource Manager service: %w", err)
	}

	return client, nil
}

// initVPCService initializes the VPC service client
func (c *Client) initVPCService() error {
	vpcSvc, err := vpcv1.NewVpcV1(&vpcv1.VpcV1Options{
		Authenticator: c.authenticator,
		URL:           fmt.Sprintf("https://%s.iaas.cloud.ibm.com/v1", c.region),
	})
	if err != nil {
		return fmt.Errorf("create VPC service: %w", err)
	}

	c.vpcSvc = vpcSvc
	return nil
}

// initIAMService initializes the IAM Identity service client
func (c *Client) initIAMService() error {
	iamSvc, err := iamidentityv1.NewIamIdentityV1(&iamidentityv1.IamIdentityV1Options{
		Authenticator: c.authenticator,
	})
	if err != nil {
		return fmt.Errorf("create IAM service: %w", err)
	}

	c.iamSvc = iamSvc
	return nil
}

// initResourceManagerService initializes the Resource Manager service client
func (c *Client) initResourceManagerService() error {
	rmSvc, err := resourcemanagerv2.NewResourceManagerV2(&resourcemanagerv2.ResourceManagerV2Options{
		Authenticator: c.authenticator,
	})
	if err != nil {
		return fmt.Errorf("create Resource Manager service: %w", err)
	}

	c.rmSvc = rmSvc
	return nil
}

// ValidateCredentials validates the IBM Cloud credentials by making a test API call
func (c *Client) ValidateCredentials(ctx context.Context) error {
	// Try to list VPCs as a credential validation test
	listOptions := &vpcv1.ListVpcsOptions{}
	_, _, err := c.vpcSvc.ListVpcsWithContext(ctx, listOptions)
	if err != nil {
		return fmt.Errorf("credential validation failed: %w", err)
	}

	return nil
}

// GetResourceGroup retrieves a resource group by name
func (c *Client) GetResourceGroup(ctx context.Context, name string) (*resourcemanagerv2.ResourceGroup, error) {
	listOptions := &resourcemanagerv2.ListResourceGroupsOptions{
		Name: &name,
	}

	result, _, err := c.rmSvc.ListResourceGroupsWithContext(ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("list resource groups: %w", err)
	}

	if len(result.Resources) == 0 {
		return nil, fmt.Errorf("resource group %q not found", name)
	}

	return &result.Resources[0], nil
}

// GetVPC retrieves a VPC by name
func (c *Client) GetVPC(ctx context.Context, name string) (*vpcv1.VPC, error) {
	listOptions := &vpcv1.ListVpcsOptions{}

	result, _, err := c.vpcSvc.ListVpcsWithContext(ctx, listOptions)
	if err != nil {
		return nil, fmt.Errorf("list VPCs: %w", err)
	}

	for _, vpc := range result.Vpcs {
		if *vpc.Name == name {
			return &vpc, nil
		}
	}

	return nil, fmt.Errorf("VPC %q not found", name)
}

// CreateServiceID creates a service ID for cluster operators
func (c *Client) CreateServiceID(ctx context.Context, name, description string) (*iamidentityv1.ServiceID, error) {
	// Get account ID from authenticator
	accountID := c.getAccountID()
	if accountID == "" {
		return nil, fmt.Errorf("could not determine account ID")
	}

	createOptions := &iamidentityv1.CreateServiceIDOptions{
		AccountID:   &accountID,
		Name:        &name,
		Description: &description,
	}

	serviceID, _, err := c.iamSvc.CreateServiceIDWithContext(ctx, createOptions)
	if err != nil {
		return nil, fmt.Errorf("create service ID: %w", err)
	}

	return serviceID, nil
}

// DeleteServiceID deletes a service ID by its ID
func (c *Client) DeleteServiceID(ctx context.Context, serviceIDID string) error {
	deleteOptions := &iamidentityv1.DeleteServiceIDOptions{
		ID: &serviceIDID,
	}

	_, err := c.iamSvc.DeleteServiceIDWithContext(ctx, deleteOptions)
	if err != nil {
		return fmt.Errorf("delete service ID: %w", err)
	}

	return nil
}

// CreateAPIKey creates an API key for a service ID
func (c *Client) CreateAPIKey(ctx context.Context, name, serviceIDID, description string) (*iamidentityv1.APIKey, error) {
	createOptions := &iamidentityv1.CreateAPIKeyOptions{
		Name:        &name,
		IamID:       &serviceIDID,
		Description: &description,
	}

	apiKey, _, err := c.iamSvc.CreateAPIKeyWithContext(ctx, createOptions)
	if err != nil {
		return nil, fmt.Errorf("create API key: %w", err)
	}

	return apiKey, nil
}

// DeleteAPIKey deletes an API key by its ID
func (c *Client) DeleteAPIKey(ctx context.Context, apiKeyID string) error {
	deleteOptions := &iamidentityv1.DeleteAPIKeyOptions{
		ID: &apiKeyID,
	}

	_, err := c.iamSvc.DeleteAPIKeyWithContext(ctx, deleteOptions)
	if err != nil {
		return fmt.Errorf("delete API key: %w", err)
	}

	return nil
}

// getAccountID extracts the account ID from the API key token
func (c *Client) getAccountID() string {
	// Get account ID from environment variable if set
	accountID := os.Getenv("IC_ACCOUNT_ID")
	if accountID != "" {
		return accountID
	}

	// TODO: Parse account ID from API key token
	// For now, require IC_ACCOUNT_ID to be set
	return ""
}

// Region returns the configured region
func (c *Client) Region() string {
	return c.region
}

// VPCService returns the VPC service client
func (c *Client) VPCService() *vpcv1.VpcV1 {
	return c.vpcSvc
}

// IAMService returns the IAM service client
func (c *Client) IAMService() *iamidentityv1.IamIdentityV1 {
	return c.iamSvc
}

// ResourceManagerService returns the Resource Manager service client
func (c *Client) ResourceManagerService() *resourcemanagerv2.ResourceManagerV2 {
	return c.rmSvc
}
