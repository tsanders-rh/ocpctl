# IAM Authentication Guide

This guide covers AWS IAM authentication for ocpctl, including the optional group membership restriction feature.

## Table of Contents

- [Overview](#overview)
- [How IAM Authentication Works](#how-iam-authentication-works)
- [Configuration](#configuration)
- [IAM Group Membership Restriction](#iam-group-membership-restriction)
- [Required IAM Permissions](#required-iam-permissions)
- [Testing and Verification](#testing-and-verification)
- [Troubleshooting](#troubleshooting)
- [Security Considerations](#security-considerations)

---

## Overview

ocpctl supports dual authentication methods:
- **JWT Authentication**: Traditional email/password login with JWT tokens
- **IAM Authentication**: AWS IAM users and roles authenticate using AWS SigV4 signatures

IAM authentication enables:
- Centralized access management through AWS IAM
- No separate password management
- Integration with existing AWS identity infrastructure
- Support for temporary credentials (assumed roles)
- Optional group-based access control

---

## How IAM Authentication Works

### Authentication Flow

1. **User makes request** with AWS SigV4 signed headers
2. **API validates credentials** by calling `sts:GetCallerIdentity`
3. **Extract principal ARN** from STS response (e.g., `arn:aws:iam::123456789012:user/alice`)
4. **Check group membership** (optional, if `IAM_ALLOWED_GROUP` configured)
5. **Lookup or auto-provision user**:
   - If user exists in `iam_principal_mappings` table → retrieve user
   - If new → auto-provision user with `USER` role
6. **Return user object** for authorization checks

### Auto-Provisioning

When an IAM user authenticates for the first time, ocpctl automatically:
- Creates a user record in the database
- Sets role to `USER` (can be changed by admin to `ADMIN` or `VIEWER`)
- Generates synthetic email: `{username}@aws-account-{account-id}`
- Creates IAM principal mapping: `ARN → user_id`
- Sets `active = true`

**Example:**
```
IAM User: arn:aws:iam::123456789012:user/alice
Auto-provisioned as:
  - Username: alice
  - Email: alice@aws-account-123456789012
  - Role: USER
  - Active: true
```

### Assumed Roles

Assumed roles (temporary credentials) are supported:
- ARN format: `arn:aws:sts::123456789012:assumed-role/MyRole/session-name`
- Automatically bypass group membership checks
- Ideal for service accounts, CI/CD pipelines, automation

---

## Configuration

### 1. Enable IAM Authentication

**File:** `/etc/ocpctl/api.env`

```bash
# Enable IAM authentication
ENABLE_IAM_AUTH=true

# IAM group membership (optional - see next section)
IAM_ALLOWED_GROUP=
```

### 2. Restart API Service

```bash
sudo systemctl restart ocpctl-api
```

### 3. Verify Configuration

```bash
# Check logs for IAM auth enabled
sudo journalctl -u ocpctl-api -n 50 | grep "IAM"

# Should show:
# "Auth enabled: true (JWT: true, IAM: true)"
```

---

## IAM Group Membership Restriction

**Optional feature** to restrict which IAM users can authenticate by requiring membership in a specific IAM group.

### Why Use This?

- Limit access to specific team members
- Centralize access control in AWS IAM
- Prevent unauthorized auto-provisioning
- Audit group membership via AWS CloudTrail

### Setup Steps

#### 1. Create IAM Group

```bash
aws iam create-group --group-name ocpctl-users
```

#### 2. Add Users to Group

```bash
aws iam add-user-to-group --user-name alice --group-name ocpctl-users
aws iam add-user-to-group --user-name bob --group-name ocpctl-users
```

#### 3. Grant API Server Permissions

The API server's IAM role needs permission to check group membership:

```bash
cat > /tmp/iam-group-check-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "iam:ListGroupsForUser"
      ],
      "Resource": "*"
    }
  ]
}
EOF

aws iam put-role-policy \
  --role-name ocpctl-api-role \
  --policy-name IAMGroupChecking \
  --policy-document file:///tmp/iam-group-check-policy.json
```

#### 4. Configure ocpctl

**File:** `/etc/ocpctl/api.env`

```bash
# Enable IAM authentication
ENABLE_IAM_AUTH=true

# Restrict to specific IAM group
IAM_ALLOWED_GROUP=ocpctl-users
```

#### 5. Restart API Service

```bash
sudo systemctl restart ocpctl-api
```

#### 6. Verify in Logs

```bash
sudo journalctl -u ocpctl-api -f | grep "IAM auth:"
```

You should see logs like:
- `"IAM auth: checking group membership for user=alice, required_group=ocpctl-users"`
- `"IAM auth: user=alice is member of group=ocpctl-users"`
- `"IAM auth: access denied - user=bob not in group=ocpctl-users"`

### Behavior

| Scenario | Result |
|----------|--------|
| `IAM_ALLOWED_GROUP` not set | All IAM users can authenticate |
| User in allowed group | Authentication succeeds (HTTP 200) |
| User NOT in allowed group | Authentication fails (HTTP 403 Forbidden) |
| Assumed role | Group check bypassed, authentication succeeds |
| Existing auto-provisioned user removed from group | Authentication fails on next login |

### Managing Group Membership

**Add user to group:**
```bash
aws iam add-user-to-group --user-name alice --group-name ocpctl-users
```

**Remove user from group:**
```bash
aws iam remove-user-from-group --user-name alice --group-name ocpctl-users
# User will be denied access on next login attempt
```

**List group members:**
```bash
aws iam get-group --group-name ocpctl-users
```

---

## Required IAM Permissions

### For IAM Users (Authenticating to ocpctl)

IAM users need minimal permissions to authenticate:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sts:GetCallerIdentity"
      ],
      "Resource": "*"
    }
  ]
}
```

### For API Server IAM Role

#### Basic IAM Authentication (no group check)

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sts:GetCallerIdentity"
      ],
      "Resource": "*"
    }
  ]
}
```

#### With Group Membership Check

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "sts:GetCallerIdentity",
        "iam:ListGroupsForUser"
      ],
      "Resource": "*"
    }
  ]
}
```

---

## Testing and Verification

### Test 1: Basic IAM Authentication (No Group Check)

```bash
# Don't set IAM_ALLOWED_GROUP (leave empty or unset)
# Restart API: sudo systemctl restart ocpctl-api

