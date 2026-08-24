CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS organizations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text UNIQUE NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  created_by text NOT NULL DEFAULT 'system', version integer NOT NULL DEFAULT 1
);
INSERT INTO organizations(name) VALUES ('local') ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS teams (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id),
  name text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  created_by text NOT NULL DEFAULT 'system', version integer NOT NULL DEFAULT 1,
  UNIQUE(organization_id, name)
);
CREATE TABLE IF NOT EXISTS users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id),
  subject text NOT NULL, email text, display_name text, disabled_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  created_by text NOT NULL DEFAULT 'system', version integer NOT NULL DEFAULT 1,
  UNIQUE(organization_id, subject)
);
CREATE TABLE IF NOT EXISTS roles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text UNIQUE NOT NULL, permissions jsonb NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS role_bindings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id),
  subject text NOT NULL, role_id uuid NOT NULL REFERENCES roles(id), scope_type text NOT NULL, scope_id text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS role_bindings_unique_scope_idx
  ON role_bindings(organization_id, subject, role_id, scope_type, scope_id);

CREATE TABLE IF NOT EXISTS services (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id),
  name text NOT NULL, display_name text NOT NULL, description text NOT NULL DEFAULT '', owner_team text NOT NULL,
  tier text NOT NULL DEFAULT '3', labels jsonb NOT NULL DEFAULT '{}', deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  created_by text NOT NULL, version integer NOT NULL DEFAULT 1,
  UNIQUE(organization_id, name)
);
CREATE INDEX IF NOT EXISTS services_org_active_idx ON services(organization_id, name) WHERE deleted_at IS NULL;
CREATE TABLE IF NOT EXISTS service_owners (
  service_id uuid NOT NULL REFERENCES services(id), team_id uuid REFERENCES teams(id), user_id uuid REFERENCES users(id),
  role text NOT NULL, PRIMARY KEY(service_id, role)
);
CREATE TABLE IF NOT EXISTS environments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), service_id uuid NOT NULL REFERENCES services(id), name text NOT NULL,
  timezone text NOT NULL DEFAULT 'America/Sao_Paulo', labels jsonb NOT NULL DEFAULT '{}', UNIQUE(service_id, name)
);
CREATE TABLE IF NOT EXISTS endpoints (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), environment_id uuid NOT NULL REFERENCES environments(id), name text NOT NULL,
  url text NOT NULL, protocol text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(environment_id, name)
);
CREATE TABLE IF NOT EXISTS telemetry_sources (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), service_id uuid NOT NULL REFERENCES services(id), type text NOT NULL,
  datasource_uid text NOT NULL, config jsonb NOT NULL DEFAULT '{}', UNIQUE(service_id, type, datasource_uid)
);

CREATE TABLE IF NOT EXISTS agents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), name text NOT NULL,
  region text, cloud_provider text, account text, cluster text, network text, location text, team text, environment text,
  labels jsonb NOT NULL DEFAULT '{}', token_hash bytea NOT NULL, client_cert_fingerprint text, revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), version integer NOT NULL DEFAULT 1,
  UNIQUE(organization_id, name)
);
ALTER TABLE agents ADD COLUMN IF NOT EXISTS client_cert_fingerprint text;
DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'agents_client_cert_fingerprint_format'
      AND conrelid = 'agents'::regclass
  ) THEN
    ALTER TABLE agents ADD CONSTRAINT agents_client_cert_fingerprint_format
      CHECK (client_cert_fingerprint IS NULL OR client_cert_fingerprint ~ '^[a-f0-9]{64}$');
  END IF;
END;
$$;
CREATE TABLE IF NOT EXISTS agent_bootstrap_tokens (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id),
  token_hash bytea NOT NULL UNIQUE, bound_agent_name text, expires_at timestamptz NOT NULL,
  max_uses integer NOT NULL DEFAULT 1 CHECK (max_uses = 1), use_count integer NOT NULL DEFAULT 0 CHECK (use_count BETWEEN 0 AND max_uses),
  used_at timestamptz, revoked_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL
);
CREATE INDEX IF NOT EXISTS agent_bootstrap_tokens_active_idx ON agent_bootstrap_tokens(organization_id, expires_at)
  WHERE revoked_at IS NULL AND use_count = 0;
