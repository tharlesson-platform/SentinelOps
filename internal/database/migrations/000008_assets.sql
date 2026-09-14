CREATE TABLE IF NOT EXISTS assets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES organizations(id),
  asset_id text NOT NULL,
  name text NOT NULL,
  kind text NOT NULL,
  site text NOT NULL DEFAULT '',
  owner_team text NOT NULL DEFAULT '',
  environment text NOT NULL DEFAULT '',
  lifecycle text NOT NULL DEFAULT 'active',
  source text NOT NULL,
  last_seen timestamptz,
  labels jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  created_by text NOT NULL,
  version integer NOT NULL DEFAULT 1,
  CONSTRAINT assets_lifecycle_check CHECK (lifecycle IN ('active','stale','retired')),
  CONSTRAINT assets_asset_id_format CHECK (asset_id ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$')
);
CREATE UNIQUE INDEX IF NOT EXISTS assets_organization_asset_id_idx ON assets(organization_id, asset_id);
CREATE INDEX IF NOT EXISTS assets_organization_lifecycle_idx ON assets(organization_id, lifecycle, updated_at DESC);

ALTER TABLE assets ENABLE ROW LEVEL SECURITY;
ALTER TABLE assets FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON assets;
CREATE POLICY tenant_isolation ON assets
  USING (sentinelops_tenant_visible(organization_id))
  WITH CHECK (sentinelops_tenant_visible(organization_id));
