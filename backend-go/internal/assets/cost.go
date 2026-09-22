package assets

import (
	"math"
	"sort"
	"strings"
)

// 成本口径：列表按原周期展示；汇总时折算为月均。
// one_time 不计入经常性成本并单列，usage 不计入月均。
// 默认按币种分组，不做跨币种自动换算；配置基准币种与汇率后才提供合计。

const (
	cycleMonthly   = "monthly"
	cycleQuarterly = "quarterly"
	cycleYearly    = "yearly"
	cycleOneTime   = "one_time"
	cycleUsage     = "usage"
)

var validCycles = map[string]bool{
	cycleMonthly:   true,
	cycleQuarterly: true,
	cycleYearly:    true,
	cycleOneTime:   true,
	cycleUsage:     true,
}

func normalizeCycle(value string) string {
	cycle := strings.ToLower(strings.TrimSpace(value))
	if cycle == "" {
		return ""
	}
	if validCycles[cycle] {
		return cycle
	}
	return ""
}

func normalizeCurrency(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

// monthlyEquivalent 把金额按周期折算为月均。one_time 与 usage 返回 0。
func monthlyEquivalent(amount float64, cycle string) float64 {
	switch normalizeCycle(cycle) {
	case cycleMonthly:
		return amount
	case cycleQuarterly:
		return amount / 3
	case cycleYearly:
		return amount / 12
	default:
		return 0
	}
}

// aggregateCosts 按币种分组汇总月均与一次性成本。
func aggregateCosts(assets []Asset) []CurrencyCost {
	byCurrency := map[string]*CurrencyCost{}
	for _, asset := range assets {
		if asset.CostAmount <= 0 {
			continue
		}
		currency := normalizeCurrency(asset.CostCurrency)
		if currency == "" {
			currency = "UNSPECIFIED"
		}
		entry, ok := byCurrency[currency]
		if !ok {
			entry = &CurrencyCost{Currency: currency}
			byCurrency[currency] = entry
		}
		entry.Count++
		switch normalizeCycle(asset.CostCycle) {
		case cycleOneTime:
			entry.OneTime += asset.CostAmount
		default:
			entry.Monthly += monthlyEquivalent(asset.CostAmount, asset.CostCycle)
		}
	}

	result := make([]CurrencyCost, 0, len(byCurrency))
	for _, entry := range byCurrency {
		entry.Monthly = math.Round(entry.Monthly*100) / 100
		entry.OneTime = math.Round(entry.OneTime*100) / 100
		result = append(result, *entry)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Currency < result[j].Currency
	})
	return result
}

// convertToBase 把各币种月均按配置汇率折算到基准币种。
// 仅当基准币种非空且汇率可用时返回合计；否则返回 nil 表示不做合计。
func convertToBase(costs []CurrencyCost, settings Settings) map[string]float64 {
	base := normalizeCurrency(settings.BaseCurrency)
	if base == "" {
		return nil
	}
	total := 0.0
	for _, cost := range costs {
		if cost.Currency == base {
			total += cost.Monthly
			continue
		}
		rate, ok := settings.ExchangeRates[cost.Currency]
		if !ok || rate <= 0 {
			return nil
		}
		total += cost.Monthly * rate
	}
	return map[string]float64{base: math.Round(total*100) / 100}
}
