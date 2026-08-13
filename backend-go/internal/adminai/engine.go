package adminai

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

const (
	maxToolCalls     = 12
	runTimeout       = 5 * time.Minute
	contentSizeLimit = 64 * 1024
	eventChBuffer    = 128
)

// SSEEvent 是 RunLoop 下推给 SSE 消费方的事件，MarshalJSON 将 Fields 与 type 合并进 JSON 对象。
type SSEEvent struct {
	Type   string                 `json:"type"`
	Fields map[string]interface{} `json:"-"`
}

func (e SSEEvent) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{}, len(e.Fields)+1)
	m["type"] = e.Type
	for k, v := range e.Fields {
		m[k] = v
	}
	return json.Marshal(m)
}

// RunLoop 创建一个运行中的执行并立即返回 runId；推理过程在后台 goroutine 中执行，
// 事件通过通道下推（由 stream.go 的 SSE handler 消费）。
func (s *Service) RunLoop(ctx context.Context, source, sessionID, prompt, identityJSON, modelHint string) (string, error) {
	if s.aiCaller == nil {
		return "", fmt.Errorf("AI 调用器未配置，请检查服务接线")
	}

	runID, err := randomID("aae_")
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	s.sessionRuns[sessionID] = runID
	s.mu.Unlock()

	eventCh := make(chan SSEEvent, eventChBuffer)
	s.mu.Lock()
	s.runs[runID] = eventCh
	s.mu.Unlock()

	go s.runInference(ctx, runID, sessionID, source, prompt, identityJSON, modelHint, eventCh)
	return runID, nil
}

