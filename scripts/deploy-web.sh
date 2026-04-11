#!/bin/bash
# Deploy ocpctl web frontend to production
# Usage: ./scripts/deploy-web.sh

set -e

# Configuration
SSH_KEY="$HOME/.ssh/ocpctl-production-key"
SSH_HOST="ubuntu@44.201.165.78"
PROD_PATH="/opt/ocpctl/web"
PROD_USER="ocpctl"

echo "🔨 Building web app..."
cd web
npm run build

echo "📦 Creating deployment tarball..."
cd ..
tar -czf /tmp/web-deploy.tar.gz \
  web/.next \
  web/package.json \
  web/package-lock.json \
  web/next.config.mjs

echo "📤 Uploading to production server..."
scp -i "$SSH_KEY" /tmp/web-deploy.tar.gz "$SSH_HOST":/tmp/

echo "🚀 Deploying to $PROD_PATH..."
ssh -i "$SSH_KEY" "$SSH_HOST" << EOF
  set -e

  # Extract to temporary location
  rm -rf /tmp/web-deploy
  mkdir -p /tmp/web-deploy
  tar -xzf /tmp/web-deploy.tar.gz -C /tmp/web-deploy

  # Backup current .next if it exists
  if [ -d "$PROD_PATH/.next" ]; then
    sudo mv "$PROD_PATH/.next" "$PROD_PATH/.next.backup-\$(date +%Y%m%d-%H%M%S)"
  fi

  # Move new build into place
  sudo cp -r /tmp/web-deploy/web/.next "$PROD_PATH/"
  sudo cp /tmp/web-deploy/web/package.json "$PROD_PATH/"
  sudo cp /tmp/web-deploy/web/package-lock.json "$PROD_PATH/"
  sudo cp /tmp/web-deploy/web/next.config.mjs "$PROD_PATH/"

  # Fix ownership
  sudo chown -R $PROD_USER:$PROD_USER "$PROD_PATH"

  # Clear cache and restart
  sudo rm -rf "$PROD_PATH/.next/cache"
  sudo systemctl restart ocpctl-web

  # Cleanup
  rm -rf /tmp/web-deploy /tmp/web-deploy.tar.gz

  echo "✅ Deployment complete"
EOF

# Cleanup local tarball
rm /tmp/web-deploy.tar.gz

echo ""
echo "🎉 Web app deployed successfully to production!"
echo "Service status:"
ssh -i "$SSH_KEY" "$SSH_HOST" 'sudo systemctl status ocpctl-web --no-pager -l | head -10'

echo ""
echo "Deployed files:"
ssh -i "$SSH_KEY" "$SSH_HOST" "ls -lh $PROD_PATH/.next/BUILD_ID $PROD_PATH/package.json 2>/dev/null"
