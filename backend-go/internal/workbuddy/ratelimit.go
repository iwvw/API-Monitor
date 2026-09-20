package workbuddy

// 本文件处理「模型级频率限制」。
//
// 上游文案实测形如：
//
//	当前您在Deepseek-V4.1-Flash模型的使用量已超出频率限制，
//	可在2026-09-12 17:22:50 重置可用。您可切换其他模型或消耗积分继续使用该模型
//
// 它与 relay.go 里的「账号冷却」是**两件事**，互补而不重叠，切勿互相替代：
//
//	账号冷却（markCooldown）：HTTP 429/5xx/网络/SSE 中断等**瞬时故障**的通用退避，
//	  把该账号的**所有模型**一起晾 5 分钟，目的是等抖动过去。
//	模型限流（markModelLimit，本文件）：上游明确点名「<模型> 超出频率限制，<时刻> 重置」。
//	  这是**账号 × 模型**维度的长窗口限制（实测重置窗口可达数天），因此：
//	  1. 只封这一个模型 —— 同一账号的**其它模型照常可用**。用账号冷却会把该账号
//	     还能用的模型一起误伤，这是本文件存在的首要原因。
//	  2. 冷却到上游给出的**真实重置时刻**，而不是固定 5 分钟。5 分钟后重试只会
//	     继续撞墙，并对上游产生无谓请求。
//	  3. 所有账号在该模型上都撞限流时，**完全不再请求上游**，直接回 429 并带上最早
//	     恢复时刻，让调用方（网关/用户）一眼看到「什么时候能再用」——这才是用户
//	     真正需要的信息，而不是一个笼统的 502。
//
// 与账号冷却一致，本状态**纯内存**：重启即清空。理由相同——上游的重置时刻会变，
// 把「猜测的窗口」持久化并跨重启沿用，比丢掉它更危险（重启后重新试一次，最坏就是
// 再撞一次限流，代价远小于长期锁死一个其实已经恢复的账号）。

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

// 兜底与边界。
const (
	// modelLimitDefault：命中限流措辞、但**解析不出**重置时刻时的兜底时长。
	// 取 30 分钟：短窗口节流很常见，而长窗口（数天）的文案一定带明确时刻；
	// 解析失败时宁可只晾 30 分钟，也不要因误判把账号锁死很久。
	modelLimitDefault = 30 * time.Minute
	// modelLimitMax：单次限流的上限，防上游文案里的时刻离谱（或解析串味）导致长期锁死。
	modelLimitMax = 30 * 24 * time.Hour
)

// rateLimitSignals 是「限流措辞」信号。命中任一 + 能解析出重置时刻 = 判定为限流。
// 中英文都收，注意只放**明确指向限流**的词：像「限流」单字太泛（可能出现在正常正文里），
// 故不收。
var rateLimitSignals = []string{
	"频率限制",
	"使用量已超出",
	"请求过于频繁",
	"rate limit",
	"rate-limit",
	"too many requests",
	// 国际版（workbuddy.ai）实测措辞：usage exceeds frequency limit,
	// ... your usage will reset at 2026-09-21 13:19:38 UTC+8。
	// 与国内版「使用量已超出频率限制」同源，但用了英文 "frequency limit"，
	// 不在上面的中文/英文清单里，导致国际版 429 被误判为普通瞬时故障、
	// 走账号冷却而不是模型限流（见 relay.go 的处置路径）。
	"frequency limit",
	"usage exceeds",
}

