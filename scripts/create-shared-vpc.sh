#!/usr/bin/env bash
#
# create-shared-vpc.sh - Create a persistent shared VPC for OpenShift BYOVPC testing
#
# Features:
# - Idempotency guard: fails if VPC name already exists
# - Configurable NAT mode: single (cheap) or ha (3 NAT gateways)
# - OpenShift/Kubernetes subnet tags for ELB discovery
# - Larger private subnets by default
# - Cleanup on failure
# - Writes profile snippet and JSON summary to disk
#
# Usage:
#   ./create-shared-vpc.sh [vpc-name] [region] [nat-mode]
#
# Examples:
#   ./create-shared-vpc.sh
#   ./create-shared-vpc.sh ocpctl-shared-vpc us-east-1
#   ./create-shared-vpc.sh ocpctl-shared-vpc us-east-1 single
#

set -euo pipefail

# -----------------------------
# Configuration
# -----------------------------
VPC_NAME="${1:-ocpctl-shared-vpc}"
REGION="${2:-us-east-1}"
NAT_MODE="${3:-single}"   # single | ha

VPC_CIDR="10.0.0.0/16"

# Larger private subnets for multiple clusters
PRIVATE_CIDRS=(
  "10.0.0.0/22"
  "10.0.4.0/22"
  "10.0.8.0/22"
)

# Smaller public subnets are usually fine
PUBLIC_CIDRS=(
  "10.0.64.0/24"
  "10.0.65.0/24"
  "10.0.66.0/24"
)

OUTPUT_DIR="${PWD}/shared-vpc-output"
SUMMARY_JSON="${OUTPUT_DIR}/${VPC_NAME}-${REGION}-summary.json"
PROFILE_YAML="${OUTPUT_DIR}/${VPC_NAME}-profile.yaml"

# -----------------------------
# Colors
# -----------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# -----------------------------
# Globals for cleanup
# -----------------------------
CREATED_VPC_ID=""
CREATED_IGW_ID=""
CREATED_PUBLIC_RT_ID=""
CREATED_PRIVATE_RT_IDS=()
CREATED_PRIVATE_SUBNET_IDS=()
CREATED_PUBLIC_SUBNET_IDS=()
CREATED_NAT_GW_IDS=()
CREATED_EIP_ALLOC_IDS=()
CLEANUP_ON_ERROR=true

# -----------------------------
# Logging
# -----------------------------
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
    echo -e "${RED}[ERROR]${NC} $1" >&2
    exit 1
}

