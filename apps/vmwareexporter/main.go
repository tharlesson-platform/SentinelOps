// SentinelOps VMware exporter collects inventory and capacity data through the
// vSphere SOAP API.  It deliberately exposes only loopback HTTP; Prometheus is
// the sole consumer of its metrics.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
)

type config struct {
	endpoint, username, passwordFile, thumbprint, address string
	interval                                              time.Duration
}

func load() (config, error) {
	c := config{endpoint: os.Getenv("VMWARE_ENDPOINT"), username: os.Getenv("VMWARE_USERNAME"), passwordFile: os.Getenv("VMWARE_PASSWORD_FILE"), thumbprint: os.Getenv("VMWARE_TLS_THUMBPRINT"), address: env("VMWARE_EXPORTER_ADDRESS", "127.0.0.1:9472"), interval: 60 * time.Second}
	if raw := os.Getenv("VMWARE_SCRAPE_INTERVAL"); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			return c, fmt.Errorf("VMWARE_SCRAPE_INTERVAL: %w", err)
		}
		c.interval = d
	}
	if c.endpoint == "" || c.username == "" || c.passwordFile == "" || c.thumbprint == "" {
		return c, errors.New("VMWARE_ENDPOINT, VMWARE_USERNAME, VMWARE_PASSWORD_FILE e VMWARE_TLS_THUMBPRINT são obrigatórios")
	}
	if c.interval < 30*time.Second || c.interval > 10*time.Minute {
		return c, errors.New("VMWARE_SCRAPE_INTERVAL deve estar entre 30s e 10m")
	}
	host, _, err := net.SplitHostPort(c.address)
	if err != nil {
		return c, fmt.Errorf("VMWARE_EXPORTER_ADDRESS: %w", err)
	}
	if host != "localhost" && !net.ParseIP(host).IsLoopback() && os.Getenv("VMWARE_ALLOW_CONTAINER_BIND") != "true" {
		return c, errors.New("VMWARE_EXPORTER_ADDRESS deve usar loopback")
	}
	parts := strings.Split(c.thumbprint, ":")
	if len(parts) != 32 {
		return c, errors.New("VMWARE_TLS_THUMBPRINT deve ser SHA-256 com 32 octetos separados por :")
	}
	for _, p := range parts {
		if len(p) != 2 {
			return c, errors.New("VMWARE_TLS_THUMBPRINT inválido")
		}
	}
	return c, nil
}

type exporter struct {
	cfg                                                 config
	logger                                              *slog.Logger
	mu                                                  sync.RWMutex
	lastOK                                              time.Time
	success                                             prometheus.Gauge
	lastSuccess                                         prometheus.Gauge
	hostCPU, hostCPUMax, hostMem, hostMemMax, hostPower *prometheus.GaugeVec
	vmCPU, vmMem, vmMemMax, vmPower, vmIP               *prometheus.GaugeVec
	dsFree, dsCapacity                                  *prometheus.GaugeVec
}

