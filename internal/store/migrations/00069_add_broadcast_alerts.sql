-- +goose Up
-- Admin-authored broadcast alerts shown in the web console until each user acknowledges them.
CREATE TABLE broadcast_alerts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  title VARCHAR(200) NOT NULL,
  body TEXT NOT NULL,
  severity VARCHAR(20) NOT NULL DEFAULT 'info' CHECK (severity IN ('info', 'warning', 'critical')),
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  expires_at TIMESTAMP WITH TIME ZONE,
  created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_broadcast_alerts_active ON broadcast_alerts(active, expires_at);

-- Per-user acknowledgment of a broadcast alert. A row means the user has dismissed it.
CREATE TABLE broadcast_alert_acks (
  alert_id UUID NOT NULL REFERENCES broadcast_alerts(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  acknowledged_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
  PRIMARY KEY (alert_id, user_id)
);

CREATE INDEX idx_broadcast_alert_acks_user ON broadcast_alert_acks(user_id);

COMMENT ON TABLE broadcast_alerts IS 'Admin-authored broadcast alerts shown in the web console until acknowledged by each user.';
COMMENT ON TABLE broadcast_alert_acks IS 'Per-user acknowledgment of a broadcast alert; a row means the user has dismissed the alert.';

-- +goose Down
DROP TABLE IF EXISTS broadcast_alert_acks;
DROP TABLE IF EXISTS broadcast_alerts;
