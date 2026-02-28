#!/bin/bash
#
# Fedora Brix Setup Script for ocpctl
#
# This script automates the complete setup of ocpctl on a Fedora system.
# Designed for headless Brix boxes but works on any Fedora installation.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/tsanders-rh/ocpctl/main/scripts/setup-fedora-brix.sh | bash
#   OR
#   ./scripts/setup-fedora-brix.sh
#

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
OCPCTL_USER=${OCPCTL_USER:-ocpctl}
OCPCTL_DIR=${OCPCTL_DIR:-/opt/ocpctl}
REPO_URL="${REPO_URL:-git@github.com:tsanders-rh/ocpctl.git}"  # Use SSH by default
DB_NAME="ocpctl"
DB_USER="ocpctl"
DB_PASSWORD=$(openssl rand -base64 32 | tr -d "=+/" | cut -c1-25)

# Functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        log_error "This script must be run as root (use sudo)"
        exit 1
    fi
}

check_fedora() {
    if [[ ! -f /etc/fedora-release ]]; then
        log_error "This script is designed for Fedora. Detected: $(cat /etc/os-release | grep PRETTY_NAME)"
        exit 1
    fi
    log_info "Detected: $(cat /etc/fedora-release)"
}

install_prerequisites() {
    log_info "Installing system prerequisites..."

    dnf update -y

    # Install build tools and utilities
    dnf install -y \
        git \
        wget \
        curl \
        tar \
        make \
        gcc \
        gcc-c++ \
        openssl \
        openssl-devel \
        vim \
        htop \
        tmux \
        firewalld \
        nginx

    log_success "System prerequisites installed"
}

install_go() {
    log_info "Installing Go 1.21..."

    GO_VERSION="1.21.6"
    GO_ARCH="linux-amd64"
    GO_TARBALL="go${GO_VERSION}.${GO_ARCH}.tar.gz"

    # Remove old installation
    rm -rf /usr/local/go

    # Download and install
    cd /tmp
    wget -q "https://golang.org/dl/${GO_TARBALL}"
    tar -C /usr/local -xzf "${GO_TARBALL}"
    rm "${GO_TARBALL}"

    # Add to PATH
    cat > /etc/profile.d/go.sh << 'EOF'
export PATH=$PATH:/usr/local/go/bin
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin
EOF

    source /etc/profile.d/go.sh

    if /usr/local/go/bin/go version &> /dev/null; then
        log_success "Go installed: $(/usr/local/go/bin/go version)"
    else
        log_error "Go installation failed"
        exit 1
    fi
}

install_nodejs() {
    log_info "Installing Node.js 20..."

    # Install Node.js 20 from NodeSource
    curl -fsSL https://rpm.nodesource.com/setup_20.x | bash -
    dnf install -y nodejs

    if node --version &> /dev/null; then
        log_success "Node.js installed: $(node --version)"
        log_success "npm installed: $(npm --version)"
    else
        log_error "Node.js installation failed"
        exit 1
    fi
}

install_postgresql() {
    log_info "Installing PostgreSQL 14..."

    dnf install -y postgresql-server postgresql-contrib

    # Initialize database cluster
    if [[ ! -f /var/lib/pgsql/data/PG_VERSION ]]; then
        postgresql-setup --initdb
    fi

    # Enable PostgreSQL
    systemctl enable postgresql || true

    # Start PostgreSQL if not running
    if ! systemctl is-active --quiet postgresql; then
        systemctl start postgresql
        sleep 3
    fi

    if systemctl is-active --quiet postgresql; then
        log_success "PostgreSQL installed and running"
    else
        log_error "PostgreSQL failed to start"
        exit 1
    fi
}

configure_postgresql() {
    log_info "Configuring PostgreSQL..."

    # Create database user
    sudo -u postgres psql -c "CREATE USER ${DB_USER} WITH PASSWORD '${DB_PASSWORD}';" 2>/dev/null || true

    # Create database
    sudo -u postgres psql -c "CREATE DATABASE ${DB_NAME} OWNER ${DB_USER};" 2>/dev/null || true

    # Grant privileges
    sudo -u postgres psql -c "GRANT ALL PRIVILEGES ON DATABASE ${DB_NAME} TO ${DB_USER};"

    # Update pg_hba.conf to allow password authentication
    PG_HBA="/var/lib/pgsql/data/pg_hba.conf"
    if ! grep -q "host.*${DB_NAME}.*${DB_USER}" "${PG_HBA}"; then
        echo "host    ${DB_NAME}    ${DB_USER}    127.0.0.1/32    scram-sha-256" >> "${PG_HBA}"
        systemctl restart postgresql
    fi

    log_success "PostgreSQL configured with database: ${DB_NAME}"
}

