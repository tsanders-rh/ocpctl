#!/bin/bash
set -e

# OCPCTL Auto-Scaling Demo Script
# This script creates 10 test clusters to demonstrate auto-scaling
# Run from your local macOS laptop against the remote API in EC2
#
# Prerequisites:
#   - AWS CLI configured with credentials
#   - jq installed (brew install jq)
#   - Network access to the EC2 API server
#
# Usage:
#   export API_URL=http://your-ec2-ip:8080/api/v1
#   export OCPCTL_EMAIL=admin@example.com
#   export OCPCTL_PASSWORD=yourpassword
#   ./scripts/demo-autoscaling.sh

# Configuration - UPDATE THESE
API_URL="${API_URL:-}"  # Set to your EC2 API URL, e.g., http://ec2-ip:8080/api/v1
EMAIL="${OCPCTL_EMAIL:-}"
PASSWORD="${OCPCTL_PASSWORD:-}"

# Validate configuration
if [ -z "$API_URL" ]; then
    echo "ERROR: API_URL not set. Set it via environment variable or edit this script."
    echo "Example: export API_URL=http://your-ec2-ip:8080/api/v1"
    exit 1
fi

if [ -z "$EMAIL" ] || [ -z "$PASSWORD" ]; then
    echo "ERROR: EMAIL and PASSWORD must be set."
    echo "Example: export OCPCTL_EMAIL=admin@example.com OCPCTL_PASSWORD=yourpassword"
    exit 1
fi

# Test cluster configuration
CLUSTER_COUNT=10
PLATFORM="aws"
VERSION="4.17.8"
PROFILE="aws-sno-test"  # Single Node OpenShift for faster/cheaper demo
REGION="us-east-1"
BASE_DOMAIN="${BASE_DOMAIN:-demo.ocpctl.io}"
TEAM="demo"
COST_CENTER="autoscaling-test"
TTL_HOURS=2

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log() {
    echo -e "${GREEN}[$(date +'%H:%M:%S')]${NC} $1"
}

error() {
    echo -e "${RED}[$(date +'%H:%M:%S')] ERROR:${NC} $1"
}

warn() {
    echo -e "${YELLOW}[$(date +'%H:%M:%S')] WARN:${NC} $1"
}

info() {
    echo -e "${BLUE}[$(date +'%H:%M:%S')]${NC} $1"
}

# Login and get access token
login() {
    log "Logging in to OCPCTL API..."

    response=$(curl -s -X POST "${API_URL}/auth/login" \
        -H "Content-Type: application/json" \
        -d "{\"email\":\"${EMAIL}\",\"password\":\"${PASSWORD}\"}" \
        -c /tmp/ocpctl-cookies.txt)

    ACCESS_TOKEN=$(echo "$response" | jq -r '.access_token')

    if [ -z "$ACCESS_TOKEN" ] || [ "$ACCESS_TOKEN" = "null" ]; then
        error "Failed to login. Response: $response"
        exit 1
    fi

    log "Successfully authenticated"
}

# Create a test cluster
create_cluster() {
    local cluster_name=$1
    local owner_email=$2

    response=$(curl -s -X POST "${API_URL}/clusters" \
        -H "Content-Type: application/json" \
        -H "Authorization: Bearer ${ACCESS_TOKEN}" \
        -b /tmp/ocpctl-cookies.txt \
        -d "{
            \"name\": \"${cluster_name}\",
            \"platform\": \"${PLATFORM}\",
            \"version\": \"${VERSION}\",
            \"profile\": \"${PROFILE}\",
            \"region\": \"${REGION}\",
            \"base_domain\": \"${BASE_DOMAIN}\",
            \"owner\": \"${owner_email}\",
            \"team\": \"${TEAM}\",
            \"cost_center\": \"${COST_CENTER}\",
            \"ttl_hours\": ${TTL_HOURS}
        }")

    cluster_id=$(echo "$response" | jq -r '.id // empty')

    if [ -z "$cluster_id" ]; then
        error "Failed to create cluster ${cluster_name}. Response: $response"
        return 1
    fi

    log "Created cluster: ${cluster_name} (ID: ${cluster_id})"
    echo "$cluster_id"
}

# Get pending jobs count
get_pending_jobs() {
    curl -s -X GET "${API_URL}/jobs?status=PENDING&limit=1000" \
        -H "Authorization: Bearer ${ACCESS_TOKEN}" \
        -b /tmp/ocpctl-cookies.txt | jq '.total // 0'
}

# Get ASG metrics from AWS
get_asg_metrics() {
    aws autoscaling describe-auto-scaling-groups \
        --auto-scaling-group-names ocpctl-worker-asg \
        --query 'AutoScalingGroups[0].[DesiredCapacity,length(Instances)]' \
        --output text 2>/dev/null || echo "0 0"
}

