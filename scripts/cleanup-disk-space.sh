#!/bin/bash
# Disk space cleanup script for OCPCTL EC2 instances
# Removes old binary releases, unused installers, and rotates logs

set -e

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}=== OCPCTL Disk Space Cleanup ===${NC}"
echo

# Function to print disk usage
print_disk_usage() {
    echo -e "${YELLOW}Current disk usage:${NC}"
    df -h / | grep -v Filesystem
    echo
}

# Show initial disk usage
print_disk_usage

# 1. Clean up old binary releases (keep only last 5)
echo -e "${YELLOW}Cleaning up old binary releases...${NC}"
RELEASES_DIR="/opt/ocpctl/releases"
if [ -d "$RELEASES_DIR" ]; then
    # Count releases
    RELEASE_COUNT=$(ls -1 "$RELEASES_DIR" | wc -l)
    echo "Found $RELEASE_COUNT releases"

    if [ "$RELEASE_COUNT" -gt 5 ]; then
        # Keep only the 5 most recent releases
        RELEASES_TO_DELETE=$(ls -1t "$RELEASES_DIR" | tail -n +6)

        echo "Removing $(echo "$RELEASES_TO_DELETE" | wc -l) old releases:"
        echo "$RELEASES_TO_DELETE" | head -5
        if [ $(echo "$RELEASES_TO_DELETE" | wc -l) -gt 5 ]; then
            echo "... and $(( $(echo "$RELEASES_TO_DELETE" | wc -l) - 5 )) more"
        fi

        echo "$RELEASES_TO_DELETE" | while read release; do
            sudo rm -rf "$RELEASES_DIR/$release"
        done

        echo -e "${GREEN}✓ Removed old releases${NC}"
    else
        echo -e "${GREEN}✓ Only $RELEASE_COUNT releases found, no cleanup needed${NC}"
    fi
else
    echo "Releases directory not found, skipping"
fi
echo

# 2. Clean up old web backups (keep only last 2)
echo -e "${YELLOW}Cleaning up old web backups...${NC}"
OCPCTL_DIR="/opt/ocpctl"
if [ -d "$OCPCTL_DIR" ]; then
    BACKUP_COUNT=$(ls -1d "$OCPCTL_DIR"/web.backup.* 2>/dev/null | wc -l)
    if [ "$BACKUP_COUNT" -gt 2 ]; then
        BACKUPS_TO_DELETE=$(ls -1td "$OCPCTL_DIR"/web.backup.* | tail -n +3)
        echo "Removing $(echo "$BACKUPS_TO_DELETE" | wc -l) old web backups"
        echo "$BACKUPS_TO_DELETE" | while read backup; do
            sudo rm -rf "$backup"
        done
        echo -e "${GREEN}✓ Removed old web backups${NC}"
    else
        echo -e "${GREEN}✓ Only $BACKUP_COUNT web backups found, no cleanup needed${NC}"
    fi
else
    echo "OCPCTL directory not found, skipping"
fi
echo

# 3. Clean up home directory build artifacts
echo -e "${YELLOW}Cleaning up home directory...${NC}"
if [ -d "$HOME/ocpctl" ]; then
    # Clean up old builds
    if [ -d "$HOME/ocpctl/web/.next" ]; then
        echo "Removing Next.js build cache..."
        rm -rf "$HOME/ocpctl/web/.next"
        echo -e "${GREEN}✓ Removed Next.js build cache${NC}"
    fi
fi

if [ -d "$HOME/ocpctl-web" ]; then
    echo "Removing old ocpctl-web directory..."
    rm -rf "$HOME/ocpctl-web"
    echo -e "${GREEN}✓ Removed old ocpctl-web directory${NC}"
fi

# Clean npm cache
if [ -d "$HOME/.npm" ]; then
    NPM_SIZE=$(du -sh "$HOME/.npm" 2>/dev/null | cut -f1)
    echo "Cleaning npm cache (current size: $NPM_SIZE)..."
    npm cache clean --force 2>/dev/null || true
    echo -e "${GREEN}✓ Cleaned npm cache${NC}"
fi
echo

# 4. Rotate systemd journal logs (keep last 7 days)
echo -e "${YELLOW}Rotating systemd journal logs...${NC}"
JOURNAL_SIZE_BEFORE=$(du -sh /var/log/journal 2>/dev/null | cut -f1)
echo "Current journal size: $JOURNAL_SIZE_BEFORE"
sudo journalctl --vacuum-time=7d
JOURNAL_SIZE_AFTER=$(du -sh /var/log/journal 2>/dev/null | cut -f1)
echo -e "${GREEN}✓ Journal logs rotated (now: $JOURNAL_SIZE_AFTER)${NC}"
echo

# 5. Clean up old installer binaries (optional - commented out for safety)
echo -e "${YELLOW}Checking openshift-install binaries...${NC}"
echo "Found installer versions:"
ls -lh /usr/local/bin/openshift-install* 2>/dev/null | awk '{print $9, $5}'
echo
echo -e "${YELLOW}NOTE: To remove old installer versions, run manually:${NC}"
echo "  sudo rm /usr/local/bin/openshift-install-4.18"
echo "  sudo rm /usr/local/bin/openshift-install-4.19"
echo "(Current version in use: $(readlink -f /usr/local/bin/openshift-install 2>/dev/null || echo 'unknown'))"
echo

# Show final disk usage
echo -e "${GREEN}=== Cleanup Complete ===${NC}"
print_disk_usage

echo -e "${GREEN}Disk space freed!${NC}"
