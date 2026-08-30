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

# Ensure the install dir exists. On-demand downloads may target an
# ocpctl-writable dir (e.g. /opt/ocpctl/bin) that isn't pre-created; without
# this, the mv of the extracted binary fails.
mkdir -p "$INSTALL_DIR"

# Extract major.minor
MAJOR_MINOR=$(echo "$FULL_VERSION" | cut -d- -f1 | cut -d. -f1,2)
MAJOR=$(echo "$FULL_VERSION" | cut -d. -f1)

# registry.ci release repo for nightly payloads. 4.x nightlies live under
# "ocp/release"; 5.x (and later) live under a per-major stream
# "ocp/release-<major>" (e.g. "ocp/release-5"). Using the wrong repo yields a
# "manifest unknown" pull error. Keep in sync with registryCIReleaseRepo() in
# internal/installer/nightly.go.
REGISTRY_CI_RELEASE_REPO="registry.ci.openshift.org/ocp/release"
if [[ "$MAJOR" =~ ^[0-9]+$ ]] && [ "$MAJOR" -ge 5 ]; then
    REGISTRY_CI_RELEASE_REPO="registry.ci.openshift.org/ocp/release-${MAJOR}"
fi

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
    [[ "$version" == *"-0.nightly"* ]] || \
    [[ "$version" == *"-fc."* ]]
}

