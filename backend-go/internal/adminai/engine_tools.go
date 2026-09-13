package adminai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/adminai/channel"
	"github.com/iwvw/api-monitor/backend-go/internal/manifest"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// adminAITools 是注入 LLM 请求的工具 schema（与 executeToolCall 的工具有一一对应）。
// 注意：接口目录不再以探查工具（list_apis/get_openapi）暴露——系统提示词已内置
// 确定性接口清单（apiCatalogText），避免模型靠猜/试浪费词元；get_route 仅用于查请求体契约。
var adminAITools = []map[string]interface{}{
	{"type": "function", "function": map[string]interface{}{
		"name":        "get_route",
		"description": "读取单个 API 接口的完整契约（请求体 schema、参数、示例）；仅在需要构造请求体时使用，且只能查询系统提示词内置接口清单中列出的路径，查询不存在的路径会直接报错",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "接口路径，如 /api/flyio/apps/{appName}/update-image"},
			},
			"required": []string{"path"},
		},
	}},
	{"type": "function", "function": map[string]interface{}{
		"name":        "get_system_status",
		"description": "读取本机系统运行状态（CPU/内存/磁盘）；displayTime/serverTime 为站点当前时间（本地时区），回答时间/换算 cron 必须用 displayTime 或 serverTime.local，禁止用 timestamp（UTC）",
		"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "required": []string{}},
	}},
	{"type": "function", "function": map[string]interface{}{
		"name":        "call_api",
		"description": "调用系统 API 接口；只能调用系统提示词接口清单中列出的路径与方法（清单已含全部可调用接口），清单之外的路径不存在或不可调用，禁止猜测、拼凑或修改路径，也禁止调用 api-docs/openapi 等文档接口；写操作（非 GET）会进入人工审批，需等待用户批准。写操作执行后必须立即回读 GET 验证真实生效，并检查 success/error 字段，不得凭 2xx 宣告成功",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"method":  map[string]interface{}{"type": "string", "description": "HTTP 方法，默认 GET"},
				"path":    map[string]interface{}{"type": "string", "description": "接口路径，如 /api/cloudflare/zones"},
				"headers": map[string]interface{}{"type": "object", "description": "请求头（选填）"},
				"body":    map[string]interface{}{"type": "object", "description": "JSON 请求体（选填）"},
			},
			"required": []string{"path"},
		},
	}},
	{"type": "function", "function": map[string]interface{}{
		"name":        "list_telegram_targets",
		"description": "列出可接收消息的 Telegram 接收者（频道 + 已绑定用户），用于主动推送简报/通知",
		"parameters":  map[string]interface{}{"type": "object", "properties": map[string]interface{}{}, "required": []string{}},
	}},
	{"type": "function", "function": map[string]interface{}{
		"name":        "send_telegram_message",
		"description": "向指定 Telegram 接收者发送消息（channelId + chatId 来自 list_telegram_targets）；用于主动推送简报",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"channelId": map[string]interface{}{"type": "string", "description": "频道 ID（如 telegram）"},
				"chatId":    map[string]interface{}{"type": "string", "description": "接收者 chatId"},
				"text":      map[string]interface{}{"type": "string", "description": "消息文本（MarkdownV2 语法）"},
			},
			"required": []string{"channelId", "chatId", "text"},
		},
	}},
		{"type": "function", "function": map[string]interface{}{
			"name":        "memory_search",
			"description": "搜索长期记忆（跨会话持久事实、用户偏好、历史决策，支持中文模糊检索与触发词标签命中）；回答涉及历史决策、环境偏好、曾做过的配置或用户习惯之前，先调用它。query 用记忆原文措辞或触发词更易命中；若用户明确给出记忆标签（如「用 xx 标签搜索记忆」），用 tags 参数按触发词精确检索。返回 0 命中即代表无相关记忆，不要更换关键词重复搜索，直接继续执行",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string", "description": "检索关键词，如「默认模型 网关」"},
					"tags":  map[string]interface{}{"type": "string", "description": "触发词标签（逗号分隔），按记忆的 triggers 精确命中；用户明确指定标签时用（选填）"},
					"limit": map[string]interface{}{"type": "integer", "description": "返回条数上限（默认 6，最大 10）"},
				},
				"required": []string{"query"},
			},
		}},
	{"type": "function", "function": map[string]interface{}{
		"name":        "memory_add",
		"description": "写入一条长期记忆（跨会话保留的用户偏好/环境事实/重要决策）；用户说「记住…」时必须调用，内容要具体到名称/ID/取值；禁止记录可通过系统接口实时查询的动态资源状态（如实例规格、IP、端口、DNS 记录、任务配置、使用量等），这类数据以接口查询为准；内容编辑重发等场景不适用",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content":    map[string]interface{}{"type": "string", "description": "记忆内容，一句话表述，具体化（含名称/ID/取值），最多 500 字"},
				"importance": map[string]interface{}{"type": "integer", "description": "重要性 1-10，默认 5；用户明确要求的偏好给 8 以上"},
				"triggers":   map[string]interface{}{"type": "string", "description": "逗号分隔的触发词，便于日后检索（选填）"},
			},
			"required": []string{"content"},
		},
	}},
	{"type": "function", "function": map[string]interface{}{
		"name":        "memory_delete",
		"description": "删除一条长期记忆（按 id）；用户说「忘了/删掉那条记忆」时先用 memory_search 找到 id 再删除",
		"parameters": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]interface{}{"type": "string", "description": "记忆条目 id"},
			},
			"required": []string{"id"},
		},
	}},
}

