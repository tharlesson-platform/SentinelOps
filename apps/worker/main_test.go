package main

import (
	"strings"
	"testing"

	"github.com/sentinelops/sentinelops/internal/config"
)

func TestTelemetryQueryClientUsesDirectSourcesOnlyWithoutGateway(t *testing.T) {
	t.Setenv("PROMETHEUS_URL", "http://prometheus.test:9090")
	t.Setenv("LOKI_URL", "http://loki.test:3100")
	t.Setenv("TEMPO_URL", "http://tempo.test:3200")
	client, urls, err := telemetryQueryClient(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if client.Transport != nil {
		t.Fatal("development client should use the default transport")
	}
	if urls.prometheus != "http://prometheus.test:9090" || urls.loki != "http://loki.test:3100" || urls.tempo != "http://tempo.test:3200" {
		t.Fatalf("unexpected direct data sources: %#v", urls)
	}
}

func TestTelemetryQueryClientRejectsNonHTTPSGateway(t *testing.T) {
	_, _, err := telemetryQueryClient(config.Config{TelemetryQueryGatewayURL: "http://gateway.test:8444"})
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("expected HTTPS gateway validation error, got %v", err)
	}
}
