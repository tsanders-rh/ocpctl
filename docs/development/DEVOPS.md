# DevOps & Deployment

How code becomes a running version in **dev** and **production**, plus migrations,
rollback, and secrets. Companion to [CONTRIBUTING.md](CONTRIBUTING.md) (how code
gets into `main`) and [NIGHTLY_PIPELINE.md](NIGHTLY_PIPELINE.md) (automated dev
validation).

---

## 1. Environments

| | Dev | Production |
|--|-----|-----------|
| API/worker host | `<DEV_HOST>` (`ubuntu`) | `<PROD_HOST>` (`ubuntu`) |
| URL | https://dev.ocpctl.<BASE_DOMAIN> | https://ocpctl.<BASE_DOMAIN> |
| DB | `ocpctl-dev-db` (RDS, own) | `ocpctl-db` (RDS) |
| Binaries bucket | `s3://ocpctl-dev-binaries` | `s3://ocpctl-binaries` |
| Artifacts bucket | `s3://ocpctl-dev-artifacts` | `s3://ocpctl-artifacts` |
| Autoscale workers | none — worker runs on the API host | `ocpctl-worker-asg` (ASG) |
| SSH key | `~/.ssh/<DEV_SSH_KEY>` | `~/.ssh/<PROD_SSH_KEY>` |

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

1. **The SSH private key** at `$SSH_KEY` — `~/.ssh/<DEV_SSH_KEY>` (dev) or
   `~/.ssh/<PROD_SSH_KEY>` (prod). Regenerate from Terraform if missing:
   `terraform -chdir=terraform/dev output -raw ssh_private_key > ~/.ssh/<DEV_SSH_KEY> && chmod 600 ~/.ssh/<DEV_SSH_KEY>`.
   Different user/path? Override with `OCPCTL_SSH_USER` / `OCPCTL_SSH_KEY` — no
   need to edit the config file.
2. **The real env-config secret files** — `config/api.env.<env>` and
   `config/worker.env.<env>` (gitignored; they carry `DATABASE_URL`, `JWT_SECRET`,
   `OCM_TOKEN`, the OpenShift pull secret, and cloud creds). Only the
   `*.template` versions are tracked. Fastest path is
   `./scripts/handover-bundle.sh pull`, which fetches these plus the SSH keys,
   `tfvars`, and `CLAUDE.local.md` from the team-owned encrypted bundle — see
   [OWNERSHIP_HANDOVER.md](../operations/OWNERSHIP_HANDOVER.md).
3. **AWS credentials** (`aws configure` / SSO) with access to the S3 binaries +
   artifacts buckets and the worker ASG, in the account that hosts the
   deployment.
4. **Local tooling**: Go (build + `go test` gate), `aws` CLI, `jq`, `ssh`/`scp`,
   and Node.js 18+ for `scripts/deploy-web.sh`.