func (s *Service) executeToolCall(ctx context.Context, db *sql.DB, toolName string, args map[string]interface{}, sessionID, tcID string, eventCh chan SSEEvent) (interface{}, error) {
	switch toolName {
	case "list_apis", "get_route", "get_openapi", "get_ai_manifest", "get_system_status":
		return s.executeReadOnlyTool(ctx, toolName, args)
	case "call_api":
		return s.executeCallAPITool(ctx, db, args, sessionID, tcID, eventCh)
	case "list_telegram_targets":
		return s.listTelegramTargets(ctx)
	case "send_telegram_message":
		return s.sendTelegramMessage(ctx, args)
	case "memory_search":
		return s.executeMemorySearch(ctx, db, args)
	case "memory_add":
		return s.executeMemoryAdd(ctx, db, args, sessionID)
	case "memory_delete":
		return s.executeMemoryDelete(ctx, db, args)
	default:
		return nil, fmt.Errorf("未知工具: %s", toolName)
	}
}

// executeReadOnlyTool 执行只读工具调用并通过 aiCaller 回环。
// get_route 走本地契约缓存（buildCatalogText 时构建），返回单条完整契约
// （含请求体 schema、字段类型/必填/枚举、参数、示例），不再回退全量 api-docs。
func (s *Service) executeReadOnlyTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	switch toolName {
	case "get_route":
		path, _ := args["path"].(string)
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, fmt.Errorf("path 不能为空")
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		if s.aiCaller == nil {
			return nil, fmt.Errorf("AI 调用器未配置")
		}
		s.catalogMu.Lock()
		routes := s.catalogRoutes
		prefixes := s.catalogPrefixes
		s.catalogMu.Unlock()
		if len(routes) == 0 {
			if _, err := s.buildCatalogText(ctx); err != nil {
				return nil, err
			}
			s.catalogMu.Lock()
			routes = s.catalogRoutes
			prefixes = s.catalogPrefixes
			s.catalogMu.Unlock()
		}
		// 聚合前缀（模块总入口）不可调用：直接报错并列出子路由，
		// 避免返回「假契约」诱导模型据此发起调用（审计实证：GET /api/scheduler 404）。
		// 与 validateCallAPIPath 保持一致：匹配前先剥离 query string，
		// 避免模型按 call_api 习惯带 ?page=1 时误报「路径不存在」。
		matchPath := path
		if i := strings.IndexByte(matchPath, '?'); i >= 0 {
			matchPath = matchPath[:i]
		}
		// 尾斜杠归一化（保留根路径 "/"）：与 call_api 预检一致，
		// 避免同一路径在 get_route 判「不存在」而 call_api 放行的矛盾。
		if len(matchPath) > 1 {
			matchPath = strings.TrimRight(matchPath, "/")
		}
		if desc, ok := prefixes[matchPath]; ok {
			children := s.catalogChildrenOf(matchPath)
			hint := "该路径是聚合前缀（模块总入口"
			if desc != "" {
				hint += "：" + desc
			}
			hint += "），不可直接调用；"
			if children != "" {
				hint += "请改用其具体子路由，例如：" + children
			} else {
				hint += "请从系统提示词接口清单中选择以该前缀开头的具体接口"
			}
			return nil, fmt.Errorf("路径 %s 未命中可调用接口：%s", path, hint)
		}
		contract := routeContractFromCache(routes, matchPath)
		if contract == nil {
			return nil, fmt.Errorf("API 路由不存在: %s（请对照系统提示词内置的接口清单选择真实路径，禁止猜测清单之外的路径）", path)
		}
		return contract, nil
	}

	if s.aiCaller == nil {
		return nil, fmt.Errorf("AI 调用器未配置")
	}
	path := ""
	switch toolName {
	case "list_apis":
		path = "/api/system/ai-access"
	case "get_openapi":
		path = "/api/system/openapi.json"
	case "get_ai_manifest":
		path = "/api/system/ai-access"
	case "get_system_status":
		path = "/api/system/host-metrics"
	}
	if path == "" {
		return nil, fmt.Errorf("未知只读工具: %s", toolName)
	}
	resp, err := s.aiCaller(ctx, systemmetrics.AICallRequest{Method: http.MethodGet, Path: path})
	if err != nil {
		return nil, err
	}
	return aiCallResult(resp)
}

