#!/bin/bash
# Ensure required OpenShift installer binaries are available
# Downloads from S3 or falls back to mirror.openshift.com

set -e

S3_BUCKET="s3://ocpctl-binaries"
INSTALL_DIR="/usr/local/bin"
REQUIRED_VERSIONS=("4.18" "4.19" "4.20" "4.21" "4.22")

# Default patch versions to use if not specified
declare -A DEFAULT_PATCHES
DEFAULT_PATCHES["4.18"]="4.18.35"
DEFAULT_PATCHES["4.19"]="4.19.23"
DEFAULT_PATCHES["4.20"]="4.20.3"
DEFAULT_PATCHES["4.21"]="4.21.0"
DEFAULT_PATCHES["4.22"]="4.22.0-ec.5"

log() {
    echo "[ensure-installers] $1"
}

# is_dev_preview_version detects if a version string is a dev-preview/candidate release
is_dev_preview_version() {
    local version=$1
    [[ "$version" == *"-ec."* ]] || \
    [[ "$version" == *"-rc."* ]] || \
    [[ "$version" == *"-0.nightly"* ]] || \
    [[ "$version" == *"-fc."* ]]
}

# get_mirror_base_path returns the mirror path component based on version type
# Returns "ocp" for stable versions, "ocp-dev-preview" for dev-preview versions
get_mirror_base_path() {
    local full_version=$1
    if is_dev_preview_version "$full_version"; then
        echo "ocp-dev-preview"
    else
        echo "ocp"
    fi
}

download_from_s3() {
    local version=$1
    local binary=$2
    local s3_path="${S3_BUCKET}/installers/${version}/${binary}"
    local local_path="${INSTALL_DIR}/${binary}-${version}"

    log "Attempting to download ${binary} ${version} from S3..."
    if aws s3 cp "${s3_path}" "${local_path}" 2>/dev/null; then
        chmod +x "${local_path}"
        log "✓ Downloaded ${binary} ${version} from S3"
        return 0
    fi
    return 1
}

download_from_mirror() {
    local full_version=$1
    local binary=$2
    # Extract major.minor version, stripping pre-release suffix if present
    # e.g., "4.22.0-ec.5" -> "4.22.0" -> "4.22"
    local base_version=$(echo "$full_version" | cut -d- -f1)
    local version=$(echo "$base_version" | cut -d. -f1,2)
    local local_path="${INSTALL_DIR}/${binary}-${version}"

    log "Downloading ${binary} ${full_version} from mirror.openshift.com..."

    # oc client has different tarball name
    # Use RHEL9 tarball for 4.22.0-ec.5 (FIPS-enabled)
    local tarball_name="${binary}-linux.tar.gz"
    if [ "$binary" = "oc" ]; then
        tarball_name="openshift-client-linux.tar.gz"
    elif [ "$binary" = "openshift-install" ] && [[ "$full_version" == "4.22.0-ec.5" ]]; then
        tarball_name="openshift-install-rhel9-amd64.tar.gz"
    elif [ "$binary" = "ccoctl" ] && [[ "$full_version" == "4.22.0-ec.5" ]]; then
        tarball_name="ccoctl-rhel9-amd64.tar.gz"
    fi

    # Select mirror path based on version type (stable or dev-preview)
    local mirror_base=$(get_mirror_base_path "$full_version")
    # Dev-preview versions use x86_64/clients path structure
    local arch_path=""
    if is_dev_preview_version "$full_version"; then
        arch_path="x86_64/"
    fi
    local mirror_url="https://mirror.openshift.com/pub/openshift-v4/${arch_path}clients/${mirror_base}/${full_version}/${tarball_name}"
    local tmp_dir=$(mktemp -d)

    if curl -sL "${mirror_url}" | tar xzf - -C "${tmp_dir}"; then
        # RHEL9 FIPS tarball contains openshift-install-fips instead of openshift-install
        # RHEL9 FIPS tarball contains ccoctl-fips instead of ccoctl
        local extracted_binary="${binary}"
        local install_suffix=""
        if [ "$binary" = "openshift-install" ] && [[ "$full_version" == "4.22.0-ec.5" ]]; then
            extracted_binary="openshift-install-fips"
        elif [ "$binary" = "ccoctl" ] && [[ "$full_version" == "4.22.0-ec.5" ]]; then
            extracted_binary="ccoctl-fips"
            install_suffix="-rhel9"  # Save as ccoctl-4.22-rhel9
        fi

        # Update local_path for RHEL9 ccoctl
        if [ -n "$install_suffix" ]; then
            local_path="${INSTALL_DIR}/${binary}-${version}${install_suffix}"
        fi

        # oc client tarball contains both 'oc' and 'kubectl'
        if [ -f "${tmp_dir}/${extracted_binary}" ]; then
            mv "${tmp_dir}/${extracted_binary}" "${local_path}"
            chmod +x "${local_path}"

            # If this is the oc binary, also install kubectl from the same tarball
            if [ "$binary" = "oc" ] && [ -f "${tmp_dir}/kubectl" ]; then
                local kubectl_path="${INSTALL_DIR}/kubectl"
                mv "${tmp_dir}/kubectl" "${kubectl_path}"
                chmod +x "${kubectl_path}"
                log "✓ Also installed kubectl from oc tarball"
            fi

            rm -rf "${tmp_dir}"

            # Upload to S3 for future use
            upload_to_s3 "${version}" "${binary}" "${local_path}"

            log "✓ Downloaded ${binary} ${full_version} from mirror"
            return 0
        else
            log "✗ Binary ${extracted_binary} not found in tarball"
            rm -rf "${tmp_dir}"
            return 1
        fi
    else
        rm -rf "${tmp_dir}"
        log "✗ Failed to download ${binary} ${full_version} from mirror"
        return 1
    fi
}

