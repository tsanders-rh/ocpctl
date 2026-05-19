#!/bin/bash
# Download a specific OpenShift version on-demand
# Used when worker encounters a version not yet installed

set -euo pipefail

FULL_VERSION="${1:-}"
INSTALL_DIR="${2:-/usr/local/bin}"
S3_BUCKET="${S3_BUCKET:-ocpctl-binaries}"

if [ -z "$FULL_VERSION" ]; then
    echo "Usage: $0 <full-version> [install_dir]"
    echo "Example: $0 4.22.0-rc.4 /usr/local/bin"
    exit 1
fi

# Extract major.minor
MAJOR_MINOR=$(echo "$FULL_VERSION" | cut -d- -f1 | cut -d. -f1,2)

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

log() {
    echo -e "${BLUE}[download-specific-version]${NC} $1"
}

success() {
    echo -e "${GREEN}[download-specific-version]${NC} $1"
}

error() {
    echo -e "${RED}[download-specific-version]${NC} $1" >&2
}

# Helper functions
is_dev_preview_version() {
    local version=$1
    [[ "$version" == *"-ec."* ]] || \
    [[ "$version" == *"-rc."* ]] || \
    [[ "$version" == *"-0.nightly"* ]] || \
    [[ "$version" == *"-fc."* ]]
}

get_mirror_base_path() {
    local full_version=$1
    if is_dev_preview_version "$full_version"; then
        echo "ocp-dev-preview"
    else
        echo "ocp"
    fi
}

download_from_s3() {
    local binary=$1
    local s3_path="s3://${S3_BUCKET}/installers/${FULL_VERSION}/${binary}"
    local local_path="${INSTALL_DIR}/${binary}-${MAJOR_MINOR}"

    log "Checking S3 cache..."
    if aws s3 cp "${s3_path}" "${local_path}" 2>/dev/null; then
        chmod +x "${local_path}"
        success "Downloaded ${binary} from S3 cache"
        return 0
    fi
    return 1
}

download_from_mirror() {
    local binary=$1
    local tarball_name="${binary}-linux.tar.gz"

    if [ "$binary" = "oc" ]; then
        tarball_name="openshift-client-linux.tar.gz"
    fi

    local mirror_base=$(get_mirror_base_path "$FULL_VERSION")
    local arch_path=""
    if is_dev_preview_version "$FULL_VERSION"; then
        arch_path="x86_64/"
    fi

    local mirror_url="https://mirror.openshift.com/pub/openshift-v4/${arch_path}clients/${mirror_base}/${FULL_VERSION}/${tarball_name}"
    local tmp_dir=$(mktemp -d)
    local local_path="${INSTALL_DIR}/${binary}-${MAJOR_MINOR}"

    log "Trying public mirror: ${mirror_url}"

    if curl -sL "${mirror_url}" | tar xzf - -C "${tmp_dir}" 2>/dev/null; then
        if [ -f "${tmp_dir}/${binary}" ]; then
            mv "${tmp_dir}/${binary}" "${local_path}"
            chmod +x "${local_path}"

            # Handle kubectl from oc tarball
            if [ "$binary" = "oc" ] && [ -f "${tmp_dir}/kubectl" ]; then
                mv "${tmp_dir}/kubectl" "${INSTALL_DIR}/kubectl"
                chmod +x "${INSTALL_DIR}/kubectl"
                log "Also installed kubectl"
            fi

            rm -rf "${tmp_dir}"

            # Upload to S3 for caching
            aws s3 cp "${local_path}" "s3://${S3_BUCKET}/installers/${FULL_VERSION}/${binary}" 2>/dev/null || true

            success "Downloaded ${binary} from public mirror"
            return 0
        fi
    fi

    rm -rf "${tmp_dir}"
    return 1
}

