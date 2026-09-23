package janitor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/orphan"
)

// Azure reaper keep-alive.
//
// The Azure subscription ocpctl provisions into is governed by a janitor we do
// not own: a service principal with Owner at the *management group* scope
// (`dpp-toolkit`, object fb4f7f9b-3eb0-4cf7-85e8-400c4a188419) that
//
//  1. sweeps hourly and stamps every resource group lacking the tag
//     `openshift_creationDate` with the current time, then
//  2. deletes that resource group and everything in it ~12h after the stamp.
//
// Two consequences, both observed in production:
//
//   - The shared base-domain DNS resource group has been deleted three times
//     (2026-09-01, 2026-09-22, 2026-09-23), and while it is gone EVERY Azure
//     OpenShift IPI create in every environment fails ~4-5 minutes in with a
//     misleading `ResourceGroupNotFound` (issue #186).
//   - Live clusters are torn down at ~12h regardless of their ocpctl TTL. Both
//     Azure profiles set `ttlHours: 72`; clusters octl-mman-3a0b536-334 and
//     octl-mman-a0cc448-335 had their VMs, disks, load balancers and resource
//     groups deleted ~12.5h in, and ocpctl's own DESTROY then "succeeded"
//     against already-empty infrastructure, so nothing flagged it.
//
// This sweep refreshes the reaper's own timestamp tag on the resource groups
// ocpctl still needs, which resets its 12h clock. It is a stopgap: the durable
// fix is an exemption from whoever owns the reaper (it already skips
// `os4-common` and `NetworkWatcherRG` by name). Until then, without this, prod
// Azure breaks roughly daily.
//
// Two deliberate constraints keep this from doing harm:
//
//   - It only ever REFRESHES a tag that is already present; it never adds one.
//     Adding the tag to a resource group the reaper had chosen not to track
//     would arm it against that group. Newly created groups get tagged by the
//     reaper within the hour anyway, and a fresh stamp is 12h from danger.
//   - It protects a cluster's resource group only while ocpctl considers that
//     cluster live. Once it is DESTROYED/FAILED/DESTROY_FAILED we stop
//     refreshing and the reaper is welcome to collect the remains.
const (
	// azureReaperTag is the tag the reaper stamps and judges age by.
	azureReaperTag = "openshift_creationDate"

	// azureReaperTagLayout matches the reaper's own format exactly
	// (e.g. "2026-09-23T05:20:54.160289+00:00", Python's
	// datetime.now(timezone.utc).isoformat()). Writing a different shape — Go's
	// default RFC3339 "Z" suffix, say — risks the reaper failing to parse our
	// stamp and falling back to who-knows-what behaviour.
	azureReaperTagLayout = "2006-01-02T15:04:05.000000-07:00"

	// azureOwnedTag / azureClusterIDTag are ocpctl's own tags on cluster resource
	// groups, used to find them without reconstructing installer infraIDs.
	azureOwnedTag     = "ocpctl_managed"
	azureClusterIDTag = "ocpctl_cluster-id"

	azureKeepaliveCmdTimeout = 2 * time.Minute
)

// azureKeepaliveDefaultRefreshAfter is how stale the reaper's stamp may get
// before we refresh it. The reaper deletes at 12h, so refreshing anything older
// than 2h leaves ~10h of margin even if several janitor cycles are missed,
// while keeping tag writes (and Azure ARM calls) to a handful per hour.
const azureKeepaliveDefaultRefreshAfter = 2 * time.Hour

// azureKeepaliveConfigFromEnv reads the keep-alive settings.
//
// Enabled by default: the failure it prevents is a daily prod outage, and the
// sweep is a no-op anywhere `az` is absent or unauthenticated (laptops, CI,
// non-Azure deployments). Set AZURE_REAPER_KEEPALIVE=false to disable, or
// AZURE_REAPER_KEEPALIVE_HOURS to change the refresh threshold.
func azureKeepaliveEnabledFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("AZURE_REAPER_KEEPALIVE"))
	if v == "" {
		return true
	}
	enabled, err := strconv.ParseBool(v)
	if err != nil {
		log.Printf("Warning: invalid AZURE_REAPER_KEEPALIVE=%q, defaulting to enabled", v)
		return true
	}
	return enabled
}

