package host

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeBinDir writes stub executables for every command the remote scripts call,
// so Provision's rendered bash runs to completion in-process. `virsh domuuid`
// echoes a per-domain UUID derived from the domain name.
func fakeBinDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	stub := func(name, body string) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// virsh: domuuid <name> -> deterministic uuid; everything else -> ok.
	stub("virsh", `case "$1" in
  domuuid) echo "uuid-$2" ;;
  *) exit 0 ;;
esac`)
	stub("dnf", "exit 0")
	stub("systemctl", "exit 0")
	stub("sysctl", "exit 0")
	stub("setsebool", "exit 0")
	stub("qemu-img", "exit 0")
	stub("virt-install", "echo '<domain/>'")
	stub("openssl", "exit 0")
	stub("htpasswd", "exit 0")
	stub("podman", "exit 0")
	stub("curl", "exit 0")
	stub("tee", `d="$(dirname "$1")"; [ -d "$d" ] || mkdir -p "$d" 2>/dev/null || true; cat >"$1" 2>/dev/null || true`)
	stub("mkdir", "exit 0")
	stub("chmod", "exit 0")
	// /dev/kvm check in packages.sh.tmpl uses [[ -e /dev/kvm ]]; provide it via a
	// stub test that always passes by shadowing the test with `test`/`[[`? Instead
	// the packages template's kvm check is the one host-specific guard; the fake
	// `bash` PATH cannot fake /dev/kvm, so packages runs against the real /dev.
	return dir
}

func TestProvision(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); err != nil {
		t.Skip("provision test requires /dev/kvm for the packages sanity check")
	}
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServerWithPath(t, pub, fakeBinDir(t))
	defer cleanup()

	spec := testSpec() // from render_test.go (same package)
	var out strings.Builder
	c := NewClient(addr, "fedora", signer, &out)
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	res, err := Provision(ctx, c, spec)
	if err != nil {
		t.Fatalf("Provision: %v\n---out---\n%s", err, out.String())
	}
	if len(res.Nodes) != len(spec.Nodes) {
		t.Fatalf("Result.Nodes = %d, want %d", len(res.Nodes), len(spec.Nodes))
	}
	if got := res.UUIDs["rhwa-lab-master-0"]; got != "uuid-rhwa-lab-master-0" {
		t.Fatalf("master UUID = %q, want uuid-rhwa-lab-master-0", got)
	}
	if got := res.UUIDs["rhwa-lab-worker-1"]; got != "uuid-rhwa-lab-worker-1" {
		t.Fatalf("spare worker UUID = %q, want uuid-rhwa-lab-worker-1", got)
	}
}

func TestWaitSushyFailure(t *testing.T) {
	signer, pub := testKeyPair(t)
	binDir := t.TempDir()
	stub := func(name, body string) {
		p := filepath.Join(binDir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub("curl", "exit 1")
	stub("podman", "echo 'podman logs output'; exit 0")
	addr, cleanup := newTestSSHServerWithPath(t, pub, binDir)
	defer cleanup()

	spec := testSpec()
	var out strings.Builder
	c := NewClient(addr, "fedora", signer, &out)
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	err := c.waitSushyN(ctx, spec, 1, time.Millisecond)
	if err == nil {
		t.Fatal("waitSushyN: expected error when curl fails, got nil")
	}
	if !strings.Contains(err.Error(), "provision host sushy:") {
		t.Fatalf("error missing expected prefix: %v", err)
	}
	if !strings.Contains(out.String(), "podman logs output") {
		t.Fatalf("podman logs not captured; out:\n%s", out.String())
	}
}

func TestRetry(t *testing.T) {
	calls := 0
	err := retry(3, time.Millisecond, func() error {
		calls++
		if calls < 3 {
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	if err != nil {
		t.Fatalf("retry returned error after eventual success: %v", err)
	}
	if calls != 3 {
		t.Fatalf("retry made %d calls, want 3", calls)
	}
}
