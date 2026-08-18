package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// TestAIRouteReachabilitySmoke 在 full 权限模式下遍历 manifest 全部 AI 可达
// 候选路由（Owner=Go、非流式/非 WebSocket），依次用 GET / POST {} 经 AI
// 通道实际调用并分类：
//   - "go route not implemented" 404 = 路由未分发（硬伤，必须修）
//   - 500 = 内部错误（候选硬伤）
//   - 有意拦截（AI 递归面 / 密钥轮换）= 预期，不计失败
//   - 其余 404/405/400 = 参数或方法语义，记录供人工判定
//
// 运行：go test ./internal/server/ -run TestAIRouteReachabilitySmoke -v
func TestAIRouteReachabilitySmoke(t *testing.T) {
	handler := testServer(t)
	cookie := loginServerForTest(t, handler)

	// 切到 full 模式（用户自用场景），随后恢复 standard
	policyReq := httptest.NewRequest(http.MethodPut, "/api/system/ai-access/policy", strings.NewReader(`{"policy":"full"}`))
	policyReq.Header.Set("Content-Type", "application/json")
	policyReq.AddCookie(cookie)
	policyRes := httptest.NewRecorder()
	handler.ServeHTTP(policyRes, policyReq)
	if policyRes.Code != http.StatusOK {
		t.Fatalf("set full policy: %d body=%s", policyRes.Code, policyRes.Body.String())
	}
	t.Cleanup(func() {
		restore := httptest.NewRequest(http.MethodPut, "/api/system/ai-access/policy", strings.NewReader(`{"policy":"standard"}`))
		restore.Header.Set("Content-Type", "application/json")
		restore.AddCookie(cookie)
		handler.ServeHTTP(httptest.NewRecorder(), restore)
	})

	var expectedBlocked, notImplemented []string
	var internalErrors, interesting []string
	var okCount, paramCount int

	tryCall := func(path, method string) (int, string) {
		resp, err := handler.callAPIFromAI(context.Background(), systemmetrics.AICallRequest{Method: method, Path: path})
		if err != nil {
			return 0, err.Error()
		}
		return resp.StatusCode, resp.Raw
	}

	// 遍历源 = OpenAPI 文档全量路由（manifest + 契约补充的具体子路由），
	// 确保"微小接口"也被实测，而不是只测 manifest 前缀聚合路由。
	type docRoute struct {
		prefix  string
		methods []string
	}
	docsReq := httptest.NewRequest(http.MethodGet, "/api/system/api-docs", nil)
	docsReq.AddCookie(cookie)
	docsRes := httptest.NewRecorder()
	handler.ServeHTTP(docsRes, docsReq)
	var docsEnvelope struct {
		Data struct {
			Routes []map[string]interface{} `json:"routes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(docsRes.Body.Bytes(), &docsEnvelope); err != nil || docsRes.Code != http.StatusOK {
		t.Fatalf("fetch api-docs: %d %v body=%s", docsRes.Code, err, shortBody(docsRes.Body.String()))
	}
	var docRoutes []docRoute
	for _, item := range docsEnvelope.Data.Routes {
		prefix, _ := item["prefix"].(string)
		owner, _ := item["owner"].(string)
		mode, _ := item["responseMode"].(string)
		if owner != string(manifest.OwnerGo) || mode == string(manifest.ResponseStream) || mode == string(manifest.ResponseWebSocket) {
			continue
		}
		if strings.HasPrefix(prefix, "/api/auth") || strings.HasPrefix(prefix, "/api/ai/") || prefix == "/socket.io/" || strings.HasPrefix(prefix, "/ws/") {
			continue
		}
		var methods []string
		if rawMethods, ok := item["methods"].([]interface{}); ok {
			for _, m := range rawMethods {
				if s, ok := m.(string); ok {
					methods = append(methods, s)
				}
			}
		}
		if len(methods) == 0 {
			methods = []string{http.MethodGet}
		}
		docRoutes = append(docRoutes, docRoute{prefix: prefix, methods: methods})
	}

	for _, r := range docRoutes {
		path := aiSmokePath(r.prefix)
		method := r.methods[0]
		if method == http.MethodPut || method == http.MethodDelete {
			method = http.MethodPost
		}

		status, body := tryCall(path, method)
		if status == 0 {
			if strings.Contains(body, "不允许递归调用") || strings.Contains(body, "密钥管理接口不允许") {
				expectedBlocked = append(expectedBlocked, path+" "+method)
				continue
			}
			internalErrors = append(internalErrors, fmt.Sprintf("%s %s -> %s", path, method, shortBody(body)))
			continue
		}
		if strings.Contains(body, "go route not implemented") {
			notImplemented = append(notImplemented, path+" "+method)
			continue
		}
		if status == http.StatusMethodNotAllowed {
			// 写语义路由：POST {} 再次验证可达性（多数会 400 参数校验，即可达）
			status, body = tryCall(path, http.MethodPost)
			if status == 0 {
				if strings.Contains(body, "不允许递归调用") || strings.Contains(body, "密钥管理接口不允许") {
					expectedBlocked = append(expectedBlocked, path+" POST")
					continue
				}
				internalErrors = append(internalErrors, fmt.Sprintf("%s POST -> %s", path, shortBody(body)))
				continue
			}
			if strings.Contains(body, "go route not implemented") {
				notImplemented = append(notImplemented, path+" POST")
				continue
			}
		}
		switch {
		case status >= 200 && status < 300:
			okCount++
		case status == http.StatusBadRequest:
			paramCount++
		case status == http.StatusNotFound:
			// 404 可能有 handler 但参数/数据不存在：POST {} 二次探测，
			// 若 POST 不再是 404，说明该路径真实存在 handler（可达）
			postStatus, postBody := tryCall(path, http.MethodPost)
			if postStatus != http.StatusNotFound && !strings.Contains(postBody, "go route not implemented") && postStatus != 0 {
				paramCount++
			} else {
				interesting = append(interesting, fmt.Sprintf("%s GET->404 POST->%d %s", path, postStatus, shortBody(postBody)))
			}
		case status >= 500:
			internalErrors = append(internalErrors, fmt.Sprintf("%s -> %d %s", path, status, shortBody(body)))
		default:
			interesting = append(interesting, fmt.Sprintf("%s -> %d %s", path, status, shortBody(body)))
		}
	}

	t.Logf("OK=%d param-required=%d expected-blocked=%d not-implemented=%d internal-errors=%d interesting=%d",
		okCount, paramCount, len(expectedBlocked), len(notImplemented), len(internalErrors), len(interesting))
	for _, item := range expectedBlocked {
		t.Logf("EXPECTED-BLOCKED: %s", item)
	}
	for _, item := range notImplemented {
		t.Logf("NOT-IMPLEMENTED: %s", item)
	}
	for _, item := range internalErrors {
		t.Logf("INTERNAL-ERROR: %s", item)
	}
	if len(interesting) > 0 {
		t.Logf("--- 其余 404/405 清单（多为根路径无业务或数据不存在，人工判定）---")
		for _, item := range interesting {
			t.Logf("  LIST: %s", item)
		}
	}

	if len(notImplemented) > 0 {
		t.Errorf("发现 %d 条 manifest 已注册但未分发的路由", len(notImplemented))
	}
}

func aiSmokePath(prefix string) string {
	parts := strings.Split(prefix, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			parts[i] = "1"
		}
	}
	return strings.Join(parts, "/")
}

func shortBody(body string) string {
	if len(body) > 120 {
		return body[:120] + "..."
	}
	s, _ := json.Marshal(strings.TrimSpace(body))
	return strings.Trim(string(s), `"`)
}