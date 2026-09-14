package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/sentinelops/sentinelops/internal/domain"
)

func TestPostgresRowLevelTenantIsolation(t *testing.T) {
	migrationURL := os.Getenv("SENTINELOPS_TEST_DATABASE_MIGRATION_URL")
	runtimeURL := os.Getenv("SENTINELOPS_TEST_DATABASE_URL")
	if migrationURL == "" || runtimeURL == "" {
		t.Skip("set SENTINELOPS_TEST_DATABASE_MIGRATION_URL and SENTINELOPS_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	migrationStore, err := Open(ctx, migrationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer migrationStore.Close()
	if err := migrationStore.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	runtimeStore, err := Open(ctx, runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()

	orgA, orgB := uuid.NewString(), uuid.NewString()
	serviceA, serviceB := uuid.NewString(), uuid.NewString()
	assetA, assetB := uuid.NewString(), uuid.NewString()
	agentA := uuid.NewString()
	nameA, nameB := "rls-a-"+orgA, "rls-b-"+orgB
	adminCtx := withMigrationTenant(ctx)
	_, err = migrationStore.Pool.Exec(adminCtx, "INSERT INTO organizations(id,name) VALUES($1,$2),($3,$4)", orgA, nameA, orgB, nameB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM audit_events WHERE organization_id=ANY($1::uuid[])", []string{orgA, orgB})
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM data_lifecycle_requests WHERE organization_id=ANY($1::uuid[])", []string{orgA, orgB})
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM tenant_request_windows WHERE organization_id=ANY($1::uuid[])", []string{orgA, orgB})
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM tenant_scoped_request_windows WHERE organization_id=ANY($1::uuid[])", []string{orgA, orgB})
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM role_bindings WHERE organization_id=ANY($1::uuid[])", []string{orgA, orgB})
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM users WHERE organization_id=ANY($1::uuid[])", []string{orgA, orgB})
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM agents WHERE id=$1", agentA)
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM assets WHERE id=ANY($1::uuid[])", []string{assetA, assetB})
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM services WHERE id=ANY($1::uuid[])", []string{serviceA, serviceB})
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM organizations WHERE id=ANY($1::uuid[])", []string{orgA, orgB})
	}()
	_, err = migrationStore.Pool.Exec(adminCtx, `INSERT INTO services(id,organization_id,name,display_name,owner_team,created_by)
VALUES($1,$2,'service-a','Service A','team-a','rls-test'),($3,$4,'service-b','Service B','team-b','rls-test')`,
		serviceA, orgA, serviceB, orgB)
	if err != nil {
		t.Fatal(err)
	}
	_, err = migrationStore.Pool.Exec(adminCtx, `INSERT INTO users(organization_id,subject,email,display_name,created_by)
VALUES($1,'user-a','user-a@example.invalid','User A','rls-test')`, orgA)
	if err != nil {
		t.Fatal(err)
	}
	_, err = migrationStore.Pool.Exec(adminCtx, `INSERT INTO agents(id,organization_id,name,token_hash)
VALUES($1,$2,'agent-a',decode('00','hex'))`, agentA, orgA)
	if err != nil {
		t.Fatal(err)
	}
	_, err = migrationStore.Pool.Exec(adminCtx, `INSERT INTO assets(id,organization_id,asset_id,name,kind,source,created_by)
VALUES($1,$2,'vm:asset-a','Asset A','vm','test','rls-test'),($3,$4,'vm:asset-b','Asset B','vm','test','rls-test')`,
		assetA, orgA, assetB, orgB)
	if err != nil {
		t.Fatal(err)
	}

	var superuser, bypassRLS bool
	if err := runtimeStore.Pool.QueryRow(ctx, "SELECT rolsuper,rolbypassrls FROM pg_roles WHERE rolname=current_user").Scan(&superuser, &bypassRLS); err != nil {
		t.Fatal(err)
	}
	if superuser || bypassRLS {
		t.Fatal("runtime database role must not be a superuser or BYPASSRLS")
	}
	for attempt := 0; attempt < 2; attempt++ {
		accepted, quotaErr := runtimeStore.ConsumeTenantRequestQuota(ctx, orgA, 2)
		if quotaErr != nil || !accepted {
			t.Fatalf("tenant A quota attempt %d: accepted=%v err=%v", attempt, accepted, quotaErr)
		}
	}
	accepted, quotaErr := runtimeStore.ConsumeTenantRequestQuota(ctx, orgA, 2)
	if quotaErr != nil || accepted {
		t.Fatalf("tenant A quota should reject third request: accepted=%v err=%v", accepted, quotaErr)
	}
	accepted, quotaErr = runtimeStore.ConsumeTenantRequestQuota(ctx, orgB, 2)
	if quotaErr != nil || !accepted {
		t.Fatalf("tenant B must not be exhausted by tenant A: accepted=%v err=%v", accepted, quotaErr)
	}
	var quotaRows int
	if err := runtimeStore.Pool.QueryRow(WithTenant(ctx, orgB), "SELECT count(*) FROM tenant_request_windows").Scan(&quotaRows); err != nil {
		t.Fatal(err)
	}
	if quotaRows != 1 {
		t.Fatalf("tenant B quota rows=%d, want 1", quotaRows)
	}
	for attempt := 0; attempt < 2; attempt++ {
		accepted, quotaErr := runtimeStore.ConsumeTenantScopedRequestQuota(ctx, orgA, "catalog-query", 2)
		if quotaErr != nil || !accepted {
			t.Fatalf("tenant A catalog quota attempt %d: accepted=%v err=%v", attempt, accepted, quotaErr)
		}
	}
	accepted, quotaErr = runtimeStore.ConsumeTenantScopedRequestQuota(ctx, orgA, "catalog-query", 2)
	if quotaErr != nil || accepted {
		t.Fatalf("tenant A catalog quota should reject third request: accepted=%v err=%v", accepted, quotaErr)
	}
	accepted, quotaErr = runtimeStore.ConsumeTenantScopedRequestQuota(ctx, orgB, "catalog-query", 2)
	if quotaErr != nil || !accepted {
		t.Fatalf("tenant B catalog quota must not be exhausted by tenant A: accepted=%v err=%v", accepted, quotaErr)
	}
	var scopedQuotaRows int
	if err := runtimeStore.Pool.QueryRow(WithTenant(ctx, orgB), "SELECT count(*) FROM tenant_scoped_request_windows WHERE scope='catalog-query'").Scan(&scopedQuotaRows); err != nil {
		t.Fatal(err)
	}
	if scopedQuotaRows != 1 {
		t.Fatalf("tenant B catalog quota rows=%d, want 1", scopedQuotaRows)
	}
	lifecycle, err := runtimeStore.CreateDataLifecycleRequest(ctx, orgA, "requester-a", "rls-lifecycle-create", "127.0.0.1", domain.DataLifecycleRequest{
		RequestType: "erasure",
		Reason:      "encerramento de conta",
		Scope:       map[string]any{"subjectRef": "user-a", "domains": []string{"control-plane-metadata"}},
	})
	if err != nil || lifecycle.Status != "requested" || lifecycle.RequestedBy != "requester-a" {
		t.Fatalf("create tenant A lifecycle request: item=%#v err=%v", lifecycle, err)
	}
	itemsB, err := runtimeStore.ListDataLifecycleRequests(ctx, orgB)
	if err != nil || len(itemsB) != 0 {
		t.Fatalf("tenant B lifecycle visibility: items=%#v err=%v", itemsB, err)
	}
	if _, err = runtimeStore.ApproveDataLifecycleRequest(ctx, orgA, "requester-a", lifecycle.ID, "rls-lifecycle-self", "127.0.0.1"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("requester must not self-approve: err=%v", err)
	}
	lifecycle, err = runtimeStore.ApproveDataLifecycleRequest(ctx, orgA, "approver-a", lifecycle.ID, "rls-lifecycle-approve", "127.0.0.1")
	if err != nil || lifecycle.Status != "approved" || lifecycle.ApprovedBy != "approver-a" || lifecycle.Evidence["approval"] == nil {
		t.Fatalf("approve tenant A lifecycle request: item=%#v err=%v", lifecycle, err)
	}
	lifecycle, err = runtimeStore.ExecuteControlPlaneErasure(ctx, orgA, "executor-a", lifecycle.ID, "rls-lifecycle-execute", "127.0.0.1")
	if err != nil || lifecycle.Status != "completed" || lifecycle.Evidence["execution"] == nil {
		t.Fatalf("execute tenant A lifecycle erasure: item=%#v err=%v", lifecycle, err)
	}
	var redactedProfiles, originalProfiles int
	if err := runtimeStore.Pool.QueryRow(WithTenant(ctx, orgA), `SELECT count(*) FROM users WHERE organization_id=$1 AND subject LIKE 'erased:%' AND email IS NULL AND display_name IS NULL AND disabled_at IS NOT NULL`, orgA).Scan(&redactedProfiles); err != nil {
		t.Fatal(err)
	}
	if err := runtimeStore.Pool.QueryRow(WithTenant(ctx, orgA), "SELECT count(*) FROM users WHERE organization_id=$1 AND subject='user-a'", orgA).Scan(&originalProfiles); err != nil {
		t.Fatal(err)
	}
	if redactedProfiles != 1 || originalProfiles != 0 {
		t.Fatalf("control plane identity was not erased: redacted=%d original=%d", redactedProfiles, originalProfiles)
	}
	if _, err = runtimeStore.ExecuteControlPlaneErasure(ctx, orgA, "executor-a", lifecycle.ID, "rls-lifecycle-execute-repeat", "127.0.0.1"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("completed lifecycle request must not execute twice: err=%v", err)
	}
	itemsA, err := runtimeStore.ListDataLifecycleRequests(ctx, orgA)
	if err != nil || len(itemsA) != 1 || itemsA[0].ID != lifecycle.ID {
		t.Fatalf("tenant A lifecycle listing: items=%#v err=%v", itemsA, err)
	}

	items, err := runtimeStore.ListServices(ctx, orgA)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "service-a" {
		t.Fatalf("tenant A received unexpected services: %#v", items)
	}
	assets, err := runtimeStore.ListAssets(ctx, orgA)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].AssetID != "vm:asset-a" {
		t.Fatalf("tenant A received unexpected assets: %#v", assets)
	}
	updated, err := runtimeStore.UpsertAsset(ctx, orgA, "rls-test", domain.Asset{AssetID: "vm:asset-a", Name: "Asset A Renomeado", Kind: "vm", Source: "test", Lifecycle: "active"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != assets[0].ID || updated.Name != "Asset A Renomeado" {
		t.Fatalf("asset upsert must preserve identity on rename: before=%#v after=%#v", assets[0], updated)
	}
	firstSnapshot, err := runtimeStore.ReconcileAssets(ctx, orgA, "rls-test", "vmware", time.Now().UTC(), []domain.Asset{
		{AssetID: "vm:reconcile-a", Name: "VM A", Kind: "vm"},
		{AssetID: "vm:reconcile-b", Name: "VM B", Kind: "vm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	secondSnapshot, err := runtimeStore.ReconcileAssets(ctx, orgA, "rls-test", "vmware", time.Now().UTC(), []domain.Asset{
		{AssetID: "vm:reconcile-a", Name: "VM A Renomeada", Kind: "vm"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstSnapshot) != 2 || len(secondSnapshot) != 1 || firstSnapshot[0].ID != secondSnapshot[0].ID {
		t.Fatalf("complete reconciliation must preserve stable identity: first=%#v second=%#v", firstSnapshot, secondSnapshot)
	}
	assets, err = runtimeStore.ListAssets(ctx, orgA)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, asset := range assets {
		states[asset.AssetID] = asset.Lifecycle
	}
	if states["vm:reconcile-a"] != "active" || states["vm:reconcile-b"] != "stale" {
		t.Fatalf("reconciliation states=%#v, want active/stale", states)
	}
	firstPage, err := runtimeStore.SearchAssets(ctx, orgA, "VM", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Items) != 1 || firstPage.NextCursor == "" {
		t.Fatalf("asset search must return bounded first page and cursor: %#v", firstPage)
	}
	secondPage, err := runtimeStore.SearchAssets(ctx, orgA, "VM", firstPage.NextCursor, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Items) != 1 || secondPage.Items[0].AssetID == firstPage.Items[0].AssetID {
		t.Fatalf("asset search cursor did not advance: first=%#v second=%#v", firstPage, secondPage)
	}
	crossTenantSearch, err := runtimeStore.SearchAssets(ctx, orgA, "Asset B", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(crossTenantSearch.Items) != 0 {
		t.Fatalf("asset search exposed tenant B: %#v", crossTenantSearch)
	}
	if err := runtimeStore.RevokeAgent(ctx, orgA, agentA); err != nil {
		t.Fatal(err)
	}
	var revokedAt *time.Time
	if err := runtimeStore.Pool.QueryRow(WithTenant(ctx, orgA), "SELECT revoked_at FROM agents WHERE id=$1", agentA).Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	if revokedAt == nil {
		t.Fatal("revoked agent must retain a revocation timestamp")
	}
	if err := runtimeStore.RevokeAgent(ctx, orgB, agentA); !IsNotFound(err) {
		t.Fatalf("cross-tenant revoke error=%v, want not found", err)
	}

	var count int
	if err := runtimeStore.Pool.QueryRow(ctx, "SELECT count(*) FROM services WHERE id=ANY($1::uuid[])", []string{serviceA, serviceB}).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("empty tenant context exposed %d rows", count)
	}
	if err := runtimeStore.Pool.QueryRow(withMigrationTenant(ctx), "SELECT count(*) FROM services WHERE id=ANY($1::uuid[])", []string{serviceA, serviceB}).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("runtime role forged migration context and exposed %d rows", count)
	}

	tenantACtx := WithTenant(ctx, orgA)
	if _, err := runtimeStore.Pool.Exec(tenantACtx, `INSERT INTO services(organization_id,name,display_name,owner_team,created_by) VALUES($1,'forbidden','Forbidden','x','rls-test')`, orgB); err == nil {
		t.Fatal("cross-tenant direct insert was allowed")
	}
	if _, err := runtimeStore.Pool.Exec(tenantACtx, `INSERT INTO environments(service_id,name) VALUES($1,'forbidden')`, serviceB); err == nil {
		t.Fatal("cross-tenant child insert was allowed")
	}
	if _, err := runtimeStore.Pool.Exec(tenantACtx, `INSERT INTO assets(organization_id,asset_id,name,kind,source,created_by) VALUES($1,'forbidden','Forbidden','vm','test','rls-test')`, orgB); err == nil {
		t.Fatal("cross-tenant asset insert was allowed")
	}

	tenantBCtx := WithTenant(ctx, orgB)
	if err := runtimeStore.Pool.QueryRow(tenantBCtx, "SELECT count(*) FROM services WHERE id=ANY($1::uuid[])", []string{serviceA, serviceB}).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tenant B expected exactly one row, got %d", count)
	}
}
