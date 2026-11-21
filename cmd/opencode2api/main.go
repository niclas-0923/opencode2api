// Command opencode2api starts the API gateway and its management interface.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	adminui "opencode2api/internal/admin"
	"opencode2api/internal/buildinfo"
	"opencode2api/internal/config"
	"opencode2api/internal/gateway"
	"opencode2api/internal/telemetry"
)

// version remains the linker injection point used by release builds.
var version = "dev"

// main loads the configuration, starts the gateway runtime and the API and
// WebUI servers, and shuts them down on SIGINT or SIGTERM.
func main() {
	buildinfo.Version = version
	configPath := flag.String("config", "config.json", "path to config.json")
	listen := flag.String("listen", "", "override the configured API listen address")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		// No validated configuration exists to seed the redactor yet. Parse
		// errors can quote credential-bearing input, so keep this log generic.
		slog.Error("configuration could not be loaded; check JSON, required fields, and file permissions")
		os.Exit(1)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	level := new(slog.LevelVar)
	telemetry.SetLogLevel(level, cfg.Logging.Level)
	hub := telemetry.LogHub(cfg.Logging.RingSize)
	redactor := config.SecretRedactor()
	redactor.Replace(cfg)
	logger := telemetry.StructuredLogger(level, hub, redactor)
	secureConfigPermissions(*configPath, logger)
	warnPublicListener(cfg, logger)
	monitor := telemetry.Monitor()
	manager, err := gateway.RuntimeManager(ctx, *configPath, cfg, logger, monitor, hub, redactor, level)
	if err != nil {
		logger.Error("failed to initialize runtime", "component", "runtime", "event", "runtime_initialization_failed", "error", err)
		os.Exit(1)
	}
	defer manager.Shutdown()

	// One listener serves everything: the gateway's API under /v1 and
	// /healthz, the management API under /api, and the WebUI under /web. When
	// the WebUI is disabled those admin paths are left to the gateway's 404.
	handler := manager.Handler()
	if cfg.WebUI.Enabled {
		admin := adminui.New(manager, monitor, hub, logger)
		web := admin.Handler()
		root := http.NewServeMux()
		root.Handle("/", handler)
		root.Handle("/api/", web)
		root.Handle("/web/", http.StripPrefix("/web", web))
		root.Handle("/web", http.RedirectHandler("/web/", http.StatusMovedPermanently))
		handler = root
	}
	// SSE responses stay open for the length of a stream, so there is
	// deliberately no WriteTimeout: it would cut active streams. Per-attempt
	// gateway timeouts bound them instead. Cap request headers so an
	// unauthenticated client cannot grow them without limit.
	apiServer := &http.Server{
		Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	servers := []*http.Server{apiServer}
	go serveHTTP(cancel, logger, apiServer, "api")

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	for _, server := range servers {
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "component", "server", "event", "shutdown_failed", "address", server.Addr, "error", err)
		}
	}
}

// secureConfigPermissions narrows a loose config file to owner-only and warns
// when the platform cannot do so. The file holds server keys, proxy passwords
// and OAuth state; a read-only mount or Windows ACL cannot be tightened here,
// so surface the condition at startup.
func secureConfigPermissions(path string, logger *slog.Logger) {
	before, tightened, err := config.TightenFilePermissions(path)
	switch {
	case err != nil:
		logger.Warn("could not inspect or tighten configuration permissions", "component", "config", "event", "config_permissions_failed", "path", path, "error", err)
	case tightened:
		logger.Warn("configuration file permissions were too broad; narrowed to owner-only", "component", "config", "event", "config_permissions_tightened", "path", path, "previous_mode", before.String())
	case runtime.GOOS == "windows":
		// Windows mode bits are synthetic and do not describe the ACL, so do
		// not claim the file is readable by others based on them.
		logger.Info("configuration ACL was not inspected; restrict the file to the service account", "component", "config", "event", "config_permissions_unverified", "path", path)
	}
}

// warnPublicListener surfaces a listener that is reachable beyond loopback.
// The program does not terminate TLS itself, so a public address needs a TLS
// reverse proxy and access controls in front of it.
func warnPublicListener(cfg config.Config, logger *slog.Logger) {
	host, _, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return
	}
	logger.Warn("API and management interface listen beyond loopback; terminate TLS in front of this port", "component", "server", "event", "public_listener", "address", cfg.Listen)
}

// serveHTTP serves server until it is shut down. An unexpected stop is logged
// and cancels the root context, which begins the shutdown of every server.
func serveHTTP(cancel context.CancelFunc, logger *slog.Logger, server *http.Server, component string) {
	logger.Info("server listening", "component", component, "event", "server_started", "address", server.Addr, "version", buildinfo.Version)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped unexpectedly", "component", component, "event", "server_failed", "address", server.Addr, "error", err)
		cancel()
	}
}
