# Developer Documentation

Process and tooling docs for contributing to and operating ocpctl.

| Doc | Covers |
|-----|--------|
| [CONTRIBUTING.md](CONTRIBUTING.md) | Branching, commit conventions, pull requests, **required reviews**, CI gates, local checks |
| [DEVOPS.md](DEVOPS.md) | Versioning, build (**unit-test-gated**), deploy to **dev** and **production**, autoscale-worker self-update, DB migrations, rollback, secrets |
| [NIGHTLY_PIPELINE.md](NIGHTLY_PIPELINE.md) | Proposed scheduled nightly: build+test → deploy to dev → provision an AWS SNO smoke cluster → verify → auto-destroy |

Related existing docs:
- [`../deployment/DEV_TEST_ENVIRONMENT_PLAN.md`](../deployment/DEV_TEST_ENVIRONMENT_PLAN.md) — original design of the (now live) dev environment
- [`../../CLAUDE.md`](../../CLAUDE.md) — system architecture, production/dev topology, debugging runbook

## TL;DR flow

```
feature branch ──PR──▶ CI (vet + unit tests + lint + build) ──1 approval──▶ main
                                                                             │
                                                          nightly (scheduled GH Action)
                                                                             │
                                          build+test ▶ deploy dev ▶ AWS SNO smoke ▶ destroy
                                                                             │
                                    manual promotion: ./scripts/deploy-env.sh production <version>
```

## Non-negotiables

- **Unit tests must pass at build.** `go test -race ./...` runs in CI, in the nightly, and now inside `deploy.sh` / `deploy-env.sh` — a failing test aborts the build/deploy. Emergency override: `SKIP_TESTS=1` (discouraged; see [DEVOPS.md](DEVOPS.md#emergency-override)).
- **`main` is protected.** No direct pushes; merge via PR with green CI + 1 approval.
- **Dev is the proving ground.** Nightlies (and ad-hoc deploys) land in dev first; production is a deliberate, manual promotion of a dev-validated version.
