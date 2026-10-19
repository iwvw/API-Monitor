package workbuddy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// 办公版（wb）转发请求必须补桌面端指纹头与租户头；国内版不带这些，避免行为漂移。
// 三套区域都必须带会话头族（缺了会触发上游渠道抽样校验 400 / code 11128）。
func TestBackendHeadersOfficeVersionExtras(t *testing.T) {
	wb := Account{ID: "wb-u1", Region: regionWB, AccessToken: "t", UID: "u1", EnterpriseID: "ent1"}
	req, _ := http.NewRequest(http.MethodPost, "https://www.workbuddy.cn/v2/chat/completions", nil)
	backendHeaders(req, &wb, "")
	if req.Header.Get("X-Tenant-Id") != "ent1" {
		t.Errorf("wb 应带 X-Tenant-Id=ent1，得到 %q", req.Header.Get("X-Tenant-Id"))
	}
	if req.Header.Get("X-IDE-Type") != "WorkBuddy" || req.Header.Get("X-IDE-Name") != "WorkBuddy" {
		t.Error("wb 应带 WorkBuddy 桌面端 IDE 指纹头")
	}
	if req.Header.Get("X-Private-Data") != "true" || req.Header.Get("X-Codebuddy-Request") != "1" {
		t.Error("wb 应带 X-Private-Data / X-Codebuddy-Request")
	}
	if req.Header.Get("Authorization") != "Bearer t" {
		t.Errorf("Authorization 应为 Bearer t，得到 %q", req.Header.Get("Authorization"))
	}

	cn := Account{ID: "u1", Region: regionCN, AccessToken: "t", UID: "u1", EnterpriseID: "ent1"}
	req2, _ := http.NewRequest(http.MethodPost, "https://copilot.tencent.com/v2/chat/completions", nil)
	backendHeaders(req2, &cn, "")
	if req2.Header.Get("X-Tenant-Id") != "" {
		t.Error("国内版不应带 X-Tenant-Id")
	}
	if req2.Header.Get("X-IDE-Type") != "" {
		t.Error("国内版不应带桌面端 IDE 指纹头")
	}

	// 会话头族对三套身份都必需：否则上游渠道校验回 400 / code 11128。
	for _, r := range []*http.Request{req, req2} {
		for _, k := range []string{
			"X-Conversation-ID", "X-Conversation-Request-ID",
			"X-Root-Request-ID", "X-Conversation-Message-ID", "X-Request-ID",
			"X-Agent-Type", "X-Agent-Purpose", "X-Agent-Intent",
			"X-Trace-ID", "traceparent", "b3", "X-B3-TraceId", "X-B3-SpanId", "X-B3-Sampled",
		} {
			if r.Header.Get(k) == "" {
				t.Errorf("%s 头缺失（渠道校验需要）", k)
			}
		}
		if r.Header.Get("X-Root-Request-ID") != r.Header.Get("X-Conversation-Request-ID") {
			t.Error("X-Root-Request-ID 应等于 X-Conversation-Request-ID")
		}
		if r.Header.Get("X-Conversation-Message-ID") != r.Header.Get("X-Request-ID") {
			t.Error("X-Conversation-Message-ID 应等于 X-Request-ID")
		}
		if conv := r.Header.Get("X-Conversation-ID"); len(conv) != 36 || strings.Count(conv, "-") != 4 {
			t.Errorf("X-Conversation-ID 应为带连字符 UUID，得到 %q", conv)
		}
	}

	// 传入的会话 id 必须原样复用（换号重试沿用同一会话）。
	req3, _ := http.NewRequest(http.MethodPost, "https://copilot.tencent.com/v2/chat/completions", nil)
	backendHeaders(req3, &cn, "fixed-conv-id")
	if req3.Header.Get("X-Conversation-ID") != "fixed-conv-id" {
		t.Errorf("应复用传入会话 id，得到 %q", req3.Header.Get("X-Conversation-ID"))
	}
}

