package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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

	models, pricing, err := s.listModelsWithPricing(context.Background(), mock.URL, "sk-test", "ep", nil, "")
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

	ok, count, err := s.verifyAPIKeyRaw(context.Background(), mock.URL, "sk-test", "ep", nil, "")
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