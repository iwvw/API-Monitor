package onepanel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestConfigLifecycle(t *testing.T) {
	service := newTestService(t)

	res := perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1","baseUrl":"https://127.0.0.1:8888"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create config status = %d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/onepanel/config", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list configs status = %d body=%s", res.Code, res.Body.String())
	}
	var listed struct {
		Success bool `json:"success"`
		Data    []struct {
			ServerID string `json:"serverId"`
			HasKey   bool   `json:"hasKey"`
			BaseURL  string `json:"baseUrl"`
		} `json:"data"`
	}
	mustDecode(t, res, &listed)
	if !listed.Success || len(listed.Data) != 1 {
		t.Fatalf("unexpected list payload: %#v", listed)
	}
	if listed.Data[0].ServerID != "srv-arm" || !listed.Data[0].HasKey || listed.Data[0].BaseURL != "https://127.0.0.1:8888" {
		t.Fatalf("unexpected config entry: %#v", listed.Data[0])
	}

	res = perform(service, http.MethodPut, "/api/onepanel/config/srv-arm", `{"apiKey":"secret-key-2","baseUrl":"https://127.0.0.1:9999"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update config status = %d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodDelete, "/api/onepanel/config/srv-arm", "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete config status = %d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/onepanel/config", "")
	mustDecode(t, res, &listed)
	if len(listed.Data) != 0 {
		t.Fatalf("expected empty config list, got %#v", listed.Data)
	}

	res = perform(service, http.MethodGet, "/api/onepanel/srv-arm/overview", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing config should return 404, got %d", res.Code)
	}
}

func TestProxyWithoutRunnerRejects(t *testing.T) {
	service := newTestService(t)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)
	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/health", "")
	if res.Code != http.StatusServiceUnavailable && res.Code != http.StatusBadGateway {
		t.Fatalf("no runner should reject proxy call, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestUpdateConfigPartialPreservesFields(t *testing.T) {
	service := newTestService(t)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1","baseUrl":"http://127.0.0.1:8888"}`)

	// 只改 apiKey，baseUrl 应保持不变。
	res := perform(service, http.MethodPut, "/api/onepanel/config/srv-arm", `{"apiKey":"secret-key-2"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("partial update status = %d body=%s", res.Code, res.Body.String())
	}

	var listed struct {
		Success bool `json:"success"`
		Data    []struct {
			ServerID string `json:"serverId"`
			HasKey   bool   `json:"hasKey"`
			BaseURL  string `json:"baseUrl"`
		} `json:"data"`
	}
	res = perform(service, http.MethodGet, "/api/onepanel/config", "")
	mustDecode(t, res, &listed)
	if len(listed.Data) != 1 || listed.Data[0].BaseURL != "http://127.0.0.1:8888" {
		t.Fatalf("partial update wiped baseUrl: %#v", listed.Data)
	}

	// 只改 baseUrl，apiKey 密文应仍然存在。
	res = perform(service, http.MethodPut, "/api/onepanel/config/srv-arm", `{"baseUrl":"https://example.com:9999"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("partial baseUrl update status = %d body=%s", res.Code, res.Body.String())
	}
	res = perform(service, http.MethodGet, "/api/onepanel/config", "")
	mustDecode(t, res, &listed)
	if len(listed.Data) != 1 || !listed.Data[0].HasKey || listed.Data[0].BaseURL != "https://example.com:9999" {
		t.Fatalf("partial baseUrl update wiped key: %#v", listed.Data)
	}

	// 更新不存在的连接应 404。
	res = perform(service, http.MethodPut, "/api/onepanel/config/nope", `{"apiKey":"x"}`)
	if res.Code != http.StatusNotFound {
		t.Fatalf("update missing config should 404, got %d", res.Code)
	}
}

func TestPotentialInjectingPanelPathsRejected(t *testing.T) {
	service := newTestService(t)
	runner := &recordingRunner{}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	// 恶意容器名（含引号/分号）应被 validPanelPath 拦截，任务不应执行。
	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/containers/%22id%3B%23/logs", "")
	if res.Code != http.StatusBadGateway {
		t.Fatalf("injection attempt should be rejected, got %d body=%s", res.Code, res.Body.String())
	}
	if runner.command != "" {
		t.Fatalf("runner should not execute injected command: %q", runner.command)
	}
}

