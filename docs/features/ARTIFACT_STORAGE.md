# S3 Artifact Storage for Autoscale Workers

## Problem

With autoscale workers, each worker instance has its own local `/var/lib/ocpctl/clusters/` directory. When a cluster is created on worker instance A, and later a resume/destroy job is picked up by worker instance B, the job fails because worker B doesn't have access to the cluster's artifacts (metadata.json, kubeconfig, etc.).

**Error Example:**
```
get infrastructure ID: read metadata.json: open /var/lib/ocpctl/clusters/<cluster-id>/metadata.json: no such file or directory
```

## Solution

All cluster artifacts are now automatically uploaded to S3 after cluster creation and downloaded on-demand before any operation that needs them.

### Architecture

**S3 Bucket Structure:**
```
s3://ocpctl-binaries/
  clusters/
    <cluster-id>/
      artifacts/
        auth/
          kubeconfig
          kubeadmin-password
        metadata.json
        openshift_install_state.json
        install-config.yaml
        openshift_install.log
        terraform.tfstate (if present)
        manifests/
          <manifest files>
        tls/
          <TLS files for STS clusters>
```

### Upload (Post-Creation)

After successful cluster creation, all artifacts are uploaded to S3:

```go
// In handler_create.go storeArtifacts()
artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
if err := artifactStorage.UploadClusterArtifacts(ctx, workDir, clusterID); err != nil {
    return fmt.Errorf("upload artifacts: %w", err)
}
```

**Uploaded artifacts:**
- `auth/kubeconfig` (required)
- `auth/kubeadmin-password` (required)
- `metadata.json` (required)
- `.openshift_install_state.json` (required for destroy/resume)
- `install-config.yaml.bak` (optional)
- `.openshift_install.log` (optional)
- `manifests/` directory (entire directory, needed for destroy)
- `tls/` directory (for STS/Manual mode clusters)

**Encryption:** All artifacts are encrypted at rest using AWS S3 server-side encryption (AES256).

### Download (Pre-Operation)

Before any operation that needs artifacts, the handler calls `ensureArtifactsAvailable()`:

```go
// Check if metadata.json already exists locally
if _, err := os.Stat(metadataPath); err == nil {
    log.Printf("Artifacts already available locally")
    return nil
}

// Download from S3
artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
if err := artifactStorage.DownloadClusterArtifacts(ctx, clusterID, workDir); err != nil {
    return fmt.Errorf("download artifacts: %w", err)
}
```

**Operations that download artifacts:**
- **Resume** (`handler_resume.go`) - needs `metadata.json` to get infraID
- **Hibernate** (`handler_hibernate.go`) - needs `metadata.json` to get infraID
- **Destroy** (`handler_destroy.go`) - needs entire work directory
- **Configure EFS** (`handler_configure_efs.go`) - needs `auth/kubeconfig`
- **Post-Configure** (`handler_post_configure.go`) - needs `auth/kubeconfig`

### Cleanup (Post-Destroy)

After successful cluster destruction, all artifacts are deleted from S3:

```go
// In handler_destroy.go
artifactStorage, err := NewArtifactStorage(ctx, h.config.S3BucketName)
if err := artifactStorage.DeleteClusterArtifacts(ctx, cluster.ID); err != nil {
    log.Printf("Warning: failed to delete artifacts from S3: %v", err)
}
```

## Configuration

**Environment Variable:**
```bash
S3_ARTIFACT_BUCKET=ocpctl-binaries  # Default bucket name
```

**IAM Permissions Required:**

Workers need additional S3 permissions:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:DeleteObject",
        "s3:ListBucket"
      ],
      "Resource": [
        "arn:aws:s3:::ocpctl-binaries/clusters/*",
        "arn:aws:s3:::ocpctl-binaries"
      ]
    }
  ]
}
```

## Deployment

### 1. Update IAM Policy

Add S3 permissions to the worker role:

```bash
# Get current policy
aws iam get-role-policy \
  --role-name ocpctl-worker-role \
  --policy-name ocpctl-worker-policy > /tmp/policy.json

# Edit /tmp/policy.json to add S3 permissions (see above)

# Update policy
aws iam put-role-policy \
  --role-name ocpctl-worker-role \
  --policy-name ocpctl-worker-policy \
  --policy-document file:///tmp/policy.json
