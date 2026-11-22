package adminai

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// firstTokenTimeout 是本机网关流式响应首块等待上限：网关侧自身有 10s 首字
// 切代理逻辑，此值作为兜底，防止上游普遍限流/慢推理被 failover 放大到分钟级
// 后超出整轮预算（实际表现为长时间无输出后「执行超时」）。
// 用 var 以便测试注入短值（运行期只读，勿修改）。
var firstTokenTimeout = 90 * time.Second

// llmRetryableError 判断 LLM 上游错误是否值得重试：网络抖动（reset/network）、
// 上游 5xx、限流 429、上游超时（首个数据块未到）等瞬时故障重试有意义；
// 参数/鉴权类 4xx 与「connection refused」（本机网关未监听，确定性失败）不重试。
func llmRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "connection refused") {
		return false
	}
	for _, marker := range []string{
		"429", "too many requests", "rate limit",
		"500", "502", "503", "504", "server error", "bad gateway", "service unavailable",
		"timeout", "timed out", "未收到首个数据块", "network", "reset",
		"temporary", "temporarily", "overloaded", "backpressure", "upstream",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// 管理 AI 经本机回环调用模型网关(/v1/chat/completions),需携带进程内部令牌,
// 由网关识别为「同进程内部调用」并免密钥放行(见 modelhub.AuthorizeGatewayRequest)。
// 令牌是 server 启动时生成的随机值,经该环境变量下发给同进程调用方。
const (
	internalGatewayTokenEnv    = "API_MONITOR_INTERNAL_CRON_TOKEN"
	internalGatewayTokenHeader = "X-Internal-Cron-Token"
)

// applyInternalGatewayAuth 给发往本机网关的请求附加内部调用令牌。取不到令牌时
// 不加(退化为普通请求,网关会要求网关密钥),不阻断调用。
func applyInternalGatewayAuth(req *http.Request) {
	if token := strings.TrimSpace(os.Getenv(internalGatewayTokenEnv)); token != "" {
		req.Header.Set(internalGatewayTokenHeader, token)
	}
}

// llmRetryDelay 指数退避(500ms → 1s → 2s → 4s → 8s 封顶)。
func llmRetryDelay(attempt int) time.Duration {
	ms := llmRetryBaseDelayMs << uint(min(attempt-1, 4))
	if ms > llmRetryMaxDelayMs {
		ms = llmRetryMaxDelayMs
	}
	return time.Duration(ms) * time.Millisecond
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

// callLLM 通过本机网关 HTTP 调用 chat/completions（内部调用免鉴权，简化版非流式，带工具 schema）。
func (s *Service) callLLM(ctx context.Context, model string, messages []map[string]interface{}) (*llmResponse, error) {
	reqBody := map[string]interface{}{"model": model, "messages": messages, "tools": adminAITools}
	return s.callLLMWithBody(ctx, reqBody)
}

// callLLMPlain 不带工具 schema 的普通对话调用（推理摘要等辅助任务，避免模型误触发工具）。
func (s *Service) callLLMPlain(ctx context.Context, model string, messages []map[string]interface{}) (*llmResponse, error) {
	reqBody := map[string]interface{}{"model": model, "messages": messages}
	return s.callLLMWithBody(ctx, reqBody)
}

// callLLMPlainWithFallback 是辅助链路（每日简报/记忆提炼/会话标题）共用的
// 多候选按序回退调用：模型串支持逗号分隔（复用 splitModelList 解析，与主对话
// 一致），某候选调用失败或返回空正文时自动切换下一个，全部失败返回最后一个错误。
func (s *Service) callLLMPlainWithFallback(ctx context.Context, modelSpec string, messages []map[string]interface{}) (*llmResponse, error) {
	var lastErr error
	for _, model := range splitModelList(modelSpec) {
		resp, err := s.callLLMPlain(ctx, model, messages)
		if err != nil {
			lastErr = err
			slog.Warn("aux-llm-model-fallback", "model", model, "err", sanitizeToolError(err).Error())
			continue
		}
		if strings.TrimSpace(resp.Content) == "" && len(resp.Choices) > 0 {
			resp.Content = strings.TrimSpace(resp.Choices[0].Message.Content)
		}
		if strings.TrimSpace(resp.Content) != "" {
			return resp, nil
		}
		lastErr = fmt.Errorf("模型 %s 返回空正文", model)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("未配置可用模型")
	}
	return nil, lastErr
}

func (s *Service) callLLMWithBody(ctx context.Context, reqBody map[string]interface{}) (*llmResponse, error) {
	bodyBytes, _ := json.Marshal(reqBody)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.Port)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyInternalGatewayAuth(httpReq)

	// 非流式调用:单次请求(连接 + 响应头 + 响应体读取)总时长上限 180s;
	// 整体执行时长仍由 runCtx 总预算管控。
	client := &http.Client{Timeout: 180 * time.Second}
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

// streamDelta 是流式响应里每个 chunk 的增量字段。
type streamDelta struct {
	Content          string           `json:"content"`
	ReasoningContent string           `json:"reasoning_content"`
	Role             string           `json:"role"`
	ToolCalls        []streamToolCall `json:"tool_calls"`
	FinishReason     string           `json:"finish_reason"`
}

type streamToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// callLLMStream 通过本机网关调用 chat/completions（stream=true），逐块解析 SSE，
// 实时把 content / reasoning 增量推给 eventCh；返回完整响应（含 usage / tool_calls）。
// 网关侧本身非流式时（上游不支持），返回单块但同样推一次 delta，行为无差异。
// withTools=false（询问模式）时不携带工具 schema，模型退化为纯对话。
func (s *Service) callLLMStream(ctx context.Context, model string, messages []map[string]interface{}, eventCh chan SSEEvent, userMsgID string, withTools bool, reasoningEffort string) (*llmResponse, error) {
	reqBody := map[string]interface{}{"model": model, "messages": messages, "stream": true}
	if withTools {
		reqBody["tools"] = adminAITools
	}
	// 思考强度：留空不携带该字段（保持上游默认），厂商差异交给网关归一化。
	if effort := normalizeReasoningEffort(reasoningEffort); effort != "" {
		reqBody["reasoning_effort"] = effort
	}
	bodyBytes, _ := json.Marshal(reqBody)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", s.cfg.Port)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	applyInternalGatewayAuth(httpReq)

	// 流式响应不能用 Client.Timeout 卡整条流(它会覆盖连接+响应头+响应体的总时长,
	// 长思考/长输出会被中途掐断);总时长由 runCtx 预算控制,这里仅用 Transport
	// 约束连接与响应头,首块等待另有 firstTokenTimeout 兜底。
	client := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 180 * time.Second}}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("LLM 调用失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("LLM 调用失败 (HTTP %d): %s", resp.StatusCode, truncateContent(string(raw)))
	}

	llmResp := &llmResponse{}
	var content, reasoning strings.Builder
	toolAcc := map[int]*toolCall{}
	var lastToolOrder []int

	// 首块等待护栏：流式响应首块（含网关 failover 重试总时长）超过
	// firstTokenTimeout 未到达即中止，避免慢代理池放大后拖垮整轮预算。
	//
	// ⚠️ 先在**当前 goroutine** 读一次再交给下面的协程。包级变量虽声明为
	// 「运行期只读」，但测试会用 defer 改回原值，而护栏协程可能仍在读 ——
	// 直接闭包引用变量会被 -race 判为数据竞争（实测
	// TestCallLLMStreamSendsReasoningEffort 就中过）。读进局部变量后协程
	// 只碰局部量，与测试的还原互不干扰。
	firstTokenWait := firstTokenTimeout
	var firstTimedOut atomic.Bool
	firstData := make(chan struct{}, 1)
	go func() {
		select {
		case <-time.After(firstTokenWait):
			firstTimedOut.Store(true)
			_ = resp.Body.Close()
		case <-firstData:
		}
	}()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case firstData <- struct{}{}:
		default:
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta        streamDelta `json:"delta"`
				FinishReason string      `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Usage != nil {
			llmResp.Usage.PromptTokens = chunk.Usage.PromptTokens
			llmResp.Usage.CompletionTokens = chunk.Usage.CompletionTokens
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		if d.ReasoningContent != "" {
			reasoning.WriteString(d.ReasoningContent)
			s.emit(eventCh, SSEEvent{Type: "reasoning", Fields: map[string]interface{}{"text": d.ReasoningContent, "userMessageId": userMsgID}})
		}
		if d.Content != "" {
			content.WriteString(d.Content)
			s.emit(eventCh, SSEEvent{Type: "delta", Fields: map[string]interface{}{"text": d.Content, "userMessageId": userMsgID}})
		}
		for _, tc := range d.ToolCalls {
			cur, exists := toolAcc[tc.Index]
			if !exists {
				cur = &toolCall{}
				toolAcc[tc.Index] = cur
				lastToolOrder = append(lastToolOrder, tc.Index)
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			cur.Type = "function"
			if tc.Function.Name != "" {
				cur.Function.Name = tc.Function.Name
			}
			cur.Function.Arguments += tc.Function.Arguments
		}
	}
	if firstTimedOut.Load() {
		return nil, fmt.Errorf("LLM 调用超时：%.0f 秒内未收到首个数据块（网关或上游模型响应过慢，可稍后重试）", firstTokenWait.Seconds())
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("读取 LLM 流失败: %w", err)
	}

	llmResp.Content = content.String()
	llmResp.ReasoningContent = reasoning.String()
	if len(lastToolOrder) > 0 {
		out := make([]toolCall, 0, len(lastToolOrder))
		for _, idx := range lastToolOrder {
			out = append(out, *toolAcc[idx])
		}
		llmResp.ToolCalls = out
		if len(out) > 0 {
			llmResp.Choices = []struct {
				Message struct {
					Content          string     `json:"content"`
					ReasoningContent string     `json:"reasoning_content"`
					ToolCalls        []toolCall `json:"tool_calls"`
				} `json:"message"`
			}{{Message: struct {
				Content          string     `json:"content"`
				ReasoningContent string     `json:"reasoning_content"`
				ToolCalls        []toolCall `json:"tool_calls"`
			}{Content: llmResp.Content, ReasoningContent: llmResp.ReasoningContent, ToolCalls: out}}}
		}
	}
	if errors.Is(scanner.Err(), context.DeadlineExceeded) {
		return nil, context.DeadlineExceeded
	}
	return llmResp, nil
}