func TestValidPanelPathAndMethod(t *testing.T) {
	if !validPanelPath("/websites/list?page=1") {
		t.Fatal("normal path with query should pass")
	}
	if validPanelPath(`/containers/x"; id; #/logs`) {
		t.Fatal("shell injection path should be rejected")
	}
	if validPanelPath("/websites/../etc/passwd") {
		t.Fatal("dot-dot path should be rejected")
	}
	if !validMethod("GET") || validMethod("EVIL") {
		t.Fatal("method validation wrong")
	}
}

func TestBuildCurlCommandContainsSignature(t *testing.T) {
	cmd := buildCurlCommand("https://127.0.0.1:8888", "abc123", "GET", "/dashboard/base/no/no", "")
	if strings.ContainsRune(cmd, '\n') {
		t.Fatalf("curl command must be single-line for agent 0.5.1 protocol:\n%s", cmd)
	}
	for _, want := range []string{"abc123", "date +%s", "md5sum", "1Panel-Timestamp", "1Panel-Token", "/api/v2/dashboard/base/no/no", "127.0.0.1:8888", "-X GET"} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("curl command missing %q:\n%s", want, cmd)
		}
	}
}

func TestBuildCurlCommandBodyEscaping(t *testing.T) {
	cmd := buildCurlCommand("https://127.0.0.1:8888", "abc123", "POST", "/websites/operate", `{"id":10,"operate":"stop"}`)
	if !strings.Contains(cmd, `-d '{"id":10,"operate":"stop"}'`) {
		t.Fatalf("curl command body not embedded:\n%s", cmd)
	}
}

type recordingRunner struct {
	serverID string
	command  string
	timeout  time.Duration
}

func (r *recordingRunner) RunCommandTaskAndWait(serverID string, command string, timeout time.Duration) (string, error) {
	r.serverID = serverID
	r.command = command
	r.timeout = timeout
	return `{"code":200,"message":"success","data":[]}`, nil
}

func TestProxyListSendsDefaultBodyForSearch(t *testing.T) {
	service := newTestService(t)
	runner := &recordingRunner{}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/containers", "")
	if res.Code != http.StatusOK {
		t.Fatalf("container list status = %d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(runner.command, "/containers/search") {
		t.Fatalf("should target /containers/search, got:\n%s", runner.command)
	}
	if !strings.Contains(runner.command, `-d '{"page":1,"pageSize":50,"state":"all","orderBy":"name","order":"ascending"}'`) {
		t.Fatalf("default search body missing:\n%s", runner.command)
	}
}

func TestProxyGenericForwardsCustomBody(t *testing.T) {
	service := newTestService(t)
	runner := &recordingRunner{}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodPost, "/api/onepanel/srv-arm/proxy", `{"method":"POST","path":"/containers/search","body":{"page":1,"pageSize":10,"state":"running"}}`)
	if res.Code != http.StatusOK {
		t.Fatalf("proxy status = %d body=%s", res.Code, res.Body.String())
	}
	if !strings.Contains(runner.command, `-d '{"page":1,"pageSize":10,"state":"running"}'`) {
		t.Fatalf("custom body not forwarded:\n%s", runner.command)
	}
}

func TestCatalogEndpointReturnsBundledSpec(t *testing.T) {
	service := newTestService(t)
	res := perform(service, http.MethodGet, "/api/onepanel/spec", "")
	if res.Code != http.StatusOK {
		t.Fatalf("spec status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Source    string `json:"source"`
			Count     int    `json:"count"`
			Endpoints []struct {
				M string `json:"m"`
				P string `json:"p"`
				S string `json:"s"`
			} `json:"endpoints"`
		} `json:"data"`
	}
	mustDecode(t, res, &payload)
	if !payload.Success {
		t.Fatalf("spec not success: %#v", payload)
	}
	if payload.Data.Count == 0 || len(payload.Data.Endpoints) == 0 {
		t.Fatalf("spec catalog empty")
	}
	if payload.Data.Source == "" {
		t.Fatalf("spec missing source")
	}
}

func newTestService(t *testing.T) *Service {
	return New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
}

func perform(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func mustDecode(t *testing.T, res *httptest.ResponseRecorder, out interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), out); err != nil {
		t.Fatalf("decode response: %v body=%s", err, res.Body.String())
	}
}