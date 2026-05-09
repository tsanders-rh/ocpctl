package worker

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// handleARODestroy destroys an ARO cluster
func (h *DestroyHandler) handleARODestroy(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("[JOB %s] Starting ARO cluster destruction: %s", job.ID, cluster.Name)

	// Update cluster status to DESTROYING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroying); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Get work directory
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)

	// Create ARO installer
	aroInstaller := installer.NewAROInstaller()

	// Try to load metadata to get resource group
	metadata, err := aroInstaller.LoadMetadata(workDir)
	if err != nil {
		// If metadata not available, construct resource group name from cluster name
		log.Printf("[JOB %s] Warning: failed to load metadata, constructing resource group name", job.ID)
		metadata = map[string]string{
			"resource_group": fmt.Sprintf("ocpctl-%s-rg", cluster.Name),
		}
	}

	resourceGroup := metadata["resource_group"]

	// Delete ARO cluster
	log.Printf("[JOB %s] Deleting ARO cluster from resource group: %s", job.ID, resourceGroup)
	output, err := aroInstaller.DestroyCluster(ctx, resourceGroup, cluster.Name)
	if err != nil {
		log.Printf("[JOB %s] ARO cluster deletion failed: %v", job.ID, err)
		log.Printf("[JOB %s] Output: %s", job.ID, output)
		return fmt.Errorf("destroy ARO cluster: %w", err)
	}

	// Delete resource group (this removes all associated resources)
	log.Printf("[JOB %s] Deleting resource group: %s", job.ID, resourceGroup)
	if err := aroInstaller.DeleteResourceGroup(ctx, resourceGroup); err != nil {
		log.Printf("[JOB %s] Resource group deletion failed: %v", job.ID, err)
		return fmt.Errorf("delete resource group: %w", err)
	}

	// Update cluster status to DESTROYED
	if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
		return fmt.Errorf("mark cluster destroyed: %w", err)
	}

	log.Printf("[JOB %s] ARO cluster destruction completed successfully", job.ID)
	return nil
}

// handleAKSDestroy destroys an AKS cluster
func (h *DestroyHandler) handleAKSDestroy(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("[JOB %s] Starting AKS cluster destruction: %s", job.ID, cluster.Name)

	// Update cluster status to DESTROYING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroying); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	// Get work directory
	workDir := filepath.Join(h.config.WorkDir, cluster.ID)

	// Create AKS installer
	aksInstaller := installer.NewAKSInstaller()

	// Create ARO installer for resource group operations
	aroInstaller := installer.NewAROInstaller()

	// Try to load metadata to get resource group
	metadata, err := aroInstaller.LoadMetadata(workDir)
	if err != nil {
		log.Printf("[JOB %s] Warning: failed to load metadata, constructing resource group name", job.ID)
		metadata = map[string]string{
			"resource_group": fmt.Sprintf("ocpctl-%s-rg", cluster.Name),
		}
	}

	resourceGroup := metadata["resource_group"]

	// Delete AKS cluster
	log.Printf("[JOB %s] Deleting AKS cluster from resource group: %s", job.ID, resourceGroup)
	output, err := aksInstaller.DestroyCluster(ctx, resourceGroup, cluster.Name)
	if err != nil {
		log.Printf("[JOB %s] AKS cluster deletion failed: %v", job.ID, err)
		log.Printf("[JOB %s] Output: %s", job.ID, output)
		return fmt.Errorf("destroy AKS cluster: %w", err)
	}

	// Delete resource group
	log.Printf("[JOB %s] Deleting resource group: %s", job.ID, resourceGroup)
	if err := aroInstaller.DeleteResourceGroup(ctx, resourceGroup); err != nil {
		log.Printf("[JOB %s] Resource group deletion failed: %v", job.ID, err)
		return fmt.Errorf("delete resource group: %w", err)
	}

	// Update cluster status to DESTROYED
	if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
		return fmt.Errorf("mark cluster destroyed: %w", err)
	}

	log.Printf("[JOB %s] AKS cluster destruction completed successfully", job.ID)
	return nil
}
