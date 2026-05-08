# OCPCTL Deployment Verification Checklist

**Purpose:** Step-by-step verification checklist to ensure successful ocpctl deployment to AWS.

**Estimated Time:** 15-20 minutes for complete verification

**When to Use:**
- After initial deployment
- After major updates or migrations
- Before deploying to production
- When troubleshooting deployment issues

---

## Pre-Deployment Checklist

Complete these steps **before** starting deployment:

### ☐ 1. AWS Account Prerequisites

- [ ] **AWS Account Access**
  ```bash
  aws sts get-caller-identity
  # Should return your AWS account ID, user ARN
  ```

- [ ] **Required AWS Services Available**
  ```bash
  # Check EC2 is accessible
  aws ec2 describe-regions --region us-east-1

  # Check RDS is accessible (if using RDS)
  aws rds describe-db-instances --region us-east-1

  # Check Route53 is accessible
  aws route53 list-hosted-zones
  ```

- [ ] **Service Quotas Sufficient**
  ```bash
  # Check EC2 instance limit
  aws service-quotas get-service-quota \
    --service-code ec2 \
    --quota-code L-1216C47A \
    --region us-east-1
  # Should show: "Value": 20 or higher for Running On-Demand Standard instances

  # Check VPC limit
  aws service-quotas get-service-quota \
    --service-code vpc \
    --quota-code L-F678F1CE \
    --region us-east-1
  # Should show: "Value": 5 or higher
  ```

### ☐ 2. DNS/Route53 Prerequisites

- [ ] **Route53 Hosted Zone Exists**
  ```bash
  aws route53 list-hosted-zones \
    --query 'HostedZones[?Name==`your-domain.com.`].{ID:Id,Name:Name}'

  # Save hosted zone ID for later
  export HOSTED_ZONE_ID=Z0123456789ABCDEFGHIJ
  ```

- [ ] **DNS Domain Verified**
  ```bash
  # Verify nameservers are configured
  dig NS your-domain.com +short

  # Should return AWS Route53 nameservers like:
  # ns-123.awsdns-12.com.
  # ns-456.awsdns-34.net.
  ```

### ☐ 3. Local Machine Prerequisites

- [ ] **AWS CLI Installed and Configured**
  ```bash
  aws --version
  # Should return: aws-cli/2.x.x or higher

  aws configure list
  # Should show configured region and credentials
  ```

- [ ] **SSH Key Pair Created**
  ```bash
  # Check if key exists
  ls -l ~/.ssh/ocpctl-production-key.pem

  # Verify key is in AWS
  aws ec2 describe-key-pairs --key-names ocpctl-production-key
  ```

- [ ] **Git Installed**
  ```bash
  git --version
  # Should return: git version 2.x.x
  ```

- [ ] **Required Build Tools (for local builds)**
  ```bash
  go version
  # Should return: go version go1.21.x or higher

  node --version
  # Should return: v18.x.x or higher

  npm --version
  # Should return: 9.x.x or higher
  ```

### ☐ 4. Secrets and Credentials

- [ ] **OpenShift Pull Secret Available**
  ```bash
  # Verify pull secret file exists
  test -f ~/pull-secret.json && echo "Pull secret found" || echo "Pull secret missing"

  # Validate JSON format
  jq . ~/pull-secret.json > /dev/null 2>&1 && echo "Valid JSON" || echo "Invalid JSON"
  ```

- [ ] **Database Password Generated (if using Parameter Store)**
  ```bash
  # Verify parameter exists
  aws ssm get-parameter \
    --name "/ocpctl/database/password" \
    --query 'Parameter.Name'

  # Should return: "/ocpctl/database/password"
  ```

- [ ] **OCM Token Available (if deploying ROSA clusters)**
  ```bash
  # Get token from: https://console.redhat.com/openshift/token/rosa
  # Verify token is valid (should be ~400 characters)
  echo $OCM_TOKEN | wc -c
  # Should return: ~400
  ```

---

## Infrastructure Setup Verification

Complete these checks after creating AWS infrastructure:

### ☐ 5. EC2 Instance Setup

- [ ] **Instance Running**
  ```bash
  export INSTANCE_ID=i-0123456789abcdef0

  aws ec2 describe-instances \
    --instance-ids $INSTANCE_ID \
    --query 'Reservations[0].Instances[0].State.Name'

  # Should return: "running"
  ```

- [ ] **Instance Has Public IP**
  ```bash
  export EC2_IP=$(aws ec2 describe-instances \
    --instance-ids $INSTANCE_ID \
    --query 'Reservations[0].Instances[0].PublicIpAddress' \
    --output text)

  echo "EC2 IP: $EC2_IP"
  # Should show valid public IP
  ```

