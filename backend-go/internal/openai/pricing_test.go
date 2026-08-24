package openai

import (
	"math"
	"testing"
)

func TestParsePricingFromItem_Tokenrhythm(t *testing.T) {
	item := map[string]interface{}{
		"currency":                           "CNY",
		"input_price_per_million":            "1.00000000",
		"output_price_per_million":           "2.00000000",
		"cache_read_price_per_million":       "0.20000000",
		"effective_input_price_per_million":  "1.00000000",
		"effective_output_price_per_million": "2.00000000",
	}
	p, ok := parsePricingFromItem(item)
	if !ok {
		t.Fatal("expected pricing to parse")
	}
	if p.Currency != "CNY" || p.Input != 1.0 || p.Output != 2.0 || p.CacheRead != 0.2 {
		t.Fatalf("unexpected pricing: %+v", p)
	}
	if p.CurrencyName() != "CNY" {
		t.Fatalf("unexpected currency name: %s", p.CurrencyName())
	}
}

func TestParsePricingFromItem_OpenAI(t *testing.T) {
	item := map[string]interface{}{
		"pricing": map[string]interface{}{
			"prompt":       "0.5",
			"completion":   "1.5",
			"cached_input": "0.05",
		},
	}
	p, ok := parsePricingFromItem(item)
	if !ok {
		t.Fatal("expected pricing to parse")
	}
	if p.Input != 0.5 || p.Output != 1.5 || p.CacheRead != 0.05 {
		t.Fatalf("unexpected pricing: %+v", p)
	}
	if p.CurrencyName() != "USD" {
		t.Fatalf("expected default USD, got %s", p.CurrencyName())
	}
}

func TestParsePricingFromItem_Google(t *testing.T) {
	item := map[string]interface{}{
		"pricePerUnit": map[string]interface{}{
			"promptTokens":     0.0000005,
			"completionTokens": 0.0000015,
		},
	}
	p, ok := parsePricingFromItem(item)
	if !ok {
		t.Fatal("expected pricing to parse")
	}
	if math.Abs(p.Input-0.5) > 1e-9 || math.Abs(p.Output-1.5) > 1e-9 {
		t.Fatalf("unexpected pricing: %+v", p)
	}
}

func TestParsePricingFromItem_DiscountPreferred(t *testing.T) {
	item := map[string]interface{}{
		"currency":                                "CNY",
		"input_price_per_million":                 "12.00000000",
		"output_price_per_million":                "24.00000000",
		"cache_read_price_per_million":            "1.00000000",
		"effective_input_price_per_million":       "3.00000000",
		"effective_output_price_per_million":      "6.00000000",
		"effective_cache_read_price_per_million":  "0.02500000",
	}
	p, ok := parsePricingFromItem(item)
	if !ok {
		t.Fatal("expected pricing to parse")
	}
	if p.Input != 3.0 || p.Output != 6.0 || p.CacheRead != 0.025 {
		t.Fatalf("expected effective (discounted) prices, got: %+v", p)
	}
}

func TestParsePricingFromItem_Missing(t *testing.T) {
	if _, ok := parsePricingFromItem(map[string]interface{}{"id": "x"}); ok {
		t.Fatal("expected no pricing for item without price fields")
	}
}

func TestModelPricingCost(t *testing.T) {
	p := ModelPricing{Currency: "USD", Input: 1.0, Output: 2.0, CacheRead: 0.1}
	// 输入 1000（其中 200 缓存命中），输出 500：
	// (800*1 + 200*0.1 + 500*2) / 1e6 = 1820/1e6 = 0.00182
	cost := p.Cost(1000, 500, 200)
	if math.Abs(cost-0.00182) > 1e-9 {
		t.Fatalf("unexpected cost: %v", cost)
	}
}

func TestModelPricingCost_CacheFallback(t *testing.T) {
	p := ModelPricing{Currency: "USD", Input: 1.0, Output: 2.0}
	// 无缓存价时按输入价 1/10：(1000*1 + 1000*0.1 + 1000*2) / 1e6 = 0.0031
	cost := p.Cost(2000, 1000, 1000)
	if math.Abs(cost-0.0031) > 1e-9 {
		t.Fatalf("unexpected cost: %v", cost)
	}
}

func TestModelPricingCost_CachedClamped(t *testing.T) {
	p := ModelPricing{Currency: "USD", Input: 1.0, Output: 2.0}
	// cached 超过 prompt 时按 prompt 截断，避免负的未缓存输入。
	cost := p.Cost(100, 100, 500)
	if cost < 0 {
		t.Fatalf("cost must not be negative: %v", cost)
	}
}

func TestComputeRecordCost(t *testing.T) {
	pricing := PricingMap{
		"deepseek-v4": {Currency: "CNY", Input: 1.0, Output: 2.0},
	}
	item := analyticsWriteItem{
		model:            "alias",
		realModel:        "deepseek-v4",
		totalTokens:      1500,
		promptTokens:     1000,
		completionTokens: 500,
		cachedTokens:     0,
	}
	cost, currency := computeRecordCost(item, pricing)
	if math.Abs(cost-0.002) > 1e-9 || currency != "CNY" {
		t.Fatalf("unexpected cost=%v currency=%s", cost, currency)
	}

	item2 := item
	item2.realModel = ""
	item2.model = "deepseek-v4"
	cost2, cur2 := computeRecordCost(item2, pricing)
	if math.Abs(cost2-0.002) > 1e-9 || cur2 != "CNY" {
		t.Fatalf("unexpected fallback cost=%v currency=%s", cost2, cur2)
	}
}

func TestComputeRecordCost_NoPricing(t *testing.T) {
	item := analyticsWriteItem{model: "m", totalTokens: 100, promptTokens: 100}
	if cost, cur := computeRecordCost(item, nil); cost != 0 || cur != "" {
		t.Fatalf("expected zero cost for no pricing, got %v %s", cost, cur)
	}
	item2 := analyticsWriteItem{model: "m", totalTokens: 100, promptTokens: 100}
	if cost, cur := computeRecordCost(item2, PricingMap{}); cost != 0 || cur != "" {
		t.Fatalf("expected zero cost for empty pricing, got %v %s", cost, cur)
	}
}
