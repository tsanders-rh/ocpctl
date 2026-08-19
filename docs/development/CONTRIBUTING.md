# Contributing to ocpctl

How we branch, commit, review, and gate changes into `main`.

---

## 1. Branching model

Trunk-based with short-lived feature branches. `main` is always releasable.

```
main ← feature/<topic>      new capability
     ← fix/<topic>          bug fix
     ← chore/<topic>        tooling, deps, docs, refactors
     ← hotfix/<topic>       urgent production fix (see §7)
```

- Branch off the latest `main`.
- Keep branches focused and short-lived; rebase on `main` before opening a PR to keep history linear.
- **Never push directly to `main`** — it is branch-protected (see §5).

---

## 2. Commit conventions

Use [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): summary`.

```
feat(profile): add azure-sno-ga prerelease track
fix(worker): create TMPDIR so openshift-install writes bootstrap ignition
docs(dev): add nightly pipeline design
chore(ci): run golangci-lint in CI
```

Types: `feat`, `fix`, `docs`, `chore`, `refactor`, `test`, `perf`, `build`, `ci`.

Guidelines:
- Imperative mood ("add", not "added"); ≤ 72-char summary line.
- Explain **why** in the body when it isn't obvious from the diff.
- One logical change per commit; squash noise before pushing.
- Reference issues/PRs (`#97`) where relevant.

---

## 3. Before you push — local checks

These mirror the CI gates. Run them so CI is a formality, not a surprise.

```bash
make fmt          # gofmt + web format
make lint         # golangci-lint + next lint   (config: .golangci.yml)
make test         # go test -race ./...  ← MUST pass
make build        # go build ./cmd/...  (compiles both binaries)
```

**Unit tests are a hard gate.** If `make test` fails, the change is not ready — CI will block it, and the deploy scripts will refuse to build it. Add or update tests alongside behavior changes; put a regression test on every bug fix (e.g. `internal/profile/azure_tags_test.go` was added for the Azure userTags fix).

Requires: Go 1.25+, `golangci-lint`, Node (for `web/`). A local Postgres is needed for integration tests: `make test-integration`.

---

## 4. Pull requests

1. Push your branch and open a PR against `main`.
2. Fill in the PR template (`.github/pull_request_template.md`): what/why, test evidence, risk, and whether it touches migrations, infra (Terraform), or worker boot scripts.
3. Ensure **CI is green** — see §6.
4. Request review; address feedback with follow-up commits (don't force-push after review starts unless asked).
5. Merge only when CI is green **and** you have the required approval.

**Merge style:** *Squash and merge* (keeps `main` history one-commit-per-PR and clean). The squash commit message should follow the Conventional Commits format from §2.

---

## 5. Reviews required (branch protection)

The standard: **every change reaches `main` via a PR with green CI and at least one approving review.**

Enforce on GitHub → Settings → Branches → add a rule for `main`:
- ☑ Require a pull request before merging
  - ☑ Require approvals: **1**
  - ☑ Dismiss stale approvals when new commits are pushed
- ☑ Require status checks to pass before merging
  - Required check: **`vet-and-test`** (add `lint` / `build` once added to CI — see §6)
  - ☑ Require branches to be up to date before merging
- ☑ Require conversation resolution before merging
- ☑ Do not allow bypassing the above settings (applies to admins too)

One-shot setup via `gh` (adjust required checks as CI grows):

```bash
gh api -X PUT repos/tsanders-rh/ocpctl/branches/main/protection \
  -H "Accept: application/vnd.github+json" \
  -f 'required_status_checks[strict]=true' \
  -f 'required_status_checks[contexts][]=vet-and-test' \
  -f 'enforce_admins=true' \
  -F 'required_pull_request_reviews[required_approving_review_count]=1' \
  -f 'required_pull_request_reviews[dismiss_stale_reviews]=true' \
  -f 'restrictions=' -f 'required_conversation_resolution=true'
```

**Reviewer checklist:**
- [ ] Behavior matches the PR description; scope is contained.
- [ ] Tests cover the change; a bug fix has a regression test.
- [ ] No secrets/credentials in code, config, or logs.
- [ ] DB migrations are additive/backward-compatible and reversible (`migrate-down` works).
- [ ] Changes to **worker boot scripts** land in the right place — the prod ASG boots from `terraform/worker-autoscaling/user-data.sh`, **not** `scripts/bootstrap-worker.sh` (legacy). See CLAUDE.md.
- [ ] Terraform changes reviewed against a `terraform plan`.

> **Solo-owner note:** while the repo is effectively single-maintainer, requiring a second approval can stall work. Two accepted paths: (a) enable the rule above and use a second account / co-maintainer for approvals; or (b) run "PR + green CI, self-approve" as an interim and turn on required-reviews when a second contributor joins. Either way, **the PR + green-CI gate always applies.**

---

## 6. CI gates

CI runs on every PR and every push to `main` (`.github/workflows/ci.yml`).

**Today:** `go vet ./...` + `go test -race ./...` against a Postgres service container.

**Recommended hardening** (so branch protection can require them):

```yaml
      - name: gofmt (check)
        run: test -z "$(gofmt -l .)"
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v6
      - name: go build
        run: go build ./cmd/...
      - name: web lint + build
        run: cd web && npm ci && npm run lint && npm run build
```

Add these as separate steps/jobs, then add their names to the required-status-checks list in §5.

---

## 7. Hotfixes

Urgent production fixes still go through a PR (`hotfix/<topic>`), but you may:
- fast-track review, and
- deploy to production immediately after merge once CI is green.

The unit-test build gate still applies. The `SKIP_TESTS=1` override exists only for a genuine emergency where tests can't run — using it is a decision to ship untested code and must be called out in the PR/incident notes. See [DEVOPS.md](DEVOPS.md#emergency-override).

---

## 8. Where things live

| Change type | Primary files | Notes |
|-------------|---------------|-------|
| New profile | `internal/profile/definitions/*.yaml` | Deploy syncs to S3; API reload picks it up |
| New addon | `internal/addon/definitions/*.yaml` | Same as profiles |
| DB schema | `internal/store/migrations/NNNNN_*.sql` | Additive + reversible; test `migrate-up`/`down` |
| API handler | `internal/api/handler_*.go` | |
| Worker job | `internal/worker/handler_*.go` | |
| **ASG worker boot** | `terraform/worker-autoscaling/user-data.sh` | Requires `terraform apply` (new LT version), **not** `deploy.sh` |
| Worker login hooks | `scripts/azure-login.sh`, `scripts/ibmcloud-login.sh` | Pulled fresh from S3 on boot; `deploy.sh` uploads them |

See [DEVOPS.md](DEVOPS.md) for the deploy and infra mechanics.
