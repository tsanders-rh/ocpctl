#!/usr/bin/env bash
#
# delete-shared-vpc.sh - Safely delete a persistent shared VPC
#
# Usage:
#   ./delete-shared-vpc.sh <vpc-id> [region]
#
# Example:
#   ./delete-shared-vpc.sh vpc-0123456789abcdef0 us-east-1
#
# This script deletes a VPC and all associated resources in the proper order:
# 1. NAT Gateways (and wait for deletion)
# 2. VPC Endpoints
# 3. Route Tables (disassociate and delete non-main tables)
# 4. Subnets
# 5. Custom Security Groups (non-default)
# 6. Internet Gateways
# 7. VPC

set -euo pipefail

VPC_ID="${1:-}"
REGION="${2:-us-east-1}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Validate inputs
if [[ -z "${VPC_ID}" ]]; then
    error "Usage: $0 <vpc-id> [region]"
fi

echo "=============================================="
echo "Deleting Shared VPC: ${VPC_ID}"
echo "Region: ${REGION}"
echo "=============================================="
echo ""

# Validate VPC exists
log "Validating VPC exists..."
if ! aws ec2 describe-vpcs --vpc-ids "${VPC_ID}" --region "${REGION}" &>/dev/null; then
    error "VPC ${VPC_ID} does not exist in region ${REGION}"
fi

# Get VPC name for display
VPC_NAME=$(aws ec2 describe-vpcs \
    --vpc-ids "${VPC_ID}" \
    --region "${REGION}" \
    --query 'Vpcs[0].Tags[?Key==`Name`].Value' \
    --output text 2>/dev/null || echo "unnamed")

log "VPC Name: ${VPC_NAME}"
echo ""

warn "This will delete the VPC and all associated resources:"
warn "  - NAT Gateways and their Elastic IPs"
warn "  - VPC Endpoints"
warn "  - Route Tables"
warn "  - Subnets"
warn "  - Custom Security Groups"
warn "  - Internet Gateways"
warn ""
warn "⚠️  IMPORTANT: Make sure no clusters are using this VPC!"
echo ""
read -p "Are you sure you want to continue? Type 'yes' to confirm: " CONFIRM

if [[ "${CONFIRM}" != "yes" ]]; then
    echo "Aborted"
    exit 0
fi

echo ""

#
# Step 1: Delete NAT Gateways and wait for completion
#
log "Step 1: Deleting NAT Gateways..."

# Get NAT gateway IDs
NAT_GW_IDS=$(aws ec2 describe-nat-gateways \
    --filter "Name=vpc-id,Values=${VPC_ID}" "Name=state,Values=pending,available" \
    --query 'NatGateways[*].NatGatewayId' \
    --output text \
    --region "${REGION}")

# Get EIP allocations ONLY from this VPC's NAT gateways (before deletion)
NAT_EIP_ALLOC_IDS=$(aws ec2 describe-nat-gateways \
    --filter "Name=vpc-id,Values=${VPC_ID}" "Name=state,Values=pending,available" \
    --query 'NatGateways[*].NatGatewayAddresses[*].AllocationId' \
    --output text \
    --region "${REGION}")

