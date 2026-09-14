-- Durable, idempotent ownership for a synthetic schedule slot.  A worker
-- claims and creates the run in one transaction, preventing duplicate runs
-- when multiple worker replicas are active.
CREATE TABLE IF NOT EXISTS synthetic_execution_claims (
  organization_id uuid NOT NULL REFERENCES organizations(id),
  scenario_id uuid NOT NULL REFERENCES synthetic_scenarios(id),
  scenario_version integer NOT NULL CHECK (scenario_version > 0),
  schedule_slot timestamptz NOT NULL,
  run_id uuid NOT NULL REFERENCES test_runs(id),
  claimed_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (organization_id, scenario_id, scenario_version, schedule_slot)
);
CREATE INDEX IF NOT EXISTS synthetic_execution_claims_run_idx ON synthetic_execution_claims(run_id);

ALTER TABLE synthetic_execution_claims ENABLE ROW LEVEL SECURITY;
ALTER TABLE synthetic_execution_claims FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON synthetic_execution_claims;
CREATE POLICY tenant_isolation ON synthetic_execution_claims
USING (sentinelops_tenant_visible(organization_id))
WITH CHECK (
  sentinelops_tenant_visible(organization_id)
  AND EXISTS (
    SELECT 1 FROM synthetic_scenarios s
    WHERE s.id = synthetic_execution_claims.scenario_id
      AND s.organization_id = synthetic_execution_claims.organization_id
  )
  AND EXISTS (
    SELECT 1 FROM test_runs r
    WHERE r.id = synthetic_execution_claims.run_id
      AND r.organization_id = synthetic_execution_claims.organization_id
  )
);