// runInference 执行推理主循环（会话载入、历史收集、LLM 调用、工具调用、回填、落库与事件推送）。
func (s *Service) runInference(ctx context.Context, runID, sessionID, source, prompt, identityJSON, modelHint string, eventCh chan SSEEvent) {
	defer func() {
		s.mu.Lock()
		delete(s.sessionRuns, sessionID)
		if ch, exists := s.runs[runID]; exists {
			close(ch)
			delete(s.runs, runID)
		}
		s.mu.Unlock()
	}()

	s.emit(eventCh, SSEEvent{Type: "meta", Fields: map[string]interface{}{"sessionId": sessionID, "runId": runID}})

	runCtx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	db, err := s.open(runCtx)
	if err != nil {
		s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": err.Error()}})
		return
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)

	sessionModel := modelHint
	if sessionModel == "" {
		sessionModel = s.cfg.AdminAIDefaultModel
	}
	var existingModel string
	err = db.QueryRowContext(runCtx, "SELECT COALESCE(model,'') FROM admin_ai_sessions WHERE id = ?", sessionID).Scan(&existingModel)
	if err == sql.ErrNoRows {
		_, err = db.ExecContext(runCtx,
			`INSERT INTO admin_ai_sessions (id, source, title, model, write_enabled, identity_json, created_at, updated_at, last_activity_at) VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?)`,
			sessionID, source, "", sessionModel, identityJSON, now, now, now)
		if err != nil {
			s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": fmt.Sprintf("创建会话失败: %v", err)}})
			return
		}
	} else if err != nil {
		s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": fmt.Sprintf("查询会话失败: %v", err)}})
		return
	} else if existingModel != "" && sessionModel == "" {
		sessionModel = existingModel
	}
	_, _ = db.ExecContext(runCtx, "UPDATE admin_ai_sessions SET last_activity_at = ?, updated_at = ? WHERE id = ?", now, now, sessionID)

	userMsgID, err := randomID("aam_")
	if err != nil {
		s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": err.Error()}})
		return
	}
	_, err = db.ExecContext(runCtx,
		`INSERT INTO admin_ai_messages (id, session_id, role, content, created_at) VALUES (?, ?, 'user', ?, ?)`,
		userMsgID, sessionID, prompt, now)
	if err != nil {
		s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": fmt.Sprintf("写入用户消息失败: %v", err)}})
		return
	}

	llmModel := sessionModel
	if llmModel == "" {
		llmModel = "default"
	}
	_, err = db.ExecContext(runCtx,
		`INSERT INTO admin_ai_executions (id, session_id, source, status, llm_model, started_at) VALUES (?, ?, ?, 'running', ?, ?)`,
		runID, sessionID, source, llmModel, now)
	if err != nil {
		s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": fmt.Sprintf("创建执行记录失败: %v", err)}})
		return
	}

	type historyMsg struct {
		Role    string
		Content string
	}
	historyRows, err := db.QueryContext(runCtx,
		`SELECT role, COALESCE(content,'') FROM admin_ai_messages WHERE session_id = ? ORDER BY created_at ASC, id ASC`,
		sessionID)
	if err != nil {
		s.finishExecution(db, runID, "error", 0, llmModel, 0, 0, err.Error())
		s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": err.Error()}})
		return
	}
	messages := make([]historyMsg, 0)
	for historyRows.Next() {
		var m historyMsg
		if err := historyRows.Scan(&m.Role, &m.Content); err == nil {
			messages = append(messages, m)
		}
	}
	historyRows.Close()

	var totalPromptTokens, totalCompletionTokens int
	toolCount := 0

	for {
		select {
		case <-runCtx.Done():
			msg := "执行超时或已取消"
			s.finishExecution(db, runID, "cancelled", toolCount, llmModel, totalPromptTokens, totalCompletionTokens, msg)
			s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": msg}})
			return
		default:
		}

		llmMessages := make([]map[string]interface{}, 0, len(messages)+1)
		llmMessages = append(llmMessages, map[string]interface{}{
			"role":    "system",
			"content": "你是 API Monitor 的管理助手。你可以调用系统工具帮助用户管理服务器、Cloudflare、GitHub 等资源。请用中文回答。",
		})
		for _, m := range messages {
			llmMessages = append(llmMessages, map[string]interface{}{"role": m.Role, "content": truncateContent(m.Content)})
		}

		resp, err := s.callLLM(runCtx, llmModel, llmMessages)
		if err != nil {
			s.finishExecution(db, runID, "error", toolCount, llmModel, totalPromptTokens, totalCompletionTokens, err.Error())
			s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": err.Error()}})
			return
		}

		totalPromptTokens += resp.Usage.PromptTokens
		totalCompletionTokens += resp.Usage.CompletionTokens
		s.emitReasoning(eventCh, resp)

		if len(resp.ToolCalls) > 0 {
			for _, tc := range resp.ToolCalls {
				if toolCount >= maxToolCalls {
					s.emit(eventCh, SSEEvent{Type: "error", Fields: map[string]interface{}{"message": fmt.Sprintf("工具调用次数已达上限 %d，执行已结束", maxToolCalls)}})
					s.finishExecution(db, runID, "completed", toolCount, llmModel, totalPromptTokens, totalCompletionTokens, "")
					s.emit(eventCh, SSEEvent{Type: "done", Fields: map[string]interface{}{"messageId": userMsgID, "usage": map[string]int{"promptTokens": totalPromptTokens, "completionTokens": totalCompletionTokens}}})
					return
				}
				toolCount++

				tcMeta, _ := json.Marshal(tc)
				_, _ = db.ExecContext(runCtx,
					`INSERT INTO admin_ai_messages (id, session_id, role, content, tool_call_meta, created_at) VALUES (?, ?, 'assistant', '', ?, ?)`,
					nextID(runCtx, db, "aam_"), sessionID, string(tcMeta), time.Now().UTC().Format(time.RFC3339))
				messages = append(messages, historyMsg{Role: "assistant", Content: ""})

				s.emit(eventCh, SSEEvent{Type: "tool_start", Fields: map[string]interface{}{"toolName": tc.Function.Name, "args": tc.Function.Arguments}})

				tcID, _ := randomID("aatc_")
				tcNow := time.Now().UTC().Format(time.RFC3339)
				_, _ = db.ExecContext(runCtx,
					`INSERT INTO admin_ai_tool_calls (id, execution_id, tool_name, input_json, status, started_at) VALUES (?, ?, ?, ?, 'running', ?)`,
					tcID, runID, tc.Function.Name, tc.Function.Arguments, tcNow)

				var args map[string]interface{}
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)

				result, callErr := s.executeToolCall(runCtx, db, tc.Function.Name, args, sessionID, tcID, eventCh)
				status := "success"
				summary := ""
				if callErr != nil {
					status = "error"
					summary = callErr.Error()
				} else {
					summary = truncateContent(fmt.Sprintf("%v", result))
				}

				s.emit(eventCh, SSEEvent{Type: "tool_result", Fields: map[string]interface{}{"toolName": tc.Function.Name, "status": status, "summary": summary}})

				tcFinished := time.Now().UTC().Format(time.RFC3339)
				_, _ = db.ExecContext(runCtx,
					`UPDATE admin_ai_tool_calls SET status = ?, output_summary = ?, finished_at = ? WHERE id = ?`,
					status, summary, tcFinished, tcID)

				_, _ = db.ExecContext(runCtx,
					`INSERT INTO admin_ai_messages (id, session_id, role, content, created_at) VALUES (?, ?, 'tool', ?, ?)`,
					nextID(runCtx, db, "aam_"), sessionID, summary, tcFinished)
				messages = append(messages, historyMsg{Role: "tool", Content: summary})
			}
			continue
		}

		content := resp.Content
		if content == "" && len(resp.Choices) > 0 {
			content = resp.Choices[0].Message.Content
		}

		assistantMsgID := nextID(runCtx, db, "aam_")
		_, _ = db.ExecContext(runCtx,
			`INSERT INTO admin_ai_messages (id, session_id, role, content, created_at) VALUES (?, ?, 'assistant', ?, ?)`,
			assistantMsgID, sessionID, content, time.Now().UTC().Format(time.RFC3339))

		s.emit(eventCh, SSEEvent{Type: "delta", Fields: map[string]interface{}{"text": content}})

		s.finishExecution(db, runID, "completed", toolCount, llmModel, totalPromptTokens, totalCompletionTokens, "")
		s.emit(eventCh, SSEEvent{Type: "done", Fields: map[string]interface{}{"messageId": assistantMsgID, "usage": map[string]int{"promptTokens": totalPromptTokens, "completionTokens": totalCompletionTokens}}})
		return
	}
}

