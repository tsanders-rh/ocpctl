# Bare-metal ODF-external + single-VM Ceph subsystem — design

Date: 2026-09-15
Status: Approved (design); implementation pending
Branch: `feat/baremetal-agent-libvirt-provider`

## Context

ocpctl has a native bare-metal / agent-based OpenShift provider (pieces 1–4:
`substrate`, `host`, `agent`, `lifecycle`, `metal3`, `rhwa`) that reproduces the
`rhwa-lab` bash orchestrator using ocpctl's own conventions, with rhwa-lab as a
*reference*. rhwa-lab has since gained an **ODF-external** feature
(`lib/odf.sh`, commit `def6978`): it stands up a single-VM Ceph cluster on the
EC2 host and connects OpenShift Data Foundation to it in **external mode**.

This spec covers integrating that feature (call it **piece 5**) following the
patterns already established, and reusing the piece-1–4 machinery.

### What rhwa-lab's ODF feature does

`odf_setup` runs at the end of `create` (after `os_provision_workers`), gated by
`ODF_ENABLED` (ceph VM by `CEPH_ENABLED`, defaulting to `ODF_ENABLED`):

1. `odf_ceph_define_vm` — define a **CentOS-Stream cloud VM** on the `rhwa`
   libvirt network (`<cluster>-ceph-0`, IP `.10`, MAC role-byte `03`, 4 vCPU /
   16 GB / 40 GB root) plus `CEPH_OSD_COUNT` blank virtio data disks (the OSDs),
   booted with a **NoCloud cloud-init seed** (own `genisoimage` ISO) that injects
   the SSH key and a **static IP** (matched by MAC). Idempotency is *health-based*
   and never destroys a bootstrapped ceph's data disks.
2. `odf_ceph_bootstrap` — over SSH to the VM: install podman/cephadm (release-
   pinned), `cephadm bootstrap --single-host-defaults`, `ceph orch apply osd
   --all-available-devices`, create + size the replicated RBD pool, enable the
   ceph-mgr prometheus module. Verified (missing OSDs/pool is fatal).
3. `odf_ceph_export` — run the rook `create-external-cluster-resources.py`
   exporter inside `cephadm shell`, capture the connection JSON.
4. `odf_install_operator` — OLM Subscription for `odf-operator` in
   `openshift-storage`.
5. `odf_import_external` — **normalize** the exporter JSON (add a
   `*-secret-namespace` for every `*-secret-name` so the generated StorageClass is
   valid) and apply it two ways: the individual `rook-ceph-*` Secrets/ConfigMaps,
   **and** the `rook-ceph-external-cluster-details` blob secret.
6. `odf_create_storagecluster` — an external-mode `StorageCluster`; ODF then
   creates the `ceph-rbd` StorageClass automatically.

Supporting changes: the EC2 **host root volume grows** to
`1000 + CEPH_OSD_COUNT * CEPH_OSD_DISK_GB` GB when ODF is enabled (to hold the OSD
qcow2s), and a companion refinement evicts DaemonSet pods stranded on masters
after unscheduling (`os_evict_stranded_daemons`, commit `ab3c742`).

## How it maps onto ocpctl (reusing the existing pieces)

- **New package `internal/baremetal/odf`** — a cohesive, feature-specific package
  (sibling to `metal3`/`rhwa`), holding the ceph VM definition, bootstrap, export,
  import, and StorageCluster steps, plus their templates. This mirrors the
  metal3/rhwa split: `metal3` is generic, `rhwa` and `odf` are the specific
  workloads layered on top.
- **Host-side libvirt work reuses the piece-2 host client.** Defining the ceph VM
  (`genisoimage` seed + `qemu-img` + `virt-install --print-xml`/`virsh define`)
  is the same `client.Run`/`client.Upload` pattern piece 2 uses for the node
  domains — rendered from `embed.FS` templates.
