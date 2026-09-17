# Bare-metal integration & lifecycle subsystem — design

Date: 2026-09-09
Status: Approved (design); implementation pending
Branch: `feat/baremetal-agent-libvirt-provider`

## Context

ocpctl is growing a native bare-metal / agent-based OpenShift provider with
**ocpctl as the orchestrator**, reproducing what `rhwa-lab` does using ocpctl's
conventions, with rhwa-lab as a *reference*, not a runtime dependency. Three
pieces are shipped:

1. **AWS substrate** (`internal/baremetal/substrate`) — `Launch`/`Teardown`.
2. **Host provisioning** (`internal/baremetal/host`) — `NewClient`/`Provision`,
   `ComputeNodes`, `DialNode`.
3. **Agent-based install** (`internal/baremetal/agent`) — `Install` → verified
   kubeconfig.

All three are built, unit-tested, and **currently unwired** — nothing outside
`internal/baremetal/` imports them. The bare-metal create/destroy handlers still
shell out to the `rhwa-lab` binary.

This spec covers **piece 4**: wire the three native pieces into the worker's
create/destroy dispatch, add the post-install cluster configuration (BareMetalHost
objects, RHWA operators, `fence_redfish` fencing, optional worker provisioning),
and delete the shell-out. This is the piece that makes an ocpctl-created cluster
**equivalent to what rhwa-lab produces**.

### The full rhwa-lab `create` sequence, mapped

```
aws_preflight … aws_dns_upsert          → substrate.Launch          (piece 1) ✅
host_provision (+ vms_define/vms_sushy)  → host.Provision            (piece 2) ✅
os_host_tools … os_wait_cluster_ready    → agent.Install             (piece 3) ✅
rhwa_configure_bmh                       → rhwa.ConfigureBMH         ┐
rhwa_install_operators                   → rhwa.InstallOperators     │ PIECE 4
rhwa_configure_fencing                   → rhwa.ConfigureFencing     │ (new)
os_provision_workers (+ unschedule)      → rhwa.ProvisionWorkers     ┘
```

Plus the wiring: a `lifecycle` orchestrator that runs the four steps in order and
is called by the worker create handler; `lifecycle.Destroy` → `substrate.Teardown`
for destroy; and removal of `internal/installer/baremetal.go` and the rhwa-lab env
contract.

## Decisions (settled during brainstorming)

- **RHWA is configured natively during create; operator versions come from the
  addon catalog.** The `fence_redfish` fencing (FAR template + NHC) and the BMHs
  need each node's libvirt UUID and the per-cluster sushy credentials — create-time
  data the generic post-deploy addon path can't obtain — and the fencing CRs must
  be applied *after* the operators' CRDs exist. So the whole RHWA setup runs
  *inside* create, in rhwa-lab's order (operators → fencing → BMH → workers). The
  operator **set itself** (names, channels, sources) is **not hardcoded**: it is
  read from the ocpctl `rhwa` addon definition (`store.PostConfigAddons`,
  `Config.Operators`) — the single declarative source of RHWA operator versions —
  and the create flow renders those Subscriptions natively. Consequence:
  **`defaultAddons: [rhwa]` is removed from the bare-metal profile** so the addon
  is not *also* auto-applied post-deploy (which would double-install and layer the
  addon's SNR-based remediation CRs on top of the lab's `fence_redfish` NHC); the
  `rhwa.yaml` addon remains the catalog source and stays available for other/manual
  use.
- **Post-install config runs `oc` on the host**, through the piece-2 host client,
  using the host's `~/oc-install/work/auth/kubeconfig` — uniform with piece 3, and
  the host is where `oc`/`openshift-install` already live. No Kubernetes client, no
  local kubeconfig plumbing on the worker. Manifests are applied by piping a
  rendered YAML to `oc apply -f -` via a quoted heredoc.
- **Per-cluster sushy credentials are generated in-process** (random user +
  password), like the ephemeral keypair. They flow to exactly three consumers —
  the host `HostSpec` (piece 2 writes the htpasswd), the BMH BMC secrets, and the
  FAR template — from a single source, so they cannot drift. Not persisted (the
  host is terminated on destroy).
- **Cluster `sshKey` = the ephemeral substrate public key** (piece-3 decision):
  `lifecycle` derives it from `Substrate.Signer.PublicKey()` and passes it as the
  install-config `sshKey`, so every node trusts the key ocpctl holds — enabling
  the piece-3 tunneled node access. The user's own key is a follow-up.