func newExporter(c config, logger *slog.Logger, r *prometheus.Registry) *exporter {
	e := &exporter{cfg: c, logger: logger,
		success:     prometheus.NewGauge(prometheus.GaugeOpts{Name: "sentinelops_vmware_scrape_success", Help: "1 when the most recent VMware inventory collection succeeded."}),
		lastSuccess: prometheus.NewGauge(prometheus.GaugeOpts{Name: "sentinelops_vmware_last_success_unixtime", Help: "Unix timestamp of the most recent successful VMware collection."}),
		hostCPU:     gauge("sentinelops_vmware_host_cpu_usage_mhz", "Host CPU usage in MHz.", "endpoint", "host"), hostCPUMax: gauge("sentinelops_vmware_host_cpu_capacity_mhz", "Host CPU capacity in MHz.", "endpoint", "host"), hostMem: gauge("sentinelops_vmware_host_memory_usage_bytes", "Host memory usage in bytes.", "endpoint", "host"), hostMemMax: gauge("sentinelops_vmware_host_memory_capacity_bytes", "Host memory capacity in bytes.", "endpoint", "host"), hostPower: gauge("sentinelops_vmware_host_power_state", "Host power state (1=on).", "endpoint", "host"),
		vmCPU: gauge("sentinelops_vmware_vm_cpu_usage_mhz", "VM CPU usage in MHz.", "endpoint", "vm_name", "vm_uuid"), vmMem: gauge("sentinelops_vmware_vm_memory_usage_bytes", "VM memory usage in bytes.", "endpoint", "vm_name", "vm_uuid"), vmMemMax: gauge("sentinelops_vmware_vm_memory_capacity_bytes", "VM configured memory in bytes.", "endpoint", "vm_name", "vm_uuid"), vmPower: gauge("sentinelops_vmware_vm_power_state", "VM power state (1=on).", "endpoint", "vm_name", "vm_uuid"), vmIP: gauge("sentinelops_vmware_vm_guest_ip_info", "VM guest IP inventory record.", "endpoint", "vm_name", "vm_uuid", "ip_address"),
		dsFree: gauge("sentinelops_vmware_datastore_free_bytes", "Datastore free capacity in bytes.", "endpoint", "datastore"), dsCapacity: gauge("sentinelops_vmware_datastore_capacity_bytes", "Datastore capacity in bytes.", "endpoint", "datastore")}
	r.MustRegister(e.success, e.lastSuccess, e.hostCPU, e.hostCPUMax, e.hostMem, e.hostMemMax, e.hostPower, e.vmCPU, e.vmMem, e.vmMemMax, e.vmPower, e.vmIP, e.dsFree, e.dsCapacity)
	return e
}
func gauge(name, help string, labels ...string) *prometheus.GaugeVec {
	return prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name, Help: help}, labels)
}

