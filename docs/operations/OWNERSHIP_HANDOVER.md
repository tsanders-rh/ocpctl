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
| registry.ci pull secret | `worker.env` (both envs) + S3 | **Expires 2026-10-17 16:19:12Z — see the warning below.** 28-day lifetime, manual refresh. Runbook: [CI_PULL_SECRET_REFRESH.md](CI_PULL_SECRET_REFRESH.md); helpers: `scripts/check-ci-pull-secret-expiry.sh` (reports days remaining), `scripts/refresh-ci-pull-secret.sh`. Nightly/prerelease installs fail when it lapses. |
| `OCM_TOKEN` | `worker.env` | Red Hat account token, used for ROSA/OCM paths. |
| Azure service principal | `worker.env` (`AZURE_CLIENT_*`) | Written to `osServicePrincipal.json` by the `azure-login.sh` ExecStartPre hook. |
| IBM Cloud API key | `worker.env` (`IC_API_KEY`) | |
| GCP service account | `GOOGLE_APPLICATION_CREDENTIALS` → `/opt/ocpctl/gcp-credentials.json` on each host | Not in the repo; present on the hosts and in S3 config. |
| `JWT_SECRET` | `api.env.*` | Rotating it logs every user out. |

> **The registry.ci token expires 2026-10-17 and cannot be renewed by the team as
> things stand.** It is a personal OAuth token minted from the *departing
> maintainer's* interactive app.ci login — there is no scriptable refresh and no
> machine identity behind it. Once that account is gone, nobody can re-mint it.
>
> It was refreshed on 2026-09-19 (the previous one was four days from lapsing) and
> deployed to both environments, so there is a **28-day runway from that date**.
> That is the whole window, and only one thing closes it:
>
> **The successor must mint their own token from their own app.ci account**, which
> first requires the `qci-image-puller` gate to be cleared for them. That is a
> request to the CI/DPTP team and is *not* instant — start it now, not when the
> token lapses. If the maintainer's account is still live near 2026-10-17 they can
> buy another 28 days, but that is a stopgap, not the fix.
>
> Check the deployed token's remaining life any time with
> `scripts/check-ci-pull-secret-expiry.sh` (exits non-zero inside the warning
> window, so it can be wired to alerting). Only nightly/prerelease installs depend
> on this; GA and EC installs are unaffected.

---

### 4d. The team's `aws-reporting` reaper will delete these hosts

This is not an ocpctl component and it is not under this repo's control, but it
**terminated the dev server on 2026-09-19** and it will do so again. Anyone owning
ocpctl has to know how it works.

It is a Lambda named `aws-reporting` in account `346869059911` (source:
`github.com/fusor/misc-env-scripts`), driven by EventBridge:

| Rule | Schedule (UTC) | Command | Effect |
|------|----------------|---------|--------|
| `aws_report_generation_schedule` | daily 14:00 | `report` | Rewrites the `EC2-Old-Instances` tab with every instance older than **30 days**. |
| `aws_ec2_deletion_summary_schedule` | Mon 14:15 | `generate_ec2_deletion_summary` | Warning email to the team list. |
| `aws_purge_instance_schedule` | **Sat 00:00** | `purge_instances` | **Terminates** every instance on that tab older than **34 days** whose `Saved` cell does not contain `save`. |

