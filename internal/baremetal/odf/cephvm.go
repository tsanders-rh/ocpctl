package odf

import (
	"context"
	"fmt"
	"strings"
)

// shQuote single-quotes a value for safe interpolation into a remote shell command.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// DefineCephVM ensures the single-VM Ceph host exists on the libvirt network.
// It is conservative and never destroys data: a running VM is left as-is; a
// stopped existing VM is started (its OSD disks may hold ceph data); only a
// missing VM is built fresh (CentOS-Stream COW root + one blank disk per OSD +
// a NoCloud seed that authorizes the ephemeral key and pins the static IP).
func DefineCephVM(ctx context.Context, exec executor, spec Spec) error {
	state, err := exec.RunCapture(ctx, "sudo virsh domstate "+shQuote(spec.NodeName)+" 2>/dev/null || true")
	if err != nil {
		return fmt.Errorf("odf ceph domstate: %w", err)
	}
	switch state = strings.TrimSpace(state); {
	case strings.HasPrefix(state, "running"):
		return nil // healthy or booting; leave it untouched
	case state != "":
		// Exists but not running — start it; never rebuild (would risk ceph data).
		if err := exec.Run(ctx, "sudo virsh start "+shQuote(spec.NodeName)); err != nil {
			return fmt.Errorf("odf start existing ceph VM %s: %w", spec.NodeName, err)
		}
		return nil
	}

	// Missing — build fresh.
	ud, err := renderUserData(spec)
	if err != nil {
		return err
	}
	md, err := renderMetaData(spec)
	if err != nil {
		return err
	}
	nc, err := renderNetworkConfig(spec)
	if err != nil {
		return err
	}
	seedDir := "/tmp/" + spec.NodeName + "-seed"
	if err := exec.Run(ctx, "mkdir -p "+seedDir); err != nil {
		return fmt.Errorf("odf ceph seed dir: %w", err)
	}
	for name, content := range map[string]string{"user-data": ud, "meta-data": md, "network-config": nc} {
		if err := exec.Upload(ctx, strings.NewReader(content), seedDir+"/"+name, 0o644); err != nil {
			return fmt.Errorf("odf upload ceph %s: %w", name, err)
		}
	}
	def, err := renderDefine(spec)
	if err != nil {
		return err
	}
	if err := exec.Run(ctx, def); err != nil {
		return fmt.Errorf("odf define ceph VM: %w", err)
	}
	return nil
}