// rateLimitResetPatterns 抓「重置时刻」。两种分隔风格都收（`-` 与 `/`），
// 秒可缺省。仅用于**严格路径**（自由文本）：必须措辞与时刻同时命中才算。
var rateLimitResetPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(\d{4})-(\d{1,2})-(\d{1,2})[ T](\d{1,2}):(\d{2})(?::(\d{2}))?`),
	regexp.MustCompile(`(\d{4})/(\d{1,2})/(\d{1,2})[ T](\d{1,2}):(\d{2})(?::(\d{2}))?`),
}

// modelLimit 是一条生效中的「账号 × 模型」限流记录。
type modelLimit struct {
	AccountID string
	Model     string
	// Until 是上游给出的恢复时刻（绝对时间）。
	Until time.Time
	// Reason 是上游原文，供排障与前端展示（不截断到字段级，仅用于人看）。
	Reason string
	SetAt  time.Time
}

// ModelLimitView 是限流记录的下发视图（诊断面 / 账号列表共用）。
type ModelLimitView struct {
	AccountID string `json:"accountId,omitempty"`
	Model     string `json:"model"`
	// Until 是恢复时刻（Unix 秒），前端按本地时区展示。
	Until int64 `json:"until"`
	// UntilText 是恢复时刻的 RFC3339（UTC），便于日志与肉眼比对。
	UntilText string `json:"untilText,omitempty"`
	// RemainingSeconds 是距离恢复的剩余秒数（视图生成时刻计算）。
	RemainingSeconds int64  `json:"remainingSeconds,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

// modelLimitKey 是「账号 × 模型」的复合键。
// 用 NUL 分隔：账号 ID 与模型名都不会含 NUL，故拼接无歧义。
func modelLimitKey(accountID, model string) string {
	return accountID + "\x00" + model
}

// hasRateLimitSignal 判断文本是否含限流措辞。
func hasRateLimitSignal(text string) bool {
	lower := strings.ToLower(text)
	for _, sig := range rateLimitSignals {
		if strings.Contains(lower, strings.ToLower(sig)) {
			return true
		}
	}
	return false
}

// parseResetTime 从文本里抓第一个可解析的重置时刻，按 loc 解释（上游文案用的是站点时区）。
func parseResetTime(text string, loc *time.Location) (time.Time, bool) {
	if loc == nil {
		loc = time.UTC
	}
	for _, re := range rateLimitResetPatterns {
		m := re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		sec := m[6]
		if sec == "" {
			sec = "00"
		}
		// 布局用 "2006-1-2 15:04:05"：月/日/时允许 1~2 位，与正则的分组一致。
		normalized := fmt.Sprintf("%s-%s-%s %s:%s:%s", m[1], m[2], m[3], m[4], m[5], sec)
		t, err := time.ParseInLocation("2006-1-2 15:04:05", normalized, loc)
		if err != nil {
			continue
		}
		return t, true
	}
	return time.Time{}, false
}

// clampModelLimitUntil 把恢复时刻收敛到「(now, now+modelLimitMax]」。
// 解析失败/已过期/离谱都各自有归宿，绝不让一个坏时刻长期锁死账号。
func clampModelLimitUntil(until, now time.Time) time.Time {
	if until.IsZero() || !until.After(now) {
		return now.Add(modelLimitDefault)
	}
	if max := now.Add(modelLimitMax); until.After(max) {
		return max
	}
	return until
}

// rateLimitFromError 宽松识别：用于**错误信封的 msg**（那里是错误说明而非模型正文），
// 故有措辞即可判定；无重置时刻时回落到 modelLimitDefault。
func rateLimitFromError(msg string, loc *time.Location, now time.Time) (time.Time, bool) {
	if !hasRateLimitSignal(msg) {
		return time.Time{}, false
	}
	if t, ok := parseResetTime(msg, loc); ok {
		return clampModelLimitUntil(t, now), true
	}
	return now.Add(modelLimitDefault), true
}

// rateLimitFromText 严格识别：用于**自由文本**（可能是模型正文）。
// 必须「措辞 + 可解析时刻」同时命中——否则用户问一句「rate limit 是什么、比如
// 2026-09-12 17:22:50」就可能把一段正常回答误判成限流。
func rateLimitFromText(text string, loc *time.Location, now time.Time) (time.Time, bool) {
	if !hasRateLimitSignal(text) {
		return time.Time{}, false
	}
	t, ok := parseResetTime(text, loc)
	if !ok {
		return time.Time{}, false
	}
	return clampModelLimitUntil(t, now), true
}

