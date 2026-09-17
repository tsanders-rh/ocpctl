package worker

import (
	"context"
	"fmt"
	"log"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/lifecycle"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// handleBareMetalDestroy tears down an agent-based bare-metal cluster natively by
// reclaiming its AWS substrate by tag (substrate.Teardown): terminating the host
// takes the libvirt VMs, sushy-tools BMCs and haproxy with it, and the security
// group, Elastic IP, keypair and Route53 records are removed. Teardown is
// idempotent, so it runs even for a create that failed partway (reclaiming any
// tagged resources it leaked). There is no metadata.json or openshift-install
// destroy involved.
func (h *DestroyHandler) handleBareMetalDestroy(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting bare-metal cluster destruction for %s", cluster.Name)

	baseDomain := ""
	if cluster.BaseDomain != nil {
		baseDomain = *cluster.BaseDomain
	}

	log.Printf("Running native bare-metal teardown for %s", cluster.Name)
	if err := lifecycle.Destroy(ctx, lifecycle.DestroyInput{
		Region:      cluster.Region,
		ClusterName: cluster.Name,
		BaseDomain:  baseDomain,
	}); err != nil {
		log.Printf("ERROR: bare-metal teardown failed for %s: %v", cluster.Name, err)
		if uerr := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyFailed); uerr != nil {
			return fmt.Errorf("mark cluster destroy failed: %w", uerr)
		}
		return fmt.Errorf("baremetal teardown: %w", err)
	}

	log.Printf("Bare-metal cluster %s destroyed successfully", cluster.Name)
	return h.finishBareMetalDestroy(ctx, cluster)
}

// finishBareMetalDestroy deletes any stored artifacts and marks the cluster
// DESTROYED.
func (h *DestroyHandler) finishBareMetalDestroy(ctx context.Context, cluster *types.Cluster) error {
	if artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName); err != nil {
		log.Printf("Warning: failed to create artifact storage client: %v", err)
	} else if err := artifactStorage.DeleteClusterArtifacts(ctx, cluster.ID); err != nil {
		log.Printf("Warning: failed to delete S3 artifacts: %v", err)
	}

	if err := h.store.Clusters.MarkDestroyed(ctx, cluster.ID); err != nil {
		return fmt.Errorf("mark cluster destroyed: %w", err)
	}
	log.Printf("Cluster %s is now DESTROYED", cluster.Name)
	return nil
}