# Authenticate with AWS credentials
aws configure
curl -X GET http://localhost:8080/api/v1/auth/me \
  --aws-sigv4 "aws:amz:us-east-1:execute-api" \
  --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"

# Expected: HTTP 200 OK with user details
```

### Test 2: User in Allowed Group

```bash
# Create user and add to group
aws iam create-user --user-name test-user
aws iam create-access-key --user-name test-user
aws iam create-group --group-name ocpctl-users
aws iam add-user-to-group --user-name test-user --group-name ocpctl-users

# Configure ocpctl with group check
echo "IAM_ALLOWED_GROUP=ocpctl-users" | sudo tee -a /etc/ocpctl/api.env
sudo systemctl restart ocpctl-api

# Authenticate with test-user credentials
export AWS_ACCESS_KEY_ID=<test-user-access-key>
export AWS_SECRET_ACCESS_KEY=<test-user-secret-key>
curl -X GET http://localhost:8080/api/v1/auth/me \
  --aws-sigv4 "aws:amz:us-east-1:execute-api" \
  --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"

# Expected: HTTP 200 OK
# Logs show: "IAM auth: user=test-user is member of group=ocpctl-users"
```

### Test 3: User NOT in Allowed Group

```bash
# Remove user from group
aws iam remove-user-from-group --user-name test-user --group-name ocpctl-users

# Attempt authentication
curl -X GET http://localhost:8080/api/v1/auth/me \
  --aws-sigv4 "aws:amz:us-east-1:execute-api" \
  --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"

# Expected: HTTP 403 Forbidden
# Error: "access denied: user test-user is not a member of required group ocpctl-users"
# Logs show: "IAM auth: access denied - user=test-user not in group=ocpctl-users"
```

### Test 4: Assumed Role (Bypasses Group Check)

```bash
# Create role with trust policy
cat > /tmp/trust-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "AWS": "arn:aws:iam::123456789012:root"
      },
      "Action": "sts:AssumeRole"
    }
  ]
}
EOF

aws iam create-role \
  --role-name ocpctl-test-role \
  --assume-role-policy-document file:///tmp/trust-policy.json

# Assume the role
aws sts assume-role \
  --role-arn arn:aws:iam::123456789012:role/ocpctl-test-role \
  --role-session-name test-session

# Use temporary credentials from assume-role output
export AWS_ACCESS_KEY_ID=<temp-access-key>
export AWS_SECRET_ACCESS_KEY=<temp-secret-key>
export AWS_SESSION_TOKEN=<temp-session-token>

curl -X GET http://localhost:8080/api/v1/auth/me \
  --aws-sigv4 "aws:amz:us-east-1:execute-api" \
  --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY"

# Expected: HTTP 200 OK (group check skipped)
# Logs show: "IAM auth: skipping group check for assumed role"
```

### Test 5: Existing User Removed from Group

```bash
# User already exists in database (auto-provisioned earlier)
# Remove user from IAM group
aws iam remove-user-from-group --user-name alice --group-name ocpctl-users

# Attempt authentication
# Expected: HTTP 403 Forbidden
# This proves group check happens on EVERY login, not just provisioning
```

---

## Troubleshooting

### Issue: "IAM authentication not enabled"

**Symptom:**
```
HTTP 401 Unauthorized
{"message": "IAM authentication not enabled"}
```

**Solution:**
```bash
# Check environment variable
sudo grep ENABLE_IAM_AUTH /etc/ocpctl/api.env

# Should be:
ENABLE_IAM_AUTH=true

