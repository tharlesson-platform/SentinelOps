package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sentinelops/sentinelops/internal/apiclient"
	"github.com/sentinelops/sentinelops/internal/domain"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	base := env("SENTINEL_API_URL", "http://api:8080")
	agentID := os.Getenv("SENTINEL_AGENT_ID")
	token := os.Getenv("SENTINEL_AGENT_TOKEN")
	client := apiclient.New(base, "", 15*time.Second)
	if agentID == "" || token == "" {
		bootstrap := os.Getenv("AGENT_BOOTSTRAP_TOKEN")
		if bootstrap == "" {
			logger.Error("agent requires existing id/token or bootstrap token")
			os.Exit(1)
		}
		input := domain.Agent{Name: env("AGENT_NAME", "local-agent"), Region: env("AGENT_REGION", "local"), CloudProvider: env("AGENT_CLOUD_PROVIDER", "local"), Location: env("AGENT_LOCATION", "docker-desktop"), Team: env("AGENT_TEAM", "platform"), Environment: env("AGENT_ENVIRONMENT", "development"), Capabilities: strings.Split(env("AGENT_CAPABILITIES", "http,dns,tcp,tls"), ",")}
		var registered struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := client.Do(ctx, "POST", "/api/v1/agents/register", input, map[string]string{"Authorization": "Bearer " + bootstrap}, &registered)
		cancel()
		if err != nil {
			logger.Error("registration failed", "error", err)
			os.Exit(1)
		}
		agentID = registered.ID
		token = registered.Token
		logger.Info("agent registered", "agent_id", agentID)
	}
	client.Token = ""
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := client.Do(ctx, "POST", "/api/v1/agents/"+agentID+"/heartbeat", map[string]any{"version": "0.1.0", "jobsRunning": 0, "capabilities": strings.Split(env("AGENT_CAPABILITIES", "http,dns,tcp,tls"), ",")}, map[string]string{"X-Agent-Token": token}, nil)
		cancel()
		if err != nil {
			logger.Warn("heartbeat failed", "error", err)
		} else {
			logger.Info("heartbeat accepted", "agent_id", agentID)
		}
		select {
		case <-ticker.C:
			continue
		case <-stop:
			return
		}
	}
}
func env(k, v string) string {
	if x := os.Getenv(k); x != "" {
		return x
	}
	return v
}
