# Refreshing the registry.ci Pull Secret (Nightly Builds)

Runbook for rotating the `registry.ci.openshift.org` credential that lets ocpctl
provision OpenShift **nightly** builds.

> **Status: manual, ~monthly chore.** There is no auto-rotation today — the token
> is minted from a personal app.ci (OpenShift CI) login and expires roughly every
> month. See [§6 Automation](#6-why-this-is-manual--automation-notes) for why, and
> what a real fix would require.

---

## 1. Why this exists

OpenShift **nightly** builds (e.g. `4.23.0-0.nightly-2026-08-25-215343`) live
**only** in `registry.ci.openshift.org`. They are never published to the customer
mirror (`mirror.openshift.com`) or to quay `ocp-release`. Pulling a nightly
payload therefore requires a `registry.ci.openshift.org` auth entry inside the
worker's pull secret (`OPENSHIFT_PULL_SECRET`).

That auth is a **personal CI token that expires ~monthly**. When it lapses,
nightly provisioning breaks; EC (`-ec.N`) and GA installs are unaffected (they
come from the mirror/quay and need none of this).

Related: [NIGHTLY_PIPELINE.md](../development/NIGHTLY_PIPELINE.md),
[DEVOPS.md](../development/DEVOPS.md).

---

## 2. Symptoms of an expired token

You'll see one or both of these when the token has lapsed:

- **From the worker at create time** (nightly cluster goes `FAILED` early):
  ```
  cannot install nightly 4.23.0-0.nightly-...: pull secret is missing
  registry.ci.openshift.org auth — the registry.ci token expires ~monthly;
  refresh it with scripts/refresh-ci-pull-secret.sh and redeploy
  ```
- **From `oc adm release extract`** inside `download-specific-version.sh`:
  ```
  oc adm release extract failed for openshift-install from
  registry.ci.openshift.org/ocp/release:... (registry.ci token may be expired)
  ```

> **Note:** the same error text appears for *any* extract failure. Before
> assuming expiry, confirm with the validation step in [§5](#5-validate).
> (A past false alarm was actually a `set -o pipefail` bug in the download
> script, since fixed — the token was fine.)

**Prerequisites** (one-time): your account must have the `qci-image-puller` gate
cleared on app.ci — i.e. `oc login` to app.ci succeeds and `oc adm release info`
can read `registry.ci.openshift.org/ocp/release` nightlies. This is already true
for the maintainer account.

---

## 3. Mint a fresh CI token

The token is **not** an ocpctl credential — it comes from your personal login to
the OpenShift CI cluster (app.ci).

```bash
# 1. Get a login command for app.ci. Open the OAuth token request page:
#      https://oauth-openshift.apps.ci.l2s4.p1.openshiftapps.com/oauth/token/request
#    (or "Copy login command" from https://console.redhat.com/openshift/ → the CI cluster)
#    then run the printed command, e.g.:
oc login --token=sha256~XXXX --server=https://api.ci.l2s4.p1.openshiftapps.com:6443

# 2. Write a docker-config JSON containing the registry.ci auth:
oc registry login --to=/tmp/ci-auth.json
```

`/tmp/ci-auth.json` now holds an `.auths["registry.ci.openshift.org"]` entry.
**Treat it as a secret** — delete it when done ([§7](#7-cleanup)).

---

## 4. Merge it into the worker pull secret(s)

```bash
./scripts/refresh-ci-pull-secret.sh /tmp/ci-auth.json
```

What the script does (see header of
[`scripts/refresh-ci-pull-secret.sh`](../../scripts/refresh-ci-pull-secret.sh)):

- Replaces only the `registry.ci.openshift.org` entry inside `OPENSHIFT_PULL_SECRET`
  in all three env files, leaving every other registry auth untouched:
  - `config/worker.env`
  - `config/worker.env.production`
  - `config/worker.env.dev`
- Preserves the exact single-line, single-quoted format the env files use.
- Validates the merged secret still parses as JSON with the new auth present, and
  refuses to touch a file whose existing secret isn't valid JSON.
- **Never prints secret material.**

> `worker.env*` are **gitignored on purpose** — do not commit them. Review the
> local diff to confirm only the `registry.ci` auth changed, then move on.

---

## 5. Deploy so workers pick it up

```bash
./scripts/deploy.sh          # production: static hosts + uploads worker.env to S3
./scripts/deploy-env.sh dev  # dev
```

How the refreshed secret reaches each worker type:

| Worker              | Delivery path                                                                 |
|---------------------|-------------------------------------------------------------------------------|
| Static API/worker   | `deploy` scp's `worker.env` → `/etc/ocpctl/worker.env`, restarts the service. systemd loads it as an `EnvironmentFile`; the Go worker passes `OPENSHIFT_PULL_SECRET` to `oc adm release extract`. |
| Autoscale (ASG)     | `deploy.sh` uploads `worker.env` to `s3://ocpctl-binaries/config/worker.env` (single source of truth) **and terminates running ASG workers**. On next boot the Terraform user-data pulls it fresh from S3. No AMI rebuild. |

### Validate

Confirm the token actually works against a real nightly payload (this reads
metadata; it does not download the payload):

```bash
# Resolve the current latest 4.x nightly:
NIGHTLY=$(curl -s \
  "https://amd64.ocp.releases.ci.openshift.org/api/v1/releasestream/4.23.0-0.nightly/latest" \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["name"])')

# On a worker host, as the ocpctl user, using the deployed pull secret:
#   (extract OPENSHIFT_PULL_SECRET from /etc/ocpctl/worker.env to a temp file first)
oc adm release info "registry.ci.openshift.org/ocp/release:${NIGHTLY}" \
  --registry-config=/path/to/pull-secret.json
```

A successful run prints the release `Name`, `Digest`, and `Pull From:` lines. An
auth failure means the token didn't take — recheck [§3](#3-mint-a-fresh-ci-token)
and [§4](#4-merge-it-into-the-worker-pull-secrets).

The definitive end-to-end check is simply to **provision a nightly SNO** (profile
`aws-sno-prerelease`, a `4.x.0-0.nightly` version) and watch it resolve, extract
the installer, and start `openshift-install`.

---

## 6. Why this is manual / automation notes

The blocker to automation is the **credential source**, not the delivery plumbing
(steps 4–5 are already fully scriptable):

- The current token is a **personal OAuth/SSO token** from an interactive app.ci
  login. It's tied to a user, expires ~monthly, and can't be re-obtained
  non-interactively (OAuth needs a browser; there's no scriptable refresh).
- A real fix needs a **machine identity** — a ServiceAccount on app.ci with
  `system:image-puller` on the `ocp` namespace and a **long-lived token**, stored
  in a secret manager (e.g. AWS Secrets Manager). A scheduled job could then
  re-run the [§4](#4-merge-it-into-the-worker-pull-secrets)/[§5](#5-deploy-so-workers-pick-it-up)
  path automatically. Whether app.ci policy permits a non-expiring SA token must
  be confirmed with the CI/DPTP team.
- **Cheap interim win:** a daily `oc adm release info` check against the latest
  nightly that alerts *before* a customer create fails, turning "discover expiry
  via a failed 40-minute provision" into "get pinged with a week's notice."

---

## 7. Cleanup

```bash
rm -f /tmp/ci-auth.json
```

Never leave the minted docker-config lying around, and never commit `worker.env*`.
