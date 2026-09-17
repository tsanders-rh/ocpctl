package metal3

import (
	"context"
	"fmt"
)

// ConfigureBMH creates the per-node BareMetalHosts: masters get a BMC-only
// (externallyProvisioned) host that ironic power-manages but never reprovisions;
// workers/spares get a provisionable one (bootMACAddress + rootDeviceHints). A
// node with no libvirt UUID is skipped. Ports rhwa-lab's rhwa_configure_bmh.
func ConfigureBMH(ctx context.Context, r runner, spec Spec) error {
	for _, vm := range spec.Nodes {
		if spec.UUIDs[vm.Name] == "" {
			continue
		}
		var (
			script string
			err    error
		)
		if vm.Role == "master" {
			script, err = renderMasterBMH(spec, vm)
		} else {
			script, err = renderWorkerBMH(spec, vm)
		}
		if err != nil {
			return err
		}
		if err := r.Run(ctx, script); err != nil {
			return fmt.Errorf("metal3 configure bmh %s: %w", vm.Host, err)
		}
	}
	return nil
}
