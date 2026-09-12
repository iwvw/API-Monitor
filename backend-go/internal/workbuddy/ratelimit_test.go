package workbuddy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// rateLimitMsg 是上游模型级限流的**真实文案**（照抄用户实际收到的那条）。
// 用它当夹具而不是自造的短句，是为了让「措辞变了就测不过」这件事真的被测到。
const rateLimitMsg = "当前您在Deepseek-V4.1-Flash模型的使用量已超出频率限制，" +
	"可在2026-09-12 17:22:50 重置可用。您可切换其他模型或消耗积分继续使用该模型"

// newLimitService 造一个只带限流簿与权重表的服务（不碰 DB）。
func newLimitService() *Service {
	return &Service{
		creditDayUsed: map[string]float64{},
		cooldownUntil: map[string]time.Time{},
		modelLimits:   map[string]modelLimit{},
	}
}

// parseResetTime 解析出的时刻必须按**传入时区**解释：上游文案用的是站点时区，
// 若按 UTC 解释会整整差 8 小时（限流提前/延后解除）。
func TestParseResetTimeUsesGivenLocation(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skipf("缺少时区数据: %v", err)
	}
	got, ok := parseResetTime(rateLimitMsg, shanghai)
	if !ok {
		t.Fatalf("应从文案里解析出重置时刻: %s", rateLimitMsg)
	}
	want := time.Date(2026, 9, 12, 17, 22, 50, 0, shanghai)
	if !got.Equal(want) {
		t.Fatalf("重置时刻 = %v，want %v", got, want)
	}
	// 同一串按 UTC 解释应差 8 小时——证明 loc 真的起作用（而不是被忽略）。
	if utc, _ := parseResetTime(rateLimitMsg, time.UTC); utc.Equal(got) {
		t.Fatal("按 UTC 解释与按东八区解释结果相同，说明 loc 未被使用")
	}
}

// 严格路径必须「措辞 + 时刻」双命中：用户只是问「rate limit 是什么」、
// 或者在正文里恰好写了一个时刻，都不能被误判成上游限流。
func TestRateLimitFromTextRequiresBothSignalAndTime(t *testing.T) {
	loc := time.UTC
	// 与文案精度对齐到秒：文案只写到秒，若 now 带纳秒，断言差值会差不到 1 秒的零头。
	now := time.Now().Truncate(time.Second)

	if _, ok := rateLimitFromText("rate limit 是什么意思？", loc, now); ok {
		t.Fatal("只有措辞、没有时刻 → 不应判定为限流")
	}
	if _, ok := rateLimitFromText("会议改到 2026-09-12 17:22:50 开始", loc, now); ok {
		t.Fatal("只有时刻、没有限流措辞 → 不应判定为限流")
	}
	// 这里用**动态未来时刻**构造夹具：若直接拿 rateLimitMsg 里的固定日期断言
	// 「在未来」，测试会在那一天之后必然失败（时间炸弹）。固定文案只用于
	// TestParseResetTimeUsesGivenLocation —— 那条断言的是解析结果，与 now 无关。
	// 生成文案要**在目标时区里**取墙上时间（now.In(loc)）：若用本地墙钟时间再按 UTC
	// 解析，绝对时刻会整整偏出一个时区（这里 8 小时）——这正是生产代码要按站点时区
	// 解释上游文案的原因，测试夹具同样绕不开。
	future := now.In(loc).Add(48 * time.Hour).Format("2006-01-02 15:04:05")
	msg := "当前您在hy3模型的使用量已超出频率限制，可在" + future + " 重置可用"
	until, ok := rateLimitFromText(msg, loc, now)
	if !ok {
		t.Fatal("措辞与时刻都有 → 应判定为限流")
	}
	if !until.After(now) || until.Sub(now) != 48*time.Hour {
		t.Fatalf("应按文案解析出 48 小时后的重置时刻，得到 %v（差 %v）", until, until.Sub(now))
	}
	// 固定文案（真实上游原样）也要能解析 —— 只验证"解析成功"，不涉及 now。
	if _, ok := rateLimitFromText(rateLimitMsg, loc, now); !ok {
		t.Fatalf("真实文案应能解析: %s", rateLimitMsg)
	}
}

