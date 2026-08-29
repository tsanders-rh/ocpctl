package types

import "time"

// BroadcastAlertSeverity determines how a broadcast alert renders in the web console.
type BroadcastAlertSeverity string

const (
	// BroadcastAlertInfo renders as a dismissible informational banner.
	BroadcastAlertInfo BroadcastAlertSeverity = "info"
	// BroadcastAlertWarning renders as a dismissible warning banner.
	BroadcastAlertWarning BroadcastAlertSeverity = "warning"
	// BroadcastAlertCritical renders as a blocking modal that must be acknowledged
	// before the user can interact with the console.
	BroadcastAlertCritical BroadcastAlertSeverity = "critical"
)

// BroadcastAlert is an admin-authored message shown to all users in the web
// console until each user acknowledges it. Acknowledgment is per-user and
// permanent (tracked in broadcast_alert_acks).
type BroadcastAlert struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Body      string                 `json:"body"`
	Severity  BroadcastAlertSeverity `json:"severity"`
	CreatedBy *string                `json:"createdBy,omitempty"`
	Active    bool                   `json:"active"`
	ExpiresAt *time.Time             `json:"expiresAt,omitempty"`
	CreatedAt time.Time              `json:"createdAt"`

	// AckCount and TotalUsers are populated for the admin list view only; they
	// are zero on the per-user "active alerts" endpoint.
	AckCount   int `json:"ackCount"`
	TotalUsers int `json:"totalUsers"`
}
