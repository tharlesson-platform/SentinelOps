package database

import (
	"context"
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
	pool, err := pgxpool.New(ctx, url)
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
	_, err := s.Pool.Exec(ctx, initialMigration)
	return err
}

func (s *Store) orgID(ctx context.Context) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, "SELECT id::text FROM organizations WHERE name='local'").Scan(&id)
	return id, err
}

func (s *Store) ListServices(ctx context.Context) ([]domain.Service, error) {
	org, err := s.orgID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT id::text,name,display_name,description,owner_team,tier,labels,created_at,updated_at FROM services WHERE organization_id=$1 AND deleted_at IS NULL ORDER BY name`, org)
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

func (s *Store) GetService(ctx context.Context, name string) (domain.Service, error) {
	org, err := s.orgID(ctx)
	if err != nil {
		return domain.Service{}, err
	}
	var v domain.Service
	var labels []byte
	err = s.Pool.QueryRow(ctx, `SELECT id::text,name,display_name,description,owner_team,tier,labels,created_at,updated_at FROM services WHERE organization_id=$1 AND name=$2 AND deleted_at IS NULL`, org, name).Scan(&v.ID, &v.Name, &v.DisplayName, &v.Description, &v.OwnerTeam, &v.Tier, &labels, &v.CreatedAt, &v.UpdatedAt)
	_ = json.Unmarshal(labels, &v.Labels)
	return v, err
}

func (s *Store) UpsertService(ctx context.Context, actor string, v domain.Service) (domain.Service, error) {
	org, err := s.orgID(ctx)
	if err != nil {
		return v, err
	}
	labels, _ := json.Marshal(v.Labels)
	err = s.Pool.QueryRow(ctx, `INSERT INTO services(organization_id,name,display_name,description,owner_team,tier,labels,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT(organization_id,name) DO UPDATE SET display_name=EXCLUDED.display_name,description=EXCLUDED.description,owner_team=EXCLUDED.owner_team,tier=EXCLUDED.tier,labels=EXCLUDED.labels,updated_at=now(),version=services.version+1,deleted_at=NULL
RETURNING id::text,created_at,updated_at`, org, v.Name, v.DisplayName, v.Description, v.OwnerTeam, v.Tier, labels, actor).Scan(&v.ID, &v.CreatedAt, &v.UpdatedAt)
	return v, err
}

func (s *Store) DeleteService(ctx context.Context, name string) error {
	org, err := s.orgID(ctx)
	if err != nil {
		return err
	}
	tag, err := s.Pool.Exec(ctx, "UPDATE services SET deleted_at=now(),updated_at=now() WHERE organization_id=$1 AND name=$2 AND deleted_at IS NULL", org, name)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

func (s *Store) CreateRelease(ctx context.Context, actor, idem string, v domain.Release) (domain.Release, error) {
	org, err := s.orgID(ctx)
	if err != nil {
		return v, err
	}
	if v.ID == "" {
		v.ID = uuid.NewString()
	}
	if v.DeployedAt.IsZero() {
		v.DeployedAt = time.Now().UTC()
	}
	labels, _ := json.Marshal(v.Labels)
	err = s.Pool.QueryRow(ctx, `INSERT INTO releases(id,organization_id,service,environment,deployment_id,commit_sha,image,image_digest,version,deployed_at,pipeline,pipeline_url,actor,deployment_strategy,cluster,namespace,labels,idempotency_key,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
ON CONFLICT(organization_id,idempotency_key) DO UPDATE SET idempotency_key=EXCLUDED.idempotency_key RETURNING id::text,created_at`, v.ID, org, v.Service, v.Environment, v.DeploymentID, v.CommitSHA, v.Image, v.ImageDigest, v.Version, v.DeployedAt, v.Pipeline, v.PipelineURL, v.Actor, v.DeploymentStrategy, v.Cluster, v.Namespace, labels, idem, actor).Scan(&v.ID, &v.CreatedAt)
	return v, err
}

func (s *Store) GetRelease(ctx context.Context, id string) (domain.Release, error) {
	org, err := s.orgID(ctx)
	if err != nil {
		return domain.Release{}, err
	}
	var v domain.Release
	var labels []byte
	err = s.Pool.QueryRow(ctx, `SELECT id::text,service,environment,coalesce(deployment_id,''),coalesce(commit_sha,''),coalesce(image,''),coalesce(image_digest,''),version,deployed_at,coalesce(pipeline,''),coalesce(pipeline_url,''),coalesce(actor,''),coalesce(deployment_strategy,''),coalesce(cluster,''),coalesce(namespace,''),labels,created_at FROM releases WHERE organization_id=$1 AND id=$2`, org, id).Scan(&v.ID, &v.Service, &v.Environment, &v.DeploymentID, &v.CommitSHA, &v.Image, &v.ImageDigest, &v.Version, &v.DeployedAt, &v.Pipeline, &v.PipelineURL, &v.Actor, &v.DeploymentStrategy, &v.Cluster, &v.Namespace, &labels, &v.CreatedAt)
	_ = json.Unmarshal(labels, &v.Labels)
	return v, err
}

func (s *Store) CreateValidation(ctx context.Context, actor, releaseID, mode string) (domain.Validation, error) {
	org, err := s.orgID(ctx)
	if err != nil {
		return domain.Validation{}, err
	}
	v := domain.Validation{ID: uuid.NewString(), ReleaseID: releaseID, Mode: mode, Status: "QUEUED", Result: "", CreatedAt: time.Now().UTC()}
	_, err = s.Pool.Exec(ctx, `INSERT INTO validations(id,organization_id,release_id,mode,status,created_by) VALUES($1,$2,$3,$4,$5,$6)`, v.ID, org, releaseID, mode, v.Status, actor)
	return v, err
}

func (s *Store) SetWorkflowID(ctx context.Context, id, wf string) error {
	_, err := s.Pool.Exec(ctx, "UPDATE validations SET temporal_workflow_id=$2 WHERE id=$1", id, wf)
	return err
}

func (s *Store) GetValidation(ctx context.Context, id string) (domain.Validation, error) {
	org, err := s.orgID(ctx)
	if err != nil {
		return domain.Validation{}, err
	}
	var v domain.Validation
	err = s.Pool.QueryRow(ctx, `SELECT id::text,release_id::text,mode,status,coalesce(result,''),summary,started_at,finished_at,created_at FROM validations WHERE organization_id=$1 AND id=$2`, org, id).Scan(&v.ID, &v.ReleaseID, &v.Mode, &v.Status, &v.Result, &v.Summary, &v.StartedAt, &v.FinishedAt, &v.CreatedAt)
	if err != nil {
		return v, err
	}
	rows, err := s.Pool.Query(ctx, `SELECT name,status,required,observed,threshold,coalesce(message,'') FROM validation_checks WHERE validation_id=$1 ORDER BY name`, id)
	if err != nil {
		return v, err
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.ValidationCheck
		var observed, threshold []byte
		if err := rows.Scan(&c.Name, &c.Status, &c.Required, &observed, &threshold, &c.Message); err != nil {
			return v, err
		}
		_ = json.Unmarshal(observed, &c.Observed)
		_ = json.Unmarshal(threshold, &c.Threshold)
		v.Checks = append(v.Checks, c)
	}
	return v, rows.Err()
}

func (s *Store) CompleteValidation(ctx context.Context, id, result, summary string, checks []domain.ValidationCheck) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `UPDATE validations SET status='COMPLETED',result=$2,summary=$3,started_at=coalesce(started_at,$4),finished_at=$4 WHERE id=$1 AND cancelled_at IS NULL`, id, result, summary, now)
	if err != nil {
		return err
	}
	for _, c := range checks {
		observed, _ := json.Marshal(c.Observed)
		threshold, _ := json.Marshal(c.Threshold)
		_, err = tx.Exec(ctx, `INSERT INTO validation_checks(validation_id,name,source,required,status,observed,threshold,message,started_at,finished_at) VALUES($1,$2,'synthetic',$3,$4,$5,$6,$7,$8,$8) ON CONFLICT(validation_id,name) DO UPDATE SET status=EXCLUDED.status,observed=EXCLUDED.observed,threshold=EXCLUDED.threshold,message=EXCLUDED.message,finished_at=EXCLUDED.finished_at`, id, c.Name, c.Required, c.Status, observed, threshold, c.Message, now)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) CancelValidation(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx, `UPDATE validations SET status='CANCELLED',result='CANCELLED',cancelled_at=now(),finished_at=now() WHERE id=$1 AND status NOT IN ('COMPLETED','CANCELLED')`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return errors.New("validation cannot be cancelled")
	}
	return err
}

func (s *Store) Audit(ctx context.Context, actor, action, typ, id, requestID, ip string, payload any) error {
	org, err := s.orgID(ctx)
	if err != nil {
		return err
	}
	data, _ := json.Marshal(payload)
	_, err = s.Pool.Exec(ctx, `INSERT INTO audit_events(organization_id,actor,action,resource_type,resource_id,request_id,source_ip,payload) VALUES($1,$2,$3,$4,$5,$6,nullif($7,'')::inet,$8)`, org, actor, action, typ, id, requestID, ip, data)
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
