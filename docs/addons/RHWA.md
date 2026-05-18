# Red Hat Workload Availability (RHWA / Medik8s) Addon

## Overview

The RHWA addon installs and configures the Medik8s suite of operators for automated node health monitoring and remediation in OpenShift clusters. This enables self-healing clusters that can automatically detect and recover from node failures without manual intervention.

**Medik8s Project**: https://www.medik8s.io/

## What is RHWA/Medik8s?

Medik8s (Medical Kubernetes) is a set of operators designed to provide high availability and workload protection for Kubernetes clusters through automated node remediation. When nodes become unhealthy, Medik8s can automatically:

1. **Detect** unhealthy nodes through health checks
2. **Fence** failed nodes to prevent split-brain scenarios
3. **Remediate** nodes by rebooting, deleting resources, or replacing machines
4. **Restore** cluster capacity by bringing nodes back online or creating replacements

## Included Operators

The RHWA addon installs **5 core operators**:

### 1. Node Health Check (NHC)
- **Purpose**: Monitors node health and triggers remediation
- **Namespace**: `openshift-operators`
- **CRD**: `NodeHealthCheck`
- **What it does**: Watches for unhealthy node conditions and invokes remediation handlers when thresholds are exceeded

### 2. Self Node Remediation (SNR)
- **Purpose**: Remediates nodes by deleting workloads and rebooting
- **Namespace**: `openshift-operators`
- **CRD**: `SelfNodeRemediationTemplate`
- **Strategies**:
  - `ResourceDeletion`: Deletes pods and stateful workloads, triggers node reboot
  - `OutOfServiceTaint`: Marks node out-of-service for immediate pod eviction (OCP 4.12+)

### 3. Fence Agents Remediation (FAR)
- **Purpose**: Hardware-level node fencing via IPMI/iDRAC/iLO
- **Namespace**: `openshift-operators`
- **CRD**: `FenceAgentsRemediationTemplate`
- **Use case**: Bare metal and on-premises clusters with BMC access

### 4. Machine Deletion Remediation (MDR)
- **Purpose**: Remediates by deleting and recreating machine resources
- **Namespace**: `openshift-operators`
- **CRD**: `MachineDeletionRemediationTemplate`
- **Use case**: Cloud-based clusters (AWS, Azure, GCP) where machines can be replaced

### 5. Node Maintenance Operator (NMO)
- **Purpose**: Graceful node cordoning and draining
- **Namespace**: `openshift-operators`
- **CRD**: `NodeMaintenance`
- **Use case**: Coordinating maintenance windows with workload migration

## Installation

### Automatic Installation (aws-rhwa-lab profile)

The easiest way to use RHWA is with a cluster that has the addon pre-configured:

```bash
# Using ocpctl CLI
ocpctl create cluster tsanders-rhwa-test --profile aws-rhwa-lab

# Using API
curl -X POST https://ocpctl.mg.dog8code.com/api/v1/clusters \
  -H "Content-Type: application/json" \
  -d '{
    "name": "tsanders-rhwa-test",
    "profile": "aws-rhwa-lab"
  }'
```

The `aws-rhwa-lab` profile is optimized for RHWA testing:
- 3-node compact cluster (control plane is schedulable)
- Manual credentials mode (required for failure injection)
- m6i.xlarge instances (4 vCPU, 16GB RAM)
- RHWA addon automatically applied

### Manual Installation (Custom Post-Config)

For other cluster profiles, add RHWA via custom post-deployment configuration:

