package janitor

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tsanders-rh/ocpctl/internal/orphan"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// autoRemediateOrphans deletes ACTIVE orphaned resources that pass the shared
// safety gate (internal/orphan.Evaluate). It is a no-op unless auto-remediation
// is enabled (ORPHAN_AUTO_DELETE). In dry-run mode it logs/audits exactly what
// it would delete without deleting. Callers gate on config.OrphanAutoDelete.
func (j *Janitor) autoRemediateOrphans(ctx context.Context) error {
	mode := j.config.OrphanAutoDelete
	if mode == orphan.AutoDeleteOff {
		return nil
	}
	dryRun := mode == orphan.AutoDeleteDryRun

	active := types.OrphanedResourceStatusActive
	resources, _, err := j.stores.orphaned.List(ctx, store.OrphanedResourceFilters{
		Status: &active,
		Limit:  1000,
	})
	if err != nil {
		return fmt.Errorf("list active orphaned resources: %w", err)
	}
	if len(resources) == 0 {
		return nil
	}

	gcpProject := os.Getenv("GCP_PROJECT")
	maxPerCycle := j.config.OrphanAutoDeleteMaxPerCycle
	if maxPerCycle <= 0 {
		maxPerCycle = orphan.DefaultAutoDeleteMaxPerCycle
	}

	deleter := j.orphanDeleter
	if deleter == nil {
		deleter = orphan.DeleteResource
	}

	log.Printf("[orphan-auto-delete] mode=%s evaluating %d active orphaned resource(s) (max deletions/cycle=%d)",
		mode, len(resources), maxPerCycle)

	realDeleted := 0
	for _, res := range resources {
		// Cap only real deletions; dry-run keeps evaluating so operators see the
		// full would-delete set.
		if !dryRun && realDeleted >= maxPerCycle {
			log.Printf("[orphan-auto-delete] reached per-cycle cap of %d; remaining resources deferred to next cycle", maxPerCycle)
			break
		}

		verdict := orphan.Evaluate(ctx, res, j.config.OrphanSafetyConfig, time.Now(), j.clusterLookup, j.vpcInspector)
		if !verdict.Safe {
			log.Printf("[orphan-auto-delete] SKIP %s %s (%s): %s",
				res.ResourceType, res.ResourceID, res.Region, strings.Join(verdict.BlockReasons, "; "))
			continue
		}

		if dryRun {
			log.Printf("[orphan-auto-delete][DRYRUN] would delete %s %s (%s) name=%q cluster=%q",
				res.ResourceType, res.ResourceID, res.Region, res.ResourceName, res.ClusterName)
			j.auditAutoDelete(ctx, res, verdict, true, nil)
			continue
		}

		// Bounded per-resource timeout; VPC teardown can take several minutes.
		delCtx, cancel := context.WithTimeout(ctx, 6*time.Minute)
		delErr := deleter(delCtx, res, orphan.DeleteOptions{GCPProject: gcpProject})
		cancel()

		if delErr != nil {
			// Unsupported types / missing GCP project aren't failures worth
			// alerting on -- just skip them (no audit noise).
			if errors.Is(delErr, orphan.ErrUnsupportedResourceType) || errors.Is(delErr, orphan.ErrGCPProjectRequired) {
				log.Printf("[orphan-auto-delete] SKIP %s %s: %v", res.ResourceType, res.ResourceID, delErr)
				continue
			}
			log.Printf("[orphan-auto-delete] FAILED to delete %s %s (%s): %v",
				res.ResourceType, res.ResourceID, res.Region, delErr)
			j.auditAutoDelete(ctx, res, verdict, false, delErr)
			continue
		}

		notes := "Auto-deleted by janitor orphan auto-remediation (issue #101 phase 3)"
		if err := j.stores.orphaned.MarkResolved(ctx, res.ID, "janitor", notes); err != nil {
			log.Printf("[orphan-auto-delete] deleted %s %s but failed to mark resolved: %v",
				res.ResourceType, res.ResourceID, err)
		}
		j.auditAutoDelete(ctx, res, verdict, false, nil)
		realDeleted++
		log.Printf("[orphan-auto-delete] deleted %s %s (%s) name=%q cluster=%q",
			res.ResourceType, res.ResourceID, res.Region, res.ResourceName, res.ClusterName)
	}

	if dryRun {
		log.Printf("[orphan-auto-delete] dry-run cycle complete")
	} else if realDeleted > 0 {
		log.Printf("[orphan-auto-delete] cycle complete: deleted %d orphaned resource(s)", realDeleted)
	}
	return nil
}

// auditAutoDelete records an audit event for an auto-remediation action.
// Fire-and-forget: audit failures never interrupt remediation.
func (j *Janitor) auditAutoDelete(ctx context.Context, res *types.OrphanedResource, verdict orphan.Verdict, dryRun bool, deleteErr error) {
	if j.stores.audit == nil {
		return
	}

	action := "orphaned_resource.auto_deleted"
	if dryRun {
		action = "orphaned_resource.auto_delete_dryrun"
	}
	status := types.AuditEventStatusSuccess
	if deleteErr != nil {
		status = types.AuditEventStatusFailure
	}

	metadata := types.JobMetadata{
		"resource_id":   res.ID,
		"resource_type": string(res.ResourceType),
		"aws_resource":  res.ResourceID,
		"region":        res.Region,
		"cluster_name":  res.ClusterName,
		"dry_run":       dryRun,
	}
	if verdict.SourceClusterStatus != nil {
		metadata["source_cluster_status"] = *verdict.SourceClusterStatus
	}
	if deleteErr != nil {
		metadata["error"] = deleteErr.Error()
	}

	event := &types.AuditEvent{
		ID:              uuid.New().String(),
		Actor:           "janitor",
		Action:          action,
		TargetClusterID: res.ClusterID,
		TargetJobID:     res.JobID,
		Status:          status,
		Metadata:        metadata,
		CreatedAt:       time.Now(),
	}
	_ = j.stores.audit.Log(ctx, event)
}