CREATE TABLE IF NOT EXISTS agent_capabilities (
  agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE, capability text NOT NULL, PRIMARY KEY(agent_id, capability)
);
CREATE TABLE IF NOT EXISTS agent_locations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), name text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}', UNIQUE(organization_id, name)
);
CREATE TABLE IF NOT EXISTS agent_heartbeats (
  id bigserial PRIMARY KEY, agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
  observed_at timestamptz NOT NULL DEFAULT now(), status jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS agent_heartbeats_latest_idx ON agent_heartbeats(agent_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS synthetic_scenarios (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), name text NOT NULL,
  service_ref text NOT NULL, environment text NOT NULL, type text NOT NULL, enabled boolean NOT NULL DEFAULT true,
  current_version integer NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  created_by text NOT NULL, UNIQUE(organization_id, name)
);
CREATE TABLE IF NOT EXISTS synthetic_scenario_versions (
  scenario_id uuid NOT NULL REFERENCES synthetic_scenarios(id), version integer NOT NULL, spec jsonb NOT NULL,
  checksum text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL,
  PRIMARY KEY(scenario_id, version)
);
CREATE TABLE IF NOT EXISTS schedules (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), scenario_id uuid NOT NULL REFERENCES synthetic_scenarios(id),
  cron text, interval_seconds integer, timezone text NOT NULL DEFAULT 'America/Sao_Paulo', jitter_seconds integer NOT NULL DEFAULT 0,
  schedule_window jsonb NOT NULL DEFAULT '{}', paused_until timestamptz
);
CREATE TABLE IF NOT EXISTS test_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id),
  scenario_id uuid NOT NULL REFERENCES synthetic_scenarios(id), agent_id uuid REFERENCES agents(id), status text NOT NULL,
  started_at timestamptz, finished_at timestamptz, result jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS test_steps (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), test_run_id uuid NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
  position integer NOT NULL, action text NOT NULL, status text NOT NULL, duration_ms bigint, evidence jsonb NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS test_assertions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), test_run_id uuid NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
  name text NOT NULL, status text NOT NULL, observed jsonb, expected jsonb, message text
);
CREATE TABLE IF NOT EXISTS test_artifacts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), test_run_id uuid NOT NULL REFERENCES test_runs(id) ON DELETE CASCADE,
  type text NOT NULL, object_key text NOT NULL, sha256 text NOT NULL, size_bytes bigint NOT NULL, redacted boolean NOT NULL DEFAULT true
);

