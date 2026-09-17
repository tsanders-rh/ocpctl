# Bare-metal Integration & Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the shipped native pieces (`substrate`, `host`, `agent`) into the worker create/destroy dispatch, add the post-install cluster configuration (BareMetalHosts, RHWA operators, `fence_redfish` fencing, optional worker provisioning), and delete the `rhwa-lab` shell-out. Result: ocpctl natively produces a cluster equivalent to rhwa-lab's.

**Architecture:** Post-install config splits into two packages (for reuse): the generic `internal/baremetal/metal3` (BareMetalHost wiring + MachineSet worker provisioning) and the RHWA-specific `internal/baremetal/rhwa` (Medik8s operators + `fence_redfish` fencing), both applying manifests over the host client (`oc` on the host). A new `internal/baremetal/lifecycle` package holds the pure profile→spec builders and a thin `Create`/`Destroy` orchestrator that chains `substrate.Launch → host.Provision → agent.Install → rhwa.* → metal3.*`. The worker handlers become thin adapters over `lifecycle`. `internal/installer/baremetal.go` and the rhwa-lab env contract are removed.

> **Note:** Tasks 1-2 below were written before the metal3/rhwa split and describe a single `rhwa` package; as built, `ConfigureBMH`/`ProvisionWorkers` and the BMH templates are in `metal3`, `InstallOperators`/`ConfigureFencing` and the operator/fencing templates in `rhwa`. Behaviour and tests are unchanged.

**Tech Stack:** Go 1.25; reuses `internal/baremetal/{substrate,host,agent}`, `internal/profile` (renderer + types), `internal/worker` helpers, `golang.org/x/crypto/ssh`; `embed`/`text/template`; `testify`.

**Spec:** `docs/superpowers/specs/2026-09-09-baremetal-integration-lifecycle-design.md`

## Global Constraints

- **ocpctl as orchestrator, rhwa-lab as reference only** — the binary and shell-out are deleted in Task 5.
- **Post-install `oc` runs on the host** via the host client, using `agent.RemoteOC --kubeconfig=agent.RemoteKubeconfig`. Manifests are applied by piping rendered YAML to `oc apply -f -` in a **single-quoted heredoc** (`<<'EOF'`) so the shell never expands the YAML (Go `text/template` substitutes first).
- **RHWA is native during create**, in rhwa-lab's order (operators → fencing → BMH … actually BMH → operators → fencing → workers per Section 3); the `rhwa` addon is removed from the profile.
- **Sushy creds + ephemeral sshKey** flow from `lifecycle` (single source), never persisted.
- **Testability:** `rhwa` renders templates (table-tested) and orchestrates over a narrow `runner` interface (fake-tested); `lifecycle` pure builders are unit-tested; the `Create`/`Destroy` glue + handler rewrite are covered by `go build ./...` and the piece-4 live E2E.
- **Comment style / shell safety:** terse doc comments; interpolated shell values via a local `shQuote` where not already schema-validated.

---

### Task 1: `rhwa` package — manifests & rendering

**Files:**
- Create: `internal/baremetal/rhwa/types.go` (`Spec`, `runner`, oc-prefix const, node filters)
- Create: `internal/baremetal/rhwa/render.go` (embed + render funcs)
- Create: `internal/baremetal/rhwa/templates/{operators,bmh-master,bmh-worker,fencing}.sh.tmpl`
- Test: `internal/baremetal/rhwa/render_test.go`

**Interfaces:**
- Consumes: `internal/baremetal/host` (`VM`), `internal/baremetal/agent` (`RemoteOC`, `RemoteKubeconfig`).
- Produces: `Spec`, `runner`; `renderOperators(Spec)`, `renderMasterBMH(Spec, host.VM)`, `renderWorkerBMH(Spec, host.VM)`, `renderFencing(Spec)`; `clusterNodes(Spec) []host.VM` (non-spare), `bmcAddress(Spec, uuid)`.

