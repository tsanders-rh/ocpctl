#!/bin/bash
# Report when the deployed registry.ci pull-secret token expires.
#
# Usage:
#   scripts/check-ci-pull-secret-expiry.sh [worker.env path]
#
#   WARN_DAYS=7   exit non-zero when fewer than this many days remain (default 7)
#
# Exit codes: 0 ok · 1 error/unknown · 2 expiring inside WARN_DAYS · 3 expired
#
# Why this works: the registry.ci credential is an app.ci OAuth token, and
# OpenShift stores each one as an OAuthAccessToken named
#   sha256~<base64url-nopad(sha256(<secret part of the token>))>
# `useroauthaccesstokens` lets a user list their own tokens, and the deployed
# token can authenticate that request about itself — no browser login needed. We
# derive the object name from the token we hold so we report the expiry of the
# token that is actually deployed, not merely the newest one on the account.
#
# See docs/operations/CI_PULL_SECRET_REFRESH.md.

set -euo pipefail

ENV_FILE="${1:-config/worker.env.production}"
WARN_DAYS="${WARN_DAYS:-7}"
APP_CI="${APP_CI_API:-https://api.ci.l2s4.p1.openshiftapps.com:6443}"

if [ ! -f "$ENV_FILE" ]; then
    echo "error: $ENV_FILE not found" >&2
    echo "(pull it with ./scripts/handover-bundle.sh pull)" >&2
    exit 1
fi

ENV_FILE="$ENV_FILE" WARN_DAYS="$WARN_DAYS" APP_CI="$APP_CI" python3 - <<'PY'
import base64, datetime, hashlib, json, os, ssl, sys, urllib.error, urllib.request

env_file = os.environ["ENV_FILE"]
warn_days = int(os.environ["WARN_DAYS"])
app_ci = os.environ["APP_CI"].rstrip("/")

raw = None
for line in open(env_file):
    if line.startswith("OPENSHIFT_PULL_SECRET="):
        raw = line.split("=", 1)[1].strip().strip("'").strip('"')
if not raw:
    sys.exit(f"error: no OPENSHIFT_PULL_SECRET in {env_file}")

try:
    auths = json.loads(raw)["auths"]
except (ValueError, KeyError) as e:
    sys.exit(f"error: could not parse OPENSHIFT_PULL_SECRET ({e})")

entry = auths.get("registry.ci.openshift.org")
if not entry:
    sys.exit("error: pull secret has no registry.ci.openshift.org auth — "
             "nightly installs cannot work. See the runbook.")

token = base64.b64decode(entry["auth"]).decode().split(":", 1)[1]
if "~" not in token:
    sys.exit("error: registry.ci auth is not an app.ci OAuth token (no 'sha256~' prefix); "
             "expiry cannot be derived this way")

secret = token.split("~", 1)[1]
name = "sha256~" + base64.urlsafe_b64encode(
    hashlib.sha256(secret.encode()).digest()).decode().rstrip("=")

req = urllib.request.Request(
    f"{app_ci}/apis/oauth.openshift.io/v1/useroauthaccesstokens",
    headers={"Authorization": "Bearer " + token})
try:
    data = json.load(urllib.request.urlopen(req, timeout=30))
except urllib.error.HTTPError as e:
    if e.code in (401, 403):
        print(f"EXPIRED or revoked: app.ci rejected the token ({e.code})")
        print("Refresh it: docs/operations/CI_PULL_SECRET_REFRESH.md")
        sys.exit(3)
    sys.exit(f"error: app.ci returned {e.code} {e.reason}")
except (urllib.error.URLError, ssl.SSLError) as e:
    sys.exit(f"error: could not reach app.ci ({e})")

now = datetime.datetime.now(datetime.timezone.utc)
for item in data.get("items", []):
    if item["metadata"]["name"] != name:
        continue
    created = datetime.datetime.fromisoformat(
        item["metadata"]["creationTimestamp"].replace("Z", "+00:00"))
    expires = created + datetime.timedelta(seconds=item.get("expiresIn", 0))
    left = expires - now
    days = left.days + left.seconds / 86400

    print(f"env file:  {env_file}")
    print(f"client:    {item.get('clientName')}")
    print(f"issued:    {created:%Y-%m-%d %H:%M:%SZ}")
    print(f"expires:   {expires:%Y-%m-%d %H:%M:%SZ}")

    if left.total_seconds() <= 0:
        print(f"status:    EXPIRED {abs(days):.1f} days ago")
        print("Refresh it: docs/operations/CI_PULL_SECRET_REFRESH.md")
        sys.exit(3)
    if days < warn_days:
        print(f"status:    EXPIRING in {days:.1f} days (warn at {warn_days})")
        print("Refresh it: docs/operations/CI_PULL_SECRET_REFRESH.md")
        sys.exit(2)
    print(f"status:    ok, {days:.1f} days remaining")
    sys.exit(0)

# Authenticated fine but the token is not in the user's own list. Don't guess.
print("warning: token authenticated to app.ci but no matching "
      "useroauthaccesstokens entry was found, so the expiry is unknown.")
print("It may be a ServiceAccount token rather than a personal OAuth token.")
sys.exit(1)
PY
