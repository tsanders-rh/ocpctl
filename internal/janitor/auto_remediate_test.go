package janitor

import (
	"context"
	"testing"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/orphan"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// safeResource returns an orphaned EBSVolume that passes both the ownership gate
// and the safety gate with a nil cluster lookup: carries ManagedBy=ocpctl, old
// enough, detected enough, ACTIVE, non-VPC.
func safeResource(id string) *types.OrphanedResource {
	return &types.OrphanedResource{
		ID:              id,
		ResourceType:    types.OrphanedResourceTypeEBSVolume,
		ResourceID:      "vol-" + id,
		ResourceName:    "orphan-" + id,
		Region:          "us-east-1",
		ClusterName:     "cluster-" + id,
		Status:          types.OrphanedResourceStatusActive,
		Tags:            types.OrphanedResourceTags{"ManagedBy": "ocpctl"},
		FirstDetectedAt: time.Now().Add(-48 * time.Hour),
		DetectionCount:  5,
	}
}

// newRemediateJanitor wires a Janitor with mock stores and a fake deleter so
// auto-remediation can be exercised without AWS or a DB. clusterLookup is left
// nil (the live-cluster guard is skipped with a warning, not a block).
func newRemediateJanitor(mode orphan.AutoDeleteMode, maxPerCycle int, orphaned *mockOrphanedResourceStore, audit *mockAuditStore, deleted *[]string) *Janitor {
	return &Janitor{
		config: &Config{
			OrphanAutoDelete:            mode,
			OrphanAutoDeleteMaxPerCycle: maxPerCycle,
			OrphanSafetyConfig:          orphan.DefaultConfig(),
		},
		stores: janitorStores{
			orphaned: orphaned,
			audit:    audit,
		},
		orphanDeleter: func(ctx context.Context, res *types.OrphanedResource, opts orphan.DeleteOptions) error {
			*deleted = append(*deleted, res.ID)
			return nil
		},
	}
}

func TestAutoRemediateOff(t *testing.T) {
	orphaned := &mockOrphanedResourceStore{listResult: []*types.OrphanedResource{safeResource("a")}}
	audit := &mockAuditStore{}
	var deleted []string
	j := newRemediateJanitor(orphan.AutoDeleteOff, 20, orphaned, audit, &deleted)

	if err := j.autoRemediateOrphans(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("off mode deleted %v, want none", deleted)
	}
	if len(orphaned.resolved) != 0 {
		t.Errorf("off mode marked resolved %v, want none", orphaned.resolved)
	}
	if len(audit.events) != 0 {
		t.Errorf("off mode logged %d audit events, want 0", len(audit.events))
	}
}

func TestAutoRemediateDryRun(t *testing.T) {
	orphaned := &mockOrphanedResourceStore{listResult: []*types.OrphanedResource{safeResource("a"), safeResource("b")}}
	audit := &mockAuditStore{}
	var deleted []string
	j := newRemediateJanitor(orphan.AutoDeleteDryRun, 20, orphaned, audit, &deleted)

	if err := j.autoRemediateOrphans(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 0 {
		t.Errorf("dry-run deleted %v, want none", deleted)
	}
	if len(orphaned.resolved) != 0 {
		t.Errorf("dry-run marked resolved %v, want none", orphaned.resolved)
	}
	if len(audit.events) != 2 {
		t.Fatalf("dry-run logged %d audit events, want 2", len(audit.events))
	}
	for _, e := range audit.events {
		if e.Action != "orphaned_resource.auto_delete_dryrun" {
			t.Errorf("dry-run audit action = %q, want auto_delete_dryrun", e.Action)
		}
	}
}

func TestAutoRemediateOnDeletesSafeSkipsUnsafe(t *testing.T) {
	safe := safeResource("safe")
	unsafe := safeResource("unsafe")
	unsafe.DetectionCount = 0 // fails MinDetections -> blocked

	orphaned := &mockOrphanedResourceStore{listResult: []*types.OrphanedResource{safe, unsafe}}
	audit := &mockAuditStore{}
	var deleted []string
	j := newRemediateJanitor(orphan.AutoDeleteOn, 20, orphaned, audit, &deleted)

	if err := j.autoRemediateOrphans(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "safe" {
		t.Errorf("deleted = %v, want [safe]", deleted)
	}
	if len(orphaned.resolved) != 1 || orphaned.resolved[0].id != "safe" {
		t.Errorf("resolved = %v, want one call for safe", orphaned.resolved)
	}
	if orphaned.resolved[0].resolvedBy != "janitor" {
		t.Errorf("resolvedBy = %q, want janitor", orphaned.resolved[0].resolvedBy)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "orphaned_resource.auto_deleted" {
		t.Errorf("audit events = %+v, want one auto_deleted", audit.events)
	}
	if audit.events[0].Status != types.AuditEventStatusSuccess {
		t.Errorf("audit status = %q, want SUCCESS", audit.events[0].Status)
	}
}

func TestAutoRemediateOnSkipsResourcesWithoutOwnershipTag(t *testing.T) {
	owned := safeResource("owned")
	heuristic := safeResource("heuristic")
	heuristic.Tags = types.OrphanedResourceTags{"kubernetes.io/cluster/foo": "owned"} // no ManagedBy=ocpctl

	orphaned := &mockOrphanedResourceStore{listResult: []*types.OrphanedResource{owned, heuristic}}
	audit := &mockAuditStore{}
	var deleted []string
	j := newRemediateJanitor(orphan.AutoDeleteOn, 20, orphaned, audit, &deleted)

	if err := j.autoRemediateOrphans(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != "owned" {
		t.Errorf("deleted = %v, want [owned] (heuristic match without ownership tag must be skipped)", deleted)
	}
	if len(orphaned.resolved) != 1 || orphaned.resolved[0].id != "owned" {
		t.Errorf("resolved = %v, want one call for owned", orphaned.resolved)
	}
}

func TestHasOcpctlOwnershipTag(t *testing.T) {
	cases := []struct {
		name string
		tags types.OrphanedResourceTags
		want bool
	}{
		{"aws managed", types.OrphanedResourceTags{"ManagedBy": "ocpctl"}, true},
		{"aws control plane", types.OrphanedResourceTags{"ManagedBy": "cluster-control-plane"}, true},
		{"gcp dash", types.OrphanedResourceTags{"managed-by": "ocpctl"}, true},
		{"gcp underscore", types.OrphanedResourceTags{"managed_by": "ocpctl"}, true},
		{"nil", nil, false},
		{"empty", types.OrphanedResourceTags{}, false},
		{"k8s only", types.OrphanedResourceTags{"kubernetes.io/cluster/foo": "owned"}, false},
		{"generic clustername", types.OrphanedResourceTags{"ClusterName": "foo", "Profile": "bar"}, false},
		{"wrong value", types.OrphanedResourceTags{"ManagedBy": "someone-else"}, false},
	}
	for _, tc := range cases {
		if got := hasOcpctlOwnershipTag(tc.tags); got != tc.want {
			t.Errorf("%s: hasOcpctlOwnershipTag(%v) = %v, want %v", tc.name, tc.tags, got, tc.want)
		}
	}
}

func TestAutoRemediateOnRespectsCap(t *testing.T) {
	resources := []*types.OrphanedResource{
		safeResource("a"), safeResource("b"), safeResource("c"),
	}
	orphaned := &mockOrphanedResourceStore{listResult: resources}
	audit := &mockAuditStore{}
	var deleted []string
	j := newRemediateJanitor(orphan.AutoDeleteOn, 2, orphaned, audit, &deleted)

	if err := j.autoRemediateOrphans(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(deleted) != 2 {
		t.Errorf("deleted %d resources, want 2 (cap)", len(deleted))
	}
	if len(orphaned.resolved) != 2 {
		t.Errorf("resolved %d resources, want 2 (cap)", len(orphaned.resolved))
	}
}
