package janitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"github.com/tsanders-rh/ocpctl/internal/metrics"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// detectOrphanedGCPResources finds GCP resources that don't match any cluster in the database
func (j *Janitor) detectOrphanedGCPResources(ctx context.Context) error {
	// Build lookup maps using streaming to prevent memory exhaustion
	_, clustersByName, err := j.buildClusterLookupMaps(ctx)
	if err != nil {
		return fmt.Errorf("build cluster lookup maps: %w", err)
	}

	// Get GCP project from environment
	project := os.Getenv("GCP_PROJECT")
	if project == "" {
		log.Printf("GCP_PROJECT environment variable not set, skipping GCP orphan detection")
		return nil
	}

	log.Printf("Checking for orphaned GCP resources in project: %s", project)

	orphans := []OrphanedResource{}

	// Check Compute VM instances
	vmOrphans, err := j.detectOrphanedGCPInstances(ctx, project, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned GCP instances: %v", err)
	} else {
		orphans = append(orphans, vmOrphans...)
	}

	// Check Persistent Disks
	diskOrphans, err := j.detectOrphanedGCPDisks(ctx, project, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned GCP disks: %v", err)
	} else {
		orphans = append(orphans, diskOrphans...)
	}

	// Check VPCs (Networks)
	vpcOrphans, err := j.detectOrphanedGCPNetworks(ctx, project, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned GCP networks: %v", err)
	} else {
		orphans = append(orphans, vpcOrphans...)
	}

	// Check Load Balancers (forwarding rules)
	lbOrphans, err := j.detectOrphanedGCPLoadBalancers(ctx, project, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned GCP load balancers: %v", err)
	} else {
		orphans = append(orphans, lbOrphans...)
	}

	// Check Service Accounts
	saOrphans, err := j.detectOrphanedGCPServiceAccounts(ctx, project, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned GCP service accounts: %v", err)
	} else {
		orphans = append(orphans, saOrphans...)
	}

	// Check GCS Buckets
	bucketOrphans, err := j.detectOrphanedGCSBuckets(ctx, project, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned GCS buckets: %v", err)
	} else {
		orphans = append(orphans, bucketOrphans...)
	}

	// Check Cloud DNS zones
	dnsOrphans, err := j.detectOrphanedGCPDNSZones(ctx, project, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned GCP DNS zones: %v", err)
	} else {
		orphans = append(orphans, dnsOrphans...)
	}

	// Check GKE clusters
	gkeOrphans, err := j.detectOrphanedGKEClusters(ctx, project, clustersByName)
	if err != nil {
		log.Printf("Error detecting orphaned GKE clusters: %v", err)
	} else {
		orphans = append(orphans, gkeOrphans...)
	}

	// Report findings
	if len(orphans) > 0 {
		log.Printf("WARNING: Found %d orphaned GCP resources:", len(orphans))
		for _, orphan := range orphans {
			log.Printf("  - %s: %s (%s) in %s", orphan.Type, orphan.ResourceName, orphan.ResourceID, orphan.Region)

			// Persist to database
			clusterName := extractGCPClusterName(orphan.ResourceName, orphan.Tags)

			dbOrphan := &types.OrphanedResource{
				ResourceType: types.OrphanedResourceType(orphan.Type),
				ResourceID:   orphan.ResourceID,
				ResourceName: orphan.ResourceName,
				Region:       orphan.Region,
				ClusterName:  clusterName,
				Tags:         types.OrphanedResourceTags(orphan.Tags),
			}

			if err := j.store.OrphanedResources.Upsert(ctx, dbOrphan); err != nil {
				log.Printf("  WARNING: Failed to persist orphaned resource to database: %v", err)
			}
		}
		log.Printf("These resources may incur costs and should be manually cleaned up.")
		log.Printf("Consider running gcloud cleanup commands or using openshift-install destroy with saved metadata.")
		log.Printf("View orphaned resources in the admin console: /admin/orphaned-resources")

		// Publish CloudWatch metrics for total orphaned resources
		if j.metricsPublisher != nil {
			if err := j.metricsPublisher.PublishGauge(ctx, metrics.MetricOrphanedResources, float64(len(orphans)), map[string]string{
				"Platform": "gcp",
				"Project":  project,
			}); err != nil {
				log.Printf("Warning: failed to publish orphaned resources metric: %v", err)
			}

			// Publish metrics by resource type
			orphansByType := make(map[string]int)
			for _, orphan := range orphans {
				orphansByType[orphan.Type]++
			}

			for resourceType, count := range orphansByType {
				if err := j.metricsPublisher.PublishCount(ctx, metrics.MetricOrphanedResourceDetected, float64(count), map[string]string{
					"ResourceType": resourceType,
					"Platform":     "gcp",
					"Project":      project,
				}); err != nil {
					log.Printf("Warning: failed to publish orphaned resource metric for type %s: %v", resourceType, err)
				}
			}
		}
	} else {
		log.Printf("No orphaned GCP resources detected")

		// Publish zero metric when no orphans found
		if j.metricsPublisher != nil {
			if err := j.metricsPublisher.PublishGauge(ctx, metrics.MetricOrphanedResources, 0, map[string]string{
				"Platform": "gcp",
				"Project":  project,
			}); err != nil {
				log.Printf("Warning: failed to publish orphaned resources metric: %v", err)
			}
		}
	}

	return nil
}

