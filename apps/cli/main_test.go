package main

import "testing"

func TestAssetSearchPathBoundsAndEncodesParameters(t *testing.T) {
	path, err := assetSearchPath(" vm edge ", "asset-01", 2)
	if err != nil {
		t.Fatalf("assetSearchPath: %v", err)
	}
	if path != "/api/v1/assets/search?cursor=asset-01&limit=2&q=vm+edge" {
		t.Fatalf("unexpected path: %s", path)
	}
	if _, err := assetSearchPath("", "", 101); err == nil {
		t.Fatal("expected limit validation error")
	}
}
