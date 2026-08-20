package janitor

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// hm builds a time.Time carrying only the hour/minute that isWithinWorkHours reads.
func hm(h, m int) time.Time {
	return time.Date(2026, 1, 1, h, m, 0, 0, time.UTC)
}

func TestIsWithinWorkHours(t *testing.T) {
	// A concrete work day and its weekday, so we don't hard-code a calendar date.
	workday := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC) // any date; we read its weekday
	includesToday := int16(1) << uint(workday.Weekday())
	excludesToday := int16(0x7F) &^ includesToday // full week minus today

	at := func(h, m int) time.Time {
		return time.Date(2026, 8, 17, h, m, 0, 0, time.UTC)
	}

	tests := []struct {
		name string
		now  time.Time
		mask int16
		want bool
	}{
		{"inside normal hours", at(10, 0), includesToday, true},
		{"at start boundary (inclusive)", at(9, 0), includesToday, true},
		{"at end boundary (exclusive)", at(17, 0), includesToday, false},
		{"before hours", at(8, 59), includesToday, false},
		{"after hours", at(18, 0), includesToday, false},
		{"non-work day, in hours", at(10, 0), excludesToday, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWithinWorkHours(tt.now, hm(9, 0), hm(17, 0), tt.mask)
			if got != tt.want {
				t.Errorf("isWithinWorkHours(%v) = %v, want %v", tt.now, got, tt.want)
			}
		})
	}
}

func TestIsWithinWorkHours_Wraparound(t *testing.T) {
	// Night shift 22:00 -> 06:00, every day a work day so we isolate the hour math.
	const allDays = int16(0x7F)
	start, end := hm(22, 0), hm(6, 0)

	cases := []struct {
		h    int
		want bool
	}{
		{23, true},  // late night, in shift
		{2, true},   // early morning, still in shift
		{6, false},  // end boundary exclusive
		{12, false}, // midday, out of shift
		{22, true},  // start boundary inclusive
	}
	for _, c := range cases {
		now := time.Date(2026, 8, 17, c.h, 0, 0, 0, time.UTC)
		if got := isWithinWorkHours(now, start, end, allDays); got != c.want {
			t.Errorf("wraparound at %02d:00 = %v, want %v", c.h, got, c.want)
		}
	}
}

func TestDefaultConfig(t *testing.T) {
	c := DefaultConfig()
	if c.CheckInterval != 5*time.Minute {
		t.Errorf("CheckInterval = %v", c.CheckInterval)
	}
	if c.StuckJobThreshold != 120*time.Minute {
		t.Errorf("StuckJobThreshold = %v", c.StuckJobThreshold)
	}
	if c.OrphanCheckInterval != 15*time.Minute {
		t.Errorf("OrphanCheckInterval = %v", c.OrphanCheckInterval)
	}
	if c.DestroyedClusterRetentionDays != 30 {
		t.Errorf("DestroyedClusterRetentionDays = %d, want 30", c.DestroyedClusterRetentionDays)
	}
	if c.FailedClusterDirRetentionDays != 7 {
		t.Errorf("FailedClusterDirRetentionDays = %d, want 7", c.FailedClusterDirRetentionDays)
	}
	if !c.ExpiredLockCleanup || !c.ExpiredKeyCleanup || !c.OrphanDetection || !c.OrphanedDirCleanup {
		t.Error("expected all boolean cleanup toggles to default true")
	}
}

func TestNewJanitor_DefaultsWhenConfigNil(t *testing.T) {
	j := NewJanitor(nil, nil, "/tmp/work")
	if j.config == nil {
		t.Fatal("config should be defaulted, got nil")
	}
	if j.config.CheckInterval != 5*time.Minute {
		t.Errorf("defaulted CheckInterval = %v", j.config.CheckInterval)
	}
	if j.workDir != "/tmp/work" {
		t.Errorf("workDir = %q", j.workDir)
	}
	if j.running {
		t.Error("new janitor should not be running")
	}
}

// ---- AWS tag / name helpers -----------------------------------------------

func TestGetTagValue(t *testing.T) {
	tags := []ec2types.Tag{
		{Key: aws.String("Name"), Value: aws.String("my-vpc")},
		{Key: aws.String("ManagedBy"), Value: aws.String("ocpctl")},
	}
	if got := getTagValue(tags, "ManagedBy"); got != "ocpctl" {
		t.Errorf("getTagValue(ManagedBy) = %q", got)
	}
	if got := getTagValue(tags, "Missing"); got != "" {
		t.Errorf("getTagValue(Missing) = %q, want empty", got)
	}
	if got := getTagValue(nil, "Name"); got != "" {
		t.Errorf("getTagValue(nil) = %q, want empty", got)
	}
}