// detectOrphanedGCPInstances finds GCP Compute instances tagged with cluster info but no matching cluster
func (j *Janitor) detectOrphanedGCPInstances(ctx context.Context, project string, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	// List all instances with managed-by=ocpctl label
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "instances", "list",
		"--project", project,
		"--filter", "labels.managed-by=ocpctl",
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list GCP instances: %w", err)
	}

	var instances []struct {
		Name   string            `json:"name"`
		Zone   string            `json:"zone"`
		Labels map[string]string `json:"labels"`
	}

	if err := json.Unmarshal(output, &instances); err != nil {
		return nil, fmt.Errorf("failed to parse GCP instances: %w", err)
	}

	orphans := []OrphanedResource{}
	for _, instance := range instances {
		clusterName := instance.Labels["cluster-name"]
		if clusterName == "" {
			log.Printf("[detectOrphanedGCPInstances] Instance %s has managed-by=ocpctl but no cluster-name label", instance.Name)
			continue
		}

		// Check if cluster exists in database
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			// Extract zone name (format: https://www.googleapis.com/compute/v1/projects/.../zones/us-central1-a)
			zoneName := instance.Zone
			if strings.Contains(zoneName, "/zones/") {
				parts := strings.Split(zoneName, "/zones/")
				if len(parts) == 2 {
					zoneName = parts[1]
				}
			}

			orphans = append(orphans, OrphanedResource{
				Type:         "GCPInstance",
				ResourceID:   instance.Name,
				ResourceName: instance.Name,
				Region:       extractGCPRegion(zoneName),
				Tags:         instance.Labels,
			})
		}
	}

	return orphans, nil
}

// detectOrphanedGCPDisks finds GCP Persistent Disks with cluster labels but no matching cluster
func (j *Janitor) detectOrphanedGCPDisks(ctx context.Context, project string, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	// List all disks with managed-by=ocpctl label
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "disks", "list",
		"--project", project,
		"--filter", "labels.managed-by=ocpctl",
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list GCP disks: %w", err)
	}

	var disks []struct {
		Name   string            `json:"name"`
		Zone   string            `json:"zone"`
		Labels map[string]string `json:"labels"`
	}

	if err := json.Unmarshal(output, &disks); err != nil {
		return nil, fmt.Errorf("failed to parse GCP disks: %w", err)
	}

	orphans := []OrphanedResource{}
	for _, disk := range disks {
		clusterName := disk.Labels["cluster-name"]
		if clusterName == "" {
			// Try kubernetes.io-cluster label
			for key, val := range disk.Labels {
				if strings.HasPrefix(key, "kubernetes-io-cluster-") && (val == "owned" || val == "shared") {
					// Extract cluster name from label key
					clusterName = strings.TrimPrefix(key, "kubernetes-io-cluster-")
					// Remove infra ID suffix if present (5-char suffix)
					if parts := strings.Split(clusterName, "-"); len(parts) >= 2 {
						if len(parts[len(parts)-1]) == 5 {
							clusterName = strings.Join(parts[0:len(parts)-1], "-")
						}
					}
					break
				}
			}
		}

		if clusterName == "" {
			log.Printf("[detectOrphanedGCPDisks] Disk %s has managed-by=ocpctl but no cluster-name label", disk.Name)
			continue
		}

		// Check if cluster exists in database
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			zoneName := disk.Zone
			if strings.Contains(zoneName, "/zones/") {
				parts := strings.Split(zoneName, "/zones/")
				if len(parts) == 2 {
					zoneName = parts[1]
				}
			}

			orphans = append(orphans, OrphanedResource{
				Type:         "GCPDisk",
				ResourceID:   disk.Name,
				ResourceName: disk.Name,
				Region:       extractGCPRegion(zoneName),
				Tags:         disk.Labels,
			})
		}
	}

	return orphans, nil
}

