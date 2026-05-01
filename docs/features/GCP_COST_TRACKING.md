# GCP Cost Tracking

This document explains how to set up and use GCP cost tracking in ocpctl.

## Overview

ocpctl provides two modes of GCP cost tracking:

1. **Estimate-Based Tracking** (Default) - Uses profile-defined hourly cost estimates
2. **Actual Billing Tracking** (Optional) - Queries GCP Cloud Billing API for real costs

## Estimate-Based Tracking

Estimate-based tracking is always available and requires no additional setup. It calculates costs based on:

- Profile's `estimatedHourlyCost` setting
- Cluster state (running vs hibernated)
- Time period

### Hibernation Cost Reduction

When clusters are hibernated, costs are significantly reduced:

| Cluster Type | Hibernated Cost | Notes |
|--------------|----------------|-------|
| GKE Standard | 3% of running cost | No control plane charges, only persistent disk costs |
| OpenShift on GCP | 10% of running cost | VMs stopped, persistent disks remain |

### Example Estimates

**GKE Standard Profile** (`gcp-gke-standard.yaml`):
- Running: ~$0.05/hour (3x e2-medium nodes)
- Hibernated: ~$0.0015/hour (only disk storage)
- Monthly (always-on): ~$37
- Monthly (with work hours): ~$15

## Actual Billing Tracking (Optional)

To query actual GCP billing data, you need to:

### Prerequisites

1. **Enable Cloud Billing API**
   ```bash
   gcloud services enable cloudbilling.googleapis.com
   ```

2. **Set up BigQuery Billing Export**
   - Go to Cloud Console → Billing → Billing Export
   - Enable "BigQuery Export"
   - Create or select a BigQuery dataset
   - Note the dataset name (e.g., `billing_export`)

3. **Grant IAM Permissions**

   Your service account needs:
   ```bash
   # BigQuery Data Viewer on the billing export dataset
   gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
     --member="serviceAccount:YOUR_SA@YOUR_PROJECT.iam.gserviceaccount.com" \
     --role="roles/bigquery.dataViewer"

   # BigQuery Job User to run queries
   gcloud projects add-iam-policy-binding YOUR_PROJECT_ID \
     --member="serviceAccount:YOUR_SA@YOUR_PROJECT.iam.gserviceaccount.com" \
     --role="roles/bigquery.jobUser"
   ```

4. **Install `bq` Command-Line Tool**

   The `bq` tool is part of Google Cloud SDK:
   ```bash
   # Verify installation
   bq version
   ```

### Configuration

Set environment variables to enable billing API integration:

```bash
# Required: Your GCP project ID
export GCP_PROJECT="your-project-id"

# Required for billing API: BigQuery dataset with billing export
export GCP_BILLING_DATASET="billing_export"

# Optional: Billing table name (defaults to gcp_billing_export_v1)
export GCP_BILLING_TABLE="gcp_billing_export_v1_XXXXXX_YYYYYY_ZZZZZZ"
```

### Billing Export Data Schema

The cost tracker expects billing data to include resource labels. Ensure your resources are tagged with:

```yaml
managed-by: ocpctl
cluster-id: <cluster-uuid>
cluster-name: <cluster-name>
profile: <profile-name>
```

These labels are automatically applied by ocpctl when creating GCP resources.

## API Endpoints

### Get Costs by Cluster

```bash
GET /api/v1/costs/gcp?group_by=cluster&start_date=2024-01-01&end_date=2024-01-31
```

**Response:**
```json
{
  "start_date": "2024-01-01",
  "end_date": "2024-01-31",
  "group_by": "cluster",
  "costs": {
    "clusters": {
      "my-gke-cluster": {
        "cluster_id": "abc-123-def",
        "total_cost": 28.50,
        "breakdown": {
          "Compute Engine": 19.95,
          "Cloud Storage": 5.25,
          "Networking": 3.30
        },
        "period": "2024-01-01 to 2024-01-31",
        "profile": "gcp-gke-standard",
        "cluster_type": "gke"
      }
    },
    "total_cost": 28.50
  }
}
```

### Get Costs by Service

```bash
GET /api/v1/costs/gcp?group_by=service&start_date=2024-01-01&end_date=2024-01-31
```

**Response:**
```json
{
  "start_date": "2024-01-01",
  "end_date": "2024-01-31",
  "group_by": "service",
  "costs": {
    "Compute Engine": 45.80,
    "Cloud Storage": 12.50,
    "Kubernetes Engine": 8.20,
    "Networking": 6.75
  }
}
```

### Get Costs by Profile

```bash
GET /api/v1/costs/gcp?group_by=profile&start_date=2024-01-01&end_date=2024-01-31
```

**Response:**
```json
{
  "start_date": "2024-01-01",
  "end_date": "2024-01-31",
  "group_by": "profile",
  "costs": {
    "gcp-gke-standard": 28.50,
    "gcp-gke-large": 52.30,
    "4.18-gcp-standard": 78.90
  }
}
```

