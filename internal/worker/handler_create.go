package worker

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/sts"
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

// Handle handles a cluster creation job by provisioning infrastructure via platform-specific installers.
// Routes to the appropriate handler based on cluster type (OpenShift, EKS, or IKS).
// Streams deployment logs to the database in real-time and stores artifacts (kubeconfig, metadata) upon completion.
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
	case types.ClusterTypeROSA:
		return h.handleROSACreate(ctx, job, cluster)
	case types.ClusterTypeEKS:
		return h.handleEKSCreate(ctx, job, cluster)
	case types.ClusterTypeIKS:
		return h.handleIKSCreate(ctx, job, cluster)
	case types.ClusterTypeGKE:
		return h.handleGKECreate(ctx, job, cluster)
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

	// Platform-specific pre-flight checks
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile for pre-flight check: %w", err)
	}

	if cluster.Platform == types.PlatformAWS {
		// AWS-specific pre-flight checks (instance type availability)
		log.Printf("Running AWS pre-flight capacity checks for region %s", cluster.Region)
		checker, err := NewAWSPreflightChecker(ctx, cluster.Region)
		if err != nil {
			log.Printf("Warning: failed to create AWS pre-flight checker: %v", err)
			// Don't fail cluster creation if pre-flight check setup fails
		} else {
			if err := checker.CheckInstanceTypeAvailability(ctx, prof); err != nil {
				// Pre-flight check failed - fail immediately before starting installation
				// Use PreflightCheckError to prevent retries and provide clear error code
				return types.NewPreflightCheckError("AWS capacity pre-flight check failed: %v", err)
			}
		}
	} else if cluster.Platform == types.PlatformGCP {
		// GCP-specific pre-flight checks (machine type availability)
		log.Printf("Running GCP pre-flight capacity checks for region %s", cluster.Region)
		checker, err := NewGCPPreflightChecker(ctx, getGCPProject(prof), cluster.Region, "")
		if err != nil {
			log.Printf("Warning: failed to create GCP pre-flight checker: %v", err)
			// Don't fail cluster creation if pre-flight check setup fails
		} else {
			defer checker.Close()
			if err := checker.CheckMachineTypeAvailability(ctx, prof); err != nil {
				// Pre-flight check failed - fail immediately before starting installation
				return types.NewPreflightCheckError("GCP capacity pre-flight check failed: %v", err)
			}
		}
	}

	// Create work directory for this cluster with secure permissions
	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
	}

	// Render install-config.yaml
	renderer := profile.NewRenderer(h.registry)

	// Get pull secret from environment
	pullSecret := os.Getenv("OPENSHIFT_PULL_SECRET")
	if pullSecret == "" {
		return fmt.Errorf("OPENSHIFT_PULL_SECRET environment variable not set")
	}

	// Merge custom pull secret if provided
	if cluster.CustomPullSecret != nil && *cluster.CustomPullSecret != "" {
		log.Printf("Merging custom pull secret with standard pull secret for cluster %s", cluster.Name)
		mergedSecret, err := mergePullSecrets(pullSecret, *cluster.CustomPullSecret)
		if err != nil {
			return fmt.Errorf("merge pull secrets: %w", err)
		}
		pullSecret = mergedSecret
		log.Printf("Successfully merged custom pull secret")
	}

	// Build create cluster request for renderer
	// Convert BaseDomain pointer to string
	baseDomain := ""
	if cluster.BaseDomain != nil {
		baseDomain = *cluster.BaseDomain
	}

	createReq := &types.CreateClusterRequest{
		Name:            cluster.Name,
		Platform:        string(cluster.Platform),
		Version:         cluster.Version,
		Profile:         cluster.Profile,
		Region:          cluster.Region,
		BaseDomain:      baseDomain,
		Owner:           cluster.Owner,
		Team:            cluster.Team,
		CostCenter:      cluster.CostCenter,
		TTLHours:        cluster.TTLHours,
		SSHPublicKey:    cluster.SSHPublicKey,
		ExtraTags:       cluster.RequestTags,
		OffhoursOptIn:   cluster.OffhoursOptIn,
		CredentialsMode: cluster.CredentialsMode,
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

	// Set credentials mode if specified (e.g., "Static" for permanent credentials)
	if cluster.CredentialsMode != nil && *cluster.CredentialsMode != "" {
		inst.SetCredentialsMode(*cluster.CredentialsMode)
		log.Printf("Set credentials mode: %s", *cluster.CredentialsMode)
	}

	// Platform-specific pre-installation steps
	if cluster.Platform == types.PlatformIBMCloud {
		// IBM Cloud requires CCO manual mode - run ccoctl before cluster creation
		log.Printf("Running IBM Cloud pre-installation (CCO workflow)...")
		if err := h.HandleIBMCloudCreate(ctx, job, inst, workDir); err != nil {
			return fmt.Errorf("IBM Cloud pre-installation: %w", err)
		}
	} else if cluster.Platform == types.PlatformGCP {
		// GCP OpenShift: verify authentication and setup
		log.Printf("Running GCP pre-installation checks...")
		if err := h.HandleGCPOpenShiftCreate(ctx, job, inst, workDir, cluster, prof); err != nil {
			return fmt.Errorf("GCP pre-installation: %w", err)
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
	time.Sleep(LogBatchFlushDelay) // Allow final batch to flush
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	if err != nil {
		// Logs are already streamed to database, but log the error
		if logData, readErr := os.ReadFile(logPath); readErr == nil {
			log.Printf("Install failed, logs:\n%s", string(logData))
		}

		// GCP-specific handling: Check if cluster API is actually accessible despite timeout
		// GCP clusters can take 60+ minutes to initialize, exceeding openshift-install's 40-minute timeout
		// If the cluster API is responding, treat it as successful
		if cluster.Platform == types.PlatformGCP && cluster.ClusterType == types.ClusterTypeOpenShift {
			log.Printf("GCP cluster creation reported failure, checking if cluster API is actually accessible...")
			if h.verifyGCPClusterAccessible(ctx, cluster) {
				log.Printf("GCP cluster API is accessible despite timeout - treating as successful")
				// Clear the error and continue with post-install steps
				err = nil
			} else {
				log.Printf("GCP cluster API is not accessible - this is a genuine failure")
			}
		}

		// If error is still set (not a GCP timeout recovery), handle the failure
		if err != nil {
			// Best-effort: try to tag whatever resources were created before failure
			// This ensures orphaned resources can be detected even from failed installations
			log.Printf("Cluster creation failed, attempting to tag partial resources...")
			inst.TagPartialResources(ctx, workDir, *metadata)

			return fmt.Errorf("openshift-install create cluster: %w\nOutput: %s", err, output)
		}
	}

	log.Printf("Cluster %s created successfully", cluster.Name)

	// Record AWS resources in cleanup manifest for fast destroy (AWS only)
	if cluster.Platform == types.PlatformAWS {
		log.Printf("Recording AWS resources in cleanup manifest for cluster %s...", cluster.Name)
		if err := h.recordAWSCleanupManifest(ctx, workDir, cluster); err != nil {
			log.Printf("Warning: failed to record AWS cleanup manifest: %v", err)
			// Don't fail cluster creation - manifest is an optimization, not required
		} else {
			log.Printf("Successfully recorded AWS cleanup manifest")
		}
	}

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

	// AWS Cleanup Manifest (for fast destroy)
	manifestPath := filepath.Join(workDir, "aws-cleanup-manifest.json")
	if stat, err := os.Stat(manifestPath); err == nil {
		size := stat.Size()
		artifacts = append(artifacts, types.ClusterArtifact{
			ID:           uuid.New().String(),
			ClusterID:    clusterID,
			ArtifactType: types.ArtifactTypeMetadata,
			S3URI:        fmt.Sprintf("s3://%s/clusters/%s/artifacts/aws-cleanup-manifest.json", h.config.S3BucketName, clusterID),
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

	// Install log - check for ibmcloud-ks log
	ibmcloudLogPath := filepath.Join(workDir, "ibmcloud-ks.log")
	if stat, err := os.Stat(ibmcloudLogPath); err == nil {
		size := stat.Size()
		artifacts = append(artifacts, types.ClusterArtifact{
			ID:           uuid.New().String(),
			ClusterID:    clusterID,
			ArtifactType: types.ArtifactTypeLog,
			S3URI:        fmt.Sprintf("s3://%s/clusters/%s/artifacts/ibmcloud-ks.log", h.config.S3BucketName, clusterID),
			SizeBytes:    &size,
			CreatedAt:    time.Now(),
		})
	}

	// Install log - check for ROSA log (contains both create and install logs)
	rosaLogPath := filepath.Join(workDir, "rosa-create.log")
	if stat, err := os.Stat(rosaLogPath); err == nil {
		size := stat.Size()
		artifacts = append(artifacts, types.ClusterArtifact{
			ID:           uuid.New().String(),
			ClusterID:    clusterID,
			ArtifactType: types.ArtifactTypeLog,
			S3URI:        fmt.Sprintf("s3://%s/clusters/%s/artifacts/rosa.log", h.config.S3BucketName, clusterID),
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

	// Create work directory for this cluster with secure permissions
	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
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
			if cluster.SSHPublicKey != nil && *cluster.SSHPublicKey != "" {
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
			if cluster.SSHPublicKey != nil && *cluster.SSHPublicKey != "" {
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
	time.Sleep(LogBatchFlushDelay) // Allow final batch to flush
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

	// Create work directory for this cluster with secure permissions
	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
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

	publicVLAN := ""
	privateVLAN := ""

	if prof.Compute.Workers != nil {
		if prof.Compute.Workers.MachineType != "" {
			machineType = prof.Compute.Workers.MachineType
		}
		if prof.Compute.Workers.Count > 0 {
			workerCount = prof.Compute.Workers.Count
		}
		publicVLAN = prof.Compute.Workers.PublicVLAN
		privateVLAN = prof.Compute.Workers.PrivateVLAN
	}

	// Resolve VLANs dynamically if not specified or set to "auto"
	if publicVLAN == "" || publicVLAN == "auto" || privateVLAN == "" || privateVLAN == "auto" {
		log.Printf("Resolving classic VLANs for zone %s...", zone)
		resolved, err := iksInstaller.ResolveClassicVLANs(ctx, zone)
		if err != nil {
			return fmt.Errorf("resolve classic VLANs for zone %s: %w", zone, err)
		}

		if publicVLAN == "" || publicVLAN == "auto" {
			publicVLAN = resolved.PublicVLAN
		}
		if privateVLAN == "" || privateVLAN == "auto" {
			privateVLAN = resolved.PrivateVLAN
		}

		log.Printf("Resolved VLANs: public=%s, private=%s", publicVLAN, privateVLAN)
	}

	createOpts := &installer.IKSClusterCreateOptions{
		Name:         cluster.Name,
		Zone:         zone,
		MachineType:  machineType,
		Workers:      workerCount,
		KubeVersion:  cluster.Version,
		PublicVLAN:   publicVLAN,
		PrivateVLAN:  privateVLAN,
		PublicServiceEndpoint:  prof.Features.PublicServiceEndpoint,
		PrivateServiceEndpoint: prof.Features.PrivateServiceEndpoint,
	}

	log.Printf("Creating IKS cluster: zone=%s, machine=%s, workers=%d, publicVLAN=%s, privateVLAN=%s",
		zone, machineType, workerCount, publicVLAN, privateVLAN)

	// Start log streaming before running ibmcloud CLI
	// This will tail ibmcloud-ks.log and stream to database in real-time
	logPath := filepath.Join(workDir, "ibmcloud-ks.log")
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logPath)

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		log.Printf("Warning: failed to start log streaming: %v", err)
	}

	// Create the cluster
	output, err := iksInstaller.CreateCluster(ctx, createOpts, logPath)

	if err != nil {
		// Stop log streaming after cluster creation fails
		streamCancel()
		time.Sleep(LogBatchFlushDelay) // Allow final batch to flush
		if stopErr := streamer.Stop(); stopErr != nil {
			log.Printf("Warning: error stopping log streamer: %v", stopErr)
		}

		log.Printf("IKS cluster creation failed: %v\nOutput: %s", err, output)
		return fmt.Errorf("ibmcloud ks cluster create: %w", err)
	}

	log.Printf("IKS cluster %s creation initiated", cluster.Name)

	// Wait for cluster to be ready (IKS clusters take 20-30 minutes)
	log.Printf("Waiting for IKS cluster %s to reach READY state...", cluster.Name)
	if err := iksInstaller.WaitForCluster(ctx, cluster.Name, "normal", 60*time.Minute, logPath); err != nil {
		// Stop log streaming after wait fails
		streamCancel()
		time.Sleep(LogBatchFlushDelay) // Allow final batch to flush
		if stopErr := streamer.Stop(); stopErr != nil {
			log.Printf("Warning: error stopping log streamer: %v", stopErr)
		}

		return fmt.Errorf("wait for cluster ready: %w", err)
	}

	// Stop log streaming after cluster is ready
	streamCancel()
	time.Sleep(LogBatchFlushDelay) // Allow final batch to flush
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
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
	// Reload cluster from database to get latest custom_post_config
	// (it may have been set by API after job started)
	freshCluster, err := h.store.Clusters.GetByID(ctx, cluster.ID)
	if err != nil {
		log.Printf("Warning: failed to reload cluster for post-deployment check: %v", err)
		return
	}
	cluster = freshCluster

	// Check if profile has post-deployment configuration enabled
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		log.Printf("Warning: failed to get profile for post-deployment check: %v", err)
		return
	}

	// Check if cluster has custom post-config (user-defined or add-ons)
	hasCustomPostConfig := cluster.CustomPostConfig != nil && (
		len(cluster.CustomPostConfig.Operators) > 0 ||
		len(cluster.CustomPostConfig.Scripts) > 0 ||
		len(cluster.CustomPostConfig.Manifests) > 0 ||
		len(cluster.CustomPostConfig.HelmCharts) > 0)

	// Determine if post-deployment should run
	profileHasPostDeploy := prof.PostDeployment != nil && prof.PostDeployment.Enabled

	if !profileHasPostDeploy && !hasCustomPostConfig {
		log.Printf("Post-deployment not needed for cluster %s (no profile config or custom config)", cluster.Name)
		return
	}

	log.Printf("Post-deployment needed for cluster %s (profile=%v, custom=%v)", cluster.Name, profileHasPostDeploy, hasCustomPostConfig)

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

	if err := h.store.Jobs.Create(ctx, nil, postConfigJob); err != nil {
		log.Printf("Warning: failed to create POST_CONFIGURE job: %v", err)
	} else {
		log.Printf("Created POST_CONFIGURE job for cluster %s", cluster.Name)
	}
}

// recordAWSCleanupManifest discovers and records AWS resources created during cluster installation
// for fast manifest-driven cleanup during destroy. This eliminates the need for account-wide
// resource discovery, reducing destroy time from 60-90s to <5s.
func (h *CreateHandler) recordAWSCleanupManifest(ctx context.Context, workDir string, cluster *types.Cluster) error {
	// Get infraID from metadata.json
	metadataPath := filepath.Join(workDir, "metadata.json")
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return fmt.Errorf("read metadata.json: %w", err)
	}

	var metadata struct {
		InfraID string `json:"infraID"`
	}

	if err := json.Unmarshal(data, &metadata); err != nil {
		return fmt.Errorf("parse metadata.json: %w", err)
	}

	if metadata.InfraID == "" {
		return fmt.Errorf("infraID not found in metadata.json")
	}

	infraID := metadata.InfraID
	log.Printf("Recording AWS cleanup manifest for infraID: %s", infraID)

	// Initialize manifest with cluster metadata
	if err := RecordAWSInfraID(workDir, cluster.Name, infraID, cluster.Region); err != nil {
		return fmt.Errorf("record infraID: %w", err)
	}

	// Discover and record IAM roles created by CCO
	// These follow the pattern: <infraID>-<component>-cloud-credentials
	if err := h.discoverAndRecordIAMRoles(ctx, workDir, infraID, cluster.Region); err != nil {
		log.Printf("Warning: failed to discover IAM roles: %v", err)
		// Don't fail - partial manifest is better than no manifest
	}

	// Record OIDC provider ARN (deterministic, can construct from infraID)
	if err := h.recordOIDCProvider(ctx, workDir, infraID, cluster.Region); err != nil {
		log.Printf("Warning: failed to record OIDC provider: %v", err)
	}

	// Discover and record Route53 hosted zone
	if cluster.BaseDomain != nil && *cluster.BaseDomain != "" {
		if err := h.discoverAndRecordRoute53Zone(ctx, workDir, cluster.Name, *cluster.BaseDomain, cluster.Region); err != nil {
			log.Printf("Warning: failed to discover Route53 zone: %v", err)
		}
	}

	log.Printf("AWS cleanup manifest recording complete")
	return nil
}

// discoverAndRecordIAMRoles discovers IAM roles created by CCO and records them in the manifest
func (h *CreateHandler) discoverAndRecordIAMRoles(ctx context.Context, workDir, infraID, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	iamClient := iam.NewFromConfig(cfg)

	// List roles matching infraID prefix
	paginator := iam.NewListRolesPaginator(iamClient, &iam.ListRolesInput{})

	prefix := infraID + "-"
	roleCount := 0

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("list IAM roles: %w", err)
		}

		for _, role := range page.Roles {
			roleName := aws.ToString(role.RoleName)
			if strings.HasPrefix(roleName, prefix) {
				if err := RecordAWSIAMRole(workDir, roleName); err != nil {
					log.Printf("Warning: failed to record IAM role %s: %v", roleName, err)
					continue
				}
				roleCount++
				log.Printf("Recorded IAM role: %s", roleName)
			}
		}
	}

	log.Printf("Discovered and recorded %d IAM roles", roleCount)
	return nil
}

// recordOIDCProvider constructs and records the OIDC provider ARN
func (h *CreateHandler) recordOIDCProvider(ctx context.Context, workDir, infraID, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	stsClient := sts.NewFromConfig(cfg)

	// Get AWS account ID
	identity, err := stsClient.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return fmt.Errorf("get AWS account ID: %w", err)
	}

	accountID := aws.ToString(identity.Account)

	// Construct OIDC provider ARN
	// Format: arn:aws:iam::{account}:oidc-provider/{infraID}-oidc.s3.{region}.amazonaws.com
	oidcProviderArn := fmt.Sprintf(
		"arn:aws:iam::%s:oidc-provider/%s-oidc.s3.%s.amazonaws.com",
		accountID, infraID, region,
	)

	if err := RecordAWSOIDCProvider(workDir, oidcProviderArn); err != nil {
		return fmt.Errorf("record OIDC provider: %w", err)
	}

	log.Printf("Recorded OIDC provider: %s", oidcProviderArn)
	return nil
}

// discoverAndRecordRoute53Zone discovers the Route53 hosted zone and records its ID
func (h *CreateHandler) discoverAndRecordRoute53Zone(ctx context.Context, workDir, clusterName, baseDomain, region string) error {
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	r53 := route53.NewFromConfig(cfg)

	// Construct zone name
	zoneName := fmt.Sprintf("%s.%s.", clusterName, baseDomain)

	// List hosted zones by name
	out, err := r53.ListHostedZonesByName(ctx, &route53.ListHostedZonesByNameInput{
		DNSName: aws.String(zoneName),
	})
	if err != nil {
		return fmt.Errorf("list hosted zones: %w", err)
	}

	// Find matching zone
	for _, zone := range out.HostedZones {
		if aws.ToString(zone.Name) == zoneName {
			zoneID := strings.TrimPrefix(aws.ToString(zone.Id), "/hostedzone/")

			if err := RecordAWSRoute53HostedZone(workDir, zoneID); err != nil {
				return fmt.Errorf("record Route53 zone: %w", err)
			}

			log.Printf("Recorded Route53 hosted zone: %s (ID: %s)", zoneName, zoneID)
			return nil
		}
	}

	return fmt.Errorf("Route53 hosted zone not found for %s", zoneName)
}

// handleGKECreate handles GKE cluster creation
func (h *CreateHandler) handleGKECreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting GKE cluster creation for %s", cluster.Name)

	// Update cluster status to CREATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Create work directory for this cluster with secure permissions
	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
	}

	// Get profile to extract configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Call GCP-specific create handler
	if err := h.HandleGKECreate(ctx, job, cluster, prof, workDir); err != nil {
		return err
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status to READY: %w", err)
	}

	log.Printf("GKE cluster %s is now READY", cluster.Name)

	// Store cluster outputs (API URL, kubeconfig path, etc.)
	if err := h.storeGKEClusterOutputs(ctx, cluster, workDir, prof); err != nil {
		log.Printf("Warning: failed to store cluster outputs: %v", err)
		// Don't fail cluster creation if output storage fails
	}

	// Handle post-deployment configuration if enabled
	h.handlePostDeployment(ctx, cluster)

	return nil
}

// handleROSACreate handles ROSA (Red Hat OpenShift Service on AWS) cluster creation
func (h *CreateHandler) handleROSACreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting ROSA cluster creation for %s", cluster.Name)

	// Update cluster status to CREATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Create work directory for this cluster with secure permissions
	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
	}

	// Create ROSA installer
	rosaInstaller := installer.NewROSAInstaller()

	// Get profile to extract configuration
	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}

	// Build rosa create cluster command arguments
	args := []string{
		"--cluster-name", cluster.Name,
		"--region", cluster.Region,
		"--version", cluster.Version,
		"--yes", // Auto-approve
	}

	// Add compute configuration from profile
	if prof.Compute.Workers.Replicas > 0 {
		args = append(args, "--replicas", fmt.Sprintf("%d", prof.Compute.Workers.Replicas))
	}
	if prof.Compute.Workers.InstanceType != "" {
		args = append(args, "--compute-machine-type", prof.Compute.Workers.InstanceType)
	}

	// Add multi-AZ if specified in profile
	if prof.PlatformConfig.ROSA != nil && prof.PlatformConfig.ROSA.MultiAZ {
		args = append(args, "--multi-az")
	}

	// Add tags (ROSA format: 'key value, key2 value2')
	if len(cluster.EffectiveTags) > 0 {
		tagStrs := make([]string, 0, len(cluster.EffectiveTags))
		for k, v := range cluster.EffectiveTags {
			tagStrs = append(tagStrs, fmt.Sprintf("%s %s", k, v))
		}
		args = append(args, "--tags", strings.Join(tagStrs, ", "))
	}

	// Add STS mode with auto mode (ROSA auto-creates operator roles)
	args = append(args, "--sts", "--mode", "auto")

	log.Printf("Creating ROSA cluster with args: %v", args)

	// Start log streaming before running rosa
	logPath := filepath.Join(workDir, "rosa-create.log")
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logPath)

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		log.Printf("Warning: failed to start log streaming: %v", err)
	}

	// Run rosa create cluster
	clusterID, output, err := rosaInstaller.CreateCluster(ctx, args, logPath)

	if err != nil {
		// Stop log streaming after rosa create fails
		streamCancel()
		time.Sleep(LogBatchFlushDelay)
		if stopErr := streamer.Stop(); stopErr != nil {
			log.Printf("Warning: error stopping log streamer: %v", stopErr)
		}

		log.Printf("ROSA cluster creation failed: %v\nOutput: %s", err, output)
		return fmt.Errorf("rosa create cluster: %w", err)
	}

	log.Printf("ROSA cluster %s created with ID: %s", cluster.Name, clusterID)

	// Start streaming installation logs in background
	// This will run 'rosa logs install --watch' and append to the same log file (rosa-create.log)
	// so all logs appear in one continuous stream in the UI
	go func() {
		if err := rosaInstaller.StreamInstallLogs(streamCtx, cluster.Name, logPath); err != nil {
			log.Printf("Warning: installation log streaming ended: %v", err)
		}
	}()

	// Wait for cluster to be ready (ROSA clusters can take 40-60 minutes)
	log.Printf("Waiting for ROSA cluster %s to reach ready state...", cluster.Name)
	waitCtx, waitCancel := context.WithTimeout(ctx, 90*time.Minute)
	defer waitCancel()

	if err := rosaInstaller.WaitForClusterReady(waitCtx, cluster.Name, 30*time.Second); err != nil {
		return fmt.Errorf("wait for cluster ready: %w", err)
	}

	log.Printf("ROSA cluster %s is ready", cluster.Name)

	// Stop log streaming after cluster is ready
	streamCancel()
	time.Sleep(LogBatchFlushDelay) // Allow final batch to flush
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	// Get cluster information
	info, err := rosaInstaller.DescribeCluster(ctx, cluster.Name)
	if err != nil {
		return fmt.Errorf("describe cluster: %w", err)
	}

	// Get kubeconfig and admin credentials
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfigPath), 0700); err != nil {
		return fmt.Errorf("create auth directory: %w", err)
	}

	var adminCreds *installer.ROSAAdminCredentials
	adminCreds, err = rosaInstaller.GetKubeconfig(ctx, cluster.Name, kubeconfigPath)
	if err != nil {
		log.Printf("Warning: failed to get kubeconfig: %v", err)
	}

	// Get machine pools to store in metadata
	pools, err := rosaInstaller.ListMachinePools(ctx, cluster.Name)
	if err != nil {
		log.Printf("Warning: failed to list machine pools: %v", err)
		pools = []installer.ROSAMachinePool{}
	}

	// Store machine pool metadata for future reference
	// (Machine pools are already saved in job metadata during hibernation)
	poolsJSON, err := json.Marshal(pools)
	if err != nil {
		log.Printf("Warning: failed to marshal machine pools: %v", err)
	} else {
		log.Printf("Stored machine pool metadata: %d pools", len(pools))
		_ = poolsJSON // Will use this for cluster metadata in a future update
	}

	// Extract cluster outputs
	outputs := &types.ClusterOutputs{
		ID:        uuid.New().String(),
		ClusterID: cluster.ID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if apiURL := info.APIURL(); apiURL != "" {
		outputs.APIURL = &apiURL
	}
	if consoleURL := info.ConsoleURL(); consoleURL != "" {
		outputs.ConsoleURL = &consoleURL
	}

	kubeconfigURI := fmt.Sprintf("file://%s", kubeconfigPath)
	outputs.KubeconfigS3URI = &kubeconfigURI

	// Store admin credentials for console login (expires in 72 hours)
	if adminCreds != nil {
		// Store credentials in format: username:password
		// Note: These credentials expire after 72 hours
		adminSecret := fmt.Sprintf("%s:%s", adminCreds.Username, adminCreds.Password)
		outputs.KubeadminSecretRef = &adminSecret
		log.Printf("Stored ROSA admin credentials (expires in 72 hours)")
	}

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

	log.Printf("ROSA cluster %s is now READY", cluster.Name)

	// Handle post-deployment configuration if enabled
	h.handlePostDeployment(ctx, cluster)

	return nil
}