func (s *Service) emit(ch chan<- SSEEvent, event SSEEvent) {
	select {
	case ch <- event:
	default:
	}
}

func nextID(ctx context.Context, db *sql.DB, prefix string) string {
	id, err := randomID(prefix)
	if err != nil {
		return fmt.Sprintf("%s_%d", prefix, time.Now().UnixMilli())
	}
	return id
}

type llmResponse struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	Choices          []struct {
		Message struct {
			Content          string     `json:"content"`
			ReasoningContent string     `json:"reasoning_content"`
			ToolCalls        []toolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	ToolCalls []toolCall `json:"tool_calls,omitempty"`
	Usage     struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// callLLM 通过本机网关 HTTP 调用 chat/completions（简化版非流式，v1 不做感知厂商差异）。
func (s *Service) callLLM(ctx context.Context, model string, messages []map[string]interface{}) (*llmResponse, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	var gatewayKey string
	err = db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'admin_ai_gateway_key'").Scan(&gatewayKey)
	if err != nil {
		return nil, fmt.Errorf("未配置管理 AI 网关密钥，请在「管理 AI 设置」中配置")
	}

	reqBody := map[string]interface{}{"model": model, "messages": messages}
	bodyBytes, _ := json.Marshal(reqBody)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.Port)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+gatewayKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("LLM 调用失败: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("LLM 调用失败 (HTTP %d): %s", resp.StatusCode, truncateContent(string(raw)))
	}

	var llmResp llmResponse
	if err := json.Unmarshal(raw, &llmResp); err != nil {
		return nil, fmt.Errorf("解析 LLM 响应失败: %w", err)
	}
	if len(llmResp.Choices) > 0 {
		llmResp.ToolCalls = llmResp.Choices[0].Message.ToolCalls
		llmResp.Content = llmResp.Choices[0].Message.Content
		llmResp.ReasoningContent = llmResp.Choices[0].Message.ReasoningContent
	}
	return &llmResp, nil
}

// emitReasoning 若 LLM 返回了思维链文本则下推 reasoning 事件（供前端折叠展示）。
func (s *Service) emitReasoning(eventCh chan<- SSEEvent, resp *llmResponse) {
	if resp == nil || resp.ReasoningContent == "" {
		return
	}
	s.emit(eventCh, SSEEvent{Type: "reasoning", Fields: map[string]interface{}{"text": resp.ReasoningContent}})
}

func (s *Service) executeToolCall(ctx context.Context, db *sql.DB, toolName string, args map[string]interface{}, sessionID, tcID string, eventCh chan<- SSEEvent) (interface{}, error) {
	switch toolName {
	case "list_apis", "get_route", "get_openapi", "get_ai_manifest", "get_system_status":
		return s.executeReadOnlyTool(ctx, toolName, args)
	case "call_api":
		return s.executeCallAPITool(ctx, db, args, sessionID, tcID, eventCh)
	default:
		return nil, fmt.Errorf("未知工具: %s", toolName)
	}
}

func (s *Service) executeReadOnlyTool(ctx context.Context, toolName string, args map[string]interface{}) (interface{}, error) {
	if s.aiCaller == nil {
		return nil, fmt.Errorf("AI 调用器未配置")
	}
	path := ""
	switch toolName {
	case "list_apis":
		path = "/api/system/ai-access"
	case "get_route":
		path = "/api/system/api-docs"
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
	return s.aiCaller(ctx, systemmetrics.AICallRequest{Method: http.MethodGet, Path: path})
}

func (s *Service) executeCallAPITool(ctx context.Context, db *sql.DB, args map[string]interface{}, sessionID, tcID string, eventCh chan<- SSEEvent) (interface{}, error) {
	method, _ := args["method"].(string)
	path, _ := args["path"].(string)
	if method == "" {
		method = http.MethodGet
	}
	if path == "" {
		return nil, fmt.Errorf("path 不能为空")
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
	if isWrite {
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
		_, _ = db.ExecContext(ctx,
			`INSERT INTO admin_ai_approvals (id, session_id, tool_call_id, status, plan_summary, method, path, body_snapshot, expires_at, created_at) VALUES (?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?)`,
			approvalID, sessionID, tcID, planSummary, method, path, string(body), expiresAt, now)

		s.emit(eventCh, SSEEvent{Type: "approval_required", Fields: map[string]interface{}{
			"approvalId":   approvalID,
			"planSummary":  planSummary,
			"expiresAt":    expiresAt,
			"method":       method,
			"path":         path,
			"bodySnapshot": string(body),
		}})

		approvalCh := make(chan string, 1)
		s.mu.Lock()
		s.approval[approvalID] = approvalCh
		s.mu.Unlock()

		defer func() {
			s.mu.Lock()
			delete(s.approval, approvalID)
			s.mu.Unlock()
		}()

		select {
		case action := <-approvalCh:
			if action != "approve" {
				_, _ = db.ExecContext(ctx, "UPDATE admin_ai_approvals SET status = 'rejected' WHERE id = ? AND status = 'pending'", approvalID)
				return nil, fmt.Errorf("写操作审批被拒绝")
			}
		case <-ctx.Done():
			return nil, fmt.Errorf("等待审批时执行已超时或取消")
		case <-time.After(approvalTTL):
			_, _ = db.ExecContext(ctx, "UPDATE admin_ai_approvals SET status = 'expired' WHERE id = ? AND status = 'pending'", approvalID)
			return nil, fmt.Errorf("审批已超时，写操作未执行")
		}
	}

	return s.aiCaller(ctx, systemmetrics.AICallRequest{
		Method: method, Path: path, Headers: headers, Body: body,
	})
}

func (s *Service) getWriteEnabled(ctx context.Context, db *sql.DB) (bool, error) {
	var value string
	err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = 'admin_ai_write_enabled'").Scan(&value)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return value == "true" || value == "1", nil
}

func (s *Service) finishExecution(db *sql.DB, execID, status string, toolCount int, llmModel string, promptTokens, completionTokens int, errMsg string) {
	now := time.Now().UTC().Format(time.RFC3339)
	var errField interface{}
	if errMsg != "" {
		errField = errMsg
	}
	_, _ = db.ExecContext(context.Background(),
		`UPDATE admin_ai_executions SET status = ?, tool_calls_count = ?, llm_model = ?, llm_prompt_tokens = ?, llm_completion_tokens = ?, finished_at = ?, error = ? WHERE id = ?`,
		status, toolCount, llmModel, promptTokens, completionTokens, now, errField, execID)
}

func truncateContent(s string) string {
	if len(s) <= contentSizeLimit {
		return s
	}
	return s[:contentSizeLimit] + "...[已截断]"
}
