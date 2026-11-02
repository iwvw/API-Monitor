package gcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/timeutil"
)

// fakeAuthProvider 返回固定 token，供 monitoring mock 测试使用。
type fakeAuthProvider struct{}

func (fakeAuthProvider) AccessToken(ctx context.Context) (string, error) {
	return "fake-token", nil
}

// testClientFor 构造 client（不经 token 交换）。
func testClientFor() *client {
	return &client{
		http: &http.Client{Timeout: 5 * time.Second},
		auth: fakeAuthProvider{},
	}
}

func TestQueryModelUsageAggregatesByModelAndDate(t *testing.T) {
	body := `{
	  "timeSeries": [
	    {
	      "resource": { "type": "aiplatform.googleapis.com/PublisherModel", "labels": { "model_user_id": "gemini-3.8-flash" } },
	      "points": [
	        { "interval": { "startTime": "2026-09-01T00:00:00Z" }, "value": { "int64Value": "100" } },
	        { "interval": { "startTime": "2026-09-02T00:00:00Z" }, "value": { "int64Value": "200" } }
	      ]
	    },
	    {
	      "resource": { "type": "aiplatform.googleapis.com/PublisherModel", "labels": { "model_user_id": "gemini-2.5-flash" } },
	      "points": [
	        { "interval": { "startTime": "2026-09-01T00:00:00Z" }, "value": { "int64Value": "50" } },
	        { "interval": { "startTime": "2026-09-02T00:00:00Z" }, "value": { "doubleValue": 25.5 } }
	      ]
	    }
	  ]
	}`
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/p1/timeSeries", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("aggregation.alignmentPeriod"); got != "86400s" {
			t.Errorf("alignmentPeriod = %q, want 86400s", got)
		}
		if got := q.Get("aggregation.perSeriesAligner"); got != "ALIGN_SUM" {
			t.Errorf("perSeriesAligner = %q, want ALIGN_SUM", got)
		}
		if !strings.Contains(q.Get("filter"), "model_invocation_count") {
			t.Errorf("filter missing model_invocation_count: %q", q.Get("filter"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	old := baseURLs["monitoring"]
	baseURLs["monitoring"] = server.URL + "/"
	t.Cleanup(func() { baseURLs["monitoring"] = old })

	service := &Service{}
	c := testClientFor()
	result, err := service.queryModelUsage(context.Background(), c, "p1", 30, time.UTC)
	if err != nil {
		t.Fatalf("queryModelUsage error: %v", err)
	}
	if result.Total != 375 {
		t.Fatalf("total = %d, want 375", result.Total)
	}
	if result.Days != 30 {
		t.Fatalf("days = %d, want 30", result.Days)
	}
	if len(result.ByModel) != 2 {
		t.Fatalf("byModel count = %d, want 2", len(result.ByModel))
	}
	if result.ByModel[0].Model != "gemini-3.8-flash" || result.ByModel[0].Count != 300 {
		t.Fatalf("first model = %#v, want gemini-3.8-flash total 300", result.ByModel[0])
	}
	if result.ByModel[1].Count != 75 {
		t.Fatalf("second model total = %d, want 75", result.ByModel[1].Count)
	}
	if len(result.Daily) != 2 || result.Daily[0].Date != "2026-09-01" || result.Daily[0].Count != 150 || result.Daily[1].Count != 225 {
		t.Fatalf("daily = %#v, want [2026-09-01:150, 2026-09-02:225]", result.Daily)
	}
}

func TestQueryModelUsageEmptyAndDaysClamp(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/projects/p1/timeSeries", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	old := baseURLs["monitoring"]
	baseURLs["monitoring"] = server.URL + "/"
	t.Cleanup(func() { baseURLs["monitoring"] = old })

	service := &Service{}
	c := testClientFor()
	result, err := service.queryModelUsage(context.Background(), c, "p1", 0, time.UTC)
	if err != nil {
		t.Fatalf("queryModelUsage error: %v", err)
	}
	if result.Total != 0 || len(result.ByModel) != 0 || len(result.Daily) != 0 {
		t.Fatalf("expected empty usage, got %#v", result)
	}
	if result.Days != 30 {
		t.Fatalf("days should clamp to 30, got %d", result.Days)
	}
	result, err = service.queryModelUsage(context.Background(), c, "p1", 999, time.UTC)
	if err != nil {
		t.Fatalf("queryModelUsage error: %v", err)
	}
	if result.Days != 30 {
		t.Fatalf("days should clamp 999 to 30, got %d", result.Days)
	}
}

func TestShortDayAndSorts(t *testing.T) {
	loc := timeutil.LocationFromName("Asia/Shanghai")
	if shortDay("2026-09-03T12:00:00Z", loc) != "2026-09-03" {
		t.Fatalf("shortDay = %q", shortDay("2026-09-03T12:00:00Z", loc))
	}
	if shortDay("2026-09-02T23:30:00Z", loc) != "2026-09-03" {
		t.Fatalf("shortDay (UTC 跨日 按站点时区) = %q", shortDay("2026-09-02T23:30:00Z", loc))
	}
	pts := []normalModelUsagePoint{{Date: "2026-09-02", Count: 1}, {Date: "2026-09-01", Count: 2}}
	sortByDate(pts)
	if pts[0].Date != "2026-09-01" || pts[1].Date != "2026-09-02" {
		t.Fatalf("sortByDate = %#v", pts)
	}
	groups := []normalModelUsageGroup{{Model: "a", Count: 5}, {Model: "b", Count: 10}}
	sortModels(groups)
	if groups[0].Model != "b" {
		t.Fatalf("sortModels = %#v", groups)
	}
}
