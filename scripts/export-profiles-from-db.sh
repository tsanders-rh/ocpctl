#!/bin/bash
# export-profiles-from-db.sh
# Exports profiles from database back to YAML files
# This allows syncing changes made via admin UI back to the git repo

set -euo pipefail

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== Exporting Profiles from Database to YAML ===${NC}"
echo ""

# Check if DATABASE_URL is set
if [ -z "${DATABASE_URL:-}" ]; then
    echo -e "${RED}ERROR: DATABASE_URL environment variable not set${NC}"
    echo "Set it with: export DATABASE_URL='postgres://...'"
    exit 1
fi

# Check if jq and yq are installed
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

# Temporary directory for exports
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

echo "Fetching profiles from database..."
psql "$DATABASE_URL" -t -c "SELECT name, profile_data FROM profiles ORDER BY name" > "$TEMP_DIR/profiles.txt"

if [ ! -s "$TEMP_DIR/profiles.txt" ]; then
    echo -e "${RED}ERROR: No profiles found in database${NC}"
    exit 1
fi

EXPORTED_COUNT=0
UPDATED_COUNT=0
NEW_COUNT=0

# Process each profile
while IFS='|' read -r name profile_data; do
    # Trim whitespace
    name=$(echo "$name" | xargs)
    profile_data=$(echo "$profile_data" | xargs)

    if [ -z "$name" ] || [ -z "$profile_data" ]; then
        continue
    fi

    echo ""
    echo -e "${YELLOW}Processing profile: ${name}${NC}"

    # Convert JSON to YAML
    YAML_FILE="$PROFILES_DIR/${name}.yaml"
    TEMP_YAML="$TEMP_DIR/${name}.yaml"

    # Extract profile_data and convert to YAML
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
done < "$TEMP_DIR/profiles.txt"

echo ""
echo -e "${GREEN}=== Export Complete ===${NC}"
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
