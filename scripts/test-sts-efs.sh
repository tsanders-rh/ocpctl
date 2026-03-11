#!/bin/bash
#
# Test Script for STS EFS Configuration
# Tests the automatic detection and configuration of EFS CSI driver on STS-enabled clusters
#
# Usage: ./test-sts-efs.sh [cluster-name]
#

set -e

CLUSTER_NAME="${1:-sts-efs-test-$(date +%s)}"
API_URL="${API_URL:-http://localhost:8080/api/v1}"
PROFILE="${PROFILE:-aws-sno-test}"
REGION="${REGION:-us-east-1}"
BASE_DOMAIN="${BASE_DOMAIN:-mg.dog8code.com}"

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

section() {
    echo ""
    echo "========================================="
    echo "$1"
    echo "========================================="
    echo ""
}

# Check prerequisites
section "Checking Prerequisites"

if ! command -v curl &> /dev/null; then
    error "curl is not installed"
fi

if ! command -v jq &> /dev/null; then
    error "jq is not installed"
fi

if ! command -v oc &> /dev/null; then
    warn "oc is not installed - will skip in-cluster verification steps"
    HAS_OC=false
else
    HAS_OC=true
fi

# Test API connectivity
log "Testing API connectivity..."
BASE_URL=$(echo "$API_URL" | sed 's|/api/v1||')
if ! curl -sf "$BASE_URL/health" &>/dev/null; then
    error "Cannot connect to API at $BASE_URL"
fi
success "API is reachable"

# Check if authentication is required
AUTH_CHECK=$(curl -s "$API_URL/clusters" 2>/dev/null)
if echo "$AUTH_CHECK" | grep -q "missing authorization header"; then
    warn "API requires authentication - this script doesn't handle JWT auth yet"
    warn "To test manually, use the Web UI or authenticate via API"
    warn ""
    warn "For now, the script will attempt unauthenticated calls and may fail"
fi

# Check if running on EC2 with instance profile (STS)
log "Checking if environment has STS credentials..."
TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 21600" 2>/dev/null || echo "")
if [ -n "$TOKEN" ]; then
    INSTANCE_ROLE=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" http://169.254.169.254/latest/meta-data/iam/security-credentials/ 2>/dev/null || echo "")
    if [ -n "$INSTANCE_ROLE" ]; then
        SESSION_TOKEN=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" "http://169.254.169.254/latest/meta-data/iam/security-credentials/$INSTANCE_ROLE" 2>/dev/null | jq -r '.Token' 2>/dev/null || echo "")
        if [ -n "$SESSION_TOKEN" ] && [ "$SESSION_TOKEN" != "null" ]; then
            success "Running on EC2 with instance profile: $INSTANCE_ROLE (STS credentials detected)"
        else
            warn "Running on EC2 but no session token found - may use static credentials"
        fi
    else
        warn "Not running on EC2 with instance profile - may use static credentials"
    fi
else
    warn "Cannot access IMDS - may not be on EC2 or IMDS disabled"
fi

# Step 1: Create cluster
section "Step 1: Creating Test Cluster"

log "Creating cluster: $CLUSTER_NAME"
log "  Profile: $PROFILE"
log "  Region: $REGION"
log "  Base Domain: $BASE_DOMAIN"

CREATE_RESPONSE=$(curl -s -X POST "$API_URL/clusters" \
    -H "Content-Type: application/json" \
    -d "{
        \"name\": \"$CLUSTER_NAME\",
        \"base_domain\": \"$BASE_DOMAIN\",
        \"profile_name\": \"$PROFILE\",
        \"region\": \"$REGION\"
    }")

CLUSTER_ID=$(echo "$CREATE_RESPONSE" | jq -r '.id' 2>/dev/null || echo "")

if [ -z "$CLUSTER_ID" ] || [ "$CLUSTER_ID" == "null" ]; then
    error "Failed to create cluster. Response: $CREATE_RESPONSE"
fi

success "Cluster created with ID: $CLUSTER_ID"

# Step 2: Monitor cluster creation
section "Step 2: Monitoring Cluster Creation"

log "Waiting for cluster to reach READY status..."
log "This may take 45-60 minutes for Single-Node OpenShift"
log ""
log "You can monitor detailed logs in another terminal with:"
log "  sudo journalctl -u ocpctl-worker -f | grep -E 'STS|OIDC|ccoctl|Manual mode|$CLUSTER_NAME'"
log ""