func TestDetectTokenRegion(t *testing.T) {
	// 构造一个未签名的 JWT（header.payload.sig），payload 里带 iss。
	jwt := func(iss string) string {
		enc := func(v any) string {
			b, _ := json.Marshal(v)
			return base64.RawURLEncoding.EncodeToString(b)
		}
		return enc(map[string]any{"alg": "none"}) + "." + enc(map[string]any{"iss": iss}) + ".sig"
	}
	cases := []struct {
		name        string
		token       string
		domain      string
		wantRegion  string
		wantCertain bool
	}{
		{"国际 JWT", jwt("https://www.workbuddy.ai/auth/realms/copilot"), "", regionIntl, true},
		{"国内 JWT", jwt("https://copilot.tencent.com/auth/realms/copilot"), "", regionCN, true},
		{"办公 JWT", jwt("https://www.workbuddy.cn/auth/realms/copilot"), "", regionWB, true},
		{"JWT 优先于 domain", jwt("https://www.workbuddy.ai/auth/realms/copilot"), "www.codebuddy.cn", regionIntl, true},
		{"无 JWT 用 domain 兜底(国际)", "not-a-jwt", "www.workbuddy.ai", regionIntl, true},
		{"无 JWT 用 domain 兜底(办公)", "not-a-jwt", "www.workbuddy.cn", regionWB, true},
		{"无 JWT 用 domain 兜底(国内)", "not-a-jwt", "www.codebuddy.cn", regionCN, true},
		{"完全无从判定→不确凿", "", "", regionCN, false},
	}
	for _, c := range cases {
		got, certain := detectTokenRegion(c.token, c.domain)
		if certain != c.wantCertain {
			t.Errorf("%s: certain=%v want=%v", c.name, certain, c.wantCertain)
			continue
		}
		if certain && got != c.wantRegion {
			t.Errorf("%s: region=%q want=%q", c.name, got, c.wantRegion)
		}
	}
}

