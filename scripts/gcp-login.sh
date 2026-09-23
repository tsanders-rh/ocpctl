#!/bin/bash
# GCP CLI authentication script for ocpctl-worker
# Activates the gcloud service account from the ADC key file so `gcloud`
# shell-outs work. Mirrors azure-login.sh / ibmcloud-login.sh and runs as an
# ExecStartPre hook on every worker (as the ocpctl user, HOME=/opt/ocpctl).
#
# There are TWO independent GCP auth paths and this script only covers the
# second one:
#   1. The Go GCP client and openshift-install read the ADC key FILE directly
#      via $GOOGLE_APPLICATION_CREDENTIALS. Nothing needs to run for that — the
#      file just has to exist (placed by deploy.sh on the static hosts, and by
#      the ASG launch-template user-data on autoscale workers).
#   2. `gcloud` keeps its own credential store and ignores
#      $GOOGLE_APPLICATION_CREDENTIALS for its own auth, so it needs an explicit
#      activate-service-account. GKE provisioning and the janitor's orphaned-GCP
#      detection both shell out to gcloud.

set -e

# No GCP configured in this environment — nothing to do.
if [ -z "$GOOGLE_APPLICATION_CREDENTIALS" ]; then
    echo "[gcp-login] GOOGLE_APPLICATION_CREDENTIALS not set, skipping authentication"
    exit 0
fi

if ! command -v gcloud &> /dev/null; then
    echo "[gcp-login] gcloud CLI not installed, skipping authentication"
    exit 0
fi

# Deliberately NON-fatal, unlike azure-login.sh. A worker serves every platform,
# so refusing to start over absent GCP credentials would idle its AWS/Azure/IBM
# capacity too — a worse outage than GCP jobs failing with the clear
# "open /opt/ocpctl/gcp-credentials.json: no such file" error they already give.
# Autoscale workers went months in exactly this state because the launch-template
# user-data never downloaded the key, so log it loudly enough to be found.
if [ ! -f "$GOOGLE_APPLICATION_CREDENTIALS" ]; then
    echo "[gcp-login] ERROR: GOOGLE_APPLICATION_CREDENTIALS points at $GOOGLE_APPLICATION_CREDENTIALS but that file does not exist." >&2
    echo "[gcp-login] ERROR: GCP and GKE jobs claimed by this worker WILL fail. Expected the key to be downloaded from" >&2
    echo "[gcp-login] ERROR: s3://<binaries-bucket>/config/gcp-credentials.json at boot (ASG: terraform/worker-autoscaling/user-data.sh)." >&2
    exit 0
fi

echo "[gcp-login] Activating GCP service account from $GOOGLE_APPLICATION_CREDENTIALS..."

if gcloud auth activate-service-account --key-file="$GOOGLE_APPLICATION_CREDENTIALS" --quiet 2>&1; then
    echo "[gcp-login] ✓ GCP service account activated"
else
    # The key file is present but unusable (malformed, revoked, wrong project).
    # Still non-fatal for the reason above; the error text is what matters.
    echo "[gcp-login] ERROR: gcloud auth activate-service-account failed; gcloud-based GCP/GKE operations will fail" >&2
    exit 0
fi

# Pin the project so gcloud commands that don't pass --project resolve correctly.
if [ -n "$GCP_PROJECT" ]; then
    if gcloud config set project "$GCP_PROJECT" --quiet 2>&1; then
        echo "[gcp-login] ✓ gcloud project set to $GCP_PROJECT"
    else
        echo "[gcp-login] WARNING: could not set gcloud project to $GCP_PROJECT" >&2
    fi
else
    echo "[gcp-login] WARNING: GCP_PROJECT not set; gcloud commands without --project may fail" >&2
fi
