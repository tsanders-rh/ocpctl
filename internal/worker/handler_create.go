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
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// CreateHandler handles cluster creation jobs
type CreateHandler struct {
	config   *Config
	store    *store.Store
	registry *profile.Registry
}

// NewCreateHandler creates a new create handler
func NewCreateHandler(config *Config, st *store.Store) *CreateHandler {
	// Load profile registry
	profilesDir := os.Getenv("PROFILES_DIR")
	if profilesDir == "" {
		profilesDir = "internal/profile/definitions"
	}

	loader := profile.NewLoader(profilesDir)
	registry, err := profile.NewRegistry(loader)
	if err != nil {
		log.Fatalf("Failed to load profile registry: %v", err)
	}

	return &CreateHandler{
		config:   config,
		store:    st,
		registry: registry,
	}
}

// Handle handles a cluster creation job
func (h *CreateHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Creating cluster %s (platform=%s, cluster_type=%s, version=%s, profile=%s)",
		cluster.Name, cluster.Platform, cluster.ClusterType, cluster.Version, cluster.Profile)

	// Route to appropriate handler based on cluster type
	switch cluster.ClusterType {
	case types.ClusterTypeOpenShift:
		return h.handleOpenShiftCreate(ctx, job, cluster)
	case types.ClusterTypeEKS:
		return h.handleEKSCreate(ctx, job, cluster)
	case types.ClusterTypeIKS:
		return h.handleIKSCreate(ctx, job, cluster)
	default:
		return fmt.Errorf("unsupported cluster type: %s", cluster.ClusterType)
	}
}

