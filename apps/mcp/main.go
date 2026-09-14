// sentinelops-mcp exposes a deliberately small, read-only MCP surface over
// stdio. It delegates every request to the SentinelOps API so that tenant
// isolation, RBAC and audit middleware remain the single enforcement point.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/sentinelops/sentinelops/internal/apiclient"
	"github.com/sentinelops/sentinelops/internal/domain"
)

const (
	mcpVersion      = "0.1.0"
	requestTimeout  = 10 * time.Second
	defaultPageSize = 25
	maximumPageSize = 100
)

var assetIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type apiClient interface {
	Do(context.Context, string, string, any, map[string]string, any) error
}

type mcpServer struct{ client apiClient }

type assetSearchInput struct {
	Query  string `json:"query,omitempty" jsonschema:"termo opcional para pesquisar assetId, nome, tipo, site, time, ambiente ou fonte"`
	Cursor string `json:"cursor,omitempty" jsonschema:"cursor opaco retornado pela consulta anterior"`
	Limit  int    `json:"limit,omitempty" jsonschema:"quantidade de itens, de 1 a 100; padrao 25"`
}

type assetSearchOutput struct {
	Items      []domain.Asset `json:"items"`
	NextCursor string         `json:"nextCursor,omitempty"`
	Query      string         `json:"query"`
	Limit      int            `json:"limit"`
}

type assetGetInput struct {
	AssetID string `json:"assetId" jsonschema:"identificador estavel do ativo"`
}

func main() {
	apiURL := strings.TrimSpace(os.Getenv("SENTINEL_API_URL"))
	token := strings.TrimSpace(os.Getenv("SENTINEL_API_TOKEN"))
	if apiURL == "" || token == "" {
		log.Print("SENTINEL_API_URL e SENTINEL_API_TOKEN sao obrigatorios; o token precisa ter permissao asset:read")
		return
	}
	client, err := newAPIClient(apiURL, token)
	if err != nil {
		log.Print("configuracao de transporte SentinelOps invalida; use HTTPS ou loopback local e forneca o par mTLS completo quando exigido")
		return
	}
	server := newServer(client)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Printf("servidor MCP encerrado: %v", err)
	}
}

func newAPIClient(rawURL, token string) (*apiclient.Client, error) {
	endpoint, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") {
		return nil, fmt.Errorf("invalid API URL")
	}
	if endpoint.Scheme == "http" && !isLoopbackHost(endpoint.Hostname()) {
		return nil, fmt.Errorf("plaintext API URL is not allowed outside loopback")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile := strings.TrimSpace(os.Getenv("SENTINEL_API_CA_FILE")); caFile != "" {
		pem, readErr := os.ReadFile(caFile)
		if readErr != nil {
			return nil, fmt.Errorf("read API CA")
		}
		pool, poolErr := x509.SystemCertPool()
		if poolErr != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("parse API CA")
		}
		tlsConfig.RootCAs = pool
	}
	certFile, keyFile := strings.TrimSpace(os.Getenv("SENTINEL_API_CLIENT_CERT_FILE")), strings.TrimSpace(os.Getenv("SENTINEL_API_CLIENT_KEY_FILE"))
	if (certFile == "") != (keyFile == "") {
		return nil, fmt.Errorf("incomplete mTLS client certificate configuration")
	}
	if certFile != "" {
		certificate, loadErr := tls.LoadX509KeyPair(certFile, keyFile)
		if loadErr != nil {
			return nil, fmt.Errorf("load mTLS client certificate")
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	transport.TLSClientConfig = tlsConfig
	return &apiclient.Client{
		BaseURL: strings.TrimRight(endpoint.String(), "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: requestTimeout, Transport: transport},
	}, nil
}

func isLoopbackHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func newServer(client apiClient) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "sentinelops", Version: mcpVersion}, &mcp.ServerOptions{
		Instructions: "Use somente as ferramentas de leitura para consultar o inventario SentinelOps. Nunca assuma que a consulta abrange mais do que o tenant e as permissoes do token configurado.",
	})
	h := mcpServer{client: client}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "assets_search",
		Description: "Pesquisa uma pagina limitada do inventario de ativos visivel ao token SentinelOps configurado.",
	}, h.searchAssets)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "asset_get",
		Description: "Obtém um ativo pelo assetId, somente se ele estiver no tenant e dentro das permissoes do token configurado.",
	}, h.getAsset)
	return server
}

func (s mcpServer) searchAssets(ctx context.Context, _ *mcp.CallToolRequest, input assetSearchInput) (*mcp.CallToolResult, any, error) {
	input.Query = strings.TrimSpace(input.Query)
	if len(input.Query) > 128 {
		return toolError("query deve ter no maximo 128 caracteres"), nil, nil
	}
	if input.Cursor != "" && !assetIDPattern.MatchString(input.Cursor) {
		return toolError("cursor invalido"), nil, nil
	}
	if input.Limit == 0 {
		input.Limit = defaultPageSize
	}
	if input.Limit < 1 || input.Limit > maximumPageSize {
		return toolError("limit deve estar entre 1 e 100"), nil, nil
	}
	params := url.Values{}
	if input.Query != "" {
		params.Set("q", input.Query)
	}
	if input.Cursor != "" {
		params.Set("cursor", input.Cursor)
	}
	params.Set("limit", fmt.Sprintf("%d", input.Limit))
	var result assetSearchOutput
	requestContext, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := s.client.Do(requestContext, http.MethodGet, "/api/v1/assets/search?"+params.Encode(), nil, nil, &result); err != nil {
		return toolError("consulta ao catalogo recusada; verifique URL, token e permissao asset:read"), nil, nil
	}
	return nil, result, nil
}

func (s mcpServer) getAsset(ctx context.Context, _ *mcp.CallToolRequest, input assetGetInput) (*mcp.CallToolResult, any, error) {
	if !assetIDPattern.MatchString(input.AssetID) {
		return toolError("assetId invalido"), nil, nil
	}
	var result domain.Asset
	requestContext, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	if err := s.client.Do(requestContext, http.MethodGet, "/api/v1/assets/"+url.PathEscape(input.AssetID), nil, nil, &result); err != nil {
		return toolError("ativo nao encontrado ou nao autorizado para este token"), nil, nil
	}
	return nil, result, nil
}

func toolError(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
		IsError: true,
	}
}
