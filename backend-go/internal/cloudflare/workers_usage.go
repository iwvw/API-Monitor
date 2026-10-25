package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

// usageWindow 是一次用量统计的时间窗（绝对时刻，边界按站点时区算出）。
type usageWindow struct {
	since time.Time
	until time.Time
}

// 数据源标识，随响应返回，便于前端/排查时判断走的是哪条链路。
const (
	usageSourceByOwner     = "workersInvocationsByOwnerAndScriptGroups"
	usageSourceOverview    = "workersOverviewRequestsAdaptiveGroups"
	usageSourceInvocations = "workersInvocationsAdaptive"
)

// workerUsage 汇总 Workers 的请求用量（今日 / 本月）。
//
// 三个候选数据源按可靠性降序尝试，每级只查一个数据集（两个时间窗在同一次请求内）：
//  1. workersInvocationsByOwnerAndScriptGroups —— Groups 数据集，直接给 sum.requests，
//     不传 dimensions 时整个时间窗只返回一行，既不会截断也没有采样行数问题；
//  2. workersOverviewRequestsAdaptiveGroups —— 账户级概览，给 ABR 修正后的 count；
//  3. workersInvocationsAdaptive —— 官方教程用的原始数据集，逐条返回，可能命中 limit。
//
// 时间窗一律用 datetime_geq/datetime_leq 传绝对时刻：Cloudflare 的 date 维度按 UTC
// 归日，用站点时区算出的「今天」必须以时刻表达，否则跨时区会错一天。
//
// 注意 Pages Functions 的请求计入同一个 Workers 配额，本接口不含 Pages Functions。
func (s *Service) workerUsage(w http.ResponseWriter, r *http.Request, accountID string) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	auth, ok := s.authForAccount(w, r, accountID)
	if !ok {
		return
	}
	cfAccountID, err := s.cloudflareAccountID(r.Context(), auth)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	loc := s.siteLocation(r.Context())
	now := time.Now().In(loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)

	today, month, source, err := s.workerUsageTotals(
		r.Context(), auth, cfAccountID,
		usageWindow{since: dayStart, until: now},
		usageWindow{since: monthStart, until: now},
	)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"usage": map[string]interface{}{
			"today":      today,
			"month":      month,
			"todayStart": dayStart.Format(time.RFC3339),
			"monthStart": monthStart.Format(time.RFC3339),
			"timeZone":   loc.String(),
			"source":     source,
			"updatedAt":  now.Format(time.RFC3339),
		},
	})
}

// siteLocation 取站点时区；查询失败时按 timeutil 约定回退服务器本地时区。
func (s *Service) siteLocation(ctx context.Context) *time.Location {
	db, err := s.open(ctx)
	if err != nil {
		return timeutil.LocationFromName("")
	}
	defer db.Close()
	return timeutil.LocationFromSettings(ctx, db)
}

// workerUsageTotals 依次尝试数据源，返回 (今日, 本月, 数据源标识, 错误)。
// 每级失败（数据集不可见、字段缺失）都退到下一级，只有最后一级失败才报错。
func (s *Service) workerUsageTotals(ctx context.Context, auth map[string]string, cfAccountID string, today, month usageWindow) (map[string]interface{}, map[string]interface{}, string, error) {
	if pair, ok := s.queryGroupedUsage(ctx, auth, cfAccountID, today, month,
		"workersInvocationsByOwnerAndScriptGroups", `sum { requests cpuTimeUs }`); ok {
		return pair.today, pair.month, usageSourceByOwner, nil
	}
	if pair, ok := s.queryGroupedUsage(ctx, auth, cfAccountID, today, month,
		"workersOverviewRequestsAdaptiveGroups", "count sum { cpuTimeUs }"); ok {
		return pair.today, pair.month, usageSourceOverview, nil
	}

	todayTotals, monthTotals, err := s.workerInvocationTotals(ctx, auth, cfAccountID, today, month)
	if err != nil {
		return nil, nil, "", err
	}
	return todayTotals, monthTotals, usageSourceInvocations, nil
}

type groupedUsage struct {
	today map[string]interface{}
	month map[string]interface{}
}

// queryGroupedUsage 查一个 Groups 数据集的两个时间窗；不传 dimensions，
// 因此每个时间窗只返回一行汇总，不会因行数上限被截断。
func (s *Service) queryGroupedUsage(ctx context.Context, auth map[string]string, cfAccountID string, today, month usageWindow, dataset, fields string) (groupedUsage, bool) {
	query := fmt.Sprintf(`{
  viewer {
    accounts(filter: {accountTag: "%s"}) {
      today: %s(limit: 100, filter: { datetime_geq: "%s", datetime_leq: "%s" }) {
        %s
      }
      month: %s(limit: 100, filter: { datetime_geq: "%s", datetime_leq: "%s" }) {
        %s
      }
    }
  }
}`,
		graphqlAccountTag(cfAccountID),
		dataset, graphqlTime(today.since), graphqlTime(today.until), fields,
		dataset, graphqlTime(month.since), graphqlTime(month.until), fields,
	)
	payload, err := s.cfRequest(ctx, http.MethodPost, "/graphql", auth, map[string]interface{}{"query": query})
	if err != nil {
		return groupedUsage{}, false
	}
	if gqlErrors := arrayValue(payload["errors"]); len(gqlErrors) > 0 {
		return groupedUsage{}, false
	}
	account, ok := firstGraphQLAccount(payload)
	if !ok {
		// 账号下没有任何数据时 accounts 为空，视为「查询成功但无用量」。
		return groupedUsage{today: emptyUsageTotals(), month: emptyUsageTotals()}, true
	}
	return groupedUsage{
		today: sumGroupedRows(arrayValue(account["today"])),
		month: sumGroupedRows(arrayValue(account["month"])),
	}, true
}