**`types.go`:**
```go
package rhwa

const (
	defaultNamespace = "openshift-workload-availability"
	defaultChannel   = "stable"
	// oc runs on the host against the installer kubeconfig (root-owned; runner.Run is sudo).
	ocCmd = agent.RemoteOC + " --kubeconfig=" + agent.RemoteKubeconfig
)

type runner interface {
	Run(ctx context.Context, script string) error
	RunCapture(ctx context.Context, cmd string) (string, error)
}

type Spec struct {
	NetGateway  string
	SushyPort   int
	SushyUser   string
	SushyPass   string
	Channel     string
	Namespace   string
	Nodes       []host.VM         // masters + workers + spares
	UUIDs       map[string]string // domain name -> libvirt UUID
	WorkerCount int
}

func (s Spec) ns() string      { if s.Namespace != "" { return s.Namespace }; return defaultNamespace }
func (s Spec) channel() string { if s.Channel != "" { return s.Channel }; return defaultChannel }

func bmcAddress(gw string, port int, uuid string) string {
	return fmt.Sprintf("redfish-virtualmedia://%s:%d/redfish/v1/Systems/%s", gw, port, uuid)
}

// clusterNodes are the installed nodes (masters + real workers), excluding spares.
func clusterNodes(nodes []host.VM) []host.VM {
	out := make([]host.VM, 0, len(nodes))
	for _, n := range nodes { if !n.Spare { out = append(out, n) } }
	return out
}
```

**Templates** (rendered with Go, then piped to `oc apply -f -` via `<<'EOF'`). Port from `rhwa.sh`:

- `operators.sh.tmpl` — `set -euo pipefail` + one `{{.OC}} apply -f - <<'EOF' … EOF` containing: `Namespace` (`{{.NS}}`), an AllNamespaces `OperatorGroup` (`spec: {}`), and six `Subscription`s (`node-healthcheck-operator`, `fence-agents-remediation`, `self-node-remediation`, `node-maintenance-operator`, `machine-deletion-remediation`) — each `channel: {{.Channel}}`, `source: redhat-operators`, `sourceNamespace: openshift-marketplace`, `installPlanApproval: Automatic`, `namespace: {{.NS}}`.
- `bmc-secret` fragment (shared) — a `Secret` `{{.Name}}-bmc-secret` in `openshift-machine-api` with `stringData.username/password` = sushy creds.
- `bmh-master.sh.tmpl` — apply the secret, then `{{.OC}} -n openshift-machine-api get baremetalhost {{.Host}} && patch (online + bmc addr/creds/disableCertificateVerification) || apply externallyProvisioned:true BMH (BMC only, no bootMAC/rootDeviceHints)`. Ports the master branch of `rhwa_configure_bmh`.
- `bmh-worker.sh.tmpl` — apply the secret, then apply a provisionable `BareMetalHost` (`online:true`, `bootMACAddress:{{.MAC}}`, `rootDeviceHints.deviceName:/dev/vda`, `bmc.address/credentialsName/disableCertificateVerification`). Shared by workers and spares. Ports `_apply_worker_bmh`.
- `fencing.sh.tmpl` — apply a `FenceAgentsRemediationTemplate` (`agent: fence_redfish`, shared `--ip/--ipport/--username/--password/--ssl-insecure:"1"`, per-node `nodeparameters.--systems-uri` = `{{.Host}} -> /redfish/v1/Systems/{{uuid}}` for **cluster nodes** only), then a `NodeHealthCheck` (`minHealthy:"51%"`, selector worker Exists + control-plane DoesNotExist, `remediationTemplate` → the FAR template, unhealthy Ready False/Unknown 60s). Ports `rhwa_configure_fencing`/`_far_nodeparams`.

The render funcs build a template-data struct carrying `OC` (the `ocCmd`), `NS`, `Channel`, and the node/uuid/bmc values, and `Execute` into a string.

- [ ] **Step 1: Write the templates** (as above; `{{.OC}}` for the oc prefix; `<<'EOF'` heredocs).
- [ ] **Step 2: Write the failing render tests** — assert: operators script contains all six Subscription names + `spec: {}` OG + `{{namespace}}`; master BMH script contains `externallyProvisioned` fallback + BMC address `redfish-virtualmedia://<gw>:<port>/redfish/v1/Systems/<uuid>` and does **not** set `bootMACAddress`; worker BMH script contains `bootMACAddress`, `/dev/vda`, and the secret; fencing script contains `fence_redfish`, the shared params, a `--systems-uri` entry per cluster node (spares excluded), and the NHC worker selector.
- [ ] **Step 3: Run tests to verify they fail.**
- [ ] **Step 4: Write `types.go` + `render.go`.**
- [ ] **Step 5: Run tests + `go build ./...` to verify pass.**
- [ ] **Step 6: Commit** `rhwa: post-install manifest templates and rendering`.

---

### Task 2: `rhwa` package — orchestration

**Files:**
- Create: `internal/baremetal/rhwa/apply.go` (`InstallOperators`, `ConfigureBMH`, `ConfigureFencing`), `internal/baremetal/rhwa/workers.go` (`ProvisionWorkers`)
- Test: `internal/baremetal/rhwa/apply_test.go`, `internal/baremetal/rhwa/workers_test.go`

