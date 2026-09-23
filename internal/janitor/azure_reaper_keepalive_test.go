package janitor

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// stamp renders a reaper timestamp of a given age, in the reaper's own format.
func stamp(age time.Duration) string {
	return time.Now().UTC().Add(-age).Format(azureReaperTagLayout)
}

type refreshCall struct {
	rg    string
	stamp string
}

// newKeepaliveJanitor builds a janitor whose Azure calls are stubbed, so the
// selection/refresh logic is exercised without an Azure subscription.
func newKeepaliveJanitor(groups []azureResourceGroup, clusters map[string]*types.Cluster) (*Janitor, *[]refreshCall) {
	var calls []refreshCall
	j := &Janitor{
		config: &Config{
			AzureReaperKeepalive:             true,
			AzureReaperKeepaliveRefreshAfter: 2 * time.Hour,
			AzureBaseDomainResourceGroups:    []string{"azure-dns-rg"},
		},
		stores: janitorStores{clusters: &mockClusterStore{byID: clusters}},
		azureGroupLister: func(ctx context.Context) ([]azureResourceGroup, error) {
			return groups, nil
		},
	}
	j.azureTagRefresher = func(ctx context.Context, rg, s string) error {
		calls = append(calls, refreshCall{rg: rg, stamp: s})
		return nil
	}
	return j, &calls
}

func refreshedNames(calls []refreshCall) []string {
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		names = append(names, c.rg)
	}
	return names
}

func TestRefreshAzureReaperTags_RefreshesStaleBaseDomainRG(t *testing.T) {
	// The base-domain DNS resource group carries no ocpctl tags, so it is only
	// protected because it is named in config. This is the #186 case.
	groups := []azureResourceGroup{
		{Name: "azure-dns-rg", Tags: map[string]string{azureReaperTag: stamp(11 * time.Hour)}},
	}
	j, calls := newKeepaliveJanitor(groups, nil)

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := refreshedNames(*calls); len(got) != 1 || got[0] != "azure-dns-rg" {
		t.Fatalf("expected azure-dns-rg to be refreshed, got %v", got)
	}
}

func TestRefreshAzureReaperTags_MatchesBaseDomainRGCaseInsensitively(t *testing.T) {
	// Azure resource group names are case-insensitive and the activity log /
	// profiles disagree on case in practice; a case mismatch must not silently
	// leave the DNS zone unprotected.
	groups := []azureResourceGroup{
		{Name: "Azure-DNS-RG", Tags: map[string]string{azureReaperTag: stamp(11 * time.Hour)}},
	}
	j, calls := newKeepaliveJanitor(groups, nil)

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected the differently-cased base-domain RG to be refreshed, got %v", refreshedNames(*calls))
	}
}

func TestRefreshAzureReaperTags_SkipsFreshStamps(t *testing.T) {
	groups := []azureResourceGroup{
		{Name: "azure-dns-rg", Tags: map[string]string{azureReaperTag: stamp(10 * time.Minute)}},
	}
	j, calls := newKeepaliveJanitor(groups, nil)

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("expected no refresh for a fresh stamp, got %v", refreshedNames(*calls))
	}
}

// TestRefreshAzureReaperTags_NeverAddsTheTag is the important safety property:
// stamping a resource group the reaper had chosen not to track would ARM the
// reaper against it. We only ever reset a countdown already running.
func TestRefreshAzureReaperTags_NeverAddsTheTag(t *testing.T) {
	groups := []azureResourceGroup{
		{Name: "azure-dns-rg", Tags: map[string]string{}},
		{Name: "cluster-rg", Tags: map[string]string{
			azureOwnedTag:     "true",
			azureClusterIDTag: "c1",
		}},
	}
	clusters := map[string]*types.Cluster{"c1": {ID: "c1", Status: types.ClusterStatusReady}}
	j, calls := newKeepaliveJanitor(groups, clusters)

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("expected no tag to be created, got %v", refreshedNames(*calls))
	}
}

