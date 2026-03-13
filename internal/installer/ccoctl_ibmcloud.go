package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IBMCloudCCOConfig holds IBM Cloud-specific CCO configuration
type IBMCloudCCOConfig struct {
	APIKey         string
	Region         string
	ResourceGroup  string
	AccountID      string
}

// RunCCOCtlIBMCloud runs ccoctl for IBM Cloud to create service IDs and API keys
func (i *Installer) RunCCOCtlIBMCloud(ctx context.Context, workDir string, config *IBMCloudCCOConfig) error {
	log.Printf("Running ccoctl for IBM Cloud...")

	// Get infraID from metadata
	infraID, err := i.getInfraID(workDir)
	if err != nil {
		return fmt.Errorf("get infraID: %w", err)
	}
	log.Printf("Found infraID for ccoctl: %s", infraID)

	// Create temporary directory for CredentialsRequests
	credsReqDir := filepath.Join(workDir, "credentialsrequests")
	if err := os.MkdirAll(credsReqDir, 0755); err != nil {
		return fmt.Errorf("create credentials requests dir: %w", err)
	}

	// Extract CredentialsRequests from release image
	log.Printf("Extracting CredentialsRequests from release image...")
	if err := i.extractCredentialRequestsIBMCloud(ctx, credsReqDir); err != nil {
		return fmt.Errorf("extract credentials requests: %w", err)
	}

	// Create output directory for ccoctl
	ccoOutputDir := filepath.Join(workDir, "cco-output")
	if err := os.MkdirAll(ccoOutputDir, 0755); err != nil {
		return fmt.Errorf("create cco output dir: %w", err)
	}
	defer os.RemoveAll(ccoOutputDir)

	// Run ccoctl to create service IDs and API keys
	log.Printf("Running ccoctl ibmcloud create-service-id for cluster %s in resource group %s...", infraID, config.ResourceGroup)
	if err := i.executeCCOCtlIBMCloud(ctx, infraID, config, credsReqDir, ccoOutputDir); err != nil {
		return fmt.Errorf("execute ccoctl: %w", err)
	}

	// Copy generated manifests back into install directory
	log.Printf("Copying generated manifests to install directory...")
	if err := i.copyManifests(ccoOutputDir, workDir); err != nil {
		return fmt.Errorf("copy manifests: %w", err)
	}

	// Store service ID metadata for cleanup
	if err := i.storeIBMCloudServiceIDs(ccoOutputDir, workDir); err != nil {
		log.Printf("Warning: failed to store service ID metadata: %v", err)
	}

	log.Printf("✓ ccoctl completed successfully for IBM Cloud")
	return nil
}

// executeCCOCtlIBMCloud runs the ccoctl binary for IBM Cloud
func (i *Installer) executeCCOCtlIBMCloud(ctx context.Context, clusterName string, config *IBMCloudCCOConfig, credsReqDir, outputDir string) error {
	// ccoctl ibmcloud create-service-id \
	//   --credentials-requests-dir=<dir> \
	//   --name=<cluster-name> \
	//   --output-dir=<dir> \
	//   --region=<region> \
	//   --resource-group-name=<rg>

	args := []string{
		"ibmcloud", "create-service-id",
		"--credentials-requests-dir=" + credsReqDir,
		"--name=" + clusterName,
		"--output-dir=" + outputDir,
	}

	// Add resource group if specified
	if config.ResourceGroup != "" {
		args = append(args, "--resource-group-name="+config.ResourceGroup)
	}

	cmd := exec.CommandContext(ctx, i.ccoCtlPath, args...)

	// Set environment variables for IBM Cloud authentication
	cmd.Env = append(os.Environ(),
		"IC_API_KEY="+config.APIKey,
	)

	if config.AccountID != "" {
		cmd.Env = append(cmd.Env, "IC_ACCOUNT_ID="+config.AccountID)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("Executing: %s", cmd.String())
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("ccoctl failed: %w\nStdout: %s\nStderr: %s", err, stdout.String(), stderr.String())
	}

	log.Printf("ccoctl output:\n%s", stdout.String())
	return nil
}

// storeIBMCloudServiceIDs stores service ID metadata for cleanup
func (i *Installer) storeIBMCloudServiceIDs(ccoOutputDir, workDir string) error {
	// ccoctl generates a service-id.json file with metadata
	serviceIDFile := filepath.Join(ccoOutputDir, "service-id.json")

	// Check if file exists
	if _, err := os.Stat(serviceIDFile); os.IsNotExist(err) {
		// Try alternate location
		serviceIDFile = filepath.Join(ccoOutputDir, "ibmcloud", "service-id.json")
		if _, err := os.Stat(serviceIDFile); os.IsNotExist(err) {
			return fmt.Errorf("service-id.json not found in ccoctl output")
		}
	}

	// Copy to work directory for later cleanup
	destFile := filepath.Join(workDir, "ibmcloud-service-ids.json")
	input, err := os.ReadFile(serviceIDFile)
	if err != nil {
		return fmt.Errorf("read service ID file: %w", err)
	}

	if err := os.WriteFile(destFile, input, 0600); err != nil {
		return fmt.Errorf("write service ID file: %w", err)
	}

	log.Printf("Stored service ID metadata in %s", destFile)
	return nil
}

