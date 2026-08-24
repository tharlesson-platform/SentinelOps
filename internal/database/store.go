package database

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sentinelops/sentinelops/internal/domain"
)

//go:embed migrations/000001_initial.sql
var initialMigration string

type Store struct{ Pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	poolConfig, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	poolConfig.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		_, err := conn.Exec(ctx, "SELECT set_config('app.organization_id', $1, false)", tenantFromContext(ctx))
		return err == nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{Pool: pool}, nil
}

func (s *Store) Migrate(ctx context.Context) error {
	// The application can start with multiple replicas. Keep the embedded
	// migration inside one transaction and serialize it across replicas so two
	// processes cannot race while creating or altering the same objects.
	ctx = withMigrationTenant(ctx)
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	const migrationLockID int64 = 0x53454e54494e454c // "SENTINEL"
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	if _, err = tx.Exec(ctx, initialMigration); err != nil {
		return fmt.Errorf("apply initial migration: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration: %w", err)
	}
	return nil
}

func (s *Store) OrganizationID(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", errors.New("organization claim is required")
	}
	var id string
	err := s.Pool.QueryRow(ctx, "SELECT id::text FROM organizations WHERE name=$1", name).Scan(&id)
	return id, err
}

func (s *Store) EffectiveRole(ctx context.Context, organizationID, subject string) (string, error) {
	if organizationID == "" || subject == "" {
		return "", errors.New("organization and subject are required")
	}
	ctx = WithTenant(ctx, organizationID)
	var role string
	err := s.Pool.QueryRow(ctx, `SELECT r.name
FROM role_bindings b
JOIN roles r ON r.id=b.role_id
WHERE b.organization_id=$1 AND b.subject=$2 AND b.scope_type='organization' AND b.scope_id='*'
ORDER BY CASE r.name
  WHEN 'Platform Administrator' THEN 1 WHEN 'SRE Administrator' THEN 2 WHEN 'SRE Operator' THEN 3
  WHEN 'Application Owner' THEN 4 WHEN 'Developer' THEN 5 WHEN 'Auditor' THEN 6 ELSE 7 END
LIMIT 1`, organizationID, subject).Scan(&role)
	return role, err
}

func (s *Store) SeedDevelopmentBootstrapToken(ctx context.Context, organizationID, token, agentName string, expiresAt time.Time) error {
	if token == "" {
		return nil
	}
	ctx = WithTenant(ctx, organizationID)
	hash := sha256.Sum256([]byte(token))
	_, err := s.Pool.Exec(ctx, `INSERT INTO agent_bootstrap_tokens(organization_id,token_hash,bound_agent_name,expires_at,created_by) VALUES($1,$2,nullif($3,''),$4,'development-seed') ON CONFLICT(token_hash) DO NOTHING`, organizationID, hash[:], agentName, expiresAt)
	return err
}

