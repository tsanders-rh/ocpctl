package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"

	"github.com/tsanders-rh/ocpctl/internal/installer"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// resumeARO resumes an ARO cluster by restoring MachineSets to original replica counts
func (h *ResumeHandler) resumeARO(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming ARO cluster %s by restoring MachineSet replicas", cluster.Name)

	// Update cluster status to RESUMING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusResuming); err != nil {
		return fmt.Errorf("update cluster status to RESUMING: %w", err)
	}

	// Get the hibernate job to retrieve original replica counts
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}
	if len(hibernateJobs) == 0 {
		return fmt.Errorf("no hibernate job found for cluster")
	}

	// Get the most recent successful hibernate job
	var hibernateJob *types.Job
	for i := len(hibernateJobs) - 1; i >= 0; i-- {
		if hibernateJobs[i].Status == types.JobStatusSucceeded {
			hibernateJob = hibernateJobs[i]
			break
		}
	}
	if hibernateJob == nil {
		return fmt.Errorf("no successful hibernate job found")
	}

	if hibernateJob.Metadata == nil {
		return fmt.Errorf("hibernate job missing metadata - cannot restore replica counts")
	}

	// Parse original replica counts from hibernate job metadata
	replicaCountsJSON, ok := hibernateJob.Metadata["aro_replica_counts"].(string)
	if !ok {
		return fmt.Errorf("hibernate job metadata missing aro_replica_counts")
	}

	var replicaCounts map[string]int
	if err := json.Unmarshal([]byte(replicaCountsJSON), &replicaCounts); err != nil {
		return fmt.Errorf("parse replica counts: %w", err)
	}

	if len(replicaCounts) == 0 {
		log.Printf("Warning: no machinesets to restore for cluster %s", cluster.Name)
		// Update cluster status to READY anyway
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
	}

	// Ensure artifacts are available locally (kubeconfig needed)
	if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
		return fmt.Errorf("ensure artifacts available: %w", err)
	}

	workDir := filepath.Join(h.config.WorkDir, cluster.ID)
	kubeconfigPath := filepath.Join(workDir, "auth", "kubeconfig")

	// Restore each MachineSet to its original replica count
	totalRestoredReplicas := 0
	restoredMachineSets := 0

	for msName, replicas := range replicaCounts {
		log.Printf("Restoring machineset %s to %d replicas", msName, replicas)

		cmd := exec.CommandContext(ctx, "oc", "--kubeconfig", kubeconfigPath,
			"scale", "machineset", msName,
			fmt.Sprintf("--replicas=%d", replicas),
			"-n", "openshift-machine-api")

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("scale machineset %s to %d: %w", msName, replicas, err)
		}

		totalRestoredReplicas += replicas
		restoredMachineSets++

		log.Printf("Successfully restored machineset %s to %d replicas", msName, replicas)
	}

	log.Printf("Restored %d machinesets with %d total replicas", restoredMachineSets, totalRestoredReplicas)

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status to READY: %w", err)
	}

	log.Printf("ARO cluster %s resumed successfully (%d replicas restored across %d machinesets)",
		cluster.Name, totalRestoredReplicas, restoredMachineSets)

	return nil
}

// resumeAKS resumes an AKS cluster by scaling node pools back to original sizes
func (h *ResumeHandler) resumeAKS(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming AKS cluster %s by restoring node pool sizes", cluster.Name)

	// Update cluster status to RESUMING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusResuming); err != nil {
		return fmt.Errorf("update cluster status to RESUMING: %w", err)
	}

	// Get the hibernate job to retrieve original node counts
	hibernateJobs, err := h.store.Jobs.GetByClusterIDAndType(ctx, cluster.ID, types.JobTypeHibernate)
	if err != nil {
		return fmt.Errorf("get hibernate jobs: %w", err)
	}
	if len(hibernateJobs) == 0 {
		return fmt.Errorf("no hibernate job found for cluster")
	}

	// Get the most recent successful hibernate job
	var hibernateJob *types.Job
	for i := len(hibernateJobs) - 1; i >= 0; i-- {
		if hibernateJobs[i].Status == types.JobStatusSucceeded {
			hibernateJob = hibernateJobs[i]
			break
		}
	}
	if hibernateJob == nil {
		return fmt.Errorf("no successful hibernate job found")
	}

	if hibernateJob.Metadata == nil {
		return fmt.Errorf("hibernate job missing metadata - cannot restore node counts")
	}

	// Parse original node counts from hibernate job metadata
	nodeCountsJSON, ok := hibernateJob.Metadata["aks_node_counts"].(string)
	if !ok {
		return fmt.Errorf("hibernate job metadata missing aks_node_counts")
	}

	var nodeCounts map[string]int
	if err := json.Unmarshal([]byte(nodeCountsJSON), &nodeCounts); err != nil {
		return fmt.Errorf("parse node counts: %w", err)
	}

	if len(nodeCounts) == 0 {
		log.Printf("Warning: no node pools to restore for cluster %s", cluster.Name)
		// Update cluster status to READY anyway
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
			return fmt.Errorf("update cluster status: %w", err)
		}
		return nil
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

	// Restore each node pool to its original size
	totalRestoredNodes := 0
	restoredPools := 0

	for poolName, count := range nodeCounts {
		log.Printf("Restoring node pool %s to %d nodes", poolName, count)

		if err := aksInstaller.ScaleNodePool(ctx, resourceGroup, cluster.Name, poolName, count); err != nil {
			return fmt.Errorf("scale node pool %s to %d: %w", poolName, count, err)
		}

		totalRestoredNodes += count
		restoredPools++

		log.Printf("Successfully restored node pool %s to %d nodes", poolName, count)
	}

	log.Printf("Restored %d node pools with %d total nodes", restoredPools, totalRestoredNodes)

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status to READY: %w", err)
	}

	log.Printf("AKS cluster %s resumed successfully (%d nodes restored across %d node pools)",
		cluster.Name, totalRestoredNodes, restoredPools)

	return nil
}
