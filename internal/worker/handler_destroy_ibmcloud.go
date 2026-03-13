package worker

import (
	"context"
	"fmt"
	"log"

	"github.com/tsanders-rh/ocpctl/internal/ibmcloud"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// HandleIBMCloudDestroy handles IBM Cloud-specific cluster cleanup
// This should be called AFTER openshift-install destroy cluster completes
func (h *DestroyHandler) HandleIBMCloudDestroy(ctx context.Context, cluster *types.Cluster, inst *installer.Installer, workDir string) error {
	log.Printf("IBM Cloud cluster cleanup: cleaning up CCO service IDs for %s", cluster.Name)

	// Detect IBM Cloud credentials
	creds, err := ibmcloud.DetectCredentials()
	if err != nil {
		// Don't fail if credentials are missing - cluster might already be destroyed
		log.Printf("Warning: failed to detect IBM Cloud credentials for cleanup: %v", err)
		log.Printf("Skipping CCO service ID cleanup - manual cleanup may be required")
		return nil
	}

	log.Printf("Using IBM Cloud credentials from: %s", creds.Source)

	// Get resource group for cleanup
	resourceGroup := creds.ResourceGroup
	if resourceGroup == "" {
		// Try to get default resource group
		ibmClient, err := ibmcloud.NewClient(&ibmcloud.Config{
			APIKey:        creds.APIKey,
			Region:        creds.Region,
			ResourceGroup: "",
		})
		if err != nil {
			log.Printf("Warning: failed to create IBM Cloud client for cleanup: %v", err)
			return nil
		}

		rg, err := ibmcloud.GetDefaultResourceGroup(ctx, ibmClient)
		if err != nil {
			log.Printf("Warning: failed to get default resource group for cleanup: %v", err)
			return nil
		}
		resourceGroup = rg
	}

	log.Printf("Using resource group for cleanup: %s", resourceGroup)

	// Prepare CCO configuration for cleanup
	ccoConfig := &installer.IBMCloudCCOConfig{
		APIKey:        creds.APIKey,
		Region:        cluster.Region,
		ResourceGroup: resourceGroup,
		AccountID:     creds.AccountID,
	}

	// Run ccoctl to delete service IDs
	log.Printf("Running ccoctl to delete IBM Cloud service IDs...")
	if err := inst.CleanupIBMCloudServiceIDs(ctx, workDir, ccoConfig); err != nil {
		// Don't fail the entire destroy operation if CCO cleanup fails
		// Service IDs can be cleaned up manually if needed
		log.Printf("Warning: failed to clean up IBM Cloud service IDs: %v", err)
		log.Printf("Manual cleanup may be required using: ccoctl ibmcloud delete-service-id --name=%s", cluster.Name)
		return nil
	}

	log.Printf("✓ CCO service IDs cleaned up successfully")

	return nil
}
