#!/bin/bash
set -e

echo "========================================="
echo "OCPCTL Demo Pre-flight Check"
echo "========================================="
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

DEV_SERVER="54.167.79.11"
DEV_KEY="$HOME/.ssh/ocpctl-dev-key"
DEV_URL="https://dev.ocpctl.mg.dog8code.com"

# Check 1: SSH Key exists
echo -n "Checking SSH key exists... "
if [ -f "$DEV_KEY" ]; then
    echo -e "${GREEN}✓${NC}"
else
    echo -e "${RED}✗${NC}"
    echo "  Error: SSH key not found at $DEV_KEY"
    exit 1
fi

# Check 2: Can reach dev server
echo -n "Checking dev server connectivity... "
if ssh -i "$DEV_KEY" -o ConnectTimeout=5 -o StrictHostKeyChecking=no ubuntu@$DEV_SERVER "exit" 2>/dev/null; then
    echo -e "${GREEN}✓${NC}"
else
    echo -e "${RED}✗${NC}"
    echo "  Error: Cannot connect to dev server"
    exit 1
fi

# Check 3: Services are running
echo "Checking dev server services..."
SERVICE_STATUS=$(ssh -i "$DEV_KEY" ubuntu@$DEV_SERVER "sudo systemctl is-active ocpctl-api ocpctl-worker ocpctl-web" 2>/dev/null || echo "error")

if echo "$SERVICE_STATUS" | grep -q "inactive\|failed\|error"; then
    echo -e "  ${RED}✗ Some services are down${NC}"
    echo "$SERVICE_STATUS"
    echo ""
    echo "  Run this to check details:"
    echo "  ssh -i $DEV_KEY ubuntu@$DEV_SERVER 'sudo systemctl status ocpctl-api ocpctl-worker ocpctl-web'"
    exit 1
else
    echo -e "  ${GREEN}✓ All services running${NC}"
fi

# Check 4: Web UI is accessible
echo -n "Checking web UI accessibility... "
HTTP_STATUS=$(curl -s -o /dev/null -w "%{http_code}" "$DEV_URL" --max-time 5 || echo "000")
if [ "$HTTP_STATUS" = "200" ] || [ "$HTTP_STATUS" = "302" ]; then
    echo -e "${GREEN}✓${NC} (HTTP $HTTP_STATUS)"
else
    echo -e "${RED}✗${NC} (HTTP $HTTP_STATUS)"
    echo "  Warning: Web UI may not be accessible"
fi

# Check 5: Check for existing demo clusters
echo -n "Checking for existing demo clusters... "
DEMO_CLUSTERS=$(ssh -i "$DEV_KEY" ubuntu@$DEV_SERVER \
    "PGPASSWORD=\$(grep DATABASE_URL /etc/ocpctl/api.env | cut -d'=' -f2- | sed 's/.*:\([^@]*\)@.*/\1/') \
     psql \$(grep DATABASE_URL /etc/ocpctl/api.env | cut -d'=' -f2-) \
     -t -c \"SELECT COUNT(*) FROM clusters WHERE name LIKE 'demo%' AND status != 'DESTROYED';\"" 2>/dev/null | tr -d ' ')

if [ "$DEMO_CLUSTERS" = "0" ]; then
    echo -e "${GREEN}✓ No demo clusters found${NC}"
else
    echo -e "${YELLOW}⚠ Found $DEMO_CLUSTERS demo cluster(s)${NC}"
    echo "  You may want to clean these up before recording:"
    echo "  ssh -i $DEV_KEY ubuntu@$DEV_SERVER"
    echo "  Then use the Web UI to destroy demo clusters"
fi

# Check 6: Disk space
echo -n "Checking disk space on dev server... "
DISK_USAGE=$(ssh -i "$DEV_KEY" ubuntu@$DEV_SERVER "df -h / | tail -n1 | awk '{print \$5}'" | tr -d '%')
if [ "$DISK_USAGE" -lt 80 ]; then
    echo -e "${GREEN}✓ ${DISK_USAGE}% used${NC}"
else
    echo -e "${YELLOW}⚠ ${DISK_USAGE}% used${NC}"
    echo "  Warning: Disk usage is high. May need cleanup."
fi

echo ""
echo "========================================="
echo -e "${GREEN}Pre-flight check complete!${NC}"
echo "========================================="
echo ""
echo "Next steps:"
echo "1. Open browser and navigate to: $DEV_URL"
echo "2. Login with: admin@example.com / changeme"
echo "3. Start screen recording tool"
echo "4. Follow demo script in DEMO_SCRIPT.md"
echo ""
echo "Recording tips:"
echo "- Close unnecessary apps and browser tabs"
echo "- Enable Do Not Disturb mode"
echo "- Set browser zoom to 100% or 110%"
echo "- Hide bookmarks bar (Cmd+Shift+B on Mac)"
echo "- Practice the flow 1-2 times first"
echo ""
echo "Good luck with your demo!"
