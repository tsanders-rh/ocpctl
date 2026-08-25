# DevOps & Deployment

How code becomes a running version in **dev** and **production**, plus migrations,
rollback, and secrets. Companion to [CONTRIBUTING.md](CONTRIBUTING.md) (how code
gets into `main`) and [NIGHTLY_PIPELINE.md](NIGHTLY_PIPELINE.md) (automated dev
validation).

---

## 1. Environments

| | Dev | Production |
|--|-----|-----------|
| API/worker host | `44.214.230.178` (`ubuntu`) | `44.201.165.78` (`ubuntu`) |
| URL | https://dev.ocpctl.mg.dog8code.com | https://ocpctl.mg.dog8code.com |
| DB | `ocpctl-dev-db` (RDS, own) | `ocpctl-db` (RDS) |
| Binaries bucket | `s3://ocpctl-dev-binaries` | `s3://ocpctl-binaries` |
| Artifacts bucket | `s3://ocpctl-dev-artifacts` | `s3://ocpctl-artifacts` |
| Autoscale workers | single node | `ocpctl-worker-asg` (ASG) |
| SSH key | `~/.ssh/ocpctl-dev-key` | `~/.ssh/ocpctl-production-key` |

**Golden rule:** changes prove out in **dev** before production. Production is a
manual promotion of a specific, dev-validated version — never a first deploy.

These per-environment targets are **not duplicated** in each script — they live in
one tracked, non-secret file, [`config/environments.sh`](../../config/environments.sh).
The deploy/ops scripts source it and call `load_environment dev|production` to get
`$API_HOST`, `$WORKER_HOSTS`, `$SSH_KEY`, `$SSH_USER`, `$S3_BUCKET`,
`$S3_ARTIFACTS_BUCKET`, `$DOMAIN`, `$AUTOSCALE_TAG`, `$CONFIG_SUFFIX`, and
`$RDS_HOST`. Change a host/bucket/domain in that one file, not in the scripts.

---

## 1a. What you need to deploy

`config/environments.sh` holds only the **non-secret targets** (hosts, buckets,
domain, key *names*). To actually push to dev or prod, a maintainer additionally
needs the following on their own machine — none of it is in git:

1. **The SSH private key** at `$SSH_KEY` — `~/.ssh/ocpctl-dev-key` (dev) or
   `~/.ssh/ocpctl-production-key` (prod). Regenerate from Terraform if missing:
   `terraform -chdir=terraform/dev output -raw ssh_private_key > ~/.ssh/ocpctl-dev-key && chmod 600 ~/.ssh/ocpctl-dev-key`.
   Different user/path? Override with `OCPCTL_SSH_USER` / `OCPCTL_SSH_KEY` — no
   need to edit the config file.
2. **The real env-config secret files** — `config/api.env.<env>` and
   `config/worker.env.<env>` (gitignored; they carry `DATABASE_URL`, `JWT_SECRET`,
   `OCM_TOKEN`, the OpenShift pull secret, and cloud creds). Only the
   `*.template` versions are tracked; copy and fill them, or pull the real ones
   from a teammate / `s3://<binaries-bucket>/config/`.
3. **AWS credentials** (`aws configure` / SSO) with access to the S3 binaries +
   artifacts buckets and the worker ASG, in the account that hosts the
   deployment.
4. **Local tooling**: Go (build + `go test` gate), `aws` CLI, `jq`, `ssh`/`scp`,
   and Node.js 18+ for `scripts/deploy-web.sh`.

With those in place, `./scripts/deploy-env.sh dev` (or `production`) is
self-contained.

---

## 2. Versioning

```
v0.YYYYMMDD.<short-commit>      e.g. v0.20260819.6c97dca
```

Computed by the deploy scripts from git (`git describe` tag, else date + short
SHA) and baked into the binaries via `-ldflags` (`main.Version`, `main.Commit`,
`main.BuildTime`, `main.Environment`). Verify a running version at
`/version` (API `:8080`, worker `:8081`).

---

## 3. Build is unit-test-gated

`scripts/deploy.sh` and `scripts/deploy-env.sh` **run `go test -race ./...`
before building** and abort the deploy if any test fails. You cannot ship a
binary that doesn't pass its unit tests.

```
Building binaries with version metadata...
Running unit tests (go test -race ./...)...
✓ Unit tests passed
✓ Built ocpctl-api-v0.YYYYMMDD.xxxx
```

<a id="emergency-override"></a>
### Emergency override