func (s *Store) ListServices(ctx context.Context, organizationID string) ([]domain.Service, error) {
	ctx = WithTenant(ctx, organizationID)
	rows, err := s.Pool.Query(ctx, `SELECT id::text,name,display_name,description,owner_team,tier,labels,created_at,updated_at FROM services WHERE organization_id=$1 AND deleted_at IS NULL ORDER BY name`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Service{}
	for rows.Next() {
		var v domain.Service
		var labels []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.DisplayName, &v.Description, &v.OwnerTeam, &v.Tier, &labels, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(labels, &v.Labels)
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) GetService(ctx context.Context, organizationID, name string) (domain.Service, error) {
	ctx = WithTenant(ctx, organizationID)
	var v domain.Service
	var labels []byte
	err := s.Pool.QueryRow(ctx, `SELECT id::text,name,display_name,description,owner_team,tier,labels,created_at,updated_at FROM services WHERE organization_id=$1 AND name=$2 AND deleted_at IS NULL`, organizationID, name).Scan(&v.ID, &v.Name, &v.DisplayName, &v.Description, &v.OwnerTeam, &v.Tier, &labels, &v.CreatedAt, &v.UpdatedAt)
	_ = json.Unmarshal(labels, &v.Labels)
	return v, err
}

func (s *Store) UpsertService(ctx context.Context, organizationID, actor string, v domain.Service) (domain.Service, error) {
	ctx = WithTenant(ctx, organizationID)
	labels, _ := json.Marshal(v.Labels)
	err := s.Pool.QueryRow(ctx, `INSERT INTO services(organization_id,name,display_name,description,owner_team,tier,labels,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(organization_id,name) DO UPDATE SET display_name=EXCLUDED.display_name,description=EXCLUDED.description,owner_team=EXCLUDED.owner_team,tier=EXCLUDED.tier,labels=EXCLUDED.labels,updated_at=now(),version=services.version+1,deleted_at=NULL
RETURNING id::text,created_at,updated_at`, organizationID, v.Name, v.DisplayName, v.Description, v.OwnerTeam, v.Tier, labels, actor).Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (s *Store) DeleteService(ctx context.Context, organizationID, name string) error {
	ctx = WithTenant(ctx, organizationID)
	tag, err := s.Pool.Exec(ctx, "UPDATE services SET deleted_at=now(),updated_at=now() WHERE organization_id=$1 AND name=$2 AND deleted_at IS NULL", organizationID, name)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

func (s *Store) CreateRelease(ctx context.Context, organizationID, actor, idem string, v domain.Release) (domain.Release, error) {
	ctx = WithTenant(ctx, organizationID)
	if v.ID == "" {
		v.ID = uuid.NewString()
	}
	if v.DeployedAt.IsZero() {
		v.DeployedAt = time.Now().UTC()
	}
	labels, _ := json.Marshal(v.Labels)
	err := s.Pool.QueryRow(ctx, `INSERT INTO releases(id,organization_id,service,environment,deployment_id,commit_sha,image,image_digest,version,deployed_at,pipeline,pipeline_url,actor,deployment_strategy,cluster,namespace,labels,idempotency_key,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT(organization_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING id::text,created_at`, v.ID, organizationID, v.Service, v.Environment, v.DeploymentID, v.CommitSHA, v.Image, v.ImageDigest, v.Version, v.DeployedAt, v.Pipeline, v.PipelineURL, v.Actor, v.DeploymentStrategy, v.Cluster, v.Namespace, labels, idem, actor).Scan(&v.ID, &v.CreatedAt)
	return v, err
}

func (s *Store) GetRelease(ctx context.Context, organizationID, id string) (domain.Release, error) {
	ctx = WithTenant(ctx, organizationID)
	var v domain.Release
	var labels []byte
	err := s.Pool.QueryRow(ctx, `SELECT id::text,service,environment,coalesce(deployment_id,''),coalesce(commit_sha,''),coalesce(image,''),coalesce(image_digest,''),version,deployed_at,coalesce(pipeline,''),coalesce(pipeline_url,''),coalesce(actor,''),coalesce(deployment_strategy,''),coalesce(cluster,''),coalesce(namespace,''),labels,created_at FROM releases WHERE organization_id=$1 AND id=$2`, organizationID, id).Scan(&v.ID, &v.Service, &v.Environment, &v.DeploymentID, &v.CommitSHA, &v.Image, &v.ImageDigest, &v.Version, &v.DeployedAt, &v.Pipeline, &v.PipelineURL, &v.Actor, &v.DeploymentStrategy, &v.Cluster, &v.Namespace, &labels, &v.CreatedAt)
	_ = json.Unmarshal(labels, &v.Labels)
	return v, err
}

func (s *Store) CreateValidation(ctx context.Context, organizationID, actor, releaseID, mode string) (domain.Validation, error) {
	ctx = WithTenant(ctx, organizationID)
	v := domain.Validation{ID: uuid.NewString(), ReleaseID: releaseID, Mode: mode, Status: "QUEUED", Result: "", CreatedAt: time.Now().UTC()}
	_, err := s.Pool.Exec(ctx, `INSERT INTO validations(id,organization_id,release_id,mode,status,created_by) VALUES($1,$2,$3,$4,$5,$6)`, v.ID, organizationID, releaseID, mode, v.Status, actor)
	return v, err
}

func (s *Store) SetWorkflowID(ctx context.Context, organizationID, id, wf string) error {
	ctx = WithTenant(ctx, organizationID)
	_, err := s.Pool.Exec(ctx, "UPDATE validations SET temporal_workflow_id=$3 WHERE organization_id=$1 AND id=$2", organizationID, id, wf)
	return err
}

func (s *Store) GetValidation(ctx context.Context, organizationID, id string) (domain.Validation, error) {
	ctx = WithTenant(ctx, organizationID)
	var v domain.Validation
	err := s.Pool.QueryRow(ctx, `SELECT id::text,release_id::text,mode,status,coalesce(result,''),summary,started_at,finished_at,created_at FROM validations WHERE organization_id=$1 AND id=$2`, organizationID, id).Scan(&v.ID, &v.ReleaseID, &v.Mode, &v.Status, &v.Result, &v.Summary, &v.StartedAt, &v.FinishedAt, &v.CreatedAt)
	if err != nil {
		return v, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT c.name,c.source,c.status,c.required,c.observed,c.threshold,coalesce(c.message,'') FROM validation_checks c JOIN validations v ON v.id=c.validation_id WHERE v.organization_id=$1 AND c.validation_id=$2 ORDER BY c.name`, organizationID, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.ValidationCheck
		var observed, threshold []byte
		if err := rows.Scan(&c.Name, &c.Source, &c.Status, &c.Required, &observed, &threshold, &c.Message); err != nil {
			return v, err
		}
		_ = json.Unmarshal(observed, &c.Observed)
		_ = json.Unmarshal(threshold, &c.Threshold)
		v.Checks = append(v.Checks, c)
	}
	return v, rows.Err()
}

func (s *Store) CompleteValidation(ctx context.Context, organizationID, id, result, summary string, checks []domain.ValidationCheck) error {
	ctx = WithTenant(ctx, organizationID)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `UPDATE validations SET status='COMPLETED',result=$3,summary=$4,started_at=coalesce(started_at,$5),finished_at=$5 WHERE organization_id=$1 AND id=$2 AND cancelled_at IS NULL`, organizationID, id, result, summary, now)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	for _, c := range checks {
		if c.Source == "" {
			c.Source = "synthetic"
		}
		observed, _ := json.Marshal(c.Observed)
		threshold, _ := json.Marshal(c.Threshold)
		_, err = tx.Exec(ctx, `INSERT INTO validation_checks(validation_id,name,source,required,status,observed,threshold,message,started_at,finished_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9) ON CONFLICT(validation_id,name) DO UPDATE SET source=EXCLUDED.source,status=EXCLUDED.status,observed=EXCLUDED.observed,threshold=EXCLUDED.threshold,message=EXCLUDED.message,finished_at=EXCLUDED.finished_at`, id, c.Name, c.Source, c.Required, c.Status, observed, threshold, c.Message, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) CancelValidation(ctx context.Context, organizationID, id string) error {
	ctx = WithTenant(ctx, organizationID)
	tag, err := s.Pool.Exec(ctx, `UPDATE validations SET status='CANCELLED',result='CANCELLED',cancelled_at=now(),finished_at=now() WHERE organization_id=$1 AND id=$2 AND status NOT IN ('COMPLETED','CANCELLED')`, organizationID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return errors.New("validation cannot be cancelled")
	}
	return err
}

func (s *Store) Audit(ctx context.Context, organizationID, actor, action, typ, id, requestID, ip string, payload any) error {
	ctx = WithTenant(ctx, organizationID)
	data, _ := json.Marshal(payload)
	_, err := s.Pool.Exec(ctx, `INSERT INTO audit_events(organization_id,actor,action,resource_type,resource_id,request_id,source_ip,payload) VALUES($1,$2,$3,$4,$5,$6,nullif($7,'')::inet,$8)`, organizationID, actor, action, typ, id, requestID, ip, data)
	return err
}

func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func (s *Store) Close() { s.Pool.Close() }
func (s *Store) Health(ctx context.Context) error {
	var n int
	return s.Pool.QueryRow(ctx, "SELECT 1").Scan(&n)
}
func Wrap(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, err)
}
