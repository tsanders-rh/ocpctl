package worker

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// resumeIBMCloudOpenShift resumes an IBM Cloud OpenShift IPI cluster by starting
// all of the cluster's VPC virtual server instances that were stopped during
// hibernation, then waiting for the cluster to return to a healthy state.
func (h *ResumeHandler) resumeIBMCloudOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Resuming IBM Cloud OpenShift cluster %s by starting all VSIs", cluster.Name)

	// Update cluster status to RESUMING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusResuming); err != nil {
		return fmt.Errorf("update cluster status to RESUMING: %w", err)
	}

	// Ensure artifacts are available locally (metadata.json + kubeconfig needed)
	if err := h.ensureArtifactsAvailable(ctx, cluster.ID); err != nil {
		return fmt.Errorf("ensure artifacts available: %w", err)
	}

	infraID, err := h.getInfraID(cluster)
	if err != nil {
		return fmt.Errorf("determine infraID: %w", err)
	}
	log.Printf("IBM Cloud OpenShift cluster %s uses infraID %s", cluster.Name, infraID)

	if err := ibmcloudLoginForVPC(ctx, cluster.Region); err != nil {
		return fmt.Errorf("ibmcloud login: %w", err)
	}

	vsis, err := listIBMCloudVSIs(ctx, infraID)
	if err != nil {
		return fmt.Errorf("list IBM Cloud VSIs: %w", err)
	}

	started := 0
	for _, vsi := range vsis {
		if vsi.Status == "running" {
			log.Printf("VSI %s already running, skipping", vsi.Name)
			continue
		}
		log.Printf("Starting VSI %s (%s)", vsi.Name, vsi.ID)
		if err := ibmcloudVSIAction(ctx, "instance-start", vsi.ID); err != nil {
			return fmt.Errorf("start VSI %s: %w", vsi.Name, err)
		}
		started++
	}

	if started == 0 && len(vsis) == 0 {
		return fmt.Errorf("no VSIs found for infraID %s - cannot resume", infraID)
	}

	// Best-effort wait for VSIs to reach running state (non-fatal); the
	// cluster-health wait below is the authoritative readiness gate.
	if err := waitForIBMCloudVSIStatus(ctx, infraID, "running", 10*time.Minute); err != nil {
		log.Printf("Warning: not all VSIs confirmed running for cluster %s: %v", cluster.Name, err)
	}

	// Wait for the cluster to come back to a healthy state.
	if err := h.waitForOpenShiftClusterHealth(ctx, cluster); err != nil {
		return fmt.Errorf("wait for cluster health: %w", err)
	}

	// Update cluster status to READY
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status to READY: %w", err)
	}

	log.Printf("IBM Cloud OpenShift cluster %s resumed successfully (started %d VSIs)",
		cluster.Name, started)

	return nil
}
