package aiagent

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialRuntime 是 AgentRuntime 的可编程替身：OpenStream 返回拨号到 dialAddr 的
// 原始 TCP 连接，模拟数据通道直接连到目标主机端口（隧道测试用）。
type dialRuntime struct {
	online         bool
	supportsStream bool
	dialAddr       string
}

func (r *dialRuntime) AgentOnline(string) bool { return r.online }

func (r *dialRuntime) AgentSupportsAIAgentStream(string) bool { return r.supportsStream }

func (r *dialRuntime) AgentSupportsLifecycle(string) bool { return false }

func (r *dialRuntime) Probe(context.Context, string, string, int, []string) (ProbeResult, error) {
	return ProbeResult{}, nil
}

func (r *dialRuntime) StartProcess(context.Context, string, LifecycleStartPayload) (LifecycleResult, error) {
	return LifecycleResult{}, nil
}

func (r *dialRuntime) StopProcess(context.Context, string, string) (LifecycleResult, error) {
	return LifecycleResult{}, nil
}

func (r *dialRuntime) ProcessStatus(context.Context, string, string) (LifecycleResult, error) {
	return LifecycleResult{}, nil
}

func (r *dialRuntime) Diagnose(context.Context, string, string) (DiagnoseResult, error) {
	return DiagnoseResult{}, nil
}

func (r *dialRuntime) RoundTrip(context.Context, string, int, AgentHTTPRequest) (AgentHTTPResponse, error) {
	return AgentHTTPResponse{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(nil)}, nil
}

func (r *dialRuntime) OpenStream(_ context.Context, _ string, _ int) (io.ReadWriteCloser, error) {
	return net.Dial("tcp", r.dialAddr)
}

// TestGatewayWebSocketTunnel 验证完整的 WebSocket 隧道：
// 客户端升级请求经网关鉴权后，通过数据通道连到目标 WebSocket 服务器，
// 完成 101 握手并双向传输帧。
func TestGatewayWebSocketTunnel(t *testing.T) {
	// 1. 启动目标 WebSocket 回显服务器（模拟主机上的 opencode /pty/connect）。
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, append([]byte("echo:"), message...)); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()

	service := newTestService(t)
	runtime := &dialRuntime{
		online:         true,
		supportsStream: true,
		dialAddr:       strings.TrimPrefix(upstream.URL, "http://"),
	}
	service.SetAgentRuntime(runtime)

	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	user, err := service.createUser(ctx, db, "ws-user", "secret-pass-1", "")
	if err != nil {
		t.Fatalf("createUser: %v", err)
	}
	plain, _, err := service.issueToken(ctx, db, user.ID, "", "203.0.113.10")
	if err != nil {
		t.Fatalf("issueToken: %v", err)
	}
	instance, err := service.createInstance(ctx, db, instancePayload{
		ServerID: "server-ws",
		Provider: "opencode",
		Label:    "ws-host",
	})
	if err != nil {
		t.Fatalf("createInstance: %v", err)
	}
	if err := service.setGrantsForUser(ctx, db, user.ID, []string{instance.ID}); err != nil {
		t.Fatalf("setGrantsForUser: %v", err)
	}
	db.Close()

	// 2. 用 httptest.Server 挂载网关（真实 Hijack 需要真实 TCP 连接）。
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.ServeHTTP(w, r)
	}))
	defer gateway.Close()

	// 3. 客户端发起 WebSocket 升级请求（带 Bearer token）。
	wsURL := "ws" + strings.TrimPrefix(gateway.URL, "http") +
		"/api/aiagent/gw/" + instance.ID + "/pty/pty_test/connect?directory=/home"
	header := http.Header{"Authorization": []string{"Bearer " + plain}}
	client, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("websocket dial: %v (upgrade resp: %+v)", err, resp)
	}
	defer client.Close()

	// 4. 双向通信：客户端发送、接收回显。
	if err := client.WriteMessage(websocket.TextMessage, []byte("hello-ws")); err != nil {
		t.Fatalf("write: %v", err)
	}
	client.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, reply, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got, want := string(reply), "echo:hello-ws"; got != want {
		t.Fatalf("echo mismatch: got %q want %q", got, want)
	}
}

// TestBuildWebSocketUpgradeRequest 验证升级请求字节的构造：
// 保留握手必需头，剥离面板鉴权/逐跳头，Host 指向目标。
func TestBuildWebSocketUpgradeRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://panel.example.com/api/aiagent/gw/inst_1/pty/p1/connect?cursor=0&st=short-token", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Authorization", "Bearer panel-secret")
	req.Header.Set("Cookie", "session=abc")
	req.Header.Set("X-Forwarded-For", "203.0.113.1")

	raw, err := buildWebSocketUpgradeRequest(req, "/pty/p1/connect?cursor=0")
	if err != nil {
		t.Fatalf("buildWebSocketUpgradeRequest: %v", err)
	}

	blob := string(raw)
	if !strings.HasPrefix(blob, "GET /pty/p1/connect?cursor=0 HTTP/1.1\r\n") {
		t.Fatalf("request line mismatch:\n%s", blob)
	}

	// header 名大小写不敏感匹配（Go 会规范化 Sec-WebSocket-Key 等头名），
	// 值必须精确匹配（base64 大小写敏感）。
	headerLines := map[string]string{}
	for _, line := range strings.Split(blob, "\r\n") {
		name, value, ok := strings.Cut(line, ": ")
		if !ok {
			continue
		}
		headerLines[strings.ToLower(name)] = value
	}
	if got := headerLines["host"]; got != "127.0.0.1" {
		t.Fatalf("host must point to target, got %q:\n%s", got, blob)
	}
	wants := map[string]string{
		"connection":         "Upgrade",
		"upgrade":            "websocket",
		"sec-websocket-key":  "dGhlIHNhbXBsZSBub25jZQ==",
		"sec-websocket-version": "13",
	}
	for name, value := range wants {
		if got, ok := headerLines[name]; !ok || got != value {
			t.Fatalf("missing header %q=%q (got %q):\n%s", name, value, got, blob)
		}
	}
	for _, forbidden := range []string{"authorization", "cookie", "x-forwarded-for", "st="} {
		for name := range headerLines {
			if name == forbidden || strings.Contains(headerLines[name], "203.0.113.1") || strings.Contains(headerLines[name], "panel-secret") || strings.Contains(headerLines[name], "session=abc") {
				t.Fatalf("must not leak %q:\n%s", forbidden, blob)
			}
		}
	}
}
