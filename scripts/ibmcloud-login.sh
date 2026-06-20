#!/bin/bash
# IBM Cloud CLI authentication script for ocpctl-worker
# This script authenticates IBM Cloud CLI using API key from environment

set -e

# Check if IBM Cloud API key is set
if [ -z "$IBMCLOUD_API_KEY" ]; then
    echo "[ibmcloud-login] IBM Cloud API key not configured, skipping authentication"
    exit 0
fi

# Check if ibmcloud CLI is installed
if ! command -v ibmcloud &> /dev/null; then
    echo "[ibmcloud-login] IBM Cloud CLI not installed, skipping authentication"
    exit 0
fi

echo "[ibmcloud-login] Authenticating IBM Cloud CLI with API key..."

# Authenticate with API key (non-interactive)
# Default to us-south region for consistency with current setup
if ibmcloud login \
    --apikey "$IBMCLOUD_API_KEY" \
    -r us-south \
    --no-region \
    2>&1 | grep -v "API endpoint"; then
    echo "[ibmcloud-login] ✓ IBM Cloud CLI authenticated successfully"
else
    # Login might succeed but output goes to stderr, check status
    if ibmcloud target &> /dev/null; then
        echo "[ibmcloud-login] ✓ IBM Cloud CLI authenticated successfully"
    else
        echo "[ibmcloud-login] WARNING: IBM Cloud CLI authentication failed (non-fatal)"
        exit 0
    fi
fi

# Target default resource group if IBMCLOUD_RESOURCE_GROUP is set
if [ -n "$IBMCLOUD_RESOURCE_GROUP" ]; then
    echo "[ibmcloud-login] Targeting resource group: $IBMCLOUD_RESOURCE_GROUP"
    if ibmcloud target -g "$IBMCLOUD_RESOURCE_GROUP" --output json &> /dev/null; then
        echo "[ibmcloud-login] ✓ Resource group targeted successfully"
    else
        echo "[ibmcloud-login] WARNING: Failed to target resource group (non-fatal)"
    fi
fi
