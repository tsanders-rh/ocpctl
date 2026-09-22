# Testing the native bare-metal / agent + ODF provider

How to stand up a test environment for the native bare-metal provider (pieces
1–5: `substrate` → `host` → `agent` → `metal3`/`rhwa`/`odf`, driven by
`lifecycle` from the `baremetal-rhwa-lab` profile) on a system where AWS
credentials are allowed to live. This exercises the real create/destroy path
end-to-end; everything below the worker is unit-tested with fakes, so this is the
first live validation.

## What actually runs where (why this is lighter than IPI)

- **The worker** only needs: AWS credentials (EC2/EIP/Route53), a Red Hat pull
  secret, a Postgres DB, and **outbound SSH (22) + HTTPS to AWS**. It does **not**
  need `openshift-install` or `oc` locally — those run on the EC2 host over the
  SSH tunnel. The heavy downloads (installer, agent ISO, ceph image, rook
  exporter) happen **on the EC2 host / ceph VM**, not on the worker.
- **The EC2 host** (m8i.12xlarge, Fedora Cloud) runs libvirt + sushy + haproxy +
  `openshift-install`, hosts the master/worker/spare VMs, and (for ODF) the ceph
  VM. It reaches the internet directly.
- **The API** must run at least once against the DB: it runs migrations **and**
  syncs profiles + addons into `post_config_addons`. The RHWA operator install
  reads the `rhwa` addon from that table, so **the create fails fast with "load
  rhwa addon operators" if the API never synced addons.**

## Prerequisites (on the credentialed system)

- Go 1.25, `git`, and this branch checked out.
- **AWS account** with permissions for EC2 / Elastic IP / Route53 and enough
  On-Demand vCPU quota (~48 for the 3 masters + host; more if you set
  `workers.replicas` > 0). Credentials via the standard chain (`~/.aws` or
  `AWS_*` env).
- A **Route53 hosted zone** matching the profile's base domain. The
  `baremetal-rhwa-lab` profile allows `migration.redhat.com`; if you don't own
  that zone, edit `baseDomains` in
  `internal/profile/definitions/baremetal-rhwa-lab.yaml` to a zone you control.
- A **Red Hat pull secret** (with registry entitlement for the RHWA/ODF
  operators) saved to a file, e.g. `~/pull-secret.json`.
- An **S3 bucket** you own for cluster artifacts (kubeconfig, logs).
- **Postgres** (a local container is fine).
- **Cost / time awareness:** with ODF enabled the host EBS root volume is sized
  `1000 + osdCount*osdDiskGB` GB (~**1.7 TB gp3** at the defaults) and the host is
  an `m8i.12xlarge`. A full create (install + workers + RHWA + ODF) takes roughly
  **60–90 min**. Destroy terminates everything.

## 1. Postgres

```bash
docker run -d --name ocpctl-pg -e POSTGRES_USER=ocpctl \
  -e POSTGRES_PASSWORD=ocpctl-dev-password -e POSTGRES_DB=ocpctl \
  -p 5432:5432 postgres:17
```

## 2. Environment

Copy `.env.example` to `.env` and set at least:

```bash
DATABASE_URL=postgres://ocpctl:ocpctl-dev-password@localhost:5432/ocpctl?sslmode=disable
AWS_REGION=us-east-1                      # a region in the profile's allowlist
S3_ARTIFACT_BUCKET=<your-s3-bucket>       # kubeconfig/kubeadmin/logs; a bucket your creds can write
PROFILES_DIR=internal/profile/definitions
ADDONS_DIR=internal/addon/definitions
OPENSHIFT_PULL_SECRET_FILE=/home/you/pull-secret.json
WORKER_WORK_DIR=/var/lib/ocpctl/clusters  # writable dir for per-cluster work
# AWS creds: rely on ~/.aws, or set AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY here
```

Export them (`set -a; . ./.env; set +a`) for the API and worker processes.

## 3. Bring up the API (migrates DB + syncs profiles/addons)

```bash
make build
./bin/ocpctl-api        # or: make run-api
```

On first start it runs migrations, syncs profiles, and syncs addons. Confirm:

```sql
-- against DATABASE_URL
select addon_id from post_config_addons where addon_id in ('rhwa','odf');
select name from profiles where name = 'baremetal-rhwa-lab';
```

`rhwa` **must** be present (the bare-metal create sources the RHWA operator set
from it). `odf` is *not* required for external mode (the operator is installed
natively), but the addon should still sync harmlessly.

