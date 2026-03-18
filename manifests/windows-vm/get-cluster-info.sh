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

# Get OIDC provider info
OIDC_ISSUER=$(oc get authentication.config.openshift.io cluster -o jsonpath='{.spec.serviceAccountIssuer}')

echo "Cluster Information:"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Cluster Name: $CLUSTER_NAME"
echo "Infrastructure ID: $INFRA_ID"
echo "Region: $REGION"
echo "OIDC Issuer: $OIDC_ISSUER"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "To setup IRSA, run:"
echo "  ./setup-irsa.sh $INFRA_ID $REGION"
echo ""
