# Bare-metal SSH host-provisioning subsystem — design

Date: 2026-09-08
Status: Approved (design); implementation pending
Branch: `feat/baremetal-agent-libvirt-provider`

## Context

ocpctl is growing a native bare-metal / agent-based OpenShift provider. The
reference implementation is the `rhwa-lab` bash orchestrator, which stands up a
nested-virt AWS EC2 host and, inside it, an agent-based OpenShift cluster whose
nodes are libvirt VMs fronted by an emulated Redfish BMC (sushy-tools) for RHWA
fence_redfish testing.

An earlier attempt made ocpctl shell out to the `rhwa-lab` script itself,
delegating the entire orchestration and depending on that repo being present at
runtime. That was the wrong boundary. The goal is for **ocpctl to be the
orchestrator** — reproducing what rhwa-lab does, step by step, using ocpctl's
own conventions (AWS Go SDK where ocpctl uses the SDK; shelling out to CLIs like
`openshift-install` where ocpctl already does) — with rhwa-lab as a *reference*,
not a runtime dependency.

The native rebuild decomposes into four independently designable/testable pieces:

1. **AWS substrate lifecycle** — launch/tag/terminate the EC2 host, security
   group, keypair, Route53 records (AWS Go SDK).
2. **Remote host provisioning (this document)** — the one subsystem with no
   existing ocpctl pattern: SSH into the host and install libvirt + sushy-tools +
   haproxy, define the libvirt network and VMs.
3. **Agent-based install** — render configs, build the agent ISO once, boot VMs
   via sushy/virsh, `openshift-install agent wait-for`, collect kubeconfig.
4. **Integration + lifecycle** — worker create/destroy dispatch, RHWA via the
   existing addon/post-deploy path, destroy/teardown.

This spec covers **piece 2 only**, chosen first because it is the riskiest
(ocpctl has zero SSH code today).

### Why this subsystem is new ground

Every existing ocpctl provider wraps a CLI (`openshift-install`, `eksctl`,
`rosa`, `az`, `gcloud`, `ibmcloud`) via `exec.CommandContext` against a cloud
API, and uses the AWS Go SDK for cloud read/lifecycle work. **No ocpctl code
SSHes into a host or provisions an OS.** rhwa-lab, by contrast, does nearly all
its substrate work over SSH.

## Decisions (settled during brainstorming)

- **Transport: Go `crypto/ssh`** (not shelling out to `ssh`/`scp`). Consistent
  with ocpctl's "library over CLI" boundary — it uses the AWS Go SDK, not the
  `aws` CLI. No `ssh`/`scp` binary dependency on the worker; streams cleanly into
  the deployment-log pipeline; unit-testable.
- **Remote logic: templated bash snippets.** Keep rhwa-lab's proven, idempotent
  bash as ocpctl-owned assets (`embed.FS` + `text/template`), parameterized and
  piped to `sudo bash -s` over the SSH session. Lowest risk; mirrors ocpctl's
  existing `text/template` use in the install-config renderer.
- **Host-key verification: `ssh.InsecureIgnoreHostKey()` for now**, documented as
  a conscious trade-off. We connect to a self-launched instance at an
  AWS-API-provided IP seconds after launch; the MITM window requires an on-path
  attacker inside AWS. Hardening follow-up: pin the host key from EC2
  `GetConsoleOutput`. rhwa-lab disables host-key checking entirely.
- **Node MAC/IP computation ported to Go** (single source of truth; piece 3's
  agent-config reuses it — avoids the MAC/IP drift that made the shell-out
  approach brittle).
- **Domain UUIDs returned in a result struct**, not persisted to a state file.

Note: the EC2 host is *not* disposable — it hosts the cluster's VMs for the
cluster's entire lifetime. It is created fresh per cluster (ephemeral in that
sense) but long-lived.

## Section 1 — Package & host-client interface

New self-contained package `internal/baremetal/host`. The **host client** is the
single abstraction every provisioning step goes through; it is the only code that
knows crypto/ssh exists.

