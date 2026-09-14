package events

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/sentinelops/sentinelops/internal/database"
)

func TestPostgresDispatcherEscalatesOnlyUnacknowledgedIncident(t *testing.T) {
	migrationURL, runtimeURL := os.Getenv("SENTINELOPS_TEST_DATABASE_MIGRATION_URL"), os.Getenv("SENTINELOPS_TEST_DATABASE_URL")
	if migrationURL == "" || runtimeURL == "" {
		t.Skip("set isolated test database URLs")
	}
	var received int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		received++
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	ctx := context.Background()
	migrator, err := database.Open(ctx, migrationURL)
	if err != nil {
		t.Fatal(err)
	}
	defer migrator.Close()
	if err = migrator.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	runtimeStore, err := database.Open(ctx, runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer runtimeStore.Close()

	orgID, incidentID, routeID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if _, err = migrator.Pool.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,$2)", orgID, "escalation-"+orgID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = migrator.Pool.Exec(ctx, "DELETE FROM organizations WHERE id=$1", orgID) }()
	spec, _ := json.Marshal(map[string]any{"channel": "webhook", "targetRef": "primary", "escalation": map[string]string{"after": "1m", "targetRef": "secondary"}})
	if _, err = migrator.Pool.Exec(ctx, "INSERT INTO incidents(id,organization_id,title,severity,status) VALUES($1,$2,'fixture','P1','OPEN')", incidentID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Pool.Exec(ctx, "INSERT INTO alert_routes(id,organization_id,name,spec,enabled) VALUES($1,$2,'escalation-route',$3,true)", routeID, orgID, spec); err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{Store: runtimeStore, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), HTTPClient: server.Client(), WebhookURLs: map[string]string{"primary": server.URL, "secondary": server.URL}, AllowedHosts: []string{u.Hostname()}}
	tenantCtx := database.WithTenant(ctx, orgID)
	if err := d.scheduleIncidentRoutes(tenantCtx, orgID, incidentID); err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Pool.Exec(ctx, "UPDATE incident_escalations SET scheduled_at=now()-interval '1 second' WHERE incident_id=$1", incidentID); err != nil {
		t.Fatal(err)
	}
	d.scheduleEscalations(tenantCtx, orgID)
	d.deliverNotifications(tenantCtx, orgID)
	var escalationStatus string
	if err = migrator.Pool.QueryRow(ctx, "SELECT status FROM incident_escalations WHERE incident_id=$1", incidentID).Scan(&escalationStatus); err != nil {
		t.Fatal(err)
	}
	if escalationStatus != "ESCALATED" || received != 2 {
		t.Fatalf("status=%s received=%d, want ESCALATED/2", escalationStatus, received)
	}

	if _, err = migrator.Pool.Exec(ctx, "UPDATE incidents SET status='ACKNOWLEDGED' WHERE id=$1", incidentID); err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Pool.Exec(ctx, "UPDATE incident_escalations SET status='SCHEDULED',scheduled_at=now()-interval '1 second',cancelled_at=NULL WHERE incident_id=$1", incidentID); err != nil {
		t.Fatal(err)
	}
	d.scheduleEscalations(tenantCtx, orgID)
	if err = migrator.Pool.QueryRow(ctx, "SELECT status FROM incident_escalations WHERE incident_id=$1", incidentID).Scan(&escalationStatus); err != nil {
		t.Fatal(err)
	}
	if escalationStatus != "CANCELLED" {
		t.Fatalf("acknowledged incident escalation must cancel, got %s", escalationStatus)
	}
}
