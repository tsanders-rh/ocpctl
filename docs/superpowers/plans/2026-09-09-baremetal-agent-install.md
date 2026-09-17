# Bare-metal Agent-Based Install Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A self-contained, unit-tested Go package `internal/baremetal/agent` that drives an agent-based OpenShift install on the provisioned host: render the agent-config, build the agent ISO once, boot the master VMs, run `openshift-install agent wait-for` to completion, and return a verified admin kubeconfig + cluster URLs. Ends at a running control plane (`compute.replicas: 0`) with operators stable. Workers/BMH/RHWA/dispatch are piece 4.

**Architecture:** `Install(ctx, *host.Client, InstallSpec) (*Result, error)` runs rhwa-lab's `os_host_tools → os_render_configs → os_build_image → vms_boot → os_wait_install → os_wait_cluster_ready` over the piece-2 host client. `openshift-install`/`oc` for the cluster run **on the host** (only there are the node IPs routable). All host access goes through a narrow `hostRunner` interface (satisfied by `*host.Client` and a fake), so orchestration is unit-testable with no live host. Node access (for the self-healing kubeconfig recovery) tunnels through the host's `*ssh.Client` with the piece-1 ephemeral `ssh.Signer`; the install-config `sshKey` is that ephemeral key so every node trusts it.

**Tech Stack:** Go 1.25; `golang.org/x/crypto/ssh`; stdlib `embed`, `text/template`, `net`; `testify`; reuses `internal/baremetal/host` (`Client`, `VM`, `ComputeNodes`) and the existing `internal/profile` install-config renderer (bytes passed in by the caller).

**Spec:** `docs/superpowers/specs/2026-09-09-baremetal-agent-install-design.md`

## Global Constraints

- **ocpctl as orchestrator, rhwa-lab as reference only** — no runtime dependency on the rhwa-lab repo/binary.
- **Runs over the piece-2 host client**, not locally. `Run` pipes `sudo bash -s`; `RunCapture` returns trimmed stdout; `Upload` writes via `sudo tee`. Output streams to the client's `out` writer (the deployment log the worker's `LogStreamer` tails).
- **Reuse, don't re-render, install-config** — the caller passes `InstallConfig []byte` from `Renderer.RenderInstallConfig`. Piece 3 renders only `agent-config.yaml`.
- **Topology from `host.ComputeNodes`** — masters filtered from `host.Result.Nodes`; no re-derivation of MAC/IP.
- **Testability:** narrow `hostRunner`/`nodeConnector` interfaces = exactly the methods used, satisfied by the real client and hand-written fakes in `_test.go`. Inject the retry sleep. **No live host / live cluster in unit tests.** `testify` (`require`/`assert`).
- **Build ISO exactly once** (cert-generation correctness): `buildImage` reuses if marker + ISO + `work/auth/kubeconfig` all exist on the host.
- **Shell safety:** interpolated values into remote scripts go through a local `shQuote` (single-quote escaping), matching piece 2's hardening. Cluster name/version are also schema-validated upstream.
- **Comment style:** follow surrounding ocpctl/piece-2 code — terse doc comments on exported identifiers, `# ITERATE`-style caveats only where rhwa-lab has them.
- **Scope:** the install engine only. NO BMH/worker provisioning, NO RHWA, NO dispatch wiring, NO teardown — piece 4.

---

### Task 1: Package types + agent-config rendering

**Files:**
- Create: `internal/baremetal/agent/types.go`
- Create: `internal/baremetal/agent/render.go`
- Create: `internal/baremetal/agent/templates/agent-config.yaml.tmpl`
- Test: `internal/baremetal/agent/render_test.go`

**Interfaces:**
- Consumes: `internal/baremetal/host` (`VM`).
- Produces: `InstallSpec`, `Result`, `hostRunner`; `renderAgentConfig(spec InstallSpec) (string, error)`; `masters(spec InstallSpec) []host.VM`; package constants.

- [ ] **Step 1: Write the template**

Create `internal/baremetal/agent/templates/agent-config.yaml.tmpl` (ports `os_render_configs`, masters only):

