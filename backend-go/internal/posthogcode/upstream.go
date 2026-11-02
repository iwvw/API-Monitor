package posthogcode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ModelPricing 是上游返回的按 token 计价表，单位为 USD/token。
// Go 网关的键为 cache_read/cache_write（旧 Python 网关为 input_cache_read/input_cache_write）。
type ModelPricing struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"cache_read"`
	InputCacheWrite string `json:"cache_write"`
}

// ModelInfo 是 Go AI 网关 /v1/models 返回的单个模型条目。
// 该目录公开（无需鉴权），字段与旧 Python 网关不同：无 allowed/restriction_reason，
// 计价键为 cache_read/cache_write。
type ModelInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Canonical     string `json:"canonical"`
	OwnedBy       string `json:"owned_by"`
	ContextWindow int    `json:"context_window"`
	// APIshapes 是该模型可用的请求形状（openai-chat-completions 等）。
	APIshapes []string `json:"api_shapes"`
	// Pricing 是计价表；上游未收录时为 null。
	Pricing *ModelPricing `json:"pricing"`
}

// freeTierModels 是 PostHog Desktop 免费层模型（未绑定付款方式的组织可用），
// 取 Go 网关的写法（Go 不认 `@cf/` 别名）。来源：
// products/tasks/backend/logic/services/gateway_model_pin.py 的 FREE_TIER_MODELS。
var freeTierModels = []string{
	"zai-org/glm-5.2",
	"deepseek-ai/deepseek-v4-flash-0731",
	"moonshotai/kimi-k3",
	"zai-org/glm-5.3-flash",
}

// isFreeTierModel 判断模型名是否属于免费层（前缀匹配，与上游一致）。
func isFreeTierModel(id string) bool {
	lower := strings.ToLower(strings.TrimSpace(goModelName(id)))
	for _, m := range freeTierModels {
		if strings.HasPrefix(lower, strings.ToLower(m)) {
			return true
		}
	}
	return false
}

// modelsURL 构造指定账号区域的 Go 网关模型目录地址（公开，无产品路径段）。
func (s *Service) modelsURL(acc Account) string {
	return goGatewayBaseURL(s.accountRegion(acc)) + "/v1/models"
}

// fetchModels 从 Go 网关拉取模型目录。该端点无需鉴权，失败时返回错误由调用方兜底。
func (s *Service) fetchModels(ctx context.Context, acc Account) ([]ModelInfo, error) {
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.modelsURL(acc), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("拉取模型列表失败: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("模型列表返回 %d: %s", resp.StatusCode, msg)
	}

	var parsed struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	return parsed.Data, nil
}

// catalog 返回带缓存的上游模型目录。
func (s *Service) catalog(ctx context.Context) []ModelInfo {
	s.modelMu.Lock()
	if len(s.modelCache) > 0 && time.Since(s.modelCacheAt) < 10*time.Minute {
		out := append([]ModelInfo(nil), s.modelCache...)
		s.modelMu.Unlock()
		return out
	}
	s.modelMu.Unlock()

	acc, ok := s.pickAccount("", nil)
	if !ok {
		s.modelMu.Lock()
		out := append([]ModelInfo(nil), s.modelCache...)
		s.modelMu.Unlock()
		return out
	}
	models, err := s.fetchModels(ctx, acc)
	if err != nil || len(models) == 0 {
		s.modelMu.Lock()
		out := append([]ModelInfo(nil), s.modelCache...)
		s.modelMu.Unlock()
		return out
	}
	s.modelMu.Lock()
	s.modelCache = models
	s.modelCacheAt = time.Now()
	s.modelMu.Unlock()
	s.applyInitialModelDefaults(ctx, models)
	return append([]ModelInfo(nil), models...)
}

// applyInitialModelDefaults 首次拿到模型目录时，自动停用所有受限（非免费层）模型，
// 只把免费层模型留为启用状态。仅执行一次：之后用户的启停选择不再被覆盖。
//
// 整个「检查标记 → 计算 → 写回」在 accountsMu 内原子完成：并发拉取目录时
// 两个 goroutine 同时看到未初始化，会各自覆盖 DisabledModels。
func (s *Service) applyInitialModelDefaults(ctx context.Context, models []ModelInfo) {
	s.accountsMu.Lock()
	defer s.accountsMu.Unlock()
	if s.Settings().ModelsInitialized {
		return
	}
	disabled := make([]string, 0, len(models))
	for _, m := range models {
		if !isFreeTierModel(m.ID) {
			disabled = append(disabled, m.ID)
		}
	}
	st := s.Settings()
	st.DisabledModels = disabled
	st.ModelsInitialized = true
	if err := s.SaveSettings(ctx, st); err != nil {
		// 落库失败则下次拉取时重试，不影响本次返回。
		return
	}
}

// visibleModels 返回对外可见的模型 id（带前缀、过滤停用与免费层限制）。
func (s *Service) visibleModels(ctx context.Context) []string {
	st := s.Settings()
	disabled := s.disabledSet()
	models := s.catalog(ctx)
	out := make([]string, 0, len(models))
	for _, m := range models {
		if disabled[m.ID] || disabled[s.prefixModel(m.ID)] {
			continue
		}
		if st.FreeTierOnly && !isFreeTierModel(m.ID) {
			continue
		}
		out = append(out, s.prefixModel(m.ID))
	}
	return out
}

// modelNamesNoFallback 返回不带前缀的上游模型名；失败时返回空切片，
// 让调用方（如前缀变更刷新）据此保留库中已有模型。
func (s *Service) modelNamesNoFallback(ctx context.Context) []string {
	models := s.catalog(ctx)
	out := make([]string, 0, len(models))
	for _, m := range models {
		out = append(out, m.ID)
	}
	return out
}

// relayTarget 描述一次转发的目标。
type relayTarget struct {
	account Account
	token   string
	url     string
}

// chatCompletionsURL 由铸造返回的 gateway_url 构造对话地址（无产品路径段）。
// 仅接受 https 且 host 属 *.posthog.com 的网关地址：铸造响应来自受信任的
// PostHog，但为防上游响应被篡改/组织被攻破时把 phe_ token 与对话内容外发给
// 不可信主机（SSRF/凭据外发），非白名单 host 一律返回空串。
func chatCompletionsURL(gatewayURL string) string {
	raw := strings.TrimRight(strings.TrimSpace(gatewayURL), "/")
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") || !isPostHogGatewayHost(u.Hostname()) {
		return ""
	}
	return raw + "/v1/chat/completions"
}

// isPostHogGatewayHost 判断 host 是否为 PostHog 官方 AI 网关域名
// （posthog.com 或其子域，如 ai-gateway.us.posthog.com / ai-gateway.eu.posthog.com）。
func isPostHogGatewayHost(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	return h == "posthog.com" || strings.HasSuffix(h, ".posthog.com")
}

// upstreamError 是一次上游失败的分类结果。
type upstreamError struct {
	msg       string
	status    int
	retryable bool
	rateLimit bool
}

func (e *upstreamError) Error() string { return e.msg }

// classifyUpstream 把上游状态码与响应体归类，决定是否换号重试。
func classifyUpstream(status int, body string) *upstreamError {
	msg := strings.TrimSpace(body)
	if len(msg) > 400 {
		msg = msg[:400]
	}
	if msg == "" {
		msg = fmt.Sprintf("上游返回 %d", status)
	}
	ue := &upstreamError{msg: msg, status: status}
	switch {
	case status == http.StatusTooManyRequests:
		ue.retryable = true
		ue.rateLimit = true
	case status >= 500:
		ue.retryable = true
	case status == http.StatusUnauthorized:
		// token 失效：换号重试即可（refresh 由 ensureFreshToken 负责）。
		ue.retryable = true
	case status == http.StatusForbidden:
		// 403 多为账号级配额/计划限制，换号可能有效（不同组织额度不同）。
		ue.retryable = true
	default:
		ue.retryable = false
	}
	return ue
}

// doUpstream 向上游发起一次请求，返回响应（调用方负责关闭 body）。
func doUpstream(ctx context.Context, target relayTarget, body []byte, stream bool) (*http.Response, error) {
	reqCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, target.url, strings.NewReader(string(body)))
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+target.token)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		cancel()
		return nil, err
	}
	// 把 cancel 挂到响应体上，读取完成后释放。
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// cancelOnCloseBody 在关闭响应体时释放请求上下文。
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

// buildUpstreamURL 返回上游 chat/completions 地址（含必要的查询参数）。
func buildUpstreamURL(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	return u.String()
}
