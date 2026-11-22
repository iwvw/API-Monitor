package modelhub

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 本文件回归「端点配置多把 API Key 时,检测/刷新只用第一把 key」的问题:
// 第一把 key 被上游拒绝时,应自动回退到其余 key,而不是直接把整体判成失败。

// newMultiKeyUpstream 返回一个只认 goodKey 的上游:Bearer 不匹配则 401,
// 匹配时按路径返回模型列表或 chat 响应。
func newMultiKeyUpstream(goodKey string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != goodKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/models") {
			_, _ = w.Write([]byte(`{"data":[{"id":"m1","object":"model"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{}}`))
	}))
}

// createSkipVerifyEndpoint 建一个跳过上游验证的多 key 端点,返回端点 id。
func createSkipVerifyEndpoint(t *testing.T, s *Service, baseURL, apiKey string, apiKeys []string) string {
	t.Helper()
	apiKeysJSON, err := json.Marshal(apiKeys)
	if err != nil {
		t.Fatalf("marshal apiKeys: %v", err)
	}
	payload := fmt.Sprintf(`{"name":"MultiKey","baseUrl":%q,"apiKey":%q,"apiKeys":%s,"skipVerify":true}`,
		baseURL, apiKey, string(apiKeysJSON))
	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(payload))
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", w.Code, w.Body.String())
	}
	var res struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, w.Body.String(), &res)
	if res.Endpoint.ID == "" {
		t.Fatal("created endpoint has empty id")
	}
	return res.Endpoint.ID
}

// seedEndpointModels 直接把模型写进库(skipVerify 端点 models 为空,需手动注入)。
func seedEndpointModels(t *testing.T, s *Service, id string, models []string) {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	raw, err := json.Marshal(models)
	if err != nil {
		t.Fatalf("marshal models: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		"UPDATE modelhub_endpoints SET models = ?, disabled_models = '[]' WHERE id = ?",
		string(raw), id); err != nil {
		t.Fatalf("seed models: %v", err)
	}
	s.invalidateRouteCache()
}

// TestHealthCheckSingleModelFallsBackToNextKey 第一把 key 失效时健康检测应回退到下一把 key。
func TestHealthCheckSingleModelFallsBackToNextKey(t *testing.T) {
	upstream := newMultiKeyUpstream("good")
	defer upstream.Close()

	s := newOpenAIService(t)
	record := s.healthCheckSingleModel(context.Background(), healthCheckEndpoint{
		baseURL:      upstream.URL + "/v1",
		apiKey:       "bad",
		apiKeys:      []string{"good"},
		upstreamType: "openai",
	}, "m1", 3*time.Second)

	if record.Status != "operational" && record.Status != "degraded" {
		t.Fatalf("第一把 key 失效时应回退到下一把 key,got status=%q error=%q", record.Status, record.Error)
	}
}

// TestHealthCheckSingleModelAllKeysRejectedFails 全部 key 均被拒绝时仍应判失败,并保留最后一次状态码。
func TestHealthCheckSingleModelAllKeysRejectedFails(t *testing.T) {
	upstream := newMultiKeyUpstream("good")
	defer upstream.Close()

	s := newOpenAIService(t)
	record := s.healthCheckSingleModel(context.Background(), healthCheckEndpoint{
		baseURL:      upstream.URL + "/v1",
		apiKey:       "bad",
		apiKeys:      []string{"worse"},
		upstreamType: "openai",
	}, "m1", 3*time.Second)

	if record.Status != "failed" {
		t.Fatalf("全部 key 失效时应判失败,got status=%q", record.Status)
	}
	if record.StatusCode != http.StatusUnauthorized {
		t.Fatalf("失败应保留最后一次 401,got %d", record.StatusCode)
	}
}

// TestVerifyEndpointUsesWorkingKey 「刷新模型列表」在多 key 端点应回退到可用 key。
func TestVerifyEndpointUsesWorkingKey(t *testing.T) {
	upstream := newMultiKeyUpstream("good")
	defer upstream.Close()

	s := newOpenAIService(t)
	id := createSkipVerifyEndpoint(t, s, upstream.URL+"/v1", "bad", []string{"good"})

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/modelhub/endpoints/"+id+"/verify", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("verify status = %d body=%s", w.Code, w.Body.String())
	}
	var res struct {
		Status string   `json:"status"`
		Models []string `json:"models"`
	}
	mustDecode(t, w.Body.String(), &res)
	if res.Status != "valid" {
		t.Fatalf("第一把 key 失效时应回退到可用 key 并判 valid,got status=%q body=%s", res.Status, w.Body.String())
	}
	if len(res.Models) != 1 || res.Models[0] != "m1" {
		t.Fatalf("应拉取到可用 key 的模型列表 [m1],got %v", res.Models)
	}
}

// TestHealthCheckRouteUsesWorkingKey 端点页「模型健康检测」在多 key 端点应回退到可用 key。
func TestHealthCheckRouteUsesWorkingKey(t *testing.T) {
	upstream := newMultiKeyUpstream("good")
	defer upstream.Close()

	s := newOpenAIService(t)
	id := createSkipVerifyEndpoint(t, s, upstream.URL+"/v1", "bad", []string{"good"})
	seedEndpointModels(t, s, id, []string{"m1"})

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/modelhub/endpoints/"+id+"/health-check", strings.NewReader(`{"model":"m1","timeout":3000}`))
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("health-check status = %d body=%s", w.Code, w.Body.String())
	}
	var record HealthRecord
	mustDecode(t, w.Body.String(), &record)
	if record.Status != "operational" && record.Status != "degraded" {
		t.Fatalf("健康检测应回退到可用 key,got status=%q error=%q", record.Status, record.Error)
	}
}

// TestRefreshAllModelsUsesWorkingKey 「刷新全部端点」在多 key 端点也应回退到可用 key。
func TestRefreshAllModelsUsesWorkingKey(t *testing.T) {
	upstream := newMultiKeyUpstream("good")
	defer upstream.Close()

	s := newOpenAIService(t)
	id := createSkipVerifyEndpoint(t, s, upstream.URL+"/v1", "bad", []string{"good"})

	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/modelhub/endpoints/refresh", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh status = %d body=%s", w.Code, w.Body.String())
	}
	var res struct {
		Success bool `json:"success"`
		Results []struct {
			ID      string `json:"id"`
			Success bool   `json:"success"`
		} `json:"results"`
	}
	mustDecode(t, w.Body.String(), &res)
	if !res.Success {
		t.Fatalf("refresh not successful: %s", w.Body.String())
	}
	found := false
	for _, item := range res.Results {
		if item.ID != id {
			continue
		}
		found = true
		if !item.Success {
			t.Fatalf("多 key 端点刷新应成功,got %+v (body=%s)", item, w.Body.String())
		}
	}
	if !found {
		t.Fatalf("refresh results 未包含端点 %s: %s", id, w.Body.String())
	}
}
