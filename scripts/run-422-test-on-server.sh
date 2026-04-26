#!/bin/bash
# Helper script to run the 4.22.0-ec.5 test on the API server
#
# SECURITY: Pull secret must be provided via environment variable
# Usage: OPENSHIFT_PULL_SECRET="$(cat ~/.openshift/pull-secret.json)" ./run-422-test-on-server.sh

set -e

echo "Deploying and running 4.22.0-ec.5 RHEL9 FIPS direct test on API server..."
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

# Validate pull secret is valid JSON
if ! echo "$OPENSHIFT_PULL_SECRET" | jq . > /dev/null 2>&1; then
    echo "ERROR: OPENSHIFT_PULL_SECRET is not valid JSON"
    exit 1
fi

PULL_SECRET="$OPENSHIFT_PULL_SECRET"

# Run on remote server (pipe 'y' to auto-confirm cluster creation)
ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78 bash <<EOF
export OPENSHIFT_PULL_SECRET='$PULL_SECRET'
echo 'y' | /tmp/test-422-ec5-direct.sh test-422-ec5-verify
EOF