## 4. Run the worker

In a second shell (same env):

```bash
make run-worker         # or ./bin/ocpctl-worker
```

It polls the job queue every ~10 s. Leave it running and watch its logs — the
bare-metal steps stream there and into the job's deployment log.

## 5. Trigger a create

Two options:

- **Web UI** (if you build/run `web/`): pick the *Bare Metal RHWA Lab* profile and
  create a cluster.
- **API**: log in and POST a create. Payload essentials: `profile:
  baremetal-rhwa-lab`, a `name`, a `region` from the allowlist, and `baseDomain`
  matching your Route53 zone. No SSH key is needed — ocpctl generates an ephemeral
  keypair and uses it for the host *and* the nodes/ceph VM. Confirm the exact
  request shape against `internal/api/handler_clusters.go` / the Swagger doc
  (`docs/swagger.yaml`).

The worker then runs `lifecycle.Create`: `substrate.Launch` (EC2 host, EIP,
Route53) → `host.Provision` (libvirt/sushy/haproxy + domains) → `agent.Install`
(ISO, boot, `wait-for`, kubeconfig) → `rhwa` (operators + `fence_redfish`) →
`metal3` (BMH + workers) → `odf` (ceph VM + external StorageCluster).

## 6. Watch / debug

- **Worker log**: the whole flow logs here (`journalctl`/stdout).
- **Deployment log**: streamed to the DB job log — the same live host/`oc` output.
  Local copy at `$WORKER_WORK_DIR/<cluster-id>/baremetal.log`.
- **AWS**: everything is tagged `ManagedBy=ocpctl` + `ClusterName=<name>`; find the
  host, SG, EIP, keypair, and Route53 records by those tags.
- **On the EC2 host** (ssh in as `fedora@<eip>` with the ephemeral key — grab it
  from the worker if you need it; SSH is only open to the IP that launched the
  host, so add your own CIDR via the profile's
  `platformConfig.baremetal.allowCIDRs` or edit the SG): `sudo virsh
  list --all`, `sudo virsh net-dhcp-leases rhwa`, and the installer log under
  `/opt/ocpctl-agent/work/.openshift_install.log`.
- **Cluster**: on the host, `/opt/ocpctl-agent/bin/oc
  --kubeconfig=/opt/ocpctl-agent/work/auth/kubeconfig get nodes/co`.
- **ODF**: `oc -n openshift-storage get storagecluster,pods`; `oc get sc`
  (expect `ocs-external-storagecluster-ceph-rbd`); the ceph VM is
  `<cluster>-ceph-0` at `192.168.126.10` (`ssh cloud-user@…` from the host).

## 7. First-run watch items

Ported logic that is most likely to need a nudge on first real hardware (search
the rhwa-lab reference for `# ITERATE`):

- **NIC name** `enp1s0` in the agent-config nmstate (matched by MAC as a hedge).
- **cdrom target dev** for the agent ISO (`sda` vs `hda`).
- **Nested-virt install speed** on `m8i.12xlarge` (bump instance type if flaky).
- **Operator channels**: now resilient — the ODF (and RHWA-catalog) channel is
  resolved from the PackageManifest (desired → catalog default → newest), so a
  pre-release catalog should no longer need a manual subscription edit.
- **Ceph release/image**: `squid` → `quay.io/ceph/ceph:v19`; the exporter URL
  pins rook `master`.
- **SG ingress**: the security group opens 22/6443/443/80 to the worker's detected
  public IP (`checkip.amazonaws.com`). If the worker is behind NAT such that its
  API egress IP differs from what it presents to the host, SSH can fail — that's
  the first thing to check on a `host not reachable` error.

## 8. Teardown

Destroy the cluster (UI or API). `lifecycle.Destroy` runs `substrate.Teardown`,
which terminates the host (taking the VMs/ceph/sushy with it) and removes the SG,
EIP, keypair, and Route53 records **by tag** — idempotent, and it runs even for a
create that failed partway. Afterwards, confirm nothing is left:

```bash
aws ec2 describe-instances --filters \
  Name=tag:ManagedBy,Values=ocpctl Name=tag:ClusterName,Values=<name> \
  --query 'Reservations[].Instances[].State.Name'
aws ec2 describe-addresses --filters Name=tag:ClusterName,Values=<name>
```

The janitor/orphan-detector also recognizes these tags, so anything a failed run
leaks is reclaimable.
