package worker

import (
	"context"
	"encoding/json"
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

	// Step 1: Preflight - Validate base domain is specified
	log.Printf("Running preflight checks for IBM Cloud OpenShift IPI...")
	baseDomain := ""
	if cluster.BaseDomain != nil {
		baseDomain = *cluster.BaseDomain
	}
	if baseDomain == "" {
		return fmt.Errorf("base domain is required for IBM Cloud OpenShift IPI")
	}
	log.Printf("✓ Base domain specified: %s", baseDomain)

	// Note: CIS DNS zone validation skipped - requires CLI login which conflicts with
	// credential detection from environment variables. The openshift-install tool will
	// validate DNS zone access during actual installation.

	// Step 2: Create manifests (required to get infraID for CCO)
	log.Printf("Creating manifests for IBM Cloud cluster...")
	if err := inst.CreateManifests(ctx, workDir); err != nil {
		return fmt.Errorf("create manifests: %w", err)
	}
	log.Printf("✓ Manifests created successfully")

	// Step 2.5: Extract VPC ID from terraform state (manifests create the VPC)
	vpcID, err := h.extractVPCIDFromState(workDir)
	if err != nil {
		log.Printf("Warning: could not extract VPC ID from state: %v", err)
		log.Printf("Ingress security group rules will not be added automatically")
	}

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

	// Step 3: Add ingress security group rules to VPC default security group
	// This fixes the issue where ingress load balancers use the VPC default security group
	// which blocks all external traffic by default
	if vpcID != "" {
		log.Printf("Adding ingress security group rules to VPC default security group...")
		if err := ibmClient.AddIngressSecurityGroupRules(ctx, vpcID); err != nil {
			log.Printf("Warning: failed to add ingress security group rules: %v", err)
			log.Printf("Manual fix may be required: add inbound TCP 80, 443, 1936 rules to VPC default security group")
		} else {
			log.Printf("✓ Ingress security group rules added successfully")
		}
	}

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

// extractVPCIDFromState extracts the VPC ID from the terraform network state
func (h *CreateHandler) extractVPCIDFromState(workDir string) (string, error) {
	// The VPC ID is in terraform.network.tfstate after manifests are created
	statePath := filepath.Join(workDir, "terraform.network.tfstate")

	data, err := os.ReadFile(statePath)
	if err != nil {
		return "", fmt.Errorf("read terraform state: %w", err)
	}

	// Parse the terraform state JSON
	var state map[string]interface{}
	if err := json.Unmarshal(data, &state); err != nil {
		return "", fmt.Errorf("parse terraform state: %w", err)
	}

	// Extract VPC ID from resources
	resources, ok := state["resources"].([]interface{})
	if !ok {
		return "", fmt.Errorf("terraform state has no resources")
	}

	for _, res := range resources {
		resource, ok := res.(map[string]interface{})
		if !ok {
			continue
		}

		// Look for the VPC resource
		if resource["type"] == "ibm_is_vpc" && resource["name"] == "vpc" {
			instances, ok := resource["instances"].([]interface{})
			if !ok || len(instances) == 0 {
				continue
			}

			instance, ok := instances[0].(map[string]interface{})
			if !ok {
				continue
			}

			attributes, ok := instance["attributes"].(map[string]interface{})
			if !ok {
				continue
			}

			vpcID, ok := attributes["id"].(string)
			if !ok {
				continue
			}

			log.Printf("Extracted VPC ID from terraform state: %s", vpcID)
			return vpcID, nil
		}
	}

	return "", fmt.Errorf("VPC resource not found in terraform state")
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
