# Ownership & Handover

How to take over the **dev** and **production** ocpctl deployments. Written for the
case where the current maintainer leaves and the team keeps the environments.

Companion to [DEVOPS.md](../development/DEVOPS.md) (how code ships) and
[TROUBLESHOOTING.md](TROUBLESHOOTING.md) (when it breaks). This document covers the
part neither of those does: **the things that are not in git**, and the things that
are attached to a *person* rather than to the team.

> **No secrets in this file.** It says what exists, where it lives, and who owns it.
> The values themselves are in the handover bundle (§3) and in the environments.

---

## 1. Owner of record

Keep this table current. An environment with no named owner is an environment
nobody will notice is broken.

| Area | Owner | Backup |
|------|-------|--------|
| Production deployment | _TBD_ | _TBD_ |
| Dev deployment | _TBD_ | _TBD_ |
| AWS account `346869059911` | _TBD_ | _TBD_ |
| DNS / TLS (`dog8code.com` registrar) | _TBD_ | _TBD_ |
| registry.ci pull-secret refresh (28-day chore) | _TBD_ | _TBD_ |
| GitHub repo `tsanders-rh/ocpctl` | _TBD_ | _TBD_ |

---

## 2. What is *not* in git

Three categories, in descending order of "how badly does losing this hurt".

### 2a. Terraform state — now shared, was laptop-only

State lives in a **team-owned remote backend**, configured in the `terraform` block
of each root module:

| Module | State key |
|--------|-----------|
| `terraform/dev` | `s3://ocpctl-tfstate-346869059911/dev/terraform.tfstate` |
| `terraform/worker-autoscaling` | `s3://ocpctl-tfstate-346869059911/worker-autoscaling/terraform.tfstate` |

Bucket is versioned and encrypted; locking is via the `ocpctl-tf-locks` DynamoDB
table. `terraform init` in either directory picks this up with no extra flags.

Why this matters more than it looks:

- `terraform/worker-autoscaling` owns the **worker launch template**, which per
  [DEVOPS.md §4](../development/DEVOPS.md) is the *only* supported way to change how
  autoscale workers boot. No state, no worker fixes.
- `terraform/dev` state contains `tls_private_key.dev_key` — the **dev SSH key is
  recoverable only from state**:
  ```bash
  terraform -chdir=terraform/dev output -raw ssh_private_key > ~/.ssh/ocpctl-dev-key
  chmod 600 ~/.ssh/ocpctl-dev-key
  ```

If you find a `terraform.tfstate` file sitting in either module directory, someone
is running against a local backend. Re-run `terraform init -migrate-state` and
delete the stray file — the S3 copy is authoritative.

### 2b. Secret files — in the handover bundle

Collected by [`scripts/handover-bundle.sh`](../../scripts/handover-bundle.sh) into
`s3://ocpctl-handover-346869059911`, encrypted with KMS key
`alias/ocpctl-handover`:

| File | Carries |
|------|---------|
| `config/worker.env.production`, `config/worker.env.dev`, `config/worker.env` | `DATABASE_URL`, OpenShift pull secret, AWS/Azure/GCP/IBM credentials, `OCM_TOKEN` |
| `config/api.env.production`, `config/api.env.dev` | `DATABASE_URL`, `JWT_SECRET` |
| `.env` | local-development API config |
| `terraform/dev/terraform.tfvars` | `db_password` for the dev RDS instance |
| `terraform/worker-autoscaling/terraform.tfvars` | `database_url`, OpenShift pull secret |
| `~/.ssh/ocpctl-production-key`, `~/.ssh/ocpctl-dev-key` | host access |

The bundle does **not** go in `s3://ocpctl-binaries`, even though `worker.env`
already lives there: every worker instance role can read the binaries bucket, and
this set is broader than what a worker needs.

