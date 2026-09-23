package antigravity

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/accountpick"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// accountCooldown 是账号遇到可重试上游失败（429/5xx/网络/401/403）后的冷却时长。
// 冷却只是**软偏好**：冷却期内选号会优先避开该账号；若可用账号全在冷却，会兜底选
// 最早恢复的那个继续尝试，绝不因为「都在冷却」就直接失败。
const accountCooldown = 5 * time.Minute

// maxAccountAttempts 是单次转发最多尝试的账号数（首号 + 最多两次换号）。
const maxAccountAttempts = 3

// accountAvailableForRelay 是选号判据：在通用 accountAvailable 之上额外要求 ProjectID
// （转发必须带项目 ID）。与 handleStatus 的可用判据保持一致，避免「界面绿色但不可转发」。
func accountAvailableForRelay(a Account) bool {
	return accountAvailable(a) && strings.TrimSpace(a.ProjectID) != ""
}

// inCooldown 返回账号是否处于失败冷却期。
func (s *Service) inCooldown(email string) bool {
	_, ok := s.cooldownUntilOf(email)
	return ok
}

// cooldownUntilOf 读取账号当前冷却的截止时刻；未冷却或已过冷却期返回 ok=false。
func (s *Service) cooldownUntilOf(email string) (time.Time, bool) {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	until, ok := s.cooldownUntil[email]
	if !ok || !time.Now().Before(until) {
		return time.Time{}, false
	}
	return until, true
}

// markCooldown 记录账号一次可重试的上游失败，进入冷却期。
func (s *Service) markCooldown(email, reason string) {
	if email == "" {
		return
	}
	until := time.Now().Add(accountCooldown)
	s.cooldownMu.Lock()
	if s.cooldownUntil == nil {
		s.cooldownUntil = map[string]time.Time{}
	}
	s.cooldownUntil[email] = until
	s.cooldownMu.Unlock()
	applog.Warn(context.Background(), "antigravity", "账号因上游失败进入冷却",
		"email", email,
		"until", until.UTC().Format(time.RFC3339),
		"reason", reason)
}

// clearCooldown 清除账号冷却（转发成功时调用）。
func (s *Service) clearCooldown(email string) {
	s.cooldownMu.Lock()
	defer s.cooldownMu.Unlock()
	delete(s.cooldownUntil, email)
}

// pickAnyAccount 返回任一可用账号（不看冷却、不推进轮询游标），供「是否有账号」
// 「拉模型 / 查配额 / 连通性测试」等非转发场景使用。无可用返回 nil。
func (s *Service) pickAnyAccount() *Account {
	st := s.Settings()
	for i := range st.Accounts {
		if accountAvailableForRelay(st.Accounts[i]) {
			return &st.Accounts[i]
		}
	}
	return nil
}

// pickAccount 是管理面取账号的入口：与 pickAnyAccount 同义。
// 刻意不套用选号策略，避免管理面调用推进 round-robin 游标而干扰转发分配，
// 也避免全部账号正在冷却时管理面（如拉模型列表）取不到账号。
func (s *Service) pickAccount() *Account {
	return s.pickAnyAccount()
}

// pickForRelay 按当前策略从符合硬条件的候选里选号。
//
// 三种策略（Settings.AccountStrategy）：
//   - first：列表首个可用账号，其余作主备（默认）。
//   - round-robin：从上次位置继续向后轮询。
//   - least-used：选剩余额度最多的账号（额度快照缺失时退化为列表序）。
//
// 三种策略都跳过：不可用（停用/无凭据/过期/缺 ProjectID）、本次已尝试、冷却中的账号。
// 若可用账号全在冷却中，退回「冷却最早结束」的那个继续尝试。
func (s *Service) pickForRelay(tried map[string]bool) (Account, bool) {
	accounts := s.Settings().Accounts
	strategy := normalizeStrategy(s.Settings().AccountStrategy)

	cands := make([]accountpick.Candidate, 0, len(accounts))
	for i := range accounts {
		a := accounts[i]
		if !accountAvailableForRelay(a) {
			continue
		}
		if tried != nil && tried[a.Email] {
			continue
		}
		if s.inCooldown(a.Email) {
			continue
		}
		cands = append(cands, accountpick.Candidate{ID: a.Email, Index: i})
	}

	byID := func(id string) (Account, bool) {
		for i := range accounts {
			if accounts[i].Email == id {
				return accounts[i], true
			}
		}
		return Account{}, false
	}

	if len(cands) > 0 {
		switch strategy {
		case strategyRoundRobin:
			if c, ok := s.rr.Pick(cands); ok {
				return byID(c.ID)
			}
		case strategyLeastUsed:
			if c, ok := accountpick.Best(cands, s.quotaWeight); ok {
				return byID(c.ID)
			}
		default: // first
			return byID(cands[0].ID)
		}
	}

	// 全部在冷却中：退回最早恢复的账号。
	bestIdx := -1
	var bestUntil time.Time
	for i := range accounts {
		a := accounts[i]
		if !accountAvailableForRelay(a) {
			continue
		}
		if tried != nil && tried[a.Email] {
			continue
		}
		until, cooled := s.cooldownUntilOf(a.Email)
		if !cooled {
			continue
		}
		if bestIdx < 0 || until.Before(bestUntil) {
			bestIdx, bestUntil = i, until
		}
	}
	if bestIdx >= 0 {
		return accounts[bestIdx], true
	}
	return Account{}, false
}

