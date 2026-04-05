#!/bin/bash
# Install cron job for automated disk cleanup
# Runs weekly on Sunday at 2 AM

set -e

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
CLEANUP_SCRIPT="$SCRIPT_DIR/cleanup-disk-space.sh"
CRON_JOB="0 2 * * 0 $CLEANUP_SCRIPT >> /var/log/ocpctl-cleanup.log 2>&1"

echo "Installing weekly disk cleanup cron job..."

# Check if cron job already exists
if sudo crontab -l 2>/dev/null | grep -q "cleanup-disk-space.sh"; then
    echo "Cron job already exists, updating..."
    # Remove old entry
    sudo crontab -l 2>/dev/null | grep -v "cleanup-disk-space.sh" | sudo crontab -
fi

# Add new cron job
(sudo crontab -l 2>/dev/null; echo "$CRON_JOB") | sudo crontab -

echo "✓ Cron job installed:"
echo "  Schedule: Weekly on Sunday at 2 AM"
echo "  Script: $CLEANUP_SCRIPT"
echo "  Log: /var/log/ocpctl-cleanup.log"
echo
echo "To verify:"
echo "  sudo crontab -l"
echo
echo "To run manually:"
echo "  $CLEANUP_SCRIPT"
