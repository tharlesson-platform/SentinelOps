package database

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sentinelops/sentinelops/internal/domain"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Store struct{ Pool *pgxpool.Pool }

type AssetSearchResult struct {
	Items      []domain.Asset
	NextCursor string
}

var quotaScopeName = regexp.MustCompile(`^[a-z][a-z0-9-]{1,62}$`)

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
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
  version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now()
)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("list embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || path.Ext(entry.Name()) != ".sql" {
			continue
		}
		version := entry.Name()
		contents, readErr := migrationFiles.ReadFile(path.Join("migrations", version))
		if readErr != nil {
			return fmt.Errorf("read migration %s: %w", version, readErr)
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(contents))
		var appliedChecksum string
		err = tx.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", version).Scan(&appliedChecksum)
		if err == nil {
			if appliedChecksum != checksum {
				return fmt.Errorf("migration %s checksum changed after application", version)
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read migration ledger for %s: %w", version, err)
		}
		if _, err = tx.Exec(ctx, string(contents)); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)", version, checksum); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
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

// ConsumeTenantRequestQuota increments one organization-scoped minute window
// atomically. PostgreSQL serializes the conflicting row update, so independent
// API replicas cannot each admit a separate full quota.
func (s *Store) ConsumeTenantRequestQuota(ctx context.Context, organizationID string, limit int) (bool, error) {
	if organizationID == "" || limit < 1 {
		return false, errors.New("organization ID and positive request quota are required")
	}
	ctx = WithTenant(ctx, organizationID)
	var accepted bool
	err := s.Pool.QueryRow(ctx, `WITH pruned AS (
  DELETE FROM tenant_request_windows
  WHERE organization_id=$1
    AND window_started < date_trunc('minute', clock_timestamp()) - interval '2 hours'
)
INSERT INTO tenant_request_windows(organization_id,window_started,request_count)
VALUES($1,date_trunc('minute', clock_timestamp()),1)
ON CONFLICT(organization_id,window_started) DO UPDATE
SET request_count=tenant_request_windows.request_count+1
WHERE tenant_request_windows.request_count < $2
RETURNING true`, organizationID, limit).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return accepted, err
}