// quotaWeight 返回账号的选号权重（剩余额度）；快照缺失或失效时返回 0。
func (s *Service) quotaWeight(email string) float64 {
	return s.quotaSnap.Get(email, quotaSnapshotTTL)
}

// -----------------------------------------------------------------------------
// 选号额度快照
// -----------------------------------------------------------------------------

// quotaSnapshotTTL 是选号额度快照的有效期。超期视为失效（least-used 按 0 处理）。
const quotaSnapshotTTL = 2 * time.Hour

// recordQuotaSnapshot 把一个账号的剩余额度写入选号快照。
// 剩余额度取「AI Credits 余额」优先，缺失时退化为各配额窗口剩余比例的最小值。
func (s *Service) recordQuotaSnapshot(view *QuotaView) {
	if view == nil || view.Email == "" {
		return
	}
	weight := quotaWeightOf(view)
	s.quotaSnap.Set(view.Email, weight)
}

// quotaWeightOf 从配额视图折算账号级剩余额度权重，无数据返回 0。
func quotaWeightOf(view *QuotaView) float64 {
	if view == nil {
		return 0
	}
	if len(view.Credits) > 0 {
		total := 0.0
		for i := range view.Credits {
			total += view.Credits[i].GetAmount()
		}
		if total > 0 {
			return total
		}
	}
	best := -1.0
	for _, g := range view.Groups {
		for _, b := range g.Buckets {
			f := b.RemainingFraction
			if best < 0 || f < best {
				best = f
			}
		}
	}
	if best < 0 {
		return 0
	}
	return best
}

// refreshQuotaSnapshots 刷新全部可用账号的选号额度快照。
// 仅在 least-used 策略下有意义，其余策略直接跳过以免无谓打上游。
func (s *Service) refreshQuotaSnapshots(ctx context.Context) {
	if normalizeStrategy(s.Settings().AccountStrategy) != strategyLeastUsed {
		return
	}
	for _, a := range s.Settings().Accounts {
		if !accountAvailableForRelay(a) {
			continue
		}
		if view, err := s.FetchQuota(ctx, a.Email); err == nil {
			s.recordQuotaSnapshot(view)
		}
	}
}

// -----------------------------------------------------------------------------
// 上游错误分类
// -----------------------------------------------------------------------------

// upstreamError 是转发链路的上游失败，区分是否可换号重试。
type upstreamError struct {
	status    int
	retryable bool
	msg       string
}

func (e *upstreamError) Error() string { return e.msg }

// attemptResult 是一次转发尝试的结果。
//   - wrote：已开始写响应体，不可再换号重试（流式尤为关键）；
//   - retryable：失败但未写出任何字节，可换号重试；
//   - err：失败原因。
type attemptResult struct {
	wrote     bool
	retryable bool
	err       error
}

// classifyUpstream 把上游非 2xx 响应归类：429/5xx/401/403 视为可换号重试，
// 其余（4xx 请求错误）直接返回，换号也不会成功。
func classifyUpstream(status int, body string) *upstreamError {
	retryable := status == http.StatusTooManyRequests ||
		status == http.StatusUnauthorized ||
		status == http.StatusForbidden ||
		status >= 500
	msg := strings.TrimSpace(body)
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &upstreamError{
		status:    status,
		retryable: retryable,
		msg:       fmt.Sprintf("上游返回 HTTP %d: %s", status, msg),
	}
}