```
apiVersion: v1alpha1
kind: AgentConfig
metadata:
  name: {{ .ClusterName }}
rendezvousIP: {{ .RendezvousIP }}
hosts:
{{- range .Masters }}
- hostname: {{ .Host }}
  role: {{ .Role }}
  interfaces:
  - name: {{ $.Iface }}
    macAddress: {{ .MAC }}
  networkConfig:
    interfaces:
    - name: {{ $.Iface }}
      type: ethernet
      state: up
      mac-address: {{ .MAC }}
      ipv4:
        enabled: true
        dhcp: false
        address:
        - ip: {{ .IP }}
          prefix-length: 24
    dns-resolver:
      config:
        server:
        - {{ $.NetGateway }}
    routes:
      config:
      - destination: 0.0.0.0/0
        next-hop-address: {{ $.NetGateway }}
        next-hop-interface: {{ $.Iface }}
{{- end }}
```

- [ ] **Step 2: Write the failing test**

Create `internal/baremetal/agent/render_test.go`:

```go
package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

func testInstallSpec() InstallSpec {
	return InstallSpec{
		ClusterName:  "rhwa-lab",
		BaseDomain:   "example.com",
		OCPVersion:   "stable-4.22",
		NetGateway:   "192.168.126.1",
		RendezvousIP: "192.168.126.11",
		Masters: []host.VM{
			{Name: "rhwa-lab-master-0", Host: "master-0", Role: "master", IP: "192.168.126.11", MAC: "52:54:00:6a:01:00"},
			{Name: "rhwa-lab-master-1", Host: "master-1", Role: "master", IP: "192.168.126.12", MAC: "52:54:00:6a:01:01"},
			{Name: "rhwa-lab-master-2", Host: "master-2", Role: "master", IP: "192.168.126.13", MAC: "52:54:00:6a:01:02"},
		},
	}
}

func TestRenderAgentConfig(t *testing.T) {
	out, err := renderAgentConfig(testInstallSpec())
	require.NoError(t, err)

	assert.Contains(t, out, "kind: AgentConfig")
	assert.Contains(t, out, "rendezvousIP: 192.168.126.11")
	// One host block per master, with MAC -> IP -> hostname wiring.
	assert.Equal(t, 3, strings.Count(out, "- hostname: master-"))
	assert.Contains(t, out, "hostname: master-0")
	assert.Contains(t, out, "macAddress: 52:54:00:6a:01:00")
	assert.Contains(t, out, "ip: 192.168.126.11")
	assert.Contains(t, out, "next-hop-address: 192.168.126.1")
	assert.Contains(t, out, "name: enp1s0")
	// nmstate matches by mac-address (hedge for NIC-name drift).
	assert.Contains(t, out, "mac-address: 52:54:00:6a:01:02")
}

func TestMastersFiltersWorkersAndSpares(t *testing.T) {
	spec := testInstallSpec()
	spec.Masters = append(spec.Masters,
		host.VM{Name: "rhwa-lab-worker-0", Host: "worker-0", Role: "worker", IP: "192.168.126.21", MAC: "52:54:00:6a:02:00"},
		host.VM{Name: "rhwa-lab-worker-1", Host: "worker-1", Role: "worker", IP: "192.168.126.22", MAC: "52:54:00:6a:02:01", Spare: true},
	)
	out, err := renderAgentConfig(spec)
	require.NoError(t, err)
	// Workers/spares never appear in agent-config (they join via MachineSet).
	assert.NotContains(t, out, "worker-0")
	assert.NotContains(t, out, "worker-1")
	assert.Equal(t, 3, strings.Count(out, "- hostname: master-"))
}
```

Note: `InstallSpec.Masters` is the caller-supplied node list; `masters()` filters to `Role=="master" && !Spare`. The test seeds workers into `Masters` to prove the filter (the caller may pass the full node list).

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/baremetal/agent/ -run 'TestRenderAgentConfig|TestMasters' -v`
Expected: FAIL — package/identifiers do not exist.

- [ ] **Step 4: Write types.go**

Create `internal/baremetal/agent/types.go`:

```go
package agent

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

const (
	defaultIface          = "enp1s0" // ITERATE: q35+virtio; nmstate matches by MAC as a hedge
	defaultInstallTimeout = 2 * time.Hour
	mirrorBase            = "https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp"
	remoteInstallDir      = "~/oc-install"
	// recoveryKC is the always-trusted kubeconfig baked into every master; unlike
	// work/auth/kubeconfig it survives cert-generation mismatches.
	recoveryKC = "/etc/kubernetes/static-pod-resources/kube-apiserver-certs/secrets/node-kubeconfigs/localhost-recovery.kubeconfig"
)