```go
package host

type Client struct {
    addr   string       // "<host-ip>:22"
    user   string       // cloud user, e.g. "fedora"
    signer ssh.Signer   // from the ephemeral keypair (piece 1 supplies it)
    out    io.Writer    // combined stdout/stderr sink → deployment-log file
}

func NewClient(addr, user string, signer ssh.Signer, out io.Writer) *Client

// Pipe a script to `sudo bash -s` on the host, streaming output to out.
func (c *Client) Run(ctx context.Context, script string) error

// Run a command and return trimmed stdout (small values: domuuid, domstate,
// curl health). stderr still streams to out.
func (c *Client) RunCapture(ctx context.Context, cmd string) (string, error)

// Write content to a remote path via `sudo tee`, chmod to mode.
func (c *Client) Upload(ctx context.Context, content io.Reader, remotePath string, mode os.FileMode) error

// Poll Dial until SSH answers or ctx/timeout — the host just booted.
func (c *Client) WaitReachable(ctx context.Context, timeout time.Duration) error
```

- **`Run` = pipe-to-`sudo bash -s`**, mirroring rhwa-lab's
  `ssh_host 'sudo bash -s' <<EOS`. The script is a rendered template (Section 2).
- **Context cancellation:** crypto/ssh sessions aren't context-aware, so
  `Run`/`RunCapture` launch the session in a goroutine and close the conn on
  `ctx.Done()`. Ties provisioning into the worker's cancellation/timeout.
- **`Upload` via `sudo tee`** (no extra dependency) — sufficient for the small
  text configs this subsystem writes. The large agent ISO belongs to piece 3,
  which will add SFTP (`pkg/sftp`). YAGNI here.
- **`Download` and a jump-host node client are omitted** — only later pieces need
  them (kubeconfig recovery, power/fencing). Conn handling is designed so a jump
  variant slots in cleanly.

## Section 2 — Remote payloads (the provisioning steps)

Host-side logic stays as rhwa-lab's proven, idempotent bash, now ocpctl-owned
assets loaded via `embed.FS` and rendered with `text/template`, then handed to
`client.Run` (or uploaded via `client.Upload` for config files).

```
internal/baremetal/host/
  client.go
  provision.go            # step orchestration
  compute.go              # node MAC/IP topology (ported compute_nodes/compute_spares)
  provision_test.go
  compute_test.go
  client_test.go
  templates/
    packages.sh.tmpl      # dnf install kvm/libvirt/podman/haproxy…, enable libvirtd,
                          # /dev/kvm check, ip_forward, setsebool
    storage-pool.sh.tmpl
    libvirt-net.xml.tmpl  # uploaded, not executed
    libvirt-net.sh.tmpl   # virsh net-define/autostart/start
    haproxy.cfg.tmpl      # uploaded
    haproxy.sh.tmpl       # systemctl enable/restart
    sushy.sh.tmpl         # openssl cert, htpasswd, conf, podman run
    domain.sh.tmpl        # per-VM: qemu-img + virt-install --print-xml + virsh define
```

Single input struct built by the caller from the profile + AWS substrate output:

```go
type HostSpec struct {
    ClusterName          string
    LibvirtNet           string   // "rhwa"
    NetCIDR, NetGateway  string    // 192.168.126.0/24, .1
    APIVIP, IngressVIP   string
    SushyPort            int
    SushyUser, SushyPass string
    NodeDiskGB           int
    Nodes                []VM      // masters + workers + spares
}

type VM struct {
    Name   string
    RAMGB  int
    VCPU   int
    MAC    string
    IP     string
    Role   string // master|worker  (spares carry role "worker")
    Spare  bool
}
```

Orchestration entry point runs steps in rhwa-lab's order:

```go
func Provision(ctx context.Context, c *Client, spec *HostSpec) (*Result, error)
// 1 packages → 2 storage pool → 3 libvirt net (upload XML, then define)
// → 4 haproxy (upload cfg, then restart) → 5 sushy (+ retry health check)
// → 6 define domains (per VM) → 7 record UUIDs (RunCapture domuuid)

type Result struct {
    UUIDs map[string]string // domain name → libvirt UUID (Redfish system id)
    Nodes []VM              // computed topology, for piece 3
}
```

- **Idempotency preserved verbatim** — each snippet keeps rhwa-lab's
  `virsh … >/dev/null 2>&1 || …` / `dominfo … && skip` guards, so re-running a
  failed create is safe.
- **UUIDs via `RunCapture`** (`sudo virsh domuuid <name>`), returned in `Result`.
- **sushy health check** (`retry 12 5 -- curl -sk …/redfish/v1/Systems`) becomes a
  `RunCapture` in a Go retry loop; on failure it pulls `podman logs --tail 30
  sushy` into the log before erroring — same fail-hard behavior.