- **Topology from the profile:** control-plane count = `compute.controlPlane.replicas`;
  worker count = `compute.workers.replicas` (the install-config's compute replicas
  are **expected 0** — masters install alone, workers/spares join via metal3);
  spare count = `baremetal.spareWorkerCount`. Node vCPU/RAM are parsed from the
  `vm-<N>vcpu-<M>gb` instance-type names (fallback: CP 8/20, worker 4/16). With the
  shipped profile (workers.replicas 0, spares 3) the cluster is masters-only with
  three available spare BMHs — matching rhwa-lab's spare model; `ProvisionWorkers`
  is a no-op when the worker count is 0.
- **Destroy = `substrate.Teardown`.** Terminating the host takes the VMs,
  sushy, and haproxy with it; Teardown also removes the SG, EIP, keypair, and
  Route53 records by tag. No `oc destroy`, no host cleanup pass.
- **Testability, ocpctl-style.** The new `rhwa` package renders manifests
  (table-tested) and orchestrates over a narrow `runner` interface (fake-tested),
  like pieces 2/3. The `lifecycle` package's **pure spec builders** (profile →
  LaunchSpec / Topology / HostSpec / InstallSpec, size parsing, cred generation)
  are unit-tested; the thin `Create`/`Destroy` glue that calls
  `Launch`/`Provision`/`Install` is exercised by the piece-4 **live E2E**
  milestone (the first real end-to-end run), matching how pieces 1-3 deferred live
  tests.

## Section 1 — New packages `internal/baremetal/metal3` and `internal/baremetal/rhwa`

> **Implemented as two packages** (split during review for reuse): the generic,
> workload-agnostic **`metal3`** package (BareMetalHost wiring + MachineSet worker
> provisioning — reusable for any emulated/physical metal3 cluster), and the
> RHWA-specific **`rhwa`** package (Medik8s operators + `fence_redfish` fencing).
> The design below describes them together; `ConfigureBMH`/`ProvisionWorkers`
> live in `metal3`, `InstallOperators`/`ConfigureFencing` in `rhwa`.

Post-install cluster configuration, applied via the host client. All steps run
`agent.RemoteOC --kubeconfig=agent.RemoteKubeconfig …` on the host (an absolute
path — `Run` is sudo/root while `RunCapture` is the login user, so `~` is
ambiguous).

```go
package rhwa

// runner is the subset of *host.Client used here (Run streams to the deploy log;
// RunCapture returns trimmed stdout for scale/wait polling).
type runner interface {
    Run(ctx context.Context, script string) error
    RunCapture(ctx context.Context, cmd string) (string, error)
}

// Spec carries the substrate-derived values the manifests need.
type Spec struct {
    NetGateway string          // sushy/BMC host, 192.168.126.1
    SushyPort  int
    SushyUser  string
    SushyPass  string
    Channel    string          // operator subscription channel, "stable"
    Namespace  string          // fencing/operator ns, "openshift-workload-availability"
    Nodes      []host.VM       // masters + workers + spares (from host.Result.Nodes)
    UUIDs      map[string]string // domain name -> libvirt UUID (Redfish system id)
    WorkerCount int
}

func InstallOperators(ctx context.Context, r runner, spec Spec) error
func ConfigureBMH(ctx context.Context, r runner, spec Spec) error
func ConfigureFencing(ctx context.Context, r runner, spec Spec) error
func ProvisionWorkers(ctx context.Context, r runner, spec Spec) error
```

Templates (`embed.FS` + `text/template`, ported from `rhwa.sh`/`openshift.sh`):

```
internal/baremetal/rhwa/
  operators.go   operators.yaml.tmpl   # Namespace + AllNamespaces OperatorGroup + 6 Subscriptions
  bmh.go         bmh.yaml.tmpl         # per-node: master BMC-only patch/secret; worker+spare provisionable BMH+secret
  fencing.go     fencing.yaml.tmpl     # FenceAgentsRemediationTemplate (fence_redfish) + NodeHealthCheck (worker selector)
  workers.go                           # scale MachineSet, wait Ready, unschedule masters (no template)
  *_test.go
```

- **InstallOperators** — apply `operators.yaml`, then poll each CSV to `Succeeded`
  (best-effort, warn-and-continue like rhwa-lab). Ports `rhwa_install_operators`.