- **The host→VM SSH hop reuses `host.Client.DialNode` (piece 3).** The ceph VM is
  only reachable from the host; piece 3 already tunnels a `crypto/ssh` client
  through the host connection. Two adaptations:
  - **User is configurable.** `DialNode` currently hardcodes `core` (RHCOS); the
    ceph VM's cloud image user is `cloud-user`. Generalize `DialNode(ctx, nodeIP,
    user)` (or add a variant) — a small, backward-compatible change.
  - **The ceph VM trusts the ephemeral substrate key.** Its cloud-init
    `ssh_authorized_keys` gets the same ephemeral public key the cluster nodes
    trust (from `Substrate.Signer`), so ocpctl tunnels to it with the in-process
    Signer. This is the ocpctl-native equivalent of rhwa-lab's agent-forwarding
    of the operator's key — no operator key, no `-A`, private key never on disk.
  - A thin `odf` VM-runner wraps `DialNode` to give `Run`/`RunCapture`/`Upload`
    against the VM (`sudo bash -s`), the same shape as `host.Client`.
- **`oc`-against-the-cluster steps run on the host** (operator install, import,
  StorageCluster), exactly like `rhwa`/`metal3` — `agent.RemoteOC
  --kubeconfig=agent.RemoteKubeconfig` piped to `oc apply -f -`.
- **The ODF operator set comes from the addon catalog** — following the piece-4
  RHWA precedent (`store.PostConfigAddons.GetByAddonID("odf")`). See Decisions:
  the existing `odf` addon is *script*-shaped (internal mode), so this needs a
  small choice.
- **`lifecycle.Create` gains an ODF step** after `metal3.ProvisionWorkers`
  (rhwa-lab's order), gated by the profile, streaming to the same deployment log.
- **Destroy is unchanged.** The ceph VM lives on the host; `substrate.Teardown`
  (terminate the host) already takes it with everything else. No ODF-specific
  teardown.

## Decisions (to settle during brainstorming)

- **ODF-external runs natively during create, not as a post-deploy addon** — same
  reasoning as RHWA fencing: the export/import/StorageCluster need create-time
  host+VM access (the ceph VM only exists on the host, reachable only from it),
  and the import is dynamic (built from the exporter JSON). Operator *install* is
  the only addon-shaped part.
- **Operator install is native, StorageCluster is external-mode** (settled). The
  existing `odf` addon is a script-based *internal*-mode install, so the `odf`
  package installs the single `odf-operator` Subscription itself and creates an
  **external-mode** `StorageCluster` pointing at the provisioned ceph host — the
  whole ODF-external flow lives in one native package.
- **Resilient operator-channel selection** (settled; see [[resilient-operator-channels]]).
  Do NOT hardcode `stable-<ocp-minor>` — a pre-release OCP 4.22 exposed a
  different ODF channel in the catalog, and rhwa-lab only falls back to a static
  `4.22`. Instead, at install time query the operator's **PackageManifest**
  (`oc get packagemanifest odf-operator -n openshift-marketplace` →
  `.status.channels[].name` + `.status.defaultChannel`) and choose: the desired
  `stable-<minor>` if present, else the catalog's `defaultChannel`, else the newest
  `stable-X.Y` by version. This lands as a small generic helper
  `internal/baremetal/olm.ResolveChannel(ctx, runner, pkg, desired)` (runs `oc` on
  the host), used by ODF now and adoptable by the RHWA operator install. The
  existing script-based `odf` addon has the same fragility — fixing it (internal
  mode, non-baremetal) is a noted follow-up.
- **Ceph VM key = ephemeral substrate key + tunneled `DialNode`** (see above) —
  the clean ocpctl-native replacement for agent-forwarding.
- **Host volume sizing is a substrate ripple.** When ODF is enabled,
  `lifecycle.buildLaunchSpec` computes `HostVolumeGB = 1000 + osdCount *
  osdDiskGB` (rhwa-lab's formula), overriding the profile default, so the EC2 root
  disk can hold the OSD qcow2s. `osdDiskGB` is derived from target usable GB ×
  replica × headroom ÷ osdCount (rhwa-lab's formula), all pure and unit-testable.
- **Static IP via cloud-init, not a DHCP reservation** — mirror rhwa-lab: the ceph
  VM pins its own `.10` via cloud-init (matched by MAC), so piece 2's libvirt
  network (which only writes reservations at first define) needs **no change**.
- **Import-JSON normalization ported to Go.** rhwa-lab's `jq` that adds
  `*-secret-namespace` for every `*-secret-name`, and that splits the array into
  Secrets/ConfigMaps + a base64 blob, becomes Go `encoding/json` manipulation —
  pure and unit-testable (the highest-value test in this piece).
- **Idempotency preserved.** The ceph VM's health-based, never-gratuitously-
  destructive rebuild logic and the bootstrap's convergent verification are ported
  faithfully (a bootstrapped ceph's data disks are never auto-destroyed).

## Section 1 — Package & API (`internal/baremetal/odf`)

```go
package odf

