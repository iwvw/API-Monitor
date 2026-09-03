package gcp

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

// normalModelUsage 模型用量汇总（按天序列 + 按模型分组）。
type normalModelUsage struct {
	Total   int64                  `json:"total"`
	Days    int64                  `json:"days"`
	Daily   []normalModelUsagePoint `json:"daily,omitempty"`
	ByModel []normalModelUsageGroup `json:"byModel,omitempty"`
}

type normalModelUsagePoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type normalModelUsageGroup struct {
	Model  string                   `json:"model"`
	Count  int64                    `json:"count"`
	Daily  []normalModelUsagePoint  `json:"daily,omitempty"`
}

// modelUsageMetric 返回调用次数时序指标。
func modelUsageFilter() string {
	return `metric.type="aiplatform.googleapis.com/publisher/online_serving/model_invocation_count" resource.type="aiplatform.googleapis.com/PublisherModel"`
}

// queryModelUsage 调 Cloud Monitoring timeSeries.list 聚合模型调用量。
// days 默认 30，按天对齐（ALIGN_SUM），按模型（model_user_id）分组。
// loc 为站点时区，用于把监控点的时间戳归入本地日期桶。
func (s *Service) queryModelUsage(ctx context.Context, c *client, projectID string, days int64, loc *time.Location) (normalModelUsage, error) {
	if days <= 0 || days > 90 {
		days = 30
	}
	now := time.Now().UTC()
	start := now.Add(-time.Duration(days) * 24 * time.Hour)
	query := url.Values{}
	query.Set("filter", modelUsageFilter())
	query.Set("interval.startTime", start.Format(time.RFC3339))
	query.Set("interval.endTime", now.Format(time.RFC3339))
	query.Set("aggregation.alignmentPeriod", "86400s")
	query.Set("aggregation.perSeriesAligner", "ALIGN_SUM")
	query.Set("aggregation.crossSeriesReducer", "REDUCE_SUM")
	query.Set("aggregation.groupByFields", "resource.labels.model_user_id")
	query.Set("view", "FULL")

	var raw struct {
		TimeSeries []struct {
			Resource struct {
				Labels map[string]string `json:"labels"`
			} `json:"resource"`
			Points []struct {
				Interval struct {
					StartTime string `json:"startTime"`
				} `json:"interval"`
				Value struct {
					Int64Value  *string  `json:"int64Value"` // GCP 以字符串返回 64 位整数
					DoubleValue *float64 `json:"doubleValue"`
				} `json:"value"`
			} `json:"points"`
		} `json:"timeSeries"`
	}
	if err := c.do(ctx, http.MethodGet, "monitoring", "projects/"+projectID+"/timeSeries", query, nil, &raw); err != nil {
		return normalModelUsage{}, err
	}

	result := normalModelUsage{Days: days}
	dailySum := map[string]int64{}
	var byModel []normalModelUsageGroup
	for _, ts := range raw.TimeSeries {
		model := strings.TrimSpace(ts.Resource.Labels["model_user_id"])
		if model == "" {
			model = "unknown"
		}
		group := normalModelUsageGroup{Model: model}
		groupDaily := map[string]int64{}
		for _, p := range ts.Points {
			var value int64
			if p.Value.Int64Value != nil {
				if parsed, err := strconv.ParseInt(*p.Value.Int64Value, 10, 64); err == nil {
					value = parsed
				}
			} else if p.Value.DoubleValue != nil {
				value = int64(*p.Value.DoubleValue)
			}
			date := shortDay(p.Interval.StartTime, loc)
			if date == "" {
				continue
			}
			group.Count += value
			groupDaily[date] += value
			dailySum[date] += value
		}
		for date, count := range groupDaily {
			group.Daily = append(group.Daily, normalModelUsagePoint{Date: date, Count: count})
		}
		sortByDate(group.Daily)
		byModel = append(byModel, group)
	}
	sortModels(byModel)
	for date, count := range dailySum {
		result.Daily = append(result.Daily, normalModelUsagePoint{Date: date, Count: count})
	}
	sortByDate(result.Daily)
	result.Total = 0
	for _, p := range result.Daily {
		result.Total += p.Count
	}
	result.ByModel = byModel
	return result, nil
}

// shortDay 把 RFC3339 时间戳按站点时区转换为 YYYY-MM-DD 日期桶。
func shortDay(value string, loc *time.Location) string {
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return ""
	}
	return instant.In(loc).Format("2006-01-02")
}

// sortByDate 按日期升序排。
func sortByDate(points []normalModelUsagePoint) {
	for i := 1; i < len(points); i++ {
		for j := i; j > 0 && points[j].Date < points[j-1].Date; j-- {
			points[j], points[j-1] = points[j-1], points[j]
		}
	}
}

// sortModels 按调用量降序排（总量大的在前）。
func sortModels(groups []normalModelUsageGroup) {
	for i := 1; i < len(groups); i++ {
		for j := i; j > 0 && groups[j].Count > groups[j-1].Count; j-- {
			groups[j], groups[j-1] = groups[j-1], groups[j]
		}
	}
}

// ==================== HTTP handler ====================

func (s *Service) modelUsage(w http.ResponseWriter, r *http.Request, idText, projectID string) {
	account, db, ok := s.accountForRequest(w, r, idText)
	if !ok {
		return
	}
	defer db.Close()
	client, ok := s.clientForAccount(r.Context(), w, account, scopeFull)
	if !ok {
		return
	}
	days := int64(30)
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil {
			days = parsed
		}
	}
	loc := timeutil.LocationFromSettings(r.Context(), db)
	item, err := s.queryModelUsage(r.Context(), client, projectID, days, loc)
	writeResult(w, map[string]interface{}{"modelUsage": item}, err)
}
