CREATE TABLE IF NOT EXISTS data_lifecycle_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  request_type text NOT NULL CHECK (request_type IN ('export','erasure')),
  status text NOT NULL CHECK (status IN ('requested','approved','running','completed','rejected','failed')),
  requested_by text NOT NULL,
  approved_by text,
  reason text NOT NULL,
  scope jsonb NOT NULL DEFAULT '{}'::jsonb,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
ALTER TABLE data_lifecycle_requests ADD CONSTRAINT data_lifecycle_requests_separation_of_duties
  CHECK (approved_by IS NULL OR approved_by <> requested_by);
CREATE INDEX IF NOT EXISTS data_lifecycle_requests_tenant_status_idx ON data_lifecycle_requests(organization_id,status,created_at DESC);
ALTER TABLE data_lifecycle_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE data_lifecycle_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON data_lifecycle_requests
USING (sentinelops_tenant_visible(organization_id))
WITH CHECK (sentinelops_tenant_visible(organization_id));
