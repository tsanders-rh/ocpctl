# Cluster Profile Schema

Cluster profiles define standardized configurations for OpenShift cluster provisioning with policy enforcement.

## Schema Definition

```yaml
# Profile metadata
name: string                    # Profile identifier (e.g., "aws-minimal-test")
displayName: string            # Human-readable name
description: string            # Profile description
platform: string               # "aws" | "ibmcloud"
enabled: boolean               # Profile availability

# Version constraints
openshiftVersions:
  allowlist:
    - string                   # Allowed OpenShift versions (e.g., "4.20.3")
  default: string              # Default version if not specified

# Region configuration
regions:
  allowlist:
    - string                   # Allowed cloud regions
  default: string              # Default region

# Base domain configuration
baseDomains:
  allowlist:
    - string                   # Allowed DNS base domains
  default: string              # Default base domain

# Compute configuration
compute:
  controlPlane:
    replicas: integer          # Number of control plane nodes (typically 3)
    instanceType: string       # Cloud instance type
    schedulable: boolean       # Allow pod scheduling on control plane
  workers:
    replicas: integer          # Number of worker nodes
    minReplicas: integer       # Minimum workers (for scaling)
    maxReplicas: integer       # Maximum workers (for scaling)
    instanceType: string       # Cloud instance type
    autoscaling: boolean       # Enable autoscaling

# Lifecycle policy
lifecycle:
  maxTTLHours: integer         # Maximum time-to-live in hours
  defaultTTLHours: integer     # Default TTL if not specified
  allowCustomTTL: boolean      # Allow users to specify custom TTL
  warnBeforeDestroyHours: integer  # Warning notification before TTL expiry

# Networking (optional overrides)
networking:
  networkType: string          # "OVNKubernetes" | "OpenShiftSDN"
  clusterNetworks:
    - cidr: string
      hostPrefix: integer
  serviceNetwork:
    - string
  machineNetwork:
    - cidr: string

# Required tags
tags:
  required:
    key: value                 # Tags that must be present
  defaults:
    key: value                 # Default tags if not specified
  allowUserTags: boolean       # Allow users to add custom tags

# Features
features:
  offHoursScaling: boolean     # Support off-hours worker scaling
  fipsMode: boolean            # FIPS compliance mode
  privateCluster: boolean      # Private cluster (no public endpoints)

# Cost controls
costControls:
  estimatedHourlyCost: float   # Estimated hourly cost (USD)
  maxMonthlyCost: float        # Maximum monthly cost threshold
  budgetAlertThreshold: float  # Alert when cost exceeds percentage

# Platform-specific configuration
platformConfig:
  aws:
    # AWS-specific settings
    instanceMetadataService: string  # "required" | "optional"
    rootVolume:
      type: string             # "gp3" | "gp2" | "io1"
      size: integer            # Size in GB
      iops: integer            # IOPS (for io1)

  ibmcloud:
    # IBM Cloud-specific settings
    resourceGroup: string
    vpcName: string
```

## Validation Rules

1. **Platform Consistency**: `platform` must match the profile name prefix (e.g., "aws-*" for AWS)
2. **Replica Constraints**:
   - Control plane replicas must be odd (1, 3, 5) for etcd quorum
   - Worker replicas >= minReplicas and <= maxReplicas
3. **TTL Constraints**: defaultTTLHours <= maxTTLHours
4. **Version Format**: OpenShift versions must match semver pattern (X.Y.Z)
5. **Region Format**: Must match cloud provider region naming conventions

## Profile Naming Convention

- Format: `{platform}-{size}-{purpose}`
- Platform: `aws` | `ibmcloud`
- Size: `minimal` | `standard` | `large`
- Purpose: `test` | `dev` | `staging` | `prod`

Examples:
- `aws-minimal-test`: Minimal AWS cluster for testing
- `aws-standard-dev`: Standard AWS cluster for development
- `ibmcloud-minimal-test`: Minimal IBM Cloud cluster for testing

## Reserved Tag Keys

The following tag keys are reserved and cannot be overridden by user tags:

- `ManagedBy`
- `ClusterId`
- `ClusterName`
- `Owner`
- `Team`
- `CostCenter`
- `Environment`
- `TTLExpiry`
- `RequestId`
- `Profile`
- `Platform`

## Profile Lifecycle

1. **Development**: Create profile YAML in `definitions/` directory
2. **Validation**: Run `make validate-profiles` to check schema compliance
3. **Testing**: Test profile with integration tests
4. **Deployment**: Profiles are loaded at service startup
5. **Updates**: Profile changes require service restart or dynamic reload

## Example Minimal Profile

```yaml
name: aws-minimal-test
displayName: AWS Minimal Test Cluster
description: Compact 3-node cluster for quick testing (masters schedulable, no workers)
platform: aws
enabled: true

openshiftVersions:
  allowlist:
    - "4.20.3"
    - "4.20.4"
  default: "4.20.3"

regions:
  allowlist:
    - us-east-1
    - us-west-2
  default: us-east-1

baseDomains:
  allowlist:
    - labs.example.com
  default: labs.example.com

compute:
  controlPlane:
    replicas: 3
    instanceType: m6i.xlarge
    schedulable: true
  workers:
    replicas: 0
    minReplicas: 0
    maxReplicas: 3
    instanceType: m6i.2xlarge
    autoscaling: false

lifecycle:
  maxTTLHours: 72
  defaultTTLHours: 24
  allowCustomTTL: true
  warnBeforeDestroyHours: 1

tags:
  required:
    Environment: test
  defaults:
    Purpose: development
  allowUserTags: true

features:
  offHoursScaling: true
  fipsMode: false
  privateCluster: false

costControls:
  estimatedHourlyCost: 2.50
  maxMonthlyCost: 1800
  budgetAlertThreshold: 0.8

platformConfig:
  aws:
    instanceMetadataService: required
    rootVolume:
      type: gp3
      size: 120
      iops: 3000
```
