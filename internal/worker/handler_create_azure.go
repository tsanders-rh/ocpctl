package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// handleAROCreate provisions an ARO (Azure Red Hat OpenShift) cluster
func (h *CreateHandler) handleAROCreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("[JOB %s] Starting ARO cluster creation: %s (region: %s)",
		job.ID, cluster.Name, cluster.Region)

	// Update cluster status to CREATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Get profile configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Validate ARO config in profile
	if prof.PlatformConfig.ARO == nil {
		return fmt.Errorf("profile missing ARO configuration")
	}

	aroConfig := prof.PlatformConfig.ARO

	// Create secure work directory
	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
	}

	// Get pull secret from environment
	pullSecret := os.Getenv("OPENSHIFT_PULL_SECRET")
	if pullSecret == "" {
		return fmt.Errorf("OPENSHIFT_PULL_SECRET environment variable not set")
	}

	// Write pull secret to file
	pullSecretPath := filepath.Join(workDir, "pull-secret.json")
	if err := os.WriteFile(pullSecretPath, []byte(pullSecret), 0600); err != nil {
		return fmt.Errorf("write pull secret: %w", err)
	}

	// Create ARO installer
	aroInstaller := installer.NewAROInstaller()

	// Verify Azure authentication
	if err := aroInstaller.VerifyAuthentication(ctx); err != nil {
		return fmt.Errorf("Azure authentication failed: %w", err)
	}

	// Generate resource group name
	resourceGroup := fmt.Sprintf("ocpctl-%s-rg", cluster.Name)

	// Create resource group
	log.Printf("[JOB %s] Creating resource group: %s", job.ID, resourceGroup)
	if err := aroInstaller.CreateResourceGroup(ctx, resourceGroup, cluster.Region, cluster.EffectiveTags); err != nil {
		return fmt.Errorf("create resource group: %w", err)
	}

	// Generate VNet and subnet names
	vnetName := fmt.Sprintf("%s-vnet", cluster.Name)
	masterSubnetName := "master-subnet"
	workerSubnetName := "worker-subnet"

	// Create VNet
	log.Printf("[JOB %s] Creating VNet: %s", job.ID, vnetName)
	if err := aroInstaller.CreateVNet(ctx, resourceGroup, vnetName, cluster.Region); err != nil {
		return fmt.Errorf("create vnet: %w", err)
	}

	// Create master subnet (10.0.0.0/23 = 512 IPs)
	log.Printf("[JOB %s] Creating master subnet", job.ID)
	if err := aroInstaller.CreateSubnet(ctx, resourceGroup, vnetName, masterSubnetName, "10.0.0.0/23", true); err != nil {
		return fmt.Errorf("create master subnet: %w", err)
	}

	// Create worker subnet (10.0.2.0/23 = 512 IPs)
	log.Printf("[JOB %s] Creating worker subnet", job.ID)
	if err := aroInstaller.CreateSubnet(ctx, resourceGroup, vnetName, workerSubnetName, "10.0.2.0/23", true); err != nil {
		return fmt.Errorf("create worker subnet: %w", err)
	}

	// Build ARO cluster config
	clusterConfig := &installer.AROClusterConfig{
		Name:             cluster.Name,
		ResourceGroup:    resourceGroup,
		Region:           cluster.Region,
		MasterVMSize:     aroConfig.MasterVMSize,
		WorkerVMSize:     aroConfig.WorkerVMSize,
		WorkerCount:      aroConfig.WorkerCount,
		OpenShiftVersion: cluster.Version,
		PullSecret:       fmt.Sprintf("@%s", pullSecretPath),
		Tags:             cluster.EffectiveTags,
	}

	// Create log file
	logFilePath := filepath.Join(workDir, ".aro_install.log")

	// Start log streaming goroutine
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logFilePath)
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		log.Printf("Warning: failed to start log streaming: %v", err)
	}

	// Create ARO cluster
	log.Printf("[JOB %s] Creating ARO cluster (this will take 30-40 minutes)", job.ID)
	output, err := aroInstaller.CreateCluster(ctx, clusterConfig, logFilePath, vnetName, masterSubnetName, workerSubnetName)
	if err != nil {
		// Stop log streaming
		streamCancel()
		time.Sleep(LogBatchFlushDelay)
		if stopErr := streamer.Stop(); stopErr != nil {
			log.Printf("Warning: error stopping log streamer: %v", stopErr)
		}

		log.Printf("[JOB %s] ARO cluster creation failed: %v", job.ID, err)
		log.Printf("[JOB %s] Output: %s", job.ID, output)
		return fmt.Errorf("create ARO cluster: %w", err)
	}

	log.Printf("[JOB %s] ARO cluster created successfully", job.ID)

	// Stop log streaming
	streamCancel()
	time.Sleep(LogBatchFlushDelay)
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	// Save metadata
	metadata := map[string]string{
		"resource_group": resourceGroup,
		"cluster_name":   cluster.Name,
		"region":         cluster.Region,
		"platform":       "azure",
		"cluster_type":   "aro",
	}
	if err := aroInstaller.SaveMetadata(workDir, metadata); err != nil {
		return fmt.Errorf("save metadata: %w", err)
	}

	// Download kubeconfig
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")
	log.Printf("[JOB %s] Downloading kubeconfig", job.ID)
	if err := aroInstaller.GetKubeconfig(ctx, resourceGroup, cluster.Name, kubeconfigPath); err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}

	// Get cluster info for outputs
	clusterInfo, err := aroInstaller.GetClusterInfo(ctx, resourceGroup, cluster.Name)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	// Upload artifacts to S3
	artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
	if err != nil {
		return fmt.Errorf("create artifact storage: %w", err)
	}

	log.Printf("[JOB %s] Uploading artifacts to S3", job.ID)
	if err := artifactStorage.UploadClusterArtifacts(ctx, workDir, cluster.ID); err != nil {
		return fmt.Errorf("upload artifacts: %w", err)
	}

	// Create cluster outputs record
	kubeconfigS3URI := fmt.Sprintf("s3://%s/clusters/%s/artifacts/auth/kubeconfig", h.config.S3BucketName, cluster.ID)
	outputs := &types.ClusterOutputs{
		ID:              uuid.New().String(),
		ClusterID:       cluster.ID,
		APIURL:          &clusterInfo.APIURL,
		ConsoleURL:      &clusterInfo.ConsoleURL,
		KubeconfigS3URI: &kubeconfigS3URI,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		return fmt.Errorf("create cluster outputs: %w", err)
	}

	// Store artifacts
	if err := h.storeArtifacts(ctx, workDir, cluster.ID); err != nil {
		log.Printf("Warning: failed to store artifacts: %v", err)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Set grace period to prevent immediate hibernation after installation
	gracePeriodExpiry := time.Now().Add(WorkHoursGracePeriod)
	if err := h.store.Clusters.SetLastWorkHoursCheck(ctx, cluster.ID, gracePeriodExpiry); err != nil {
		log.Printf("Warning: failed to set work hours grace period for cluster %s: %v", cluster.Name, err)
	}

	log.Printf("[JOB %s] ARO cluster creation completed successfully", job.ID)

	// Handle post-deployment configuration if enabled
	h.handlePostDeployment(ctx, cluster)

	return nil
}

