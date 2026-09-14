ALTER TABLE notification_deliveries ADD COLUMN IF NOT EXISTS target_ref text;
ALTER TABLE notification_deliveries ADD COLUMN IF NOT EXISTS delivery_key text NOT NULL DEFAULT 'initial';
ALTER TABLE notification_deliveries DROP CONSTRAINT IF EXISTS notification_deliveries_incident_id_route_id_key;
CREATE UNIQUE INDEX IF NOT EXISTS notification_deliveries_incident_route_key_idx
  ON notification_deliveries(incident_id,route_id,delivery_key);

CREATE TABLE IF NOT EXISTS incident_escalations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  incident_id uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  route_id uuid NOT NULL REFERENCES alert_routes(id) ON DELETE CASCADE,
  target_ref text NOT NULL,
  scheduled_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'SCHEDULED'
    CHECK (status IN ('SCHEDULED','ESCALATED','CANCELLED')),
  cancelled_at timestamptz,
  escalated_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(incident_id,route_id)
);
CREATE INDEX IF NOT EXISTS incident_escalations_ready_idx
  ON incident_escalations(organization_id,status,scheduled_at);
ALTER TABLE incident_escalations ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_escalations FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON incident_escalations;
CREATE POLICY tenant_isolation ON incident_escalations
USING (sentinelops_tenant_visible(organization_id))
WITH CHECK (sentinelops_tenant_visible(organization_id) AND EXISTS (
  SELECT 1 FROM incidents i
  WHERE i.id=incident_escalations.incident_id
    AND i.organization_id=incident_escalations.organization_id
));