// CleanupIBMCloudServiceIDs cleans up service IDs created by ccoctl
func (i *Installer) CleanupIBMCloudServiceIDs(ctx context.Context, workDir string, config *IBMCloudCCOConfig) error {
	// Read service ID metadata
	serviceIDFile := filepath.Join(workDir, "ibmcloud-service-ids.json")

	if _, err := os.Stat(serviceIDFile); os.IsNotExist(err) {
		log.Printf("No IBM Cloud service ID metadata found, skipping cleanup")
		return nil
	}

	data, err := os.ReadFile(serviceIDFile)
	if err != nil {
		return fmt.Errorf("read service ID metadata: %w", err)
	}

	var serviceIDs map[string]interface{}
	if err := json.Unmarshal(data, &serviceIDs); err != nil {
		return fmt.Errorf("parse service ID metadata: %w", err)
	}

	// Get infraID
	infraID, err := i.getInfraID(workDir)
	if err != nil {
		// If we can't get infraID, try to use cluster name from metadata
		log.Printf("Warning: could not get infraID: %v", err)
		return nil
	}

	// Run ccoctl delete
	log.Printf("Deleting IBM Cloud service IDs for cluster %s...", infraID)
	if err := i.executeCCOCtlIBMCloudDelete(ctx, infraID, config); err != nil {
		return fmt.Errorf("delete service IDs: %w", err)
	}

	log.Printf("✓ IBM Cloud service IDs deleted successfully")
	return nil
}

// executeCCOCtlIBMCloudDelete runs ccoctl to delete service IDs
func (i *Installer) executeCCOCtlIBMCloudDelete(ctx context.Context, clusterName string, config *IBMCloudCCOConfig) error {
	// ccoctl ibmcloud delete-service-id \
	//   --name=<cluster-name> \
	//   --region=<region> \
	//   --resource-group-name=<rg>

	args := []string{
		"ibmcloud", "delete-service-id",
		"--name=" + clusterName,
	}

	if config.ResourceGroup != "" {
		args = append(args, "--resource-group-name="+config.ResourceGroup)
	}

	cmd := exec.CommandContext(ctx, i.ccoCtlPath, args...)

	// Set environment variables
	cmd.Env = append(os.Environ(),
		"IC_API_KEY="+config.APIKey,
	)

	if config.AccountID != "" {
		cmd.Env = append(cmd.Env, "IC_ACCOUNT_ID="+config.AccountID)
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("Executing: %s", cmd.String())
	err := cmd.Run()
	if err != nil {
		// Don't fail if service IDs are already deleted
		if strings.Contains(stderr.String(), "not found") {
			log.Printf("Service IDs already deleted or not found")
			return nil
		}
		return fmt.Errorf("ccoctl delete failed: %w\nStdout: %s\nStderr: %s", err, stdout.String(), stderr.String())
	}

	log.Printf("ccoctl delete output:\n%s", stdout.String())
	return nil
}

// extractCredentialRequestsIBMCloud extracts CredentialsRequest manifests for IBM Cloud
func (i *Installer) extractCredentialRequestsIBMCloud(ctx context.Context, outputDir string) error {
	// Read the release image from install-config or detect from openshift-install
	releaseImage, err := i.getReleaseImage(ctx)
	if err != nil {
		return fmt.Errorf("get release image: %w", err)
	}

	log.Printf("Extracting CredentialsRequests from release image: %s", releaseImage)

	// Get pull secret from environment
	pullSecret := os.Getenv("OPENSHIFT_PULL_SECRET")
	if pullSecret == "" {
		return fmt.Errorf("OPENSHIFT_PULL_SECRET environment variable not set")
	}

	// Write pull secret to temporary file for oc command
	pullSecretFile := filepath.Join(os.TempDir(), fmt.Sprintf("pull-secret-%d.json", os.Getpid()))
	if err := os.WriteFile(pullSecretFile, []byte(pullSecret), 0600); err != nil {
		return fmt.Errorf("write pull secret: %w", err)
	}
	defer os.Remove(pullSecretFile)

	// Use oc adm release extract to get CredentialsRequests for IBM Cloud
	cmd := exec.CommandContext(ctx, "oc", "adm", "release", "extract",
		"--credentials-requests",
		"--cloud=ibmcloud",
		"--to="+outputDir,
		"--from="+releaseImage,
		"--registry-config="+pullSecretFile,
	)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	log.Printf("Running: oc adm release extract --credentials-requests --cloud=ibmcloud --to=%s --from=%s", outputDir, releaseImage)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("oc adm release extract failed: %w\nStdout: %s\nStderr: %s", err, stdout.String(), stderr.String())
	}

	log.Printf("oc output:\n%s", stdout.String())

	// Verify that CredentialsRequest files were extracted
	files, err := os.ReadDir(outputDir)
	if err != nil {
		return fmt.Errorf("read output dir: %w", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no CredentialsRequest manifests extracted")
	}

	log.Printf("Successfully extracted %d CredentialsRequest manifests", len(files))
	return nil
}

// ValidateIBMCloudCCOPrerequisites validates that ccoctl and IBM Cloud credentials are ready
func ValidateIBMCloudCCOPrerequisites(ccoCtlPath string, config *IBMCloudCCOConfig) error {
	// Check ccoctl binary exists
	if _, err := os.Stat(ccoCtlPath); err != nil {
		return fmt.Errorf("ccoctl binary not found at %s: %w", ccoCtlPath, err)
	}

	// Validate ccoctl supports ibmcloud
	cmd := exec.Command(ccoCtlPath, "ibmcloud", "--help")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ccoctl does not support ibmcloud subcommand (may need newer version): %w", err)
	}

	// Validate IBM Cloud credentials
	if config.APIKey == "" {
		return fmt.Errorf("IBM Cloud API key is required (IC_API_KEY)")
	}

	if config.Region == "" {
		return fmt.Errorf("IBM Cloud region is required (IC_REGION)")
	}

	log.Printf("✓ IBM Cloud CCO prerequisites validated")
	return nil
}
