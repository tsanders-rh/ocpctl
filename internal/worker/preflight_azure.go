package worker

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/profile"
)

// probeTimeout bounds a single `az vm create` allocation probe. Allocation
// failures (SkuNotAvailable / Capacity Restrictions) surface within seconds, but
// a successful create provisions the VM (~60-90s), so give it generous headroom.
const probeTimeout = 5 * time.Minute

// azureCapacityMarkers are substrings (lowercased) that identify an Azure
// out-of-capacity / SKU-unavailable failure, whether from a probe's `az vm
// create` output or from openshift-install's log. Any match means "no capacity
// here" — a permanent condition for that (region, zone, SKU) that a plain retry
// cannot fix.
var azureCapacityMarkers = []string{
	"skunotavailable",
	"not available in location",
	"capacity restrictions",
	"notavailableforsubscription",
	"zonalallocationfailed",
	"allocationfailed",
	"overconstrainedallocationrequest",
}

// isAzureCapacityFailure reports whether s contains an Azure capacity/SKU
// unavailability marker.
func isAzureCapacityFailure(s string) bool {
	lower := strings.ToLower(s)
	for _, m := range azureCapacityMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// azureSelectionZones safely extracts the zones from a selection for logging.
func azureSelectionZones(sel *AzureCapacitySelection) []string {
	if sel == nil {
		return nil
	}
	return sel.Zones
}

// AzureCapacitySelection is the winning candidate chosen by the capacity
// pre-flight. The worker pins the create (region + zones + SKUs) to it so the
// OpenShift installer only lands machines in zones with real allocation capacity.
type AzureCapacitySelection struct {
	Region          string
	Zones           []string
	ControlPlaneSku string
	ComputeSku      string
}

// azureCandidate is a fully-resolved candidate (profile defaults filled in).
type azureCandidate struct {
	Region          string
	Zones           []string
	ControlPlaneSku string
	ComputeSku      string
}

// SelectAzureCapacity walks the profile's ordered capacity-fallback matrix and
// returns the first candidate whose required (zone, SKU) combinations all have
// real allocation capacity, verified by throwaway `az vm create` probes.
//
// Azure availability zones are physically separate datacenters with independent
// capacity, and `az vm list-skus` does not predict transient runtime capacity —
// only an actual allocation attempt does. This is deliberately heavier than the
// AWS/GCP API-based pre-flights (it creates and deletes real VMs) because Azure
// gives no reliable capacity signal short of allocating.
//
// requestedRegion (the cluster's chosen region) is probed first when it appears
// in the matrix; otherwise the matrix order stands. Returns a selection to pin
// the create to, or an error (wrap in types.NewPreflightCheckError at the call
// site) if no candidate has capacity.
func SelectAzureCapacity(ctx context.Context, prof *profile.Profile, requestedRegion, clusterID string) (*AzureCapacitySelection, error) {
	candidates := buildAzureCandidates(prof, requestedRegion)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no Azure capacity candidates could be derived from profile %q", prof.Name)
	}

	// Workers with 0 replicas (SNO) don't need a worker-SKU probe.
	needWorker := prof.Compute.Workers != nil && prof.Compute.Workers.Replicas > 0

	log.Printf("Azure capacity pre-flight: %d candidate(s) to probe (needWorker=%v)", len(candidates), needWorker)

	var attempts []string
	for i, cand := range candidates {
		log.Printf("Azure capacity pre-flight: probing candidate %d/%d region=%s zones=%v cp=%s worker=%s",
			i+1, len(candidates), cand.Region, cand.Zones, cand.ControlPlaneSku, cand.ComputeSku)

		ok, detail := probeCandidate(ctx, cand, needWorker, clusterID)
		attempts = append(attempts, fmt.Sprintf("  • %s zones=%v: %s", cand.Region, cand.Zones, detail))
		if ok {
			log.Printf("Azure capacity pre-flight: ✓ selected region=%s zones=%v cp=%s worker=%s",
				cand.Region, cand.Zones, cand.ControlPlaneSku, cand.ComputeSku)
			return &AzureCapacitySelection{
				Region:          cand.Region,
				Zones:           cand.Zones,
				ControlPlaneSku: cand.ControlPlaneSku,
				ComputeSku:      cand.ComputeSku,
			}, nil
		}
	}

	return nil, fmt.Errorf("❌ Azure capacity pre-flight failed: no candidate region/zone has allocation capacity for the required VM sizes.\n\n"+
		"Probed (in order):\n%s\n\n"+
		"Azure availability zones have independent, fluctuating capacity. Recommendations:\n"+
		"  1. Retry later — transient capacity restrictions usually clear within hours\n"+
		"  2. Add more regions/zones to the profile's capacityFallback matrix\n"+
		"  3. Choose a profile with a more widely-available VM size",
		strings.Join(attempts, "\n"))
}

