package adminai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// apiCatalogText 返回系统提示词中的确定性接口清单（进程内构建一次并缓存；
// 构建失败返回空串，下次 run 自动重试）。
func (s *Service) apiCatalogText(ctx context.Context) string {
	s.catalogMu.Lock()
	if s.catalogDone {
		text := s.catalogText
		s.catalogMu.Unlock()
		return text
	}
	s.catalogMu.Unlock()

	text, err := s.buildCatalogText(ctx)
	s.catalogMu.Lock()
	if err == nil {
		s.catalogText = text
		s.catalogDone = true
		slog.Info("adminai-catalog", "lines", strings.Count(text, "\n")+1, "bytes", len(text))
	} else {
		slog.Warn("adminai-catalog-failed", "err", err.Error())
	}
	text = s.catalogText
	s.catalogMu.Unlock()
	return text
}

// buildCatalogText 从系统 auto-docs（含 Methods 与请求契约的确定性文档）生成紧凑清单。
// 只保留 /api/ 下会话内可直接调用的 JSON 接口，排除流式/WebSocket/代理、公共路由、
// 聚合前缀（matchMode=prefix 的模块总入口，不可直接调用）与文档类元接口
// （api-docs/openapi 自身，避免模型拉全量文档重复浪费词元）。
// 每条路由附带：请求体字段摘要（类型/必填/枚举）、废弃状态；完整契约缓存供 get_route 使用。
func (s *Service) buildCatalogText(ctx context.Context) (string, error) {
	if s.aiCaller == nil {
		return "", fmt.Errorf("AI 调用器未配置")
	}
	resp, err := s.aiCaller(ctx, systemmetrics.AICallRequest{Method: http.MethodGet, Path: "/api/system/api-docs"})
	if err != nil {
		return "", err
	}
	payload, ok := resp.Body.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("api-docs 返回格式异常")
	}
	// 内部接口经 response.OK 统一包装为 {success, data}，先解包
	if inner, ok := payload["data"].(map[string]interface{}); ok {
		payload = inner
	}
	rawRoutes, ok := payload["routes"].([]interface{})
	if !ok {
		return "", fmt.Errorf("api-docs 缺少 routes")
	}

	lines := make([]string, 0, len(rawRoutes))
	descs := make(map[string]string, len(rawRoutes))
	routes := make([]map[string]interface{}, 0, len(rawRoutes))
	prefixes := make(map[string]string, 8)
	for _, raw := range rawRoutes {
		r, _ := raw.(map[string]interface{})
		prefix, _ := r["prefix"].(string)
		if !strings.HasPrefix(prefix, "/api/") {
			continue
		}
		auth, _ := r["auth"].(string)
		if auth != string(manifest.AuthSession) {
			continue
		}
		mode, _ := r["responseMode"].(string)
		if mode != string(manifest.ResponseJSON) {
			continue
		}
		if isCatalogMetaEndpoint(prefix) {
			continue
		}
		desc, _ := r["detail"].(string)
		if desc == "" {
			desc, _ = r["description"].(string)
		}
		matchMode, _ := r["matchMode"].(string)
		if matchMode == string(manifest.MatchPrefix) {
			// 聚合前缀（模块总入口）：不可直接调用，单独记录供 get_route/call_api 提示子路由
			prefixes[prefix] = desc
			continue
		}
		rawMethods, _ := r["methods"].([]interface{})
		if len(rawMethods) == 0 {
			continue
		}
		methods := make([]string, 0, len(rawMethods))
		for _, m := range rawMethods {
			methods = append(methods, fmt.Sprint(m))
		}
		status, _ := r["status"].(string)
		descs[prefix] = desc
		line := strings.Join(methods, ",") + " " + prefix
		if desc != "" {
			line += " —— " + desc
		}
		if summary := compactSchemaSummary(r); summary != "" {
			line += " 请求体: " + summary
		}
		if status == "retired" {
			line += " [已废弃]"
		}
		lines = append(lines, line)
		routes = append(routes, r)
	}
	sort.Strings(lines)
	s.catalogMu.Lock()
	s.catalogDescs = descs
	s.catalogRoutes = routes
	s.catalogPrefixes = prefixes
	s.catalogMu.Unlock()
	return strings.Join(lines, "\n"), nil
}

// isCatalogMetaEndpoint 判断是否为文档类元接口：清单已完整覆盖接口，禁入清单，
// 避免模型为省事拉全量 api-docs/openapi 造成信息重复与词元浪费；
// ai-access 家族整体排除：其 overview 返回明文 Agent Key（密钥只应在
// 「AI 接入」设置页对人工展示），进入模型上下文即构成凭据泄露面。
func isCatalogMetaEndpoint(prefix string) bool {
	switch prefix {
	case "/api/system/api-docs", "/api/system/openapi.json", "/api/openapi.json":
		return true
	}
	for _, family := range []string{"/api/system/ai-access", "/api/ai-access", "/api/ai/"} {
		if prefix == family || strings.HasPrefix(prefix, family+"/") {
			return true
		}
	}
	return false
}

