#!/bin/bash
set -e

# Add oc to PATH if needed
export PATH="$HOME/.local/bin:$PATH"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
VM_COUNT=5
VM_PREFIX="windows-test"
VM_NAMESPACE="default"
TEMPLATE_NAME="windows10-oadp-vm"
TEMPLATE_NAMESPACE="openshift-virtualization-os-images"
STORAGE_CLASS="gp3-csi-wfc"
SEPARATE_NAMESPACES=false

# Parse command-line flags
POSITIONAL_ARGS=()
while [[ $# -gt 0 ]]; do
  case $1 in
    -s|--separate-namespaces)
      SEPARATE_NAMESPACES=true
      shift
      ;;
    *)
      POSITIONAL_ARGS+=("$1")
      shift
      ;;
  esac
done
set -- "${POSITIONAL_ARGS[@]}"

# Function to print colored messages
log_info() {
    echo -e "${BLUE}$1${NC}"
}

log_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

log_warning() {
    echo -e "${YELLOW}⚠ $1${NC}"
}

log_error() {
    echo -e "${RED}✗ $1${NC}"
}

# Function to get namespace for a VM
get_vm_namespace() {
    local vm_index=$1
    if [ "$SEPARATE_NAMESPACES" = true ]; then
        echo "${VM_PREFIX}-${vm_index}-ns"
    else
        echo "$VM_NAMESPACE"
    fi
}

# Function to ensure namespace exists
ensure_namespace() {
    local ns=$1
    if ! oc get namespace "$ns" &>/dev/null; then
        log_info "Creating namespace: $ns"
        oc create namespace "$ns"
        log_success "Created namespace: $ns"
    fi
}

# Function to create VMs
create_vms() {
    if [ "$SEPARATE_NAMESPACES" = true ]; then
        log_info "Creating $VM_COUNT Windows VMs in separate namespaces..."
    else
        log_info "Creating $VM_COUNT Windows VMs in namespace: $VM_NAMESPACE..."
    fi
    echo ""

    for i in $(seq 1 $VM_COUNT); do
        VM_NAME="${VM_PREFIX}-${i}"
        NS=$(get_vm_namespace $i)

        # Ensure namespace exists
        ensure_namespace "$NS"

        log_info "Creating VM: $VM_NAME in namespace: $NS"

        if oc get vm "$VM_NAME" -n "$NS" &>/dev/null; then
            log_warning "VM $VM_NAME already exists in namespace $NS, skipping"
        else
            oc process "$TEMPLATE_NAME" -n "$TEMPLATE_NAMESPACE" \
                -p VM_NAME="$VM_NAME" \
                -p VM_NAMESPACE="$NS" \
                -p STORAGE_CLASS="$STORAGE_CLASS" | oc apply -f -

            log_success "Created VM: $VM_NAME in namespace: $NS"
        fi
    done

    echo ""
    log_success "VM creation completed"
}

