# Bare-metal AWS substrate lifecycle subsystem — design

Date: 2026-09-08
Status: Approved (design); implementation pending
Branch: `feat/baremetal-agent-libvirt-provider`

## Context

ocpctl is growing a native bare-metal / agent-based OpenShift provider with
**ocpctl as the orchestrator**, reproducing what the `rhwa-lab` bash script does
using ocpctl's own conventions (AWS Go SDK where ocpctl uses the SDK; shelling
out to CLIs like `openshift-install` where ocpctl already does), with rhwa-lab
as a *reference*, not a runtime dependency.

The native rebuild decomposes into four independently designable/testable pieces:

1. **AWS substrate lifecycle (this document)** — launch/tag/terminate the
   nested-virt EC2 host, security group, ephemeral keypair, Elastic IP, and
   Route53 records, via the AWS Go SDK.
2. **Remote host provisioning** (`internal/baremetal/host`, shipped) — SSH in and
   install libvirt + sushy-tools + haproxy; define the libvirt network and VMs.
3. **Agent-based install** — render configs, build the agent ISO once, boot VMs
   via sushy/virsh, `openshift-install agent wait-for`, collect kubeconfig.
4. **Integration + lifecycle** — worker create/destroy dispatch, RHWA via the
   existing addon path, teardown, and removal of the `rhwa-lab` shell-out
   handlers.

This spec covers **piece 1 only**. It is the substrate that piece 2 consumes: it
produces the host address, cloud user, and `ssh.Signer` that `host.NewClient`
needs.

### The rhwa-lab reference (what this reproduces)

`lib/aws.sh` performs, in order: `sts get-caller-identity`; resolve a Route53
hosted zone by walking up from the base domain; check the instance type is
offered in the region (+ advisory vCPU-quota warning); import the operator's SSH
public key as an EC2 keypair; create a security group in the default VPC opening
22/6443/443/80 to the caller's public IP; resolve the latest Fedora Cloud Base
AMI (owner `125523088429`); `run-instances` with `NestedVirtualization=enabled`
and a large gp3 root volume; wait `instance-running`; allocate + associate an
Elastic IP; add a **hairpin** SG rule opening 6443/443/80 to the instance's own
EIP (so the host can reach its cluster API during `agent wait-for`); Route53
UPSERT `api.<cluster>.<base>` and `*.apps.<cluster>.<base>` A records → EIP.
Teardown reverses it, driven by a `.state` file.

The host is **not disposable**: it hosts the cluster's libvirt VMs for the
cluster's entire lifetime. It is created fresh per cluster but long-lived.

## ocpctl conventions this subsystem follows

- **SDK:** `aws-sdk-go-v2` (`v1.41.x`). Clients built with
  `config.LoadDefaultConfig(ctx, config.WithRegion(region))` + `<svc>.NewFromConfig(cfg)`;
  the default credential chain (no `AssumeRole`), matching every existing AWS
  path (`internal/worker/preflight_aws.go:26-37`).
- **Testability:** narrow per-package interfaces covering exactly the SDK methods
  used, satisfied by the real client and by hand-written mocks in `_test.go`,
  with an injectable sleep — the `internal/aws/cleanup/vpc.go` model. `testify`
  for assertions. **No live AWS in unit tests.**
- **Tagging:** every taggable resource carries `types.ProvenanceTags(clusterID,
  clusterName, createdAt)` (`pkg/types/cluster.go:90-108`) merged with
  `ManagedBy=ocpctl`, `ClusterName=<name>`, and `Name=<name>`, so the existing
  janitor/orphan-detector (which keys off `ManagedBy` + `ClusterName`,
  `internal/janitor/orphan_detector.go`) already recognises this substrate.

## Decisions (settled during brainstorming)

