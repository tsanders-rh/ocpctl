# OpenShift Ephemeral Cluster Control Plane
## Complete Design Specification

Version: 1.0
Target date: 2026-02-27
Audience: Platform engineering, SRE, security, FinOps

## 1. Purpose

Build an internal service that lets authorized users request and terminate OpenShift 4.20 clusters on AWS and IBM Cloud through a standardized workflow, with reliable full cleanup, cost tracking, and policy controls.

Key outcome:

- Eliminate leaked infrastructure caused by ad hoc installs and lossy cleanup scripts.
- Preserve installer/Terraform state artifacts needed for deterministic teardown.
- Provide auditability, ownership, lifecycle controls, and self-service UX.

## 2. Scope

In scope:

- Self-service web form for `create`, `destroy`, and lifecycle operations.
- Backend API and async job execution for `openshift-install` workflows.
- Central state and artifact storage per cluster.
- Standardized tags and usage/cost attribution.
- TTL-based auto-destroy and janitor processes.
- Optional off-hours worker scale-down/scale-up (not control-plane hibernation).

Out of scope (v1):

- Full multi-tenant isolation by separate pull secrets.
- Full cluster hibernation of installer-provisioned OCP control plane.
- Arbitrary user-defined networking/topology.
- Supporting all OpenShift versions. (Start with pinned 4.20.z allowlist.)

## 3. Non-goals

- Replacing OpenShift installer internals.
- Letting users run direct `openshift-install` in unmanaged contexts.
- Building a generic cloud resource orchestrator.

## 4. High-level architecture

Components:

1. Frontend Web UI
2. API Service
3. Job Queue
4. Worker Service
5. State Store (PostgreSQL)
6. Artifact Store (S3 + KMS)
7. Secrets Manager (pull secret, cloud credentials if needed)
8. Scheduler/Janitor (TTL destroy, retries, orphan scans)
9. Metrics/Logging/Audit pipeline

Control flow:

1. User submits cluster request in UI.
2. API validates policy and records `cluster_request` + `cluster` rows.
3. API enqueues `CREATE_CLUSTER` job.
4. Worker dequeues, materializes install directory, runs `openshift-install create cluster`.
5. Worker streams logs, stores artifacts to S3, updates DB status.
6. On destroy request or TTL expiry, worker restores exact state dir from S3 and runs `openshift-install destroy cluster`.
7. Worker marks cluster destroyed and records usage/cost metadata.

## 5. Core design principles

- Single path to provision and destroy.
- State durability first: keep everything needed for teardown.
- Idempotent API operations with request keys.
- Policy-driven input constraints.
- Explicit ownership and tagging.
- Recoverability over convenience.

## 6. Functional requirements

FR-1 Create cluster from approved profiles using one action.
FR-2 Destroy cluster reliably and completely.
FR-3 Persist installer state and logs for each lifecycle step.
FR-4 Display credentials and endpoints securely after successful create.
FR-5 Track owner/team/cost center and cluster lifetime.
FR-6 Auto-destroy clusters at TTL expiration.
FR-7 Support AWS and IBM Cloud variants.
FR-8 Support minimal test compact profile.
FR-9 Support off-hours worker scaling for opted-in clusters.
FR-10 Provide admin reporting for active clusters, failures, and cost by tag.

## 7. Non-functional requirements

NFR-1 Availability: API 99.9%, workers horizontally scalable.
NFR-2 Reliability: destroy success >= 99% without manual intervention.
NFR-3 Security: AWS IAM-based authn/authz, encrypted secrets, audit logs.
NFR-4 Performance: API enqueue < 2s; job start < 60s under normal load.
NFR-5 Compliance: immutable audit event trail for create/destroy actions.
NFR-6 Operability: all jobs observable with structured logs and metrics.

## 8. Supported user roles

1. Requester
- Create/destroy owned team clusters.
- View cluster status and outputs for authorized clusters.

2. Platform Admin
- Manage profiles, policy, retries, break-glass destroy, orphan sweeps.

3. Auditor/FinOps
- Read-only access to inventory, usage, and cost reports.

## 9. Standardized cluster profiles

Start with a strict set:

- `aws-minimal-test`
- `aws-standard`
- `ibm-minimal-test`
- `ibm-standard`

Profile defines:

- Platform
- Region allowlist
- Instance types
- Control-plane/worker replicas
- `mastersSchedulable` default
- Required tags
- Max TTL

Example policy defaults:

- Minimal test: 3 control-plane, 0 workers, `mastersSchedulable=true`, max TTL 72h.
- Standard: 3 control-plane, 3 workers, max TTL 168h.

## 10. API specification

Base path: `/api/v1`

### 10.1 Authentication

- Web login uses AWS IAM Identity Center.
- Only IAM Identity Center users/groups assigned in this AWS account can access the web UI.
- API is private and callable only by approved IAM roles (web backend, worker, scheduler).
- API authorization maps IAM identity claims to platform RBAC (team/user role scopes).
- No external IdP or local user database in v1.

### 10.2 Endpoints

1. `POST /clusters`
- Purpose: request create.
- Idempotency: required header `Idempotency-Key`.
- Body:
```json
{
  "name": "team-a-smoke-01",
  "platform": "aws",
  "version": "4.20.3",
  "profile": "aws-minimal-test",
  "region": "us-east-1",
  "baseDomain": "labs.example.com",
  "owner": "team-a",
  "costCenter": "sandbox",
  "ttlHours": 24,
  "sshPublicKey": "ssh-ed25519 AAAA...",
  "extraTags": {
    "Environment": "test",
    "RequestSource": "webform"
  }
}
```
- Response `202 Accepted`:
```json
{
  "clusterId": "clu_01J...",
  "jobId": "job_01J...",
  "status": "PENDING"
}
```

2. `POST /clusters/{clusterId}/destroy`
- Purpose: request destroy.
- Response `202 Accepted` with `jobId`.

3. `POST /clusters/{clusterId}/scale-workers`
- Purpose: off-hours worker control.
- Body: `{ "replicas": 0 }` or `{ "replicas": 3 }`.

4. `GET /clusters`
- Filter by status/platform/owner/team.

5. `GET /clusters/{clusterId}`
- Full inventory record + latest job status.

6. `GET /clusters/{clusterId}/outputs`
- Returns sanitized outputs:
  - console URL
  - api URL
  - kubeconfig download reference
  - kubeadmin secret reference (not plaintext by default)

7. `GET /jobs/{jobId}`
- Job status and recent logs.

8. `GET /reports/usage`
- Aggregated runtime and spend by tag/team.

## 11. Data model (PostgreSQL)

### 11.1 Tables

1. `clusters`
- `id` (PK)
- `name` (unique active constraint)
- `platform` (`aws|ibmcloud`)
- `version`
- `profile`
- `region`
- `base_domain`
- `owner`
- `team`
- `cost_center`
- `status` (`PENDING|CREATING|READY|DESTROYING|DESTROYED|FAILED`)
- `requested_by`
- `ttl_hours`
- `destroy_at`
- `created_at`
- `updated_at`
- `destroyed_at`
- `request_tags` (jsonb)
- `effective_tags` (jsonb)

2. `cluster_outputs`
- `cluster_id` (FK)
- `api_url`
- `console_url`
- `kubeconfig_s3_uri`
- `kubeadmin_secret_ref`
- `metadata_s3_uri`

3. `cluster_artifacts`
- `cluster_id` (FK)
- `artifact_type` (`INSTALL_DIR_SNAPSHOT|LOG|AUTH_BUNDLE|METADATA`)
- `s3_uri`
- `checksum`
- `created_at`

4. `jobs`
- `id` (PK)
- `cluster_id` (FK)
- `job_type` (`CREATE|DESTROY|SCALE_WORKERS|JANITOR_DESTROY|ORPHAN_SWEEP`)
- `status` (`PENDING|RUNNING|SUCCEEDED|FAILED|RETRYING`)
- `attempt`
- `error_code`
- `error_message`
- `started_at`
- `ended_at`

5. `audit_events`
- `id` (PK)
- `actor`
- `action`
- `target_cluster_id`
- `metadata` (jsonb)
- `created_at`

6. `usage_samples`
- `cluster_id`
- `sample_time`
- `state`
- `worker_replicas`
- `estimated_hourly_cost`

### 11.2 Constraints

- Unique active cluster name per platform/base domain.
- Max TTL enforced by profile policy.
- Immutable `platform`, `profile`, and `base_domain` after create accepted.

