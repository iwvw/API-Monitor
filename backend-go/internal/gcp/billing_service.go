package gcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// ==================== 费用 ====================

func (s *Service) listBillingAccounts(ctx context.Context, c *client) ([]normalBillingAccount, error) {
	query := url.Values{}
	var items []normalBillingAccount
	err := c.listJSON(ctx, http.MethodGet, "billing", "billingAccounts", query, "billingAccounts", nil, func(raw json.RawMessage) error {
		var account struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Open        bool   `json:"open"`
		}
		_ = json.Unmarshal(raw, &account)
		items = append(items, normalBillingAccount{Name: account.Name, DisplayName: account.DisplayName, Open: account.Open})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Service) getBillingInfo(ctx context.Context, c *client, projectID string) (normalBillingInfo, error) {
	path := "projects/" + projectID + "/billingInfo"
	var info normalBillingInfo
	if err := c.do(ctx, http.MethodGet, "billing", path, nil, nil, &info); err != nil {
		return normalBillingInfo{}, err
	}
	info.Name = strings.TrimPrefix(info.Name, "projects/")
	return info, nil
}

func (s *Service) listBudgets(ctx context.Context, c *client, billingAccountID string) ([]normalBudget, error) {
	query := url.Values{}
	path := "billingAccounts/" + strings.TrimPrefix(billingAccountID, "billingAccounts/") + "/budgets"
	var items []normalBudget
	err := c.listJSON(ctx, http.MethodGet, "budgets", path, query, "budgets", nil, func(raw json.RawMessage) error {
		items = append(items, budgetFromRaw(raw))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return items, nil
}

func budgetFromRaw(raw json.RawMessage) normalBudget {
	var budget struct {
		Name         string `json:"name"`
		DisplayName  string `json:"displayName"`
		Amount       map[string]interface{} `json:"amount"`
		ThresholdRules []struct {
			ThresholdPercent float64 `json:"thresholdPercent"`
			SpendBasis       string  `json:"spendBasis"`
		} `json:"thresholdRules"`
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &budget); err != nil {
		return normalBudget{}
	}
	normalized := normalBudget{
		Name:         strings.TrimPrefix(budget.Name, "billingAccounts/"),
		DisplayName:  budget.DisplayName,
		State:        budget.State,
	}
	if units, ok := budget.Amount["units"].(string); ok {
		normalized.Amount = units
	}
	if currency, ok := budget.Amount["currencyCode"].(string); ok {
		normalized.CurrencyCode = currency
	}
	for _, rule := range budget.ThresholdRules {
		normalized.ThresholdRules = append(normalized.ThresholdRules, normalBudgetThreshold{ThresholdPercent: rule.ThresholdPercent, SpendBasis: rule.SpendBasis})
	}
	return normalized
}

// ==================== HTTP handler ====================

func (s *Service) billingAccounts(w http.ResponseWriter, r *http.Request, idText string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listBillingAccounts(r.Context(), client)
	writeResult(w, map[string]interface{}{"billingAccounts": items}, err)
}

func (s *Service) billingInfo(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	item, err := s.getBillingInfo(r.Context(), client, projectID)
	writeResult(w, map[string]interface{}{"billingInfo": item}, err)
}

func (s *Service) budgets(w http.ResponseWriter, r *http.Request, idText, billingAccountID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	items, err := s.listBudgets(r.Context(), client, billingAccountID)
	writeResult(w, map[string]interface{}{"budgets": items}, err)
}