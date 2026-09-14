package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadDSNRequiresPrivateFileAndVerifiedTLS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "postgres.dsn")
	if err := os.WriteFile(path, []byte("postgres://monitor:secret@db.example.invalid:5432/app?sslmode=verify-full\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readDSN(path); err != nil {
		t.Fatalf("verified TLS DSN was rejected: %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := readDSN(path); err == nil {
		t.Fatal("group-readable DSN was accepted")
	}
}

func TestLoadRejectsNonLoopbackAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "postgres.dsn")
	if err := os.WriteFile(path, []byte("postgres://monitor:secret@db.example.invalid:5432/app?sslmode=verify-full"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POSTGRES_MONITOR_DSN_FILE", path)
	t.Setenv("POSTGRES_EXPORTER_ADDRESS", "0.0.0.0:9187")
	t.Setenv("POSTGRES_ALLOW_CONTAINER_BIND", "")
	t.Setenv("POSTGRES_MONITOR_ASSET_ID", "postgres-hml-01")
	t.Setenv("POSTGRES_MONITOR_TEAM", "database")
	t.Setenv("POSTGRES_MONITOR_ENVIRONMENT", "hml")
	if _, err := load(); err == nil {
		t.Fatal("non-loopback address was accepted")
	}
}

func TestLoadRequiresStableRoutingLabels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "postgres.dsn")
	if err := os.WriteFile(path, []byte("postgres://monitor:secret@db.example.invalid:5432/app?sslmode=verify-full"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POSTGRES_MONITOR_DSN_FILE", path)
	t.Setenv("POSTGRES_MONITOR_ASSET_ID", "")
	t.Setenv("POSTGRES_MONITOR_TEAM", "database")
	t.Setenv("POSTGRES_MONITOR_ENVIRONMENT", "hml")
	if _, err := load(); err == nil {
		t.Fatal("missing asset label was accepted")
	}
}

func TestAggregateQueryAndErrorClassDoNotExposeQueryText(t *testing.T) {
	for _, forbidden := range []string{"insert", "update", "delete", "pg_stat_activity.query"} {
		if containsFold(aggregateQuery, forbidden) {
			t.Fatalf("aggregate query contains forbidden fragment %q", forbidden)
		}
	}
	if got := errorClass(assertError("permission denied for relation pg_stat_activity")); got != "access_denied" {
		t.Fatalf("unexpected error class %q", got)
	}
}

type assertError string

func (e assertError) Error() string { return string(e) }

func containsFold(value, fragment string) bool {
	return len(value) >= len(fragment) && containsLower(value, fragment)
}

func containsLower(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if equalFoldASCII(value[i:i+len(fragment)], fragment) {
			return true
		}
	}
	return false
}

func equalFoldASCII(value, fragment string) bool {
	for i := range value {
		a, b := value[i], fragment[i]
		if a >= 'A' && a <= 'Z' {
			a += 'a' - 'A'
		}
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
