package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func validVMwareEnv(t *testing.T) {
	t.Helper()
	t.Setenv("VMWARE_ENDPOINT", "https://vcenter.example.invalid/sdk")
	t.Setenv("VMWARE_USERNAME", "sentinelops-readonly")
	t.Setenv("VMWARE_PASSWORD_FILE", "/run/secrets/vmware-password")
	t.Setenv("VMWARE_TLS_THUMBPRINT", strings.Repeat("AA:", 31)+"AA")
	t.Setenv("VMWARE_EXPORTER_ADDRESS", "127.0.0.1:9472")
	t.Setenv("VMWARE_ALLOW_CONTAINER_BIND", "")
	t.Setenv("VMWARE_SCRAPE_INTERVAL", "60s")
}

func TestLoadRejectsUnsafeVMwareEndpoint(t *testing.T) {
	for name, endpoint := range map[string]string{
		"http":       "http://vcenter.example.invalid/sdk",
		"credential": "https://user:password@vcenter.example.invalid/sdk",
		"query":      "https://vcenter.example.invalid/sdk?debug=true",
		"no-host":    "https:///sdk",
		"fragment":   "https://vcenter.example.invalid/sdk#unsafe",
	} {
		t.Run(name, func(t *testing.T) {
			validVMwareEnv(t)
			t.Setenv("VMWARE_ENDPOINT", endpoint)
			if _, err := load(); err == nil {
				t.Fatal("load() aceitou endpoint VMware inseguro")
			}
		})
	}
}

func TestLoadValidatesThumbprintAndLoopback(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		validVMwareEnv(t)
		if _, err := load(); err != nil {
			t.Fatalf("load() falhou para configuração válida: %v", err)
		}
	})
	t.Run("hex", func(t *testing.T) {
		validVMwareEnv(t)
		t.Setenv("VMWARE_TLS_THUMBPRINT", strings.Repeat("GG:", 31)+"GG")
		if _, err := load(); err == nil {
			t.Fatal("load() aceitou thumbprint não hexadecimal")
		}
	})
	t.Run("bind", func(t *testing.T) {
		validVMwareEnv(t)
		t.Setenv("VMWARE_EXPORTER_ADDRESS", "0.0.0.0:9472")
		if _, err := load(); err == nil {
			t.Fatal("load() aceitou bind não-loopback")
		}
	})
}

func TestRefreshRejectsGroupReadablePasswordBeforeConnecting(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "vmware-password")
	if err := os.WriteFile(passwordFile, []byte("not-used"), 0o640); err != nil {
		t.Fatal(err)
	}
	e := newExporter(config{
		endpoint:     "https://vcenter.example.invalid/sdk",
		username:     "sentinelops-readonly",
		passwordFile: passwordFile,
		thumbprint:   strings.Repeat("AA:", 31) + "AA",
		interval:     time.Minute,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), prometheus.NewRegistry())
	err := e.refresh(context.Background())
	if err == nil || !strings.Contains(err.Error(), "permissões a grupo ou outros") {
		t.Fatalf("refresh() deveria recusar segredo group-readable antes de conectar: %v", err)
	}
}
