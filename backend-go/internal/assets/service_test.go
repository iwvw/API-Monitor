package assets

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	cfg := config.Config{DataDir: t.TempDir(), DBName: "data.db"}
	return New(cfg)
}

func TestSchemaIdempotent(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("re-run schema: %v", err)
	}
	db.Close()
}

func TestCreateAndLoadAsset(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	asset, err := service.CreateAsset(ctx, map[string]interface{}{
		"name":          "测试物理机",
		"category":      "physical",
		"asset_type":    "server",
		"provider":      "自建机房",
		"expire_at":     "2027-01-01",
		"cost_amount":   float64(1200),
		"cost_currency": "cny",
		"cost_cycle":    "yearly",
		"tags":          []interface{}{"生产", "核心"},
	})
	if err != nil {
		t.Fatalf("create asset: %v", err)
	}
	if asset.ID == "" {
		t.Fatal("expected generated id")
	}
	if asset.Origin != "manual" {
		t.Fatalf("expected manual origin, got %s", asset.Origin)
	}
	if asset.ExpireAt != "2027-01-01T00:00:00Z" {
		t.Fatalf("expected normalized expire_at, got %s", asset.ExpireAt)
	}
	if asset.CostCurrency != "CNY" {
		t.Fatalf("expected normalized currency CNY, got %s", asset.CostCurrency)
	}
	if len(asset.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(asset.Tags))
	}

	loaded, ok, err := service.LoadAsset(ctx, asset.ID)
	if err != nil || !ok {
		t.Fatalf("load asset: ok=%v err=%v", ok, err)
	}
	if loaded.Name != "测试物理机" {
		t.Fatalf("unexpected name %s", loaded.Name)
	}
}

func TestCreateAssetValidation(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	cases := []map[string]interface{}{
		{"category": "physical", "asset_type": "server"},
		{"name": "x", "asset_type": "server"},
		{"name": "x", "category": "physical"},
		{"name": "x", "category": "bogus", "asset_type": "server"},
		{"name": "x", "category": "physical", "asset_type": "domain"},
		{"name": "x", "category": "physical", "asset_type": "server", "cost_cycle": "weekly"},
	}
	for i, payload := range cases {
		if _, err := service.CreateAsset(ctx, payload); err == nil {
			t.Fatalf("case %d expected validation error", i)
		}
	}
}