- **ConfigureBMH** — for each node: masters get a BMC-only secret + BMH
  (`externallyProvisioned: true`, never a `bootMACAddress`/`rootDeviceHints`);
  workers/spares get a provisionable BMH (`bootMACAddress`, `rootDeviceHints:
  /dev/vda`) + secret. `bmc.address =
  redfish-virtualmedia://<gw>:<port>/redfish/v1/Systems/<uuid>`. Ports
  `rhwa_configure_bmh`/`_apply_worker_bmh`. A node with no UUID is skipped with a
  warning.
- **ConfigureFencing** — a `FenceAgentsRemediationTemplate` with
  `agent: fence_redfish`, shared `--ip/--ipport/--username/--password/--ssl-insecure`,
  and per-node `--systems-uri` (Node hostname → `/redfish/v1/Systems/<uuid>`), plus
  a `NodeHealthCheck` selecting worker-role, non-control-plane nodes. Ports
  `rhwa_configure_fencing`/`_far_nodeparams`.
- **ProvisionWorkers** — if `WorkerCount == 0`, no-op. Else scale the baremetal
  MachineSet to `WorkerCount` (`oc … scale machineset`), poll until that many
  worker-role/non-control-plane nodes are `Ready` (`RunCapture` + count), then
  `oc patch schedulers/cluster mastersSchedulable=false`. Ports
  `os_provision_workers`/`os_unschedule_masters`.

## Section 2 — New package `internal/baremetal/lifecycle`

The orchestrator plus the pure profile→spec builders.

```go
package lifecycle

// Input is built by the worker handler from the cluster + profile + pull secret.
type Input struct {
    ClusterID, ClusterName, Region, BaseDomain string
    Version    string   // ocpctl version, e.g. "4.22"
    PullSecret string
    CreatedAt  time.Time
    BareMetal  *profile.BareMetalConfig
    Compute    *profile.ComputeConfig
    Networking *profile.NetworkingConfig
    InstallConfig []byte // rendered by the caller (existing renderer), sshKey filled below
}

// Result is what the handler persists.
type Result struct {
    Kubeconfig        []byte
    KubeadminPassword []byte
    APIURL, ConsoleURL string
    Recovered         bool
}

func Create(ctx context.Context, out io.Writer, in Input) (*Result, error)
func Destroy(ctx context.Context, in DestroyInput) error // DestroyInput{Region,ClusterName,BaseDomain,ZoneID}
```

`Create` (thin glue, in create.go):
1. `generateSushyCreds()` → user/pass.
2. `buildLaunchSpec(in)` → `substrate.Launch` → `*Substrate`.
3. Derive `sshKey` from `sub.Signer.PublicKey()`; re-render or patch the
   install-config's `sshKey` (the caller passes install-config with a placeholder;
   lifecycle owns the ephemeral-key substitution — see Section 3).
4. `buildTopology(in)` → `host.ComputeNodes` → nodes; `buildHostSpec(in, nodes,
   sushy)`; `c := host.NewClient(sub.Addr, sub.User, sub.Signer, out)`;
   `c.WaitReachable`; `c.Connect`; `defer c.Close()`; `host.Provision(ctx, c, hs)`
   → `*host.Result`.
5. `buildInstallSpec(in, sub, provResult)` → `agent.Install(ctx, c, is)` → creds.
6. `rhwaSpec` from `in` + `provResult.UUIDs` + sushy; `rhwa.ConfigureBMH` →
   `InstallOperators` → `ConfigureFencing` → `ProvisionWorkers`.
7. Return `Result` from the agent creds + URLs.

`Destroy` → `substrate.Teardown(ctx, substrate.TeardownSpec{…})`.

Pure builders (spec.go, unit-tested):
- `parseNodeSize(instanceType string) (vcpu, ramGB int)` — `vm-(\d+)vcpu-(\d+)gb`,
  with CP/worker fallbacks.
- `generateSushyCreds() (user, pass string, err error)` — random.
- `buildLaunchSpec`, `buildTopology`, `buildHostSpec`, `buildInstallSpec` — field
  mappings from `Input` (asserted in tests: instance type, host volume, region,
  base domain, tags/provenance, CIDR/gateway/VIPs, master/worker/spare counts and
  sizes, rendezvous IP = first master, sushy wiring).

## Section 3 — install-config sshKey substitution

The existing renderer (`Renderer.RenderInstallConfig`) already emits the correct
agent/baremetal install-config; its `sshKey` comes from the caller's
`SSHPublicKey`. To use the **ephemeral** key (known only after `substrate.Launch`),
the worker handler renders the install-config with the ephemeral public key: the
handler calls `Launch` is *not* an option (Launch is inside `lifecycle`), so
instead **`lifecycle.Create` renders the install-config itself** from the profile
via the existing renderer, after `Launch`, injecting
`ssh.MarshalAuthorizedKey(sub.Signer.PublicKey())` as `SSHPublicKey`. `Input`
therefore carries the pieces the renderer needs (or a prepared `*CreateClusterRequest`)
rather than pre-rendered bytes. This keeps the ephemeral-key decision entirely
inside `lifecycle` and out of the handler. The renderer stays untouched.