# Function to wait for VMs to be provisioned
wait_for_provisioning() {
    log_info "Waiting for VMs to provision (disk cloning)..."
    echo ""

    for i in $(seq 1 $VM_COUNT); do
        VM_NAME="${VM_PREFIX}-${i}"
        DV_NAME="${VM_NAME}-disk"
        NS=$(get_vm_namespace $i)

        log_info "Waiting for $VM_NAME disk to provision in namespace: $NS..."

        TIMEOUT=3600  # 60 minutes
        ELAPSED=0

        while [ $ELAPSED -lt $TIMEOUT ]; do
            PHASE=$(oc get datavolume "$DV_NAME" -n "$NS" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
            PROGRESS=$(oc get datavolume "$DV_NAME" -n "$NS" -o jsonpath='{.status.progress}' 2>/dev/null || echo "N/A")

            if [ "$PHASE" = "Succeeded" ]; then
                log_success "$VM_NAME disk provisioned"
                break
            elif [ "$PHASE" = "Failed" ]; then
                log_error "$VM_NAME disk provisioning failed"
                oc get datavolume "$DV_NAME" -n "$NS" -o yaml | grep -A10 "conditions:" || true
                exit 1
            fi

            # Log progress every 60 seconds
            if [ $((ELAPSED % 60)) -eq 0 ]; then
                ELAPSED_MIN=$((ELAPSED / 60))
                echo "  Progress: $PROGRESS | Elapsed: ${ELAPSED_MIN}m"
            fi

            sleep 10
            ELAPSED=$((ELAPSED + 10))
        done

        if [ $ELAPSED -ge $TIMEOUT ]; then
            log_error "$VM_NAME disk provisioning timed out"
            exit 1
        fi
    done

    echo ""
    log_success "All VMs provisioned successfully"
}

# Function to start VMs
start_vms() {
    log_info "Starting all VMs..."
    echo ""

    for i in $(seq 1 $VM_COUNT); do
        VM_NAME="${VM_PREFIX}-${i}"
        NS=$(get_vm_namespace $i)

        log_info "Starting VM: $VM_NAME in namespace: $NS"
        oc patch vm "$VM_NAME" -n "$NS" --type=merge -p '{"spec":{"runStrategy":"RerunOnFailure"}}'
        log_success "Started VM: $VM_NAME"
    done

    echo ""
    log_success "All VMs started"
}

# Function to stop VMs
stop_vms() {
    log_info "Stopping all VMs..."
    echo ""

    for i in $(seq 1 $VM_COUNT); do
        VM_NAME="${VM_PREFIX}-${i}"
        NS=$(get_vm_namespace $i)

        log_info "Stopping VM: $VM_NAME in namespace: $NS"
        oc patch vm "$VM_NAME" -n "$NS" --type=merge -p '{"spec":{"runStrategy":"Halted"}}'
        log_success "Stopped VM: $VM_NAME"
    done

    echo ""
    log_success "All VMs stopped"
}

# Function to delete VMs
delete_vms() {
    if [ "$SEPARATE_NAMESPACES" = true ]; then
        log_warning "This will delete all $VM_COUNT VMs, their disks, and their namespaces"
    else
        log_warning "This will delete all $VM_COUNT VMs and their disks"
    fi
    read -p "Are you sure? (yes/no): " CONFIRM

    if [ "$CONFIRM" != "yes" ]; then
        log_info "Deletion cancelled"
        exit 0
    fi

    log_info "Deleting all VMs..."
    echo ""

    for i in $(seq 1 $VM_COUNT); do
        VM_NAME="${VM_PREFIX}-${i}"
        NS=$(get_vm_namespace $i)

        if oc get vm "$VM_NAME" -n "$NS" &>/dev/null; then
            log_info "Deleting VM: $VM_NAME from namespace: $NS"
            oc delete vm "$VM_NAME" -n "$NS"
            log_success "Deleted VM: $VM_NAME"

            # Delete namespace if using separate namespaces
            if [ "$SEPARATE_NAMESPACES" = true ]; then
                log_info "Deleting namespace: $NS"
                oc delete namespace "$NS" --wait=false
                log_success "Deleted namespace: $NS"
            fi
        else
            log_warning "VM $VM_NAME not found in namespace $NS, skipping"
        fi
    done

    echo ""
    log_success "All VMs deleted"
}

# Function to show VM status
status_vms() {
    log_info "VM Status:"
    echo ""

    if [ "$SEPARATE_NAMESPACES" = true ]; then
        printf "%-20s %-20s %-15s %-10s %-15s\n" "NAME" "NAMESPACE" "STATUS" "READY" "DISK PHASE"
        printf "%-20s %-20s %-15s %-10s %-15s\n" "----" "---------" "------" "-----" "----------"
    else
        printf "%-20s %-15s %-10s %-15s\n" "NAME" "STATUS" "READY" "DISK PHASE"
        printf "%-20s %-15s %-10s %-15s\n" "----" "------" "-----" "----------"
    fi

    RUNNING_VMS=0

    for i in $(seq 1 $VM_COUNT); do
        VM_NAME="${VM_PREFIX}-${i}"
        DV_NAME="${VM_NAME}-disk"
        NS=$(get_vm_namespace $i)

        if oc get vm "$VM_NAME" -n "$NS" &>/dev/null; then
            VM_STATUS=$(oc get vm "$VM_NAME" -n "$NS" -o jsonpath='{.status.printableStatus}' 2>/dev/null || echo "Unknown")
            VM_READY=$(oc get vm "$VM_NAME" -n "$NS" -o jsonpath='{.status.ready}' 2>/dev/null || echo "false")
            DV_PHASE=$(oc get datavolume "$DV_NAME" -n "$NS" -o jsonpath='{.status.phase}' 2>/dev/null || echo "N/A")

            if [ "$SEPARATE_NAMESPACES" = true ]; then
                printf "%-20s %-20s %-15s %-10s %-15s\n" "$VM_NAME" "$NS" "$VM_STATUS" "$VM_READY" "$DV_PHASE"
            else
                printf "%-20s %-15s %-10s %-15s\n" "$VM_NAME" "$VM_STATUS" "$VM_READY" "$DV_PHASE"
            fi

            # Count running VMs
            if [ "$VM_STATUS" = "Running" ]; then
                RUNNING_VMS=$((RUNNING_VMS + 1))
            fi
        else
            if [ "$SEPARATE_NAMESPACES" = true ]; then
                printf "%-20s %-20s %-15s %-10s %-15s\n" "$VM_NAME" "$NS" "Not Found" "N/A" "N/A"
            else
                printf "%-20s %-15s %-10s %-15s\n" "$VM_NAME" "Not Found" "N/A" "N/A"
            fi
        fi
    done

    echo ""

    # Show resource usage
    log_info "Resource Usage (for running VMs):"

    TOTAL_CPU=$((RUNNING_VMS * 4))
    TOTAL_RAM=$((RUNNING_VMS * 8))

    echo "  Running VMs: $RUNNING_VMS"
    echo "  Total vCPUs: $TOTAL_CPU"
    echo "  Total RAM:   ${TOTAL_RAM}Gi"
}

# Main script
case "${1:-}" in
    create)
        create_vms
        ;;
    wait)
        wait_for_provisioning
        ;;
    start)
        start_vms
        ;;
    stop)
        stop_vms
        ;;
    delete)
        delete_vms
        ;;
    status)
        status_vms
        ;;
    deploy)
        create_vms
        wait_for_provisioning
        start_vms
        echo ""
        log_success "Deployment complete! VMs are starting up."
        echo ""
        status_vms
        ;;
    *)
        echo "Usage: $0 [-s|--separate-namespaces] {create|wait|start|stop|delete|status|deploy}"
        echo ""
        echo "Flags:"
        echo "  -s, --separate-namespaces  Create each VM in its own namespace (e.g., windows-test-1-ns)"
        echo ""
        echo "Commands:"
        echo "  create  - Create $VM_COUNT VMs (without starting them)"
        echo "  wait    - Wait for all VM disks to finish provisioning"
        echo "  start   - Start all VMs"
        echo "  stop    - Stop all VMs"
        echo "  delete  - Delete all VMs and their disks (and namespaces if -s used)"
        echo "  status  - Show status of all VMs"
        echo "  deploy  - Create, wait for provisioning, and start VMs (all-in-one)"
        echo ""
        echo "Examples:"
        echo "  $0 deploy                        # Full deployment in default namespace (recommended)"
        echo "  $0 -s deploy                     # Full deployment with separate namespaces"
        echo "  $0 --separate-namespaces create  # Create VMs in separate namespaces"
        echo "  $0 status                        # Check VM status"
        echo "  $0 -s status                     # Check VM status (separate namespaces)"
        echo "  $0 stop                          # Stop all VMs"
        echo "  $0 -s delete                     # Delete all VMs and their namespaces"
        exit 1
        ;;
esac
