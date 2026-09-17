package metal3

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ProvisionWorkers scales the baremetal MachineSet to the spec's worker count,
// waits for that many worker nodes to be Ready (metal3 provisioning), then takes
// the control plane out of the schedulable pool. A worker count of 0 is a no-op
// (masters stay schedulable). Ports os_provision_workers/os_unschedule_masters.
func ProvisionWorkers(ctx context.Context, r runner, spec Spec) error {
	return provisionWorkers(ctx, r, spec, time.Sleep)
}

func provisionWorkers(ctx context.Context, r runner, spec Spec, sleep func(time.Duration)) error {
	if spec.WorkerCount == 0 {
		return nil
	}
	ms, err := r.RunCapture(ctx, ocCmd+" -n "+machineAPINS+" get machineset -o jsonpath='{.items[0].metadata.name}'")
	if err != nil {
		return fmt.Errorf("metal3 get machineset: %w", err)
	}
	ms = strings.TrimSpace(ms)
	if ms == "" {
		return fmt.Errorf("metal3 provision workers: no baremetal MachineSet found")
	}
	if err := r.Run(ctx, fmt.Sprintf("%s -n %s scale machineset %s --replicas=%d", ocCmd, machineAPINS, ms, spec.WorkerCount)); err != nil {
		return fmt.Errorf("metal3 scale machineset: %w", err)
	}

	// Count only dedicated workers: with compute.replicas 0 the installer marks
	// masters schedulable (worker role), so exclude control-plane nodes.
	countCmd := ocCmd + " get nodes -l 'node-role.kubernetes.io/worker=,!node-role.kubernetes.io/control-plane' --no-headers 2>/dev/null | awk '$2==\"Ready\"' | wc -l"
	for i := 0; i < 120; i++ {
		out, err := r.RunCapture(ctx, countCmd)
		if err == nil {
			if n, convErr := strconv.Atoi(strings.TrimSpace(out)); convErr == nil && n >= spec.WorkerCount {
				unscheduleMasters(ctx, r)
				return nil
			}
		}
		sleep(30 * time.Second)
	}
	// Best-effort: workers did not all report Ready; leave masters schedulable.
	return nil
}

// unscheduleMasters removes the worker role from the control plane once dedicated
// workers exist, then evicts DaemonSet pods stranded on the now-tainted masters.
// Best-effort.
func unscheduleMasters(ctx context.Context, r runner) {
	if err := r.Run(ctx, ocCmd+` patch schedulers.config.openshift.io/cluster --type=merge -p '{"spec":{"mastersSchedulable":false}}'`); err != nil {
		return
	}
	script, err := renderEvict()
	if err != nil {
		return
	}
	// Best-effort: pods stranded on the now-tainted masters reschedule on their
	// own even if this eviction sweep fails.
	if err := r.Run(ctx, script); err != nil {
		return
	}
}
