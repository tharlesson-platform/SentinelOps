package database

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
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
	nameA, nameB := "rls-a-"+orgA, "rls-b-"+orgB
	adminCtx := withMigrationTenant(ctx)
	_, err = migrationStore.Pool.Exec(adminCtx, "INSERT INTO organizations(id,name) VALUES($1,$2),($3,$4)", orgA, nameA, orgB, nameB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM services WHERE id=ANY($1::uuid[])", []string{serviceA, serviceB})
		_, _ = migrationStore.Pool.Exec(adminCtx, "DELETE FROM organizations WHERE id=ANY($1::uuid[])", []string{orgA, orgB})
	}()
	_, err = migrationStore.Pool.Exec(adminCtx, `INSERT INTO services(id,organization_id,name,display_name,owner_team,created_by)
VALUES($1,$2,'service-a','Service A','team-a','rls-test'),($3,$4,'service-b','Service B','team-b','rls-test')`,
		serviceA, orgA, serviceB, orgB)
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

	items, err := runtimeStore.ListServices(ctx, orgA)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "service-a" {
		t.Fatalf("tenant A received unexpected services: %#v", items)
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

	tenantBCtx := WithTenant(ctx, orgB)
	if err := runtimeStore.Pool.QueryRow(tenantBCtx, "SELECT count(*) FROM services WHERE id=ANY($1::uuid[])", []string{serviceA, serviceB}).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("tenant B expected exactly one row, got %d", count)
	}
}
