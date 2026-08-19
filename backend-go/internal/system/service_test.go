package system

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
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
	if cpuPayload["physicalCores"].(int) < 1 {
		t.Fatalf("expected at least one physical cpu core, got %#v", cpuPayload)
	}
	if cpuPayload["logicalCores"].(int) < cpuPayload["physicalCores"].(int) {
		t.Fatalf("expected logical cores to be >= physical cores, got %#v", cpuPayload)
	}
	if cpuPayload["threads"].(int) != cpuPayload["logicalCores"].(int) {
		t.Fatalf("expected threads to match logical cores, got %#v", cpuPayload)
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

func TestAIAccessOverviewAndMCP(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_ai_access_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
		Version: "test",
	}

	service := New(cfg)
	defer service.Shutdown()

	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/system/ai-access", nil)
	overview, err := service.aiAccessOverview(req)
	if err != nil {
		t.Fatal(err)
	}
	agentKey := overview["agentKey"].(map[string]interface{})["value"].(string)
	if agentKey == "" {
		t.Fatal("expected agent key")
	}

	guide, _ := overview["guide"].(string)
	if !strings.Contains(guide, "/api/ai/mcp") || !strings.Contains(guide, agentKey) || !strings.Contains(guide, "list_apis") {
		t.Fatalf("expected AI access guide to include endpoint, key and tools, got: %s", guide)
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "http://example.test/api/ai/manifest", nil)
	if _, err := service.aiManifest(unauthorized); err == nil {
		t.Fatal("expected unauthorized manifest request to fail")
	}

	authorized := httptest.NewRequest(http.MethodGet, "http://example.test/api/ai/manifest", nil)
	authorized.Header.Set("Authorization", "Bearer "+agentKey)
	manifestPayload, err := service.aiManifest(authorized)
	if err != nil {
		t.Fatal(err)
	}
	if manifestPayload["name"] != "API Monitor" {
		t.Fatalf("unexpected manifest: %#v", manifestPayload)
	}

	body, _ := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	})
	mcpReq := httptest.NewRequest(http.MethodPost, "http://example.test/api/ai/mcp", bytes.NewReader(body))
	mcpReq.Header.Set("Authorization", "Bearer "+agentKey)
	mcpPayload, status, err := service.handleMCP(mcpReq)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}
	result := mcpPayload.(map[string]interface{})["result"].(map[string]interface{})
	if len(result["tools"].([]map[string]interface{})) == 0 {
		t.Fatal("expected MCP tools")
	}
}

