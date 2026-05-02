# OpenTelemetry Distributed Tracing

OCPCTL includes built-in OpenTelemetry distributed tracing to help identify performance bottlenecks and debug issues in production.

## Features

- **End-to-end request tracing** across API → Worker → Database
- **Automatic HTTP request instrumentation** with status codes, paths, and response times
- **Job processing spans** with detailed metadata (job type, cluster info, duration)
- **Flexible exporters** - stdout for development, OTLP for production
- **Configurable sampling** - trace all requests in dev, sample 10% in production
- **Context propagation** - traces flow across service boundaries

## Configuration

Tracing is configured via environment variables:

### Enable Tracing

```bash
# Enable OpenTelemetry tracing (disabled by default)
export OTEL_ENABLED=true
```

### Environment

```bash
# Set deployment environment (affects sampling rate)
export ENVIRONMENT=production  # or development, staging
```

**Sampling rates by environment:**
- `development`: 100% (all requests traced)
- `staging`: 100% (all requests traced)
- `production`: 10% (sample 10% to reduce overhead)

### OTLP Exporter (Production)

```bash
# Set OTLP collector endpoint (e.g., Jaeger, AWS X-Ray, Datadog)
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317

# For AWS X-Ray, use the X-Ray daemon endpoint
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:2000
```

If `OTEL_EXPORTER_OTLP_ENDPOINT` is not set, traces are exported to stdout (development mode).

## Backends

### Jaeger (Recommended for Development)

1. **Run Jaeger all-in-one:**
   ```bash
   docker run -d --name jaeger \
     -e COLLECTOR_OTLP_ENABLED=true \
     -p 16686:16686 \
     -p 4317:4317 \
     jaegertracing/all-in-one:latest
   ```

2. **Configure OCPCTL:**
   ```bash
   export OTEL_ENABLED=true
   export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
   ```

3. **View traces:**
   Open http://localhost:16686 in your browser

### AWS X-Ray (Production)

1. **Run X-Ray daemon:**
   ```bash
   # Download and run X-Ray daemon
   wget https://s3.us-east-2.amazonaws.com/aws-xray-assets.us-east-2/xray-daemon/aws-xray-daemon-linux-3.x.zip
   unzip aws-xray-daemon-linux-3.x.zip
   ./xray -o -n us-east-1
   ```

2. **Configure OCPCTL:**
   ```bash
   export OTEL_ENABLED=true
   export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:2000
   export ENVIRONMENT=production
   ```

3. **IAM Permissions:**
   Ensure the EC2 instance or ECS task has permissions:
   ```json
   {
     "Version": "2012-10-17",
     "Statement": [
       {
         "Effect": "Allow",
         "Action": [
           "xray:PutTraceSegments",
           "xray:PutTelemetryRecords"
         ],
         "Resource": "*"
       }
     ]
   }
   ```

4. **View traces:**
   Open AWS X-Ray console: https://console.aws.amazon.com/xray/

### Datadog APM

1. **Run Datadog Agent:**
   ```bash
   docker run -d --name dd-agent \
     -e DD_API_KEY=<your-api-key> \
     -e DD_APM_ENABLED=true \
     -e DD_APM_NON_LOCAL_TRAFFIC=true \
     -p 8126:8126 \
     -p 4317:4317 \
     datadog/agent:latest
   ```

2. **Configure OCPCTL:**
   ```bash
   export OTEL_ENABLED=true
   export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317
   ```

3. **View traces:**
   Open Datadog APM: https://app.datadoghq.com/apm/traces

## Trace Examples

### API Request Trace

When a user creates a cluster via the API, you'll see a trace like:

```
POST /api/v1/clusters
  ├─ database: insert cluster record
  ├─ database: create CREATE job
  └─ duration: 45ms, status: 200
```

### Job Processing Trace

When the worker processes a cluster creation job:

```
process_job:CREATE
  ├─ job.id: abc-123
  ├─ job.type: CREATE
  ├─ cluster.name: my-cluster
  ├─ cluster.platform: aws
  ├─ cluster.profile: small-dev
  ├─ database: mark job started
  ├─ openshift-install: create cluster (40m)
  ├─ database: store cluster outputs
  ├─ database: mark job succeeded
  └─ duration: 40m 23s, status: ok
```