## 12. Artifact and state retention strategy

S3 layout:

- `s3://cluster-ops-artifacts/clusters/<cluster-id>/create/<timestamp>/install-dir.tar.zst`
- `s3://cluster-ops-artifacts/clusters/<cluster-id>/destroy/<timestamp>/destroy.log`
- `s3://cluster-ops-artifacts/clusters/<cluster-id>/outputs/metadata.json`

Install directory packaging requirements:

- Include all files created by installer for lifecycle management.
- Preserve permissions/paths.
- Capture checksums and write manifest.

Retention:

- Active clusters: retain latest create snapshot + incremental logs.
- Destroyed clusters: retain state/logs for 30-90 days per policy.

## 13. Worker execution contract

Preconditions:

- Profile and policy validation passed by API.
- Required secrets available.
- Job lock acquired for cluster.

Create flow:

1. Create isolated working dir `/work/<job-id>/`.
2. Render `install-config.yaml` from profile + request values.
3. Inject pull secret from secrets manager.
4. Run `openshift-install create cluster --dir <dir> --log-level=info`.
5. Parse outputs and write `cluster_outputs`.
6. Snapshot and upload install dir + logs to S3.
7. Mark cluster `READY` and schedule TTL destroy.

Destroy flow:

1. Restore install-dir snapshot from S3.
2. Run `openshift-install destroy cluster --dir <dir> --log-level=debug`.
3. Upload destroy logs.
4. Verify no tagged resources remain.
5. Mark `DESTROYED` and close lifecycle.

Failure handling:

- Retry with exponential backoff for transient failures.
- On repeated failure, mark `FAILED` and raise alert.
- Provide operator runbook for break-glass/manual remediation.

## 14. Tagging standard

Required tags for all cloud resources:

- `ManagedBy=cluster-control-plane`
- `ClusterId=<cluster-id>`
- `ClusterName=<name>`
- `Owner=<owner>`
- `Team=<team>`
- `CostCenter=<cost_center>`
- `Environment=<env>`
- `TTLExpiry=<iso8601>`
- `RequestId=<job-id>`

Rules:

- User tags are additive and merged.
- Reserved tags cannot be overridden by users.

## 15. Off-hours strategy

Supported in v1:

- Scale workers to `0` on schedule for opted-in clusters.
- Restore workers to profile baseline at start-of-day.

Not supported in v1:

- Stopping control plane/etcd.
- Full cluster hibernate.

Scheduler jobs:

- `OFFHOURS_SCALE_DOWN` (cron by team timezone)
- `OFFHOURS_SCALE_UP`

## 16. Security design

- IAM-only access model:
  - Human users authenticate via AWS IAM Identity Center.
  - Service components authenticate with IAM roles.
  - Trust policies restrict access to this AWS account (and optionally AWS Organization ID).
- No local user accounts.
- RBAC by team and role.
- Pull secret stored only in secrets manager.
- Kubeadmin password stored as secret reference, not plaintext in DB.
- S3 bucket with SSE-KMS, private access, VPC endpoints.
- Workers run with least-privilege IAM role.
- Full audit log for create/destroy/access outputs.

## 17. Observability

Metrics:

- Job counts by type/status.
- Create and destroy duration percentiles.
- Destroy failure rate.
- Orphan resource count.
- Active cluster count by team/profile.

Logs:

- Structured JSON logs with correlation IDs.
- Installer logs shipped and indexed.

Alerts:

- Destroy failure > threshold.
- Cluster past TTL not destroyed.
- Orphan resources detected.

## 18. UI specification

Mockup reference:

- [/Users/tsanders/Documents/New project/cluster-webform-mockup.html](/Users/tsanders/Documents/New project/cluster-webform-mockup.html)

Pages:

1. Create Cluster form
2. Cluster list/inventory
3. Cluster detail/status/logs/outputs
4. Admin policy/profile page
5. Cost and usage dashboard

Form fields (user editable):

- `cluster_name`
- `platform`
- `version` (allowlist)
- `profile`
- `region` (allowlist)
- `base_domain` (allowlist)
- `owner/team`
- `cost_center`
- `ttl_hours`
- `ssh_public_key` (optional)
- `extra_tags`
- `offhours_opt_in`

