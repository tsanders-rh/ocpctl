# Bare-metal agent-based install subsystem — design

Date: 2026-09-09
Status: Approved (design); implementation pending
Branch: `feat/baremetal-agent-libvirt-provider`

## Context

ocpctl is growing a native bare-metal / agent-based OpenShift provider with
**ocpctl as the orchestrator**, reproducing what the `rhwa-lab` bash script does
using ocpctl's own conventions, with rhwa-lab as a *reference*, not a runtime
dependency. The rebuild decomposes into four pieces:

1. **AWS substrate lifecycle** (`internal/baremetal/substrate`, **shipped**) —
   launch/tear down the nested-virt EC2 host, keypair, SG, EIP, Route53.
2. **Remote host provisioning** (`internal/baremetal/host`, **shipped**) — SSH in
   and install libvirt + sushy-tools + haproxy; define the libvirt network and
   the VM domains (defined-but-not-started); run sushy.
3. **Agent-based install (this document)** — render the agent-config, build the
   agent ISO once, boot the master VMs, drive `openshift-install agent wait-for`
   to completion, and collect a verified kubeconfig.
4. **Integration + lifecycle** — worker create/destroy dispatch, worker/BMH
   provisioning (metal3), RHWA via the addon path, teardown, and removal of the
   `rhwa-lab` shell-out.

This spec covers **piece 3 only**: the agent-based install *engine*. It consumes
the piece-1 `*Substrate` (address, user, `ssh.Signer`) and the piece-2 host
`Client` + `Provision` result (domain topology, Redfish UUIDs), and produces a
running control plane with a verified admin kubeconfig.

### Where piece 3 sits in rhwa-lab's `create` sequence

```
aws_preflight → aws_import_keypair → aws_security_group →
aws_launch_instance → aws_dns_upsert            ← PIECE 1 (substrate.Launch) ✅
host_provision                                   ← PIECE 2 (host.Provision)  ✅
os_host_tools                                    ┐
os_render_configs                                │
os_build_image                                   │ PIECE 3 (this document)
vms_boot                                         │
os_wait_install  (+ os_fetch_creds)              │
os_wait_cluster_ready                            ┘
rhwa_setup (operators + fencing + BMH)           ┐
os_provision_workers (+ os_unschedule_masters)   │ PIECE 4
record_ssh_channel / report                      ┘
```

`vms_define` and `vms_sushy` already run inside piece 2's `Provision` (the domain
XML references the not-yet-built ISO path — libvirt accepts a cdrom whose file is
absent until boot, so defining before building is correct). Piece 3 therefore
begins at `os_host_tools` and ends at `os_wait_cluster_ready`: a running control
plane (`compute.replicas: 0`, masters schedulable) with cluster operators stable
and a working kubeconfig. **Workers, BMHs, and RHWA are piece 4.**

## Conventions this subsystem follows (and reuses)

- **Runs over the piece-2 host client.** `openshift-install` and `oc` for the
  cluster run **on the EC2 host** (only there can the installer reach the
  192.168.126.0/24 node IPs), driven through `host.Client.Run/RunCapture/Upload`
  — exactly as rhwa-lab runs them over `ssh_host`. ocpctl does *not* run the
  agent installer locally.
- **Log streaming for free.** `host.Client` streams remote stdout/stderr to its
  `out io.Writer`. The caller (piece 4) points `out` at the per-cluster
  deployment-log file that the worker's existing `LogStreamer`
  (`internal/worker/log_streamer.go`) tails into the DB, so `agent wait-for`
  progress shows up live in the UI like every other installer. No new streaming
  machinery.
- **Reuse the existing install-config renderer.** `Renderer.RenderInstallConfig`
  (`internal/profile/renderer.go:79`) already emits a correct agent/baremetal
  `install-config.yaml` (`compute.replicas: 0`, `controlPlane.replicas: N`,
  `platform.baremetal.apiVIPs/ingressVIPs`, `renderer.go:187-190,658-691`). Piece
  3 does **not** re-render install-config; the caller passes the rendered bytes
  in. Piece 3 owns only the **agent-config** (the per-node nmstate that has no
  ocpctl precedent).
