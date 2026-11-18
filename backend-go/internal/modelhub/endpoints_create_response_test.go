package modelhub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateEndpointResponseEchoesSubmittedConfig 固化创建端点的响应契约：
// 响应体里的 endpoint 必须回显本次提交的多 key、代理开关与强制代理状态。
// 回归背景：resEndpoint 曾漏赋值 APIKeys/ProxyEnabled/ForceProxy，导致响应只
// 带主 key，前端若用创建响应渲染会误以为扩展 key 与代理设置没保存（落库是完整的）。
func TestCreateEndpointResponseEchoesSubmittedConfig(t *testing.T) {
	service := newOpenAIService(t)

	payload := `{
		"name": "Echo Config",
		"baseUrl": "https://upstream.example.com/v1",
		"apiKey": "main-key",
		"apiKeys": ["extra-1", "extra-2"],
		"proxyEnabled": true,
		"forceProxy": true,
		"autoSwitch": true,
		"keyRetryRounds": 3,
		"keyStrategy": "random",
		"keyCooldownSeconds": 120,
		"skipVerify": true
	}`

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(payload))
	service.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}

	var res struct {
		Success  bool     `json:"success"`
		Endpoint Endpoint `json:"endpoint"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.Success {
		t.Fatalf("create not successful: %s", w.Body.String())
	}

	ep := res.Endpoint
	if ep.APIKey != "main-key" {
		t.Fatalf("APIKey = %q, want main-key", ep.APIKey)
	}
	if len(ep.APIKeys) != 2 || ep.APIKeys[0] != "extra-1" || ep.APIKeys[1] != "extra-2" {
		t.Fatalf("APIKeys not echoed in create response: %#v", ep.APIKeys)
	}
	if !ep.ProxyEnabled {
		t.Fatalf("ProxyEnabled not echoed in create response")
	}
	if !ep.ForceProxy {
		t.Fatalf("ForceProxy not echoed in create response")
	}
	if !ep.AutoSwitch {
		t.Fatalf("AutoSwitch not echoed in create response")
	}
	if ep.KeyRetryRounds != 3 {
		t.Fatalf("KeyRetryRounds = %d, want 3", ep.KeyRetryRounds)
	}
	if ep.KeyStrategy != "random" {
		t.Fatalf("KeyStrategy = %q, want random", ep.KeyStrategy)
	}
	if ep.KeyCooldownSeconds != 120 {
		t.Fatalf("KeyCooldownSeconds = %d, want 120", ep.KeyCooldownSeconds)
	}
	// 统一口径：响应里的 key 集合必须与落库后可服务的一致。
	if got := len(ep.AllKeys()); got != 3 {
		t.Fatalf("AllKeys() = %d, want 3 (main + 2 extra)", got)
	}
}