package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
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
	credentialFile := env("SENTINEL_AGENT_CREDENTIAL_FILE", "/var/lib/sentinelops-agent/credentials.json")
	if agentID == "" || token == "" {
		var stored struct {
			ID    string `json:"id"`
			Token string `json:"token"`
		}
		if data, err := os.ReadFile(credentialFile); err == nil && json.Unmarshal(data, &stored) == nil {
			agentID, token = stored.ID, stored.Token
		}
	}
	client := apiclient.New(base, "", 15*time.Second)
	if err := configureMTLS(client, base); err != nil {
		logger.Error("mTLS configuration invalid", "error", err)
		os.Exit(1)
	}
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
		if err := persistCredentials(credentialFile, agentID, token); err != nil {
			logger.Error("registered credentials could not be persisted", "error", err)
			os.Exit(1)
		}
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

func configureMTLS(client *apiclient.Client, baseURL string) error {
	caFile := os.Getenv("SENTINEL_TLS_CA_FILE")
	certFile := os.Getenv("SENTINEL_TLS_CERT_FILE")
	keyFile := os.Getenv("SENTINEL_TLS_KEY_FILE")
	serverName := os.Getenv("SENTINEL_TLS_SERVER_NAME")
	configured := caFile != "" || certFile != "" || keyFile != "" || serverName != ""
	if !configured {
		if strings.HasPrefix(baseURL, "https://") {
			return fmt.Errorf("HTTPS requires SENTINEL_TLS_CA_FILE, SENTINEL_TLS_CERT_FILE, SENTINEL_TLS_KEY_FILE and SENTINEL_TLS_SERVER_NAME")
		}
		return nil
	}
	if caFile == "" || certFile == "" || keyFile == "" || serverName == "" || !strings.HasPrefix(baseURL, "https://") {
		return fmt.Errorf("mTLS requires HTTPS and all certificate settings")
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return fmt.Errorf("read CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return fmt.Errorf("CA file contains no valid certificate")
	}
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("load client certificate: %w", err)
	}
	client.HTTP = &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{certificate}, ServerName: serverName,
	}}}
	return nil
}

func persistCredentials(path, id, token string) error {
	data, err := json.Marshal(struct {
		ID    string `json:"id"`
		Token string `json:"token"`
	}{ID: id, Token: token})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}
func env(k, v string) string {
	if x := os.Getenv(k); x != "" {
		return x
	}
	return v
}
