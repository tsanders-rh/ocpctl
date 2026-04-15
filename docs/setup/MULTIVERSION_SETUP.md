# Multi-Version OpenShift Support Setup

ocpctl supports deploying OpenShift clusters across multiple versions (4.18, 4.19, 4.20) by using version-specific binaries.

## Overview

Each OpenShift version requires its own `openshift-install` and `ccoctl` binaries. ocpctl automatically selects the correct binary based on the cluster version specified in the creation request.

**Supported Versions:**
- OpenShift 4.18 (Kubernetes 1.31 - Maintenance support)
- OpenShift 4.19 (Kubernetes 1.32 - Fully supported)
- OpenShift 4.20 (Kubernetes 1.33 - Latest GA)

## Binary Requirements

Each version requires approximately 500MB-1GB of storage:

```
/usr/local/bin/openshift-install-4.18    (~600MB)
/usr/local/bin/openshift-install-4.19    (~600MB)
/usr/local/bin/openshift-install-4.20    (~600MB)
/usr/local/bin/ccoctl-4.18               (~100MB)
/usr/local/bin/ccoctl-4.19               (~100MB)
/usr/local/bin/ccoctl-4.20               (~100MB)
```

**Total storage needed:** ~4GB

## Installation

### Step 1: Download OpenShift Installer Binaries

Download the latest patch version for each minor release:

```bash
# OpenShift 4.18 (latest: 4.18.35)
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-4.18/openshift-install-linux.tar.gz
tar xvf openshift-install-linux.tar.gz
sudo mv openshift-install /usr/local/bin/openshift-install-4.18
sudo chmod +x /usr/local/bin/openshift-install-4.18

# OpenShift 4.19 (latest: 4.19.23)
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-4.19/openshift-install-linux.tar.gz
tar xvf openshift-install-linux.tar.gz
sudo mv openshift-install /usr/local/bin/openshift-install-4.19
sudo chmod +x /usr/local/bin/openshift-install-4.19

# OpenShift 4.20 (latest: 4.20.5)
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-4.20/openshift-install-linux.tar.gz
tar xvf openshift-install-linux.tar.gz
sudo mv openshift-install /usr/local/bin/openshift-install-4.20
sudo chmod +x /usr/local/bin/openshift-install-4.20
```

### Step 2: Download ccoctl Binaries

```bash
# ccoctl 4.18
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-4.18/ccoctl-linux.tar.gz
tar xvf ccoctl-linux.tar.gz
sudo mv ccoctl /usr/local/bin/ccoctl-4.18
sudo chmod +x /usr/local/bin/ccoctl-4.18

# ccoctl 4.19
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-4.19/ccoctl-linux.tar.gz
tar xvf ccoctl-linux.tar.gz
sudo mv ccoctl /usr/local/bin/ccoctl-4.19
sudo chmod +x /usr/local/bin/ccoctl-4.19

# ccoctl 4.20
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-4.20/ccoctl-linux.tar.gz
tar xvf ccoctl-linux.tar.gz
sudo mv ccoctl /usr/local/bin/ccoctl-4.20
sudo chmod +x /usr/local/bin/ccoctl-4.20
```

### Step 3: Verify Installation

```bash
# Verify all binaries are installed
ls -lh /usr/local/bin/openshift-install-*
ls -lh /usr/local/bin/ccoctl-*

# Test each version
/usr/local/bin/openshift-install-4.18 version
/usr/local/bin/openshift-install-4.19 version
/usr/local/bin/openshift-install-4.20 version

/usr/local/bin/ccoctl-4.18 version
/usr/local/bin/ccoctl-4.19 version
/usr/local/bin/ccoctl-4.20 version
```

## Alternative: Custom Binary Locations

If you want to use custom paths, set environment variables:

```bash
# In worker environment (/etc/ocpctl/worker.env)
export OPENSHIFT_INSTALL_BINARY_4_18=/custom/path/openshift-install-4.18
export OPENSHIFT_INSTALL_BINARY_4_19=/custom/path/openshift-install-4.19
export OPENSHIFT_INSTALL_BINARY_4_20=/custom/path/openshift-install-4.20

export CCOCTL_BINARY_4_18=/custom/path/ccoctl-4.18
export CCOCTL_BINARY_4_19=/custom/path/ccoctl-4.19
export CCOCTL_BINARY_4_20=/custom/path/ccoctl-4.20
```

## How It Works

When a cluster is created:

1. User selects an OpenShift version from the profile (e.g., "4.19.23")
2. ocpctl extracts the major.minor version ("4.19")
3. The installer package selects the correct binary (`/usr/local/bin/openshift-install-4.19`)
4. Cluster is provisioned using that specific version

**Example logs:**
```
Creating installer for OpenShift version 4.19.23
Using OpenShift installer version 4.19: /usr/local/bin/openshift-install-4.19
Using ccoctl version 4.19: /usr/local/bin/ccoctl-4.19
Running openshift-install create cluster for my-cluster (version 4.19.23)
```

## Upgrading Binaries

When new patch versions are released (e.g., 4.20.6), update the binary:

```bash
# Download new version
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/4.20.6/openshift-install-linux.tar.gz
tar xvf openshift-install-linux.tar.gz

# Replace existing binary
sudo mv openshift-install /usr/local/bin/openshift-install-4.20
sudo chmod +x /usr/local/bin/openshift-install-4.20

# Restart worker to pick up new binary
sudo systemctl restart ocpctl-worker
```

**Note:** Patch version updates (4.20.5 -> 4.20.6) use the same binary location since they share the same major.minor version (4.20).

## Supported Versions in Profiles

All cluster profiles now support versions 4.18, 4.19, and 4.20:

```yaml
openshiftVersions:
  allowlist:
    - "4.18.35"  # Latest 4.18 patch
    - "4.19.23"  # Latest 4.19 patch
    - "4.20.3"   # 4.20 patch versions
    - "4.20.4"
    - "4.20.5"
  default: "4.20.3"
```

Users can select any of these versions when creating a cluster.

## Developer Preview Support

ocpctl supports deploying pre-release OpenShift versions alongside stable versions. This allows testing of early candidate (EC), release candidate (RC), and nightly builds.

### Supported Dev-Preview Versions

- **OpenShift 4.22.0-ec.5** (Developer Preview - Early Candidate 5)

### Version Format Patterns

Dev-preview versions use specific naming patterns that ocpctl automatically detects:

| Pattern | Example | Description |
|---------|---------|-------------|
| `-ec.N` | `4.22.0-ec.5` | Early Candidate builds |
| `-rc.N` | `4.21.0-rc.1` | Release Candidate builds |
| `-0.nightly` | `4.22.0-0.nightly-2024-03-15` | Nightly builds with timestamp |
| `-fc.N` | `4.22.0-fc.2` | Feature Candidate builds |

### Mirror Path Differences

Dev-preview versions are downloaded from a different mirror path:

- **Stable versions**: `https://mirror.openshift.com/pub/openshift-v4/clients/ocp/{version}/`
- **Dev-preview versions**: `https://mirror.openshift.com/pub/openshift-v4/clients/ocp-dev-preview/{version}/`

ocpctl automatically detects the version type and uses the correct mirror path.

### Installing Dev-Preview Binaries

Dev-preview binaries follow the same major.minor naming convention:

```bash
# Download 4.22 dev-preview (Early Candidate 5)
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp-dev-preview/4.22.0-ec.5/openshift-install-linux.tar.gz
tar xvf openshift-install-linux.tar.gz
sudo mv openshift-install /usr/local/bin/openshift-install-4.22
sudo chmod +x /usr/local/bin/openshift-install-4.22

# Download ccoctl for 4.22
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp-dev-preview/4.22.0-ec.5/ccoctl-linux.tar.gz
tar xvf ccoctl-linux.tar.gz
sudo mv ccoctl /usr/local/bin/ccoctl-4.22
sudo chmod +x /usr/local/bin/ccoctl-4.22
```

**Note:** Binary naming uses major.minor version (4.22), not the full version string (4.22.0-ec.5). Multiple dev-preview versions with the same major.minor will share the same binary location.

### Profile Configuration

Dev-preview versions can be mixed with stable versions in profile allowlists:

```yaml
openshiftVersions:
  allowlist:
    - "4.18.35"      # Stable
    - "4.19.23"      # Stable
    - "4.20.3"       # Stable
    - "4.20.4"       # Stable
    - "4.20.5"       # Stable
    - "4.22.0-ec.5"  # Dev-preview (Early Candidate 5)
  default: "4.20.3"  # Keep default as stable version
```

**Important:** Always keep the default version as a stable release. Users must explicitly select dev-preview versions.

### When to Use Dev-Preview Versions

