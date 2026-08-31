package oracle

import (
	"context"
	"time"

	"github.com/oracle/oci-go-sdk/v65/budget"
	"github.com/oracle/oci-go-sdk/v65/common"
	"github.com/oracle/oci-go-sdk/v65/usageapi"
)

// CostOverview 账号整体成本概览（按月汇总 + 预算）。
type CostOverview struct {
	Currency       string         `json:"currency,omitempty"`
	CurrentMonth   *CostBucket    `json:"currentMonth,omitempty"`
	PreviousMonth  *CostBucket    `json:"previousMonth,omitempty"`
	Budgets        []BudgetItem   `json:"budgets,omitempty"`
	ByService      []CostBreakdown `json:"byService,omitempty"`
	MonthlyHistory []CostBucket   `json:"monthlyHistory,omitempty"`
}

// CostBucket 单个月份的成本桶（cost + 用量）。
type CostBucket struct {
	Month   string  `json:"month"`   // YYYY-MM
	Cost    float64 `json:"cost"`
	Usage   float64 `json:"usage,omitempty"`
	Forecast float64 `json:"forecast,omitempty"`
}

// CostBreakdown 按服务维度的成本分解。
type CostBreakdown struct {
	Service string  `json:"service"`
	Cost    float64 `json:"cost"`
}

// BudgetItem 单个预算项。
type BudgetItem struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Amount        float64 `json:"amount"`
	ActualSpend   float64 `json:"actualSpend"`
	ForecastSpend float64 `json:"forecastSpend"`
	ResetPeriod   string  `json:"resetPeriod"`
}

// monthlyRange 返回某月首日/末日（UTC）。
func monthlyRange(year int, month time.Month) (time.Time, time.Time) {
	start := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0).Add(-time.Nanosecond)
	return start, end
}

// summarizeMonth 拉取指定月份的成本/用量（MONTHLY 粒度，全租户聚合）。
func (s *Service) summarizeMonth(ctx context.Context, account Account, start, end time.Time, queryType usageapi.RequestSummarizedUsagesDetailsQueryTypeEnum) (*CostBucket, error) {
	client, err := s.clients.usage(account)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := contextWithReadTimeout(ctx)
	defer cancel()
	req := usageapi.RequestSummarizedUsagesRequest{
		RequestSummarizedUsagesDetails: usageapi.RequestSummarizedUsagesDetails{
			TenantId:         common.String(account.TenancyOCID),
			TimeUsageStarted: &common.SDKTime{Time: start},
			TimeUsageEnded:   &common.SDKTime{Time: end},
			Granularity:      usageapi.RequestSummarizedUsagesDetailsGranularityMonthly,
			QueryType:        queryType,
			IsAggregateByTime: common.Bool(true),
		},
	}
	resp, err := client.RequestSummarizedUsages(callCtx, req)
	if err != nil {
		return nil, err
	}
	bucket := &CostBucket{Month: start.Format("2006-01")}
	for _, item := range resp.Items {
		isForecast := item.IsForecast != nil && *item.IsForecast
		if item.ComputedAmount != nil {
			if isForecast {
				bucket.Forecast += float64(*item.ComputedAmount)
			} else {
				bucket.Cost += float64(*item.ComputedAmount)
			}
		}
		if item.ComputedQuantity != nil {
			bucket.Usage += float64(*item.ComputedQuantity)
		}
	}
	return bucket, nil
}

