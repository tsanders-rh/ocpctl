#!/bin/bash
# Script to run database migrations against the RDS instance
# Usage: ./scripts/migrate-rds.sh [up|down|status]

set -e

# RDS connection details
RDS_HOST="${RDS_HOST:-44.201.165.78}"
RDS_PORT="${RDS_PORT:-5432}"
RDS_USER="${RDS_USER:-ocpctl}"
RDS_DB="${RDS_DB:-ocpctl}"
RDS_PASSWORD="${RDS_PASSWORD}"

# Check if password is set
if [ -z "$RDS_PASSWORD" ]; then
    echo "Error: RDS_PASSWORD environment variable not set"
    echo "Usage: RDS_PASSWORD='your-password' ./scripts/migrate-rds.sh [up|down|status]"
    exit 1
fi

# Build connection string
export DATABASE_URL="postgresql://${RDS_USER}:${RDS_PASSWORD}@${RDS_HOST}:${RDS_PORT}/${RDS_DB}?sslmode=require"

# Default action is "up"
ACTION="${1:-up}"

echo "========================================="
echo "Database Migration Tool (RDS)"
echo "========================================="
echo "Host: ${RDS_HOST}"
echo "Port: ${RDS_PORT}"
echo "Database: ${RDS_DB}"
echo "User: ${RDS_USER}"
echo "Action: ${ACTION}"
echo "========================================="
echo ""

# Check connectivity first
echo "Testing database connectivity..."
if ! timeout 5 bash -c "cat < /dev/null > /dev/tcp/${RDS_HOST}/${RDS_PORT}" 2>/dev/null; then
    echo ""
    echo "❌ Cannot connect to ${RDS_HOST}:${RDS_PORT}"
    echo ""
    echo "Possible solutions:"
    echo "1. Check if you have network access to the RDS instance"
    echo "2. Verify the RDS security group allows inbound traffic from your IP"
    echo "3. If using a bastion/jump host, set up SSH tunnel:"
    echo "   ssh -L 5432:${RDS_HOST}:5432 user@bastion-host"
    echo "   Then set RDS_HOST=localhost before running this script"
    echo ""
    exit 1
fi

echo "✓ Connection successful"
echo ""

# Run goose migration
cd "$(dirname "$0")/.."

case $ACTION in
    up)
        echo "Running migrations..."
        goose -dir internal/store/migrations postgres "$DATABASE_URL" up
        ;;
    down)
        echo "Rolling back last migration..."
        goose -dir internal/store/migrations postgres "$DATABASE_URL" down
        ;;
    status)
        echo "Checking migration status..."
        goose -dir internal/store/migrations postgres "$DATABASE_URL" status
        ;;
    version)
        echo "Checking current version..."
        goose -dir internal/store/migrations postgres "$DATABASE_URL" version
        ;;
    *)
        echo "Unknown action: $ACTION"
        echo "Valid actions: up, down, status, version"
        exit 1
        ;;
esac

echo ""
echo "✓ Migration completed successfully"
