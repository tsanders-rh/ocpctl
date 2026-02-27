package types

import "time"

// Role represents a user's role in the system
type Role string

const (
	RoleRequester     Role = "requester"
	RolePlatformAdmin Role = "platform_admin"
	RoleAuditor       Role = "auditor"
)

// IAMPrincipalType represents the type of IAM principal
type IAMPrincipalType string

const (
	IAMPrincipalTypeUser  IAMPrincipalType = "user"
	IAMPrincipalTypeRole  IAMPrincipalType = "role"
	IAMPrincipalTypeGroup IAMPrincipalType = "group"
)

// RBACMapping maps IAM principals to teams and roles
type RBACMapping struct {
	ID               string           `db:"id"`
	IAMPrincipalARN  string           `db:"iam_principal_arn"`
	IAMPrincipalType IAMPrincipalType `db:"iam_principal_type"`
	Team             string           `db:"team"`
	Role             Role             `db:"role"`
	Enabled          bool             `db:"enabled"`
	CreatedAt        time.Time        `db:"created_at"`
	UpdatedAt        time.Time        `db:"updated_at"`
	CreatedBy        *string          `db:"created_by"`
	Notes            *string          `db:"notes"`
}

// AuditEventStatus represents the outcome of an audited action
type AuditEventStatus string

const (
	AuditEventStatusSuccess AuditEventStatus = "SUCCESS"
	AuditEventStatusFailure AuditEventStatus = "FAILURE"
	AuditEventStatusDenied  AuditEventStatus = "DENIED"
)

// AuditEvent represents an immutable audit log entry
type AuditEvent struct {
	ID              string           `db:"id"`
	Actor           string           `db:"actor"` // IAM principal ARN
	Action          string           `db:"action"`
	TargetClusterID *string          `db:"target_cluster_id"`
	TargetJobID     *string          `db:"target_job_id"`
	Status          AuditEventStatus `db:"status"`
	Metadata        JobMetadata      `db:"metadata"`
	IPAddress       *string          `db:"ip_address"`
	UserAgent       *string          `db:"user_agent"`
	CreatedAt       time.Time        `db:"created_at"`
}

// IdempotencyKey represents a cached request for idempotent operations
type IdempotencyKey struct {
	ID                 string     `db:"id"`
	Key                string     `db:"key"`
	RequestHash        string     `db:"request_hash"`
	ResponseStatusCode *int       `db:"response_status_code"`
	ResponseBody       []byte     `db:"response_body"`
	CreatedAt          time.Time  `db:"created_at"`
	ExpiresAt          time.Time  `db:"expires_at"`
}
