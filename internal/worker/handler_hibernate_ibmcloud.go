package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// ibmVSI is a trimmed view of an IBM Cloud VPC virtual server instance as
// returned by `ibmcloud is instances --output json`.
type ibmVSI struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// hibernateIBMCloudOpenShift hibernates an IBM Cloud OpenShift IPI cluster by
// stopping all of the cluster's VPC virtual server instances. openshift-install
// names every VSI with the cluster's infraID as a prefix (<infraID>-*), so we
// discover the cluster's VSIs by that prefix. Stopped VSIs incur no compute
// charges and are started again on resume.
func (h *HibernateHandler) hibernateIBMCloudOpenShift(ctx context.Context, cluster *types.Cluster, job *types.Job) error {
	log.Printf("Hibernating IBM Cloud OpenShift cluster %s by stopping all VSIs", cluster.Name)

	// Update cluster status to HIBERNATING
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernating); err != nil {
		return fmt.Errorf("update cluster status to HIBERNATING: %w", err)
	}

	// Ensure artifacts are available locally (metadata.json needed for infraID)
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

	// Best-effort cleanup of ephemeral addon namespaces before shutting VSIs down.
	h.cleanupEphemeralAddonNamespaces(ctx, cluster)

	vsis, err := listIBMCloudVSIs(ctx, infraID)
	if err != nil {
		return fmt.Errorf("list IBM Cloud VSIs: %w", err)
	}

	stopped := 0
	for _, vsi := range vsis {
		if vsi.Status != "running" {
			log.Printf("Skipping VSI %s (status %q)", vsi.Name, vsi.Status)
			continue
		}
		log.Printf("Stopping VSI %s (%s)", vsi.Name, vsi.ID)
		if err := ibmcloudVSIAction(ctx, "instance-stop", vsi.ID); err != nil {
			return fmt.Errorf("stop VSI %s: %w", vsi.Name, err)
		}
		stopped++
	}

	if stopped == 0 {
		log.Printf("Warning: no running VSIs found for cluster %s, may already be hibernated", cluster.Name)
	} else {
		// Best-effort wait for VSIs to reach stopped state (non-fatal).
		if err := waitForIBMCloudVSIStatus(ctx, infraID, "stopped", 10*time.Minute); err != nil {
			log.Printf("Warning: not all VSIs confirmed stopped for cluster %s: %v", cluster.Name, err)
		}
	}

	// Record hibernation metadata for resume/visibility.
	if job.Metadata == nil {
		job.Metadata = make(types.JobMetadata)
	}
	job.Metadata["ibmcloud_infra_id"] = infraID
	job.Metadata["ibmcloud_vsi_count"] = fmt.Sprintf("%d", stopped)

	// Update cluster status to HIBERNATED
	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusHibernated); err != nil {
		return fmt.Errorf("update cluster status to HIBERNATED: %w", err)
	}

	log.Printf("IBM Cloud OpenShift cluster %s hibernated successfully (stopped %d VSIs)",
		cluster.Name, stopped)

	return nil
}

// ibmcloudLoginForVPC logs the ibmcloud CLI into the given region using the
// IBMCLOUD_API_KEY environment variable, targeting the VPC infrastructure
// generation-2 service.
func ibmcloudLoginForVPC(ctx context.Context, region string) error {
	apiKey := os.Getenv("IBMCLOUD_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("IBMCLOUD_API_KEY is not set")
	}
	if region == "" {
		region = "us-south"
	}

	cmd := exec.CommandContext(ctx, "ibmcloud", "login",
		"--apikey", apiKey, "-r", region, "-q")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ibmcloud login: %w: %s", err, strings.TrimSpace(string(output)))
	}

	// Target VPC infrastructure gen-2 so `ibmcloud is` commands work.
	cmd = exec.CommandContext(ctx, "ibmcloud", "is", "target", "--gen", "2")
	// Older CLIs default to gen 2 and lack this subcommand; ignore failures.
	if err := cmd.Run(); err != nil {
		log.Printf("Note: `ibmcloud is target --gen 2` not applied (%v); assuming gen-2 default", err)
	}
	return nil
}

// listIBMCloudVSIs returns all VPC virtual server instances whose name begins
// with "<infraID>-", i.e. the instances that belong to this cluster.
func listIBMCloudVSIs(ctx context.Context, infraID string) ([]ibmVSI, error) {
	cmd := exec.CommandContext(ctx, "ibmcloud", "is", "instances", "--output", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ibmcloud is instances: %w", err)
	}

	var all []ibmVSI
	if err := json.Unmarshal(output, &all); err != nil {
		return nil, fmt.Errorf("parse instances JSON: %w", err)
	}

	prefix := infraID + "-"
	var matched []ibmVSI
	for _, vsi := range all {
		if strings.HasPrefix(vsi.Name, prefix) {
			matched = append(matched, vsi)
		}
	}
	return matched, nil
}

// ibmcloudVSIAction runs a power action ("instance-stop" or "instance-start")
// against a single VSI by ID. Stop requires --force to avoid an interactive
// confirmation prompt.
func ibmcloudVSIAction(ctx context.Context, action, vsiID string) error {
	args := []string{"is", action, vsiID, "--output", "json"}
	if action == "instance-stop" {
		args = append(args, "--force")
	}
	cmd := exec.CommandContext(ctx, "ibmcloud", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ibmcloud %s: %w: %s", action, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// waitForIBMCloudVSIStatus polls until every cluster VSI reaches targetStatus or
// the timeout elapses.
func waitForIBMCloudVSIStatus(ctx context.Context, infraID, targetStatus string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		vsis, err := listIBMCloudVSIs(ctx, infraID)
		if err != nil {
			return fmt.Errorf("list VSIs while waiting: %w", err)
		}

		allMatch := true
		for _, vsi := range vsis {
			if vsi.Status != targetStatus {
				allMatch = false
				break
			}
		}
		if allMatch {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for VSIs to reach status %q", targetStatus)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for VSI status %q", targetStatus)
		case <-time.After(15 * time.Second):
		}
	}
}