// detectOrphanedGCPNetworks finds GCP VPC networks with cluster labels but no matching cluster
func (j *Janitor) detectOrphanedGCPNetworks(ctx context.Context, project string, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	// GCP VPC networks don't support labels, so we can't easily detect orphaned networks
	// We would need to check subnets or use naming conventions
	// For now, return empty slice - this can be enhanced later
	log.Printf("[detectOrphanedGCPNetworks] Skipping network detection - GCP networks don't support labels")
	return []OrphanedResource{}, nil
}

// detectOrphanedGCPLoadBalancers finds GCP load balancers (forwarding rules) with cluster labels but no matching cluster
func (j *Janitor) detectOrphanedGCPLoadBalancers(ctx context.Context, project string, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	// List all forwarding rules with kubernetes.io-cluster label
	// Note: Forwarding rules don't support filtering by labels in gcloud, so we need to list all and filter
	cmd := exec.CommandContext(ctx, "gcloud", "compute", "forwarding-rules", "list",
		"--project", project,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list GCP forwarding rules: %w", err)
	}

	var forwardingRules []struct {
		Name   string `json:"name"`
		Region string `json:"region"`
	}

	if err := json.Unmarshal(output, &forwardingRules); err != nil {
		return nil, fmt.Errorf("failed to parse GCP forwarding rules: %w", err)
	}

	orphans := []OrphanedResource{}
	for _, rule := range forwardingRules {
		// Get detailed info including labels
		var cmd *exec.Cmd
		regionName := rule.Region
		if strings.Contains(regionName, "/regions/") {
			parts := strings.Split(regionName, "/regions/")
			if len(parts) == 2 {
				regionName = parts[1]
			}
		}

		if regionName != "" {
			cmd = exec.CommandContext(ctx, "gcloud", "compute", "forwarding-rules", "describe",
				rule.Name,
				"--region", regionName,
				"--project", project,
				"--format", "json")
		} else {
			// Global forwarding rule
			cmd = exec.CommandContext(ctx, "gcloud", "compute", "forwarding-rules", "describe",
				rule.Name,
				"--global",
				"--project", project,
				"--format", "json")
		}

		output, err := cmd.Output()
		if err != nil {
			log.Printf("Warning: failed to describe forwarding rule %s: %v", rule.Name, err)
			continue
		}

		var ruleDetails struct {
			Name   string            `json:"name"`
			Labels map[string]string `json:"labels"`
		}

		if err := json.Unmarshal(output, &ruleDetails); err != nil {
			log.Printf("Warning: failed to parse forwarding rule details for %s: %v", rule.Name, err)
			continue
		}

		// Check for managed-by label
		if ruleDetails.Labels["managed-by"] != "ocpctl" {
			continue
		}

		clusterName := ruleDetails.Labels["cluster-name"]
		if clusterName == "" {
			// Try kubernetes.io-cluster label
			for key, val := range ruleDetails.Labels {
				if strings.HasPrefix(key, "kubernetes-io-cluster-") && (val == "owned" || val == "shared") {
					clusterName = strings.TrimPrefix(key, "kubernetes-io-cluster-")
					if parts := strings.Split(clusterName, "-"); len(parts) >= 2 {
						if len(parts[len(parts)-1]) == 5 {
							clusterName = strings.Join(parts[0:len(parts)-1], "-")
						}
					}
					break
				}
			}
		}

		if clusterName == "" {
			continue
		}

		// Check if cluster exists in database
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			orphans = append(orphans, OrphanedResource{
				Type:         "GCPLoadBalancer",
				ResourceID:   rule.Name,
				ResourceName: rule.Name,
				Region:       regionName,
				Tags:         ruleDetails.Labels,
			})
		}
	}

	return orphans, nil
}

// detectOrphanedGCPServiceAccounts finds GCP service accounts with cluster labels but no matching cluster
func (j *Janitor) detectOrphanedGCPServiceAccounts(ctx context.Context, project string, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	// List all service accounts
	cmd := exec.CommandContext(ctx, "gcloud", "iam", "service-accounts", "list",
		"--project", project,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list GCP service accounts: %w", err)
	}

	var serviceAccounts []struct {
		Email       string `json:"email"`
		DisplayName string `json:"displayName"`
	}

	if err := json.Unmarshal(output, &serviceAccounts); err != nil {
		return nil, fmt.Errorf("failed to parse GCP service accounts: %w", err)
	}

	orphans := []OrphanedResource{}
	for _, sa := range serviceAccounts {
		// Check if this is an ocpctl-managed service account by naming pattern
		// Pattern: <cluster-name>-<component>@<project>.iam.gserviceaccount.com
		// or: gke-<cluster-name>-<hash>@<project>.iam.gserviceaccount.com
		email := sa.Email

		// Skip default service accounts
		if strings.HasSuffix(email, "-compute@developer.gserviceaccount.com") ||
			strings.HasPrefix(email, "service-") {
			continue
		}

		// Extract cluster name from service account email
		// This is a heuristic - ideally service accounts would have labels too
		clusterName := extractClusterNameFromGCPServiceAccount(email)
		if clusterName == "" {
			continue
		}

		// Check if cluster exists in database
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			orphans = append(orphans, OrphanedResource{
				Type:         "GCPServiceAccount",
				ResourceID:   email,
				ResourceName: sa.DisplayName,
				Region:       "global", // Service accounts are global
				Tags:         map[string]string{},
			})
		}
	}

	return orphans, nil
}

