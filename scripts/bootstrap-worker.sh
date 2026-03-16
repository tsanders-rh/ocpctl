#!/bin/bash
# Bootstrap script for autoscaling workers
# Usage: /opt/ocpctl/bootstrap.sh <version>
# This script should be installed in the base AMI

set -e

VERSION="${1:-latest}"
S3_BUCKET="s3://ocpctl-binaries"
REMOTE_BASE="/opt/ocpctl"

echo "OCPCTL Worker Bootstrap"
echo "Version: ${VERSION}"

# Resolve "latest" to actual version
if [ "$VERSION" = "latest" ]; then
    echo "Resolving 'latest' version from S3..."
    VERSION=$(aws s3 cp ${S3_BUCKET}/LATEST - 2>/dev/null || echo "")

    if [ -z "$VERSION" ]; then
        echo "ERROR: Could not resolve latest version from ${S3_BUCKET}/LATEST"
        exit 1
    fi

    echo "Latest version: ${VERSION}"
fi

# Check if this version is already installed
if [ -L "${REMOTE_BASE}/current" ]; then
    CURRENT_VERSION=$(readlink ${REMOTE_BASE}/current | xargs basename)
    if [ "$CURRENT_VERSION" = "$VERSION" ]; then
        echo "Version ${VERSION} already installed and current"
        exit 0
    fi
fi

# Create versioned directory
mkdir -p ${REMOTE_BASE}/releases/${VERSION}

# Download binary from S3
echo "Downloading ocpctl-worker ${VERSION} from S3..."
if aws s3 cp ${S3_BUCKET}/releases/${VERSION}/ocpctl-worker \
    ${REMOTE_BASE}/releases/${VERSION}/ocpctl-worker; then
    chmod +x ${REMOTE_BASE}/releases/${VERSION}/ocpctl-worker
    echo "✓ Binary downloaded successfully"
else
    echo "ERROR: Failed to download binary from S3"
    exit 1
fi

# Verify version in binary
BINARY_VERSION=$(${REMOTE_BASE}/releases/${VERSION}/ocpctl-worker --version 2>&1 | head -1 | awk '{print $3}')
if [ "$BINARY_VERSION" != "$VERSION" ]; then
    echo "WARNING: Binary version mismatch. Expected: ${VERSION}, Got: ${BINARY_VERSION}"
fi

# Update symlink atomically
ln -snf ${REMOTE_BASE}/releases/${VERSION} ${REMOTE_BASE}/current

echo "✓ Symlink updated: ${REMOTE_BASE}/current -> ${REMOTE_BASE}/releases/${VERSION}"

# Verify symlink
CURRENT=$(readlink ${REMOTE_BASE}/current)
echo "Current version: $(basename ${CURRENT})"

# Sync profiles from S3
echo "Syncing profiles from S3..."
mkdir -p ${REMOTE_BASE}/profiles
if aws s3 sync ${S3_BUCKET}/profiles/ ${REMOTE_BASE}/profiles/; then
    echo "✓ Profiles synced successfully"
    PROFILE_COUNT=$(ls -1 ${REMOTE_BASE}/profiles/*.yaml 2>/dev/null | wc -l)
    echo "  Found ${PROFILE_COUNT} profiles"
else
    echo "WARNING: Failed to sync profiles from S3"
fi

# Cleanup old versions (keep last 3)
echo "Cleaning up old releases (keeping last 3)..."
cd ${REMOTE_BASE}/releases
ls -t | tail -n +4 | xargs -I {} rm -rf {}
cd - > /dev/null

echo "✓ Bootstrap complete"