if [[ -n "${NAT_GW_IDS}" ]]; then
    for NAT_GW_ID in ${NAT_GW_IDS}; do
        log "  Deleting NAT Gateway: ${NAT_GW_ID}"
        aws ec2 delete-nat-gateway --nat-gateway-id "${NAT_GW_ID}" --region "${REGION}"
    done

    log "  Waiting for NAT Gateways to be deleted..."
    for NAT_GW_ID in ${NAT_GW_IDS}; do
        # Use proper wait instead of sleep
        if ! aws ec2 wait nat-gateway-deleted --nat-gateway-ids "${NAT_GW_ID}" --region "${REGION}" 2>/dev/null; then
            # If wait command not available (AWS CLI v1), poll instead
            log "  Polling deletion status for ${NAT_GW_ID}..."
            for i in {1..60}; do
                STATE=$(aws ec2 describe-nat-gateways \
                    --nat-gateway-ids "${NAT_GW_ID}" \
                    --region "${REGION}" \
                    --query 'NatGateways[0].State' \
                    --output text 2>/dev/null || echo "deleted")

                if [[ "${STATE}" == "deleted" ]]; then
                    break
                fi

                if [[ $i -eq 60 ]]; then
                    error "NAT Gateway ${NAT_GW_ID} did not delete within 5 minutes"
                fi

                sleep 5
            done
        fi
    done

    # Release ONLY the EIPs that were associated with NAT gateways in this VPC
    if [[ -n "${NAT_EIP_ALLOC_IDS}" ]]; then
        log "  Releasing NAT Gateway Elastic IPs..."
        for EIP_ALLOC_ID in ${NAT_EIP_ALLOC_IDS}; do
            log "    Releasing EIP: ${EIP_ALLOC_ID}"
            aws ec2 release-address --allocation-id "${EIP_ALLOC_ID}" --region "${REGION}" || warn "    Failed to release ${EIP_ALLOC_ID}"
        done
    fi

    success "NAT Gateways deleted"
else
    log "  No NAT Gateways found"
fi

echo ""

#
# Step 2: Delete VPC Endpoints
#
log "Step 2: Deleting VPC Endpoints..."

VPC_ENDPOINT_IDS=$(aws ec2 describe-vpc-endpoints \
    --filters "Name=vpc-id,Values=${VPC_ID}" \
    --query 'VpcEndpoints[*].VpcEndpointId' \
    --output text \
    --region "${REGION}")

if [[ -n "${VPC_ENDPOINT_IDS}" ]]; then
    for VPCE_ID in ${VPC_ENDPOINT_IDS}; do
        log "  Deleting VPC Endpoint: ${VPCE_ID}"
        aws ec2 delete-vpc-endpoints --vpc-endpoint-ids "${VPCE_ID}" --region "${REGION}"
    done
    success "VPC Endpoints deleted"
else
    log "  No VPC Endpoints found"
fi

echo ""

#
# Step 3: Delete Route Tables (non-main)
#
log "Step 3: Deleting Route Tables..."

# Get all route tables in VPC
ALL_RT_IDS=$(aws ec2 describe-route-tables \
    --filters "Name=vpc-id,Values=${VPC_ID}" \
    --query 'RouteTables[*].[RouteTableId,Associations[?Main==`true`]]' \
    --output text \
    --region "${REGION}")

# Parse to get non-main route table IDs
RT_IDS=$(echo "${ALL_RT_IDS}" | grep -v "True" | awk '{print $1}' || true)

if [[ -n "${RT_IDS}" ]]; then
    for RT_ID in ${RT_IDS}; do
        # Disassociate from subnets first
        ASSOC_IDS=$(aws ec2 describe-route-tables \
            --route-table-ids "${RT_ID}" \
            --query 'RouteTables[0].Associations[?SubnetId!=`null`].RouteTableAssociationId' \
            --output text \
            --region "${REGION}")

        for ASSOC_ID in ${ASSOC_IDS}; do
            log "  Disassociating route table ${RT_ID} from subnet (${ASSOC_ID})"
            aws ec2 disassociate-route-table --association-id "${ASSOC_ID}" --region "${REGION}"
        done

        log "  Deleting route table: ${RT_ID}"
        aws ec2 delete-route-table --route-table-id "${RT_ID}" --region "${REGION}"
    done
    success "Route Tables deleted"
else
    log "  No non-main route tables found"
fi

echo ""

#
# Step 4: Delete Subnets
#
log "Step 4: Deleting Subnets..."

SUBNET_IDS=$(aws ec2 describe-subnets \
    --filters "Name=vpc-id,Values=${VPC_ID}" \
    --query 'Subnets[*].SubnetId' \
    --output text \
    --region "${REGION}")

if [[ -n "${SUBNET_IDS}" ]]; then
    for SUBNET_ID in ${SUBNET_IDS}; do
        log "  Deleting subnet: ${SUBNET_ID}"
        aws ec2 delete-subnet --subnet-id "${SUBNET_ID}" --region "${REGION}"
    done
    success "Subnets deleted"