// detectOrphanedGCSBuckets finds GCS buckets with cluster labels but no matching cluster
func (j *Janitor) detectOrphanedGCSBuckets(ctx context.Context, project string, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	// List all buckets
	cmd := exec.CommandContext(ctx, "gsutil", "ls", "-p", project)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list GCS buckets: %w", err)
	}

	bucketNames := strings.Split(strings.TrimSpace(string(output)), "\n")

	orphans := []OrphanedResource{}
	for _, bucketURL := range bucketNames {
		if bucketURL == "" {
			continue
		}

		// Extract bucket name from gs:// URL
		bucketName := strings.TrimPrefix(bucketURL, "gs://")
		bucketName = strings.TrimSuffix(bucketName, "/")

		// Get bucket labels
		cmd := exec.CommandContext(ctx, "gsutil", "label", "get", bucketURL)
		output, err := cmd.Output()
		if err != nil {
			log.Printf("Warning: failed to get labels for bucket %s: %v", bucketName, err)
			continue
		}

		var labels map[string]string
		if err := json.Unmarshal(output, &labels); err != nil {
			log.Printf("Warning: failed to parse labels for bucket %s: %v", bucketName, err)
			continue
		}

		// Check for managed-by label
		if labels["managed-by"] != "ocpctl" && labels["managed_by"] != "ocpctl" {
			continue
		}

		clusterName := labels["cluster-name"]
		if clusterName == "" {
			clusterName = labels["cluster_name"] // Try underscore version
		}

		if clusterName == "" {
			log.Printf("[detectOrphanedGCSBuckets] Bucket %s has managed-by=ocpctl but no cluster-name label", bucketName)
			continue
		}

		// Check if cluster exists in database
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			orphans = append(orphans, OrphanedResource{
				Type:         "GCSBucket",
				ResourceID:   bucketName,
				ResourceName: bucketName,
				Region:       "global", // Buckets can be multi-region
				Tags:         labels,
			})
		}
	}

	return orphans, nil
}

// detectOrphanedGCPDNSZones finds Cloud DNS zones with cluster labels but no matching cluster
func (j *Janitor) detectOrphanedGCPDNSZones(ctx context.Context, project string, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	// List all DNS managed zones
	cmd := exec.CommandContext(ctx, "gcloud", "dns", "managed-zones", "list",
		"--project", project,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list GCP DNS zones: %w", err)
	}

	var zones []struct {
		Name        string            `json:"name"`
		DnsName     string            `json:"dnsName"`
		Description string            `json:"description"`
		Labels      map[string]string `json:"labels"`
	}

	if err := json.Unmarshal(output, &zones); err != nil {
		return nil, fmt.Errorf("failed to parse GCP DNS zones: %w", err)
	}

	orphans := []OrphanedResource{}
	for _, zone := range zones {
		// Check for managed-by label
		if zone.Labels["managed-by"] != "ocpctl" && zone.Labels["managed_by"] != "ocpctl" {
			continue
		}

		clusterName := zone.Labels["cluster-name"]
		if clusterName == "" {
			clusterName = zone.Labels["cluster_name"]
		}

		if clusterName == "" {
			// Try extracting from DNS name (e.g., cluster-name.domain.com.)
			dnsName := strings.TrimSuffix(zone.DnsName, ".")
			parts := strings.Split(dnsName, ".")
			if len(parts) >= 3 {
				// Assume first part is cluster name
				clusterName = parts[0]
			}
		}

		if clusterName == "" {
			log.Printf("[detectOrphanedGCPDNSZones] DNS zone %s has managed-by=ocpctl but no cluster-name", zone.Name)
			continue
		}

		// Check if cluster exists in database
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			orphans = append(orphans, OrphanedResource{
				Type:         "GCPDNSZone",
				ResourceID:   zone.Name,
				ResourceName: zone.DnsName,
				Region:       "global", // Cloud DNS is global
				Tags:         zone.Labels,
			})
		}
	}

	return orphans, nil
}

