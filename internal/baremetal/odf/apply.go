package odf

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/olm"
)

// Bootstrap waits for the ceph VM to answer SSH, then runs the cephadm bootstrap
// + OSD + pool + prometheus sequence on it (over the host tunnel).
func Bootstrap(ctx context.Context, exec executor, spec Spec) error {
	return bootstrap(ctx, exec, spec, time.Sleep)
}

func bootstrap(ctx context.Context, exec executor, spec Spec, sleep func(time.Duration)) error {
	reachable := false
	for i := 0; i < 60; i++ {
		if err := exec.RunNode(ctx, spec.IP, spec.sshUser(), "true"); err == nil {
			reachable = true
			break
		}
		sleep(10 * time.Second)
	}
	if !reachable {
		// Dump host-side diagnostics so the failure explains itself (ports
		// rhwa-lab's _ceph_wait_ssh).
		diag := fmt.Sprintf("set +e\n"+
			"echo '--- ceph VM domstate ---'; virsh domstate %s 2>&1\n"+
			"echo '--- domain interfaces (lease view) ---'; virsh domifaddr %s --source lease 2>&1\n"+
			"echo '--- dhcp leases on %s ---'; virsh net-dhcp-leases %s 2>&1\n"+
			"echo '--- can the host reach %s? ---'; ping -c1 -W2 %s 2>&1\n",
			shQuote(spec.NodeName), shQuote(spec.NodeName), spec.LibvirtNet, spec.LibvirtNet, spec.IP, spec.IP)
		if derr := exec.Run(ctx, diag); derr != nil {
			return fmt.Errorf("odf: ceph VM %s did not become reachable over SSH (diagnostics dump also failed: %v)", spec.IP, derr)
		}
		return fmt.Errorf("odf: ceph VM %s did not become reachable over SSH (see diagnostics above)", spec.IP)
	}
	script, err := renderBootstrap(spec)
	if err != nil {
		return err
	}
	if err := exec.RunNode(ctx, spec.IP, spec.sshUser(), script); err != nil {
		return fmt.Errorf("odf ceph bootstrap: %w", err)
	}
	return nil
}

// Export runs the rook external-cluster exporter on the ceph VM and returns its
// JSON (validated as a non-empty array).
func Export(ctx context.Context, exec executor, spec Spec) ([]byte, error) {
	script, err := renderExport(spec)
	if err != nil {
		return nil, err
	}
	out, err := exec.RunNodeCapture(ctx, spec.IP, spec.sshUser(), script)
	if err != nil {
		return nil, fmt.Errorf("odf ceph export: %w", err)
	}
	var arr []json.RawMessage
	if json.Unmarshal([]byte(out), &arr) != nil || len(arr) == 0 {
		return nil, fmt.Errorf("odf ceph export: exporter did not produce a non-empty JSON array")
	}
	return []byte(out), nil
}

// InstallOperator installs the ODF operator, resolving the Subscription channel
// against the catalog (resilient to a catalog that lacks the desired channel).
func InstallOperator(ctx context.Context, exec executor, spec Spec) error {
	return installOperator(ctx, exec, spec, time.Sleep)
}

func installOperator(ctx context.Context, exec executor, spec Spec, sleep func(time.Duration)) error {
	channel, err := olm.ResolveChannel(ctx, exec, ocCmd, "odf-operator", spec.ODFChannel)
	if err != nil {
		return fmt.Errorf("odf resolve channel: %w", err)
	}
	script, err := renderOperator(spec, channel)
	if err != nil {
		return err
	}
	if err := exec.Run(ctx, script); err != nil {
		return fmt.Errorf("odf install operator: %w", err)
	}
	waitCSV(ctx, exec, spec.ns(), "odf-operator", sleep)
	waitCSV(ctx, exec, spec.ns(), "ocs-operator", sleep)
	return nil
}

func waitCSV(ctx context.Context, exec executor, ns, prefix string, sleep func(time.Duration)) {
	cmd := fmt.Sprintf("%s -n %s get csv --no-headers -o custom-columns=N:.metadata.name,P:.status.phase 2>/dev/null | awk -v p=%q '$1 ~ (\"^\" p){print $2}' | head -1",
		ocCmd, ns, prefix)
	for i := 0; i < 60; i++ {
		out, err := exec.RunCapture(ctx, cmd)
		if err == nil && strings.TrimSpace(out) == "Succeeded" {
			return
		}
		sleep(15 * time.Second)
	}
}

// ImportExternal materializes the exporter JSON as the rook-ceph-* objects and
// the blob secret the external StorageCluster ingests (namespaces normalized).
func ImportExternal(ctx context.Context, exec executor, spec Spec, raw []byte) error {
	listJSON, blobJSON, err := buildImportManifests(raw, spec.ns())
	if err != nil {
		return fmt.Errorf("odf import: %w", err)
	}
	if err := ocApply(ctx, exec, listJSON); err != nil {
		return fmt.Errorf("odf import objects: %w", err)
	}
	if err := ocApply(ctx, exec, blobJSON); err != nil {
		return fmt.Errorf("odf import blob secret: %w", err)
	}
	return nil
}

// ocApply pipes a manifest to `oc apply -f -` on the host via a quoted heredoc
// (the shell never expands the machine-generated JSON).
func ocApply(ctx context.Context, exec executor, manifest []byte) error {
	return exec.Run(ctx, ocCmd+" apply -f - <<'OCPCTL_EOF'\n"+string(manifest)+"\nOCPCTL_EOF")
}

// CreateStorageCluster creates the external-mode StorageCluster and waits
// (best-effort) for it to report Ready.
func CreateStorageCluster(ctx context.Context, exec executor, spec Spec) error {
	return createStorageCluster(ctx, exec, spec, time.Sleep)
}

func createStorageCluster(ctx context.Context, exec executor, spec Spec, sleep func(time.Duration)) error {
	script, err := renderStorageCluster(spec)
	if err != nil {
		return err
	}
	if err := exec.Run(ctx, script); err != nil {
		return fmt.Errorf("odf create storagecluster: %w", err)
	}
	phaseCmd := fmt.Sprintf("%s -n %s get storagecluster ocs-external-storagecluster -o jsonpath='{.status.phase}' 2>/dev/null || true",
		ocCmd, spec.ns())
	for i := 0; i < 60; i++ {
		out, err := exec.RunCapture(ctx, phaseCmd)
		if err == nil && strings.TrimSpace(out) == "Ready" {
			return nil
		}
		sleep(20 * time.Second)
	}
	// Best-effort: not Ready yet is not fatal (the connection may still settle).
	return nil
}
