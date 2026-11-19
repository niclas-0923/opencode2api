// Command opencode2api starts the API gateway and its management interface.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
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
	apiServer := &http.Server{
		Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second,
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

// serveHTTP serves server until it is shut down. An unexpected stop is logged
// and cancels the root context, which begins the shutdown of every server.
func serveHTTP(cancel context.CancelFunc, logger *slog.Logger, server *http.Server, component string) {
	logger.Info("server listening", "component", component, "event", "server_started", "address", server.Addr, "version", buildinfo.Version)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server stopped unexpectedly", "component", component, "event", "server_failed", "address", server.Addr, "error", err)
		cancel()
	}
}
