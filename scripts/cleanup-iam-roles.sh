#!/bin/bash
set -e

# cleanup-iam-roles.sh - Identify and delete orphaned OpenShift IAM roles
# Created by ocpctl to clean up IAM roles from failed cluster installations

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Parse command line arguments
DRY_RUN=true
FORCE=false
REGION="${AWS_REGION:-us-east-1}"

usage() {
    cat <<EOF
Usage: $0 [OPTIONS]

Identify and delete orphaned OpenShift IAM roles created by ccoctl.

OPTIONS:
    --execute       Actually delete roles (default: dry-run)
    --force         Skip confirmation prompts
    --region REGION AWS region (default: us-east-1)
    -h, --help      Show this help message

EXAMPLES:
    # Dry run (shows what would be deleted)
    $0

    # Actually delete orphaned roles with confirmation
    $0 --execute

    # Delete without confirmation (USE WITH CAUTION)
    $0 --execute --force

EOF
    exit 0
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --execute)
            DRY_RUN=false
            shift
            ;;
        --force)
            FORCE=true
            shift
            ;;
        --region)
            REGION="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            echo "Unknown option: $1"
            usage
            ;;
    esac
done

echo -e "${BLUE}=== OpenShift IAM Role Cleanup ===${NC}"
echo ""
echo -e "Region: ${YELLOW}$REGION${NC}"
echo -e "Mode: ${YELLOW}$([ "$DRY_RUN" = true ] && echo "DRY RUN" || echo "EXECUTE")${NC}"
echo ""

# Get list of active clusters from database directly
echo -e "${BLUE}Fetching active clusters from ocpctl database...${NC}"
ACTIVE_CLUSTERS_DB=$(ssh -i ~/.ssh/ocpctl-production-key ubuntu@44.201.165.78 \
    "sudo bash -c \"source /etc/ocpctl/api.env && psql \\\$DATABASE_URL -t -c \\\"SELECT name FROM clusters WHERE status NOT IN ('destroyed', 'failed');\\\"\" 2>/dev/null" \
    | grep -v '^$' | tr -d ' ' || echo "")

if [ -z "$ACTIVE_CLUSTERS_DB" ]; then
    echo -e "${RED}ERROR: Could not fetch cluster list from database${NC}"
    exit 1
fi

DB_CLUSTER_COUNT=$(echo "$ACTIVE_CLUSTERS_DB" | wc -l | tr -d ' ')
echo -e "${GREEN}Found $DB_CLUSTER_COUNT active clusters in ocpctl database${NC}"

# CRITICAL: Also check for clusters running in AWS but not in database
echo -e "${BLUE}Scanning AWS for running clusters not in database...${NC}"
RUNNING_INFRA_IDS=$(aws ec2 describe-instances \
    --region "$REGION" \
    --filters "Name=tag-key,Values=kubernetes.io/cluster/*" "Name=instance-state-name,Values=running,pending,stopping,stopped" \
    --query 'Reservations[].Instances[].Tags[?starts_with(Key, `kubernetes.io/cluster/`)].Key' \
    --output text 2>/dev/null | tr '\t' '\n' | sed 's/kubernetes.io\/cluster\///' | sort -u || echo "")

# Extract cluster names from infraIDs and combine with database clusters
ACTIVE_CLUSTERS="$ACTIVE_CLUSTERS_DB"
AWS_ONLY_COUNT=0
for infra_id in $RUNNING_INFRA_IDS; do
    # Extract cluster name (remove 5-char suffix)
    if [[ $infra_id =~ ^(.+)-[a-z0-9]{5}$ ]]; then
        cluster_name="${BASH_REMATCH[1]}"
    else
        cluster_name="$infra_id"
    fi

    # Add to active list if not already there
    if ! echo "$ACTIVE_CLUSTERS" | grep -q "^${cluster_name}$"; then
        echo -e "${YELLOW}  Found untracked running cluster: $cluster_name (infraID: $infra_id)${NC}"
        ACTIVE_CLUSTERS="${ACTIVE_CLUSTERS}"$'\n'"${cluster_name}"
        ((AWS_ONLY_COUNT++))
    fi
done

TOTAL_CLUSTER_COUNT=$(echo "$ACTIVE_CLUSTERS" | grep -v '^$' | wc -l | tr -d ' ')
if [ $AWS_ONLY_COUNT -gt 0 ]; then
    echo -e "${YELLOW}Found $AWS_ONLY_COUNT running clusters in AWS not tracked by ocpctl${NC}"
