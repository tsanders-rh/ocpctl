package janitor

import (
	"context"
	"log"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// leakSourceRefs holds best-effort back-references from an orphaned resource to
// the cluster/job that leaked it. Either field may be nil when it can't be
// resolved (unknown cluster name, no matching cluster, or no lifecycle jobs).
type leakSourceRefs struct {
	clusterID *string
	jobID     *string
}

// leakSourceCache memoizes leak-source resolution within a single detection
// cycle so we issue at most one cluster lookup + one job lookup per distinct
// cluster name, regardless of how many orphaned resources map to that name.
type leakSourceCache map[string]leakSourceRefs

func newLeakSourceCache() leakSourceCache { return make(leakSourceCache) }

// resolveLeakSource best-effort-resolves the cluster and originating job that
// leaked an orphaned resource, keyed by the cluster name extracted from the
// resource's tags/name. Matching is name-based (there is no infra_id column) and
// intentionally includes DESTROYED clusters, since the leaking cluster is almost
// always already torn down. Any failure is logged and yields nil refs -- leak-
// source tracing must never break orphan detection or persistence.
func (j *Janitor) resolveLeakSource(ctx context.Context, clusterName string, cache leakSourceCache) leakSourceRefs {
	if clusterName == "" {
		return leakSourceRefs{}
	}
	if refs, ok := cache[clusterName]; ok {
		return refs
	}

	var refs leakSourceRefs

	clusterID, err := j.stores.clusters.GetMostRecentIDByName(ctx, clusterName)
	if err != nil {
		log.Printf("  WARNING: leak-source: failed to resolve cluster by name %q: %v", clusterName, err)
		cache[clusterName] = refs
		return refs
	}

	if clusterID != "" {
		cid := clusterID
		refs.clusterID = &cid

		jobs, err := j.stores.jobs.ListByClusterID(ctx, clusterID)
		if err != nil {
			log.Printf("  WARNING: leak-source: failed to list jobs for cluster %s: %v", clusterID, err)
		} else if job := selectOriginatingJob(jobs); job != nil {
			jid := job.ID
			refs.jobID = &jid
		}
	}

	cache[clusterName] = refs
	return refs
}

// selectOriginatingJob picks the lifecycle job most likely responsible for
// leaking a resource from a cluster's job history. jobs must be ordered newest
// first (as store.ListByClusterID returns them). It prefers the most recent
// FAILED create/destroy job -- a failed teardown or a partial create is the
// usual leak cause -- and otherwise falls back to the most recent create/destroy
// job of any status. Returns nil when the cluster has no create/destroy job.
func selectOriginatingJob(jobs []*types.Job) *types.Job {
	isLifecycle := func(t types.JobType) bool {
		return t == types.JobTypeCreate || t == types.JobTypeDestroy || t == types.JobTypeJanitorDestroy
	}

	var latestLifecycle *types.Job
	for _, job := range jobs {
		if job == nil || !isLifecycle(job.JobType) {
			continue
		}
		if latestLifecycle == nil {
			latestLifecycle = job // newest lifecycle job (jobs are newest-first)
		}
		if job.Status == types.JobStatusFailed {
			return job
		}
	}
	return latestLifecycle
}