// envelopeError 判断一段 JSON 是不是上游的**错误信封**（{code,msg,data}）。
//
// 为什么需要：上游在部分失败场景用 **HTTP 200 + 信封**而不是 4xx，若不解出来，
// 中继会把「空回答」当成功下发（下游收到一个没有内容的 completion，无从分辨）。
// 判定刻意保守，避免误伤正常分片：
//   - 必须是 JSON 对象；
//   - 必须带 code 字段且 **非 0**（code 缺失或 0 都不算错误——0 是成功码）；
//   - 不能带 choices（那是正常的 chat chunk）。
func envelopeError(text string) (code int, msg string, ok bool) {
	var probe struct {
		Code    *int   `json:"code"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
		Choices []any  `json:"choices"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(text)), &probe) != nil {
		return 0, "", false
	}
	if probe.Code == nil || *probe.Code == 0 || len(probe.Choices) > 0 {
		return 0, "", false
	}
	return *probe.Code, strings.TrimSpace(firstNonEmpty(probe.Msg, probe.Message)), true
}

// -----------------------------------------------------------------------------
// 冷却簿
// -----------------------------------------------------------------------------

// markModelLimit 记录一次「账号 × 模型」限流。
// until 会先经 clampModelLimitUntil 收敛；顺带清掉已过期条目，避免 map 无限增长。
func (s *Service) markModelLimit(accountID, model string, until time.Time, reason string) {
	if accountID == "" || model == "" {
		return
	}
	now := time.Now()
	until = clampModelLimitUntil(until, now)
	// 账号标签要在取锁前解析：accountLogLabel 会经 Settings() 取 mu 读锁，
	// 若在持有 modelLimitMu 时调用会形成嵌套取锁，且日志 I/O 会拖长临界区。
	label := s.accountLogLabel(accountID)
	truncated := truncate(reason, 300)
	s.modelLimitMu.Lock()
	if s.modelLimits == nil {
		s.modelLimits = map[string]modelLimit{}
	}
	for k, l := range s.modelLimits {
		if !l.Until.After(now) {
			delete(s.modelLimits, k)
		}
	}
	s.modelLimits[modelLimitKey(accountID, model)] = modelLimit{
		AccountID: accountID,
		Model:     model,
		Until:     until,
		Reason:    truncated,
		SetAt:     now,
	}
	s.rateLimitSample = truncated
	s.rateLimitSampleAt = now.UTC().Format(time.RFC3339)
	s.modelLimitMu.Unlock()
	applog.Warn(context.Background(), "workbuddy", "model rate limited for account",
		"account", label,
		"model", model,
		"until", until.UTC().Format(time.RFC3339),
		"reason", truncated)
}

// noteRateLimit 只留痕最近的限流原文（不进冷却簿）。
// 用于**观察**：上游的措辞/字段形状可能变化，留一份原文，下次排障不用再猜。
func (s *Service) noteRateLimit(reason string) {
	s.modelLimitMu.Lock()
	s.rateLimitSample = truncate(reason, 300)
	s.rateLimitSampleAt = time.Now().UTC().Format(time.RFC3339)
	s.modelLimitMu.Unlock()
}

// modelLimitUntil 返回该「账号 × 模型」的生效截止时刻；不在限流中返回 false。
func (s *Service) modelLimitUntil(accountID, model string) (time.Time, bool) {
	if accountID == "" || model == "" {
		return time.Time{}, false
	}
	s.modelLimitMu.Lock()
	defer s.modelLimitMu.Unlock()
	l, ok := s.modelLimits[modelLimitKey(accountID, model)]
	if !ok || !l.Until.After(time.Now()) {
		return time.Time{}, false
	}
	return l.Until, true
}

// inModelLimit 报告该「账号 × 模型」是否处于限流中（选号路径每请求调用一次）。
func (s *Service) inModelLimit(accountID, model string) bool {
	_, ok := s.modelLimitUntil(accountID, model)
	return ok
}