// handleOpenShiftCreate handles OpenShift cluster creation
func (h *CreateHandler) handleOpenShiftCreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting OpenShift cluster creation for %s", cluster.Name)

	// Update cluster status to CREATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Create work directory for this cluster
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return fmt.Errorf("create work directory: %w", err)
	}

	// Render install-config.yaml
	renderer := profile.NewRenderer(h.registry)

	// Get pull secret from environment
	pullSecret := os.Getenv("OPENSHIFT_PULL_SECRET")
	if pullSecret == "" {
		return fmt.Errorf("OPENSHIFT_PULL_SECRET environment variable not set")
	}

	// Build create cluster request for renderer
	// Convert BaseDomain pointer to string
	baseDomain := ""
	if cluster.BaseDomain != nil {
		baseDomain = *cluster.BaseDomain
	}

	createReq := &types.CreateClusterRequest{
		Name:          cluster.Name,
		Platform:      string(cluster.Platform),
		Version:       cluster.Version,
		Profile:       cluster.Profile,
		Region:        cluster.Region,
		BaseDomain:    baseDomain,
		Owner:         cluster.Owner,
		Team:          cluster.Team,
		CostCenter:    cluster.CostCenter,
		TTLHours:      cluster.TTLHours,
		SSHPublicKey:  cluster.SSHPublicKey,
		ExtraTags:     cluster.RequestTags,
		OffhoursOptIn: cluster.OffhoursOptIn,
	}

	installConfig, err := renderer.RenderInstallConfig(createReq, pullSecret, cluster.EffectiveTags)
	if err != nil {
		return fmt.Errorf("render install-config: %w", err)
	}

	// Write install-config.yaml
	installConfigPath := filepath.Join(workDir, "install-config.yaml")
	if err := os.WriteFile(installConfigPath, installConfig, 0600); err != nil {
		return fmt.Errorf("write install-config: %w", err)
	}

	log.Printf("Generated install-config.yaml for cluster %s", cluster.Name)

	// Start log streaming before running openshift-install
	// This will tail .openshift_install.log and stream to database in real-time
	logPath := filepath.Join(workDir, ".openshift_install.log")
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logPath)

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		log.Printf("Warning: failed to start log streaming: %v", err)
	}

	// Create version-specific installer for this cluster
	log.Printf("Creating installer for OpenShift version %s", cluster.Version)
	inst, err := installer.NewInstallerForVersion(cluster.Version)
	if err != nil {
		return fmt.Errorf("create installer for version %s: %w", cluster.Version, err)
	}

	// Platform-specific pre-installation steps
	if cluster.Platform == types.PlatformIBMCloud {
		// IBM Cloud requires CCO manual mode - run ccoctl before cluster creation
		log.Printf("Running IBM Cloud pre-installation (CCO workflow)...")
		if err := h.HandleIBMCloudCreate(ctx, job, inst, workDir); err != nil {
			return fmt.Errorf("IBM Cloud pre-installation: %w", err)
		}
	}

	// Run openshift-install create cluster
	log.Printf("Running openshift-install create cluster for %s (version %s)", cluster.Name, cluster.Version)

	// Build cluster metadata for tagging
	metadata := &installer.ClusterMetadata{
		ClusterName: cluster.Name,
		ProfileName: cluster.Profile,
		InfraID:     "", // Will be populated during cluster creation
		CreatedAt:   cluster.CreatedAt,
		Region:      cluster.Region,
	}

	var output string

	if cluster.Platform == types.PlatformIBMCloud {
		// IBM Cloud: use direct cluster creation (CCO already done)
		output, err = inst.CreateClusterDirect(ctx, workDir)
	} else {
		// AWS and other platforms: use standard workflow
		output, err = inst.CreateCluster(ctx, workDir, metadata)
	}

	// Stop log streaming after installer completes
	streamCancel()
	time.Sleep(500 * time.Millisecond) // Allow final batch to flush
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	if err != nil {
		// Logs are already streamed to database, but log the error
		if logData, readErr := os.ReadFile(logPath); readErr == nil {
			log.Printf("Install failed, logs:\n%s", string(logData))
		}

		return fmt.Errorf("openshift-install create cluster: %w\nOutput: %s", err, output)
	}

	log.Printf("Cluster %s created successfully", cluster.Name)

	// Tag all AWS resources now that cluster creation is complete
	// By now (30-45 min later), IAM eventual consistency has resolved
	log.Printf("Tagging all AWS resources for cluster %s...", cluster.Name)
	if err := inst.TagAWSResources(ctx, workDir, *metadata); err != nil {
		log.Printf("Warning: failed to tag AWS resources: %v", err)
		// Don't fail cluster creation - it's already installed and working
		// Resources will be detected as orphaned and can be tagged retroactively
	} else {
		log.Printf("Successfully tagged all AWS resources")
	}

	// Extract cluster outputs (API URL, console URL, etc.)
	outputs, err := h.extractClusterOutputs(workDir, cluster)
	if err != nil {
		log.Printf("Warning: failed to extract cluster outputs: %v", err)
	} else {
		// Store cluster outputs (upsert to handle re-runs)
		if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
			log.Printf("Warning: failed to store cluster outputs: %v", err)
		}
	}

	// Store artifacts (kubeconfig, metadata, install dir)
	if err := h.storeArtifacts(ctx, workDir, cluster.ID); err != nil {
		log.Printf("Warning: failed to store artifacts: %v", err)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status to ready: %w", err)
	}

	log.Printf("Cluster %s is now READY", cluster.Name)

	// Handle post-deployment configuration if enabled
	h.handlePostDeployment(ctx, cluster)

	return nil
}