The one and only exemption is a spreadsheet cell. In the
[Mig Eng AWS Report](https://docs.google.com/spreadsheets/d/1XOMu12uPJgtX_gN3mUTQu89kArzBft4edhbkXqlae5M/edit)
sheet, tab `EC2-Old-Instances`, the row for the instance must have `Saved` set to
`Save`. That is why production survives — `i-033657f517e3be9c4` / `ocpctl-production`
is marked `Save`. The old dev instance was not, so it was terminated. There is no
tag-based, name-based, or account-based exemption in the code.

**You cannot pre-mark a young instance.** The daily `report` run clears the tab and
rewrites it from instances that are *currently* older than 30 days, carrying the
`Saved` value forward only for rows it re-emits. A row added early is wiped by the
next daily run.

So for the rebuilt dev instance `i-0d7d3ef3ee0477078` (launched 2026-09-19) the
action window is:

- **~2026-10-20** — it first appears on `EC2-Old-Instances` (age > 30 days).
- **Mark `Saved` = `Save` during the following days.**
- **2026-10-24 00:00 UTC** — first Saturday purge at which it is older than 34 days.
  If it is unmarked then, it is terminated.

Treat that as a hard deadline, and re-check after any dev rebuild (a rebuild means a
new instance ID, so the mark does not carry over). The durable fix is to change the
reaper to honor an instance tag instead of a spreadsheet cell, which requires a PR
to `misc-env-scripts` and that team's agreement — worth raising, because every team
using this account has the same failure mode.

**Second defense: EC2 termination protection.** The reaper's `terminate_instance`
wraps the API call in a `try`/`except` and only logs failures, so termination
protection blocks it without disrupting the rest of the purge for other teams. Both
static hosts now have `DisableApiTermination = true` — production
(`i-033657f517e3be9c4`), which always did, and dev (`i-0d7d3ef3ee0477078`), enabled
2026-09-19 after the reaper destroyed its predecessor.

Dev's is codified as `disable_api_termination = true` on `aws_instance.dev_server`
in `terraform/dev/main.tf`, so a rebuild keeps it. Production's is set out-of-band
and is **not** in Terraform — if production is ever rebuilt from config, it comes
back unprotected.

This makes the spreadsheet mark a backstop rather than the only thing standing
between dev and deletion, but **do both** — protection stops the terminate call,
while the mark keeps the instance off the deletion list and out of the weekly
warning email.

> **To intentionally destroy either host**, clear the flag first
> (`aws ec2 modify-instance-attribute --instance-id <id> --no-disable-api-termination`,
> or set the Terraform attribute to `false` and apply); otherwise the destroy fails
> with `OperationNotPermitted`.

> Unrelated but noticed while reading the Lambda: its configuration holds SMTP
> credentials (`SMTP_USERNAME`/`SMTP_PASSWORD`, an IAM access key) in **plaintext
> environment variables**, readable by anyone with `lambda:GetFunctionConfiguration`.
> Worth reporting to whoever owns that function.

---

## 5. Recurring chores

| Cadence | Task |
|---------|------|
| ~Every 28 days — **next due 2026-10-17** | Refresh the registry.ci pull secret ([runbook](CI_PULL_SECRET_REFRESH.md)). Run `scripts/check-ci-pull-secret-expiry.sh` to see days remaining; nothing alerts on this yet. |
| Weekly | Check orphaned-resource counts and the janitor's auto-remediation mode. Dev must stay `dryrun` (see §6). |
| Per deploy | `./scripts/deploy-env.sh dev`, validate, then promote to production. |
| Quarterly | Re-push the handover bundle so it does not drift from the running config. |
| After any EC2 rebuild, and by **2026-10-24** for the current dev box | Mark the instance `Save` in the reaper's spreadsheet or it gets terminated (§4d). |

---

## 6. Known state at handover (2026-09-19)

Verified against AWS at the time of writing. A new owner should not discover these
the hard way.

**Dev was destroyed and has been rebuilt.** The previous dev instance
(`i-0d5b343fb88ad0160`) was terminated by the team's `aws-reporting` reaper on
2026-09-19 (see §4d — this *will* recur). It has been rebuilt via `terraform apply`
in `terraform/dev`; the RDS instance was never touched, so no data was lost. The new
instance is `i-0d7d3ef3ee0477078` at **`3.229.198.9`** (a new IP, already written
back to `config/environments.sh` and `CLAUDE.local.md`). API, worker, and web are
all running `v0.20260919.07ae79e`, all four services are enabled at boot, and
https://dev.ocpctl.<BASE_DOMAIN> serves the UI and authenticates.

Rebuilding from scratch exposed a set of bugs that had been fixed by hand on the old
box and never committed, so the scripts could not actually reproduce a working host.
Those are fixed now (release symlink, `/var/log/ocpctl`, worker ExecStartPre hooks,
`~/.azure` ownership, Node.js install, nginx frontend routing, web unit and
`web.env` install, boot enablement). **If you rebuild dev again, the scripted path
should work end to end** — but re-verify rather than assume.

One known gap remains on the live dev box: Let's Encrypt renewal is still configured
with `authenticator = standalone` while nginx holds port 80, so renewal will fail.
The certificate is valid until 2026-12-18, so this is not urgent, but fix it before
then. `scripts/bootstrap-dev-server.sh` already does the right thing for a fresh
host; the live box needs the same edit to
`/etc/letsencrypt/renewal/dev.ocpctl.<BASE_DOMAIN>.conf`.

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
- [x] **Refresh the registry.ci pull secret before your app.ci account goes away** —
      done 2026-09-19, now expires 2026-10-17.
- [ ] **Open the `qci-image-puller` request for your successor** so they can mint
      their own token before 2026-10-17 (§4c). This has lead time; it is the only
      thing that makes nightly installs survive your departure.
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