func TestAIAuditPage(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_ai_audit_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
		Version: "test",
	}

	service := New(cfg)
	defer service.Shutdown()

	overviewReq := httptest.NewRequest(http.MethodGet, "http://example.test/api/system/ai-access", nil)
	if _, err := service.aiAccessOverview(overviewReq); err != nil {
		t.Fatal(err)
	}

	rotateReq := httptest.NewRequest(http.MethodPost, "http://example.test/api/system/ai-access/key/rotate", nil)
	if _, err := service.rotateAIAgentKey(rotateReq); err != nil {
		t.Fatal(err)
	}
	if _, err := service.rotateAIAgentKey(rotateReq); err != nil {
		t.Fatal(err)
	}

	pageReq := httptest.NewRequest(http.MethodGet, "http://example.test/api/system/ai-access/audit?days=7&page=1&pageSize=10", nil)
	payload, err := service.listAIAuditPage(pageReq)
	if err != nil {
		t.Fatal(err)
	}
	total, ok := payload["total"].(int)
	if !ok || total < 2 {
		t.Fatalf("expected total >= 2, got %#v", payload["total"])
	}
	records, ok := payload["records"].([]aiAuditEntry)
	if !ok || len(records) == 0 {
		t.Fatalf("expected non-empty records, got %#v", payload["records"])
	}
	if records[0].Action != "rotate_key" {
		t.Fatalf("unexpected newest audit action: %#v", records[0])
	}
}
func TestAIMCPToolSchemasAreValidJSONSchema(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var walk func(t *testing.T, path string, node interface{}, depth int)
	walk = func(t *testing.T, path string, node interface{}, depth int) {
		if depth > 24 {
			t.Fatalf("schema too deep at %s", path)
		}
		if node == nil {
			return
		}
		if boolVal, ok := node.(bool); ok {
			_ = boolVal
			return
		}
		obj, ok := node.(map[string]interface{})
		if !ok {
			t.Fatalf("%s: schema node must be object or boolean, got %T", path, node)
		}
		typeVal, _ := obj["type"].(string)
		if typeVal == "" {
			t.Fatalf("%s: property missing type: %#v", path, obj)
		}
		switch typeVal {
		case "object":
			props, _ := obj["properties"].(map[string]interface{})
			if required, ok := obj["required"].([]string); ok {
				for _, r := range required {
					if _, exists := props[r]; !exists {
						t.Fatalf("%s: required %q not in properties", path, r)
					}
				}
			}
			for propName, propNode := range props {
				walk(t, path+"."+propName, propNode, depth+1)
			}
			if ap, ok := obj["additionalProperties"].(map[string]interface{}); ok {
				walk(t, path+".additionalProperties", ap, depth+1)
			}
		case "array":
			if items, ok := obj["items"].(map[string]interface{}); ok {
				walk(t, path+".items", items, depth+1)
			}
		}
	}

	// 1) 校验 MCP 工具 schema
	for _, tool := range service.aiTools() {
		name, _ := tool["name"].(string)
		raw, ok := tool["inputSchema"].(map[string]interface{})
		if !ok {
			t.Fatalf("tool %s missing inputSchema", name)
		}
		walk(t, "tool."+name, raw, 0)
	}

	// 2) 校验 get_route 返回的全部契约 schema（apiDocs 注入的 requestSchema）
	checked := 0
	for _, route := range service.apiDocs()["routes"].([]apiDocRoute) {
		if route.RequestSchema != nil {
			walk(t, "requestSchema."+route.Prefix, route.RequestSchema, 0)
			checked++
		}
	}
	// 3) 校验契约注册表本身
	for prefix, raw := range routeRequestContracts {
		if m, ok := raw.(map[string]interface{}); ok {
			walk(t, "contract."+prefix, m, 0)
		}
	}
	t.Logf("validated request schemas for %d routes", checked)
}

func TestAIMCPSpec(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_mcp_spec_test_*")
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

	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/system/ai-access", nil)
	overview, err := service.aiAccessOverview(req)
	if err != nil {
		t.Fatal(err)
	}
	agentKey := overview["agentKey"].(map[string]interface{})["value"].(string)

	mcpRequest := func(method string, params map[string]interface{}, id interface{}) (interface{}, int, error) {
		payload := map[string]interface{}{"jsonrpc": "2.0", "method": method}
		if id != nil {
			payload["id"] = id
		}
		if params != nil {
			payload["params"] = params
		}
		body, _ := json.Marshal(payload)
		r := httptest.NewRequest(http.MethodPost, "http://example.test/api/ai/mcp", bytes.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+agentKey)
		return service.handleMCP(r)
	}

	pingResult, status, err := mcpRequest("ping", nil, 1)
	if err != nil {
		t.Fatalf("ping failed: %v", err)
	}
	if status != http.StatusOK || pingResult == nil {
		t.Fatalf("ping status = %d, result = %#v", status, pingResult)
	}

	notifResult, notifStatus, err := mcpRequest("notifications/initialized", nil, nil)
	if err != nil {
		t.Fatalf("notification failed: %v", err)
	}
	if notifStatus != http.StatusAccepted {
		t.Fatalf("notification status = %d, want 202", notifStatus)
	}
	if notifResult != nil {
		t.Fatalf("notification must have no payload, got %#v", notifResult)
	}

	resList, _, err := mcpRequest("resources/list", nil, 2)
	if err != nil {
		t.Fatalf("resources/list failed: %v", err)
	}
	resources := resList.(map[string]interface{})["result"].(map[string]interface{})["resources"].([]map[string]interface{})
	if len(resources) != 4 {
		t.Fatalf("expected 4 resources, got %d", len(resources))
	}

	readResult, _, err := mcpRequest("resources/read", map[string]interface{}{"uri": "api-monitor://routes"}, 3)
	if err != nil {
		t.Fatalf("resources/read routes failed: %v", err)
	}
	contents := readResult.(map[string]interface{})["result"].(map[string]interface{})["contents"].([]map[string]interface{})
	if len(contents) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(contents))
	}

	if unknownRes, _, err := mcpRequest("resources/read", map[string]interface{}{"uri": "unknown://uri"}, 4); err != nil {
		t.Fatalf("resources/read unknown returned transport error: %v", err)
	} else if _, isError := unknownRes.(map[string]interface{})["error"]; !isError {
		t.Fatal("expected unknown resource to produce a JSON-RPC error")
	}
}