// extractClusterOutputs extracts cluster access information
func (h *CreateHandler) extractClusterOutputs(workDir string, cluster *types.Cluster) (*types.ClusterOutputs, error) {
	outputs := &types.ClusterOutputs{
		ID:        uuid.New().String(),
		ClusterID: cluster.ID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Construct API URL and Console URL from cluster name and base domain (OpenShift only)
	if cluster.Name != "" && cluster.BaseDomain != nil && *cluster.BaseDomain != "" {
		apiURL := fmt.Sprintf("https://api.%s.%s:6443", cluster.Name, *cluster.BaseDomain)
		outputs.APIURL = &apiURL

		consoleURL := fmt.Sprintf("https://console-openshift-console.apps.%s.%s", cluster.Name, *cluster.BaseDomain)
		outputs.ConsoleURL = &consoleURL

		log.Printf("Extracted cluster URLs - API: %s, Console: %s", apiURL, consoleURL)
	}

	// Set metadata S3 URI
	metadataPath := filepath.Join(workDir, "metadata.json")
	if _, err := os.Stat(metadataPath); err == nil {
		metadataURI := fmt.Sprintf("file://%s", metadataPath)
		outputs.MetadataS3URI = &metadataURI
	}

	// Set kubeconfig S3 URI
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")
	if _, err := os.Stat(kubeconfigPath); err == nil {
		kubeconfigURI := fmt.Sprintf("file://%s", kubeconfigPath)
		outputs.KubeconfigS3URI = &kubeconfigURI
		log.Printf("Kubeconfig found at: %s", kubeconfigPath)
	} else {
		log.Printf("Warning: kubeconfig not found at %s", kubeconfigPath)
	}

	// Set kubeadmin secret reference (path to kubeadmin-password file)
	kubeadminPasswordPath := filepath.Join(workDir, "auth", "kubeadmin-password")
	if _, err := os.Stat(kubeadminPasswordPath); err == nil {
		kubeadminRef := fmt.Sprintf("file://%s", kubeadminPasswordPath)
		outputs.KubeadminSecretRef = &kubeadminRef
		log.Printf("Kubeadmin password found at: %s", kubeadminPasswordPath)
	} else {
		log.Printf("Warning: kubeadmin password not found at %s", kubeadminPasswordPath)
	}

	return outputs, nil
}

// storeArtifacts stores cluster artifacts (kubeconfig, logs, metadata)
func (h *CreateHandler) storeArtifacts(ctx context.Context, workDir, clusterID string) error {
	// Create artifact storage client
	artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
	if err != nil {
		return fmt.Errorf("create artifact storage: %w", err)
	}

	// Upload all artifacts to S3
	if err := artifactStorage.UploadClusterArtifacts(ctx, workDir, clusterID); err != nil {
		return fmt.Errorf("upload artifacts: %w", err)
	}

	// Create artifact records for tracking
	artifacts := []types.ClusterArtifact{}

	// Kubeconfig
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")
	if stat, err := os.Stat(kubeconfigPath); err == nil {
		size := stat.Size()
		artifacts = append(artifacts, types.ClusterArtifact{
			ID:           uuid.New().String(),
			ClusterID:    clusterID,
			ArtifactType: types.ArtifactTypeAuthBundle,
			S3URI:        fmt.Sprintf("s3://%s/clusters/%s/artifacts/auth/kubeconfig", h.config.S3BucketName, clusterID),
			SizeBytes:    &size,
			CreatedAt:    time.Now(),
		})
	}

	// Metadata
	metadataPath := filepath.Join(workDir, "metadata.json")
	if stat, err := os.Stat(metadataPath); err == nil {
		size := stat.Size()
		artifacts = append(artifacts, types.ClusterArtifact{
			ID:           uuid.New().String(),
			ClusterID:    clusterID,
			ArtifactType: types.ArtifactTypeMetadata,
			S3URI:        fmt.Sprintf("s3://%s/clusters/%s/artifacts/metadata.json", h.config.S3BucketName, clusterID),
			SizeBytes:    &size,
			CreatedAt:    time.Now(),
		})
	}

	// Install log - check for OpenShift log
	openshiftLogPath := filepath.Join(workDir, ".openshift_install.log")
	if stat, err := os.Stat(openshiftLogPath); err == nil {
		size := stat.Size()
		artifacts = append(artifacts, types.ClusterArtifact{
			ID:           uuid.New().String(),
			ClusterID:    clusterID,
			ArtifactType: types.ArtifactTypeLog,
			S3URI:        fmt.Sprintf("s3://%s/clusters/%s/artifacts/openshift_install.log", h.config.S3BucketName, clusterID),
			SizeBytes:    &size,
			CreatedAt:    time.Now(),
		})
	}

	// Install log - check for eksctl log
	eksctlLogPath := filepath.Join(workDir, "eksctl.log")
	if stat, err := os.Stat(eksctlLogPath); err == nil {
		size := stat.Size()
		artifacts = append(artifacts, types.ClusterArtifact{
			ID:           uuid.New().String(),
			ClusterID:    clusterID,
			ArtifactType: types.ArtifactTypeLog,
			S3URI:        fmt.Sprintf("s3://%s/clusters/%s/artifacts/eksctl.log", h.config.S3BucketName, clusterID),
			SizeBytes:    &size,
			CreatedAt:    time.Now(),
		})
	}

	// Store artifact records
	for _, artifact := range artifacts {
		if err := h.store.Artifacts.Create(ctx, &artifact); err != nil {
			log.Printf("Failed to create artifact record %s: %v", artifact.ID, err)
		}
	}

	log.Printf("Stored %d artifact records for cluster %s", len(artifacts), clusterID)

	return nil
}

// handleEKSCreate handles EKS cluster creation
func (h *CreateHandler) handleEKSCreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting EKS cluster creation for %s", cluster.Name)

	// Update cluster status to CREATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Create work directory for this cluster
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return fmt.Errorf("create work directory: %w", err)
	}

	// Create EKS installer
	eksInstaller := installer.NewEKSInstaller()

	// Get profile to extract configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Build eksctl configuration
	eksConfig := &installer.EKSClusterConfig{
		APIVersion: "eksctl.io/v1alpha5",
		Kind:       "ClusterConfig",
		Metadata: installer.EKSMetadata{
			Name:    cluster.Name,
			Region:  cluster.Region,
			Version: cluster.Version,
			Tags:    cluster.EffectiveTags,
		},
		IAM: &installer.EKSIAM{
			WithOIDC: true, // Enable OIDC provider for IRSA
		},
		NodeGroups: []installer.EKSNodeGroup{},
	}

	// Add node groups from profile (unmanaged)
	if len(prof.Compute.NodeGroups) > 0 {
		for _, ng := range prof.Compute.NodeGroups {
			eksNodeGroup := installer.EKSNodeGroup{
				Name:            ng.Name,
				InstanceType:    ng.InstanceType,
				DesiredCapacity: ng.DesiredCapacity,
				MinSize:         ng.MinSize,
				MaxSize:         ng.MaxSize,
				VolumeSize:      ng.VolumeSize,
				VolumeType:      ng.VolumeType,
				Tags:            cluster.EffectiveTags,
			}

			// Add SSH configuration if public key provided
			if cluster.SSHPublicKey != nil {
				sshKeyPath := filepath.Join(workDir, "ssh-key.pub")
				if err := os.WriteFile(sshKeyPath, []byte(*cluster.SSHPublicKey), 0600); err != nil {
					return fmt.Errorf("write SSH public key: %w", err)
				}
				eksNodeGroup.SSH = &installer.EKSNodeGroupSSH{
					Allow:         true,
					PublicKeyPath: sshKeyPath,
				}
			}

			eksConfig.NodeGroups = append(eksConfig.NodeGroups, eksNodeGroup)
		}
	}

	// Add managed node groups from profile (EKS-managed)
	if len(prof.Compute.ManagedNodeGroups) > 0 {
		for _, ng := range prof.Compute.ManagedNodeGroups {
			eksManagedNodeGroup := installer.EKSManagedNodeGroup{
				Name:            ng.Name,
				InstanceType:    ng.InstanceType,
				DesiredCapacity: ng.DesiredCapacity,
				MinSize:         ng.MinSize,
				MaxSize:         ng.MaxSize,
				VolumeSize:      ng.VolumeSize,
				VolumeType:      ng.VolumeType,
				AMIFamily:       ng.AMIFamily,
				Tags:            cluster.EffectiveTags,
			}

			// Add SSH configuration if public key provided
			if cluster.SSHPublicKey != nil {
				sshKeyPath := filepath.Join(workDir, "ssh-key.pub")
				if err := os.WriteFile(sshKeyPath, []byte(*cluster.SSHPublicKey), 0600); err != nil {
					return fmt.Errorf("write SSH public key: %w", err)
				}
				eksManagedNodeGroup.SSH = &installer.EKSNodeGroupSSH{
					Allow:         true,
					PublicKeyPath: sshKeyPath,
				}
			}

			eksConfig.ManagedNodeGroups = append(eksConfig.ManagedNodeGroups, eksManagedNodeGroup)
		}
	}

	// Set VPC configuration from profile
	if prof.Networking.VpcCIDR != "" {
		eksConfig.VPC = &installer.EKSVPC{
			CIDR: prof.Networking.VpcCIDR,
		}
		// Set NAT gateway mode
		if prof.Networking.NatGateway != "" {
			eksConfig.VPC.NAT = &installer.EKSNAT{
				Gateway: prof.Networking.NatGateway,
			}
		}
	}

	// Write eksctl configuration
	if err := eksInstaller.WriteConfig(workDir, eksConfig); err != nil {
		return fmt.Errorf("write eksctl config: %w", err)
	}

	log.Printf("Generated eksctl config for cluster %s", cluster.Name)

	// Start log streaming before running eksctl
	// This will tail eksctl.log and stream to database in real-time
	logPath := filepath.Join(workDir, "eksctl.log")
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logPath)

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		log.Printf("Warning: failed to start log streaming: %v", err)
	}

	// Run eksctl create cluster
	configPath := filepath.Join(workDir, "eksctl-config.yaml")
	_, err = eksInstaller.CreateCluster(ctx, configPath, logPath)

	// Stop log streaming after eksctl completes
	streamCancel()
	time.Sleep(500 * time.Millisecond) // Allow final batch to flush
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	if err != nil {
		// Logs are already streamed to database
		log.Printf("EKS cluster creation failed: %v", err)
		return fmt.Errorf("eksctl create cluster: %w", err)
	}

	log.Printf("EKS cluster %s created successfully", cluster.Name)

	// Get kubeconfig
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0700); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	if err := eksInstaller.GetKubeconfig(ctx, cluster.Name, cluster.Region, kubeconfigPath); err != nil {
		log.Printf("Warning: failed to get kubeconfig: %v", err)
	}

	// Extract cluster outputs
	outputs := &types.ClusterOutputs{
		ID:        uuid.New().String(),
		ClusterID: cluster.ID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// For EKS, construct API URL from cluster name and region
	apiURL := fmt.Sprintf("https://%s.%s.eks.amazonaws.com", cluster.Name, cluster.Region)
	outputs.APIURL = &apiURL

	// EKS doesn't have a console URL like OpenShift
	consoleURL := fmt.Sprintf("https://console.aws.amazon.com/eks/home?region=%s#/clusters/%s", cluster.Region, cluster.Name)
	outputs.ConsoleURL = &consoleURL

	kubeconfigURI := fmt.Sprintf("file://%s", kubeconfigPath)
	outputs.KubeconfigS3URI = &kubeconfigURI

	// Store cluster outputs
	if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		log.Printf("Warning: failed to store cluster outputs: %v", err)
	}

	// Store artifacts
	if err := h.storeArtifacts(ctx, workDir, cluster.ID); err != nil {
		log.Printf("Warning: failed to store artifacts: %v", err)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status to ready: %w", err)
	}

	log.Printf("EKS cluster %s is now READY", cluster.Name)

	// Handle post-deployment configuration if enabled
	h.handlePostDeployment(ctx, cluster)

	return nil
}

