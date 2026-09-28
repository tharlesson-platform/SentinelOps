package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sentinelops/sentinelops/dashboards"
	"github.com/sentinelops/sentinelops/internal/telemetryquery"
)

var metricName = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]{0,199}$`)
var labelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,127}$`)
var singleTenant = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var variableToken = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::regex)?\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
var catalogOnce sync.Once
var panelCatalog []dashboards.Dashboard
var panelCatalogErr error

func loadCatalog() ([]dashboards.Dashboard, error) {
	catalogOnce.Do(func() { panelCatalog, panelCatalogErr = dashboards.Load() })
	return panelCatalog, panelCatalogErr
}
func findDashboard(uid string) (dashboards.Dashboard, bool) {
	items, err := loadCatalog()
	if err != nil {
		return dashboards.Dashboard{}, false
	}
	for _, d := range items {
		if d.UID == uid {
			return d, true
		}
	}
	return dashboards.Dashboard{}, false
}

type cachedDiscovery struct {
	Until    time.Time
	Names    []string
	Metadata map[string][]telemetryquery.MetricMetadata
	Status   map[string]sourceStatus
}
type metricEntry struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Unit   string `json:"unit"`
	Help   string `json:"help"`
	Domain string `json:"domain"`
}

func metricDomain(name string) string {
	for prefix, domain := range map[string]string{"node_": "Servidores Linux", "container_": "Containers", "machine_": "Containers", "cadvisor_": "Containers", "http_": "Aplicações HTTP", "jvm_": "Aplicações JVM", "db_": "Aplicações e banco", "messaging_": "Mensageria", "traces_": "Traces e dependências", "sentinelops_vmware_": "VMware", "prometheus_": "Prometheus", "go_": "Runtime Go", "process_": "Processos", "otelcol_": "Coletores", "alloy_": "Coletores", "ALERTS": "Alertas"} {
		if strings.HasPrefix(name, prefix) {
			return domain
		}
	}
	return "Outras métricas"
}
func consistentMetadata(rows []telemetryquery.MetricMetadata) telemetryquery.MetricMetadata {
	if len(rows) != 1 {
		return telemetryquery.MetricMetadata{Type: "unknown", Help: "Metadados ambíguos entre alvos; tipo e unidade não inferidos."}
	}
	return rows[0]
}
func resolveMetadata(name string, metadata map[string][]telemetryquery.MetricMetadata) telemetryquery.MetricMetadata {
	if rows := metadata[name]; len(rows) > 0 {
		return consistentMetadata(rows)
	}
	for _, suffix := range []string{"_bucket", "_sum", "_count", "_total", "_created"} {
		base := strings.TrimSuffix(name, suffix)
		if base == name {
			continue
		}
		rows := metadata[base]
		if len(rows) == 0 {
			continue
		}
		m := consistentMetadata(rows)
		if (m.Type == "counter" && (suffix == "_total" || suffix == "_created")) || ((m.Type == "histogram" || m.Type == "summary") && suffix != "_total") {
			if suffix == "_created" {
				m.Type = "gauge"
				m.Unit = "seconds"
			} else if suffix == "_count" || suffix == "_bucket" {
				m.Type = "counter"
				m.Unit = ""
			} else if suffix == "_sum" {
				m.Type = "unknown"
			}
			return m
		}
	}
	return telemetryquery.MetricMetadata{Type: "unknown"}
}

