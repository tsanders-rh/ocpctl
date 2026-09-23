package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/profile"
)

// azureDNSCheckTimeout bounds a single `az` lookup in the base-domain pre-flight.
// These are metadata reads that normally answer in under a second.
const azureDNSCheckTimeout = 60 * time.Second

// ValidateAzureBaseDomainZone verifies that the public DNS infrastructure an
// Azure OpenShift IPI install depends on actually exists BEFORE anything is
// provisioned.
//
// Why this check earns its place: `openshift-install` does not create the
// base-domain resource group or DNS zone — it expects them to exist and only
// discovers otherwise at the very end of infrastructure provisioning, when it
// writes the public `api.<cluster>` record. By then the install has run ~4-5
// minutes and provisioned real VNets, NSGs, public IPs and VMs, and the failure
// surfaces as a bare `ResourceGroupNotFound` that reads like a credential or
// subscription problem (issue #186). Checking first turns that into an instant,
// self-explanatory failure with no cloud spend.
//
// This is not a hypothetical: the base-domain RG/zone in the shared
// subscription has been deleted three times by a management-group-scope janitor
// that reaps resource groups ~12h after stamping them (see
// internal/janitor/azure_reaper_keepalive.go, which defends against it). Every
// Azure create in the fleet fails for as long as it is missing.
//
// Returns nil when there is nothing to validate (no base-domain resource group
// configured in the profile). Wrap the error in types.NewPreflightCheckError at
// the call site so the job fails permanently instead of burning retries on a
// condition no retry can fix.
func ValidateAzureBaseDomainZone(ctx context.Context, prof *profile.Profile, baseDomain string) error {
	if prof == nil || prof.PlatformConfig.Azure == nil {
		return nil
	}
	rg := strings.TrimSpace(prof.PlatformConfig.Azure.BaseDomainResourceGroup)
	if rg == "" {
		// Profile doesn't pin a base-domain RG, so the installer isn't being asked
		// to write into one. Nothing to pre-validate.
		return nil
	}

	if baseDomain == "" && prof.BaseDomains != nil {
		baseDomain = prof.BaseDomains.Default
	}
	if baseDomain == "" {
		return nil
	}

	log.Printf("Azure DNS pre-flight: checking base domain %q in resource group %q", baseDomain, rg)

	// 1. Does the resource group exist? This is the exact check that fails in #186.
	if err := azureResourceGroupExists(ctx, rg); err != nil {
		if isAzureNotFound(err) {
			return fmt.Errorf("%s", azureDNSMissingMessage(
				fmt.Sprintf("Azure resource group %q does not exist.", rg), rg, baseDomain))
		}
		// Couldn't tell (auth failure, az missing, throttling). Don't block the
		// create on an inconclusive check — the installer will report the real
		// problem.
		log.Printf("Azure DNS pre-flight: warning: could not verify resource group %q, continuing: %v", rg, err)
		return nil
	}

	// 2. Does the DNS zone exist inside it? The RG can exist while the zone is
	// gone — the reaper deletes the zone first, and a hand-rolled recreate can
	// stop at `az group create`.
	nameServers, err := azureDNSZoneNameServers(ctx, rg, baseDomain)
	if err != nil {
		if isAzureNotFound(err) {
			return fmt.Errorf("%s", azureDNSMissingMessage(
				fmt.Sprintf("Azure DNS zone %q does not exist in resource group %q.", baseDomain, rg), rg, baseDomain))
		}
		log.Printf("Azure DNS pre-flight: warning: could not verify DNS zone %q, continuing: %v", baseDomain, err)
		return nil
	}

	// 3. Does the public delegation point at this zone? A recreated zone can come
	// back on a different Azure nameserver set, which leaves the parent
	// delegation pointing at a zone that no longer serves the domain. The install
	// then succeeds while api.<cluster>.<baseDomain> never resolves for anyone.
	checkAzureDNSDelegation(baseDomain, nameServers)

	log.Printf("Azure DNS pre-flight: ✓ resource group %q and zone %q exist (nameServers: %s)",
		rg, baseDomain, strings.Join(nameServers, ", "))
	return nil
}

