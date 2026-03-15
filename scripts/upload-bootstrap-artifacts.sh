#!/bin/bash
# Upload bootstrap artifacts to S3 for autoscaling workers
# Run this once to set up S3 bucket, then run after any config changes

set -e

S3_BUCKET="s3://ocpctl-binaries"

echo "Uploading bootstrap artifacts to S3..."

# Upload bootstrap script
echo "Uploading bootstrap-worker.sh..."
aws s3 cp scripts/bootstrap-worker.sh ${S3_BUCKET}/scripts/bootstrap-worker.sh

# Upload systemd service file
echo "Uploading ocpctl-worker.service..."
aws s3 cp scripts/ocpctl-worker.service ${S3_BUCKET}/scripts/ocpctl-worker.service

# Upload worker environment file (if exists)
if [ -f config/worker.env ]; then
    echo "Uploading worker.env (contains secrets - stored privately in S3)..."
    aws s3 cp config/worker.env ${S3_BUCKET}/config/worker.env
fi

echo ""
echo "✓ Bootstrap artifacts uploaded to S3"
echo ""
echo "Next steps:"
echo "  1. Update launch template user-data with scripts/user-data-worker.sh"
echo "  2. Or create new AMI with bootstrap.sh pre-installed at /opt/ocpctl/bootstrap.sh"
echo ""
echo "Test bootstrap on existing instance:"
echo "  ssh -i ~/.ssh/ocpctl-test-key.pem ec2-user@54.235.4.38"
echo "  sudo /opt/ocpctl/bootstrap.sh latest"