5. **For Terraform changes only**: nothing extra. State is in a shared S3 backend
   declared in each root module, so `terraform -chdir=terraform/<module> init` is
   enough — never keep a local `terraform.tfstate`.

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
./scripts/deploy-web.sh dev                 # UI changes only — separate step, see below
```

### Production (deliberate promotion)

```bash
./scripts/deploy-env.sh production v0.20260819.6c97dca
# or the production-only script:
./scripts/deploy.sh v0.20260819.6c97dca
./scripts/deploy-web.sh production          # UI changes only — separate step
```

**What a deploy does** (see CLAUDE.md → Deployment Process for detail):
1. Run unit tests (gate) → build linux binaries with version metadata.
2. Upload versioned + `binaries/` (stable) to S3; update `LATEST`.
3. Sync profiles, addons, manifests, scripts to S3.
4. **Terminate running autoscale workers** so the ASG relaunches them fresh from
   S3 — no manual termination needed. Instances are matched by the
   environment's `$AUTOSCALE_TAG`; in practice only production has an ASG
   (`ocpctl-worker-asg`) to relaunch them.
5. Deploy the **API host first** (copy binary → flip `current` symlink →
   `systemctl restart`), then each worker host (same, plus requeue RUNNING jobs
   and clear stale locks). API-first is deliberate: the API restart is what
   applies pending DB migrations (see §5).
6. Verify `/version` on API and worker.

### The web frontend is a separate deploy

`deploy.sh` and `deploy-env.sh` build and ship **only the Go binaries** — they
do not touch the Next.js frontend at all. A UI change is not live until you also
run:

```bash
./scripts/deploy-web.sh dev          # or: production
```

It deploys to the same host as the API (`$API_HOST`), installs `web.env`, and
restarts `ocpctl-web`. Both environments run an `ocpctl-web` service.

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

### Dev and prod: the API applies them automatically

**You do not run a migration command against dev or production.** The migration
files are `go:embed`-ed into the API binary, and `cmd/api/main.go` calls
`store.Migrate()` on startup — so *deploying the API is what migrates the
schema*. That happens before the API binds its port, which is why `deploy.sh`
polls `/version` for up to 60s instead of checking once, and why the API host is
deployed before the workers. The worker does **not** migrate; it opens the pool
and assumes the schema is already forward.

The runner (`internal/store/store.go`, `Migrate()`) tracks applied versions in
its own `schema_migrations` table, keyed on the numeric filename prefix, and
executes only the `-- +goose Up` half. **It has no down path** — the `Down`
sections exist for manual/goose use, not for anything the app will run.

> ⚠️ **Do not point the `goose` CLI at a dev or production database.** The
> `make migrate-up` / `make migrate-down` targets below invoke `goose`, which
> keeps its bookkeeping in a *different* table (`goose_db_version`) that the
> application never writes. Against an app-migrated database goose therefore
> sees an unmigrated schema and `migrate-up` would attempt to re-apply every
> migration from `00001`. Those targets are for a **local scratch database**
> only:

```bash
DATABASE_URL=postgres://…/localdb make migrate-up      # local dev DB only
DATABASE_URL=postgres://…/localdb make migrate-down    # local dev DB only
```

### Rules

- **Never reuse a version prefix.** The runner keys on the `NNNNN` prefix and
  skips anything already recorded, so a second file with an existing prefix is
  **silently never applied** — no error, no warning. Always take the next free
  number; `internal/store/migrations_test.go` fails the build on duplicates.
- **Additive and backward-compatible** — a new binary and the previous one may run
  against the same schema briefly during a rollout, and a rollback (§6) leaves the
  new schema in place.
- **Reversible** — every migration has a working `-- +goose Down`, for manual
  recovery.
- Apply migrations to **dev first** (i.e. deploy to dev), exercise the app, then
  production.
- Never edit a migration that has already run in any environment; add a new one.

---

## 6. Rollback

```bash
# List available versions
ssh -i ~/.ssh/<PROD_SSH_KEY> ubuntu@<PROD_HOST> 'sudo ls -d /opt/ocpctl/releases/*'

# Redeploy a previous version
./scripts/deploy-env.sh production v0.20260413.1346b69
```

Redeploying an older binary rolls back **code only — never the schema.** The old
API will re-run `Migrate()` on startup, find every version already recorded in
`schema_migrations`, and leave the newer schema exactly as it is. This is fine
precisely because migrations are required to be backward-compatible (§5): the
previous binary has to be able to run against the newer schema.

If a migration itself is the problem, a code rollback will not save you. Undo it
deliberately and out-of-band: apply that migration's `-- +goose Down` SQL by
hand (reverse order if several), then `DELETE FROM schema_migrations WHERE
version = 'NNNNN'` for each — otherwise the runner still considers it applied and
will not re-run it once the fix ships. Take an RDS snapshot first.

---

## 7. Secrets

- **Single source of truth:** `s3://ocpctl-binaries/config/worker.env` (prod) /
  `ocpctl-dev-binaries` (dev) — DATABASE_URL, pull secret, all cloud credentials
  (incl. the Azure service principal). Autoscale workers pull it at boot, so
  **secrets never land in the launch template or tfstate.**
- Local env config: `config/{api,worker,web}.env.{dev,production}` (git-ignored;
  templates checked in as `*.template`). A maintainer's full untracked set —
  these plus SSH keys, `tfvars`, and `CLAUDE.local.md` — is kept in the
  KMS-encrypted handover bundle (`scripts/handover-bundle.sh`); see
  [OWNERSHIP_HANDOVER.md](../operations/OWNERSHIP_HANDOVER.md). **Changing a
  worker credential means updating both the bundle and the S3 runtime copy** —
  workers only read the latter.
- **Never** commit secrets or paste them into logs/PRs. The pre-commit security
  check will flag obvious leaks, but it is not a substitute for care.
- CI/nightly secrets live in GitHub Actions repo secrets (see NIGHTLY_PIPELINE.md).

---

## 8. Quick health checks

```bash
# Service status / logs (dev shown; prod runs the same three services)
ssh -i ~/.ssh/<DEV_SSH_KEY> ubuntu@<DEV_HOST> \
  'sudo systemctl status ocpctl-api ocpctl-worker ocpctl-web'
ssh -i ~/.ssh/<DEV_SSH_KEY> ubuntu@<DEV_HOST> 'sudo journalctl -u ocpctl-worker -f'

# Versions
curl -s https://dev.ocpctl.<BASE_DOMAIN>/version
```

See CLAUDE.md → Troubleshooting for stuck-job, profile-loading, and
cluster-create debugging.