- **Topology from a single source.** Master MAC/IP/hostname come from
  `host.ComputeNodes` (`internal/baremetal/host/compute.go`, already exported and
  used by piece 2), so the agent-config reservations and the libvirt net
  reservations can never drift.
- **Testability, ocpctl-style.** The install orchestration goes through a narrow
  `hostRunner` interface (the subset of `*host.Client` it uses), satisfied by the
  real client and by a hand-written fake in `_test.go`. Agent-config rendering is
  table-tested like piece 2's `render_test.go`. **No live host / live cluster in
  unit tests.**

## Decisions (settled during brainstorming)

- **Piece 3 stops at an installed control plane.** `compute.replicas: 0`; masters
  stay schedulable (the installer's default when compute is 0). Worker BMH
  creation, MachineSet scale-up, and `os_unschedule_masters` require applying
  metal3 manifests against the running cluster and share the sushy/BMC wiring with
  RHWA — they move to **piece 4** as one coherent "make it a metal3-managed,
  fenceable cluster" chunk. This matches the canonical 4-line description of
  piece 3 ("…collect kubeconfig") and keeps piece 3 free of any Kubernetes-client
  / manifest-apply machinery.
- **Build the agent ISO exactly once — a correctness requirement, not an
  optimization.** Each `openshift-install agent create image` mints a *fresh* set
  of cluster certs; `work/auth/` holds the only copy of the matching kubeconfig +
  kubeadmin password. Rebuilding after nodes have booted orphans those
  credentials permanently. `buildImage` is idempotent: if the marker + staged ISO
  + `work/auth/kubeconfig` all exist on the host, it reuses them. (ocpctl builds
  once per create and never rebuilds in place, so it avoids rhwa-lab's rebuild
  footgun by construction.)
- **Cluster `sshKey` = the ephemeral substrate public key.** The install-config's
  `sshKey` gets the piece-1 ephemeral key's public half, so **every node trusts
  the key ocpctl already holds the private half of** (piece 1's `ssh.Signer`,
  passed through by the caller). This is what unlocks node-level access — reached
  by tunneling through the host `*ssh.Client` with that same Signer (see Section
  4) — for the creds-recovery safety-net now, and for piece 4/5's power, monitor,
  and fence-testing later. The private key still never touches disk. rhwa-lab
  instead reuses the operator's key for both host and nodes; ocpctl's ephemeral
  key is the self-contained equivalent. *(The user's requested SSH key — if
  present — handling is a follow-up: `sshKey` is a single field, so surfacing the
  user key too is deferred to piece 4.)*
- **Host-key verification: `ssh.InsecureIgnoreHostKey()`**, both host and tunneled
  node connections, consistent with piece 2 and documented as the same conscious
  trade-off (self-launched instance; hardening follow-up: pin via EC2
  `GetConsoleOutput`).
- **Remote logic stays as templated bash + the installer CLI**, mirroring piece
  2: agent-config is `text/template`; ISO build / boot / wait are short scripts
  piped to the host via `host.Client`. `openshift-install agent wait-for` is the
  installer's own command, streamed straight through.
- **`agent create image` for the ISO** (not `agent create config-image` /
  external image serving). One ISO, staged into `/var/lib/libvirt/images/`,
  welded onto the master domains' cdrom (piece 2 already wrote that cdrom into the
  master XML).

## Section 1 — Package & public API

New self-contained package `internal/baremetal/agent` (sibling to `substrate` and
`host`).

