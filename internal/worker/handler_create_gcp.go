package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// HandleGKECreate handles GKE cluster creation using gcloud CLI
func (h *CreateHandler) HandleGKECreate(ctx context.Context, job *types.Job, cluster *types.Cluster, prof *profile.Profile, workDir string) error {
	log.Printf("Starting GKE cluster creation for %s in project %s, region %s",
		cluster.Name, prof.PlatformConfig.GKE, cluster.Region)

	// Verify GCP authentication
	if err := VerifyGCPAuthentication(ctx, getGCPProject(prof)); err != nil {
		return fmt.Errorf("GCP authentication failed: %w", err)
	}

	// Create GKE installer
	gkeInstaller := installer.NewGKEInstaller()

	// Build GKE cluster configuration
	config := h.buildGKEClusterConfig(cluster, prof)

	// Create log file path and ensure it exists before starting log streamer
	logPath := filepath.Join(workDir, "gke-create.log")

	// Create empty log file to ensure it exists before log streamer starts
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create log file: %w", err)
	}
	logFile.Close()

	// Start log streaming before running gcloud CLI
	// This will tail gke-create.log and stream to database in real-time
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logPath)

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	if err := streamer.Start(streamCtx); err != nil {
		log.Printf("Warning: failed to start log streaming: %v", err)
	}

	// Write initial progress message to log file for user feedback
	// gcloud is very quiet during cluster creation, so we add manual progress updates
	initialMsg := fmt.Sprintf("Starting GKE cluster creation for %s in region %s (this typically takes 5-10 minutes)...\n", cluster.Name, cluster.Region)
	if err := appendToLogFile(logPath, initialMsg); err != nil {
		log.Printf("Warning: failed to write initial message to log file: %v", err)
	}

	// Check if cluster already exists (for idempotent retries)
	existingCluster, err := gkeInstaller.GetClusterInfo(ctx, cluster.Name, getGCPProject(prof), cluster.Region, "")
	if err == nil && existingCluster != nil {
		log.Printf("GKE cluster %s already exists (likely from previous attempt), skipping creation", cluster.Name)
		appendToLogFile(logPath, fmt.Sprintf("Cluster already exists in GCP, reusing existing cluster...\n"))
	} else {
		// Create GKE cluster
		log.Printf("Creating GKE cluster %s...", cluster.Name)
		output, err := gkeInstaller.CreateCluster(ctx, config, logPath)

		if err != nil {
			// Stop log streaming before returning error
			streamCancel()
			time.Sleep(LogBatchFlushDelay)
			streamer.Stop()
			return fmt.Errorf("GKE cluster creation failed: %w\nOutput: %s", err, output)
		}

		log.Printf("GKE cluster %s created successfully", cluster.Name)
	}

	// Stop log streaming after cluster creation or verification
	streamCancel()
	time.Sleep(LogBatchFlushDelay) // Allow final batch to flush
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	// Get cluster info to extract endpoint and other details
	info, err := gkeInstaller.GetClusterInfo(ctx, cluster.Name, getGCPProject(prof), cluster.Region, "")
	if err != nil {
		log.Printf("Warning: failed to get GKE cluster info: %v", err)
	} else {
		// Store cluster info in job metadata
		job.Metadata["gke_endpoint"] = info.Endpoint
		job.Metadata["master_version"] = info.MasterVersion
		job.Metadata["status"] = info.Status
	}

	// Get kubeconfig
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")
	if err := gkeInstaller.GetKubeconfig(ctx, cluster.Name, getGCPProject(prof), cluster.Region, "", kubeconfigPath); err != nil {
		return fmt.Errorf("failed to get GKE kubeconfig: %w", err)
	}

	log.Printf("GKE kubeconfig saved to %s", kubeconfigPath)

	return nil
}

// HandleGCPOpenShiftCreate handles OpenShift cluster creation on GCP using openshift-install
func (h *CreateHandler) HandleGCPOpenShiftCreate(ctx context.Context, job *types.Job, inst *installer.Installer, workDir string, cluster *types.Cluster, prof *profile.Profile) error {
	log.Printf("Starting OpenShift on GCP creation for %s in project %s, region %s",
		cluster.Name, getGCPProject(prof), cluster.Region)

	// Verify GCP authentication
	if err := VerifyGCPAuthentication(ctx, getGCPProject(prof)); err != nil {
		return fmt.Errorf("GCP authentication failed: %w", err)
	}

	// GCP OpenShift installation follows standard OpenShift installer workflow
	// The installer will use GCP credentials from GOOGLE_APPLICATION_CREDENTIALS
	// No special pre-installation steps needed (unlike IBM Cloud CCO workflow)

	log.Printf("OpenShift on GCP will use standard installer workflow")
	return nil
}