- **Ephemeral, in-process keypair.** Generate a fresh ed25519 keypair per cluster
  in Go; `ImportKeyPair` the public half; the private half becomes the
  `ssh.Signer` returned to piece 2. The private key never touches disk or the
  operator's `~/.ssh`, and is deleted from EC2 on teardown. This is the one
  deliberate departure from rhwa-lab (which imports the operator's key), chosen
  for self-containment and to match piece 2's spec assumption.
- **Elastic IP, not the instance's ephemeral public IP** — Route53 A records need
  a stable address, and the host reaches its own cluster through those hostnames.
- **Tag-based teardown, not a state file.** Teardown discovers resources by tag
  (`ManagedBy=ocpctl` + `ClusterName`), the ocpctl-idiomatic improvement over
  rhwa-lab's `.state`; the same tags make leaked resources visible to the
  janitor.
- **Default VPC** is assumed (as rhwa-lab does); absence is a clear error.
- **Piece 1 is a pure library.** Wiring `Launch`/`Teardown` into the worker's
  create/destroy dispatch, and retiring the `rhwa-lab` shell-out, is **piece 4**.
  The branch keeps building and the existing baremetal path keeps working until
  that swap.

## Section 1 — Package & public API

New self-contained package `internal/baremetal/substrate` (sibling to
`internal/baremetal/host`). Package name `substrate` (avoids shadowing the SDK's
`aws` package).

```go
package substrate

// LaunchSpec is built by the caller (piece 4) from the profile's BareMetalConfig
// + the shared Region/BaseDomain blocks.
type LaunchSpec struct {
    ClusterID     string    // for provenance tags
    ClusterName   string    // resource Name/ClusterName tag; DNS + resource names
    Region        string
    BaseDomain    string
    ZoneID        string    // optional; resolved from BaseDomain when empty
    InstanceType  string    // BareMetalConfig.HostInstanceType; default "m8i.12xlarge"
    AMIOwner      string    // default "125523088429" (Fedora Project)
    FedoraRelease string    // default "44"
    AMIOverride   string    // optional explicit "ami-..."
    HostVolumeGB  int       // gp3 root volume; default 1000
    User          string    // cloud user; default "fedora"
    AllowCIDRs    []string  // ingress CIDRs; empty => detect caller public IP /32
    CreatedAt     time.Time // provenance tag value
}

// Substrate is what piece 2 consumes.
type Substrate struct {
    InstanceID string
    Addr       string     // "<eip>:22"
    User       string
    Signer     ssh.Signer // ephemeral private key
    EIP        string
    AllocID    string
    SGID       string
    KeyName    string
    ZoneID     string
    APIFQDN    string     // "api.<cluster>.<base>"
    AppsFQDN   string     // "*.apps.<cluster>.<base>"
}

func Launch(ctx context.Context, spec LaunchSpec) (*Substrate, error)
func Teardown(ctx context.Context, spec TeardownSpec) error

// Teardown discovers resources by tag but needs the base domain / zone to
// delete the Route53 records (records are not tag-discoverable). See Section 3.
type TeardownSpec struct {
    Region      string
    ClusterName string
    BaseDomain  string
    ZoneID      string // optional; resolved from BaseDomain when empty
}
```

The AWS clients are reached only through narrow interfaces so the whole package
is mock-testable:

```go
type ec2API interface {
    DescribeInstanceTypeOfferings(...) (...)
    DescribeVpcs(...) (...)
    CreateSecurityGroup(...) (...)
    AuthorizeSecurityGroupIngress(...) (...)
    DescribeSecurityGroups(...) (...)      // teardown discovery
    DeleteSecurityGroup(...) (...)
    ImportKeyPair(...) (...)
    DescribeKeyPairs(...) (...)            // teardown discovery
    DeleteKeyPair(...) (...)
    DescribeImages(...) (...)
    RunInstances(...) (...)
    DescribeInstances(...) (...)           // waiters + teardown discovery
    TerminateInstances(...) (...)
    AllocateAddress(...) (...)
    AssociateAddress(...) (...)
    DescribeAddresses(...) (...)
    ReleaseAddress(...) (...)
}
type route53API interface {
    ListHostedZonesByName(...) (...)
    ListResourceRecordSets(...) (...)      // teardown: fetch exact records to DELETE
    ChangeResourceRecordSets(...) (...)
}
type stsAPI interface { GetCallerIdentity(...) (...) }
```

Instance-running / instance-terminated waits use the SDK's
`ec2.NewInstanceRunningWaiter` / `NewInstanceTerminatedWaiter`, which are backed
by the `DescribeInstances` method already on `ec2API`.

## Section 2 — Launch steps

`Launch` runs rhwa-lab's order with ocpctl idioms:

1. **Clients + preflight.** Build ec2/route53/sts clients from the region.
   Preflight: `sts:GetCallerIdentity` (fail fast on bad creds); resolve the
   Route53 zone (use `ZoneID` if given, else `ListHostedZonesByName` walking up
   from `BaseDomain` to the closest parent zone); `DescribeInstanceTypeOfferings`
   to confirm the instance type is offered in the region. (The advisory vCPU
   quota check is out of scope — ocpctl already has `preflight_aws.go` for that
   class of check.)
2. **Ephemeral keypair.** `ed25519.GenerateKey`; marshal the public key to the
   OpenSSH authorized-keys format; `ImportKeyPair` (name `<cluster>-key`, tagged);
   build `ssh.Signer` from the private key via `ssh.NewSignerFromKey`.
3. **Security group.** `DescribeVpcs` for the default VPC (error if none);
   `CreateSecurityGroup` `<cluster>-sg` (tagged); `AuthorizeSecurityGroupIngress`
   for tcp 22/6443/443/80 from each `AllowCIDRs` entry, or from the detected
   caller public IP `/32` (HTTP GET `https://checkip.amazonaws.com`, short
   timeout; on failure fall back to `0.0.0.0/32`, which fails safe — opens
   nothing — rather than opening to the world).
4. **AMI.** `AMIOverride` if set, else `DescribeImages` (owner `AMIOwner`, name
   `Fedora-Cloud-Base-AmazonEC2*-<FedoraRelease>-*`, `architecture=x86_64`,
   `state=available`) and pick the newest `CreationDate`; read its
   `RootDeviceName` for the block-device mapping.
5. **Instance.** `RunInstances`: AMI, `InstanceType`, key, SG; **nested virt** via
   the run-instances CpuOptions nested-virtualization setting (rhwa-lab:
   `--cpu-options NestedVirtualization=enabled`; the implementer maps this to the
   corresponding `CpuOptionsRequest` field in the pinned SDK version); a gp3 root
   volume of `HostVolumeGB` on the AMI's `RootDeviceName` with
   `DeleteOnTermination=true`; `TagSpecifications` for the instance (provenance +
   ManagedBy + ClusterName + Name). Then `NewInstanceRunningWaiter.Wait`.
6. **Elastic IP.** `AllocateAddress` (domain vpc, tagged); `AssociateAddress` to
   the instance; read the public IP; add the **hairpin** ingress rule opening
   6443/443/80 to `<eip>/32`.
7. **DNS.** `ChangeResourceRecordSets` UPSERT: `api.<cluster>.<base>` and
   `*.apps.<cluster>.<base>` A records (TTL 60) → EIP.
8. Assemble and return `*Substrate`.

Piece 1 stops at "instance running, EIP associated, DNS live." **SSH-reachability
polling is piece 2's `Client.WaitReachable`** — no overlap. Errors wrap with the
step name (`fmt.Errorf("substrate keypair: %w", err)`).

## Section 3 — Teardown

`Teardown(ctx, region, clusterName)` is best-effort and idempotent, discovering
by tag (`tag:ManagedBy=ocpctl`, `tag:ClusterName=<name>`):

1. `DescribeInstances` (states pending/running/stopping/stopped). For each:
   `DescribeAddresses` (filter `instance-id`) to find associated EIPs; record
   their `AllocationId` and public IP.
2. **DNS delete** — resolve the zone from `spec.ZoneID`, else
   `ListHostedZonesByName` walking up from `<cluster>.<base>` (records are named
   `api.<cluster>.<base>` / `*.apps.<cluster>.<base>` and are not
   tag-discoverable, which is why `TeardownSpec` carries the base domain).
   `ListResourceRecordSets` to fetch the exact A records, then
   `ChangeResourceRecordSets` DELETE them.
3. `TerminateInstances`; `NewInstanceTerminatedWaiter.Wait`.
4. `ReleaseAddress` for each recorded allocation.
5. `DescribeSecurityGroups` by tag → `DeleteSecurityGroup` (retry with the
   injectable sleep — dependencies linger briefly after termination).
6. `DescribeKeyPairs` by tag → `DeleteKeyPair`.

Missing resources at any step are not errors (idempotent re-run).

## Section 4 — Testing

- **Launch, mock-backed** (`launch_test.go`): a `fakeEC2`/`fakeRoute53`/`fakeSTS`
  implementing the interfaces with canned outputs and a per-method call log.
  Assert: keypair generated + imported with a valid OpenSSH public key and the
  returned `Signer`'s public key matches; SG created with all four base ports +
  the hairpin EIP rule; AMI filter/owner and newest-by-date selection; instance
  tags include provenance + ManagedBy + ClusterName + Name and
  `NestedVirtualization=enabled` + gp3 `HostVolumeGB`; EIP associated; both DNS
  records UPSERTed to the EIP; assembled `Substrate` fields (Addr `<eip>:22`,
  User, FQDNs). Zone-walk resolution across a couple of base-domain shapes.
  Caller-IP fallback to `0.0.0.0/32` on detector failure.
- **Teardown, mock-backed** (`teardown_test.go`): tag discovery finds the
  instance; DNS records fetched + DELETEd; terminate → release → delete SG (with
  a simulated first-delete failure to exercise the retry) → delete keypair;
  every step tolerant of a not-found resource.
- **No live-host / live-AWS test** — a real E2E needs pieces 2 and 3 and belongs
  to the piece-4 integration milestone.

## Section 5 — Boundaries with the other pieces

- **Consumes:** a `LaunchSpec` built (in piece 4) from `BareMetalConfig` +
  the profile's Region/BaseDomain. Reads AWS creds from the default chain.
- **Exposes:** `Launch → *Substrate` carrying `Addr`, `User`, `Signer` (exactly
  what `host.NewClient(addr, user, signer, out)` needs), plus the resource IDs
  and `APIFQDN`/`AppsFQDN` for piece 3, and `Teardown(TeardownSpec)`.
- **Not in scope:** SSH reachability (piece 2's `WaitReachable`); host OS
  provisioning (piece 2); the agent ISO / boot / `agent wait-for` (piece 3);
  worker dispatch wiring + shell-out removal (piece 4); host-key pinning via
  `GetConsoleOutput` (hardening follow-up).

## Section 6 — Ripple change to profile types

`BareMetalConfig` (`internal/profile/types.go:168-179`) gains one field:

```go
HostVolumeGB int `yaml:"hostVolumeGB,omitempty"` // host root disk; default 1000
```

The host root disk holds every VM's qcow2 (masters + workers + spares), so it
must be large (rhwa-lab's `EC2_VOLUME_SIZE_GB=1000`). No other profile change is
needed — VIPs/sushy/CIDR already exist and are piece 2/3 concerns; region and
base domain come from the shared profile blocks.

## Out of scope / follow-ups

- EC2 host-key pinning via `GetConsoleOutput` (hardening; also noted in piece 2).
- Advisory vCPU-quota preflight (ocpctl's `preflight_aws.go` already covers this
  class; can be extended for baremetal in piece 4 if desired).
- Non-default-VPC support.
- Worker create/destroy dispatch wiring and removal of the `rhwa-lab` shell-out
  handlers (`installer/baremetal.go`, `handler_create_baremetal.go`,
  `handler_destroy_baremetal.go`) — piece 4.
</content>
</invoke>
