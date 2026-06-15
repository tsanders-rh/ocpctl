#!/bin/bash
# Sync OpenShift binaries for all versions referenced in active profiles
# This ensures that all versions allowed in profiles are actually installed

set -euo pipefail

PROFILES_DIR="${1:-/opt/ocpctl/profiles}"
INSTALL_DIR="${2:-/usr/local/bin}"
DRY_RUN="${DRY_RUN:-false}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    echo -e "${BLUE}[sync-profile-binaries]${NC} $1"
}

success() {
    echo -e "${GREEN}[sync-profile-binaries]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[sync-profile-binaries]${NC} $1"
}

error() {
    echo -e "${RED}[sync-profile-binaries]${NC} $1" >&2
}

# Check if directory exists
if [ ! -d "$PROFILES_DIR" ]; then
    error "Profiles directory not found: $PROFILES_DIR"
    exit 1
fi

log "Scanning profiles in $PROFILES_DIR..."

# Extract all OpenShift versions from YAML profiles
# Look for versions in allowlist arrays and default version fields
ALL_VERSIONS=()

for profile in "$PROFILES_DIR"/*.yaml; do
    if [ ! -f "$profile" ]; then
        continue
    fi

    profile_name=$(basename "$profile" .yaml)

    # Skip non-OpenShift profiles (EKS, GKE, etc.)
    cluster_type=$(grep -E '^\s*clusterType:' "$profile" | awk '{print $2}' || echo "")
    if [ "$cluster_type" != "openshift" ]; then
        log "Skipping non-OpenShift profile: $profile_name (type: $cluster_type)"
        continue
    fi

    log "Scanning profile: $profile_name"

    # Extract versions from allowlist (handles both formats: allowlist and versions.allowlist)
    # Only look in openshiftVersions section to avoid picking up CIDR blocks from networking config
    versions=$(grep -A 50 -E 'openshiftVersions:' "$profile" | \
               grep -B 50 -E '(kubernetesVersions:|compute:|networking:|^[a-z]|^$)' | head -n -1 | \
               grep -E '^\s*-\s+' | \
               grep -oE '4\.[0-9]+(\.[0-9]+)?(-[a-z0-9.]+)?' || echo "")

    # Extract default version (only OpenShift versions starting with 4.)
    default=$(grep -A 2 -E 'openshiftVersions:' "$profile" | \
              grep -E '^\s*default:' | \
              grep -oE '4\.[0-9]+(\.[0-9]+)?(-[a-z0-9.]+)?' || echo "")

    # Combine all versions
    for version in $versions $default; do
        if [ -n "$version" ] && [[ "$version" =~ ^4\. ]]; then
            ALL_VERSIONS+=("$version")
        fi
    done
done

# Remove duplicates and sort
UNIQUE_VERSIONS=($(printf '%s\n' "${ALL_VERSIONS[@]}" | sort -V | uniq))

log "Found ${#UNIQUE_VERSIONS[@]} unique OpenShift versions across all profiles"

# Check which versions are installed
MISSING_VERSIONS=()
INSTALLED_VERSIONS=()

for version in "${UNIQUE_VERSIONS[@]}"; do
    # Extract major.minor (e.g., "4.21.19" -> "4.21", "4.22.0-ec.5" -> "4.22")
    major_minor=$(echo "$version" | cut -d- -f1 | cut -d. -f1,2)

    # Check if binary exists (either exact version or major.minor symlink)
    if [ -f "$INSTALL_DIR/openshift-install-$version" ] || \
       [ -f "$INSTALL_DIR/openshift-install-$major_minor" ]; then
        INSTALLED_VERSIONS+=("$version")
        success "✓ $version - installed"
    else
        MISSING_VERSIONS+=("$version")
        warn "✗ $version - NOT installed"
    fi
done

log ""
log "Summary:"
log "  Total versions in profiles: ${#UNIQUE_VERSIONS[@]}"
success "  Installed: ${#INSTALLED_VERSIONS[@]}"
warn "  Missing: ${#MISSING_VERSIONS[@]}"

if [ ${#MISSING_VERSIONS[@]} -eq 0 ]; then
    success ""
    success "All profile versions are installed!"
    exit 0
fi

log ""
warn "The following versions are referenced in profiles but not installed:"
for version in "${MISSING_VERSIONS[@]}"; do
    warn "  - $version"
done

if [ "$DRY_RUN" = "true" ]; then
    log ""
    log "Dry run mode - not downloading binaries"
    log "To download missing binaries, run without DRY_RUN=true"
    exit 0
fi

log ""
log "Downloading missing binaries..."

DOWNLOAD_SCRIPT="/opt/ocpctl/scripts/download-specific-version.sh"
if [ ! -f "$DOWNLOAD_SCRIPT" ]; then
    error "Download script not found: $DOWNLOAD_SCRIPT"
    exit 1
fi

FAILED_DOWNLOADS=()
SUCCESSFUL_DOWNLOADS=()

for version in "${MISSING_VERSIONS[@]}"; do
    log ""
    log "Downloading OpenShift $version..."

    if $DOWNLOAD_SCRIPT "$version" "$INSTALL_DIR"; then
        SUCCESSFUL_DOWNLOADS+=("$version")
        success "✓ Successfully downloaded $version"
    else
        FAILED_DOWNLOADS+=("$version")
        error "✗ Failed to download $version"
    fi
done

log ""
log "Download Summary:"
success "  Successful: ${#SUCCESSFUL_DOWNLOADS[@]}"
if [ ${#FAILED_DOWNLOADS[@]} -gt 0 ]; then
    error "  Failed: ${#FAILED_DOWNLOADS[@]}"
    log ""
    error "Failed downloads:"
    for version in "${FAILED_DOWNLOADS[@]}"; do
        error "  - $version"
    done
    exit 1
fi

success ""
success "All profile binaries synced successfully!"
