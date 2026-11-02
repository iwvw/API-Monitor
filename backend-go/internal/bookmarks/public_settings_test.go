package bookmarks

import (
	"net/http"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// 背景属于「整个公开页」而不是单个分组：写入全局设置后，
// 公开端点应当把它下发给免登录访客。
func TestPublicSettingsGlobalBackground(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	// 初始为空
	rec, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public-settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{}); len(cfg) != 0 {
		t.Errorf("expected empty config, got %v", cfg)
	}

	// 写入背景
	rec, payload = doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"bgImage":"https://cdn.example/bg.jpg","bgBlur":8,"bgDim":0.4,"bgSize":"cover","bgFixed":true}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rec.Code, rec.Body.String())
	}
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if cfg["bgImage"] != "https://cdn.example/bg.jpg" {
		t.Errorf("bgImage=%v", cfg["bgImage"])
	}
	if cfg["bgBlur"].(float64) != 8 {
		t.Errorf("bgBlur=%v", cfg["bgBlur"])
	}

	// 公开聚合端点应带上该配置（免登录可见）
	_, agg := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	aggCfg, ok := agg["data"].(map[string]interface{})["config"].(map[string]interface{})
	if !ok {
		t.Fatalf("aggregate response missing config: %v", agg["data"])
	}
	if aggCfg["bgImage"] != "https://cdn.example/bg.jpg" {
		t.Errorf("aggregate config bgImage=%v", aggCfg["bgImage"])
	}
}

// 单分组页也要带全局背景（背景不随分组变化）。
func TestPublicGroupCarriesGlobalBackground(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"公开组","public":true,"slug":"pub"}`)
	doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"bgColor":"#112233"}}`)

	_, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public/groups/pub", "")
	data := payload["data"].(map[string]interface{})
	cfg, ok := data["config"].(map[string]interface{})
	if !ok {
		t.Fatalf("single-group response missing config: %v", data)
	}
	if cfg["bgColor"] != "#112233" {
		t.Errorf("bgColor=%v", cfg["bgColor"])
	}
}

// 局部更新：只传一个字段不应清掉其它字段。
func TestPublicSettingsPartialUpdate(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"bgImage":"https://cdn.example/a.jpg","bgBlur":6,"bgDim":0.5}}`)

	// 只改模糊
	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings", `{"config":{"bgBlur":12}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if cfg["bgImage"] != "https://cdn.example/a.jpg" {
		t.Errorf("bgImage should be preserved, got %v", cfg["bgImage"])
	}
	if cfg["bgDim"].(float64) != 0.5 {
		t.Errorf("bgDim should be preserved, got %v", cfg["bgDim"])
	}
	if cfg["bgBlur"].(float64) != 12 {
		t.Errorf("bgBlur should be updated, got %v", cfg["bgBlur"])
	}
}

// 传空字符串表示清除该字段。
func TestPublicSettingsClearField(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"bgImage":"https://cdn.example/a.jpg","bgColor":"#123456"}}`)

	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings", `{"config":{"bgImage":""}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if _, exists := cfg["bgImage"]; exists {
		t.Errorf("bgImage should be removed, got %v", cfg["bgImage"])
	}
	if cfg["bgColor"] != "#123456" {
		t.Errorf("bgColor should be preserved, got %v", cfg["bgColor"])
	}
}

// 非法值必须被拒绝/丢弃，且不写进库。
func TestPublicSettingsRejectsInvalidValues(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"bgImage":"javascript:alert(1)","bgColor":"not-a-color","bgBlur":999,"bgDim":-5,"bgSize":"bogus"}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})

	if _, exists := cfg["bgImage"]; exists {
		t.Errorf("javascript: URL must be rejected, got %v", cfg["bgImage"])
	}
	if _, exists := cfg["bgColor"]; exists {
		t.Errorf("invalid color must be rejected, got %v", cfg["bgColor"])
	}
	if _, exists := cfg["bgSize"]; exists {
		t.Errorf("invalid size must be rejected, got %v", cfg["bgSize"])
	}
	if blur, ok := cfg["bgBlur"].(float64); ok && blur > 40 {
		t.Errorf("bgBlur must be clamped to 40, got %v", blur)
	}
	if dim, ok := cfg["bgDim"].(float64); ok && dim < 0 {
		t.Errorf("bgDim must be clamped to 0, got %v", dim)
	}
}

