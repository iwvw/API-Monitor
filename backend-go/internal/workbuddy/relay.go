package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// maxChatBodyBytes 是对话请求体的大小上限（16MB），超出视为异常请求。
const maxChatBodyBytes = 16 << 20

// serveRelay 处理 /api/workbuddy/v1/* 的中继请求。
// 网关把本插件当作一个 OpenAI 兼容上游端点，因此这里只说 OpenAI 协议。
// stripped 是剥掉 /api/workbuddy 前缀后的路径。
func (s *Service) serveRelay(w http.ResponseWriter, r *http.Request, stripped string) {
	if !s.Settings().Enabled {
		writeOpenAIError(w, http.StatusServiceUnavailable, "WorkBuddy 插件未启用", "service_unavailable")
		return
	}
	switch {
	case r.Method == http.MethodGet && (stripped == "/v1/models" || stripped == "/v1/models/"):
		s.serveRelayModels(w)
	case r.Method == http.MethodPost && stripped == "/v1/chat/completions":
		s.serveChatCompletions(w, r)
	default:
		writeOpenAIError(w, http.StatusNotFound, "未支持的中继路径: "+stripped, "invalid_request_error")
	}
}

// serveRelayModels 输出带前缀、且过滤掉停用项的模型列表。
// 网关用它校验/刷新端点模型名单。
func (s *Service) serveRelayModels(w http.ResponseWriter) {
	models := s.catalog(context.Background())
	disabled := s.disabledSet()
	out := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		id := s.prefixModel(m.ID)
		if disabled[id] {
			continue
		}
		out = append(out, map[string]interface{}{
			"id":       id,
			"object":   "model",
			"created":  0,
			"owned_by": providerName,
		})
	}
	responseJSON(w, map[string]interface{}{"object": "list", "data": out})
}

// serveChatCompletions 转发一次对话请求。
func (s *Service) serveChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxChatBodyBytes))
	_ = r.Body.Close()
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "读取请求体失败: "+err.Error(), "invalid_request_error")
		return
	}
	if len(body) == 0 {
		writeOpenAIError(w, http.StatusBadRequest, "请求体为空", "invalid_request_error")
		return
	}

	// 停用模型先判、且不依赖是否有可用账号：这是最廉价也最准确的一道闸，
	// 也覆盖了直连 /api/workbuddy/v1（绕过网关）的情况。
	requested := s.clientsModelName(body)
	if requested != "" && s.disabledSet()[requested] {
		writeOpenAIError(w, http.StatusNotFound,
			"模型 "+requested+" 已在 WorkBuddy 插件中停用", "model_not_found")
		return
	}

	acc, ok := s.pickAccount(r.Context())
	if !ok {
		writeOpenAIError(w, http.StatusServiceUnavailable,
			"没有可用账号：请先扫码登录，或检查账号是否已停用/token 是否已失效", "service_unavailable")
		return
	}

	model := s.stripModelPrefix(requested)
	// 上游拒绝非流式（业务码 11101）：发给上游的 body 一律强制 stream:true，
	// 非流式由本层把 SSE 聚合回整包。模型名先剥本插件前缀，
	// 否则思考档位判定（hy3/hy4 前缀）会被前缀干扰。
	payload := forceStreamBody(rewriteForUpstream(body, model))

	// chatCompletions 保证只在写出任何响应之前返回错误，所以这里可以安全地回错误体。
	if err := s.chatCompletions(r.Context(), w, &chatRequest{
		Account: acc,
		Body:    payload,
		Model:   model,
		Stream:  s.clientWantsStream(body),
	}); err != nil {
		writeOpenAIError(w, http.StatusBadGateway, err.Error(), "upstream_error")
	}
}

// pickAccount 选取本次转发的账号。
//
// 选号策略：**站点时区「今天已消耗 credit 最少」的可用账号**（消耗相同取列表序靠前者）。
// 这样按实际计费额度自然拉平各账号消耗，也避免某个账号先撞上限额；
// 权重来自内存快照，选号本身不查库。
//
// 一个可用账号都没有时，再试着刷新「已过期但可刷新」的账号（对应设置页的
// 「扫码登录 + 自动刷新」语义）。
//
// TODO(workbuddy-调度): 仍缺 429/配额超限的冷却与失败换号 —— 现在上游报错会直接
// 502 抛给调用方，不会自动换另一个账号重试。
func (s *Service) pickAccount(ctx context.Context) (Account, bool) {
	if acc, ok := s.pickLeastConsumed(s.Settings().Accounts); ok {
		return acc, true
	}
	if acc, ok := s.refreshFirstStaleAccount(ctx); ok {
		return acc, true
	}
	return Account{}, false
}

// refreshFirstStaleAccount 找出第一个「未停用、有 refresh token、token 已失效」的账号，
// 尝试刷新并把结果落库；刷新成功才算可用。
// 仅在「所有账号都不可用」时才走到这里，是兜底路径而非常规选号。
func (s *Service) refreshFirstStaleAccount(ctx context.Context) (Account, bool) {
	for _, a := range s.Settings().Accounts {
		if a.Disabled || a.RefreshToken == "" || tokenState(a) == "valid" {
			continue
		}
		acc := a
		if err := s.refreshAccessToken(ctx, &acc); err != nil {
			acc.LastError = err.Error()
			_ = s.upsertAccount(ctx, acc)
			continue
		}
		if err := s.upsertAccount(ctx, acc); err != nil {
			continue
		}
		return acc, accountAvailable(acc)
	}
	return Account{}, false
}

// autoRefreshInterval 是后台自动刷新的扫描周期。
const autoRefreshInterval = 5 * time.Minute

// StartAutoRefresh 启动 access token 自动刷新：定期把即将过期/已过期的账号
// 提前换成新 token，避免转发时才发现失效。ctx 取消时退出。
func (s *Service) StartAutoRefresh(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(autoRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.RefreshStaleAccounts(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// RefreshStaleAccounts 刷新所有「未停用、有 refresh token、剩余寿命低于提前量」的账号。
// 单个账号失败不影响其它账号，失败原因写回 LastError 便于前端排障。
func (s *Service) RefreshStaleAccounts(ctx context.Context) {
	for _, a := range s.Settings().Accounts {
		if a.Disabled || a.RefreshToken == "" {
			continue
		}
		if state := tokenState(a); state == "valid" {
			continue
		}
		acc := a
		before := acc.AccessToken
		if err := s.refreshAccessToken(ctx, &acc); err != nil {
			acc.LastError = err.Error()
			_ = s.upsertAccount(ctx, acc)
			continue
		}
		if acc.AccessToken == before && acc.LastRefreshAt == a.LastRefreshAt {
			continue
		}
		_ = s.upsertAccount(ctx, acc)
	}
}

// clientsModelName 从请求体读出客户端模型名（仅顶层 model 字段），不去前缀。
func (s *Service) clientsModelName(body []byte) string {
	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) != nil {
		return ""
	}
	m, _ := payload["model"].(string)
	return strings.TrimSpace(m)
}

// clientWantsStream 读取客户端原始请求里的 stream 意图。
// 必须在 forceStreamBody 之前调用（那之后 stream 恒为 true）。
func (s *Service) clientWantsStream(body []byte) bool {
	var payload map[string]interface{}
	if json.Unmarshal(body, &payload) != nil {
		return false
	}
	v, _ := payload["stream"].(bool)
	return v
}