// storeGKEClusterOutputs stores GKE cluster access information in the database
func (h *CreateHandler) storeGKEClusterOutputs(ctx context.Context, cluster *types.Cluster, workDir string, prof *profile.Profile) error {
	// For GKE, we need to get the cluster endpoint from gcloud
	gkeInstaller := installer.NewGKEInstaller()

	info, err := gkeInstaller.GetClusterInfo(ctx, cluster.Name, getGCPProject(prof), cluster.Region, "")
	if err != nil {
		return fmt.Errorf("get GKE cluster info: %w", err)
	}

	// Build API URL from endpoint
	apiURL := fmt.Sprintf("https://%s", info.Endpoint)

	// Create cluster outputs record
	outputs := &types.ClusterOutputs{
		ID:              uuid.New().String(),
		ClusterID:       cluster.ID,
		APIURL:          &apiURL,
		KubeconfigS3URI: func() *string { s := fmt.Sprintf("file://%s/auth/kubeconfig", workDir); return &s }(),
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	// Upsert outputs to database
	if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		return fmt.Errorf("upsert cluster outputs: %w", err)
	}

	log.Printf("Stored GKE cluster outputs: API URL=%s", apiURL)
	return nil
}

// verifyGCPClusterAccessible checks if a GCP OpenShift cluster API is accessible
// This is used to handle cases where openshift-install times out but the cluster actually succeeds
// GCP clusters can take 60+ minutes to initialize, exceeding the installer's 40-minute timeout
func (h *CreateHandler) verifyGCPClusterAccessible(ctx context.Context, cluster *types.Cluster) bool {
	if cluster.BaseDomain == nil || *cluster.BaseDomain == "" {
		log.Printf("Cannot verify GCP cluster: base domain is not set")
		return false
	}

	// Build API URL: https://api.{cluster-name}.{base-domain}:6443
	apiURL := fmt.Sprintf("https://api.%s.%s:6443", cluster.Name, *cluster.BaseDomain)
	healthURL := fmt.Sprintf("%s/healthz", apiURL)

	log.Printf("Checking if GCP cluster API is accessible at %s", healthURL)

	// Create HTTP client with TLS skip verification (cluster uses self-signed certs)
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// Try to reach the health endpoint
	req, err := http.NewRequestWithContext(ctx, "GET", healthURL, nil)
	if err != nil {
		log.Printf("Failed to create request: %v", err)
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to reach cluster API: %v", err)
		return false
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Failed to read response: %v", err)
		return false
	}

	// Check if response is "ok"
	responseText := string(body)
	if resp.StatusCode == 200 && strings.TrimSpace(responseText) == "ok" {
		log.Printf("GCP cluster API is accessible and healthy: %s", responseText)
		return true
	}

	log.Printf("GCP cluster API responded but not healthy: status=%d, body=%s", resp.StatusCode, responseText)
	return false
}