```go
package agent

// InstallSpec is built by the caller (piece 4) from the profile, the piece-1
// Substrate, and the piece-2 host Provision result.
type InstallSpec struct {
    ClusterName   string
    BaseDomain    string
    OCPVersion    string     // mirror.openshift.com path segment: "stable-4.22" or "4.22.3"
    NetGateway    string     // 192.168.126.1 — nmstate gateway/DNS
    RendezvousIP  string     // = Masters[0].IP
    Masters       []host.VM  // masters only (host.Result.Nodes filtered to Role==master, !Spare)
    InstallConfig []byte     // rendered install-config.yaml (existing renderer)
    InstallTimeout time.Duration // overall budget; default 2h
}

// Result is what piece 4 persists (kubeconfig → S3, URLs → cluster_outputs).
type Result struct {
    Kubeconfig        []byte
    KubeadminPassword []byte // sentinel text if the kubeconfig was recovered
    APIURL            string // https://api.<name>.<base>:6443
    ConsoleURL        string // https://console-openshift-console.apps.<name>.<base>
    Recovered         bool   // true if the recovery-kubeconfig fallback was used
}

// Install runs os_host_tools → render+upload configs → build ISO (once) → boot
// masters → agent wait-for → fetch+verify kubeconfig → wait operators stable.
func Install(ctx context.Context, c *host.Client, spec InstallSpec) (*Result, error)
```

The host is reached only through a narrow interface so the orchestration is
mock-testable:

```go
// hostRunner is the subset of *host.Client Install uses. *host.Client satisfies
// it; a fake in install_test.go records scripts/uploads and returns canned
// RunCapture output.
type hostRunner interface {
    Run(ctx context.Context, script string) error
    RunCapture(ctx context.Context, cmd string) (string, error)
    Upload(ctx context.Context, content io.Reader, remotePath string, mode os.FileMode) error
}
```

`Install`'s exported signature takes `*host.Client` (which the caller already
holds, connected); internally it delegates to `install(ctx, hostRunner, nodeDialer, spec)`
so tests inject fakes. `nodeDialer` (Section 4) is nil-able — the happy path
never needs it.

## Section 2 — Agent-config rendering

The one genuinely new artifact. `install-config.yaml` is reused verbatim from the
existing renderer; piece 3 renders **`agent-config.yaml`** from the master
topology, an `embed.FS` + `text/template` asset like piece 2:

```
internal/baremetal/agent/
  install.go          # orchestration
  render.go           # agent-config rendering + upload helper
  creds.go            # fetch + verify (+ recovery via nodeDialer)
  node.go             # tunneled node client (Section 4)
  install_test.go     # orchestration over a fake hostRunner
  render_test.go      # table-driven agent-config assertions
  node_test.go
  templates/
    agent-config.yaml.tmpl
```

`agent-config.yaml.tmpl` reproduces `os_render_configs` (openshift.sh:100-146):
`apiVersion: v1alpha1`, `kind: AgentConfig`, `rendezvousIP`, and one `hosts[]`
entry **per master only** (workers join later via the MachineSet, not ABI
rendezvous) with `interfaces[].macAddress` and a `networkConfig` (nmstate)
block: interface `enp1s0` matched by `mac-address`, static `ipv4` (the node IP,
`/24`), `dns-resolver` → `NetGateway`, default route → `NetGateway`.

- **Interface name `enp1s0`** carried as a template constant with the same
  `# ITERATE` caveat rhwa-lab documents (q35+virtio); nmstate matches by MAC as
  the hedge, so a different kernel NIC name still binds.
- Rendering is pure and table-tested: from a known `[]host.VM` assert the
  rendezvousIP, the master count (spares/workers excluded), and each host's
  MAC→IP→hostname triple and gateway.

## Section 3 — Install orchestration

`install` runs rhwa-lab's order with ocpctl idioms; each step writes a header line
to the host client's `out` and wraps errors `fmt.Errorf("agent <step>: %w", err)`.

1. **host tools** (`os_host_tools`) — `Run` a script that curls
   `openshift-install` + `oc` for `OCPVersion` from `mirror.openshift.com` into
   `~/bin` on the host and prints the version. Idempotent (re-download is cheap).
