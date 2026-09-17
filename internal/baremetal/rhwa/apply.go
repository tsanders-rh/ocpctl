package rhwa

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// InstallOperators applies the RHWA operator Subscriptions, then waits
// best-effort for each CSV to reach Succeeded (never fails the create).
func InstallOperators(ctx context.Context, r runner, spec Spec) error {
	return installOperators(ctx, r, spec, time.Sleep)
}

func installOperators(ctx context.Context, r runner, spec Spec, sleep func(time.Duration)) error {
	script, err := renderOperators(spec)
	if err != nil {
		return err
	}
	if err := r.Run(ctx, script); err != nil {
		return fmt.Errorf("rhwa install operators: %w", err)
	}
	for _, op := range spec.Operators {
		waitCSV(ctx, r, spec.ns(), op.Name, sleep)
	}
	return nil
}

// waitCSV polls for a CSV whose name starts with prefix to reach Succeeded.
// Best-effort: returns after a bounded number of attempts regardless.
func waitCSV(ctx context.Context, r runner, ns, prefix string, sleep func(time.Duration)) {
	cmd := fmt.Sprintf("%s -n %s get csv --no-headers -o custom-columns=N:.metadata.name,P:.status.phase 2>/dev/null | awk -v p=%q '$1 ~ (\"^\" p){print $2}' | head -1",
		ocCmd, ns, prefix)
	for i := 0; i < 60; i++ {
		out, err := r.RunCapture(ctx, cmd)
		if err == nil && strings.TrimSpace(out) == "Succeeded" {
			return
		}
		sleep(15 * time.Second)
	}
}

// ConfigureFencing applies the fence_redfish FenceAgentsRemediationTemplate and
// the NodeHealthCheck.
func ConfigureFencing(ctx context.Context, r runner, spec Spec) error {
	script, err := renderFencing(spec)
	if err != nil {
		return err
	}
	if err := r.Run(ctx, script); err != nil {
		return fmt.Errorf("rhwa configure fencing: %w", err)
	}
	return nil
}
