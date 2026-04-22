#!/bin/bash
# Update OpenShift pull secret in AWS Secrets Manager
# Usage: ./update-pull-secret.sh

set -e

REGION="${AWS_REGION:-us-west-2}"
SECRET_NAME="${OPENSHIFT_PULL_SECRET_NAME:-ocpctl/pull-secret}"

echo "============================================"
echo "Update OpenShift Pull Secret"
echo "============================================"
echo "Region: $REGION"
echo "Secret Name: $SECRET_NAME"
echo ""

# The valid pull secret
PULL_SECRET='{"auths":{"cloud.openshift.com":{"auth":"***REMOVED***K3JobmVuZ2luZWVyaW5ndHNhbmRlcnMxbnNqMGdnNWwzNWlyZ3ZubW9keG5jaDB3NXA6NUs1OEZPTUNCRDZHRkZKVzQ2WU03VVc3TlFDRk03S0wxRU05VlZBUlRGNEpUUDdVWlJUTFFGVjRJWDJEUFg4Mw==","email":"tsanders@redhat.com"},"quay.io":{"auth":"***REMOVED***K3JobmVuZ2luZWVyaW5ndHNhbmRlcnMxbnNqMGdnNWwzNWlyZ3ZubW9keG5jaDB3NXA6NUs1OEZPTUNCRDZHRkZKVzQ2WU03VVc3TlFDRk03S0wxRU05VlZBUlRGNEpUUDdVWlJUTFFGVjRJWDJEUFg4Mw==","email":"tsanders@redhat.com"},"registry.connect.redhat.com":{"auth":"***REMOVED***MU5zajBHRzVsMzVpUmd2Tm1PZFhuY2gwVzVwOmV5SmhiR2NpT2lKU1V6VXhNaUo5LmV5SnpkV0lpT2lKaFpEQXlOVFJpTm1SbU1UazBNamN6T0RVMVltRTNOelExTURsak5tUXhNaUo5LnM0MTU4NUdJZmVmTGYxMkduZ2ZVNlEzZFNEclY2cGhwUVJuRHh0NFlTbFFhbjg4cTdCRXBGYkpDYUkwQnkzdXRfQUYzc0twV1ZONU9mOVBuXzRmQzNhVDRSc2JINnozMTVrRlgwcVZRaGpOMWFZQnl1S3JENWtkVkJUd05vSHVkWm5qUDdHNlVkNHctSVZ1VkVpNGN4NDUxVkh0QlQ3TlZVVENELWpjNnZIM1VmSmRVaHdLaDJsY2U0emFsUTZqekdJcjg3NXlLTWJBM2tQQTRfZUkzTVRDbC13T0FYLXZGNkl5SWdoWG9mdFEzVXBjbE03cEFpcGE5ZjZoNUJMdDB5UVM4OFE3cE9Nby1FWFJ0eGtYc3RqMXU0X3NVOGM3T2ZHVWpIazM1Y0tLRkJac2pLZ0RITTM5bWphbVF4dk8yWVhpbDJFc0VLLV90ZXlzNXF5eXdzWE0yWGZoTXdCVkhyc3hJMzktSnZaV041Y0hWUTNVYjQtRi0wVnVTbU55X0JpTkkyN2Vvb09EdWYzWFRVU2RlbHUwRlN2c0l5cG5QUjZ2MVNtMEZCN1ZlNGZmQzRvc2lXTHFVOElscU9XQTkwZEJ6eWgybVFUNVRlR3FyTzhydzVpRHdiMUVCTGhEb2dHTlNmNlljZkE2Ulk3bWJnbURhVktQSlBaZFhERDVkZEhmSGg3T0pjS2xVTHhnSVpiekpLcXZ0MGRyaXBxV1lrS2djeVNHa1NrVGEzYWthS3Z3TDRHTm43Mi1ZUE50Z192VnRJZ3VTbkthaGF0SmVpdzJvYnVPRndtMFUwc2J1U0JiWmFCdHZFcEFNcWhNVXhFYWRJZk5BX08yZWJ2bnRFOHFhdTR2MVpnRWh4bzZYX1pzV2dudVVQT3FuZTFHNllYb3d1ZS14Y2F3","email":"tsanders@redhat.com"},"registry.redhat.io":{"auth":"***REMOVED***MU5zajBHRzVsMzVpUmd2Tm1PZFhuY2gwVzVwOmV5SmhiR2NpT2lKU1V6VXhNaUo5LmV5SnpkV0lpT2lKaFpEQXlOVFJpTm1SbU1UazBNamN6T0RVMVltRTNOelExTURsak5tUXhNaUo5LnM0MTU4NUdJZmVmTGYxMkduZ2ZVNlEzZFNEclY2cGhwUVJuRHh0NFlTbFFhbjg4cTdCRXBGYkpDYUkwQnkzdXRfQUYzc0twV1ZONU9mOVBuXzRmQzNhVDRSc2JINnozMTVrRlgwcVZRaGpOMWFZQnl1S3JENWtkVkJUd05vSHVkWm5qUDdHNlVkNHctSVZ1VkVpNGN4NDUxVkh0QlQ3TlZVVENELWpjNnZIM1VmSmRVaHdLaDJsY2U0emFsUTZqekdJcjg3NXlLTWJBM2tQQTRfZUkzTVRDbC13T0FYLXZGNkl5SWdoWG9mdFEzVXBjbE03cEFpcGE5ZjZoNUJMdDB5UVM4OFE3cE9Nby1FWFJ0eGtYc3RqMXU0X3NVOGM3T2ZHVWpIazM1Y0tLRkJac2pLZ0RITTM5bWphbVF4dk8yWVhpbDJFc0VLLV90ZXlzNXF5eXdzWE0yWGZoTXdCVkhyc3hJMzktSnZaV041Y0hWUTNVYjQtRi0wVnVTbU55X0JpTkkyN2Vvb09EdWYzWFRVU2RlbHUwRlN2c0l5cG5QUjZ2MVNtMEZCN1ZlNGZmQzRvc2lXTHFVOElscU9XQTkwZEJ6eWgybVFUNVRlR3FyTzhydzVpRHdiMUVCTGhEb2dHTlNmNlljZkE2Ulk3bWJnbURhVktQSlBaZFhERDVkZEhmSGg3T0pjS2xVTHhnSVpiekpLcXZ0MGRyaXBxV1lrS2djeVNHa1NrVGEzYWthS3Z3TDRHTm43Mi1ZUE50Z192VnRJZ3VTbkthaGF0SmVpdzJvYnVPRndtMFUwc2J1U0JiWmFCdHZFcEFNcWhNVXhFYWRJZk5BX08yZWJ2bnRFOHFhdTR2MVpnRWh4bzZYX1pzV2dudVVQT3FuZTFHNllYb3d1ZS14Y2F3","email":"tsanders@redhat.com"}}}'

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