CREATE TABLE IF NOT EXISTS releases (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), service text NOT NULL,
  environment text NOT NULL, deployment_id text, commit_sha text, branch text, repository text, image text, image_digest text,
  version text NOT NULL, deployed_at timestamptz NOT NULL, pipeline text, pipeline_url text, actor text,
  deployment_strategy text, cluster text, namespace text, labels jsonb NOT NULL DEFAULT '{}',
  idempotency_key text, created_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL,
  UNIQUE(organization_id, idempotency_key)
);
CREATE INDEX IF NOT EXISTS releases_service_time_idx ON releases(organization_id, service, environment, deployed_at DESC);
CREATE TABLE IF NOT EXISTS deployments (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), release_id uuid NOT NULL REFERENCES releases(id), provider text,
  external_id text, metadata jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS release_policies (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), name text NOT NULL,
  current_version integer NOT NULL DEFAULT 1, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(organization_id, name)
);
CREATE TABLE IF NOT EXISTS release_policy_versions (
  policy_id uuid NOT NULL REFERENCES release_policies(id), version integer NOT NULL, spec jsonb NOT NULL, checksum text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL, PRIMARY KEY(policy_id, version)
);
CREATE TABLE IF NOT EXISTS validations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id),
  release_id uuid NOT NULL REFERENCES releases(id), policy_version text, mode text NOT NULL, status text NOT NULL,
  result text, summary text NOT NULL DEFAULT '', temporal_workflow_id text UNIQUE, started_at timestamptz,
  finished_at timestamptz, cancelled_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL
);
CREATE TABLE IF NOT EXISTS validation_checks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), validation_id uuid NOT NULL REFERENCES validations(id) ON DELETE CASCADE,
  name text NOT NULL, source text NOT NULL, required boolean NOT NULL, status text NOT NULL,
  observed jsonb, threshold jsonb, message text, started_at timestamptz, finished_at timestamptz,
  UNIQUE(validation_id, name)
);
CREATE TABLE IF NOT EXISTS validation_evidence (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), validation_id uuid NOT NULL REFERENCES validations(id) ON DELETE CASCADE,
  check_id uuid REFERENCES validation_checks(id), type text NOT NULL, uri text, payload jsonb, sha256 text,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS slos (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), name text NOT NULL,
  service_ref text NOT NULL, current_version integer NOT NULL DEFAULT 1, UNIQUE(organization_id, name)
);
CREATE TABLE IF NOT EXISTS slo_versions (
  slo_id uuid NOT NULL REFERENCES slos(id), version integer NOT NULL, spec jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
  created_by text NOT NULL, PRIMARY KEY(slo_id, version)
);
CREATE TABLE IF NOT EXISTS error_budgets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), slo_id uuid NOT NULL REFERENCES slos(id), window_start timestamptz NOT NULL,
  window_end timestamptz NOT NULL, total numeric NOT NULL, consumed numeric NOT NULL, calculated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS dashboards (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), uid text NOT NULL,
  name text NOT NULL, ownership text NOT NULL, managed boolean NOT NULL DEFAULT false, current_version integer NOT NULL DEFAULT 1,
  UNIQUE(organization_id, uid)
);
CREATE TABLE IF NOT EXISTS dashboard_versions (
  dashboard_id uuid NOT NULL REFERENCES dashboards(id), version integer NOT NULL, grafana_json jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL, PRIMARY KEY(dashboard_id, version)
);
CREATE TABLE IF NOT EXISTS alert_routes (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), name text NOT NULL,
  spec jsonb NOT NULL, enabled boolean NOT NULL DEFAULT true
);
CREATE TABLE IF NOT EXISTS maintenance_windows (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), name text NOT NULL,
  starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL, scope jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS incidents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), title text NOT NULL,
  severity text NOT NULL, status text NOT NULL, commander text, summary text, created_at timestamptz NOT NULL DEFAULT now(),
  resolved_at timestamptz
);
CREATE TABLE IF NOT EXISTS incident_events (
  id bigserial PRIMARY KEY, incident_id uuid NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
  type text NOT NULL, payload jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), created_by text NOT NULL
);
CREATE TABLE IF NOT EXISTS runbooks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), slug text NOT NULL,
  title text NOT NULL, content text NOT NULL, version integer NOT NULL DEFAULT 1, UNIQUE(organization_id, slug)
);
CREATE TABLE IF NOT EXISTS secret_references (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), name text NOT NULL,
  provider text NOT NULL, reference text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(organization_id, name)
);
CREATE TABLE IF NOT EXISTS integrations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), name text NOT NULL,
  type text NOT NULL, config jsonb NOT NULL, enabled boolean NOT NULL DEFAULT true, UNIQUE(organization_id, name)
);
CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), organization_id uuid NOT NULL REFERENCES organizations(id), integration text NOT NULL,
  idempotency_key text NOT NULL, nonce text NOT NULL, signature text NOT NULL, received_at timestamptz NOT NULL DEFAULT now(),
  status text NOT NULL, UNIQUE(organization_id, integration, idempotency_key), UNIQUE(organization_id, nonce)
);
CREATE TABLE IF NOT EXISTS audit_events (
  id bigserial PRIMARY KEY, organization_id uuid NOT NULL REFERENCES organizations(id), occurred_at timestamptz NOT NULL DEFAULT now(),
  actor text NOT NULL, action text NOT NULL, resource_type text NOT NULL, resource_id text, request_id text NOT NULL,
  source_ip inet, payload jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS audit_events_org_time_idx ON audit_events(organization_id, occurred_at DESC);

INSERT INTO roles(name, permissions) VALUES
('Platform Administrator', '["*"]'), ('SRE Administrator', '["service:*","scenario:*","validation:*","agent:*"]'),
('SRE Operator', '["service:read","scenario:*","validation:*","agent:read"]'),
('Developer', '["service:read","scenario:read","validation:read","release:create"]'),
('Application Owner', '["service:read","scenario:*","validation:*"]'), ('Auditor', '["*:read"]'),
('Viewer', '["service:read","scenario:read","validation:read"]') ON CONFLICT DO NOTHING;

INSERT INTO role_bindings(organization_id,subject,role_id,scope_type,scope_id,created_by)
SELECT o.id,'admin',r.id,'organization','*','system-bootstrap'
FROM organizations o JOIN roles r ON r.name='Platform Administrator'
WHERE o.name='local'
ON CONFLICT DO NOTHING;

-- Defense in depth for tenant isolation. The application resets this setting
-- every time a pooled connection is acquired. An absent/invalid tenant is
-- denied, while the internal migration context uses "*" so repeatable schema
-- startup remains possible after FORCE ROW LEVEL SECURITY is enabled.
CREATE OR REPLACE FUNCTION sentinelops_migration_authorized()
RETURNS boolean
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $$
  SELECT current_user = pg_get_userbyid(p.proowner)
  FROM pg_proc p
  WHERE p.oid = 'sentinelops_migration_authorized()'::regprocedure
$$;

CREATE OR REPLACE FUNCTION sentinelops_tenant_visible(candidate uuid)
RETURNS boolean
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $$
  SELECT (current_setting('app.organization_id', true) = '*' AND sentinelops_migration_authorized())
      OR candidate::text = nullif(current_setting('app.organization_id', true), '')
$$;

DO $rls$
DECLARE
  tenant_table text;
BEGIN
  FOREACH tenant_table IN ARRAY ARRAY[
    'teams', 'users', 'role_bindings', 'services', 'agents',
    'agent_bootstrap_tokens', 'agent_locations', 'synthetic_scenarios',
    'test_runs', 'releases', 'release_policies', 'validations', 'slos',
    'dashboards', 'alert_routes', 'maintenance_windows', 'incidents',
    'runbooks', 'secret_references', 'integrations', 'webhook_deliveries',
    'audit_events'
  ] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', tenant_table);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', tenant_table);
    EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', tenant_table);
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I USING (sentinelops_tenant_visible(organization_id)) WITH CHECK (sentinelops_tenant_visible(organization_id))',
      tenant_table
    );
  END LOOP;
