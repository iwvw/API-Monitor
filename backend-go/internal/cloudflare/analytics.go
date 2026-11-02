package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
)

func (s *Service) zoneAnalytics(w http.ResponseWriter, r *http.Request, accountID, zoneID string) {
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	timeRange := strings.TrimSpace(r.URL.Query().Get("timeRange"))
	if timeRange == "" {
		timeRange = "24h"
	}
	analytics, err := s.simpleAnalytics(r.Context(), auth, zoneID, timeRange)
	if err != nil {
		analytics = emptyAnalytics()
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "analytics": analytics, "timeRange": timeRange})
}

func (s *Service) simpleAnalytics(ctx context.Context, auth map[string]string, zoneID, timeRange string) (map[string]interface{}, error) {
	window := analyticsWindow(timeRange)
	groupName := "httpRequests1hGroups"
	dimensionField := "datetime"
	filter := fmt.Sprintf(`datetime_geq: "%s", datetime_leq: "%s"`, window["since"], window["until"])
	limit := 24
	if timeRange == "7d" || timeRange == "30d" {
		groupName = "httpRequests1dGroups"
		dimensionField = "date"
		filter = fmt.Sprintf(`date_geq: "%s", date_leq: "%s"`, window["sinceDate"], window["untilDate"])
		if timeRange == "7d" {
			limit = 7
		} else {
			limit = 30
		}
	}
	query := fmt.Sprintf(`{
 viewer {
  zones(filter: {zoneTag: "%s"}) {
   totals: %s(limit: 1, filter: { %s }) {
    sum { requests bytes cachedRequests cachedBytes threats pageViews }
    uniq { uniques }
   }
   series: %s(limit: %d, filter: { %s }, orderBy: [%s_ASC]) {
    dimensions { %s }
    sum { requests bytes cachedRequests cachedBytes threats pageViews }
    uniq { uniques }
   }
  }
 }
}`, zoneID, groupName, filter, groupName, limit, filter, dimensionField, dimensionField)
	payload, err := s.cfRequest(ctx, http.MethodPost, "/graphql", auth, map[string]interface{}{"query": query})
	if err != nil {
		return nil, err
	}
	if gqlErrors := arrayValue(payload["errors"]); len(gqlErrors) > 0 {
		first := objectValue(gqlErrors[0])
		return nil, errors.New(stringValue(first["message"], "GraphQL error"))
	}
	data := objectValue(payload["data"])
	viewer := objectValue(data["viewer"])
	zones := arrayValue(viewer["zones"])
	if len(zones) == 0 {
		return emptyAnalytics(), nil
	}
	zone := objectValue(zones[0])
	totals := map[string]interface{}{}
	if groups := arrayValue(zone["totals"]); len(groups) > 0 {
		totals = objectValue(groups[0])
	}
	totalSum := objectValue(totals["sum"])
	totalUniq := objectValue(totals["uniq"])
	totalRequests := numberValue(totalSum["requests"])
	totalCached := numberValue(totalSum["cachedRequests"])
	timeseries := []map[string]interface{}{}
	for _, item := range arrayValue(zone["series"]) {
		group := objectValue(item)
		sum := objectValue(group["sum"])
		uniq := objectValue(group["uniq"])
		dimensions := objectValue(group["dimensions"])
		requests := numberValue(sum["requests"])
		cachedRequests := numberValue(sum["cachedRequests"])
		timeseries = append(timeseries, map[string]interface{}{
			"datetime":       stringValue(dimensions["datetime"], stringValue(dimensions["date"], "")),
			"requests":       requests,
			"bandwidth":      numberValue(sum["bytes"]),
			"cachedRequests": cachedRequests,
			"cachedBytes":    numberValue(sum["cachedBytes"]),
			"threats":        numberValue(sum["threats"]),
			"pageViews":      numberValue(sum["pageViews"]),
			"uniques":        numberValue(uniq["uniques"]),
			"cacheHitRate":   cacheHitRate(requests, cachedRequests),
		})
	}
	return map[string]interface{}{
		"requests":       totalRequests,
		"bandwidth":      numberValue(totalSum["bytes"]),
		"cachedRequests": totalCached,
		"cachedBytes":    numberValue(totalSum["cachedBytes"]),
		"threats":        numberValue(totalSum["threats"]),
		"pageViews":      numberValue(totalSum["pageViews"]),
		"uniques":        numberValue(totalUniq["uniques"]),
		"cacheHitRate":   cacheHitRate(totalRequests, totalCached),
		"timeseries":     timeseries,
	}, nil
}

func emptyAnalytics() map[string]interface{} {
	return map[string]interface{}{
		"requests":       0,
		"bandwidth":      0,
		"cachedRequests": 0,
		"cachedBytes":    0,
		"threats":        0,
		"pageViews":      0,
		"uniques":        0,
		"cacheHitRate":   0,
		"timeseries":     []interface{}{},
	}
}

func analyticsWindow(timeRange string) map[string]string {
	until := time.Now().UTC()
	if timeRange == "7d" || timeRange == "30d" {
		days := 7
		if timeRange == "30d" {
			days = 30
		}
		since := until.Add(-time.Duration(days-1) * 24 * time.Hour)
		return map[string]string{
			"since":     since.Format(time.RFC3339),
			"until":     until.Format(time.RFC3339),
			"sinceDate": since.Format("2006-01-02"),
			"untilDate": until.Format("2006-01-02"),
		}
	}
	since := until.Add(-24 * time.Hour)
	return map[string]string{
		"since":     since.Format(time.RFC3339),
		"until":     until.Format(time.RFC3339),
		"sinceDate": since.Format("2006-01-02"),
		"untilDate": until.Format("2006-01-02"),
	}
}

func cacheHitRate(requests, cachedRequests float64) int {
	if requests <= 0 {
		return 0
	}
	return int((cachedRequests/requests)*100 + 0.5)
}

func numberValue(value interface{}) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err == nil {
			return parsed
		}
		return 0
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
		return 0
	default:
		return 0
	}
}
