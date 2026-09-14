package httpapi

import "testing"

func TestSafeAssetID(t *testing.T) {
	for _, value := range []string{"vm:421d7e2f", "azure-8f3a_01", "serial.123"} {
		if !safeAssetID.MatchString(value) {
			t.Fatalf("expected asset ID %q to be valid", value)
		}
	}
	for _, value := range []string{"", "has space", "/subscriptions/example", "../escape"} {
		if safeAssetID.MatchString(value) {
			t.Fatalf("expected asset ID %q to be rejected", value)
		}
	}
}

func TestValidDataLifecycleScope(t *testing.T) {
	valid := dataLifecycleScope{SubjectRef: "employee-123", Domains: []string{"control-plane-metadata", "operational-evidence"}}
	if !validDataLifecycleScope(valid) {
		t.Fatal("expected explicit lifecycle scope to be valid")
	}
	for _, invalid := range []dataLifecycleScope{
		{},
		{SubjectRef: "raw email@example.com", Domains: []string{"control-plane-metadata"}},
		{SubjectRef: "subject", Domains: []string{"unknown"}},
		{SubjectRef: "subject", Domains: []string{"control-plane-metadata", "control-plane-metadata"}},
	} {
		if validDataLifecycleScope(invalid) {
			t.Fatalf("expected scope to be rejected: %#v", invalid)
		}
	}
}