- **`vms_boot`/`vms_teardown` are not here** — booting from the agent ISO is
  piece 3; teardown is piece 4. This subsystem stops at "host provisioned,
  domains defined-but-not-started, sushy serving."
- **MAC/IP computation ported to Go** (`compute.go`): masters `.11+`, workers
  `.21+`, MAC `52:54:00:6a:<role>:<idx>`; feeds both the libvirt net reservations
  and (later) the agent config.

## Section 3 — Cross-cutting concerns

- **Host-key verification:** `ssh.InsecureIgnoreHostKey()`, documented at the call
  site (see Decisions). Hardening follow-up: pin via EC2 `GetConsoleOutput`.
- **Output streaming:** the client's `out io.Writer` is the deployment-log file
  the worker's existing `LogStreamer` tails, so remote provisioning shows up live
  in the UI like other installers. `session.Stdout`/`Stderr` both point at `out`
  (combined, in order). `RunCapture` splits: stdout captured/returned, stderr
  streamed. Each step writes a human header line to `out` (rhwa-lab `log "…"`
  equivalents).
- **Connection lifecycle:** one `*ssh.Client` per `Provision`, opened after
  `WaitReachable`, reused across steps, closed at the end. Each `Run`/`RunCapture`
  opens a fresh one-shot `ssh.Session`. No mid-run reconnect — the retriable
  window is *reachability* (`WaitReachable`); a mid-run drop means an unhealthy
  host and fails the create.
- **Retries & waits:** `WaitReachable` dial loop with fixed backoff until timeout
  (the only network-flake retry). A small internal `retry(n, delay, fn)` used only
  where rhwa-lab retries (sushy health).
- **Error handling:** errors wrap with the step name
  (`fmt.Errorf("provision host packages: %w", err)`). `Run` treats any non-zero
  remote exit as an error (remote scripts are `set -euo pipefail`). The sushy step
  reproduces fail-hard-with-logs.

## Section 4 — Testing

- **Unit tests against an in-process SSH server** (`gliderlabs/ssh` or stdlib
  `crypto/ssh` server) that authenticates the test keypair and executes against a
  temp dir / fake `bash`. Exercises real client paths: `Run` streaming,
  `RunCapture` stdout/stderr split, `Upload` via `tee`, context-cancel closing the
  conn, `WaitReachable` polling. No AWS.
- **Template rendering tests** (table-driven, like existing renderer tests):
  render each template from a known `HostSpec`; assert reservations block
  (MAC→IP→name), VIPs in `haproxy.cfg`, sushy conf values, per-VM `virt-install`
  flags (master gets agent ISO cdrom; worker/spare gets empty tray).
- **MAC/IP computation test:** assert masters `.11+`, workers `.21+`, MAC scheme,
  spare offsets across a few topologies vs. rhwa-lab's output.
- **No live-host test in this slice** — a real E2E needs pieces 1 and 3; that's a
  later integration milestone.

## Section 5 — Boundaries with the other pieces

- **Consumes (piece 1, AWS substrate):** host addr, cloud user, `ssh.Signer` from
  the ephemeral private key. Does not launch instances or know about AWS.
- **Consumes (profile/renderer):** the `HostSpec`. MAC/IP/node computation lives
  here and is exported so piece 3's agent-config reuses the same values (single
  source of truth).
- **Exposes:** `Provision(ctx, client, spec) (*Result, error)` with domain UUIDs
  (Redfish system ids) and computed topology. Piece 3 builds/uploads the ISO,
  boots masters, `agent wait-for`. Piece 4 uses the same client for
  `vms_teardown` + sushy removal before host termination.
- **Not in scope:** `vms_boot`, `vms_teardown`, `Download`, jump-host node client
  (power/fencing/monitor). Designed so those drop in later.

## Out of scope / follow-ups

- EC2 host-key pinning via `GetConsoleOutput` (hardening).
- SFTP upload for the agent ISO (piece 3).
- `vms_boot`, `vms_teardown`, kubeconfig download, jump-host node client (later
  pieces).
- Removal of the earlier shell-out handlers (`installer/baremetal.go`,
  `handler_create_baremetal.go`, `handler_destroy_baremetal.go`) — happens in
  piece 4 integration, not here.