func (s *Service) executeCallAPITool(ctx context.Context, db *sql.DB, args map[string]interface{}, sessionID, tcID string, eventCh chan SSEEvent) (interface{}, error) {
	method, _ := args["method"].(string)
	path, _ := args["path"].(string)
	if method == "" {
		method = http.MethodGet
	}
	if path == "" {
		return nil, fmt.Errorf("path 不能为空")
	}

	// 契约预检（防臆造路径）：本地清单校验路径与方法，不命中的请求不发起真实
	// HTTP（省掉触发 404 的一整轮 LLM 往返），并给出可操作的修正路径/方法提示。
	if hint, ok := s.validateCallAPIPath(method, path); !ok {
		return nil, errors.New(hint)
	}

	headers := map[string]string{}
	if rawHeaders, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range rawHeaders {
			headers[k] = fmt.Sprint(v)
		}
	}
	var body json.RawMessage
	if rawBody, ok := args["body"]; ok && rawBody != nil {
		encoded, _ := json.Marshal(rawBody)
		body = encoded
	}

	isWrite := method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
	// fullApprove 标记本次写操作是否在「完全批准」语境下被自动放行：
	// 开启时向目标接口注入内部头 X-Admin-AI-Full-Approve，供命令执行等受
	// 危险操作拦截的端点识别（配合 X-AI-Agent 内部头放行危险操作）。
	fullApprove := false
	if isWrite {
		// 完全批准模式（admin_ai_auto_approve）：所有写操作免审批直接执行，
		// 不再弹审批卡片；对模型标记「已自动批准」避免困惑。
		autoApprove, err := s.getAutoApprove(ctx, db)
		if err != nil {
			return nil, err
		}
		if !autoApprove {
			// 定时 AI 任务（X-Internal-Cron）策略：readonly 时写操作直接拒绝；
			// allow 时该次执行内写操作免审批，但「写操作全局开关」是硬底线，
			// 必须先校验通过（关闭时拒绝），不得借定时任务绕过。
			s.mu.Lock()
			runID := s.sessionRuns[sessionID]
			policy := s.runPolicy[runID]
			s.mu.Unlock()
			if policy == "readonly" {
				return nil, fmt.Errorf("readonly 策略禁止写操作")
			}
			if policy == "allow" {
				writeAllowed, err := s.getWriteEnabled(ctx, db)
				if err != nil {
					return nil, err
				}
				if !writeAllowed {
					return nil, fmt.Errorf("写操作未启用")
				}
				autoApprove = true
			}
		}
		if !autoApprove {
			// 会话级写授权（“允许此对话”）优先于全局开关，授权后本会话后续写操作免审批
			sessionWrite, err := s.isSessionWriteEnabled(ctx, db, sessionID)
			if err != nil {
				return nil, err
			}
			if !sessionWrite {
				writeAllowed, err := s.getWriteEnabled(ctx, db)
				if err != nil {
					return nil, err
				}
				if !writeAllowed {
					return nil, fmt.Errorf("写操作未启用")
				}

				planSummary := fmt.Sprintf("执行 %s %s", method, path)
				approvalID, _ := randomID("aaa_")
				expiresAt := time.Now().UTC().Add(approvalTTL).Format(time.RFC3339)
				now := time.Now().UTC().Format(time.RFC3339)

				// 先注册等待 channel 再落库/发事件：用户批准可能在任何时刻到达，
				// 若注册在 INSERT 之后，期间的决议会被 resolveApproval 静默丢弃。
				approvalCh := make(chan approvalResolution, 1)
				s.mu.Lock()
				s.approval[approvalID] = approvalCh
				s.mu.Unlock()

				// 落库走忙锁重试；失败必须撤销已注册的等待通道并立即返回错误：
				// 否则执行会静默挂起等待 30 分钟，而用户批准时因无 pending 行必然 409。
				if err := execBusyRetry(ctx, db,
					`INSERT INTO admin_ai_approvals (id, session_id, tool_call_id, status, plan_summary, method, path, body_snapshot, expires_at, created_at) VALUES (?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?)`,
					approvalID, sessionID, tcID, planSummary, method, path, string(body), expiresAt, now); err != nil {
					s.mu.Lock()
					delete(s.approval, approvalID)
					s.mu.Unlock()
					return nil, fmt.Errorf("创建审批记录失败: %w", err)
				}

				s.emit(eventCh, SSEEvent{Type: "approval_required", Fields: map[string]interface{}{
					"approvalId":   approvalID,
					"planSummary":  planSummary,
					"expiresAt":    expiresAt,
					"method":       method,
					"path":         path,
					"bodySnapshot": string(body),
				}})

				defer func() {
					s.mu.Lock()
					delete(s.approval, approvalID)
					s.mu.Unlock()
				}()

				select {
				case res := <-approvalCh:
					if res.Action != "approve" {
						_, _ = db.ExecContext(ctx, "UPDATE admin_ai_approvals SET status = 'rejected' WHERE id = ? AND status = 'pending'", approvalID)
						if res.Reason != "" {
							return nil, fmt.Errorf("写操作审批被拒绝（用户请求更改：%s）", res.Reason)
						}
						return nil, fmt.Errorf("写操作审批被拒绝")
					}
				case <-ctx.Done():
					return nil, fmt.Errorf("等待审批时执行已超时或取消")
				case <-time.After(approvalTTL):
					_, _ = db.ExecContext(ctx, "UPDATE admin_ai_approvals SET status = 'expired' WHERE id = ? AND status = 'pending'", approvalID)
					return nil, fmt.Errorf("审批已超时，写操作未执行")
				}
			}
		}
		// 走到此处即写操作已被授权（完全批准 / cron allow / 会话级授权 / 用户批准）。
		// 注入内部头，允许命令执行等受危险操作拦截的端点在本调用中放行危险命令。
		fullApprove = true
	}

	if fullApprove {
		headers[adminAIFullApproveHeader] = "true"
	}

	resp, err := s.aiCaller(ctx, systemmetrics.AICallRequest{
		Method: method, Path: path, Headers: headers, Body: body,
	})
	if err != nil {
		return nil, err
	}
	return aiCallResult(resp)
}