**Interfaces:**
- Produces: `InstallOperators(ctx, runner, Spec) error`, `ConfigureBMH(ctx, runner, Spec) error`, `ConfigureFencing(ctx, runner, Spec) error`, `ProvisionWorkers(ctx, runner, Spec, sleep func(time.Duration)) error`. Public wrappers use `time.Sleep`.

**Design notes:**
- `InstallOperators` — `Run(renderOperators(spec))`, then best-effort poll each CSV prefix to `Succeeded` via `RunCapture(ocCmd + " -n <ns> get csv -o jsonpath …")` in a bounded loop; warn-and-continue (never fail the create). Ports `rhwa_install_operators`/`_wait_csv`.
- `ConfigureBMH` — for each `spec.Nodes`: skip if `UUIDs[node.Name]==""` (warn to the runner's log); masters → `Run(renderMasterBMH(spec, node))`; workers/spares → `Run(renderWorkerBMH(spec, node))`. Ports `rhwa_configure_bmh`.
- `ConfigureFencing` — `Run(renderFencing(spec))`. Ports `rhwa_configure_fencing`.
- `ProvisionWorkers` — if `spec.WorkerCount == 0` return nil (masters stay schedulable). Else: `ms := RunCapture(ocCmd + " -n openshift-machine-api get machineset -o jsonpath='{.items[0].metadata.name}'")`; `Run(ocCmd + " -n openshift-machine-api scale machineset <ms> --replicas=<n>")`; poll `RunCapture(ocCmd + " get nodes -l 'node-role.kubernetes.io/worker=,!node-role.kubernetes.io/control-plane' --no-headers | awk '$2==\"Ready\"' | wc -l")` until `>= WorkerCount` or bounded attempts (`sleep` between); then `Run(ocCmd + " patch schedulers.config.openshift.io/cluster --type=merge -p '{\"spec\":{\"mastersSchedulable\":false}}'")`. Worker-Ready timeout and unschedule failure are warn-and-continue. Ports `os_provision_workers`/`os_unschedule_masters`.
- Errors from `Run` on the apply steps (operators/BMH/fencing) fail the step (`fmt.Errorf("rhwa <step>: %w", err)`), except the explicitly best-effort waits.

- [ ] **Step 1: Write the failing tests** against a fake `runner` (records scripts; `RunCapture` returns canned values keyed by substring):
  - `ConfigureBMH` applies one script per node; a node whose UUID is missing is skipped; a master node's script differs from a worker's (assert via recorded substrings).
  - `InstallOperators` applies the operators script then issues CSV polls.
  - `ProvisionWorkers` with `WorkerCount==0` issues no scale; with `>0` scales, polls until Ready count reached, then patches `mastersSchedulable`.
- [ ] **Step 2: Run tests to verify they fail.**
- [ ] **Step 3: Write `apply.go` + `workers.go`.**
- [ ] **Step 4: Run tests + `go build ./...`.**
- [ ] **Step 5: Commit** `rhwa: apply operators/BMH/fencing and provision workers`.

---

### Task 3: `lifecycle` package — pure builders

**Files:**
- Create: `internal/baremetal/lifecycle/types.go` (`Input`, `Result`, `DestroyInput`), `internal/baremetal/lifecycle/spec.go`
- Test: `internal/baremetal/lifecycle/spec_test.go`

**Interfaces:**
- Consumes: `substrate` (`LaunchSpec`), `host` (`Topology`, `HostSpec`, `VM`, `ComputeNodes`), `agent` (`InstallSpec`), `rhwa` (`Spec`), `profile` types, `pkg/types`.
- Produces: pure `parseNodeSize(string) (vcpu, ramGB int)`, `generateSushyCreds() (user, pass string, err error)`, `buildLaunchSpec(Input) substrate.LaunchSpec`, `buildTopology(Input) host.Topology`, `buildHostSpec(Input, nodes []host.VM, user, pass string) host.HostSpec`, `buildRHWASpec(Input, nodes, uuids, user, pass) rhwa.Spec`, `netGateway(cidr) string`, `firstMasterIP(nodes) string`.

**Design notes:**
- `parseNodeSize`: regex `^vm-(\d+)vcpu-(\d+)gb$`; fallback CP (8, 20) / worker (4, 16) chosen by a `role` arg or two wrappers.
- `generateSushyCreds`: user `"sushy"`; password = 24 hex chars from `crypto/rand`.
- `buildTopology`: `ControlPlaneCount = Compute.ControlPlane.Replicas`, `WorkerCount = Compute.Workers.Replicas`, `SpareCount = BareMetal.SpareWorkerCount`, CP/WK vcpu/ram from `parseNodeSize(instanceType)`, `NetCIDR = BareMetal.NetworkCIDR`, `ClusterName`.
- `buildLaunchSpec`: cluster id/name/region/basedomain, `InstanceType=BareMetal.HostInstanceType`, `AMIOwner=BareMetal.HostAMIOwner`, `FedoraRelease`, `HostVolumeGB`, `CreatedAt`. (Zone/AMI overrides left default.)
- `buildHostSpec`: `LibvirtNet="rhwa"`, `NetCIDR`, `NetGateway=netGateway(cidr)` (`.1`), VIPs from `BareMetal`, `SushyPort`, sushy creds, `NodeDiskGB`, `Nodes`.
- `netGateway`: replace last octet of the CIDR network with `.1`.
- `firstMasterIP`: first `Role=="master"` node's IP → rendezvous.

- [ ] **Step 1: Write the failing table tests** — `parseNodeSize` cases (`vm-8vcpu-20gb`→(8,20); junk→fallback); sushy creds non-empty and distinct across two calls; `buildTopology`/`buildHostSpec`/`buildLaunchSpec` field assertions from a representative `Input` (mirroring the shipped profile: 3 masters vm-8vcpu-20gb, 0 workers, 3 spares vm-4vcpu-16gb, CIDR 192.168.126.0/24, VIPs .5/.6, sushyPort 8000).
- [ ] **Step 2: Run tests to verify they fail.**
- [ ] **Step 3: Write `types.go` + `spec.go`.**
- [ ] **Step 4: Run tests + `go build ./...`.**
- [ ] **Step 5: Commit** `lifecycle: profile-to-spec builders and sushy creds`.

---

### Task 4: `lifecycle` — Create/Destroy orchestration

**Files:**
- Create: `internal/baremetal/lifecycle/create.go`, `internal/baremetal/lifecycle/destroy.go`
- (No new unit test — glue is covered by build + the E2E; add a small `create_smoke_test.go` only if a seam is cheaply fakeable.)

**Interfaces:**
- Produces: `Create(ctx, out io.Writer, in Input) (*Result, error)`, `Destroy(ctx, in DestroyInput) error`.

**Design notes (create.go), following spec Section 2:**
1. `user, pass, err := generateSushyCreds()`.
2. `sub, err := substrate.Launch(ctx, buildLaunchSpec(in))`.
3. `sshKey := string(ssh.MarshalAuthorizedKey(sub.Signer.PublicKey()))`; render install-config via the existing renderer with `SSHPublicKey=&sshKey` (Input carries the `*types.CreateClusterRequest` + pullSecret + effectiveTags needed, or lifecycle builds the request from Input); the rendered bytes feed `agent.InstallSpec.InstallConfig`.
4. `nodes := host.ComputeNodes(buildTopology(in))`; `hs := buildHostSpec(in, nodes, user, pass)`; `c := host.NewClient(sub.Addr, sub.User, sub.Signer, out)`; `c.WaitReachable(ctx, 5*time.Minute)`; `c.Connect(ctx)`; `defer c.Close()`; `pr, err := host.Provision(ctx, c, &hs)`.
5. `is := buildInstallSpec(in, sub, pr, installConfigBytes)` (masters = filter `pr.Nodes` to non-spare masters; `RendezvousIP=firstMasterIP`, `NetGateway`, `OCPVersion=mirrorVersion(in.Version)`); `creds, err := agent.Install(ctx, c, is)`.
6. `rs := buildRHWASpec(in, pr.Nodes, pr.UUIDs, user, pass)`; `rhwa.ConfigureBMH` → `InstallOperators` → `ConfigureFencing` → `ProvisionWorkers`.
7. Return `&Result{creds.Kubeconfig, creds.KubeadminPassword, creds.APIURL, creds.ConsoleURL, creds.Recovered}`.
- `mirrorVersion`: a bare `X.Y` → `stable-X.Y`; a full `X.Y.Z` passes through (ports `rhwaOCPVersion`).
- `Destroy`: `substrate.Teardown(ctx, substrate.TeardownSpec{Region, ClusterName, BaseDomain, ZoneID})`.
- Wrap errors `fmt.Errorf("baremetal create <step>: %w", err)`.

- [ ] **Step 1: Write create.go + destroy.go per the notes.**
- [ ] **Step 2: `go build ./...` + `go vet ./internal/baremetal/...`.**
- [ ] **Step 3: Commit** `lifecycle: native create/destroy orchestration`.

---

### Task 5: Worker handler rewrite + shell-out removal

**Files:**
- Modify: `internal/worker/handler_create_baremetal.go` (rewrite; drop rhwa-lab env helpers)
- Modify: `internal/worker/handler_destroy_baremetal.go` (rewrite)
- Delete: `internal/installer/baremetal.go`
- Modify: `internal/profile/definitions/baremetal-rhwa-lab.yaml` (remove `defaultAddons: [rhwa]`; refresh notes)
- Test: adjust any worker test that referenced the removed symbols; `go build ./...`

**Design notes:**
- **Create** (`handleBareMetalCreate`): keep status→Creating, profile/pull-secret load, `ensureSecureWorkDir`, and the LogStreamer on `filepath.Join(workDir, "baremetal.log")`. Replace the shell-out block with:
  ```go
  logFile, err := os.Create(logPath)         // host client out; streamer tails logPath
  ...
  in := lifecycle.Input{ /* cluster + prof.PlatformConfig.BareMetal + prof.Compute + prof.Networking + pullSecret + createReq + effectiveTags */ }
  res, err := lifecycle.Create(ctx, logFile, in)
  logFile.Close(); streamCancel(); streamer.Stop()
  if err != nil { h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusFailed); return err }
  writeAuthBundle(workDir, res.Kubeconfig, res.KubeadminPassword)   // workDir/auth/{kubeconfig,kubeadmin-password}, 0600
  outputs, _ := h.extractClusterOutputs(workDir, cluster); h.store.ClusterOutputs.Upsert(ctx, outputs)
  h.storeArtifacts(ctx, workDir, cluster.ID, false)
  h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady)
  h.store.Clusters.SetLastWorkHoursCheck(ctx, cluster.ID, time.Now().Add(WorkHoursGracePeriod))
  h.handlePostDeployment(ctx, cluster)
  ```
  Delete `baremetalEnv`, `collectBareMetalAuth`, `rhwaLabStateDir`, `rhwaOCPVersion`, `rhwaMinorVersion` (mirrorVersion now lives in `lifecycle`). Add a small `writeAuthBundle` helper (or reuse an existing auth-writing helper if present).
- **Destroy** (`handleBareMetalDestroy`): keep the failed-before-launch short-circuit + destroy-log + `finishBareMetalDestroy`. Replace the shell-out with:
  ```go
  err := lifecycle.Destroy(ctx, lifecycle.DestroyInput{Region: cluster.Region, ClusterName: cluster.Name, BaseDomain: baseDomain})
  if err != nil { h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusDestroyFailed); return err }
  ```
  Keep streaming to a `baremetal-destroy.log` around `lifecycle.Destroy` if desired (Teardown does not stream, so this log will be brief; optional).
- **Delete** `internal/installer/baremetal.go`; confirm `grep -rn "NewBareMetalInstaller\|installer.BareMetal" internal/` is clean afterward.
- **Profile:** remove the `defaultAddons` block (RHWA is native now) and update `metadata.notes` (drop "via the rhwa-lab orchestrator").

- [ ] **Step 1: Rewrite the two handlers; add `writeAuthBundle`.**
- [ ] **Step 2: Delete `internal/installer/baremetal.go`; edit the profile YAML.**
- [ ] **Step 3: `go build ./...`; fix any dangling references (imports, removed helpers).**
- [ ] **Step 4: `go vet ./...` and run `go test ./internal/worker/... ./internal/profile/... ./internal/baremetal/...`.**
- [ ] **Step 5: Commit** `worker: native bare-metal create/destroy; remove rhwa-lab shell-out`.

---

## Self-Review

- **Spec coverage:** Section 1 (`rhwa`) → Tasks 1-2. Section 2 (`lifecycle` builders + orchestration) → Tasks 3-4. Section 3 (sshKey inside lifecycle) → Task 4 step 3. Section 4 (handler rewrite + removals + profile) → Task 5. Section 6 testing → template + fake-runner + builder unit tests; glue via build + E2E.
- **RHWA equivalence:** operators + `fence_redfish` fencing + BMH + workers all native and parameterized by the substrate UUIDs/sushy creds — the addon (SNR-only) is removed to avoid double install.
- **Reuse:** existing renderer, `extractClusterOutputs`, `storeArtifacts`, `handlePostDeployment`, LogStreamer, store methods; `agent.RemoteOC/RemoteKubeconfig` as the single host-path source.
- **Removals confirmed:** `internal/installer/baremetal.go`, `baremetalEnv`, `collectBareMetalAuth`, `rhwaLabStateDir`, `rhwaOCPVersion`, `rhwaMinorVersion`, `defaultAddons: [rhwa]`.
- **Out of scope:** monitor/power/test-fence, host-key pinning, user sshKey, install-config-replicas decoupling — all follow-ups.