END
$rls$;

ALTER TABLE service_owners ENABLE ROW LEVEL SECURITY;
ALTER TABLE service_owners FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON service_owners;
CREATE POLICY tenant_isolation ON service_owners
USING (EXISTS (
  SELECT 1 FROM services s
  WHERE s.id = service_owners.service_id AND sentinelops_tenant_visible(s.organization_id)
))
WITH CHECK (
  EXISTS (
    SELECT 1 FROM services s
    WHERE s.id = service_owners.service_id AND sentinelops_tenant_visible(s.organization_id)
  )
  AND (team_id IS NULL OR EXISTS (
    SELECT 1 FROM teams t
    JOIN services s ON s.organization_id = t.organization_id
    WHERE t.id = service_owners.team_id AND s.id = service_owners.service_id
      AND sentinelops_tenant_visible(t.organization_id)
  ))
  AND (user_id IS NULL OR EXISTS (
    SELECT 1 FROM users u
    JOIN services s ON s.organization_id = u.organization_id
    WHERE u.id = service_owners.user_id AND s.id = service_owners.service_id
      AND sentinelops_tenant_visible(u.organization_id)
  ))
);

ALTER TABLE environments ENABLE ROW LEVEL SECURITY;
ALTER TABLE environments FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON environments;
CREATE POLICY tenant_isolation ON environments
USING (EXISTS (
  SELECT 1 FROM services s
  WHERE s.id = environments.service_id AND sentinelops_tenant_visible(s.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM services s
  WHERE s.id = environments.service_id AND sentinelops_tenant_visible(s.organization_id)
));

ALTER TABLE endpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE endpoints FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON endpoints;
CREATE POLICY tenant_isolation ON endpoints
USING (EXISTS (
  SELECT 1 FROM environments e JOIN services s ON s.id = e.service_id
  WHERE e.id = endpoints.environment_id AND sentinelops_tenant_visible(s.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM environments e JOIN services s ON s.id = e.service_id
  WHERE e.id = endpoints.environment_id AND sentinelops_tenant_visible(s.organization_id)
));

ALTER TABLE telemetry_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE telemetry_sources FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON telemetry_sources;
CREATE POLICY tenant_isolation ON telemetry_sources
USING (EXISTS (
  SELECT 1 FROM services s
  WHERE s.id = telemetry_sources.service_id AND sentinelops_tenant_visible(s.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM services s
  WHERE s.id = telemetry_sources.service_id AND sentinelops_tenant_visible(s.organization_id)
));

ALTER TABLE agent_capabilities ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_capabilities FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON agent_capabilities;
CREATE POLICY tenant_isolation ON agent_capabilities
USING (EXISTS (
  SELECT 1 FROM agents a
  WHERE a.id = agent_capabilities.agent_id AND sentinelops_tenant_visible(a.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM agents a
  WHERE a.id = agent_capabilities.agent_id AND sentinelops_tenant_visible(a.organization_id)
));

ALTER TABLE agent_heartbeats ENABLE ROW LEVEL SECURITY;
ALTER TABLE agent_heartbeats FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON agent_heartbeats;
CREATE POLICY tenant_isolation ON agent_heartbeats
USING (EXISTS (
  SELECT 1 FROM agents a
  WHERE a.id = agent_heartbeats.agent_id AND sentinelops_tenant_visible(a.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM agents a
  WHERE a.id = agent_heartbeats.agent_id AND sentinelops_tenant_visible(a.organization_id)
));

ALTER TABLE synthetic_scenario_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE synthetic_scenario_versions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON synthetic_scenario_versions;
CREATE POLICY tenant_isolation ON synthetic_scenario_versions
USING (EXISTS (
  SELECT 1 FROM synthetic_scenarios s
  WHERE s.id = synthetic_scenario_versions.scenario_id AND sentinelops_tenant_visible(s.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM synthetic_scenarios s
  WHERE s.id = synthetic_scenario_versions.scenario_id AND sentinelops_tenant_visible(s.organization_id)
));

ALTER TABLE schedules ENABLE ROW LEVEL SECURITY;
ALTER TABLE schedules FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON schedules;
CREATE POLICY tenant_isolation ON schedules
USING (EXISTS (
  SELECT 1 FROM synthetic_scenarios s
  WHERE s.id = schedules.scenario_id AND sentinelops_tenant_visible(s.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM synthetic_scenarios s
  WHERE s.id = schedules.scenario_id AND sentinelops_tenant_visible(s.organization_id)
));

ALTER TABLE test_steps ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_steps FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON test_steps;
CREATE POLICY tenant_isolation ON test_steps
USING (EXISTS (
  SELECT 1 FROM test_runs r
  WHERE r.id = test_steps.test_run_id AND sentinelops_tenant_visible(r.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM test_runs r
  WHERE r.id = test_steps.test_run_id AND sentinelops_tenant_visible(r.organization_id)
));

ALTER TABLE test_assertions ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_assertions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON test_assertions;
CREATE POLICY tenant_isolation ON test_assertions
USING (EXISTS (
  SELECT 1 FROM test_runs r
  WHERE r.id = test_assertions.test_run_id AND sentinelops_tenant_visible(r.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM test_runs r
  WHERE r.id = test_assertions.test_run_id AND sentinelops_tenant_visible(r.organization_id)
));

ALTER TABLE test_artifacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE test_artifacts FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON test_artifacts;
CREATE POLICY tenant_isolation ON test_artifacts
USING (EXISTS (
  SELECT 1 FROM test_runs r
  WHERE r.id = test_artifacts.test_run_id AND sentinelops_tenant_visible(r.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM test_runs r
  WHERE r.id = test_artifacts.test_run_id AND sentinelops_tenant_visible(r.organization_id)
));

ALTER TABLE deployments ENABLE ROW LEVEL SECURITY;
ALTER TABLE deployments FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON deployments;
CREATE POLICY tenant_isolation ON deployments
USING (EXISTS (
  SELECT 1 FROM releases r
  WHERE r.id = deployments.release_id AND sentinelops_tenant_visible(r.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM releases r
  WHERE r.id = deployments.release_id AND sentinelops_tenant_visible(r.organization_id)
));

ALTER TABLE release_policy_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE release_policy_versions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON release_policy_versions;
CREATE POLICY tenant_isolation ON release_policy_versions
USING (EXISTS (
  SELECT 1 FROM release_policies p
  WHERE p.id = release_policy_versions.policy_id AND sentinelops_tenant_visible(p.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM release_policies p
  WHERE p.id = release_policy_versions.policy_id AND sentinelops_tenant_visible(p.organization_id)
));

ALTER TABLE validation_checks ENABLE ROW LEVEL SECURITY;
ALTER TABLE validation_checks FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON validation_checks;
CREATE POLICY tenant_isolation ON validation_checks
USING (EXISTS (
  SELECT 1 FROM validations v
  WHERE v.id = validation_checks.validation_id AND sentinelops_tenant_visible(v.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM validations v
  WHERE v.id = validation_checks.validation_id AND sentinelops_tenant_visible(v.organization_id)
));

ALTER TABLE validation_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE validation_evidence FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON validation_evidence;
CREATE POLICY tenant_isolation ON validation_evidence
USING (EXISTS (
  SELECT 1 FROM validations v
  WHERE v.id = validation_evidence.validation_id AND sentinelops_tenant_visible(v.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM validations v
  WHERE v.id = validation_evidence.validation_id AND sentinelops_tenant_visible(v.organization_id)
));

ALTER TABLE slo_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE slo_versions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON slo_versions;
CREATE POLICY tenant_isolation ON slo_versions
USING (EXISTS (
  SELECT 1 FROM slos s
  WHERE s.id = slo_versions.slo_id AND sentinelops_tenant_visible(s.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM slos s
  WHERE s.id = slo_versions.slo_id AND sentinelops_tenant_visible(s.organization_id)
));

ALTER TABLE error_budgets ENABLE ROW LEVEL SECURITY;
ALTER TABLE error_budgets FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON error_budgets;
CREATE POLICY tenant_isolation ON error_budgets
USING (EXISTS (
  SELECT 1 FROM slos s
  WHERE s.id = error_budgets.slo_id AND sentinelops_tenant_visible(s.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM slos s
  WHERE s.id = error_budgets.slo_id AND sentinelops_tenant_visible(s.organization_id)
));

ALTER TABLE dashboard_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE dashboard_versions FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON dashboard_versions;
CREATE POLICY tenant_isolation ON dashboard_versions
USING (EXISTS (
  SELECT 1 FROM dashboards d
  WHERE d.id = dashboard_versions.dashboard_id AND sentinelops_tenant_visible(d.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM dashboards d
  WHERE d.id = dashboard_versions.dashboard_id AND sentinelops_tenant_visible(d.organization_id)
));

ALTER TABLE incident_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE incident_events FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON incident_events;
CREATE POLICY tenant_isolation ON incident_events
USING (EXISTS (
  SELECT 1 FROM incidents i
  WHERE i.id = incident_events.incident_id AND sentinelops_tenant_visible(i.organization_id)
))
WITH CHECK (EXISTS (
  SELECT 1 FROM incidents i
  WHERE i.id = incident_events.incident_id AND sentinelops_tenant_visible(i.organization_id)
));

-- Cross-organization foreign-key references are denied even when the caller
-- knows another tenant's UUID.
DROP POLICY IF EXISTS tenant_isolation ON test_runs;
CREATE POLICY tenant_isolation ON test_runs
USING (sentinelops_tenant_visible(organization_id))
WITH CHECK (
  sentinelops_tenant_visible(organization_id)
  AND EXISTS (
    SELECT 1 FROM synthetic_scenarios s
    WHERE s.id = test_runs.scenario_id AND s.organization_id = test_runs.organization_id
  )
  AND (agent_id IS NULL OR EXISTS (
    SELECT 1 FROM agents a
    WHERE a.id = test_runs.agent_id AND a.organization_id = test_runs.organization_id
  ))
);

DROP POLICY IF EXISTS tenant_isolation ON validations;
CREATE POLICY tenant_isolation ON validations
USING (sentinelops_tenant_visible(organization_id))
WITH CHECK (
  sentinelops_tenant_visible(organization_id)
  AND EXISTS (
    SELECT 1 FROM releases r
    WHERE r.id = validations.release_id AND r.organization_id = validations.organization_id
  )
);