// No queue: a busy process rejects immediately. All upstream calls and retries share one deadline.
func (s *Server) explorerRequest(w http.ResponseWriter, r *http.Request) (*http.Request, func(), bool) {
	if s.cfg.AuthMode != "local" && !s.cfg.TelemetryDiscoveryTenantAware {
		fail(w, r, 503, "discovery_isolation_unverified", "Descoberta desabilitada: o isolamento da fonte precisa ser validado para múltiplas organizações")
		return r, func() {}, false
	}
	if !singleTenant.MatchString(getPrincipal(r.Context()).OrganizationID) {
		fail(w, r, 403, "invalid_tenant", "organização de consulta inválida")
		return r, func() {}, false
	}
	if !s.allowObservabilityQuery(w, r) {
		return r, func() {}, false
	}
	s.explorerMu.Lock()
	if s.explorerSlots == nil {
		s.explorerSlots = make(chan struct{}, 2)
	}
	slots := s.explorerSlots
	s.explorerMu.Unlock()
	select {
	case slots <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "2")
		fail(w, r, 429, "explorer_busy", "Consultas ocupadas; tente novamente em instantes")
		return r, func() {}, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	return r.WithContext(ctx), func() { cancel(); <-slots }, true
}
func explorerWindow(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, time.Duration, bool) {
	start, end, _, ok := parseTelemetryWindow(w, r)
	if !ok {
		return start, end, 0, false
	}
	if end.Sub(start) > 24*time.Hour || end.After(time.Now().Add(time.Minute)) {
		fail(w, r, 400, "explorer_window", "Escolha uma janela de até 24h, sem término futuro")
		return start, end, 0, false
	}
	step := time.Duration(math.Ceil(end.Sub(start).Seconds()/359)) * time.Second
	if step < 15*time.Second {
		step = 15 * time.Second
	}
	return start, end, step, true
}
func readFilters(raw string) (map[string]string, error) {
	out := map[string]string{}
	if raw == "" {
		return out, nil
	}
	if len(raw) > 4096 {
		return nil, errors.New("filters too large")
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, err
	}
	if len(out) > 16 {
		return nil, errors.New("too many filters")
	}
	for k, v := range out {
		if !labelName.MatchString(k) || len(v) > 256 || strings.ContainsAny(v, "\x00\r\n") {
			return nil, errors.New("invalid filter")
		}
	}
	return out, nil
}
func exactMetricSelector(name string, filters map[string]string) (string, error) {
	if !metricName.MatchString(name) {
		return "", errors.New("invalid metric")
	}
	keys := []string{}
	for k := range filters {
		if !labelName.MatchString(k) || k == "__name__" {
			return "", errors.New("invalid label")
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Quote(filters[k]))
	}
	if len(parts) == 0 {
		return name, nil
	}
	return name + "{" + strings.Join(parts, ",") + "}", nil
}
func (s *Server) explorerCatalog(w http.ResponseWriter, r *http.Request) {
	r, done, ok := s.explorerRequest(w, r)
	if !ok {
		return
	}
	defer done()
	start, end, _, ok := explorerWindow(w, r)
	if !ok {
		return
	}
	org := getPrincipal(r.Context()).OrganizationID
	key := org + "\x00" + start.Format(time.RFC3339) + "\x00" + end.Format(time.RFC3339)
	s.explorerMu.Lock()
	cache, hit := s.explorerCache[key]
	s.explorerMu.Unlock()
	if !hit || time.Now().After(cache.Until) {
		cache = cachedDiscovery{Until: time.Now().Add(time.Minute), Status: map[string]sourceStatus{}}
		var err error
		cache.Names, err = s.telemetry.MetricNames(r.Context(), org, start, end)
		cache.Status["prometheus_names"] = discoveryStatus(err, len(cache.Names))
		if !s.allowObservabilityQuery(w, r) {
			return
		}
		cache.Metadata, err = s.telemetry.Metadata(r.Context(), org)
		cache.Status["prometheus_metadata"] = discoveryStatus(err, len(cache.Metadata))
		s.explorerMu.Lock()
		if len(s.explorerCache) > 32 {
			s.explorerCache = nil
		}
		if s.explorerCache == nil {
			s.explorerCache = map[string]cachedDiscovery{}
		}
		s.explorerCache[key] = cache
		s.explorerMu.Unlock()
	}
	items := []metricEntry{}
	names := append([]string(nil), cache.Names...)
	sort.Strings(names)
	truncated := len(names) > 10000
	if truncated {
		names = names[:10000]
	}
	for _, name := range names {
		m := resolveMetadata(name, cache.Metadata)
		items = append(items, metricEntry{Name: name, Type: m.Type, Unit: m.Unit, Help: m.Help, Domain: metricDomain(name)})
	}
	catalog, err := loadCatalog()
	if err != nil {
		fail(w, r, 500, "catalog_invalid", "Catálogo de painéis inválido")
		return
	}
	count := 0
	for _, d := range catalog {
		count += len(d.Panels)
	}
	write(w, 200, map[string]any{"metrics": items, "dashboards": catalog, "panelCount": count, "sources": cache.Status, "start": start, "end": end, "truncated": truncated, "catalogMode": "versioned", "catalogNote": "Definições versionadas junto ao provisionamento Grafana; presença no índice não comprova dados no período."})
}
func discoveryStatus(err error, count int) sourceStatus {
	state := sourceStatus{State: "available", FetchedAt: time.Now().UTC()}
	if err != nil {
		state.State = "unavailable"
		state.Message = "Fonte indisponível para consulta"
		var backend *telemetryquery.BackendError
		if errors.As(err, &backend) && (backend.Status == 401 || backend.Status == 403) {
			state.Message = "Fonte negou permissão para consulta"
		}
	} else if count == 0 {
		state.State = "no_data"
	}
	return state
}
func (s *Server) explorerDimensions(w http.ResponseWriter, r *http.Request) {
	r, done, ok := s.explorerRequest(w, r)
	if !ok {
		return
	}
	defer done()
	start, end, _, ok := explorerWindow(w, r)
	if !ok {
		return
	}
	filters, err := readFilters(r.URL.Query().Get("filters"))
	if err != nil {
		fail(w, r, 400, "invalid_filters", "Filtros inválidos")
		return
	}
	selector, err := exactMetricSelector(r.URL.Query().Get("metric"), filters)
	if err != nil {
		fail(w, r, 400, "invalid_metric", "Selecione uma métrica válida")
		return
	}
	series, truncated, err := s.telemetry.MetricSeries(r.Context(), getPrincipal(r.Context()).OrganizationID, selector, start, end)
	labels := map[string][]string{}
	sets := map[string]map[string]bool{}
	for _, row := range series {
		for k, v := range row {
			if k == "__name__" {
				continue
			}
			if sets[k] == nil {
				sets[k] = map[string]bool{}
			}
			sets[k][v] = true
		}
	}
	for k, values := range sets {
		for v := range values {
			labels[k] = append(labels[k], v)
		}
		sort.Strings(labels[k])
	}
	write(w, 200, map[string]any{"labels": labels, "seriesCount": len(series), "truncated": truncated, "selector": selector, "source": discoveryStatus(err, len(series)), "note": "Labels observados na janela; não equivalem à confirmação de amostras finitas."})
}
func (s *Server) explorerMetric(w http.ResponseWriter, r *http.Request) {
	r, done, ok := s.explorerRequest(w, r)
	if !ok {
		return
	}
	defer done()
	start, end, step, ok := explorerWindow(w, r)
	if !ok {
		return
	}
	filters, err := readFilters(r.URL.Query().Get("filters"))
	if err != nil {
		fail(w, r, 400, "invalid_filters", "Filtros inválidos")
		return
	}
	selector, err := exactMetricSelector(r.URL.Query().Get("metric"), filters)
	if err != nil {
		fail(w, r, 400, "invalid_metric", "Métrica inválida")
		return
	}
	operation := r.URL.Query().Get("operation")
	if operation == "rate" || operation == "increase" {
		if !s.allowObservabilityQuery(w, r) {
			return
		}
		metadata, e := s.telemetry.Metadata(r.Context(), getPrincipal(r.Context()).OrganizationID)
		if e != nil || resolveMetadata(r.URL.Query().Get("metric"), metadata).Type != "counter" {
			fail(w, r, 422, "counter_required", "A fonte deve identificar esta série como contador para aplicar taxa ou incremento")
			return
		}
	}
	query := selector
	switch r.URL.Query().Get("operation") {
	case "", "raw":
	case "rate":
		query = "rate(" + selector + "[5m])"
	case "increase":
		query = "increase(" + selector + "[5m])"
	default:
		fail(w, r, 400, "invalid_operation", "Operação inválida")
		return
	}
	result, err := s.telemetry.BoundedQuery(r.Context(), getPrincipal(r.Context()).OrganizationID, "prometheus", query, start, end, step, false)
	write(w, 200, map[string]any{"result": result, "query": query, "filters": filters, "start": start, "end": end, "stepSeconds": step.Seconds(), "source": queryResultStatus(result, err), "limits": explorerLimits()})
}
func explorerLimits() map[string]int {
	return map[string]int{"series": 40, "pointsPerSeries": 360, "timeoutSeconds": 10, "concurrentPerReplica": 2, "logEvents": 200}
}
func queryResultStatus(result telemetryquery.QueryResult, err error) sourceStatus {
	count := len(result.Logs)
	for _, s := range result.Series {
		count += len(s.Points)
	}
	status := discoveryStatus(err, count)
	if err == nil && (result.Truncated || len(result.Warnings) > 0 || result.Nonfinite > 0) {
		status.State = "partial"
		status.Message = "Resultado limitado, com avisos ou amostras não finitas; não representa todas as séries."
	}
	return status
}