// checkAzureDNSDelegation compares the zone's Azure nameservers against the
// domain's live public delegation. It only warns: a resolver hiccup or
// mid-propagation delegation must not block creates, and the authoritative
// signal (the zone exists and we can write to it) has already passed.
func checkAzureDNSDelegation(baseDomain string, zoneNameServers []string) {
	if len(zoneNameServers) == 0 {
		return
	}
	delegated, err := net.LookupNS(baseDomain)
	if err != nil {
		log.Printf("Azure DNS pre-flight: note: could not resolve NS records for %q (%v); skipping delegation check", baseDomain, err)
		return
	}
	if len(delegated) == 0 {
		log.Printf("Azure DNS pre-flight: WARNING: %q has no public NS delegation; cluster API/apps URLs will not resolve outside Azure", baseDomain)
		return
	}

	zoneSet := make(map[string]bool, len(zoneNameServers))
	for _, ns := range zoneNameServers {
		zoneSet[normalizeHost(ns)] = true
	}
	var matched, unmatched []string
	for _, ns := range delegated {
		host := normalizeHost(ns.Host)
		if zoneSet[host] {
			matched = append(matched, host)
		} else {
			unmatched = append(unmatched, host)
		}
	}

	switch {
	case len(matched) == 0:
		// Complete disagreement: the delegation points somewhere else entirely,
		// which is what a zone recreated onto a fresh nameserver set looks like.
		log.Printf("Azure DNS pre-flight: WARNING: the public NS delegation for %q does not match the Azure DNS zone. "+
			"Delegation: %s. Zone: %s. The cluster will install but its API/console URLs will not resolve publicly — "+
			"update the parent zone's NS record to the Azure nameservers.",
			baseDomain, strings.Join(unmatched, ", "), strings.Join(zoneNameServers, ", "))
	case len(unmatched) > 0:
		log.Printf("Azure DNS pre-flight: note: public NS delegation for %q partially matches the zone (extra: %s)",
			baseDomain, strings.Join(unmatched, ", "))
	}
}

// normalizeHost lowercases a hostname and strips the trailing root dot so
// Azure's "ns1-02.azure-dns.com." and a resolver's answer compare equal.
func normalizeHost(h string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(h), "."))
}

// azureDNSMissingMessage builds the operator-facing failure text. It leads with
// what is missing, then the exact recovery commands, because the person reading
// it is usually looking at a wall of red CI and needs to act, not diagnose.
func azureDNSMissingMessage(problem, rg, baseDomain string) string {
	return fmt.Sprintf(`❌ Azure DNS pre-flight failed: %s

OpenShift IPI does not create this infrastructure — it must already exist, and
the installer only fails once it has provisioned real VMs (~4-5 min in), where
it surfaces as a misleading "ResourceGroupNotFound" (issue #186).

The profile expects:
  • resource group : %s
  • public DNS zone: %s

To restore it:
  az group create --name %s --location eastus
  az network dns zone create --name %s --resource-group %s
  # then confirm the parent zone's NS delegation still matches:
  az network dns zone show --name %s --resource-group %s --query nameServers -o tsv
  dig +short NS %s

Note: this resource group has been deleted repeatedly by a management-group
janitor that reaps resource groups ~12h after tagging them openshift_creationDate.
No Azure cluster in any environment can be created while it is missing.`,
		problem, rg, baseDomain, rg, baseDomain, rg, baseDomain, rg, baseDomain)
}

// azureResourceGroupExists returns nil when the resource group exists, an error
// wrapping "not found" when Azure says it doesn't, or another error when the
// check itself failed.
func azureResourceGroupExists(ctx context.Context, rg string) error {
	_, err := runAzureQuery(ctx, "group", "show", "--name", rg, "--query", "name", "-o", "tsv")
	return err
}

// azureDNSZoneNameServers returns the zone's assigned Azure nameservers.
func azureDNSZoneNameServers(ctx context.Context, rg, zone string) ([]string, error) {
	out, err := runAzureQuery(ctx, "network", "dns", "zone", "show",
		"--name", zone, "--resource-group", rg, "--query", "nameServers", "-o", "json")
	if err != nil {
		return nil, err
	}
	var ns []string
	if err := json.Unmarshal([]byte(out), &ns); err != nil {
		// The zone exists (the command succeeded); we just can't read the
		// nameservers. Not worth failing the create over.
		return nil, nil
	}
	sort.Strings(ns)
	return ns, nil
}

// runAzureQuery runs a read-only `az` command and returns its trimmed stdout.
// On failure the error carries the combined output so callers can classify it.
func runAzureQuery(ctx context.Context, args ...string) (string, error) {
	c, cancel := context.WithTimeout(ctx, azureDNSCheckTimeout)
	defer cancel()

	args = append(args, "--only-show-errors")
	out, err := exec.CommandContext(c, "az", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("az %s: %v: %s", strings.Join(args, " "), err, truncate(string(out), 400))
	}
	return strings.TrimSpace(string(out)), nil
}

// azureNotFoundMarkers are the substrings Azure/the CLI use to say "this
// resource does not exist", as opposed to "I could not check".
var azureNotFoundMarkers = []string{
	"resourcegroupnotfound",
	"resourcenotfound",
	"was not found",
	"could not be found",
	"not found",
	"does not exist",
}

// isAzureNotFound reports whether err is Azure reporting a missing resource.
// Auth failures, a missing `az` binary and throttling deliberately do NOT match
// — those mean the check was inconclusive and must not fail a create.
func isAzureNotFound(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "executable file not found") {
		return false
	}
	for _, m := range azureNotFoundMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}