// 未知字段不得落库（避免任意 JSON 写进配置）。
func TestPublicSettingsDropsUnknownKeys(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"bgColor":"#000000","evil":"<script>","admin":true}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if _, exists := cfg["evil"]; exists {
		t.Errorf("unknown key 'evil' must be dropped, got %v", cfg["evil"])
	}
	if _, exists := cfg["admin"]; exists {
		t.Errorf("unknown key 'admin' must be dropped")
	}
	if cfg["bgColor"] != "#000000" {
		t.Errorf("bgColor should be kept, got %v", cfg["bgColor"])
	}
}

// 配置损坏时公开端点仍应可用（返回空配置而不是 500）。
func TestPublicSettingsCorruptedConfigDegradesGracefully(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	ctx := t.Context()
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"g","public":true}`)

	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO bookmark_public_settings (id, config_json) VALUES (1, '{not valid json')`); err != nil {
		db.Close()
		t.Fatalf("seed corrupt config: %v", err)
	}
	db.Close()

	rec, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("corrupt config must not break the public page, got %d", rec.Code)
	}
	cfg, ok := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected an (empty) config object, got %v", payload["data"])
	}
	if len(cfg) != 0 {
		t.Errorf("corrupt config should degrade to empty, got %v", cfg)
	}
}

// 背景与分组无关：改分组不应影响全局背景，反之亦然。
func TestBackgroundIsIndependentOfGroups(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	_, gp := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"g","public":true,"slug":"g1"}`)
	gid := int64Value(gp["group"].(map[string]interface{})["id"])

	doJSON(service, http.MethodPut, "/api/bookmarks/public-settings", `{"config":{"bgColor":"#abcdef"}}`)
	// 更新分组（写它自己的 config）
	doJSON(service, http.MethodPut, "/api/bookmarks/groups/"+itoa(gid),
		`{"title":"g","config":{"publicIconId":"icon-1"}}`)

	_, payload := doJSON(service, http.MethodGet, "/api/bookmarks/public-settings", "")
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if cfg["bgColor"] != "#abcdef" {
		t.Errorf("group update must not affect the global background, got %v", cfg)
	}
	// 分组自己的 config 也不该混入背景字段
	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/groups", "")
	groupCfg := listing["groups"].([]interface{})[0].(map[string]interface{})["config"].(map[string]interface{})
	if _, exists := groupCfg["bgColor"]; exists {
		t.Errorf("group config must not carry the global background, got %v", groupCfg)
	}
}

// pageWidth 是新增的公开页全局字段，必须能存能读，
// 且不能被 sanitize 的白名单静默丢弃（否则表现为「保存无效」）。
func TestPublicSettingsPageWidth(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"pageWidth":"normal"}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if cfg["pageWidth"] != "normal" {
		t.Fatalf("pageWidth should be persisted, got %v", cfg["pageWidth"])
	}

	// 回读
	_, again := doJSON(service, http.MethodGet, "/api/bookmarks/public-settings", "")
	readCfg := again["data"].(map[string]interface{})["config"].(map[string]interface{})
	if readCfg["pageWidth"] != "normal" {
		t.Errorf("pageWidth should survive a read, got %v", readCfg["pageWidth"])
	}

	// 改回默认值也必须生效（不能因为「等于默认」就不写库）
	doJSON(service, http.MethodPut, "/api/bookmarks/public-settings", `{"config":{"pageWidth":"full"}}`)
	_, back := doJSON(service, http.MethodGet, "/api/bookmarks/public-settings", "")
	backCfg := back["data"].(map[string]interface{})["config"].(map[string]interface{})
	if backCfg["pageWidth"] != "full" {
		t.Errorf("pageWidth should be updatable back to full, got %v", backCfg["pageWidth"])
	}
}

// pageWidth 的非法值必须被丢弃，不能写进库。
func TestPublicSettingsRejectsInvalidPageWidth(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"pageWidth":"9999px; background:url(x)"}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if _, exists := cfg["pageWidth"]; exists {
		t.Errorf("invalid pageWidth must be dropped, got %v", cfg["pageWidth"])
	}

	// 非法值不该破坏其它合法字段
	_, payload = doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"bgBlur":9,"pageWidth":"huge"}}`)
	cfg = payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if cfg["bgBlur"].(float64) != 9 {
		t.Errorf("valid fields must survive alongside an invalid pageWidth, got %v", cfg)
	}
	if _, exists := cfg["pageWidth"]; exists {
		t.Errorf("invalid pageWidth must not be stored, got %v", cfg["pageWidth"])
	}
}