upload_to_s3() {
    local version=$1
    local binary=$2
    local local_path=$3
    local s3_path="${S3_BUCKET}/installers/${version}/${binary}"

    log "Uploading ${binary} ${version} to S3 for future use..."
    if aws s3 cp "${local_path}" "${s3_path}" 2>/dev/null; then
        log "✓ Uploaded ${binary} ${version} to S3"
    else
        log "Warning: Failed to upload ${binary} ${version} to S3 (non-fatal)"
    fi
}

ensure_binary() {
    local version=$1
    local binary=$2
    local local_path="${INSTALL_DIR}/${binary}-${version}"

    # Check if binary already exists
    if [ -f "${local_path}" ]; then
        log "✓ ${binary} ${version} already installed"
        return 0
    fi

    log "${binary} ${version} not found, attempting download..."

    # Try S3 first
    if download_from_s3 "${version}" "${binary}"; then
        return 0
    fi

    # Fall back to mirror.openshift.com with default patch version
    local full_version="${DEFAULT_PATCHES[${version}]}"
    if [ -z "${full_version}" ]; then
        log "✗ No default patch version configured for ${version}"
        return 1
    fi

    if download_from_mirror "${full_version}" "${binary}"; then
        return 0
    fi

    log "✗ Failed to download ${binary} ${version}"
    return 1
}

ensure_eksctl() {
    local binary_path="${INSTALL_DIR}/eksctl"

    # Check if already installed
    if [ -f "${binary_path}" ]; then
        log "✓ eksctl already installed"
        return 0
    fi

    log "Installing eksctl..."
    local tmp_dir=$(mktemp -d)

    # Download from GitHub releases
    local download_url="https://github.com/weaveworks/eksctl/releases/latest/download/eksctl_Linux_amd64.tar.gz"

    if curl -sL "${download_url}" | tar xzf - -C "${tmp_dir}"; then
        if [ -f "${tmp_dir}/eksctl" ]; then
            mv "${tmp_dir}/eksctl" "${binary_path}"
            chmod +x "${binary_path}"
            rm -rf "${tmp_dir}"
            log "✓ Installed eksctl"
            return 0
        else
            log "✗ eksctl binary not found in tarball"
            rm -rf "${tmp_dir}"
            return 1
        fi
    else
        rm -rf "${tmp_dir}"
        log "✗ Failed to download eksctl"
        return 1
    fi
}

ensure_ibmcloud() {
    local binary_path="${INSTALL_DIR}/ibmcloud"

    # Check if ibmcloud CLI is installed
    if ! command -v ibmcloud &> /dev/null; then
        log "Installing IBM Cloud CLI..."

        # Use the official IBM Cloud CLI installer script
        if curl -fsSL https://clis.cloud.ibm.com/install/linux | sh; then
            log "✓ Installed IBM Cloud CLI"
        else
            log "✗ Failed to install IBM Cloud CLI"
            return 1
        fi
    else
        log "✓ IBM Cloud CLI already installed"
    fi

    # Always ensure container-service plugin is installed (needed for IKS)
    log "Checking IBM Cloud container-service plugin..."
    if ibmcloud plugin list | grep -q container-service; then
        log "✓ container-service plugin already installed"
    else
        log "Installing container-service plugin..."
        if ibmcloud plugin install container-service -f; then
            log "✓ Installed container-service plugin"
        else
            log "WARNING: Failed to install container-service plugin (non-fatal)"
        fi
    fi

    return 0
}

