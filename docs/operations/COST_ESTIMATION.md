# OCPCTL Cost Estimation Guide

**Purpose:** Detailed cost breakdown for running ocpctl and provisioning clusters on AWS.

**Last Updated:** 2026-05-08
**Pricing Source:** AWS US-East-1 (prices vary by region)

---

## Table of Contents

1. [Platform Infrastructure Costs](#platform-infrastructure-costs)
2. [Cluster Costs](#cluster-costs)
3. [Network and Data Transfer](#network-and-data-transfer)
4. [Storage Costs](#storage-costs)
5. [Cost Optimization Strategies](#cost-optimization-strategies)
6. [Monthly Cost Calculator](#monthly-cost-calculator)
7. [Cost Monitoring and Alerts](#cost-monitoring-and-alerts)

---

## Platform Infrastructure Costs

These are the costs for running the ocpctl platform itself (API server, workers, database).

### Option A: PostgreSQL on EC2 (Recommended for Testing)

| Component | Instance Type | Monthly Cost | Notes |
|-----------|---------------|--------------|-------|
| **API Server + DB** | t3.large (2 vCPU, 8GB RAM) | $60.74 | Runs API, backup worker, PostgreSQL |
| **EBS Root Volume** | 100 GB gp3 | $8.00 | OS, binaries, cluster work directories |
| **Elastic IP** | 1 EIP (optional) | $3.65 | Static IP for production |
| **Data Transfer Out** | ~10 GB/month | $0.90 | API responses, web UI |
| **Route53 Queries** | ~1M queries/month | $0.50 | DNS lookups |
| **Parameter Store** | Standard tier | $0.00 | Free for standard parameters |
| **CloudWatch Logs** | ~5 GB/month | $2.50 | Service logs (optional) |
| **S3 Standard** | ~50 GB | $1.15 | Cluster artifacts, binaries |
| | | |
| **Total (Base)** | | **~$73/month** | Minimal viable production |
| **Total (with CloudWatch)** | | **~$77/month** | With monitoring |

**Breakdown:**
- EC2 t3.large: $0.0832/hour × 730 hours = $60.74/month
- EBS gp3: 100 GB × $0.08/GB = $8.00/month
- Elastic IP: $3.65/month (free if attached to running instance)
- Data Transfer: 10 GB × $0.09/GB = $0.90/month (first GB free)
- Route53: (1,000,000 - 1,000,000 free) × $0.50 = $0.50/month
- S3: 50 GB × $0.023/GB = $1.15/month
- CloudWatch Logs: 5 GB × $0.50/GB = $2.50/month

### Option B: RDS PostgreSQL (Production Recommended)

| Component | Instance Type | Monthly Cost | Notes |
|-----------|---------------|--------------|-------|
| **API Server** | t3.large (2 vCPU, 8GB RAM) | $60.74 | Runs API and backup worker |
| **RDS PostgreSQL** | db.t3.micro (1 vCPU, 1GB RAM) | $12.41 | Managed database |
| **RDS Storage** | 20 GB gp3 | $2.53 | Database storage |
| **RDS Backup** | 20 GB | $2.00 | 7-day automated backups |
| **EBS Root Volume** | 100 GB gp3 | $8.00 | API server OS and binaries |
| **Elastic IP** | 1 EIP (optional) | $3.65 | Static IP |
| **Data Transfer Out** | ~10 GB/month | $0.90 | |
| **Route53 Queries** | ~1M queries/month | $0.50 | |
| **S3 Standard** | ~50 GB | $1.15 | |
| **CloudWatch Logs** | ~5 GB/month | $2.50 | |
| | | |
| **Total (Base)** | | **~$89/month** | Production-ready |
| **Total (with monitoring)** | | **~$94/month** | With CloudWatch |

**RDS Benefits:**
- ✅ Automated backups (7-30 days retention)
- ✅ Automated patching
- ✅ Multi-AZ failover option (adds ~100% cost)
- ✅ Read replicas option
- ✅ Point-in-time recovery

**When to use RDS:**
- Production deployments
- Compliance requirements (automated backups)
- Long-term deployments (6+ months)
- Need for high availability

### Option C: Autoscaling Workers (Production Scale)

For deployments with high cluster creation volume, add autoscaling workers:

| Component | Configuration | Monthly Cost | Notes |
|-----------|---------------|--------------|-------|
| **Autoscaling Workers** | 1-10 × t3.small | $15.18 - $151.80 | Scales based on job queue |
| **Worker EBS Volumes** | 1-10 × 50 GB gp3 | $4.00 - $40.00 | Work directories |
| **Data Transfer (workers)** | ~50 GB/month | $4.50 | Cluster provisioning traffic |
| | | |
| **Average (3 workers)** | | **~$57/month** | Typical production load |
| **Peak (10 workers)** | | **~$196/month** | High load periods |

**Autoscaling Configuration:**
- Min: 1 worker (always on)
- Max: 10 workers (burst capacity)
- Average: 2-3 workers (normal load)
- Scale up when: Job queue > 3 pending jobs
- Scale down when: No jobs for 15 minutes

**Combined Platform Cost (Production with ASG):**
```
Option B (RDS) + Average ASG = $94 + $57 = ~$151/month
```

---

## Cluster Costs

These are the costs for OpenShift/Kubernetes clusters provisioned by ocpctl.

### OpenShift Clusters (IPI - Installer Provisioned Infrastructure)

#### Single Node OpenShift (SNO)

**Configuration:**
- 1 × m6i.2xlarge (8 vCPU, 32 GB RAM) - Control plane (schedulable)
- 120 GB gp3 root volume
- 2 × Network Load Balancers (API + apps)
- Route53 hosted zone

| Component | Specification | Hourly Cost | Monthly Cost (730 hrs) |
|-----------|---------------|-------------|------------------------|
| EC2 Instance | m6i.2xlarge | $0.384 | $280.32 |
| EBS Volume | 120 GB gp3 | $0.011 | $8.00 |
| NLB (2×) | 2 × $0.0225/hr + 0.006/LCU | $0.045 | $32.85 |
| Route53 Zone | Hosted zone | $0.068 | $50.00 |
| Data Transfer | ~10 GB/month | - | $0.90 |
| | | |
| **Total** | | **$0.508/hour** | **~$372/month** |

**Hibernation Costs:**
- Hibernated state: ~$0.038/hour (~$28/month) - 92% savings
- Only charged for: EBS storage + Route53 zone

**Recommended Usage:**
- Development clusters
- Test environments
- Demo clusters
- Cost-effective single-workload deployments

#### Standard 3-Node Cluster

**Configuration:**
- 3 × m6i.xlarge (4 vCPU, 16 GB RAM) - Control plane
- 3 × m6i.2xlarge (8 vCPU, 32 GB RAM) - Workers
- 6 × 120 GB gp3 volumes
- 2 × Network Load Balancers
- Route53 hosted zone

| Component | Specification | Hourly Cost | Monthly Cost |
|-----------|---------------|-------------|--------------|
| Control Plane (3×) | 3 × m6i.xlarge | $0.576 | $420.48 |
| Workers (3×) | 3 × m6i.2xlarge | $1.152 | $840.96 |
| EBS Volumes (6×) | 6 × 120 GB gp3 | $0.066 | $48.00 |
| NLB (2×) | 2 × NLB | $0.045 | $32.85 |
| Route53 Zone | Hosted zone | $0.068 | $50.00 |
| NAT Gateway | 3 × NAT (HA) | $0.135 | $98.55 |
| Data Transfer | ~50 GB/month | - | $4.50 |
| | | |
| **Total** | | **$2.042/hour** | **~$1,491/month** |

**Hibernation Costs:**
- Hibernated: ~$0.203/hour (~$148/month) - 90% savings
- Charged for: EBS + Route53 + NAT Gateways (can't stop)

**Cost Optimization:**
- Use single NAT gateway (not HA): Save $65/month
- Reduce worker replicas to 2: Save $280/month
- Use spot instances for workers: Save ~70% on worker costs

#### Minimal Test Cluster

**Configuration:**
- 3 × m6i.large (2 vCPU, 8 GB RAM) - Control plane
- 0 workers (control plane schedulable)
- 3 × 120 GB gp3 volumes
- 2 × NLB
- Route53 zone
- Single NAT gateway

| Component | Specification | Hourly Cost | Monthly Cost |
|-----------|---------------|-------------|--------------|
| Control Plane (3×) | 3 × m6i.large | $0.288 | $210.24 |
| EBS Volumes (3×) | 3 × 120 GB gp3 | $0.033 | $24.00 |
| NLB (2×) | 2 × NLB | $0.045 | $32.85 |
| Route53 Zone | Hosted zone | $0.068 | $50.00 |
| NAT Gateway | 1 × NAT | $0.045 | $32.85 |
| Data Transfer | ~20 GB/month | - | $1.80 |
| | | |
| **Total** | | **$0.479/hour** | **~$350/month** |

**Recommended For:**
- Short-lived test clusters (< 24 hours)
- CI/CD pipelines
- Training environments

### ROSA Clusters (Red Hat OpenShift Service on AWS)

ROSA has a different cost model with AWS managing the control plane:

| Component | Specification | Hourly Cost | Monthly Cost |
|-----------|---------------|-------------|--------------|
| **ROSA Service Fee** | Control plane managed by AWS | $0.03 | $21.90 |
| **Worker Nodes (2×)** | 2 × m6i.2xlarge | $0.768 | $560.64 |
| **EBS Volumes (2×)** | 2 × 120 GB gp3 | $0.022 | $16.00 |
| **Data Transfer** | ~30 GB/month | - | $2.70 |
| | | |
| **Total** | | **$0.820/hour** | **~$601/month** |

**ROSA Hibernation:**
- Control plane: Cannot be stopped ($0.03/hour always)
- Workers: Can scale to 0 (saves $0.79/hour)
- Hibernated cost: ~$22/month (control plane only)

**ROSA Benefits:**
- ✅ AWS-managed control plane (SLA)
- ✅ Integrated with AWS services
- ✅ Red Hat support included
- ✅ Simplified upgrades

### EKS Clusters (Elastic Kubernetes Service)

| Component | Specification | Hourly Cost | Monthly Cost |
|-----------|---------------|-------------|--------------|
| **EKS Control Plane** | Managed by AWS | $0.10 | $73.00 |
| **Worker Nodes (3×)** | 3 × m6i.large | $0.288 | $210.24 |
| **EBS Volumes (3×)** | 3 × 50 GB gp3 | $0.014 | $10.20 |
| **Data Transfer** | ~20 GB/month | - | $1.80 |
| | | |
| **Total** | | **$0.402/hour** | **~$295/month** |

**EKS Hibernation:**
- Control plane: Cannot be stopped ($0.10/hour always)
- Workers: Can scale to 0
- Hibernated cost: ~$73/month

### GKE Clusters (Google Kubernetes Engine)

*Note: Pricing is for GCP us-east1 region*

| Component | Specification | Hourly Cost | Monthly Cost |
|-----------|---------------|-------------|--------------|
| **GKE Control Plane** | Zonal cluster (free) | $0.00 | $0.00 |
| **Worker Nodes (3×)** | 3 × n2-standard-4 | $0.388 | $283.24 |
| **Persistent Disks (3×)** | 3 × 50 GB SSD | $0.020 | $14.60 |
| **Data Transfer** | ~20 GB/month | - | $2.40 |
| | | |
| **Total** | | **$0.408/hour** | **~$300/month** |

**GKE Cost Advantages:**
- Free zonal control plane (1 zone)
- Autopilot mode: Pay only for pods (not nodes)
- Preemptible nodes: ~80% discount

---

## Network and Data Transfer

### Data Transfer Costs

| Type | Price | Notes |
|------|-------|-------|
| **Data Transfer IN** | $0.00/GB | Always free |
| **Data Transfer OUT (Internet)** | $0.09/GB (first 10 TB/month) | To internet |
| **Data Transfer OUT (Internet)** | $0.085/GB (next 40 TB) | Volume discount |
| **Data Transfer OUT (Same Region)** | $0.01/GB | Between AZs |
| **Data Transfer OUT (Cross Region)** | $0.02/GB | Between regions |
| **NAT Gateway Data Processing** | $0.045/GB | All traffic through NAT |

### Typical Data Transfer by Cluster Type

| Cluster Type | Monthly Transfer | Cost |
|--------------|------------------|------|
| SNO (light usage) | ~10 GB | $0.90 |
| Standard (moderate) | ~50 GB | $4.50 |
| Production (heavy) | ~200 GB | $18.00 |
| ROSA (managed) | ~30 GB | $2.70 |

**High Transfer Scenarios:**
- Image registry pulls: ~5-20 GB/cluster/month
- Logging to external systems: ~10-50 GB/month
- Monitoring metrics export: ~5-10 GB/month
- CI/CD pipelines: ~20-100 GB/month

### NAT Gateway Costs

| Configuration | Hourly Cost | Monthly Cost | Use Case |
|---------------|-------------|--------------|----------|
| **Single NAT** | $0.045 | $32.85 | Testing, cost-sensitive |
| **HA (3 NATs)** | $0.135 | $98.55 | Production, high availability |

**Data Processing:**
- $0.045/GB for all traffic through NAT
- Average cluster: ~100 GB/month = $4.50

---

## Storage Costs

### S3 Storage

| Storage Class | Price/GB/month | Use Case |
|---------------|----------------|----------|
| **S3 Standard** | $0.023 | Frequently accessed (kubeconfigs, binaries) |
| **S3 Intelligent-Tiering** | $0.023 (frequent) | Automatic cost optimization |
| **S3 Standard-IA** | $0.0125 | Infrequently accessed (old cluster artifacts) |
| **S3 Glacier Instant** | $0.004 | Archive compliance logs |

**Typical S3 Usage:**
- ocpctl binaries: ~2 GB
- Cluster artifacts (per cluster): ~100-500 MB
- 50 clusters × 300 MB average = ~15 GB
- **Total: ~17 GB = $0.39/month** (S3 Standard)

**Lifecycle Policies:**
```
0-30 days: S3 Standard ($0.023/GB)
30-90 days: S3 Standard-IA ($0.0125/GB)
90+ days: Delete or Glacier ($0.004/GB)
```

### EBS Storage

| Volume Type | Price/GB/month | IOPS | Throughput | Use Case |
|-------------|----------------|------|------------|----------|
| **gp3** | $0.08 | 3,000 (baseline) | 125 MB/s | General purpose (recommended) |
| **gp2** | $0.10 | 3 IOPS/GB | - | Legacy general purpose |
| **io2** | $0.125 + IOPS | Custom | Custom | High performance databases |

**Typical EBS Costs:**
- API server: 100 GB gp3 = $8.00/month
- SNO cluster: 120 GB gp3 = $9.60/month
- 3-node cluster: 6 × 120 GB = $57.60/month

### EFS Storage (Shared File Systems)

| Storage Class | Price/GB/month | Use Case |
|---------------|----------------|----------|
| **EFS Standard** | $0.30 | Frequently accessed |
| **EFS Infrequent Access** | $0.025 | Rarely accessed (lifecycle) |

**Typical EFS Usage:**
- Migration shared storage: ~50 GB = $15.00/month
- Lifecycle to IA after 30 days: ~$1.25/month

---

## Cost Optimization Strategies

### 1. Cluster Lifecycle Management

**Automatic TTL Destruction:**
- Set aggressive TTLs for test clusters: 4-8 hours
- Development clusters: 24-72 hours
- Automatic destruction saves: **~$300-400/month per unused cluster**

**Hibernation Schedules:**
- Hibernate during off-hours (6 PM - 8 AM): **70-80% savings**
- Hibernate weekends: **Additional 30% monthly savings**
- SNO hibernation savings: **$280/month → $28/month**

### 2. Right-Sizing Instances

| Scenario | Original | Optimized | Monthly Savings |
|----------|----------|-----------|-----------------|
| API Server (low traffic) | t3.large | t3.medium | $30 |
| SNO (light workload) | m6i.2xlarge | m6i.xlarge | $140 |
| Worker nodes (bursty) | m6i.2xlarge | m6i.xlarge + auto-scale | $280+ |

### 3. Spot Instances for Workers

**Use Case:** Non-production clusters, CI/CD, batch workloads

| Instance Type | On-Demand | Spot (avg) | Savings |
|---------------|-----------|----------|---------|
| m6i.xlarge | $0.192/hr | $0.058/hr | 70% |
| m6i.2xlarge | $0.384/hr | $0.115/hr | 70% |

**Savings Example:**
- 3 worker nodes: $840/month → $252/month = **$588/month saved**

**Caution:**
- Spot instances can be interrupted
- Not suitable for production control planes
- Best for stateless, fault-tolerant workloads

### 4. NAT Gateway Optimization

**Options:**
1. **Single NAT (non-HA):** $98.55 → $32.85 = **$65.70/month saved**
2. **NAT Instance (t3.micro):** $6.57/month = **$91.98/month saved**
3. **VPC Endpoints (S3, ECR):** Eliminate NAT for AWS services

### 5. Reserved Instances / Savings Plans

**For long-term deployments (1-3 years):**

| Commitment | EC2 Discount | RDS Discount |
|------------|--------------|--------------|
| 1 year, no upfront | 30-40% | 35% |
| 1 year, partial upfront | 35-45% | 40% |
| 3 years, all upfront | 50-60% | 50% |

**Example Savings:**
- API Server (t3.large, 1-year): $60.74 → $36.44 = **$24.30/month saved**
- SNO (m6i.2xlarge, 1-year): $280 → $168 = **$112/month saved**

### 6. Storage Optimization

**S3 Lifecycle Policies:**
```yaml
Rules:
  - Delete cluster artifacts after 90 days
  - Move old binaries to Glacier after 180 days
  - Delete destroyed cluster artifacts after 30 days
```
**Savings:** ~$10-20/month for 100+ clusters

**EBS Snapshots:**
- Only snapshot critical data
- Delete old snapshots (> 30 days)
- **Savings:** $5-10/month

### 7. Network Optimization

**Reduce Data Transfer:**
- Use VPC endpoints for S3, ECR, ECR-API
- Keep clusters in same region as ocpctl
- Use CloudFront for static content (if web UI heavy)

**Savings:** ~$10-30/month depending on usage

---

## Monthly Cost Calculator

### Platform Infrastructure

```
Base Platform Cost:
  - PostgreSQL on EC2: $73/month
  - OR RDS PostgreSQL: $94/month
  - Autoscaling workers (avg 3): +$57/month

Total Platform: $73-151/month
```

### Cluster Costs

```
Formula per cluster:
  Hourly Cost × Hours Active × (1 - Hibernation %)

Examples:
  SNO (24/7 active):
    $0.508 × 730 × 100% = $371/month

  SNO (hibernated 16 hrs/day + weekends):
    Active: $0.508 × 232 hrs = $118
    Hibernated: $0.038 × 498 hrs = $19
    Total: $137/month (63% savings)

  Standard (work hours only):
    Active: $2.042 × 200 hrs = $408
    Hibernated: $0.203 × 530 hrs = $108
    Total: $516/month (65% savings)
```

### Example Monthly Bills

**Scenario 1: Small Team (5 dev clusters)**
```
Platform (RDS + 2 workers):        $131
5 × SNO (hibernated off-hours):    $685
S3 storage (30 GB):                  $1
Data transfer (100 GB):              $9
───────────────────────────────────────
Total:                             ~$826/month
```

**Scenario 2: Medium Team (15 clusters, mixed)**
```
Platform (RDS + 3 workers):        $151
10 × SNO (aggressive hibernation): $1,370
3 × Standard (work hours only):    $1,548
2 × ROSA (testing):                $1,202
S3 storage (100 GB):                  $2
Data transfer (500 GB):             $45
───────────────────────────────────────
Total:                           ~$4,318/month
```

**Scenario 3: Large Team (50 clusters, production)**
```
Platform (RDS + 5 workers):        $189
30 × SNO (varied usage):          $4,110
15 × Standard (production):       $22,365
5 × ROSA (production):             $3,005
S3 storage (500 GB):                 $12
Data transfer (2 TB):               $180
CloudWatch monitoring:               $50
───────────────────────────────────────
Total:                          ~$29,911/month
```

---

## Cost Monitoring and Alerts

### AWS Cost Explorer

**Set up custom reports:**
1. Filter by tag: `ManagedBy=ocpctl`
2. Group by: `Owner`, `Team`, `CostCenter`
3. Daily granularity for active clusters

### Budget Alerts

**Recommended Budgets:**

```bash
# Platform budget
aws budgets create-budget \
  --account-id 123456789012 \
  --budget '{
    "BudgetName": "ocpctl-platform",
    "BudgetLimit": {
      "Amount": "200",
      "Unit": "USD"
    },
    "TimeUnit": "MONTHLY",
    "BudgetType": "COST"
  }'

# Per-cluster budget
aws budgets create-budget \
  --account-id 123456789012 \
  --budget '{
    "BudgetName": "ocpctl-clusters",
    "BudgetLimit": {
      "Amount": "5000",
      "Unit": "USD"
    },
    "TimeUnit": "MONTHLY",
    "CostFilters": {
      "TagKeyValue": ["ManagedBy$ocpctl"]
    }
  }'
```

### Cost Anomaly Detection

Enable AWS Cost Anomaly Detection:
```bash
aws ce create-anomaly-monitor \
  --anomaly-monitor '{
    "MonitorName": "ocpctl-anomalies",
    "MonitorType": "DIMENSIONAL",
    "MonitorDimension": "SERVICE"
  }'
```

### Tagging Strategy for Cost Tracking

**Required Tags (set by ocpctl):**
- `ManagedBy: ocpctl`
- `Owner: user@example.com`
- `Team: platform-team`
- `CostCenter: 733`
- `ClusterID: cluster-abc123`
- `Environment: production|development|test`

**Cost Allocation Report:**
```sql
SELECT
  owner,
  team,
  cost_center,
  COUNT(*) as cluster_count,
  SUM(estimated_monthly_cost) as total_cost
FROM clusters
WHERE status IN ('READY', 'CREATING', 'HIBERNATED')
GROUP BY owner, team, cost_center
ORDER BY total_cost DESC;
```

---

## Cost Comparison Summary

| Cluster Type | Hourly (Active) | Hourly (Hibernated) | Monthly (24/7) | Monthly (Work Hours) |
|--------------|-----------------|---------------------|----------------|----------------------|
| **SNO** | $0.51 | $0.04 | $371 | $137 |
| **3-Node Standard** | $2.04 | $0.20 | $1,491 | $516 |
| **Minimal Test** | $0.48 | $0.15 | $350 | $198 |
| **ROSA (2 workers)** | $0.82 | $0.03 | $601 | $218 |
| **EKS (3 workers)** | $0.40 | $0.10 | $295 | $153 |
| **GKE (3 workers)** | $0.41 | $0.02 | $300 | $110 |

**Key Takeaways:**
1. Hibernation saves 70-90% on cluster costs
2. Work-hours-only clusters cost 50-65% less than 24/7
3. Platform costs ($73-151/month) are small compared to cluster costs
4. Autoscaling workers add flexibility with minimal cost increase
5. Proper tagging and lifecycle management are essential for cost control

---

## Related Documentation

- [AWS Quickstart Guide](../deployment/AWS_QUICKSTART.md)
- [Deployment Checklist](../deployment/DEPLOYMENT_CHECKLIST.md)
- [AWS IAM Permissions](AWS_IAM_PERMISSIONS.md)
- [Disaster Recovery](DISASTER_RECOVERY.md)

---

**Last Updated:** 2026-05-08
**Pricing Version:** 2026 Q2
**Next Review:** 2026-08-01 (quarterly)
