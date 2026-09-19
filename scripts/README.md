# Deployment Scripts

Index of the scripts that ship code to **dev** and **production**.

The full procedure — prerequisites, the test gate, migrations, rollback — is in
[docs/development/DEVOPS.md](../docs/development/DEVOPS.md). This file is the
map of which script does what. If you are taking over the deployment, start at
[docs/operations/OWNERSHIP_HANDOVER.md](../docs/operations/OWNERSHIP_HANDOVER.md).

> **Targets are centralized.** Hosts, buckets, domain and SSH key paths live in
> [`config/environments.sh`](../config/environments.sh), which every script below
> sources via `load_environment dev|production`. Change a host there — never by
> editing a script.

---

## The scripts

| Script | What it deploys | Usage |
|--------|-----------------|-------|
| `deploy-env.sh` | Go binaries (API + worker) to either environment | `./scripts/deploy-env.sh dev\|production [version]` |
| `deploy.sh` | Same, production only | `./scripts/deploy.sh [version]` |
| `deploy-web.sh` | Next.js frontend | `./scripts/deploy-web.sh dev\|production` |

**Backend and frontend are separate deploys.** `deploy-env.sh` and `deploy.sh`
build and ship only the Go binaries — they do not touch the web tier. A UI change
is not live until you also run `deploy-web.sh`.

```bash
# Full deploy of both tiers
./scripts/deploy-env.sh dev && ./scripts/deploy-web.sh dev
```

> ⚠️ **`deploy-web.sh` defaults to `production` when given no argument.** Always
> pass the environment explicitly.

Worker boot behavior (systemd unit, directory layout, ExecStartPre hooks) is **not**
deployed by these scripts — it lives in the Terraform launch template. See
[DEVOPS.md §4](../docs/development/DEVOPS.md).

---

## Typical flow

```bash
# 1. Dev first, always. Runs go test -race as a gate, then builds and deploys.
./scripts/deploy-env.sh dev
./scripts/deploy-web.sh dev          # only if the frontend changed

# 2. Validate against https://dev.ocpctl.<BASE_DOMAIN>

# 3. Promote the exact version you validated
./scripts/deploy-env.sh production v0.20260919.abc1234
./scripts/deploy-web.sh production
```

Rollback is a redeploy of an older version; see
[DEVOPS.md §6](../docs/development/DEVOPS.md).

---

## Prerequisites

You need the SSH private key for the environment, the real (gitignored)
`config/{api,worker}.env.<env>` files, AWS credentials, Go, and Node.js 18+.
`./scripts/handover-bundle.sh pull` fetches the untracked pieces. Full list:
[DEVOPS.md §1a](../docs/development/DEVOPS.md).

---

## Web deployment details

`deploy-web.sh` builds **locally** (`npm install`, lint, `npm run build`),
packages the result, ships it, and runs only `npm install --production` on the
host — so the build cannot be reproduced in place on the server.

It deploys to `/opt/ocpctl/web`, owned by the `ocpctl` user, served by the
`ocpctl-web` systemd unit, on the same host as the API.

**Backups.** The previous `.next` is kept as
`/opt/ocpctl/web/.next.backup-YYYYMMDD-HHMMSS`. To restore:

```bash
source config/environments.sh && load_environment production
ssh -i "$SSH_KEY" "$SSH_USER@$API_HOST" \
  'sudo rm -rf /opt/ocpctl/web/.next && \
   sudo mv /opt/ocpctl/web/.next.backup-YYYYMMDD-HHMMSS /opt/ocpctl/web/.next && \
   sudo systemctl restart ocpctl-web'
```

**Check what is deployed:**

```bash
source config/environments.sh && load_environment production
ssh -i "$SSH_KEY" "$SSH_USER@$API_HOST" \
  'ls -lh /opt/ocpctl/web/.next/BUILD_ID && sudo systemctl status ocpctl-web'
```

---

## Other operational scripts

| Script | Purpose |
|--------|---------|
| `handover-bundle.sh` | Push/pull the KMS-encrypted bundle of untracked secrets and keys |
| `check-ci-pull-secret-expiry.sh` | Days remaining on the registry.ci pull secret (28-day lifetime) |
| `refresh-ci-pull-secret.sh` | Rotate it — see [CI_PULL_SECRET_REFRESH.md](../docs/operations/CI_PULL_SECRET_REFRESH.md) |
| `azure-login.sh`, `ibmcloud-login.sh` | Worker `ExecStartPre` credential hooks, pulled fresh from S3 on each ASG boot |
| `ensure-installers.sh` | Installs `openshift-install`, `oc`, `az`, `eksctl`, `gcloud`, `ibmcloud` on workers |

> `bootstrap-worker.sh` and `user-data-worker.sh` are the **legacy manual-AMI**
> worker path and are not used by the Terraform-managed ASG. Edits there reach
> nothing in production.
