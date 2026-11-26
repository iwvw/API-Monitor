package onepanel

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// scriptedRunner 让测试精确控制上游返回,用于验证错误收敛与成功信封。
type scriptedRunner struct {
	output  string
	err     error
	command string
}

func (r *scriptedRunner) RunCommandTaskAndWait(_ string, command string, _ time.Duration) (string, error) {
	r.command = command
	return r.output, r.err
}

func (r *scriptedRunner) RunOnePanelProbe(_ string, _ time.Duration) (string, error) {
	return "cmd=1\ndir=1\nproc=1\nport=1\n", nil
}

// TestPanelProxySurfacesUpstreamError 验证 1Panel 的逻辑失败不再以 200 静默返回,
// 而是映射成真实 HTTP 状态并带上可重试的上下文(serverId / 上游路径 / 发送的 id / 取 id 指引)。
func TestPanelProxySurfacesUpstreamError(t *testing.T) {
	service := newTestService(t)
	runner := &scriptedRunner{output: `{"code":500,"message":"服务错误: record not found","data":null}`}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/ssl/17", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("record not found should map to 404, got %d body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	mustDecode(t, res, &env)
	if env.Success {
		t.Fatalf("failed response must set success=false: %s", res.Body.String())
	}
	for _, want := range []string{"record not found", "serverId=srv-arm", "/websites/ssl/17", "id=17", "/ssl/search"} {
		if !strings.Contains(env.Error, want) {
			t.Fatalf("error message missing %q:\n%s", want, env.Error)
		}
	}
}

// TestPanelProxySuccessAddsEnvelope 验证成功响应保留 1Panel 顶层字段,并附加 success:true
// (向后兼容既有调用方按 .data 取值,同时与面板统一信封对齐)。
func TestPanelProxySuccessAddsEnvelope(t *testing.T) {
	service := newTestService(t)
	runner := &scriptedRunner{output: `{"code":200,"message":"","data":{"items":[{"id":1}]}}`}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/ssl", "")
	if res.Code != http.StatusOK {
		t.Fatalf("success should stay 200, got %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Items []map[string]interface{} `json:"items"`
		} `json:"data"`
	}
	mustDecode(t, res, &payload)
	if !payload.Success {
		t.Fatalf("success response must set success=true: %s", res.Body.String())
	}
	if len(payload.Data.Items) != 1 {
		t.Fatalf("data should be preserved verbatim: %s", res.Body.String())
	}
}

// TestProxyGenericSurfacesUpstreamError 验证通用代理同样收敛错误并带上上游路径。
func TestProxyGenericSurfacesUpstreamError(t *testing.T) {
	service := newTestService(t)
	runner := &scriptedRunner{output: `{"code":500,"message":"服务错误: record not found","data":null}`}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodPost, "/api/onepanel/srv-arm/proxy", `{"method":"GET","path":"/websites/ssl/99"}`)
	if res.Code != http.StatusNotFound {
		t.Fatalf("generic proxy should surface 404, got %d body=%s", res.Code, res.Body.String())
	}
	var env struct {
		Error string `json:"error"`
	}
	mustDecode(t, res, &env)
	if !strings.Contains(env.Error, "/websites/ssl/99") || !strings.Contains(env.Error, "serverId=srv-arm") {
		t.Fatalf("generic proxy error missing context: %s", env.Error)
	}
}

// TestPanelProxyNonEnvelopePassthrough 验证非 1Panel 信封的裸 JSON 不被误判为失败。
func TestPanelProxyNonEnvelopePassthrough(t *testing.T) {
	service := newTestService(t)
	runner := &scriptedRunner{output: `{"hello":"world"}`}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodPost, "/api/onepanel/srv-arm/proxy", `{"method":"GET","path":"/raw/thing"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("non-envelope body should pass through 200, got %d body=%s", res.Code, res.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["hello"] != "world" || payload["success"] != true {
		t.Fatalf("passthrough should keep fields and add success:true: %s", res.Body.String())
	}
}

func TestPanelFailureStatusMapping(t *testing.T) {
	cases := []struct {
		code    int
		message string
		want    int
	}{
		{500, "服务错误: record not found", http.StatusNotFound},
		{400, "参数错误", http.StatusBadRequest},
		{401, "token 无效", http.StatusUnauthorized},
		{403, "permission denied", http.StatusForbidden},
		{500, "内部错误", http.StatusBadGateway},
		{500, "exec error: no such file", http.StatusBadGateway},
	}
	for _, tc := range cases {
		if got := panelFailureStatus(tc.code, tc.message); got != tc.want {
			t.Fatalf("panelFailureStatus(%d,%q)=%d want %d", tc.code, tc.message, got, tc.want)
		}
	}
}
