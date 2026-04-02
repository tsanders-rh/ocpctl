# Add-on Definition Schema

Each add-on is defined in a separate YAML file with this structure:

## Top-level Fields
- `id` (string, required): Unique identifier (e.g., "oadp")
- `name` (string, required): Display name
- `description` (string, required): User-facing description
- `category` (string, required): One of: backup, migration, cicd, monitoring, security, storage, networking
- `enabled` (boolean, required): Whether add-on is available for selection
- `supportedPlatforms` (array, required): Platforms where this add-on can run (openshift, eks, iks)
- `versions` (array, required): List of available versions

## Version Object
- `channel` (string, required): Operator channel identifier (e.g., "stable-1.4")
- `displayName` (string, required): Human-readable version name
- `isDefault` (boolean, required): Whether this is the recommended version
- `config` (object, required): Post-deployment configuration
  - `operators` (array): Operator installations
  - `scripts` (array): Custom scripts to run
  - `manifests` (array): Kubernetes manifests to apply
  - `helmCharts` (array): Helm charts to install

## Validation Rules
1. Exactly one version must have `isDefault: true` per add-on
2. Version channels must be unique within an add-on
3. Category must be from allowed list
4. At least one supported platform required
5. Config must be valid CustomPostConfig structure