func TestTagsToMap(t *testing.T) {
	tags := []ec2types.Tag{
		{Key: aws.String("a"), Value: aws.String("1")},
		{Key: aws.String("b"), Value: aws.String("2")},
	}
	m := tagsToMap(tags)
	if m["a"] != "1" || m["b"] != "2" {
		t.Errorf("tagsToMap = %v", m)
	}
	if len(tagsToMap(nil)) != 0 {
		t.Error("tagsToMap(nil) should be empty")
	}
}

func TestExtractClusterName(t *testing.T) {
	tests := map[string]string{
		"d-cluster-lqrc7-vpc":       "d-cluster",
		"d-cluster-lqrc7-ext":       "d-cluster",
		"c-cluster-dhbrh-bootstrap": "c-cluster",
		"no-match-here":             "",
		"short":                     "",
		"a-cluster":                 "", // fewer than 3 parts
	}
	for in, want := range tests {
		if got := extractClusterName(in); got != want {
			t.Errorf("extractClusterName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractClusterNameFromDNS(t *testing.T) {
	tests := map[string]string{
		"api.d-cluster.mg.dog8code.com.": "d-cluster",
		"d-cluster.mg.dog8code.com.":     "d-cluster",
		"api.d-cluster.mg.dog8code.com":  "d-cluster", // no trailing dot
		"unrelated.example.com":          "",
		"single":                         "",
	}
	for in, want := range tests {
		if got := extractClusterNameFromDNS(in); got != want {
			t.Errorf("extractClusterNameFromDNS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractClusterNameFromIAMRole(t *testing.T) {
	tests := map[string]string{
		"sanders12-9hfvt-openshift-cloud-credential-operator-cloud-creden": "sanders12",
		"sanders12-9hfvt-master-role":                                      "sanders12",
		"d-cluster-lqrc7-openshift-ingress-operator-cloud-credentials":     "d-cluster",
		"d-cluster-lqrc7-worker-role":                                      "d-cluster",
		"no-infra-here":                                                    "", // no openshift/master/worker marker
		"prefixonly-toolongsegment-master-role":                            "", // infra id not 5 chars
	}
	for in, want := range tests {
		if got := extractClusterNameFromIAMRole(in); got != want {
			t.Errorf("extractClusterNameFromIAMRole(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---- GCP name helpers ------------------------------------------------------

func TestExtractGCPRegion(t *testing.T) {
	tests := map[string]string{
		"us-central1-a":  "us-central1",
		"europe-west1-b": "europe-west1",
		"us-central1":    "us-central1", // already a region, fewer than 3 parts
	}
	for in, want := range tests {
		if got := extractGCPRegion(in); got != want {
			t.Errorf("extractGCPRegion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExtractGCPClusterName(t *testing.T) {
	tests := []struct {
		name     string
		resource string
		labels   map[string]string
		want     string
	}{
		{"cluster-name label", "whatever", map[string]string{"cluster-name": "mycluster"}, "mycluster"},
		{"cluster_name label", "whatever", map[string]string{"cluster_name": "mycluster"}, "mycluster"},
		{"k8s owned label strips infra id", "x", map[string]string{"kubernetes-io-cluster-mycluster-abcde": "owned"}, "mycluster"},
		{"gke resource name", "gke-mycluster-abc123", nil, "mycluster"},
		{"no signal", "random-thing", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractGCPClusterName(tt.resource, tt.labels); got != tt.want {
				t.Errorf("extractGCPClusterName(%q,%v) = %q, want %q", tt.resource, tt.labels, got, tt.want)
			}
		})
	}
}

func TestExtractClusterNameFromGCPServiceAccount(t *testing.T) {
	tests := map[string]string{
		"gke-mycluster-abc123@proj.iam.gserviceaccount.com": "mycluster",
		"mycluster-sa@proj.iam.gserviceaccount.com":         "mycluster",
		"mycluster-compute@proj.iam.gserviceaccount.com":    "mycluster",
		"mycluster-storage@proj.iam.gserviceaccount.com":    "mycluster",
		"notanemail": "",
		"unknown-pattern@proj.iam.gserviceaccount.com": "",
	}
	for in, want := range tests {
		if got := extractClusterNameFromGCPServiceAccount(in); got != want {
			t.Errorf("extractClusterNameFromGCPServiceAccount(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDetectOrphanedGCPNetworks_AlwaysEmpty(t *testing.T) {
	// GCP networks don't carry labels, so this detector is intentionally a no-op.
	j := &Janitor{}
	got, err := j.detectOrphanedGCPNetworks(context.Background(), "proj", map[string]*types.Cluster{})
	if err != nil {
		t.Fatalf("detectOrphanedGCPNetworks: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no orphaned networks, got %d", len(got))
	}
}