type Spec struct {
    ClusterName   string
    BaseDomain    string
    Namespace     string        // openshift-storage
    ODFChannel    string        // stable-<minor>
    // ceph VM
    NodeName      string        // <cluster>-ceph-0
    Host          string        // ceph-0
    IP            string        // <net>.10
    MAC           string        // 52:54:00:6a:03:00
    LibvirtNet    string        // rhwa
    NetGateway    string
    SSHUser       string        // cloud-user
    SSHPubKey     string        // ephemeral substrate public key (authorized on the VM)
    CloudImageURL string
    OSVariant     string        // centos-stream9
    VCPU, RAMGB   int
    RootDiskGB    int
    OSDCount      int
    OSDDiskGB     int
    // ceph pool
    Release       string        // squid
    Image         string        // optional quay.io/ceph/ceph:<tag>
    RBDPool       string        // ocs-storagepool
    PoolReplica   int
    PoolUsableGB  int
    ExporterURL   string
}

// vmDialer tunnels an ssh client to the ceph VM through the host connection
// (host.Client.DialNode with SSHUser). Nil-able for tests.
type vmDialer interface {
    Dial(ctx context.Context, ip, user string) (*ssh.Client, error)
}

// Setup runs the whole ODF-external feature. hostRunner drives host-side libvirt +
// oc (the piece-2 client); dialer reaches the ceph VM. Streams to the client's log.
func Setup(ctx context.Context, hostRunner HostRunner, dialer vmDialer, spec Spec) error
```

Files (mirroring piece 2/3/4 shape):

```
internal/baremetal/odf/
  types.go        Spec, interfaces, defaults
  cephvm.go       DefineCephVM  (render seed + Upload + virt-install via hostRunner)
  bootstrap.go    Bootstrap     (cephadm/OSD/pool/prometheus over the VM tunnel)
  export.go       Export        (run exporter on the VM, capture JSON)
  importer.go     ImportExternal (normalize + apply Secrets/ConfigMaps + blob; PURE normalize)
  storagecluster.go CreateStorageCluster + InstallOperator
  setup.go        Setup orchestration
  *_test.go
  templates/
    ceph-user-data.yaml.tmpl
    ceph-meta-data.tmpl
    ceph-network-config.yaml.tmpl
    ceph-define.sh.tmpl      # genisoimage + qemu-img + virt-install
    ceph-bootstrap.sh.tmpl   # cephadm bootstrap + OSDs + pool + prometheus
    ceph-export.sh.tmpl
    storagecluster.yaml.tmpl
    odf-operator.yaml.tmpl   # Namespace + OperatorGroup + Subscription