### Error Trace

When a job fails, the trace includes error details:

```
process_job:CREATE
  ├─ job.id: def-456
  ├─ job.type: CREATE
  ├─ cluster.name: my-cluster
  ├─ error.type: *errors.errorString
  ├─ error.message: openshift-install failed: insufficient capacity
  └─ duration: 12m 5s, status: error
```

## Instrumented Operations

### API Server

- **HTTP requests**: All API endpoints are automatically traced with:
  - HTTP method, path, status code
  - Request/response sizes
  - Client IP and user agent
  - Error details (if status >= 400)

### Worker

- **Job processing**: Each job is traced with:
  - Job type, ID, attempt number
  - Cluster metadata (name, platform, profile, region)
  - Job duration and result (success/error)
  - Error type and message (on failure)

## Performance Impact

Tracing adds minimal overhead:

- **Development (100% sampling)**: ~1-2ms per request
- **Production (10% sampling)**: ~0.1-0.2ms per request (amortized)
- **Batch exporters**: Traces are batched every 5 seconds (max 512 spans/batch)

## Troubleshooting

### Traces not appearing

1. **Check if tracing is enabled:**
   ```bash
   echo $OTEL_ENABLED
   # Should output: true
   ```

2. **Check logs for initialization:**
   ```bash
   # API logs
   grep "OpenTelemetry tracing" api.log
   # Should see: "OpenTelemetry tracing initialized successfully"

   # Worker logs
   grep "OpenTelemetry tracing" worker.log
   # Should see: "OpenTelemetry tracing initialized successfully"
   ```

3. **Verify OTLP endpoint:**
   ```bash
   # Test connectivity to collector
   nc -zv localhost 4317
   # Should output: Connection to localhost 4317 port [tcp/*] succeeded!
   ```

4. **Check sampling rate:**
   ```bash
   echo $ENVIRONMENT
   # If "production", only 10% of requests are traced
   # Set to "development" to trace all requests
   ```

### High cardinality warnings

If you see warnings about high cardinality attributes:

- Avoid adding unbounded attributes (e.g., timestamps, UUIDs in attribute values)
- Use structured logging for detailed debugging, not traces
- Traces should capture request flow, not every variable

### Missing context propagation

If traces aren't connecting across services:

- Ensure all services use compatible propagators (TraceContext, Baggage)
- Verify HTTP headers are being forwarded (`traceparent`, `tracestate`)
- Check that context is passed through all function calls

## Best Practices

1. **Use appropriate span names:**
   - ✅ `process_job:CREATE`, `GET /api/v1/clusters`
   - ❌ `job`, `api_call`

2. **Add relevant attributes:**
   - ✅ `cluster.name`, `job.type`, `error.type`
   - ❌ `debug_var_1`, `temp_value`

3. **Sample in production:**
   - Always set `ENVIRONMENT=production` in prod
   - This automatically enables 10% sampling
   - For high-traffic services, consider 1% sampling

4. **Monitor exporter health:**
   - Set up alerts for failed exports
   - Monitor batch queue size
   - Check for dropped spans

## Integration with Logs and Metrics

Traces complement logs and metrics:

- **Logs**: Detailed event data (what happened)
- **Metrics**: Aggregated statistics (how many, how fast)
- **Traces**: Request flow (how requests move through the system)

Example workflow:
1. **Metric** shows job failure rate increased
2. **Trace** identifies which service/function is failing
3. **Log** provides detailed error message and stack trace

## Future Enhancements

Planned improvements:

- [ ] Database query instrumentation (automatic span per query)
- [ ] AWS SDK instrumentation (S3, STS, IAM calls)
- [ ] Custom metrics via OpenTelemetry Metrics API
- [ ] Trace-based alerting (e.g., alert on slow requests)
- [ ] Trace sampling based on error status (always trace errors)

## References

- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/instrumentation/go/)
- [OTLP Specification](https://opentelemetry.io/docs/reference/specification/protocol/otlp/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- [AWS X-Ray Documentation](https://docs.aws.amazon.com/xray/)
- [Datadog APM](https://docs.datadoghq.com/tracing/)
