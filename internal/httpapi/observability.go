package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/sentinelops/sentinelops/internal/telemetryquery"
)

type sourceStatus struct {
	State     string    `json:"state"`
	Message   string    `json:"message,omitempty"`
	FetchedAt time.Time `json:"fetchedAt"`
}

type hostSummary struct {
	Name          string   `json:"name"`
	Instance      string   `json:"instance"`
	OS            string   `json:"os,omitempty"`
	Kernel        string   `json:"kernel,omitempty"`
	Architecture  string   `json:"architecture,omitempty"`
	Environment   string   `json:"environment,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	CPUPercent    *float64 `json:"cpuPercent"`
	MemoryPercent *float64 `json:"memoryPercent"`
	DiskPercent   *float64 `json:"diskPercent"`
	Up            *bool    `json:"up"`
	State         string   `json:"state"`
}

type containerSummary struct {
	Name          string    `json:"name"`
	ID            string    `json:"id,omitempty"`
	Image         string    `json:"image,omitempty"`
	Host          string    `json:"host"`
	Service       string    `json:"service,omitempty"`
	Environment   string    `json:"environment,omitempty"`
	LastSeen      time.Time `json:"lastSeen"`
	CPUPercent    *float64  `json:"cpuPercent"`
	MemoryBytes   *float64  `json:"memoryBytes"`
	MemoryLimit   *float64  `json:"memoryLimitBytes"`
	MemoryPercent *float64  `json:"memoryPercent"`
	Restarts24h   *float64  `json:"restarts24h"`
	State         string    `json:"state"`
}

type apmService struct {
	Name           string   `json:"name"`
	Environment    string   `json:"environment,omitempty"`
	Host           string   `json:"host,omitempty"`
	RequestsPerS   *float64 `json:"requestsPerSecond"`
	ErrorPercent   *float64 `json:"errorPercent"`
	P95Seconds     *float64 `json:"p95Seconds"`
	P99Seconds     *float64 `json:"p99Seconds"`
	TelemetryState string   `json:"telemetryState"`
}

type instantResult struct {
	samples []telemetryquery.VectorSample
	err     error
}

type rangeResult struct {
	series []telemetryquery.TimeSeries
	err    error
}

func (s *Server) observabilityOverview(w http.ResponseWriter, r *http.Request) {
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	queries := map[string]string{
		"hostsUp":        `count(up{job="linux-node"} == 1)`,
		"hostsDown":      `count(up{job="linux-node"} == 0)`,
		"containers":     `count(container_last_seen{name!=""})`,
		"recentRestarts": `sum(changes(container_start_time_seconds{name!=""}[1h]))`,
		"services":       `count(sum by (service_name) (rate(http_server_request_duration_seconds_count{service_name!=""}[5m])) or label_replace(sum by (service) (rate(demo_pipeline_request_duration_seconds_count{service!=""}[5m])), "service_name", "$1", "service", "(.+)"))`,
	}
	results := s.instantQueries(r.Context(), getPrincipal(r.Context()).OrganizationID, queries)
	values := map[string]*float64{}
	for name, result := range results {
		values[name] = firstValue(result.samples)
	}
	write(w, http.StatusOK, map[string]any{
		"values":  values,
		"sources": map[string]sourceStatus{"prometheus": statusFromInstantResults(results)},
	})
}

func (s *Server) listObservedHosts(w http.ResponseWriter, r *http.Request) {
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	queries := map[string]string{
		"identity": `node_uname_info`,
		"up":       `up{job="linux-node"}`,
		"cpu":      `100 - avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100`,
		"memory":   `100 * (1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)`,
		"disk":     `max by (instance) (100 * (1 - node_filesystem_avail_bytes{fstype!~"tmpfs|overlay|squashfs"} / node_filesystem_size_bytes{fstype!~"tmpfs|overlay|squashfs"}))`,
	}
	results := s.instantQueries(r.Context(), getPrincipal(r.Context()).OrganizationID, queries)
	hosts := buildHosts(results)
	environment := strings.TrimSpace(r.URL.Query().Get("environment"))
	if environment != "" {
		filtered := hosts[:0]
		for _, host := range hosts {
			if host.Environment == environment {
				filtered = append(filtered, host)
			}
		}
		hosts = filtered
	}
	write(w, http.StatusOK, map[string]any{"items": hosts, "sources": map[string]sourceStatus{"prometheus": statusFromInstantResults(results)}})
}

func (s *Server) observedHostDetails(w http.ResponseWriter, r *http.Request) {
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	host := strings.TrimSpace(r.PathValue("host"))
	if !validSelector(host) {
		fail(w, r, http.StatusBadRequest, "invalid_host", "host inválido")
		return
	}
	start, end, step, ok := parseTelemetryWindow(w, r)
	if !ok {
		return
	}
	selector := strconv.Quote(host)
	queries := map[string]string{
		"cpuByMode":  fmt.Sprintf(`sum by (mode) (rate(node_cpu_seconds_total{instance=%s}[5m])) * 100`, selector),
		"load1":      fmt.Sprintf(`node_load1{instance=%s}`, selector),
		"load5":      fmt.Sprintf(`node_load5{instance=%s}`, selector),
		"load15":     fmt.Sprintf(`node_load15{instance=%s}`, selector),
		"memoryUsed": fmt.Sprintf(`100 * (1 - node_memory_MemAvailable_bytes{instance=%s} / node_memory_MemTotal_bytes{instance=%s})`, selector, selector),
		"swapUsed":   fmt.Sprintf(`100 * (1 - node_memory_SwapFree_bytes{instance=%s} / clamp_min(node_memory_SwapTotal_bytes{instance=%s}, 1))`, selector, selector),
		"diskUsed":   fmt.Sprintf(`100 * (1 - node_filesystem_avail_bytes{instance=%s,fstype!~"tmpfs|overlay|squashfs"} / node_filesystem_size_bytes{instance=%s,fstype!~"tmpfs|overlay|squashfs"})`, selector, selector),
		"diskRead":   fmt.Sprintf(`rate(node_disk_read_bytes_total{instance=%s}[5m])`, selector),
		"diskWrite":  fmt.Sprintf(`rate(node_disk_written_bytes_total{instance=%s}[5m])`, selector),
		"networkRx":  fmt.Sprintf(`rate(node_network_receive_bytes_total{instance=%s,device!="lo"}[5m])`, selector),
		"networkTx":  fmt.Sprintf(`rate(node_network_transmit_bytes_total{instance=%s,device!="lo"}[5m])`, selector),
	}
	results := s.rangeQueries(r.Context(), getPrincipal(r.Context()).OrganizationID, queries, start, end, step)
	metrics := map[string][]telemetryquery.TimeSeries{}
	for name, result := range results {
		metrics[name] = result.series
	}
	write(w, http.StatusOK, map[string]any{
		"host": host, "start": start, "end": end, "stepSeconds": step.Seconds(), "metrics": metrics,
		"sources": map[string]sourceStatus{"prometheus": statusFromRangeResults(results)},
	})
}

func (s *Server) listObservedContainers(w http.ResponseWriter, r *http.Request) {
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	queries := map[string]string{
		"identity": `max by (instance,name,id,image,container_label_com_docker_compose_service,deployment_environment) (container_last_seen{name!=""})`,
		"cpu":      `sum by (instance,name) (rate(container_cpu_usage_seconds_total{name!=""}[5m])) * 100`,
		"memory":   `max by (instance,name) (container_memory_working_set_bytes{name!=""})`,
		"limit":    `max by (instance,name) (container_spec_memory_limit_bytes{name!=""} > 0)`,
		"restarts": `sum by (instance,name) (changes(container_start_time_seconds{name!=""}[24h]))`,
	}
	results := s.instantQueries(r.Context(), getPrincipal(r.Context()).OrganizationID, queries)
	containers := buildContainers(results)
	hostFilter := strings.TrimSpace(r.URL.Query().Get("host"))
	serviceFilter := strings.TrimSpace(r.URL.Query().Get("service"))
	filtered := containers[:0]
	for _, item := range containers {
		if hostFilter != "" && item.Host != hostFilter {
			continue
		}
		if serviceFilter != "" && item.Service != serviceFilter {
			continue
		}
		filtered = append(filtered, item)
	}
	write(w, http.StatusOK, map[string]any{"items": filtered, "sources": map[string]sourceStatus{"prometheus": statusFromInstantResults(results)}})
}

func (s *Server) observedContainerDetails(w http.ResponseWriter, r *http.Request) {
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	container := strings.TrimSpace(r.PathValue("container"))
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	if !validSelector(container) || (host != "" && !validSelector(host)) {
		fail(w, r, http.StatusBadRequest, "invalid_container", "container ou host inválido")
		return
	}
	start, end, step, ok := parseTelemetryWindow(w, r)
	if !ok {
		return
	}
	match := fmt.Sprintf(`name=%s`, strconv.Quote(container))
	if host != "" {
		match += fmt.Sprintf(`,instance=%s`, strconv.Quote(host))
	}
	queries := map[string]string{
		"cpu":         fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{%s}[5m])) * 100`, match),
		"throttling":  fmt.Sprintf(`sum(rate(container_cpu_cfs_throttled_seconds_total{%s}[5m])) * 100`, match),
		"memory":      fmt.Sprintf(`container_memory_working_set_bytes{%s}`, match),
		"memoryLimit": fmt.Sprintf(`container_spec_memory_limit_bytes{%s}`, match),
		"networkRx":   fmt.Sprintf(`sum(rate(container_network_receive_bytes_total{%s}[5m]))`, match),
		"networkTx":   fmt.Sprintf(`sum(rate(container_network_transmit_bytes_total{%s}[5m]))`, match),
		"filesystem":  fmt.Sprintf(`sum(rate(container_fs_reads_bytes_total{%s}[5m]) + rate(container_fs_writes_bytes_total{%s}[5m]))`, match, match),
	}
	results := s.rangeQueries(r.Context(), getPrincipal(r.Context()).OrganizationID, queries, start, end, step)
	metrics := map[string][]telemetryquery.TimeSeries{}
	for name, result := range results {
		metrics[name] = result.series
	}
	write(w, http.StatusOK, map[string]any{
		"container": container, "host": host, "start": start, "end": end, "stepSeconds": step.Seconds(), "metrics": metrics,
		"sources": map[string]sourceStatus{"prometheus": statusFromRangeResults(results)},
	})
}

