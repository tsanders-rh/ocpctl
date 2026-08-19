<!-- See docs/development/CONTRIBUTING.md for the full process. -->

## What & why
<!-- What does this change do, and why? Link issues (e.g. Closes #97). -->

## How tested
<!-- Commands run + result. Unit tests MUST pass. -->
- [ ] `make test` passes locally (`go test -race ./...`)
- [ ] `make lint` clean
- [ ] Manual/dev verification (describe):

## Risk & rollout
- [ ] Touches DB migrations (additive + reversible; `migrate-down` works)
- [ ] Touches infra / Terraform (attach `terraform plan`)
- [ ] Touches worker boot / login scripts (correct path: `terraform/worker-autoscaling/user-data.sh` for ASG boot, **not** legacy `bootstrap-worker.sh`)
- [ ] No secrets in code, config, or logs

## Reviewer checklist
- [ ] Scope contained; behavior matches description
- [ ] Tests cover the change (regression test on bug fixes)
- [ ] CI green + 1 approval before merge (squash & merge)
