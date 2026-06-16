#!/bin/bash
set -e

# Standardize profile versions:
# - GA releases: x.y (4.18, 4.19, 4.20, 4.21)
# - Pre-release: x.y.z-suffix (4.22.0-ec.5)

echo "Standardizing profile versions..."

# Function to consolidate GA versions in a YAML file
standardize_yaml() {
    local file=$1
    echo "Processing $file..."

    # Skip files that only have pre-release versions
    if grep -q "4.22.0-ec.5" "$file" && ! grep -q "4.21.10\|4.20.5\|4.19.23" "$file"; then
        echo "  Skipping (pre-release only)"
        return
    fi

    # Replace specific patch versions with major.minor for GA releases
    # 4.21.x → 4.21
    sed -i.bak 's/"4.21.10"/"4.21"/g; s/"4.21.9"/"4.21"/g; s/"4.21.8"/"4.21"/g' "$file"
    sed -i.bak 's/4.21.10/4.21/g; s/4.21.9/4.21/g; s/4.21.8/4.21/g' "$file"

    # 4.20.x → 4.20
    sed -i.bak 's/"4.20.15"/"4.20"/g; s/"4.20.5"/"4.20"/g; s/"4.20.4"/"4.20"/g; s/"4.20.3"/"4.20"/g' "$file"
    sed -i.bak 's/4.20.15/4.20/g; s/4.20.5/4.20/g; s/4.20.4/4.20/g; s/4.20.3/4.20/g' "$file"

    # 4.19.x → 4.19
    sed -i.bak 's/"4.19.24"/"4.19"/g; s/"4.19.23"/"4.19"/g' "$file"
    sed -i.bak 's/4.19.24/4.19/g; s/4.19.23/4.19/g' "$file"

    # 4.18.x → 4.18
    sed -i.bak 's/"4.18.35"/"4.18"/g; s/"4.18.34"/"4.18"/g' "$file"
    sed -i.bak 's/4.18.35/4.18/g; s/4.18.34/4.18/g' "$file"

    # 4.17.x → 4.17
    sed -i.bak 's/"4.17.49"/"4.17"/g' "$file"
    sed -i.bak 's/4.17.49/4.17/g' "$file"

    # 4.16.x → 4.16
    sed -i.bak 's/"4.16.57"/"4.16"/g' "$file"
    sed -i.bak 's/4.16.57/4.16/g' "$file"

    # Remove duplicate entries and backup files
    rm -f "$file.bak"
}

# Process all OpenShift profile YAMLs
for file in internal/profile/definitions/*.yaml; do
    # Only process files with openshiftVersions
    if grep -q "openshiftVersions:" "$file"; then
        standardize_yaml "$file"
    fi
done

echo ""
echo "Removing duplicate version entries..."

# Use Python to deduplicate allowlist entries while preserving order
python3 - <<'EOF'
import yaml
import glob

for filepath in glob.glob("internal/profile/definitions/*.yaml"):
    with open(filepath, 'r') as f:
        try:
            data = yaml.safe_load(f)
        except:
            continue

    # Skip if no version config
    if not data:
        continue

    # Process openshiftVersions
    if 'openshiftVersions' in data and 'allowlist' in data['openshiftVersions']:
        versions = data['openshiftVersions']['allowlist']
        # Deduplicate while preserving order
        seen = set()
        deduped = []
        for v in versions:
            if v not in seen:
                seen.add(v)
                deduped.append(v)
        data['openshiftVersions']['allowlist'] = deduped

    # Process kubernetesVersions
    if 'kubernetesVersions' in data and 'allowlist' in data['kubernetesVersions']:
        versions = data['kubernetesVersions']['allowlist']
        seen = set()
        deduped = []
        for v in versions:
            if v not in seen:
                seen.add(v)
                deduped.append(v)
        data['kubernetesVersions']['allowlist'] = deduped

    # Write back
    with open(filepath, 'w') as f:
        yaml.dump(data, f, default_flow_style=False, sort_keys=False)

print("✓ Deduplicated version entries")
EOF

echo ""
echo "✓ Profile YAML files updated"
echo ""
echo "Next steps:"
echo "1. Review changes: git diff internal/profile/definitions/"
echo "2. Update database with: ./scripts/sync-profile-versions-to-db.sh"
