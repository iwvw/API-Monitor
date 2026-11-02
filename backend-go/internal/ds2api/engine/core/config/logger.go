package config

import (
	"log/slog"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

var Logger = newLogger()

func newLogger() *slog.Logger {
	// 复用主程序统一日志入口，保证全站日志格式一致（console + app.log 双写）。
	// 级别由 applog 统一控制（LevelInfo），不再单独读 LOG_LEVEL。
	return applog.Logger()
}

func RefreshLogger() {
	Logger = newLogger()
}
