package system

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	"github.com/iwvw/api-monitor/backend-go/internal/database"
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

	// GET 按 MCP Streamable HTTP 规范必须返回 405（本服务不提供 server→client SSE）。
	// 此前返回 200 + 完整工具清单，导致 opencode 等客户端无限重连（实测每秒 4~5 次）。
	getReq := httptest.NewRequest(http.MethodGet, "http://example.test/api/ai/mcp", nil)
	getReq.Header.Set("Authorization", "Bearer "+agentKey)
	getRes := httptest.NewRecorder()
	service.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/ai/mcp = %d, want 405 (body=%s)", getRes.Code, getRes.Body.String())
	}
	if got := getRes.Header().Get("Allow"); got != http.MethodPost {
		t.Fatalf("Allow = %q, want POST", got)
	}
	// 405 不能带工具清单，否则客户端仍可能当成正常响应继续重连。
	if strings.Contains(getRes.Body.String(), "list_apis") {
		t.Fatalf("405 body must not contain tool catalog: %s", getRes.Body.String())
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

// TestOpenAPIRequestBodyUsesSchema OpenAPI 导出的请求体必须带 schema（可生成客户端），
// 不能只有 example——那是「接口索引」而不是「契约」。这条测试守住 P0 回归。
func TestOpenAPIRequestBodyUsesSchema(t *testing.T) {
	s := New(config.Config{Version: "test", DataDir: t.TempDir(), DBName: "data.db"})
	defer s.Shutdown()
	req, _ := http.NewRequest(http.MethodGet, "/api/system/openapi.json", nil)
	doc := s.openapiDocument(req)

	schemas, ok := doc["components"].(map[string]interface{})["schemas"].(map[string]interface{})
	if !ok || len(schemas) == 0 {
		t.Fatalf("components.schemas 为空，外部工具无法据此生成客户端")
	}
	if _, ok := schemas["Envelope"]; !ok {
		t.Fatalf("components.schemas 缺少统一响应外壳 Envelope")
	}

	withBody, withSchema := 0, 0
	paths := doc["paths"].(map[string]interface{})
	for _, ops := range paths {
		for _, raw := range ops.(map[string]interface{}) {
			op, _ := raw.(map[string]interface{})
			rb, ok := op["requestBody"].(map[string]interface{})
			if !ok {
				continue
			}
			withBody++
			content, _ := rb["content"].(map[string]interface{})
			for _, rawMedia := range content {
				if media, ok := rawMedia.(map[string]interface{}); ok {
					if _, has := media["schema"]; has {
						withSchema++
						break
					}
				}
			}
		}
	}
	if withBody == 0 {
		t.Fatal("OpenAPI 里没有任何 requestBody")
	}
	// 允许极少数只有示例的旧契约，但绝大多数必须有 schema。
	if withSchema*10 < withBody*9 {
		t.Errorf("requestBody 带 schema 比例过低: %d/%d（要求 ≥90%%）", withSchema, withBody)
	}
	t.Logf("requestBody: %d, 带 schema: %d, components.schemas: %d", withBody, withSchema, len(schemas))
}

// TestOpenAPIAuthNotDuplicatedAsParameter 鉴权信息只能出现在 security 里，
// 不能把 sid Cookie 当普通参数重复声明（会误导调用方去手写 Cookie）。
func TestOpenAPIAuthNotDuplicatedAsParameter(t *testing.T) {
	s := New(config.Config{Version: "test", DataDir: t.TempDir(), DBName: "data.db"})
	defer s.Shutdown()
	req, _ := http.NewRequest(http.MethodGet, "/api/system/openapi.json", nil)
	doc := s.openapiDocument(req)

	for path, ops := range doc["paths"].(map[string]interface{}) {
		for method, raw := range ops.(map[string]interface{}) {
			op, _ := raw.(map[string]interface{})
			params, _ := op["parameters"].([]map[string]interface{})
			for _, p := range params {
				if p["in"] == "cookie" {
					t.Errorf("%s %s 把 Cookie 参数 %v 写进了 parameters，应只用 security 表达", method, path, p["name"])
				}
			}
		}
	}
}

// TestOpenAPIServerURLHonorsForwardedHeaders 反向代理后的 servers.url 必须可被外部调用。
func TestOpenAPIServerURLHonorsForwardedHeaders(t *testing.T) {
	s := New(config.Config{Version: "test", DataDir: t.TempDir(), DBName: "data.db"})
	defer s.Shutdown()

	req, _ := http.NewRequest(http.MethodGet, "/api/system/openapi.json", nil)
	req.Host = "10.0.0.5:3000"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "panel.example.com")
	doc := s.openapiDocument(req)
	servers := doc["servers"].([]map[string]string)
	if got := servers[0]["url"]; got != "https://panel.example.com" {
		t.Errorf("servers.url = %q, want https://panel.example.com", got)
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
		{"/api/modelhub/endpoints", "模型网关"},
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
		{"/api/onepanel/config", "1Panel"},
		{"/api/koyeb/accounts", "PaaS"},
		{"/api/flyio/accounts", "PaaS"},
		{"/api/scheduler/tasks", "定时任务"},
		{"/api/cron/tasks", "定时任务"},
		{"/api/uptime/monitors", "可用性监测"},
		{"/api/filebox/files", "文件柜"},
		{"/api/prompts/entries", "提示词库"},
		{"/api/totp/accounts", "双因子认证"},
		{"/api/assets", "资产管理"},
		{"/api/assets/overview", "资产管理"},
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

// TestApiStatsTrafficCycleMatchesPanel 验证仪表盘 API 趋势的顶层 traffic 采用
// 「当前周期 cycles 累计」（与订阅面板同口径），而 trend 逐日仍来自 hourly：
// 即便 30 天 hourly 窗口只有部分数据，顶部总额也不与订阅面板打架。
func TestApiStatsTrafficCycleMatchesPanel(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_traffic_cycle_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	service := New(config.Config{DataDir: tempDir, DBName: "data.db"})
	defer service.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS subscription_usage_cycles (
		subscription_id TEXT NOT NULL, cycle_start TEXT NOT NULL, cycle_end TEXT NOT NULL DEFAULT '',
		upload_bytes INTEGER NOT NULL DEFAULT 0, download_bytes INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL DEFAULT '', PRIMARY KEY(subscription_id, cycle_start))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS subscription_usage_hourly (
		server_id TEXT NOT NULL, node_id TEXT NOT NULL, subscription_id TEXT NOT NULL, hour TEXT NOT NULL,
		upload_bytes INTEGER NOT NULL DEFAULT 0, download_bytes INTEGER NOT NULL DEFAULT 0,
		reported_at TEXT NOT NULL DEFAULT '', PRIMARY KEY(server_id,node_id,subscription_id,hour))`); err != nil {
		t.Fatal(err)
	}
	// 周期累计（面板口径）：未来月窗口覆盖 now，总额 upload10+download20=30。
	nowUTC := time.Now().UTC()
	cycleStart := nowUTC.AddDate(0, -1, 0).Format(time.RFC3339)
	cycleEnd := nowUTC.AddDate(0, 1, 0).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO subscription_usage_cycles(subscription_id,cycle_start,cycle_end,upload_bytes,download_bytes,updated_at)
		VALUES('sub-1',?,?,10,20,datetime('now'))`, cycleStart, cycleEnd); err != nil {
		t.Fatal(err)
	}
	// 今日 hourly（趋势口径）：upload5+download7=12，远小于周期累计。
	hour := nowUTC.Truncate(time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO subscription_usage_hourly(server_id,node_id,subscription_id,hour,upload_bytes,download_bytes,reported_at)
		VALUES('srv','node','sub-1',?,5,7,datetime('now'))`, hour); err != nil {
		t.Fatal(err)
	}
	db.Close()

	stats, err := service.apiStats(7)
	if err != nil {
		t.Fatal(err)
	}
	// 顶层 traffic = 当前周期累计 30（与订阅面板一致），而不是 hourly 窗口合计 12。
	if got := stats["traffic"].(int64); got != 30 {
		t.Fatalf("top-level traffic = %d, want cycle total 30", got)
	}
	// trend 逐日来自 hourly:当天桶合计应为 12。按站点时区分桶,这里用「合计」
	// 断言而非固定日期字符串,避免 UTC 与站点时区跨天时(time-dependent)误判。
	trend, ok := stats["trend"].([]map[string]interface{})
	if !ok {
		t.Fatalf("trend type = %T", stats["trend"])
	}
	var trendTraffic int64
	for _, item := range trend {
		if v, ok := item["traffic"].(int64); ok {
			trendTraffic += v
		}
	}
	if trendTraffic != 12 {
		t.Fatalf("trend hourly total = %d, want 12", trendTraffic)
	}

}

// TestValidateAIAgentBearerKey 验证 Bearer 密钥校验的接受与拒绝路径：
// 正确密钥通过；同长度、异长度错误密钥以及缺失头一律拒绝。
func TestValidateAIAgentBearerKey(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "api_monitor_ai_bearer_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cfg := config.Config{DataDir: tempDir, DBName: "data.db", Version: "test"}
	service := New(cfg)
	defer service.Shutdown()

	req := httptest.NewRequest(http.MethodGet, "http://example.test/api/system/ai-access", nil)
	overview, err := service.aiAccessOverview(req)
	if err != nil {
		t.Fatal(err)
	}
	expectedKey := overview["agentKey"].(map[string]interface{})["value"].(string)
	if expectedKey == "" {
		t.Fatal("expected agent key")
	}

	db, err := service.store.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	valid := httptest.NewRequest(http.MethodGet, "http://example.test/api/ai/manifest", nil)
	valid.Header.Set("Authorization", "Bearer "+expectedKey)
	if !service.validateAIAgent(valid, db) {
		t.Fatal("correct bearer key must validate")
	}

	wrongSameLength := httptest.NewRequest(http.MethodGet, "http://example.test/api/ai/manifest", nil)
	wrongSameLength.Header.Set("Authorization", "Bearer "+strings.Repeat("x", len(expectedKey)))
	if service.validateAIAgent(wrongSameLength, db) {
		t.Fatal("same-length wrong key must be rejected")
	}

	wrongDifferentLength := httptest.NewRequest(http.MethodGet, "http://example.test/api/ai/manifest", nil)
	wrongDifferentLength.Header.Set("Authorization", "Bearer "+expectedKey[:len(expectedKey)-2])
	if service.validateAIAgent(wrongDifferentLength, db) {
		t.Fatal("different-length wrong key must be rejected")
	}

	missing := httptest.NewRequest(http.MethodGet, "http://example.test/api/ai/manifest", nil)
	if service.validateAIAgent(missing, db) {
		t.Fatal("missing authorization header must be rejected")
	}

	malformed := httptest.NewRequest(http.MethodGet, "http://example.test/api/ai/manifest", nil)
	malformed.Header.Set("Authorization", "Basic "+expectedKey)
	if service.validateAIAgent(malformed, db) {
		t.Fatal("non-bearer authorization must be rejected")
	}
}

func TestFileboxShareFromAgentTool(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var captured AICallRequest
	service.SetAICaller(func(ctx context.Context, req AICallRequest) (AICallResponse, error) {
		captured = req
		return AICallResponse{StatusCode: 200, Body: map[string]interface{}{"success": true}}, nil
	})

	// 缺 serverId / remotePath 必须报错
	if _, err := service.fileboxShareFromAgent(context.Background(), map[string]interface{}{"serverId": "srv-1"}); err == nil {
		t.Fatal("expected error when remotePath missing")
	}

	// 正常调用：应组装成对 share-from-agent 的 POST，并透传可选字段
	result, err := service.fileboxShareFromAgent(context.Background(), map[string]interface{}{
		"serverId":           "srv-1",
		"remotePath":         "/tmp/a.txt",
		"storageTarget":      "local",
		"expiry":             "1",
		"max_downloads":      "3",
		"burn_after_reading": true,
	})
	if err != nil {
		t.Fatalf("fileboxShareFromAgent err: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if captured.Method != http.MethodPost || captured.Path != "/api/filebox/share-from-agent" {
		t.Fatalf("unexpected call: %s %s", captured.Method, captured.Path)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["serverId"] != "srv-1" || body["remotePath"] != "/tmp/a.txt" {
		t.Fatalf("unexpected required fields: %#v", body)
	}
	if body["storageTarget"] != "local" || body["expiry"] != "1" || body["max_downloads"] != "3" {
		t.Fatalf("optional fields not passed through: %#v", body)
	}
	if body["burn_after_reading"] != true {
		t.Fatalf("burn_after_reading not passed through: %#v", body)
	}

	// 工具必须出现在 MCP 工具清单里
	found := false
	for _, tool := range service.aiTools() {
		if tool["name"] == "filebox_share_from_agent" {
			found = true
		}
	}
	if !found {
		t.Fatal("filebox_share_from_agent not present in aiTools()")
	}
}

func TestAgentTransferFileTool(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var gotSrc, gotDst string
	service.agentFileOps = fakeFileOperator{onCopy: func(src, dst string) { gotSrc, gotDst = src, dst }}

	// 必填缺失必须报错
	if _, err := service.agentTransferFile(context.Background(), map[string]interface{}{
		"sourceServerId": "srv-a", "sourcePath": "/tmp/a.sh", "targetServerId": "srv-b",
	}); err == nil {
		t.Fatal("expected error when targetPath missing")
	}

	result, err := service.agentTransferFile(context.Background(), map[string]interface{}{
		"sourceServerId": "srv-a",
		"sourcePath":     "/tmp/a.sh",
		"targetServerId": "srv-b",
		"targetPath":     "/opt/a.sh",
	})
	if err != nil {
		t.Fatalf("agentTransferFile err: %v", err)
	}
	payload, ok := result.(map[string]interface{})
	if !ok || payload["success"] != true || payload["transport"] != "agent-to-agent" {
		t.Fatalf("unexpected result: %#v", result)
	}
	if gotSrc != "srv-a:/tmp/a.sh" || gotDst != "srv-b:/opt/a.sh" {
		t.Fatalf("panel-relay 参数错误: src=%s dst=%s", gotSrc, gotDst)
	}
}

func TestAgentUploadFileTool(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var gotServerID, gotPath string
	var gotData []byte
	service.SetAgentFileUploader(uploaderFunc(func(ctx context.Context, serverID, remotePath string, data []byte, timeout time.Duration) (int64, error) {
		gotServerID, gotPath, gotData = serverID, remotePath, data
		return int64(len(data)), nil
	}))

	// 必填缺失必须报错
	if _, err := service.agentUploadFile(context.Background(), map[string]interface{}{"serverId": "srv-1"}); err == nil {
		t.Fatal("expected error when path and content missing")
	}
	if _, err := service.agentUploadFile(context.Background(), map[string]interface{}{
		"serverId": "srv-1", "path": "/opt/a.bin", "content_base64": "!!!not-base64!!!",
	}); err == nil {
		t.Fatal("expected error for invalid base64")
	}

	payload := []byte("hello world")
	encoded := base64.StdEncoding.EncodeToString(payload)
	result, err := service.agentUploadFile(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "/opt/a.bin",
		"content_base64": encoded,
	})
	if err != nil {
		t.Fatalf("agentUploadFile err: %v", err)
	}
	out, ok := result.(map[string]interface{})
	if !ok || out["success"] != true || out["size"] != int64(len(payload)) {
		t.Fatalf("unexpected result: %#v", result)
	}
	if gotServerID != "srv-1" || gotPath != "/opt/a.bin" || string(gotData) != "hello world" {
		t.Fatalf("uploader not called correctly: server=%q path=%q data=%q", gotServerID, gotPath, string(gotData))
	}

	found := false
	for _, tool := range service.aiTools() {
		if tool["name"] == "agent_upload_file" {
			found = true
		}
	}
	if !found {
		t.Fatal("agent_upload_file not present in aiTools()")
	}
}

type uploaderFunc func(ctx context.Context, serverID, remotePath string, data []byte, timeout time.Duration) (int64, error)

func (f uploaderFunc) WriteAgentFileBytes(ctx context.Context, serverID, remotePath string, data []byte, timeout time.Duration) (int64, error) {
	return f(ctx, serverID, remotePath, data, timeout)
}

// 超过 256KB 的内联 base64 必须被拒绝，并给出「字节不经模型」的替代命令。
// 旧实现的 88MB 上限是虚构的：真正的瓶颈是模型输出长度，放行只会让它反复失败。
func TestAgentUploadFileRejectsOversizedInlinePayload(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()
	service.SetAgentFileUploader(uploaderFunc(func(context.Context, string, string, []byte, time.Duration) (int64, error) {
		t.Fatal("超限内容不应触达写入层")
		return 0, nil
	}))

	oversized := base64.StdEncoding.EncodeToString(make([]byte, 300*1024))
	_, err := service.agentUploadFile(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "/opt/big.bin",
		"content_base64": oversized,
	})
	if err == nil {
		t.Fatal("超限内容应报错")
	}
	for _, hint := range []string{"agent/upload", "agent_transfer_file", "agent_fetch_url", "agent_upload_chunk"} {
		if !strings.Contains(err.Error(), hint) {
			t.Fatalf("错误信息应给出替代通道 %q：%v", hint, err)
		}
	}
}

// sha256 不匹配必须报错：模型复述长 base64 会静默改写字节，没有校验就写坏文件。
func TestAgentUploadFileVerifiesSHA256(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()
	wrote := false
	service.SetAgentFileUploader(uploaderFunc(func(_ context.Context, _, _ string, data []byte, _ time.Duration) (int64, error) {
		wrote = true
		return int64(len(data)), nil
	}))

	payload := []byte("#!/bin/sh\necho hi\n")
	encoded := base64.StdEncoding.EncodeToString(payload)

	_, err := service.agentUploadFile(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "/opt/a.sh",
		"content_base64": encoded,
		"sha256":         strings.Repeat("0", 64),
	})
	if err == nil {
		t.Fatal("sha256 不匹配应报错")
	}
	if wrote {
		t.Fatal("校验失败不应写入")
	}

	sum := sha256.Sum256(payload)
	if _, err := service.agentUploadFile(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "/opt/a.sh",
		"content_base64": encoded,
		"sha256":         hex.EncodeToString(sum[:]),
	}); err != nil {
		t.Fatalf("校验通过时应写入: %v", err)
	}
	if !wrote {
		t.Fatal("校验通过后应写入")
	}
}

// 分块上传：offset 必须与目标文件当前大小一致，否则报错而不是写出错位文件。
func TestAgentUploadChunkRejectsDiscontinuousOffset(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()
	service.agentFileOps = fakeFileOperator{
		statSize: 100,
	}
	_, err := service.agentUploadChunk(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "/opt/a.bin",
		"offset":         float64(50),
		"content_base64": base64.StdEncoding.EncodeToString([]byte("abc")),
	})
	if err == nil {
		t.Fatal("offset 与文件当前大小不一致时应报错")
	}
	if !strings.Contains(err.Error(), "offset") {
		t.Fatalf("错误应说明 offset 问题: %v", err)
	}
}

// 续写(offset>0)前的连续性校验必须携带 realPath:首块按真实路径写、这里退回沙箱路径时,
// 会误判「文件不存在」,真实路径下的分块上传永远续不上(实测 offset=262144 必失败)。
func TestAgentUploadChunkContinuationPassesRealPath(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var statRealPath bool
	service.agentFileOps = fakeFileOperator{
		statSize:     262144,
		realPath:     "D:/x.bin",
		statRealPath: &statRealPath,
	}

	_, err := service.agentUploadChunk(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "D:/x.bin",
		"offset":         float64(262144),
		"content_base64": base64.StdEncoding.EncodeToString([]byte("abc")),
		"realPath":       true,
	})
	if err != nil {
		t.Fatalf("agentUploadChunk 续写 err: %v", err)
	}
	if !statRealPath {
		t.Fatal("续写前的 stat 必须带 realPath=true,否则真实路径下永远续不上")
	}
}

// 分块上传首块（offset=0）不校验前序状态，并应回传 realPath。
func TestAgentUploadChunkFirstBlockReturnsRealPath(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()
	service.agentFileOps = fakeFileOperator{realPath: "/var/lib/api-monitor/agent/files/opt/a.bin"}

	var gotOffset int64
	var gotTruncate bool
	service.agentFileOps = fakeFileOperator{
		realPath: "/var/lib/api-monitor/agent/files/opt/a.bin",
		onChunk: func(offset int64, truncate bool) {
			gotOffset, gotTruncate = offset, truncate
		},
	}

	result, err := service.agentUploadChunk(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "/opt/a.bin",
		"offset":         float64(0),
		"content_base64": base64.StdEncoding.EncodeToString([]byte("abc")),
	})
	if err != nil {
		t.Fatalf("agentUploadChunk err: %v", err)
	}
	out := result.(map[string]interface{})
	if out["realPath"] != "/var/lib/api-monitor/agent/files/opt/a.bin" {
		t.Fatalf("应回传 realPath: %#v", out)
	}
	if gotOffset != 0 || !gotTruncate {
		t.Fatalf("首块应 offset=0 且 truncate=true，实际 offset=%d truncate=%v", gotOffset, gotTruncate)
	}
}

// 新工具必须出现在 MCP 工具清单里，否则客户端永远发现不了它们。
// realPath 开关:显式 true 时写入必须按主机真实路径透传给 Agent 层;缺省仍为沙箱语义。
// 这是「直接更新 /opt/... 真实路径」的关键——否则文件只会落进沙箱。
func TestAgentUploadRealPathOptIn(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var gotRealPath bool
	service.agentFileOps = fakeFileOperator{
		realPath:    "/opt/1panel/www/sites/x/index/ships.json",
		gotRealPath: &gotRealPath,
	}
	result, err := service.agentUploadFile(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "/opt/1panel/www/sites/x/index/ships.json",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("{}")),
		"realPath":       true,
	})
	if err != nil {
		t.Fatalf("agentUploadFile err: %v", err)
	}
	if !gotRealPath {
		t.Fatal("realPath=true 应透传到 Agent 写入层")
	}
	out := result.(map[string]interface{})
	if out["realPathMode"] != true {
		t.Fatalf("响应应标注 realPathMode=true: %#v", out)
	}
	if out["realPath"] != "/opt/1panel/www/sites/x/index/ships.json" {
		t.Fatalf("realPath 应为目标真实路径: %#v", out)
	}

	// 缺省(不传 realPath)必须仍走沙箱语义,不改变既有行为。
	gotRealPath = true
	service.agentFileOps = fakeFileOperator{realPath: "/var/lib/api-monitor/agent/files/opt/a.json", gotRealPath: &gotRealPath}
	if _, err := service.agentUploadFile(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "/opt/a.json",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("{}")),
	}); err != nil {
		t.Fatalf("agentUploadFile(默认) err: %v", err)
	}
	if gotRealPath {
		t.Fatal("缺省应保持沙箱语义(realPath=false)")
	}
}

func TestAgentFileToolsRegistered(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	names := map[string]bool{}
	for _, tool := range service.aiTools() {
		if name, ok := tool["name"].(string); ok {
			names[name] = true
		}
	}
	for _, want := range []string{
		"agent_run_command", "agent_task_result",
		"agent_upload_file", "agent_upload_chunk",
		"agent_file_roots", "agent_file_stat", "agent_file_chmod",
		"agent_file_list", "agent_file_read", "agent_file_write",
		"agent_file_mkdir", "agent_file_rename", "agent_file_delete", "agent_file_rmdir",
		"host_file_copy",
	} {
		if !names[want] {
			t.Fatalf("工具 %s 未注册", want)
		}
	}
}

// 分块上传必须显式提供 offset：缺省按 0 处理会带 truncate=true，
// 把调用方本意的「续写」静默变成「覆盖」，是真实的数据丢失路径。
// SFTP 复刻工具:应把主机标识与路径正确组装成对 /api/server/sftp/* 的调用。
// fetch-url 的 realPath 开关必须透传到 HTTP 请求体(否则目标永远写沙箱)。
func TestAgentFetchURLPassesRealPath(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var gotBody map[string]interface{}
	service.SetAICaller(func(_ context.Context, req AICallRequest) (AICallResponse, error) {
		if req.Path == "/api/server/agent/fetch-url" {
			_ = json.Unmarshal(req.Body, &gotBody)
		}
		return AICallResponse{StatusCode: 200, Body: map[string]interface{}{"success": true}}, nil
	})

	if _, err := service.agentFetchURL(context.Background(), map[string]interface{}{
		"serverId": "srv-1",
		"url":      "https://example.com/a.bin",
		"path":     "/opt/1panel/a.bin",
		"realPath": true,
	}); err != nil {
		t.Fatalf("agentFetchURL err: %v", err)
	}
	if gotBody["realPath"] != true {
		t.Fatalf("realPath 未透传到 fetch-url: %#v", gotBody)
	}
}

func TestAgentFileListBuildsSFTPCall(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var gotPath string
	var gotBody map[string]interface{}
	service.SetAICaller(func(_ context.Context, req AICallRequest) (AICallResponse, error) {
		gotPath = req.Path
		_ = json.Unmarshal(req.Body, &gotBody)
		return AICallResponse{StatusCode: 200, Body: map[string]interface{}{"success": true}}, nil
	})

	if _, err := service.agentFileList(context.Background(), map[string]interface{}{
		"serverId": "srv-1",
		"path":     "/opt/1panel/www",
	}); err != nil {
		t.Fatalf("agentFileList err: %v", err)
	}
	if gotPath != "/api/server/sftp/list" {
		t.Fatalf("应调用 sftp/list,实际 %s", gotPath)
	}
	if gotBody["serverId"] != "srv-1" || gotBody["path"] != "/opt/1panel/www" {
		t.Fatalf("请求体组装错误: %#v", gotBody)
	}
}

// agent_file_rename 需要 oldPath/newPath,且不需要 path。
func TestAgentFileRenameBuildsSFTPCall(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var gotPath string
	var gotBody map[string]interface{}
	service.SetAICaller(func(_ context.Context, req AICallRequest) (AICallResponse, error) {
		gotPath = req.Path
		_ = json.Unmarshal(req.Body, &gotBody)
		return AICallResponse{StatusCode: 200, Body: map[string]interface{}{"success": true}}, nil
	})

	if _, err := service.agentFileRename(context.Background(), map[string]interface{}{
		"serverId": "srv-1",
		"oldPath":  "/opt/a.txt",
		"newPath":  "/opt/b.txt",
	}); err != nil {
		t.Fatalf("agentFileRename err: %v", err)
	}
	if gotPath != "/api/server/sftp/rename" {
		t.Fatalf("应调用 sftp/rename,实际 %s", gotPath)
	}
	if gotBody["oldPath"] != "/opt/a.txt" || gotBody["newPath"] != "/opt/b.txt" {
		t.Fatalf("请求体组装错误: %#v", gotBody)
	}
	if _, hasPath := gotBody["path"]; hasPath {
		t.Fatalf("rename 不应携带 path: %#v", gotBody)
	}
}

func TestHostFileCopyBuildsRelayCall(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var gotSrc, gotDst string
	service.agentFileOps = fakeFileOperator{onCopy: func(src, dst string) { gotSrc, gotDst = src, dst }}

	if _, err := service.hostFileCopy(context.Background(), map[string]interface{}{
		"sourceServerId": "srv-a",
		"sourcePath":     "/data/a.bin",
		"targetServerId": "srv-b",
		"targetPath":     "/data/b.bin",
	}); err != nil {
		t.Fatalf("hostFileCopy err: %v", err)
	}
	if gotSrc != "srv-a:/data/a.bin" || gotDst != "srv-b:/data/b.bin" {
		t.Fatalf("copy 参数错误: src=%s dst=%s", gotSrc, gotDst)
	}
}

func TestAgentUploadChunkRequiresExplicitOffset(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()
	wrote := false
	service.agentFileOps = fakeFileOperator{onChunk: func(int64, bool) { wrote = true }}

	_, err := service.agentUploadChunk(context.Background(), map[string]interface{}{
		"serverId":       "srv-1",
		"path":           "/opt/a.bin",
		"content_base64": base64.StdEncoding.EncodeToString([]byte("abc")),
	})
	if err == nil {
		t.Fatal("缺少 offset 应报错，避免静默覆盖已有文件")
	}
	if wrote {
		t.Fatal("缺少 offset 时不应写入")
	}
}

// 数据库被导入/恢复整体替换后，AI 接入的建表缓存必须失效重建，
// 否则新库里没有审计表却仍认为「已就绪」。
func TestEnsureAIAccessSchemaRebuildsAfterPoolReset(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ensureAIAccessSchema(ctx, db); err != nil {
		t.Fatalf("首次建表失败: %v", err)
	}
	db.Close()

	// 模拟导入换库：换一个全新的空库文件，表结构全部消失。
	database.ResetPool(service.store.DatabasePath())

	db2, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if _, err := db2.ExecContext(ctx, "DROP TABLE IF EXISTS ai_access_audit"); err != nil {
		t.Fatal(err)
	}
	if err := service.ensureAIAccessSchema(ctx, db2); err != nil {
		t.Fatalf("换库后重建表失败: %v", err)
	}
	var count int
	if err := db2.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='ai_access_audit'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("换库后 ai_access_audit 表应被重新创建")
	}
}

// fakeFileOperator 是 AgentFileOperator 的测试替身。
type fakeFileOperator struct {
	statSize     int64
	realPath     string
	onChunk      func(offset int64, truncate bool)
	gotRealPath  *bool
	statRealPath *bool
	onCopy       func(src, dst string)
}

func (f fakeFileOperator) CopyAgentFile(_ context.Context, srcID, srcPath string, _ bool, dstID, dstPath string, _ bool, _ time.Duration) (int64, error) {
	if f.onCopy != nil {
		f.onCopy(srcID+":"+srcPath, dstID+":"+dstPath)
	}
	return 0, nil
}

func (f fakeFileOperator) CopyAgentPath(_ context.Context, srcID, srcPath string, _ bool, dstID, dstPath string, _ bool, _ time.Duration) (int64, int64, error) {
	if f.onCopy != nil {
		f.onCopy(srcID+":"+srcPath, dstID+":"+dstPath)
	}
	return 1, 0, nil
}

func (f fakeFileOperator) CopyAgentPathEx(_ context.Context, srcID, srcPath, dstID, dstPath string, _ AgentCopyOptions, _ time.Duration) (int64, int64, error) {
	if f.onCopy != nil {
		f.onCopy(srcID+":"+srcPath, dstID+":"+dstPath)
	}
	return 1, 0, nil
}

func (f fakeFileOperator) WriteAgentFileChunk(_ context.Context, _, _ string, data []byte, offset int64, truncate bool, _ time.Duration) (AgentFileWriteResult, error) {
	if f.onChunk != nil {
		f.onChunk(offset, truncate)
	}
	return AgentFileWriteResult{Size: offset + int64(len(data)), RealPath: f.realPath}, nil
}

func (f fakeFileOperator) WriteAgentFileChunkMode(_ context.Context, _, _ string, data []byte, offset int64, truncate bool, _ time.Duration, realPath bool) (AgentFileWriteResult, error) {
	if f.gotRealPath != nil {
		*f.gotRealPath = realPath
	}
	if f.onChunk != nil {
		f.onChunk(offset, truncate)
	}
	return AgentFileWriteResult{Size: offset + int64(len(data)), RealPath: f.realPath}, nil
}

func (f fakeFileOperator) WriteAgentFileBytesDetailed(_ context.Context, _, _ string, data []byte, _ time.Duration) (AgentFileWriteResult, error) {
	return AgentFileWriteResult{Size: int64(len(data)), RealPath: f.realPath}, nil
}

func (f fakeFileOperator) WriteAgentFileBytesDetailedMode(_ context.Context, _, _ string, data []byte, _ time.Duration, realPath bool) (AgentFileWriteResult, error) {
	if f.gotRealPath != nil {
		*f.gotRealPath = realPath
	}
	return AgentFileWriteResult{Size: int64(len(data)), RealPath: f.realPath}, nil
}

func (f fakeFileOperator) AgentFileRoots(_ context.Context, _ string) (map[string]interface{}, error) {
	return map[string]interface{}{
		"sandboxRoot":  "/var/lib/api-monitor/agent/files",
		"allowedRoots": []string{"/var/lib/api-monitor/agent/files"},
	}, nil
}

func (f fakeFileOperator) AgentFileStat(_ context.Context, _, _ string) (map[string]interface{}, error) {
	return f.AgentFileStatMode(context.Background(), "", "", false)
}

func (f fakeFileOperator) AgentFileStatMode(_ context.Context, _, _ string, realPath bool) (map[string]interface{}, error) {
	if f.statRealPath != nil {
		*f.statRealPath = realPath
	}
	return map[string]interface{}{"size": float64(f.statSize), "realPath": f.realPath, "isFile": true}, nil
}

func (f fakeFileOperator) AgentFileChmod(_ context.Context, _, _ string, mode uint32) (string, error) {
	return "权限修改成功", nil
}

func TestAgentRunCommandTool(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var captured AICallRequest
	service.SetAICaller(func(ctx context.Context, req AICallRequest) (AICallResponse, error) {
		captured = req
		return AICallResponse{StatusCode: 200, Body: map[string]interface{}{"success": true}}, nil
	})

	if _, err := service.agentRunCommand(context.Background(), map[string]interface{}{"serverId": "srv-1"}); err == nil {
		t.Fatal("expected error when command missing")
	}

	if _, err := service.agentRunCommand(context.Background(), map[string]interface{}{
		"serverId": "srv-1",
		"command":  "uname -a",
		"timeout":  float64(15),
	}); err != nil {
		t.Fatalf("agentRunCommand err: %v", err)
	}
	if captured.Method != http.MethodPost || captured.Path != "/api/server/agent/command/srv-1" {
		t.Fatalf("unexpected call: %s %s", captured.Method, captured.Path)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["command"] != "uname -a" || body["timeout"] != float64(15) {
		t.Fatalf("unexpected body: %#v", body)
	}

	found := false
	for _, tool := range service.aiTools() {
		if tool["name"] == "agent_run_command" {
			found = true
		}
	}
	if !found {
		t.Fatal("agent_run_command not present in aiTools()")
	}
}

// agent_run_command 的 cwd / wait 参数应透传到下游请求体。
func TestAgentRunCommandForwardsCwdAndAsync(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var captured AICallRequest
	service.SetAICaller(func(ctx context.Context, req AICallRequest) (AICallResponse, error) {
		captured = req
		return AICallResponse{StatusCode: 202, Body: map[string]interface{}{"success": true, "task_id": "t-1"}}, nil
	})

	if _, err := service.agentRunCommand(context.Background(), map[string]interface{}{
		"serverId": "srv-1",
		"command":  "make build",
		"cwd":      "/srv/app",
		"wait":     false,
		"timeout":  float64(120),
	}); err != nil {
		t.Fatalf("agentRunCommand err: %v", err)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(captured.Body, &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["cwd"] != "/srv/app" {
		t.Fatalf("cwd 未透传: %#v", body)
	}
	if body["wait"] != false {
		t.Fatalf("wait=false 未透传: %#v", body)
	}
}

// serverId 缺失时应能按名称/主机解析；命中多台或全不命中要给出可操作错误。
func TestAgentRunCommandResolvesHostByNameOrHost(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	ctx := context.Background()
	db, err := service.store.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// server_accounts 属于 serveragent 模块的 schema，这里按需建最小表结构。
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS server_accounts (id TEXT PRIMARY KEY, name TEXT, host TEXT)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range [][3]string{
		{"srv-1", "edge-tokyo", "192.0.2.10"},
		{"srv-2", "edge-osaka", "192.0.2.11"},
	} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO server_accounts (id,name,host) VALUES (?,?,?)`,
			row[0], row[1], row[2]); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	var captured AICallRequest
	service.SetAICaller(func(_ context.Context, req AICallRequest) (AICallResponse, error) {
		captured = req
		return AICallResponse{StatusCode: 200, Body: map[string]interface{}{"success": true}}, nil
	})

	if _, err := service.agentRunCommand(ctx, map[string]interface{}{
		"serverName": "edge-tokyo",
		"command":    "uptime",
	}); err != nil {
		t.Fatalf("按名称解析失败: %v", err)
	}
	if captured.Path != "/api/server/agent/command/srv-1" {
		t.Fatalf("名称未解析为真实 ID: %s", captured.Path)
	}

	if _, err := service.agentRunCommand(ctx, map[string]interface{}{
		"host":    "192.0.2.11",
		"command": "uptime",
	}); err != nil {
		t.Fatalf("按主机地址解析失败: %v", err)
	}
	if captured.Path != "/api/server/agent/command/srv-2" {
		t.Fatalf("host 未解析为真实 ID: %s", captured.Path)
	}

	if _, err := service.agentRunCommand(ctx, map[string]interface{}{
		"serverName": "no-such-host",
		"command":    "uptime",
	}); err == nil {
		t.Fatal("未知主机应报错")
	} else if !strings.Contains(err.Error(), "/api/aiagent/servers") {
		t.Fatalf("错误信息应给出取 ID 的接口: %v", err)
	}
}

// agent_task_result 应查询任务终态接口。
func TestAgentTaskResultTool(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	service := New(cfg)
	defer service.Shutdown()

	var captured AICallRequest
	service.SetAICaller(func(_ context.Context, req AICallRequest) (AICallResponse, error) {
		captured = req
		return AICallResponse{StatusCode: 200, Body: map[string]interface{}{"success": true}}, nil
	})

	if _, err := service.agentTaskResult(context.Background(), map[string]interface{}{}); err == nil {
		t.Fatal("taskId 缺失应报错")
	}
	if _, err := service.agentTaskResult(context.Background(), map[string]interface{}{"taskId": "t-1"}); err != nil {
		t.Fatalf("agentTaskResult err: %v", err)
	}
	if captured.Method != http.MethodGet || captured.Path != "/api/server/tasks/t-1" {
		t.Fatalf("unexpected call: %s %s", captured.Method, captured.Path)
	}

	found := false
	for _, tool := range service.aiTools() {
		if tool["name"] == "agent_task_result" {
			found = true
		}
	}
	if !found {
		t.Fatal("agent_task_result not present in aiTools()")
	}
}