```json
{
  "operators": [
    {
      "name": "node-healthcheck-operator",
      "namespace": "openshift-operators",
      "source": "redhat-operators",
      "channel": "stable"
    },
    {
      "name": "self-node-remediation",
      "namespace": "openshift-operators",
      "source": "redhat-operators",
      "channel": "stable",
      "dependsOn": ["node-healthcheck-operator"]
    },
    {
      "name": "fence-agents-remediation",
      "namespace": "openshift-operators",
      "source": "redhat-operators",
      "channel": "stable",
      "dependsOn": ["node-healthcheck-operator"]
    },
    {
      "name": "machine-deletion-remediation",
      "namespace": "openshift-operators",
      "source": "redhat-operators",
      "channel": "stable",
      "dependsOn": ["node-healthcheck-operator"]
    },
    {
      "name": "node-maintenance-operator",
      "namespace": "openshift-operators",
      "source": "redhat-operators",
      "channel": "stable"
    }
  ],
  "manifests": [
    {
      "name": "workload-availability-namespace",
      "description": "Create openshift-workload-availability namespace",
      "content": "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: openshift-workload-availability\n  labels:\n    openshift.io/cluster-monitoring: \"true\"\n"
    },
    {
      "name": "self-node-remediation-template",
      "description": "SelfNodeRemediationTemplate for automatic node recovery",
      "namespace": "openshift-workload-availability",
      "content": "apiVersion: self-node-remediation.medik8s.io/v1alpha1\nkind: SelfNodeRemediationTemplate\nmetadata:\n  name: self-node-remediation-compact-template\n  namespace: openshift-workload-availability\nspec:\n  template:\n    spec:\n      remediationStrategy: ResourceDeletion\n",
      "dependsOn": ["workload-availability-namespace", "self-node-remediation"]
    },
    {
      "name": "machine-deletion-template",
      "description": "MachineDeletionRemediationTemplate for machine replacement",
      "namespace": "openshift-workload-availability",
      "content": "apiVersion: machine-deletion-remediation.medik8s.io/v1alpha1\nkind: MachineDeletionRemediationTemplate\nmetadata:\n  name: machine-deletion-template\n  namespace: openshift-workload-availability\nspec:\n  template:\n    spec:\n      order: 0\n",
      "dependsOn": ["workload-availability-namespace", "machine-deletion-remediation"]
    },
    {
      "name": "nhc-compact-baseline",
      "description": "Baseline NodeHealthCheck for compact 3-node clusters",
      "namespace": "openshift-workload-availability",
      "content": "apiVersion: remediation.medik8s.io/v1alpha1\nkind: NodeHealthCheck\nmetadata:\n  name: compact-cluster-baseline\n  namespace: openshift-workload-availability\nspec:\n  minHealthy: 51%\n  remediationTemplate:\n    apiVersion: self-node-remediation.medik8s.io/v1alpha1\n    kind: SelfNodeRemediationTemplate\n    namespace: openshift-workload-availability\n    name: self-node-remediation-compact-template\n  selector:\n    matchLabels:\n      node-role.kubernetes.io/master: \"\"\n  unhealthyConditions:\n    - type: Ready\n      status: \"False\"\n      duration: 300s\n    - type: Ready\n      status: Unknown\n      duration: 300s\n",
      "dependsOn": ["workload-availability-namespace", "node-healthcheck-operator", "self-node-remediation-template"]
    }
  ]
}
```

## Deployed Resources

After installation, the following resources are created:

### Operators (openshift-operators namespace)
```bash
oc get deployments -n openshift-operators | grep -E 'node-healthcheck|self-node|fence|machine-deletion|node-maintenance'
```

Expected output:
```
fence-agents-remediation-controller-manager        2/2     2            2
machine-deletion-remediation-controller-manager    1/1     1            1
node-healthcheck-controller-manager                2/2     2            2
node-healthcheck-node-remediation-console-plugin   1/1     1            1
node-maintenance-operator-controller-manager       1/1     1            1
self-node-remediation-controller-manager           2/2     2            2
```

### Custom Resources (openshift-workload-availability namespace)
```bash
oc get selfnoderemediationtemplates,machinedeletionremediationtemplates,nodehealthchecks -n openshift-workload-availability
```

