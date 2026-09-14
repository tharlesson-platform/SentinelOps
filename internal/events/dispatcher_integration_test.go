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

func TestPostgresDispatcherProcessesWebhookOutboxAndSupportsReplay(t *testing.T) {
	migrationURL, runtimeURL := os.Getenv("SENTINELOPS_TEST_DATABASE_MIGRATION_URL"), os.Getenv("SENTINELOPS_TEST_DATABASE_URL")
	if migrationURL == "" || runtimeURL == "" {
		t.Skip("set isolated test database URLs")
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

	orgID, deliveryID := uuid.NewString(), uuid.NewString()
	if _, err = migrator.Pool.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,$2)", orgID, "outbox-"+orgID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = migrator.Pool.Exec(ctx, "DELETE FROM organizations WHERE id=$1", orgID) }()
	if _, err = migrator.Pool.Exec(ctx, `INSERT INTO webhook_deliveries(id,organization_id,integration,idempotency_key,nonce,signature,status)
VALUES($1,$2,'github',$3,$4,'signature','ACCEPTED')`, deliveryID, orgID, "idem-"+deliveryID, "nonce-"+deliveryID); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(map[string]string{"deliveryId": deliveryID})
	if _, err = migrator.Pool.Exec(ctx, "INSERT INTO outbox_events(organization_id,event_type,payload) VALUES($1,'webhook.received',$2)", orgID, payload); err != nil {
		t.Fatal(err)
	}

	dispatcher := &Dispatcher{Store: runtimeStore, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	dispatcher.dispatchTenant(database.WithTenant(ctx, orgID), orgID)
	var deliveryStatus, eventStatus string
	if err = migrator.Pool.QueryRow(ctx, "SELECT status FROM webhook_deliveries WHERE id=$1", deliveryID).Scan(&deliveryStatus); err != nil {
		t.Fatal(err)
	}
	if err = migrator.Pool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE organization_id=$1", orgID).Scan(&eventStatus); err != nil {
		t.Fatal(err)
	}
	if deliveryStatus != "PROCESSED" || eventStatus != "DELIVERED" {
		t.Fatalf("delivery=%s event=%s, want PROCESSED/DELIVERED", deliveryStatus, eventStatus)
	}
}

func TestPostgresDispatcherDeliversWebhookExactlyOnce(t *testing.T) {
	migrationURL, runtimeURL := os.Getenv("SENTINELOPS_TEST_DATABASE_MIGRATION_URL"), os.Getenv("SENTINELOPS_TEST_DATABASE_URL")
	if migrationURL == "" || runtimeURL == "" {
		t.Skip("set isolated test database URLs")
	}
	var received int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received++
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("missing idempotency key")
		}
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
	if _, err = migrator.Pool.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,$2)", orgID, "delivery-"+orgID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = migrator.Pool.Exec(ctx, "DELETE FROM organizations WHERE id=$1", orgID) }()
	spec, _ := json.Marshal(map[string]string{"channel": "webhook", "targetRef": "test"})
	if _, err = migrator.Pool.Exec(ctx, `INSERT INTO incidents(id,organization_id,title,severity,status) VALUES($1,$2,'fixture','P2','OPEN')`, incidentID, orgID); err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Pool.Exec(ctx, `INSERT INTO alert_routes(id,organization_id,name,spec,enabled) VALUES($1,$2,'test-route',$3,true)`, routeID, orgID, spec); err != nil {
		t.Fatal(err)
	}
	if _, err = migrator.Pool.Exec(ctx, `INSERT INTO notification_deliveries(organization_id,incident_id,route_id) VALUES($1,$2,$3)`, orgID, incidentID, routeID); err != nil {
		t.Fatal(err)
	}
	d := &Dispatcher{Store: runtimeStore, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), HTTPClient: server.Client(), WebhookURLs: map[string]string{"test": server.URL}, AllowedHosts: []string{u.Hostname()}}
	d.deliverNotifications(database.WithTenant(ctx, orgID), orgID)
	var status string
	if err = migrator.Pool.QueryRow(ctx, "SELECT status FROM notification_deliveries WHERE incident_id=$1", incidentID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "DELIVERED" || received != 1 {
		t.Fatalf("status=%s received=%d, want DELIVERED/1", status, received)
	}
}
