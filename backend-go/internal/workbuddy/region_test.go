package workbuddy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

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
		{"JWT 优先于 domain", jwt("https://www.workbuddy.ai/auth/realms/copilot"), "www.codebuddy.cn", regionIntl, true},
		{"无 JWT 用 domain 兜底(国际)", "not-a-jwt", "www.workbuddy.ai", regionIntl, true},
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
	// 同一 uid 在两区必须得到不同 ID（否则会互相覆盖）。
	if accountIDForRegion(regionCN, "u1") == accountIDForRegion(regionIntl, "u1") {
		t.Error("同一 uid 在两区应得到不同 ID")
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
