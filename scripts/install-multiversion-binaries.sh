#!/bin/bash
# Install openshift-install and ccoctl binaries for multiple OpenShift versions
# Supports: 4.18, 4.19, 4.20, 4.21, 4.22 (dev-preview)

set -e

VERSIONS=("4.18" "4.19" "4.20" "4.21" "4.22")
INSTALL_DIR="/usr/local/bin"

echo "OpenShift Multi-Version Binary Installer"
echo "========================================"
echo ""
echo "This script will install openshift-install and ccoctl binaries for:"
echo "  - OpenShift 4.18 (Kubernetes 1.31)"
echo "  - OpenShift 4.19 (Kubernetes 1.32)"
echo "  - OpenShift 4.20 (Kubernetes 1.33)"
echo "  - OpenShift 4.21 (Kubernetes 1.34)"
echo "  - OpenShift 4.22 (Developer Preview - Early Candidate)"
echo ""
echo "Installation directory: $INSTALL_DIR"
echo "Total download size: ~6GB"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
  echo "ERROR: This script must be run as root (or with sudo)"
  echo "Usage: sudo $0"
  exit 1
fi

# Create temporary directory
TMP_DIR=$(mktemp -d)
trap "rm -rf $TMP_DIR" EXIT

cd "$TMP_DIR"

for VERSION in "${VERSIONS[@]}"; do
  echo "======================================"
  echo "Installing OpenShift $VERSION binaries"
  echo "======================================"

  # Determine mirror path and version based on whether this is dev-preview
  if [ "$VERSION" = "4.22" ]; then
    MIRROR_PATH="ocp-dev-preview"
    FULL_VERSION="4.22.0-ec.5"
    echo "Using dev-preview version: $FULL_VERSION"
  else
    MIRROR_PATH="ocp"
    FULL_VERSION="stable-$VERSION"
  fi

  # Download openshift-install
  # Use RHEL9 tarball for 4.22 (FIPS-enabled)
  # Dev-preview versions use x86_64/clients path structure
  if [ "$VERSION" = "4.22" ]; then
    INSTALLER_TARBALL="openshift-install-rhel9-amd64.tar.gz"
    ARCH_PATH="x86_64/"
  else
    INSTALLER_TARBALL="openshift-install-linux.tar.gz"
    ARCH_PATH=""
  fi

  echo "Downloading openshift-install-$VERSION ($INSTALLER_TARBALL)..."
  if ! wget -q --show-progress https://mirror.openshift.com/pub/openshift-v4/${ARCH_PATH}clients/$MIRROR_PATH/$FULL_VERSION/$INSTALLER_TARBALL; then
    echo "ERROR: Failed to download openshift-install for version $VERSION"
    exit 1
  fi

  echo "Extracting openshift-install-$VERSION..."
  tar xzf $INSTALLER_TARBALL
  # RHEL9 FIPS tarball contains openshift-install-fips
  if [ "$VERSION" = "4.22" ]; then
    mv openshift-install-fips "$INSTALL_DIR/openshift-install-$VERSION"
  else
    mv openshift-install "$INSTALL_DIR/openshift-install-$VERSION"
  fi
  chmod +x "$INSTALL_DIR/openshift-install-$VERSION"
  rm $INSTALLER_TARBALL README.md 2>/dev/null || true

  echo "✓ Installed openshift-install-$VERSION"

  # Download ccoctl
  # Use RHEL9 tarball for 4.22 (FIPS-enabled, matches installer.go expectations)
  if [ "$VERSION" = "4.22" ]; then
    CCOCTL_TARBALL="ccoctl-linux-rhel9.tar.gz"
    CCOCTL_BINARY_NAME="ccoctl-$VERSION-rhel9"
  else
    CCOCTL_TARBALL="ccoctl-linux.tar.gz"
    CCOCTL_BINARY_NAME="ccoctl-$VERSION"
  fi

  echo "Downloading $CCOCTL_BINARY_NAME ($CCOCTL_TARBALL)..."
  if ! wget -q --show-progress https://mirror.openshift.com/pub/openshift-v4/${ARCH_PATH}clients/$MIRROR_PATH/$FULL_VERSION/$CCOCTL_TARBALL; then
    echo "ERROR: Failed to download ccoctl for version $VERSION"
    exit 1
  fi

  echo "Extracting $CCOCTL_BINARY_NAME..."
  tar xzf $CCOCTL_TARBALL
  mv ccoctl "$INSTALL_DIR/$CCOCTL_BINARY_NAME"
  chmod +x "$INSTALL_DIR/$CCOCTL_BINARY_NAME"
  rm $CCOCTL_TARBALL 2>/dev/null || true

  echo "✓ Installed $CCOCTL_BINARY_NAME"
  echo ""
done

echo "======================================"
echo "Installation Complete!"
echo "======================================"
echo ""
echo "Installed binaries:"
ls -lh "$INSTALL_DIR/openshift-install-"* "$INSTALL_DIR/ccoctl-"* 2>/dev/null || true
echo ""
echo "Verifying installations:"
echo ""

for VERSION in "${VERSIONS[@]}"; do
  echo "--- OpenShift $VERSION ---"
  "$INSTALL_DIR/openshift-install-$VERSION" version || echo "ERROR: openshift-install-$VERSION failed"

  # Use RHEL9 binary name for 4.22
  if [ "$VERSION" = "4.22" ]; then
    "$INSTALL_DIR/ccoctl-$VERSION-rhel9" version || echo "ERROR: ccoctl-$VERSION-rhel9 failed"
  else
    "$INSTALL_DIR/ccoctl-$VERSION" version || echo "ERROR: ccoctl-$VERSION failed"
  fi
  echo ""
done

echo "======================================"
echo "Next Steps:"
echo "======================================"
echo ""
echo "1. Restart the ocpctl worker service:"
echo "   sudo systemctl restart ocpctl-worker"
echo ""
echo "2. Verify worker logs show multi-version support:"
echo "   sudo journalctl -u ocpctl-worker -f"
echo ""
echo "3. Create clusters with different versions through the web UI"
echo "   Available stable versions: 4.18.35, 4.19.23, 4.20.3, 4.20.4, 4.20.5"
echo "   Available dev-preview: 4.22.0-ec.5 (RHEL9 FIPS-enabled)"
echo ""
