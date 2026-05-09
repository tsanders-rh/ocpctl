package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// hibernateARO hibernates an ARO cluster by scaling worker MachineSets to 0
func (h *HibernateHandler) hibernateARO(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Hibernating ARO cluster %s by scaling MachineSets to 0", cluster.Name)

	// Update cluster status to HIBERNATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernating); err != nil {
		return fmt.Errorf("update cluster status to HIBERNATING: %w", err)
	}

	// Ensure artifacts are available locally (kubeconfig needed)
	if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
		return fmt.Errorf("ensure artifacts available: %w", err)
	}

	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

	// List all MachineSets in openshift-machine-api namespace
	cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
		"get", "machineset", "-n", "openshift-machine-api",
		"-o", "jsonpath={.items[*].metadata.name}")

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("list machinesets: %w", err)
	}

	machineSets := strings.Fields(string(output))
	if len(machineSets) == 0 {
		return fmt.Errorf("no machinesets found")
	}

	// Save original replica counts to job metadata
	replicaCounts := make(map[string]int)
	totalOriginalReplicas := 0

	for _, ms := range machineSets {
		// Get current replica count
		cmd = exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
			"get", "machineset", ms, "-n", "openshift-machine-api",
			"-o", "jsonpath={.spec.replicas}")

		output, err := cmd.Output()
		if err != nil {
			log.Printf("Warning: failed to get replicas for machineset %s: %v", ms, err)
			continue
		}

		var replicas int
		if _, err := fmt.Sscanf(string(output), "%d", &replicas); err != nil {
			log.Printf("Warning: failed to parse replicas for machineset %s: %v", ms, err)
			continue
		}

		// Skip if already at 0
		if replicas == 0 {
			log.Printf("Skipping machineset %s (already at 0 replicas)", ms)
			continue
		}

		replicaCounts[ms] = replicas
		totalOriginalReplicas += replicas

		// Scale to 0
		log.Printf("Scaling machineset %s from %d to 0 replicas", ms, replicas)
		cmd = exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
			"scale", "machineset", ms, "--replicas=0",
			"-n", "openshift-machine-api")

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("scale machineset %s: %w", ms, err)
		}

		log.Printf("Successfully scaled machineset %s to 0 replicas", ms)
	}

	// Save replica counts to job metadata for resume
	configsJSON, err := json.Marshal(replicaCounts)
	if err != nil {
		return fmt.Errorf("marshal replica counts: %w", err)
	}

	if job.Metadata == nil {
		job.Metadata = make(types.JobMetadata)
	}
	job.Metadata["aro_replica_counts"] = string(configsJSON)
	job.Metadata["total_original_replicas"] = fmt.Sprintf("%d", totalOriginalReplicas)

	log.Printf("Stored original machineset configurations in job metadata (total: %d replicas)", totalOriginalReplicas)

	// Update cluster status to HIBERNATED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
		return fmt.Errorf("update cluster status to HIBERNATED: %w", err)
	}

	log.Printf("ARO cluster %s hibernated successfully (scaled %d replicas to 0 across %d machinesets)",
		cluster.Name, totalOriginalReplicas, len(replicaCounts))

	return nil
}

// hibernateAKS hibernates an AKS cluster by scaling all node pools to 0
func (h *HibernateHandler) hibernateAKS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Hibernating AKS cluster %s by scaling node pools to 0", cluster.Name)

	// Update cluster status to HIBERNATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernating); err != nil {
		return fmt.Errorf("update cluster status to HIBERNATING: %w", err)
	}

	// Ensure artifacts are available locally to get resource group from metadata
	if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
		return fmt.Errorf("ensure artifacts available: %w", err)
	}

	// Get work directory
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)

	// Load metadata to get resource group
	aroInstaller := installer.NewAROInstaller()
	metadata, err := aroInstaller.LoadMetadata(workDir)
	if err != nil {
		// If metadata not available, construct resource group name from cluster name
		log.Printf("Warning: failed to load metadata, constructing resource group name")
		metadata = map[string]string{
			"resource_group": fmt.Sprintf("ocpctl-%s-rg", cluster.Name),
		}
	}

	resourceGroup := metadata["resource_group"]

	// Create AKS installer
	aksInstaller := installer.NewAKSInstaller()

	// List all node pools
	nodePools, err := aksInstaller.ListNodePools(ctx, resourceGroup, cluster.Name)
	if err != nil {
		return fmt.Errorf("list node pools: %w", err)
	}

	if len(nodePools) == 0 {
		log.Printf("Warning: no node pools found for cluster %s, may already be hibernated", cluster.Name)
		// Update cluster status to HIBERNATED anyway
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
	}

	// Get current node pool sizes using Azure CLI
	cmd := exec.CommandContext(ctx, "az", "aks", "nodepool", "list",
		"--resource-group", resourceGroup,
		"--cluster-name", cluster.Name,
		"--query", "[].{name:name,count:count}",
		"-o", "json")

	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("get node pool details: %w", err)
	}

	var poolDetails []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	if err := json.Unmarshal(output, &poolDetails); err != nil {
		return fmt.Errorf("parse node pool details: %w", err)
	}

	// Save original node counts to job metadata
	nodeCounts := make(map[string]int)
	totalOriginalCount := 0

	for _, pool := range poolDetails {
		// Skip if already at 0
		if pool.Count == 0 {
			log.Printf("Skipping node pool %s (already at 0 nodes)", pool.Name)
			continue
		}

		nodeCounts[pool.Name] = pool.Count
		totalOriginalCount += pool.Count

		// Scale node pool to 0
		log.Printf("Scaling node pool %s from %d to 0 nodes", pool.Name, pool.Count)
		if err := aksInstaller.ScaleNodePool(ctx, resourceGroup, cluster.Name, pool.Name, 0); err != nil {
			return fmt.Errorf("scale node pool %s: %w", pool.Name, err)
		}

		log.Printf("Successfully scaled node pool %s to 0 nodes", pool.Name)
	}

	// Save node counts to job metadata for resume
	countsJSON, err := json.Marshal(nodeCounts)
	if err != nil {
		return fmt.Errorf("marshal node counts: %w", err)
	}

	if job.Metadata == nil {
		job.Metadata = make(types.JobMetadata)
	}
	job.Metadata["aks_node_counts"] = string(countsJSON)
	job.Metadata["total_original_count"] = fmt.Sprintf("%d", totalOriginalCount)

	log.Printf("Stored original node pool configurations in job metadata (total: %d nodes)", totalOriginalCount)

	// Update cluster status to HIBERNATED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
		return fmt.Errorf("update cluster status to HIBERNATED: %w", err)
	}

	log.Printf("AKS cluster %s hibernated successfully (scaled %d nodes to 0 across %d node pools)",
		cluster.Name, totalOriginalCount, len(nodeCounts))

	return nil
}