// runToolWithRetry 执行一次工具调用并应用失败重试（仅幂等只读工具在偶发故障时重试，
// 写操作/审批拒绝/参数错误不重试）；与串行路径共用同一套重试语义。
func (s *Service) runToolWithRetry(ctx context.Context, db *sql.DB, toolName string, args map[string]interface{}, sessionID, tcID string, eventCh chan SSEEvent) (interface{}, error) {
	var result interface{}
	var callErr error
	for attempt := 0; attempt <= maxToolRetries; attempt++ {
		result, callErr = s.executeToolCall(ctx, db, toolName, args, sessionID, tcID, eventCh)
		callErr = sanitizeToolError(callErr)
		if callErr == nil || !retryableToolError(callErr) || !toolCallIdempotent(toolName, args) || attempt == maxToolRetries {
			break
		}
		slog.Warn("tool-retry", "tool", toolName, "attempt", attempt+1, "err", callErr.Error())
		select {
		case <-time.After(time.Duration(attempt+1) * 500 * time.Millisecond):
		case <-ctx.Done():
			break
		}
	}
	return result, callErr
}

// toolParallelSafe 判定工具调用可进入同轮并行段：纯内存只读（契约/清单缓存）
// 与 call_api 的幂等 HTTP 方法；DB 工具（memory_*/telegram_*）与写操作
// 保持串行，避免并发写库或副作用竞态。
func toolParallelSafe(toolName string, args map[string]interface{}) bool {
	if toolName == "call_api" {
		method, _ := args["method"].(string)
		switch strings.ToUpper(method) {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return true
		default:
			return false
		}
	}
	switch toolName {
	case "list_apis", "get_route", "get_openapi", "get_ai_manifest", "get_system_status":
		return true
	}
	return false
}