ensure_4_22_standard_binaries() {
    # Download standard (non-RHEL9) binaries for 4.22.0-ec.5 testing
    # These are used when CCOCTL_BINARY_4_22 and OPENSHIFT_INSTALL_BINARY_4_22 env vars are set
    local full_version="4.22.0-ec.5"
    local version="4.22"

    log "Downloading standard (non-RHEL9) binaries for ${full_version}..."

    # Download standard openshift-install
    local installer_path="${INSTALL_DIR}/openshift-install-${version}-standard"
    if [ ! -f "${installer_path}" ]; then
        log "Downloading standard openshift-install ${full_version}..."
        local tmp_dir=$(mktemp -d)
        local mirror_url="https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp-dev-preview/${full_version}/openshift-install-linux.tar.gz"

        if curl -sL "${mirror_url}" | tar xzf - -C "${tmp_dir}"; then
            if [ -f "${tmp_dir}/openshift-install" ]; then
                mv "${tmp_dir}/openshift-install" "${installer_path}"
                chmod +x "${installer_path}"
                log "✓ Downloaded openshift-install-${version}-standard"
            else
                log "WARNING: openshift-install not found in tarball"
            fi
        else
            log "WARNING: Failed to download standard openshift-install ${full_version}"
        fi
        rm -rf "${tmp_dir}"
    else
        log "✓ openshift-install-${version}-standard already installed"
    fi

    # Download standard ccoctl
    local ccoctl_path="${INSTALL_DIR}/ccoctl-${version}-standard"
    if [ ! -f "${ccoctl_path}" ]; then
        log "Downloading standard ccoctl ${full_version}..."
        local tmp_dir=$(mktemp -d)
        local mirror_url="https://mirror.openshift.com/pub/openshift-v4/x86_64/clients/ocp-dev-preview/${full_version}/ccoctl-linux.tar.gz"

        if curl -sL "${mirror_url}" | tar xzf - -C "${tmp_dir}"; then
            if [ -f "${tmp_dir}/ccoctl" ]; then
                mv "${tmp_dir}/ccoctl" "${ccoctl_path}"
                chmod +x "${ccoctl_path}"
                log "✓ Downloaded ccoctl-${version}-standard"
            else
                log "WARNING: ccoctl not found in tarball"
            fi
        else
            log "WARNING: Failed to download standard ccoctl ${full_version}"
        fi
        rm -rf "${tmp_dir}"
    else
        log "✓ ccoctl-${version}-standard already installed"
    fi
}

main() {
    log "Ensuring required installer binaries..."

    local failed=0

    # OpenShift installers
    for version in "${REQUIRED_VERSIONS[@]}"; do
        if ! ensure_binary "${version}" "openshift-install"; then
            log "ERROR: Failed to ensure openshift-install ${version}"
            failed=1
        fi

        if ! ensure_binary "${version}" "ccoctl"; then
            log "WARNING: Failed to ensure ccoctl ${version} (non-fatal)"
            # Don't fail - ccoctl is only needed for Manual/STS mode
        fi

        if ! ensure_binary "${version}" "oc"; then
            log "WARNING: Failed to ensure oc ${version} (non-fatal)"
            # Don't fail - we'll create symlink to latest available version
        fi

        # For 4.22, also download standard (non-RHEL9) binaries for testing
        if [ "$version" = "4.22" ]; then
            ensure_4_22_standard_binaries
        fi
    done

    # Create symlink for oc to latest version (for PATH)
    # Try versions in reverse order (4.20, 4.19, 4.18) to get the latest
    for version in $(printf '%s\n' "${REQUIRED_VERSIONS[@]}" | tac); do
        if [ -f "${INSTALL_DIR}/oc-${version}" ]; then
            ln -sf "${INSTALL_DIR}/oc-${version}" "${INSTALL_DIR}/oc"
            log "✓ Created symlink: oc -> oc-${version}"
            break
        fi
    done

    # EKS and IKS installers
    if ! ensure_eksctl; then
        log "ERROR: Failed to ensure eksctl"
        failed=1
    fi

    if ! ensure_ibmcloud; then
        log "WARNING: Failed to ensure ibmcloud CLI (non-fatal)"
        # Don't fail - only needed for IKS clusters
    fi

    if [ $failed -eq 1 ]; then
        log "ERROR: Failed to ensure all required binaries"
        exit 1
    fi

    log "✓ All required installer binaries are available"
}

main "$@"
