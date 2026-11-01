package serveragent

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// originTestServer 构造只带配置的 EngineIOServer：Origin 校验只读 cfg，
// 不需要打开数据库，避免 New() 起后台任务导致 t.TempDir 清理时目录非空。
func originTestServer(allowed ...string) *EngineIOServer {
	server := NewEngineIOServer(nil)
	server.service = &Service{cfg: config.Config{CORSAllowedOrigins: allowed}}
	return server
}

/**
 * engine.io 的 WebSocket 升级必须接受 CORS 白名单来源。
 *
 * 回归背景（2026-11-01）：engineio.go 原先直接用包级 sameWebSocketOrigin，
 * 只做严格同源比较。OCC 部署在 https://occ.dmuk.org，面板在
 * https://dsukhub.com —— 跨源时 Origin 的 host 与请求 Host 永远不同，
 * 严格判断一律拒绝，浏览器侧表现为 WebSocket 立即 CLOSE 1006。
 * 当时 CORS_ALLOWED_ORIGINS 里已经列了 occ.dmuk.org，却仍连不上。
 */
func TestEngineIOUpgraderAcceptsWhitelistedOrigin(t *testing.T) {
	server := originTestServer("https://occ.dmuk.org")

	req := httptest.NewRequest(http.MethodGet, "https://dsukhub.com/socket.io/?EIO=4&transport=websocket", nil)
	req.Host = "dsukhub.com"
	req.Header.Set("Origin", "https://occ.dmuk.org")

	if !server.engineIOUpgrader().CheckOrigin(req) {
		t.Fatal("白名单来源应被接受，否则外部前端无法建立实时通道")
	}
}

// 同源请求继续放行（面板自己的页面）。
func TestEngineIOUpgraderAcceptsSameOrigin(t *testing.T) {
	server := originTestServer()

	req := httptest.NewRequest(http.MethodGet, "https://dsukhub.com/socket.io/", nil)
	req.Host = "dsukhub.com"
	req.Header.Set("Origin", "https://dsukhub.com")

	if !server.engineIOUpgrader().CheckOrigin(req) {
		t.Fatal("同源请求应被接受")
	}
}

// 未列白名单的第三方来源必须拒绝 —— 不能为了跨域而放开全部。
func TestEngineIOUpgraderRejectsUnknownOrigin(t *testing.T) {
	server := originTestServer("https://occ.dmuk.org")

	req := httptest.NewRequest(http.MethodGet, "https://dsukhub.com/socket.io/", nil)
	req.Host = "dsukhub.com"
	req.Header.Set("Origin", "https://evil.example.com")

	if server.engineIOUpgrader().CheckOrigin(req) {
		t.Fatal("未列入白名单的来源不得放行")
	}
}

// 无 Origin 头（非浏览器客户端，如 Agent 自身的 Go 客户端）应放行。
func TestEngineIOUpgraderAllowsMissingOrigin(t *testing.T) {
	server := originTestServer()

	req := httptest.NewRequest(http.MethodGet, "https://dsukhub.com/socket.io/", nil)
	req.Host = "dsukhub.com"

	if !server.engineIOUpgrader().CheckOrigin(req) {
		t.Fatal("无 Origin 的客户端应被接受")
	}
}
