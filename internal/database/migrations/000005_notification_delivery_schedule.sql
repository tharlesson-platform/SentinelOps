ALTER TABLE notification_deliveries ADD COLUMN IF NOT EXISTS available_at timestamptz NOT NULL DEFAULT now();
CREATE INDEX IF NOT EXISTS notification_deliveries_ready_idx ON notification_deliveries(organization_id,status,available_at,created_at);