// buildAzureCandidates resolves the profile's capacityFallback into fully-populated
// candidates, filling missing SKUs from the profile's compute config. When no
// matrix is defined it falls back to a single candidate built from the requested
// region and profile SKUs (with no zone pinning). When requestedRegion matches a
// matrix entry, that entry is moved to the front so the user's choice is tried first.
func buildAzureCandidates(prof *profile.Profile, requestedRegion string) []azureCandidate {
	cpDefault, workerDefault := profileAzureSkus(prof)

	var azCfg *profile.AzureConfig
	if prof.PlatformConfig.Azure != nil {
		azCfg = prof.PlatformConfig.Azure
	}

	if azCfg == nil || len(azCfg.CapacityFallback) == 0 {
		// No matrix: single region-level candidate (no zone pinning).
		region := requestedRegion
		if region == "" && len(prof.Regions.Allowlist) > 0 {
			region = prof.Regions.Default
		}
		return []azureCandidate{{
			Region:          region,
			ControlPlaneSku: cpDefault,
			ComputeSku:      workerDefault,
		}}
	}

	candidates := make([]azureCandidate, 0, len(azCfg.CapacityFallback))
	for _, c := range azCfg.CapacityFallback {
		cp := c.ControlPlaneSku
		if cp == "" {
			cp = cpDefault
		}
		worker := c.ComputeSku
		if worker == "" {
			worker = workerDefault
		}
		candidates = append(candidates, azureCandidate{
			Region:          c.Region,
			Zones:           append([]string(nil), c.Zones...),
			ControlPlaneSku: cp,
			ComputeSku:      worker,
		})
	}

	// Prefer the user's requested region: move its candidate to the front.
	if requestedRegion != "" {
		for i := range candidates {
			if candidates[i].Region == requestedRegion {
				sel := candidates[i]
				candidates = append(candidates[:i], candidates[i+1:]...)
				candidates = append([]azureCandidate{sel}, candidates...)
				break
			}
		}
	}

	return candidates
}

// profileAzureSkus returns the control-plane and worker VM sizes from the profile,
// preferring platformConfig.azure sizes and falling back to compute.instanceType.
func profileAzureSkus(prof *profile.Profile) (controlPlane, worker string) {
	if prof.Compute.ControlPlane != nil {
		controlPlane = prof.Compute.ControlPlane.InstanceType
	}
	if prof.Compute.Workers != nil {
		worker = prof.Compute.Workers.InstanceType
	}
	if prof.PlatformConfig.Azure != nil {
		if prof.PlatformConfig.Azure.ControlPlane != nil && prof.PlatformConfig.Azure.ControlPlane.VMSize != "" {
			controlPlane = prof.PlatformConfig.Azure.ControlPlane.VMSize
		}
		if prof.PlatformConfig.Azure.Compute != nil && prof.PlatformConfig.Azure.Compute.VMSize != "" {
			worker = prof.PlatformConfig.Azure.Compute.VMSize
		}
	}
	return controlPlane, worker
}

