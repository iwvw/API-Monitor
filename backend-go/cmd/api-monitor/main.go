package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/httpcompress"
	"github.com/iwvw/api-monitor/backend-go/internal/memguard"
	"github.com/iwvw/api-monitor/backend-go/internal/server"
)

var version = "2.0.0-go-shell"

func main() {
	config.LoadDotEnv()
	cfg := config.Load(version)
	if err := applog.Init(cfg.DataDir, 10); err != nil {
		_, _ = os.Stderr.WriteString("failed to initialize logger: " + err.Error() + "\n")
		os.Exit(1)
	}
	memguard.Start(context.Background())
	appServer, err := server.NewChecked(cfg)
	if err != nil {
		applog.Error(nil, "startup", "backend initialization failed", "error", err.Error())
		os.Exit(1)
	}
	handler := applog.Middleware(httpcompress.Middleware(appServer))

	applog.Info(nil, "startup", "api-monitor go shell listening", "address", cfg.ListenAddress())
	applog.Info(nil, "startup", "static files configured", "dist", cfg.DistDir, "public", cfg.PublicDir)
	if cfg.LegacyBaseURL != "" {
		applog.Info(nil, "startup", "legacy node adapter enabled", "url", cfg.LegacyBaseURL)
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddress(),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       120 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		applog.Error(nil, "startup", "http server stopped", "error", err.Error())
		os.Exit(1)
	}
}