// toolIsCacheable 判定工具调用是否可走同轮去重缓存：
// 只读工具（get_route 等）与 call_api 的 GET/HEAD/OPTIONS 幂等，可缓存；
// 写方法（POST/PUT/DELETE，含审批链）有副作用，禁止缓存。
func toolIsCacheable(toolName string, args map[string]interface{}) bool {
	if toolName != "call_api" {
		return true
	}
	if method, ok := args["method"].(string); ok {
		switch strings.ToUpper(method) {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			return true
		default:
			return false
		}
	}
	return true // 未显式指定方法时 call_api 默认 GET
}

// toolCacheKey 构造同轮去重缓存的键：工具名 + 规范化参数（JSON 稳定序列化，
// map 键排序由 encoding/json 保证），相同参数的并行重复调用命中同一键。
func toolCacheKey(toolName string, args map[string]interface{}) string {
	raw, err := json.Marshal(args)
	if err != nil {
		raw = []byte(fmt.Sprintf("%v", args))
	}
	return toolName + "|" + string(raw)
}

// catalogChildrenOf 返回聚合前缀下的具体子路由提示（最多 5 条），
// 用于 get_route/call_api 命中聚合前缀时纠正模型改用真实子接口。
func (s *Service) catalogChildrenOf(prefix string) string {
	s.catalogMu.Lock()
	routes := s.catalogRoutes
	s.catalogMu.Unlock()
	var children []string
	for _, r := range routes {
		p, _ := r["prefix"].(string)
		if strings.HasPrefix(p, prefix+"/") {
			children = append(children, p)
			if len(children) >= 5 {
				break
			}
		}
	}
	return strings.Join(children, "、")
}