- [ ] **Instance Tagged Correctly (NOT as 'test')**
  ```bash
  aws ec2 describe-instances \
    --instance-ids $INSTANCE_ID \
    --query 'Reservations[0].Instances[0].Tags[?Key==`Name`].Value' \
    --output text

  # Should return: "ocpctl-production" NOT "ocpctl-test"
  # (Prevents automated Friday cleanup)
  ```

- [ ] **SSH Access Works**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem -o ConnectTimeout=5 ubuntu@$EC2_IP echo "SSH OK"

  # Should return: "SSH OK"
  ```

- [ ] **IAM Instance Profile Attached**
  ```bash
  aws ec2 describe-instances \
    --instance-ids $INSTANCE_ID \
    --query 'Reservations[0].Instances[0].IamInstanceProfile.Arn'

  # Should return ARN like:
  # arn:aws:iam::123456789012:instance-profile/ocpctl-worker-role
  ```

### ☐ 6. Security Group Configuration

- [ ] **Security Group Allows HTTP/HTTPS**
  ```bash
  export SG_ID=$(aws ec2 describe-instances \
    --instance-ids $INSTANCE_ID \
    --query 'Reservations[0].Instances[0].SecurityGroups[0].GroupId' \
    --output text)

  aws ec2 describe-security-groups \
    --group-ids $SG_ID \
    --query 'SecurityGroups[0].IpPermissions[?FromPort==`80`]'

  # Should show HTTP ingress rule

  aws ec2 describe-security-groups \
    --group-ids $SG_ID \
    --query 'SecurityGroups[0].IpPermissions[?FromPort==`443`]'

  # Should show HTTPS ingress rule
  ```

- [ ] **Security Group Allows SSH (Your IP Only)**
  ```bash
  export MY_IP=$(curl -s https://checkip.amazonaws.com)

  aws ec2 describe-security-groups \
    --group-ids $SG_ID \
    --query "SecurityGroups[0].IpPermissions[?FromPort==\`22\`].IpRanges[?CidrIp==\`${MY_IP}/32\`]"

  # Should show your IP address
  ```

### ☐ 7. IAM Permissions Verification

- [ ] **Worker Role Has Required Policy**
  ```bash
  aws iam list-attached-role-policies \
    --role-name ocpctl-worker-role \
    --query 'AttachedPolicies[?contains(PolicyName, `ocpctl`)].PolicyName'

  # Should return: "ocpctl-worker-full" or similar
  ```

- [ ] **Role Can Be Assumed by EC2**
  ```bash
  # SSH to instance and test
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "aws sts get-caller-identity --query 'Arn' --output text"

  # Should return:
  # arn:aws:sts::123456789012:assumed-role/ocpctl-worker-role/i-0123456789abcdef0
  ```

- [ ] **Parameter Store Access Works**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "aws ssm get-parameter --name '/ocpctl/database/password' --query 'Parameter.Name' --output text"

  # Should return: "/ocpctl/database/password"
  ```

### ☐ 8. Database Setup Verification

**For PostgreSQL on EC2:**

- [ ] **PostgreSQL Running**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo systemctl is-active postgresql"

  # Should return: "active"
  ```

- [ ] **Database Exists**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo -u postgres psql -l | grep ocpctl"

  # Should show ocpctl database
  ```

- [ ] **Database Connection Works**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    'DB_PASSWORD=$(aws ssm get-parameter --name /ocpctl/database/password --with-decryption --query Parameter.Value --output text); psql "postgres://ocpctl_user:${DB_PASSWORD}@localhost:5432/ocpctl" -c "\dt"'

  # Should connect and list tables (may be empty before migrations)
  ```

**For RDS PostgreSQL:**

- [ ] **RDS Instance Available**
  ```bash
  aws rds describe-db-instances \
    --db-instance-identifier ocpctl-db \
    --query 'DBInstances[0].DBInstanceStatus'

  # Should return: "available"
  ```

- [ ] **RDS Endpoint Accessible**
  ```bash
  export DB_ENDPOINT=$(aws rds describe-db-instances \
    --db-instance-identifier ocpctl-db \
    --query 'DBInstances[0].Endpoint.Address' \
    --output text)

  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "DB_PASSWORD=\$(aws ssm get-parameter --name /ocpctl/database/password --with-decryption --query Parameter.Value --output text); psql \"postgres://ocpctl_user:\${DB_PASSWORD}@${DB_ENDPOINT}:5432/ocpctl\" -c 'SELECT version();'"

  # Should return PostgreSQL version
  ```

---

## Service Deployment Verification

Complete these checks after deploying binaries and configuring services:

### ☐ 9. Binary Deployment

- [ ] **Binaries Exist with Correct Permissions**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "ls -lh /opt/ocpctl/bin/"

  # Should show:
  # -rwxr-xr-x ocpctl ocpctl-api
  # -rwxr-xr-x ocpctl ocpctl-worker
  ```

- [ ] **Binaries Are Executable**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "/opt/ocpctl/bin/ocpctl-api --version"

  # Should return version like: v0.20260508.abc1234

  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "/opt/ocpctl/bin/ocpctl-worker --version"

  # Should return version
  ```

- [ ] **Profiles Deployed**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "ls -1 /opt/ocpctl/profiles/ | wc -l"

  # Should return: 30+ (number of profile files)
  ```

- [ ] **Addons Deployed**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "ls -1 /opt/ocpctl/addons/ 2>/dev/null | wc -l"

  # Should return: 4+ (cnv, mta, mtc, oadp)
  ```

### ☐ 10. Environment Configuration

- [ ] **API Environment File Exists**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo test -f /etc/ocpctl/api.env && echo 'Found' || echo 'Missing'"

  # Should return: "Found"
  ```

- [ ] **API Environment File Permissions**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo stat -c '%a %U:%G' /etc/ocpctl/api.env"

  # Should return: "600 root:root" or "600 ocpctl:ocpctl"
  ```

- [ ] **Worker Environment File Exists**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo test -f /etc/ocpctl/worker.env && echo 'Found' || echo 'Missing'"

  # Should return: "Found"
  ```

- [ ] **Worker Has Pull Secret**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo grep 'OPENSHIFT_PULL_SECRET' /etc/ocpctl/worker.env | grep -q 'CHANGEME' && echo 'NOT SET' || echo 'SET'"

  # Should return: "SET"
  ```

- [ ] **Database URL Configured**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo grep 'DATABASE_URL' /etc/ocpctl/api.env | grep -v 'CHANGEME' | wc -l"

  # Should return: 1
  ```

### ☐ 11. Required CLI Tools Installed

- [ ] **openshift-install Available**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "openshift-install version"

  # Should return version like: 4.20.3
  ```

- [ ] **oc CLI Available**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "oc version --client"

  # Should return client version
  ```

- [ ] **ccoctl Available**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "ccoctl version"

  # Should return version or help text
  ```

- [ ] **AWS CLI Available**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "aws --version"

  # Should return: aws-cli/2.x.x
  ```

### ☐ 12. Systemd Services

- [ ] **Service Files Installed**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "ls -1 /etc/systemd/system/ocpctl-*.service"

  # Should show:
  # /etc/systemd/system/ocpctl-api.service
  # /etc/systemd/system/ocpctl-worker.service
  ```

- [ ] **Services Enabled**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "systemctl is-enabled ocpctl-api ocpctl-worker"

  # Should return: "enabled" for both
  ```

- [ ] **Services Running**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "systemctl is-active ocpctl-api ocpctl-worker"

  # Should return: "active" for both
  ```

- [ ] **No Service Failures in Logs**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo journalctl -u ocpctl-api --since '5 minutes ago' | grep -i error | wc -l"

  # Should return: 0 (no errors)

  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo journalctl -u ocpctl-worker --since '5 minutes ago' | grep -i error | wc -l"

  # Should return: 0 (no errors)
  ```

---

## Post-Deployment Verification

Complete these checks after all services are running:

### ☐ 13. Health Endpoints

- [ ] **API Health Check (Internal)**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "curl -s http://localhost:8080/health | jq '.status'"

  # Should return: "ok"
  ```

- [ ] **Worker Health Check (Internal)**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "curl -s http://localhost:8081/health | jq '.status'"

  # Should return: "ok"
  ```

- [ ] **Worker Ready Check**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "curl -s http://localhost:8081/ready | jq '.status'"

  # Should return: "ready"
  ```

### ☐ 14. Database Migrations

- [ ] **Migrations Completed**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo journalctl -u ocpctl-api --since '10 minutes ago' | grep -i migration | tail -5"

  # Should show migration completion messages
  ```

- [ ] **Tables Created**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    'DB_PASSWORD=$(aws ssm get-parameter --name /ocpctl/database/password --with-decryption --query Parameter.Value --output text); psql "postgres://ocpctl_user:${DB_PASSWORD}@localhost:5432/ocpctl" -c "\dt" | grep -E "clusters|jobs|users" | wc -l'

  # Should return: 3 or more (core tables exist)
  ```

- [ ] **Schema Version Recorded**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    'DB_PASSWORD=$(aws ssm get-parameter --name /ocpctl/database/password --with-decryption --query Parameter.Value --output text); psql "postgres://ocpctl_user:${DB_PASSWORD}@localhost:5432/ocpctl" -c "SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;"'

  # Should return latest migration version (e.g., 00041)
  ```

### ☐ 15. Nginx/Web Access (if configured)

- [ ] **Nginx Running**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "systemctl is-active nginx"

  # Should return: "active"
  ```

- [ ] **Nginx Configuration Valid**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo nginx -t"

  # Should return: "syntax is ok" and "test is successful"
  ```

- [ ] **HTTP Access Works**
  ```bash
  curl -s -o /dev/null -w "%{http_code}" http://$EC2_IP/health

  # Should return: 200
  ```

- [ ] **HTTPS Access Works (if SSL configured)**
  ```bash
  curl -s -o /dev/null -w "%{http_code}" https://ocpctl.your-domain.com/health

  # Should return: 200
  ```

### ☐ 16. API Endpoints

- [ ] **Version Endpoint**
  ```bash
  curl -s http://$EC2_IP/version | jq '.'

  # Should return version info with git commit
  ```

- [ ] **Profiles Endpoint**
  ```bash
  curl -s http://$EC2_IP/api/v1/profiles | jq 'length'

  # Should return: 30+ (number of profiles)
  ```

- [ ] **Addons Endpoint**
  ```bash
  curl -s http://$EC2_IP/api/v1/addons | jq 'length'

  # Should return: 4+ (number of addons)
  ```

### ☐ 17. Web Frontend (if deployed)

- [ ] **Web Service Running**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "systemctl is-active ocpctl-web"

  # Should return: "active"
  ```

- [ ] **Web Accessible**
  ```bash
  curl -s -o /dev/null -w "%{http_code}" http://$EC2_IP/

  # Should return: 200
  ```

- [ ] **Login Page Loads**
  ```bash
  curl -s http://$EC2_IP/ | grep -q "login" && echo "Login page found" || echo "Login page missing"

  # Should return: "Login page found"
  ```

---

## Security Verification

### ☐ 18. Security Hardening Checks

- [ ] **Environment Files Secured**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo find /etc/ocpctl -name '*.env' -exec stat -c '%a %n' {} \;"

  # All should return: "600 /etc/ocpctl/*.env"
  ```

- [ ] **No Secrets in Logs**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo journalctl -u ocpctl-api -u ocpctl-worker | grep -i 'password\|secret\|token' | grep -v 'REDACTED' | wc -l"

  # Should return: 0 (secrets should be redacted)
  ```

- [ ] **Database Password Not Default**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo grep 'CHANGEME' /etc/ocpctl/api.env | wc -l"

  # Should return: 0
  ```

- [ ] **JWT Secret Not Default**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo grep 'CHANGEME-generate' /etc/ocpctl/api.env | wc -l"

  # Should return: 0
  ```

- [ ] **Pull Secret File Removed from Home**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "test -f ~/pull-secret.json && echo 'SECURITY RISK' || echo 'OK'"

  # Should return: "OK"
  ```

---

## First Cluster Test

### ☐ 19. Test Cluster Creation (Optional but Recommended)

**Note:** This will provision a real OpenShift cluster (~30-45 minutes, costs ~$5-7).

- [ ] **Create Test Cluster**
  ```bash
  curl -X POST http://$EC2_IP/api/v1/clusters \
    -H "Content-Type: application/json" \
    -d '{
      "name": "verification-test",
      "platform": "aws",
      "cluster_type": "openshift",
      "version": "4.20.3",
      "profile": "aws-sno-ga",
      "region": "us-east-1",
      "base_domain": "your-domain.com",
      "owner": "admin",
      "team": "platform",
      "cost_center": "test",
      "ttl_hours": 4
    }' | jq '.id'

  # Should return cluster ID
  export CLUSTER_ID=<returned-id>
  ```

- [ ] **Cluster Status Changes to CREATING**
  ```bash
  curl -s http://$EC2_IP/api/v1/clusters/$CLUSTER_ID | jq '.status'

  # Should return: "CREATING" (after ~30 seconds)
  ```

- [ ] **Worker Picks Up Job**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo journalctl -u ocpctl-worker --since '1 minute ago' | grep -i 'processing job' | tail -1"

  # Should show job processing log
  ```

- [ ] **Work Directory Created**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo ls -d /var/lib/ocpctl/clusters/$CLUSTER_ID"

  # Should show directory path
  ```

- [ ] **Cluster Installation Progressing**
  ```bash
  # Wait 5-10 minutes, then check
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo tail -20 /var/lib/ocpctl/clusters/$CLUSTER_ID/.openshift_install.log"

  # Should show openshift-install progress
  ```

**IMPORTANT:** Remember to destroy the test cluster to avoid costs:
```bash
curl -X DELETE http://$EC2_IP/api/v1/clusters/$CLUSTER_ID
```

---

## Performance Baselines

### ☐ 20. Expected Performance Metrics

- [ ] **API Response Time**
  ```bash
  time curl -s http://$EC2_IP/health > /dev/null

  # Should complete in < 100ms
  ```

- [ ] **Database Query Performance**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    'DB_PASSWORD=$(aws ssm get-parameter --name /ocpctl/database/password --with-decryption --query Parameter.Value --output text); time psql "postgres://ocpctl_user:${DB_PASSWORD}@localhost:5432/ocpctl" -c "SELECT COUNT(*) FROM clusters;"'

  # Should complete in < 50ms
  ```

- [ ] **Memory Usage Reasonable**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "free -h"

  # Available memory should be > 2GB on t3.large
  ```

- [ ] **Disk Space Sufficient**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "df -h / | tail -1 | awk '{print \$5}'"

  # Usage should be < 50%
  ```

---

## Monitoring and Logging

### ☐ 21. Log Verification

- [ ] **API Logs Accessible**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo journalctl -u ocpctl-api -n 10"

  # Should show recent log entries
  ```

- [ ] **Worker Logs Accessible**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo journalctl -u ocpctl-worker -n 10"

  # Should show recent log entries (job polling)
  ```

- [ ] **Log Rotation Configured**
  ```bash
  ssh -i ~/.ssh/ocpctl-production-key.pem ubuntu@$EC2_IP \
    "sudo journalctl --disk-usage"

  # Should show reasonable disk usage (< 500MB)
  ```

---

## Troubleshooting Decision Tree

If any check fails, use this decision tree:

### Service Won't Start
1. Check environment file exists and has correct permissions
2. Verify DATABASE_URL is correct
3. Check for port conflicts: `sudo netstat -tlnp | grep 808`
4. Review logs: `sudo journalctl -u ocpctl-api -n 50`

### Database Connection Failed
1. Verify PostgreSQL is running: `systemctl status postgresql`
2. Test connection string manually
3. Check firewall rules (RDS security group)
4. Verify password in Parameter Store matches database

### Health Check Returns 500
1. Check database migrations completed
2. Verify profiles directory exists and is readable
3. Check API logs for specific error
4. Verify environment variables are set

### Cluster Creation Fails
1. Verify IAM role permissions
2. Check Route53 hosted zone exists
3. Verify pull secret is valid
4. Check worker logs for specific error

---

## Deployment Acceptance Criteria

✅ **Deployment is successful when:**

1. All services running: API, Worker, (Web optional)
2. Health endpoints return "ok"
3. Database migrations completed
4. Profiles and addons loaded
5. No errors in service logs (last 10 minutes)
6. IAM permissions verified
7. Security checks passed (no default passwords, secrets secured)
8. Test cluster can be created (or skipped if verified previously)

---

## Post-Deployment Next Steps

After successful verification:

1. **Set up monitoring**
   - Configure CloudWatch (optional)
   - Set up log aggregation
   - Create health check alarms

2. **Configure backups**
   - Database backup schedule
   - S3 lifecycle policies
   - Disaster recovery plan

3. **Document your deployment**
   - Save all environment variables (securely)
   - Document any deviations from standard deployment
   - Record instance IDs, IPs, and other infrastructure details

4. **Create runbook**
   - Common operations
   - Troubleshooting procedures
   - Emergency contacts

5. **Schedule maintenance windows**
   - Database password rotation (every 90 days)
   - System package updates
   - Security patches

---

## Checklist Summary

Print this quick reference:

```
PRE-DEPLOYMENT:
☐ AWS account access
☐ Route53 hosted zone
☐ SSH key pair
☐ Pull secret
☐ Database password in Parameter Store

INFRASTRUCTURE:
☐ EC2 instance running
☐ IAM role attached
☐ Security groups configured
☐ Database accessible

DEPLOYMENT:
☐ Binaries deployed
☐ Environment files configured
☐ CLI tools installed
☐ Services running

VERIFICATION:
☐ Health checks passing
☐ Database migrations completed
☐ API endpoints responding
☐ Security checks passed

OPTIONAL:
☐ Test cluster created
☐ Performance baseline met
☐ Monitoring configured
```

---

**Checklist Version:** 1.0
**Last Updated:** 2026-05-08
**Estimated Completion Time:** 15-20 minutes
