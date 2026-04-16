# A/B Test Plan: Raw openshift-install vs ocpctl

## Goal

Determine what difference between raw `openshift-install` and `ocpctl` causes NAT gateway route reconciliation failures in 4.22.0-ec.4.

## Failure Signature

```
failed to discover routes on route table ... no nat gateways available ... for private subnet
↓
instance is not running while trying to register bootstrap/control-plane with LB
```

## Known Differences

Based on code analysis, ocpctl differs from raw openshift-install in these ways:

1. **Environment Variable**: Sets `OPENSHIFT_INSTALL_INVOKER=ocpctl`
2. **Tags**: Adds extensive `userTags` in install-config.yaml:
   - Owner
   - Team
   - CostCenter
   - ManagedBy
   - Platform
   - Profile
   - Purpose
   - Environment
   - ClusterName

3. **Workflow**: Always uses Manual credentials mode (no difference from raw install with Manual mode)
4. **NO manifest modification**: Ocpctl does NOT edit manifests after generation

## A/B Test Setup

### Controlled Variables (IDENTICAL)
- Version: 4.22.0-ec.4
- Region: us-west-2 (NOT us-east-1 to avoid quota/regional issues)
- Instance types: m6i.2xlarge (control plane), m6i.2xlarge (worker)
- Topology: SNO (1 control plane, 0 workers)
- Credentials mode: Manual
- Pull secret: same
- SSH key: same
- Base domain: mg.dog8code.com
- Network type: OVNKubernetes
- Network CIDRs: identical

### Variable Being Tested

**Path A**: Raw openshift-install (NO OPENSHIFT_INSTALL_INVOKER, NO userTags)
**Path B**: Simulated ocpctl (WITH OPENSHIFT_INSTALL_INVOKER=ocpctl, WITH userTags)

## Running the Test

```bash
cd /Users/tsanders/Workspace2/ocpctl

# Set environment
export OPENSHIFT_PULL_SECRET="$(cat /path/to/pull-secret.txt)"
export REGION="us-west-2"  # Using us-west-2 to avoid us-east-1 issues

# Run A/B test
./scripts/test-ab-comparison.sh
```

The script will:
1. Create two install directories
2. Generate install-config.yaml for both paths
3. Run `create manifests` for both
4. Run `ccoctl` for both
5. Save all artifacts before running `create cluster`
6. Optionally run `create cluster` for both
7. Generate diff files for comparison

## What to Compare

### Priority 1: Cluster API Manifests

```bash
cat /tmp/ab-comparison-*/diff-cluster-api.txt | grep -A10 -B10 'AWSCluster\|subnet\|nat\|route\|failureDomain'
```

Look for:
- Different subnet specifications
- Different AZ/failure domain layouts
- Different route table references
- NAT gateway expectations

### Priority 2: install-config.yaml

```bash
cat /tmp/ab-comparison-*/diff-install-config.txt
```

Expect to see:
- userTags difference (expected)
- Cluster name difference (expected)
- **Nothing else should differ**

### Priority 3: All Manifests

```bash
cat /tmp/ab-comparison-*/diff-manifests.txt | less
```

Look for unexpected differences in:
- Machine configs
- Network configs
- Infrastructure objects

## AWS State Inspection

If both clusters are created, compare AWS resources:

```bash
INFRA_A="<from script output>"
INFRA_B="<from script output>"
REGION="us-west-2"

# NAT Gateways
aws ec2 describe-nat-gateways --region $REGION \
  --filters "Name=tag:kubernetes.io/cluster/$INFRA_A,Values=owned" \
  --output json > /tmp/nat-raw.json

aws ec2 describe-nat-gateways --region $REGION \
  --filters "Name=tag:kubernetes.io/cluster/$INFRA_B,Values=owned" \
  --output json > /tmp/nat-ocpctl.json

diff <(jq '.NatGateways[] | {State, SubnetId, CreateTime, NatGatewayAddresses}' /tmp/nat-raw.json) \
     <(jq '.NatGateways[] | {State, SubnetId, CreateTime, NatGatewayAddresses}' /tmp/nat-ocpctl.json)

# Route Tables
aws ec2 describe-route-tables --region $REGION \
  --filters "Name=tag:kubernetes.io/cluster/$INFRA_A,Values=owned" \
  --query 'RouteTables[].Routes' > /tmp/routes-raw.json

aws ec2 describe-route-tables --region $REGION \
  --filters "Name=tag:kubernetes.io/cluster/$INFRA_B,Values=owned" \
  --query 'RouteTables[].Routes' > /tmp/routes-ocpctl.json

diff /tmp/routes-raw.json /tmp/routes-ocpctl.json

# Subnets
aws ec2 describe-subnets --region $REGION \
  --filters "Name=tag:kubernetes.io/cluster/$INFRA_A,Values=owned" > /tmp/subnets-raw.json

aws ec2 describe-subnets --region $REGION \
  --filters "Name=tag:kubernetes.io/cluster/$INFRA_B,Values=owned" > /tmp/subnets-ocpctl.json

diff <(jq '.Subnets[] | {SubnetId, AvailabilityZone, CidrBlock, MapPublicIpOnLaunch, Tags}' /tmp/subnets-raw.json) \
     <(jq '.Subnets[] | {SubnetId, AvailabilityZone, CidrBlock, MapPublicIpOnLaunch, Tags}' /tmp/subnets-ocpctl.json)
```

## Evidence That Would Confirm ocpctl is the Culprit

Any of these would be smoking guns:

1. **Raw install succeeds, ocpctl fails** (with identical inputs)
2. **Different AWSCluster manifest** (subnet specs, AZ layout, route table refs)
3. **Different number of NAT gateways created** (5 vs 1 for SNO)
4. **Different subnet-to-route-table mappings**
5. **NAT reaches available in both, but only ocpctl proceeds too early**
6. **Tags affect CAPA resource discovery logic**

## Evidence That Would Weaken ocpctl Theory

If these are true, the problem is elsewhere:

1. **Both paths produce identical manifests** (except cluster name/tags)
2. **Both paths fail identically** in us-west-2
3. **Both paths create identical AWS resources**
4. **NAT gateway timing is identical** in both paths

## Expected Timeline

1. **Manifests generated**: 30 seconds
2. **ccoctl completes**: 30 seconds
3. **NAT gateways created**: 1-2 minutes after cluster creation starts
4. **Route reconciliation**: 2-3 minutes after cluster creation starts
5. **Bootstrap complete**: 10-15 minutes
6. **Control plane ready**: 20-30 minutes

Watch the logs at the **2-3 minute mark** - that's when NAT/route errors appear.

## Quick Manifest-Only Test

If you just want to compare manifests without waiting for cluster creation:

```bash
./scripts/test-ab-comparison.sh
# When prompted "Continue with cluster creation?" press 'n'
```

Then examine:
```bash
COMP_DIR="/tmp/ab-comparison-<timestamp>"
cat $COMP_DIR/diff-install-config.txt
cat $COMP_DIR/diff-cluster-api.txt
cat $COMP_DIR/diff-manifests.txt | head -100
```

## Next Steps Based on Results

### If manifests differ significantly:
→ File bug report: "ocpctl generates different Cluster API manifests causing NAT failures"

### If manifests are identical but behavior differs:
→ Investigate OPENSHIFT_INSTALL_INVOKER environment variable impact on installer behavior
→ Check if tags affect CAPA AWS resource discovery

### If both fail identically:
→ This is an upstream 4.22.0-ec.4 bug, not ocpctl-specific
→ File upstream OpenShift installer bug report

### If both succeed:
→ Problem is environmental (region-specific, IAM permissions, AWS quota/capacity)