func azureKeepaliveRefreshAfterFromEnv() time.Duration {
	v := strings.TrimSpace(os.Getenv("AZURE_REAPER_KEEPALIVE_HOURS"))
	if v == "" {
		return azureKeepaliveDefaultRefreshAfter
	}
	hours, err := strconv.ParseFloat(v, 64)
	if err != nil || hours <= 0 {
		log.Printf("Warning: invalid AZURE_REAPER_KEEPALIVE_HOURS=%q, using %s", v, azureKeepaliveDefaultRefreshAfter)
		return azureKeepaliveDefaultRefreshAfter
	}
	return time.Duration(hours * float64(time.Hour))
}

// azureResourceGroup is the subset of `az group list` output we need.
type azureResourceGroup struct {
	Name string            `json:"name"`
	Tags map[string]string `json:"tags"`
}

// refreshAzureReaperTags resets the reaper's countdown on the resource groups
// ocpctl still needs: the profiles' shared base-domain DNS groups, and the
// resource groups of Azure clusters that are still live.
func (j *Janitor) refreshAzureReaperTags(ctx context.Context) error {
	if !j.config.AzureReaperKeepalive {
		return nil
	}

	// The Azure seams are wired by NewJanitor. A Janitor built as a struct
	// literal (only tests do this) deliberately gets a no-op rather than a
	// fallback to the real `az` shell-outs, so a unit test can never reach into
	// a live subscription — and never a nil-pointer panic either.
	lister, refresher := j.azureGroupLister, j.azureTagRefresher
	if lister == nil || refresher == nil {
		return nil
	}

	groups, err := lister(ctx)
	if err != nil {
		// No Azure CLI, no credentials, or ARM is unhappy. This host simply
		// cannot do the sweep; that is the normal case off the worker hosts, so
		// don't escalate it to the caller's error log every 5 minutes.
		log.Printf("Azure reaper keep-alive: skipped (%v)", err)
		return nil
	}

	protect := j.azureGroupsToProtect(ctx, groups)
	if len(protect) == 0 {
		return nil
	}

	now := time.Now().UTC()
	stamp := now.Format(azureReaperTagLayout)
	cutoff := now.Add(-j.config.AzureReaperKeepaliveRefreshAfter)

	var refreshed, failed int
	for _, g := range protect {
		raw, ok := g.Tags[azureReaperTag]
		if !ok {
			// Untagged: the reaper isn't counting down on this group yet, and we
			// must not start the clock for it by adding the tag ourselves.
			continue
		}
		age := "unparseable stamp"
		if stamped, parseErr := parseAzureReaperStamp(raw); parseErr != nil {
			log.Printf("Azure reaper keep-alive: resource group %s has an unparseable %s=%q (%v); refreshing it to be safe",
				g.Name, azureReaperTag, raw, parseErr)
		} else if stamped.After(cutoff) {
			continue // still fresh enough
		} else {
			age = fmt.Sprintf("was %s old", now.Sub(stamped).Round(time.Minute))
		}

		if err := refresher(ctx, g.Name, stamp); err != nil {
			log.Printf("Azure reaper keep-alive: FAILED to refresh %s on resource group %s: %v", azureReaperTag, g.Name, err)
			failed++
			continue
		}
		log.Printf("Azure reaper keep-alive: refreshed %s on resource group %s (%s)", azureReaperTag, g.Name, age)
		refreshed++
	}

	if refreshed > 0 || failed > 0 {
		log.Printf("Azure reaper keep-alive: %d resource group(s) refreshed, %d failed, %d protected in total",
			refreshed, failed, len(protect))
	}
	if failed > 0 {
		return fmt.Errorf("failed to refresh %s on %d resource group(s)", azureReaperTag, failed)
	}
	return nil
}

