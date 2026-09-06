package huawei

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

// currentBillCycle 华为云账期按东八区结算，返回 YYYY-MM（如 2026-09）。
// 不使用 time.Local/time.UTC 做站点日期归属，账期由 BSS 服务端时区决定。
func currentBillCycle() string {
	return time.Now().UTC().Add(8 * time.Hour).Format("2006-01")
}

func (s *Service) billing(w http.ResponseWriter, r *http.Request, accountID, view string) {
	account, db, ok := s.accountForRequest(w, r, accountID)
	if !ok {
		return
	}
	defer db.Close()
	c, ok := s.clientForAccount(r.Context(), w, account)
	if !ok {
		return
	}
	switch view {
	case "overview":
		overview, err := s.billingOverview(r.Context(), c, r.URL.Query().Get("cycle"))
		if err != nil {
			response.Error(w, http.StatusBadGateway, "获取费用概览失败："+err.Error())
			return
		}
		response.OK(w, overview)
	case "free-resources":
		usage, err := s.accountFreeResources(r.Context(), c, account.DomainID)
		if err != nil {
			response.Error(w, http.StatusBadGateway, "获取资源包用量失败："+err.Error())
			return
		}
		response.OK(w, usage)
	default:
		response.Error(w, http.StatusNotFound, "huawei billing view not found")
	}
}

type normalBalance struct {
	AccountType int     `json:"accountType"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency,omitempty"`
}

type normalMonthlySum struct {
	ServiceTypeName  string  `json:"serviceTypeName"`
	ResourceTypeName string  `json:"resourceTypeName,omitempty"`
	ConsumeAmount    float64 `json:"consumeAmount"`
	OfficialAmount   float64 `json:"officialAmount,omitempty"`
	ChargeMode       int     `json:"chargeMode"`
}

type normalBillingOverview struct {
	Cycle        string             `json:"cycle"`
	Currency     string             `json:"currency,omitempty"`
	DebtAmount   float64            `json:"debtAmount"`
	Balances     []normalBalance    `json:"balances,omitempty"`
	MonthlySums  []normalMonthlySum `json:"monthlySums,omitempty"`
	TotalConsume float64            `json:"totalConsume"`
}

func (s *Service) billingOverview(ctx context.Context, c *client, cycle string) (normalBillingOverview, error) {
	if cycle == "" {
		cycle = currentBillCycle()
	}

	var balanceRes struct {
		AccountBalances []struct {
			AccountType int     `json:"account_type"`
			Amount      float64 `json:"amount"`
			Currency    string  `json:"currency"`
		} `json:"account_balances"`
		DebtAmount float64 `json:"debt_amount"`
		Currency   string  `json:"currency"`
	}
	if err := c.do(ctx, "bss", http.MethodGet, "/v2/accounts/customer-accounts/balances", nil, nil, &balanceRes); err != nil {
		return normalBillingOverview{}, err
	}

	var monthRes struct {
		BillSums []struct {
			ServiceTypeName  string  `json:"service_type_name"`
			ResourceTypeName string  `json:"resource_type_name"`
			ConsumeAmount    float64 `json:"consume_amount"`
			OfficialAmount   float64 `json:"official_amount"`
			ChargeMode       int     `json:"charging_mode"`
		} `json:"bill_sums"`
	}
	monthQuery := url.Values{}
	monthQuery.Set("bill_cycle", cycle)
	monthQuery.Set("limit", "1000")
	if err := c.do(ctx, "bss", http.MethodGet, "/v2/bills/customer-bills/monthly-sum", monthQuery, nil, &monthRes); err != nil {
		return normalBillingOverview{}, err
	}

	overview := normalBillingOverview{
		Cycle:      cycle,
		Currency:   balanceRes.Currency,
		DebtAmount: balanceRes.DebtAmount,
	}
	for _, balance := range balanceRes.AccountBalances {
		overview.Balances = append(overview.Balances, normalBalance{
			AccountType: balance.AccountType,
			Amount:      balance.Amount,
			Currency:    balance.Currency,
		})
	}
	for _, sum := range monthRes.BillSums {
		overview.MonthlySums = append(overview.MonthlySums, normalMonthlySum{
			ServiceTypeName:  sum.ServiceTypeName,
			ResourceTypeName: sum.ResourceTypeName,
			ConsumeAmount:    sum.ConsumeAmount,
			OfficialAmount:   sum.OfficialAmount,
			ChargeMode:       sum.ChargeMode,
		})
		overview.TotalConsume += sum.ConsumeAmount
	}
	return overview, nil
}

// accountFreeResources 聚合账号下 Flexus L 套餐的流量包用量。
func (s *Service) accountFreeResources(ctx context.Context, c *client, domainID string) ([]normalFlexusTraffic, error) {
	instances, err := listFlexusInstances(ctx, c, domainID)
	if err != nil {
		return nil, err
	}
	freeResourceIDs := []string{}
	for _, instance := range instances {
		for _, res := range instance.ComposedResources {
			if strings.HasPrefix(res.TypeName, "huaweicloudinternal_cbc_freeresource") {
				freeResourceIDs = append(freeResourceIDs, res.ID)
			}
		}
	}
	return queryFlexusTraffic(ctx, c, freeResourceIDs)
}