Expected output:
```
NAME                                                                                                  AGE
selfnoderemediationtemplate.self-node-remediation.medik8s.io/self-node-remediation-compact-template   1h

NAME                                                                                                   AGE
machinedeletionremediationtemplate.machine-deletion-remediation.medik8s.io/machine-deletion-template   1h

NAME                                                              AGE
nodehealthcheck.remediation.medik8s.io/compact-cluster-baseline   1h
```

## Configuration Details

### NodeHealthCheck Configuration

The baseline `compact-cluster-baseline` NodeHealthCheck is configured for 3-node compact clusters:

```yaml
spec:
  minHealthy: 51%  # At least 2 out of 3 nodes must be healthy
  selector:
    matchLabels:
      node-role.kubernetes.io/master: ""  # Monitor control plane nodes
  unhealthyConditions:
    - type: Ready
      status: "False"
      duration: 300s  # Node must be NotReady for 5 minutes
    - type: Ready
      status: Unknown
      duration: 300s  # Node status Unknown for 5 minutes
  remediationTemplate:
    apiVersion: self-node-remediation.medik8s.io/v1alpha1
    kind: SelfNodeRemediationTemplate
    name: self-node-remediation-compact-template
```

**Key Parameters:**
- **minHealthy: 51%** - Remediation will NOT trigger if < 51% nodes are healthy (prevents cascading failures)
- **duration: 300s** - 5-minute grace period before remediation starts
- **selector** - Targets control plane nodes (master nodes)
- **remediationTemplate** - Uses Self Node Remediation with ResourceDeletion strategy

### Self Node Remediation Strategy

The `ResourceDeletion` strategy:
1. Taints the unhealthy node
2. Deletes all pods on the node
3. Deletes StatefulSet PVCs if needed
4. Reboots the node (via self-termination)
5. Node rejoins cluster after reboot

## Walkthrough: Testing RHWA Node Remediation

This walkthrough demonstrates RHWA's automatic node recovery capabilities by simulating a node failure.

### Prerequisites

- Access to a cluster with RHWA addon installed
- `oc` CLI authenticated to the cluster
- `watch` command available (or use `-w` flag with oc)

### Step 1: Verify RHWA Installation

```bash
# Check all operators are running
oc get deployments -n openshift-operators | grep -E 'node-healthcheck|self-node|fence|machine-deletion|node-maintenance'

# Verify NodeHealthCheck is configured
oc get nodehealthcheck -n openshift-workload-availability
oc describe nodehealthcheck compact-cluster-baseline -n openshift-workload-availability
```

Expected output should show:
- All operator deployments with ready replicas (e.g., `2/2`, `1/1`)
- NodeHealthCheck named `compact-cluster-baseline`
- Status showing `Enabled: true`

### Step 2: Monitor Initial Cluster State

Open **three terminal windows** for real-time monitoring:

**Terminal 1: Watch Nodes**
```bash
watch -n 2 'oc get nodes'
```

**Terminal 2: Watch NodeHealthCheck Status**
```bash
watch -n 2 'oc get nodehealthcheck compact-cluster-baseline -n openshift-workload-availability -o yaml | grep -A 20 "^status:"'
```

**Terminal 3: Watch Self Node Remediation Events**
```bash
watch -n 2 'oc get selfnoderemediation -A'
```

### Step 3: Record Baseline State

```bash
# Save current node list
oc get nodes -o wide > baseline-nodes.txt

# Check current pod distribution
oc get pods -A -o wide | grep -v Completed > baseline-pods.txt

# Identify a target node to fail (choose one control plane node)
TARGET_NODE=$(oc get nodes -l node-role.kubernetes.io/master --no-headers | head -1 | awk '{print $1}')
echo "Target node for failure injection: $TARGET_NODE"
```

### Step 4: Simulate Node Failure

There are several ways to simulate node failure. Choose one:

#### Option A: Simulate Unresponsive kubelet (Recommended for testing)

