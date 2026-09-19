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

# Dev targeting from the single source of truth (config/environments.sh)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/../config/environments.sh"
load_environment dev

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

# Set up directory structure with correct ownership.
#
# NOTE: 'current' is deliberately NOT created here. deploy-env.sh publishes a
# release with `ln -snf .../releases/<version> /opt/ocpctl/current`; if 'current'
# already exists as a directory, ln drops the symlink *inside* it and the unit's
# ExecStart=/opt/ocpctl/current/ocpctl-api resolves to nothing (203/EXEC).
sudo mkdir -p /opt/ocpctl/{releases,profiles,addons,manifests,scripts}
sudo mkdir -p /var/lib/ocpctl/{clusters,tmp}
sudo mkdir -p /etc/ocpctl
# The systemd units run with ProtectSystem=strict and
# ReadWritePaths=/opt/ocpctl /var/lib/ocpctl /var/log/ocpctl. A ReadWritePaths
# entry that does not exist is a hard start failure (226/NAMESPACE), so this
# directory must exist before the services are started.
sudo mkdir -p /var/log/ocpctl

sudo chown -R ocpctl:ocpctl /opt/ocpctl
sudo chown -R ocpctl:ocpctl /var/lib/ocpctl
sudo chown -R ocpctl:ocpctl /etc/ocpctl
sudo chown ocpctl:ocpctl /var/log/ocpctl

sudo chmod 750 /opt/ocpctl
sudo chmod 750 /var/lib/ocpctl
sudo chmod 750 /etc/ocpctl
sudo chmod 755 /var/log/ocpctl

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

# Node.js for the Next.js frontend. The ocpctl-web unit runs /usr/bin/npm start,
# so without this the service fails at 203/EXEC and only the API comes up.
# Ubuntu's own nodejs package is too old for Next.js 14 — use NodeSource 20.x,
# matching production (v20.x).
if ! command -v node &> /dev/null; then
  curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
  sudo apt-get install -y nodejs
  echo "✓ Installed Node.js $(node --version)"
else
  echo "✓ Node.js already installed ($(node --version))"
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

    # Routing mirrors production: the Next.js frontend owns '/', and the Go API
    # is reached under '/api/'. web.env sets NEXT_PUBLIC_API_URL=/api/v1 so the
    # browser calls back through this same vhost (no CORS, no hardcoded host).

    # Swagger API documentation
    location /swagger/ {
        proxy_pass http://127.0.0.1:8080/swagger/;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }

    # API backend (Go)
    location /api/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;

        # Long-running operations (VPC deletion, cluster creation, ...)
        proxy_connect_timeout 10s;
        proxy_send_timeout 360s;
        proxy_read_timeout 360s;
    }

    # Health check endpoint (direct, no auth)
    location /health {
        proxy_pass http://127.0.0.1:8080/health;
        access_log off;
    }

    location /ready {
        proxy_pass http://127.0.0.1:8080/ready;
        access_log off;
    }

    # Version endpoint
    location /version {
        proxy_pass http://127.0.0.1:8080/version;
        access_log off;
    }

    # Next.js frontend
    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;

        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
    }

    # Static files (Next.js)
    location /_next/static {
        proxy_pass http://127.0.0.1:3000;
        proxy_cache_valid 200 60m;
        add_header Cache-Control "public, immutable";
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
scp -i "$SSH_KEY" deploy/systemd/ocpctl-web.service $SSH_USER@$DEV_SERVER_IP:/tmp/

ssh -i "$SSH_KEY" $SSH_USER@$DEV_SERVER_IP 'bash -s' << 'ENDSSH'
set -e

# Install systemd service files
sudo install -m 644 /tmp/ocpctl-api.service /etc/systemd/system/
sudo install -m 644 /tmp/ocpctl-worker.service /etc/systemd/system/
sudo install -m 644 /tmp/ocpctl-web.service /etc/systemd/system/
sudo rm /tmp/ocpctl-api.service /tmp/ocpctl-worker.service /tmp/ocpctl-web.service

# Reload systemd
sudo systemctl daemon-reload

# Enable at boot. The units are started by deploy-env.sh/deploy-web.sh, which
# only ever calls `systemctl start` — without this the whole stack stays down
# after a reboot.
sudo systemctl enable ocpctl-api ocpctl-worker ocpctl-web

echo "✓ systemd service files installed and enabled"
ENDSSH

echo -e "${GREEN}✓ systemd services configured${NC}"
echo ""

echo -e "${YELLOW}Step 6: Setting up certbot auto-renewal...${NC}"
ssh -i "$SSH_KEY" $SSH_USER@$DEV_SERVER_IP "bash -s" << ENDSSH
set -e

# Create renewal hook to reload nginx
sudo tee /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh << 'EOF'
#!/bin/bash
systemctl reload nginx
EOF

sudo chmod +x /etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh

# The cert above was issued with --standalone while nginx was stopped. nginx now
# owns port 80, so leaving authenticator=standalone makes every future renewal
# fail with "Could not bind TCP port 80". Switch renewal to the nginx
# authenticator, which validates through the running server with no downtime.
sudo apt-get install -y python3-certbot-nginx
RENEWAL_CONF=/etc/letsencrypt/renewal/$DOMAIN.conf
if sudo grep -q '^authenticator = standalone' "\$RENEWAL_CONF"; then
  sudo sed -i 's|^authenticator = standalone|authenticator = nginx\ninstaller = nginx|' "\$RENEWAL_CONF"
  echo "✓ Switched renewal authenticator to nginx"
fi

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
echo "5. Deploy services (API + worker, then the web frontend):"
echo "   ./scripts/deploy-env.sh dev"
echo "   ./scripts/deploy-web.sh dev"
echo ""
echo "6. Access dev environment:"
echo "   https://$DOMAIN"
echo ""
