package main

import (
	"net/http"
	"os"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/server"
)

var version = "2.0.0-go-shell"

func main() {
	cfg := config.Load(version)
	if err := applog.Init(cfg.DataDir, 10); err != nil {
		_, _ = os.Stderr.WriteString("failed to initialize logger: " + err.Error() + "\n")
		os.Exit(1)
	}
	handler := applog.Middleware(server.New(cfg))

	applog.Info(nil, "startup", "api-monitor go shell listening", "address", cfg.ListenAddress())
	applog.Info(nil, "startup", "static files configured", "dist", cfg.DistDir, "public", cfg.PublicDir)
	if cfg.LegacyBaseURL != "" {
		applog.Info(nil, "startup", "legacy node adapter enabled", "url", cfg.LegacyBaseURL)
	}

	if err := http.ListenAndServe(cfg.ListenAddress(), handler); err != nil {
		applog.Error(nil, "startup", "http server stopped", "error", err.Error())
		os.Exit(1)
	}
}