`SKIP_TESTS=1 ./scripts/deploy-env.sh production <version>` skips the gate. This
is **shipping untested code on purpose** — reserve it for a genuine incident
where tests can't run, and record the decision in the incident/PR notes. Default
behavior always runs tests.

---

## 4. Deploying

### Dev (do this first, always)

```bash
./scripts/deploy-env.sh dev                 # build+test, deploy latest to dev
./scripts/deploy-env.sh dev v0.20260819.6c97dca   # deploy a specific version
```

### Production (deliberate promotion)

```bash
./scripts/deploy-env.sh production v0.20260819.6c97dca
# or the production-only script:
./scripts/deploy.sh v0.20260819.6c97dca
```

**What a deploy does** (see CLAUDE.md → Deployment Process for detail):
1. Run unit tests (gate) → build linux binaries with version metadata.
2. Upload versioned + `binaries/` (stable) to S3; update `LATEST`.
3. Sync profiles, addons, manifests, scripts to S3.
4. **Terminate running autoscale workers** so the ASG relaunches them fresh from
   S3 (prod only) — no manual termination needed.
5. Deploy to the API/worker host(s): copy binary → flip `current` symlink →
   `systemctl restart`; requeue RUNNING jobs, clear stale locks.
6. Verify `/version` on API and worker.

### Autoscale workers (production)

New ASG instances boot from the **Terraform launch-template user-data**
(`terraform/worker-autoscaling/user-data.sh`), pulling binary/profiles/hook
scripts/`worker.env` fresh from S3 each boot and regenerating the systemd unit
inline (no drift).

- Fixes to **hook scripts** (`azure-login.sh`, `ibmcloud-login.sh`,
  `ensure-installers.sh`): edit in repo → `deploy.sh` uploads to S3 → next worker
  boot self-heals. `deploy.sh` already recycles workers.
- Fixes to the **systemd unit or boot steps** (e.g. runtime dirs, `ExecStartPre`):
  edit `user-data.sh` → `terraform apply` in `terraform/worker-autoscaling/` (cuts
  a new LT version; ASG tracks `$Latest`). **`deploy.sh` does NOT apply Terraform.**

```bash
cd terraform/worker-autoscaling
terraform plan          # confirm only launch_template user_data changes
terraform apply
# then let the ASG relaunch, or terminate the current worker to validate on fresh boot
```

> The legacy `scripts/bootstrap-worker.sh` / `scripts/user-data-worker.sh` are the
> old manual-AMI path and are **not** used by the current ASG. Don't put worker
> boot fixes there.

---

## 5. Database migrations

Migrations live in `internal/store/migrations/` (`NNNNN_name.sql`, goose format).

```bash
DATABASE_URL=... make migrate-up      # apply
DATABASE_URL=... make migrate-down    # roll back one
```

Rules:
- **Additive and backward-compatible** — a new binary and the previous one may run
  against the same schema briefly during a rollout.
- **Reversible** — every migration has a working `-- +goose Down`.
- Apply migrations to **dev first**, exercise the app, then production.
- Never edit a migration that has already run in any environment; add a new one.

---

## 6. Rollback

```bash
# List available versions
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78 'sudo ls -d /opt/ocpctl/releases/*'

# Redeploy a previous version
./scripts/deploy-env.sh production v0.20260413.1346b69
```

If a bad migration is involved, roll the schema back (`migrate-down`) as part of
the rollback, in the reverse order it was applied.

---

## 7. Secrets

- **Single source of truth:** `s3://ocpctl-binaries/config/worker.env` (prod) /
  `ocpctl-dev-binaries` (dev) — DATABASE_URL, pull secret, all cloud credentials
  (incl. the Azure service principal). Autoscale workers pull it at boot, so
  **secrets never land in the launch template or tfstate.**
- Local env config: `config/{api,worker,web}.env.{dev,production}` (git-ignored;
  templates checked in as `*.template`).
- **Never** commit secrets or paste them into logs/PRs. The pre-commit security
  check will flag obvious leaks, but it is not a substitute for care.
- CI/nightly secrets live in GitHub Actions repo secrets (see NIGHTLY_PIPELINE.md).

---

## 8. Quick health checks

```bash
# Service status / logs (dev shown; prod is the same minus -web)
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@44.214.230.178 \
  'sudo systemctl status ocpctl-api ocpctl-worker ocpctl-web'
ssh -i ~/.ssh/ocpctl-dev-key ubuntu@44.214.230.178 'sudo journalctl -u ocpctl-worker -f'

# Versions
curl -s https://dev.ocpctl.mg.dog8code.com/version
```

See CLAUDE.md → Troubleshooting for stuck-job, profile-loading, and
cluster-create debugging.