```

## Section 2 — Steps (ported from `odf.sh`)

Each writes a header to the log and wraps errors `fmt.Errorf("odf <step>: %w",
err)`; the whole thing is a no-op when the profile disables ODF.

1. **DefineCephVM** — render `meta-data`/`user-data`/`network-config` (user-data
   authorizes `SSHPubKey`; network-config pins `IP` by `MAC`); `Upload` them;
   `Run` `ceph-define.sh` (health check → cache CentOS base → COW root + N blank
   OSD qcow2s → `genisoimage` seed → `virt-install --import --print-xml` → `virsh
   define`/`start`), preserving rhwa-lab's health-based idempotency.
2. **Bootstrap** — poll VM SSH reachability (via `dialer`); `Run` `ceph-bootstrap.sh`
   on the VM (dnf prereqs with retries; release-pinned cephadm; `bootstrap
   --single-host-defaults`; `orch apply osd --all-available-devices`; wait OSDs up;
   create/size RBD pool; enable prometheus) — verified, fatal on missing OSDs/pool.
3. **Export** — `Run` `ceph-export.sh` on the VM (`curl` exporter → `cephadm shell
   -- python3 -`), capture stdout; validate it's a non-empty JSON array.
4. **InstallOperator** — resolve the channel via `olm.ResolveChannel` (desired
   `stable-<minor>` → catalog `defaultChannel` → newest), then `oc apply` Namespace
   + `openshift-storage` OperatorGroup + `odf-operator` Subscription with the
   resolved channel; wait CSVs (`odf-operator`, `ocs-operator`) best-effort.
5. **ImportExternal** — **normalize** the JSON in Go (add `*-secret-namespace`),
   then `oc apply` the `List` of Secrets/ConfigMaps and the base64 blob secret.
6. **CreateStorageCluster** — `oc apply` the external `StorageCluster`; poll to
   `Ready` (best-effort warn).

## Section 3 — Profile & substrate ripple

`BareMetalConfig` gains an optional ODF block (or flat fields), e.g.:

```go
ODF *ODFConfig `yaml:"odf,omitempty"`
// ODFConfig: Enabled bool; Channel string; CephOSDCount, CephPoolUsableGB,
// CephReplica, CephVCPU, CephRAMGB, CephRootDiskGB int; CloudImageURL, CephRelease string.
```

The `baremetal-rhwa-lab` profile enables ODF with rhwa-lab's defaults. Defaults
(OSD count 3, usable 200 GB, replica 3, squid, CentOS-Stream URL, channel
`stable-<minor>`) live in the `odf` package like piece-1/3 defaults.
`lifecycle.buildLaunchSpec` overrides `HostVolumeGB` to `1000 + osdCount *
osdDiskGB` when ODF is enabled (pure, unit-tested).

## Section 4 — Testing

- **Import normalization** (the crux): from a representative exporter array, assert
  every `StorageClass.data` `*-secret-name` gains a matching `*-secret-namespace`,
  and that Secrets→stringData / ConfigMaps→data / the base64 blob are emitted
  correctly. Pure Go, table-driven.
- **Template rendering** (table-driven): cloud-init seed (authorized key, static IP
  by MAC), `ceph-define.sh` (N OSD disks, seed ISO, os-variant), `ceph-bootstrap.sh`
  (release-pinned cephadm, OSD count, pool sizing), `storagecluster.yaml`,
  `odf-operator.yaml`.
- **OSD-disk sizing** (`osdDiskGB`) and **host-volume** formulas — pure.
- **Setup orchestration** over fake `HostRunner` + `vmDialer` (canned outputs) —
  step order, no-op-when-disabled, the health-based VM idempotency branches.
- **No live AWS / host / VM / ceph** in unit tests; the live path is the piece-4/5
  E2E.

## Section 5 — Companion fix (parity with rhwa-lab)

Port `os_evict_stranded_daemons` (commit `ab3c742`) into
`metal3.ProvisionWorkers`' unschedule step: after `mastersSchedulable=false`,
delete DaemonSet-owned pods stranded on the (now-tainted) masters whose tolerations
don't cover the master taint, so `KubeDaemonSetMisScheduled`/`RolloutStuck` don't
fire. Small, self-contained, keeps metal3 faithful to the current reference.

## Section 6 — Boundaries & out of scope

- **Consumes:** `host.Client` (+ a user-parameterized `DialNode`), the ephemeral
  `Substrate.Signer` public key, `agent.RemoteOC/RemoteKubeconfig`, the profile,
  and (optionally) the `odf` addon channel.
- **Exposes:** `odf.Setup(...)`, called by `lifecycle.Create` when ODF is enabled.
- **Out of scope / follow-ups:** internal-mode ODF (the existing addon already
  covers non-baremetal clusters); CephFS/NooBaa specifics beyond what external mode
  auto-creates; multi-VM/HA ceph; and any RHWA operator-version bumps rhwa-lab made
  separately (e.g. the 5.0.0-rc.1 default) — those belong to the `rhwa` addon, not
  here.