// azureGroupsToProtect selects the resource groups whose reaper clock we reset:
// the configured base-domain DNS groups, plus ocpctl-owned cluster groups whose
// cluster is still live.
func (j *Janitor) azureGroupsToProtect(ctx context.Context, groups []azureResourceGroup) []azureResourceGroup {
	baseDomainRGs := make(map[string]bool, len(j.config.AzureBaseDomainResourceGroups))
	for _, rg := range j.config.AzureBaseDomainResourceGroups {
		if rg = strings.TrimSpace(rg); rg != "" {
			baseDomainRGs[strings.ToLower(rg)] = true
		}
	}

	var protect []azureResourceGroup
	for _, g := range groups {
		if baseDomainRGs[strings.ToLower(g.Name)] {
			protect = append(protect, g)
			continue
		}
		if !strings.EqualFold(g.Tags[azureOwnedTag], "true") {
			continue // not ours
		}
		clusterID := g.Tags[azureClusterIDTag]
		if clusterID == "" {
			continue
		}
		if j.azureClusterIsLive(ctx, clusterID, g.Name) {
			protect = append(protect, g)
		}
	}
	return protect
}

// azureClusterIsLive reports whether ocpctl still considers the cluster behind a
// resource group live. An unreadable/unknown cluster is treated as NOT live: the
// reaper's 12h default is the safe outcome for a group we can't account for, and
// we'd rather leak a teardown than indefinitely preserve mystery infrastructure.
func (j *Janitor) azureClusterIsLive(ctx context.Context, clusterID, rgName string) bool {
	if j.stores.clusters == nil {
		return false
	}
	cluster, err := j.stores.clusters.GetByID(ctx, clusterID)
	if err != nil {
		log.Printf("Azure reaper keep-alive: could not look up cluster %s for resource group %s: %v", clusterID, rgName, err)
		return false
	}
	if cluster == nil {
		return false
	}
	return orphan.IsLiveStatus(cluster.Status)
}

// parseAzureReaperStamp parses the reaper's timestamp tag. It accepts the
// reaper's own microsecond+offset format and plain RFC3339, since the tag has
// been written by hand during incidents too.
func parseAzureReaperStamp(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{azureReaperTagLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp %q", s)
}

// listAzureResourceGroups shells out to `az group list`. Returns an error (not
// an empty list) when the CLI is missing or unauthenticated so the caller can
// tell "nothing to protect" from "cannot check".
func listAzureResourceGroups(ctx context.Context) ([]azureResourceGroup, error) {
	c, cancel := context.WithTimeout(ctx, azureKeepaliveCmdTimeout)
	defer cancel()

	out, err := exec.CommandContext(c, "az", "group", "list",
		"--query", "[].{name:name,tags:tags}", "-o", "json", "--only-show-errors").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("az group list: %v: %s", err, truncateJanitor(string(out), 200))
	}

	var groups []azureResourceGroup
	if err := json.Unmarshal(out, &groups); err != nil {
		return nil, fmt.Errorf("parse az group list output: %w", err)
	}
	return groups, nil
}

// refreshAzureResourceGroupTag merges a new reaper timestamp onto one resource
// group. `az tag update --operation merge` is deliberate: it preserves every
// other tag (ocpctl provenance, CAPZ ownership, cost center), which a
// `az group update --tags` would replace wholesale.
func refreshAzureResourceGroupTag(ctx context.Context, rgName, stamp string) error {
	c, cancel := context.WithTimeout(ctx, azureKeepaliveCmdTimeout)
	defer cancel()

	idOut, err := exec.CommandContext(c, "az", "group", "show",
		"--name", rgName, "--query", "id", "-o", "tsv", "--only-show-errors").CombinedOutput()
	if err != nil {
		return fmt.Errorf("resolve resource group id: %v: %s", err, truncateJanitor(string(idOut), 200))
	}
	resourceID := strings.TrimSpace(string(idOut))
	if resourceID == "" {
		return fmt.Errorf("resource group %s has no id", rgName)
	}

	out, err := exec.CommandContext(c, "az", "tag", "update",
		"--resource-id", resourceID,
		"--operation", "merge",
		"--tags", fmt.Sprintf("%s=%s", azureReaperTag, stamp),
		"-o", "none", "--only-show-errors").CombinedOutput()
	if err != nil {
		return fmt.Errorf("az tag update: %v: %s", err, truncateJanitor(string(out), 200))
	}
	return nil
}

func truncateJanitor(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