## Section 4 — Worker handler rewrite

- **`handleBareMetalCreate`** (`handler_create_baremetal.go`): keep the existing
  scaffolding (status → Creating, profile/pull-secret load, `ensureSecureWorkDir`,
  LogStreamer on a `baremetal.log`), but replace the rhwa-lab shell-out with:
  open the log file as the `out` writer, call `lifecycle.Create(ctx, out, input)`,
  write the returned kubeconfig + password to `workDir/auth/`, then reuse the
  existing `extractClusterOutputs` + `ClusterOutputs.Upsert` + `storeArtifacts(…,
  false)` + status → Ready + `SetLastWorkHoursCheck` + `handlePostDeployment`. On
  error, status → Failed (destroy will clean up the tagged substrate).
- **`handleBareMetalDestroy`** (`handler_destroy_baremetal.go`): replace the
  shell-out with `lifecycle.Destroy(ctx, …)`; keep the failed-before-launch
  short-circuit, the destroy-log storage, `DeleteClusterArtifacts`, and
  `MarkDestroyed`. On error, status → DestroyFailed.
- **Delete** `internal/installer/baremetal.go` and the rhwa-lab env helpers in
  `handler_create_baremetal.go` (`baremetalEnv`, `collectBareMetalAuth`,
  `rhwaLabStateDir`, `rhwaOCPVersion`/`rhwaMinorVersion`) and the destroy env slice.
- **Profile:** remove `defaultAddons: [rhwa]` from `baremetal-rhwa-lab.yaml` and
  refresh the `metadata.notes` that reference the rhwa-lab orchestrator.

## Section 5 — Cross-cutting

- **Streaming:** `lifecycle.Create` writes every step (substrate, provision,
  install, rhwa) to the one `out` file the LogStreamer tails — the whole native
  flow shows up live like the old shell-out did.
- **Failure semantics:** any step error fails the create; the cluster goes Failed;
  the tagged substrate is reclaimed by a later destroy (or the janitor —
  everything is tagged `ManagedBy=ocpctl` + `ClusterName`). rhwa steps that
  rhwa-lab treats as non-fatal (CSV waits, worker Ready timeout, unschedule) stay
  warn-and-continue.
- **Host-key verification:** `InsecureIgnoreHostKey` throughout (pieces 2/3),
  documented; hardening follow-up unchanged.

## Section 6 — Testing

- **rhwa templates** (table-driven): master BMC-only vs. worker/spare
  provisionable BMH; `redfish-virtualmedia` address with the right uuid/port;
  operator subscription set + AllNamespaces OperatorGroup; FAR `fence_redfish`
  shared params + per-node `--systems-uri`; NHC worker selector.
- **rhwa orchestration** over a fake `runner`: ConfigureBMH applies one manifest
  per node and skips a UUID-less node; InstallOperators applies then polls CSVs;
  ProvisionWorkers no-ops at 0 and otherwise scales + polls + unschedules.
- **lifecycle builders** (table-driven): `parseNodeSize` cases; LaunchSpec/
  Topology/HostSpec/InstallSpec field mappings; sushy creds are non-empty and
  distinct across calls; sshKey wiring uses the ephemeral key.
- **Handler rewrite:** covered by `go build ./...` + existing worker tests
  compiling against the new call; the live create/destroy is the piece-4 E2E.
- **No live AWS / host / cluster** in unit tests.

## Section 7 — Boundaries & out of scope

- **Consumes:** pieces 1-3 (`substrate`, `host`, `agent`) and the existing
  renderer, worker helpers (`ensureSecureWorkDir`, `extractClusterOutputs`,
  `storeArtifacts`, `handlePostDeployment`, `LogStreamer`), and store methods.
- **Exposes:** a fully native bare-metal create/destroy; the rhwa-lab binary and
  `internal/installer/baremetal.go` are gone.
- **Out of scope / follow-ups:** `monitor`, out-of-band power, `test`/fence
  *verification* (the lab's `test` subcommand), EC2 host-key pinning, surfacing the
  user's own SSH key, and decoupling install-config compute-replicas from the
  post-install worker count (only needed if a profile sets workers.replicas > 0).