create_user() {
    log_info "Creating ocpctl user..."

    if id "${OCPCTL_USER}" &>/dev/null; then
        log_warn "User ${OCPCTL_USER} already exists"
    else
        useradd -r -m -d /home/${OCPCTL_USER} -s /bin/bash ${OCPCTL_USER}
        log_success "User ${OCPCTL_USER} created"
    fi
}

clone_repository() {
    log_info "Cloning ocpctl repository..."

    # Create directory and set ownership
    mkdir -p "${OCPCTL_DIR}"
    chown ${OCPCTL_USER}:${OCPCTL_USER} "${OCPCTL_DIR}"

    # Clone if not exists
    if [[ -d "${OCPCTL_DIR}/.git" ]]; then
        log_warn "Repository already cloned, pulling latest..."
        cd "${OCPCTL_DIR}"
        sudo -u ${OCPCTL_USER} git pull
    else
        # Attempt to clone
        if ! sudo -u ${OCPCTL_USER} git clone "${REPO_URL}" "${OCPCTL_DIR}" 2>&1; then
            log_error "Failed to clone repository from ${REPO_URL}"
            echo ""
            log_info "If using SSH (git@github.com), ensure SSH keys are configured:"
            echo "  1. Generate key: ssh-keygen -t ed25519 -C \"your-email@example.com\""
            echo "  2. Display key: cat ~/.ssh/id_ed25519.pub"
            echo "  3. Add to GitHub: https://github.com/settings/keys"
            echo ""
            log_info "Alternatively, use HTTPS with Personal Access Token:"
            echo "  REPO_URL=\"https://github.com/tsanders-rh/ocpctl.git\" ./setup-fedora-brix.sh"
            echo ""
            exit 1
        fi
    fi

    cd "${OCPCTL_DIR}"
    log_success "Repository cloned to ${OCPCTL_DIR}"
}

setup_environment() {
    log_info "Setting up environment files..."

    # Backend .env
    cat > "${OCPCTL_DIR}/.env" << EOF
DATABASE_URL=postgresql://${DB_USER}:${DB_PASSWORD}@localhost:5432/${DB_NAME}?sslmode=disable
JWT_SECRET=$(openssl rand -base64 48)
JWT_ACCESS_TTL=15m
JWT_REFRESH_TTL=168h
PORT=8080
ENVIRONMENT=production
AWS_REGION=us-east-1
ENABLE_IAM_AUTH=false
ALLOWED_ORIGINS=http://localhost:3000,http://localhost:8080
ENABLE_AUTH=true
LOG_LEVEL=info
WORK_DIR=/var/lib/ocpctl/workdir
WORKER_CONCURRENCY=3
EOF

    # Frontend .env.local
    mkdir -p "${OCPCTL_DIR}/web"
    cat > "${OCPCTL_DIR}/web/.env.local" << EOF
NEXT_PUBLIC_API_URL=http://localhost:8080/api/v1
NEXT_PUBLIC_AUTH_MODE=jwt
NEXT_PUBLIC_AWS_REGION=us-east-1
NODE_ENV=production
EOF

    # Create work directory
    mkdir -p /var/lib/ocpctl/workdir
    chown -R ${OCPCTL_USER}:${OCPCTL_USER} /var/lib/ocpctl

    # Set ownership
    chown -R ${OCPCTL_USER}:${OCPCTL_USER} "${OCPCTL_DIR}"

    log_success "Environment files created"
}

