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
	"github.com/sentinelops/sentinelops/internal/workflows"
	"go.temporal.io/sdk/client"
)

type WorkflowStarter interface {
	ExecuteWorkflow(context.Context, client.StartWorkflowOptions, interface{}, ...interface{}) (client.WorkflowRun, error)
	CancelWorkflow(context.Context, string, string) error
}

type Server struct {
	cfg       config.Config
	store     *database.Store
	auth      auth.Authenticator
	localAuth *auth.Manager
	temporal  WorkflowStarter
	logger    *slog.Logger
	mux       *http.ServeMux
	requests  *prometheus.CounterVec
	duration  *prometheus.HistogramVec
	limiter   *ipLimiter
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
	Subject string
	Role    string
}
type contextKey string

const principalKey contextKey = "principal"

var safeName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

func New(ctx context.Context, cfg config.Config, store *database.Store, temporal WorkflowStarter, logger *slog.Logger, registry *prometheus.Registry) (*Server, error) {
	var authenticator auth.Authenticator
	var local *auth.Manager
	if cfg.AuthMode == "local" {
		local = auth.New(cfg.JWTSecret, cfg.LocalUser, cfg.LocalPasswordHash)
		authenticator = local
	} else {
		oidcAuth, err := auth.NewOIDC(ctx, cfg.OIDCIssuerURL, cfg.OIDCClientID)
		if err != nil {
			return nil, fmt.Errorf("initialize OIDC: %w", err)
		}
		authenticator = oidcAuth
	}
	s := &Server{cfg: cfg, store: store, auth: authenticator, localAuth: local, temporal: temporal, logger: logger, mux: http.NewServeMux(), limiter: newIPLimiter(120, time.Minute),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "sentinel_http_requests_total", Help: "HTTP requests processed."}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "sentinel_http_request_duration_seconds", Help: "HTTP request duration.", Buckets: prometheus.DefBuckets}, []string{"method", "route"})}
	registry.MustRegister(s.requests, s.duration)
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
	s.mux.Handle("GET /api/v1/releases/{id}", s.require("validation:read", http.HandlerFunc(s.getRelease)))
	s.mux.Handle("POST /api/v1/releases", s.require("release:create", http.HandlerFunc(s.createRelease)))
	s.mux.Handle("POST /api/v1/releases/{releaseId}/validate", s.require("validation:write", http.HandlerFunc(s.validateRelease)))
	s.mux.Handle("GET /api/v1/validations/{id}", s.require("validation:read", http.HandlerFunc(s.getValidation)))
	s.mux.Handle("POST /api/v1/validations/{id}/cancel", s.require("validation:write", http.HandlerFunc(s.cancelValidation)))
	s.mux.Handle("POST /api/v1/agents/register", http.HandlerFunc(s.registerAgent))
	s.mux.Handle("POST /api/v1/agents/{id}/heartbeat", http.HandlerFunc(s.agentHeartbeat))
	s.mux.Handle("GET /api/v1/agents", s.require("agent:read", http.HandlerFunc(s.listAgents)))
	s.mux.Handle("GET /api/v1/scenarios", s.require("scenario:read", http.HandlerFunc(s.listScenarios)))
	s.mux.Handle("POST /api/v1/scenarios", s.require("scenario:write", http.HandlerFunc(s.applyScenario)))
	s.mux.Handle("GET /api/v1/events", s.require("validation:read", http.HandlerFunc(s.events)))
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
	write(w, http.StatusOK, map[string]any{"accessToken": token, "tokenType": "Bearer", "expiresIn": 900})
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	items, err := s.store.ListServices(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	write(w, http.StatusOK, items)
}
func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetService(r.Context(), r.PathValue("name"))
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
	item, err := s.store.UpsertService(r.Context(), p.Subject, in)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, p, "service.apply", "service", item.ID, in)
	write(w, http.StatusOK, item)
}
func (s *Server) deleteService(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteService(r.Context(), r.PathValue("name")); database.IsNotFound(err) {
		fail(w, r, http.StatusNotFound, "not_found", "serviço não encontrado")
		return
	} else if err != nil {
		s.internal(w, r, err)
		return
	}
	p := getPrincipal(r.Context())
	s.audit(r, p, "service.delete", "service", r.PathValue("name"), nil)
	w.WriteHeader(http.StatusNoContent)
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
	item, err := s.store.CreateRelease(r.Context(), p.Subject, idem, in)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.audit(r, p, "release.create", "release", item.ID, map[string]any{"service": item.Service, "environment": item.Environment, "version": item.Version})
	writeStatus(w, http.StatusCreated, item)
}
func (s *Server) getRelease(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetRelease(r.Context(), r.PathValue("id"))
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
	if _, err := s.store.GetRelease(r.Context(), releaseID); err != nil {
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
	p := getPrincipal(r.Context())
	v, err := s.store.CreateValidation(r.Context(), p.Subject, releaseID, in.Mode)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	wfID := "validation-" + v.ID
	run, err := s.temporal.ExecuteWorkflow(r.Context(), client.StartWorkflowOptions{ID: wfID, TaskQueue: workflows.ReleaseValidationTaskQueue}, workflows.ReleaseValidationWorkflow, workflows.ValidationInput{ValidationID: v.ID, ReleaseID: releaseID, Mode: in.Mode})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	_ = s.store.SetWorkflowID(r.Context(), v.ID, wfID)
	s.audit(r, p, "validation.start", "validation", v.ID, map[string]string{"workflowId": run.GetID(), "runId": run.GetRunID()})
	writeStatus(w, http.StatusAccepted, v)
}
func (s *Server) getValidation(w http.ResponseWriter, r *http.Request) {
	item, err := s.store.GetValidation(r.Context(), r.PathValue("id"))
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
	var wf string
	_ = s.store.Pool.QueryRow(r.Context(), "SELECT coalesce(temporal_workflow_id,'') FROM validations WHERE id=$1", id).Scan(&wf)
	if wf != "" && s.temporal != nil {
		_ = s.temporal.CancelWorkflow(r.Context(), wf, "")
	}
	if err := s.store.CancelValidation(r.Context(), id); err != nil {
		fail(w, r, http.StatusConflict, "cannot_cancel", err.Error())
		return
	}
	p := getPrincipal(r.Context())
	s.audit(r, p, "validation.cancel", "validation", id, nil)
	write(w, http.StatusOK, map[string]string{"status": "CANCELLED"})
}

func (s *Server) registerAgent(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AgentBootstrap == "" || subtleToken(r.Header.Get("Authorization"), s.cfg.AgentBootstrap) == false {
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
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		s.internal(w, r, err)
		return
	}
	token := hex.EncodeToString(tokenBytes)
	hash := sha256.Sum256([]byte(token))
	org, err := localOrg(r.Context(), s.store)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	labels, _ := json.Marshal(in.Labels)
	var id string
	tx, err := s.store.Pool.Begin(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer tx.Rollback(r.Context())
	err = tx.QueryRow(r.Context(), `
		INSERT INTO agents(organization_id,name,region,cloud_provider,account,cluster,network,location,team,environment,labels,token_hash)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (organization_id,name) DO UPDATE SET
			region=EXCLUDED.region, cloud_provider=EXCLUDED.cloud_provider, account=EXCLUDED.account,
			cluster=EXCLUDED.cluster, network=EXCLUDED.network, location=EXCLUDED.location,
			team=EXCLUDED.team, environment=EXCLUDED.environment, labels=EXCLUDED.labels,
			token_hash=EXCLUDED.token_hash, revoked_at=NULL, updated_at=now(), version=agents.version+1
		RETURNING id::text`, org, in.Name, in.Region, in.CloudProvider, in.Account, in.Cluster, in.Network, in.Location, in.Team, in.Environment, labels, hash[:]).Scan(&id)
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
func (s *Server) agentHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	token := r.Header.Get("X-Agent-Token")
	sum := sha256.Sum256([]byte(token))
	var ok bool
	err := s.store.Pool.QueryRow(r.Context(), "SELECT token_hash=$2 AND revoked_at IS NULL FROM agents WHERE id=$1", id, sum[:]).Scan(&ok)
	if err != nil || !ok {
		fail(w, r, http.StatusUnauthorized, "invalid_agent_token", "token do agente inválido")
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
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.Pool.Query(r.Context(), `SELECT a.id::text,a.name,coalesce(a.region,''),coalesce(a.cloud_provider,''),coalesce(a.account,''),coalesce(a.cluster,''),coalesce(a.network,''),coalesce(a.location,''),coalesce(a.team,''),coalesce(a.environment,''),a.labels,(SELECT max(observed_at) FROM agent_heartbeats h WHERE h.agent_id=a.id) FROM agents a WHERE a.revoked_at IS NULL ORDER BY a.name`)
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
	if in.Type != "http" && in.Type != "browser" && in.Type != "k6" {
		fail(w, r, http.StatusUnprocessableEntity, "validation_error", "type deve ser http, browser ou k6")
		return
	}
	org, err := localOrg(r.Context(), s.store)
	if err != nil {
		s.internal(w, r, err)
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
	err = tx.QueryRow(r.Context(), `INSERT INTO synthetic_scenarios(organization_id,name,service_ref,environment,type,enabled,created_by) VALUES($1,$2,$3,$4,$5,true,$6) ON CONFLICT(organization_id,name) DO UPDATE SET service_ref=EXCLUDED.service_ref,environment=EXCLUDED.environment,type=EXCLUDED.type,current_version=synthetic_scenarios.current_version+1,updated_at=now() RETURNING id::text,current_version`, org, in.Name, in.ServiceRef, in.Environment, in.Type, p.Subject).Scan(&id, &version)
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
	rows, err := s.store.Pool.Query(r.Context(), `SELECT s.id::text,s.name,s.service_ref,s.environment,s.type,s.enabled,s.current_version,v.spec FROM synthetic_scenarios s JOIN synthetic_scenario_versions v ON v.scenario_id=s.id AND v.version=s.current_version ORDER BY s.name`)
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

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebhookSecret == "" {
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
	mac := hmac.New(sha256.New, []byte(s.cfg.WebhookSecret))
	_, _ = mac.Write([]byte(timestamp + "." + nonce + "."))
	_, _ = mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	provided, err := hex.DecodeString(signature)
	if err != nil || !hmac.Equal([]byte(expected), []byte(hex.EncodeToString(provided))) || nonce == "" || idem == "" {
		fail(w, r, http.StatusUnauthorized, "invalid_signature", "assinatura, nonce ou idempotency key inválidos")
		return
	}
	org, err := localOrg(r.Context(), s.store)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	integration := r.PathValue("")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	integration = parts[len(parts)-1]
	_, err = s.store.Pool.Exec(r.Context(), `INSERT INTO webhook_deliveries(organization_id,integration,idempotency_key,nonce,signature,status) VALUES($1,$2,$3,$4,$5,'ACCEPTED')`, org, integration, idem, nonce, signature)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || strings.Contains(err.Error(), "duplicate key") {
			fail(w, r, http.StatusConflict, "replay_detected", "webhook duplicado ou replay detectado")
			return
		}
		s.internal(w, r, err)
		return
	}
	writeStatus(w, http.StatusAccepted, map[string]string{"status": "ACCEPTED"})
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		fail(w, r, http.StatusNotImplemented, "stream_unsupported", "streaming indisponível")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	fmt.Fprintf(w, "event: connected\ndata: {\"time\":%q}\n\n", time.Now().UTC().Format(time.RFC3339))
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case t := <-ticker.C:
			fmt.Fprintf(w, "event: heartbeat\ndata: {\"time\":%q}\n\n", t.UTC().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}

func (s *Server) require(permission string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := s.auth.ParseAuthorization(r.Context(), r.Header.Get("Authorization"))
		if err != nil {
			fail(w, r, http.StatusUnauthorized, "unauthorized", err.Error())
			return
		}
		if !auth.Can(claims.Role, permission) {
			fail(w, r, http.StatusForbidden, "forbidden", "permissão insuficiente")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, principal{Subject: claims.Subject, Role: claims.Role})))
	})
}
func getPrincipal(ctx context.Context) principal {
	p, _ := ctx.Value(principalKey).(principal)
	return p
}
func (s *Server) audit(r *http.Request, p principal, action, typ, id string, payload any) {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if err := s.store.Audit(r.Context(), p.Subject, action, typ, id, requestID(r), ip, payload); err != nil {
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
func subtleToken(header, expected string) bool {
	parts := strings.SplitN(header, " ", 2)
	return len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && hmac.Equal([]byte(parts[1]), []byte(expected))
}
func localOrg(ctx context.Context, s *database.Store) (string, error) {
	var id string
	err := s.Pool.QueryRow(ctx, "SELECT id::text FROM organizations WHERE name='local'").Scan(&id)
	return id, err
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
