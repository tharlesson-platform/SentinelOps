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
  labels jsonb NOT NULL DEFAULT '{}', token_hash bytea NOT NULL, revoked_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), version integer NOT NULL DEFAULT 1,
  UNIQUE(organization_id, name)
);
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