// compactSchemaSummary 将请求体 JSON Schema 压缩成单行字段摘要：
// 「字段名:类型(必填)(枚举a|b) 说明」多字段以逗号分隔，超出长度截断。
func compactSchemaSummary(r map[string]interface{}) string {
	rawSchema, ok := r["requestSchema"].(map[string]interface{})
	if !ok {
		return ""
	}
	props, _ := rawSchema["properties"].(map[string]interface{})
	if len(props) == 0 {
		return ""
	}
	required := map[string]bool{}
	if rawRequired, ok := rawSchema["required"].([]interface{}); ok {
		for _, item := range rawRequired {
			if name, ok := item.(string); ok {
				required[name] = true
			}
		}
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		field := "「" + name + "」"
		if prop, ok := props[name].(map[string]interface{}); ok {
			if t, ok := prop["type"].(string); ok && t != "" {
				field += ":" + typeLabel(t)
			}
			if required[name] {
				field += " 必填"
			}
			if rawEnum, ok := prop["enum"].([]interface{}); ok && len(rawEnum) > 0 {
				values := make([]string, 0, len(rawEnum))
				for _, v := range rawEnum {
					values = append(values, fmt.Sprint(v))
				}
				field += " 枚举[" + strings.Join(values, "|") + "]"
			}
			if d, ok := prop["description"].(string); ok && d != "" {
				// 说明仅保留前 24 字符（按字符合计，避免截断 UTF-8 序列），
				// 防止长描述挤占字段列表导致关键字段被截断。
				const maxDesc = 24
				if runes := []rune(d); len(runes) > maxDesc {
					d = string(runes[:maxDesc]) + "…"
				}
				field += " " + d
			}
		}
		parts = append(parts, field)
	}
	summary := strings.Join(parts, "，")
	// 单行超长截断，防止清单膨胀超出上下文窗口。
	const maxSummary = 160
	if len(summary) > maxSummary {
		summary = summary[:maxSummary] + "…"
	}
	return summary
}

// typeLabel 把 JSON Schema 类型转成简短中文标签。
func typeLabel(t string) string {
	switch t {
	case "string":
		return "字符串"
	case "integer":
		return "整数"
	case "number":
		return "数字"
	case "boolean":
		return "布尔"
	case "array":
		return "数组"
	case "object":
		return "对象"
	default:
		return t
	}
}

// toolDesc 返回工具调用的中文动作描述（来自接口清单的中文描述，前端工具步骤展示用）。
func (s *Service) toolDesc(toolName, argsJSON string) string {
	switch toolName {
	case "get_system_status":
		return "读取本机系统状态"
	case "get_openapi":
		return "导出 OpenAPI 文档"
	case "list_apis":
		return "读取接口目录"
	case "list_telegram_targets":
		return "列出 Telegram 接收者"
	case "send_telegram_message":
		var args struct {
			ChatID string `json:"chatId"`
			Text   string `json:"text"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &args)
		if args.ChatID != "" {
			text := args.Text
			if r := []rune(text); len(r) > 12 {
				text = string(r[:12]) + "…"
			}
			if text != "" {
				return "发送 TG 消息：" + text
			}
			return "发送 TG 消息"
		}
		return "发送 Telegram 消息"
	case "memory_search":
		var args struct {
			Query string `json:"query"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &args)
		if q := []rune(strings.TrimSpace(args.Query)); len(q) > 0 {
			if len(q) > 12 {
				return "搜索长期记忆：" + string(q[:12]) + "…"
			}
			return "搜索长期记忆：" + string(q)
		}
		return "搜索长期记忆"
	case "memory_add":
		var args struct {
			Content string `json:"content"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &args)
		if c := []rune(strings.TrimSpace(args.Content)); len(c) > 0 {
			if len(c) > 14 {
				return "写入长期记忆：" + string(c[:14]) + "…"
			}
			return "写入长期记忆：" + string(c)
		}
		return "写入长期记忆"
	case "memory_delete":
		return "删除长期记忆"
	}
	if argsJSON == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	path, _ := args["path"].(string)
	if path == "" {
		return ""
	}
	if toolName == "get_route" {
		return "查询接口契约"
	}
	if desc := s.lookupCatalogDesc(path); desc != "" {
		return desc
	}
	// 清单未命中时不回退到 方法+路径：语义视图只展示中文动作描述，
	// 具体路径由前端「路径」视图按 args 自行推导
	return ""
}

// lookupCatalogDesc 按具体路径取接口中文描述：先按原样查（无参路径直接命中），
// 未命中时对清单里的模板路径做逐段匹配（{id} 等参数段通配），使
// /api/aliyun/accounts/1/domains 也能命中 /api/aliyun/accounts/{id}/domains 的描述。
func (s *Service) lookupCatalogDesc(path string) string {
	s.catalogMu.Lock()
	descs := s.catalogDescs
	s.catalogMu.Unlock()
	if desc, ok := descs[path]; ok {
		return desc
	}
	for template, desc := range descs {
		if catalogTemplateMatches(template, path) {
			return desc
		}
	}
	return ""
}

// catalogTemplateMatches 判断具体路径是否匹配模板路径（{...} 段通配，段数必须一致）。
func catalogTemplateMatches(template, path string) bool {
	parts := strings.Split(template, "/")
	target := strings.Split(path, "/")
	if len(parts) != len(target) {
		return false
	}
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			continue
		}
		if part != target[i] {
			return false
		}
	}
	return true
}
