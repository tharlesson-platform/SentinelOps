package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sentinelops/sentinelops/internal/auth"
	"github.com/sentinelops/sentinelops/internal/config"
	"github.com/sentinelops/sentinelops/internal/database"
	"github.com/sentinelops/sentinelops/internal/domain"
	"github.com/sentinelops/sentinelops/internal/synthetics"
	"github.com/sentinelops/sentinelops/internal/telemetryquery"
	"github.com/sentinelops/sentinelops/internal/workflows"
	"go.temporal.io/sdk/client"
)

type WorkflowStarter interface {
	ExecuteWorkflow(context.Context, client.StartWorkflowOptions, interface{}, ...interface{}) (client.WorkflowRun, error)
	CancelWorkflow(context.Context, string, string) error
}

type workflowHealthChecker interface {
	CheckHealth(context.Context, *client.CheckHealthRequest) (*client.CheckHealthResponse, error)
}

type Server struct {
	cfg           config.Config
	store         *database.Store
	auth          auth.Authenticator
	localAuth     *auth.Manager
	temporal      WorkflowStarter
	logger        *slog.Logger
	mux           *http.ServeMux
	requests      *prometheus.CounterVec
	duration      *prometheus.HistogramVec
	limiter       *ipLimiter
	quotaRejects  *prometheus.CounterVec
	telemetry     *telemetryquery.Client
	explorerMu    sync.Mutex
	explorerSlots chan struct{}
	explorerCache map[string]cachedDiscovery
}

type response struct {
	Data  any       `json:"data,omitempty"`
	Error *apiError `json:"error,omitempty"`
	Meta  any       `json:"meta,omitempty"`
}
type apiError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}
type principal struct {
	Subject        string
	Role           string
	Organization   string
	OrganizationID string
}
type contextKey string

const principalKey contextKey = "principal"

var safeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
var safeAssetID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var certificateFingerprint = regexp.MustCompile(`^[a-f0-9]{64}$`)

func New(ctx context.Context, cfg config.Config, store *database.Store, temporal WorkflowStarter, logger *slog.Logger, registry *prometheus.Registry) (*Server, error) {
	if err := cfg.ValidateAPI(); err != nil {
		return nil, fmt.Errorf("validate API configuration: %w", err)
	}
	var authenticator auth.Authenticator
	var local *auth.Manager
	if cfg.AuthMode == "local" {
		local = auth.New(cfg.JWTSecret, cfg.LocalUser, cfg.LocalPasswordHash)
		authenticator = local
	} else {
		oidcAuth, err := auth.NewOIDC(ctx, cfg.OIDCIssuerURL, cfg.OIDCAudience, cfg.OIDCRequiredScope, cfg.OIDCRequiredGroup, cfg.OIDCOrganization)
		if err != nil {
			return nil, fmt.Errorf("initialize OIDC: %w", err)
		}
		authenticator = oidcAuth
	}
	telemetry, err := telemetryquery.New(telemetryquery.Options{
		GatewayURL: cfg.TelemetryQueryGatewayURL, PrometheusURL: cfg.PrometheusURL, LokiURL: cfg.LokiURL,
		TempoURL: cfg.TempoURL, PyroscopeURL: cfg.PyroscopeURL, ClientCertFile: cfg.TelemetryQueryAPICert,
		ClientKeyFile: cfg.TelemetryQueryAPIKey, RequestTimeout: cfg.RequestTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize telemetry query client: %w", err)
	}
	s := &Server{cfg: cfg, store: store, auth: authenticator, localAuth: local, temporal: temporal, logger: logger, mux: http.NewServeMux(), limiter: newIPLimiter(120, time.Minute),
		requests:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sentinel_http_requests_total", Help: "HTTP requests processed."}, []string{"method", "route", "status"}),
		duration:     prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "sentinel_http_request_duration_seconds", Help: "HTTP request duration.", Buckets: prometheus.DefBuckets}, []string{"method", "route"}),
		quotaRejects: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sentinel_tenant_quota_rejections_total", Help: "Authenticated requests rejected by the per-tenant quota."}, []string{"scope"}),
		telemetry:    telemetry}
	registry.MustRegister(s.requests, s.duration, s.quotaRejects)
	s.routes(registry)
	return s, nil
}

func (s *Server) Handler() http.Handler {
	return s.recover(s.securityHeaders(s.cors(s.observe(s.rateLimit(s.limitBody(s.mux))))))
}