**Use dev-preview for:**
- Testing new OpenShift features before GA
- Early validation of upcoming releases
- Bug verification in candidate builds
- Development environments only

**Do not use dev-preview for:**
- Production workloads
- Long-lived test environments
- Performance benchmarking
- Customer demonstrations

### Caveats and Risks

1. **Stability**: Dev-preview builds may contain bugs and breaking changes
2. **Support**: Limited or no support for pre-release versions
3. **Upgrades**: Upgrade paths from dev-preview versions may not be supported
4. **Breaking Changes**: APIs and features may change between dev-preview releases
5. **Binary Replacement**: Different dev-preview versions (e.g., 4.22.0-ec.5 vs 4.22.0-ec.6) will overwrite each other at `/usr/local/bin/openshift-install-4.22`

### Finding Available Dev-Preview Versions

Browse the dev-preview mirror to find available versions:

```bash
# View available dev-preview versions
curl -s https://mirror.openshift.com/pub/openshift-v4/clients/ocp-dev-preview/ | grep -o 'href="[^"]*"' | grep -E '4\.[0-9]+\.[0-9]+'
```

Or visit in a browser: https://mirror.openshift.com/pub/openshift-v4/clients/ocp-dev-preview/

## Troubleshooting

### Binary Not Found Error

**Error:**
```
create installer for version 4.19.23: openshift-install binary not found for version 4.19.23 at /usr/local/bin/openshift-install-4.19: no such file or directory
```

**Solution:**
```bash
# Download and install the missing binary
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-4.19/openshift-install-linux.tar.gz
tar xvf openshift-install-linux.tar.gz
sudo mv openshift-install /usr/local/bin/openshift-install-4.19
sudo chmod +x /usr/local/bin/openshift-install-4.19
```

### Unsupported Version Error

**Error:**
```
unsupported OpenShift version: 4.17.10 (supported: 4.18, 4.19, 4.20)
```

**Solution:** Update the profile to only include supported versions (4.18-4.20), or update ocpctl to support additional versions.

### ccoctl Missing Warning

**Warning:**
```
Warning: ccoctl binary not found for version 4.19.23 at /usr/local/bin/ccoctl-4.19 (Manual mode may not work)
```

**Impact:** Cluster creation will fail if using EC2 instance profile (STS/Manual mode). Static AWS credentials work without ccoctl.

**Solution:**
```bash
wget https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-4.19/ccoctl-linux.tar.gz
tar xvf ccoctl-linux.tar.gz
sudo mv ccoctl /usr/local/bin/ccoctl-4.19
sudo chmod +x /usr/local/bin/ccoctl-4.19
```

## Automated Setup Script

For convenience, use this script to download all binaries:

```bash
#!/bin/bash
# File: scripts/install-multiversion-binaries.sh

set -e

VERSIONS=("4.18" "4.19" "4.20")

for VERSION in "${VERSIONS[@]}"; do
  echo "Installing OpenShift $VERSION binaries..."

  # Download openshift-install
  wget -q https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-$VERSION/openshift-install-linux.tar.gz
  tar xzf openshift-install-linux.tar.gz
  sudo mv openshift-install /usr/local/bin/openshift-install-$VERSION
  sudo chmod +x /usr/local/bin/openshift-install-$VERSION
  rm openshift-install-linux.tar.gz

  # Download ccoctl
  wget -q https://mirror.openshift.com/pub/openshift-v4/clients/ocp/stable-$VERSION/ccoctl-linux.tar.gz
  tar xzf ccoctl-linux.tar.gz
  sudo mv ccoctl /usr/local/bin/ccoctl-$VERSION
  sudo chmod +x /usr/local/bin/ccoctl-$VERSION
  rm ccoctl-linux.tar.gz

  echo "✓ Installed OpenShift $VERSION"
done

echo ""
echo "All binaries installed successfully!"
echo ""
echo "Verify installation:"
for VERSION in "${VERSIONS[@]}"; do
  /usr/local/bin/openshift-install-$VERSION version
done
```

**Usage:**
```bash
chmod +x scripts/install-multiversion-binaries.sh
sudo ./scripts/install-multiversion-binaries.sh
```

## See Also

- [OpenShift Install Setup](OPENSHIFT_INSTALL_SETUP.md) - Basic installer setup
- [AWS Quick Start](../deployment/AWS_QUICKSTART.md) - Full deployment guide
- [Cluster Profiles](../../internal/profile/definitions/) - Available profiles and versions
