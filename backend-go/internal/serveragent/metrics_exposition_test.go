package serveragent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- Prometheus exposition（issue #79）----

func seedExpositionConfig(t *testing.T, service *Service, enabled, token string) {
	t.Helper()
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	for key, value := range map[string]string{
		"agent_metrics_enabled": enabled,
		"agent_metrics_token":   token,
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO system_config (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
}

func registerExpositionHost(service *Service, serverID string, metadata map[string]interface{}) {
	conn := service.registry.Register(serverID, &terminalCaptureSocket{})
	for key, value := range metadata {
		conn.SetMetadata(key, value)
	}
}

// 默认关闭：未显式开启时必须 403，不能因为「只是看看」就暴露全部主机指标。
func TestMetricsExpositionDisabledByDefault(t *testing.T) {
	service, _ := testService(t)
	registerExpositionHost(service, "srv-a", map[string]interface{}{"cpu_usage": 10.0})

	rec := httptest.NewRecorder()
	service.MetricsExposition(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// 开启后应输出 text exposition，且每条样本带 server_id 标签。
func TestMetricsExpositionOutputsPrometheusText(t *testing.T) {
	service, _ := testService(t)
	seedExpositionConfig(t, service, "1", "")
	registerExpositionHost(service, "srv-a", map[string]interface{}{
		"cpu_usage":         12.5,
		"mem_usage_percent": 40.0,
		"disk_usage":        55.0,
		"tcp_conn_count":    123.0,
		"process_count":     42.0,
	})

	rec := httptest.NewRecorder()
	service.MetricsExposition(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Fatalf("Content-Type = %q", ct)
	}
	body := rec.Body.String()

	for _, want := range []string{
		"# TYPE api_monitor_up gauge",
		`api_monitor_up{server_id="srv-a"} 1`,
		`api_monitor_cpu_usage_percent{server_id="srv-a"} 12.5`,
		`api_monitor_processes{server_id="srv-a"} 42`,
		`api_monitor_tcp_connections{server_id="srv-a"} 123`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("输出缺少 %q\n完整输出:\n%s", want, body)
		}
	}
	// 必须带 TYPE 声明，否则 Prometheus 会按 untyped 处理
	if !strings.Contains(body, "# TYPE api_monitor_cpu_usage_percent gauge") {
		t.Fatalf("缺少 cpu_usage_percent 的 TYPE 声明:\n%s", body)
	}
}

// 配置了 token 后，无/错 token 一律拒绝；正确 token 放行（常量时间比较）。
func TestMetricsExpositionRequiresTokenWhenConfigured(t *testing.T) {
	service, _ := testService(t)
	seedExpositionConfig(t, service, "1", "s3cret")
	registerExpositionHost(service, "srv-a", map[string]interface{}{"cpu_usage": 1.0})

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"no header", "", http.StatusForbidden},
		{"wrong token", "Bearer nope", http.StatusForbidden},
		{"wrong scheme", "Basic s3cret", http.StatusForbidden},
		{"correct token", "Bearer s3cret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			service.MetricsExposition(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

// Agent 自监控字段要作为独立指标导出（只有新版 Agent 才上报）。
func TestMetricsExpositionExportsAgentSelfMetrics(t *testing.T) {
	service, _ := testService(t)
	seedExpositionConfig(t, service, "1", "")
	registerExpositionHost(service, "srv-self", map[string]interface{}{
		"cpu_usage": 5.0,
		"self_metrics": map[string]interface{}{
			"rss_bytes":        float64(8 * 1024 * 1024),
			"cpu_percent":      0.5,
			"collect_state_us": float64(2500),
			"dropped_low":      float64(3),
			"reconnect_count":  float64(2),
		},
	})

	rec := httptest.NewRecorder()
	service.MetricsExposition(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	for _, want := range []string{
		`api_monitor_agent_rss_bytes{server_id="srv-self"} 8388608`,
		`api_monitor_agent_cpu_percent{server_id="srv-self"} 0.5`,
		// 微秒 → 秒
		`api_monitor_agent_collect_seconds{server_id="srv-self"} 0.0025`,
		`api_monitor_agent_dropped_low{server_id="srv-self"} 3`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("缺少 %q\n完整输出:\n%s", want, body)
		}
	}
}

// 老版本 Agent 不上报 self_metrics 时，不能凭空生成 0 值样本
// （否则外部监控会以为 Agent 内存占用是 0）。
func TestMetricsExpositionSkipsSelfMetricsWhenAbsent(t *testing.T) {
	service, _ := testService(t)
	seedExpositionConfig(t, service, "1", "")
	registerExpositionHost(service, "srv-old", map[string]interface{}{"cpu_usage": 3.0})

	rec := httptest.NewRecorder()
	service.MetricsExposition(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()

	if strings.Contains(body, "api_monitor_agent_rss_bytes") {
		t.Fatalf("缺少 self_metrics 时不应导出 agent_* 指标:\n%s", body)
	}
	if !strings.Contains(body, `api_monitor_cpu_usage_percent{server_id="srv-old"} 3`) {
		t.Fatalf("主机指标仍应导出:\n%s", body)
	}
}

// 只允许 GET。
func TestMetricsExpositionRejectsNonGet(t *testing.T) {
	service, _ := testService(t)
	rec := httptest.NewRecorder()
	service.MetricsExposition(rec, httptest.NewRequest(http.MethodPost, "/metrics", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// 格式化必须避免科学计数法（Prometheus 规范只接受十进制）。
func TestFormatPromFloat(t *testing.T) {
	cases := map[float64]string{
		0:           "0",
		12.5:        "12.5",
		1000000:     "1000000",
		0.0025:      "0.0025",
		12345678901: "12345678901",
	}
	for input, want := range cases {
		if got := formatPromFloat(input); got != want {
			t.Fatalf("formatPromFloat(%v) = %q, want %q", input, got, want)
		}
	}
	// 极小正数归零，避免出现 1e-09 这种非规范写法
	if got := formatPromFloat(1e-9); got != "0" {
		t.Fatalf("极小值应为 0, got %q", got)
	}
}