build_application() {
    log_info "Building ocpctl application..."

    cd "${OCPCTL_DIR}"

    # Install Go dependencies
    sudo -u ${OCPCTL_USER} /usr/local/go/bin/go mod download

    # Install goose for migrations
    sudo -u ${OCPCTL_USER} /usr/local/go/bin/go install github.com/pressly/goose/v3/cmd/goose@latest

    # Build backend binaries
    sudo -u ${OCPCTL_USER} make build

    # Install frontend dependencies
    cd "${OCPCTL_DIR}/web"
    sudo -u ${OCPCTL_USER} npm install

    # Build frontend
    sudo -u ${OCPCTL_USER} npm run build

    cd "${OCPCTL_DIR}"
    log_success "Application built successfully"
}

setup_database() {
    log_info "Setting up database..."

    cd "${OCPCTL_DIR}"

    # Run migrations
    sudo -u ${OCPCTL_USER} /home/${OCPCTL_USER}/go/bin/goose -dir internal/store/migrations postgres "postgresql://${DB_USER}:${DB_PASSWORD}@localhost:5432/${DB_NAME}?sslmode=disable" up

    log_success "Database migrations completed"
}

create_systemd_services() {
    log_info "Creating systemd services..."

    # API Service
    cat > /etc/systemd/system/ocpctl-api.service << EOF
[Unit]
Description=ocpctl API Server
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
User=${OCPCTL_USER}
WorkingDirectory=${OCPCTL_DIR}
EnvironmentFile=${OCPCTL_DIR}/.env
ExecStart=${OCPCTL_DIR}/bin/ocpctl-api
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/ocpctl

[Install]
WantedBy=multi-user.target
EOF

    # Worker Service
    cat > /etc/systemd/system/ocpctl-worker.service << EOF
[Unit]
Description=ocpctl Worker Service
After=network.target postgresql.service ocpctl-api.service
Wants=postgresql.service

[Service]
Type=simple
User=${OCPCTL_USER}
WorkingDirectory=${OCPCTL_DIR}
EnvironmentFile=${OCPCTL_DIR}/.env
ExecStart=${OCPCTL_DIR}/bin/ocpctl-worker
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/ocpctl

[Install]
WantedBy=multi-user.target
EOF

    # Janitor Service
    cat > /etc/systemd/system/ocpctl-janitor.service << EOF
[Unit]
Description=ocpctl Janitor Service
After=network.target postgresql.service ocpctl-api.service
Wants=postgresql.service

[Service]
Type=simple
User=${OCPCTL_USER}
WorkingDirectory=${OCPCTL_DIR}
EnvironmentFile=${OCPCTL_DIR}/.env
ExecStart=${OCPCTL_DIR}/bin/ocpctl-janitor
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/ocpctl

[Install]
WantedBy=multi-user.target
EOF

    # Web Service
    cat > /etc/systemd/system/ocpctl-web.service << EOF
[Unit]
Description=ocpctl Web Frontend
After=network.target ocpctl-api.service

[Service]
Type=simple
User=${OCPCTL_USER}
WorkingDirectory=${OCPCTL_DIR}/web
EnvironmentFile=${OCPCTL_DIR}/web/.env.local
Environment=NODE_ENV=production
ExecStart=/usr/bin/npm start
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true

[Install]
WantedBy=multi-user.target
EOF

    # Reload systemd
    systemctl daemon-reload

    log_success "Systemd services created"
}

configure_nginx() {
    log_info "Configuring nginx..."

    cat > /etc/nginx/conf.d/ocpctl.conf << 'EOF'
server {
    listen 80;
    server_name _;

    client_max_body_size 10M;

    # API backend
    location /api/ {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support (for future use)
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }

    # Health checks
    location /health {
        proxy_pass http://localhost:8080;
        access_log off;
    }

    # Next.js frontend
    location / {
        proxy_pass http://localhost:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # WebSocket support for Next.js dev mode
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }
}
EOF

    # Test nginx configuration
    nginx -t

    # Enable and restart nginx
    systemctl enable nginx || true
    systemctl restart nginx

    log_success "Nginx configured"
}

configure_firewall() {
    log_info "Configuring firewall..."

    # Enable firewalld
    systemctl enable firewalld || true

    # Start firewalld if not running
    if ! systemctl is-active --quiet firewalld; then
        systemctl start firewalld
    fi

    # Allow HTTP
    firewall-cmd --permanent --add-service=http

    # Allow PostgreSQL (for remote access if needed)
    # firewall-cmd --permanent --add-service=postgresql

    # Reload firewall
    firewall-cmd --reload

    log_success "Firewall configured"
}