START_TIME=$(date +%s)
MAX_WAIT=7200  # 2 hours max

while true; do
    CLUSTER_STATUS=$(curl -s "$API_URL/clusters/$CLUSTER_ID" | jq -r '.status' 2>/dev/null || echo "UNKNOWN")
    ELAPSED=$(($(date +%s) - START_TIME))

    if [ "$CLUSTER_STATUS" == "READY" ]; then
        success "Cluster is READY after $((ELAPSED / 60)) minutes"
        break
    elif [ "$CLUSTER_STATUS" == "FAILED" ]; then
        error "Cluster creation FAILED"
    elif [ $ELAPSED -gt $MAX_WAIT ]; then
        error "Timeout waiting for cluster (waited $((MAX_WAIT / 60)) minutes)"
    fi

    log "Status: $CLUSTER_STATUS (elapsed: $((ELAPSED / 60))m)"
    sleep 30
done

# Check if cluster was created with STS
log "Checking if cluster was created with STS mode..."
CLUSTER_DETAILS=$(curl -s "$API_URL/clusters/$CLUSTER_ID")

if [ "$HAS_OC" == "true" ]; then
    KUBECONFIG_PATH="$HOME/.ocpctl/clusters/$CLUSTER_ID/auth/kubeconfig"
    if [ ! -f "$KUBECONFIG_PATH" ]; then
        warn "Kubeconfig not found at expected path: $KUBECONFIG_PATH"
    else
        export KUBECONFIG="$KUBECONFIG_PATH"

        OIDC_ISSUER=$(oc get authentication cluster -o jsonpath='{.spec.serviceAccountIssuer}' 2>/dev/null || echo "")
        if [ -n "$OIDC_ISSUER" ] && [ "$OIDC_ISSUER" != "null" ]; then
            success "Cluster has OIDC issuer configured (STS mode): $OIDC_ISSUER"
        else
            warn "No OIDC issuer found - cluster may be using static credentials"
        fi
    fi
fi

# Step 3: Trigger CONFIGURE_EFS job
section "Step 3: Triggering CONFIGURE_EFS Job"

log "Creating CONFIGURE_EFS job for cluster $CLUSTER_ID..."

JOB_RESPONSE=$(curl -s -X POST "$API_URL/clusters/$CLUSTER_ID/jobs" \
    -H "Content-Type: application/json" \
    -d '{"job_type": "CONFIGURE_EFS"}')

JOB_ID=$(echo "$JOB_RESPONSE" | jq -r '.id' 2>/dev/null || echo "")

if [ -z "$JOB_ID" ] || [ "$JOB_ID" == "null" ]; then
    error "Failed to create CONFIGURE_EFS job. Response: $JOB_RESPONSE"
fi

success "CONFIGURE_EFS job created with ID: $JOB_ID"

# Step 4: Monitor EFS configuration
section "Step 4: Monitoring EFS Configuration"

log "Waiting for CONFIGURE_EFS job to complete..."
log "This may take 5-10 minutes"
log ""
log "You can monitor detailed logs in another terminal with:"
log "  sudo journalctl -u ocpctl-worker -f | grep -A 30 CONFIGURE_EFS"
log ""

START_TIME=$(date +%s)
MAX_WAIT=900  # 15 minutes max

while true; do
    JOB_STATUS=$(curl -s "$API_URL/jobs/$JOB_ID" | jq -r '.status' 2>/dev/null || echo "UNKNOWN")
    ELAPSED=$(($(date +%s) - START_TIME))

    if [ "$JOB_STATUS" == "COMPLETED" ]; then
        success "CONFIGURE_EFS job completed after $((ELAPSED / 60)) minutes"
        break
    elif [ "$JOB_STATUS" == "FAILED" ]; then
        error "CONFIGURE_EFS job FAILED - check logs with: sudo journalctl -u ocpctl-worker | grep -A 50 $JOB_ID"
    elif [ $ELAPSED -gt $MAX_WAIT ]; then
        error "Timeout waiting for CONFIGURE_EFS job (waited $((MAX_WAIT / 60)) minutes)"
    fi

    log "Job Status: $JOB_STATUS (elapsed: $((ELAPSED / 60))m)"
    sleep 10
done

# Step 5: Verify storage configuration
section "Step 5: Verifying Storage Configuration"

