#!/bin/bash
# Azure CLI authentication script for ocpctl-worker
# This script authenticates Azure CLI using service principal credentials from environment

set -e

# Check if Azure environment variables are set
if [ -z "$AZURE_CLIENT_ID" ] || [ -z "$AZURE_CLIENT_SECRET" ] || [ -z "$AZURE_TENANT_ID" ]; then
    echo "[azure-login] Azure credentials not configured, skipping authentication"
    exit 0
fi

# Check if az CLI is installed
if ! command -v az &> /dev/null; then
    echo "[azure-login] Azure CLI not installed, skipping authentication"
    exit 0
fi

echo "[azure-login] Authenticating Azure CLI with service principal..."

# Authenticate with service principal
if az login \
    --service-principal \
    --username "$AZURE_CLIENT_ID" \
    --password "$AZURE_CLIENT_SECRET" \
    --tenant "$AZURE_TENANT_ID" \
    --output none 2>&1; then
    echo "[azure-login] ✓ Azure CLI authenticated successfully"
else
    # Credentials ARE configured (checked above) but the login failed. Fail hard so
    # the worker does not start and silently accept Azure/ARO jobs it cannot run
    # (see #80). Workers without Azure credentials skip this via the early exit above.
    echo "[azure-login] ERROR: Azure credentials are configured but 'az login' failed" >&2
    exit 1
fi

# Write the service-principal credentials file that openshift-install reads for
# Azure IPI. Without it the installer prompts interactively and fails with
# "failed to retrieve credentials from user: EOF". ensure-installers.sh also
# writes this file, but autoscale workers run a systemd unit whose ExecStartPre
# does not invoke ensure-installers.sh — so create it here too, since azure-login.sh
# is wired into the unit and runs on every worker as the ocpctl user with
# HOME=/opt/ocpctl (guaranteeing correct location and ownership).
if [ -n "$AZURE_SUBSCRIPTION_ID" ]; then
    mkdir -p ~/.azure
    umask 077
    cat > ~/.azure/osServicePrincipal.json <<EOF
{
  "subscriptionId": "$AZURE_SUBSCRIPTION_ID",
  "clientId": "$AZURE_CLIENT_ID",
  "clientSecret": "$AZURE_CLIENT_SECRET",
  "tenantId": "$AZURE_TENANT_ID"
}
EOF
    chmod 600 ~/.azure/osServicePrincipal.json
    echo "[azure-login] ✓ Wrote Azure credentials to ~/.azure/osServicePrincipal.json"
else
    echo "[azure-login] WARNING: AZURE_SUBSCRIPTION_ID not set; skipping osServicePrincipal.json (openshift-install will fail for Azure)" >&2
fi