func TestRefreshAzureReaperTags_ProtectsLiveClusterButNotDestroyed(t *testing.T) {
	groups := []azureResourceGroup{
		{Name: "live-rg", Tags: map[string]string{
			azureOwnedTag:     "true",
			azureClusterIDTag: "live",
			azureReaperTag:    stamp(6 * time.Hour),
		}},
		{Name: "hibernated-rg", Tags: map[string]string{
			azureOwnedTag:     "true",
			azureClusterIDTag: "hibernated",
			azureReaperTag:    stamp(6 * time.Hour),
		}},
		{Name: "destroyed-rg", Tags: map[string]string{
			azureOwnedTag:     "true",
			azureClusterIDTag: "destroyed",
			azureReaperTag:    stamp(6 * time.Hour),
		}},
		{Name: "failed-rg", Tags: map[string]string{
			azureOwnedTag:     "true",
			azureClusterIDTag: "failed",
			azureReaperTag:    stamp(6 * time.Hour),
		}},
	}
	clusters := map[string]*types.Cluster{
		"live":       {ID: "live", Status: types.ClusterStatusReady},
		"hibernated": {ID: "hibernated", Status: types.ClusterStatusHibernated},
		"destroyed":  {ID: "destroyed", Status: types.ClusterStatusDestroyed},
		"failed":     {ID: "failed", Status: types.ClusterStatusFailed},
	}
	j, calls := newKeepaliveJanitor(groups, clusters)

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := refreshedNames(*calls)
	want := map[string]bool{"live-rg": true, "hibernated-rg": true}
	if len(got) != len(want) {
		t.Fatalf("expected only live/hibernated RGs refreshed, got %v", got)
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("refreshed a resource group whose cluster is gone: %s (all: %v)", name, got)
		}
	}
}

func TestRefreshAzureReaperTags_IgnoresForeignResourceGroups(t *testing.T) {
	// Someone else's resource groups in the shared subscription are none of our
	// business — refreshing them would keep other teams' garbage alive.
	groups := []azureResourceGroup{
		{Name: "someone-elses-rg", Tags: map[string]string{azureReaperTag: stamp(11 * time.Hour)}},
		{Name: "os4-common", Tags: map[string]string{azureReaperTag: stamp(400 * 24 * time.Hour)}},
	}
	j, calls := newKeepaliveJanitor(groups, nil)

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("expected foreign resource groups to be left alone, got %v", refreshedNames(*calls))
	}
}

func TestRefreshAzureReaperTags_UnknownClusterIsNotProtected(t *testing.T) {
	// The cluster record is gone (pruned, or a different environment's DB), so we
	// can't vouch for the resource group. Let the reaper have it.
	groups := []azureResourceGroup{
		{Name: "mystery-rg", Tags: map[string]string{
			azureOwnedTag:     "true",
			azureClusterIDTag: "not-in-db",
			azureReaperTag:    stamp(11 * time.Hour),
		}},
	}
	j, calls := newKeepaliveJanitor(groups, map[string]*types.Cluster{})

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("expected no refresh for an unknown cluster, got %v", refreshedNames(*calls))
	}
}

func TestRefreshAzureReaperTags_RefreshesUnparseableStamp(t *testing.T) {
	// A hand-edited or reformatted tag must not become a silent hole in the
	// defense: if we can't tell how old it is, refresh it.
	groups := []azureResourceGroup{
		{Name: "azure-dns-rg", Tags: map[string]string{azureReaperTag: "yesterday-ish"}},
	}
	j, calls := newKeepaliveJanitor(groups, nil)

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected an unparseable stamp to be refreshed, got %v", refreshedNames(*calls))
	}
}

func TestRefreshAzureReaperTags_DisabledIsNoOp(t *testing.T) {
	groups := []azureResourceGroup{
		{Name: "azure-dns-rg", Tags: map[string]string{azureReaperTag: stamp(11 * time.Hour)}},
	}
	j, calls := newKeepaliveJanitor(groups, nil)
	j.config.AzureReaperKeepalive = false

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("expected no calls when disabled, got %v", refreshedNames(*calls))
	}
}

// TestRefreshAzureReaperTags_MissingAzureCLIIsNotAnError keeps the sweep quiet
// on hosts with no Azure CLI or credentials (API host, laptops, CI) — it must
// not log an error every janitor cycle there.
func TestRefreshAzureReaperTags_MissingAzureCLIIsNotAnError(t *testing.T) {
	j, calls := newKeepaliveJanitor(nil, nil)
	j.azureGroupLister = func(ctx context.Context) ([]azureResourceGroup, error) {
		return nil, fmt.Errorf(`az group list: exec: "az": executable file not found in $PATH`)
	}

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("expected a missing az CLI to be a silent no-op, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("expected no calls, got %v", refreshedNames(*calls))
	}
}

