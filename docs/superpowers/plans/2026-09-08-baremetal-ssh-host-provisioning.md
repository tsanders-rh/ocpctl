# Bare-metal SSH host-provisioning subsystem — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `internal/baremetal/host`, a self-contained Go package that SSHes into a freshly-launched nested-virt EC2 host and provisions it into an agent-based OpenShift substrate — libvirt + sushy-tools (emulated Redfish BMC) + haproxy, a libvirt NAT network with pinned reservations, and libvirt domains defined (but not started) for every node.

**Architecture:** A single `Client` abstraction wraps `golang.org/x/crypto/ssh` and is the only code aware SSH exists; it pipes rendered bash to `sudo bash -s`, captures small values, and uploads config files via `sudo tee`. Remote logic is rhwa-lab's proven, idempotent bash kept as ocpctl-owned `embed.FS` assets rendered with `text/template`. Node MAC/IP topology is computed in Go (single source of truth reused later by the agent-config renderer). `Provision` runs the steps in rhwa-lab's order and returns domain UUIDs (Redfish system ids) plus the computed topology in a result struct — nothing is persisted to a state file.

**Tech Stack:** Go 1.25, `golang.org/x/crypto/ssh` (already a direct dependency, v0.49.0), `embed` + `text/template` (mirrors `internal/profile/renderer.go`), stdlib `crypto/ssh` in-process server for tests (no new test dependency).

**Spec:** `docs/superpowers/specs/2026-09-08-baremetal-ssh-host-provisioning-design.md`

## Global Constraints

- Package path: `internal/baremetal/host`; module `github.com/tsanders-rh/ocpctl`; branch `feat/baremetal-agent-libvirt-provider`.
- Transport is `golang.org/x/crypto/ssh` only — never shell out to `ssh`/`scp`. No new module dependency (x/crypto v0.49.0 is already required).
- Host-key verification is `ssh.InsecureIgnoreHostKey()` for now, documented at the call site as a conscious trade-off (hardening follow-up: pin via EC2 `GetConsoleOutput`). Do not add host-key pinning in this slice.
- Remote logic stays as templated bash loaded via `embed.FS` + `text/template`; preserve rhwa-lab's idempotency guards (`virsh … >/dev/null 2>&1 || …`, `dominfo … && skip`) verbatim so re-running a failed create is safe.
- Remote scripts run under `set -euo pipefail`; any non-zero remote exit is a Go error.
- Scope stops at "host provisioned, domains defined-but-not-started, sushy serving." Do NOT implement `vms_boot`, `vms_teardown`, `Download`, a jump-host node client, or SFTP/agent-ISO upload — those belong to pieces 3 and 4.
- Follow existing ocpctl code style: no excessive comments, no inline comments other providers lack, match the surrounding idiom.
- MAC/IP scheme (ported verbatim from rhwa-lab `lib/common.sh`): masters IP `<net3>.11+i`, workers/spares IP `<net3>.21+idx`; MAC `52:54:00:6a:01:<i>` for masters and `52:54:00:6a:02:<idx>` for workers/spares (`%02x`). Spares continue the worker index sequence (`idx = WorkerCount + i`).

---

### Task 1: Node topology + shared types

**Files:**
- Create: `internal/baremetal/host/types.go`
- Create: `internal/baremetal/host/compute.go`
- Test: `internal/baremetal/host/compute_test.go`

**Interfaces:**
- Consumes: nothing (pure Go; no SSH, no AWS).
- Produces:
  - `type VM struct { Name string; Host string; Role string; IP string; MAC string; VCPU int; RAMGB int; Spare bool }`
  - `type HostSpec struct { ClusterName string; LibvirtNet string; NetCIDR string; NetGateway string; APIVIP string; IngressVIP string; SushyPort int; SushyUser string; SushyPass string; NodeDiskGB int; Nodes []VM }`
  - `type Result struct { UUIDs map[string]string; Nodes []VM }`
  - `type Topology struct { ClusterName string; NetCIDR string; ControlPlaneCount int; WorkerCount int; SpareCount int; CPVCPU int; CPRAMGB int; WKVCPU int; WKRAMGB int }`
  - `func ComputeNodes(t Topology) []VM` — returns masters, then workers, then spares (spares have `Spare: true`, `Role: "worker"`).
  - `func net3(cidr string) string` — unexported; `"192.168.126.0/24" → "192.168.126"`.

- [ ] **Step 1: Write the failing test**

```go
package host

import (
	"reflect"
	"testing"
)

func TestNet3(t *testing.T) {
	if got := net3("192.168.126.0/24"); got != "192.168.126" {
		t.Fatalf("net3 = %q, want 192.168.126", got)
	}
	if got := net3("10.0.5.0/24"); got != "10.0.5" {
		t.Fatalf("net3 = %q, want 10.0.5", got)
	}
}

func TestComputeNodes(t *testing.T) {
	got := ComputeNodes(Topology{
		ClusterName:       "rhwa-lab",
		NetCIDR:           "192.168.126.0/24",
		ControlPlaneCount: 3,
		WorkerCount:       3,
		SpareCount:        3,
		CPVCPU:            8, CPRAMGB: 20,
		WKVCPU: 4, WKRAMGB: 16,
	})
	want := []VM{
		{Name: "rhwa-lab-master-0", Host: "master-0", Role: "master", IP: "192.168.126.11", MAC: "52:54:00:6a:01:00", VCPU: 8, RAMGB: 20, Spare: false},
		{Name: "rhwa-lab-master-1", Host: "master-1", Role: "master", IP: "192.168.126.12", MAC: "52:54:00:6a:01:01", VCPU: 8, RAMGB: 20, Spare: false},
		{Name: "rhwa-lab-master-2", Host: "master-2", Role: "master", IP: "192.168.126.13", MAC: "52:54:00:6a:01:02", VCPU: 8, RAMGB: 20, Spare: false},
		{Name: "rhwa-lab-worker-0", Host: "worker-0", Role: "worker", IP: "192.168.126.21", MAC: "52:54:00:6a:02:00", VCPU: 4, RAMGB: 16, Spare: false},
		{Name: "rhwa-lab-worker-1", Host: "worker-1", Role: "worker", IP: "192.168.126.22", MAC: "52:54:00:6a:02:01", VCPU: 4, RAMGB: 16, Spare: false},
		{Name: "rhwa-lab-worker-2", Host: "worker-2", Role: "worker", IP: "192.168.126.23", MAC: "52:54:00:6a:02:02", VCPU: 4, RAMGB: 16, Spare: false},
		{Name: "rhwa-lab-worker-3", Host: "worker-3", Role: "worker", IP: "192.168.126.24", MAC: "52:54:00:6a:02:03", VCPU: 4, RAMGB: 16, Spare: true},
		{Name: "rhwa-lab-worker-4", Host: "worker-4", Role: "worker", IP: "192.168.126.25", MAC: "52:54:00:6a:02:04", VCPU: 4, RAMGB: 16, Spare: true},
		{Name: "rhwa-lab-worker-5", Host: "worker-5", Role: "worker", IP: "192.168.126.26", MAC: "52:54:00:6a:02:05", VCPU: 4, RAMGB: 16, Spare: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComputeNodes mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl && go test ./internal/baremetal/host/ -run 'TestNet3|TestComputeNodes' -v`
