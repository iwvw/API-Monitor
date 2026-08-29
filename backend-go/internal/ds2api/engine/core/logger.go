package config

import (
	"log/slog"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// Logger 复用宿主进程统一日志（applog）：控制台输出走站点统一样式，
// 同时写入轮转文件。引擎以内嵌方式跑在 host 进程内，无需独立的 stdout 日志器。
var Logger = newLogger()

func newLogger() *slog.Logger {
	return applog.Logger()
}

func RefreshLogger() {
	Logger = applog.Logger()
}