# If not set, add it and restart
echo "ENABLE_IAM_AUTH=true" | sudo tee -a /etc/ocpctl/api.env
sudo systemctl restart ocpctl-api
```

### Issue: "access denied: failed to list groups for user"

**Symptom:**
```
HTTP 403 Forbidden
{"message": "access denied: failed to list groups for user alice: AccessDenied"}
```

**Cause:** API server IAM role lacks `iam:ListGroupsForUser` permission

**Solution:**
```bash
# Add permission to API server role
aws iam put-role-policy \
  --role-name ocpctl-api-role \
  --policy-name IAMGroupChecking \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": ["iam:ListGroupsForUser"],
      "Resource": "*"
    }]
  }'

# Restart API service
sudo systemctl restart ocpctl-api
```

### Issue: "access denied: user bob is not a member of required group"

**Symptom:**
```
HTTP 403 Forbidden
{"message": "access denied: user bob is not a member of required group ocpctl-users"}
```

**Cause:** User is not in the required IAM group

**Solution:**
```bash
# Add user to group
aws iam add-user-to-group --user-name bob --group-name ocpctl-users

# Verify
aws iam get-group --group-name ocpctl-users

# User can now authenticate
```

### Issue: "access denied: principal is not an IAM user"

**Symptom:**
```
HTTP 403 Forbidden
{"message": "access denied: principal is not an IAM user: arn:aws:iam::123:federated-user/alice"}
```

**Cause:** Federated users or non-user principals can't be checked for group membership

**Solution:**
- Use IAM users or assumed roles instead of federated users
- Or disable group checking: `IAM_ALLOWED_GROUP=`

### Issue: Auto-provisioned user has wrong role

**Symptom:** New IAM user auto-provisioned with `USER` role, but should be `ADMIN`

**Solution:**
```bash
# Get user ID from database
psql ocpctl -c "SELECT id, email, role FROM users WHERE email LIKE '%alice%';"

# Update role
psql ocpctl -c "UPDATE users SET role = 'ADMIN' WHERE id = '<user-id>';"

# Verify
psql ocpctl -c "SELECT id, email, role FROM users WHERE id = '<user-id>';"
```

### Check Logs

```bash
# All IAM auth logs
sudo journalctl -u ocpctl-api | grep "IAM auth:"

# Recent errors
sudo journalctl -u ocpctl-api -n 100 --priority err

# Follow live logs
sudo journalctl -u ocpctl-api -f | grep "IAM"
```

---

## Security Considerations

### 1. Group Membership is Checked on Every Login

- Not just during auto-provisioning
- Removing a user from the IAM group immediately denies access on next login
- No need to disable the user in ocpctl database

### 2. Assumed Roles Bypass Group Check

- **Design decision**: Assumed roles don't have groups
- **Use case**: Service accounts, CI/CD, automation
- **Security**: Control via IAM role trust policy instead

To restrict which roles can be assumed:
```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "AWS": ["arn:aws:iam::123456789012:role/AllowedRole"]
    },
    "Action": "sts:AssumeRole"
  }]
}
```

### 3. Error Messages Don't Reveal User Existence

- Same error message whether user doesn't exist or not in group
- Prevents enumeration of valid usernames
- Rate limiting (existing middleware) prevents brute force

### 4. Audit Logging

All IAM authentication attempts are logged:
```bash
# View authentication attempts
sudo journalctl -u ocpctl-api | grep "IAM auth:" | tail -50

# Failed authentication attempts
sudo journalctl -u ocpctl-api | grep "access denied"
```

Consider forwarding to centralized logging (CloudWatch, Splunk, etc.)

### 5. Least Privilege

API server only needs:
- `sts:GetCallerIdentity` - Verify credentials
- `iam:ListGroupsForUser` - Check group membership (optional)

IAM users only need:
- `sts:GetCallerIdentity` - Authenticate to ocpctl
- Plus permissions for their ocpctl role (cluster management, etc.)

### 6. Rollback Plan

If issues occur with group membership checking:
```bash
# Disable group checking (allow all IAM users)
sudo sed -i 's/IAM_ALLOWED_GROUP=.*/IAM_ALLOWED_GROUP=/' /etc/ocpctl/api.env
sudo systemctl restart ocpctl-api

# No database changes needed - feature is runtime-only
```

---

## Related Documentation

- [AWS_QUICKSTART.md](./AWS_QUICKSTART.md) - Complete deployment guide
- [SECURITY_CONFIGURATION.md](./SECURITY_CONFIGURATION.md) - Security features overview
- [API Documentation](../architecture/api.yaml) - API reference

---

## Summary

IAM authentication provides:
- ✅ Centralized access management via AWS IAM
- ✅ No password management
- ✅ Auto-provisioning of new users
- ✅ Optional group-based access control
- ✅ Support for assumed roles (service accounts)
- ✅ Comprehensive audit logging

Best practices:
1. Enable IAM authentication: `ENABLE_IAM_AUTH=true`
2. Use group membership restriction: `IAM_ALLOWED_GROUP=ocpctl-users`
3. Grant API server `iam:ListGroupsForUser` permission
4. Monitor logs for authentication attempts
5. Use assumed roles for automation
6. Regularly audit group membership in AWS