func TestRefreshAzureReaperTags_ReportsRefreshFailures(t *testing.T) {
	groups := []azureResourceGroup{
		{Name: "azure-dns-rg", Tags: map[string]string{azureReaperTag: stamp(11 * time.Hour)}},
	}
	j, _ := newKeepaliveJanitor(groups, nil)
	j.azureTagRefresher = func(ctx context.Context, rg, s string) error {
		return fmt.Errorf("AuthorizationFailed")
	}

	if err := j.refreshAzureReaperTags(context.Background()); err == nil {
		t.Fatal("expected an error when a tag refresh fails — silently losing the DNS zone is the outage")
	}
}

// TestAzureReaperStampFormat pins the wire format. The reaper judges age by
// parsing this tag; emitting Go's default RFC3339 "Z" form instead of the
// "+00:00" offset it writes itself risks it mis-parsing our stamp.
func TestAzureReaperStampFormat(t *testing.T) {
	got := time.Date(2026, 9, 23, 5, 20, 54, 160289000, time.UTC).Format(azureReaperTagLayout)
	want := "2026-09-23T05:20:54.160289+00:00"
	if got != want {
		t.Fatalf("stamp format drifted: got %q, want %q", got, want)
	}
}

func TestParseAzureReaperStamp(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		valid bool
	}{
		{"reaper format", "2026-09-23T05:20:54.160289+00:00", true},
		{"rfc3339 z", "2026-09-23T05:20:54Z", true},
		{"rfc3339 nano z", "2026-09-23T05:20:54.160289Z", true},
		{"non-utc offset", "2026-09-23T01:20:54.160289-04:00", true},
		{"garbage", "not-a-date", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAzureReaperStamp(tc.in)
			if tc.valid && err != nil {
				t.Fatalf("expected %q to parse, got %v", tc.in, err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("expected %q to fail parsing", tc.in)
			}
		})
	}
}

func TestAzureKeepaliveEnabledFromEnv(t *testing.T) {
	cases := map[string]bool{
		"":        true, // unset => on, the outage it prevents is a daily one
		"true":    true,
		"false":   false,
		"0":       false,
		"1":       true,
		"garbage": true, // unparseable => fail safe (protected)
	}
	for in, want := range cases {
		// An empty value is indistinguishable from unset via os.Getenv, which is
		// exactly the "operator never set it" case.
		t.Setenv("AZURE_REAPER_KEEPALIVE", in)
		if got := azureKeepaliveEnabledFromEnv(); got != want {
			t.Fatalf("AZURE_REAPER_KEEPALIVE=%q: got %v, want %v", in, got, want)
		}
	}
}

func TestAzureKeepaliveRefreshAfterFromEnv(t *testing.T) {
	t.Setenv("AZURE_REAPER_KEEPALIVE_HOURS", "4")
	if got := azureKeepaliveRefreshAfterFromEnv(); got != 4*time.Hour {
		t.Fatalf("got %s, want 4h", got)
	}

	// Invalid and non-positive values must not disable the defense by yielding 0
	// (which would refresh nothing... or everything, every cycle).
	for _, bad := range []string{"nonsense", "0", "-3"} {
		t.Setenv("AZURE_REAPER_KEEPALIVE_HOURS", bad)
		if got := azureKeepaliveRefreshAfterFromEnv(); got != azureKeepaliveDefaultRefreshAfter {
			t.Fatalf("AZURE_REAPER_KEEPALIVE_HOURS=%q: got %s, want the %s default", bad, got, azureKeepaliveDefaultRefreshAfter)
		}
	}
}

// TestRefreshAzureReaperTags_UnwiredSeamsAreNoOp guards two things at once: a
// Janitor built as a struct literal must not panic, and it must not fall back
// to the real `az` shell-outs — otherwise a unit test on a developer machine
// with live Azure credentials would start retagging production resource groups.
func TestRefreshAzureReaperTags_UnwiredSeamsAreNoOp(t *testing.T) {
	j := &Janitor{config: &Config{
		AzureReaperKeepalive:             true,
		AzureReaperKeepaliveRefreshAfter: 2 * time.Hour,
		AzureBaseDomainResourceGroups:    []string{"azure-dns-rg"},
	}}

	if err := j.refreshAzureReaperTags(context.Background()); err != nil {
		t.Fatalf("expected a no-op for an unwired janitor, got %v", err)
	}
}
