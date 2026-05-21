# OCPCTL Monitoring with Prometheus + Grafana

This directory contains configuration for monitoring OCPCTL with Prometheus and Grafana.

## Quick Start (Local Development)

### 1. Start the Monitoring Stack

```bash
# From the project root
docker-compose -f deploy/docker-compose.monitoring.yml up -d
```

This starts:
- **Prometheus** on http://localhost:9091
- **Grafana** on http://localhost:3001

### 2. Start OCPCTL

```bash
# Start API server (exposes metrics on /metrics)
make run-api

# The API server will expose metrics at http://localhost:8080/metrics
```

### 3. Access Grafana

1. Open http://localhost:3001
2. Login with:
   - Username: `admin`
   - Password: `admin`
3. Navigate to **Dashboards** → **OCPCTL** → **OCPCTL Overview**

The dashboard will auto-refresh every 15 seconds.

### 4. Access Prometheus

1. Open http://localhost:9091
2. Go to **Status** → **Targets** to see scrape targets
3. Use the query interface to explore metrics

## Available Metrics

### API Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `ocpctl_http_requests_total` | Counter | Total HTTP requests by method, endpoint, status |
| `ocpctl_http_request_duration_seconds` | Histogram | Request latency distribution |
| `ocpctl_http_requests_in_flight` | Gauge | Current concurrent requests |
| `ocpctl_auth_requests_total` | Counter | Authentication attempts by method and result |
| `ocpctl_rate_limit_hits_total` | Counter | Rate limit violations by endpoint |

### Worker Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `ocpctl_workers_total` | Gauge | Total workers by type (static/autoscale) |
| `ocpctl_workers_active` | Gauge | Workers currently processing jobs |
| `ocpctl_worker_uptime_seconds` | Gauge | Worker uptime per instance |

### Job Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `ocpctl_jobs_queued_total` | Gauge | Jobs in queue by type |
| `ocpctl_jobs_processing_total` | Gauge | Jobs currently being processed |
| `ocpctl_jobs_completed_total` | Counter | Completed jobs by type and status |
| `ocpctl_job_duration_seconds` | Histogram | Job processing duration |
| `ocpctl_job_wait_time_seconds` | Histogram | Time jobs spend in queue |
| `ocpctl_job_locks_acquired_total` | Counter | Lock acquisition attempts |

### Cluster Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `ocpctl_clusters_total` | Gauge | Total clusters by status |
| `ocpctl_clusters_by_profile` | Gauge | Clusters by profile |
| `ocpctl_clusters_by_region` | Gauge | Clusters by platform and region |
| `ocpctl_cluster_provision_duration_seconds` | Histogram | Cluster provisioning time |
| `ocpctl_cluster_created_total` | Counter | Cluster creation attempts |
| `ocpctl_cluster_cost_hourly_usd` | Gauge | Estimated hourly cost |

### Autoscale Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `ocpctl_autoscale_desired_workers` | Gauge | Desired worker count |
| `ocpctl_autoscale_current_workers` | Gauge | Current worker count |
| `ocpctl_autoscale_events_total` | Counter | Scaling events |

### Database Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `ocpctl_database_connections_open` | Gauge | Open database connections |
| `ocpctl_database_queries_total` | Counter | Database queries by operation |
| `ocpctl_database_query_duration_seconds` | Histogram | Query latency |

### System Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `ocpctl_build_info` | Gauge | Build version information |
| `ocpctl_uptime_seconds` | Gauge | Service uptime |

## Example PromQL Queries

### API Performance

```promql
# Request rate (requests/second)
rate(ocpctl_http_requests_total[1m])

# Error rate percentage
100 * sum(rate(ocpctl_http_requests_total{status=~"5.."}[5m])) / sum(rate(ocpctl_http_requests_total[5m]))

# P95 latency by endpoint
histogram_quantile(0.95, sum(rate(ocpctl_http_request_duration_seconds_bucket[5m])) by (le, endpoint))

# Requests by endpoint (top 10)
topk(10, sum(rate(ocpctl_http_requests_total[5m])) by (endpoint))
```

### Job Queue Analysis

```promql
# Total jobs in queue
sum(ocpctl_jobs_queued_total)

# Jobs by type
sum(ocpctl_jobs_queued_total) by (type)

# Job completion rate
rate(ocpctl_jobs_completed_total[5m])

# Job success rate percentage
100 * sum(rate(ocpctl_jobs_completed_total{status="success"}[5m])) / sum(rate(ocpctl_jobs_completed_total[5m]))

# Average job duration (last 1 hour)
avg(rate(ocpctl_job_duration_seconds_sum[1h]) / rate(ocpctl_job_duration_seconds_count[1h])) by (type)
```

### Cluster Insights

```promql
# Total clusters
sum(ocpctl_clusters_total)

# Clusters by status
sum(ocpctl_clusters_total) by (status)

# Clusters by profile
sum(ocpctl_clusters_by_profile) by (profile)

# Provisioning clusters (CREATING status)
ocpctl_clusters_total{status="CREATING"}

# Failed clusters in last hour
increase(ocpctl_cluster_created_total{result="failed"}[1h])
```

