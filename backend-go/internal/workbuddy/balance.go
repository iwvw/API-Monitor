package workbuddy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// balanceQueryTimeout 是单次余额查询的超时。
const balanceQueryTimeout = 20 * time.Second

// BalancePackage 是一个计费包（上游 billing 的一个 Capacity 记录）。
type BalancePackage struct {
	Name      string  `json:"name,omitempty"`
	Remain    float64 `json:"remain"`
	Size      float64 `json:"size"`
	Used      float64 `json:"used"`
	CycleEnd  string  `json:"cycleEnd,omitempty"`
	CapacityT int64   `json:"capacityType,omitempty"`
}

// Balance 是一个账号的余额汇总（下发前端）。
type Balance struct {
	// Remain 是剩余额度（各计费包求和）。
	Remain float64 `json:"remain"`
	// Size 是总额度。
	Size float64 `json:"size"`
	// Used 是已用额度。
	Used float64 `json:"used"`
	// Packages 是各计费包明细（可选展示）。
	Packages []BalancePackage `json:"packages,omitempty"`
	// UpdatedAt 是本次查询时刻（RFC3339，UTC）。
	UpdatedAt string `json:"updatedAt,omitempty"`
	// Error 非空表示查询失败（账号行仍可展示，只是无余额）。
	Error string `json:"error,omitempty"`
}

// endpointBilling 返回区域计费接口的 URL。
func endpointBilling(region string) string {
	return regionBillingHost(region) + "/v2/billing/meter/get-user-resource"
}

// fetchBalance 查询单个账号的余额。国内走 codebuddy.cn、国际走 workbuddy.ai。
func (s *Service) fetchBalance(ctx context.Context, acc Account) (Balance, error) {
	if strings.TrimSpace(acc.AccessToken) == "" {
		return Balance{}, fmt.Errorf("账号缺少 access token")
	}
	region := normalizeRegion(acc.Region)
	referer := regionBillingHost(region)
	headers := func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Accept", "application/json")
		r.Header.Set("User-Agent", regionUA(region))
		r.Header.Set("Origin", referer)
		r.Header.Set("Referer", referer+"/")
		r.Header.Set("Authorization", "Bearer "+acc.AccessToken)
		if acc.UID != "" {
			r.Header.Set("X-User-Id", acc.UID)
		}
		if acc.EnterpriseID != "" {
			r.Header.Set("X-Enterprise-Id", acc.EnterpriseID)
			r.Header.Set("X-Tenant-Id", acc.EnterpriseID)
		}
		if acc.Domain != "" {
			r.Header.Set("X-Domain", acc.Domain)
		}
		r.Header.Set("X-Product", "SaaS")
	}

	now := time.Now()
	body, _ := json.Marshal(map[string]any{
		"PageNumber":               1,
		"PageSize":                 100,
		"ProductCode":              "p_tcaca",
		"Status":                   []int{0, 3},
		"PackageEndTimeRangeBegin": now.Format("2006-01-02 15:04:05"),
		"PackageEndTimeRangeEnd":   "2036-01-01 00:00:00",
	})

	ctx, cancel := context.WithTimeout(ctx, balanceQueryTimeout)
	defer cancel()
	data, _, err := doJSON(ctx, s.httpClientFor(acc.ID), http.MethodPost, endpointBilling(region), headers, bytes.NewReader(body))
	if err != nil {
		return Balance{}, err
	}
	return parseBalance(data)
}

// parseBalance 解析 billing 响应。结构：
//
//	{data:{Response:{Data:{Accounts:[{CapacitySize,CapacityRemain,
//	  CycleCapacitySize,CycleCapacityRemain,...}]}}}}
//
// 与参考实现一致：优先用 Cycle* 字段（周期额度），否则退回 Capacity*；
// 负值按 0 处理，size 不小于 remain。
func parseBalance(data json.RawMessage) (Balance, error) {
	var resp struct {
		Response struct {
			Data struct {
				Accounts []struct {
					DealName            string  `json:"DealName"`
					CapacityType        int64   `json:"CapacityType"`
					CapacitySize        float64 `json:"CapacitySize"`
					CapacityRemain      float64 `json:"CapacityRemain"`
					CycleCapacitySize   float64 `json:"CycleCapacitySize"`
					CycleCapacityRemain float64 `json:"CycleCapacityRemain"`
					CycleEndTime        string  `json:"CycleEndTime"`
				} `json:"Accounts"`
			} `json:"Data"`
		} `json:"Response"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return Balance{}, fmt.Errorf("余额响应解析失败: %w", err)
	}
	out := Balance{Packages: []BalancePackage{}, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}
	for _, p := range resp.Response.Data.Accounts {
		size, remain := p.CapacitySize, p.CapacityRemain
		if p.CycleCapacitySize != 0 || p.CycleCapacityRemain != 0 {
			size, remain = p.CycleCapacitySize, p.CycleCapacityRemain
		}
		if remain < 0 {
			remain = 0
		}
		if size < remain {
			size = remain
		}
		used := size - remain
		if used < 0 {
			used = 0
		}
		out.Size += size
		out.Remain += remain
		out.Used += used
		out.Packages = append(out.Packages, BalancePackage{
			Name:      p.DealName,
			Size:      size,
			Remain:    remain,
			Used:      used,
			CycleEnd:  p.CycleEndTime,
			CapacityT: p.CapacityType,
		})
	}
	return out, nil
}

// handleAccountBalance 查询单个账号余额（GET /api/workbuddy/accounts/{id}/balance）。
func (s *Service) handleAccountBalance(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		responseJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"success": false, "error": "method not allowed"})
		return
	}
	acc, ok := s.findAccount(id)
	if !ok {
		responseJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "error": "账号不存在"})
		return
	}
	bal, err := s.fetchBalance(r.Context(), acc)
	if err != nil {
		responseJSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	responseJSON(w, map[string]interface{}{"success": true, "balance": bal})
}