// detectOrphanedGKEClusters finds GKE clusters with labels but no matching database entry
func (j *Janitor) detectOrphanedGKEClusters(ctx context.Context, project string, clustersByName map[string]*types.Cluster) ([]OrphanedResource, error) {
	// List all GKE clusters
	cmd := exec.CommandContext(ctx, "gcloud", "container", "clusters", "list",
		"--project", project,
		"--format", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list GKE clusters: %w", err)
	}

	var gkeClusters []struct {
		Name              string            `json:"name"`
		Location          string            `json:"location"`
		ResourceLabels    map[string]string `json:"resourceLabels"`
	}

	if err := json.Unmarshal(output, &gkeClusters); err != nil {
		return nil, fmt.Errorf("failed to parse GKE clusters: %w", err)
	}

	orphans := []OrphanedResource{}
	for _, gke := range gkeClusters {
		// Check for managed-by label
		if gke.ResourceLabels["managed-by"] != "ocpctl" && gke.ResourceLabels["managed_by"] != "ocpctl" {
			continue
		}

		clusterName := gke.Name // GKE cluster name should match our cluster name

		// Check if cluster exists in database
		cluster, exists := clustersByName[clusterName]
		if !exists || cluster.Status == types.ClusterStatusDestroyed {
			orphans = append(orphans, OrphanedResource{
				Type:         "GKECluster",
				ResourceID:   gke.Name,
				ResourceName: gke.Name,
				Region:       gke.Location,
				Tags:         gke.ResourceLabels,
			})
		}
	}

	return orphans, nil
}

// Helper functions

// extractGCPRegion extracts region from zone name (e.g., us-central1-a -> us-central1)
func extractGCPRegion(zone string) string {
	parts := strings.Split(zone, "-")
	if len(parts) >= 3 {
		return strings.Join(parts[0:2], "-")
	}
	return zone
}

// extractGCPClusterName extracts cluster name from resource name and labels
func extractGCPClusterName(resourceName string, labels map[string]string) string {
	// First try cluster-name label
	if clusterName := labels["cluster-name"]; clusterName != "" {
		return clusterName
	}
	if clusterName := labels["cluster_name"]; clusterName != "" {
		return clusterName
	}

	// Try kubernetes.io-cluster label
	for key, val := range labels {
		if strings.HasPrefix(key, "kubernetes-io-cluster-") && (val == "owned" || val == "shared") {
			clusterName := strings.TrimPrefix(key, "kubernetes-io-cluster-")
			// Remove infra ID suffix if present (5-char suffix)
			if parts := strings.Split(clusterName, "-"); len(parts) >= 2 {
				if len(parts[len(parts)-1]) == 5 {
					return strings.Join(parts[0:len(parts)-1], "-")
				}
			}
			return clusterName
		}
	}

	// Try extracting from resource name
	// GKE pattern: gke-<cluster-name>-<hash>
	if strings.HasPrefix(resourceName, "gke-") {
		parts := strings.Split(resourceName, "-")
		if len(parts) >= 3 {
			// Return everything between "gke-" and the last segment (hash)
			return strings.Join(parts[1:len(parts)-1], "-")
		}
	}

	return ""
}

// extractClusterNameFromGCPServiceAccount extracts cluster name from service account email
// Patterns:
//   - gke-<cluster-name>-<hash>@<project>.iam.gserviceaccount.com -> <cluster-name>
//   - <cluster-name>-sa@<project>.iam.gserviceaccount.com -> <cluster-name>
func extractClusterNameFromGCPServiceAccount(email string) string {
	// Extract local part before @
	parts := strings.Split(email, "@")
	if len(parts) < 2 {
		return ""
	}

	localPart := parts[0]

	// Handle GKE service account pattern
	if strings.HasPrefix(localPart, "gke-") {
		parts := strings.Split(localPart, "-")
		if len(parts) >= 3 {
			// Return everything between "gke-" and the last segment (hash)
			return strings.Join(parts[1:len(parts)-1], "-")
		}
	}

	// Handle simple pattern: <cluster-name>-sa
	if strings.HasSuffix(localPart, "-sa") {
		return strings.TrimSuffix(localPart, "-sa")
	}

	// Handle pattern: <cluster-name>-<component>
	// Common components: sa, compute, storage, etc.
	components := []string{"-compute", "-storage", "-registry", "-backup"}
	for _, component := range components {
		if strings.HasSuffix(localPart, component) {
			return strings.TrimSuffix(localPart, component)
		}
	}

	return ""
}