func TestRouteContractCoverage(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	routes := service.apiDocs()["routes"].([]apiDocRoute)
	uncovered := []string{}
	covered := 0
	familyPrefix := 0
	for _, route := range routes {
		isWrite := false
		for _, method := range route.Methods {
			if method == "POST" || method == "PUT" || method == "PATCH" {
				isWrite = true
				break
			}
		}
		if !isWrite {
			continue
		}
		if route.MatchMode == "prefix" {
			// 家族前缀路由只是索引条目，不是真实写接口，不参与契约覆盖统计。
			familyPrefix++
			continue
		}
		if route.RequestSchema != nil || route.RequestBody != nil {
			covered++
		} else {
			uncovered = append(uncovered, route.Prefix)
		}
	}
	t.Logf("write routes covered: %d, uncovered: %d, family-prefix skipped: %d", covered, len(uncovered), familyPrefix)
	for _, prefix := range uncovered {
		t.Logf("  uncovered: %s", prefix)
	}
	if len(uncovered) > 0 {
		t.Fatalf("仍有 %d 个写接口缺少请求契约，请在 route_contracts.go 中登记：%v", len(uncovered), uncovered)
	}
}

func TestAIProgressiveDisclosure(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_disclosure_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.Config{DataDir: tempDir, DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	overviewReq := httptest.NewRequest(http.MethodGet, "http://example.test/api/system/ai-access", nil)
	overview, err := service.aiAccessOverview(overviewReq)
	if err != nil {
		t.Fatal(err)
	}
	agentKey := overview["agentKey"].(map[string]interface{})["value"].(string)

	callTool := func(name string, args map[string]interface{}) (interface{}, error) {
		body, _ := json.Marshal(map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]interface{}{"name": name, "arguments": args}})
		req := httptest.NewRequest(http.MethodPost, "http://example.test/api/ai/mcp", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+agentKey)
		result, status, err := service.handleMCP(req)
		if err != nil || status != http.StatusOK {
			return nil, err
		}
		return result, nil
	}

	// list_apis 紧凑 + 过滤
	raw, err := callTool("list_apis", map[string]interface{}{"group": "PaaS"})
	if err != nil {
		t.Fatal(err)
	}
	catalog := raw.(map[string]interface{})["result"].(map[string]interface{})["structuredContent"].(map[string]interface{})
	routes := catalog["routes"].([]map[string]interface{})
	if len(routes) == 0 {
		t.Fatal("expected PaaS routes in compact catalog")
	}
	first := routes[0]
	for _, key := range []string{"path", "methods", "group", "auth", "desc", "hasBody"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("compact catalog missing key %s: %#v", key, first)
		}
	}
	if _, ok := first["requestSchema"]; ok {
		t.Fatal("compact catalog should not include requestSchema")
	}

	// get_route 返回完整契约（含 requestSchema）
	raw, err = callTool("get_route", map[string]interface{}{"path": "/api/flyio/apps/apimnt/update-image"})
	if err != nil {
		t.Fatal(err)
	}
	contract := raw.(map[string]interface{})["result"].(map[string]interface{})["structuredContent"].(map[string]interface{})
	if contract["matchedPath"] != "/api/flyio/apps/apimnt/update-image" {
		t.Fatalf("unexpected matchedPath: %#v", contract["matchedPath"])
	}
	schema := contract["requestSchema"].(map[string]interface{})
	required, _ := schema["required"].([]string)
	found := map[string]bool{}
	for _, r := range required {
		found[r] = true
	}
	if !found["accountId"] || !found["image"] {
		t.Fatalf("expected required accountId+image in flyio update-image schema: %#v", schema)
	}
}

