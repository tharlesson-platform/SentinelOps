CREATE TABLE IF NOT EXISTS tenant_scoped_request_windows (
  organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  scope text NOT NULL CHECK (scope ~ '^[a-z][a-z0-9-]{1,62}$'),
  window_started timestamptz NOT NULL,
  request_count integer NOT NULL CHECK (request_count > 0),
  PRIMARY KEY (organization_id,scope,window_started)
);
ALTER TABLE tenant_scoped_request_windows ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_scoped_request_windows FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON tenant_scoped_request_windows
USING (sentinelops_tenant_visible(organization_id))
WITH CHECK (sentinelops_tenant_visible(organization_id));