// validateCallAPIPath 本地校验 call_api 的目标路径与方法是否在确定性清单中：
// 聚合前缀直接拒绝（给出子路由提示）；清单（JSON session 接口）内路径做方法
// 校验；不在清单但 manifest 中存在（如 ResponseProxy 的 R2 下载、public 路由等
// 非 JSON 接口）放行，保持旧行为由真实 HTTP 层兜底——预检只负责拦截「必 404」
// 的臆造路径，不缩小合法调用面。清单未构建（catalogRoutes 为空）时也放行。
func (s *Service) validateCallAPIPath(method, path string) (string, bool) {
	s.catalogMu.Lock()
	routes := s.catalogRoutes
	prefixes := s.catalogPrefixes
	s.catalogMu.Unlock()
	if len(routes) == 0 {
		return "", true
	}
	p := path
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	// 尾斜杠归一化（保留根路径 "/"）：与 get_route 一致
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	if desc, ok := prefixes[p]; ok {
		children := s.catalogChildrenOf(p)
		hint := "路径 " + path + " 是聚合前缀（模块总入口"
		if desc != "" {
			hint += "：" + desc
		}
		hint += "），不可直接调用；"
		if children != "" {
			hint += "请改用其具体子路由，例如：" + children
		} else {
			hint += "请从接口清单中选择以该前缀开头的具体接口"
		}
		return hint, false
	}
	best := routeContractFromCache(routes, p)
	if best == nil {
		// 不在清单但 manifest 中存在（非 JSON 响应接口，如 R2 下载代理/
		// public 路由）：保留旧行为放行，由真实 HTTP 层校验（与 callAPIFromAI
		// 的 manifest.Match + Owner/ResponseMode 限制一致）。
		if route, ok := manifest.Match(p); ok {
			if route.Owner != manifest.OwnerGo {
				return "接口不可调用: " + p + "（非本端路由）", false
			}
			if route.ResponseMode == manifest.ResponseStream || route.ResponseMode == manifest.ResponseWebSocket {
				return "流式或 WebSocket 接口不允许通过 AI 直接调用: " + p, false
			}
			if route.Auth == manifest.AuthAPIKey || route.Auth == manifest.AuthAgent {
				return "该接口需要专用密钥鉴权（" + string(route.Auth) + "），不允许通过 AI 直接调用: " + p, false
			}
			return "", true
		}
		return "路径 " + path + " 不在可调用接口清单中（不存在或不可调用）：请对照系统提示词内置的接口清单选择真实路径与正确方法，禁止猜测或拼凑路径", false
	}
	// 契约命中但属密钥鉴权路由（AuthAPIKey/AuthAgent）：AI 无对应凭据，
	// 且触发仅限本机/网关专用端点有越权风险，一律拒绝。
	if auth, _ := best["auth"].(string); auth == string(manifest.AuthAPIKey) || auth == string(manifest.AuthAgent) {
		return "该接口需要专用密钥鉴权，不允许通过 AI 直接调用: " + p, false
	}
	if method == "" {
		method = http.MethodGet
	}
	methods, _ := best["methods"].([]interface{})
	methodOK := false
	for _, m := range methods {
		if strings.EqualFold(fmt.Sprint(m), method) {
			methodOK = true
			break
		}
	}
	if !methodOK {
		var list []string
		for _, m := range methods {
			list = append(list, fmt.Sprint(m))
		}
		return fmt.Sprintf("接口 %s 不支持 %s 方法；其真实可用方法为 %s，请改用正确方法（可先用 get_route 读取契约确认）", path, method, strings.Join(list, "/")), false
	}
	return "", true
}