func (e *exporter) refresh(ctx context.Context) error {
	info, err := os.Stat(e.cfg.passwordFile)
	if err != nil {
		return fmt.Errorf("stat password file: %w", err)
	}
	if info.Mode().Perm()&0o007 != 0 {
		return errors.New("VMWARE_PASSWORD_FILE não pode conceder permissões a outros usuários")
	}
	data, err := os.ReadFile(e.cfg.passwordFile)
	if err != nil {
		return fmt.Errorf("read password file: %w", err)
	}
	password := strings.TrimSpace(string(data))
	if password == "" {
		return errors.New("VMWARE_PASSWORD_FILE está vazio")
	}
	u, err := soap.ParseURL(e.cfg.endpoint)
	if err != nil {
		return fmt.Errorf("parse endpoint: %w", err)
	}
	u.User = url.UserPassword(e.cfg.username, password)
	sc := soap.NewClient(u, false)
	sc.SetThumbprint(u.Host, e.cfg.thumbprint)
	vc, err := vim25.NewClient(ctx, sc)
	if err != nil {
		return fmt.Errorf("connect VMware: %w", err)
	}
	client := &govmomi.Client{Client: vc, SessionManager: session.NewManager(vc)}
	defer client.Logout(context.Background())
	if err = client.Login(ctx, u.User); err != nil {
		return fmt.Errorf("login VMware: %w", err)
	}
	finder := find.NewFinder(client.Client, true)
	dc, err := finder.DefaultDatacenter(ctx)
	if err != nil {
		return fmt.Errorf("find datacenter: %w", err)
	}
	finder.SetDatacenter(dc)
	hosts, err := finder.HostSystemList(ctx, "*")
	if err != nil {
		return fmt.Errorf("list hosts: %w", err)
	}
	vms, err := finder.VirtualMachineList(ctx, "*")
	if err != nil {
		return fmt.Errorf("list VMs: %w", err)
	}
	dss, err := finder.DatastoreList(ctx, "*")
	if err != nil {
		return fmt.Errorf("list datastores: %w", err)
	}
	e.reset()
	for _, h := range hosts {
		var x mo.HostSystem
		if err := h.Properties(ctx, h.Reference(), []string{"summary"}, &x); err != nil {
			return err
		}
		s := x.Summary
		name := s.Config.Name
		e.hostCPU.WithLabelValues(e.cfg.endpoint, name).Set(float64(s.QuickStats.OverallCpuUsage))
		e.hostCPUMax.WithLabelValues(e.cfg.endpoint, name).Set(float64(s.Hardware.CpuMhz) * float64(s.Hardware.NumCpuCores))
		e.hostMem.WithLabelValues(e.cfg.endpoint, name).Set(float64(s.QuickStats.OverallMemoryUsage) * 1024 * 1024)
		e.hostMemMax.WithLabelValues(e.cfg.endpoint, name).Set(float64(s.Hardware.MemorySize))
		if string(s.Runtime.PowerState) == "poweredOn" {
			e.hostPower.WithLabelValues(e.cfg.endpoint, name).Set(1)
		}
	}
	for _, vm := range vms {
		var x mo.VirtualMachine
		if err := vm.Properties(ctx, vm.Reference(), []string{"summary"}, &x); err != nil {
			return err
		}
		s := x.Summary
		name, id := s.Config.Name, s.Config.Uuid
		e.vmCPU.WithLabelValues(e.cfg.endpoint, name, id).Set(float64(s.QuickStats.OverallCpuUsage))
		e.vmMem.WithLabelValues(e.cfg.endpoint, name, id).Set(float64(s.QuickStats.GuestMemoryUsage) * 1024 * 1024)
		e.vmMemMax.WithLabelValues(e.cfg.endpoint, name, id).Set(float64(s.Config.MemorySizeMB) * 1024 * 1024)
		if string(s.Runtime.PowerState) == "poweredOn" {
			e.vmPower.WithLabelValues(e.cfg.endpoint, name, id).Set(1)
		}
		if s.Guest.IpAddress != "" {
			e.vmIP.WithLabelValues(e.cfg.endpoint, name, id, s.Guest.IpAddress).Set(1)
		}
	}
	for _, ds := range dss {
		var x mo.Datastore
		if err := ds.Properties(ctx, ds.Reference(), []string{"summary"}, &x); err != nil {
			return err
		}
		e.dsFree.WithLabelValues(e.cfg.endpoint, x.Summary.Name).Set(float64(x.Summary.FreeSpace))
		e.dsCapacity.WithLabelValues(e.cfg.endpoint, x.Summary.Name).Set(float64(x.Summary.Capacity))
	}
	now := time.Now().UTC()
	e.mu.Lock()
	e.lastOK = now
	e.mu.Unlock()
	e.success.Set(1)
	e.lastSuccess.Set(float64(now.Unix()))
	return nil
}
func (e *exporter) reset() {
	e.hostCPU.Reset()
	e.hostCPUMax.Reset()
	e.hostMem.Reset()
	e.hostMemMax.Reset()
	e.hostPower.Reset()
	e.vmCPU.Reset()
	e.vmMem.Reset()
	e.vmMemMax.Reset()
	e.vmPower.Reset()
	e.vmIP.Reset()
	e.dsFree.Reset()
	e.dsCapacity.Reset()
}
func (e *exporter) healthy() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return !e.lastOK.IsZero() && time.Since(e.lastOK) < 2*e.cfg.interval
}
func (e *exporter) run() {
	e.collect()
	t := time.NewTicker(e.cfg.interval)
	defer t.Stop()
	for range t.C {
		e.collect()
	}
}
func (e *exporter) collect() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := e.refresh(ctx); err != nil {
		e.success.Set(0)
		e.logger.Error("VMware collection failed", "error", err)
	} else {
		e.logger.Info("VMware collection completed")
	}
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := load()
	if err != nil {
		logger.Error("invalid VMware exporter configuration", "error", err)
		os.Exit(1)
	}
	r := prometheus.NewRegistry()
	e := newExporter(cfg, logger, r)
	go e.run()
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(r, promhttp.HandlerOpts{}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		if !e.healthy() {
			http.Error(w, "collection unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	s := &http.Server{Addr: cfg.address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("exporter stopped", "error", err)
			os.Exit(1)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.Shutdown(ctx)
}
func env(k, v string) string {
	if x := os.Getenv(k); x != "" {
		return x
	}
	return v
}