func (s *Server) searchObservedLogs(w http.ResponseWriter, r *http.Request) {
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	start, end, _, ok := parseTelemetryWindow(w, r)
	if !ok {
		return
	}
	filters := []string{}
	for _, filter := range []struct{ query, label string }{{"host", "host_name"}, {"containerId", "container_id"}, {"service", "service_name"}} {
		value := strings.TrimSpace(r.URL.Query().Get(filter.query))
		if value != "" {
			if !validSelector(value) {
				fail(w, r, http.StatusBadRequest, "invalid_filter", filter.query+" inválido")
				return
			}
			filters = append(filters, filter.label+"="+strconv.Quote(value))
		}
	}
	query := `{service_name=~".+"}`
	if len(filters) > 0 {
		query = `{` + strings.Join(filters, ",") + `}`
	}
	severity := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("severity")))
	severityExpressions := map[string]string{"ERROR": `(?i)error|exception|fatal`, "WARN": `(?i)warn|warning`, "INFO": `(?i)info`, "DEBUG": `(?i)debug`}
	if expression, exists := severityExpressions[severity]; exists {
		query += ` |~ ` + strconv.Quote(expression)
	} else if severity != "" {
		fail(w, r, http.StatusBadRequest, "invalid_severity", "severity deve ser DEBUG, INFO, WARN ou ERROR")
		return
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))
	if search != "" {
		if len(search) > 256 || !utf8.ValidString(search) {
			fail(w, r, http.StatusBadRequest, "invalid_search", "busca deve ter até 256 caracteres UTF-8")
			return
		}
		query += ` |= ` + strconv.Quote(search)
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	entries, err := s.telemetry.LokiRange(r.Context(), getPrincipal(r.Context()).OrganizationID, query, start, end, limit, "backward")
	status := sourceStatus{State: "available", FetchedAt: time.Now().UTC()}
	if err != nil {
		status.State, status.Message = "unavailable", "Loki indisponível; métricas e demais páginas continuam utilizáveis"
		entries = []telemetryquery.LogEntry{}
	} else if len(entries) == 0 {
		status.State, status.Message = "no_data", "Nenhum log corresponde aos filtros e à janela"
	}
	write(w, http.StatusOK, map[string]any{"items": entries, "query": query, "start": start, "end": end, "sources": map[string]sourceStatus{"loki": status}})
}

