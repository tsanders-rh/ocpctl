# Nightly Pipeline (Proposed)

A scheduled GitHub Actions workflow that, every night, builds `main`, runs the
full test suite, deploys to **dev**, provisions one **AWS OpenShift SNO** smoke
cluster end-to-end, verifies it reaches `READY`, then **destroys it**. Catches
regressions in the real provisioning path before they reach production.

> **Status: proposed / not yet enabled.** It provisions a real (billable)
> cluster and needs repo secrets, so it ships disabled. Enable per §6 once
> secrets are set. Scope is a single AWS SNO (cheapest path that exercises the
> full create → READY → destroy lifecycle); widen later via the matrix knob in §7.

---

## 1. Goals & non-goals

**Goals**
- Nightly proof that `main` builds, passes tests, deploys to dev, and can take an
  OpenShift cluster all the way to `READY` and cleanly destroy it.
- Fast, visible signal (GitHub check + issue/Slack on failure).
- **No leaked cloud resources** — teardown is guaranteed even on failure.

**Non-goals**
- Not a load test, not a full platform matrix (see §7), not a production deploy.
  Production remains a manual promotion of a dev-validated version
  ([DEVOPS.md](DEVOPS.md#4-deploying)).

---

## 2. Stages

```
┌───────────┐  ┌───────────┐  ┌────────────┐  ┌─────────────────┐  ┌───────────┐
│ build+test│─▶│ deploy dev│─▶│ create SNO │─▶│ poll until READY│─▶│  destroy  │
└───────────┘  └───────────┘  └────────────┘  └─────────────────┘  └───────────┘
   go test        deploy-env.sh   POST /clusters   GET /clusters/:id   DELETE /clusters/:id
   -race ./...    dev             (aws-sno-ga)     (timeout ~75m)      (if: always)
```

1. **build + test** — `go vet` + `go test -race ./...` (hard gate; matches CI and
   the deploy-script gate). Compute the version string.
2. **deploy dev** — `./scripts/deploy-env.sh dev` (uploads to S3, restarts dev
   services). The script re-runs tests as its own gate; that's fine.
3. **create** — call the dev **public** API to create an `aws-sno-ga` cluster
   with a short TTL.
4. **poll** — poll cluster status until `READY` (pass) or `FAILED`/timeout (fail).
   On failure, pull cluster logs into the job output/artifacts.
5. **destroy** — `DELETE` the cluster in an `if: always()` step so it runs whether
   the test passed, failed, or the job was cancelled. The short TTL is a
   belt-and-suspenders fallback (the janitor reaps it even if destroy fails).

---

## 3. API endpoints used (verified in `internal/api/server.go`)

| Purpose | Method + path | Auth |
|--------|---------------|------|
| Login (get JWT) | `POST /api/v1/auth/login` | none (rate-limited 5/min) |
| Create cluster | `POST /api/v1/clusters` | Bearer / API key |
| Get cluster | `GET /api/v1/clusters/{id}` | Bearer / API key |
| Cluster logs | `GET /api/v1/clusters/{id}/logs` | Bearer / API key |
| Destroy cluster | `DELETE /api/v1/clusters/{id}` | Bearer / API key |

Prefer a **long-lived API key** (`/api/v1/api-keys`, `RequireAuthDual`) for the
nightly over a username/password login, and store it as a secret. All calls hit
the dev public URL `https://dev.ocpctl.<BASE_DOMAIN>`.

---

## 4. Required GitHub secrets

| Secret | Used for |
|--------|----------|
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | `deploy-env.sh dev` (S3 upload) + worker AWS access |
| `DEV_SSH_KEY` | private key matching `~/.ssh/<DEV_SSH_KEY>` (restart dev services) |
| `DEV_API_TOKEN` | dev API key for the smoke test's cluster calls |
| `NIGHTLY_PULL_SECRET` *(if not already on dev)* | OpenShift pull secret |
| `SLACK_WEBHOOK_URL` *(optional)* | failure notification |

Least privilege: scope the AWS key to what dev deploy + SNO create needs; scope
the API key to a service account with cluster create/read/delete only.

---

## 5. Workflow skeleton

Save as `.github/workflows/nightly.yml` when enabling (see §6). Adjust the SNO
create payload to match your `CreateClusterRequest` shape (see
`internal/api/handler_clusters.go` / Swagger).

```yaml
name: Nightly (dev smoke)

on:
  schedule:
    - cron: "0 7 * * *"     # 07:00 UTC ≈ 03:00 ET; nightly
  workflow_dispatch: {}      # allow manual runs

concurrency:                 # never run two nightlies at once
  group: nightly-dev
  cancel-in-progress: false

permissions:
  contents: read
  issues: write              # to open a failure issue

jobs:
  build-test-deploy-smoke:
    runs-on: ubuntu-latest
    timeout-minutes: 120
    services:
      postgres:
        image: postgres:17
        env: { POSTGRES_PASSWORD: test, POSTGRES_DB: ocpctl_test }
        ports: ["5432:5432"]
        options: >-
          --health-cmd pg_isready --health-interval 10s
          --health-timeout 5s --health-retries 5
    env:
      TEST_DATABASE_URL: postgres://postgres:test@localhost:5432/ocpctl_test
      API: https://dev.ocpctl.<BASE_DOMAIN>
      AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
      AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
      AWS_DEFAULT_REGION: us-east-1
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod, cache: true }

      # 1. Hard test gate
      - run: go vet ./...
      - run: go test -race ./...

      # 2. Deploy to dev (build is unit-test-gated inside the script too)
      - name: Install dev SSH key
        run: |
          mkdir -p ~/.ssh && echo "${{ secrets.DEV_SSH_KEY }}" > ~/.ssh/<DEV_SSH_KEY>
          chmod 600 ~/.ssh/<DEV_SSH_KEY>
          ssh-keyscan <DEV_HOST> >> ~/.ssh/known_hosts 2>/dev/null || true
      - name: Deploy to dev
        run: ./scripts/deploy-env.sh dev

      # 3-5. Smoke: create → poll → destroy (teardown guaranteed)
      - name: Smoke test (AWS SNO)
        id: smoke
        env: { DEV_API_TOKEN: ${{ secrets.DEV_API_TOKEN }} }
        run: ./scripts/nightly-smoke.sh
      - name: Teardown (always)
        if: always()
        env: { DEV_API_TOKEN: ${{ secrets.DEV_API_TOKEN }} }
        run: ./scripts/nightly-smoke.sh --teardown

      # 6. Notify on failure
      - name: Open failure issue
        if: failure()
        uses: actions/github-script@v7
        with:
          script: |
            github.rest.issues.create({
              owner: context.repo.owner, repo: context.repo.repo,
              title: `Nightly dev smoke failed — ${new Date().toISOString().slice(0,10)}`,
              body: `Run: ${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`,
              labels: ['nightly','ci']
            })
```

### `scripts/nightly-smoke.sh` (sketch)

```bash
#!/usr/bin/env bash
# Create an AWS SNO on dev, wait for READY, then destroy. --teardown only destroys.
set -euo pipefail
API="${API:?}"; TOKEN="${DEV_API_TOKEN:?}"
AUTH=(-H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json")
NAME="nightly-$(date -u +%Y%m%d)"
STATE=".nightly-cluster-id"

destroy() {
  [ -f "$STATE" ] || return 0
  local id; id=$(cat "$STATE")
  echo "Destroying cluster $id"
  curl -fsS -X DELETE "${AUTH[@]}" "$API/api/v1/clusters/$id" || true
}

if [ "${1:-}" = "--teardown" ]; then destroy; exit 0; fi

# Create — short TTL so a leaked cluster is reaped by the janitor even if destroy fails.
ID=$(curl -fsS -X POST "${AUTH[@]}" "$API/api/v1/clusters" -d "{
  \"name\": \"$NAME\", \"profile\": \"aws-sno-ga\", \"platform\": \"aws\",
  \"region\": \"us-east-1\", \"ttlHours\": 2, \"skipPostDeployment\": true
}" | jq -r .id)
echo "$ID" > "$STATE"
echo "Created cluster $ID ($NAME)"

# Poll up to ~75 min
for i in $(seq 1 90); do
  S=$(curl -fsS "${AUTH[@]}" "$API/api/v1/clusters/$ID" | jq -r .status)
  echo "[$i] status=$S"
  case "$S" in
    READY)  echo "✓ SNO reached READY"; exit 0 ;;
    FAILED) echo "✗ create FAILED"; curl -fsS "${AUTH[@]}" "$API/api/v1/clusters/$ID/logs" | tail -100; exit 1 ;;
  esac
  sleep 50
done
echo "✗ timed out waiting for READY"; exit 1
```

Confirm the create-request field names against `CreateClusterRequest`
(`pkg/types` / `internal/api/handler_clusters.go`) before enabling — the JSON
keys above are illustrative.

---

## 6. Enabling it

1. Add the secrets in §4 (repo → Settings → Secrets and variables → Actions).
2. Add `scripts/nightly-smoke.sh` (from §5) and make it executable.
3. Commit `.github/workflows/nightly.yml`.
4. Trigger once manually via **workflow_dispatch** and watch it end-to-end
   (verify the cluster is actually destroyed and no resources leak).
5. Leave the `schedule` trigger to run nightly thereafter.

---

## 7. Guardrails & tuning

- **Guaranteed teardown:** `if: always()` destroy step + short cluster `ttlHours`
  so the janitor reaps anything the destroy step misses. Verify no orphans via
  the orphaned-resources handler / AWS console after the first few runs.
- **Cost:** one SNO for ~1–1.5h/night. An AWS SNO is ~$0.38/hr running — a few
  dollars a month. Widen scope only when the signal justifies the spend.
- **Concurrency:** the `concurrency` group prevents overlapping nightlies from
  fighting over the dev environment.
- **Matrix knob (future):** make the profile list a workflow input / matrix
  (`aws-sno-ga`, then `azure-sno-ga`, `gke-standard`, `eks-standard`, …). Keep
  each as its own create→poll→destroy so one platform's failure doesn't mask
  another's. **If you cap or sample the matrix, log what was skipped** so a green
  run isn't mistaken for full coverage.
- **Flake handling:** distinguish infra flakes (transient cloud errors) from real
  regressions before auto-filing issues — e.g. one retry on create, and include
  the installer log tail in the failure report.