// 错误信封的 msg 是**错误说明**而非模型正文，故有措辞即可判定；
// 没有重置时刻时回落到 30 分钟兜底（短期节流很常见）。
func TestRateLimitFromErrorFallsBackToDefault(t *testing.T) {
	now := time.Now()
	until, ok := rateLimitFromError("请求过于频繁，请稍后再试", time.UTC, now)
	if !ok {
		t.Fatal("有措辞即应判定为限流")
	}
	got := until.Sub(now)
	if got < modelLimitDefault-time.Minute || got > modelLimitDefault+time.Minute {
		t.Fatalf("无时刻时应回落到 %v，得到 %v", modelLimitDefault, got)
	}
	if _, ok := rateLimitFromError("参数错误", time.UTC, now); ok {
		t.Fatal("无措辞不应判定为限流")
	}
}

// 坏时刻必须有归宿：已过期→兜底 30 分钟；离谱（10 年后）→封顶 30 天。
// 这条防的是「一个解析串味的时刻把账号长期锁死」。
func TestClampModelLimitUntil(t *testing.T) {
	now := time.Now()
	if got := clampModelLimitUntil(time.Time{}, now); got.Sub(now) != modelLimitDefault {
		t.Fatalf("零值应回落兜底，得到 %v", got)
	}
	if got := clampModelLimitUntil(now.Add(-time.Hour), now); got.Sub(now) != modelLimitDefault {
		t.Fatalf("已过去应回落兜底，得到 %v", got)
	}
	if got := clampModelLimitUntil(now.Add(365*24*time.Hour), now); got.Sub(now) != modelLimitMax {
		t.Fatalf("离谱未来应封顶 %v，得到 %v", modelLimitMax, got)
	}
	ok := now.Add(2 * time.Hour)
	if got := clampModelLimitUntil(ok, now); !got.Equal(ok) {
		t.Fatalf("合理未来应原样保留，得到 %v", got)
	}
}

// 信封判定要保守：正常 chunk（带 choices）不能被当错误信封，否则每个正常回答都会失败。
func TestEnvelopeErrorIsConservative(t *testing.T) {
	if _, _, ok := envelopeError(`{"choices":[{"delta":{"content":"hi"}}],"code":0}`); ok {
		t.Fatal("带 choices 的正常 chunk 不应判为错误信封")
	}
	if _, _, ok := envelopeError(`{"code":0,"msg":"","data":{}}`); ok {
		t.Fatal("code=0 是成功码，不应判为错误信封")
	}
	if _, _, ok := envelopeError(`{"choices":[]}`); ok {
		t.Fatal("无 code 字段不应判为错误信封")
	}
	code, msg, ok := envelopeError(`{"code":11218,"msg":"使用量已超出频率限制"}`)
	if !ok || code != 11218 || !strings.Contains(msg, "频率限制") {
		t.Fatalf("错误信封应被识别，得到 code=%d msg=%q ok=%v", code, msg, ok)
	}
}