// handleIKSCreate handles IKS cluster creation
func (h *CreateHandler) handleIKSCreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting IKS cluster creation for %s", cluster.Name)

	// Update cluster status to CREATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Create work directory for this cluster
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	if err := os.MkdirAll(workDir, 0700); err != nil {
		return fmt.Errorf("create work directory: %w", err)
	}

	// Get profile to extract configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Extract resource group from profile (if specified)
	resourceGroup := ""
	if prof.PlatformConfig.IBMCloud != nil {
		resourceGroup = prof.PlatformConfig.IBMCloud.ResourceGroup
	}

	// Create IKS installer
	iksInstaller := installer.NewIKSInstaller()

	// Get IBM Cloud API key from environment
	apiKey := os.Getenv("IBMCLOUD_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("IBMCLOUD_API_KEY environment variable not set")
	}

	// Login to IBM Cloud (will query for resource groups if not specified)
	if err := iksInstaller.Login(ctx, apiKey, cluster.Region, resourceGroup); err != nil {
		return fmt.Errorf("IBM Cloud login: %w", err)
	}

	// Build IKS cluster create options
	zone := prof.Zones.Default
	if zone == "" {
		return fmt.Errorf("no default zone configured in profile")
	}

	machineType := "bx2.4x16" // Default machine type
	workerCount := 2          // Default worker count

	if prof.Compute.Workers != nil {
		if prof.Compute.Workers.MachineType != "" {
			machineType = prof.Compute.Workers.MachineType
		}
		if prof.Compute.Workers.Count > 0 {
			workerCount = prof.Compute.Workers.Count
		}
	}

	createOpts := &installer.IKSClusterCreateOptions{
		Name:         cluster.Name,
		Zone:         zone,
		MachineType:  machineType,
		Workers:      workerCount,
		KubeVersion:  cluster.Version,
		PublicVLAN:   "auto",
		PrivateVLAN:  "auto",
		PublicServiceEndpoint:  prof.Features.PublicServiceEndpoint,
		PrivateServiceEndpoint: prof.Features.PrivateServiceEndpoint,
	}

	log.Printf("Creating IKS cluster with options: zone=%s, machine=%s, workers=%d", zone, machineType, workerCount)

	// Create the cluster
	output, err := iksInstaller.CreateCluster(ctx, createOpts)
	if err != nil {
		log.Printf("IKS cluster creation failed: %v\nOutput: %s", err, output)
		return fmt.Errorf("ibmcloud ks cluster create: %w", err)
	}

	log.Printf("IKS cluster %s creation initiated", cluster.Name)

	// Wait for cluster to be ready (IKS clusters take 20-30 minutes)
	log.Printf("Waiting for IKS cluster %s to reach READY state...", cluster.Name)
	if err := iksInstaller.WaitForCluster(ctx, cluster.Name, "normal", 60*time.Minute); err != nil {
		return fmt.Errorf("wait for cluster ready: %w", err)
	}

	log.Printf("IKS cluster %s is ready", cluster.Name)

	// Get cluster info
	info, err := iksInstaller.GetClusterInfo(ctx, cluster.Name)
	if err != nil {
		return fmt.Errorf("get cluster info: %w", err)
	}

	// Get kubeconfig
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0700); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	if err := iksInstaller.GetKubeconfig(ctx, cluster.Name, kubeconfigPath); err != nil {
		log.Printf("Warning: failed to get kubeconfig: %v", err)
	}

	// Extract cluster outputs
	outputs := &types.ClusterOutputs{
		ID:        uuid.New().String(),
		ClusterID: cluster.ID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	outputs.APIURL = &info.MasterURL

	// IKS console URL
	consoleURL := fmt.Sprintf("https://cloud.ibm.com/kubernetes/clusters/%s/overview", info.ID)
	outputs.ConsoleURL = &consoleURL

	kubeconfigURI := fmt.Sprintf("file://%s", kubeconfigPath)
	outputs.KubeconfigS3URI = &kubeconfigURI

	// Store cluster outputs
	if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		log.Printf("Warning: failed to store cluster outputs: %v", err)
	}

	// Store artifacts
	if err := h.storeArtifacts(ctx, workDir, cluster.ID); err != nil {
		log.Printf("Warning: failed to store artifacts: %v", err)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status to ready: %w", err)
	}

	log.Printf("IKS cluster %s is now READY", cluster.Name)

	// Handle post-deployment configuration if enabled
	h.handlePostDeployment(ctx, cluster)

	return nil
}