// handleAKSCreate provisions an AKS (Azure Kubernetes Service) cluster
func (h *CreateHandler) handleAKSCreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("[JOB %s] Starting AKS cluster creation: %s (region: %s)",
		job.ID, cluster.Name, cluster.Region)

	// Update cluster status to CREATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Get profile configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Validate AKS config in profile
	if prof.PlatformConfig.AKS == nil {
		return fmt.Errorf("profile missing AKS configuration")
	}

	aksConfig := prof.PlatformConfig.AKS

	// Create secure work directory
	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
	}

	// Create AKS installer
	aksInstaller := installer.NewAKSInstaller()

	// Generate resource group name
	resourceGroup := fmt.Sprintf("ocpctl-%s-rg", cluster.Name)

	// Create resource group using ARO installer (shares resource group creation logic)
	aroInstaller := installer.NewAROInstaller()
	log.Printf("[JOB %s] Creating resource group: %s", job.ID, resourceGroup)
	if err := aroInstaller.CreateResourceGroup(ctx, resourceGroup, cluster.Region, cluster.EffectiveTags); err != nil {
		return fmt.Errorf("create resource group: %w", err)
	}

	// Build AKS cluster config
	clusterConfig := &installer.AKSClusterConfig{
		Name:              cluster.Name,
		ResourceGroup:     resourceGroup,
		Region:            cluster.Region,
		KubernetesVersion: cluster.Version,
		NetworkPlugin:     aksConfig.NetworkPlugin,
		NetworkPolicy:     aksConfig.NetworkPolicy,
		NodePools:         []installer.AKSNodePoolConfig{},
		Tags:              cluster.EffectiveTags,
	}

	// Convert profile node pools to installer node pools
	for _, pool := range aksConfig.NodePools {
		clusterConfig.NodePools = append(clusterConfig.NodePools, installer.AKSNodePoolConfig{
			Name:            pool.Name,
			VMSize:          pool.VMSize,
			Count:           pool.Count,
			MinCount:        pool.MinCount,
			MaxCount:        pool.MaxCount,
			EnableAutoScale: pool.EnableAutoScale,
			OSDiskSizeGB:    pool.OSDiskSizeGB,
		})
	}

	// Create log file
	logFilePath := filepath.Join(workDir, ".aks_install.log")

	// Start log streaming goroutine
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logFilePath)
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		log.Printf("Warning: failed to start log streaming: %v", err)
	}

	// Create AKS cluster
	log.Printf("[JOB %s] Creating AKS cluster (this will take 10-15 minutes)", job.ID)
	output, err := aksInstaller.CreateCluster(ctx, clusterConfig, logFilePath)
	if err != nil {
		// Stop log streaming
		streamCancel()
		time.Sleep(LogBatchFlushDelay)
		if stopErr := streamer.Stop(); stopErr != nil {
			log.Printf("Warning: error stopping log streamer: %v", stopErr)
		}

		log.Printf("[JOB %s] AKS cluster creation failed: %v", job.ID, err)
		log.Printf("[JOB %s] Output: %s", job.ID, output)
		return fmt.Errorf("create AKS cluster: %w", err)
	}

	log.Printf("[JOB %s] AKS cluster created successfully", job.ID)

	// Stop log streaming
	streamCancel()
	time.Sleep(LogBatchFlushDelay)
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	// Save metadata
	metadata := map[string]string{
		"resource_group": resourceGroup,
		"cluster_name":   cluster.Name,
		"region":         cluster.Region,
		"platform":       "azure",
		"cluster_type":   "aks",
	}
	if err := aroInstaller.SaveMetadata(workDir, metadata); err != nil {
		return fmt.Errorf("save metadata: %w", err)
	}

	// Download kubeconfig
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")
	log.Printf("[JOB %s] Downloading kubeconfig", job.ID)
	if err := aksInstaller.GetKubeconfig(ctx, resourceGroup, cluster.Name, kubeconfigPath); err != nil {
		return fmt.Errorf("get kubeconfig: %w", err)
	}

	// Get cluster info for outputs
	clusterInfo, err := aksInstaller.GetClusterInfo(ctx, resourceGroup, cluster.Name)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	// Upload artifacts to S3
	artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
	if err != nil {
		return fmt.Errorf("create artifact storage: %w", err)
	}

	log.Printf("[JOB %s] Uploading artifacts to S3", job.ID)
	if err := artifactStorage.UploadClusterArtifacts(ctx, workDir, cluster.ID); err != nil {
		return fmt.Errorf("upload artifacts: %w", err)
	}

	// Create cluster outputs record
	kubeconfigS3URI := fmt.Sprintf("s3://%s/clusters/%s/artifacts/auth/kubeconfig", h.config.S3BucketName, cluster.ID)
	apiURL := fmt.Sprintf("https://%s", clusterInfo.FQDN)
	outputs := &types.ClusterOutputs{
		ID:              uuid.New().String(),
		ClusterID:       cluster.ID,
		APIURL:          &apiURL,
		KubeconfigS3URI: &kubeconfigS3URI,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		return fmt.Errorf("create cluster outputs: %w", err)
	}

	// Store artifacts
	if err := h.storeArtifacts(ctx, workDir, cluster.ID); err != nil {
		log.Printf("Warning: failed to store artifacts: %v", err)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Set grace period to prevent immediate hibernation after installation
	gracePeriodExpiry := time.Now().Add(WorkHoursGracePeriod)
	if err := h.store.Clusters.SetLastWorkHoursCheck(ctx, cluster.ID, gracePeriodExpiry); err != nil {
		log.Printf("Warning: failed to set work hours grace period for cluster %s: %v", cluster.Name, err)
	}

	log.Printf("[JOB %s] AKS cluster creation completed successfully", job.ID)

	// Handle post-deployment configuration if enabled
	h.handlePostDeployment(ctx, cluster)

	return nil
}
