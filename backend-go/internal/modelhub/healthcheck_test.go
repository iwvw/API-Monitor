package modelhub

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// clineGroupedModels 模拟 Cline recommended-models 的典型返回：顶层按分类分组，
// 每组是含 id/name 的对象数组（非标准 data/models 结构）。
const clineGroupedModels = `{
	"recommended":[{"id":"anthropic/claude-opus-5","name":"claude-opus-5"}],
	"free":[{"id":"deepseek/deepseek-v4-flash","name":"deepseek-v4-flash"}],
	"clinePass":[{"id":"cline-pass/kimi-k3","name":"cline-pass/kimi-k3"}]
}`

func TestCollectGroupedModels(t *testing.T) {
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(clineGroupedModels), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	models := []string{}
	pricing := PricingMap{}
	if n := collectGroupedModels(parsed, &models, &pricing); n != 3 {
		t.Fatalf("collectGroupedModels count = %d; want 3", n)
	}
	if len(models) != 3 {
		t.Fatalf("models len = %d; want 3: %v", len(models), models)
	}
}

func TestListModelsWithPricingGroupedStructure(t *testing.T) {
	s := newOpenAIService(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(clineGroupedModels))
	}))
	defer mock.Close()

	models, pricing, err := s.listModelsWithPricing(context.Background(), mock.URL, "sk-test", auxEndpoint{id: "ep"}, "", "openai")
	if err != nil {
		t.Fatalf("listModelsWithPricing error: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("models len = %d; want 3: %v", len(models), models)
	}
	if pricing == nil {
		t.Fatal("pricing should be non-nil")
	}
}

func TestVerifyAPIKeyRawGroupedStructure(t *testing.T) {
	s := newOpenAIService(t)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(clineGroupedModels))
	}))
	defer mock.Close()

	ok, count, err := s.verifyAPIKeyRaw(context.Background(), mock.URL, "sk-test", auxEndpoint{id: "ep"}, "", "openai")
	if err != nil {
		t.Fatalf("verifyAPIKeyRaw error: %v", err)
	}
	if !ok {
		t.Fatal("verifyAPIKeyRaw ok = false; want true")
	}
	if count != 3 {
		t.Fatalf("verifyAPIKeyRaw count = %d; want 3", count)
	}
}

type failingProxyPoolSelector struct{}

func (failingProxyPoolSelector) SelectProxy(context.Context, string, string) (string, error) {
	return "", errors.New("boom")
}

func (failingProxyPoolSelector) ReportResult(context.Context, string, string, bool, bool, *time.Duration) error {
	return nil
}

func TestHealthCheckIgnoresInlinePoolWhenProxyDisabled(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer mock.Close()

	s := newOpenAIService(t)
	record := s.healthCheckSingleModel(context.Background(), healthCheckEndpoint{
		baseURL:      mock.URL + "/v1",
		apiKey:       "sk-test",
		pool:         []string{"http://127.0.0.1:1"},
		proxyEnabled: false,
		upstreamType: "openai",
	}, "m", 3*time.Second)
	if record.Status == "failed" {
		t.Fatalf("proxy disabled should bypass inline pool, got failed: %s", record.Error)
	}
}

func TestHealthCheckForceProxyWithEmptyPoolFails(t *testing.T) {
	s := newOpenAIService(t)
	record := s.healthCheckSingleModel(context.Background(), healthCheckEndpoint{
		baseURL:      "http://127.0.0.1:1/v1",
		apiKey:       "sk-test",
		proxyEnabled: true,
		forceProxy:   true,
		upstreamType: "openai",
	}, "m", time.Second)
	if record.Status != "failed" {
		t.Fatalf("force proxy with empty pool should fail, got %s", record.Status)
	}
	if !strings.Contains(record.Error, "强制走代理") {
		t.Fatalf("expected force-proxy error, got %q", record.Error)
	}
}

func TestHealthCheckReportsExternalPoolSelectionError(t *testing.T) {
	s := newOpenAIService(t)
	s.externalPool = failingProxyPoolSelector{}
	record := s.healthCheckSingleModel(context.Background(), healthCheckEndpoint{
		baseURL:      "http://127.0.0.1:1/v1",
		apiKey:       "sk-test",
		proxyPoolID:  "pool-1",
		upstreamType: "openai",
	}, "m", time.Second)
	if record.Status != "failed" {
		t.Fatalf("external pool selection error should fail, got %s", record.Status)
	}
	if !strings.Contains(record.Error, "独立代理池选择失败") {
		t.Fatalf("expected external pool error, got %q", record.Error)
	}
}

func TestListModelsIgnoresInlinePoolWhenProxyDisabled(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer mock.Close()

	s := newOpenAIService(t)
	models, _, err := s.listModelsWithPricing(context.Background(), mock.URL, "sk-test", auxEndpoint{
		id:           "ep",
		pool:         []string{"http://127.0.0.1:1"},
		proxyEnabled: false,
	}, "", "openai")
	if err != nil {
		t.Fatalf("proxy disabled should bypass inline pool, got error: %v", err)
	}
	if len(models) != 1 || models[0] != "m1" {
		t.Fatalf("models = %v; want [m1]", models)
	}
}

func TestVerifyAPIKeyForceProxyWithEmptyPoolFails(t *testing.T) {
	s := newOpenAIService(t)
	_, _, err := s.verifyAPIKeyRaw(context.Background(), "http://127.0.0.1:1", "sk-test", auxEndpoint{
		id:           "ep",
		proxyEnabled: true,
		forceProxy:   true,
	}, "", "openai")
	if err == nil || !strings.Contains(err.Error(), "强制走代理") {
		t.Fatalf("expected force-proxy error, got %v", err)
	}
}