### Get Total Project Costs

```bash
GET /api/v1/costs/gcp/project?start_date=2024-01-01&end_date=2024-01-31
```

**Response:**
```json
{
  "start_date": "2024-01-01",
  "end_date": "2024-01-31",
  "total_cost": 159.70
}
```

## Cost Optimization Tips

### 1. Use Work Hours Automation

Enable work hours to automatically hibernate clusters during off-hours:

```yaml
work_hours_enabled: true
work_hours:
  start_time: "09:00"
  end_time: "17:00"
  work_days: ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"]
  timezone: "America/New_York"
```

**Savings:** ~65% reduction for 9-5 workdays (40 hours/week vs 168 hours/week)

### 2. Use Appropriate Instance Types

Choose the smallest instance type that meets your needs:

- **e2-micro**: Development/testing ($6-7/month)
- **e2-small**: Light workloads ($13-14/month)
- **e2-medium**: Standard workloads ($27-28/month) ← GKE default
- **e2-standard-4**: Heavy workloads ($100-110/month)

### 3. Enable Cluster Autoscaling

GKE node pools can scale down during low usage:

```yaml
compute:
  workers:
    minReplicas: 1
    maxReplicas: 10
    autoscaling: true
```

### 4. Use Preemptible/Spot VMs (For non-prod)

Preemptible VMs cost ~70% less but can be interrupted:

```yaml
platformConfig:
  gke:
    nodePools:
      - name: preemptible-pool
        machineType: e2-medium
        nodeCount: 3
        preemptible: true
```

### 5. Optimize Storage

- Delete unused persistent disks
- Use `pd-standard` for non-performance-critical workloads
- Enable disk autodelete when deleting nodes

### 6. Monitor and Alert

Set up budget alerts in GCP Cloud Billing:

```bash
# Example: Alert at 80% of $100 monthly budget
gcloud billing budgets create \
  --billing-account=BILLING_ACCOUNT_ID \
  --display-name="ocpctl Monthly Budget" \
  --budget-amount=100 \
  --threshold-rule=percent=80
```

## Troubleshooting

### "Billing API not enabled" Error

**Cause:** `GCP_BILLING_DATASET` environment variable not set

**Solution:**
- Set up BigQuery billing export (see Prerequisites)
- Set `GCP_BILLING_DATASET` environment variable
- Or continue using estimate-based tracking (no action needed)

### "Failed to query billing data" Error

**Possible causes:**
1. IAM permissions insufficient
2. `bq` command not installed
3. Billing export not configured correctly
4. Dataset/table name incorrect

**Debug:**
```bash
# Test BigQuery access manually
bq ls $GCP_BILLING_DATASET

# Test query manually
bq query --use_legacy_sql=false \
  "SELECT SUM(cost) as total FROM $GCP_BILLING_DATASET.$GCP_BILLING_TABLE LIMIT 1"
```

### Costs Show as Zero

**Possible causes:**
1. Billing export has a delay (up to 24 hours)
2. Resources not tagged with required labels
3. Query date range too recent

**Solution:**
- Wait 24-48 hours for billing data to populate
- Verify resources have `cluster-id` or `cluster-name` labels
- Query older date ranges to verify setup

## Example: Monthly Cost Report

To generate a monthly cost report for GCP clusters:

```bash
#!/bin/bash
# monthly-gcp-cost-report.sh

MONTH=$(date -d "last month" +%Y-%m)
START_DATE="${MONTH}-01"
END_DATE=$(date -d "${START_DATE} +1 month -1 day" +%Y-%m-%d)

echo "GCP Cost Report for ${MONTH}"
echo "=============================="
echo ""

# Total project costs
echo "Total Project Cost:"
curl -s -H "Authorization: Bearer $TOKEN" \
  "https://ocpctl.example.com/api/v1/costs/gcp/project?start_date=${START_DATE}&end_date=${END_DATE}" | \
  jq -r '.total_cost'

echo ""
echo "Costs by Cluster:"
curl -s -H "Authorization: Bearer $TOKEN" \
  "https://ocpctl.example.com/api/v1/costs/gcp?group_by=cluster&start_date=${START_DATE}&end_date=${END_DATE}" | \
  jq -r '.costs.clusters | to_entries[] | "\(.key): $\(.value.total_cost)"'
```

## References

- [GCP Cloud Billing Documentation](https://cloud.google.com/billing/docs)
- [BigQuery Billing Export](https://cloud.google.com/billing/docs/how-to/export-data-bigquery)
- [GCP Cost Optimization Best Practices](https://cloud.google.com/architecture/cost-optimization-best-practices)
- [GKE Pricing](https://cloud.google.com/kubernetes-engine/pricing)