Expected: FAIL — package does not compile (`undefined: net3`, `undefined: ComputeNodes`, `undefined: Topology`, `undefined: VM`).

- [ ] **Step 3: Write the shared types**

Create `internal/baremetal/host/types.go`:

```go
package host

// VM is one libvirt domain backing an OpenShift node. Spares carry Role
// "worker" but are left unconsumed (defined and BMC-addressable, never booted
// during install).
type VM struct {
	Name  string // libvirt domain name, e.g. rhwa-lab-master-0
	Host  string // hostname / DHCP reservation name, e.g. master-0
	Role  string // master | worker
	IP    string
	MAC   string
	VCPU  int
	RAMGB int
	Spare bool
}

// HostSpec is the full input to Provision, built by the caller from the
// profile and the AWS substrate output (piece 1).
type HostSpec struct {
	ClusterName string
	LibvirtNet  string
	NetCIDR     string
	NetGateway  string
	APIVIP      string
	IngressVIP  string
	SushyPort   int
	SushyUser   string
	SushyPass   string
	NodeDiskGB  int
	Nodes       []VM
}

// Result is what Provision returns: domain UUIDs (Redfish system ids) keyed by
// domain name, and the computed topology for piece 3.
type Result struct {
	UUIDs map[string]string
	Nodes []VM
}

// Topology is the input to ComputeNodes.
type Topology struct {
	ClusterName       string
	NetCIDR           string
	ControlPlaneCount int
	WorkerCount       int
	SpareCount        int
	CPVCPU            int
	CPRAMGB           int
	WKVCPU            int
	WKRAMGB           int
}
```

- [ ] **Step 4: Write the topology computation**

Create `internal/baremetal/host/compute.go`:

```go
package host

import (
	"fmt"
	"strings"
)

func net3(cidr string) string {
	ip := cidr
	if i := strings.IndexByte(ip, '/'); i >= 0 {
		ip = ip[:i]
	}
	if i := strings.LastIndexByte(ip, '.'); i >= 0 {
		return ip[:i]
	}
	return ip
}

// ComputeNodes ports rhwa-lab's compute_nodes/compute_spares: masters .11+,
// workers/spares .21+, MAC 52:54:00:6a:<01|02>:<idx>. Spares continue the
// worker index sequence and are indistinguishable from installed workers
// except for the Spare flag.
func ComputeNodes(t Topology) []VM {
	n3 := net3(t.NetCIDR)
	vms := make([]VM, 0, t.ControlPlaneCount+t.WorkerCount+t.SpareCount)
	for i := 0; i < t.ControlPlaneCount; i++ {
		vms = append(vms, VM{
			Name:  fmt.Sprintf("%s-master-%d", t.ClusterName, i),
			Host:  fmt.Sprintf("master-%d", i),
			Role:  "master",
			IP:    fmt.Sprintf("%s.%d", n3, 11+i),
			MAC:   fmt.Sprintf("52:54:00:6a:01:%02x", i),
			VCPU:  t.CPVCPU,
			RAMGB: t.CPRAMGB,
		})
	}
	for i := 0; i < t.WorkerCount; i++ {
		vms = append(vms, VM{
			Name:  fmt.Sprintf("%s-worker-%d", t.ClusterName, i),
			Host:  fmt.Sprintf("worker-%d", i),
			Role:  "worker",
			IP:    fmt.Sprintf("%s.%d", n3, 21+i),
			MAC:   fmt.Sprintf("52:54:00:6a:02:%02x", i),
			VCPU:  t.WKVCPU,
			RAMGB: t.WKRAMGB,
		})
	}
	for i := 0; i < t.SpareCount; i++ {
		idx := t.WorkerCount + i
		vms = append(vms, VM{
			Name:  fmt.Sprintf("%s-worker-%d", t.ClusterName, idx),
			Host:  fmt.Sprintf("worker-%d", idx),
			Role:  "worker",
			IP:    fmt.Sprintf("%s.%d", n3, 21+idx),
			MAC:   fmt.Sprintf("52:54:00:6a:02:%02x", idx),
			VCPU:  t.WKVCPU,
			RAMGB: t.WKRAMGB,
			Spare: true,
		})
	}
	return vms
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl && go test ./internal/baremetal/host/ -run 'TestNet3|TestComputeNodes' -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl
git add internal/baremetal/host/types.go internal/baremetal/host/compute.go internal/baremetal/host/compute_test.go
git commit -m "baremetal/host: node topology computation and shared types"
```

---

### Task 2: Host SSH client

**Files:**
- Create: `internal/baremetal/host/client.go`
- Test: `internal/baremetal/host/client_test.go`
- Test helper (in `client_test.go`, reused by Task 4): `newTestSSHServer`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces:
  - `func NewClient(addr, user string, signer ssh.Signer, out io.Writer) *Client`
  - `func (c *Client) WaitReachable(ctx context.Context, timeout time.Duration) error`
  - `func (c *Client) Connect(ctx context.Context) error`
  - `func (c *Client) Close() error`
  - `func (c *Client) Run(ctx context.Context, script string) error` — pipes `script` to `sudo bash -s`.
  - `func (c *Client) RunCapture(ctx context.Context, cmd string) (string, error)` — runs `cmd` verbatim, returns trimmed stdout; stderr streams to `out`.
  - `func (c *Client) Upload(ctx context.Context, content io.Reader, remotePath string, mode os.FileMode) error` — via `sudo tee` + `sudo chmod`.
  - test helper `func newTestSSHServer(t *testing.T, authorized ssh.PublicKey) (addr string, cleanup func())` — an in-process stdlib `crypto/ssh` server that authenticates `authorized`, executes each exec request via `/bin/sh -c` after stripping a leading `sudo ` token, wiring stdin/stdout/stderr and exit status.