### Worker Health

```promql
# Active workers
ocpctl_workers_active

# Worker utilization percentage
100 * ocpctl_workers_active / sum(ocpctl_workers_total)

# Desired vs current workers (autoscaling)
ocpctl_autoscale_desired_workers - ocpctl_autoscale_current_workers
```

## Production Deployment

### Prerequisites

1. Install Prometheus on production server:
```bash
wget https://github.com/prometheus/prometheus/releases/download/v2.45.0/prometheus-2.45.0.linux-amd64.tar.gz
tar xzf prometheus-2.45.0.linux-amd64.tar.gz
sudo mv prometheus-2.45.0.linux-amd64/prometheus /usr/local/bin/
sudo mv prometheus-2.45.0.linux-amd64/promtool /usr/local/bin/
```

2. Copy configuration:
```bash
sudo mkdir -p /etc/prometheus /var/lib/prometheus
sudo cp deploy/prometheus.yml /etc/prometheus/
```

3. Create systemd service `/etc/systemd/system/prometheus.service`:
```ini
[Unit]
Description=Prometheus
After=network.target

[Service]
Type=simple
User=prometheus
Group=prometheus
ExecStart=/usr/local/bin/prometheus \
  --config.file=/etc/prometheus/prometheus.yml \
  --storage.tsdb.path=/var/lib/prometheus/ \
  --storage.tsdb.retention.time=30d

Restart=always

[Install]
WantedBy=multi-user.target
```

4. Start Prometheus:
```bash
sudo systemctl daemon-reload
sudo systemctl enable prometheus
sudo systemctl start prometheus
```

### Configure Grafana

1. Install Grafana:
```bash
sudo apt-get install -y software-properties-common
sudo add-apt-repository "deb https://packages.grafana.com/oss/deb stable main"
wget -q -O - https://packages.grafana.com/gpg.key | sudo apt-key add -
sudo apt-get update
sudo apt-get install grafana
```

2. Start Grafana:
```bash
sudo systemctl enable grafana-server
sudo systemctl start grafana-server
```

3. Add Prometheus data source:
   - URL: http://localhost:9090
   - Access: Server (default)

4. Import dashboard:
   - Upload `deploy/grafana/dashboards/ocpctl-overview.json`

## Troubleshooting

### Metrics not showing in Grafana

1. Check Prometheus is scraping successfully:
   - Visit http://localhost:9091/targets
   - Look for `ocpctl-api` target
   - Should be "UP" state

2. Check API metrics endpoint:
```bash
curl http://localhost:8080/metrics
```

Should return Prometheus-formatted metrics.

3. Check Grafana data source:
   - Go to Configuration → Data Sources → Prometheus
   - Click "Test" button
   - Should show "Data source is working"

### No data in time series panels

- Wait 30-60 seconds for metrics to be collected
- Check time range in Grafana (default: last 1 hour)
- Verify metrics collector is running (check API logs)

### High memory usage

Prometheus stores metrics in memory. To reduce:
- Decrease retention time in `prometheus.yml`:
  ```yaml
  --storage.tsdb.retention.time=7d  # Instead of 30d
  ```
- Reduce scrape frequency for less critical targets

## Advanced Configuration

### Custom Alerting Rules

Create `/etc/prometheus/alerts.yml`:

```yaml
groups:
  - name: ocpctl_alerts
    interval: 30s
    rules:
      - alert: HighErrorRate
        expr: |
          100 * sum(rate(ocpctl_http_requests_total{status=~"5.."}[5m]))
          / sum(rate(ocpctl_http_requests_total[5m])) > 5
        for: 5m
        annotations:
          summary: "High API error rate (>5%)"

      - alert: JobQueueBacklog
        expr: sum(ocpctl_jobs_queued_total) > 10
        for: 10m
        annotations:
          summary: "{{ $value }} jobs in queue"

      - alert: NoWorkersAvailable
        expr: ocpctl_workers_active == 0
        for: 2m
        annotations:
          summary: "No workers available"
```

Reference in `prometheus.yml`:
```yaml
rule_files:
  - "alerts.yml"
```

### Recording Rules (Pre-computed Queries)

For expensive queries that are used frequently:

```yaml
groups:
  - name: ocpctl_recording_rules
    interval: 15s
    rules:
      - record: job:ocpctl_http_request_duration_seconds:p95
        expr: histogram_quantile(0.95, sum(rate(ocpctl_http_request_duration_seconds_bucket[5m])) by (le, job))
```

## Monitoring Best Practices

1. **Set up alerts** - Don't just collect metrics, alert on anomalies
2. **Use recording rules** - Pre-compute expensive queries
3. **Monitor the monitors** - Alert if Prometheus stops scraping
4. **Retention planning** - Balance storage cost vs historical data needs
5. **Dashboard organization** - Create role-specific dashboards (ops, dev, business)

## Resources

- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
- [PromQL Cheat Sheet](https://promlabs.com/promql-cheat-sheet/)
