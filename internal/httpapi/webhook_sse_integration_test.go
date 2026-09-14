package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sentinelops/sentinelops/internal/config"
	"github.com/sentinelops/sentinelops/internal/database"
	"github.com/sentinelops/sentinelops/internal/events"
)

var webhookTestSecret = strings.Repeat("test-only-", 4)

func TestPostgresSignedWebhookIsDeliveredAndReplayedBySSE(t *testing.T) {
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
	store, err := database.Open(ctx, runtimeURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	organizationID, organization := uuid.NewString(), "webhook-"+uuid.NewString()
	if _, err := migrator.Pool.Exec(ctx, "INSERT INTO organizations(id,name) VALUES($1,$2)", organizationID, organization); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = migrator.Pool.Exec(ctx, "DELETE FROM organizations WHERE id=$1", organizationID) }()
	server := &Server{cfg: config.Config{WebhookSecrets: map[string]string{organization: webhookTestSecret}}, store: store, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	body := []byte(`{"event":"deployment.finished","status":"success"}`)
	timestamp, nonce, idempotencyKey := strconv.FormatInt(time.Now().UTC().Unix(), 10), "nonce-"+uuid.NewString(), "idem-"+uuid.NewString()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(string(body)))
	request.Header.Set("X-Sentinel-Organization", organization)
	request.Header.Set("X-Sentinel-Timestamp", timestamp)
	request.Header.Set("X-Sentinel-Nonce", nonce)
	request.Header.Set("Idempotency-Key", idempotencyKey)
	request.Header.Set("X-Sentinel-Signature", "sha256="+webhookSignature(webhookTestSecret, organization, timestamp, nonce, body))
	accepted := httptest.NewRecorder()
	server.webhook(accepted, request)
	if accepted.Code != http.StatusAccepted {
		t.Fatalf("webhook status=%d body=%s", accepted.Code, accepted.Body.String())
	}
	var acceptedBody struct {
		Data struct {
			DeliveryID string `json:"deliveryId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(accepted.Body.Bytes(), &acceptedBody); err != nil || acceptedBody.Data.DeliveryID == "" {
		t.Fatalf("invalid accepted webhook response: err=%v body=%s", err, accepted.Body.String())
	}

	dispatcher := &events.Dispatcher{Store: store, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Interval: time.Hour}
	workerContext, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan struct{})
	go func() { dispatcher.Run(workerContext); close(workerDone) }()
	defer func() { stopWorker(); <-workerDone }()
	dispatcherContext := database.WithTenant(ctx, organizationID)
	var status string
	deadlineForOutbox := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadlineForOutbox) {
		if err := migrator.Pool.QueryRow(ctx, "SELECT status FROM outbox_events WHERE organization_id=$1 AND event_type='webhook.received'", organizationID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status == "DELIVERED" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status != "DELIVERED" {
		t.Fatalf("outbox status=%s, want DELIVERED", status)
	}

	streamContext, cancel := context.WithCancel(context.WithValue(dispatcherContext, principalKey, principal{Subject: "integration-test", Role: "Platform Administrator", Organization: organization, OrganizationID: organizationID}))
	defer cancel()
	streamRequest := httptest.NewRequest(http.MethodGet, "/api/v1/events?cursor=0", nil).WithContext(streamContext)
	stream := newSSEWriter()
	done := make(chan struct{})
	go func() { server.events(stream, streamRequest); close(done) }()
	defer func() { cancel(); <-done }()
	deadline := time.NewTimer(4 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case <-stream.writes:
			if strings.Contains(stream.String(), "event: webhook.received") && strings.Contains(stream.String(), acceptedBody.Data.DeliveryID) {
				return
			}
		case <-deadline.C:
			t.Fatalf("SSE did not replay delivered webhook event: %s", stream.String())
		}
	}
}

func webhookSignature(secret, organization, timestamp, nonce string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(organization + "." + timestamp + "." + nonce + "."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

type sseWriter struct {
	header http.Header
	mu     sync.Mutex
	body   strings.Builder
	writes chan struct{}
}

func newSSEWriter() *sseWriter {
	return &sseWriter{header: make(http.Header), writes: make(chan struct{}, 8)}
}
func (w *sseWriter) Header() http.Header { return w.header }
func (w *sseWriter) WriteHeader(int)     {}
func (w *sseWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	_, _ = w.body.Write(data)
	w.mu.Unlock()
	w.signal()
	return len(data), nil
}
func (w *sseWriter) Flush() { w.signal() }
func (w *sseWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.body.String()
}
func (w *sseWriter) signal() {
	select {
	case w.writes <- struct{}{}:
	default:
	}
}
