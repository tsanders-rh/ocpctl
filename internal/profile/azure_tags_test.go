package profile

import (
	"strings"
	"testing"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// TestAzureUserTags_TrimsAndSanitizes reproduces the exact set of effective
// tags that failed cluster "tsanders-azure-zzz" (14 tags, 4 with ':' in the
// key) and verifies the result is Azure-compliant.
func TestAzureUserTags_TrimsAndSanitizes(t *testing.T) {
	merged := map[string]string{
		// 10 request tags
		"Team":        "Staff",
		"Owner":       "admin@example.com",
		"Profile":     "azure-sno-ga",
		"Purpose":     "development",
		"Platform":    "azure",
		"ManagedBy":   "ocpctl",
		"CostCenter":  "733",
		"ClusterName": "tsanders-azure-zzz",
		"ClusterType": "sno",
		"Environment": "development",
		// 4 provenance tags (colon-prefixed keys)
		types.TagKeyManaged:     "true",
		types.TagKeyClusterID:   "1f917e0d-b292-4535-ba09-18946f6993d9",
		types.TagKeyClusterName: "tsanders-azure-zzz",
		types.TagKeyCreatedAt:   "2026-08-19T13:14:49Z",
	}

	got := azureUserTags(merged)

	// 1. Must not exceed Azure's installer limit.
	if len(got) > azureMaxUserTags {
		t.Fatalf("got %d tags, want <= %d", len(got), azureMaxUserTags)
	}
	if len(got) != azureMaxUserTags {
		t.Errorf("expected the result to be filled to %d tags, got %d: %v", azureMaxUserTags, len(got), got)
	}

	// 2. No key may contain a colon (or otherwise invalid chars).
	for k := range got {
		if strings.ContainsRune(k, ':') {
			t.Errorf("key %q contains a forbidden ':'", k)
		}
	}

	// 3. All 4 provenance tags must survive, with sanitized keys.
	for _, want := range []string{"ocpctl_managed", "ocpctl_cluster-id", "ocpctl_cluster-name", "ocpctl_created-at"} {
		if _, ok := got[want]; !ok {
			t.Errorf("provenance tag %q missing from result: %v", want, got)
		}
	}

	// 4. Redundant keys should be dropped first.
	for _, redundant := range []string{"ClusterName", "Platform", "ManagedBy"} {
		if _, ok := got[redundant]; ok {
			t.Errorf("redundant request key %q should have been dropped", redundant)
		}
	}
}

func TestSanitizeAzureTagKey(t *testing.T) {
	cases := map[string]string{
		"ocpctl:managed":      "ocpctl_managed",
		"ocpctl:cluster-name": "ocpctl_cluster-name",
		"ocpctl:created-at":   "ocpctl_created-at",
		"Owner":               "Owner",
		"":                    "",
		":::":                 "",    // nothing valid remains
		"1abc":                "abc", // must begin with a letter
	}
	for in, want := range cases {
		if got := sanitizeAzureTagKey(in); got != want {
			t.Errorf("sanitizeAzureTagKey(%q) = %q, want %q", in, got, want)
		}
	}
}
