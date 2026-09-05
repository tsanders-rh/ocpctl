package janitor

import (
	"context"
	"encoding/json"
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

// autoRemediateListLimit bounds how many ACTIVE orphaned resources a single
// auto-remediation cycle evaluates. The safety gate runs a live VPC-alive probe
// per VPC-type resource, so this caps per-cycle AWS API cost. When the true
// ACTIVE backlog exceeds this, the status row reports TotalActive + Truncated so
// operators aren't misled into thinking Evaluated/WouldDelete cover everything.
const autoRemediateListLimit = 1000

// autoRemediateOrphans deletes ACTIVE orphaned resources that pass the shared
// safety gate (internal/orphan.Evaluate). It is a no-op unless auto-remediation
// is enabled. The effective mode/cap come from the DB (admin console) when set,
// otherwise the ORPHAN_AUTO_DELETE* env bootstrap defaults -- see
// effectiveAutoRemediation. In dry-run mode it logs/audits exactly what it would
// delete without deleting. At the end of every enabled pass it writes a per-cycle
// telemetry row so the console can show what actually happened.
func (j *Janitor) autoRemediateOrphans(ctx context.Context) error {
	mode, maxPerCycle := j.effectiveAutoRemediation(ctx)
	if mode == orphan.AutoDeleteOff {
		return nil
	}
	dryRun := mode == orphan.AutoDeleteDryRun

	active := types.OrphanedResourceStatusActive
	resources, totalActive, err := j.stores.orphaned.List(ctx, store.OrphanedResourceFilters{
		Status: &active,
		Limit:  autoRemediateListLimit,
	})
	if err != nil {
		return fmt.Errorf("list active orphaned resources: %w", err)
	}

	gcpProject := os.Getenv("GCP_PROJECT")
	if maxPerCycle <= 0 {
		maxPerCycle = orphan.DefaultAutoDeleteMaxPerCycle
	}

	deleter := j.orphanDeleter
	if deleter == nil {
		deleter = orphan.DeleteResource
	}

	log.Printf("[orphan-auto-delete] mode=%s evaluating %d of %d active orphaned resource(s) (max deletions/cycle=%d)",
		mode, len(resources), totalActive, maxPerCycle)
	if totalActive > len(resources) {
		log.Printf("[orphan-auto-delete] backlog %d exceeds per-cycle listing limit %d; evaluating most-recently-detected slice only",
			totalActive, autoRemediateListLimit)
	}

	// Per-cycle telemetry, persisted at the end for the admin console.
	status := types.OrphanAutoRemediationStatus{
		LastRunAt:   time.Now(),
		Mode:        string(mode),
		TotalActive: totalActive,
		// True when the backlog exceeds what this cycle could list, so the console
		// shows "evaluated N of M" instead of implying full coverage.
		Truncated: totalActive > len(resources),
	}

	for _, res := range resources {
		// Cap only real deletions; dry-run keeps evaluating so operators see the
		// full would-delete set.
		if !dryRun && status.Deleted >= maxPerCycle {
			status.Capped = true
			log.Printf("[orphan-auto-delete] reached per-cycle cap of %d; remaining resources deferred to next cycle", maxPerCycle)
			break
		}
		status.Evaluated++

		// Ownership guard: auto-remediation deletes ONLY resources that carry a
		// verified ocpctl ownership tag. Detection is not uniformly tag-gated --
		// several paths flag resources by heuristic (CloudWatch log groups by
		// /aws/eks/ prefix, DNS records/zones by generic ClusterName/Profile tags,
		// GCP service accounts by email, by-infraID IAM roles by name prefix, EC2
		// nodes by the kubernetes.io/cluster tag). Those matches lack the ocpctl
		// ownership tag, so they are recorded for visibility/manual cleanup but are
		// never auto-deleted here, even if they pass the safety gate. Manual admin
		// force-delete via the API is unaffected.
		if !hasOcpctlOwnershipTag(res.Tags) {
			status.SkippedUnowned++
			log.Printf("[orphan-auto-delete] SKIP %s %s (%s): no verified ocpctl ownership tag (heuristic match); manual delete only",
				res.ResourceType, res.ResourceID, res.Region)
			continue
		}

		verdict := orphan.Evaluate(ctx, res, j.config.OrphanSafetyConfig, time.Now(), j.clusterLookup, j.vpcInspector)
		if !verdict.Safe {
			status.SkippedUnsafe++
			log.Printf("[orphan-auto-delete] SKIP %s %s (%s): %s",
				res.ResourceType, res.ResourceID, res.Region, strings.Join(verdict.BlockReasons, "; "))
			continue
		}
		// Passed every gate: this is a would-delete (dry-run) or a deletion target.
		status.WouldDelete++

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
			status.Failed++
			log.Printf("[orphan-auto-delete] FAILED to delete %s %s (%s): %v",
				res.ResourceType, res.ResourceID, res.Region, delErr)
			j.auditAutoDelete(ctx, res, verdict, false, delErr)
			continue
		}

		notes := "Auto-deleted by janitor orphan auto-remediation (issue #101)"
		if err := j.stores.orphaned.MarkResolved(ctx, res.ID, "janitor", notes); err != nil {
			log.Printf("[orphan-auto-delete] deleted %s %s but failed to mark resolved: %v",
				res.ResourceType, res.ResourceID, err)
		}
		j.auditAutoDelete(ctx, res, verdict, false, nil)
		status.Deleted++
		log.Printf("[orphan-auto-delete] deleted %s %s (%s) name=%q cluster=%q",
			res.ResourceType, res.ResourceID, res.Region, res.ResourceName, res.ClusterName)
	}

	if dryRun {
		log.Printf("[orphan-auto-delete] dry-run cycle complete: %d would-delete, %d skipped-unsafe, %d skipped-unowned",
			status.WouldDelete, status.SkippedUnsafe, status.SkippedUnowned)
	} else {
		log.Printf("[orphan-auto-delete] cycle complete: deleted %d, failed %d, skipped-unsafe %d, skipped-unowned %d",
			status.Deleted, status.Failed, status.SkippedUnsafe, status.SkippedUnowned)
	}

	j.writeRemediationStatus(ctx, status)
	return nil
}