// handlePostDeployment checks if post-deployment is enabled and creates the POST_CONFIGURE job
// This is called by all cluster create handlers (OpenShift, EKS, IKS)
func (h *CreateHandler) handlePostDeployment(ctx context.Context, cluster *types.Cluster) {
	// Check if profile has post-deployment configuration enabled
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		log.Printf("Warning: failed to get profile for post-deployment check: %v", err)
		return
	}

	if prof.PostDeployment == nil || !prof.PostDeployment.Enabled {
		log.Printf("Post-deployment not enabled for profile %s", cluster.Profile)
		return
	}

	// Check if user opted to skip post-deployment
	if cluster.SkipPostDeployment {
		log.Printf("Post-deployment skipped for cluster %s (user opted out)", cluster.Name)
		return
	}

	// Check if post-deployment has already been completed (e.g., from previous run)
	// This prevents duplicate POST_CONFIGURE jobs
	if cluster.PostDeployStatus != nil && *cluster.PostDeployStatus == "completed" {
		log.Printf("Post-deployment already completed for cluster %s, skipping", cluster.Name)
		return
	}

	// Create POST_CONFIGURE job
	log.Printf("Profile %s has post-deployment enabled, creating POST_CONFIGURE job", cluster.Profile)

	postConfigJob := &types.Job{
		ID:          uuid.New().String(),
		ClusterID:   cluster.ID,
		JobType:     types.JobTypePostConfigure,
		Status:      types.JobStatusPending,
		Attempt:     1,
		MaxAttempts: 3,
	}

	if err := h.store.Jobs.Create(ctx, postConfigJob); err != nil {
		log.Printf("Warning: failed to create POST_CONFIGURE job: %v", err)
	} else {
		log.Printf("Created POST_CONFIGURE job for cluster %s", cluster.Name)
	}
}
