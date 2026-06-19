package system

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestHostMetricsShape(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_system_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
	}

	service := New(cfg)
	defer service.Shutdown()

	payload, err := service.hostMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if payload["hostname"] == "" {
		t.Fatal("expected hostname")
	}
	if payload["platform"] == "" || payload["platformLabel"] == "" {
		t.Fatalf("expected platform details: %#v", payload)
	}
	cpuPayload, ok := payload["cpu"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected cpu payload, got %#v", payload["cpu"])
	}
	if cpuPayload["cores"].(int) < 1 {
		t.Fatalf("expected at least one cpu core, got %#v", cpuPayload)
	}
	memoryPayload, ok := payload["memory"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected memory payload, got %#v", payload["memory"])
	}
	if _, ok := memoryPayload["total"]; !ok {
		t.Fatalf("expected memory total, got %#v", memoryPayload)
	}
	diskPayload, ok := payload["disk"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected disk payload, got %#v", payload["disk"])
	}
	if diskPayload["root"] == "" {
		t.Fatalf("expected disk root, got %#v", diskPayload)
	}
	processPayload, ok := payload["process"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected process payload, got %#v", payload["process"])
	}
	if _, ok := processPayload["memoryRss"]; !ok {
		t.Fatalf("expected process memoryRss, got %#v", processPayload)
	}
}

func TestAPICallStats(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_api_stats_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
	}

	service := New(cfg)
	defer service.Shutdown()

	// 记录若干请求
	service.RecordAPICall(http.MethodGet, "/api/totp/accounts") // Audit
	service.RecordAPICall(http.MethodGet, "/api/uptime/monitors") // Audit
	service.RecordAPICall(http.MethodPost, "/api/auth/login") // Ops
	service.RecordAPICall(http.MethodDelete, "/api/server/docker/delete") // Ops

	// 过滤的请求不应该被计入
	service.RecordAPICall(http.MethodGet, "/api/system/host-metrics")
	service.RecordAPICall(http.MethodGet, "/api/system/api-stats")

	stats, err := service.apiStats()
	if err != nil {
		t.Fatal(err)
	}

	totalPayload, ok := stats["total"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected total segment, got %#v", stats["total"])
	}

	if totalPayload["audit"].(int64) != 2 {
		t.Errorf("expected 2 audit requests, got %d", totalPayload["audit"])
	}
	if totalPayload["ops"].(int64) != 2 {
		t.Errorf("expected 2 ops requests, got %d", totalPayload["ops"])
	}
	if totalPayload["all"].(int64) != 4 {
		t.Errorf("expected 4 total requests, got %d", totalPayload["all"])
	}

	// 触发落盘并验证数据
	service.flushToDB()

	// 再次查询看是否依然正确合并
	stats2, err := service.apiStats()
	if err != nil {
		t.Fatal(err)
	}

	totalPayload2 := stats2["total"].(map[string]interface{})
	if totalPayload2["all"].(int64) != 4 {
		t.Errorf("expected 4 total requests after flush, got %d", totalPayload2["all"])
	}

	trend, ok := stats2["trend"].([]map[string]interface{})
	if !ok || len(trend) != 7 {
		t.Fatalf("expected 7 days of trend data, got %#v", stats2["trend"])
	}
	
	today := time.Now().Format("2006-01-02")
	foundToday := false
	for _, item := range trend {
		if item["bucket"].(string) == today {
			foundToday = true
			if item["total"].(int64) != 4 {
				t.Errorf("expected today total count to be 4, got %v", item["total"])
			}
		}
	}
	if !foundToday {
		t.Error("expected today to be present in trend data")
	}
}
