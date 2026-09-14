CREATE TABLE IF NOT EXISTS tenant_request_windows (
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  window_started timestamptz NOT NULL,
  request_count integer NOT NULL CHECK (request_count > 0),
  PRIMARY KEY (organization_id, window_started)
);

ALTER TABLE tenant_request_windows ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_request_windows FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON tenant_request_windows;
CREATE POLICY tenant_isolation ON tenant_request_windows
USING (sentinelops_tenant_visible(organization_id))
WITH CHECK (sentinelops_tenant_visible(organization_id));