Note the overlap — `s3://<binaries-bucket>/config/worker.env` remains the runtime
source of truth that autoscale workers pull at boot ([DEVOPS.md §7](../development/DEVOPS.md)).
The bundle is the *maintainer's* copy. Change worker credentials in both, or
workers will boot with the old ones.

### 2c. Local context file

`CLAUDE.local.md` resolves the `<PLACEHOLDER>` tokens in `CLAUDE.md` to real hosts
and keys. Gitignored by design so the public repo stays generic. It is in the
bundle; the same values are in the tracked, non-secret
[`config/environments.sh`](../../config/environments.sh).

---

## 3. Day one for a new maintainer

```bash
# 1. AWS credentials for account 346869059911, then:
git clone https://github.com/tsanders-rh/ocpctl && cd ocpctl

# 2. Pull every untracked file (env configs, tfvars, SSH keys, CLAUDE.local.md)
./scripts/handover-bundle.sh pull

# 3. Confirm you have the full set
./scripts/handover-bundle.sh verify

# 4. Attach to shared Terraform state (no -migrate-state; the backend is in the repo)
terraform -chdir=terraform/dev init
terraform -chdir=terraform/worker-autoscaling init

# 5. Prove access end to end
curl -s https://ocpctl.<BASE_DOMAIN>/version
ssh -i ~/.ssh/<PROD_SSH_KEY> ubuntu@<PROD_HOST> 'sudo systemctl status ocpctl-api ocpctl-worker'
```

Then read [DEVOPS.md](../development/DEVOPS.md) before deploying anything. The
golden rule there holds: dev first, production is a promotion of a dev-validated
version.

---

## 4. Identities and external dependencies

These are the failure modes that a file copy does **not** fix. Each one keeps
working right up until the person or account behind it is deprovisioned.

### 4a. Worker AWS credentials are a personal IAM user — must be re-issued

`AWS_ACCESS_KEY_ID=AKIAVBQ…VRDR` in **both** `config/worker.env.production` and
`config/worker.env.dev` belongs to the IAM user `tsanders@redhat.com`.

Every worker — including every ASG instance that pulls `worker.env` from S3 at boot
— provisions clusters with that identity. When the user is deprovisioned, cluster
creation stops in both environments, with cloud-auth errors rather than an obvious
"credentials expired" message.

**Fix before offboarding:** create a service IAM user (or, better, lean on the
worker instance role that already exists) with the permissions in
[AWS_IAM_PERMISSIONS.md](AWS_IAM_PERMISSIONS.md), then update:
1. `config/worker.env.{dev,production}` locally,
2. `s3://ocpctl-binaries/config/worker.env` and the dev equivalent — **the runtime
   source of truth**,
3. recycle autoscale workers so they pick it up (`deploy.sh` does this),
4. re-push the bundle.

### 4b. DNS root is outside the AWS account

`mg.dog8code.com` is a Route53 hosted zone in account `346869059911`, so it
transfers with the account. But the parent domain **`dog8code.com` is registered at
GoDaddy** (`ns43/ns44.domaincontrol.com`) and is *not* in this AWS account or in
Route53 Domains. The NS delegation from GoDaddy to Route53 is what makes both
`ocpctl.<BASE_DOMAIN>` and `dev.ocpctl.<BASE_DOMAIN>` resolve, and what lets
Let's Encrypt renew.

Whoever holds that registrar login holds the keys to both environments' URLs.
Either transfer the registration to a team-held account, or move ocpctl onto a
domain the team already owns. Until then this is a single point of failure that no
amount of AWS access compensates for.

### 4c. Tokens with their own expiry clocks

