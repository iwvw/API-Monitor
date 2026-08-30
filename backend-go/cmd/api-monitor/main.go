package main

import (
	"context"
	"net/http"
	_ "net/http/pprof"
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

	// 诊断端点（默认关闭）：仅当显式设置 API_MONITOR_PPROF=1 时监听本机
	// 127.0.0.1:6060 的 /debug/pprof/*（heap/goroutine 剖析用）。
	// 生产环境不设置即不暴露，无安全面。
	if os.Getenv("API_MONITOR_PPROF") == "1" {
		go func() {
			pprofServer := &http.Server{
				Addr:              "127.0.0.1:6060",
				ReadHeaderTimeout: 5 * time.Second,
			}
			applog.Info(nil, "startup", "pprof diagnostics listening", "address", pprofServer.Addr)
			if err := pprofServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				applog.Error(nil, "startup", "pprof server stopped", "error", err.Error())
			}
		}()
	}

	appServer, err := server.NewChecked(cfg)
	if err != nil {
		applog.Error(nil, "startup", "backend initialization failed", "error", err.Error())
		os.Exit(1)
	}
	// SSEWriteDeadline 为 SSE 流式响应（如 DS2API 引擎的 DeepSeek 思考/生成流）
	// 做写超时续期：Go http.Server 的 WriteTimeout 是总时长硬限，引擎只 Flush
	// 不续期，60s 一到长流就会被掐断。此中间件让持续有输出的 SSE 不受该限制。
	handler := applog.Middleware(httpcompress.Middleware(httpcompress.SSEWriteDeadline(appServer)))

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
		// SSE 流由 SSEWriteDeadline 中间件按写入续期，不受此总时长限制；
		// 600s 是给未走续期路径的响应留的兜底（原 60s 会掐断思考超一分钟的长流）。
		WriteTimeout:   600 * time.Second,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	if err := httpServer.ListenAndServe(); err != nil {
		applog.Error(nil, "startup", "http server stopped", "error", err.Error())
		os.Exit(1)
	}
}
