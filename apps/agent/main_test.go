package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sentinelops/sentinelops/internal/apiclient"
)

func TestPublishInventoryUsesAgentCredentialAndCompleteSnapshot(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/agents/agent-id/inventory-reconcile" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Agent-Token") != "agent-token" {
			t.Fatal("missing agent token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.json")
	if err := os.WriteFile(path, []byte(`{"complete":true,"assets":[{"assetId":"vm:123","name":"vm-1","kind":"vm"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	client := apiclient.New(server.URL, "", time.Second)
	if err := publishInventory(context.Background(), client, "agent-id", "agent-token", path); err != nil {
		t.Fatal(err)
	}
}

func TestPublishInventoryRejectsPartialSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(path, []byte(`{"complete":false,"assets":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := publishInventory(context.Background(), apiclient.New("http://127.0.0.1", "", time.Second), "agent-id", "agent-token", path); err == nil {
		t.Fatal("partial snapshot was accepted")
	}
}

func TestPublishInventoryRejectsStaleSnapshotWhenFreshnessIsRequired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.json")
	if err := os.WriteFile(path, []byte(`{"complete":true,"assets":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(path, stale, stale); err != nil {
		t.Fatal(err)
	}
	if err := publishInventoryWithMaxAge(context.Background(), apiclient.New("http://127.0.0.1", "", time.Second), "agent-id", "agent-token", path, time.Minute); err == nil {
		t.Fatal("stale inventory was accepted")
	}
}