// routeContractFromCache 从契约缓存中匹配具体路径，返回该路由的完整契约视图。
// 匹配规则：exact 精确相等；pattern 按 {param} 通配且段数一致；其余按前缀。多命中取字面量更具体者。
func routeContractFromCache(routes []map[string]interface{}, path string) map[string]interface{} {
	best := (map[string]interface{})(nil)
	for _, r := range routes {
		prefix, _ := r["prefix"].(string)
		mode, _ := r["matchMode"].(string)
		if !catalogRouteMatches(prefix, mode, path) {
			continue
		}
		if best == nil || manifest.CompareRouteSpecificity(prefix, best["prefix"].(string)) > 0 {
			best = r
		}
	}
	if best == nil {
		return nil
	}
	desc, _ := best["detail"].(string)
	if desc == "" {
		desc, _ = best["description"].(string)
	}
	return map[string]interface{}{
		"path":               best["prefix"],
		"matchedPath":        path,
		"methods":            best["methods"],
		"group":              best["group"],
		"module":             best["module"],
		"auth":               best["auth"],
		"responseMode":       best["responseMode"],
		"matchMode":          best["matchMode"],
		"status":             best["status"],
		"description":        desc,
		"pathParams":         best["pathParams"],
		"queryParams":        best["queryParams"],
		"headers":            best["headers"],
		"requestContentType": best["requestContentType"],
		"requestSchema":      best["requestSchema"],
		"requestExample":     best["requestExample"],
		"responseExample":    best["responseExample"],
		"notes":              best["notes"],
	}
}

