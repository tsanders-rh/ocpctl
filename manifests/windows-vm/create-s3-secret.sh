#!/bin/bash
#
# Create the S3 credentials secret for Windows image import
#
# This script prompts for AWS credentials and creates the
# windows-image-s3-creds secret used by CDI to pull the
# Windows QCOW2 image from S3.
#
# Usage:
#   ./create-s3-secret.sh
#

set -euo pipefail

NAMESPACE="openshift-virtualization-os-images"
SECRET_NAME="windows-image-s3-creds"

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Windows Image S3 Credentials Setup"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Pre-flight: must be logged into the cluster
if ! oc whoami &>/dev/null; then
  echo "ERROR: Not logged into an OpenShift cluster."
  echo "Run: oc login <cluster-url>"
  exit 1
fi

# Pre-flight: verify AWS credentials work before storing them
echo "Enter your AWS credentials. These will be stored as a Kubernetes"
echo "secret and used by CDI to download the Windows image from S3."
echo ""

read -rp "AWS Access Key ID:     " AWS_ACCESS_KEY_ID
if [ -z "$AWS_ACCESS_KEY_ID" ]; then
  echo "ERROR: Access Key ID cannot be empty."
  exit 1
fi

read -rsp "AWS Secret Access Key: " AWS_SECRET_ACCESS_KEY
echo ""
if [ -z "$AWS_SECRET_ACCESS_KEY" ]; then
  echo "ERROR: Secret Access Key cannot be empty."
  exit 1
fi

echo ""
echo "Verifying credentials against AWS..."
if ! IDENTITY=$(AWS_ACCESS_KEY_ID="$AWS_ACCESS_KEY_ID" \
                AWS_SECRET_ACCESS_KEY="$AWS_SECRET_ACCESS_KEY" \
                aws sts get-caller-identity --output text --query 'Arn' 2>&1); then
  echo ""
  echo "ERROR: AWS credentials are invalid."
  echo "  $IDENTITY"
  echo ""
  echo "Check your keys and try again."
  exit 1
fi
echo "✓ Credentials verified: $IDENTITY"

# Create namespace if it doesn't exist
if ! oc get namespace "$NAMESPACE" &>/dev/null; then
  echo ""
  echo "Creating namespace $NAMESPACE..."
  oc create namespace "$NAMESPACE"
  echo "✓ Namespace created"
fi

# Create or replace the secret
echo ""
if oc get secret "$SECRET_NAME" -n "$NAMESPACE" &>/dev/null; then
  echo "Secret '$SECRET_NAME' already exists — replacing it..."
  oc delete secret "$SECRET_NAME" -n "$NAMESPACE"
fi

oc create secret generic "$SECRET_NAME" \
  -n "$NAMESPACE" \
  --from-literal=accessKeyId="$AWS_ACCESS_KEY_ID" \
  --from-literal=secretKey="$AWS_SECRET_ACCESS_KEY"

echo "✓ Secret '$SECRET_NAME' created in namespace '$NAMESPACE'"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Next step: import the Windows image:"
echo "  ./2_setup-storageclass.sh --watch"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