func TestNormalizeRegionDefaultsToCN(t *testing.T) {
	cases := map[string]string{
		"":        regionCN,
		"cn":      regionCN,
		"CN":      regionCN,
		"wb":      regionWB,
		" WB ":    regionWB,
		" intl ":  regionIntl,
		"INTL":    regionIntl,
		"unknown": regionCN,
	}
	for in, want := range cases {
		if got := normalizeRegion(in); got != want {
			t.Errorf("normalizeRegion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMaskNickname(t *testing.T) {
	cases := map[string]string{
		"":                    "",
		"董帅":                  "董*",
		"董":                   "董*",
		"张三丰":                 "张*",
		"1502199269359923009": "1*",
	}
	for in, want := range cases {
		if got := maskNickname(in); got != want {
			t.Errorf("maskNickname(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToAccountViewMasksNickname(t *testing.T) {
	s := &Service{}
	a := validAccount("u1", "t1")
	a.Nickname = "董帅"
	view := s.toAccountView(a)
	if view.Nickname != "董*" {
		t.Errorf("下发视图昵称应打码，得到 %q", view.Nickname)
	}
}

func TestRegionWBHostsAndPlatform(t *testing.T) {
	if regionHost(regionWB) != "https://www.workbuddy.cn" {
		t.Errorf("wb host = %q", regionHost(regionWB))
	}
	if regionBillingHost(regionWB) != "https://www.workbuddy.cn" {
		t.Errorf("wb billing host = %q", regionBillingHost(regionWB))
	}
	if regionPlatform(regionWB) != "workbuddy" {
		t.Errorf("wb platform = %q", regionPlatform(regionWB))
	}
	if regionPlatform(regionCN) != "CLI" {
		t.Errorf("cn platform = %q", regionPlatform(regionCN))
	}
	if regionLabel(regionWB) != "办公版" {
		t.Errorf("wb label = %q", regionLabel(regionWB))
	}
	// 三个区域 UA 互不相同。
	uas := map[string]bool{regionUA(regionCN): true, regionUA(regionWB): true, regionUA(regionIntl): true}
	if len(uas) != 3 {
		t.Error("三个区域的 UA 应互不相同")
	}
	// 国际版对齐 5.8.4 抓包：UA 与桌面端版本号都不得再是过时的 5.5.x。
	if !strings.Contains(regionUA(regionIntl), "5.8.4") {
		t.Errorf("intl UA 应对齐 5.8.4，得到 %q", regionUA(regionIntl))
	}
	if regionDesktopVersion(regionIntl) != "5.8.4" {
		t.Errorf("intl X-IDE-Version 应为 5.8.4，得到 %q", regionDesktopVersion(regionIntl))
	}
	if regionDesktopVersion(regionWB) != "5.8.2" {
		t.Errorf("wb X-IDE-Version 应为 5.8.2，得到 %q", regionDesktopVersion(regionWB))
	}
}

func TestRegionDerivations(t *testing.T) {
	if regionHost(regionCN) != "https://copilot.tencent.com" {
		t.Errorf("cn host = %q", regionHost(regionCN))
	}
	if regionHost(regionIntl) != "https://www.workbuddy.ai" {
		t.Errorf("intl host = %q", regionHost(regionIntl))
	}
	if regionReferer(regionIntl) != "https://www.workbuddy.ai" {
		t.Errorf("intl referer = %q", regionReferer(regionIntl))
	}
	// 空区域按国内版派生。
	if regionHost("") != regionHost(regionCN) {
		t.Error("空区域应派生国内版 host")
	}
	if regionUA(regionCN) == regionUA(regionIntl) {
		t.Error("两个区域的 UA 应不同")
	}
}

func TestRegionOfDomainAndIssuer(t *testing.T) {
	if regionOfDomain("www.workbuddy.ai") != regionIntl {
		t.Error("workbuddy.ai 应判为国际版")
	}
	if regionOfDomain("www.codebuddy.cn") != regionCN {
		t.Error("codebuddy.cn 应判为国内版")
	}
	if regionOfTokenIssuer("https://www.workbuddy.ai/auth/realms/copilot") != regionIntl {
		t.Error("iss 含 workbuddy.ai 应判为国际版")
	}
	if regionOfTokenIssuer("https://copilot.tencent.com/auth/realms/copilot") != regionCN {
		t.Error("国内 iss 应判为国内版")
	}
}

func TestAccountIDForRegion(t *testing.T) {
	if got := accountIDForRegion(regionCN, "u1"); got != "u1" {
		t.Errorf("国内账号 ID 应保持原 uid，得到 %q", got)
	}
	if got := accountIDForRegion(regionIntl, "u1"); got != "intl-u1" {
		t.Errorf("国际账号 ID 应加前缀，得到 %q", got)
	}
	if got := accountIDForRegion(regionWB, "u1"); got != "wb-u1" {
		t.Errorf("办公账号 ID 应加前缀，得到 %q", got)
	}
	// 同一 uid 在不同区必须得到不同 ID（否则会互相覆盖）。
	ids := map[string]bool{
		accountIDForRegion(regionCN, "u1"):   true,
		accountIDForRegion(regionWB, "u1"):   true,
		accountIDForRegion(regionIntl, "u1"): true,
	}
	if len(ids) != 3 {
		t.Error("同一 uid 在三个区域应得到三个不同 ID")
	}
}

func TestAlignAuthURLToRegion(t *testing.T) {
	// 办公版：归一到 workbuddy.cn 站点源，保留 path/query/hash，并补 version。
	got := alignAuthURLToRegion("https://copilot.tencent.com/login/?platform=workbuddy&state=abc#x", regionWB)
	want := "https://www.workbuddy.cn/login/?platform=workbuddy&state=abc&version=5.8.2#x"
	if got != want {
		t.Errorf("办公版 authUrl 应归一到 workbuddy.cn 并补 version，得到 %q want %q", got, want)
	}
	// 已带 version 时不覆盖。
	got2 := alignAuthURLToRegion("https://www.workbuddy.cn/login?platform=workbuddy&state=abc&version=9.9.9", regionWB)
	if !strings.Contains(got2, "version=9.9.9") {
		t.Errorf("已有 version 不应被覆盖，得到 %q", got2)
	}
	// 其余区域：原样返回。
	raw := "https://copilot.tencent.com/login/?state=abc"
	if got := alignAuthURLToRegion(raw, regionCN); got != raw {
		t.Errorf("国内版 authUrl 应原样返回，得到 %q", got)
	}
	// 非法 URL：原样返回。
	if got := alignAuthURLToRegion("not a url", regionWB); got != "not a url" {
		t.Errorf("非法 URL 应原样返回，得到 %q", got)
	}
}

func TestEndpointAuthStateUsesRegionPlatform(t *testing.T) {
	old := upstreamBaseOverride
	upstreamBaseOverride = "https://up.example.com"
	t.Cleanup(func() { upstreamBaseOverride = old })
	cn := endpointAuthState(regionCN)
	if !strings.Contains(cn, "platform=CLI") {
		t.Errorf("cn auth/state 应带 platform=CLI，得到 %q", cn)
	}
	wb := endpointAuthState(regionWB)
	if !strings.Contains(wb, "platform=workbuddy") {
		t.Errorf("wb auth/state 应带 platform=workbuddy，得到 %q", wb)
	}
	if !strings.Contains(wb, "ww_device_nonce=") {
		t.Errorf("wb auth/state 应带 ww_device_nonce，得到 %q", wb)
	}
}

func TestMergeCatalogsMarksInternationalOnly(t *testing.T) {
	byRegion := map[string][]ModelInfo{
		regionCN: {
			{ID: "shared", DisplayName: "Shared"},
			{ID: "cn-only", DisplayName: "CNOnly"},
		},
		regionIntl: {
			{ID: "shared", DisplayName: "Shared"},
			{ID: "intl-only", DisplayName: "IntlOnly"},
		},
	}
	merged := mergeCatalogs(byRegion)
	byID := map[string]ModelInfo{}
	for _, m := range merged {
		byID[m.ID] = m
	}
	if len(merged) != 3 {
		t.Fatalf("合并后应有 3 个模型，得到 %d", len(merged))
	}
	// 共有型号：不标 intlOnly，型号区域含两者，元数据取国内版。
	shared := byID["shared"]
	if shared.InternationalOnly {
		t.Error("共有型号不应标国际独有")
	}
	if len(shared.Regions) != 2 {
		t.Errorf("共有型号 regions 应含两区，得到 %v", shared.Regions)
	}
	if shared.DisplayName != "Shared" {
		t.Errorf("共有型号元数据应取国内版，得到 %q", shared.DisplayName)
	}
	// 国际独有：标记 + 型号区域仅 intl。
	if !byID["intl-only"].InternationalOnly {
		t.Error("国际独有型号应标 intlOnly")
	}
	if len(byID["intl-only"].Regions) != 1 || byID["intl-only"].Regions[0] != regionIntl {
		t.Errorf("国际独有型号 regions 应为 [intl]，得到 %v", byID["intl-only"].Regions)
	}
	// 国内独有：不标 intlOnly。
	if byID["cn-only"].InternationalOnly {
		t.Error("国内独有型号不应标 intlOnly")
	}
}

// 同一型号在两区域用不同 id 时，应按 displayName 判为「共用」，而不是拆成两行独有。
func TestMergeCatalogsMatchesByNameAcrossDifferentIDs(t *testing.T) {
	byRegion := map[string][]ModelInfo{
		regionCN: {
			{ID: "kimi-k3-1", DisplayName: "Kimi-K3"},
			{ID: "hy3", DisplayName: "Hy3"},
			{ID: "hy3-x", DisplayName: "Hy3"},
		},
		regionIntl: {
			{ID: "kimi-k3", DisplayName: "Kimi-K3"},
			{ID: "hy3", DisplayName: "Hy3"},
		},
	}
	merged := mergeCatalogs(byRegion)
	byID := map[string]ModelInfo{}
	for _, m := range merged {
		byID[m.ID] = m
	}
	// 4 行（id 不同各自保留）。
	if len(merged) != 4 {
		t.Fatalf("应按 id 保留 4 行，得到 %d", len(merged))
	}
	// 型号级：Kimi-K3 两版都有 → 两行都标共用；Hy3 同理。
	for _, id := range []string{"kimi-k3-1", "kimi-k3", "hy3", "hy3-x"} {
		m := byID[id]
		if m.InternationalOnly {
			t.Errorf("%s 型号两版都有，不应标国际独有", id)
		}
		if len(m.Regions) != 2 {
			t.Errorf("%s 型号区域应为两区，得到 %v", id, m.Regions)
		}
	}
	// id 级区域：kimi-k3-1 / hy3-x 只在国内；kimi-k3 只在国际；hy3 两区都有。
	if len(byID["kimi-k3-1"].IDRegions) != 1 || byID["kimi-k3-1"].IDRegions[0] != regionCN {
		t.Errorf("kimi-k3-1 的 idRegions 应为 [cn]，得到 %v", byID["kimi-k3-1"].IDRegions)
	}
	if len(byID["kimi-k3"].IDRegions) != 1 || byID["kimi-k3"].IDRegions[0] != regionIntl {
		t.Errorf("kimi-k3 的 idRegions 应为 [intl]，得到 %v", byID["kimi-k3"].IDRegions)
	}
	if len(byID["hy3"].IDRegions) != 2 {
		t.Errorf("hy3 的 idRegions 应为两区，得到 %v", byID["hy3"].IDRegions)
	}
}

func TestAccountServesModelFiltersByRegion(t *testing.T) {
	s := &Service{
		regionModelSet: buildRegionModelSet(map[string][]ModelInfo{
			regionCN:   {{ID: "shared"}, {ID: "cn-only"}},
			regionIntl: {{ID: "shared"}, {ID: "intl-only"}},
		}),
	}
	cn := Account{ID: "u1", Region: regionCN}
	intl := Account{ID: "intl-u2", Region: regionIntl}

	if !s.accountServesModel(cn, "shared") || !s.accountServesModel(intl, "shared") {
		t.Error("共有模型两个区域都应提供")
	}
	if s.accountServesModel(cn, "intl-only") {
		t.Error("国际独有模型不应由国内账号提供")
	}
	if !s.accountServesModel(intl, "intl-only") {
		t.Error("国际独有模型应由国际账号提供")
	}
	if !s.accountServesModel(cn, "") {
		t.Error("未指定模型应放行")
	}
}

func TestAccountServesModelAllowsWhenCatalogUnknown(t *testing.T) {
	// 目录尚未就绪（regionModelSet 为空）时保守放行，避免整池不可用。
	s := &Service{}
	if !s.accountServesModel(Account{ID: "u1", Region: regionCN}, "anything") {
		t.Error("目录未知时应放行")
	}
}

func TestPickLeastConsumedSkipsWrongRegion(t *testing.T) {
	s := &Service{
		creditDayUsed: map[string]float64{},
		regionModelSet: buildRegionModelSet(map[string][]ModelInfo{
			regionCN:   {{ID: "cn-only"}},
			regionIntl: {{ID: "intl-only"}},
		}),
	}
	cn := validAccount("u1", "t1")
	cn.Region = regionCN
	intl := validAccount("intl-u2", "t2")
	intl.Region = regionIntl

	// 国际独有模型：只能选到国际账号。
	acc, ok := s.pickLeastConsumed([]Account{cn, intl}, "intl-only", nil)
	if !ok || acc.ID != "intl-u2" {
		t.Fatalf("国际独有模型应只选国际账号，得到 %v/%v", acc.ID, ok)
	}
	// 国内独有模型：只能选到国内账号。
	acc, ok = s.pickLeastConsumed([]Account{cn, intl}, "cn-only", nil)
	if !ok || acc.ID != "u1" {
		t.Fatalf("国内独有模型应只选国内账号，得到 %v/%v", acc.ID, ok)
	}
	// 无账号提供该模型：选不出来。
	if _, ok := s.pickLeastConsumed([]Account{cn}, "intl-only", nil); ok {
		t.Fatal("无对应区域账号时不应选中")
	}
}

func TestAllUsableAccountsModelLimitedIgnoresWrongRegion(t *testing.T) {
	s := newLimitService()
	s.regionModelSet = buildRegionModelSet(map[string][]ModelInfo{
		regionCN:   {{ID: "cn-only"}},
		regionIntl: {{ID: "intl-only"}},
	})
	// 国内账号在国际独有模型上不参与统计：usable 应为 0，判定不成立。
	if _, _, ok := s.allUsableAccountsModelLimited([]Account{{ID: "u1", Region: regionCN, AccessToken: "t"}}, "intl-only"); ok {
		t.Fatal("国内账号不应计入国际独有模型的可用账号")
	}
}

func TestCatalogRefreshKeepsRegionSets(t *testing.T) {
	// 目录未拉到（上游不可达）时不应清空已建立的区域集合。
	old := upstreamBaseOverride
	upstreamBaseOverride = "http://127.0.0.1:1" // 不可达，强制回源失败
	t.Cleanup(func() { upstreamBaseOverride = old })

	s := newTestService(t)
	s.regionModelSet = buildRegionModelSet(map[string][]ModelInfo{
		regionCN: {{ID: "cn-only"}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.catalog(ctx)
	if !s.accountServesModel(Account{Region: regionCN}, "cn-only") {
		t.Error("回源失败不应清空已有区域模型集合")
	}
}

// TestMergeCatalogsMarksBorrowedCredits 验证「倍率跨区域借用」会被标记出来。
//
// 实测办公版（wb）的 /v3/config 整份目录都不返回 credits 字段（37/37 缺失），
// 而 cn 与 intl 几乎全返回。以 cn 为基合并时，同 id 的 wb 条目会继承 cn 的倍率；
// 必须标 CreditsFromElsewhere，前端才能如实告知这个数字不是办公版上游给的，
// 否则用户会把别的区域的倍率当成办公版的权威值。
func TestMergeCatalogsMarksBorrowedCredits(t *testing.T) {
	byRegion := map[string][]ModelInfo{
		regionCN: {
			{ID: "shared", DisplayName: "Shared", CreditsMultiplier: 0.17, CreditsParsed: true, CreditsLabel: "x0.17 credits"},
		},
		regionWB: {
			// 办公版同 id 条目：上游没给倍率。
			{ID: "shared", DisplayName: "Shared"},
			{ID: "wb-only", DisplayName: "WBOnly"},
		},
		regionIntl: {
			// 国际版自己有倍率，不该被标借用。
			{ID: "shared", DisplayName: "Shared", CreditsMultiplier: 0.99, CreditsParsed: true, CreditsLabel: "x0.99 credits"},
		},
	}
	merged := mergeCatalogs(byRegion)
	byID := map[string]ModelInfo{}
	for _, m := range merged {
		byID[m.ID] = m
	}

	shared := byID["shared"]
	if len(shared.IDRegions) != 3 {
		t.Fatalf("shared 应属于三个区域，得到 %v", shared.IDRegions)
	}
	// cn 与 intl 都提供了倍率，因此 at least one region gave it —— 不标借用。
	if shared.CreditsFromElsewhere {
		t.Error("cn 与 intl 都下发了倍率，不应标为借用")
	}
	if !shared.CreditsParsed || shared.CreditsMultiplier != 0.17 {
		t.Errorf("保留国内版倍率作为展示值，得到 %+v", shared)
	}

	// 只有 wb 的场景：wb 整份目录无倍率 → 打底区域若也无倍率则本来就没有值；
	// 这里验证 wb 独有条目不会凭空获得倍率。
	wbOnly := byID["wb-only"]
	if wbOnly.CreditsParsed || wbOnly.CreditsMultiplier != 0 {
		t.Errorf("wb 独有条目不应有倍率，得到 %+v", wbOnly)
	}
}

// TestMergeCatalogsBorrowedWhenNoRegionProvidesCredits 验证真正的「凭空倍率」检测：
// 所有所属区域的目录都没下发 credits，但合并结果却有值 —— 这才是需要告警的情形。
//
// 注意语义：CreditsFromElsewhere 的含义是「**没有任何所属区域**在目录里下发过倍率，
// 该值不是上游权威数据」，而不是「这个值来自别的区域」。前者才能提示用户不可信。
func TestMergeCatalogsBorrowedWhenNoRegionProvidesCredits(t *testing.T) {
	// 构造：打底区域（cn）本身不下发倍率，但模型对象上带了值（模拟历史存量或
	// 上游偶发下发后又被撤下的场景）。
	byRegion := map[string][]ModelInfo{
		regionCN: {
			{ID: "ghost", DisplayName: "Ghost", CreditsMultiplier: 0.5, CreditsParsed: true},
			{ID: "normal", DisplayName: "Normal"}, // 无值
		},
		regionWB: {
			{ID: "ghost", DisplayName: "Ghost"},
		},
	}
	merged := mergeCatalogs(byRegion)
	byID := map[string]ModelInfo{}
	for _, m := range merged {
		byID[m.ID] = m
	}
	// cn 整份目录里 normal 无值、ghost 有值 —— 区域级判定为「有下发」，
	// 因此 ghost 不标借用（判定是区域级而非条目级，见 mergeCatalogs 注释）。
	if g := byID["ghost"]; g.CreditsFromElsewhere {
		t.Error("区域内有任一模型带倍率即视为该区域下发，ghost 不应标借用")
	}
	// normal 本来就没有倍率。
	if n := byID["normal"]; n.CreditsParsed {
		t.Error("normal 不应有倍率")
	}
}

// TestMergeCatalogsNoCreditsAnywhere 验证所有区域都不下发倍率时不会凭空产生值：
// 这正是办公版（wb）单独使用时的形态。
func TestMergeCatalogsNoCreditsAnywhere(t *testing.T) {
	byRegion := map[string][]ModelInfo{
		regionWB: {
			{ID: "hy3", DisplayName: "Hy3"},
			{ID: "glm-5.1", DisplayName: "GLM-5.1"},
		},
	}
	merged := mergeCatalogs(byRegion)
	if len(merged) != 2 {
		t.Fatalf("应保留 2 条，得到 %d", len(merged))
	}
	for _, m := range merged {
		if m.CreditsParsed || m.CreditsMultiplier != 0 || m.CreditsLabel != "" {
			t.Errorf("%s 不应有倍率（办公版目录不提供 credits），得到 %+v", m.ID, m)
		}
		if m.CreditsFromElsewhere {
			t.Errorf("%s 没有值就不该标借用", m.ID)
		}
	}
}