else
    log "  No subnets found"
fi

echo ""

#
# Step 5: Delete Custom Security Groups (non-default)
#
log "Step 5: Deleting Custom Security Groups..."

# Get all non-default security groups
SG_IDS=$(aws ec2 describe-security-groups \
    --filters "Name=vpc-id,Values=${VPC_ID}" \
    --query 'SecurityGroups[?GroupName!=`default`].GroupId' \
    --output text \
    --region "${REGION}")

if [[ -n "${SG_IDS}" ]]; then
    # First pass: Remove all ingress/egress rules to break dependencies
    for SG_ID in ${SG_IDS}; do
        log "  Removing rules from security group: ${SG_ID}"

        # Revoke all ingress rules
        aws ec2 describe-security-groups \
            --group-ids "${SG_ID}" \
            --region "${REGION}" \
            --query 'SecurityGroups[0].IpPermissions' \
            --output json > /tmp/sg-ingress-${SG_ID}.json 2>/dev/null || true

        if [[ -s /tmp/sg-ingress-${SG_ID}.json ]] && [[ "$(cat /tmp/sg-ingress-${SG_ID}.json)" != "[]" ]]; then
            aws ec2 revoke-security-group-ingress \
                --group-id "${SG_ID}" \
                --ip-permissions file:///tmp/sg-ingress-${SG_ID}.json \
                --region "${REGION}" 2>/dev/null || true
        fi

        # Revoke all egress rules
        aws ec2 describe-security-groups \
            --group-ids "${SG_ID}" \
            --region "${REGION}" \
            --query 'SecurityGroups[0].IpPermissionsEgress' \
            --output json > /tmp/sg-egress-${SG_ID}.json 2>/dev/null || true

        if [[ -s /tmp/sg-egress-${SG_ID}.json ]] && [[ "$(cat /tmp/sg-egress-${SG_ID}.json)" != "[]" ]]; then
            aws ec2 revoke-security-group-egress \
                --group-id "${SG_ID}" \
                --ip-permissions file:///tmp/sg-egress-${SG_ID}.json \
                --region "${REGION}" 2>/dev/null || true
        fi

        rm -f /tmp/sg-ingress-${SG_ID}.json /tmp/sg-egress-${SG_ID}.json
    done

    # Second pass: Delete security groups
    for SG_ID in ${SG_IDS}; do
        log "  Deleting security group: ${SG_ID}"
        aws ec2 delete-security-group --group-id "${SG_ID}" --region "${REGION}" || warn "  Failed to delete ${SG_ID}"
    done

    success "Custom Security Groups deleted"
else
    log "  No custom security groups found"
fi

echo ""

#
# Step 6: Detach and Delete Internet Gateway
#
log "Step 6: Deleting Internet Gateway..."

IGW_IDS=$(aws ec2 describe-internet-gateways \
    --filters "Name=attachment.vpc-id,Values=${VPC_ID}" \
    --query 'InternetGateways[*].InternetGatewayId' \
    --output text \
    --region "${REGION}")

if [[ -n "${IGW_IDS}" ]]; then
    for IGW_ID in ${IGW_IDS}; do
        log "  Detaching Internet Gateway: ${IGW_ID}"
        aws ec2 detach-internet-gateway --internet-gateway-id "${IGW_ID}" --vpc-id "${VPC_ID}" --region "${REGION}"
        log "  Deleting Internet Gateway: ${IGW_ID}"
        aws ec2 delete-internet-gateway --internet-gateway-id "${IGW_ID}" --region "${REGION}"
    done
    success "Internet Gateway deleted"
else
    log "  No Internet Gateway found"
fi

echo ""

#
# Step 7: Delete VPC
#
log "Step 7: Deleting VPC: ${VPC_ID}"
aws ec2 delete-vpc --vpc-id "${VPC_ID}" --region "${REGION}"

echo ""
success "✅ VPC ${VPC_ID} (${VPC_NAME}) deleted successfully!"
echo ""