func (s *Server) routes(registry *prometheus.Registry) {
	s.mux.Handle("GET /metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{}))
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /readyz", s.ready)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.login)
	s.mux.Handle("GET /api/v1/services", s.require("service:read", http.HandlerFunc(s.listServices)))
	s.mux.Handle("POST /api/v1/services", s.require("service:write", http.HandlerFunc(s.upsertService)))
	s.mux.Handle("GET /api/v1/services/{name}", s.require("service:read", http.HandlerFunc(s.getService)))
	s.mux.Handle("PUT /api/v1/services/{name}", s.require("service:write", http.HandlerFunc(s.upsertService)))
	s.mux.Handle("DELETE /api/v1/services/{name}", s.require("service:delete", http.HandlerFunc(s.deleteService)))
	s.mux.Handle("GET /api/v1/assets", s.require("asset:read", http.HandlerFunc(s.listAssets)))
	s.mux.Handle("GET /api/v1/assets/search", s.require("asset:read", http.HandlerFunc(s.searchAssets)))
	s.mux.Handle("POST /api/v1/assets", s.require("asset:write", http.HandlerFunc(s.upsertAsset)))
	s.mux.Handle("POST /api/v1/assets/reconcile", s.require("asset:write", http.HandlerFunc(s.reconcileAssets)))
	s.mux.Handle("GET /api/v1/assets/{assetID}", s.require("asset:read", http.HandlerFunc(s.getAsset)))
	s.mux.Handle("GET /api/v1/releases/{id}", s.require("validation:read", http.HandlerFunc(s.getRelease)))
	s.mux.Handle("POST /api/v1/releases", s.require("release:create", http.HandlerFunc(s.createRelease)))
	s.mux.Handle("POST /api/v1/releases/{releaseId}/validate", s.require("validation:write", http.HandlerFunc(s.validateRelease)))
	s.mux.Handle("GET /api/v1/validations/{id}", s.require("validation:read", http.HandlerFunc(s.getValidation)))
	s.mux.Handle("POST /api/v1/validations/{id}/cancel", s.require("validation:write", http.HandlerFunc(s.cancelValidation)))
	s.mux.Handle("POST /api/v1/agent-bootstrap-tokens", s.require("agent:write", http.HandlerFunc(s.createAgentBootstrapToken)))
	s.mux.Handle("POST /api/v1/agents/register", http.HandlerFunc(s.registerAgent))
	s.mux.Handle("POST /api/v1/agents/{id}/heartbeat", http.HandlerFunc(s.agentHeartbeat))
	s.mux.Handle("POST /api/v1/agents/{id}/inventory-reconcile", http.HandlerFunc(s.agentInventoryReconcile))
	s.mux.Handle("POST /api/v1/agents/{id}/revoke", s.require("agent:write", http.HandlerFunc(s.revokeAgent)))
	s.mux.Handle("GET /api/v1/agents", s.require("agent:read", http.HandlerFunc(s.listAgents)))
	s.mux.Handle("GET /api/v1/scenarios", s.require("scenario:read", http.HandlerFunc(s.listScenarios)))
	s.mux.Handle("POST /api/v1/scenarios", s.require("scenario:write", http.HandlerFunc(s.applyScenario)))
	s.mux.Handle("GET /api/v1/synthetic-runs", s.require("scenario:read", http.HandlerFunc(s.listSyntheticRuns)))
	s.mux.Handle("GET /api/v1/incidents", s.require("incident:read", http.HandlerFunc(s.listIncidents)))
	s.mux.Handle("POST /api/v1/incidents", s.require("incident:write", http.HandlerFunc(s.createIncident)))
	s.mux.Handle("POST /api/v1/incidents/{id}/ack", s.require("incident:write", http.HandlerFunc(s.ackIncident)))
	s.mux.Handle("POST /api/v1/incidents/{id}/resolve", s.require("incident:write", http.HandlerFunc(s.resolveIncident)))
	s.mux.Handle("GET /api/v1/incidents/{id}/deliveries", s.require("incident:read", http.HandlerFunc(s.listIncidentDeliveries)))
	s.mux.Handle("GET /api/v1/incidents/{id}/escalations", s.require("incident:read", http.HandlerFunc(s.listIncidentEscalations)))
	s.mux.Handle("GET /api/v1/alert-routes", s.require("alert-route:read", http.HandlerFunc(s.listAlertRoutes)))
	s.mux.Handle("POST /api/v1/alert-routes", s.require("alert-route:write", http.HandlerFunc(s.applyAlertRoute)))
	s.mux.Handle("GET /api/v1/data-lifecycle-requests", s.require("data-lifecycle:read", http.HandlerFunc(s.listDataLifecycleRequests)))
	s.mux.Handle("POST /api/v1/data-lifecycle-requests", s.require("data-lifecycle:write", http.HandlerFunc(s.createDataLifecycleRequest)))
	s.mux.Handle("POST /api/v1/data-lifecycle-requests/{id}/approve", s.require("data-lifecycle:write", http.HandlerFunc(s.approveDataLifecycleRequest)))
	s.mux.Handle("POST /api/v1/data-lifecycle-requests/{id}/execute", s.require("data-lifecycle:write", http.HandlerFunc(s.executeDataLifecycleRequest)))
	s.mux.Handle("GET /api/v1/events", s.require("validation:read", http.HandlerFunc(s.events)))
	s.mux.Handle("GET /api/v1/observability/explorer/platform", s.require("telemetry:read", http.HandlerFunc(s.explorerPlatform)))
	s.mux.Handle("GET /api/v1/observability/explorer/profiles/catalog", s.require("telemetry:read", http.HandlerFunc(s.explorerProfilesCatalog)))
	s.mux.Handle("GET /api/v1/observability/explorer/profiles/labels", s.require("telemetry:read", http.HandlerFunc(s.explorerProfilesLabels)))
	s.mux.Handle("GET /api/v1/observability/explorer/profiles/query", s.require("telemetry:read", http.HandlerFunc(s.explorerProfilesQuery)))
	s.mux.Handle("GET /api/v1/observability/explorer/catalog", s.require("telemetry:read", http.HandlerFunc(s.explorerCatalog)))
	s.mux.Handle("GET /api/v1/observability/explorer/dimensions", s.require("telemetry:read", http.HandlerFunc(s.explorerDimensions)))
	s.mux.Handle("GET /api/v1/observability/explorer/metric", s.require("telemetry:read", http.HandlerFunc(s.explorerMetric)))
	s.mux.Handle("GET /api/v1/observability/explorer/variables/{dashboard}/{variable}", s.require("telemetry:read", http.HandlerFunc(s.explorerVariable)))
	s.mux.Handle("GET /api/v1/observability/explorer/panels/{dashboard}/{panel}", s.require("telemetry:read", http.HandlerFunc(s.explorerPanel)))
	s.mux.Handle("GET /api/v1/observability/overview", s.require("telemetry:read", http.HandlerFunc(s.observabilityOverview)))
	s.mux.Handle("GET /api/v1/observability/hosts", s.require("telemetry:read", http.HandlerFunc(s.listObservedHosts)))
	s.mux.Handle("GET /api/v1/observability/hosts/{host}", s.require("telemetry:read", http.HandlerFunc(s.observedHostDetails)))
	s.mux.Handle("GET /api/v1/observability/containers", s.require("telemetry:read", http.HandlerFunc(s.listObservedContainers)))
	s.mux.Handle("GET /api/v1/observability/containers/{container}", s.require("telemetry:read", http.HandlerFunc(s.observedContainerDetails)))
	s.mux.Handle("GET /api/v1/observability/logs", s.require("telemetry:read", http.HandlerFunc(s.searchObservedLogs)))
	s.mux.Handle("GET /api/v1/observability/apm", s.require("telemetry:read", http.HandlerFunc(s.observedAPM)))
	s.mux.Handle("GET /api/v1/observability/traces", s.require("telemetry:read", http.HandlerFunc(s.searchObservedTraces)))
	for _, provider := range []string{"deployment", "github", "gitlab", "jenkins", "azure-devops", "aws-codepipeline"} {
		s.mux.HandleFunc("POST /api/v1/webhooks/"+provider, s.webhook)
	}
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	write(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now().UTC()})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Health(ctx); err != nil {
		fail(w, r, http.StatusServiceUnavailable, "database_unavailable", "dependência obrigatória indisponível")
		return
	}
	if s.temporal == nil {
		fail(w, r, http.StatusServiceUnavailable, "temporal_unavailable", "dependência obrigatória indisponível")
		return
	}
	healthChecker, ok := s.temporal.(workflowHealthChecker)
	if !ok {
		fail(w, r, http.StatusServiceUnavailable, "temporal_healthcheck_unavailable", "dependência obrigatória indisponível")
		return
	}
	if _, err := healthChecker.CheckHealth(ctx, &client.CheckHealthRequest{}); err != nil {
		fail(w, r, http.StatusServiceUnavailable, "temporal_unavailable", "dependência obrigatória indisponível")
		return
	}
	write(w, http.StatusOK, map[string]string{"status": "ready"})
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.localAuth == nil {
		fail(w, r, http.StatusNotFound, "local_auth_disabled", "login local desabilitado; use o provedor OIDC")
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	token, err := s.localAuth.Login(in.Username, in.Password)
	if err != nil {
		fail(w, r, http.StatusUnauthorized, "invalid_credentials", "usuário ou senha inválidos")
		return
	}
	write(w, http.StatusOK, map[string]any{"accessToken": token, "tokenType": "Bearer"})
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	items, err := s.store.ListServices(r.Context(), p.OrganizationID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, items)
}
func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	item, err := s.store.GetService(r.Context(), p.OrganizationID, r.PathValue("name"))
	if database.IsNotFound(err) {
		fail(w, r, http.StatusNotFound, "not_found", "serviço não encontrado")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, item)
}
func (s *Server) upsertService(w http.ResponseWriter, r *http.Request) {
	var in domain.Service
	if !decode(w, r, &in) {
		return
	}
	if name := r.PathValue("name"); name != "" {
		in.Name = name
	}
	if !safeName.MatchString(in.Name) || strings.TrimSpace(in.DisplayName) == "" || strings.TrimSpace(in.OwnerTeam) == "" {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "name, displayName e ownerTeam são obrigatórios; name deve ser DNS-safe")
		return
	}
	if in.Tier == "" {
		in.Tier = "3"
	}
	p := getPrincipal(r.Context())
	item, err := s.store.UpsertService(r.Context(), p.OrganizationID, p.Subject, in)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, p, "service.apply", "service", item.ID, in)
	write(w, http.StatusOK, item)
}
func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	if err := s.store.DeleteService(r.Context(), p.OrganizationID, r.PathValue("name")); database.IsNotFound(err) {
		fail(w, r, http.StatusNotFound, "not_found", "serviço não encontrado")
		return
	} else if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, p, "service.delete", "service", r.PathValue("name"), nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listAssets(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	items, err := s.store.ListAssets(r.Context(), p.OrganizationID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, items)
}

func (s *Server) searchAssets(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	if !s.allowTenantScoped(w, r, p.OrganizationID, "catalog-query", s.cfg.CatalogQueriesPerMinute) {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	if len(query) > 128 || (cursor != "" && !safeAssetID.MatchString(cursor)) {
		fail(w, r, http.StatusBadRequest, "validation_error", "q ou cursor inválido")
		return
	}
	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			fail(w, r, http.StatusBadRequest, "validation_error", "limit deve estar entre 1 e 100")
			return
		}
		limit = parsed
	}
	result, err := s.store.SearchAssets(r.Context(), p.OrganizationID, query, cursor, limit)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"items": result.Items, "nextCursor": result.NextCursor, "query": query, "limit": limit})
}

func (s *Server) getAsset(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	if !s.allowTenantScoped(w, r, p.OrganizationID, "catalog-query", s.cfg.CatalogQueriesPerMinute) {
		return
	}
	item, err := s.store.GetAsset(r.Context(), p.OrganizationID, r.PathValue("assetID"))
	if database.IsNotFound(err) {
		fail(w, r, http.StatusNotFound, "not_found", "ativo não encontrado")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, item)
}