// summarizeByService 按服务聚合成本（MONTHLY，groupBy=service）。
func (s *Service) summarizeByService(ctx context.Context, account Account, start, end time.Time) ([]CostBreakdown, error) {
	client, err := s.clients.usage(account)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := contextWithReadTimeout(ctx)
	defer cancel()
	req := usageapi.RequestSummarizedUsagesRequest{
		RequestSummarizedUsagesDetails: usageapi.RequestSummarizedUsagesDetails{
			TenantId:          common.String(account.TenancyOCID),
			TimeUsageStarted:  &common.SDKTime{Time: start},
			TimeUsageEnded:    &common.SDKTime{Time: end},
			Granularity:       usageapi.RequestSummarizedUsagesDetailsGranularityMonthly,
			QueryType:         usageapi.RequestSummarizedUsagesDetailsQueryTypeCost,
			IsAggregateByTime: common.Bool(true),
			GroupBy:           []string{"service"},
		},
	}
	resp, err := client.RequestSummarizedUsages(callCtx, req)
	if err != nil {
		return nil, err
	}
	byService := make([]CostBreakdown, 0, len(resp.Items))
	for _, item := range resp.Items {
		name := ""
		if item.Service != nil {
			name = *item.Service
		}
		cost := 0.0
		if item.ComputedAmount != nil {
			cost = float64(*item.ComputedAmount)
		}
		if name != "" {
			byService = append(byService, CostBreakdown{Service: name, Cost: cost})
		}
	}
	return byService, nil
}

// listBudgets 列出账号预算。
func (s *Service) listBudgets(ctx context.Context, account Account) ([]BudgetItem, error) {
	client, err := s.clients.budget(account)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := contextWithReadTimeout(ctx)
	defer cancel()
	resp, err := client.ListBudgets(callCtx, budget.ListBudgetsRequest{
		CompartmentId: common.String(account.TenancyOCID),
	})
	if err != nil {
		return nil, err
	}
	items := make([]BudgetItem, 0, len(resp.Items))
	for _, b := range resp.Items {
		item := BudgetItem{
			ID:          stringValuePtr(b.Id),
			Name:        stringValuePtr(b.DisplayName),
			Amount:      float64(float32ValuePtr(b.Amount)),
			ResetPeriod: string(b.ResetPeriod),
		}
		if b.ActualSpend != nil {
			item.ActualSpend = float64(*b.ActualSpend)
		}
		if b.ForecastedSpend != nil {
			item.ForecastSpend = float64(*b.ForecastedSpend)
		}
		items = append(items, item)
	}
	return items, nil
}

// costOverview 聚合账号成本概览：当前月/上月成本、按服务分解、预算、近 6 月历史。
func (s *Service) costOverview(ctx context.Context, account Account) (CostOverview, error) {
	now := time.Now().UTC()
	curY, curM := now.Year(), now.Month()
	prevStart, _ := monthlyRange(curY, curM)
	prevEnd := prevStart.Add(-time.Nanosecond)
	prevY, prevM := prevEnd.Year(), prevEnd.Month()

	var ov CostOverview
	var err error

	// 当前月成本
	if ov.CurrentMonth, err = s.summarizeMonth(ctx, account, prevStart, now, usageapi.RequestSummarizedUsagesDetailsQueryTypeCost); err != nil {
		return ov, err
	}
	// 上月成本
	pmStart, pmEnd := monthlyRange(prevY, prevM)
	if ov.PreviousMonth, err = s.summarizeMonth(ctx, account, pmStart, pmEnd, usageapi.RequestSummarizedUsagesDetailsQueryTypeCost); err != nil {
		return ov, err
	}
	// 按服务分解（当前月）
	if ov.ByService, err = s.summarizeByService(ctx, account, prevStart, now); err != nil {
		return ov, err
	}
	// 预算
	if ov.Budgets, err = s.listBudgets(ctx, account); err != nil {
		return ov, err
	}
	// 近 6 个月历史
	for i := 5; i >= 0; i-- {
		y, m := curY, curM
		mm := int(m) - i
		for mm <= 0 {
			mm += 12
			y--
		}
		start, end := monthlyRange(y, time.Month(mm))
		if y == curY && time.Month(mm) == curM {
			end = now
		}
		bucket, err := s.summarizeMonth(ctx, account, start, end, usageapi.RequestSummarizedUsagesDetailsQueryTypeCost)
		if err != nil {
			continue
		}
		ov.MonthlyHistory = append(ov.MonthlyHistory, *bucket)
	}
	return ov, nil
}