- [ ] **Step 1: Write the failing test**

```go
package host

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testKeyPair returns a signer for the client and the matching public key to
// authorize on the server.
func testKeyPair(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromSigner(priv)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return signer, sshPub
}

// newTestSSHServer starts an in-process SSH server that authenticates
// `authorized` via publickey and executes each "exec" request through the local
// shell. A leading "sudo " is stripped so scripts written for the real host
// (which run as root via sudo) execute unprivileged in tests. Returns the
// listener address and a cleanup func.
func newTestSSHServer(t *testing.T, authorized ssh.PublicKey) (string, func()) {
	t.Helper()
	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner, err := ssh.NewSignerFromSigner(hostPriv)
	if err != nil {
		t.Fatal(err)
	}
	authMarshaled := authorized.Marshal()
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if bytes.Equal(key.Marshal(), authMarshaled) {
				return &ssh.Permissions{}, nil
			}
			return nil, errors.New("unauthorized")
		},
	}
	cfg.AddHostKey(hostSigner)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			nConn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveTestConn(nConn, cfg)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestWaitReachableAndRun(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	var out bytes.Buffer
	c := NewClient(addr, "fedora", signer, &out)
	ctx := context.Background()

	if err := c.WaitReachable(ctx, 5*time.Second); err != nil {
		t.Fatalf("WaitReachable: %v", err)
	}
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	// Run pipes the script to `sudo bash -s`; the test server strips sudo and
	// runs `bash -s`, so the script executes and its stdout streams to out.
	if err := c.Run(ctx, "echo hello-from-script"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "hello-from-script") {
		t.Fatalf("Run output not streamed to out: %q", out.String())
	}
}

func TestRunNonZeroExitIsError(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	c := NewClient(addr, "fedora", signer, io.Discard)
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Run(ctx, "exit 7"); err == nil {
		t.Fatal("expected error for non-zero remote exit, got nil")
	}
}

func TestRunCaptureSplitsStdoutStderr(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	var out bytes.Buffer
	c := NewClient(addr, "fedora", signer, &out)
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	got, err := c.RunCapture(ctx, "printf out-value; printf err-value 1>&2")
	if err != nil {
		t.Fatalf("RunCapture: %v", err)
	}
	if got != "out-value" {
		t.Fatalf("RunCapture stdout = %q, want %q", got, "out-value")
	}
	if !strings.Contains(out.String(), "err-value") {
		t.Fatalf("stderr not streamed to out: %q", out.String())
	}
	if strings.Contains(out.String(), "out-value") {
		t.Fatalf("stdout leaked into out: %q", out.String())
	}
}

func TestUpload(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	c := NewClient(addr, "fedora", signer, io.Discard)
	ctx := context.Background()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	dst := filepath.Join(t.TempDir(), "uploaded.txt")
	if err := c.Upload(ctx, strings.NewReader("file-body"), dst, 0o644); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	body, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read uploaded: %v", err)
	}
	if string(body) != "file-body" {
		t.Fatalf("uploaded content = %q, want %q", body, "file-body")
	}
}

func TestRunContextCancel(t *testing.T) {
	signer, pub := testKeyPair(t)
	addr, cleanup := newTestSSHServer(t, pub)
	defer cleanup()

	c := NewClient(addr, "fedora", signer, io.Discard)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(200 * time.Millisecond); cancel() }()
	err := c.Run(ctx, "sleep 10")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
}
```

- [ ] **Step 2: Add the test server exec handler to `client_test.go`**

Append the connection/exec handler used by `newTestSSHServer`:

```go
func serveTestConn(nConn net.Conn, cfg *ssh.ServerConfig) {
	sConn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		nConn.Close()
		return
	}
	defer sConn.Close()
	go ssh.DiscardRequests(reqs)
	for newChan := range chans {
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "only session")
			continue
		}
		ch, chReqs, err := newChan.Accept()
		if err != nil {
			continue
		}
		go handleTestSession(ch, chReqs)
	}
}

func handleTestSession(ch ssh.Channel, reqs <-chan *ssh.Request) {
	for req := range reqs {
		if req.Type != "exec" {
			req.Reply(false, nil)
			continue
		}
		var payload struct{ Command string }
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
			req.Reply(false, nil)
			continue
		}
		req.Reply(true, nil)
		// Strip a leading "sudo " so root-only scripts run unprivileged here.
		cmd := strings.TrimPrefix(payload.Command, "sudo ")
		sh := exec.Command("/bin/sh", "-c", cmd)
		sh.Stdin = ch
		sh.Stdout = ch
		sh.Stderr = ch.Stderr()
		code := 0
		if err := sh.Run(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		status := struct{ Status uint32 }{uint32(code)}
		ch.SendRequest("exit-status", false, ssh.Marshal(&status))
		ch.Close()
		return
	}
}
```

Add `"os/exec"` to the `client_test.go` import block.

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl && go test ./internal/baremetal/host/ -run 'TestWaitReachableAndRun|TestRun|TestUpload' -v`
Expected: FAIL — `undefined: NewClient`, `undefined: Client`.

- [ ] **Step 4: Write the client**

Create `internal/baremetal/host/client.go`:

```go
package host

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Client is the single abstraction every provisioning step goes through; it is
// the only code that knows crypto/ssh exists. One *ssh.Client is opened by
// Connect and reused across steps; each Run/RunCapture/Upload opens a fresh
// one-shot session.
type Client struct {
	addr   string // "<host-ip>:22"
	user   string // cloud user, e.g. "fedora"
	signer ssh.Signer
	out    io.Writer

	conn *ssh.Client
}

func NewClient(addr, user string, signer ssh.Signer, out io.Writer) *Client {
	return &Client{addr: addr, user: user, signer: signer, out: out}
}