```

### 2. Deploy New Worker Version

```bash
# Build and deploy
./scripts/deploy.sh

# Restart workers (autoscale instances will get new version automatically)
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@52.90.135.148 'sudo systemctl restart ocpctl-worker'
```

### 3. Verify

Check that artifacts are being uploaded:

```bash
# List artifacts for a cluster
aws s3 ls s3://ocpctl-binaries/clusters/<cluster-id>/artifacts/ --recursive

# Example output:
# 2026-03-17 14:30:00   12345 clusters/.../artifacts/auth/kubeconfig
# 2026-03-17 14:30:00    567 clusters/.../artifacts/auth/kubeadmin-password
# 2026-03-17 14:30:00   8901 clusters/.../artifacts/metadata.json
# 2026-03-17 14:30:00  234567 clusters/.../artifacts/openshift_install_state.json
```

## Backward Compatibility

**Existing clusters (created before this feature):**
- Artifacts were only stored locally on the worker that created them
- Resume/destroy may fail if picked up by a different worker
- **Workaround:** SSH to the original worker to perform operations

**Migrating existing clusters:**

1. Find which worker has the cluster's artifacts:
   ```bash
   # Check main worker
   ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@52.90.135.148 \
     "ls /var/lib/ocpctl/clusters/<cluster-id>/metadata.json"

   # Check autoscale worker
   ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@54.235.4.38 \
     "ls /var/lib/ocpctl/clusters/<cluster-id>/metadata.json"
   ```

2. Upload artifacts to S3 manually:
   ```bash
   # SSH to worker with artifacts
   ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@<worker-ip>

   # Upload to S3
   aws s3 sync /var/lib/ocpctl/clusters/<cluster-id>/ \
     s3://ocpctl-binaries/clusters/<cluster-id>/artifacts/ \
     --exclude "*.log" \
     --exclude ".terraform/*"
   ```

3. Now any worker can resume/destroy the cluster

## Troubleshooting

### Artifact Upload Fails

**Error:** `upload artifacts: PutObject: Access Denied`

**Cause:** Worker IAM role missing S3 permissions

**Fix:**
```bash
aws iam get-role-policy --role-name ocpctl-worker-role --policy-name ocpctl-worker-policy
# Add S3 permissions as shown above
```

### Artifact Download Fails

**Error:** `download artifacts: no artifacts found for cluster <id>`

**Cause:** Cluster was created before artifact storage feature was deployed

**Fix:** Manually upload artifacts (see Backward Compatibility section)

### Resume Still Fails After Download

**Error:** `metadata.json: no such file or directory` (even after download succeeds)

**Cause:** Download succeeded but extracted to wrong location

**Debug:**
```bash
# Check what was downloaded
ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@<worker-ip> \
  "ls -la /var/lib/ocpctl/clusters/<cluster-id>/"

# Check S3 structure
aws s3 ls s3://ocpctl-binaries/clusters/<cluster-id>/artifacts/ --recursive
```

## Performance

**Upload time:** ~2-5 seconds (for typical cluster artifacts ~10-20MB)

**Download time:** ~2-5 seconds

**Storage cost:** ~$0.02/cluster/month (based on ~20MB per cluster at $0.023/GB-month S3 Standard)

## Security

**Encryption:**
- At rest: AWS S3 server-side encryption (AES256)
- In transit: HTTPS (TLS 1.2+)

**Access Control:**
- IAM role-based access only
- No public access
- Bucket policy restricts to worker role

**Sensitive Data:**
- Kubeconfig contains cluster admin credentials
- Kubeadmin password is cluster root password
- NEVER expose S3 bucket publicly
- NEVER share IAM credentials

## Future Enhancements

**Phase 2:**
1. Add artifact versioning (keep historical states)
2. Add artifact compression (reduce S3 costs)
3. Add artifact lifecycle policies (auto-delete after cluster destroy)
4. Add artifact integrity checks (SHA256 checksums)
5. Add metrics for upload/download success rates

**Phase 3:**
1. Support multiple storage backends (EFS, EBS, Azure Blob)
2. Add artifact caching layer (reduce S3 API calls)
3. Add artifact deduplication (share common files across clusters)