start_services() {
    log_info "Starting ocpctl services..."

    # Enable all services
    systemctl enable ocpctl-api
    systemctl enable ocpctl-worker
    systemctl enable ocpctl-janitor
    systemctl enable ocpctl-web

    # Start API first
    systemctl start ocpctl-api
    sleep 3

    # Start worker and janitor
    systemctl start ocpctl-worker
    systemctl start ocpctl-janitor

    # Start web
    systemctl start ocpctl-web
    sleep 3

    log_success "All services started"
}

verify_installation() {
    log_info "Verifying installation..."

    # Check service status
    for service in ocpctl-api ocpctl-worker ocpctl-janitor ocpctl-web nginx; do
        if systemctl is-active --quiet ${service}; then
            log_success "${service} is running"
        else
            log_error "${service} is not running"
        fi
    done

    # Check API health
    sleep 2
    if curl -f http://localhost:8080/health &> /dev/null; then
        log_success "API health check passed"
    else
        log_warn "API health check failed (may need more time to start)"
    fi

    # Check web frontend
    if curl -f http://localhost:3000 &> /dev/null; then
        log_success "Web frontend is responding"
    else
        log_warn "Web frontend not responding (may need more time to start)"
    fi
}

print_summary() {
    echo ""
    echo "================================================================"
    log_success "ocpctl installation completed!"
    echo "================================================================"
    echo ""
    echo "📦 Installation Details:"
    echo "   - Install directory: ${OCPCTL_DIR}"
    echo "   - User: ${OCPCTL_USER}"
    echo "   - Database: ${DB_NAME}"
    echo "   - Database user: ${DB_USER}"
    echo "   - Database password: ${DB_PASSWORD}"
    echo ""
    echo "🌐 Access URLs:"
    echo "   - Web UI: http://$(hostname -I | awk '{print $1}')"
    echo "   - API: http://$(hostname -I | awk '{print $1}')/api/v1"
    echo ""
    echo "🔑 Default Login:"
    echo "   - Email: admin@localhost"
    echo "   - Password: changeme"
    echo "   ⚠️  Change this password after first login!"
    echo ""
    echo "📊 Service Status:"
    systemctl status ocpctl-api --no-pager -l | head -3
    systemctl status ocpctl-worker --no-pager -l | head -3
    systemctl status ocpctl-web --no-pager -l | head -3
    echo ""
    echo "📝 Useful Commands:"
    echo "   - View API logs:      journalctl -u ocpctl-api -f"
    echo "   - View worker logs:   journalctl -u ocpctl-worker -f"
    echo "   - View web logs:      journalctl -u ocpctl-web -f"
    echo "   - Restart services:   systemctl restart ocpctl-api ocpctl-worker ocpctl-web"
    echo "   - View all services:  systemctl status 'ocpctl-*'"
    echo "   - Database access:    psql -U ${DB_USER} -d ${DB_NAME}"
    echo ""
    echo "🔧 Next Steps:"
    echo "   1. Access the web UI in your browser"
    echo "   2. Login with default credentials"
    echo "   3. Change the admin password"
    echo "   4. (Optional) Configure AWS credentials for cluster provisioning"
    echo "   5. (Optional) Install openshift-install binary"
    echo ""
    echo "📚 Documentation:"
    echo "   - Development guide: ${OCPCTL_DIR}/DEVELOPMENT.md"
    echo "   - OpenShift setup: ${OCPCTL_DIR}/docs/OPENSHIFT_INSTALL_SETUP.md"
    echo "   - Testing guide: ${OCPCTL_DIR}/docs/TESTING_WITHOUT_OPENSHIFT.md"
    echo ""
    echo "================================================================"
}

# Main execution
main() {
    echo ""
    echo "================================================================"
    echo "  ocpctl Fedora Brix Setup Script"
    echo "================================================================"
    echo ""

    check_root
    check_fedora

    log_info "Starting ocpctl installation..."
    echo ""

    install_prerequisites
    install_go
    install_nodejs
    install_postgresql
    configure_postgresql
    create_user
    clone_repository
    setup_environment
    build_application
    setup_database
    create_systemd_services
    configure_nginx
    configure_firewall
    start_services
    verify_installation

    echo ""
    print_summary
}

# Run main function
main "$@"
