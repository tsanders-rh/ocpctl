# OCPCTL Deployment Files

This directory contains deployment configurations and scripts for running ocpctl on AWS EC2.

## Files

### Configuration
- **`config/api.env.template`** - API server environment variables template
- **`config/worker.env.template`** - Worker environment variables template

### Systemd Services
- **`systemd/ocpctl-api.service`** - API server systemd unit file
- **`systemd/ocpctl-worker.service`** - Worker and janitor systemd unit file

### Scripts
- **`setup.sh`** - Initial server setup script (run once on fresh EC2 instance)

### Documentation
- **`QUICKSTART.md`** - Quick deployment guide (15 minutes)
- **`DEPLOYMENT.md`** - Comprehensive deployment documentation

## Quick Start

For a small AWS EC2 deployment:

1. **Read the quick start guide:**
   ```bash
   cat QUICKSTART.md
   ```

2. **Launch EC2 instance and setup:**
   ```bash
   # On EC2 instance:
   sudo bash setup.sh
   ```

3. **Deploy from your local machine:**
   ```bash
   export DEPLOY_HOST=ubuntu@your-ec2-ip
   make build-linux
   make deploy
   ```

4. **Configure and start services (on EC2):**
   ```bash
   # Create /etc/ocpctl/api.env and worker.env
   # See config/*.env.template for examples

   sudo make install-services
   sudo systemctl start ocpctl-api ocpctl-worker
   ```

## Directory Structure on Server

After deployment, the EC2 instance will have:

```
/opt/ocpctl/
├── bin/
│   ├── ocpctl-api
│   └── ocpctl-worker
└── profiles/
    └── (profile definitions)

/etc/ocpctl/
├── api.env
└── worker.env

/var/lib/ocpctl/
└── clusters/
    └── (cluster work directories)

/var/log/ocpctl/
└── (application logs, if configured)

/etc/systemd/system/
├── ocpctl-api.service
└── ocpctl-worker.service
```

## Environment Variables

### Required for API

- `DATABASE_URL` - PostgreSQL connection string
- `PORT` - API server port (default: 8080)
- `PROFILES_DIR` - Path to profile definitions

### Required for Worker

- `DATABASE_URL` - PostgreSQL connection string
- `WORKER_WORK_DIR` - Working directory for clusters
- `OPENSHIFT_PULL_SECRET` - OpenShift pull secret JSON
- `OPENSHIFT_INSTALL_BINARY` - Path to openshift-install binary
- `AWS_REGION` - AWS region for cluster deployment

See `config/*.env.template` for complete list.

## Makefile Targets

From your **local machine**:
- `make build-linux` - Build binaries for Linux
- `make deploy` - Deploy binaries and profiles to EC2
- `make deploy-binaries` - Deploy only binaries
- `make deploy-profiles` - Deploy only profiles

On the **EC2 instance**:
- `make install-services` - Install systemd services
- `make start` - Start services
- `make stop` - Stop services
- `make restart` - Restart services
- `make status` - Check service status
- `make logs` - View all logs
- `make logs-api` - View API logs
- `make logs-worker` - View worker logs

## Security Notes

1. **Environment Files**: Mode 600, owned by ocpctl user
2. **IAM Role**: Attach EC2 instance role with required AWS permissions
3. **Security Group**: Restrict API access (don't expose to 0.0.0.0/0)
4. **Database**: Use strong password, enable SSL in production
5. **Pull Secret**: Stored in environment file, mode 600

## Monitoring

Check service status:
```bash
sudo systemctl status ocpctl-api ocpctl-worker
```

View logs:
```bash
sudo journalctl -u ocpctl-api -u ocpctl-worker -f
```

Check clusters:
```bash
curl http://localhost:8080/api/v1/clusters | jq
```

## Support

For issues or questions:
- See [DEPLOYMENT.md](DEPLOYMENT.md) for detailed troubleshooting
- Check logs: `sudo journalctl -u ocpctl-worker -n 100`
- Review database: `psql ocpctl -c "SELECT * FROM jobs WHERE status='FAILED' LIMIT 5;"`

## Architecture

```
Developer Machine                  AWS EC2 Instance
┌────────────────┐                ┌──────────────────────────┐
│                │                │                          │
│  make deploy   │───────────────▶│  /opt/ocpctl/bin/        │
│                │   (SCP/rsync)  │  - ocpctl-api            │
│                │                │  - ocpctl-worker         │
└────────────────┘                │                          │
                                  │  systemd services:       │
                                  │  - ocpctl-api.service    │
                                  │  - ocpctl-worker.service │
                                  │                          │
                                  │  PostgreSQL (local/RDS)  │
                                  │                          │
                                  │  /var/lib/ocpctl/        │
                                  │  clusters/               │
                                  └──────────────────────────┘
                                           │
                                           ▼
                                  OpenShift Clusters (AWS)
```