func (s *Server) upsertAsset(w http.ResponseWriter, r *http.Request) {
	var in domain.Asset
	if !decode(w, r, &in) {
		return
	}
	in.AssetID = strings.TrimSpace(in.AssetID)
	in.Name = strings.TrimSpace(in.Name)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Source = strings.TrimSpace(in.Source)
	if !safeAssetID.MatchString(in.AssetID) || in.Name == "" || in.Kind == "" || in.Source == "" {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "assetId estável, name, kind e source são obrigatórios; assetId aceita somente letras, números, ponto, dois-pontos, hífen e sublinhado")
		return
	}
	if in.Lifecycle == "" {
		in.Lifecycle = "active"
	}
	if in.Lifecycle != "active" && in.Lifecycle != "stale" && in.Lifecycle != "retired" {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "lifecycle deve ser active, stale ou retired")
		return
	}
	p := getPrincipal(r.Context())
	item, err := s.store.UpsertAsset(r.Context(), p.OrganizationID, p.Subject, in)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, p, "asset.apply", "asset", item.ID, in)
	write(w, http.StatusOK, item)
}

func (s *Server) reconcileAssets(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Source     string         `json:"source"`
		ObservedAt time.Time      `json:"observedAt"`
		Complete   bool           `json:"complete"`
		Assets     []domain.Asset `json:"assets"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Source = strings.TrimSpace(in.Source)
	if !safeName.MatchString(in.Source) || !in.Complete || len(in.Assets) > 1000 {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "source DNS-safe, complete=true e no máximo 1000 ativos são obrigatórios")
		return
	}
	seen := map[string]bool{}
	for i := range in.Assets {
		asset := &in.Assets[i]
		asset.AssetID, asset.Name, asset.Kind = strings.TrimSpace(asset.AssetID), strings.TrimSpace(asset.Name), strings.TrimSpace(asset.Kind)
		if !safeAssetID.MatchString(asset.AssetID) || asset.Name == "" || asset.Kind == "" || seen[asset.AssetID] {
			fail(w, r, http.StatusUnprocessableEntity, "validation_error", "cada ativo requer assetId único, name e kind")
			return
		}
		seen[asset.AssetID] = true
	}
	p := getPrincipal(r.Context())
	items, err := s.store.ReconcileAssets(r.Context(), p.OrganizationID, p.Subject, in.Source, in.ObservedAt, in.Assets)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, p, "asset.reconcile", "asset-source", in.Source, map[string]any{"source": in.Source, "count": len(items), "complete": true})
	write(w, http.StatusOK, items)
}

func (s *Server) createRelease(w http.ResponseWriter, r *http.Request) {
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" || len(idem) > 128 {
		fail(w, r, http.StatusBadRequest, "idempotency_key_required", "Idempotency-Key é obrigatório e deve ter até 128 caracteres")
		return
	}
	var in domain.Release
	if !decode(w, r, &in) {
		return
	}
	if !safeName.MatchString(in.Service) || !safeName.MatchString(in.Environment) || strings.TrimSpace(in.Version) == "" {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "service, environment e version são obrigatórios")
		return
	}
	p := getPrincipal(r.Context())
	item, err := s.store.CreateRelease(r.Context(), p.OrganizationID, p.Subject, idem, in)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, p, "release.create", "release", item.ID, map[string]any{"service": item.Service, "environment": item.Environment, "version": item.Version})
	writeStatus(w, http.StatusCreated, item)
}
func (s *Server) getRelease(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	item, err := s.store.GetRelease(r.Context(), p.OrganizationID, r.PathValue("id"))
	if database.IsNotFound(err) {
		fail(w, r, http.StatusNotFound, "not_found", "release não encontrada")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, item)
}
func (s *Server) validateRelease(w http.ResponseWriter, r *http.Request) {
	if s.temporal == nil {
		fail(w, r, http.StatusServiceUnavailable, "workflow_unavailable", "Temporal indisponível")
		return
	}
	releaseID := r.PathValue("releaseId")
	p := getPrincipal(r.Context())
	if _, err := s.store.GetRelease(r.Context(), p.OrganizationID, releaseID); err != nil {
		if database.IsNotFound(err) {
			fail(w, r, http.StatusNotFound, "not_found", "release não encontrada")
			return
		}
		s.internal(w, r, err)
		return
	}
	var in struct {
		Mode string `json:"mode"`
	}
	if !decodeOptional(w, r, &in) {
		return
	}
	if in.Mode == "" {
		in.Mode = "standard"
	}
	allowed := map[string]bool{"smoke": true, "standard": true, "regression": true, "canary": true, "performance": true, "deep": true}
	if !allowed[in.Mode] {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "mode inválido")
		return
	}
	v, err := s.store.CreateValidation(r.Context(), p.OrganizationID, p.Subject, releaseID, in.Mode)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	wfID := "validation-" + v.ID
	run, err := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{ID: wfID, TaskQueue: workflows.ReleaseValidationTaskQueue}, workflows.ReleaseValidationWorkflow, workflows.ValidationInput{OrganizationID: p.OrganizationID, ValidationID: v.ID, ReleaseID: releaseID, Mode: in.Mode})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	_ = s.store.SetWorkflowID(r.Context(), p.OrganizationID, v.ID, wfID)
	s.audit(r, p, "validation.start", "validation", v.ID, map[string]string{"workflowId": run.GetID(), "runId": run.GetRunID()})
	writeStatus(w, http.StatusAccepted, v)
}
func (s *Server) getValidation(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	item, err := s.store.GetValidation(r.Context(), p.OrganizationID, r.PathValue("id"))
	if database.IsNotFound(err) {
		fail(w, r, http.StatusNotFound, "not_found", "validação não encontrada")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, item)
}
func (s *Server) cancelValidation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p := getPrincipal(r.Context())
	var wf string
	_ = s.store.Pool.QueryRow(r.Context(), "SELECT coalesce(temporal_workflow_id,'') FROM validations WHERE organization_id=$1 AND id=$2", p.OrganizationID, id).Scan(&wf)
	if wf != "" && s.temporal != nil {
		_ = s.temporal.CancelWorkflow(r.Context(), wf, "")
	}
	if err := s.store.CancelValidation(r.Context(), p.OrganizationID, id); err != nil {
		fail(w, r, http.StatusConflict, "cannot_cancel", err.Error())
		return
	}
	s.audit(r, p, "validation.cancel", "validation", id, nil)
	write(w, http.StatusOK, map[string]string{"status": "CANCELLED"})
}

func (s *Server) registerAgent(w http.ResponseWriter, r *http.Request) {
	clientFingerprint := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Sentinel-Client-Cert-Fingerprint")))
	clientOrganization := strings.TrimSpace(r.Header.Get("X-Sentinel-Client-Organization"))
	clientName := strings.TrimSpace(r.Header.Get("X-Sentinel-Client-Name"))
	proxyAuthorized := s.mtlsProxyAuthorized(r)
	metadataSupplied := clientFingerprint != "" || clientOrganization != "" || clientName != ""
	if s.cfg.Environment == "production" && !proxyAuthorized {
		fail(w, r, http.StatusUnauthorized, "mtls_required", "registro de agente exige certificado cliente mTLS verificado")
		return
	}
	if metadataSupplied && !proxyAuthorized {
		fail(w, r, http.StatusUnauthorized, "untrusted_mtls_proxy", "metadados de certificado vieram de proxy não confiável")
		return
	}
	if proxyAuthorized && (!certificateFingerprint.MatchString(clientFingerprint) || uuid.Validate(clientOrganization) != nil || !safeName.MatchString(clientName)) {
		fail(w, r, http.StatusBadRequest, "invalid_client_certificate", "identidade do certificado cliente inválida")
		return
	}
	bootstrapToken := bearerToken(r.Header.Get("Authorization"))
	if bootstrapToken == "" {
		fail(w, r, http.StatusUnauthorized, "invalid_bootstrap_token", "bootstrap token inválido")
		return
	}
	var in domain.Agent
	if !decode(w, r, &in) {
		return
	}
	if !safeName.MatchString(in.Name) {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "nome do agente inválido")
		return
	}
	if proxyAuthorized && clientName != in.Name {
		fail(w, r, http.StatusUnauthorized, "certificate_name_mismatch", "nome do agente difere da identidade do certificado")
		return
	}
	tenantID := clientOrganization
	if tenantID == "" {
		var err error
		tenantID, err = s.store.OrganizationID(r.Context(), "local")
		if err != nil {
			s.internal(w, r, err)
			return
		}
	}
	r = r.WithContext(database.WithTenant(r.Context(), tenantID))
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		s.internal(w, r, err)
		return
	}
	token := hex.EncodeToString(tokenBytes)
	hash := sha256.Sum256([]byte(token))
	labels, _ := json.Marshal(in.Labels)
	var id string
	tx, err := s.store.Pool.Begin(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	bootstrapHash := sha256.Sum256([]byte(bootstrapToken))
	var bootstrapID, org, boundName string
	err = tx.QueryRow(r.Context(), `SELECT id::text,organization_id::text,coalesce(bound_agent_name,'') FROM agent_bootstrap_tokens WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at>now() AND use_count=0 FOR UPDATE`, bootstrapHash[:]).Scan(&bootstrapID, &org, &boundName)
	if err != nil || (boundName != "" && boundName != in.Name) || (clientOrganization != "" && clientOrganization != org) {
		fail(w, r, http.StatusUnauthorized, "invalid_bootstrap_token", "bootstrap token inválido, expirado, consumido ou vinculado a outro agente")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE agent_bootstrap_tokens SET use_count=1,used_at=now() WHERE id=$1 AND use_count=0`, bootstrapID); err != nil {
		s.internal(w, r, err)
		return
	}
	err = tx.QueryRow(r.Context(), `
		INSERT INTO agents(organization_id,name,region,cloud_provider,account,cluster,network,location,team,environment,labels,token_hash,client_cert_fingerprint)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,nullif($13,''))
		ON CONFLICT (organization_id,name) DO UPDATE SET
			region=EXCLUDED.region, cloud_provider=EXCLUDED.cloud_provider, account=EXCLUDED.account,
			cluster=EXCLUDED.cluster, network=EXCLUDED.network, location=EXCLUDED.location,
			team=EXCLUDED.team, environment=EXCLUDED.environment, labels=EXCLUDED.labels,
			token_hash=EXCLUDED.token_hash, client_cert_fingerprint=EXCLUDED.client_cert_fingerprint,
			revoked_at=NULL, updated_at=now(), version=agents.version+1
		RETURNING id::text`, org, in.Name, in.Region, in.CloudProvider, in.Account, in.Cluster, in.Network, in.Location, in.Team, in.Environment, labels, hash[:], clientFingerprint).Scan(&id)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if _, err := tx.Exec(r.Context(), "DELETE FROM agent_capabilities WHERE agent_id=$1", id); err != nil {
		s.internal(w, r, err)
		return
	}
	for _, c := range in.Capabilities {
		if _, err := tx.Exec(r.Context(), "INSERT INTO agent_capabilities(agent_id,capability) VALUES($1,$2) ON CONFLICT DO NOTHING", id, c); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.internal(w, r, err)
		return
	}
	writeStatus(w, http.StatusCreated, map[string]any{"id": id, "token": token, "warning": "o token é exibido uma única vez"})
}