fi
echo -e "${GREEN}Total active clusters (DB + AWS): $TOTAL_CLUSTER_COUNT${NC}"
echo ""

# Function to extract infraID from role name
# Example: "tsanders-test-445-kqbf7-openshift-cloud-credential-operator-clou" -> "tsanders-test-445-kqbf7"
extract_infra_id() {
    local role_name="$1"

    # Pattern: {name}-{5chars}-openshift-*
    if [[ $role_name =~ ^(.+)-([a-z0-9]{5})-openshift- ]]; then
        echo "${BASH_REMATCH[1]}-${BASH_REMATCH[2]}"
    elif [[ $role_name =~ ^(.+)-([a-z0-9]{5})-(master|worker)-role ]]; then
        echo "${BASH_REMATCH[1]}-${BASH_REMATCH[2]}"
    else
        echo ""
    fi
}

# Function to extract cluster name from infraID
# Example: "tsanders-test-445-kqbf7" -> "tsanders-test-445"
extract_cluster_name() {
    local infra_id="$1"

    # Remove the 5-char suffix
    if [[ $infra_id =~ ^(.+)-[a-z0-9]{5}$ ]]; then
        echo "${BASH_REMATCH[1]}"
    else
        echo ""
    fi
}

# Function to check if a role is orphaned
is_orphaned() {
    local role_name="$1"
    local infra_id=$(extract_infra_id "$role_name")

    if [ -z "$infra_id" ]; then
        # Not an OpenShift role
        return 1
    fi

    local cluster_name=$(extract_cluster_name "$infra_id")
    if [ -z "$cluster_name" ]; then
        # Can't determine cluster name
        return 1
    fi

    # Check if cluster exists in active clusters
    if echo "$ACTIVE_CLUSTERS" | grep -q "^${cluster_name}$"; then
        # Cluster is active
        return 1
    fi

    # Cluster is not active - role is orphaned
    return 0
}

echo -e "${BLUE}Scanning IAM roles for orphans...${NC}"
echo ""

# Get all IAM roles and filter for OpenShift-related ones
TOTAL_ROLES=0
OPENSHIFT_ROLES=0
ORPHANED_ROLES=()
ORPHANED_INFRA_IDS=()

# Use pagination to handle large role lists
MARKER=""
while true; do
    if [ -z "$MARKER" ]; then
        RESULT=$(aws iam list-roles --region "$REGION" --output json 2>/dev/null)
    else
        RESULT=$(aws iam list-roles --region "$REGION" --marker "$MARKER" --output json 2>/dev/null)
    fi

    # Extract roles from this page
    ROLES=$(echo "$RESULT" | jq -r '.Roles[].RoleName')

    for role in $ROLES; do
        ((TOTAL_ROLES++))

        # Check if this is an OpenShift role (contains "-openshift-" or ends with "-role")
        if [[ $role =~ -openshift- ]] || [[ $role =~ -(master|worker)-role$ ]]; then
            ((OPENSHIFT_ROLES++))

            # Check if it's orphaned
            if is_orphaned "$role"; then
                ORPHANED_ROLES+=("$role")
                infra_id=$(extract_infra_id "$role")
                if [ -n "$infra_id" ]; then
                    ORPHANED_INFRA_IDS+=("$infra_id")
                fi
            fi
        fi
    done

    # Check if there are more pages
    MARKER=$(echo "$RESULT" | jq -r '.Marker // empty')
    if [ -z "$MARKER" ]; then
        break
    fi
done

echo -e "${BLUE}Scan complete:${NC}"
echo -e "  Total IAM roles: ${YELLOW}$TOTAL_ROLES${NC}"
echo -e "  OpenShift roles: ${YELLOW}$OPENSHIFT_ROLES${NC}"
echo -e "  Orphaned roles: ${RED}${#ORPHANED_ROLES[@]}${NC}"
echo ""

