package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tsanders-rh/ocpctl/internal/ibmcloud"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// HandleIBMCloudCreate handles IBM Cloud-specific cluster creation
func (h *CreateHandler) HandleIBMCloudCreate(ctx context.Context, job *types.Job, inst *installer.Installer, workDir string) error {
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("IBM Cloud cluster creation: starting CCO workflow for %s", cluster.Name)

	// Step 1: Create manifests (required to get infraID for CCO)
	log.Printf("Creating manifests for IBM Cloud cluster...")
	if err := inst.CreateManifests(ctx, workDir); err != nil {
		return fmt.Errorf("create manifests: %w", err)
	}
	log.Printf("✓ Manifests created successfully")

	// Detect IBM Cloud credentials
	creds, err := ibmcloud.DetectCredentials()
	if err != nil {
		return fmt.Errorf("detect IBM Cloud credentials: %w", err)
	}

	log.Printf("Using IBM Cloud credentials from: %s", creds.Source)
	log.Printf("Region: %s, Resource Group: %s", creds.Region, creds.ResourceGroup)

	// Create IBM Cloud client
	ibmClient, err := ibmcloud.NewClient(&ibmcloud.Config{
		APIKey:        creds.APIKey,
		Region:        creds.Region,
		ResourceGroup: creds.ResourceGroup,
	})
	if err != nil {
		return fmt.Errorf("create IBM Cloud client: %w", err)
	}

	// Validate credentials
	if err := ibmClient.ValidateCredentials(ctx); err != nil {
		return fmt.Errorf("validate IBM Cloud credentials: %w", err)
	}

	// Validate IAM permissions
	if err := ibmcloud.ValidateIAMPermissions(ctx, ibmClient); err != nil {
		return fmt.Errorf("validate IAM permissions: %w", err)
	}

	log.Printf("✓ IBM Cloud credentials validated")

	// Get profile to extract resource group
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile %s: %w", cluster.Profile, err)
	}

	// Use resource group from profile
	resourceGroup := "Default" // fallback default
	if prof.PlatformConfig.IBMCloud != nil && prof.PlatformConfig.IBMCloud.ResourceGroup != "" {
		resourceGroup = prof.PlatformConfig.IBMCloud.ResourceGroup
	}

	log.Printf("Using resource group: %s", resourceGroup)

	// Prepare CCO configuration
	ccoConfig := &installer.IBMCloudCCOConfig{
		APIKey:        creds.APIKey,
		Region:        cluster.Region, // Use cluster's region
		ResourceGroup: resourceGroup,
		AccountID:     creds.AccountID,
	}

	// Validate CCO prerequisites
	if err := installer.ValidateIBMCloudCCOPrerequisites(inst.CCOCtlPath(), ccoConfig); err != nil {
		return fmt.Errorf("validate CCO prerequisites: %w", err)
	}

	// Run ccoctl to create service IDs and API keys
	log.Printf("Running ccoctl to create IBM Cloud service IDs...")
	if err := inst.RunCCOCtlIBMCloud(ctx, workDir, ccoConfig); err != nil {
		return fmt.Errorf("run ccoctl for IBM Cloud: %w", err)
	}

	log.Printf("✓ CCO workflow completed - service IDs and manifests created")

	// Set environment variables for openshift-installer
	os.Setenv("IC_API_KEY", creds.APIKey)
	if creds.AccountID != "" {
		os.Setenv("IC_ACCOUNT_ID", creds.AccountID)
	}

	log.Printf("IBM Cloud environment configured for openshift-installer")

	return nil
}

// StoreIBMCloudMetadata stores IBM Cloud-specific metadata after cluster creation
func (h *CreateHandler) StoreIBMCloudMetadata(ctx context.Context, workDir string, clusterID string) error {
	// Read metadata.json to extract IBM Cloud-specific info
	metadataPath := filepath.Join(workDir, "metadata.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("read metadata.json: %w", err)
	}

	// TODO: Parse metadata.json and extract IBM Cloud VPC ID, resource group, etc.
	// For now, just log that we would store it
	log.Printf("IBM Cloud metadata available: %d bytes", len(data))
	log.Printf("TODO: Store IBM Cloud VPC ID and resource group in database")

	// Store service ID metadata for cleanup
	serviceIDPath := filepath.Join(workDir, "ibmcloud-service-ids.json")
	if _, err := os.Stat(serviceIDPath); err == nil {
		log.Printf("Service ID metadata stored at: %s", serviceIDPath)
	}

	return nil
}