download_from_ci_release() {
    local binary=$1

    if ! command -v oc &> /dev/null; then
        error "oc CLI not found - cannot extract from CI release"
        return 1
    fi

    local release_image="quay.io/openshift-release-dev/ocp-release:${FULL_VERSION}-x86_64"
    local tmp_dir=$(mktemp -d)
    local local_path="${INSTALL_DIR}/${binary}-${MAJOR_MINOR}"

    log "Extracting from CI release image: ${release_image}"
    log "This may take 1-2 minutes..."

    if ! oc adm release extract --tools "${release_image}" --to="${tmp_dir}" 2>&1 | grep -v "warning:"; then
        rm -rf "${tmp_dir}"
        return 1
    fi

    # Find tarball
    local tarball=""
    if [ "$binary" = "openshift-install" ]; then
        tarball=$(ls "${tmp_dir}"/openshift-install-linux*.tar.gz 2>/dev/null | head -1)
    elif [ "$binary" = "oc" ]; then
        tarball=$(ls "${tmp_dir}"/openshift-client-linux*.tar.gz 2>/dev/null | head -1)
    elif [ "$binary" = "ccoctl" ]; then
        tarball=$(ls "${tmp_dir}"/ccoctl-linux*.tar.gz 2>/dev/null | head -1)
    fi

    if [ -z "$tarball" ] || [ ! -f "$tarball" ]; then
        error "${binary} tarball not found"
        rm -rf "${tmp_dir}"
        return 1
    fi

    if tar -xzf "${tarball}" -C "${tmp_dir}" 2>/dev/null; then
        if [ -f "${tmp_dir}/${binary}" ]; then
            mv "${tmp_dir}/${binary}" "${local_path}"
            chmod +x "${local_path}"

            if [ "$binary" = "oc" ] && [ -f "${tmp_dir}/kubectl" ]; then
                mv "${tmp_dir}/kubectl" "${INSTALL_DIR}/kubectl"
                chmod +x "${INSTALL_DIR}/kubectl"
                log "Also installed kubectl"
            fi

            rm -rf "${tmp_dir}"

            # Upload to S3
            aws s3 cp "${local_path}" "s3://${S3_BUCKET}/installers/${FULL_VERSION}/${binary}" 2>/dev/null || true

            success "Downloaded ${binary} from CI release stream"
            return 0
        fi
    fi

    rm -rf "${tmp_dir}"
    return 1
}

# Main download logic
main() {
    log "Downloading OpenShift ${FULL_VERSION} installer binaries..."

    local failed=0

    # Download openshift-install
    if [ -f "${INSTALL_DIR}/openshift-install-${MAJOR_MINOR}" ]; then
        log "✓ openshift-install-${MAJOR_MINOR} already exists"
    else
        log "Downloading openshift-install..."
        if ! download_from_s3 "openshift-install"; then
            if ! download_from_mirror "openshift-install"; then
                if ! download_from_ci_release "openshift-install"; then
                    error "Failed to download openshift-install from all sources"
                    failed=1
                fi
            fi
        fi
    fi

    # Download ccoctl (non-fatal)
    if [ -f "${INSTALL_DIR}/ccoctl-${MAJOR_MINOR}" ]; then
        log "✓ ccoctl-${MAJOR_MINOR} already exists"
    else
        log "Downloading ccoctl..."
        if ! download_from_s3 "ccoctl"; then
            if ! download_from_mirror "ccoctl"; then
                if ! download_from_ci_release "ccoctl"; then
                    log "Warning: Failed to download ccoctl (non-fatal)"
                fi
            fi
        fi
    fi

    # Download oc (non-fatal)
    if [ -f "${INSTALL_DIR}/oc-${MAJOR_MINOR}" ]; then
        log "✓ oc-${MAJOR_MINOR} already exists"
    else
        log "Downloading oc..."
        if ! download_from_s3 "oc"; then
            if ! download_from_mirror "oc"; then
                if ! download_from_ci_release "oc"; then
                    log "Warning: Failed to download oc (non-fatal)"
                fi
            fi
        fi
    fi

    if [ $failed -eq 1 ]; then
        error "Failed to download required binaries"
        exit 1
    fi

    success "Successfully downloaded OpenShift ${FULL_VERSION} installer binaries"

    # Verify installed version
    if [ -f "${INSTALL_DIR}/openshift-install-${MAJOR_MINOR}" ]; then
        log "Verifying installed version..."
        ACTUAL_VERSION=$("${INSTALL_DIR}/openshift-install-${MAJOR_MINOR}" version 2>/dev/null | head -1 | awk '{print $2}' || echo "unknown")
        log "Installed version: ${ACTUAL_VERSION}"

        if [ "$ACTUAL_VERSION" != "$FULL_VERSION" ]; then
            log "WARNING: Requested ${FULL_VERSION} but installed ${ACTUAL_VERSION}"
        fi
    fi
}

main "$@"
