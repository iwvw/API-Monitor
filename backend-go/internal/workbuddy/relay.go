package workbuddy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/accountpick"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
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

	model := s.stripModelPrefix(requested)
	// 上游拒绝非流式（业务码 11101）：发给上游的 body 一律强制 stream:true，
	// 非流式由本层把 SSE 聚合回整包。模型名先剥本插件前缀，
	// 否则思考档位判定（hy3/hy4 前缀）会被前缀干扰。
	payload := forceStreamBody(rewriteForUpstream(body, model))

	// 失败换号重试：单次转发最多尝试 maxAccountAttempts 个账号。
	// 失败分两类，处置**不同**（见 ratelimit.go 开头）：
	//   - 瞬时故障（429/5xx/网络/流中断）→ 换下一个账号继续尝试（不做账号级冷却）；
	//   - 模型级限流（上游点名「<模型> 超出频率限制，<时刻> 重置」）→ 只封
	//     「该账号 × 该模型」，同一账号的其它模型照常可用，且冷却到上游给的真实时刻。
	// chatCompletions 保证错误发生在写出任何响应之前，因此重试不会破坏已下发的字节。
	var lastErr error
	// 本请求期间见过的「最晚恢复时刻」：用于最后把「还要等多久」告诉调用方。
	var lastLimitUntil time.Time
	// attempted 记录本轮是否真的打过上游：没打过（全被限流拦下）与「打完全部失败」
	// 是两种截然不同的情形，报错也该不同。
	attempted := false
	// tried 记录本次请求已尝试过的账号：保证换号不会反复命中同一个账号。
	tried := make(map[string]bool, maxAccountAttempts)
	for attempt := 0; attempt < maxAccountAttempts; attempt++ {
		acc, ok := s.pickAccount(r.Context(), model, tried)
		if !ok {
			break
		}
		tried[acc.ID] = true
		attempted = true
		if err := s.chatCompletions(r.Context(), w, &chatRequest{
			Account: acc,
			Body:    payload,
			Model:   model,
			Stream:  s.clientWantsStream(body),
		}); err == nil {
			return
		} else {
			lastErr = err
			var ue *upstreamError
			if errors.As(err, &ue) && ue.retryable {
				if ue.rateLimit {
					s.markModelLimit(acc.ID, model, ue.rateLimitUntil, ue.msg)
					if ue.rateLimitUntil.After(lastLimitUntil) {
						lastLimitUntil = ue.rateLimitUntil
					}
					continue
				}
				// 瞬时故障（429/5xx/网络/流中断）：不记账号级冷却，直接换下一个账号。
				continue
			}
			break
		}
	}

	loc := s.siteLocation(r.Context())
	// 一个号都没打成：**不能**就此断定「所有账号都被该模型限流」——选号返回 false
	// 还有别的常见原因（全部停用/无凭据/选号出错）。若不加区分地
	// 报「该模型最早 N 天后恢复」，而那个 N 天其实来自一个已停用账号的历史记录，
	// 就把用户往错误方向带了。故这里要求**确有可用账号、且它们全部被该模型限流**
	// 才报 429，否则回落到下面的常规提示。
	//
	// 此时**不发上游请求**，直接把最早恢复时刻告诉调用方——这比 502 有用得多。
	if !attempted {
		applog.Warn(r.Context(), "workbuddy", "no usable account for relay",
			"model", model,
			"accounts", s.accountPoolDiagnosis(s.Settings().Accounts, model))
		if usable, until, ok := s.allUsableAccountsModelLimited(s.Settings().Accounts, model); ok {
			writeOpenAIError(w, http.StatusTooManyRequests, fmt.Sprintf(
				"模型 %s 在当前所有可用账号（%d 个）上均处于频率限制中，最早将于 %s 恢复可用",
				model, usable, until.In(loc).Format("2006-01-02 15:04:05")), "rate_limit_error")
			return
		}
	}
	if lastErr != nil {
		// 因限流而失败：回 429 并带上恢复时刻，而不是含糊的 502 —— 调用方据此决定
		// 「换模型」还是「等到几点」。这是用户真正需要的信息。
		if lastLimitUntil.After(time.Now()) {
			writeOpenAIError(w, http.StatusTooManyRequests, fmt.Sprintf("%s（该模型将于 %s 恢复可用）",
				lastErr.Error(), lastLimitUntil.In(loc).Format("2006-01-02 15:04:05")), "rate_limit_error")
			return
		}
		writeOpenAIError(w, http.StatusBadGateway, lastErr.Error(), "upstream_error")
		return
	}
	writeOpenAIError(w, http.StatusServiceUnavailable,
		"没有可用账号：请先扫码登录，或检查账号是否已停用/token 是否已失效", "service_unavailable")
}

// maxAccountAttempts 是单次转发最多尝试的账号数（首号 + 最多两次换号）。
// 账号再多也不无限重试，避免拖垮单请求延迟。
const maxAccountAttempts = 3

