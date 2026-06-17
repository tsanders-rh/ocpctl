#!/bin/bash
# sync-profiles-from-production.sh
# Syncs profiles from production database back to local YAML files

set -euo pipefail

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== Syncing Profiles from Production Database ===${NC}"
echo ""

# Production server details
PROD_SERVER="44.201.165.78"
SSH_KEY="$HOME/.ssh/ocpctl-production-key"

# Check if SSH key exists
if [ ! -f "$SSH_KEY" ]; then
    echo -e "${RED}ERROR: SSH key not found: $SSH_KEY${NC}"
    exit 1
fi

# Check if jq and yq are installed locally
if ! command -v jq &> /dev/null; then
    echo -e "${RED}ERROR: jq is required but not installed${NC}"
    echo "Install with: brew install jq"
    exit 1
fi

if ! command -v yq &> /dev/null; then
    echo -e "${RED}ERROR: yq is required but not installed${NC}"
    echo "Install with: brew install yq"
    exit 1
fi

# Profiles directory
PROFILES_DIR="internal/profile/definitions"

if [ ! -d "$PROFILES_DIR" ]; then
    echo -e "${RED}ERROR: Profiles directory not found: $PROFILES_DIR${NC}"
    echo "Run this script from the ocpctl repository root"
    exit 1
fi

echo "Fetching profiles from production database..."

# Get DATABASE_URL from production
DATABASE_URL=$(ssh -i "$SSH_KEY" ubuntu@"$PROD_SERVER" 'sudo cat /etc/ocpctl/api.env' | grep '^DATABASE_URL=' | cut -d= -f2- | tr -d '"' | tr -d "'")

if [ -z "$DATABASE_URL" ]; then
    echo -e "${RED}ERROR: Could not fetch DATABASE_URL from production${NC}"
    exit 1
fi

# Export profiles from production database (run psql on production server)
PROFILES_JSON=$(ssh -i "$SSH_KEY" ubuntu@"$PROD_SERVER" "psql '$DATABASE_URL' -t -A -F'|' -c \"SELECT name, profile_data FROM profiles ORDER BY name\"")

if [ -z "$PROFILES_JSON" ]; then
    echo -e "${RED}ERROR: No profiles found in database${NC}"
    exit 1
fi

EXPORTED_COUNT=0
UPDATED_COUNT=0
NEW_COUNT=0

# Process each profile
while IFS='|' read -r name profile_data; do
    if [ -z "$name" ] || [ -z "$profile_data" ]; then
        continue
    fi

    echo ""
    echo -e "${YELLOW}Processing profile: ${name}${NC}"

    # Convert JSON to YAML
    YAML_FILE="$PROFILES_DIR/${name}.yaml"

    # Create temp file for comparison
    TEMP_YAML=$(mktemp)
    trap "rm -f $TEMP_YAML" EXIT

    # Convert JSON to YAML using jq and yq
    echo "$profile_data" | jq -r '.' | yq eval -P - > "$TEMP_YAML"

    if [ -f "$YAML_FILE" ]; then
        # Check if file has changed
        if ! diff -q "$YAML_FILE" "$TEMP_YAML" > /dev/null 2>&1; then
            echo -e "  ${GREEN}✓${NC} Updated: $YAML_FILE"
            cp "$TEMP_YAML" "$YAML_FILE"
            ((UPDATED_COUNT++))
        else
            echo -e "  ${GREEN}✓${NC} No changes: $YAML_FILE"
        fi
    else
        echo -e "  ${GREEN}✓${NC} Created: $YAML_FILE"
        cp "$TEMP_YAML" "$YAML_FILE"
        ((NEW_COUNT++))
    fi

    ((EXPORTED_COUNT++))
done <<< "$PROFILES_JSON"

echo ""
echo -e "${GREEN}=== Sync Complete ===${NC}"
echo ""
echo "Summary:"
echo "  Total profiles exported: $EXPORTED_COUNT"
echo "  Updated files: $UPDATED_COUNT"
echo "  New files: $NEW_COUNT"
echo ""

if [ $UPDATED_COUNT -gt 0 ] || [ $NEW_COUNT -gt 0 ]; then
    echo -e "${YELLOW}Next steps:${NC}"
    echo "1. Review changes:"
    echo "   git diff $PROFILES_DIR/"
    echo ""
    echo "2. Commit and push:"
    echo "   git add $PROFILES_DIR/"
    echo "   git commit -m 'Sync profiles from database (admin UI changes)'"
    echo "   git push"
    echo ""
    echo "3. Deploy to sync changes back:"
    echo "   ./scripts/deploy.sh"
else
    echo -e "${GREEN}All profiles are up to date!${NC}"
fi