// workerInvocationTotals 最后回退：加总 workersInvocationsAdaptive 的 sum.requests。
// 该数据集逐条返回，命中 limit 时置 truncated 提示数值可能偏低。
func (s *Service) workerInvocationTotals(ctx context.Context, auth map[string]string, cfAccountID string, today, month usageWindow) (map[string]interface{}, map[string]interface{}, error) {
	const rowLimit = 10000
	query := fmt.Sprintf(`{
  viewer {
    accounts(filter: {accountTag: "%s"}) {
      today: workersInvocationsAdaptive(limit: %d, filter: { datetime_geq: "%s", datetime_leq: "%s" }) {
        sum { requests errors subrequests cpuTimeUs }
      }
      month: workersInvocationsAdaptive(limit: %d, filter: { datetime_geq: "%s", datetime_leq: "%s" }) {
        sum { requests errors subrequests cpuTimeUs }
      }
    }
  }
}`,
		graphqlAccountTag(cfAccountID),
		rowLimit, graphqlTime(today.since), graphqlTime(today.until),
		rowLimit, graphqlTime(month.since), graphqlTime(month.until),
	)
	payload, err := s.cfRequest(ctx, http.MethodPost, "/graphql", auth, map[string]interface{}{"query": query})
	if err != nil {
		return nil, nil, err
	}
	if gqlErrors := arrayValue(payload["errors"]); len(gqlErrors) > 0 {
		first := objectValue(gqlErrors[0])
		return nil, nil, errors.New(stringValue(first["message"], "GraphQL error"))
	}
	account, ok := firstGraphQLAccount(payload)
	if !ok {
		return emptyUsageTotals(), emptyUsageTotals(), nil
	}
	todayRows := arrayValue(account["today"])
	monthRows := arrayValue(account["month"])
	return sumInvocationRows(todayRows, len(todayRows) >= rowLimit),
		sumInvocationRows(monthRows, len(monthRows) >= rowLimit), nil
}

func firstGraphQLAccount(payload map[string]interface{}) (map[string]interface{}, bool) {
	viewer := objectValue(objectValue(payload["data"])["viewer"])
	accounts := arrayValue(viewer["accounts"])
	if len(accounts) == 0 {
		return nil, false
	}
	return objectValue(accounts[0]), true
}

func emptyUsageTotals() map[string]interface{} {
	return map[string]interface{}{
		"requests":    int64(0),
		"errors":      int64(0),
		"subrequests": int64(0),
		"cpuTimeMs":   0.0,
		"truncated":   false,
	}
}

// sumGroupedRows 汇总 Groups 数据集的行：
//   - workersInvocationsByOwnerAndScriptGroups 给 sum.requests / sum.cpuTimeUs；
//   - workersOverviewRequestsAdaptiveGroups 给 count（ABR 修正后的请求数）+ sum.cpuTimeUs。
func sumGroupedRows(rows []interface{}) map[string]interface{} {
	totals := emptyUsageTotals()
	requests := int64(0)
	cpuTimeMs := 0.0
	for _, row := range rows {
		entry := objectValue(row)
		sum := objectValue(entry["sum"])
		if _, hasCount := entry["count"]; hasCount {
			requests += int64Value(entry["count"])
		} else {
			requests += int64Value(sum["requests"])
		}
		cpuTimeMs += float64(int64Value(sum["cpuTimeUs"])) / 1000
	}
	totals["requests"] = requests
	totals["cpuTimeMs"] = cpuTimeMs
	return totals
}

// sumInvocationRows 加总按行返回的调用记录。
func sumInvocationRows(rows []interface{}, truncated bool) map[string]interface{} {
	totals := emptyUsageTotals()
	requests := int64(0)
	errs := int64(0)
	subrequests := int64(0)
	cpuTimeMs := 0.0
	for _, row := range rows {
		sum := objectValue(objectValue(row)["sum"])
		requests += int64Value(sum["requests"])
		errs += int64Value(sum["errors"])
		subrequests += int64Value(sum["subrequests"])
		cpuTimeMs += float64(int64Value(sum["cpuTimeUs"])) / 1000
	}
	totals["requests"] = requests
	totals["errors"] = errs
	totals["subrequests"] = subrequests
	totals["cpuTimeMs"] = cpuTimeMs
	totals["truncated"] = truncated
	return totals
}

// graphqlAccountTag 去掉可能混入的引号/反斜杠/换行，避免拼接出非法 GraphQL 字符串。
func graphqlAccountTag(accountID string) string {
	replacer := strings.NewReplacer(`"`, "", `\`, "", "\n", "", "\r", "")
	return replacer.Replace(accountID)
}

func graphqlTime(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

func int64Value(value interface{}) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0
		}
		return parsed
	default:
		return 0
	}
}