// effectiveAutoRemediation resolves the mode and per-cycle cap for this pass and
// applies the environment interlock. The DB setting (admin console) wins when
// present and valid; otherwise it falls back to the janitor's env-derived config.
// Finally, real deletion ("on") is clamped to "dryrun" unless this environment
// has explicitly opted in via ORPHAN_AUTO_DELETE_ALLOW_ON -- defense-in-depth so
// an env sharing a cloud account with another ocpctl deployment (e.g. dev sharing
// prod's AWS account) can never delete, regardless of DB or env config.
func (j *Janitor) effectiveAutoRemediation(ctx context.Context) (orphan.AutoDeleteMode, int) {
	mode, maxPerCycle := j.resolveAutoRemediation(ctx)
	if mode == orphan.AutoDeleteOn && !orphan.AutoDeleteOnAllowed() {
		log.Printf("[orphan-auto-delete] mode 'on' requested but ORPHAN_AUTO_DELETE_ALLOW_ON is not set in this environment; clamping to dryrun (no deletions)")
		mode = orphan.AutoDeleteDryRun
	}
	return mode, maxPerCycle
}

// resolveAutoRemediation reads the raw mode and per-cycle cap (DB override, else
// env bootstrap) WITHOUT applying the on-allowed interlock -- callers use
// effectiveAutoRemediation, which layers that clamp on top.
func (j *Janitor) resolveAutoRemediation(ctx context.Context) (orphan.AutoDeleteMode, int) {
	mode := j.config.OrphanAutoDelete
	maxPerCycle := j.config.OrphanAutoDeleteMaxPerCycle

	if j.stores.settings == nil {
		return mode, maxPerCycle
	}

	raw, err := j.stores.settings.Get(ctx, types.SettingOrphanAutoRemediation)
	if err != nil {
		log.Printf("[orphan-auto-delete] could not read auto-remediation setting from DB, using env default (%s): %v", mode, err)
		return mode, maxPerCycle
	}
	if raw == nil {
		return mode, maxPerCycle // not configured in console -> env bootstrap default
	}

	var s types.OrphanAutoRemediationSettings
	if err := json.Unmarshal(raw, &s); err != nil {
		log.Printf("[orphan-auto-delete] invalid auto-remediation setting JSON in DB, using env default (%s): %v", mode, err)
		return mode, maxPerCycle
	}
	if m, ok := orphan.ParseAutoDeleteMode(s.Mode); ok {
		mode = m
	} else {
		log.Printf("[orphan-auto-delete] unrecognized mode %q in DB setting, using env default (%s)", s.Mode, mode)
	}
	if s.MaxPerCycle > 0 {
		maxPerCycle = s.MaxPerCycle
	}
	return mode, maxPerCycle
}

// writeRemediationStatus persists per-cycle telemetry for the admin console.
// Best-effort: a status-write failure never affects remediation.
func (j *Janitor) writeRemediationStatus(ctx context.Context, status types.OrphanAutoRemediationStatus) {
	if j.stores.settings == nil {
		return
	}
	raw, err := json.Marshal(status)
	if err != nil {
		log.Printf("[orphan-auto-delete] failed to marshal remediation status: %v", err)
		return
	}
	if err := j.stores.settings.Upsert(ctx, types.SettingOrphanAutoRemediationStatus, raw, "janitor"); err != nil {
		log.Printf("[orphan-auto-delete] failed to persist remediation status: %v", err)
	}
}

// hasOcpctlOwnershipTag reports whether the recorded resource tags carry a
// verified ocpctl ownership signal. AWS detectors tag managed resources with
// ManagedBy=ocpctl (or cluster-control-plane); GCP detectors label them
// managed-by=ocpctl. Resources matched by weaker heuristics do not carry these,
// so this is the gate that keeps auto-remediation from ever deleting a resource
// ocpctl did not create.
func hasOcpctlOwnershipTag(tags types.OrphanedResourceTags) bool {
	switch {
	case tags["ManagedBy"] == "ocpctl", tags["ManagedBy"] == "cluster-control-plane":
		return true
	case tags["managed-by"] == "ocpctl", tags["managed_by"] == "ocpctl":
		return true
	default:
		return false
	}
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
