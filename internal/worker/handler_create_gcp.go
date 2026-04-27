package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

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

	// Create log file path
	logPath := filepath.Join(workDir, "gke-create.log")

	// Create GKE cluster
	log.Printf("Creating GKE cluster %s...", cluster.Name)
	output, err := gkeInstaller.CreateCluster(ctx, config, logPath)
	if err != nil {
		return fmt.Errorf("GKE cluster creation failed: %w\nOutput: %s", err, output)
	}

	log.Printf("GKE cluster %s created successfully", cluster.Name)

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
// GCP labels must be lowercase alphanumeric plus hyphens, max 63 chars
func sanitizeGCPLabel(s string) string {
	// TODO: Implement proper GCP label sanitization
	// For now, just return the input (will be implemented in a follow-up)
	return s
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