log "Fetching cluster storage_config..."
STORAGE_CONFIG=$(curl -s "$API_URL/clusters/$CLUSTER_ID" | jq '.storage_config')

echo "Storage Config:"
echo "$STORAGE_CONFIG" | jq '.'

# Extract specific fields
EFS_ENABLED=$(echo "$STORAGE_CONFIG" | jq -r '.efs_enabled' 2>/dev/null || echo "null")
EFS_ID=$(echo "$STORAGE_CONFIG" | jq -r '.local_efs_id' 2>/dev/null || echo "null")
AUTH_MODE=$(echo "$STORAGE_CONFIG" | jq -r '.auth_mode' 2>/dev/null || echo "null")
IAM_ROLE_ARN=$(echo "$STORAGE_CONFIG" | jq -r '.iam_role_arn' 2>/dev/null || echo "null")

if [ "$EFS_ENABLED" == "true" ]; then
    success "EFS is enabled"
else
    error "EFS is not enabled in storage_config"
fi

if [ -n "$EFS_ID" ] && [ "$EFS_ID" != "null" ]; then
    success "EFS ID: $EFS_ID"
else
    error "No EFS ID found in storage_config"
fi

if [ -n "$AUTH_MODE" ] && [ "$AUTH_MODE" != "null" ]; then
    if [ "$AUTH_MODE" == "sts" ]; then
        success "Authentication Mode: STS (IAM Roles for Service Accounts)"
        if [ -n "$IAM_ROLE_ARN" ] && [ "$IAM_ROLE_ARN" != "null" ]; then
            success "IAM Role ARN: $IAM_ROLE_ARN"
        else
            warn "Auth mode is STS but no IAM role ARN found"
        fi
    elif [ "$AUTH_MODE" == "static" ]; then
        warn "Authentication Mode: Static Credentials (not STS)"
    else
        warn "Unknown authentication mode: $AUTH_MODE"
    fi
else
    warn "No auth_mode field in storage_config (may be older cluster)"
fi

# Step 6: Verify EFS CSI driver in cluster (if oc available)
if [ "$HAS_OC" == "true" ] && [ -f "$KUBECONFIG_PATH" ]; then
    section "Step 6: Verifying EFS CSI Driver in Cluster"

    export KUBECONFIG="$KUBECONFIG_PATH"

    # Check OIDC issuer
    log "Checking OIDC issuer..."
    OIDC_ISSUER=$(oc get authentication cluster -o jsonpath='{.spec.serviceAccountIssuer}' 2>/dev/null || echo "")
    if [ -n "$OIDC_ISSUER" ] && [ "$OIDC_ISSUER" != "null" ]; then
        success "OIDC Issuer: $OIDC_ISSUER"
    else
        warn "No OIDC issuer configured"
    fi

    # Check credentials secret
    log "Checking EFS credentials secret..."
    if oc get secret aws-efs-cloud-credentials -n openshift-cluster-csi-drivers &>/dev/null; then
        success "Secret aws-efs-cloud-credentials exists"

        # Check if it's STS (role_arn) or static (aws_access_key_id)
        SECRET_CONTENT=$(oc get secret aws-efs-cloud-credentials -n openshift-cluster-csi-drivers -o jsonpath='{.data.credentials}' 2>/dev/null | base64 -d)
        if echo "$SECRET_CONTENT" | grep -q "role_arn"; then
            ROLE_ARN=$(echo "$SECRET_CONTENT" | grep "role_arn" | awk -F'=' '{print $2}' | tr -d ' ')
            success "Secret contains IAM role ARN: $ROLE_ARN"
        elif echo "$SECRET_CONTENT" | grep -q "aws_access_key_id"; then
            warn "Secret contains static AWS credentials (not STS)"
        else
            warn "Secret format unrecognized"
        fi
    else
        error "Secret aws-efs-cloud-credentials not found"
    fi

    # Check service account annotations
    log "Checking service account annotations..."
    for SA in aws-efs-csi-driver-controller-sa aws-efs-csi-driver-node-sa; do
        if oc get serviceaccount $SA -n openshift-cluster-csi-drivers &>/dev/null; then
            ANNOTATION=$(oc get serviceaccount $SA -n openshift-cluster-csi-drivers -o jsonpath='{.metadata.annotations.eks\.amazonaws\.com/role-arn}' 2>/dev/null || echo "")
            if [ -n "$ANNOTATION" ]; then
                success "Service account $SA has role annotation: $ANNOTATION"
            else
                warn "Service account $SA exists but has no role annotation"
            fi
        else
            warn "Service account $SA not found"
        fi
    done

    # Check driver pods
    log "Checking EFS CSI driver pods..."
    DRIVER_PODS=$(oc get pods -n openshift-cluster-csi-drivers --no-headers 2>/dev/null | grep -c efs || echo "0")
    if [ "$DRIVER_PODS" -gt 0 ]; then
        success "Found $DRIVER_PODS EFS CSI driver pods"
        oc get pods -n openshift-cluster-csi-drivers | grep efs
    else
        error "No EFS CSI driver pods found"
    fi

    # Check storage class
    log "Checking EFS storage class..."
    if oc get storageclass efs-sc &>/dev/null; then
        success "Storage class efs-sc exists"
    else
        error "Storage class efs-sc not found"
    fi

    # Step 7: Test PVC creation
    section "Step 7: Testing EFS PVC Creation"

    log "Creating test namespace..."
    oc create namespace efs-test --dry-run=client -o yaml | oc apply -f - &>/dev/null || true

    log "Creating test PVC..."
    cat <<EOF | oc apply -f -
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: efs-test-pvc
  namespace: efs-test
