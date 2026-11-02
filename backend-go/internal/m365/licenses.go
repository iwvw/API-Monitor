package m365

import (
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) listSKUs(w http.ResponseWriter, r *http.Request, idText string) {
	account, err := s.loadDecryptedAccount(r.Context(), idText)
	if err != nil {
		s.writeAccountError(w, err)
		return
	}
	result := map[string]interface{}{}
	if err := s.graphJSON(r.Context(), account, http.MethodGet, "/subscribedSkus", nil, nil, &result); err != nil {
		response.Error(w, http.StatusBadGateway, err.Error())
		return
	}

	items := objectArray(result["value"])
	subscriptions := map[string]interface{}{}
	payload := map[string]interface{}{"items": items}
	if err := s.graphJSON(r.Context(), account, http.MethodGet, "/directory/subscriptions", nil, nil, &subscriptions); err == nil {
		enrichSKUsWithSubscriptions(items, objectArray(subscriptions["value"]))
		payload["subscriptionLookupAvailable"] = true
	} else {
		payload["subscriptionLookupAvailable"] = false
	}
	response.OK(w, payload)
}

func normalizeLicenseAssignments(value interface{}) []map[string]interface{} {
	items := []map[string]interface{}{}
	for _, raw := range interfaceArray(value) {
		item := objectValue(raw)
		skuID := strings.TrimSpace(stringValue(item["skuId"], ""))
		if skuID == "" {
			continue
		}
		items = append(items, map[string]interface{}{
			"skuId":         skuID,
			"disabledPlans": stringArray(item["disabledPlans"]),
		})
	}
	return items
}

func enrichSKUsWithSubscriptions(skus []map[string]interface{}, subscriptions []map[string]interface{}) {
	subscriptionsBySKU := map[string][]map[string]interface{}{}
	subscriptionsByID := map[string]map[string]interface{}{}
	for _, subscription := range subscriptions {
		normalized := normalizeCompanySubscription(subscription)
		id := strings.TrimSpace(stringValue(normalized["id"], ""))
		skuID := strings.TrimSpace(stringValue(normalized["skuId"], ""))
		if id != "" {
			subscriptionsByID[id] = normalized
		}
		if skuID != "" {
			subscriptionsBySKU[skuID] = append(subscriptionsBySKU[skuID], normalized)
		}
	}

	for _, sku := range skus {
		matched := []map[string]interface{}{}
		seen := map[string]bool{}
		addSubscription := func(subscription map[string]interface{}) {
			key := strings.TrimSpace(stringValue(subscription["id"], ""))
			if key == "" {
				key = strings.TrimSpace(stringValue(subscription["skuId"], ""))
			}
			if key != "" && seen[key] {
				return
			}
			if key != "" {
				seen[key] = true
			}
			matched = append(matched, subscription)
		}

		for _, subscriptionID := range stringArray(sku["subscriptionIds"]) {
			if subscription, ok := subscriptionsByID[subscriptionID]; ok {
				addSubscription(subscription)
			}
		}
		skuID := strings.TrimSpace(stringValue(sku["skuId"], ""))
		for _, subscription := range subscriptionsBySKU[skuID] {
			addSubscription(subscription)
		}
		if len(matched) == 0 {
			continue
		}

		sku["subscriptions"] = matched
		for _, subscription := range matched {
			nextLifecycleDateTime := strings.TrimSpace(stringValue(subscription["nextLifecycleDateTime"], ""))
			if nextLifecycleDateTime == "" {
				continue
			}
			current := strings.TrimSpace(stringValue(sku["nextLifecycleDateTime"], ""))
			if current == "" || nextLifecycleDateTime < current {
				sku["nextLifecycleDateTime"] = nextLifecycleDateTime
				sku["subscriptionStatus"] = subscription["status"]
				sku["isTrial"] = subscription["isTrial"]
			}
		}
	}
}

func normalizeCompanySubscription(subscription map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"id":                    stringValue(subscription["id"], ""),
		"skuId":                 stringValue(subscription["skuId"], ""),
		"skuPartNumber":         stringValue(subscription["skuPartNumber"], ""),
		"status":                stringValue(subscription["status"], ""),
		"isTrial":               boolValue(subscription["isTrial"], false),
		"nextLifecycleDateTime": stringValue(subscription["nextLifecycleDateTime"], ""),
	}
}

func licenseAssignmentsFromIDs(ids []string) []map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(ids))
	for _, id := range ids {
		normalized := strings.TrimSpace(id)
		if normalized == "" {
			continue
		}
		items = append(items, map[string]interface{}{"skuId": normalized})
	}
	return items
}