// User values are literals. Regex operators belong only to reviewed templates and the explicit All token.
func renderTemplate(expression string, d dashboards.Dashboard, values map[string]string, span, step time.Duration) (string, map[string]string, error) {
	return renderResolvedTemplate(expression, d, values, nil, span, step)
}

func renderResolvedTemplate(expression string, d dashboards.Dashboard, values, resolved map[string]string, span, step time.Duration) (string, map[string]string, error) {
	definitions := map[string]dashboards.Variable{}
	for _, v := range d.Templating.List {
		definitions[v.Name] = v
	}
	for k := range values {
		if _, ok := definitions[k]; !ok {
			return "", nil, fmt.Errorf("unknown variable %s", k)
		}
	}
	applied := map[string]string{}
	var invalid error
	rendered := variableToken.ReplaceAllStringFunc(expression, func(token string) string {
		m := variableToken.FindStringSubmatch(token)
		key := m[1]
		if key == "" {
			key = m[2]
		}
		if key == "__range" {
			return strconv.FormatInt(int64(span.Seconds()), 10) + "s"
		}
		if key == "__interval" || key == "__rate_interval" {
			s := step
			if key == "__rate_interval" && s < time.Minute {
				s = time.Minute
			}
			return strconv.FormatInt(int64(s.Seconds()), 10) + "s"
		}
		def, ok := definitions[key]
		if !ok {
			invalid = errors.New("unknown template variable")
			return ""
		}
		value, provided := values[key]
		if !provided {
			value = def.Default()
		}
		if value == "__all__" {
			if !def.IncludeAll {
				invalid = errors.New("all is not enabled")
				return ""
			}
			all := def.AllValue
			if all == "" {
				var found bool
				all, found = resolved[key]
				if !found {
					invalid = fmt.Errorf("all values unresolved: %s", key)
					return ""
				}
			} else if all != ".*" && all != ".+" {
				invalid = errors.New("unsupported allValue")
				return ""
			}
			applied[key] = "Todos (valores da fonte)"
			return quotedContent(all)
		}
		if len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			invalid = errors.New("invalid variable value")
			return ""
		}
		applied[key] = value
		// Empty text search means no filter; all other supplied text is literal.
		if value == "" && def.Type == "textbox" {
			return ".*"
		}
		return quotedContent(regexp.QuoteMeta(value))
	})
	if invalid != nil {
		return "", nil, invalid
	}
	if strings.Contains(variableToken.ReplaceAllString(expression, ""), "$") {
		return "", nil, errors.New("unresolved macro")
	}
	return rendered, applied, nil
}
func (s *Server) explorerVariable(w http.ResponseWriter, r *http.Request) {
	r, done, ok := s.explorerRequest(w, r)
	if !ok {
		return
	}
	defer done()
	start, end, step, ok := explorerWindow(w, r)
	if !ok {
		return
	}
	d, ok := findDashboard(r.PathValue("dashboard"))
	if !ok {
		fail(w, r, 404, "dashboard_missing", "Visão não encontrada")
		return
	}
	var selected *dashboards.Variable
	for _, v := range d.Templating.List {
		if v.Name == r.PathValue("variable") {
			copy := v
			selected = &copy
			break
		}
	}
	if selected == nil {
		fail(w, r, 404, "variable_missing", "Filtro não encontrado")
		return
	}
	values, err := readFilters(r.URL.Query().Get("variables"))
	if err != nil {
		fail(w, r, 400, "invalid_variables", "Filtros inválidos")
		return
	}
	if selected.Type == "custom" {
		items := []string{}
		for _, item := range strings.Split(selected.Expression(), ",") {
			if _, v, ok := strings.Cut(item, ":"); ok {
				item = v
			}
			items = append(items, strings.TrimSpace(item))
		}
		write(w, 200, map[string]any{"values": items, "source": discoveryStatus(nil, len(items)), "truncated": false})
		return
	}
	items, truncated, err := s.variableValues(r.Context(), getPrincipal(r.Context()).OrganizationID, *selected, d, values, start, end, step)

	write(w, 200, map[string]any{"values": items, "truncated": truncated, "source": discoveryStatus(err, len(items))})
}
func (s *Server) explorerPanel(w http.ResponseWriter, r *http.Request) {
	r, done, ok := s.explorerRequest(w, r)
	if !ok {
		return
	}
	defer done()
	start, end, step, ok := explorerWindow(w, r)
	if !ok {
		return
	}
	d, ok := findDashboard(r.PathValue("dashboard"))
	if !ok {
		fail(w, r, 404, "dashboard_missing", "Visão não encontrada")
		return
	}
	id, err := strconv.Atoi(r.PathValue("panel"))
	if err != nil {
		fail(w, r, 400, "panel_invalid", "Painel inválido")
		return
	}
	var panel *dashboards.Panel
	for _, p := range d.Panels {
		if p.ID == id {
			copy := p
			panel = &copy
			break
		}
	}
	if panel == nil {
		fail(w, r, 404, "panel_missing", "Painel não encontrado")
		return
	}
	values, err := readFilters(r.URL.Query().Get("variables"))
	if err != nil {
		fail(w, r, 400, "invalid_variables", "Filtros inválidos")
		return
	}
	if len(panel.Targets) > 4 {
		fail(w, r, 400, "target_limit", "Painel excede limite de consultas")
		return
	}
	// Broad all-metric cardinality scans require a specific metric selection.
	for _, t := range panel.Targets {
		if strings.Contains(t.Expr, "$metric") && (values["metric"] == "" || values["metric"] == "__all__") {
			fail(w, r, 400, "metric_required", "Escolha uma métrica antes de consultar cardinalidade")
			return
		}
	}
	outputs := []map[string]any{}
	org := getPrincipal(r.Context()).OrganizationID
	resolved := map[string]string{}
	for _, def := range d.Templating.List {
		value, supplied := values[def.Name]
		if !supplied {
			value = def.Default()
		}
		if value != "__all__" || def.AllValue != "" {
			continue
		}
		used := false
		for _, t := range panel.Targets {
			for _, token := range variableToken.FindAllStringSubmatch(t.Expr+t.Query, -1) {
				if token[1] == def.Name || token[2] == def.Name {
					used = true
				}
			}
		}
		if !used {
			continue
		}
		if !s.allowObservabilityQuery(w, r) {
			return
		}
		items, truncated, e := s.variableValues(r.Context(), org, def, d, values, start, end, step)
		if e != nil || truncated {
			fail(w, r, 422, "variable_resolution", "Não foi possível resolver Todos integralmente; escolha um valor explícito")
			return
		}
		parts := make([]string, 0, len(items))
		for _, item := range items {
			parts = append(parts, regexp.QuoteMeta(item))
		}
		resolved[def.Name] = "a^"
		if len(parts) > 0 {
			resolved[def.Name] = "(" + strings.Join(parts, "|") + ")"
			if len(resolved[def.Name]) > 8192 {
				fail(w, r, 422, "variable_resolution", "Lista de valores extensa; escolha um ID explícito")
				return
			}
		}
	}
	for i, t := range panel.Targets {
		if i > 0 && !s.allowObservabilityQuery(w, r) {
			return
		}
		expression := t.Expr
		if expression == "" {
			expression = t.Query
		}
		query, applied, err := renderResolvedTemplate(expression, d, values, resolved, end.Sub(start), step)
		if err != nil {
			fail(w, r, 400, "template_invalid", "Filtro ou variável não suportada")
			return
		}
		source := t.Source.Type
		if source == "" {
			source = panel.Source.Type
		}
		out := map[string]any{"refId": t.RefID, "sourceName": source, "query": query, "appliedFilters": applied, "legendFormat": t.LegendFormat}
		switch source {
		case "prometheus", "loki":
			instant := t.Instant
			result, e := s.telemetry.BoundedQuery(r.Context(), org, source, query, start, end, step, instant)
			sort.Slice(result.Logs, func(i, j int) bool { return result.Logs[i].Timestamp.After(result.Logs[j].Timestamp) })
			out["result"], out["source"] = result, queryResultStatus(result, e)
		case "tempo":
			traces, e := s.telemetry.TempoSearch(r.Context(), org, query, start, end, 50)
			out["traces"] = traces
			out["source"] = discoveryStatus(e, len(traces))
			out["limit"] = 50
		default:
			out["source"] = sourceStatus{State: "unavailable", Message: "Tipo de fonte não suportado por esta visão", FetchedAt: time.Now().UTC()}
		}
		outputs = append(outputs, out)
	}
	write(w, 200, map[string]any{"panel": panel, "targets": outputs, "start": start, "end": end, "stepSeconds": step.Seconds(), "limits": explorerLimits(), "note": "Somente os filtros listados em cada consulta foram aplicados. Identidades de exporters não são convertidas em host_name."})
}

