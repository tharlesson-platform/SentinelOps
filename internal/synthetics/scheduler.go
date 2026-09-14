package synthetics

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sentinelops/sentinelops/internal/database"
)

const maxResponseBodyBytes = 1 << 20

// AllowedTarget binds a hostname to the network prefixes it is permitted to
// resolve to. This makes private TQI targets possible without making RFC1918
// generally reachable from a synthetic worker.
type AllowedTarget struct {
	Host     string
	Prefixes []netip.Prefix
}

type HTTPScenarioSpec struct {
	URL        string            `json:"url"`
	Method     string            `json:"method"`
	Timeout    string            `json:"timeout"`
	Headers    map[string]string `json:"headers"`
	Assertions []HTTPAssertion   `json:"assertions"`
}

// HTTPAssertion deliberately has a closed Type enum; arbitrary scripts and
// expressions are not accepted from a scenario.
type HTTPAssertion struct {
	Type     string `json:"type"`
	Expected any    `json:"expected,omitempty"`
	Min      *int   `json:"min,omitempty"`
	Max      *int   `json:"max,omitempty"`
	Name     string `json:"name,omitempty"`
	Value    string `json:"value,omitempty"`
	Path     string `json:"path,omitempty"`
	MaxMS    *int64 `json:"maxMs,omitempty"`
}

type assertionResult struct {
	Name     string
	Status   string
	Observed any
	Expected any
	Message  string
}

type Scheduler struct {
	Store          *database.Store
	Client         *http.Client
	Logger         *slog.Logger
	Interval       time.Duration
	AllowedTargets []AllowedTarget
}

func ParseAllowedTargets(raw string) ([]AllowedTarget, error) {
	var targets []AllowedTarget
	for _, entry := range strings.Split(raw, ",") {
		host, prefixes, ok := strings.Cut(strings.TrimSpace(entry), "@")
		if !ok || host == "" || prefixes == "" || strings.Contains(host, "/") {
			return nil, fmt.Errorf("synthetic target %q must use hostname@cidr[,cidr]", entry)
		}
		target := AllowedTarget{Host: strings.ToLower(strings.TrimSuffix(host, "."))}
		for _, rawPrefix := range strings.Split(prefixes, "|") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(rawPrefix))
			if err != nil {
				return nil, fmt.Errorf("synthetic target %q has invalid CIDR: %w", entry, err)
			}
			target.Prefixes = append(target.Prefixes, prefix.Masked())
		}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one SYNTHETIC_ALLOWED_TARGETS entry is required")
	}
	return targets, nil
}

func ValidateHTTPScenarioSpec(spec map[string]any) error {
	raw, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("synthetic spec must be JSON: %w", err)
	}
	var parsed HTTPScenarioSpec
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return fmt.Errorf("invalid HTTP synthetic spec: %w", err)
	}
	if _, err := parseTarget(parsed.URL); err != nil {
		return err
	}
	if parsed.Method == "" {
		parsed.Method = http.MethodGet
	}
	if !validHTTPMethod(parsed.Method) {
		return fmt.Errorf("HTTP method %q is not supported", parsed.Method)
	}
	if parsed.Timeout == "" {
		return fmt.Errorf("timeout is required")
	}
	timeout, err := time.ParseDuration(parsed.Timeout)
	if err != nil || timeout <= 0 || timeout > 2*time.Minute {
		return fmt.Errorf("timeout must be between 1ns and 2m")
	}
	if len(parsed.Assertions) == 0 {
		return fmt.Errorf("at least one assertion is required")
	}
	for _, assertion := range parsed.Assertions {
		if err := validateAssertion(assertion); err != nil {
			return err
		}
	}
	return nil
}

func validHTTPMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func validateAssertion(assertion HTTPAssertion) error {
	switch assertion.Type {
	case "status_exact":
		if _, ok := integer(assertion.Expected); !ok {
			return fmt.Errorf("status_exact requires integer expected")
		}
	case "status_range":
		if assertion.Min == nil || assertion.Max == nil || *assertion.Min < 100 || *assertion.Max > 599 || *assertion.Min > *assertion.Max {
			return fmt.Errorf("status_range requires min/max between 100 and 599")
		}
	case "header_present":
		if assertion.Name == "" {
			return fmt.Errorf("header_present requires name")
		}
	case "header_equals":
		if assertion.Name == "" {
			return fmt.Errorf("header_equals requires name")
		}
	case "body_contains":
		if assertion.Value == "" {
			return fmt.Errorf("body_contains requires value")
		}
	case "json_path_equals":
		if !validJSONPath(assertion.Path) || assertion.Expected == nil {
			return fmt.Errorf("json_path_equals requires supported path and expected")
		}
	case "json_path_exists":
		if !validJSONPath(assertion.Path) {
			return fmt.Errorf("json_path_exists requires a supported path")
		}
	case "latency_max_ms":
		if assertion.MaxMS == nil || *assertion.MaxMS < 1 || *assertion.MaxMS > 120000 {
			return fmt.Errorf("latency_max_ms requires maxMs between 1 and 120000")
		}
	default:
		return fmt.Errorf("unsupported HTTP assertion type %q", assertion.Type)
	}
	return nil
}