func TestRouteGroupsFollowSidebarModules(t *testing.T) {
	cases := []struct {
		prefix string
		group  string
	}{
		{"/api/openai/endpoints", "模型网关"},
		{"/v1/chat/completions", "模型网关"},
		{"/api/subscription/accounts", "订阅分发"},
		{"/sub/{token}", "订阅分发"},
		{"/api/cloudflare/accounts", "Cloudflare"},
		{"/api/aliyun/accounts", "阿里云"},
		{"/api/tencent/accounts", "腾讯云"},
		{"/api/oracle/accounts", "甲骨文云"},
		{"/api/m365/accounts", "Microsoft 365"},
		{"/api/github/tokens", "GitHub"},
		{"/api/server/accounts", "主机实例"},
		{"/api/onepanel/config", "主机实例"},
		{"/api/koyeb/accounts", "PaaS"},
		{"/api/flyio/accounts", "PaaS"},
		{"/api/scheduler/tasks", "定时任务"},
		{"/api/cron/tasks", "定时任务"},
		{"/api/uptime/monitors", "可用性监测"},
		{"/api/filebox/files", "文件柜"},
		{"/api/drawio/documents", "图编辑器"},
		{"/api/prompts/entries", "提示词库"},
		{"/api/totp/accounts", "双因子认证"},
		{"/api/notification/rules", "通知中心"},
		{"/api/auth/login", "认证"},
		{"/api/system/logs/stream", "系统日志"},
		{"/api/settings/sys-logs", "系统日志"},
		{"/api/system/api-docs", "API 接口"},
		{"/api/openapi.json", "API 接口"},
		{"/api/api-keys", "API 接口"},
		{"/api/ai/manifest", "API 接口"},
		{"/api/ai/mcp", "API 接口"},
		{"/api/ai-access", "API 接口"},
		{"/api/settings/site-brand", "系统设置"},
		{"/api/backup/run", "系统设置"},
		{"/health", "仪表盘"},
		{"/api/system/host-metrics", "仪表盘"},
	}
	for _, tc := range cases {
		got := routeGroup(manifest.Route{Prefix: tc.prefix})
		if got != tc.group {
			t.Errorf("routeGroup(%s) = %q, want %q", tc.prefix, got, tc.group)
		}
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
	service.RecordAPICall(http.MethodGet, "/api/totp/accounts")           // Audit
	service.RecordAPICall(http.MethodGet, "/api/uptime/monitors")         // Audit
	service.RecordAPICall(http.MethodPost, "/api/auth/login")             // Ops
	service.RecordAPICall(http.MethodDelete, "/api/server/docker/delete") // Ops

	// 过滤的请求不应该被计入
	service.RecordAPICall(http.MethodGet, "/api/system/host-metrics")
	service.RecordAPICall(http.MethodGet, "/api/system/api-stats")

	stats, err := service.apiStats(7)
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
	stats2, err := service.apiStats(7)
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

	// 14 天窗口应返回 14 个趋势桶。
	stats14, err := service.apiStats(14)
	if err != nil {
		t.Fatal(err)
	}
	trend14, ok := stats14["trend"].([]map[string]interface{})
	if !ok || len(trend14) != 14 {
		t.Fatalf("expected 14 days of trend data, got %#v", stats14["trend"])
	}

	// 每个趋势桶应带 tokens / traffic 字段（无数据时回落为 0）。
	for _, item := range trend {
		if _, hasTokens := item["tokens"]; !hasTokens {
			t.Errorf("expected tokens key on trend bucket %#v", item)
		}
		if _, hasTraffic := item["traffic"]; !hasTraffic {
			t.Errorf("expected traffic key on trend bucket %#v", item)
		}
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

func TestApiStatsCacheWithinTTL(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_apistats_cache_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	service := New(config.Config{DataDir: tempDir, DBName: "data.db"})
	defer service.Shutdown()

	service.RecordAPICall(http.MethodGet, "/api/totp/accounts")
	service.RecordAPICall(http.MethodPost, "/api/auth/login")

	first, err := service.apiStats(7)
	if err != nil {
		t.Fatal(err)
	}
	firstTotal := first["total"].(map[string]interface{})
	if firstTotal["all"].(int64) != 2 {
		t.Fatalf("expected 2 recorded calls, got %v", firstTotal["all"])
	}

	// TTL 内第二次查询应直接命中缓存：返回同一份 payload（指针恒等）。
	second, err := service.apiStats(7)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.ValueOf(second).Pointer() != reflect.ValueOf(first).Pointer() {
		t.Error("expected apiStats(7) within TTL to serve the cached payload")
	}

	// 落盘后（内存计数已清空），不同 days 窗口绕过缓存重新计算，结果仍应合并正确。
	service.flushToDB()
	other, err := service.apiStats(14)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.ValueOf(other).Pointer() == reflect.ValueOf(first).Pointer() {
		t.Error("expected apiStats(14) to bypass the cached payload")
	}
	otherTotal := other["total"].(map[string]interface{})
	if otherTotal["all"].(int64) != 2 {
		t.Errorf("expected recomputed payload for different days, got %v", otherTotal["all"])
	}
	trend, ok := other["trend"].([]map[string]interface{})
	if !ok || len(trend) != 14 {
		t.Fatalf("expected 14 trend buckets, got %#v", other["trend"])
	}

	// 超过 TTL 后应重新计算而非返回旧缓存。
	service.apiStatsMu.Lock()
	service.apiStatsCacheAt = time.Now().Add(-2 * apiStatsCacheTTL)
	service.apiStatsMu.Unlock()
	fresh, err := service.apiStats(7)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.ValueOf(fresh).Pointer() == reflect.ValueOf(first).Pointer() {
		t.Error("expected apiStats(7) after TTL expiry to recompute")
	}
	freshTotal := fresh["total"].(map[string]interface{})
	if freshTotal["all"].(int64) != 2 {
		t.Errorf("expected recomputed payload after TTL expiry, got %v", freshTotal["all"])
	}
}

func TestAPIDocsIncludeSupplementalRouteMetadata(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_docs_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	service := New(config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
		Version: "test",
	})
	defer service.Shutdown()

	payload := service.apiDocs()
	routes, ok := payload["routes"].([]apiDocRoute)
	if !ok {
		t.Fatalf("expected []apiDocRoute, got %#v", payload["routes"])
	}

	var loginRoute *apiDocRoute
	for i := range routes {
		if routes[i].Prefix == "/api/auth/login" {
			loginRoute = &routes[i]
			break
		}
	}
	if loginRoute == nil {
		t.Fatal("expected /api/auth/login in api docs")
	}
	if len(loginRoute.Methods) != 1 || loginRoute.Methods[0] != http.MethodPost {
		t.Fatalf("unexpected methods: %#v", loginRoute.Methods)
	}
	if loginRoute.RequestBody == nil {
		t.Fatal("expected login request example")
	}
	if len(loginRoute.Headers) != 0 {
		t.Fatalf("login route should not require auth headers, got %#v", loginRoute.Headers)
	}
}

func TestOpenAPIDocumentIncludesParametersAndSecurity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_openapi_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	service := New(config.Config{
		DataDir: tempDir,
		DBName:  "data.db",
		Version: "test",
	})
	defer service.Shutdown()

	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/openapi.json", nil)
	doc := service.openapiDocument(req)

	components := doc["components"].(map[string]interface{})
	securitySchemes := components["securitySchemes"].(map[string]interface{})
	if _, ok := securitySchemes["sessionCookie"]; !ok {
		t.Fatalf("expected sessionCookie security scheme, got %#v", securitySchemes)
	}

	paths := doc["paths"].(map[string]interface{})
	settingsPath := paths["/api/settings"].(map[string]interface{})
	getOp := settingsPath["get"].(map[string]interface{})
	security := getOp["security"].([]map[string][]string)
	if len(security) == 0 {
		t.Fatal("expected /api/settings GET security")
	}

	aliyunPath := paths["/api/aliyun/accounts/{id}/domains/{domainName}/records"].(map[string]interface{})
	getAliyun := aliyunPath["get"].(map[string]interface{})
	parameters := getAliyun["parameters"].([]map[string]interface{})
	if len(parameters) < 2 {
		t.Fatalf("expected path parameters for aliyun records route, got %#v", parameters)
	}

	loginPath := paths["/api/auth/login"].(map[string]interface{})
	postLogin := loginPath["post"].(map[string]interface{})
	requestBody := postLogin["requestBody"].(map[string]interface{})
	content := requestBody["content"].(map[string]interface{})
	if _, ok := content["application/json"]; !ok {
		t.Fatalf("expected application/json request body, got %#v", content)
	}
	if !strings.Contains(postLogin["description"].(string), "管理员密码登录") && !strings.Contains(postLogin["description"].(string), "登录") {
		t.Fatalf("expected enriched description, got %q", postLogin["description"])
	}
}

func TestAIFindAPISemanticRecall(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	cases := []struct {
		label       string
		intent      string
		wantPathSub string // 期望命中的路由路径片段
		wantAny     bool   // 只要非空即可（低置信度兜底）
	}{
		{"dns 解析记录", "帮我列出所有 DNS 解析记录", "/api/cloudflare/accounts/{accountId}/zones/{zoneId}/records", false},
		{"更新 flyio 镜像", "给 flyio 应用更新镜像", "/api/flyio/apps/{appName}/update-image", false},
		{"主机监控状态", "查看主机监控状态", "/api/server/monitor/status", false},
		{"定时任务", "创建定时任务", "/api/scheduler/tasks", false},
		{"提示词库", "提示词库怎么用", "/api/prompts", false},
		{"阿里云 DNS", "查阿里云的解析记录", "/api/aliyun/accounts/{id}/domains/{domainName}/records", false},
		{"腾讯云 DNS", "看腾讯云的 dns 记录", "/api/tencent/accounts/{id}/domains/{domain}/records", false},
		{"m365", "微软 365 用户列表", "/api/m365", false},
		{"oracle 云", "oracle oci 实例", "/api/oracle", false},
		{"代理节点", "看看梯子节点连通情况", "/api/server/agent/proxy", false},
		{"郎才", "郎才不存在的意图随便写一个", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			result, err := service.aiFindAPIs(map[string]interface{}{"intent": tc.intent})
			if err != nil {
				t.Fatalf("aiFindAPIs err: %v", err)
			}
			payload := result.(map[string]interface{})
			routes, ok := payload["routes"].([]map[string]interface{})
			if !ok {
				t.Fatalf("routes not []map[string]interface{}: %#v", payload["routes"])
			}
			if tc.wantAny {
				suggestions, hasS := payload["suggestions"].([]string)
				if !hasS || len(suggestions) == 0 {
					t.Fatalf("intent %q missing suggestions for low-confidence case: %#v", tc.intent, payload["suggestions"])
				}
				return
			}
			if len(routes) == 0 {
				t.Fatalf("intent %q returned no routes", tc.intent)
			}
			found := false
			for _, r := range routes {
				path, _ := r["path"].(string)
				if strings.Contains(path, tc.wantPathSub) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("intent %q top%v results missing %q: %#v", tc.intent, len(routes), tc.wantPathSub, routes)
			}
			conf, _ := payload["confidence"].(string)
			if conf == "" {
				t.Fatalf("intent %q missing confidence", tc.intent)
			}
			for _, r := range routes {
				if _, hasScore := r["score"]; !hasScore {
					t.Fatalf("intent %q route missing score: %#v", tc.intent, r["path"])
				}
				if reasons, hasReason := r["matchReason"].([]map[string]interface{}); hasReason {
					for _, reason := range reasons {
						if _, hasTerm := reason["term"]; !hasTerm {
							t.Fatalf("intent %q matchReason missing term: %#v", tc.intent, reason)
						}
					}
				} else {
					t.Fatalf("intent %q route missing matchReason: %#v", tc.intent, r["path"])
				}
			}
		})
	}
}

