ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS payload_sha256 text;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS payload_size bigint;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS processed_at timestamptz;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS processing_error text;

CREATE TABLE IF NOT EXISTS outbox_events (
  id bigserial PRIMARY KEY,
  organization_id uuid NOT NULL REFERENCES organizations(id),
  event_type text NOT NULL CHECK (event_type ~ '^[a-z][a-z0-9_.-]{2,127}$'),
  payload jsonb NOT NULL DEFAULT '{}',
  status text NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','PROCESSING','DELIVERED','DLQ')),
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  available_at timestamptz NOT NULL DEFAULT now(),
  locked_at timestamptz,
  delivered_at timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS outbox_events_dispatch_idx ON outbox_events(organization_id,status,available_at,id);

ALTER TABLE outbox_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE outbox_events FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON outbox_events;
CREATE POLICY tenant_isolation ON outbox_events
USING (sentinelops_tenant_visible(organization_id))
WITH CHECK (sentinelops_tenant_visible(organization_id));
