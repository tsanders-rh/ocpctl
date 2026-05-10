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
    echo "[azure-login] WARNING: Azure CLI authentication failed (non-fatal)"
    exit 0
fi