```bash
# SSH to the target node and stop kubelet
oc debug node/$TARGET_NODE -- chroot /host systemctl stop kubelet

# Verify node starts reporting NotReady after ~40 seconds
oc get nodes -w
```

#### Option B: Simulate Network Partition

```bash
# SSH to target node and block API server communication
oc debug node/$TARGET_NODE -- chroot /host iptables -A OUTPUT -d <API_SERVER_IP> -j DROP

# Find API server IP
oc get endpoints kubernetes -n default
```

#### Option C: Simulate Complete Node Failure (AWS)

```bash
# Get the AWS instance ID for the target node
INSTANCE_ID=$(oc get node $TARGET_NODE -o jsonpath='{.spec.providerID}' | cut -d'/' -f5)

# Stop the instance
aws ec2 stop-instances --instance-ids $INSTANCE_ID
```

### Step 5: Observe RHWA Detection Phase (0-5 minutes)

Watch for the following sequence:

**Minutes 0-1: Node Status Change**
```bash
# Node transitions to NotReady
oc get nodes
# NAME                                        STATUS     ROLES                  AGE   VERSION
# ip-10-0-1-100.us-east-1.compute.internal   NotReady   control-plane,master   2h    v1.29.0
# ip-10-0-1-101.us-east-1.compute.internal   Ready      control-plane,master   2h    v1.29.0
# ip-10-0-1-102.us-east-1.compute.internal   Ready      control-plane,master   2h    v1.29.0
```

**Minutes 1-5: NHC Monitoring**
```bash
# NodeHealthCheck starts tracking unhealthy duration
oc get nodehealthcheck compact-cluster-baseline -n openshift-workload-availability -o jsonpath='{.status.unhealthyNodes}' | jq
```

**Minute 5: Remediation Triggered**
```bash
# NHC creates a SelfNodeRemediation resource
oc get selfnoderemediation -A
# NAMESPACE                        NAME                                           PHASE
# openshift-workload-availability   self-node-remediation-ip-10-0-1-100          Started
```

### Step 6: Observe RHWA Remediation Phase (5-10 minutes)

**Phase 1: Node Fencing**
```bash
# SNR taints the node to prevent new workloads
oc get node $TARGET_NODE -o jsonpath='{.spec.taints}' | jq
```

Expected taint:
```json
[
  {
    "effect": "NoExecute",
    "key": "medik8s.io/remediation",
    "timeAdded": "2026-05-17T20:00:00Z"
  }
]
```

**Phase 2: Resource Deletion**
```bash
# SNR deletes pods on the unhealthy node
oc get pods -A -o wide | grep $TARGET_NODE

# Watch pod evictions
oc get events -A --sort-by='.lastTimestamp' | grep -i evict
```

**Phase 3: Node Reboot**
```bash
# SNR triggers node reboot (if using ResourceDeletion strategy)
# Check Self Node Remediation status
oc get selfnoderemediation -A -o yaml
```

### Step 7: Observe Recovery Phase (10-15 minutes)

**Node Restart**
```bash
# For AWS: Node instance restarts
aws ec2 describe-instances --instance-ids $INSTANCE_ID --query 'Reservations[0].Instances[0].State.Name'

# Node rejoins cluster
oc get nodes -w
```

**Recovery Completion**
```bash
# Node returns to Ready state
oc get nodes
# NAME                                        STATUS   ROLES                  AGE   VERSION
# ip-10-0-1-100.us-east-1.compute.internal   Ready    control-plane,master   2h    v1.29.0
# ip-10-0-1-101.us-east-1.compute.internal   Ready    control-plane,master   2h    v1.29.0
# ip-10-0-1-102.us-east-1.compute.internal   Ready    control-plane,master   2h    v1.29.0

# SelfNodeRemediation resource is cleaned up
oc get selfnoderemediation -A
# No resources found
```

### Step 8: Validate Cluster Health

