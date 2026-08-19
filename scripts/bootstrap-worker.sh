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

# Create runtime directories used by the worker.
# TMPDIR (worker.env) points at /var/lib/ocpctl/tmp and openshift-install writes
# its bootstrap ignition + CAPI/envtest scratch files there; a missing dir makes
# a create fail with ENOENT. WORKER_WORK_DIR uses /var/lib/ocpctl/clusters.
# user-data-worker.sh creates these on first boot, but ASG relaunches run this
# script directly, so create them here too. Owned by the worker service user.
mkdir -p /var/lib/ocpctl/tmp /var/lib/ocpctl/clusters
if id ocpctl >/dev/null 2>&1; then
    chown -R ocpctl:ocpctl /var/lib/ocpctl
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

# Sync manifests from S3
echo "Syncing manifests from S3..."
mkdir -p ${REMOTE_BASE}/manifests
if aws s3 sync ${S3_BUCKET}/manifests/ ${REMOTE_BASE}/manifests/; then
    echo "✓ Manifests synced successfully"
    # Set execute permissions on all scripts
    chmod -R 755 ${REMOTE_BASE}/manifests
    MANIFEST_COUNT=$(find ${REMOTE_BASE}/manifests -type f 2>/dev/null | wc -l)
    echo "  Found ${MANIFEST_COUNT} manifest files"
else
    echo "WARNING: Failed to sync manifests from S3"
fi

# Download GCP credentials from S3
echo "Downloading GCP credentials from S3..."
if aws s3 cp ${S3_BUCKET}/config/gcp-credentials.json ${REMOTE_BASE}/gcp-credentials.json; then
    chmod 600 ${REMOTE_BASE}/gcp-credentials.json
    echo "✓ GCP credentials downloaded"
else
    echo "WARNING: Failed to download GCP credentials from S3 (OK if not using GCP)"
fi


# Download ensure-installers script
echo "Downloading ensure-installers script from S3..."
mkdir -p ${REMOTE_BASE}/scripts
if aws s3 cp ${S3_BUCKET}/scripts/ensure-installers.sh ${REMOTE_BASE}/scripts/ensure-installers.sh; then
    chmod +x ${REMOTE_BASE}/scripts/ensure-installers.sh
    echo "✓ ensure-installers.sh downloaded and made executable"
else
    echo "WARNING: Failed to download ensure-installers.sh from S3"
fi

# Download cloud CLI login hooks referenced by the systemd unit's ExecStartPre.
# These are baked into the AMI, but the unit runs whatever is at these paths;
# refresh them from S3 on every boot so autoscale workers pick up fixes without
# an AMI rebuild. In particular azure-login.sh writes ~/.azure/osServicePrincipal.json
# which openshift-install needs for Azure IPI (without it: "failed to retrieve
# credentials from user: EOF").
for hook in azure-login.sh ibmcloud-login.sh; do
    echo "Downloading ${hook} from S3..."
    if aws s3 cp ${S3_BUCKET}/scripts/${hook} ${REMOTE_BASE}/scripts/${hook}; then
        chmod +x ${REMOTE_BASE}/scripts/${hook}
        echo "✓ ${hook} downloaded and made executable"
    else
        echo "WARNING: Failed to download ${hook} from S3 (unit may use stale AMI copy)"
    fi
done

# Refresh the systemd unit from S3 so autoscale workers pick up unit changes
# without an AMI rebuild. user-data-worker.sh only installs the unit when it is
# absent (if [ ! -f ]), so a stale AMI-baked unit would otherwise persist
# forever -- e.g. one missing the ensure-installers.sh ExecStartPre, which is
# what left autoscale workers without Azure credentials. Only reload (and, if
# already running, restart) when the unit actually changed.
UNIT_PATH="/etc/systemd/system/ocpctl-worker.service"
echo "Refreshing systemd unit from S3..."
if aws s3 cp ${S3_BUCKET}/scripts/ocpctl-worker.service ${UNIT_PATH}.new; then
    if ! cmp -s ${UNIT_PATH}.new "${UNIT_PATH}"; then
        mv ${UNIT_PATH}.new "${UNIT_PATH}"
        systemctl daemon-reload
        echo "✓ systemd unit updated and daemon reloaded"
        # On an ASG relaunch bootstrap may run against an already-active service;
        # restart so the new unit takes effect. On first boot the service is not
        # yet started (user-data starts it after this script), so this is skipped.
        if systemctl is-active --quiet ocpctl-worker; then
            systemctl restart ocpctl-worker
            echo "✓ ocpctl-worker restarted to apply new unit"
        fi
    else
        rm -f ${UNIT_PATH}.new
        echo "✓ systemd unit already up to date"
    fi
else
    rm -f ${UNIT_PATH}.new
    echo "WARNING: Failed to download systemd unit from S3 (using existing unit)"
fi

# Cleanup old versions (keep last 3)
echo "Cleaning up old releases (keeping last 3)..."
cd ${REMOTE_BASE}/releases
ls -t | tail -n +4 | xargs -I {} rm -rf {}
cd - > /dev/null

echo "✓ Bootstrap complete"
