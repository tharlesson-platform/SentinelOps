ALTER TABLE notification_deliveries ADD COLUMN IF NOT EXISTS locked_at timestamptz;
ALTER TABLE notification_deliveries DROP CONSTRAINT IF EXISTS notification_deliveries_status_check;
ALTER TABLE notification_deliveries ADD CONSTRAINT notification_deliveries_status_check
CHECK (status IN ('PENDING','PROCESSING','DELIVERED','FAILED','DLQ','BLOCKED'));
