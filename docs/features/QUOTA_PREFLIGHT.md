# AWS Quota & Capacity Pre-flight

**Issue:** [#100](https://github.com/tsanders-rh/ocpctl/issues/100)

## Problem

Before this check, the AWS pre-flight (`internal/worker/preflight_aws.go`,
`CheckInstanceTypeAvailability`) validated only that the requested instance
types were offered across enough availability zones. It did **not** look at
account service quotas, so a create could pass pre-flight and then fail deep in
CAPI provisioning when a shared-account quota was exhausted.

This is exactly what bit nadeem on 2026-08-19: `us-east-1` had hit its
Gateway-VPC-endpoint limit (`VpcEndpointLimitExceeded`) and the failure only
surfaced ~15 minutes into the create, after infra was already being built.

## What it checks

`CheckQuotas` (`internal/worker/quota_preflight.go`) compares, for the target
region, **applied quota vs. current usage vs. this cluster's need** for the
four quotas every OpenShift IPI cluster consumes:

| Quota | Code | Need per cluster |
|-------|------|------------------|
| Gateway VPC endpoints per Region | `L-1B52E74A` | 1 (S3 gateway endpoint) |
| VPCs per Region | `L-F678F1CE` | 1 |
| Elastic IPs per Region | `L-0263D0A3` | 1 per AZ (NAT gateway) |
| Running On-Demand Standard vCPUs | `L-1216C47A` | control-plane + workers + 1 bootstrap |

Applied quota comes from `servicequotas:GetServiceQuota`; usage is counted with
EC2 `Describe*` calls. The vCPU need is derived from the profile's control-plane
and worker instance types (looked up via `DescribeInstanceTypes`).

## Design principles

- **Only blocks on a confirmed shortfall.** If `need > free` for any quota, the
  create fails fast with a `PreflightCheckError` (no retry) listing every
  short quota and remediation steps.
- **Never a new source of outages.** Any lookup that errors — missing IAM
  permission, throttling, an unrecognized quota, an empty value — is logged and
  **skipped**, not treated as a failure. A degraded quota check falls back to
  the old behavior (create proceeds).
- **vCPU check is best-effort.** It only applies to Standard-family instance
  types (A, C, D, H, I, M, R, T, Z), which is what the `L-1216C47A` quota
  counts. Non-Standard families (e.g. GPU `g5`/`p4d`) skip the vCPU check with a
  warning rather than compare against the wrong quota.

## IAM permissions

All required permissions are already granted to the worker role — no policy
change was needed for this feature:

- `servicequotas:GetServiceQuota`
- `ec2:DescribeVpcEndpoints`, `ec2:DescribeVpcs`, `ec2:DescribeAddresses`,
  `ec2:DescribeInstances`, `ec2:DescribeInstanceTypes`

See `deploy/iam-policy-worker-full.json` (Sid `ServiceQuotasRead`) and
`terraform/dev/openshift-provisioning-policy.json`.

## Tests

`internal/worker/quota_preflight_test.go` uses mocked `ec2QuotaAPI` /
`quotasAPI` implementations to cover: sufficient headroom passes, gateway-
endpoint exhaustion blocks, Standard vCPU shortfall blocks, quota-lookup failure
degrades gracefully, usage-lookup failure degrades gracefully, and the vCPU
check is skipped for non-Standard families.