func TestAIRouteCatalogPagination(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	result, err := service.aiRouteCatalog(map[string]interface{}{"limit": float64(20), "offset": float64(0)})
	if err != nil {
		t.Fatalf("aiRouteCatalog err: %v", err)
	}
	payload := result.(map[string]interface{})
	if returned, _ := payload["returned"].(int); returned != 20 {
		t.Fatalf("expected 20 returned, got %d", returned)
	}
	if total, _ := payload["count"].(int); total <= 20 {
		t.Fatalf("expected total > 20, got %d", total)
	}

	page2, err := service.aiRouteCatalog(map[string]interface{}{"limit": float64(20), "offset": float64(20)})
	if err != nil {
		t.Fatalf("aiRouteCatalog page2 err: %v", err)
	}
	p2 := page2.(map[string]interface{})
	r1 := payload["routes"].([]map[string]interface{})
	r2 := p2["routes"].([]map[string]interface{})
	if len(r2) == 0 || r1[0]["path"] == r2[0]["path"] {
		t.Fatalf("expected different page content, got same first path %v", r1[0]["path"])
	}

	limited, err := service.aiRouteCatalog(map[string]interface{}{"limit": float64(500)})
	if err != nil {
		t.Fatalf("aiRouteCatalog clamp err: %v", err)
	}
	if returned, _ := limited.(map[string]interface{})["returned"].(int); returned > 100 {
		t.Fatalf("expected limit clamped to 100, got %d", returned)
	}
}

