ALTER TABLE incidents ADD COLUMN IF NOT EXISTS deduplication_key text;
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS acknowledged_at timestamptz;
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS acknowledged_by text;
ALTER TABLE incidents ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();
CREATE UNIQUE INDEX IF NOT EXISTS incidents_open_dedup_idx ON incidents(organization_id,deduplication_key)
WHERE deduplication_key IS NOT NULL AND status <> 'RESOLVED';

CREATE TABLE IF NOT EXISTS notification_deliveries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  incident_id uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  route_id uuid REFERENCES alert_routes(id),
  status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','DELIVERED','FAILED','DLQ','BLOCKED')),
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  provider_message_id text,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  delivered_at timestamptz,
  UNIQUE(incident_id,route_id)
);
CREATE INDEX IF NOT EXISTS notification_deliveries_incident_idx ON notification_deliveries(organization_id,incident_id,status);
ALTER TABLE notification_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE notification_deliveries FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON notification_deliveries;
CREATE POLICY tenant_isolation ON notification_deliveries
USING (sentinelops_tenant_visible(organization_id))
WITH CHECK (sentinelops_tenant_visible(organization_id) AND EXISTS (
  SELECT 1 FROM incidents i WHERE i.id=notification_deliveries.incident_id AND i.organization_id=notification_deliveries.organization_id
));
