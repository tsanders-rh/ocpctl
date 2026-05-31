#!/bin/bash
# check-snapshot-regions.sh
# Check Windows VM snapshot availability across AWS regions

REGIONS=(
  "us-east-1" "us-west-2" "us-east-2" "eu-west-1" "ap-southeast-1"
  "ca-central-1" "sa-east-1" "ap-northeast-1" "ap-northeast-2"
  "ap-south-1" "eu-central-1" "eu-west-2" "eu-west-3" "eu-north-1"
)

echo "Checking Windows VM snapshot availability across regions..."
echo ""
echo "Snapshot Version: 1.0"
echo "SSM Path: /ocpctl/windows-snapshots/1.0/{region}"
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
printf "%-20s %-25s %-15s\n" "REGION" "SNAPSHOT ID" "STATUS"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

for REGION in "${REGIONS[@]}"; do
  printf "%-20s " "$REGION"

  # Check SSM Parameter Store
  SNAPSHOT_ID=$(aws ssm get-parameter \
    --name /ocpctl/windows-snapshots/1.0/$REGION \
    --region $REGION \
    --query 'Parameter.Value' \
    --output text 2>/dev/null)

  if [ -n "$SNAPSHOT_ID" ] && [ "$SNAPSHOT_ID" != "None" ]; then
    # Verify snapshot exists and get state
    STATE=$(aws ec2 describe-snapshots \
      --snapshot-ids $SNAPSHOT_ID \
      --region $REGION \
      --query 'Snapshots[0].State' \
      --output text 2>/dev/null)

    if [ "$STATE" = "completed" ]; then
      printf "%-25s " "$SNAPSHOT_ID"
      printf "\033[0;32m%-15s\033[0m\n" "✓ COMPLETED"
    elif [ "$STATE" = "pending" ]; then
      PROGRESS=$(aws ec2 describe-snapshots \
        --snapshot-ids $SNAPSHOT_ID \
        --region $REGION \
        --query 'Snapshots[0].Progress' \
        --output text)
      printf "%-25s " "$SNAPSHOT_ID"
      printf "\033[0;33m%-15s\033[0m\n" "⏳ $PROGRESS"
    else
      printf "%-25s " "$SNAPSHOT_ID"
      printf "\033[0;31m%-15s\033[0m\n" "⚠ $STATE"
    fi
  else
    printf "%-25s " "—"
    printf "\033[0;31m%-15s\033[0m\n" "❌ NOT FOUND"
  fi
done

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Legend:"
echo "  ✓ COMPLETED  - Snapshot ready, fast path (2-3 min) will be used"
echo "  ⏳ PENDING   - Snapshot being created (background process)"
echo "  ❌ NOT FOUND - No snapshot, will use S3 fallback (30-50 min)"
echo ""
echo "Testing Recommendations:"
echo "  • Fast path test: Use any region with ✓ COMPLETED status"
echo "  • Slow path test: Use any region with ❌ NOT FOUND status"
echo "  • Verify reuse: Create 2nd cluster in same region after ⏳ completes"
