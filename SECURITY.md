# Security Policy

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues,
pull requests, or discussions.**

Report suspected vulnerabilities privately through GitHub's built-in private
vulnerability reporting:

1. Go to the repository's **Security** tab.
2. Click **Report a vulnerability** (under *Security advisories*).
3. Fill in the advisory form.

This opens a private channel with the maintainers. If private reporting is
unavailable for any reason, contact a maintainer listed in [OWNERS](OWNERS)
directly rather than filing publicly.

Please include as much of the following as you can:

- The type of issue (e.g. authentication bypass, injection, secret exposure,
  privilege escalation, SSRF).
- The affected component and version/commit (API server, worker, web UI,
  Terraform/infra, deploy scripts).
- Step-by-step reproduction, proof-of-concept, or configuration required to
  trigger it.
- The impact — what an attacker could do.

## What to Expect

This is a community-maintained project; response is best-effort.

- We aim to acknowledge a report within a few business days.
- We will investigate, keep you updated on progress, and coordinate a fix and
  disclosure timeline with you.
- Please give us reasonable time to release a fix before any public disclosure
  (coordinated disclosure).

## Scope

This policy covers the ocpctl codebase in this repository — the API server,
worker, web UI, and the deployment/infrastructure code under `scripts/` and
`terraform/`.

Note that ocpctl provisions clusters and handles credentials for AWS, GCP, and
IBM Cloud; reports involving credential handling, secret storage, or the job/lock
model are especially welcome. For guidance on hardening a deployment, see
[docs/deployment/SECURITY_CONFIGURATION.md](docs/deployment/SECURITY_CONFIGURATION.md).
