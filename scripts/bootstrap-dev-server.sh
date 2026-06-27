#!/bin/bash
# Bootstrap script for OCPCTL dev server
# Usage: ./bootstrap-dev-server.sh <dev-server-ip>
#
# This script configures a freshly provisioned dev server with:
# - ocpctl user and group
# - Directory structure and permissions
# - nginx reverse proxy
# - Let's Encrypt SSL certificate
# - systemd service files

set -e

if [ -z "$1" ]; then
  echo "Usage: $0 <dev-server-ip>"
  echo "Example: $0 3.87.45.123"
  exit 1
fi

DEV_SERVER_IP=$1
SSH_KEY="$HOME/.ssh/ocpctl-dev-key"
SSH_USER="ubuntu"
DOMAIN="dev.ocpctl.mg.dog8code.com"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${YELLOW}=== OCPCTL Dev Server Bootstrap ===${NC}"
echo ""
echo "Server IP: $DEV_SERVER_IP"
echo "Domain: $DOMAIN"
echo ""

# Check if SSH key exists
if [ ! -f "$SSH_KEY" ]; then
  echo -e "${RED}Error: SSH key not found at $SSH_KEY${NC}"
  echo "Run: terraform -chdir=terraform/dev output -raw ssh_private_key > $SSH_KEY"
  echo "      chmod 600 $SSH_KEY"
  exit 1
fi

echo -e "${YELLOW}Step 1: Creating ocpctl user and group...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$DEV_SERVER_IP 'bash -s' << 'ENDSSH'
set -e

# Create ocpctl user and group
if ! id -u ocpctl > /dev/null 2>&1; then
  sudo useradd --system --create-home --shell /bin/bash ocpctl
  echo "✓ Created ocpctl user"
else
  echo "✓ ocpctl user already exists"
fi

# Set up directory structure with correct ownership
sudo mkdir -p /opt/ocpctl/{current,releases,profiles,addons,manifests,scripts}
sudo mkdir -p /var/lib/ocpctl/{clusters,tmp}
sudo mkdir -p /etc/ocpctl

sudo chown -R ocpctl:ocpctl /opt/ocpctl
sudo chown -R ocpctl:ocpctl /var/lib/ocpctl
sudo chown -R ocpctl:ocpctl /etc/ocpctl

sudo chmod 750 /opt/ocpctl
sudo chmod 750 /var/lib/ocpctl
sudo chmod 750 /etc/ocpctl

echo "✓ Directory structure created"
ENDSSH

echo -e "${GREEN}✓ ocpctl user and directories configured${NC}"
echo ""

echo -e "${YELLOW}Step 2: Installing nginx...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$DEV_SERVER_IP 'bash -s' << 'ENDSSH'
set -e

# Install nginx
if ! command -v nginx &> /dev/null; then
  sudo apt-get update
  sudo apt-get install -y nginx
  echo "✓ Installed nginx"
else
  echo "✓ nginx already installed"
fi

# Stop nginx for certbot standalone mode
sudo systemctl stop nginx
ENDSSH

echo -e "${GREEN}✓ nginx installed${NC}"
echo ""

echo -e "${YELLOW}Step 3: Setting up Let's Encrypt SSL certificate...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$DEV_SERVER_IP "bash -s" << ENDSSH
set -e

# Install certbot
if ! command -v certbot &> /dev/null; then
  sudo apt-get install -y certbot
  echo "✓ Installed certbot"
fi

# Get SSL certificate
sudo certbot certonly --standalone --non-interactive --agree-tos \
  --email tsanders@redhat.com \
  -d $DOMAIN

echo "✓ SSL certificate obtained"
ENDSSH

echo -e "${GREEN}✓ SSL certificate configured${NC}"
echo ""

echo -e "${YELLOW}Step 4: Configuring nginx reverse proxy...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$DEV_SERVER_IP "bash -s" << ENDSSH
set -e

# Create nginx site configuration
sudo tee /etc/nginx/sites-available/ocpctl << 'EOF'
# Redirect HTTP to HTTPS
server {
    listen 80;
    server_name $DOMAIN;
    return 301 https://\$host\$request_uri;
}

# HTTPS server
server {
    listen 443 ssl http2;
    server_name $DOMAIN;

    # SSL configuration
    ssl_certificate /etc/letsencrypt/live/$DOMAIN/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/$DOMAIN/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;

    # Proxy to API server
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        # WebSocket support (for future use)
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";

        # Timeouts
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }

    # Health check endpoint (direct, no auth)
    location /health {
        proxy_pass http://127.0.0.1:8080/health;
        access_log off;
    }

    # Version endpoint
    location /version {
        proxy_pass http://127.0.0.1:8080/version;
    }
}
EOF

# Enable site
sudo ln -sf /etc/nginx/sites-available/ocpctl /etc/nginx/sites-enabled/
sudo rm -f /etc/nginx/sites-enabled/default

# Test configuration
sudo nginx -t

# Start nginx
sudo systemctl enable nginx
sudo systemctl start nginx

echo "✓ nginx configured and started"
ENDSSH

echo -e "${GREEN}✓ nginx reverse proxy configured${NC}"
echo ""

echo -e "${YELLOW}Step 5: Deploying systemd service files...${NC}"
scp -i "$SSH_KEY" deploy/systemd/ocpctl-api.service $SSH_USER@$DEV_SERVER_IP:/tmp/
scp -i "$SSH_KEY" deploy/systemd/ocpctl-worker.service $SSH_USER@$DEV_SERVER_IP:/tmp/

ssh -i "$SSH_KEY" $SSH_USER@$DEV_SERVER_IP 'bash -s' << 'ENDSSH'
set -e

# Install systemd service files
sudo install -m 644 /tmp/ocpctl-api.service /etc/systemd/system/
sudo install -m 644 /tmp/ocpctl-worker.service /etc/systemd/system/
sudo rm /tmp/ocpctl-api.service /tmp/ocpctl-worker.service

# Reload systemd
sudo systemctl daemon-reload

echo "✓ systemd service files installed"
ENDSSH

echo -e "${GREEN}✓ systemd services configured${NC}"
echo ""

echo -e "${YELLOW}Step 6: Setting up certbot auto-renewal...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$DEV_SERVER_IP 'bash -s' << 'ENDSSH'
set -e

# Create renewal hook to reload nginx
sudo tee /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh << 'EOF'
#!/bin/bash
systemctl reload nginx
EOF

sudo chmod +x /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh

# Test renewal (dry run)
sudo certbot renew --dry-run

echo "✓ certbot auto-renewal configured"
ENDSSH

echo -e "${GREEN}✓ SSL auto-renewal configured${NC}"
echo ""

echo -e "${GREEN}=== Bootstrap Complete ===${NC}"
echo ""
echo "Next steps:"
echo "1. Create config files:"
echo "   cp config/api.env.dev.template config/api.env.dev"
echo "   cp config/worker.env.dev.template config/worker.env.dev"
echo ""
echo "2. Update config files with database connection and cloud credentials"
echo ""
echo "3. Update scripts/deploy-env.sh with dev server IP:"
echo "   API_HOST=\"$DEV_SERVER_IP\""
echo ""
echo "4. Initialize database:"
echo "   ./scripts/init-dev-database.sh"
echo ""
echo "5. Deploy services:"
echo "   ./scripts/deploy-env.sh dev"
echo ""
echo "6. Access dev environment:"
echo "   https://$DOMAIN"
echo ""