// InstallSpec is built by the caller (piece 4) from the profile, the piece-1
// Substrate, and the piece-2 host Provision result.
type InstallSpec struct {
	ClusterName    string
	BaseDomain     string
	OCPVersion     string    // mirror path segment: "stable-4.22" or "4.22.3"
	NetGateway     string    // 192.168.126.1
	RendezvousIP   string    // = first master IP
	Masters        []host.VM // may be the full node list; masters() filters it
	InstallConfig  []byte    // rendered install-config.yaml (existing renderer)
	InstallTimeout time.Duration
}

// Result is what piece 4 persists.
type Result struct {
	Kubeconfig        []byte
	KubeadminPassword []byte
	APIURL            string
	ConsoleURL        string
	Recovered         bool
}

// hostRunner is the subset of *host.Client the install orchestration uses.
type hostRunner interface {
	Run(ctx context.Context, script string) error
	RunCapture(ctx context.Context, cmd string) (string, error)
	Upload(ctx context.Context, content io.Reader, remotePath string, mode os.FileMode) error
}
```

- [ ] **Step 5: Write render.go**

Create `internal/baremetal/agent/render.go`:

```go
package agent

import (
	"bytes"
	"embed"
	"text/template"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

//go:embed templates/agent-config.yaml.tmpl
var templatesFS embed.FS

var agentConfigTmpl = template.Must(template.ParseFS(templatesFS, "templates/agent-config.yaml.tmpl"))

// masters returns the control-plane VMs (workers/spares excluded — they join via
// the MachineSet, not ABI rendezvous).
func masters(spec InstallSpec) []host.VM {
	out := make([]host.VM, 0, len(spec.Masters))
	for _, vm := range spec.Masters {
		if vm.Role == "master" && !vm.Spare {
			out = append(out, vm)
		}
	}
	return out
}

// renderAgentConfig renders agent-config.yaml with per-master static nmstate.
func renderAgentConfig(spec InstallSpec) (string, error) {
	data := struct {
		ClusterName  string
		RendezvousIP string
		NetGateway   string
		Iface        string
		Masters      []host.VM
	}{spec.ClusterName, spec.RendezvousIP, spec.NetGateway, defaultIface, masters(spec)}
	var buf bytes.Buffer
	if err := agentConfigTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/baremetal/agent/ -run 'TestRenderAgentConfig|TestMasters' -v` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 7: Commit**

```bash
git add internal/baremetal/agent/types.go internal/baremetal/agent/render.go internal/baremetal/agent/templates/agent-config.yaml.tmpl internal/baremetal/agent/render_test.go
git commit -m "agent: package types and agent-config rendering"
```

---

### Task 2: Install orchestration + creds (happy path)

**Files:**
- Create: `internal/baremetal/agent/install.go`
- Create: `internal/baremetal/agent/creds.go`
- Test: `internal/baremetal/agent/install_test.go`

**Interfaces:**
- Consumes: `hostRunner`, `renderAgentConfig`, `masters`, package constants.
- Produces: `Install(ctx, *host.Client, InstallSpec) (*Result, error)`; internal `install(ctx, hostRunner, nodeConnector, sleep func(time.Duration), spec InstallSpec) (*Result, error)`; `fetchCreds(...)`; `apiURL/consoleURL`; `shQuote`. `nodeConnector` (defined in Task 3) is nil on the happy path.

**Design notes for the implementer:**
- `Install` delegates: `return install(ctx, c, newNodeConnector(c), time.Sleep, spec)`. In Task 2, `newNodeConnector` may be a stub returning nil; Task 3 fills it in. `install` treats a nil `nodeConnector` as "no recovery available."
- Steps, each writing a header to the client's `out` via a `Run`/`RunCapture`, wrapping errors `fmt.Errorf("agent <step>: %w", err)`:
  1. **hostTools** — `Run` a `set -euo pipefail` script: `mkdir -p ~/bin && cd ~/bin`, curl `${mirrorBase}/<version>/openshift-install-linux.tar.gz | tar xz openshift-install`, same for `openshift-client-linux.tar.gz` (`oc kubectl`), `./openshift-install version`. `<version>` = `shQuote(spec.OCPVersion)`.
  2. **uploadConfigs** — `Run 'mkdir -p ~/oc-install/orig'`; `Upload` `spec.InstallConfig` → `~/oc-install/orig/install-config.yaml` (0600); `Upload` `renderAgentConfig(spec)` → `~/oc-install/orig/agent-config.yaml` (0600).
  3. **buildImage** — idempotency check via `RunCapture`: `test -f ~/oc-install/work/auth/kubeconfig && test -f /var/lib/libvirt/images/<name>-agent.iso && echo REUSE || echo BUILD` (name `shQuote`d). If `REUSE`, log and skip. Else `Run`: `cd ~/oc-install && rm -rf work && mkdir -p work && cp -f orig/install-config.yaml orig/agent-config.yaml work/ && ~/bin/openshift-install --dir work agent create image --log-level=info && sudo cp work/agent.x86_64.iso /var/lib/libvirt/images/<name>-agent.iso && sudo chmod 644 /var/lib/libvirt/images/<name>-agent.iso`.
  4. **bootMasters** — for each `masters(spec)`: `st, _ := RunCapture("sudo virsh domstate " + shQuote(vm.Name))`; if `strings.TrimSpace(st) != "running"` then `Run("sudo virsh start " + shQuote(vm.Name))`. Idempotent; a start failure is an error.
  5. **waitInstall** — deadline = now + `InstallTimeout` (default). Fast path: if `nodeConnector != nil` and `clusterAvailableViaRecovery` reports Available (Task 3), skip waiting and go to creds. Otherwise re-attach loop for `bootstrap-complete` then `install-complete`: `Run("cd ~/oc-install && ~/bin/openshift-install --dir work agent wait-for bootstrap-complete --log-level=info")`; on nonzero, if past deadline → error; else (for install-complete) cross-check `clusterAvailableViaRecovery` and break if Available; `sleep(5s)`; retry.
  6. **fetchCreds** (creds.go) → `(kubeconfig, kubeadmin []byte, recovered bool, err error)`.
  7. Assemble `Result`: `APIURL = "https://api."+name+"."+base+":6443"`, `ConsoleURL = "https://console-openshift-console.apps."+name+"."+base`.
- `creds.go` happy path: `verifyOnHost` runs `Run("export KUBECONFIG=~/oc-install/work/auth/kubeconfig; ~/bin/oc get clusterversion >/dev/null 2>&1")`; on success `RunCapture("cat ~/oc-install/work/auth/kubeconfig")` and `RunCapture("cat ~/oc-install/work/auth/kubeadmin-password")` → bytes, `recovered=false`. On failure: if `nodeConnector == nil` return an error (`kubeconfig failed to authenticate and no recovery path available`); else call the recovery path (Task 3). (RunCapture trims trailing whitespace; re-append a newline to the kubeconfig bytes so the file is well-formed.)
- `shQuote(s string) string` — wrap in single quotes, replacing `'` with `'\''`. Small local helper (host's is unexported).

- [ ] **Step 1: Write the failing test**

Create `internal/baremetal/agent/install_test.go` with a `fakeHost` implementing `hostRunner` (records scripts + uploads; `RunCapture`/`Run` behaviour driven by per-test func fields keyed on a substring match of the command), and a happy-path test:

```go
package agent

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHost struct {
	runScripts []string
	captures   []string
	uploads    map[string][]byte
	// capture returns canned stdout for a command; matched by substring.
	capture func(cmd string) (string, error)
	// run returns an error for a command; matched by substring.
	run func(script string) error
}

func newFakeHost() *fakeHost { return &fakeHost{uploads: map[string][]byte{}} }

func (f *fakeHost) Run(_ context.Context, script string) error {
	f.runScripts = append(f.runScripts, script)
	if f.run != nil {
		return f.run(script)
	}
	return nil
}
func (f *fakeHost) RunCapture(_ context.Context, cmd string) (string, error) {
	f.captures = append(f.captures, cmd)
	if f.capture != nil {
		return f.capture(cmd)
	}
	return "", nil
}
func (f *fakeHost) Upload(_ context.Context, content io.Reader, remotePath string, _ os.FileMode) error {
	b, _ := io.ReadAll(content)
	f.uploads[remotePath] = b
	return nil
}

func (f *fakeHost) ran(sub string) bool {
	for _, s := range f.runScripts {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestInstall_HappyPath_BuildsAndFetches(t *testing.T) {
	f := newFakeHost()
	f.capture = func(cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "echo REUSE || echo BUILD"):
			return "BUILD", nil // ISO not present -> build
		case strings.Contains(cmd, "domstate"):
			return "shut off", nil // not running -> start
		case strings.Contains(cmd, "auth/kubeconfig"):
			return "apiVersion: v1\nkind: Config", nil
		case strings.Contains(cmd, "kubeadmin-password"):
			return "hunter2", nil
		}
		return "", nil
	}
	spec := testInstallSpec()
	spec.InstallConfig = []byte("apiVersion: v1\n# install-config")

	res, err := install(context.Background(), f, nil, func(time.Duration) {}, spec)
	require.NoError(t, err)

	// Configs uploaded.
	assert.Contains(t, string(f.uploads["~/oc-install/orig/install-config.yaml"]), "install-config")
	assert.Contains(t, string(f.uploads["~/oc-install/orig/agent-config.yaml"]), "kind: AgentConfig")
	// Tools fetched, ISO built, masters started, wait-for run.
	assert.True(t, f.ran("openshift-install-linux.tar.gz"))
	assert.True(t, f.ran("agent create image"))
	assert.True(t, f.ran("virsh start"))
	assert.True(t, f.ran("wait-for bootstrap-complete"))
	assert.True(t, f.ran("wait-for install-complete"))
	// Creds returned, URLs assembled.
	assert.False(t, res.Recovered)
	assert.Contains(t, string(res.Kubeconfig), "kind: Config")
	assert.Equal(t, []byte("hunter2"), bytes.TrimSpace(res.KubeadminPassword))
	assert.Equal(t, "https://api.rhwa-lab.example.com:6443", res.APIURL)
	assert.Equal(t, "https://console-openshift-console.apps.rhwa-lab.example.com", res.ConsoleURL)
}

func TestInstall_ReusesExistingISO(t *testing.T) {
	f := newFakeHost()
	f.capture = func(cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "echo REUSE || echo BUILD"):
			return "REUSE", nil
		case strings.Contains(cmd, "domstate"):
			return "running", nil // already running -> no start
		case strings.Contains(cmd, "auth/kubeconfig"):
			return "kind: Config", nil
		}
		return "", nil
	}
	spec := testInstallSpec()
	spec.InstallConfig = []byte("x")
	_, err := install(context.Background(), f, nil, func(time.Duration) {}, spec)
	require.NoError(t, err)
	assert.False(t, f.ran("agent create image"), "must not rebuild the ISO")
	assert.False(t, f.ran("virsh start"), "must not start already-running masters")
}