func (s *Server) createAgentBootstrapToken(w http.ResponseWriter, r *http.Request) {
	var in struct {
		AgentName string `json:"agentName"`
		TTL       string `json:"ttl"`
	}
	if !decodeOptional(w, r, &in) {
		return
	}
	if in.AgentName != "" && !safeName.MatchString(in.AgentName) {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "agentName inválido")
		return
	}
	ttl := 10 * time.Minute
	var err error
	if in.TTL != "" {
		ttl, err = time.ParseDuration(in.TTL)
		if err != nil || ttl < time.Minute || ttl > time.Hour {
			fail(w, r, http.StatusUnprocessableEntity, "validation_error", "ttl deve estar entre 1m e 1h")
			return
		}
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		s.internal(w, r, err)
		return
	}
	token := hex.EncodeToString(tokenBytes)
	hash := sha256.Sum256([]byte(token))
	p := getPrincipal(r.Context())
	expiresAt := time.Now().UTC().Add(ttl)
	var id string
	err = s.store.Pool.QueryRow(r.Context(), `INSERT INTO agent_bootstrap_tokens(organization_id,token_hash,bound_agent_name,expires_at,created_by) VALUES($1,$2,nullif($3,''),$4,$5) RETURNING id::text`, p.OrganizationID, hash[:], in.AgentName, expiresAt, p.Subject).Scan(&id)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, p, "agent.bootstrap-token.create", "agent_bootstrap_token", id, map[string]any{"agentName": in.AgentName, "expiresAt": expiresAt})
	writeStatus(w, http.StatusCreated, map[string]any{"id": id, "organizationId": p.OrganizationID, "token": token, "expiresAt": expiresAt, "maxUses": 1, "warning": "o token é exibido uma única vez"})
}
func (s *Server) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	token := r.Header.Get("X-Agent-Token")
	sum := sha256.Sum256([]byte(token))
	clientFingerprint := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Sentinel-Client-Cert-Fingerprint")))
	clientOrganization := strings.TrimSpace(r.Header.Get("X-Sentinel-Client-Organization"))
	clientName := strings.TrimSpace(r.Header.Get("X-Sentinel-Client-Name"))
	proxyAuthorized := s.mtlsProxyAuthorized(r)
	metadataSupplied := clientFingerprint != "" || clientOrganization != "" || clientName != ""
	if s.cfg.Environment == "production" && !proxyAuthorized {
		fail(w, r, http.StatusUnauthorized, "mtls_required", "heartbeat exige certificado cliente mTLS verificado")
		return
	}
	if metadataSupplied && !proxyAuthorized {
		fail(w, r, http.StatusUnauthorized, "untrusted_mtls_proxy", "metadados de certificado vieram de proxy não confiável")
		return
	}
	if proxyAuthorized && (!certificateFingerprint.MatchString(clientFingerprint) || uuid.Validate(clientOrganization) != nil || !safeName.MatchString(clientName)) {
		fail(w, r, http.StatusBadRequest, "invalid_client_certificate", "identidade do certificado cliente inválida")
		return
	}
	tenantID := clientOrganization
	if tenantID == "" {
		var err error
		tenantID, err = s.store.OrganizationID(r.Context(), "local")
		if err != nil {
			s.internal(w, r, err)
			return
		}
	}
	r = r.WithContext(database.WithTenant(r.Context(), tenantID))
	var ok bool
	err := s.store.Pool.QueryRow(r.Context(), `SELECT token_hash=$2 AND revoked_at IS NULL
AND (client_cert_fingerprint IS NULL OR client_cert_fingerprint=$3)
AND ($4::boolean=false OR (client_cert_fingerprint IS NOT NULL AND organization_id::text=$5 AND name=$6)) FROM agents WHERE id=$1`, id, sum[:], clientFingerprint, s.cfg.Environment == "production" || proxyAuthorized, clientOrganization, clientName).Scan(&ok)
	if err != nil || !ok {
		fail(w, r, http.StatusUnauthorized, "invalid_agent_token", "token do agente inválido")
		return
	}
	if !s.allowTenant(w, r, tenantID, "agent") {
		return
	}
	var status map[string]any
	if !decodeOptional(w, r, &status) {
		return
	}
	_, err = s.store.Pool.Exec(r.Context(), "INSERT INTO agent_heartbeats(agent_id,status) VALUES($1,$2)", id, status)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, map[string]any{"status": "accepted", "serverTime": time.Now().UTC()})
}

