package events

import "testing"

func TestAllowedEndpointRequiresExactConfiguredHost(t *testing.T) {
	d := &Dispatcher{AllowedHosts: []string{"hooks.tqi.example"}}
	if !d.allowedEndpoint("https://hooks.tqi.example/services/abc") {
		t.Fatal("allowed endpoint rejected")
	}
	if d.allowedEndpoint("https://hooks.tqi.example.evil.example/services/abc") {
		t.Fatal("suffix attack accepted")
	}
	if d.allowedEndpoint("not a URL") {
		t.Fatal("invalid URL accepted")
	}
}