// probeCandidate probes every required (zone, SKU) combination for a candidate in
// parallel. It returns true only if ALL probes allocate. The second return value
// is a short human-readable detail for the failure summary.
func probeCandidate(ctx context.Context, cand azureCandidate, needWorker bool, clusterID string) (bool, string) {
	// Distinct SKUs to probe (control plane always; worker only when it differs
	// and the cluster actually has workers).
	skus := []string{cand.ControlPlaneSku}
	if needWorker && cand.ComputeSku != "" && cand.ComputeSku != cand.ControlPlaneSku {
		skus = append(skus, cand.ComputeSku)
	}

	// Zones to probe. Empty means region-level (no zone pinning) — probe once
	// with an empty zone.
	zones := cand.Zones
	if len(zones) == 0 {
		zones = []string{""}
	}

	// One throwaway resource group per candidate; always torn down.
	rg := probeResourceGroup(clusterID, cand.Region)
	if err := createProbeResourceGroup(ctx, rg, cand.Region); err != nil {
		return false, fmt.Sprintf("could not create probe resource group: %v", err)
	}
	defer deleteProbeResourceGroup(rg)

	type probeResult struct {
		zone string
		sku  string
		ok   bool
		err  error
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []probeResult
	)

	for _, zone := range zones {
		for _, sku := range skus {
			wg.Add(1)
			go func(zone, sku string) {
				defer wg.Done()
				ok, err := probeAzureAllocation(ctx, rg, cand.Region, zone, sku)
				mu.Lock()
				results = append(results, probeResult{zone: zone, sku: sku, ok: ok, err: err})
				mu.Unlock()
			}(zone, sku)
		}
	}
	wg.Wait()

	// A candidate is good only if every probe allocated. Report the first
	// blocking/erroring combination.
	sort.Slice(results, func(i, j int) bool {
		if results[i].zone != results[j].zone {
			return results[i].zone < results[j].zone
		}
		return results[i].sku < results[j].sku
	})
	for _, r := range results {
		zoneLabel := r.zone
		if zoneLabel == "" {
			zoneLabel = "(region)"
		}
		if r.err != nil {
			return false, fmt.Sprintf("probe error in zone %s for %s: %v", zoneLabel, r.sku, r.err)
		}
		if !r.ok {
			return false, fmt.Sprintf("no capacity in zone %s for %s", zoneLabel, r.sku)
		}
	}
	return true, "all probes allocated"
}

// probeAzureAllocation attempts to allocate a single throwaway VM. Returns
// (true, nil) if it provisions, (false, nil) if Azure reports no capacity
// (SkuNotAvailable / Capacity Restrictions), or (false, err) for an unexpected
// failure (auth, CLI missing, etc.) that shouldn't be read as "no capacity".
func probeAzureAllocation(ctx context.Context, rg, region, zone, sku string) (bool, error) {
	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	vmName := probeVMName(sku, zone)

	args := []string{
		"vm", "create",
		"--resource-group", rg,
		"--name", vmName,
		"--location", region,
		"--image", "Ubuntu2204",
		"--size", sku,
		"--public-ip-address", "",
		"--nsg", "",
		"--admin-username", "azureuser",
		"--generate-ssh-keys",
		"--os-disk-size-gb", "30",
		"--only-show-errors",
	}
	if zone != "" {
		args = append(args, "--zone", zone)
	}

	cmd := exec.CommandContext(probeCtx, "az", args...)
	out, err := cmd.CombinedOutput()

	// Best-effort delete of the probe VM (the whole RG is deleted later too).
	defer func() {
		delCtx, delCancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer delCancel()
		_ = exec.CommandContext(delCtx, "az", "vm", "delete",
			"--resource-group", rg, "--name", vmName, "--yes", "--no-wait",
			"--only-show-errors").Run()
	}()

	if err == nil {
		return true, nil
	}

	if isAzureCapacityFailure(string(out)) {
		return false, nil // definitively "no capacity here"
	}

	// Unexpected failure: surface it so the caller doesn't misread it as capacity.
	return false, fmt.Errorf("az vm create failed: %v: %s", err, truncate(string(out), 300))
}

// probeResourceGroup returns a deterministic per-candidate probe RG name.
func probeResourceGroup(clusterID, region string) string {
	short := strings.ReplaceAll(clusterID, "-", "")
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("ocpctl-pf-%s-%s", short, region)
}

// probeVMName builds an Azure-legal VM name from a SKU and zone.
func probeVMName(sku, zone string) string {
	name := strings.ToLower(strings.ReplaceAll(sku, "_", "-"))
	if zone == "" {
		zone = "r"
	}
	name = fmt.Sprintf("pf-%s-z%s", name, zone)
	if len(name) > 60 {
		name = name[:60]
	}
	return name
}

func createProbeResourceGroup(ctx context.Context, rg, region string) error {
	c, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(c, "az", "group", "create",
		"--name", rg, "--location", region, "--only-show-errors", "-o", "none")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, truncate(string(out), 300))
	}
	return nil
}

// deleteProbeResourceGroup tears down a probe RG in the background. It uses a
// fresh context so cleanup runs even if the parent create context is cancelled.
func deleteProbeResourceGroup(rg string) {
	c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(c, "az", "group", "delete",
		"--name", rg, "--yes", "--no-wait", "--only-show-errors")
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("Azure capacity pre-flight: warning: could not delete probe resource group %s: %v: %s",
			rg, err, truncate(string(out), 200))
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