// buildGKEClusterConfig builds GKE cluster configuration from profile and cluster
func (h *CreateHandler) buildGKEClusterConfig(cluster *types.Cluster, prof *profile.Profile) *installer.GKEClusterConfig {
	config := &installer.GKEClusterConfig{
		Name:    cluster.Name,
		Project: getGCPProject(prof),
		Region:  cluster.Region,
		Labels:  make(map[string]string),
	}

	// Add GKE-specific configuration from profile
	if prof.PlatformConfig.GKE != nil {
		gkeConfig := prof.PlatformConfig.GKE

		// Set cluster version (Kubernetes version)
		if cluster.Version != "" {
			config.ClusterVersion = cluster.Version
		}

		// Set release channel
		if gkeConfig.ReleaseChannel != "" {
			config.ReleaseChannel = gkeConfig.ReleaseChannel
		}

		// Enable Workload Identity
		if gkeConfig.EnableWorkloadIdentity {
			config.EnableWorkloadIdentity = true
		}

		// Set logging
		if len(gkeConfig.EnabledClusterLogTypes) > 0 {
			config.EnableClusterLogging = gkeConfig.EnabledClusterLogTypes
			// Also enable monitoring if logging is enabled
			config.EnableClusterMonitoring = []string{"SYSTEM_COMPONENTS"}
		}

		// Set access configuration
		config.PublicAccess = gkeConfig.PublicAccess
		config.PrivateAccess = gkeConfig.PrivateAccess

		// Configure node pools
		if len(gkeConfig.NodePools) > 0 {
			config.NodePools = make([]installer.GKENodePoolConfig, len(gkeConfig.NodePools))
			for i, pool := range gkeConfig.NodePools {
				config.NodePools[i] = installer.GKENodePoolConfig{
					Name:              pool.Name,
					MachineType:       pool.MachineType,
					DiskType:          pool.DiskType,
					DiskSize:          pool.DiskSizeGB,
					NumNodes:          pool.NodeCount,
					MinNodes:          pool.MinNodeCount,
					MaxNodes:          pool.MaxNodeCount,
					EnableAutoscaling: pool.EnableAutoScale,
					Labels:            make(map[string]string),
				}

				// Add cluster tags to node pool labels
				if cluster.EffectiveTags != nil {
					for k, v := range cluster.EffectiveTags {
						config.NodePools[i].Labels[k] = v
					}
				}
			}
		}
	}

	// Add compute configuration from profile workers
	if prof.Compute.Workers != nil {
		workers := prof.Compute.Workers

		// Set default node pool configuration if no node pools specified
		if len(config.NodePools) == 0 {
			config.MachineType = workers.MachineType
			if workers.Autoscaling {
				config.EnableAutoscaling = true
				config.MinNodes = workers.MinReplicas
				config.MaxNodes = workers.MaxReplicas
				config.NumNodes = workers.MinReplicas // Start at minimum
			} else {
				config.NumNodes = workers.Replicas
			}
		}
	}

	// Add cluster labels for cost tracking and management
	if cluster.EffectiveTags != nil {
		for k, v := range cluster.EffectiveTags {
			// GCP labels have restrictions: lowercase, alphanumeric, hyphens
			// Convert to valid GCP label format
			labelKey := sanitizeGCPLabel(k)
			labelValue := sanitizeGCPLabel(v)
			config.Labels[labelKey] = labelValue
		}
	}

	// Add standard ocpctl labels
	config.Labels["managed-by"] = "ocpctl"
	config.Labels["cluster-id"] = cluster.ID
	config.Labels["profile"] = sanitizeGCPLabel(cluster.Profile)

	return config
}

// sanitizeGCPLabel converts a string to a valid GCP label value
// GCP labels must be lowercase alphanumeric plus hyphens and underscores, max 63 chars
// Keys must start with a lowercase letter
func sanitizeGCPLabel(s string) string {
	if s == "" {
		return ""
	}

	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace invalid characters with hyphens
	// Valid characters: lowercase letters, numbers, hyphens, underscores
	var result strings.Builder
	for i, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		} else {
			// Replace invalid characters with hyphen
			result.WriteRune('-')
		}

		// For keys (first position), ensure it starts with a letter
		// If first character is not a letter, prepend 'x'
		if i == 0 && !((r >= 'a' && r <= 'z')) {
			// Move to beginning
			temp := result.String()
			result.Reset()
			result.WriteString("x")
			result.WriteString(temp)
		}
	}

	// Truncate to 63 characters max
	sanitized := result.String()
	if len(sanitized) > 63 {
		sanitized = sanitized[:63]
	}

	// Remove trailing hyphens or underscores (GCP requirement)
	sanitized = strings.TrimRight(sanitized, "-_")

	return sanitized
}

// getGCPProject extracts the GCP project ID from profile configuration
func getGCPProject(prof *profile.Profile) string {
	// Check GKE config first
	if prof.PlatformConfig.GKE != nil {
		// GKE config doesn't have a project field in the current schema
		// Will need to be added or pulled from environment
	}

	// Check GCP config (for OpenShift on GCP)
	if prof.PlatformConfig.GCP != nil && prof.PlatformConfig.GCP.Project != "" {
		return prof.PlatformConfig.GCP.Project
	}

	// Fall back to environment variable
	if project := os.Getenv("GCP_PROJECT"); project != "" {
		return project
	}

	// Default to empty - will cause error in GCP API calls
	log.Printf("Warning: GCP project ID not found in profile or GCP_PROJECT environment variable")
	return ""
}

// saveGKEMetadata saves GKE cluster metadata to a JSON file
func (h *CreateHandler) saveGKEMetadata(workDir string, info *installer.GKEClusterInfo) error {
	metadataPath := filepath.Join(workDir, "gke-metadata.json")

	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal GKE metadata: %w", err)
	}

	if err := os.WriteFile(metadataPath, data, 0644); err != nil {
		return fmt.Errorf("write GKE metadata: %w", err)
	}

	log.Printf("GKE metadata saved to %s", metadataPath)
	return nil
}

// appendToLogFile writes a message to the log file
// This is used to add manual progress updates that will be picked up by the LogStreamer
func appendToLogFile(logPath string, message string) error {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(message)
	return err
}