// mergePullSecrets merges a custom pull secret with the standard pull secret
// Both inputs are JSON strings in Docker config format: {"auths": {"registry": {"auth": "..."}}}
// Returns merged JSON string with auths from both secrets
func mergePullSecrets(standardSecret, customSecret string) (string, error) {
	// Parse standard pull secret
	var standard map[string]interface{}
	if err := json.Unmarshal([]byte(standardSecret), &standard); err != nil {
		return "", fmt.Errorf("parse standard pull secret: %w", err)
	}

	// Parse custom pull secret
	var custom map[string]interface{}
	if err := json.Unmarshal([]byte(customSecret), &custom); err != nil {
		return "", fmt.Errorf("parse custom pull secret: %w", err)
	}

	// Get auths from both secrets
	standardAuths, ok := standard["auths"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("standard pull secret missing 'auths' field")
	}

	customAuths, ok := custom["auths"].(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("custom pull secret missing 'auths' field")
	}

	// Merge: custom auths override standard auths for same registry
	for registry, creds := range customAuths {
		standardAuths[registry] = creds
	}

	// Update merged auths back into standard
	standard["auths"] = standardAuths

	// Marshal back to JSON
	merged, err := json.Marshal(standard)
	if err != nil {
		return "", fmt.Errorf("marshal merged pull secret: %w", err)
	}

	return string(merged), nil
}