Fields locked by policy:

- machine sizes/replicas for profile
- required tags
- max TTL

## 19. Operational runbooks

Runbook A: Failed create

1. Inspect job logs and installer output.
2. Retry create if transient.
3. If partial resources exist, run destroy using same state dir.
4. Mark request failed with operator notes.

Runbook B: Failed destroy

1. Retry destroy from restored install dir.
2. Run cloud API orphan sweep by `ClusterId` and `RequestId` tags.
3. Escalate to platform admin if persistent blockers.

Runbook C: Missing state artifact

1. Attempt restore from prior lifecycle snapshot.
2. If unavailable, execute controlled orphan cleanup by tags.
3. Record incident and root cause.

## 20. CI/CD and release strategy

- Trunk-based development.
- Required tests on merge:
  - API unit tests
  - worker workflow tests (mocked installer)
  - policy tests
  - migration tests
- Staged rollout:
  - dev -> staging -> prod
- Feature flags:
  - IBM support
  - off-hours worker scaling
  - automated orphan sweeps

## 21. Testing strategy

Test types:

1. Unit tests for profile rendering and policy validation.
2. Integration tests for API + queue + DB.
3. End-to-end tests in sandbox cloud account.
4. Chaos tests for worker interruption and retry behavior.
5. Security tests for IAM authz and secret leakage prevention.

Acceptance criteria:

- `create` to `ready` works for each profile.
- `destroy` removes all tagged resources for each profile.
- No orphaned resources in repeated create/destroy cycles.
- TTL janitor destroys expired clusters within SLA.

## 22. Implementation plan (phased)

Phase 1 (MVP):

- API + DB + queue + worker for AWS create/destroy.
- S3 artifact retention.
- Basic UI form + inventory page.
- Required tags and TTL janitor.

Phase 2:

- IBM Cloud support.
- Cost report ingestion by tags.
- Retry orchestration and orphan detector.

Phase 3:

- Off-hours worker scale down/up.
- Admin policy UI.
- Enhanced reporting and SLA dashboards.

## 23. Risks and mitigations

Risk: state artifact loss prevents clean destroy.
Mitigation: immutable S3 versioning, checksum verification, dual-region backup.

Risk: users bypass service and create unmanaged clusters.
Mitigation: IAM guardrails and policy to deny ad hoc provisioning patterns.

Risk: destroy failures due to cloud quota/API issues.
Mitigation: retries, alerting, operator runbooks, orphan sweep by tags.

Risk: secret exposure in logs.
Mitigation: redact filters and strict log hygiene tests.

## 24. Recommended tech stack

- Frontend: React + TypeScript
- API: Go or Node.js (TypeScript)
- Queue: SQS + worker autoscaling
- DB: PostgreSQL (RDS)
- Object storage: S3 with KMS
- Secrets: AWS Secrets Manager
- Auth: AWS IAM Identity Center + IAM roles/policies
- IaC for this platform: Terraform

## 25. Deliverables checklist

- API OpenAPI spec
- DB migration set
- Worker command runner module
- Profile catalog and policy engine
- UI screens (create/list/detail)
- Janitor and orphan scan jobs
- Runbooks and on-call docs
- Security threat model and controls checklist

## 26. Example `ocpctl` wrapper contract

CLI commands:

- `ocpctl create <profile> --name <cluster> --ttl 24 --owner team-a --cost-center sandbox`
- `ocpctl destroy --cluster-id <id>`
- `ocpctl scale-workers --cluster-id <id> --replicas 0`
- `ocpctl status --cluster-id <id>`

CLI behavior:

- Calls API, does not run installer locally in end-user context.
- Prints job ID and URL to track progress.

## 27. Decision log (initial)

1. Centralized pull secret management: approved.
2. Installer state retention in S3: approved.
3. Compact test clusters as default self-service profile: approved.
4. Off-hours strategy uses worker scaling, not full hibernation: approved.
5. Webform/API access restricted to AWS account IAM users/groups via IAM Identity Center: approved.

## 28. Open questions

1. What is the max allowed concurrent creates per team?
2. How long must artifacts be retained for compliance?
3. Should kubeadmin password retrieval require break-glass approval?
4. Should IBM Cloud launch in MVP or Phase 2?