| Credential | Where | Notes |
|-----------|-------|-------|
| registry.ci pull secret | `worker.env` (both envs) + S3 | **~28-day lifetime, manual refresh.** Runbook: [CI_PULL_SECRET_REFRESH.md](CI_PULL_SECRET_REFRESH.md); helper: `scripts/refresh-ci-pull-secret.sh`. Expiry is only visible on app.ci. Nightly/prerelease installs fail when it lapses. |
| `OCM_TOKEN` | `worker.env` | Red Hat account token, used for ROSA/OCM paths. |
| Azure service principal | `worker.env` (`AZURE_CLIENT_*`) | Written to `osServicePrincipal.json` by the `azure-login.sh` ExecStartPre hook. |
| IBM Cloud API key | `worker.env` (`IC_API_KEY`) | |
| GCP service account | `GOOGLE_APPLICATION_CREDENTIALS` → `/opt/ocpctl/gcp-credentials.json` on each host | Not in the repo; present on the hosts and in S3 config. |
| `JWT_SECRET` | `api.env.*` | Rotating it logs every user out. |

---

## 5. Recurring chores

| Cadence | Task |
|---------|------|
| ~Every 28 days | Refresh the registry.ci pull secret ([runbook](CI_PULL_SECRET_REFRESH.md)). Set a calendar reminder — nothing alerts on this. |
| Weekly | Check orphaned-resource counts and the janitor's auto-remediation mode. Dev must stay `dryrun` (see §6). |
| Per deploy | `./scripts/deploy-env.sh dev`, validate, then promote to production. |
| Quarterly | Re-push the handover bundle so it does not drift from the running config. |

---

## 6. Known state at handover (2026-09-19)

Verified against AWS at the time of writing. A new owner should not discover these
the hard way.

**Dev is currently down.** The dev EC2 instance recorded in Terraform state
(`i-0d5b343fb88ad0160`) no longer exists, and its Elastic IP `44.214.230.178` has
been released. `dev.ocpctl.<BASE_DOMAIN>` is a dangling A record pointing at an
address the account no longer holds, and the URL does not respond. The **dev RDS
instance `ocpctl-dev-db` is still running and still billing.** Production is
unaffected and healthy (`/version` returns 200).

Recovery is a `terraform apply` in `terraform/dev` (the plan shows the instance and
EIP as "to be created"), followed by `scripts/bootstrap-dev-server.sh` and a
`deploy-env.sh dev`. Note that this yields a **new IP**, which must be written back
to `config/environments.sh` and `CLAUDE.local.md`. Decide deliberately whether dev
is worth rebuilding before applying — if it is not, destroy the RDS instance rather
than paying for an orphan.

**Pre-existing Terraform drift.** `terraform plan` in `terraform/worker-autoscaling`
shows one in-place update to `aws_autoscaling_group.worker` (a `ManagedBy`
propagating tag). Unrelated to the state migration; apply it when convenient.

**~105 unassociated Elastic IPs** in the account, each billing hourly. Consistent
with the orphaned-resource backlog the janitor tracks; worth a cleanup pass.

**Dev and production share AWS account `346869059911`.** This is why the two
environments report wildly different orphan counts, and why dev's auto-remediation
must never be set to `on` — it would delete production's live infrastructure. The
`ORPHAN_AUTO_DELETE_ALLOW_ON` interlock exists for exactly this and is intentionally
unset. See CLAUDE.md → Recent Changes.

---

## 7. Offboarding checklist for the departing owner

- [ ] Fill in §1 with real names.
- [ ] Re-issue the worker AWS credentials off the personal IAM user (§4a) and
      confirm a cluster create still succeeds afterward.
- [ ] Transfer or document the `dog8code.com` registrar account (§4b).
- [ ] Grant the new owners GitHub admin on `tsanders-rh/ocpctl`; consider moving the
      repo to a team-owned org.
- [ ] Confirm the new owners can read `s3://ocpctl-handover-346869059911` and use
      KMS key `alias/ocpctl-handover`.
- [ ] Walk one new owner through §3 on their own machine, end to end. A handover is
      not done until someone else has deployed to dev without you.
- [ ] Hand off the registry.ci refresh chore with a calendar invite, not a mention.
- [ ] Re-push the bundle after any of the above changes a value.