func (s *Scheduler) Run(ctx context.Context) {
	interval := intervalFor(s.Interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.runOnce(ctx, time.Now().UTC().Truncate(interval))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context, slot time.Time) {
	rows, err := s.Store.Pool.Query(ctx, "SELECT id::text FROM organizations ORDER BY id")
	if err != nil {
		s.Logger.Error("synthetic organization query failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var organizationID string
		if err := rows.Scan(&organizationID); err != nil {
			s.Logger.Error("synthetic organization scan failed", "error", err)
			continue
		}
		s.runTenant(database.WithTenant(ctx, organizationID), organizationID, slot, intervalFor(s.Interval))
	}
	if err := rows.Err(); err != nil {
		s.Logger.Error("synthetic organization iteration failed", "error", err)
	}
}

func intervalFor(value time.Duration) time.Duration {
	if value <= 0 {
		return time.Minute
	}
	return value
}

func (s *Scheduler) runTenant(ctx context.Context, organizationID string, slot time.Time, interval time.Duration) {
	// A crash must never leave a prior execution looking healthy. It is marked
	// INCONCLUSIVE with evidence; the next slot can then be claimed normally.
	_, _ = s.Store.Pool.Exec(ctx, `UPDATE test_runs
SET status='INCONCLUSIVE', finished_at=now(), result=jsonb_build_object('message','worker interrupted before completion','recovered_at',now())
WHERE organization_id=$1 AND status='RUNNING' AND started_at < now() - $2::interval`, organizationID, (2 * interval).String())
	rows, err := s.Store.Pool.Query(ctx, `SELECT s.id::text,s.current_version,v.spec
FROM synthetic_scenarios s
JOIN synthetic_scenario_versions v ON v.scenario_id=s.id AND v.version=s.current_version
WHERE s.enabled=true AND s.type='http'`)
	if err != nil {
		s.Logger.Error("synthetic schedule query failed", "organization_id", organizationID, "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var version int
		var raw []byte
		if err := rows.Scan(&id, &version, &raw); err != nil {
			continue
		}
		var spec HTTPScenarioSpec
		if err := json.Unmarshal(raw, &spec); err != nil {
			s.Logger.Warn("invalid persisted synthetic spec", "scenario_id", id, "error", err)
			continue
		}
		s.execute(ctx, organizationID, id, version, slot, spec)
	}
}

func (s *Scheduler) execute(ctx context.Context, organizationID, scenarioID string, version int, slot time.Time, spec HTTPScenarioSpec) {
	runID := uuid.NewString()
	claimed, err := s.claimRun(ctx, organizationID, scenarioID, version, slot, runID)
	if err != nil || !claimed {
		if err != nil {
			s.Logger.Error("synthetic run claim failed", "scenario_id", scenarioID, "error", err)
		}
		return
	}
	if err := ValidateHTTPScenarioSpec(toMap(spec)); err != nil {
		s.recordBlocked(ctx, runID, "invalid scenario version: "+err.Error())
		return
	}
	target, err := parseTarget(spec.URL)
	if err != nil {
		s.recordBlocked(ctx, runID, "invalid target: "+err.Error())
		return
	}
	policy := newTargetPolicy(s.AllowedTargets)
	if err := policy.authorizeHost(target.Hostname()); err != nil {
		s.recordBlocked(ctx, runID, "target rejected by explicit site allowlist")
		return
	}
	timeout, _ := time.ParseDuration(spec.Timeout)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := s.safeClient(policy)
	started := time.Now().UTC()
	req, err := http.NewRequestWithContext(runCtx, spec.Method, spec.URL, nil)
	if err == nil {
		for key, value := range spec.Headers {
			req.Header.Set(key, value)
		}
		req.Header.Set("X-Synthetic-Test", "sentinelops-scheduler")
	}
	status, httpStatus, body, headers, message := "FAIL", 0, []byte(nil), http.Header{}, "request creation failed"
	if err == nil {
		resp, requestErr := client.Do(req)
		if requestErr != nil {
			message = "request failed: " + requestErr.Error()
		} else {
			httpStatus, headers = resp.StatusCode, resp.Header.Clone()
			body, _ = io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes+1))
			_ = resp.Body.Close()
			if len(body) > maxResponseBodyBytes {
				message = "response body exceeds 1 MiB assertion limit"
			} else {
				message = "assertions evaluated"
			}
		}
	}
	finished := time.Now().UTC()
	results := evaluateAssertions(spec.Assertions, httpStatus, headers, body, finished.Sub(started))
	if err == nil && message == "assertions evaluated" && allPass(results) {
		status = "PASS"
	}
	result := map[string]any{
		"url": spec.URL, "httpStatus": httpStatus, "durationMs": finished.Sub(started).Milliseconds(), "message": message,
		"scheduleSlot": slot.Format(time.RFC3339Nano), "connectedIPs": policy.connectedIPs(), "assertions": results,
	}
	data, _ := json.Marshal(result)
	_, _ = s.Store.Pool.Exec(ctx, `UPDATE test_runs SET status=$2,finished_at=$3,result=$4 WHERE id=$1`, runID, status, finished, data)
	for _, result := range results {
		observed, _ := json.Marshal(result.Observed)
		expected, _ := json.Marshal(result.Expected)
		_, _ = s.Store.Pool.Exec(ctx, `INSERT INTO test_assertions(test_run_id,name,status,observed,expected,message) VALUES($1,$2,$3,$4,$5,$6)`, runID, result.Name, result.Status, observed, expected, result.Message)
	}
	sum := sha256.Sum256(data)
	s.Logger.Info("synthetic run completed", "scenario_id", scenarioID, "run_id", runID, "status", status, "evidence_sha256", fmt.Sprintf("%x", sum))
}

func (s *Scheduler) claimRun(ctx context.Context, organizationID, scenarioID string, version int, slot time.Time, runID string) (bool, error) {
	tx, err := s.Store.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `INSERT INTO test_runs(id,organization_id,scenario_id,status,started_at)
VALUES($1,$2,$3,'RUNNING',now())`, runID, organizationID, scenarioID)
	if err != nil || command.RowsAffected() != 1 {
		return false, err
	}
	command, err = tx.Exec(ctx, `INSERT INTO synthetic_execution_claims(organization_id,scenario_id,scenario_version,schedule_slot,run_id)
VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, organizationID, scenarioID, version, slot, runID)
	if err != nil {
		return false, err
	}
	if command.RowsAffected() == 0 {
		return false, nil
	}
	return true, tx.Commit(ctx)
}

func (s *Scheduler) safeClient(policy *targetPolicy) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if s.Client != nil {
		if provided, ok := s.Client.Transport.(*http.Transport); ok && provided != nil {
			transport = provided.Clone()
		}
	}
	transport.DisableKeepAlives = true
	transport.DialContext = policy.dialContext
	client := &http.Client{Transport: transport}
	if s.Client != nil {
		client.Timeout = s.Client.Timeout
	}
	client.CheckRedirect = func(request *http.Request, _ []*http.Request) error {
		if _, err := parseTarget(request.URL.String()); err != nil {
			return err
		}
		return policy.authorizeHost(request.URL.Hostname())
	}
	return client
}

func parseTarget(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme")
	}
	if u.User != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("userinfo or empty host forbidden")
	}
	return u, nil
}

type targetPolicy struct {
	targets   []AllowedTarget
	mu        sync.Mutex
	connected []string
}

func newTargetPolicy(targets []AllowedTarget) *targetPolicy { return &targetPolicy{targets: targets} }

func (p *targetPolicy) authorizeHost(host string) error {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, target := range p.targets {
		if host == target.Host {
			return nil
		}
	}
	return fmt.Errorf("host %q is not an approved synthetic site", host)
}

func (p *targetPolicy) allowedAddress(host string, address netip.Addr) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, target := range p.targets {
		if host != target.Host {
			continue
		}
		for _, prefix := range target.Prefixes {
			if prefix.Contains(address) {
				return true
			}
		}
	}
	return false
}

func (p *targetPolicy) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if err := p.authorizeHost(host); err != nil {
		return nil, err
	}
	resolved, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve approved host %q: %w", host, err)
	}
	dialer := net.Dialer{}
	var rejected []string
	for _, candidate := range resolved {
		if !p.allowedAddress(host, candidate) {
			rejected = append(rejected, candidate.String())
			continue
		}
		connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.String(), port))
		if dialErr == nil {
			p.mu.Lock()
			p.connected = append(p.connected, candidate.String())
			p.mu.Unlock()
			return connection, nil
		}
	}
	return nil, fmt.Errorf("no permitted address for %q (rejected: %s)", host, strings.Join(rejected, ","))
}

func (p *targetPolicy) connectedIPs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.connected...)
}

func evaluateAssertions(assertions []HTTPAssertion, status int, headers http.Header, body []byte, duration time.Duration) []assertionResult {
	results := make([]assertionResult, 0, len(assertions))
	for _, assertion := range assertions {
		result := assertionResult{Name: assertion.Type, Status: "FAIL"}
		switch assertion.Type {
		case "status_exact":
			expected, _ := integer(assertion.Expected)
			result.Observed, result.Expected = status, expected
			result.Status = passIf(status == expected)
			result.Message = "HTTP status must match exactly"
		case "status_range":
			result.Observed, result.Expected = status, map[string]int{"min": *assertion.Min, "max": *assertion.Max}
			result.Status = passIf(status >= *assertion.Min && status <= *assertion.Max)
			result.Message = "HTTP status must be within configured range"
		case "header_present":
			value := headers.Get(assertion.Name)
			result.Observed, result.Expected = value, "present"
			result.Status = passIf(value != "")
			result.Message = "required response header"
		case "header_equals":
			value := headers.Get(assertion.Name)
			result.Observed, result.Expected = value, assertion.Value
			result.Status = passIf(value == assertion.Value)
			result.Message = "response header equality"
		case "body_contains":
			result.Observed, result.Expected = string(body), assertion.Value
			result.Status = passIf(strings.Contains(string(body), assertion.Value))
			result.Message = "response body content"
		case "json_path_exists", "json_path_equals":
			var payload any
			decodeErr := json.Unmarshal(body, &payload)
			value, found := jsonPath(payload, assertion.Path)
			result.Observed = value
			if assertion.Type == "json_path_exists" {
				result.Expected, result.Status = "present", passIf(decodeErr == nil && found)
			} else {
				result.Expected, result.Status = assertion.Expected, passIf(decodeErr == nil && found && reflect.DeepEqual(value, assertion.Expected))
			}
			result.Message = "JSON contract path " + assertion.Path
		case "latency_max_ms":
			observed := duration.Milliseconds()
			result.Observed, result.Expected = observed, *assertion.MaxMS
			result.Status = passIf(observed <= *assertion.MaxMS)
			result.Message = "response latency budget"
		}
		results = append(results, result)
	}
	return results
}

func passIf(value bool) string {
	if value {
		return "PASS"
	}
	return "FAIL"
}

func allPass(results []assertionResult) bool {
	for _, result := range results {
		if result.Status != "PASS" {
			return false
		}
	}
	return len(results) > 0
}

func validJSONPath(path string) bool {
	if path == "$" {
		return true
	}
	if !strings.HasPrefix(path, "$.") {
		return false
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		if part == "" {
			return false
		}
	}
	return true
}

func jsonPath(value any, path string) (any, bool) {
	if path == "$" {
		return value, true
	}
	current := value
	for _, part := range strings.Split(strings.TrimPrefix(path, "$."), ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func integer(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		return int(typed), typed == float64(int(typed))
	case json.Number:
		parsed, err := strconv.Atoi(string(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func toMap(spec HTTPScenarioSpec) map[string]any {
	raw, _ := json.Marshal(spec)
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value
}

func (s *Scheduler) recordBlocked(ctx context.Context, runID, message string) {
	data, _ := json.Marshal(map[string]any{"message": message})
	_, _ = s.Store.Pool.Exec(ctx, `UPDATE test_runs SET status='INCONCLUSIVE',finished_at=now(),result=$2 WHERE id=$1`, runID, data)
	s.Logger.Warn("synthetic target blocked", "run_id", runID, "reason", message)
}
