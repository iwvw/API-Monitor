package openai

import (
	"encoding/json"
	"strconv"
	"strings"
)

// ModelPricing 描述单个上游模型的价格。价格单位统一为「每百万 token 的金额」，
// 计算单次调用费用时再除以 1e6 换算为 per-token。CacheRead 为 0 表示上游未提供
// 缓存价，计费时按 OpenAI 惯例取输入价的 1/10。
type ModelPricing struct {
	Currency  string  `json:"currency"`
	Input     float64 `json:"input"`
	Output    float64 `json:"output"`
	CacheRead float64 `json:"cacheRead"`
}

// PricingMap 按模型 id 索引端点定价。
type PricingMap map[string]ModelPricing

// Cost 计算单次调用的费用。缓存命中的 token 计入缓存价；未提供的缓存价按输入价 1/10。
func (p ModelPricing) Cost(promptTokens, completionTokens, cachedTokens int) float64 {
	cached := float64(cachedTokens)
	if cached < 0 {
		cached = 0
	}
	if cached > float64(promptTokens) {
		cached = float64(promptTokens)
	}
	prompt := float64(promptTokens) - cached
	completion := float64(completionTokens)
	cacheUnit := p.CacheRead
	if cacheUnit <= 0 {
		cacheUnit = p.Input / 10
	}
	return (prompt*p.Input + completion*p.Output + cached*cacheUnit) / 1e6
}

// CurrencyName 返回定价货币，缺省 USD。
func (p ModelPricing) CurrencyName() string {
	if strings.TrimSpace(p.Currency) == "" {
		return "USD"
	}
	return strings.ToUpper(strings.TrimSpace(p.Currency))
}

// parsePricingFromItem 从 /models 单条模型 JSON 中提取定价。支持三种格式：
//  1. OpenAI 官方：item.pricing.{prompt,completion,cached_input}（字符串，USD/百万 token）；
//  2. 中转站常见：item.input_price_per_million / output_price_per_million /
//     cache_read_price_per_million（字符串，每百万 token，可带 item.currency），
//     或 item.pricing.{prompt,completion,cache_read} + unit=per_1m_tokens；
//     存在折扣时优先取 effective_* / effective_pricing（实际结算价）；
//  3. Google：item.pricePerUnit.{promptTokens,completionTokens}（数字，per token，需 ×1e6）。
func parsePricingFromItem(item map[string]interface{}) (ModelPricing, bool) {
	var p ModelPricing
	if currency, ok := item["currency"].(string); ok {
		p.Currency = strings.TrimSpace(currency)
	}

	// 中转站顶层字段：折扣价（effective_*）优先，未折扣时与原价相同或缺失。
	topInput := numberString(item["effective_input_price_per_million"])
	if topInput <= 0 {
		topInput = numberString(item["input_price_per_million"])
	}
	topOutput := numberString(item["effective_output_price_per_million"])
	if topOutput <= 0 {
		topOutput = numberString(item["output_price_per_million"])
	}
	if topInput > 0 || topOutput > 0 {
		p.Input = topInput
		p.Output = topOutput
		p.CacheRead = numberString(item["effective_cache_read_price_per_million"])
		if p.CacheRead <= 0 {
			p.CacheRead = numberString(item["cache_read_price_per_million"])
		}
		return p, true
	}

	if pricingObj, ok := item["pricing"].(map[string]interface{}); ok {
		if currency, ok := pricingObj["currency"].(string); ok && currency != "" {
			p.Currency = strings.TrimSpace(currency)
		}
		source := pricingObj
		if effectiveObj, ok := item["effective_pricing"].(map[string]interface{}); ok {
			source = effectiveObj
		}
		multiplier := 1.0
		if unit, _ := source["unit"].(string); strings.Contains(strings.ToLower(unit), "per_1k") {
			multiplier = 1000.0
		}
		p.Input = numberString(source["prompt"]) * multiplier
		p.Output = numberString(source["completion"]) * multiplier
		p.CacheRead = numberString(source["cache_read"]) * multiplier
		if p.CacheRead <= 0 {
			p.CacheRead = numberString(pricingObj["cached_input"]) * multiplier
		}
		if p.Input > 0 || p.Output > 0 {
			return p, true
		}
	}

	if unitObj, ok := item["pricePerUnit"].(map[string]interface{}); ok {
		// Google 的 per-token 金额换算为每百万 token。
		p.Input = numberString(unitObj["promptTokens"]) * 1e6
		p.Output = numberString(unitObj["completionTokens"]) * 1e6
		if p.Input > 0 || p.Output > 0 {
			return p, true
		}
	}

	return ModelPricing{}, false
}

// numberString 从 interface{} 提取正浮点数：兼容 string、float64、json.Number。
func numberString(v interface{}) float64 {
	switch val := v.(type) {
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(val), 64)
		if err != nil || f <= 0 {
			return 0
		}
		return f
	case float64:
		if val <= 0 {
			return 0
		}
		return val
	case json.Number:
		f, err := val.Float64()
		if err != nil || f <= 0 {
			return 0
		}
		return f
	default:
		return 0
	}
}