// 公开端点（聚合页与单分组页）都应下发 pageWidth，否则访客拿到的是默认宽度。
func TestPublicEndpointsCarryPageWidth(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"公开组","public":true,"slug":"pub"}`)
	doJSON(service, http.MethodPut, "/api/bookmarks/public-settings", `{"config":{"pageWidth":"narrow"}}`)

	for _, path := range []string{"/api/bookmarks/public/all", "/api/bookmarks/public/groups/pub"} {
		_, payload := doJSON(service, http.MethodGet, path, "")
		cfg, ok := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
		if !ok {
			t.Fatalf("%s: missing config in response: %v", path, payload["data"])
		}
		if cfg["pageWidth"] != "narrow" {
			t.Errorf("%s: pageWidth=%v, want narrow", path, cfg["pageWidth"])
		}
	}
}

// 自定义搜索引擎：合法项落库并能回读。
func TestPublicSettingsSearchEngines(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"searchEngines":[{"id":"bing","label":"必应","url":"https://www.bing.com/search?q=%s","icon":"logos:bing","color":"#258FFB"}]}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	list, ok := cfg["searchEngines"].([]interface{})
	if !ok || len(list) != 1 {
		t.Fatalf("searchEngines should be persisted, got %v", cfg["searchEngines"])
	}
	first := list[0].(map[string]interface{})
	if first["id"] != "bing" || first["label"] != "必应" {
		t.Errorf("engine fields wrong: %v", first)
	}

	// 公开聚合端点也要带上（免登录访客需要它来渲染搜索框）
	_, agg := doJSON(service, http.MethodGet, "/api/bookmarks/public/all", "")
	aggCfg := agg["data"].(map[string]interface{})["config"].(map[string]interface{})
	if _, exists := aggCfg["searchEngines"]; !exists {
		t.Errorf("aggregate response must carry searchEngines, got %v", aggCfg)
	}
}

// 非法引擎项必须被丢弃：缺 %s、非 http 协议、缺名称。
func TestPublicSettingsRejectsInvalidSearchEngines(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"searchEngines":[
			{"label":"无占位符","url":"https://a.com/search"},
			{"label":"危险协议","url":"javascript:alert(1)%s"},
			{"label":"","url":"https://c.com/?q=%s"},
			{"label":"合法","url":"https://ok.com/?q=%s"}
		]}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	list, _ := cfg["searchEngines"].([]interface{})
	if len(list) != 1 {
		t.Fatalf("only the valid engine should survive, got %v", cfg["searchEngines"])
	}
	if list[0].(map[string]interface{})["label"] != "合法" {
		t.Errorf("wrong engine survived: %v", list[0])
	}
}

// 全部非法 -> 删除该键（公开页回落到内置默认）。
func TestPublicSettingsDropsEmptySearchEngines(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"searchEngines":[{"label":"x","url":"ftp://a/%s"}]}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	if _, exists := cfg["searchEngines"]; exists {
		t.Errorf("all-invalid list should be dropped, got %v", cfg["searchEngines"])
	}
}

// 缺 id 时按主机名补全；重复 id 要去重。
func TestPublicSettingsSearchEngineIDFallback(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"searchEngines":[
			{"label":"A","url":"https://www.example.com/s?q=%s"},
			{"label":"B","url":"https://example.com/other?q=%s"}
		]}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	list := cfg["searchEngines"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("expected 2 engines, got %v", list)
	}
	idA := list[0].(map[string]interface{})["id"]
	idB := list[1].(map[string]interface{})["id"]
	if idA != "example" {
		t.Errorf("id should be derived from hostname, got %v", idA)
	}
	if idA == idB {
		t.Errorf("duplicate ids must be de-duplicated, both = %v", idA)
	}
}

// 数量超限时截断，避免把任意大的 JSON 塞进公开响应。
func TestPublicSettingsSearchEngineCountCap(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})

	var items []string
	for i := 0; i < searchEngineMaxCount+5; i++ {
		n := itoa(int64(i))
		items = append(items, `{"label":"e`+n+`","url":"https://e`+n+`.com/?q=%s"}`)
	}
	_, payload := doJSON(service, http.MethodPut, "/api/bookmarks/public-settings",
		`{"config":{"searchEngines":[`+strings.Join(items, ",")+`]}}`)
	cfg := payload["data"].(map[string]interface{})["config"].(map[string]interface{})
	list, _ := cfg["searchEngines"].([]interface{})
	if len(list) > searchEngineMaxCount {
		t.Errorf("engine list must be capped at %d, got %d", searchEngineMaxCount, len(list))
	}
}