// allUsableAccountsModelLimited 判断「确有可用账号，且它们**全部**在该模型上限流」。
//
// 返回可用账号数与最早恢复时刻。三条缺一不可：
//   - 没有可用账号时返回 false —— 那是「没账号可用」而非「模型被限流」，
//     调用方（relay 的 !attempted 分支）该报后者才是准确的；
//   - 只要有一个可用账号没被限流，就返回 false —— 那说明选号失败另有原因
//     （例如该账号正在瞬时冷却中），不该拿限流来解释。
//
// 注意：ok == false 时，usable 只是「扫到中途」的计数、无意义，调用方不得使用；
// 仅 ok == true 时 usable 才是完整的可用账号数。
func (s *Service) allUsableAccountsModelLimited(accounts []Account, model string) (usable int, until time.Time, ok bool) {
	if model == "" {
		return 0, time.Time{}, false
	}
	for _, a := range accounts {
		if !accountAvailable(a) {
			continue
		}
		// 只统计「所属区域提供该模型」的账号：否则国际独有模型会被国内账号
		// （永远不可能被它限流）稀释，导致 429 判定失效。
		if !s.accountServesModel(a, model) {
			continue
		}
		usable++
		t, limited := s.modelLimitUntil(a.ID, model)
		if !limited {
			// 存在一个没被限流的可用账号 → 不成立。
			return usable, time.Time{}, false
		}
		if until.IsZero() || t.Before(until) {
			until = t
		}
	}
	if usable == 0 || until.IsZero() {
		return usable, time.Time{}, false
	}
	return usable, until, true
}

// accountLogLabel 返回日志里可读的账号标识：昵称(ID)；查不到昵称时退回 ID。
func (s *Service) accountLogLabel(id string) string {
	if id == "" {
		return "<empty>"
	}
	for _, a := range s.Settings().Accounts {
		if a.ID == id {
			return accountLabel(a)
		}
	}
	return id
}

// accountLabel 构造「昵称(ID)」形式的账号标签，昵称缺失时只留 ID。
func accountLabel(a Account) string {
	if a.Nickname != "" && a.Nickname != a.ID {
		return fmt.Sprintf("%s(%s)", a.Nickname, a.ID)
	}
	if a.ID == "" {
		return "<empty>"
	}
	return a.ID
}

// accountUnavailableReason 用一句话说明账号此刻为何不参与选号；可用时返回空串。
// 判定顺序与 pickLeastConsumed 的跳过条件保持一致，确保「诊断到的不选」和「实际没选」同源。
func (s *Service) accountUnavailableReason(a Account, model string) string {
	switch {
	case a.Disabled:
		return "已停用"
	case a.ID == "":
		return "缺少账号标识"
	case a.AccessToken == "":
		return "无凭据"
	case tokenState(a) == "expired":
		return "token 已过期"
	}
	s.cooldownMu.Lock()
	until, cooled := s.cooldownUntil[a.ID]
	s.cooldownMu.Unlock()
	if cooled && time.Now().Before(until) {
		return "失败冷却至 " + until.UTC().Format(time.RFC3339)
	}
	if model != "" {
		if t, limited := s.modelLimitUntil(a.ID, model); limited {
			return "该模型限流至 " + t.UTC().Format(time.RFC3339)
		}
		if !s.accountServesModel(a, model) {
			return "所属区域(" + regionLabel(a.Region) + ")不提供该模型"
		}
	}
	return ""
}

// accountPoolDiagnosis 汇总账号池里每个账号此刻的状态，供「没有可用账号」时的日志排查。
// 形如：昵称(ID)=失败冷却至 ...; 昵称2(ID2)=可用; ...
func (s *Service) accountPoolDiagnosis(accounts []Account, model string) string {
	if len(accounts) == 0 {
		return "<账号池为空>"
	}
	parts := make([]string, 0, len(accounts))
	for _, a := range accounts {
		reason := s.accountUnavailableReason(a, model)
		if reason == "" {
			reason = "可用"
		}
		parts = append(parts, accountLabel(a)+"="+reason)
	}
	return strings.Join(parts, "; ")
}

