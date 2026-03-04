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
	config    *Config
	store     *store.Store
	installer *installer.Installer
	registry  *profile.Registry
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
		config:    config,
		store:     st,
		installer: installer.NewInstaller(),
		registry:  registry,
	}
}

// Handle handles a cluster creation job
func (h *CreateHandler) Handle(ctx context.Context, job *types.Job) error {
	// Get cluster details
	cluster, err := h.store.Clusters.GetByID(ctx, job.ClusterID)
	if err != nil {
		return fmt.Errorf("get cluster: %w", err)
	}

	log.Printf("Creating cluster %s (platform=%s, version=%s, profile=%s)",
		cluster.Name, cluster.Platform, cluster.Version, cluster.Profile)

	// Update cluster status to CREATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Create work directory for this cluster
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	if err := os.MkdirAll(workDir, 0755); err != nil {
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
	createReq := &types.CreateClusterRequest{
		Name:          cluster.Name,
		Platform:      string(cluster.Platform),
		Version:       cluster.Version,
		Profile:       cluster.Profile,
		Region:        cluster.Region,
		BaseDomain:    cluster.BaseDomain,
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

	// Run openshift-install create cluster
	log.Printf("Running openshift-install create cluster for %s", cluster.Name)

	output, err := h.installer.CreateCluster(ctx, workDir)

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

	// Construct API URL and Console URL from cluster name and base domain
	if cluster.Name != "" && cluster.BaseDomain != "" {
		apiURL := fmt.Sprintf("https://api.%s.%s:6443", cluster.Name, cluster.BaseDomain)
		outputs.APIURL = &apiURL

		consoleURL := fmt.Sprintf("https://console-openshift-console.apps.%s.%s", cluster.Name, cluster.BaseDomain)
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
	// TODO: Implement proper artifact storage (S3)
	// For now, artifacts remain in the work directory

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
			S3URI:        fmt.Sprintf("file://%s", kubeconfigPath),
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
			S3URI:        fmt.Sprintf("file://%s", metadataPath),
			SizeBytes:    &size,
			CreatedAt:    time.Now(),
		})
	}

	// Install log
	logPath := filepath.Join(workDir, ".openshift_install.log")
	if stat, err := os.Stat(logPath); err == nil {
		size := stat.Size()
		artifacts = append(artifacts, types.ClusterArtifact{
			ID:           uuid.New().String(),
			ClusterID:    clusterID,
			ArtifactType: types.ArtifactTypeLog,
			S3URI:        fmt.Sprintf("file://%s", logPath),
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