func (s *Server) observedAPM(w http.ResponseWriter, r *http.Request) {
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	queries := map[string]string{
		"rps": `sum by (service_name,deployment_environment,host_name) (rate(http_server_request_duration_seconds_count{service_name!=""}[5m]))
or label_replace(sum by (service,deployment_environment) (rate(demo_pipeline_request_duration_seconds_count{service!=""}[5m])), "service_name", "$1", "service", "(.+)")`,
		"errors": `100 * sum by (service_name) (rate(http_server_request_duration_seconds_count{service_name!="",http_response_status_code=~"5.."}[5m])) / clamp_min(sum by (service_name) (rate(http_server_request_duration_seconds_count{service_name!=""}[5m])), 0.000001)
or label_replace(100 * sum by (service) (rate(demo_pipeline_requests_total{service!="",status=~"5.."}[5m])) / clamp_min(sum by (service) (rate(demo_pipeline_requests_total{service!=""}[5m])), 0.000001), "service_name", "$1", "service", "(.+)")`,
		"p95": `histogram_quantile(0.95, sum by (le,service_name) (rate(http_server_request_duration_seconds_bucket{service_name!=""}[5m])))
or label_replace(histogram_quantile(0.95, sum by (le,service) (rate(demo_pipeline_request_duration_seconds_bucket{service!=""}[5m]))), "service_name", "$1", "service", "(.+)")`,
		"p99": `histogram_quantile(0.99, sum by (le,service_name) (rate(http_server_request_duration_seconds_bucket{service_name!=""}[5m])))
or label_replace(histogram_quantile(0.99, sum by (le,service) (rate(demo_pipeline_request_duration_seconds_bucket{service!=""}[5m]))), "service_name", "$1", "service", "(.+)")`,
	}
	results := s.instantQueries(r.Context(), getPrincipal(r.Context()).OrganizationID, queries)
	items := buildAPMServices(results)
	write(w, http.StatusOK, map[string]any{"items": items, "sources": map[string]sourceStatus{"prometheus": statusFromInstantResults(results)}})
}