```bash
# Verify all nodes are Ready
oc get nodes

# Check cluster operators
oc get clusteroperators

# Verify workloads are running
oc get pods -A | grep -v Running | grep -v Completed

# Compare pod distribution
oc get pods -A -o wide | grep -v Completed > after-remediation-pods.txt
diff baseline-pods.txt after-remediation-pods.txt
```

### Expected Timeline

| Time | Event | Observable Behavior |
|------|-------|---------------------|
| T+0m | Node failure injected | Node status changes to NotReady |
| T+1m | NHC detects unhealthy condition | NodeHealthCheck status shows unhealthy node |
| T+5m | Remediation triggered | SelfNodeRemediation resource created |
| T+6m | Resource deletion | Pods evicted from unhealthy node |
| T+7m | Node reboot initiated | Node goes offline |
| T+10m | Node restarts | Node rejoins cluster |
| T+12m | Node ready | Node status returns to Ready |
| T+15m | Cleanup | SelfNodeRemediation resource deleted |

### Success Criteria

✅ Node detected as unhealthy within 1 minute
✅ Remediation triggered after 5-minute threshold
✅ Workloads successfully evicted from failed node
✅ Node rebooted and rejoined cluster
✅ Node returned to Ready state
✅ No manual intervention required
✅ Cluster operators remain Available

## Troubleshooting

### Issue: NodeHealthCheck Not Detecting Unhealthy Nodes

**Symptoms:**
- Node is NotReady but NHC doesn't show it as unhealthy
- No SelfNodeRemediation resources created

**Diagnosis:**
```bash
# Check NodeHealthCheck status
oc get nodehealthcheck -n openshift-workload-availability -o yaml

# Verify selector matches target nodes
oc get nodes --show-labels | grep master

# Check NHC controller logs
oc logs -n openshift-operators deployment/node-healthcheck-controller-manager -c manager
```

**Common Causes:**
1. **Selector mismatch**: NodeHealthCheck selector doesn't match node labels
2. **Duration not met**: Node hasn't been unhealthy for full 300 seconds
3. **minHealthy violation**: Too many nodes unhealthy (< 51%)

**Resolution:**
```bash
# Adjust selector if needed
oc edit nodehealthcheck compact-cluster-baseline -n openshift-workload-availability

# Reduce duration for testing (not recommended for production)
oc patch nodehealthcheck compact-cluster-baseline -n openshift-workload-availability --type=merge -p '{"spec":{"unhealthyConditions":[{"type":"Ready","status":"False","duration":"60s"}]}}'
```

### Issue: Remediation Stuck in "Started" Phase

**Symptoms:**
- SelfNodeRemediation resource exists but node doesn't reboot
- Remediation phase shows "Started" for > 10 minutes

**Diagnosis:**
```bash
# Check SNR resource status
oc get selfnoderemediation -A -o yaml

# Check SNR controller logs
oc logs -n openshift-operators deployment/self-node-remediation-controller-manager -c manager

# Check node conditions
oc describe node $TARGET_NODE
```

**Common Causes:**
1. **API server unreachable**: Node can't communicate to delete resources
2. **Daemonset pods blocking**: Critical daemonsets preventing node drain
3. **PVC deletion issues**: StatefulSet PVCs can't be deleted

**Resolution:**
```bash
# Manually force node drain
oc adm drain $TARGET_NODE --ignore-daemonsets --delete-emptydir-data --force

# Delete stuck SNR resource (triggers cleanup)
oc delete selfnoderemediation -n openshift-workload-availability <snr-name>
```

### Issue: Node Never Rejoins After Reboot

**Symptoms:**
- Node rebooted but remains NotReady
- kubelet not starting

**Diagnosis:**
```bash
# SSH to node and check kubelet
oc debug node/$TARGET_NODE
chroot /host
systemctl status kubelet
journalctl -u kubelet -n 100
```

**Common Causes:**
1. **Certificate issues**: Node certificates expired
2. **Network issues**: Can't reach API server
3. **kubelet crash loop**: Configuration problem