func (c *Client) clientConfig() *ssh.ClientConfig {
	return &ssh.ClientConfig{
		User:            c.user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(c.signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // self-launched instance; hardening: pin via EC2 GetConsoleOutput
		Timeout:         15 * time.Second,
	}
}

// WaitReachable polls Dial until SSH answers or ctx/timeout expires — the host
// has just booted. It does not retain the connection.
func (c *Client) WaitReachable(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	cfg := c.clientConfig()
	for {
		conn, err := ssh.Dial("tcp", c.addr, cfg)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("host %s not reachable over SSH within %s: %w", c.addr, timeout, err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// Connect establishes the reused *ssh.Client.
func (c *Client) Connect(ctx context.Context) error {
	conn, err := ssh.Dial("tcp", c.addr, c.clientConfig())
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", c.addr, err)
	}
	c.conn = conn
	return nil
}

func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Run pipes script to `sudo bash -s`, streaming combined stdout/stderr to out.
func (c *Client) Run(ctx context.Context, script string) error {
	sess, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()
	sess.Stdout = c.out
	sess.Stderr = c.out
	stdin, err := sess.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	if err := sess.Start("sudo bash -s"); err != nil {
		return fmt.Errorf("start remote shell: %w", err)
	}
	go func() {
		io.WriteString(stdin, script)
		stdin.Close()
	}()
	return c.wait(ctx, sess)
}

// RunCapture runs cmd verbatim and returns trimmed stdout; stderr streams to
// out. Used for small values (domuuid, health checks, podman logs).
func (c *Client) RunCapture(ctx context.Context, cmd string) (string, error) {
	sess, err := c.conn.NewSession()
	if err != nil {
		return "", fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()
	var stdout strings.Builder
	sess.Stdout = &stdout
	sess.Stderr = c.out
	if err := sess.Start(cmd); err != nil {
		return "", fmt.Errorf("start %q: %w", cmd, err)
	}
	if err := c.wait(ctx, sess); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Upload writes content to remotePath via `sudo tee`, then chmods to mode.
func (c *Client) Upload(ctx context.Context, content io.Reader, remotePath string, mode os.FileMode) error {
	sess, err := c.conn.NewSession()
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}
	defer sess.Close()
	sess.Stdin = content
	sess.Stdout = io.Discard
	sess.Stderr = c.out
	cmd := fmt.Sprintf("sudo tee %s >/dev/null && sudo chmod %o %s", remotePath, mode.Perm(), remotePath)
	if err := sess.Start(cmd); err != nil {
		return fmt.Errorf("start upload %s: %w", remotePath, err)
	}
	if err := c.wait(ctx, sess); err != nil {
		return fmt.Errorf("upload %s: %w", remotePath, err)
	}
	return nil
}

// wait blocks on the session, closing the underlying connection if ctx is
// cancelled (crypto/ssh sessions are not context-aware).
func (c *Client) wait(ctx context.Context, sess *ssh.Session) error {
	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()
	select {
	case <-ctx.Done():
		c.conn.Close()
		return ctx.Err()
	case err := <-done:
		return err
	}
}

var _ = net.Dial // ensure net import retained if unused after edits
```

Note: remove the trailing `var _ = net.Dial` line and the `"net"` import if `net` ends up unused — it is only listed here defensively. Run `gofmt`/`goimports` and drop unused imports before committing.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl && go test ./internal/baremetal/host/ -run 'TestWaitReachableAndRun|TestRun|TestUpload' -v`
Expected: PASS (all five client tests).

- [ ] **Step 6: Commit**

```bash
cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl
git add internal/baremetal/host/client.go internal/baremetal/host/client_test.go
git commit -m "baremetal/host: crypto/ssh host client (Run/RunCapture/Upload/WaitReachable)"
```

---

### Task 3: Remote payload templates + rendering

**Files:**
- Create: `internal/baremetal/host/templates/packages.sh.tmpl`
- Create: `internal/baremetal/host/templates/storage-pool.sh.tmpl`
- Create: `internal/baremetal/host/templates/libvirt-net.xml.tmpl`
- Create: `internal/baremetal/host/templates/libvirt-net.sh.tmpl`
- Create: `internal/baremetal/host/templates/haproxy.cfg.tmpl`
- Create: `internal/baremetal/host/templates/haproxy.sh.tmpl`
- Create: `internal/baremetal/host/templates/sushy.sh.tmpl`
- Create: `internal/baremetal/host/templates/domain.sh.tmpl`
- Create: `internal/baremetal/host/render.go`
- Test: `internal/baremetal/host/render_test.go`

**Interfaces:**
- Consumes: `HostSpec`, `VM`, `net3` (Task 1).
- Produces (all return `(string, error)`):
  - `func renderPackages() (string, error)`
  - `func renderStoragePool() (string, error)`
  - `func renderLibvirtNetXML(spec *HostSpec) (string, error)`
  - `func renderLibvirtNetDefine(spec *HostSpec) (string, error)`
  - `func renderHAProxyCfg(spec *HostSpec) (string, error)`
  - `func renderHAProxyReload() (string, error)`
  - `func renderSushy(spec *HostSpec) (string, error)`
  - `func renderDomain(spec *HostSpec, vm VM) (string, error)`
  - `const remoteNetXMLPath` and `const remoteHAProxyCfgPath` — remote upload targets used by both this task and Task 4.

- [ ] **Step 1: Create the template files**

`templates/packages.sh.tmpl` (ported verbatim from rhwa-lab `host_install_packages`; no template vars):

```bash
set -euo pipefail
dnf -y install qemu-kvm libvirt virt-install libvirt-client \
     podman haproxy jq httpd-tools openssl bind-utils nmstate >/dev/null
systemctl enable --now libvirtd
if [[ ! -e /dev/kvm ]]; then echo "ERROR: /dev/kvm missing - nested virt not active"; exit 1; fi
sysctl -w net.ipv4.ip_forward=1 >/dev/null
setsebool -P haproxy_connect_any 1 2>/dev/null || true
echo "host packages OK"
```

`templates/storage-pool.sh.tmpl` (ported from `host_storage_pool`):

```bash
set -euo pipefail
mkdir -p /var/lib/libvirt/images
virsh pool-info default >/dev/null 2>&1 || virsh pool-define-as default dir --target /var/lib/libvirt/images
virsh pool-autostart default 2>/dev/null || true
virsh pool-start default 2>/dev/null || true
virsh pool-refresh default 2>/dev/null || true
echo "storage pool default OK"
```

`templates/libvirt-net.xml.tmpl` (ported from `host_libvirt_network`; reservations for every node incl. spares):

```xml
<network>
  <name>{{ .LibvirtNet }}</name>
  <forward mode='nat'/>
  <bridge name='virbr-rhwa' stp='on' delay='0'/>
  <ip address='{{ .NetGateway }}' netmask='255.255.255.0'>
    <dhcp>
      <range start='{{ .Net3 }}.100' end='{{ .Net3 }}.199'/>
{{- range .Nodes }}
      <host mac='{{ .MAC }}' name='{{ .Host }}' ip='{{ .IP }}'/>
{{- end }}
    </dhcp>
  </ip>
</network>
```

`templates/libvirt-net.sh.tmpl` (ported define/autostart/start; reads the uploaded XML):

```bash
set -euo pipefail
virsh net-info {{ .LibvirtNet }} >/dev/null 2>&1 || virsh net-define {{ .NetXMLPath }}
virsh net-autostart {{ .LibvirtNet }} 2>/dev/null || true
virsh net-start {{ .LibvirtNet }} 2>/dev/null || true
echo "libvirt network {{ .LibvirtNet }} OK"
```

`templates/haproxy.cfg.tmpl` (ported verbatim from `host_haproxy`):

```
global
    log /dev/log local0
    maxconn 4000
defaults
    mode tcp
    log global
    option tcplog
    timeout connect 10s
    timeout client 5m
    timeout server 5m
frontend api
    bind *:6443
    default_backend api
backend api
    server apivip {{ .APIVIP }}:6443 check
frontend ingress_https
    bind *:443
    default_backend ingress_https
backend ingress_https
    server ingressvip {{ .IngressVIP }}:443 check
frontend ingress_http
    bind *:80
    default_backend ingress_http
backend ingress_http
    server ingressvip {{ .IngressVIP }}:80 check
```

`templates/haproxy.sh.tmpl`:

```bash
set -euo pipefail
systemctl enable --now haproxy
systemctl restart haproxy
echo "haproxy OK"
```

`templates/sushy.sh.tmpl` (ported verbatim from `vms_sushy`; conf paths are the in-container mount `/etc/sushy` since `/etc/rhwa-sushy` is bind-mounted there):

```bash
set -euo pipefail
mkdir -p /etc/rhwa-sushy
if [[ ! -f /etc/rhwa-sushy/cert.pem ]]; then
  openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout /etc/rhwa-sushy/key.pem -out /etc/rhwa-sushy/cert.pem \
    -days 3650 -subj "/CN={{ .NetGateway }}" >/dev/null 2>&1
fi
htpasswd -bcB /etc/rhwa-sushy/htpasswd '{{ .SushyUser }}' '{{ .SushyPass }}' >/dev/null 2>&1
tee /etc/rhwa-sushy/sushy-emulator.conf >/dev/null <<CONF
SUSHY_EMULATOR_LISTEN_IP = u'0.0.0.0'
SUSHY_EMULATOR_LISTEN_PORT = {{ .SushyPort }}
SUSHY_EMULATOR_LIBVIRT_URI = u'qemu:///system'
SUSHY_EMULATOR_IGNORE_BOOT_DEVICE = False
SUSHY_EMULATOR_VMEDIA_VERIFY_SSL = False
SUSHY_EMULATOR_SSL_CERT = u'/etc/sushy/cert.pem'
SUSHY_EMULATOR_SSL_KEY = u'/etc/sushy/key.pem'
SUSHY_EMULATOR_AUTH_FILE = u'/etc/sushy/htpasswd'
CONF
podman rm -f sushy >/dev/null 2>&1 || true
podman run -d --name sushy --restart always \
  --net host --privileged \
  -v /var/run/libvirt:/var/run/libvirt \
  -v /etc/rhwa-sushy:/etc/sushy:ro \
  quay.io/metal3-io/sushy-tools:latest \
  sushy-emulator --config /etc/sushy/sushy-emulator.conf >/dev/null
echo "sushy started"
```

`templates/domain.sh.tmpl` (ported verbatim from `_vm_define_domain`; masters weld the agent ISO cdrom, workers/spares get an empty tray):

```bash
set -e
if virsh dominfo '{{ .Name }}' >/dev/null 2>&1; then
  echo "domain {{ .Name }} exists, skipping"
else
  qemu-img create -f qcow2 /var/lib/libvirt/images/{{ .Name }}.qcow2 {{ .NodeDiskGB }}G >/dev/null
  virt-install \
    --name '{{ .Name }}' \
    --memory {{ .RAMMB }} \
    --vcpus {{ .VCPU }} \
    --cpu host-passthrough \
    --os-variant rhel9.4 \
    --disk path=/var/lib/libvirt/images/{{ .Name }}.qcow2,bus=virtio \
    {{ .CDROM }} \
    --network network={{ .LibvirtNet }},mac='{{ .MAC }}',model=virtio \
    --boot uefi,hd,cdrom \
    --graphics none --noautoconsole --import --print-xml 1 > /tmp/{{ .Name }}.xml
  virsh define /tmp/{{ .Name }}.xml >/dev/null
fi
```

- [ ] **Step 2: Write the failing render tests**

Create `internal/baremetal/host/render_test.go`:

```go
package host

import (
	"strings"
	"testing"
)

func testSpec() *HostSpec {
	return &HostSpec{
		ClusterName: "rhwa-lab",
		LibvirtNet:  "rhwa",
		NetCIDR:     "192.168.126.0/24",
		NetGateway:  "192.168.126.1",
		APIVIP:      "192.168.126.5",
		IngressVIP:  "192.168.126.6",
		SushyPort:   8000,
		SushyUser:   "admin",
		SushyPass:   "password",
		NodeDiskGB:  120,
		Nodes: []VM{
			{Name: "rhwa-lab-master-0", Host: "master-0", Role: "master", IP: "192.168.126.11", MAC: "52:54:00:6a:01:00", VCPU: 8, RAMGB: 20},
			{Name: "rhwa-lab-worker-0", Host: "worker-0", Role: "worker", IP: "192.168.126.21", MAC: "52:54:00:6a:02:00", VCPU: 4, RAMGB: 16},
			{Name: "rhwa-lab-worker-1", Host: "worker-1", Role: "worker", IP: "192.168.126.22", MAC: "52:54:00:6a:02:01", VCPU: 4, RAMGB: 16, Spare: true},
		},
	}
}

func TestRenderLibvirtNetXML(t *testing.T) {
	got, err := renderLibvirtNetXML(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<name>rhwa</name>",
		"<range start='192.168.126.100' end='192.168.126.199'/>",
		"<host mac='52:54:00:6a:01:00' name='master-0' ip='192.168.126.11'/>",
		"<host mac='52:54:00:6a:02:01' name='worker-1' ip='192.168.126.22'/>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("net XML missing %q:\n%s", want, got)
		}
	}
}

func TestRenderHAProxyCfg(t *testing.T) {
	got, err := renderHAProxyCfg(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "server apivip 192.168.126.5:6443 check") {
		t.Fatalf("haproxy cfg missing api vip:\n%s", got)
	}
	if !strings.Contains(got, "server ingressvip 192.168.126.6:443 check") {
		t.Fatalf("haproxy cfg missing ingress https vip:\n%s", got)
	}
	if !strings.Contains(got, "server ingressvip 192.168.126.6:80 check") {
		t.Fatalf("haproxy cfg missing ingress http vip:\n%s", got)
	}
}

func TestRenderSushy(t *testing.T) {
	got, err := renderSushy(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"SUSHY_EMULATOR_LISTEN_PORT = 8000",
		"/CN=192.168.126.1",
		"htpasswd -bcB /etc/rhwa-sushy/htpasswd 'admin' 'password'",
		"quay.io/metal3-io/sushy-tools:latest",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sushy script missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDomainMasterGetsAgentISO(t *testing.T) {
	spec := testSpec()
	got, err := renderDomain(spec, spec.Nodes[0]) // master
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "path=/var/lib/libvirt/images/rhwa-lab-agent.iso,device=cdrom") {
		t.Fatalf("master domain missing agent ISO cdrom:\n%s", got)
	}
	if !strings.Contains(got, "qemu-img create -f qcow2 /var/lib/libvirt/images/rhwa-lab-master-0.qcow2 120G") {
		t.Fatalf("master domain missing disk create:\n%s", got)
	}
	if !strings.Contains(got, "--memory 20480") {
		t.Fatalf("master domain RAM MB wrong (want 20480):\n%s", got)
	}
}

func TestRenderDomainWorkerGetsEmptyTray(t *testing.T) {
	spec := testSpec()
	got, err := renderDomain(spec, spec.Nodes[1]) // worker
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "--disk device=cdrom,bus=sata") {
		t.Fatalf("worker domain missing empty cdrom tray:\n%s", got)
	}
	if strings.Contains(got, "agent.iso") {
		t.Fatalf("worker domain should not weld agent ISO:\n%s", got)
	}
}

func TestRenderStaticScriptsParse(t *testing.T) {
	if _, err := renderPackages(); err != nil {
		t.Fatalf("renderPackages: %v", err)
	}
	if _, err := renderStoragePool(); err != nil {
		t.Fatalf("renderStoragePool: %v", err)
	}
	if _, err := renderHAProxyReload(); err != nil {
		t.Fatalf("renderHAProxyReload: %v", err)
	}
	got, err := renderLibvirtNetDefine(testSpec())
	if err != nil {
		t.Fatalf("renderLibvirtNetDefine: %v", err)
	}
	if !strings.Contains(got, "virsh net-info rhwa") {
		t.Fatalf("net define missing net name:\n%s", got)
	}
	if !strings.Contains(got, remoteNetXMLPath) {
		t.Fatalf("net define missing xml path %q:\n%s", remoteNetXMLPath, got)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl && go test ./internal/baremetal/host/ -run 'TestRender' -v`
Expected: FAIL — `undefined: renderLibvirtNetXML` etc.

- [ ] **Step 4: Write the renderer**

Create `internal/baremetal/host/render.go`:

```go
package host

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

var tmpls = template.Must(template.ParseFS(templatesFS, "templates/*.tmpl"))

const (
	remoteNetXMLPath    = "/tmp/rhwa-net.xml"
	remoteHAProxyCfgPath = "/etc/haproxy/haproxy.cfg"
)

func render(name string, data any) (string, error) {
	var buf strings.Builder
	if err := tmpls.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	return buf.String(), nil
}

func renderPackages() (string, error)    { return render("packages.sh.tmpl", nil) }
func renderStoragePool() (string, error) { return render("storage-pool.sh.tmpl", nil) }
func renderHAProxyReload() (string, error) {
	return render("haproxy.sh.tmpl", nil)
}

func renderLibvirtNetXML(spec *HostSpec) (string, error) {
	return render("libvirt-net.xml.tmpl", struct {
		LibvirtNet string
		NetGateway string
		Net3       string
		Nodes      []VM
	}{spec.LibvirtNet, spec.NetGateway, net3(spec.NetCIDR), spec.Nodes})
}

func renderLibvirtNetDefine(spec *HostSpec) (string, error) {
	return render("libvirt-net.sh.tmpl", struct {
		LibvirtNet string
		NetXMLPath string
	}{spec.LibvirtNet, remoteNetXMLPath})
}

func renderHAProxyCfg(spec *HostSpec) (string, error) {
	return render("haproxy.cfg.tmpl", spec)
}

func renderSushy(spec *HostSpec) (string, error) {
	return render("sushy.sh.tmpl", spec)
}

func renderDomain(spec *HostSpec, vm VM) (string, error) {
	cdrom := "--disk device=cdrom,bus=sata"
	if vm.Role == "master" {
		cdrom = fmt.Sprintf("--disk path=/var/lib/libvirt/images/%s-agent.iso,device=cdrom", spec.ClusterName)
	}
	return render("domain.sh.tmpl", struct {
		Name       string
		RAMMB      int
		VCPU       int
		MAC        string
		CDROM      string
		LibvirtNet string
		NodeDiskGB int
	}{vm.Name, vm.RAMGB * 1024, vm.VCPU, vm.MAC, cdrom, spec.LibvirtNet, spec.NodeDiskGB})
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl && go test ./internal/baremetal/host/ -run 'TestRender' -v`
Expected: PASS (all render tests).

- [ ] **Step 6: Commit**

```bash
cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl
git add internal/baremetal/host/render.go internal/baremetal/host/render_test.go internal/baremetal/host/templates/
git commit -m "baremetal/host: embedded remote payload templates and renderers"
```

---

### Task 4: Provision orchestration

**Files:**
- Create: `internal/baremetal/host/provision.go`
- Test: `internal/baremetal/host/provision_test.go`

**Interfaces:**
- Consumes: `Client` + `Connect`/`Run`/`RunCapture`/`Upload` (Task 2); `HostSpec`, `VM`, `Result` (Task 1); all `render*` funcs + `remoteNetXMLPath`/`remoteHAProxyCfgPath` (Task 3); `newTestSSHServer`/`testKeyPair` (Task 2 test helpers).
- Produces:
  - `func Provision(ctx context.Context, c *Client, spec *HostSpec) (*Result, error)` — runs packages → storage pool → libvirt net → haproxy → sushy (+health retry) → define domains → record UUIDs; returns `Result{UUIDs, Nodes: spec.Nodes}`. Assumes `c.Connect` has already succeeded (caller opens the connection after `WaitReachable`).
  - unexported `func retry(n int, delay time.Duration, fn func() error) error`.

- [ ] **Step 1: Write the failing test**

The test drives `Provision` end-to-end against the in-process SSH server from Task 2, with a fake `virsh`/`curl`/`podman`/`htpasswd`/etc. on `PATH` so the remote scripts succeed and `virsh domuuid` returns a deterministic UUID.

Create `internal/baremetal/host/provision_test.go`:

```go
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
	stub("tee", `cat >/dev/null`)
	stub("mkdir", "exit 0")
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
```

- [ ] **Step 2: Extend the test server helper to inject a fake PATH**

The Task 2 server executes commands with the process environment. Add a variant that prepends a fake bin dir to `PATH` for the executed shell. In `client_test.go`, refactor `handleTestSession` to accept an optional extra PATH entry, and add `newTestSSHServerWithPath`. Replace the body of `handleTestSession`'s command execution so it sets the environment:

```go
// in client_test.go — add alongside newTestSSHServer:

func newTestSSHServerWithPath(t *testing.T, authorized ssh.PublicKey, binDir string) (string, func()) {
	t.Helper()
	return startTestServer(t, authorized, binDir)
}
```

Refactor the existing `newTestSSHServer` to delegate:

```go
func newTestSSHServer(t *testing.T, authorized ssh.PublicKey) (string, func()) {
	t.Helper()
	return startTestServer(t, authorized, "")
}
```

Add `startTestServer` (the former body of `newTestSSHServer`, now threading `binDir` into `serveTestConn` → `handleTestSession`), and update the exec in `handleTestSession` to prepend `binDir`:

```go
sh := exec.Command("/bin/sh", "-c", cmd)
sh.Stdin = ch
sh.Stdout = ch
sh.Stderr = ch.Stderr()
if binDir != "" {
	sh.Env = append(os.Environ(), "PATH="+binDir+":"+os.Getenv("PATH"))
}
```

(Thread `binDir string` through `startTestServer`, `serveTestConn`, and `handleTestSession`.) Add `"os"` to the `client_test.go` imports if not already present.

- [ ] **Step 3: Run test to verify it fails**

Run: `cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl && go test ./internal/baremetal/host/ -run 'TestProvision|TestRetry' -v`
Expected: FAIL — `undefined: Provision`, `undefined: retry`.

- [ ] **Step 4: Write the orchestrator**

Create `internal/baremetal/host/provision.go`:

```go
package host

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Provision brings a freshly-launched host up to "provisioned": libvirt +
// sushy-tools + haproxy installed and running, the libvirt NAT network defined
// with pinned reservations, and every domain defined (not started). The caller
// opens c (WaitReachable + Connect) beforehand. Steps run in rhwa-lab's order
// and preserve its idempotency guards, so re-running a failed create is safe.
func Provision(ctx context.Context, c *Client, spec *HostSpec) (*Result, error) {
	steps := []struct {
		name   string
		render func() (string, error)
	}{
		{"packages", renderPackages},
		{"storage pool", renderStoragePool},
	}
	for _, s := range steps {
		script, err := s.render()
		if err != nil {
			return nil, err
		}
		c.logf("=== %s ===", s.name)
		if err := c.Run(ctx, script); err != nil {
			return nil, fmt.Errorf("provision host %s: %w", s.name, err)
		}
	}

	// libvirt network: upload XML, then define.
	c.logf("=== libvirt network ===")
	netXML, err := renderLibvirtNetXML(spec)
	if err != nil {
		return nil, err
	}
	if err := c.Upload(ctx, strings.NewReader(netXML), remoteNetXMLPath, 0o644); err != nil {
		return nil, fmt.Errorf("provision host libvirt net xml: %w", err)
	}
	netDefine, err := renderLibvirtNetDefine(spec)
	if err != nil {
		return nil, err
	}
	if err := c.Run(ctx, netDefine); err != nil {
		return nil, fmt.Errorf("provision host libvirt net: %w", err)
	}

	// haproxy: upload cfg, then enable/restart.
	c.logf("=== haproxy ===")
	haCfg, err := renderHAProxyCfg(spec)
	if err != nil {
		return nil, err
	}
	if err := c.Upload(ctx, strings.NewReader(haCfg), remoteHAProxyCfgPath, 0o644); err != nil {
		return nil, fmt.Errorf("provision host haproxy cfg: %w", err)
	}
	haReload, err := renderHAProxyReload()
	if err != nil {
		return nil, err
	}
	if err := c.Run(ctx, haReload); err != nil {
		return nil, fmt.Errorf("provision host haproxy: %w", err)
	}

	// sushy-tools + health check.
	c.logf("=== sushy-tools ===")
	sushy, err := renderSushy(spec)
	if err != nil {
		return nil, err
	}
	if err := c.Run(ctx, sushy); err != nil {
		return nil, fmt.Errorf("provision host sushy: %w", err)
	}
	if err := c.waitSushy(ctx, spec); err != nil {
		return nil, err
	}

	// define domains (not started).
	c.logf("=== define domains ===")
	for _, vm := range spec.Nodes {
		script, err := renderDomain(spec, vm)
		if err != nil {
			return nil, err
		}
		if err := c.Run(ctx, script); err != nil {
			return nil, fmt.Errorf("provision host domain %s: %w", vm.Name, err)
		}
	}

	// record UUIDs (Redfish system ids).
	uuids := make(map[string]string, len(spec.Nodes))
	for _, vm := range spec.Nodes {
		uuid, err := c.RunCapture(ctx, fmt.Sprintf("sudo virsh domuuid '%s'", vm.Name))
		if err != nil {
			return nil, fmt.Errorf("provision host domuuid %s: %w", vm.Name, err)
		}
		uuids[vm.Name] = uuid
	}

	return &Result{UUIDs: uuids, Nodes: spec.Nodes}, nil
}

// waitSushy polls the emulator's Redfish endpoint; on final failure it pulls the
// container logs into the deployment log before failing, mirroring rhwa-lab.
func (c *Client) waitSushy(ctx context.Context, spec *HostSpec) error {
	health := fmt.Sprintf("curl -sk -u '%s:%s' https://%s:%d/redfish/v1/Systems >/dev/null",
		spec.SushyUser, spec.SushyPass, spec.NetGateway, spec.SushyPort)
	err := retry(12, 5*time.Second, func() error {
		_, e := c.RunCapture(ctx, health)
		return e
	})
	if err != nil {
		logs, _ := c.RunCapture(ctx, "sudo podman logs --tail 30 sushy 2>&1 || true")
		c.logf("sushy-tools did not answer; last container logs:\n%s", logs)
		return fmt.Errorf("provision host sushy: not serving Redfish on %s:%d: %w", spec.NetGateway, spec.SushyPort, err)
	}
	return nil
}

func (c *Client) logf(format string, args ...any) {
	io.WriteString(c.out, fmt.Sprintf(format, args...)+"\n")
}

func retry(n int, delay time.Duration, fn func() error) error {
	var err error
	for i := 0; i < n; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i < n-1 {
			time.Sleep(delay)
		}
	}
	return err
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl && go test ./internal/baremetal/host/ -run 'TestProvision|TestRetry' -v`
Expected: PASS (`TestProvision` skips only if `/dev/kvm` is absent on the build host; `TestRetry` always runs).

- [ ] **Step 6: Run the full package suite + vet**

Run: `cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl && go test ./internal/baremetal/host/ -v && go vet ./internal/baremetal/host/`
Expected: PASS, no vet complaints.

- [ ] **Step 7: Commit**

```bash
cd /home/jmontleo/Documents/go/src/github.com/tsanders-rh/ocpctl
git add internal/baremetal/host/provision.go internal/baremetal/host/provision_test.go internal/baremetal/host/client_test.go
git commit -m "baremetal/host: provision orchestration (packages/net/haproxy/sushy/domains)"
```

---

## Self-Review

**1. Spec coverage:**

- Section 1 (Package & host-client interface: `NewClient`, `Run`, `RunCapture`, `Upload`, `WaitReachable`, context-cancel via goroutine + conn close, `Upload` via `sudo tee`, no `Download`/jump-host) → **Task 2**. Connection lifecycle (`Connect`/`Close`, one client reused, fresh session per call) → **Task 2** (`Connect`/`Close`/`wait`).
- Section 2 (remote payloads as embedded templates; `HostSpec`/`VM`/`Result`; `Provision` step order; idempotency guards preserved; UUIDs via `RunCapture`; sushy health retry + logs-on-failure; MAC/IP computation; `vms_boot`/`vms_teardown` excluded) → **Tasks 1, 3, 4**. Templates enumerated in the spec's tree all created in **Task 3** (packages, storage-pool, libvirt-net xml+sh, haproxy cfg+sh, sushy, domain).
- Section 3 (host-key `InsecureIgnoreHostKey` documented at call site; output streaming to `out`; combined stdout/stderr for `Run`, split for `RunCapture`; per-step header line via `logf`; one conn per Provision, no mid-run reconnect; `retry` only for sushy; errors wrap with step name; non-zero remote exit is error under `set -euo pipefail`) → **Tasks 2 + 4**.
- Section 4 (in-process SSH server tests exercising Run streaming, RunCapture split, Upload via tee, context-cancel, WaitReachable polling; template rendering tests incl. reservations/VIPs/sushy/per-VM cdrom; MAC/IP computation test; no live-host test) → **Tasks 1, 2, 3, 4**.
- Section 5 (consumes addr/user/signer from piece 1; exposes `Provision(ctx, client, spec)`; MAC/IP exported for reuse; not-in-scope items excluded) → interfaces in **Tasks 1–4**; exclusions honored (no boot/teardown/download/jump/SFTP).

No gaps.

**2. Placeholder scan:** No TBD/TODO/"handle edge cases"/"similar to Task N". Every code step has literal content. The only prose caveats are actionable notes (drop the defensive `net` import if unused; `/dev/kvm` skip guard), not deferrals.

**3. Type consistency:** `VM` fields (`Name`, `Host`, `Role`, `IP`, `MAC`, `VCPU`, `RAMGB`, `Spare`) are identical across Tasks 1, 3, 4. `HostSpec`/`Result`/`Topology` defined once in Task 1 and used unchanged. Render function names in Task 3's interface block match their definitions and their call sites in Task 4 (`renderPackages`, `renderStoragePool`, `renderLibvirtNetXML`, `renderLibvirtNetDefine`, `renderHAProxyCfg`, `renderHAProxyReload`, `renderSushy`, `renderDomain`). Constants `remoteNetXMLPath`/`remoteHAProxyCfgPath` defined in Task 3, used in Tasks 3 (test) and 4. `Client` methods (`Connect`, `Close`, `Run`, `RunCapture`, `Upload`, `WaitReachable`, `wait`, `logf`, `waitSushy`) consistent between definition (Tasks 2/4) and use (Task 4). Test helpers `testKeyPair`/`newTestSSHServer`/`newTestSSHServerWithPath`/`startTestServer`/`testSpec` are defined once and reused across `_test.go` files in the same package.

One note surfaced by review and already reflected above: `testSpec()` is defined in `render_test.go` (Task 3) and reused by `provision_test.go` (Task 4) — both compile into the same test binary, so no redefinition. Task 4 must not redefine it.