// pickAccount 选取本次转发的账号。
//
// 选号策略（Settings.AccountStrategy）：
//   - least-consumed（默认）：站点时区「今天已消耗 credit 最少」的可用账号
//     （消耗相同取列表序靠前者），按实际计费额度自然拉平各账号消耗；
//   - first：列表首个可用账号，其余作主备，行为最可预期；
//   - round-robin：从上次位置继续向后轮询，请求均匀分摊。
//
// 三种策略都跳过本次请求已尝试过的账号、在该 model 上被限流的
// 账号，以及不提供该模型区域外的账号（区域过滤）。权重来自内存快照，选号本身不查库。
//
// 一个可用账号都没有时，再试着刷新「已过期但可刷新」的账号（对应设置页的
// 「扫码登录 + 自动刷新」语义）。
func (s *Service) pickAccount(ctx context.Context, model string, tried map[string]bool) (Account, bool) {
	if acc, ok := s.pickByStrategy(model, tried); ok {
		return acc, true
	}
	if acc, ok := s.refreshFirstStaleAccount(ctx, model, tried); ok {
		return acc, true
	}
	return Account{}, false
}

// pickByStrategy 按配置的策略从符合硬条件的候选里选号。
func (s *Service) pickByStrategy(model string, tried map[string]bool) (Account, bool) {
	switch normalizeStrategy(s.Settings().AccountStrategy) {
	case strategyFirst:
		return s.pickFirst(model, tried)
	case strategyRoundRobin:
		return s.pickRoundRobin(model, tried)
	default:
		return s.pickLeastConsumed(s.Settings().Accounts, model, tried)
	}
}

// eligibleCandidates 收集符合硬条件（可用、未尝试、未被该模型限流、
// 区域提供该模型）的候选，保持列表序并记录原始下标。
func (s *Service) eligibleCandidates(accounts []Account, model string, tried map[string]bool) []accountpick.Candidate {
	cands := make([]accountpick.Candidate, 0, len(accounts))
	for i, a := range accounts {
		if !accountAvailable(a) {
			continue
		}
		if tried != nil && tried[a.ID] {
			continue
		}
		if model != "" && s.inModelLimit(a.ID, model) {
			continue
		}
		if !s.accountServesModel(a, model) {
			continue
		}
		cands = append(cands, accountpick.Candidate{ID: a.ID, Index: i})
	}
	return cands
}

// pickFirst 取列表序首个符合硬条件的账号。
func (s *Service) pickFirst(model string, tried map[string]bool) (Account, bool) {
	accounts := s.Settings().Accounts
	cands := s.eligibleCandidates(accounts, model, tried)
	if len(cands) == 0 {
		return Account{}, false
	}
	return accounts[cands[0].Index], true
}

// pickRoundRobin 从上次位置继续向后轮询，符合硬条件的候选里选下一个。
func (s *Service) pickRoundRobin(model string, tried map[string]bool) (Account, bool) {
	accounts := s.Settings().Accounts
	cands := s.eligibleCandidates(accounts, model, tried)
	if len(cands) == 0 {
		return Account{}, false
	}
	c, ok := s.rr.Pick(cands)
	if !ok {
		return Account{}, false
	}
	return accounts[c.Index], true
}

// refreshFirstStaleAccount 找出第一个「未停用、有 refresh token、token 已失效
// 且本次请求尚未尝试过」的账号，尝试刷新并把结果落库；刷新成功才算可用。
// 仅在「所有账号都不可用」时才走到这里，是兜底路径而非常规选号。
func (s *Service) refreshFirstStaleAccount(ctx context.Context, model string, tried map[string]bool) (Account, bool) {
	for _, a := range s.Settings().Accounts {
		// 转发停用但签到仍启用的账号也要刷新：其 token 必须保持有效才能签到。
		if (a.Disabled && !checkinEligible(a)) || a.RefreshToken == "" || tokenState(a) == "valid" {
			continue
		}
		if tried != nil && tried[a.ID] {
			continue
		}
		// 刷新 token 不影响模型级限流：该模型仍被限流的账号，刷了也用不了。
		if model != "" && s.inModelLimit(a.ID, model) {
			continue
		}
		// 区域过滤：刷新兜底也不能跨区（国际独有模型不去国内账号）。
		if !s.accountServesModel(a, model) {
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

// RefreshStaleAccounts 刷新所有「参与转发或参与签到、有 refresh token、剩余寿命低于提前量」
// 的账号。转发停用但签到仍启用的账号也要刷新，否则其 token 过期会导致签到中断。
// 单个账号失败不影响其它账号，失败原因写回 LastError 便于前端排障。
func (s *Service) RefreshStaleAccounts(ctx context.Context) {
	for _, a := range s.Settings().Accounts {
		if (a.Disabled && !checkinEligible(a)) || a.RefreshToken == "" {
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
