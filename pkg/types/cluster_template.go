package types

import (
	"encoding/json"
	"time"
)

// ClusterTemplate stores reusable cluster-creation parameters for a single user.
// Config is a partial create-cluster payload; when a template is applied only the
// fields present in Config are pre-filled, and the cluster name is never stored.
type ClusterTemplate struct {
	ID        string          `db:"id" json:"id"`
	Name      string          `db:"name" json:"name"`
	OwnerID   string          `db:"owner_id" json:"ownerId"`
	Config    json.RawMessage `db:"config" json:"config"`
	CreatedAt time.Time       `db:"created_at" json:"createdAt"`
	UpdatedAt time.Time       `db:"updated_at" json:"updatedAt"`
}