func (s *Server) agentInventoryReconcile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	token := r.Header.Get("X-Agent-Token")
	sum := sha256.Sum256([]byte(token))
	clientFingerprint := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Sentinel-Client-Cert-Fingerprint")))
	clientOrganization := strings.TrimSpace(r.Header.Get("X-Sentinel-Client-Organization"))
	clientName := strings.TrimSpace(r.Header.Get("X-Sentinel-Client-Name"))
	proxyAuthorized := s.mtlsProxyAuthorized(r)
	metadataSupplied := clientFingerprint != "" || clientOrganization != "" || clientName != ""
	if s.cfg.Environment == "production" && !proxyAuthorized {
		fail(w, r, http.StatusUnauthorized, "mtls_required", "reconciliação de inventário exige certificado cliente mTLS verificado")
		return
	}
	if metadataSupplied && !proxyAuthorized {
		fail(w, r, http.StatusUnauthorized, "untrusted_mtls_proxy", "metadados de certificado vieram de proxy não confiável")
		return
	}
	if proxyAuthorized && (!certificateFingerprint.MatchString(clientFingerprint) || uuid.Validate(clientOrganization) != nil || !safeName.MatchString(clientName)) {
		fail(w, r, http.StatusBadRequest, "invalid_client_certificate", "identidade do certificado cliente inválida")
		return
	}
	var organizationID, name string
	err := s.store.Pool.QueryRow(r.Context(), `SELECT organization_id::text,name FROM agents a
WHERE a.id=$1 AND a.token_hash=$2 AND a.revoked_at IS NULL
AND EXISTS (SELECT 1 FROM agent_capabilities c WHERE c.agent_id=a.id AND c.capability='inventory:write')
AND ($3::boolean=false OR (a.client_cert_fingerprint IS NOT NULL AND a.organization_id::text=$4 AND a.name=$5))`,
		id, sum[:], s.cfg.Environment == "production" || proxyAuthorized, clientOrganization, clientName).Scan(&organizationID, &name)
	if err != nil {
		fail(w, r, http.StatusUnauthorized, "inventory_capability_required", "agente sem credencial ou capability inventory:write")
		return
	}
	if !s.allowTenant(w, r, organizationID, "agent") {
		return
	}
	var in struct {
		ObservedAt time.Time      `json:"observedAt"`
		Complete   bool           `json:"complete"`
		Assets     []domain.Asset `json:"assets"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !in.Complete || len(in.Assets) > 1000 {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "complete=true e no máximo 1000 ativos são obrigatórios")
		return
	}
	seen := map[string]bool{}
	for i := range in.Assets {
		asset := &in.Assets[i]
		asset.AssetID, asset.Name, asset.Kind = strings.TrimSpace(asset.AssetID), strings.TrimSpace(asset.Name), strings.TrimSpace(asset.Kind)
		if !safeAssetID.MatchString(asset.AssetID) || asset.Name == "" || asset.Kind == "" || seen[asset.AssetID] {
			fail(w, r, http.StatusUnprocessableEntity, "validation_error", "cada ativo requer assetId único, name e kind")
			return
		}
		seen[asset.AssetID] = true
	}
	items, err := s.store.ReconcileAssets(r.Context(), organizationID, "agent:"+id, "agent-"+name, in.ObservedAt, in.Assets)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	_ = s.store.Audit(database.WithTenant(r.Context(), organizationID), organizationID, "agent:"+id, "asset.reconcile", "asset-source", "agent-"+name, requestID(r), "", map[string]any{"count": len(items), "complete": true})
	write(w, http.StatusOK, items)
}

func (s *Server) mtlsProxyAuthorized(r *http.Request) bool {
	provided := r.Header.Get("X-Sentinel-MTLS-Proxy-Authorization")
	return s.cfg.MTLSProxySecret != "" && hmac.Equal([]byte(provided), []byte(s.cfg.MTLSProxySecret))
}

func (s *Server) revokeAgent(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	if err := s.store.RevokeAgent(r.Context(), p.OrganizationID, r.PathValue("id")); database.IsNotFound(err) {
		fail(w, r, http.StatusNotFound, "not_found", "agente não encontrado")
		return
	} else if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, p, "agent.revoke", "agent", r.PathValue("id"), nil)
	write(w, http.StatusOK, map[string]string{"status": "REVOKED"})
}

func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	rows, err := s.store.Pool.Query(r.Context(), `SELECT a.id::text,a.name,coalesce(a.region,''),coalesce(a.cloud_provider,''),coalesce(a.account,''),coalesce(a.cluster,''),coalesce(a.network,''),coalesce(a.location,''),coalesce(a.team,''),coalesce(a.environment,''),a.labels,(SELECT max(observed_at) FROM agent_heartbeats h WHERE h.agent_id=a.id) FROM agents a WHERE a.organization_id=$1 AND a.revoked_at IS NULL ORDER BY a.name`, p.OrganizationID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []domain.Agent{}
	for rows.Next() {
		var a domain.Agent
		var labels []byte
		if err := rows.Scan(&a.ID, &a.Name, &a.Region, &a.CloudProvider, &a.Account, &a.Cluster, &a.Network, &a.Location, &a.Team, &a.Environment, &labels, &a.LastHeartbeat); err != nil {
			s.internal(w, r, err)
			return
		}
		_ = json.Unmarshal(labels, &a.Labels)
		a.Status = "offline"
		if a.LastHeartbeat != nil && time.Since(*a.LastHeartbeat) < 90*time.Second {
			a.Status = "online"
		}
		items = append(items, a)
	}
	write(w, http.StatusOK, items)
}

func (s *Server) applyScenario(w http.ResponseWriter, r *http.Request) {
	var in domain.Scenario
	if !decode(w, r, &in) {
		return
	}
	if !safeName.MatchString(in.Name) || !safeName.MatchString(in.ServiceRef) || !safeName.MatchString(in.Environment) {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "name, serviceRef e environment devem ser DNS-safe")
		return
	}
	if in.Type != "http" {
		fail(w, r, http.StatusUnprocessableEntity, "unsupported_capability", "somente cenários HTTP possuem runner distribuído habilitado; browser e k6 permanecem desabilitados")
		return
	}
	if err := synthetics.ValidateHTTPScenarioSpec(in.Spec); err != nil {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "spec HTTP inválida: "+err.Error())
		return
	}
	p := getPrincipal(r.Context())
	spec, _ := json.Marshal(in.Spec)
	sum := sha256.Sum256(spec)
	tx, err := s.store.Pool.Begin(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var id string
	var version int
	err = tx.QueryRow(r.Context(), `INSERT INTO synthetic_scenarios(organization_id,name,service_ref,environment,type,enabled,created_by) VALUES($1,$2,$3,$4,$5,true,$6) ON CONFLICT(organization_id,name) DO UPDATE SET service_ref=EXCLUDED.service_ref,environment=EXCLUDED.environment,type=EXCLUDED.type,current_version=synthetic_scenarios.current_version+1,updated_at=now() RETURNING id::text,current_version`, p.OrganizationID, in.Name, in.ServiceRef, in.Environment, in.Type, p.Subject).Scan(&id, &version)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO synthetic_scenario_versions(scenario_id,version,spec,checksum,created_by) VALUES($1,$2,$3,$4,$5)`, id, version, spec, hex.EncodeToString(sum[:]), p.Subject)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, r, err)
		return
	}
	in.ID = id
	in.Version = version
	in.Enabled = true
	s.audit(r, p, "scenario.apply", "scenario", id, map[string]any{"version": version})
	write(w, http.StatusOK, in)
}
func (s *Server) listScenarios(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	rows, err := s.store.Pool.Query(r.Context(), `SELECT s.id::text,s.name,s.service_ref,s.environment,s.type,s.enabled,s.current_version,v.spec FROM synthetic_scenarios s JOIN synthetic_scenario_versions v ON v.scenario_id=s.id AND v.version=s.current_version WHERE s.organization_id=$1 ORDER BY s.name`, p.OrganizationID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []domain.Scenario{}
	for rows.Next() {
		var v domain.Scenario
		var spec []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.ServiceRef, &v.Environment, &v.Type, &v.Enabled, &v.Version, &spec); err != nil {
			s.internal(w, r, err)
			return
		}
		_ = json.Unmarshal(spec, &v.Spec)
		items = append(items, v)
	}
	write(w, http.StatusOK, items)
}
func (s *Server) listSyntheticRuns(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			fail(w, r, http.StatusUnprocessableEntity, "validation_error", "limit deve estar entre 1 e 200")
			return
		}
		limit = parsed
	}
	rows, err := s.store.Pool.Query(r.Context(), `SELECT r.id::text,r.scenario_id::text,s.name,r.status,r.started_at,r.finished_at,r.result
FROM test_runs r JOIN synthetic_scenarios s ON s.id=r.scenario_id
WHERE r.organization_id=$1 ORDER BY r.started_at DESC NULLS LAST,r.created_at DESC LIMIT $2`, p.OrganizationID, limit)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []domain.SyntheticRun{}
	for rows.Next() {
		var item domain.SyntheticRun
		var result []byte
		if err := rows.Scan(&item.ID, &item.ScenarioID, &item.Scenario, &item.Status, &item.StartedAt, &item.FinishedAt, &result); err != nil {
			s.internal(w, r, err)
			return
		}
		if err := json.Unmarshal(result, &item.Result); err != nil {
			item.Result = map[string]any{"warning": "resultado armazenado inválido"}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, items)
}
func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	rows, err := s.store.Pool.Query(r.Context(), `SELECT id::text,title,severity,status,coalesce(commander,''),coalesce(summary,''),coalesce(deduplication_key,''),acknowledged_at,coalesce(acknowledged_by,''),created_at,resolved_at
FROM incidents WHERE organization_id=$1 ORDER BY created_at DESC LIMIT 200`, p.OrganizationID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []domain.Incident{}
	for rows.Next() {
		var item domain.Incident
		if err := rows.Scan(&item.ID, &item.Title, &item.Severity, &item.Status, &item.Commander, &item.Summary, &item.DeduplicationKey, &item.AcknowledgedAt, &item.AcknowledgedBy, &item.CreatedAt, &item.ResolvedAt); err != nil {
			s.internal(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, items)
}
func (s *Server) createIncident(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title            string `json:"title"`
		Severity         string `json:"severity"`
		Summary          string `json:"summary"`
		Commander        string `json:"commander"`
		DeduplicationKey string `json:"deduplicationKey"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Title, in.Severity = strings.TrimSpace(in.Title), strings.ToUpper(strings.TrimSpace(in.Severity))
	if in.Title == "" || len(in.Title) > 240 || (in.Severity != "P1" && in.Severity != "P2" && in.Severity != "P3" && in.Severity != "P4") {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "title e severidade P1-P4 são obrigatórios")
		return
	}
	p := getPrincipal(r.Context())
	tx, err := s.store.Pool.Begin(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var item domain.Incident
	err = tx.QueryRow(r.Context(), `INSERT INTO incidents(organization_id,title,severity,status,commander,summary,deduplication_key)
VALUES($1,$2,$3,'OPEN',nullif($4,''),nullif($5,''),nullif($6,''))
ON CONFLICT (organization_id,deduplication_key) WHERE deduplication_key IS NOT NULL AND status <> 'RESOLVED'
DO UPDATE SET updated_at=now() RETURNING id::text,title,severity,status,coalesce(commander,''),coalesce(summary,''),coalesce(deduplication_key,''),acknowledged_at,coalesce(acknowledged_by,''),created_at,resolved_at`, p.OrganizationID, in.Title, in.Severity, in.Commander, in.Summary, in.DeduplicationKey).Scan(&item.ID, &item.Title, &item.Severity, &item.Status, &item.Commander, &item.Summary, &item.DeduplicationKey, &item.AcknowledgedAt, &item.AcknowledgedBy, &item.CreatedAt, &item.ResolvedAt)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	payload, _ := json.Marshal(map[string]any{"incidentId": item.ID, "severity": item.Severity, "title": item.Title})
	if _, err = tx.Exec(r.Context(), `INSERT INTO incident_events(incident_id,type,payload,created_by) VALUES($1,'CREATED',$2,$3)`, item.ID, payload, p.Subject); err != nil {
		s.internal(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO outbox_events(organization_id,event_type,payload) VALUES($1,'incident.created',$2)`, p.OrganizationID, payload); err != nil {
		s.internal(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(organization_id,actor,action,resource_type,resource_id,request_id,payload) VALUES($1,$2,'incident.create','incident',$3,$4,$5)`, p.OrganizationID, p.Subject, item.ID, requestID(r), payload); err != nil {
		s.internal(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusCreated, item)
}
func (s *Server) ackIncident(w http.ResponseWriter, r *http.Request) {
	s.transitionIncident(w, r, "ACKNOWLEDGED", "incident.ack")
}
func (s *Server) resolveIncident(w http.ResponseWriter, r *http.Request) {
	s.transitionIncident(w, r, "RESOLVED", "incident.resolve")
}
func (s *Server) transitionIncident(w http.ResponseWriter, r *http.Request, status, eventType string) {
	p := getPrincipal(r.Context())
	id := r.PathValue("id")
	if uuid.Validate(id) != nil {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "incident id inválido")
		return
	}
	tx, err := s.store.Pool.Begin(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var item domain.Incident
	query := `UPDATE incidents SET status=$3, acknowledged_at=CASE WHEN $3='ACKNOWLEDGED' THEN now() ELSE acknowledged_at END, acknowledged_by=CASE WHEN $3='ACKNOWLEDGED' THEN $4 ELSE acknowledged_by END,resolved_at=CASE WHEN $3='RESOLVED' THEN now() ELSE resolved_at END,updated_at=now()
WHERE id=$1 AND organization_id=$2 AND status <> 'RESOLVED' RETURNING id::text,title,severity,status,coalesce(commander,''),coalesce(summary,''),coalesce(deduplication_key,''),acknowledged_at,coalesce(acknowledged_by,''),created_at,resolved_at`
	err = tx.QueryRow(r.Context(), query, id, p.OrganizationID, status, p.Subject).Scan(&item.ID, &item.Title, &item.Severity, &item.Status, &item.Commander, &item.Summary, &item.DeduplicationKey, &item.AcknowledgedAt, &item.AcknowledgedBy, &item.CreatedAt, &item.ResolvedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, http.StatusConflict, "invalid_transition", "incidente inexistente ou já resolvido")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	payload, _ := json.Marshal(map[string]any{"incidentId": id, "status": status, "actor": p.Subject})
	if _, err = tx.Exec(r.Context(), `INSERT INTO incident_events(incident_id,type,payload,created_by) VALUES($1,$2,$3,$4)`, id, eventType, payload, p.Subject); err != nil {
		s.internal(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO outbox_events(organization_id,event_type,payload) VALUES($1,$2,$3)`, p.OrganizationID, eventType, payload); err != nil {
		s.internal(w, r, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(organization_id,actor,action,resource_type,resource_id,request_id,payload) VALUES($1,$2,$3,'incident',$4,$5,$6)`, p.OrganizationID, p.Subject, eventType, id, requestID(r), payload); err != nil {
		s.internal(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, item)
}
func (s *Server) listIncidentDeliveries(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	incidentID := r.PathValue("id")
	if uuid.Validate(incidentID) != nil {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "incident id inválido")
		return
	}
	rows, err := s.store.Pool.Query(r.Context(), `SELECT id::text,coalesce(route_id::text,''),status,attempts,coalesce(last_error,''),created_at,delivered_at FROM notification_deliveries WHERE organization_id=$1 AND incident_id=$2 ORDER BY created_at`, p.OrganizationID, incidentID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []domain.NotificationDelivery{}
	for rows.Next() {
		var item domain.NotificationDelivery
		if err := rows.Scan(&item.ID, &item.RouteID, &item.Status, &item.Attempts, &item.LastError, &item.CreatedAt, &item.DeliveredAt); err != nil {
			s.internal(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, items)
}

func (s *Server) listIncidentEscalations(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	rows, err := s.store.Pool.Query(r.Context(), `SELECT id::text,route_id::text,target_ref,status,scheduled_at,cancelled_at,escalated_at
FROM incident_escalations WHERE organization_id=$1 AND incident_id=$2 ORDER BY scheduled_at`, p.OrganizationID, r.PathValue("id"))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []domain.IncidentEscalation{}
	for rows.Next() {
		var item domain.IncidentEscalation
		if err := rows.Scan(&item.ID, &item.RouteID, &item.TargetRef, &item.Status, &item.ScheduledAt, &item.CancelledAt, &item.EscalatedAt); err != nil {
			s.internal(w, r, err)
			return
		}
		items = append(items, item)
	}
	write(w, http.StatusOK, items)
}
func (s *Server) listAlertRoutes(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	rows, err := s.store.Pool.Query(r.Context(), "SELECT id::text,name,spec,enabled FROM alert_routes WHERE organization_id=$1 ORDER BY name", p.OrganizationID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer rows.Close()
	items := []domain.AlertRoute{}
	for rows.Next() {
		var item domain.AlertRoute
		var raw []byte
		if err := rows.Scan(&item.ID, &item.Name, &raw, &item.Enabled); err != nil {
			s.internal(w, r, err)
			return
		}
		if err := json.Unmarshal(raw, &item.Spec); err != nil {
			s.internal(w, r, err)
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, items)
}
func (s *Server) applyAlertRoute(w http.ResponseWriter, r *http.Request) {
	var in domain.AlertRoute
	if !decode(w, r, &in) {
		return
	}
	if !safeName.MatchString(in.Name) {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "name deve ser DNS-safe")
		return
	}
	var route struct {
		Channel    string `json:"channel"`
		TargetRef  string `json:"targetRef"`
		Escalation *struct {
			After     string `json:"after"`
			TargetRef string `json:"targetRef"`
		} `json:"escalation,omitempty"`
	}
	raw, _ := json.Marshal(in.Spec)
	if err := json.Unmarshal(raw, &route); err != nil || route.Channel != "webhook" || !safeName.MatchString(route.TargetRef) {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "spec requer channel=webhook e targetRef DNS-safe")
		return
	}
	if route.Escalation != nil {
		after, err := time.ParseDuration(route.Escalation.After)
		if err != nil || after < time.Minute || after > 7*24*time.Hour || !safeName.MatchString(route.Escalation.TargetRef) {
			fail(w, r, http.StatusUnprocessableEntity, "validation_error", "escalation requer after entre 1m e 168h e targetRef DNS-safe")
			return
		}
	}
	p := getPrincipal(r.Context())
	if !in.Enabled {
		in.Enabled = false
	} else {
		in.Enabled = true
	}
	var out domain.AlertRoute
	var stored []byte
	err := s.store.Pool.QueryRow(r.Context(), `INSERT INTO alert_routes(organization_id,name,spec,enabled) VALUES($1,$2,$3,$4) ON CONFLICT (organization_id,name) DO UPDATE SET spec=EXCLUDED.spec,enabled=EXCLUDED.enabled RETURNING id::text,name,spec,enabled`, p.OrganizationID, in.Name, raw, in.Enabled).Scan(&out.ID, &out.Name, &stored, &out.Enabled)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	_ = json.Unmarshal(stored, &out.Spec)
	s.audit(r, p, "alert-route.apply", "alert-route", out.ID, map[string]any{"name": out.Name, "enabled": out.Enabled})
	write(w, http.StatusOK, out)
}

type dataLifecycleRequestInput struct {
	RequestType string             `json:"requestType"`
	Reason      string             `json:"reason"`
	Scope       dataLifecycleScope `json:"scope"`
}

// dataLifecycleScope is intentionally small and explicit. It identifies a
// person or system reference without accepting raw account data, while the
// approved domain list prevents an accidental request for an unbounded tenant
// export or deletion.
type dataLifecycleScope struct {
	SubjectRef string   `json:"subjectRef"`
	Domains    []string `json:"domains"`
}

var lifecycleDomains = map[string]struct{}{
	"control-plane-metadata": {},
	"operational-evidence":   {},
	"telemetry-references":   {},
}

func validDataLifecycleScope(scope dataLifecycleScope) bool {
	if !safeAssetID.MatchString(scope.SubjectRef) || len(scope.Domains) == 0 || len(scope.Domains) > len(lifecycleDomains) {
		return false
	}
	seen := make(map[string]struct{}, len(scope.Domains))
	for _, domain := range scope.Domains {
		if _, allowed := lifecycleDomains[domain]; !allowed {
			return false
		}
		if _, duplicate := seen[domain]; duplicate {
			return false
		}
		seen[domain] = struct{}{}
	}
	return true
}

func sourceIP(r *http.Request) string {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

func (s *Server) listDataLifecycleRequests(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	items, err := s.store.ListDataLifecycleRequests(r.Context(), p.OrganizationID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, items)
}

func (s *Server) createDataLifecycleRequest(w http.ResponseWriter, r *http.Request) {
	var in dataLifecycleRequestInput
	if !decode(w, r, &in) {
		return
	}
	in.RequestType = strings.TrimSpace(in.RequestType)
	in.Reason = strings.TrimSpace(in.Reason)
	if (in.RequestType != "export" && in.RequestType != "erasure") || len(in.Reason) == 0 || len(in.Reason) > 1024 || !validDataLifecycleScope(in.Scope) {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "requestType deve ser export ou erasure; reason deve ter até 1024 caracteres; scope requer subjectRef e domains permitidos sem duplicatas")
		return
	}
	p := getPrincipal(r.Context())
	item, err := s.store.CreateDataLifecycleRequest(r.Context(), p.OrganizationID, p.Subject, requestID(r), sourceIP(r), domain.DataLifecycleRequest{
		RequestType: in.RequestType,
		Reason:      in.Reason,
		Scope:       map[string]any{"subjectRef": in.Scope.SubjectRef, "domains": in.Scope.Domains},
	})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeStatus(w, http.StatusCreated, item)
}

func (s *Server) approveDataLifecycleRequest(w http.ResponseWriter, r *http.Request) {
	p := getPrincipal(r.Context())
	id := r.PathValue("id")
	if uuid.Validate(id) != nil {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "solicitação de ciclo de vida id inválida")
		return
	}
	item, err := s.store.ApproveDataLifecycleRequest(r.Context(), p.OrganizationID, p.Subject, id, requestID(r), sourceIP(r))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, http.StatusConflict, "invalid_transition", "solicitação inexistente, já tratada ou solicitada pelo mesmo aprovador")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, item)
}

func (s *Server) executeDataLifecycleRequest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirm string `json:"confirm"`
	}
	if !decode(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	if uuid.Validate(id) != nil || in.Confirm != "ERASE_CONTROL_PLANE_METADATA" {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "id válido e confirmação ERASE_CONTROL_PLANE_METADATA são obrigatórios")
		return
	}
	p := getPrincipal(r.Context())
	item, err := s.store.ExecuteControlPlaneErasure(r.Context(), p.OrganizationID, p.Subject, id, requestID(r), sourceIP(r))
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, r, http.StatusConflict, "invalid_transition", "somente solicitação aprovada de erasure com domínio exclusivo control-plane-metadata pode ser executada")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, item)
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	organization := strings.TrimSpace(r.Header.Get("X-Sentinel-Organization"))
	secret := s.cfg.WebhookSecrets[organization]
	if organization == "" || secret == "" {
		fail(w, r, http.StatusServiceUnavailable, "webhook_disabled", "webhooks não configurados")
		return
	}
	timestamp := r.Header.Get("X-Sentinel-Timestamp")
	nonce := r.Header.Get("X-Sentinel-Nonce")
	idem := r.Header.Get("Idempotency-Key")
	signature := strings.TrimPrefix(r.Header.Get("X-Sentinel-Signature"), "sha256=")
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Until(time.Unix(unix, 0)) > time.Minute {
		fail(w, r, http.StatusUnauthorized, "stale_webhook", "timestamp inválido ou expirado")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(organization + "." + timestamp + "." + nonce + "."))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	provided, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal([]byte(expected), []byte(hex.EncodeToString(provided))) || nonce == "" || idem == "" {
		fail(w, r, http.StatusUnauthorized, "invalid_signature", "assinatura, nonce ou idempotency key inválidos")
		return
	}
	org, err := s.store.OrganizationID(r.Context(), organization)
	if err != nil {
		fail(w, r, http.StatusUnauthorized, "unknown_organization", "organização inválida")
		return
	}
	r = r.WithContext(database.WithTenant(r.Context(), org))
	integration := r.PathValue("")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	integration = parts[len(parts)-1]
	payloadHash := sha256.Sum256(body)
	tx, err := s.store.Pool.Begin(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer func() { _ = tx.Rollback(r.Context()) }()
	var deliveryID string
	err = tx.QueryRow(r.Context(), `INSERT INTO webhook_deliveries(organization_id,integration,idempotency_key,nonce,signature,status,payload_sha256,payload_size)
VALUES($1,$2,$3,$4,$5,'ACCEPTED',$6,$7) RETURNING id::text`, org, integration, idem, nonce, signature, hex.EncodeToString(payloadHash[:]), len(body)).Scan(&deliveryID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "duplicate key") {
			fail(w, r, http.StatusConflict, "replay_detected", "webhook duplicado ou replay detectado")
			return
		}
		s.internal(w, r, err)
		return
	}
	eventPayload, _ := json.Marshal(map[string]any{"deliveryId": deliveryID, "integration": integration, "payloadSha256": hex.EncodeToString(payloadHash[:]), "payloadSize": len(body), "receivedAt": time.Now().UTC().Format(time.RFC3339Nano)})
	if _, err = tx.Exec(r.Context(), `INSERT INTO outbox_events(organization_id,event_type,payload) VALUES($1,'webhook.received',$2)`, org, eventPayload); err != nil {
		s.internal(w, r, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.internal(w, r, err)
		return
	}
	writeStatus(w, http.StatusAccepted, map[string]string{"status": "ACCEPTED", "deliveryId": deliveryID})
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, r, http.StatusNotImplemented, "stream_unsupported", "streaming indisponível")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	p := getPrincipal(r.Context())
	cursor, _ := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed >= 0 {
			cursor = parsed
		}
	}
	ticker := time.NewTicker(2 * time.Second)
	heartbeat := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	fmt.Fprintf(w, "event: connected\ndata: {\"time\":%q,\"cursor\":%d}\n\n", time.Now().UTC().Format(time.RFC3339), cursor)
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			rows, err := s.store.Pool.Query(r.Context(), `SELECT id,event_type,payload,delivered_at FROM outbox_events
WHERE organization_id=$1 AND status='DELIVERED' AND id>$2 ORDER BY id LIMIT 100`, p.OrganizationID, cursor)
			if err != nil {
				return
			}
			for rows.Next() {
				var id int64
				var eventType string
				var payload []byte
				var observedAt time.Time
				if err := rows.Scan(&id, &eventType, &payload, &observedAt); err != nil {
					continue
				}
				data, _ := json.Marshal(map[string]any{"eventId": id, "data": json.RawMessage(payload), "observedAt": observedAt.UTC().Format(time.RFC3339Nano), "partial": false})
				fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", id, eventType, data)
				cursor = id
			}
			rows.Close()
			flusher.Flush()
		case t := <-heartbeat.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: {\"time\":%q}\n\n", t.UTC().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}

func (s *Server) require(permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := s.auth.ParseAuthorization(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			s.logger.Warn("oidc authorization rejected", "route", r.URL.Path, "reason", err.Error())
			fail(w, r, http.StatusUnauthorized, "unauthorized", err.Error())
			return
		}
		organizationID, err := s.store.OrganizationID(r.Context(), claims.Organization)
		if err != nil {
			fail(w, r, http.StatusForbidden, "unknown_organization", "organização não provisionada")
			return
		}
		tenantCtx := database.WithTenant(r.Context(), organizationID)
		role := claims.Role
		if s.cfg.AuthMode == "oidc" {
			role, err = s.store.EffectiveRole(tenantCtx, organizationID, claims.Subject)
			if err != nil {
				fail(w, r, http.StatusForbidden, "role_binding_required", "identidade sem vínculo RBAC provisionado para a organização")
				return
			}
		}
		if !auth.Can(role, permission) {
			fail(w, r, http.StatusForbidden, "forbidden", "permissão insuficiente")
			return
		}
		if !s.allowTenant(w, r, organizationID, "api") {
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(tenantCtx, principalKey, principal{Subject: claims.Subject, Role: role, Organization: claims.Organization, OrganizationID: organizationID})))
	})
}

func (s *Server) allowTenant(w http.ResponseWriter, r *http.Request, organizationID, scope string) bool {
	accepted, err := s.store.ConsumeTenantRequestQuota(r.Context(), organizationID, s.cfg.TenantRequestsPerMinute)
	if err != nil {
		s.logger.Error("tenant quota check failed", "scope", scope, "request_id", requestID(r), "error", err)
		fail(w, r, http.StatusServiceUnavailable, "tenant_quota_unavailable", "quota da organização indisponível")
		return false
	}
	if accepted {
		return true
	}
	s.quotaRejects.WithLabelValues(scope).Inc()
	w.Header().Set("Retry-After", "60")
	fail(w, r, http.StatusTooManyRequests, "tenant_quota_exceeded", "quota de requisições da organização excedida; tente novamente após a janela")
	return false
}

func (s *Server) allowTenantScoped(w http.ResponseWriter, r *http.Request, organizationID, scope string, limit int) bool {
	accepted, err := s.store.ConsumeTenantScopedRequestQuota(r.Context(), organizationID, scope, limit)
	if err != nil {
		s.logger.Error("tenant scoped quota check failed", "scope", scope, "request_id", requestID(r), "error", err)
		fail(w, r, http.StatusServiceUnavailable, "tenant_quota_unavailable", "quota da organização indisponível")
		return false
	}
	if accepted {
		return true
	}
	s.quotaRejects.WithLabelValues(scope).Inc()
	w.Header().Set("Retry-After", "60")
	fail(w, r, http.StatusTooManyRequests, "tenant_quota_exceeded", "quota de consultas da organização excedida; tente novamente após a janela")
	return false
}
func getPrincipal(ctx context.Context) principal {
	p, _ := ctx.Value(principalKey).(principal)
	return p
}
func (s *Server) audit(r *http.Request, p principal, action, typ, id string, payload any) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if err := s.store.Audit(r.Context(), p.OrganizationID, p.Subject, action, typ, id, requestID(r), ip, payload); err != nil {
		s.logger.Error("audit write failed", "error", err, "action", action, "request_id", requestID(r))
	}
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && origin == s.cfg.AllowedOrigin {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,Idempotency-Key")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxBodyBytes)
		next.ServeHTTP(w, r)
	})
}
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-ID", id)
		r.Header.Set("X-Request-ID", id)
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		start := time.Now()
		next.ServeHTTP(rec, r)
		route := routeLabel(r)
		s.requests.WithLabelValues(r.Method, route, strconv.Itoa(rec.status)).Inc()
		s.duration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		s.logger.Info("http_request", "method", r.Method, "route", route, "status", rec.status, "duration_ms", time.Since(start).Milliseconds(), "request_id", id)
	})
}
func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				s.logger.Error("panic recovered", "panic", v, "request_id", requestID(r))
				fail(w, r, http.StatusInternalServerError, "internal_error", "erro interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Error("request failed", "error", err, "request_id", requestID(r))
	fail(w, r, http.StatusInternalServerError, "internal_error", "erro interno; consulte o requestId")
}

func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		fail(w, r, http.StatusBadRequest, "invalid_json", "JSON inválido: "+err.Error())
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		fail(w, r, http.StatusBadRequest, "invalid_json", "apenas um objeto JSON é permitido")
		return false
	}
	return true
}
func decodeOptional(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.ContentLength == 0 {
		return true
	}
	return decode(w, r, out)
}
func write(w http.ResponseWriter, status int, data any) { writeStatus(w, status, data) }
func writeStatus(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response{Data: data})
}
func fail(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response{Error: &apiError{Code: code, Message: message, RequestID: requestID(r)}})
}
func requestID(r *http.Request) string { return r.Header.Get("X-Request-ID") }
func routeLabel(r *http.Request) string {
	if p := r.Pattern; p != "" {
		return p
	}
	return "unmatched"
}
func bearerToken(header string) string {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(s int) { r.status = s; r.ResponseWriter.WriteHeader(s) }
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type ipLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]*limitEntry
}
type limitEntry struct {
	start time.Time
	count int
}

func newIPLimiter(limit int, window time.Duration) *ipLimiter {
	return &ipLimiter{limit: limit, window: window, entries: map[string]*limitEntry{}}
}
func (l *ipLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e := l.entries[ip]
	if e == nil || now.Sub(e.start) > l.window {
		l.entries[ip] = &limitEntry{start: now, count: 1}
		return true
	}
	e.count++
	return e.count <= l.limit
}
func (s *Server) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if !s.limiter.allow(ip) {
			fail(w, r, http.StatusTooManyRequests, "rate_limited", "limite de requisições excedido")
			return
		}
		next.ServeHTTP(w, r)
	})
}