**Resolution:**
```bash
# Restart kubelet
systemctl restart kubelet

# Approve pending CSRs
oc get csr
oc adm certificate approve <csr-name>

# Check node logs
journalctl -u kubelet -f
```

### Issue: minHealthy Prevents Remediation

**Symptoms:**
- Multiple nodes unhealthy
- No remediation triggered
- NodeHealthCheck status shows "InsufficientHealthy"

**Diagnosis:**
```bash
# Check how many nodes are healthy
oc get nodes | grep -c Ready

# Check NodeHealthCheck status
oc get nodehealthcheck compact-cluster-baseline -n openshift-workload-availability -o jsonpath='{.status}'
```

**Explanation:**
The `minHealthy: 51%` setting prevents remediation when too many nodes are unhealthy. This is a **safety feature** to prevent cascading failures.

For a 3-node cluster:
- ✅ 1 unhealthy node (66% healthy) → Remediation proceeds
- ❌ 2 unhealthy nodes (33% healthy) → Remediation blocked

**Resolution:**
This is working as designed. Fix the underlying issue causing multiple node failures:
```bash
# Check cluster-level issues
oc get clusteroperators
oc get nodes -o wide

# Check for network or storage problems
oc get events -A --sort-by='.lastTimestamp'
```

## Monitoring and Metrics

### Key Metrics to Monitor

```bash
# Node health status
oc get nodes

# NodeHealthCheck status
oc get nodehealthcheck -A -o wide

# Active remediation events
oc get selfnoderemediation -A

# Remediation history (check events)
oc get events -n openshift-workload-availability --sort-by='.lastTimestamp'
```

### Prometheus Metrics

RHWA operators expose Prometheus metrics:

```bash
# Query NHC metrics
oc exec -n openshift-monitoring prometheus-k8s-0 -- \
  promtool query instant http://localhost:9090 \
  'node_healthcheck_unhealthy_nodes'

# Query SNR metrics
oc exec -n openshift-monitoring prometheus-k8s-0 -- \
  promtool query instant http://localhost:9090 \
  'self_node_remediation_remediations_total'
```

## Best Practices

1. **Testing in Non-Production First**
   - Always test RHWA behavior in lab environments
   - Understand remediation timing for your cluster size
   - Verify workload tolerance for node failures

2. **minHealthy Configuration**
   - 3-node cluster: Use 51% (allows 1 node failure)
   - 5-node cluster: Use 60% (allows 2 node failures)
   - Large clusters: Adjust based on failure domain tolerance

3. **Duration Tuning**
   - Default 300s (5 minutes) is conservative
   - Reduce to 60s for faster recovery (testing only)
   - Increase to 600s for flaky network environments

4. **Monitoring**
   - Set up alerts for NodeHealthCheck unhealthy nodes
   - Track remediation frequency (shouldn't be frequent)
   - Monitor node reboot success rates

5. **Manual Credentials Mode (AWS)**
   - Required for full machine deletion/replacement
   - Enables RHWA to call AWS APIs for instance termination
   - Use `aws-rhwa-lab` profile which enables this by default

## Additional Resources

- **Medik8s Documentation**: https://www.medik8s.io/
- **Node Health Check Operator**: https://github.com/medik8s/node-healthcheck-operator
- **Self Node Remediation**: https://github.com/medik8s/self-node-remediation
- **OpenShift Docs**: https://docs.openshift.com/container-platform/latest/nodes/nodes/eco-node-health-check-operator.html
- **RHWA Addon Source**: `/Users/tsanders/Workspace2/ocpctl/internal/addon/definitions/rhwa.yaml`

## Support

For issues or questions:
- Check troubleshooting section above
- Review operator logs in `openshift-operators` namespace
- Check ocpctl GitHub issues: https://github.com/tsanders-rh/ocpctl/issues
- Tag issues with `addon:rhwa` label