// 【最关键的断言】模型级限流只封「账号 × 模型」：
// 同一账号在**别的模型**上必须照常可选 —— 这正是它不能复用账号冷却的原因。
func TestPickLeastConsumedSkipsModelLimitedOnlyForThatModel(t *testing.T) {
	s := newLimitService()
	a1 := validAccount("u1", "t1")
	a2 := validAccount("u2", "t2")
	accounts := []Account{a1, a2}

	// u1 在 "模型A" 上限流到 2 小时后。
	s.markModelLimit("u1", "模型A", time.Now().Add(2*time.Hour), rateLimitMsg)

	// 同一模型：跳过 u1，选 u2。
	acc, ok := s.pickLeastConsumed(accounts, "模型A")
	if !ok || acc.ID != "u2" {
		t.Fatalf("限流模型上应选 u2，得到 %v/%v", acc.ID, ok)
	}
	// 别的模型：u1 仍应被选（列表序靠前，且消耗相同）。
	acc, ok = s.pickLeastConsumed(accounts, "模型B")
	if !ok || acc.ID != "u1" {
		t.Fatalf("其它模型上 u1 必须照常可用，得到 %v/%v", acc.ID, ok)
	}
	// 限流过期后回到可选。这里直接写簿子而**不用 markModelLimit**：
	// 后者会把「已过去的时刻」收敛成兜底的 30 分钟（见 TestClampModelLimitUntil），
	// 那是识别路径的正确行为，但不适合用来模拟「时间流逝、限流自然到期」。
	s.modelLimitMu.Lock()
	s.modelLimits[modelLimitKey("u1", "模型A")] = modelLimit{
		AccountID: "u1", Model: "模型A", Until: time.Now().Add(-time.Minute),
	}
	s.modelLimitMu.Unlock()
	if acc, _ := s.pickLeastConsumed(accounts, "模型A"); acc.ID != "u1" {
		t.Fatalf("限流过期后 u1 应恢复可选，得到 %v", acc.ID)
	}
	if s.inModelLimit("u1", "模型A") {
		t.Fatal("已到期的限流不应再判定为生效")
	}
}

// 账号列表要能看出「哪个账号的哪个模型被限流到几点」，否则用户只能干等。
func TestAccountViewExposesModelLimits(t *testing.T) {
	s := newLimitService()
	s.markModelLimit("u1", "hy3", time.Now().Add(2*time.Hour), rateLimitMsg)

	view := s.toAccountView(validAccount("u1", "t1"))
	if len(view.LimitedModels) != 1 {
		t.Fatalf("应下发 1 条限流记录，得到 %d 条", len(view.LimitedModels))
	}
	got := view.LimitedModels[0]
	if got.Model != "hy3" || got.Until <= time.Now().Unix() || got.RemainingSeconds <= 0 {
		t.Fatalf("限流记录字段不对: %+v", got)
	}
	// 其它账号不带这条记录。
	if other := s.toAccountView(validAccount("u2", "t2")); len(other.LimitedModels) != 0 {
		t.Fatalf("u2 不应带限流记录，得到 %+v", other.LimitedModels)
	}
}

