package synthetics

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sentinelops/sentinelops/internal/database"
)

type Scheduler struct {
	Store        *database.Store
	Client       *http.Client
	Logger       *slog.Logger
	Interval     time.Duration
	AllowedHosts []string
}

func (s *Scheduler) Run(ctx context.Context) {
	interval := s.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		s.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Scheduler) runOnce(ctx context.Context) {
	rows, err := s.Store.Pool.Query(ctx, `SELECT s.id::text,v.spec FROM synthetic_scenarios s JOIN synthetic_scenario_versions v ON v.scenario_id=s.id AND v.version=s.current_version WHERE s.enabled=true AND s.type='http'`)
	if err != nil {
		s.Logger.Error("synthetic schedule query failed", "error", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		var spec struct {
			URL        string            `json:"url"`
			Method     string            `json:"method"`
			Timeout    string            `json:"timeout"`
			Headers    map[string]string `json:"headers"`
			Assertions []map[string]any  `json:"assertions"`
		}
		if json.Unmarshal(raw, &spec) != nil || spec.URL == "" {
			continue
		}
		s.execute(ctx, id, spec.URL, spec.Method, spec.Headers)
	}
}

func (s *Scheduler) execute(ctx context.Context, scenarioID, url, method string, headers map[string]string) {
	if method == "" {
		method = http.MethodGet
	}
	started := time.Now().UTC()
	runID := uuid.NewString()
	org := ""
	_ = s.Store.Pool.QueryRow(ctx, "SELECT id::text FROM organizations WHERE name='local'").Scan(&org)
	_, err := s.Store.Pool.Exec(ctx, `INSERT INTO test_runs(id,organization_id,scenario_id,status,started_at) VALUES($1,$2,$3,'RUNNING',$4)`, runID, org, scenarioID, started)
	if err != nil {
		return
	}
	target, parseErr := parseTarget(url)
	if parseErr != nil || !s.allowed(target.Hostname()) {
		s.recordBlocked(ctx, runID, "target rejeitado pela allowlist SSRF")
		return
	}
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err == nil {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		req.Header.Set("X-Synthetic-Test", "sentinelops-scheduler")
	}
	status := "FAIL"
	httpStatus := 0
	message := "request creation failed"
	if err == nil {
		resp, requestErr := s.Client.Do(req)
		if requestErr != nil {
			message = requestErr.Error()
		} else {
			httpStatus = resp.StatusCode
			_ = resp.Body.Close()
			if httpStatus >= 200 && httpStatus < 400 {
				status = "PASS"
				message = "HTTP status within 200-399"
			} else {
				message = "HTTP status outside 200-399"
			}
		}
	}
	finished := time.Now().UTC()
	result := map[string]any{"url": url, "httpStatus": httpStatus, "durationMs": finished.Sub(started).Milliseconds(), "message": message}
	data, _ := json.Marshal(result)
	_, _ = s.Store.Pool.Exec(ctx, `UPDATE test_runs SET status=$2,finished_at=$3,result=$4 WHERE id=$1`, runID, status, finished, data)
	observed, _ := json.Marshal(httpStatus)
	expected, _ := json.Marshal("200-399")
	_, _ = s.Store.Pool.Exec(ctx, `INSERT INTO test_assertions(test_run_id,name,status,observed,expected,message) VALUES($1,'http-status',$2,$3,$4,$5)`, runID, status, observed, expected, message)
	sum := sha256.Sum256(data)
	s.Logger.Info("synthetic run completed", "scenario_id", scenarioID, "run_id", runID, "status", status, "evidence_sha256", sum)
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
func (s *Scheduler) allowed(host string) bool {
	for _, entry := range s.AllowedHosts {
		entry = strings.TrimSpace(entry)
		if entry != "" && (host == entry || strings.HasSuffix(host, "."+entry)) {
			return true
		}
	}
	return false
}
func (s *Scheduler) recordBlocked(ctx context.Context, runID, message string) {
	data, _ := json.Marshal(map[string]any{"message": message})
	_, _ = s.Store.Pool.Exec(ctx, `UPDATE test_runs SET status='INCONCLUSIVE',finished_at=now(),result=$2 WHERE id=$1`, runID, data)
	s.Logger.Warn("synthetic target blocked", "run_id", runID, "reason", message)
}