// modelLimitsView 返回当前生效的全部限流记录，按恢复时刻升序（最先恢复的排前面）。
func (s *Service) modelLimitsView() []ModelLimitView {
	now := time.Now()
	s.modelLimitMu.Lock()
	defer s.modelLimitMu.Unlock()
	out := make([]ModelLimitView, 0, len(s.modelLimits))
	for _, l := range s.modelLimits {
		if !l.Until.After(now) {
			continue
		}
		out = append(out, ModelLimitView{
			AccountID:        l.AccountID,
			Model:            l.Model,
			Until:            l.Until.Unix(),
			UntilText:        l.Until.UTC().Format(time.RFC3339),
			RemainingSeconds: int64(l.Until.Sub(now).Seconds()),
			Reason:           l.Reason,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Until != out[j].Until {
			return out[i].Until < out[j].Until
		}
		if out[i].Model != out[j].Model {
			return out[i].Model < out[j].Model
		}
		return out[i].AccountID < out[j].AccountID
	})
	return out
}

// accountModelLimits 返回某账号当前被限流的模型（账号列表下发用）。
func (s *Service) accountModelLimits(accountID string) []ModelLimitView {
	all := s.modelLimitsView()
	out := make([]ModelLimitView, 0, 2)
	for _, l := range all {
		if l.AccountID != accountID {
			continue
		}
		l.AccountID = ""
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ModelLimitStat 是「某个模型在当前账号池下的可用性」汇总（模型列表下发用）。
//
// 为什么模型列表需要它：账号表能看出「谁被限流」，但用户更常问的是
// 「**这个模型现在还能不能用**」——那要看的是模型维度的聚合，两者不可互替。
type ModelLimitStat struct {
	// Limited 是当前在该模型上被限流的**可用账号**数。
	Limited int `json:"limited"`
	// Usable 是该模型上仍可用的账号数。
	Usable int `json:"usable"`
	// AllLimited 表示「有可用账号，但全部都在该模型上限流」——
	// 也就是这个模型此刻**完全打不通**（此刻再发请求会被直接回 429，且不打上游）。
	AllLimited bool `json:"allLimited"`
	// NextRecoveryAt 是最早的恢复时刻（Unix 秒；0 表示未知）。
	NextRecoveryAt int64 `json:"nextRecoveryAt,omitempty"`
}

// modelLimitStats 汇总每个模型在当前账号池下的限流情况。
//
// 只统计**可用账号**（accountAvailable）：停用/无凭据/token 已过期的账号本来就不参与选号，
// 把它们算进分母会让「全部限流」永远不成立，提示也就永远不出现。
func (s *Service) modelLimitStats(accounts []Account) map[string]ModelLimitStat {
	usable := map[string]bool{}
	for _, a := range accounts {
		if accountAvailable(a) {
			usable[a.ID] = true
		}
	}
	now := time.Now()
	s.modelLimitMu.Lock()
	limits := make([]modelLimit, 0, len(s.modelLimits))
	for _, l := range s.modelLimits {
		if l.Until.After(now) && usable[l.AccountID] {
			limits = append(limits, l)
		}
	}
	s.modelLimitMu.Unlock()

	stats := map[string]ModelLimitStat{}
	for _, l := range limits {
		st := stats[l.Model]
		st.Limited++
		if st.NextRecoveryAt == 0 || l.Until.Unix() < st.NextRecoveryAt {
			st.NextRecoveryAt = l.Until.Unix()
		}
		stats[l.Model] = st
	}
	for model, st := range stats {
		st.Usable = len(usable) - st.Limited
		if st.Usable < 0 {
			st.Usable = 0
		}
		st.AllLimited = len(usable) > 0 && st.Usable == 0
		stats[model] = st
	}
	return stats
}

// rateLimitDiag 返回限流面的诊断快照（原文 + 留痕时刻 + 当前生效记录）。
func (s *Service) rateLimitDiag() (sample string, at string, view []ModelLimitView) {
	s.modelLimitMu.Lock()
	sample, at = s.rateLimitSample, s.rateLimitSampleAt
	s.modelLimitMu.Unlock()
	return sample, at, s.modelLimitsView()
}