# Nightly builds (e.g. 4.23.0-0.nightly-2026-08-25-215343) exist ONLY in
# registry.ci.openshift.org — they are never published to the public mirror or
# quay ocp-release — so they need a dedicated extraction path.
is_nightly_version() {
    [[ "$1" == *"-0.nightly-"* ]]
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
    local local_path="${INSTALL_DIR}/${binary}-${FULL_VERSION}"

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
    local local_path="${INSTALL_DIR}/${binary}-${FULL_VERSION}"

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

    # Check if pull secret is available (required for CI releases)
    if [ -z "${OPENSHIFT_PULL_SECRET:-}" ]; then
        error "OPENSHIFT_PULL_SECRET not set - cannot extract from CI release"
        return 1
    fi

    local release_image="quay.io/openshift-release-dev/ocp-release:${FULL_VERSION}-x86_64"
    local tmp_dir=$(mktemp -d)
    local local_path="${INSTALL_DIR}/${binary}-${FULL_VERSION}"
    local pull_secret_file="${tmp_dir}/pull-secret.json"

    # Write pull secret to temp file for oc command
    echo "$OPENSHIFT_PULL_SECRET" > "$pull_secret_file"

    log "Extracting from CI release image: ${release_image}"
    log "This may take 1-2 minutes..."

    # Capture output and check oc's own exit status; piping through
    # `grep -v warning:` under `set -o pipefail` can turn a clean (output-less)
    # success into a false failure (grep exits 1 on empty input).
    local extract_log="${tmp_dir}/extract.log"
    if ! oc adm release extract --tools "${release_image}" --to="${tmp_dir}" --registry-config="${pull_secret_file}" >"${extract_log}" 2>&1; then
        grep -v "warning:" "${extract_log}" >&2 || true
        rm -rf "${tmp_dir}"
        return 1
    fi
    grep -v "warning:" "${extract_log}" >&2 || true
    rm -f "${extract_log}"

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

# Extract a single binary from a nightly payload in registry.ci.openshift.org.
# Unlike download_from_ci_release (which pulls the multi-tool tarball from quay
# ocp-release), nightly payloads only live in registry.ci, and `oc adm release
# extract --command=<name>` pulls the one binary directly (no tarball).
download_from_registry_ci() {
    local binary=$1

    if ! command -v oc &> /dev/null; then
        error "oc CLI not found - cannot extract from registry.ci"
        return 1
    fi

    if [ -z "${OPENSHIFT_PULL_SECRET:-}" ]; then
        error "OPENSHIFT_PULL_SECRET not set - cannot extract from registry.ci"
        return 1
    fi

    local release_image="${REGISTRY_CI_RELEASE_REPO}:${FULL_VERSION}"
    local tmp_dir=$(mktemp -d)
    local local_path="${INSTALL_DIR}/${binary}-${FULL_VERSION}"
    local pull_secret_file="${tmp_dir}/pull-secret.json"

    echo "$OPENSHIFT_PULL_SECRET" > "$pull_secret_file"

    log "Extracting ${binary} from registry.ci payload: ${release_image}"
    log "This may take 1-2 minutes..."

    # --command extracts the named binary directly into the target dir.
    # NOTE: capture output to a file and check oc's own exit status. Do NOT pipe
    # oc through `grep -v warning:` — on a clean success oc prints nothing, grep
    # then exits 1 on empty input, and `set -o pipefail` would turn that into a
    # false "extract failed" even though the binary extracted fine.
    local extract_log="${tmp_dir}/extract.log"
    if ! oc adm release extract --command="${binary}" --to="${tmp_dir}" "${release_image}" --registry-config="${pull_secret_file}" >"${extract_log}" 2>&1; then
        grep -v "warning:" "${extract_log}" >&2 || true
        error "oc adm release extract failed for ${binary} from ${release_image} (registry.ci token may be expired)"
        rm -rf "${tmp_dir}"
        return 1
    fi
    grep -v "warning:" "${extract_log}" >&2 || true
    rm -f "${extract_log}"

    if [ -f "${tmp_dir}/${binary}" ]; then
        mv "${tmp_dir}/${binary}" "${local_path}"
        chmod +x "${local_path}"
        rm -rf "${tmp_dir}"

        # Cache in S3 for future workers.
        aws s3 cp "${local_path}" "s3://${S3_BUCKET}/installers/${FULL_VERSION}/${binary}" 2>/dev/null || true

        success "Extracted ${binary} from registry.ci payload"
        return 0
    fi

    error "${binary} not found in registry.ci payload after extract"
    rm -rf "${tmp_dir}"
    return 1
}

# Download one binary trying sources in the right order for this version.
# For nightlies, S3 cache -> registry.ci (mirror/quay have no nightlies).
# Otherwise, S3 cache -> public mirror -> quay ocp-release.
download_binary() {
    local binary=$1

    if download_from_s3 "$binary"; then
        return 0
    fi

    if is_nightly_version "$FULL_VERSION"; then
        download_from_registry_ci "$binary"
        return $?
    fi

    if download_from_mirror "$binary"; then
        return 0
    fi
    download_from_ci_release "$binary"
}

# Main download logic
main() {
    log "Downloading OpenShift ${FULL_VERSION} installer binaries..."

    local failed=0

    # Download openshift-install (fatal)
    if [ -f "${INSTALL_DIR}/openshift-install-${FULL_VERSION}" ]; then
        log "✓ openshift-install-${FULL_VERSION} already exists"
    else
        log "Downloading openshift-install..."
        if ! download_binary "openshift-install"; then
            error "Failed to download openshift-install from all sources"
            failed=1
        fi
    fi

    # Download ccoctl (non-fatal)
    if [ -f "${INSTALL_DIR}/ccoctl-${FULL_VERSION}" ]; then
        log "✓ ccoctl-${FULL_VERSION} already exists"
    else
        log "Downloading ccoctl..."
        if ! download_binary "ccoctl"; then
            log "Warning: Failed to download ccoctl (non-fatal)"
        fi
    fi

    # Download oc (non-fatal)
    if [ -f "${INSTALL_DIR}/oc-${FULL_VERSION}" ]; then
        log "✓ oc-${FULL_VERSION} already exists"
    else
        log "Downloading oc..."
        if ! download_binary "oc"; then
            log "Warning: Failed to download oc (non-fatal)"
        fi
    fi

    if [ $failed -eq 1 ]; then
        error "Failed to download required binaries"
        exit 1
    fi

    success "Successfully downloaded OpenShift ${FULL_VERSION} installer binaries"

    # Verify installed version
    if [ -f "${INSTALL_DIR}/openshift-install-${FULL_VERSION}" ]; then
        log "Verifying installed version..."
        ACTUAL_VERSION=$("${INSTALL_DIR}/openshift-install-${FULL_VERSION}" version 2>/dev/null | head -1 | awk '{print $2}' || echo "unknown")
        log "Installed version: ${ACTUAL_VERSION}"

        if [ "$ACTUAL_VERSION" != "$FULL_VERSION" ]; then
            log "WARNING: Requested ${FULL_VERSION} but installed ${ACTUAL_VERSION}"
        fi
    fi
}

main "$@"
