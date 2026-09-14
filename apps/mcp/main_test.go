package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sentinelops/sentinelops/internal/domain"
)

type stubClient struct {
	do func(context.Context, string, string, any, map[string]string, any) error
}

func (s stubClient) Do(ctx context.Context, method, path string, body any, headers map[string]string, out any) error {
	return s.do(ctx, method, path, body, headers, out)
}

func TestAssetsSearchUsesBoundedAPIPath(t *testing.T) {
	client := stubClient{do: func(_ context.Context, method, path string, body any, headers map[string]string, out any) error {
		if method != http.MethodGet || body != nil || headers != nil {
			t.Fatalf("unexpected API request: method=%s body=%v headers=%v", method, body, headers)
		}
		if path != "/api/v1/assets/search?cursor=asset-01&limit=2&q=vm+edge" {
			t.Fatalf("unexpected path: %s", path)
		}
		*out.(*assetSearchOutput) = assetSearchOutput{Items: []domain.Asset{{AssetID: "asset-02", Name: "VM edge"}}, NextCursor: "asset-02", Query: "vm edge", Limit: 2}
		return nil
	}}
	result := callTool(t, newServer(client), "assets_search", map[string]any{"query": " vm edge ", "cursor": "asset-01", "limit": 2})
	if result.IsError {
		t.Fatalf("tool failed: %#v", result.Content)
	}
	output, err := json.Marshal(result.StructuredContent)
	if err != nil || !strings.Contains(string(output), "asset-02") {
		t.Fatalf("unexpected structured output: %s err=%v", output, err)
	}
}

func TestAssetsSearchRejectsOversizedLimitWithoutAPIRequest(t *testing.T) {
	called := false
	client := stubClient{do: func(context.Context, string, string, any, map[string]string, any) error {
		called = true
		return nil
	}}
	result := callTool(t, newServer(client), "assets_search", map[string]any{"limit": 101})
	if !result.IsError || called {
		t.Fatalf("expected local validation error, called=%v result=%#v", called, result)
	}
}

func TestAssetGetRejectsUnsafeIdentifierWithoutAPIRequest(t *testing.T) {
	called := false
	client := stubClient{do: func(context.Context, string, string, any, map[string]string, any) error {
		called = true
		return nil
	}}
	result := callTool(t, newServer(client), "asset_get", map[string]any{"assetId": "../../etc/passwd"})
	if !result.IsError || called {
		t.Fatalf("expected local validation error, called=%v result=%#v", called, result)
	}
}

func TestNewAPIClientRejectsPlaintextRemoteAndIncompleteMTLS(t *testing.T) {
	if _, err := newAPIClient("http://sentinelops.internal", "token"); err == nil {
		t.Fatal("expected remote plaintext API URL to be rejected")
	}
	t.Setenv("SENTINEL_API_CLIENT_CERT_FILE", "/tmp/client.crt")
	if _, err := newAPIClient("https://sentinelops.internal", "token"); err == nil {
		t.Fatal("expected incomplete mTLS configuration to be rejected")
	}
}

func callTool(t *testing.T, server *mcp.Server, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	defer serverSession.Close()
	client := mcp.NewClient(&mcp.Implementation{Name: "sentinelops-mcp-test"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	defer clientSession.Close()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if result.StructuredContent == nil && !result.IsError {
		encoded, marshalErr := json.Marshal(result.Content)
		if marshalErr != nil || !strings.Contains(string(encoded), "asset") {
			t.Fatalf("expected structured or textual asset output: content=%s err=%v", encoded, marshalErr)
		}
	}
	return result
}
