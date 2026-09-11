#!/bin/bash
#
# Get cluster infraID and region for IRSA setup
#
# This helper script extracts the infraID and region from a running cluster
# so you can use them with setup-irsa.sh
#
# Usage:
#   ./get-cluster-info.sh
#
# Prerequisites:
# - oc CLI must be installed and logged into the cluster
#

set -e

echo "Getting cluster information..."
echo ""

# Get infraID from cluster
INFRA_ID=$(oc get infrastructure cluster -o jsonpath='{.status.infrastructureName}')
if [ -z "$INFRA_ID" ]; then
  echo "ERROR: Could not get infraID from cluster"
  echo "Make sure you're logged in: oc login <cluster-url>"
  exit 1
fi

# Get region from cluster
REGION=$(oc get infrastructure cluster -o jsonpath='{.status.platformStatus.aws.region}')
if [ -z "$REGION" ]; then
  echo "ERROR: Could not get region from cluster"
  echo "Is this an AWS cluster?"
  exit 1
fi

# Get cluster name
CLUSTER_NAME=$(oc get infrastructure cluster -o jsonpath='{.status.apiServerURL}' | sed 's|https://api\.||; s|:.*||; s|\..*||')

# Get OIDC provider info - try multiple paths used by different cluster types
OIDC_ISSUER=$(oc get authentication.config.openshift.io cluster \
  -o jsonpath='{.spec.serviceAccountIssuer}' 2>/dev/null || true)

# Fallback: some clusters expose it via the infrastructure object
if [ -z "$OIDC_ISSUER" ]; then
  OIDC_ISSUER=$(oc get infrastructure cluster \
    -o jsonpath='{.status.platformStatus.aws.serviceEndpoints[?(@.name=="sts")].url}' 2>/dev/null || true)
fi

echo "Cluster Information:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Cluster Name: $CLUSTER_NAME"
echo "Infrastructure ID: $INFRA_ID"
echo "Region: $REGION"
if [ -n "$OIDC_ISSUER" ]; then
  echo "OIDC Issuer: $OIDC_ISSUER"
else
  echo "OIDC Issuer: (not found - cluster may not use STS/OIDC mode)"
fi
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "To setup IRSA, run:"
if [ -n "$OIDC_ISSUER" ]; then
  echo "  ./setup-irsa.sh $INFRA_ID $REGION $OIDC_ISSUER"
else
  echo "  ./setup-irsa.sh $INFRA_ID $REGION"
  echo ""
  echo "Note: OIDC Issuer was not found. setup-irsa.sh will use the standard"
  echo "IPI STS pattern. If that fails, check the issuer manually:"
  echo "  oc get authentication.config.openshift.io cluster -o jsonpath='{.spec.serviceAccountIssuer}'"
fi
echo ""