func TestInstall_KubeconfigUnverified_NoRecovery_Errors(t *testing.T) {
	f := newFakeHost()
	f.capture = func(cmd string) (string, error) {
		if strings.Contains(cmd, "echo REUSE || echo BUILD") {
			return "REUSE", nil
		}
		if strings.Contains(cmd, "domstate") {
			return "running", nil
		}
		return "", nil
	}
	f.run = func(s string) error {
		if strings.Contains(s, "oc get clusterversion") {
			return assertErr("kubeconfig invalid") // verify fails
		}
		return nil
	}
	spec := testInstallSpec()
	spec.InstallConfig = []byte("x")
	_, err := install(context.Background(), f, nil, func(time.Duration) {}, spec)
	require.Error(t, err) // no nodeConnector -> cannot recover
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
```

The implementer refines the `run`/`capture` substring keys to match the exact scripts written, and adds a `TestInstall_WaitReattach` (first `wait-for install-complete` returns an error, second succeeds) if convenient.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/baremetal/agent/ -run TestInstall -v`
Expected: FAIL — `install`, `fetchCreds`, etc. undefined.

- [ ] **Step 3: Write install.go and creds.go**

Implement per the Design notes. Keep step funcs small (`hostTools`, `uploadConfigs`, `buildImage`, `bootMasters`, `waitInstall`, `fetchCreds`). `install` calls them in order and assembles `Result`. `Install` builds the real deps and delegates.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/baremetal/agent/ -run TestInstall -v` and `go build ./...`
Expected: PASS; build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/baremetal/agent/install.go internal/baremetal/agent/creds.go internal/baremetal/agent/install_test.go
git commit -m "agent: install orchestration (tools, ISO build-once, boot, wait, creds)"
```

---

### Task 3: Tunneled node client + kubeconfig recovery

**Files:**
- Modify: `internal/baremetal/host/client.go` (add `DialNode`)
- Test: `internal/baremetal/host/client_test.go` (cover `DialNode` via the existing in-process server)
- Create: `internal/baremetal/agent/node.go`
- Modify: `internal/baremetal/agent/creds.go` + `install.go` (wire recovery + status)
- Test: `internal/baremetal/agent/node_test.go`

**Interfaces:**
- Produces (host): `func (c *Client) DialNode(ctx, nodeIP string) (*ssh.Client, error)` — tunnels a new ssh client through the reused host connection, authenticating with the host signer.
- Produces (agent): `type nodeConnector interface { runOnNode(ctx, nodeIP, cmd string) (string, error) }`; `newNodeConnector(c *host.Client) nodeConnector`; `clusterAvailableViaRecovery(ctx, nc nodeConnector, nodeIP string) bool`; `recoverKubeconfig(ctx, nc nodeConnector, nodeIP, apiFQDN string) ([]byte, error)`; pure `rewriteRecoveryKubeconfig(raw, apiFQDN string) string`.

**Design notes:**
- `DialNode`: `conn, err := c.conn.Dial("tcp", net.JoinHostPort(nodeIP, "22"))`; `ncc, chans, reqs, err := ssh.NewClientConn(conn, nodeIP+":22", &ssh.ClientConfig{User: "core", Auth: []ssh.AuthMethod{ssh.PublicKeys(c.signer)}, HostKeyCallback: ssh.InsecureIgnoreHostKey(), Timeout: 15*time.Second})`; `return ssh.NewClient(ncc, chans, reqs), nil`. `core` is the RHCOS node user. Guard `c.conn == nil` with a clear error (Connect must precede).
- `nodeConnector.runOnNode`: `client := c.DialNode(...); defer client.Close(); sess, _ := client.NewSession(); return trimmed combined output`. Real impl wraps `*host.Client`; tests use a fake returning canned output.
- `clusterAvailableViaRecovery`: `runOnNode(nodeIP, "sudo /usr/bin/oc --kubeconfig=<recoveryKC> get clusterversion version -o jsonpath='{.status.conditions[?(@.type==\"Available\")].status}'")`; return `strings.TrimSpace(out) == "True"` (any error → false).
- `recoverKubeconfig`: `raw, err := runOnNode(nodeIP, "sudo cat <recoveryKC>")`; return `[]byte(rewriteRecoveryKubeconfig(raw, apiFQDN))`.
- `rewriteRecoveryKubeconfig` (pure, the tested core): replace `server: https://localhost:6443` → `server: https://<apiFQDN>:6443`; replace the `certificate-authority-data: ...` line with `insecure-skip-tls-verify: true`; drop any `tls-server-name:` line. Ports `os_fetch_creds`'s `sed`.
- Wire into `creds.go`: on verify failure with a non-nil `nodeConnector`, `recoverKubeconfig(ctx, nc, spec.RendezvousIP, apiFQDN)`; set `recovered=true`, kubeadmin password = sentinel bytes (`"<unavailable — recovered kubeconfig; use it for oc access>"`).
- Wire into `install.go`: `newNodeConnector(c)` (real) in `Install`; the waitInstall fast-path + install-complete cross-check call `clusterAvailableViaRecovery` only when `nodeConnector != nil`.

- [ ] **Step 1: Write the failing tests**

`internal/baremetal/agent/node_test.go` — pure rewrite + recovery over a fake `nodeConnector`:

```go
package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNode struct {
	out map[string]string // substring(cmd) -> stdout
}

func (f *fakeNode) runOnNode(_ context.Context, _ string, cmd string) (string, error) {
	for k, v := range f.out {
		if strings.Contains(cmd, k) {
			return v, nil
		}
	}
	return "", nil
}

func TestRewriteRecoveryKubeconfig(t *testing.T) {
	raw := "apiVersion: v1\n" +
		"clusters:\n- cluster:\n    certificate-authority-data: AAAA\n    server: https://localhost:6443\n" +
		"    tls-server-name: api-int.local\n"
	out := rewriteRecoveryKubeconfig(raw, "api.rhwa-lab.example.com")
	assert.Contains(t, out, "server: https://api.rhwa-lab.example.com:6443")
	assert.Contains(t, out, "insecure-skip-tls-verify: true")
	assert.NotContains(t, out, "certificate-authority-data")
	assert.NotContains(t, out, "tls-server-name")
}

func TestClusterAvailableViaRecovery(t *testing.T) {
	nc := &fakeNode{out: map[string]string{"get clusterversion": "True\n"}}
	assert.True(t, clusterAvailableViaRecovery(context.Background(), nc, "192.168.126.11"))
	nc2 := &fakeNode{out: map[string]string{"get clusterversion": "False"}}
	assert.False(t, clusterAvailableViaRecovery(context.Background(), nc2, "192.168.126.11"))
}

func TestRecoverKubeconfig(t *testing.T) {
	nc := &fakeNode{out: map[string]string{"cat ": "server: https://localhost:6443\n    certificate-authority-data: X\n"}}
	kc, err := recoverKubeconfig(context.Background(), nc, "192.168.126.11", "api.rhwa-lab.example.com")
	require.NoError(t, err)
	assert.Contains(t, string(kc), "api.rhwa-lab.example.com:6443")
}
```

Add to `internal/baremetal/host/client_test.go` a `TestDialNode` that reuses the in-process server: because the test server does not implement TCP forwarding, assert the reachable path you can — e.g. that `DialNode` errors cleanly when `conn` is nil (Connect not called), and that `DialNode` attempts a `direct-tcpip` channel (extend `serveTestConn` to accept and echo a forwarded channel if practical). If forwarding is impractical in-process, keep the host-side assertion to the nil-conn guard and rely on the agent-level fakes + the piece-4 E2E for the live tunnel (documented, mirroring piece 2's deferral of live E2E).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/baremetal/agent/ -run 'TestRewrite|TestClusterAvailable|TestRecover' -v` and `go test ./internal/baremetal/host/ -run TestDialNode -v`
Expected: FAIL — identifiers undefined.

- [ ] **Step 3: Write host DialNode, agent node.go, and wire recovery**

Implement `DialNode` in `client.go`, `node.go` (real `nodeConnector` + `clusterAvailableViaRecovery` + `recoverKubeconfig` + `rewriteRecoveryKubeconfig`), and wire recovery into `creds.go`/`install.go` per Design notes. Update `Install` to pass `newNodeConnector(c)`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/baremetal/agent/ -count=1 -v` and `go test ./internal/baremetal/host/ -count=1`
Expected: PASS.

- [ ] **Step 5: Full package gate**

Run: `go test ./internal/baremetal/... -count=1`, `go vet ./internal/baremetal/...`, `go build ./...`
Expected: all PASS/clean.

- [ ] **Step 6: Commit**

```bash
git add internal/baremetal/host/client.go internal/baremetal/host/client_test.go internal/baremetal/agent/node.go internal/baremetal/agent/creds.go internal/baremetal/agent/install.go internal/baremetal/agent/node_test.go
git commit -m "agent: tunneled node client and self-healing kubeconfig recovery"
```

---

## Self-Review

- **Spec coverage:** Section 1 API → Task 1 (types) + Task 2 (`Install`). Section 2 agent-config rendering → Task 1. Section 3 orchestration (tools/upload/build-once/boot/wait/creds/operators) → Task 2. Section 4 node access + recovery → Task 3. Section 6 testing → fake `hostRunner`/`nodeConnector` + table-driven render + pure rewrite in every task. Section 7 boundaries → no BMH/worker/RHWA/dispatch (piece 4).
- **Reuse honoured:** install-config passed in (not re-rendered); topology via `host.VM`/`ComputeNodes`; host access via `*host.Client`; log streaming via the client's `out`.
- **Build-once correctness:** Task 2 asserts both the REUSE skip and the BUILD path; no in-place rebuild.
- **Type consistency:** `InstallSpec`/`Result`/`hostRunner`/`nodeConnector`, `install`, `fetchCreds`, and helper names are used identically across tasks. Fakes defined once per package.
- **Out of scope confirmed:** BMH, `os_provision_workers`, `os_unschedule_masters`, RHWA, dispatch wiring, teardown, shell-out removal — all piece 4.
- **Known deferral:** the *live* tunnel through the host (`DialNode` end-to-end) is exercised by the piece-4 integration milestone, not unit tests, matching piece 2's live-E2E deferral; the pure recovery logic and the decision paths are unit-tested here.