// 模型列表要把「这个模型还能不能用」直接告诉前端 —— 账号表回答「谁被限流」，
// 模型列表回答「还能不能用」，两者不可互替。这里覆盖全限流与部分限流两种形态。
func TestModelsEndpointExposesLimitStats(t *testing.T) {
	s := newTestService(t)
	// 直接写模型缓存：handleModels 读的是缓存，这样不产生任何上游请求。
	s.modelMu.Lock()
	s.modelCache = []ModelInfo{{ID: "hy3", DisplayName: "Hunyuan3"}}
	s.modelCacheAt = time.Now()
	s.modelMu.Unlock()
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{
			validAccount("u1", "t1"),
			validAccount("u2", "t2"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	fetch := func() (int, map[string]interface{}) {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workbuddy/models", nil))
		var body map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("解析模型列表失败: %v (%s)", err, rec.Body.String())
		}
		return rec.Code, body
	}
	firstLimit := func(b map[string]interface{}) map[string]interface{} {
		models, _ := b["models"].([]interface{})
		if len(models) == 0 {
			t.Fatalf("模型列表为空: %v", b)
		}
		entry, _ := models[0].(map[string]interface{})
		limit, _ := entry["limit"].(map[string]interface{})
		if limit == nil {
			t.Fatalf("模型条目应带 limit 字段: %v", entry)
		}
		return limit
	}

	// 只有一个账号被限流：不阻断，仍应给出「部分限流」信息。
	s.markModelLimit("u1", "hy3", time.Now().Add(time.Hour), rateLimitMsg)
	if code, body := fetch(); code != http.StatusOK {
		t.Fatalf("模型列表应 200，得到 %d", code)
	} else if l := firstLimit(body); l["allLimited"] != false || l["limited"] != float64(1) || l["usable"] != float64(1) {
		t.Fatalf("部分限流应 limited=1/usable=1/allLimited=false，得到 %v", l)
	}

	// 两个账号都被限流 → allLimited，且给出**最早**恢复时刻（u1 的 1 小时 < u2 的 2 小时）。
	s.markModelLimit("u2", "hy3", time.Now().Add(2*time.Hour), rateLimitMsg)
	_, body := fetch()
	l := firstLimit(body)
	if l["allLimited"] != true || l["limited"] != float64(2) || l["usable"] != float64(0) {
		t.Fatalf("全部限流应 allLimited=true/limited=2/usable=0，得到 %v", l)
	}
	if got, _ := l["nextRecoveryAt"].(float64); got <= float64(time.Now().Unix()) ||
		got > float64(time.Now().Add(70*time.Minute).Unix()) {
		t.Fatalf("nextRecoveryAt 应是最早的恢复时刻（约 1 小时后），得到 %v", got)
	}

	// 关键的边界：**被停用的账号不进分母**。停掉那个「被限流」的账号后，
	// 模型不该再显示限流 —— 否则一个早就停用的旧账号会让这个提示永远亮着。
	s.modelLimitMu.Lock()
	s.modelLimits = map[string]modelLimit{}
	s.modelLimitMu.Unlock()
	s.markModelLimit("u1", "hy3", time.Now().Add(time.Hour), rateLimitMsg)

	acc, ok := s.findAccount("u1")
	if !ok {
		t.Fatal("找不到 u1")
	}
	acc.Disabled = true
	if err := s.upsertAccount(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	_, body = fetch()
	models, _ := body["models"].([]interface{})
	entry, _ := models[0].(map[string]interface{})
	if _, has := entry["limit"]; has {
		t.Fatalf("被停用账号的限流不应下发（会让提示常亮）: %v", entry["limit"])
	}
}

// 端到端：首号撞模型级限流 → 换号成功；且**只封该模型**、不进账号冷却。
func TestRelayFailoverOnModelRateLimit(t *testing.T) {
	// 首号用「200 + 错误信封」下发（且前面有空行，验证窥探能跨过空行），
	// 二号正常回答 —— 上游确有这种失败形态。
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		if token == "Bearer t2" {
			return http.StatusOK, okSSE
		}
		return http.StatusOK, "\n\ndata: {\"code\":11218,\"msg\":\"" + rateLimitMsg + "\"}\n\n"
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{
			validAccount("u1", "t1"),
			validAccount("u2", "t2"),
		},
	}); err != nil {
		t.Fatal(err)
	}

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("换号后应成功，得到 %d: %s", rec.Code, rec.Body.String())
	}
	got := getAuths()
	if len(got) != 2 || got[0] != "Bearer t1" || got[1] != "Bearer t2" {
		t.Fatalf("应依次尝试两个账号，实际 %v", got)
	}
	if !s.inModelLimit("u1", "hy3") {
		t.Fatal("u1 应在 hy3 上被标记限流")
	}
	if s.inCooldown("u1") {
		t.Fatal("模型级限流**不应**把整个账号打进冷却（会连坐它的其它模型）")
	}
	if s.inModelLimit("u2", "hy3") {
		t.Fatal("成功的 u2 不应被限流")
	}
}