# -----------------------------
# Cleanup
# -----------------------------
cleanup_on_failure() {
    local exit_code=$?

    if [[ $exit_code -eq 0 || "${CLEANUP_ON_ERROR}" != "true" ]]; then
        return
    fi

    warn "Script failed. Attempting cleanup of resources created in this run..."

    # Delete NAT Gateways first
    for nat_id in "${CREATED_NAT_GW_IDS[@]:-}"; do
        if [[ -n "${nat_id}" ]]; then
            warn "Deleting NAT Gateway ${nat_id}"
            aws ec2 delete-nat-gateway --nat-gateway-id "${nat_id}" --region "${REGION}" >/dev/null 2>&1 || true
        fi
    done

    if [[ ${#CREATED_NAT_GW_IDS[@]} -gt 0 ]]; then
        warn "Waiting briefly for NAT Gateway deletions to propagate..."
        sleep 10
    fi

    # Release EIPs
    for alloc_id in "${CREATED_EIP_ALLOC_IDS[@]:-}"; do
        if [[ -n "${alloc_id}" ]]; then
            warn "Releasing Elastic IP allocation ${alloc_id}"
            aws ec2 release-address --allocation-id "${alloc_id}" --region "${REGION}" >/dev/null 2>&1 || true
        fi
    done

    # Delete route tables (non-main only)
    for rt_id in "${CREATED_PRIVATE_RT_IDS[@]:-}"; do
        if [[ -n "${rt_id}" ]]; then
            warn "Deleting route table ${rt_id}"
            aws ec2 delete-route-table --route-table-id "${rt_id}" --region "${REGION}" >/dev/null 2>&1 || true
        fi
    done

    if [[ -n "${CREATED_PUBLIC_RT_ID}" ]]; then
        warn "Deleting public route table ${CREATED_PUBLIC_RT_ID}"
        aws ec2 delete-route-table --route-table-id "${CREATED_PUBLIC_RT_ID}" --region "${REGION}" >/dev/null 2>&1 || true
    fi

    # Delete subnets
    for subnet_id in "${CREATED_PRIVATE_SUBNET_IDS[@]:-}"; do
        if [[ -n "${subnet_id}" ]]; then
            warn "Deleting subnet ${subnet_id}"
            aws ec2 delete-subnet --subnet-id "${subnet_id}" --region "${REGION}" >/dev/null 2>&1 || true
        fi
    done

    for subnet_id in "${CREATED_PUBLIC_SUBNET_IDS[@]:-}"; do
        if [[ -n "${subnet_id}" ]]; then
            warn "Deleting subnet ${subnet_id}"
            aws ec2 delete-subnet --subnet-id "${subnet_id}" --region "${REGION}" >/dev/null 2>&1 || true
        fi
    done

    # Detach and delete IGW
    if [[ -n "${CREATED_IGW_ID}" && -n "${CREATED_VPC_ID}" ]]; then
        warn "Detaching Internet Gateway ${CREATED_IGW_ID} from ${CREATED_VPC_ID}"
        aws ec2 detach-internet-gateway \
            --internet-gateway-id "${CREATED_IGW_ID}" \
            --vpc-id "${CREATED_VPC_ID}" \
            --region "${REGION}" >/dev/null 2>&1 || true

        warn "Deleting Internet Gateway ${CREATED_IGW_ID}"
        aws ec2 delete-internet-gateway \
            --internet-gateway-id "${CREATED_IGW_ID}" \
            --region "${REGION}" >/dev/null 2>&1 || true
    fi

    # Delete VPC
    if [[ -n "${CREATED_VPC_ID}" ]]; then
        warn "Deleting VPC ${CREATED_VPC_ID}"
        aws ec2 delete-vpc --vpc-id "${CREATED_VPC_ID}" --region "${REGION}" >/dev/null 2>&1 || true
    fi

    warn "Cleanup attempt completed"
    exit "$exit_code"
}

trap cleanup_on_failure EXIT

# -----------------------------
# Validation
# -----------------------------
validate_inputs() {
    case "${NAT_MODE}" in
        single|ha)
            ;;
        *)
            error "Invalid NAT mode '${NAT_MODE}'. Must be 'single' or 'ha'."
            ;;
    esac
}

check_prerequisites() {
    log "Checking prerequisites..."

    command -v aws >/dev/null 2>&1 || error "AWS CLI not found. Please install it first."
    command -v jq >/dev/null 2>&1 || error "jq not found. Please install it first."

    aws sts get-caller-identity >/dev/null 2>&1 || error "AWS credentials not configured."

    mkdir -p "${OUTPUT_DIR}"

    success "Prerequisites check passed"
}

check_existing_vpc() {
    log "Checking for existing VPC named ${VPC_NAME} in ${REGION}..."

    local existing_vpc
    existing_vpc=$(
        aws ec2 describe-vpcs \
            --region "${REGION}" \
            --filters "Name=tag:Name,Values=${VPC_NAME}" \
            --query 'Vpcs[].VpcId' \
            --output text
    )

    if [[ -n "${existing_vpc}" ]]; then
        error "A VPC with name '${VPC_NAME}' already exists in ${REGION}: ${existing_vpc}"
    fi

    success "No existing VPC with that name found"
}

# -----------------------------
# Create resources
# -----------------------------
create_vpc() {
    log "Creating VPC ${VPC_NAME} with CIDR ${VPC_CIDR}..."

    VPC_ID=$(
        aws ec2 create-vpc \
            --cidr-block "${VPC_CIDR}" \
            --region "${REGION}" \
            --tag-specifications "ResourceType=vpc,Tags=[{Key=Name,Value=${VPC_NAME}},{Key=Purpose,Value=shared-cluster-testing},{Key=ManagedBy,Value=ocpctl}]" \
            --query 'Vpc.VpcId' \
            --output text
    )

    [[ -n "${VPC_ID}" ]] || error "Failed to create VPC"

    CREATED_VPC_ID="${VPC_ID}"

    success "Created VPC: ${VPC_ID}"

    aws ec2 modify-vpc-attribute --vpc-id "${VPC_ID}" --enable-dns-hostnames "{\"Value\":true}" --region "${REGION}"
    aws ec2 modify-vpc-attribute --vpc-id "${VPC_ID}" --enable-dns-support "{\"Value\":true}" --region "${REGION}"

    log "Enabled DNS hostnames and DNS resolution"
}

create_internet_gateway() {
    log "Creating Internet Gateway..."

    IGW_ID=$(
        aws ec2 create-internet-gateway \
            --region "${REGION}" \
            --tag-specifications "ResourceType=internet-gateway,Tags=[{Key=Name,Value=${VPC_NAME}-igw},{Key=Purpose,Value=shared-cluster-testing},{Key=ManagedBy,Value=ocpctl}]" \
            --query 'InternetGateway.InternetGatewayId' \
            --output text
    )

    CREATED_IGW_ID="${IGW_ID}"

    aws ec2 attach-internet-gateway \
        --vpc-id "${VPC_ID}" \
        --internet-gateway-id "${IGW_ID}" \
        --region "${REGION}"

    success "Created and attached Internet Gateway: ${IGW_ID}"
}

get_availability_zones() {
    log "Getting up to 3 availability zones in ${REGION}..."

    mapfile -t AZS < <(
        aws ec2 describe-availability-zones \
            --region "${REGION}" \
            --filters "Name=state,Values=available" \
            --query 'AvailabilityZones[0:3].ZoneName' \
            --output text | tr '\t' '\n'
    )

    [[ ${#AZS[@]} -ge 3 ]] || error "Need at least 3 AZs, found ${#AZS[@]}"

    success "Using AZs: ${AZS[*]}"
}

create_subnets() {
    log "Creating subnets..."

    PRIVATE_SUBNET_IDS=()
    PUBLIC_SUBNET_IDS=()

    for i in 0 1 2; do
        local subnet_id

        subnet_id=$(
            aws ec2 create-subnet \
                --vpc-id "${VPC_ID}" \
                --cidr-block "${PRIVATE_CIDRS[$i]}" \
                --availability-zone "${AZS[$i]}" \
                --region "${REGION}" \
                --tag-specifications "ResourceType=subnet,Tags=[{Key=Name,Value=${VPC_NAME}-private-${AZS[$i]}},{Key=Type,Value=private},{Key=Purpose,Value=shared-cluster-testing},{Key=ManagedBy,Value=ocpctl},{Key=kubernetes.io/role/internal-elb,Value=1}]" \
                --query 'Subnet.SubnetId' \
                --output text
        )

        PRIVATE_SUBNET_IDS+=("${subnet_id}")
        CREATED_PRIVATE_SUBNET_IDS+=("${subnet_id}")
        log "Created private subnet ${subnet_id} in ${AZS[$i]} (${PRIVATE_CIDRS[$i]})"
    done

    for i in 0 1 2; do
        local subnet_id

        subnet_id=$(
            aws ec2 create-subnet \
                --vpc-id "${VPC_ID}" \
                --cidr-block "${PUBLIC_CIDRS[$i]}" \
                --availability-zone "${AZS[$i]}" \
                --region "${REGION}" \
                --tag-specifications "ResourceType=subnet,Tags=[{Key=Name,Value=${VPC_NAME}-public-${AZS[$i]}},{Key=Type,Value=public},{Key=Purpose,Value=shared-cluster-testing},{Key=ManagedBy,Value=ocpctl},{Key=kubernetes.io/role/elb,Value=1}]" \
                --query 'Subnet.SubnetId' \
                --output text
        )

        aws ec2 modify-subnet-attribute \
            --subnet-id "${subnet_id}" \
            --map-public-ip-on-launch \
            --region "${REGION}"

        PUBLIC_SUBNET_IDS+=("${subnet_id}")
        CREATED_PUBLIC_SUBNET_IDS+=("${subnet_id}")
        log "Created public subnet ${subnet_id} in ${AZS[$i]} (${PUBLIC_CIDRS[$i]})"
    done

    success "Created ${#PRIVATE_SUBNET_IDS[@]} private and ${#PUBLIC_SUBNET_IDS[@]} public subnets"
}

create_nat_gateways() {
    log "Creating NAT Gateways using mode: ${NAT_MODE}"

    NAT_GW_IDS=()
    local nat_count=3

    if [[ "${NAT_MODE}" == "single" ]]; then
        nat_count=1
    fi

    for ((i=0; i<nat_count; i++)); do
        local eip_alloc_id nat_gw_id

        eip_alloc_id=$(
            aws ec2 allocate-address \
                --domain vpc \
                --region "${REGION}" \
                --tag-specifications "ResourceType=elastic-ip,Tags=[{Key=Name,Value=${VPC_NAME}-nat-eip-${AZS[$i]}},{Key=Purpose,Value=shared-cluster-testing},{Key=ManagedBy,Value=ocpctl}]" \
                --query 'AllocationId' \
                --output text
        )

        CREATED_EIP_ALLOC_IDS+=("${eip_alloc_id}")

        nat_gw_id=$(
            aws ec2 create-nat-gateway \
                --subnet-id "${PUBLIC_SUBNET_IDS[$i]}" \
                --allocation-id "${eip_alloc_id}" \
                --region "${REGION}" \
                --tag-specifications "ResourceType=natgateway,Tags=[{Key=Name,Value=${VPC_NAME}-nat-${AZS[$i]}},{Key=Purpose,Value=shared-cluster-testing},{Key=ManagedBy,Value=ocpctl}]" \
                --query 'NatGateway.NatGatewayId' \
                --output text
        )

        NAT_GW_IDS+=("${nat_gw_id}")
        CREATED_NAT_GW_IDS+=("${nat_gw_id}")
        log "Created NAT Gateway ${nat_gw_id} in ${AZS[$i]}"
    done

    log "Waiting for NAT Gateways to become available..."
    for nat_id in "${NAT_GW_IDS[@]}"; do
        aws ec2 wait nat-gateway-available --nat-gateway-ids "${nat_id}" --region "${REGION}"
    done

    success "NAT Gateways are ready"
}

create_route_tables() {
    log "Creating route tables..."

    PUBLIC_RT_ID=$(
        aws ec2 create-route-table \
            --vpc-id "${VPC_ID}" \
            --region "${REGION}" \
            --tag-specifications "ResourceType=route-table,Tags=[{Key=Name,Value=${VPC_NAME}-public-rt},{Key=Type,Value=public},{Key=ManagedBy,Value=ocpctl}]" \
            --query 'RouteTable.RouteTableId' \
            --output text
    )

    CREATED_PUBLIC_RT_ID="${PUBLIC_RT_ID}"

    aws ec2 create-route \
        --route-table-id "${PUBLIC_RT_ID}" \
        --destination-cidr-block "0.0.0.0/0" \
        --gateway-id "${IGW_ID}" \
        --region "${REGION}" >/dev/null

    for subnet_id in "${PUBLIC_SUBNET_IDS[@]}"; do
        aws ec2 associate-route-table \
            --route-table-id "${PUBLIC_RT_ID}" \
            --subnet-id "${subnet_id}" \
            --region "${REGION}" >/dev/null
    done

    log "Created public route table: ${PUBLIC_RT_ID}"

    PRIVATE_RT_IDS=()

    for i in 0 1 2; do
        local private_rt_id nat_index nat_gw_id

        private_rt_id=$(
            aws ec2 create-route-table \
                --vpc-id "${VPC_ID}" \
                --region "${REGION}" \
                --tag-specifications "ResourceType=route-table,Tags=[{Key=Name,Value=${VPC_NAME}-private-${AZS[$i]}-rt},{Key=Type,Value=private},{Key=ManagedBy,Value=ocpctl}]" \
                --query 'RouteTable.RouteTableId' \
                --output text
        )

        if [[ "${NAT_MODE}" == "single" ]]; then
            nat_index=0
        else
            nat_index=$i
        fi

        nat_gw_id="${NAT_GW_IDS[$nat_index]}"

        aws ec2 create-route \
            --route-table-id "${private_rt_id}" \
            --destination-cidr-block "0.0.0.0/0" \
            --nat-gateway-id "${nat_gw_id}" \
            --region "${REGION}" >/dev/null

        aws ec2 associate-route-table \
            --route-table-id "${private_rt_id}" \
            --subnet-id "${PRIVATE_SUBNET_IDS[$i]}" \
            --region "${REGION}" >/dev/null

        PRIVATE_RT_IDS+=("${private_rt_id}")
        CREATED_PRIVATE_RT_IDS+=("${private_rt_id}")
        log "Created private route table ${private_rt_id} for ${AZS[$i]} via NAT ${nat_gw_id}"
    done

    success "Route tables configured"
}

# -----------------------------
# Outputs
# -----------------------------
write_profile_yaml() {
    log "Writing profile snippet to ${PROFILE_YAML}"

    cat > "${PROFILE_YAML}" <<EOF
name: ${VPC_NAME}
displayName: ${VPC_NAME}
description: Clusters deployed in persistent shared VPC
platform: aws
enabled: true

platformConfig:
  aws:
    instanceMetadataService: required
    rootVolume:
      type: gp3
      size: 120
      iops: 3000
    subnets:
$(for i in 0 1 2; do echo "      - ${PRIVATE_SUBNET_IDS[$i]}  # ${AZS[$i]} private"; done)
$(for i in 0 1 2; do echo "      - ${PUBLIC_SUBNET_IDS[$i]}  # ${AZS[$i]} public"; done)
EOF

    success "Wrote profile YAML: ${PROFILE_YAML}"
}

write_summary_json() {
    log "Writing machine-readable summary to ${SUMMARY_JSON}"

    jq -n \
      --arg vpc_name "${VPC_NAME}" \
      --arg region "${REGION}" \
      --arg vpc_id "${VPC_ID}" \
      --arg vpc_cidr "${VPC_CIDR}" \
      --arg igw_id "${IGW_ID}" \
      --arg nat_mode "${NAT_MODE}" \
      --arg public_rt_id "${PUBLIC_RT_ID}" \
      --arg profile_yaml "${PROFILE_YAML}" \
      --argjson azs "$(printf '%s\n' "${AZS[@]}" | jq -R . | jq -s .)" \
      --argjson private_subnet_ids "$(printf '%s\n' "${PRIVATE_SUBNET_IDS[@]}" | jq -R . | jq -s .)" \
      --argjson public_subnet_ids "$(printf '%s\n' "${PUBLIC_SUBNET_IDS[@]}" | jq -R . | jq -s .)" \
      --argjson private_cidrs "$(printf '%s\n' "${PRIVATE_CIDRS[@]}" | jq -R . | jq -s .)" \
      --argjson public_cidrs "$(printf '%s\n' "${PUBLIC_CIDRS[@]}" | jq -R . | jq -s .)" \
      --argjson nat_gateway_ids "$(printf '%s\n' "${NAT_GW_IDS[@]}" | jq -R . | jq -s .)" \
      --argjson private_route_table_ids "$(printf '%s\n' "${PRIVATE_RT_IDS[@]}" | jq -R . | jq -s .)" \
      '
      {
        vpc_name: $vpc_name,
        region: $region,
        vpc_id: $vpc_id,
        vpc_cidr: $vpc_cidr,
        internet_gateway_id: $igw_id,
        nat_mode: $nat_mode,
        public_route_table_id: $public_rt_id,
        availability_zones: $azs,
        private_subnet_ids: $private_subnet_ids,
        public_subnet_ids: $public_subnet_ids,
        private_cidrs: $private_cidrs,
        public_cidrs: $public_cidrs,
        nat_gateway_ids: $nat_gateway_ids,
        private_route_table_ids: $private_route_table_ids,
        profile_yaml: $profile_yaml
      }
      ' > "${SUMMARY_JSON}"

    success "Wrote summary JSON: ${SUMMARY_JSON}"
}

output_summary() {
    echo
    echo "=================================================="
    success "Shared VPC Created Successfully"
    echo "=================================================="
    echo
    echo "VPC:"
    echo "  Name:           ${VPC_NAME}"
    echo "  ID:             ${VPC_ID}"
    echo "  Region:         ${REGION}"
    echo "  CIDR:           ${VPC_CIDR}"
    echo "  NAT Mode:       ${NAT_MODE}"
    echo
    echo "Availability Zones:"
    for az in "${AZS[@]}"; do
        echo "  - ${az}"
    done
    echo
    echo "Private Subnets:"
    for i in 0 1 2; do
        echo "  - ${PRIVATE_SUBNET_IDS[$i]}  (${AZS[$i]}, ${PRIVATE_CIDRS[$i]})"
    done
    echo
    echo "Public Subnets:"
    for i in 0 1 2; do
        echo "  - ${PUBLIC_SUBNET_IDS[$i]}  (${AZS[$i]}, ${PUBLIC_CIDRS[$i]})"
    done
    echo
    echo "NAT Gateways:"
    for i in "${!NAT_GW_IDS[@]}"; do
        echo "  - ${NAT_GW_IDS[$i]}"
    done
    echo
    echo "Internet Gateway:"
    echo "  - ${IGW_ID}"
    echo
    echo "Artifacts:"
    echo "  Profile YAML:   ${PROFILE_YAML}"
    echo "  Summary JSON:   ${SUMMARY_JSON}"
    echo
    echo "Next steps:"
    echo "  1. Review ${PROFILE_YAML}"
    echo "  2. Copy it into your profile definitions directory"
    echo "  3. Restart ocpctl-server if needed"
    echo "  4. Use the subnet IDs for BYOVPC cluster creation"
    echo
    echo "Deletion:"
    echo "  Use delete-shared-vpc.sh or delete resources in reverse dependency order."
    echo
}

# -----------------------------
# Main
# -----------------------------
main() {
    echo "=================================================="
    echo "Creating Persistent Shared VPC for OpenShift BYOVPC"
    echo "=================================================="
    echo

    validate_inputs
    check_prerequisites
    check_existing_vpc
    create_vpc
    create_internet_gateway
    get_availability_zones
    create_subnets
    create_nat_gateways
    create_route_tables
    write_profile_yaml
    write_summary_json

    CLEANUP_ON_ERROR=false
    output_summary
}

main "$@"