func TestUpdateAndDeleteAsset(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	asset, err := service.CreateAsset(ctx, map[string]interface{}{
		"name": "待改", "category": "virtual", "asset_type": "domain",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := service.UpdateAsset(ctx, asset.ID, map[string]interface{}{
		"name": "已改", "remark": "备注", "auto_renew": true,
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	loaded, _, _ := service.LoadAsset(ctx, asset.ID)
	if loaded.Name != "已改" || !loaded.AutoRenew || loaded.Remark != "备注" {
		t.Fatalf("update not applied: %#v", loaded)
	}

	if err := service.UpdateAsset(ctx, "missing", map[string]interface{}{"name": "x"}); err == nil {
		t.Fatal("expected not found error")
	}

	if err := service.DeleteAsset(ctx, asset.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := service.LoadAsset(ctx, asset.ID); ok {
		t.Fatal("expected asset deleted")
	}
}

func TestDeriveStatus(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name       string
		expireAt   string
		autoRenew  bool
		warnDays   []int
		wantStatus string
		wantDays   int
	}{
		{"正常", "2026-12-31T00:00:00Z", false, nil, statusActive, 100},
		{"30天内", "2026-10-10T00:00:00Z", false, nil, statusExpiring, 18},
		{"已过期", "2026-09-01T00:00:00Z", false, nil, statusExpired, -21},
		{"今天到期", "2026-09-22T00:00:00Z", false, nil, statusExpiring, 0},
		{"自动续费不告警", "2026-10-01T00:00:00Z", true, nil, statusActive, 9},
		{"自定义阈值", "2026-10-01T00:00:00Z", false, []int{90}, statusExpiring, 9},
		{"无到期信息", "", false, nil, statusUnknown, 0},
	}
	for _, tc := range cases {
		asset := Asset{ExpireAt: tc.expireAt, AutoRenew: tc.autoRenew, WarnDays: tc.warnDays}
		status, days := deriveStatus(asset, now, loc)
		if status != tc.wantStatus {
			t.Errorf("%s: status=%s want %s", tc.name, status, tc.wantStatus)
		}
		if tc.wantStatus == statusUnknown {
			if days != nil {
				t.Errorf("%s: expected nil days", tc.name)
			}
			continue
		}
		if days == nil || *days != tc.wantDays {
			t.Errorf("%s: days=%v want %d", tc.name, days, tc.wantDays)
		}
	}
}

func TestDeriveStatusRetiredOrphan(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	for _, status := range []string{statusRetired, statusOrphan} {
		asset := Asset{Status: status, ExpireAt: "2026-12-31T00:00:00Z"}
		got, _ := deriveStatus(asset, now, time.UTC)
		if got != status {
			t.Errorf("expected %s preserved, got %s", status, got)
		}
	}
}

func TestDaysUntilTimezoneBoundary(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("Asia/Shanghai unavailable")
	}
	// now = 上海 09-22 23:00，expire = 上海 09-23 01:00。
	// 两者在 UTC 同属 09-22，但在站点时区跨了一天。
	now := time.Date(2026, 9, 22, 15, 0, 0, 0, time.UTC)
	expire := time.Date(2026, 9, 22, 17, 0, 0, 0, time.UTC)

	if got := daysUntil(expire, now, time.UTC); got != 0 {
		t.Errorf("UTC same-day: got %d want 0", got)
	}
	if got := daysUntil(expire, now, shanghai); got != 1 {
		t.Errorf("Shanghai next-day: got %d want 1", got)
	}
}

func TestMonthlyEquivalent(t *testing.T) {
	cases := []struct {
		amount float64
		cycle  string
		want   float64
	}{
		{120, cycleMonthly, 120},
		{120, cycleQuarterly, 40},
		{1200, cycleYearly, 100},
		{999, cycleOneTime, 0},
		{999, cycleUsage, 0},
		{999, "", 0},
	}
	for _, tc := range cases {
		if got := monthlyEquivalent(tc.amount, tc.cycle); got != tc.want {
			t.Errorf("monthlyEquivalent(%v,%s)=%v want %v", tc.amount, tc.cycle, got, tc.want)
		}
	}
}

func TestAggregateCosts(t *testing.T) {
	assets := []Asset{
		{CostAmount: 120, CostCurrency: "CNY", CostCycle: "yearly"},
		{CostAmount: 10, CostCurrency: "cny", CostCycle: "monthly"},
		{CostAmount: 60, CostCurrency: "USD", CostCycle: "yearly"},
		{CostAmount: 500, CostCurrency: "CNY", CostCycle: "one_time"},
		{CostAmount: 0, CostCurrency: "CNY", CostCycle: "monthly"},
	}
	costs := aggregateCosts(assets)
	if len(costs) != 2 {
		t.Fatalf("expected 2 currencies, got %d", len(costs))
	}
	byCurrency := map[string]CurrencyCost{}
	for _, cost := range costs {
		byCurrency[cost.Currency] = cost
	}
	if got := byCurrency["CNY"].Monthly; got != 20 {
		t.Errorf("CNY monthly=%v want 20", got)
	}
	if got := byCurrency["CNY"].OneTime; got != 500 {
		t.Errorf("CNY one-time=%v want 500", got)
	}
	if got := byCurrency["USD"].Monthly; got != 5 {
		t.Errorf("USD monthly=%v want 5", got)
	}
}

func TestConvertToBase(t *testing.T) {
	costs := []CurrencyCost{
		{Currency: "CNY", Monthly: 100},
		{Currency: "USD", Monthly: 10},
	}
	if got := convertToBase(costs, Settings{BaseCurrency: ""}); got != nil {
		t.Errorf("expected nil without base currency, got %v", got)
	}
	missing := convertToBase(costs, Settings{BaseCurrency: "CNY"})
	if missing != nil {
		t.Errorf("expected nil when USD rate missing, got %v", missing)
	}
	got := convertToBase(costs, Settings{
		BaseCurrency:  "CNY",
		ExchangeRates: map[string]float64{"USD": 7.2},
	})
	if got == nil || got["CNY"] != 172 {
		t.Errorf("expected CNY total 172, got %v", got)
	}
}

func TestLoadAssetsFilterAndDerive(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	mustCreate := func(payload map[string]interface{}) {
		t.Helper()
		if _, err := service.CreateAsset(ctx, payload); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	mustCreate(map[string]interface{}{"name": "物理A", "category": "physical", "asset_type": "server", "provider": "机房", "tags": []interface{}{"生产"}})
	mustCreate(map[string]interface{}{"name": "虚拟B", "category": "virtual", "asset_type": "domain"})
	mustCreate(map[string]interface{}{"name": "虚拟C", "category": "virtual", "asset_type": "ssl_cert", "expire_at": "2000-01-01"})

	physical, err := service.LoadAssets(ctx, assetFilter{Category: "physical", Limit: 100})
	if err != nil {
		t.Fatalf("filter physical: %v", err)
	}
	if len(physical) != 1 || physical[0].Name != "物理A" {
		t.Fatalf("unexpected physical result: %#v", physical)
	}

	byTag, err := service.LoadAssets(ctx, assetFilter{Tag: "生产", Limit: 100})
	if err != nil {
		t.Fatalf("filter tag: %v", err)
	}
	if len(byTag) != 1 {
		t.Fatalf("expected 1 tagged asset, got %d", len(byTag))
	}

	expired, err := service.LoadAssets(ctx, assetFilter{Status: "expired", Limit: 100})
	if err != nil {
		t.Fatalf("filter expired: %v", err)
	}
	if len(expired) != 1 || expired[0].Name != "虚拟C" {
		t.Fatalf("unexpected expired result: %#v", expired)
	}
	if expired[0].DaysLeft == nil || *expired[0].DaysLeft >= 0 {
		t.Fatalf("expected negative days_left, got %v", expired[0].DaysLeft)
	}

	search, err := service.LoadAssets(ctx, assetFilter{Query: "虚拟", Limit: 100})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(search) != 2 {
		t.Fatalf("expected 2 search hits, got %d", len(search))
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	initial, err := service.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	if len(initial.WarnDays) == 0 {
		t.Fatal("expected default warn days")
	}

	next := Settings{
		BaseCurrency:  "usd",
		ExchangeRates: map[string]float64{"CNY": 0.14},
		WarnDays:      []int{60, 30},
	}
	if err := service.SaveSettings(ctx, next); err != nil {
		t.Fatalf("save settings: %v", err)
	}
	saved, err := service.LoadSettings(ctx)
	if err != nil {
		t.Fatalf("reload settings: %v", err)
	}
	if saved.BaseCurrency != "USD" {
		t.Fatalf("expected USD, got %s", saved.BaseCurrency)
	}
	if saved.ExchangeRates["CNY"] != 0.14 {
		t.Fatalf("unexpected rates: %#v", saved.ExchangeRates)
	}
	if len(saved.WarnDays) != 2 || saved.WarnDays[0] != 60 {
		t.Fatalf("unexpected warn days: %#v", saved.WarnDays)
	}
}

func TestEventsRecorded(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	asset, err := service.CreateAsset(ctx, map[string]interface{}{
		"name": "事件", "category": "virtual", "asset_type": "saas",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := service.UpdateAsset(ctx, asset.ID, map[string]interface{}{"name": "事件2"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	events, err := service.LoadEvents(ctx, asset.ID, 10)
	if err != nil {
		t.Fatalf("load events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestServeHTTPRouting(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	asset, err := service.CreateAsset(ctx, map[string]interface{}{
		"name": "路由", "category": "physical", "asset_type": "network",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	handler := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		rec := httptest.NewRecorder()
		service.ServeHTTP(rec, req)
		return rec
	}

	if rec := handler("GET", "/api/assets"); rec.Code != 200 {
		t.Fatalf("list assets status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := handler("GET", "/api/assets/"+asset.ID); rec.Code != 200 {
		t.Fatalf("get asset status=%d", rec.Code)
	}
	if rec := handler("GET", "/api/assets/overview"); rec.Code != 200 {
		t.Fatalf("overview status=%d", rec.Code)
	}
	if rec := handler("GET", "/api/assets/expiring"); rec.Code != 200 {
		t.Fatalf("expiring status=%d", rec.Code)
	}
	if rec := handler("GET", "/api/assets/settings"); rec.Code != 200 {
		t.Fatalf("settings status=%d", rec.Code)
	}
	if rec := handler("GET", "/api/assets/categories"); rec.Code != 200 {
		t.Fatalf("categories status=%d", rec.Code)
	}
	if rec := handler("DELETE", "/api/assets/"+asset.ID); rec.Code != 200 {
		t.Fatalf("delete status=%d", rec.Code)
	}
	if rec := handler("GET", "/api/assets/nope"); rec.Code != 404 {
		t.Fatalf("expected 404 for missing asset, got %d", rec.Code)
	}
}