2. **render + upload configs** — `Upload` the caller's `InstallConfig` bytes and
   the rendered `agent-config.yaml` into `~/oc-install/orig/` (mode 0600).
3. **build ISO once** (`os_build_image`) — if the marker file + staged ISO +
   `work/auth/kubeconfig` already exist (`RunCapture` a `test -f && echo ok`),
   skip. Otherwise `Run`: clean `work/`, copy the two configs in,
   `openshift-install --dir work agent create image --log-level=info`, `sudo cp`
   the ISO to `/var/lib/libvirt/images/<cluster>-agent.iso`, write the marker.
4. **boot masters** (`vms_boot`) — for each master: `RunCapture sudo virsh
   domstate`; if not already running, `Run sudo virsh start <name>`. Idempotent.
5. **wait for install** (`os_wait_install`) — `Run openshift-install --dir work
   agent wait-for bootstrap-complete --log-level=info`, then `install-complete`,
   each in a **re-attach loop**: `wait-for` has its own internal timeout and can
   return early, so re-invoke until it succeeds or the `InstallTimeout` budget is
   spent. Output streams straight to `out` (the live installer log). *(The
   recovery cross-check between attempts — Section 4 — is engaged only if a node
   dialer is supplied; without one, budget exhaustion is a hard error.)*
6. **fetch + verify creds** (`os_fetch_creds`) — verify the installer kubeconfig
   authenticates **on the host** (`Run 'KUBECONFIG=work/auth/kubeconfig oc get
   clusterversion'`); on success `RunCapture 'cat work/auth/kubeconfig'` and the
   kubeadmin password back into `Result`. On failure, recover (Section 4) or
   error.
7. **wait operators stable** (`os_wait_cluster_ready`) — poll (on the host, via
   the fetched kubeconfig) `oc wait --for=condition=Available=True
   clusterversion/version` until Available or a bounded number of attempts; a
   timeout here is a warning, not a failure (operators may still be settling),
   matching rhwa-lab.
8. Assemble `Result` (URLs built from name+baseDomain exactly as
   `extractClusterOutputs`, `handler_create.go:521-524`).

## Section 4 — Node access (tunneled) and the creds safety-net

Node IPs are reachable only from the host, and (per the sshKey decision) every
node trusts the ephemeral substrate key. Piece 3 adds the **jump-host node
client** piece 2 designed for but deferred: dial the node *through* the host's
reused `*ssh.Client` and authenticate with the same `ssh.Signer`.

```go
// node.go — reached via the host's existing connection; private key stays in-process.
type nodeDialer interface {
    // Dial opens an ssh client to nodeIP:22 tunneled through the host conn.
    Dial(ctx context.Context, nodeIP string) (*ssh.Client, error)
}
```

Implemented as `hostConn.Dial("tcp", nodeIP+":22")` → `net.Conn` →
`ssh.NewClientConn(conn, ..., cfg)` with the host client's signer and
`InsecureIgnoreHostKey`. `*host.Client` gains one small exported method
(`DialNode`) returning such a dialer, since only piece 3+ needs it.

Used for two things, both mirroring `openshift.sh`:

- **`os_cluster_status_via_recovery`** — read a master's always-trusted
  `localhost-recovery.kubeconfig` and check `ClusterVersion Available=True`. Used
  as the safety-net cross-check in step 5 (break out of a wedged `wait-for` if the
  cluster is already up) and by piece 4's `monitor`.
- **`os_fetch_creds` fallback** — if the installer kubeconfig fails verification,
  recover a working cluster-admin kubeconfig from the rendezvous master's recovery
  kubeconfig, repointed at the public API with `insecure-skip-tls-verify`, and set
  `Result.Recovered = true` with a sentinel kubeadmin password.

**Scoping note:** the tunneled node client + recovery is *included* in piece 3
because `os_fetch_creds` explicitly verifies and self-heals — handing back a
kubeconfig that silently doesn't authenticate would be a regression from
rhwa-lab. It is kept minimal (recovery + status only); `monitor`, power, and
fence-testing that also use node access are piece 4/5.