if [ ${#ORPHANED_ROLES[@]} -eq 0 ]; then
    echo -e "${GREEN}No orphaned IAM roles found!${NC}"
    exit 0
fi

# Get unique infraIDs
UNIQUE_INFRA_IDS=($(printf '%s\n' "${ORPHANED_INFRA_IDS[@]}" | sort -u))

echo -e "${YELLOW}Found ${#ORPHANED_ROLES[@]} orphaned roles from ${#UNIQUE_INFRA_IDS[@]} failed clusters:${NC}"
echo ""

# Group by infraID and display
for infra_id in "${UNIQUE_INFRA_IDS[@]}"; do
    cluster_name=$(extract_cluster_name "$infra_id")
    role_count=0

    for role in "${ORPHANED_ROLES[@]}"; do
        if [[ $role == ${infra_id}-* ]]; then
            ((role_count++))
        fi
    done

    echo -e "  ${YELLOW}$cluster_name${NC} (infraID: $infra_id) - $role_count roles"
done
echo ""

# Show sample roles
echo -e "${BLUE}Sample orphaned roles (first 10):${NC}"
for i in "${!ORPHANED_ROLES[@]}"; do
    if [ $i -ge 10 ]; then
        break
    fi
    echo "  - ${ORPHANED_ROLES[$i]}"
done
if [ ${#ORPHANED_ROLES[@]} -gt 10 ]; then
    echo "  ... and $((${#ORPHANED_ROLES[@]} - 10)) more"
fi
echo ""

if [ "$DRY_RUN" = true ]; then
    echo -e "${YELLOW}DRY RUN MODE - No roles will be deleted${NC}"
    echo ""
    echo "To actually delete these roles, run:"
    echo "  $0 --execute"
    exit 0
fi

# Confirmation prompt
if [ "$FORCE" = false ]; then
    echo -e "${RED}WARNING: This will permanently delete ${#ORPHANED_ROLES[@]} IAM roles!${NC}"
    echo ""
    read -p "Are you sure you want to continue? (type 'yes' to confirm): " confirm
    if [ "$confirm" != "yes" ]; then
        echo -e "${YELLOW}Cancelled.${NC}"
        exit 0
    fi
    echo ""
fi

# Delete roles
echo -e "${BLUE}Deleting orphaned IAM roles...${NC}"
DELETED=0
FAILED=0

for role in "${ORPHANED_ROLES[@]}"; do
    echo -n "  Deleting $role... "

    # First, detach all managed policies
    ATTACHED_POLICIES=$(aws iam list-attached-role-policies --role-name "$role" --query 'AttachedPolicies[].PolicyArn' --output text 2>/dev/null || echo "")
    for policy_arn in $ATTACHED_POLICIES; do
        aws iam detach-role-policy --role-name "$role" --policy-arn "$policy_arn" 2>/dev/null || true
    done

    # Delete inline policies
    INLINE_POLICIES=$(aws iam list-role-policies --role-name "$role" --query 'PolicyNames' --output text 2>/dev/null || echo "")
    for policy_name in $INLINE_POLICIES; do
        aws iam delete-role-policy --role-name "$role" --policy-name "$policy_name" 2>/dev/null || true
    done

    # Delete the role
    if aws iam delete-role --role-name "$role" 2>/dev/null; then
        echo -e "${GREEN}OK${NC}"
        ((DELETED++))
    else
        echo -e "${RED}FAILED${NC}"
        ((FAILED++))
    fi
done

echo ""
echo -e "${BLUE}Cleanup complete:${NC}"
echo -e "  Successfully deleted: ${GREEN}$DELETED${NC}"
if [ $FAILED -gt 0 ]; then
    echo -e "  Failed: ${RED}$FAILED${NC}"
fi
echo ""

# Also clean up associated OIDC providers
echo -e "${BLUE}Checking for orphaned OIDC providers...${NC}"
OIDC_DELETED=0
for infra_id in "${UNIQUE_INFRA_IDS[@]}"; do
    # Get AWS account ID
    ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text 2>/dev/null)
    PROVIDER_ARN="arn:aws:iam::${ACCOUNT_ID}:oidc-provider/${infra_id}-oidc.s3.${REGION}.amazonaws.com"

    echo -n "  Checking OIDC provider for $infra_id... "
    if aws iam get-open-id-connect-provider --open-id-connect-provider-arn "$PROVIDER_ARN" >/dev/null 2>&1; then
        if aws iam delete-open-id-connect-provider --open-id-connect-provider-arn "$PROVIDER_ARN" 2>/dev/null; then
            echo -e "${GREEN}DELETED${NC}"
            ((OIDC_DELETED++))
        else
            echo -e "${RED}FAILED${NC}"
        fi
    else
        echo -e "${YELLOW}NOT FOUND${NC}"
    fi
done

if [ $OIDC_DELETED -gt 0 ]; then
    echo ""
    echo -e "${GREEN}Deleted $OIDC_DELETED OIDC providers${NC}"
fi

echo ""
echo -e "${GREEN}=== Cleanup Complete ===${NC}"
echo ""
echo "Next steps:"
echo "  1. Verify current IAM role count: aws iam list-roles --query 'length(Roles)'"
echo "  2. Run orphan detection: Check admin console"
echo "  3. Retry failed cluster creation: ocpctl create cluster"