// ConsumeTenantScopedRequestQuota applies an additional, durable limit to a
// bounded class of expensive requests. Scope names are fixed by the server,
// not supplied by callers, and the database row is shared by API replicas.
func (s *Store) ConsumeTenantScopedRequestQuota(ctx context.Context, organizationID, scope string, limit int) (bool, error) {
	if organizationID == "" || !quotaScopeName.MatchString(scope) || limit < 1 {
		return false, errors.New("organization ID, quota scope and positive request quota are required")
	}
	ctx = WithTenant(ctx, organizationID)
	var accepted bool
	err := s.Pool.QueryRow(ctx, `WITH pruned AS (
  DELETE FROM tenant_scoped_request_windows
  WHERE organization_id=$1
    AND window_started < date_trunc('minute', clock_timestamp()) - interval '2 hours'
)
INSERT INTO tenant_scoped_request_windows(organization_id,scope,window_started,request_count)
VALUES($1,$2,date_trunc('minute', clock_timestamp()),1)
ON CONFLICT(organization_id,scope,window_started) DO UPDATE
SET request_count=tenant_scoped_request_windows.request_count+1
WHERE tenant_scoped_request_windows.request_count < $3
RETURNING true`, organizationID, scope, limit).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return accepted, err
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

func (s *Store) RevokeAgent(ctx context.Context, organizationID, id string) error {
	ctx = WithTenant(ctx, organizationID)
	tag, err := s.Pool.Exec(ctx, `UPDATE agents SET revoked_at=coalesce(revoked_at,now()),updated_at=now(),version=version+1
WHERE organization_id=$1 AND id=$2`, organizationID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
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

func (s *Store) ListAssets(ctx context.Context, organizationID string) ([]domain.Asset, error) {
	ctx = WithTenant(ctx, organizationID)
	rows, err := s.Pool.Query(ctx, `SELECT id::text,asset_id,name,kind,site,owner_team,environment,lifecycle,source,last_seen,labels,created_at,updated_at
FROM assets WHERE organization_id=$1 ORDER BY lifecycle,name,asset_id`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Asset{}
	for rows.Next() {
		var item domain.Asset
		var labels []byte
		if err := rows.Scan(&item.ID, &item.AssetID, &item.Name, &item.Kind, &item.Site, &item.OwnerTeam, &item.Environment, &item.Lifecycle, &item.Source, &item.LastSeen, &labels, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(labels, &item.Labels); err != nil {
			return nil, fmt.Errorf("decode asset labels: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// SearchAssets provides a bounded, tenant-scoped catalog query. The cursor is
// the last stable asset ID, so it stays valid when a display name changes.
func (s *Store) SearchAssets(ctx context.Context, organizationID, query, cursor string, limit int) (AssetSearchResult, error) {
	ctx = WithTenant(ctx, organizationID)
	if limit < 1 || limit > 100 {
		return AssetSearchResult{}, errors.New("asset search limit must be between 1 and 100")
	}
	rows, err := s.Pool.Query(ctx, `SELECT id::text,asset_id,name,kind,site,owner_team,environment,lifecycle,source,last_seen,labels,created_at,updated_at
FROM assets
WHERE organization_id=$1
  AND asset_id > $2
  AND ($3='' OR concat_ws(' ',asset_id,name,kind,site,owner_team,environment,source) ILIKE '%' || $3 || '%')
ORDER BY asset_id
LIMIT $4`, organizationID, cursor, query, limit+1)
	if err != nil {
		return AssetSearchResult{}, err
	}
	defer rows.Close()
	result := AssetSearchResult{Items: make([]domain.Asset, 0, limit)}
	for rows.Next() {
		var item domain.Asset
		var labels []byte
		if err := rows.Scan(&item.ID, &item.AssetID, &item.Name, &item.Kind, &item.Site, &item.OwnerTeam, &item.Environment, &item.Lifecycle, &item.Source, &item.LastSeen, &labels, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return AssetSearchResult{}, err
		}
		if err := json.Unmarshal(labels, &item.Labels); err != nil {
			return AssetSearchResult{}, fmt.Errorf("decode asset labels: %w", err)
		}
		if len(result.Items) == limit {
			result.NextCursor = result.Items[len(result.Items)-1].AssetID
			break
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return AssetSearchResult{}, err
	}
	return result, nil
}

func (s *Store) GetAsset(ctx context.Context, organizationID, assetID string) (domain.Asset, error) {
	ctx = WithTenant(ctx, organizationID)
	var item domain.Asset
	var labels []byte
	err := s.Pool.QueryRow(ctx, `SELECT id::text,asset_id,name,kind,site,owner_team,environment,lifecycle,source,last_seen,labels,created_at,updated_at
FROM assets WHERE organization_id=$1 AND asset_id=$2`, organizationID, assetID).Scan(
		&item.ID, &item.AssetID, &item.Name, &item.Kind, &item.Site, &item.OwnerTeam, &item.Environment, &item.Lifecycle, &item.Source, &item.LastSeen, &labels, &item.CreatedAt, &item.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(labels, &item.Labels)
	}
	return item, err
}

func (s *Store) UpsertAsset(ctx context.Context, organizationID, actor string, item domain.Asset) (domain.Asset, error) {
	ctx = WithTenant(ctx, organizationID)
	if item.Labels == nil {
		item.Labels = map[string]string{}
	}
	labels, err := json.Marshal(item.Labels)
	if err != nil {
		return domain.Asset{}, fmt.Errorf("encode asset labels: %w", err)
	}
	err = s.Pool.QueryRow(ctx, `INSERT INTO assets(organization_id,asset_id,name,kind,site,owner_team,environment,lifecycle,source,last_seen,labels,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT(organization_id,asset_id) DO UPDATE SET
 name=EXCLUDED.name,kind=EXCLUDED.kind,site=EXCLUDED.site,owner_team=EXCLUDED.owner_team,
 environment=EXCLUDED.environment,lifecycle=EXCLUDED.lifecycle,source=EXCLUDED.source,
 last_seen=EXCLUDED.last_seen,labels=EXCLUDED.labels,updated_at=now(),version=assets.version+1
RETURNING id::text,created_at,updated_at`,
		organizationID, item.AssetID, item.Name, item.Kind, item.Site, item.OwnerTeam, item.Environment, item.Lifecycle, item.Source, item.LastSeen, labels, actor).
		Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

// ReconcileAssets applies a complete, read-only discovery snapshot from one
// source. Missing formerly active assets become stale only after the snapshot
// itself has been atomically persisted.
func (s *Store) ReconcileAssets(ctx context.Context, organizationID, actor, source string, observedAt time.Time, items []domain.Asset) ([]domain.Asset, error) {
	ctx = WithTenant(ctx, organizationID)
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	result := make([]domain.Asset, 0, len(items))
	assetIDs := make([]string, 0, len(items))
	for _, item := range items {
		item.Source, item.LastSeen = source, &observedAt
		if item.Lifecycle == "" {
			item.Lifecycle = "active"
		}
		if item.Labels == nil {
			item.Labels = map[string]string{}
		}
		labels, err := json.Marshal(item.Labels)
		if err != nil {
			return nil, fmt.Errorf("encode asset labels: %w", err)
		}
		err = tx.QueryRow(ctx, `INSERT INTO assets(organization_id,asset_id,name,kind,site,owner_team,environment,lifecycle,source,last_seen,labels,created_by)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
ON CONFLICT(organization_id,asset_id) DO UPDATE SET
 name=EXCLUDED.name,kind=EXCLUDED.kind,site=EXCLUDED.site,owner_team=EXCLUDED.owner_team,
 environment=EXCLUDED.environment,lifecycle=EXCLUDED.lifecycle,source=EXCLUDED.source,
 last_seen=EXCLUDED.last_seen,labels=EXCLUDED.labels,updated_at=now(),version=assets.version+1
RETURNING id::text,created_at,updated_at`,
			organizationID, item.AssetID, item.Name, item.Kind, item.Site, item.OwnerTeam, item.Environment, item.Lifecycle, item.Source, item.LastSeen, labels, actor).
			Scan(&item.ID, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			return nil, err
		}
		assetIDs = append(assetIDs, item.AssetID)
		result = append(result, item)
	}
	if len(assetIDs) > 0 {
		_, err = tx.Exec(ctx, `UPDATE assets SET lifecycle='stale',updated_at=now(),version=version+1
WHERE organization_id=$1 AND source=$2 AND lifecycle='active' AND asset_id <> ALL($3::text[])`, organizationID, source, assetIDs)
	} else {
		_, err = tx.Exec(ctx, `UPDATE assets SET lifecycle='stale',updated_at=now(),version=version+1
WHERE organization_id=$1 AND source=$2 AND lifecycle='active'`, organizationID, source)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
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

func scanDataLifecycleRequest(row pgx.Row, item *domain.DataLifecycleRequest) error {
	var scope, evidence []byte
	err := row.Scan(&item.ID, &item.RequestType, &item.Status, &item.RequestedBy, &item.ApprovedBy, &item.Reason, &scope, &evidence, &item.CreatedAt, &item.UpdatedAt, &item.CompletedAt)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(scope, &item.Scope); err != nil {
		return fmt.Errorf("decode data lifecycle scope: %w", err)
	}
	if err := json.Unmarshal(evidence, &item.Evidence); err != nil {
		return fmt.Errorf("decode data lifecycle evidence: %w", err)
	}
	return nil
}

// CreateDataLifecycleRequest persists the request and its audit record in the
// same transaction. It deliberately does not initiate an export or erasure.
func (s *Store) CreateDataLifecycleRequest(ctx context.Context, organizationID, actor, requestID, sourceIP string, item domain.DataLifecycleRequest) (domain.DataLifecycleRequest, error) {
	ctx = WithTenant(ctx, organizationID)
	scope, err := json.Marshal(item.Scope)
	if err != nil {
		return domain.DataLifecycleRequest{}, fmt.Errorf("encode data lifecycle scope: %w", err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	item.Status, item.RequestedBy = "requested", actor
	err = scanDataLifecycleRequest(tx.QueryRow(ctx, `INSERT INTO data_lifecycle_requests(organization_id,request_type,status,requested_by,reason,scope)
VALUES($1,$2,$3,$4,$5,$6)
RETURNING id::text,request_type,status,requested_by,coalesce(approved_by,''),reason,scope,evidence,created_at,updated_at,completed_at`, organizationID, item.RequestType, item.Status, item.RequestedBy, item.Reason, scope), &item)
	if err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	auditPayload, _ := json.Marshal(map[string]any{"requestType": item.RequestType, "scope": item.Scope, "status": item.Status})
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(organization_id,actor,action,resource_type,resource_id,request_id,source_ip,payload)
VALUES($1,$2,'data-lifecycle.request','data-lifecycle-request',$3,$4,nullif($5,'')::inet,$6)`, organizationID, actor, item.ID, requestID, sourceIP, auditPayload); err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	return item, nil
}

func (s *Store) ListDataLifecycleRequests(ctx context.Context, organizationID string) ([]domain.DataLifecycleRequest, error) {
	ctx = WithTenant(ctx, organizationID)
	rows, err := s.Pool.Query(ctx, `SELECT id::text,request_type,status,requested_by,coalesce(approved_by,''),reason,scope,evidence,created_at,updated_at,completed_at
FROM data_lifecycle_requests WHERE organization_id=$1 ORDER BY created_at DESC,id DESC`, organizationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DataLifecycleRequest{}
	for rows.Next() {
		var item domain.DataLifecycleRequest
		if err := scanDataLifecycleRequest(rows, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ApproveDataLifecycleRequest enforces the four-eyes rule atomically. The
// approved state remains intentionally non-destructive until a fulfilment
// component with an explicit target/data-retention contract is deployed.
func (s *Store) ApproveDataLifecycleRequest(ctx context.Context, organizationID, actor, id, requestID, sourceIP string) (domain.DataLifecycleRequest, error) {
	ctx = WithTenant(ctx, organizationID)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item domain.DataLifecycleRequest
	err = scanDataLifecycleRequest(tx.QueryRow(ctx, `UPDATE data_lifecycle_requests
SET status='approved',approved_by=$3,updated_at=now(),evidence=jsonb_set(evidence,'{approval}',jsonb_build_object('approvedAt',to_jsonb(now()),'approvedBy',$3),true)
WHERE organization_id=$1 AND id=$2 AND status='requested' AND requested_by <> $3
RETURNING id::text,request_type,status,requested_by,coalesce(approved_by,''),reason,scope,evidence,created_at,updated_at,completed_at`, organizationID, id, actor), &item)
	if err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	auditPayload, _ := json.Marshal(map[string]any{"requestType": item.RequestType, "scope": item.Scope, "status": item.Status, "requestedBy": item.RequestedBy})
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(organization_id,actor,action,resource_type,resource_id,request_id,source_ip,payload)
VALUES($1,$2,'data-lifecycle.approve','data-lifecycle-request',$3,$4,nullif($5,'')::inet,$6)`, organizationID, actor, item.ID, requestID, sourceIP, auditPayload); err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	return item, nil
}

// ExecuteControlPlaneErasure is intentionally restricted to the one data
// domain whose storage contract is implemented here: identity profile fields
// and active RBAC bindings in the control-plane database. Every other domain
// fails closed until its own exporter/eraser has an approved contract.
func (s *Store) ExecuteControlPlaneErasure(ctx context.Context, organizationID, actor, id, requestID, sourceIP string) (domain.DataLifecycleRequest, error) {
	ctx = WithTenant(ctx, organizationID)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var item domain.DataLifecycleRequest
	err = scanDataLifecycleRequest(tx.QueryRow(ctx, `SELECT id::text,request_type,status,requested_by,coalesce(approved_by,''),reason,scope,evidence,created_at,updated_at,completed_at
FROM data_lifecycle_requests WHERE organization_id=$1 AND id=$2 FOR UPDATE`, organizationID, id), &item)
	if err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	subject, supported := controlPlaneIdentityScope(item.Scope)
	if item.RequestType != "erasure" || item.Status != "approved" || !supported {
		return domain.DataLifecycleRequest{}, pgx.ErrNoRows
	}
	digest := sha256.Sum256([]byte(organizationID + "\x00" + subject))
	erasedSubject := fmt.Sprintf("erased:%x", digest[:12])
	bindings, err := tx.Exec(ctx, `DELETE FROM role_bindings WHERE organization_id=$1 AND subject=$2`, organizationID, subject)
	if err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	profiles, err := tx.Exec(ctx, `UPDATE users SET subject=$3,email=NULL,display_name=NULL,disabled_at=coalesce(disabled_at,now()),updated_at=now(),version=version+1
WHERE organization_id=$1 AND subject=$2`, organizationID, subject, erasedSubject)
	if err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	executionEvidence, _ := json.Marshal(map[string]any{
		"mode":              "control-plane-identity",
		"profileRows":       profiles.RowsAffected(),
		"roleBindingRows":   bindings.RowsAffected(),
		"subjectRedacted":   true,
		"externalDomains":   false,
		"executionContract": "v1",
	})
	err = scanDataLifecycleRequest(tx.QueryRow(ctx, `UPDATE data_lifecycle_requests
SET status='completed',updated_at=now(),completed_at=now(),evidence=jsonb_set(evidence,'{execution}',$3::jsonb,true)
WHERE organization_id=$1 AND id=$2
RETURNING id::text,request_type,status,requested_by,coalesce(approved_by,''),reason,scope,evidence,created_at,updated_at,completed_at`, organizationID, id, executionEvidence), &item)
	if err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	auditPayload, _ := json.Marshal(map[string]any{"requestType": item.RequestType, "status": item.Status, "execution": json.RawMessage(executionEvidence)})
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(organization_id,actor,action,resource_type,resource_id,request_id,source_ip,payload)
VALUES($1,$2,'data-lifecycle.execute','data-lifecycle-request',$3,$4,nullif($5,'')::inet,$6)`, organizationID, actor, item.ID, requestID, sourceIP, auditPayload); err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return domain.DataLifecycleRequest{}, err
	}
	return item, nil
}

func controlPlaneIdentityScope(scope map[string]any) (string, bool) {
	subject, ok := scope["subjectRef"].(string)
	if !ok || subject == "" {
		return "", false
	}
	domains, ok := scope["domains"].([]any)
	if !ok || len(domains) != 1 {
		return "", false
	}
	domain, ok := domains[0].(string)
	return subject, ok && domain == "control-plane-metadata"
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