// catalogRouteMatches 判断具体路径是否命中缓存路由：
// exact 精确相等；pattern 按 {param} 段通配（段数一致）；prefix 则前缀匹配。
func catalogRouteMatches(prefix, mode, path string) bool {
	switch mode {
	case "exact":
		return path == prefix
	case "pattern":
		return catalogTemplateMatches(prefix, path)
	default:
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
}

// listTelegramTargets 列出 Telegram 频道的绑定接收者（channelId + chatId），
// 供 send_telegram_message 工具构造推送目标。未配置频道或未初始化时返回空列表。
func (s *Service) listTelegramTargets(ctx context.Context) (interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx,
		`SELECT b.channel_id, b.channel_user_id, COALESCE(b.channel_username,''), COALESCE(b.panel_user_id,''), b.created_at
		 FROM admin_ai_channel_bindings b JOIN admin_ai_channels c ON c.id = b.channel_id
		 WHERE c.type = 'telegram' AND c.enabled = 1
		 ORDER BY b.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type target struct {
		ChannelID string `json:"channelId"`
		ChatID    string `json:"chatId"`
		Username  string `json:"username,omitempty"`
		PanelID   string `json:"panelUserId,omitempty"`
		CreatedAt string `json:"createdAt"`
	}
	targets := make([]target, 0)
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.ChannelID, &t.ChatID, &t.Username, &t.PanelID, &t.CreatedAt); err != nil {
			continue
		}
		targets = append(targets, t)
	}
	return map[string]interface{}{"targets": targets, "count": len(targets)}, nil
}

// sendTelegramMessage 通过已注册的 Telegram 频道向指定 chatId 发送消息。
// 参数：channelId（频道配置 id，为空时自动选一个 telegram 频道）、chatId（接收者唯一键）、text。
func (s *Service) sendTelegramMessage(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if s.chanMgr == nil {
		return nil, fmt.Errorf("频道未初始化")
	}
	channelID, _ := args["channelId"].(string)
	chatID, _ := args["chatId"].(string)
	text, _ := args["text"].(string)
	if channelID == "" {
		// 多频道时代仍允许省略 channelId：从注册表里挑第一个 telegram 频道
		best := ""
		for _, ch := range s.chanMgr.registry.All() {
			if strings.HasPrefix(ch.ID(), "aac_") {
				best = ch.ID()
				break
			}
		}
		channelID = best
	}
	if chatID == "" {
		return nil, fmt.Errorf("chatId 不能为空")
	}
	if text == "" {
		return nil, fmt.Errorf("text 不能为空")
	}
	ch, ok := s.chanMgr.registry.Get(channelID)
	if !ok {
		return nil, fmt.Errorf("频道未注册: %s", channelID)
	}
	msgID, err := ch.Send(ctx, chatID, channel.OutboundMessage{Text: text})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"ok": true, "messageId": msgID, "channelId": channelID, "chatId": chatID}, nil
}

// retryableToolError 决定工具调用失败是否值得重试：
// 审批类（拒绝/未启用/超时）与参数类（4xx）是确定结果，重试无意义；网络/5xx 等偶发故障才重试。
// 注意：是否重试还需同时满足幂等（toolCallIdempotent），写操作不因偶发故障重试。
func retryableToolError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "审批") || strings.Contains(msg, "未启用") {
		return false
	}
	if strings.Contains(msg, "HTTP 4") {
		return false
	}
	return true
}

// toolCallIdempotent 判断工具调用是否幂等（只读/无副作用），决定失败后能否自动重试：
// 写操作（call_api 非 GET/HEAD/OPTIONS、send_telegram_message 等）失败不重试，
// 避免"服务端已提交但响应丢失"时重复创建/删除资源，或重复插入同一审批再次打扰用户。
func toolCallIdempotent(toolName string, args map[string]interface{}) bool {
	if toolName != "call_api" {
		return toolName != "send_telegram_message"
	}
	return toolIsCacheable(toolName, args)
}

// toolLoopFingerprint 计算工具调用指纹：call_api 只读（GET/HEAD）仅取 method+path，
// 用于捕获"轮询同一接口"式风暴；写操作（POST/PUT/PATCH/DELETE）纳入 body 摘要，
// 不同诉求的重发不被误判成循环阻断（同 body 的同路径重复写仍视为循环）。
func toolLoopFingerprint(toolName string, args map[string]interface{}) string {
	if toolName == "call_api" {
		method, _ := args["method"].(string)
		if method == "" {
			method = "GET"
		}
		path, _ := args["path"].(string)
		fp := "call_api|" + strings.ToUpper(method) + "|" + path
		if method != "GET" && method != "HEAD" {
			body, _ := args["body"].(map[string]interface{})
			if body != nil {
				if raw, err := json.Marshal(body); err == nil {
					fp += "|body:" + string(raw)
				}
			}
		}
		return fp
	}
	raw, err := json.Marshal(args)
	if err != nil {
		raw = []byte(fmt.Sprintf("%v", args))
	}
	return toolName + "|" + string(raw)
}

// toolLoopCheck 跨轮重复调用计数：返回（是否允许执行, 累计次数）。
// s.mu 保护 toolLoops；runInference 结束后由 clearToolLoops 清理该 run 的计数。
func (s *Service) toolLoopCheck(runID, toolName string, args map[string]interface{}) (bool, int) {
	key := runID + "|" + toolLoopFingerprint(toolName, args)
	s.mu.Lock()
	count := s.toolLoops[key] + 1
	s.toolLoops[key] = count
	s.mu.Unlock()
	return count < toolLoopBlockThreshold, count
}

// clearToolLoops 清理指定 run 的循环计数（run 结束调用，防计数跨执行累积）。
func (s *Service) clearToolLoops(runID string) {
	prefix := runID + "|"
	s.mu.Lock()
	for k := range s.toolLoops {
		if strings.HasPrefix(k, prefix) {
			delete(s.toolLoops, k)
		}
	}
	s.mu.Unlock()
}

// sanitizeToolError 清洗进入 LLM 上下文/审计的错误文本：剥离控制字符（防 prompt 注入），
// 截断超长文本。错误语义不变（retryableToolError 依赖的「审批/未启用/HTTP 4」关键词保留）。
func sanitizeToolError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	var sb strings.Builder
	sb.Grow(len(msg))
	cleaned := false
	for _, r := range msg {
		if r == '\n' || r == '\t' || r >= 0x20 {
			sb.WriteRune(r)
		} else {
			cleaned = true
		}
	}
	out := sb.String()
	runes := []rune(out)
	if len(runes) > toolErrorMaxChars {
		out = string(runes[:toolErrorMaxChars]) + "…"
		cleaned = true
	}
	if !cleaned {
		return err
	}
	return errors.New(out)
}

// toolErrorText 提取工具错误的可读文本（教训沉淀用，去掉敏感参数与长堆栈）。
func toolErrorText(err error) string {
	if err == nil {
		return ""
	}
	text := sanitizeToolError(err).Error()
	if runes := []rune(text); len(runes) > lessonMaxErrChars {
		text = string(runes[:lessonMaxErrChars])
	}
	return text
}
