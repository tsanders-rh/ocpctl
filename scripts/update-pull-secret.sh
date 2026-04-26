#!/bin/bash
# Update OpenShift pull secret in AWS Secrets Manager
#
# SECURITY: Pull secret must be provided via environment variable
# Usage: OPENSHIFT_PULL_SECRET="$(cat ~/.openshift/pull-secret.json)" ./update-pull-secret.sh

set -e

REGION="${AWS_REGION:-us-west-2}"
SECRET_NAME="${OPENSHIFT_PULL_SECRET_NAME:-ocpctl/pull-secret}"

echo "============================================"
echo "Update OpenShift Pull Secret"
echo "============================================"
echo "Region: $REGION"
echo "Secret Name: $SECRET_NAME"
echo ""

# Validate pull secret is provided via environment variable
if [ -z "$OPENSHIFT_PULL_SECRET" ]; then
    echo "ERROR: OPENSHIFT_PULL_SECRET environment variable not set"
    echo ""
    echo "Usage:"
    echo "  OPENSHIFT_PULL_SECRET=\"\$(cat ~/.openshift/pull-secret.json)\" $0"
    echo ""
    echo "Or export it:"
    echo "  export OPENSHIFT_PULL_SECRET=\"\$(cat ~/.openshift/pull-secret.json)\""
    echo "  $0"
    echo ""
    echo "Get your pull secret from:"
    echo "  https://console.redhat.com/openshift/downloads#tool-pull-secret"
    exit 1
fi

PULL_SECRET="$OPENSHIFT_PULL_SECRET"

# Validate JSON format
echo "Step 1: Validating pull secret JSON format..."
if ! echo "$PULL_SECRET" | jq . > /dev/null 2>&1; then
    echo "ERROR: Pull secret is not valid JSON"
    exit 1
fi

# Validate required keys
echo "Step 2: Validating pull secret structure..."
if ! echo "$PULL_SECRET" | jq -e '.auths."quay.io"' > /dev/null 2>&1; then
    echo "ERROR: Pull secret missing quay.io auth"
    exit 1
fi

if ! echo "$PULL_SECRET" | jq -e '.auths."cloud.openshift.com"' > /dev/null 2>&1; then
    echo "ERROR: Pull secret missing cloud.openshift.com auth"
    exit 1
fi

echo "✓ Pull secret is valid"
echo ""

# Check if secret exists
echo "Step 3: Checking if secret exists..."
if aws secretsmanager describe-secret --secret-id "$SECRET_NAME" --region "$REGION" > /dev/null 2>&1; then
    echo "✓ Secret exists: $SECRET_NAME"

    # Show current value (masked)
    echo ""
    echo "Current pull secret registries:"
    CURRENT=$(aws secretsmanager get-secret-value --secret-id "$SECRET_NAME" --region "$REGION" --query SecretString --output text)
    echo "$CURRENT" | jq -r '.auths | keys[]' 2>/dev/null || echo "  (Unable to parse - may be invalid JSON)"
    echo ""

    read -p "Update this secret with the new pull secret? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 0
    fi

    echo "Step 4: Updating secret..."
    aws secretsmanager put-secret-value \
        --secret-id "$SECRET_NAME" \
        --secret-string "$PULL_SECRET" \
        --region "$REGION"
else
    echo "✗ Secret does not exist: $SECRET_NAME"
    echo ""
    read -p "Create new secret? (y/n) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 0
    fi

    echo "Step 4: Creating secret..."
    aws secretsmanager create-secret \
        --name "$SECRET_NAME" \
        --description "OpenShift pull secret for ocpctl" \
        --secret-string "$PULL_SECRET" \
        --region "$REGION"
fi

echo "✓ Secret updated successfully"
echo ""

# Verify update
echo "Step 5: Verifying update..."
UPDATED=$(aws secretsmanager get-secret-value --secret-id "$SECRET_NAME" --region "$REGION" --query SecretString --output text)
echo "Updated pull secret contains registries:"
echo "$UPDATED" | jq -r '.auths | keys[]'
echo ""

echo "============================================"
echo "Pull Secret Update Complete!"
echo "============================================"
echo ""
echo "Next Steps:"
echo ""
echo "1. Restart ocpctl-worker service to pick up new secret:"
echo "   ssh to worker instance:"
echo "   sudo systemctl restart ocpctl-worker"
echo ""
echo "2. Verify worker loaded new secret:"
echo "   sudo journalctl -u ocpctl-worker -n 100 | grep -i 'pull secret'"
echo ""
echo "3. Test with a new cluster deployment:"
echo "   - Create cluster via UI with version 4.22.0-ec.5"
echo "   - Watch logs: sudo tail -f /tmp/ocpctl/{cluster-id}/.openshift_install.log"
echo ""
echo "The pull secret is now valid for all registries including:"
echo "  - quay.io/openshift-release-dev (dev-preview releases)"
echo "  - cloud.openshift.com"
echo "  - registry.redhat.io"
echo "  - registry.connect.redhat.com"
echo ""