func TestAIRunBatch(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	batchReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "http://example.test/api/ai/mcp", nil)
		r.Header.Set("Authorization", "Bearer test")
		r.RemoteAddr = "127.0.0.1:1234"
		return r
	}

	// 注入只读 caller：/health 成功，/api/forbidden 失败，其余返回 404 语义。
	service.SetAICaller(func(ctx context.Context, req AICallRequest) (AICallResponse, error) {
		if req.Method != http.MethodGet {
			return AICallResponse{}, fmt.Errorf("write not allowed")
		}
		switch req.Path {
		case "/health":
			return AICallResponse{StatusCode: 200, Body: map[string]interface{}{"status": "ok"}}, nil
		default:
			return AICallResponse{StatusCode: 404, Body: map[string]interface{}{"error": "not found"}}, nil
		}
	})

	// serial 全成功
	result, err := service.aiRunBatch(batchReq(), map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"name": "first", "path": "/health", "method": "GET"},
			map[string]interface{}{"name": "second", "path": "/health", "method": "GET"},
		},
	})
	if err != nil {
		t.Fatalf("aiRunBatch err: %v", err)
	}
	payload := result.(map[string]interface{})
	if total, _ := payload["total"].(int); total != 2 {
		t.Fatalf("expected total 2, got %d", total)
	}
	if failed, _ := payload["failed"].(int); failed != 0 {
		t.Fatalf("expected 0 failed, got %d", failed)
	}
	items := payload["items"].([]map[string]interface{})
	if items[0]["name"] != "first" || items[0]["ok"] != true {
		t.Fatalf("unexpected item0: %#v", items[0])
	}
	if items[1]["ok"] != true {
		t.Fatalf("unexpected item1: %#v", items[1])
	}

	// 单个失败不中断整批
	result2, err := service.aiRunBatch(batchReq(), map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"path": "/health"},
			map[string]interface{}{"path": "/api/does-not-exist"},
			map[string]interface{}{"path": "/health"},
		},
	})
	if err != nil {
		t.Fatalf("aiRunBatch batch2 err: %v", err)
	}
	p2 := result2.(map[string]interface{})
	if failed, _ := p2["failed"].(int); failed != 1 {
		t.Fatalf("expected 1 failed, got %d", failed)
	}
	if total, _ := p2["total"].(int); total != 3 {
		t.Fatalf("expected total 3, got %d", total)
	}

	// stopOnError 短路
	result3, err := service.aiRunBatch(batchReq(), map[string]interface{}{
		"mode":        "serial",
		"stopOnError": true,
		"operations": []interface{}{
			map[string]interface{}{"path": "/health"},
			map[string]interface{}{"path": "/api/does-not-exist"},
			map[string]interface{}{"path": "/health"},
		},
	})
	if err != nil {
		t.Fatalf("aiRunBatch batch3 err: %v", err)
	}
	p3 := result3.(map[string]interface{})
	items3 := p3["items"].([]map[string]interface{})
	if len(items3) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items3))
	}
	if items3[1]["ok"] != false || items3[2]["ok"] != false {
		t.Fatalf("expected stopOnError to mark later ops failed: %#v", items3)
	}

	// 写操作被注入 caller 拒绝，且复用调用约束
	result4, err := service.aiRunBatch(batchReq(), map[string]interface{}{
		"mode": "parallel",
		"operations": []interface{}{
			map[string]interface{}{"path": "/health", "method": "GET"},
			map[string]interface{}{"path": "/health", "method": "POST"},
		},
	})
	if err != nil {
		t.Fatalf("aiRunBatch batch4 err: %v", err)
	}
	p4 := result4.(map[string]interface{})
	if failed, _ := p4["failed"].(int); failed != 1 {
		t.Fatalf("expected 1 failed in parallel batch, got %d", failed)
	}

	// 超限拒绝
	if _, err := service.aiRunBatch(batchReq(), map[string]interface{}{
		"operations": []interface{}{map[string]interface{}{"path": "/health"}},
	}); err == nil {
		// 1 个操作不应报错
	} else {
		t.Fatalf("unexpected error for single op: %v", err)
	}
	tooMany := make([]interface{}, 21)
	for i := range tooMany {
		tooMany[i] = map[string]interface{}{"path": "/health"}
	}
	if _, err := service.aiRunBatch(batchReq(), map[string]interface{}{"operations": tooMany}); err == nil {
		t.Fatal("expected error for >20 operations")
	}

	// 子操作审计入库：run_batch.<index> 记录应存在于 ai_access_audit。
	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatalf("store open err: %v", err)
	}
	defer db.Close()
	var auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_access_audit WHERE action LIKE 'run_batch.%'`).Scan(&auditCount); err != nil {
		t.Fatalf("audit query err: %v", err)
	}
	if auditCount < 4 {
		t.Fatalf("expected >=4 run_batch audit rows, got %d", auditCount)
	}
}

func TestAIRouteIndexResource(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/ai/mcp", nil)
	payload := service.routeIndexPayload("")
	total, _ := payload["count"].(int)
	if total <= 0 {
		t.Fatalf("expected route index count > 0, got %d", total)
	}
	routes := payload["routes"].([]map[string]interface{})
	if len(routes) != total {
		t.Fatalf("routes length %d != count %d", len(routes), total)
	}
	if _, ok := routes[0]["path"].(string); !ok {
		t.Fatalf("expected route path string: %#v", routes[0])
	}
	if _, ok := routes[0]["desc"].(string); !ok {
		t.Fatalf("expected route desc string: %#v", routes[0])
	}

	// 分组分片：取一个存在的分组，返回的路由都属于该分组。
	groups := payload["groups"].(map[string]int)
	var sampleGroup string
	for g, n := range groups {
		if n > 0 {
			sampleGroup = g
			break
		}
	}
	if sampleGroup == "" {
		t.Fatal("expected at least one route group")
	}
	slice := service.routeIndexPayload(sampleGroup)
	sliceRoutes := slice["routes"].([]map[string]interface{})
	if len(sliceRoutes) == 0 {
		t.Fatalf("expected non-empty group slice for %q", sampleGroup)
	}
	for _, r := range sliceRoutes {
		if r["group"] != sampleGroup {
			t.Fatalf("route group %v not expected %q", r["group"], sampleGroup)
		}
	}
	if g, _ := slice["group"].(string); g != sampleGroup {
		t.Fatalf("expected slice group %q, got %q", sampleGroup, g)
	}

	// 资源可通过 MCP resources/read 读取（全量与分组）
	res, err := service.mcpReadResource(req, "api-monitor://route-index")
	if err != nil {
		t.Fatalf("mcpReadResource err: %v", err)
	}
	contents := res.(map[string]interface{})["contents"].([]map[string]interface{})
	if len(contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(contents))
	}

	groupRes, err := service.mcpReadResource(req, "api-monitor://route-index/"+sampleGroup)
	if err != nil {
		t.Fatalf("mcpReadResource group err: %v", err)
	}
	groupContents := groupRes.(map[string]interface{})["contents"].([]map[string]interface{})
	if len(groupContents) != 1 {
		t.Fatalf("expected 1 group content, got %d", len(groupContents))
	}

	if _, err := service.mcpReadResource(req, "api-monitor://route-index/"); err == nil {
		t.Fatal("expected error for empty group resource uri")
	}
}
