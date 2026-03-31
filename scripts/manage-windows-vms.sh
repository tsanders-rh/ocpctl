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
STORAGE_CLASS="gp3-csi-immediate"

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

# Function to create VMs
create_vms() {
    log_info "Creating $VM_COUNT Windows VMs..."
    echo ""

    for i in $(seq 1 $VM_COUNT); do
        VM_NAME="${VM_PREFIX}-${i}"

        log_info "Creating VM: $VM_NAME"

        if oc get vm "$VM_NAME" -n "$VM_NAMESPACE" &>/dev/null; then
            log_warning "VM $VM_NAME already exists, skipping"
        else
            oc process "$TEMPLATE_NAME" -n "$TEMPLATE_NAMESPACE" \
                -p VM_NAME="$VM_NAME" \
                -p VM_NAMESPACE="$VM_NAMESPACE" \
                -p STORAGE_CLASS="$STORAGE_CLASS" | oc apply -f -

            log_success "Created VM: $VM_NAME"
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

        log_info "Waiting for $VM_NAME disk to provision..."

        TIMEOUT=3600  # 60 minutes
        ELAPSED=0

        while [ $ELAPSED -lt $TIMEOUT ]; do
            PHASE=$(oc get datavolume "$DV_NAME" -n "$VM_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "NotFound")
            PROGRESS=$(oc get datavolume "$DV_NAME" -n "$VM_NAMESPACE" -o jsonpath='{.status.progress}' 2>/dev/null || echo "N/A")

            if [ "$PHASE" = "Succeeded" ]; then
                log_success "$VM_NAME disk provisioned"
                break
            elif [ "$PHASE" = "Failed" ]; then
                log_error "$VM_NAME disk provisioning failed"
                oc get datavolume "$DV_NAME" -n "$VM_NAMESPACE" -o yaml | grep -A10 "conditions:" || true
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

        log_info "Starting VM: $VM_NAME"
        oc patch vm "$VM_NAME" -n "$VM_NAMESPACE" --type=merge -p '{"spec":{"runStrategy":"RerunOnFailure"}}'
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

        log_info "Stopping VM: $VM_NAME"
        oc patch vm "$VM_NAME" -n "$VM_NAMESPACE" --type=merge -p '{"spec":{"runStrategy":"Halted"}}'
        log_success "Stopped VM: $VM_NAME"
    done

    echo ""
    log_success "All VMs stopped"
}

# Function to delete VMs
delete_vms() {
    log_warning "This will delete all $VM_COUNT VMs and their disks"
    read -p "Are you sure? (yes/no): " CONFIRM

    if [ "$CONFIRM" != "yes" ]; then
        log_info "Deletion cancelled"
        exit 0
    fi

    log_info "Deleting all VMs..."
    echo ""

    for i in $(seq 1 $VM_COUNT); do
        VM_NAME="${VM_PREFIX}-${i}"

        if oc get vm "$VM_NAME" -n "$VM_NAMESPACE" &>/dev/null; then
            log_info "Deleting VM: $VM_NAME"
            oc delete vm "$VM_NAME" -n "$VM_NAMESPACE"
            log_success "Deleted VM: $VM_NAME"
        else
            log_warning "VM $VM_NAME not found, skipping"
        fi
    done

    echo ""
    log_success "All VMs deleted"
}

# Function to show VM status
status_vms() {
    log_info "VM Status:"
    echo ""

    printf "%-20s %-15s %-10s %-15s\n" "NAME" "STATUS" "READY" "DISK PHASE"
    printf "%-20s %-15s %-10s %-15s\n" "----" "------" "-----" "----------"

    for i in $(seq 1 $VM_COUNT); do
        VM_NAME="${VM_PREFIX}-${i}"
        DV_NAME="${VM_NAME}-disk"

        if oc get vm "$VM_NAME" -n "$VM_NAMESPACE" &>/dev/null; then
            VM_STATUS=$(oc get vm "$VM_NAME" -n "$VM_NAMESPACE" -o jsonpath='{.status.printableStatus}' 2>/dev/null || echo "Unknown")
            VM_READY=$(oc get vm "$VM_NAME" -n "$VM_NAMESPACE" -o jsonpath='{.status.ready}' 2>/dev/null || echo "false")
            DV_PHASE=$(oc get datavolume "$DV_NAME" -n "$VM_NAMESPACE" -o jsonpath='{.status.phase}' 2>/dev/null || echo "N/A")

            printf "%-20s %-15s %-10s %-15s\n" "$VM_NAME" "$VM_STATUS" "$VM_READY" "$DV_PHASE"
        else
            printf "%-20s %-15s %-10s %-15s\n" "$VM_NAME" "Not Found" "N/A" "N/A"
        fi
    done

    echo ""

    # Show resource usage
    log_info "Resource Usage (for running VMs):"
    RUNNING_VMS=$(oc get vm -n "$VM_NAMESPACE" -l "vm.kubevirt.io/name" -o json | \
        jq -r '.items[] | select(.status.printableStatus == "Running") | .metadata.name' | wc -l)

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
        echo "Usage: $0 {create|wait|start|stop|delete|status|deploy}"
        echo ""
        echo "Commands:"
        echo "  create  - Create $VM_COUNT VMs (without starting them)"
        echo "  wait    - Wait for all VM disks to finish provisioning"
        echo "  start   - Start all VMs"
        echo "  stop    - Stop all VMs"
        echo "  delete  - Delete all VMs and their disks"
        echo "  status  - Show status of all VMs"
        echo "  deploy  - Create, wait for provisioning, and start VMs (all-in-one)"
        echo ""
        echo "Examples:"
        echo "  $0 deploy           # Full deployment (recommended)"
        echo "  $0 create           # Just create VMs"
        echo "  $0 status           # Check VM status"
        echo "  $0 stop             # Stop all VMs"
        echo "  $0 delete           # Delete all VMs"
        exit 1
        ;;
esac
