package synthetics

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sentinelops/sentinelops/internal/database"
)

func TestPostgresSyntheticClaimIsUniqueAcrossWorkers(t *testing.T) {
	migrationURL, runtimeURL := os.Getenv("SENTINELOPS_TEST_DATABASE_MIGRATION_URL"), os.Getenv("SENTINELOPS_TEST_DATABASE_URL")
	if migrationURL == "" || runtimeURL == "" {
		t.Skip("set isolated SENTINELOPS_TEST_DATABASE_MIGRATION_URL and SENTINELOPS_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	migrator, err := database.Open(ctx, migrationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()
	if err := migrator.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	runtimeStore, err := database.Open(ctx, runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()

	orgID, scenarioID := uuid.NewString(), uuid.NewString()
	// The migration URL is intentionally the isolated database owner; runtime
	// assertions below use the restricted application role.
	_, err = migrator.Pool.Exec(ctx, `INSERT INTO organizations(id,name) VALUES($1,$2)`, orgID, "claim-"+orgID)
	if err == nil {
		_, err = migrator.Pool.Exec(ctx, `INSERT INTO synthetic_scenarios(id,organization_id,name,service_ref,environment,type,created_by) VALUES($1,$2,'claim-test','svc','test','http','test')`, scenarioID, orgID)
	}
	if err == nil {
		_, err = migrator.Pool.Exec(ctx, `INSERT INTO synthetic_scenario_versions(scenario_id,version,spec,checksum,created_by) VALUES($1,1,'{}','test','test')`, scenarioID)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = migrator.Pool.Exec(ctx, "DELETE FROM organizations WHERE id=$1", orgID) }()

	scheduler := &Scheduler{Store: runtimeStore}
	slot := time.Now().UTC().Truncate(time.Minute)
	var claimed [2]bool
	var errs [2]error
	var wg sync.WaitGroup
	for i := range claimed {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			claimed[index], errs[index] = scheduler.claimRun(database.WithTenant(ctx, orgID), orgID, scenarioID, 1, slot, uuid.NewString())
		}(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("claim errors: %v %v", errs[0], errs[1])
	}
	if (claimed[0] && claimed[1]) || (!claimed[0] && !claimed[1]) {
		t.Fatalf("claims=%v, want exactly one winner", claimed)
	}
	var runs, claims int
	if err := migrator.Pool.QueryRow(ctx, "SELECT count(*) FROM test_runs WHERE scenario_id=$1", scenarioID).Scan(&runs); err != nil {
		t.Fatal(err)
	}
	if err := migrator.Pool.QueryRow(ctx, "SELECT count(*) FROM synthetic_execution_claims WHERE scenario_id=$1", scenarioID).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if runs != 1 || claims != 1 {
		t.Fatalf("runs=%d claims=%d, want 1/1", runs, claims)
	}
}