func (s *Server) searchObservedTraces(w http.ResponseWriter, r *http.Request) {
	if !s.allowObservabilityQuery(w, r) {
		return
	}
	start, end, _, ok := parseTelemetryWindow(w, r)
	if !ok {
		return
	}
	clauses := []string{}
	for _, filter := range []struct{ query, attribute string }{{"service", "resource.service.name"}, {"host", "resource.host.name"}, {"container", "resource.container.name"}} {
		value := strings.TrimSpace(r.URL.Query().Get(filter.query))
		if value == "" {
			continue
		}
		if !validSelector(value) {
			fail(w, r, http.StatusBadRequest, "invalid_filter", filter.query+" inválido")
			return
		}
		clauses = append(clauses, filter.attribute+" = "+strconv.Quote(value))
	}
	if strings.EqualFold(r.URL.Query().Get("error"), "true") {
		clauses = append(clauses, `status = error`)
	}
	traceQL := `{ true }`
	if len(clauses) > 0 {
		traceQL = `{ ` + strings.Join(clauses, ` && `) + ` }`
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	traces, err := s.telemetry.TempoSearch(r.Context(), getPrincipal(r.Context()).OrganizationID, traceQL, start, end, limit)
	status := sourceStatus{State: "available", FetchedAt: time.Now().UTC()}
	if err != nil {
		status.State, status.Message = "unavailable", "Tempo indisponível; logs e métricas continuam utilizáveis"
		traces = []telemetryquery.TraceSummary{}
	} else if len(traces) == 0 {
		status.State, status.Message = "no_data", "Nenhum trace corresponde aos filtros e à janela"
	}
	write(w, http.StatusOK, map[string]any{"items": traces, "query": traceQL, "start": start, "end": end, "sources": map[string]sourceStatus{"tempo": status}})
}

func (s *Server) allowObservabilityQuery(w http.ResponseWriter, r *http.Request) bool {
	return s.allowTenantScoped(w, r, getPrincipal(r.Context()).OrganizationID, "telemetry-query", s.cfg.CatalogQueriesPerMinute)
}

func (s *Server) instantQueries(ctx context.Context, organizationID string, queries map[string]string) map[string]instantResult {
	results := make(map[string]instantResult, len(queries))
	var mutex sync.Mutex
	var group sync.WaitGroup
	for name, query := range queries {
		name, query := name, query
		group.Add(1)
		go func() {
			defer group.Done()
			samples, err := s.telemetry.PrometheusInstant(ctx, organizationID, query, time.Now().UTC())
			mutex.Lock()
			results[name] = instantResult{samples: samples, err: err}
			mutex.Unlock()
		}()
	}
	group.Wait()
	return results
}

func (s *Server) rangeQueries(ctx context.Context, organizationID string, queries map[string]string, start, end time.Time, step time.Duration) map[string]rangeResult {
	results := make(map[string]rangeResult, len(queries))
	var mutex sync.Mutex
	var group sync.WaitGroup
	for name, query := range queries {
		name, query := name, query
		group.Add(1)
		go func() {
			defer group.Done()
			series, err := s.telemetry.PrometheusRange(ctx, organizationID, query, start, end, step)
			mutex.Lock()
			results[name] = rangeResult{series: series, err: err}
			mutex.Unlock()
		}()
	}
	group.Wait()
	return results
}

func buildHosts(results map[string]instantResult) []hostSummary {
	hosts := map[string]*hostSummary{}
	for _, sample := range results["identity"].samples {
		instance := label(sample.Labels, "instance", "host_name", "nodename")
		name := label(sample.Labels, "nodename", "host_name", "instance")
		if instance == "" || name == "" {
			continue
		}
		hosts[instance] = &hostSummary{Name: name, Instance: instance, OS: sample.Labels["sysname"], Kernel: sample.Labels["release"], Architecture: sample.Labels["machine"], Environment: label(sample.Labels, "deployment_environment", "environment"), Provider: label(sample.Labels, "cloud_provider", "provider"), State: "telemetry_unknown"}
	}
	for _, sample := range results["up"].samples {
		instance := label(sample.Labels, "instance", "host_name")
		if sample.Labels["job"] != "linux-node" || instance == "" {
			continue
		}
		item := ensureHost(hosts, instance, sample.Labels)
		up := sample.Value == 1
		item.Up = &up
		if up {
			item.State = "up"
		} else {
			item.State = "down"
		}
	}
	applyHostMetric(hosts, results["cpu"].samples, func(host *hostSummary, value float64) { host.CPUPercent = numberPointer(value) })
	applyHostMetric(hosts, results["memory"].samples, func(host *hostSummary, value float64) { host.MemoryPercent = numberPointer(value) })
	applyHostMetric(hosts, results["disk"].samples, func(host *hostSummary, value float64) { host.DiskPercent = numberPointer(value) })
	items := make([]hostSummary, 0, len(hosts))
	for _, host := range hosts {
		items = append(items, *host)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func ensureHost(hosts map[string]*hostSummary, instance string, labels map[string]string) *hostSummary {
	if existing := hosts[instance]; existing != nil {
		return existing
	}
	name := label(labels, "host_name", "nodename", "instance")
	host := &hostSummary{Name: name, Instance: instance, Environment: label(labels, "deployment_environment", "environment"), State: "telemetry_unknown"}
	hosts[instance] = host
	return host
}

func applyHostMetric(hosts map[string]*hostSummary, samples []telemetryquery.VectorSample, apply func(*hostSummary, float64)) {
	for _, sample := range samples {
		instance := label(sample.Labels, "instance", "host_name")
		if instance != "" {
			apply(ensureHost(hosts, instance, sample.Labels), sample.Value)
		}
	}
}

func buildContainers(results map[string]instantResult) []containerSummary {
	containers := map[string]*containerSummary{}
	for _, sample := range results["identity"].samples {
		name, host := sample.Labels["name"], label(sample.Labels, "instance", "host_name")
		if name == "" || host == "" {
			continue
		}
		key := host + "\x00" + name
		state := "running"
		if time.Since(sample.Timestamp) > 2*time.Minute || time.Since(time.Unix(int64(sample.Value), 0)) > 2*time.Minute {
			state = "stale"
		}
		containers[key] = &containerSummary{Name: name, ID: sample.Labels["id"], Image: sample.Labels["image"], Host: host, Service: sample.Labels["container_label_com_docker_compose_service"], Environment: sample.Labels["deployment_environment"], LastSeen: time.Unix(int64(sample.Value), 0).UTC(), State: state}
	}
	applyContainerMetric(containers, results["cpu"].samples, func(item *containerSummary, value float64) { item.CPUPercent = numberPointer(value) })
	applyContainerMetric(containers, results["memory"].samples, func(item *containerSummary, value float64) { item.MemoryBytes = numberPointer(value) })
	applyContainerMetric(containers, results["limit"].samples, func(item *containerSummary, value float64) { item.MemoryLimit = numberPointer(value) })
	applyContainerMetric(containers, results["restarts"].samples, func(item *containerSummary, value float64) { item.Restarts24h = numberPointer(value) })
	items := make([]containerSummary, 0, len(containers))
	for _, item := range containers {
		if item.MemoryBytes != nil && item.MemoryLimit != nil && *item.MemoryLimit > 0 {
			item.MemoryPercent = numberPointer(*item.MemoryBytes / *item.MemoryLimit * 100)
		}
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Host == items[j].Host {
			return items[i].Name < items[j].Name
		}
		return items[i].Host < items[j].Host
	})
	return items
}

func applyContainerMetric(containers map[string]*containerSummary, samples []telemetryquery.VectorSample, apply func(*containerSummary, float64)) {
	for _, sample := range samples {
		key := label(sample.Labels, "instance", "host_name") + "\x00" + sample.Labels["name"]
		if item := containers[key]; item != nil {
			apply(item, sample.Value)
		}
	}
}

func buildAPMServices(results map[string]instantResult) []apmService {
	items := map[string]*apmService{}
	for _, sample := range results["rps"].samples {
		name := label(sample.Labels, "service_name", "service")
		if name == "" {
			continue
		}
		items[name] = &apmService{Name: name, Environment: sample.Labels["deployment_environment"], Host: sample.Labels["host_name"], RequestsPerS: numberPointer(sample.Value), TelemetryState: "available"}
	}
	for _, metric := range []struct {
		name  string
		apply func(*apmService, float64)
	}{{"errors", func(item *apmService, value float64) { item.ErrorPercent = numberPointer(value) }}, {"p95", func(item *apmService, value float64) { item.P95Seconds = numberPointer(value) }}, {"p99", func(item *apmService, value float64) { item.P99Seconds = numberPointer(value) }}} {
		for _, sample := range results[metric.name].samples {
			name := label(sample.Labels, "service_name", "service")
			if item := items[name]; item != nil {
				metric.apply(item, sample.Value)
			}
		}
	}
	result := make([]apmService, 0, len(items))
	for _, item := range items {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

func parseTelemetryWindow(w http.ResponseWriter, r *http.Request) (time.Time, time.Time, time.Duration, bool) {
	end := time.Now().UTC()
	start := end.Add(-15 * time.Minute)
	var err error
	if raw := strings.TrimSpace(r.URL.Query().Get("end")); raw != "" {
		end, err = parseTelemetryTime(raw)
		if err != nil {
			fail(w, r, http.StatusBadRequest, "invalid_end", "end deve ser RFC3339 ou Unix seconds")
			return time.Time{}, time.Time{}, 0, false
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("start")); raw != "" {
		start, err = parseTelemetryTime(raw)
		if err != nil {
			fail(w, r, http.StatusBadRequest, "invalid_start", "start deve ser RFC3339 ou Unix seconds")
			return time.Time{}, time.Time{}, 0, false
		}
	}
	if !start.Before(end) || end.Sub(start) > 30*24*time.Hour {
		fail(w, r, http.StatusBadRequest, "invalid_window", "janela deve ser positiva e não pode exceder 30 dias")
		return time.Time{}, time.Time{}, 0, false
	}
	step := end.Sub(start) / 240
	if step < 15*time.Second {
		step = 15 * time.Second
	}
	if step > 5*time.Minute {
		step = 5 * time.Minute
	}
	return start, end, step, true
}

func parseTelemetryTime(raw string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), nil
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func validSelector(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

func label(labels map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := labels[key]; value != "" {
			return value
		}
	}
	return ""
}

func numberPointer(value float64) *float64 { return &value }

func firstValue(samples []telemetryquery.VectorSample) *float64 {
	if len(samples) == 0 {
		return nil
	}
	return numberPointer(samples[0].Value)
}

func statusFromInstantResults(results map[string]instantResult) sourceStatus {
	succeeded, failed, samples := 0, 0, 0
	for _, result := range results {
		if result.err != nil {
			failed++
			continue
		}
		succeeded++
		samples += len(result.samples)
	}
	return aggregateSourceStatus(succeeded, failed, samples)
}

func statusFromRangeResults(results map[string]rangeResult) sourceStatus {
	succeeded, failed, samples := 0, 0, 0
	for _, result := range results {
		if result.err != nil {
			failed++
			continue
		}
		succeeded++
		for _, series := range result.series {
			samples += len(series.Points)
		}
	}
	return aggregateSourceStatus(succeeded, failed, samples)
}

func aggregateSourceStatus(succeeded, failed, samples int) sourceStatus {
	status := sourceStatus{FetchedAt: time.Now().UTC()}
	switch {
	case succeeded == 0:
		status.State, status.Message = "unavailable", "backend de telemetria indisponível"
	case failed > 0:
		status.State, status.Message = "partial", "algumas consultas falharam; campos ausentes permanecem Sem dados"
	case samples == 0:
		status.State, status.Message = "no_data", "backend disponível, sem amostras para a janela"
	default:
		status.State = "available"
	}
	return status
}