## Section 5 — Cross-cutting concerns

- **Context cancellation / timeouts:** all remote calls go through `host.Client`,
  which already ties `ctx` to session teardown. The `wait-for` re-attach loop
  honours both `ctx` and the `InstallTimeout` budget.
- **Idempotency / resumability:** build-once (marker), boot (domstate guard), and
  a fast-path in step 5 (if the cluster already reports Available via recovery,
  skip the wait and just fetch creds) make a re-run of a partially-completed
  create safe — matching rhwa-lab's `state_get` guards without a state file
  (state is derived from the host/cluster, not persisted).
- **Error handling:** every step wraps with its name. `Run` treats a non-zero
  remote exit as an error (remote scripts are `set -euo pipefail`). Budget
  exhaustion in `wait-for` is a hard error pointing at the deployment log.

## Section 6 — Testing

- **Agent-config rendering** (`render_test.go`, table-driven): from a known master
  set assert rendezvousIP, master-only host list, per-host MAC/IP/hostname and the
  nmstate gateway/DNS/route. A spare/worker in the input must not appear.
- **Install orchestration** (`install_test.go`) against a **fake `hostRunner`**
  that records every script/upload and returns canned `RunCapture` values: assert
  the ISO build-once skip path (marker+iso+kubeconfig present ⇒ no `create image`
  call) *and* the build path; boot issues `virsh start` only for non-running
  masters; the `wait-for` re-attach loop retries then succeeds; creds fetched and
  URLs assembled; a verification failure with **no** dialer returns an error.
- **Node client** (`node_test.go`) against an in-process `crypto/ssh` server (as
  piece 2 tested its client): a tunneled dial authenticates with the signer and
  runs a command; the recovery-kubeconfig rewrite (server URL + insecure-skip)
  produces a well-formed kubeconfig.
- **No live host / live AWS / live cluster** in unit tests — a real E2E needs
  pieces 1+2+3 wired together and belongs to the piece-4 integration milestone.

## Section 7 — Boundaries with the other pieces

- **Consumes (piece 1):** `ssh.Signer` (already inside the host `Client`), and the
  decision that the same key is the cluster `sshKey`.
- **Consumes (piece 2):** the connected `*host.Client`; `host.Result.Nodes` for
  the master topology; the fact that master domains already carry the agent-ISO
  cdrom and sushy is already serving.
- **Consumes (profile/renderer):** the rendered `install-config.yaml` bytes.
- **Exposes:** `Install(ctx, *host.Client, InstallSpec) (*Result, error)` — a
  verified kubeconfig, kubeadmin password, and URLs. Piece 4 persists these
  (kubeconfig→S3, URLs→`cluster_outputs`) via the existing `extractClusterOutputs`
  / `storeArtifacts` paths, then does BMH + worker provisioning + RHWA over the
  same node/host access.
- **Not in scope (piece 4):** worker/spare BMH creation
  (`rhwa_configure_bmh`/`_apply_worker_bmh`), MachineSet scale-up
  (`os_provision_workers`), `os_unschedule_masters`, RHWA operators + fencing
  (`rhwa_setup`), worker create/destroy dispatch wiring, `vms_teardown` +
  substrate teardown, and removal of the shell-out handlers. Not in scope
  (later/hardening): `monitor`, out-of-band power, `test`/fence verification, EC2
  host-key pinning, surfacing the user's own SSH key on the cluster.

## Out of scope / follow-ups

- Worker/BMH/RHWA/dispatch/teardown — **piece 4** (needs its own design + plan).
- `monitor`, power control, fence testing — later pieces (need the node client
  this piece introduces).
- EC2 host-key pinning via `GetConsoleOutput` (hardening; shared with pieces 1/2).
- User-supplied cluster `sshKey` alongside the ephemeral key.