// 该模型在所有账号上都限流 → **不再打扰上游**，直接回 429 并给出最早恢复时刻。
func TestRelayAllAccountsModelLimitedReturns429WithoutUpstreamCall(t *testing.T) {
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		return http.StatusOK, okSSE
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled: true,
		Accounts: []Account{
			validAccount("u1", "t1"),
			validAccount("u2", "t2"),
		},
	}); err != nil {
		t.Fatal(err)
	}
	s.markModelLimit("u1", "hy3", time.Now().Add(time.Hour), rateLimitMsg)
	s.markModelLimit("u2", "hy3", time.Now().Add(2*time.Hour), rateLimitMsg)

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("应返回 429，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rate_limit_error") ||
		!strings.Contains(rec.Body.String(), "最早将于") {
		t.Fatalf("错误体应说明限流与恢复时刻: %s", rec.Body.String())
	}
	if got := getAuths(); len(got) != 0 {
		t.Fatalf("全部账号限流时不应再请求上游，实际收到 %v", got)
	}
	// 别的模型不受影响：同一个服务换个模型应正常转发。
	rec = serveChat(t, s, `{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("其它模型应照常可用，得到 %d: %s", rec.Code, rec.Body.String())
	}
}

// 上游用 4xx 回限流文案：默认 4xx 不可重试，但限流必须被识别为可重试，
// 且最终错误要带上恢复时刻（429 + rate_limit_error），而不是含糊的 502。
func TestRelayRateLimitOnHTTPErrorReturns429WithReset(t *testing.T) {
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		return http.StatusForbidden, "当前您在hy3模型的使用量已超出频率限制，可在2026-12-31 10:00:00 重置可用"
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:  true,
		Accounts: []Account{validAccount("u1", "t1")},
	}); err != nil {
		t.Fatal(err)
	}

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("限流应回 429 而非 502，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "恢复可用") {
		t.Fatalf("错误体应带恢复时刻: %s", rec.Body.String())
	}
	if got := getAuths(); len(got) != 1 {
		t.Fatalf("只有一个账号时只应请求一次，实际 %v", got)
	}
	if _, ok := s.modelLimitUntil("u1", "hy3"); !ok {
		t.Fatal("应在 hy3 上记录限流")
	}
	if s.inCooldown("u1") {
		t.Fatal("限流不应同时把账号打进账号冷却")
	}
}

// 普通业务错误信封（非限流）也不能被当成「成功但空回答」透给下游 ——
// 那会让下游收到一个没有内容的 completion，既不是错误也无从分辨。
func TestRelayBusinessErrorEnvelopeBecomesErrorNotEmptyAnswer(t *testing.T) {
	_, _ = failoverUpstream(t, func(token string) (int, string) {
		return http.StatusOK, "data: {\"code\":11001,\"msg\":\"请求参数不合法\"}\n\n"
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:  true,
		Accounts: []Account{validAccount("u1", "t1")},
	}); err != nil {
		t.Fatal(err)
	}

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("业务错误信封不应被当成功下发: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "11001") {
		t.Fatalf("错误体应带上游业务码: %s", rec.Body.String())
	}
}

// 【回归】「一个号都没打成」**不等于**「所有账号都被该模型限流」。
//
// 反例：一个早已停用的账号身上留着一条历史限流记录（恢复时刻在很远的将来），
// 而真正可用的账号只是因为瞬时故障在冷却中 —— 此时选号同样返回 false。
// 若不加区分地把限流簿里最早的时刻报出去，用户会看到「该模型 N 天后恢复」，
// 而那个 N 天来自一个根本不参与转发的账号，纯属误导（实际几分钟后就能用）。
func TestNoAttemptDoesNotBlamModelLimitFromUnusableAccount(t *testing.T) {
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		return http.StatusOK, okSSE
	})

	s := newTestService(t)
	dead := validAccount("dead", "t9")
	dead.Disabled = true // 不参与选号，但限流簿里仍留着它的记录
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:  true,
		Accounts: []Account{dead, validAccount("u1", "t1")},
	}); err != nil {
		t.Fatal(err)
	}
	// 停用账号在 hy3 上的历史限流：3 天后才恢复（足够显眼，一旦被报出就能测到）。
	s.markModelLimit("dead", "hy3", time.Now().Add(72*time.Hour), rateLimitMsg)
	// 唯一可用账号处于瞬时冷却中 → pickAccount 一个号都选不出来。
	s.markCooldown("u1")

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code == http.StatusTooManyRequests {
		t.Fatalf("不应把停用账号的历史限流当成「全部账号被限流」报 429: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "频率限制") {
		t.Fatalf("错误体不应声称模型被限流: %s", rec.Body.String())
	}
	if got := getAuths(); len(got) != 0 {
		t.Fatalf("没有可用账号时不应请求上游，实际收到 %v", got)
	}
}

// 【回归】确有可用账号、但只有一个没被限流时，不构成「全部限流」。
// 选号失败另有原因（这里是瞬时冷却），应报常规错误而非限流 429。
func TestAllUsableAccountsModelLimitedRequiresEveryUsableAccount(t *testing.T) {
	s := newLimitService()
	limited := validAccount("u1", "t1")
	free := validAccount("u2", "t2")
	dead := validAccount("dead", "t9")
	dead.Disabled = true

	s.markModelLimit("u1", "hy3", time.Now().Add(time.Hour), rateLimitMsg)
	s.markModelLimit("dead", "hy3", time.Now().Add(72*time.Hour), rateLimitMsg)

	// u2 没被限流 → 不成立（哪怕 u1 与已停用的 dead 都在限流）。
	if _, _, ok := s.allUsableAccountsModelLimited([]Account{limited, free, dead}, "hy3"); ok {
		t.Fatal("存在未被限流的可用账号时不应判定为「全部限流」")
	}
	// 去掉 u2 后：可用账号只剩 u1，且它被限流 → 成立，且不得把停用账号的 72 小时算进来。
	usable, until, ok := s.allUsableAccountsModelLimited([]Account{limited, dead}, "hy3")
	if !ok || usable != 1 {
		t.Fatalf("应判定为「全部限流」且可用账号数为 1，得到 usable=%d ok=%v", usable, ok)
	}
	if d := time.Until(until); d > 2*time.Hour {
		t.Fatalf("最早恢复时刻不应来自停用账号（应约 1 小时），得到 %v", d)
	}
	// 一个可用账号都没有 → 不是「模型被限流」，而是「没账号可用」。
	if _, _, ok := s.allUsableAccountsModelLimited([]Account{dead}, "hy3"); ok {
		t.Fatal("没有可用账号时不应判定为「全部限流」")
	}
}

// 【回归】SSE 窥探必须能跨过开头的一串心跳/空行看到错误信封。
// 上游确有这种形态；窥探上限是 streamPeekLines 行，错误信封落在其内就必须被拦下。
func TestStreamPeekSkipsHeartbeatsBeforeErrorEnvelope(t *testing.T) {
	// 3 个空行 + 1 个注释心跳，然后才是错误信封 —— 真负载出现前全是噪声。
	body := "\n\ndata: \n: keep-alive\ndata: {\"code\":11218,\"msg\":\"" + rateLimitMsg + "\"}\n\n"
	getAuths, _ := failoverUpstream(t, func(token string) (int, string) {
		return http.StatusOK, body
	})

	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:  true,
		Accounts: []Account{validAccount("u1", "t1")},
	}); err != nil {
		t.Fatal(err)
	}

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("心跳之后的错误信封必须被拦下，不能当成功下发: %s", rec.Body.String())
	}
	if !s.inModelLimit("u1", "hy3") {
		t.Fatal("应被识别为 hy3 上的模型级限流")
	}
	_ = getAuths
}

// 【回归】truncate 不得把中文切成半个字符（那会在 JSON/日志里变成乱码 U+FFFD）。
func TestTruncateKeepsUTF8Boundary(t *testing.T) {
	s := strings.Repeat("限", 300) // 每个汉字 3 字节
	got := truncate(s, 100)
	if !utf8.ValidString(got) {
		t.Fatalf("截断结果不是合法 UTF-8: %q", got)
	}
	if len(got) > 100 {
		t.Fatalf("截断后长度应 <= 100，得到 %d", len(got))
	}
	if !strings.HasPrefix(s, got) {
		t.Fatal("截断结果应是原串前缀")
	}
	// 落在字符中间的边界：99 字节正好切开第 33 个字。
	if got := truncate(s, 99); !utf8.ValidString(got) || len(got) != 99 {
		t.Fatalf("非对齐边界应退到完整字符，得到 len=%d valid=%v", len(got), utf8.ValidString(got))
	}
	// 短于上限时原样返回。
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("未超限应原样返回，得到 %q", got)
	}
}