# Get CloudWatch metrics
get_cloudwatch_metrics() {
    local end_time=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    # macOS date command syntax
    local start_time=$(date -u -v-5M +"%Y-%m-%dT%H:%M:%SZ")

    # Get pending jobs metric
    pending=$(aws cloudwatch get-metric-statistics \
        --namespace OCPCTL \
        --metric-name PendingJobs \
        --start-time "$start_time" \
        --end-time "$end_time" \
        --period 60 \
        --statistics Average \
        --query 'Datapoints[-1].Average' \
        --output text 2>/dev/null || echo "0")

    # Handle "None" response from AWS
    if [ "$pending" = "None" ]; then
        pending="0"
    fi

    # Get active workers
    active=$(aws cloudwatch get-metric-statistics \
        --namespace OCPCTL \
        --metric-name WorkerActive \
        --start-time "$start_time" \
        --end-time "$end_time" \
        --period 60 \
        --statistics Sum \
        --query 'Datapoints[-1].Sum' \
        --output text 2>/dev/null || echo "0")

    # Handle "None" response from AWS
    if [ "$active" = "None" ]; then
        active="0"
    fi

    echo "${pending} ${active}"
}

# Monitor auto-scaling activity
monitor_autoscaling() {
    log "Monitoring auto-scaling activity (press Ctrl+C to stop)..."
    echo ""
    printf "%-8s | %-12s | %-15s | %-15s | %-20s\n" "TIME" "PENDING JOBS" "ASG DESIRED" "ASG INSTANCES" "ACTIVE WORKERS"
    printf "%-8s-+-%-12s-+-%-15s-+-%-15s-+-%-20s\n" "--------" "------------" "---------------" "---------------" "--------------------"

    while true; do
        # Get pending jobs from API
        pending_jobs=$(get_pending_jobs)

        # Get ASG metrics
        read asg_desired asg_instances <<< "$(get_asg_metrics)"

        # Get CloudWatch metrics
        read cw_pending cw_active <<< "$(get_cloudwatch_metrics)"

        # Display metrics
        timestamp=$(date +"%H:%M:%S")
        printf "%-8s | %-12s | %-15s | %-15s | %-20s\n" \
            "$timestamp" \
            "${pending_jobs}" \
            "${asg_desired}" \
            "${asg_instances}" \
            "${cw_active}"

        sleep 10
    done
}

# Main execution
main() {
    echo "========================================="
    echo "  OCPCTL Auto-Scaling Demo"
    echo "========================================="
    echo ""
    info "Configuration:"
    info "  API URL: ${API_URL}"
    info "  Cluster Count: ${CLUSTER_COUNT}"
    info "  Profile: ${PROFILE}"
    info "  Region: ${REGION}"
    info "  TTL: ${TTL_HOURS} hours"
    echo ""

    # Login
    login

    # Get current user info
    user_response=$(curl -s -X GET "${API_URL}/auth/me" \
        -H "Authorization: Bearer ${ACCESS_TOKEN}" \
        -b /tmp/ocpctl-cookies.txt)

    owner_email=$(echo "$user_response" | jq -r '.email')
    log "Authenticated as: ${owner_email}"
    echo ""

    # Check initial state
    initial_pending=$(get_pending_jobs)
    log "Current pending jobs: ${initial_pending}"
    echo ""

    # Create test clusters
    log "Creating ${CLUSTER_COUNT} test clusters to trigger auto-scaling..."
    echo ""

    cluster_ids=()
    for i in $(seq 1 $CLUSTER_COUNT); do
        cluster_name="autoscale-demo-${i}"
        cluster_id=$(create_cluster "$cluster_name" "$owner_email")

        if [ -n "$cluster_id" ]; then
            cluster_ids+=("$cluster_id")
        fi

        # Small delay to avoid rate limiting
        sleep 0.5
    done

    echo ""
    log "Successfully created ${#cluster_ids[@]} clusters"
    log "This should trigger auto-scaling from 1 to ~5 workers"
    echo ""

    # Wait a moment for jobs to be created
    sleep 2

    # Show final pending count
    final_pending=$(get_pending_jobs)
    log "Pending jobs after creation: ${final_pending}"
    echo ""

    info "Auto-scaling behavior:"
    info "  - Current: 1 worker"
    info "  - Target: 2 pending jobs per worker"
    info "  - Expected scale-out: ${final_pending} jobs ÷ 2 = ~$((final_pending / 2)) workers"
    info "  - Scale-out should complete in 2-3 minutes"
    echo ""

    # Start monitoring
    monitor_autoscaling
}

# Cleanup on exit
cleanup() {
    echo ""
    log "Cleaning up..."
    rm -f /tmp/ocpctl-cookies.txt
}

trap cleanup EXIT

# Run main
main