func quotedContent(value string) string { q := strconv.Quote(value); return q[1 : len(q)-1] }

func (s *Server) explorerPlatform(w http.ResponseWriter, r *http.Request) {
	// Targets describe a Prometheus process, not an organization. Never expose them in federated/OIDC mode.
	if s.cfg.AuthMode != "local" {
		fail(w, r, 403, "platform_scope", "Inventário de alvos e regras disponível somente no perfil local de uma organização")
		return
	}
	r, done, ok := s.explorerRequest(w, r)
	if !ok {
		return
	}
	defer done()
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	data, err := s.telemetry.PlatformInventory(r.Context(), getPrincipal(r.Context()).OrganizationID)
	write(w, 200, map[string]any{"inventory": data, "source": discoveryStatus(err, 1), "scope": "single-organization-local", "note": "Alvos de scrape deste Prometheus; métricas recebidas por remote_write podem vir de outros jobs. Um alvo up não comprova saúde da aplicação."})
}

// Apply the provisioned Grafana regex exactly; never guess a correlation from IDs.
func filterVariableValues(def dashboards.Variable, items []string) ([]string, error) {
	if def.Regex == "" {
		return items, nil
	}
	pattern := def.Regex
	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") {
		pattern = pattern[1 : len(pattern)-1]
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	out := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		match := re.FindStringSubmatch(item)
		if len(match) == 0 {
			continue
		}
		value := match[0]
		if len(match) > 1 {
			value = match[1]
		}
		if !seen[value] {
			out = append(out, value)
			seen[value] = true
		}
	}
	sort.Strings(out)
	return out, nil
}
func (s *Server) variableValues(ctx context.Context, org string, def dashboards.Variable, d dashboards.Dashboard, values map[string]string, start, end time.Time, step time.Duration) ([]string, bool, error) {
	expr, _, err := renderTemplate(def.Expression(), d, values, end.Sub(start), step)
	if err != nil {
		return nil, false, err
	}
	if !strings.HasPrefix(expr, "label_values(") || !strings.HasSuffix(expr, ")") {
		return nil, false, errors.New("unsupported variable query")
	}
	inner := expr[len("label_values(") : len(expr)-1]
	pos := strings.LastIndex(inner, ",")
	selector := ""
	label := strings.TrimSpace(inner)
	if pos >= 0 {
		selector = strings.TrimSpace(inner[:pos])
		label = strings.TrimSpace(inner[pos+1:])
	}
	if !labelName.MatchString(label) {
		return nil, false, errors.New("invalid label")
	}
	source := def.Source.Type
	if source == "" {
		source = "prometheus"
	}
	if source != "prometheus" && source != "loki" {
		return nil, false, errors.New("unsupported source")
	}
	items, truncated, err := s.telemetry.LabelValues(ctx, org, source, label, selector, start, end)
	if err != nil {
		return nil, truncated, err
	}
	items, err = filterVariableValues(def, items)
	return items, truncated, err
}