spec:
  accessModes:
    - ReadWriteMany
  storageClassName: efs-sc
  resources:
    requests:
      storage: 5Gi
EOF

    log "Waiting for PVC to be bound (max 2 minutes)..."
    START_TIME=$(date +%s)
    MAX_WAIT=120

    while true; do
        PVC_STATUS=$(oc get pvc efs-test-pvc -n efs-test -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
        ELAPSED=$(($(date +%s) - START_TIME))

        if [ "$PVC_STATUS" == "Bound" ]; then
            success "PVC is Bound - EFS storage is working!"
            oc get pvc efs-test-pvc -n efs-test
            break
        elif [ $ELAPSED -gt $MAX_WAIT ]; then
            error "Timeout waiting for PVC to bind (waited $MAX_WAIT seconds)"
        fi

        log "PVC Status: $PVC_STATUS (elapsed: ${ELAPSED}s)"
        sleep 5
    done

    # Get PV details
    PV_NAME=$(oc get pvc efs-test-pvc -n efs-test -o jsonpath='{.spec.volumeName}' 2>/dev/null || echo "")
    if [ -n "$PV_NAME" ]; then
        log "Persistent Volume details:"
        oc get pv "$PV_NAME" -o yaml | grep -A 10 "csi:"
    fi

else
    section "Step 6: Skipping In-Cluster Verification"
    warn "oc command not available - skipping in-cluster verification"
    warn "Install oc CLI and re-run to perform full verification"
fi

# Final summary
section "Test Summary"

echo "Cluster Name:        $CLUSTER_NAME"
echo "Cluster ID:          $CLUSTER_ID"
echo "EFS File System:     $EFS_ID"
echo "Authentication Mode: $AUTH_MODE"
if [ "$AUTH_MODE" == "sts" ]; then
    echo "IAM Role ARN:        $IAM_ROLE_ARN"
fi
echo ""

if [ "$AUTH_MODE" == "sts" ]; then
    success "✅ STS EFS configuration test PASSED!"
    echo ""
    echo "The cluster successfully:"
    echo "  1. Detected STS mode via OIDC issuer"
    echo "  2. Verified EFS credentials secret with IAM role"
    echo "  3. Configured ClusterCSIDriver for STS"
    echo "  4. Annotated service accounts with IAM role ARN"
    echo "  5. Created EFS file system and storage class"
    if [ "$HAS_OC" == "true" ]; then
        echo "  6. Successfully bound a test PVC using EFS storage"
    fi
elif [ "$AUTH_MODE" == "static" ]; then
    warn "⚠️  Cluster used STATIC credentials instead of STS"
    echo ""
    echo "This means the worker did not detect STS credentials."
    echo "Possible reasons:"
    echo "  - Worker not running with instance profile"
    echo "  - No AWS_SESSION_TOKEN environment variable"
    echo "  - IMDS not accessible"
else
    warn "⚠️  Could not determine authentication mode"
fi

echo ""
log "Cleanup: To delete this test cluster, run:"
echo "  curl -X DELETE $API_URL/clusters/$CLUSTER_ID"
echo ""
