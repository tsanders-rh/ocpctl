package janitor

import (
	"testing"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestClusterInfraBase(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"short name unchanged", "d-cluster", "d-cluster"},
		{"exactly 21 chars unchanged", "jsussman-2026-09-09-r", "jsussman-2026-09-09-r"},
		{"long name truncated to 21", "jsussman-2026-09-09-rhwa", "jsussman-2026-09-09-r"},
		{"even longer name truncated to 21", "jsussman-2026-09-09-rhwa-02", "jsussman-2026-09-09-r"},
		{"trailing hyphen trimmed after cut", "abcdefghij-klmnopqrs-tuvwxyz", "abcdefghij-klmnopqrs"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := clusterInfraBase(tc.in); got != tc.want {
				t.Fatalf("clusterInfraBase(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestStripInfraIDSuffix(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		want   string
		wantOK bool
	}{
		{"valid 5-char suffix", "jsussman-2026-09-09-r-s86nm", "jsussman-2026-09-09-r", true},
		{"short cluster with suffix", "d-cluster-lqrc7", "d-cluster", true},
		{"suffix not 5 chars", "d-cluster-abc", "", false},
		{"no hyphen", "single", "", false},
		{"trailing hyphen", "d-cluster-", "", false},
		{"leading hyphen only", "-s86nm", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := stripInfraIDSuffix(tc.in)
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("stripInfraIDSuffix(%q) = (%q, %v), want (%q, %v)", tc.in, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

// TestResolveClusterFromInfraTags is the regression guard for the incident:
// openshift-install truncates a >21-char cluster name before appending the infraID
// suffix, so a live cluster's infraID-tagged resources (LBs/EBS/EIPs) must still
// resolve to that live cluster instead of being flagged as orphans.
func TestResolveClusterFromInfraTags(t *testing.T) {
	live := &types.Cluster{ID: "id-rhwa", Name: "jsussman-2026-09-09-rhwa", Status: types.ClusterStatusReady}
	short := &types.Cluster{ID: "id-short", Name: "d-cluster", Status: types.ClusterStatusReady}

	byName := map[string]*types.Cluster{
		live.Name:  live,
		short.Name: short,
	}
	byInfraBase := map[string]*types.Cluster{
		clusterInfraBase(live.Name):  live,  // "jsussman-2026-09-09-r"
		clusterInfraBase(short.Name): short, // "d-cluster"
	}

	cases := []struct {
		name        string
		k8sName     string
		clusterTag  string
		wantCluster *types.Cluster
		wantFound   bool
	}{
		{
			name:        "truncated infraID resolves to live cluster (the incident)",
			k8sName:     "jsussman-2026-09-09-r-s86nm",
			wantCluster: live,
			wantFound:   true,
		},
		{
			name:        "short-name infraID resolves via suffix strip",
			k8sName:     "d-cluster-lqrc7",
			wantCluster: short,
			wantFound:   true,
		},
		{
			name:        "exact infraID-shaped cluster name",
			k8sName:     "jsussman-2026-09-09-rhwa",
			wantCluster: live,
			wantFound:   true,
		},
		{
			name:        "falls back to ClusterName tag when no k8s tag",
			clusterTag:  "jsussman-2026-09-09-rhwa",
			wantCluster: live,
			wantFound:   true,
		},
		{
			name:        "ClusterName tag fallback with full name when infraID unmatched",
			k8sName:     "someother-cluster-zzzzz",
			clusterTag:  "d-cluster",
			wantCluster: short,
			wantFound:   true,
		},
		{
			name:      "genuinely orphaned: no matching cluster",
			k8sName:   "deadcluster-abc-xxxxx",
			wantFound: false,
		},
		{
			name:      "empty tags: not found",
			wantFound: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _, found := resolveClusterFromInfraTags(byName, byInfraBase, tc.k8sName, tc.clusterTag)
			if found != tc.wantFound {
				t.Fatalf("found = %v, want %v", found, tc.wantFound)
			}
			if found && got != tc.wantCluster {
				t.Fatalf("cluster = %+v, want %+v", got, tc.wantCluster)
			}
		})
	}
}
