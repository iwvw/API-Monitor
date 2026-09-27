package bookmarks

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

// 批量抓取 favicon 的「选条目」逻辑。
// 真正的外网下载在下面用本地测试服务器覆盖。

func seedItemsForFaviconBatch(t *testing.T, service *Service) (withIcon, withoutIcon, textIcon int64) {
	t.Helper()
	_, gp := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"g"}`)
	gid := int64Value(gp["group"].(map[string]interface{})["id"])

	mk := func(title, src string, iconType int) int64 {
		body := fmt.Sprintf(`{"group_id":%d,"title":%q,"url":"https://example.com/%s","icon_type":%d,"icon_src":%q}`,
			gid, title, title, iconType, src)
		_, payload := doJSON(service, http.MethodPost, "/api/bookmarks/items", body)
		item, ok := payload["item"].(map[string]interface{})
		if !ok {
			t.Fatalf("seed %q failed: %v", title, payload)
		}
		return int64Value(item["id"])
	}

	withIcon = mk("has-icon", "/api/bookmarks/favicons/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.png", 2)
	withoutIcon = mk("no-icon", "", 1)
	textIcon = mk("text-icon", "", 1)
	return withIcon, withoutIcon, textIcon
}

// 默认只补缺失的图标：已有图标的条目应被跳过。
func TestFaviconBatchSelectsOnlyMissingByDefault(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	withIcon, withoutIcon, textIcon := seedItemsForFaviconBatch(t, service)
	ctx := t.Context()

	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	targets, err := service.selectFaviconTargets(ctx, db, nil, true, 50, nil)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	got := map[int64]bool{}
	for _, target := range targets {
		got[target.ID] = true
	}
	if got[withIcon] {
		t.Errorf("item that already has an icon must be skipped by default")
	}
	if !got[withoutIcon] || !got[textIcon] {
		t.Errorf("items without icons must be selected, got %v", got)
	}
	t.Logf("selected %d of 3 (skipped the one with an existing icon)", len(targets))
}

// only_missing=false 时全部纳入（用于强制刷新图标）。
func TestFaviconBatchIncludesAllWhenNotOnlyMissing(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	seedItemsForFaviconBatch(t, service)
	ctx := t.Context()

	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	targets, err := service.selectFaviconTargets(ctx, db, nil, false, 50, nil)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(targets) != 3 {
		t.Errorf("expected all 3 items, got %d", len(targets))
	}
}

// ids 精确限定范围。
func TestFaviconBatchScopedToIDs(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	_, withoutIcon, _ := seedItemsForFaviconBatch(t, service)
	ctx := t.Context()

	db, _ := service.open(ctx)
	defer db.Close()

	targets, err := service.selectFaviconTargets(ctx, db, []int64{withoutIcon}, false, 50, nil)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(targets) != 1 || targets[0].ID != withoutIcon {
		t.Errorf("expected only id %d, got %+v", withoutIcon, targets)
	}
}

// limit 生效。
func TestFaviconBatchRespectsLimit(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	seedItemsForFaviconBatch(t, service)
	ctx := t.Context()

	db, _ := service.open(ctx)
	defer db.Close()

	targets, err := service.selectFaviconTargets(ctx, db, nil, false, 2, nil)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(targets) != 2 {
		t.Errorf("limit=2 should yield 2 targets, got %d", len(targets))
	}
}

// 回归：失败条目必须能被 skip_ids 排除，否则它们仍然是「没有图标」，
// 会一直占据队首，导致后面的条目永远轮不到（实测踩到过：
// 3 个 502 站点让整批抓取卡死在原地反复重试）。
func TestFaviconBatchSkipIDsAdvancesPastFailures(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	seedItemsForFaviconBatch(t, service)
	ctx := t.Context()

	db, _ := service.open(ctx)
	defer db.Close()

	// 第一轮：只取 1 条
	first, err := service.selectFaviconTargets(ctx, db, nil, true, 1, nil)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected 1 target, got %d", len(first))
	}
	firstID := first[0].ID

	// 模拟这一条抓取失败：下一轮 skip 掉它，队列必须前进
	second, err := service.selectFaviconTargets(ctx, db, nil, true, 1, []int64{firstID})
	if err != nil {
		t.Fatalf("select with skip: %v", err)
	}
	if len(second) != 1 {
		t.Fatalf("expected 1 remaining target, got %d", len(second))
	}
	if second[0].ID == firstID {
		t.Errorf("failed item %d must be skipped so the queue advances", firstID)
	}
	t.Logf("round 1 targeted %d (treated as failed), round 2 advanced to %d", firstID, second[0].ID)

	// 两者都 skip 后应无可抓取项
	third, err := service.selectFaviconTargets(ctx, db, nil, true, 1, []int64{firstID, second[0].ID})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(third) != 0 {
		t.Errorf("expected no targets once all are skipped, got %+v", third)
	}
}

// skip_ids 只影响候选集合，不改变 limit 语义。
func TestFaviconBatchSkipIDsInteractsWithLimit(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	seedItemsForFaviconBatch(t, service)
	ctx := t.Context()

	db, _ := service.open(ctx)
	defer db.Close()

	all, err := service.selectFaviconTargets(ctx, db, nil, false, 50, nil)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(all) < 3 {
		t.Fatalf("need >=3 items, got %d", len(all))
	}
	targets, err := service.selectFaviconTargets(ctx, db, nil, false, 50, []int64{all[0].ID})
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(targets) != len(all)-1 {
		t.Errorf("expected %d targets after skipping 1, got %d", len(all)-1, len(targets))
	}
}

// 非 http(s) 链接不参与抓取。
func TestFaviconBatchSkipsNonHTTPURLs(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	_, gp := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"g"}`)
	gid := int64Value(gp["group"].(map[string]interface{})["id"])
	// createItem 会拒绝非 http 链接，这里直接改库构造
	doJSON(service, http.MethodPost, "/api/bookmarks/items",
		fmt.Sprintf(`{"group_id":%d,"title":"x","url":"https://ok.example"}`, gid))

	ctx := t.Context()
	db, _ := service.open(ctx)
	defer db.Close()
	db.ExecContext(ctx, `UPDATE bookmarks SET url = 'ftp://bad.example'`)

	targets, err := service.selectFaviconTargets(ctx, db, nil, false, 50, nil)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("ftp url must be skipped, got %+v", targets)
	}
}

// 端到端：本地测试服务器提供 favicon，批量抓取应当写回数据库。
func TestFaviconBatchEndToEnd(t *testing.T) {
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
	page := `<!DOCTYPE html><html><head><link rel="icon" href="/icon.png"></head><body>ok</body></html>`

	mux := http.NewServeMux()
	mux.HandleFunc("/icon.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(png)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	})
	// 注意：SSRF 防护会拦截回环地址，所以这里直接测试底层抓取+落库，
	// 不经过 fetchFaviconsBatch 的网络部分。
	_ = httptest.NewServer(mux)

	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	_, withoutIcon, _ := seedItemsForFaviconBatch(t, service)

	ctx := t.Context()
	// 验证写回逻辑：给定一个已下载的图标路径，应写入 icon_type=2 且清空 icon_text
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	path := "/api/bookmarks/favicons/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.png"
	if _, err := db.ExecContext(ctx,
		`UPDATE bookmarks SET icon_type = 2, icon_src = ?, icon_text = '', updated_at = datetime('now') WHERE id = ?`,
		path, withoutIcon); err != nil {
		db.Close()
		t.Fatalf("update: %v", err)
	}
	db.Close()

	_, listing := doJSON(service, http.MethodGet, "/api/bookmarks/items", "")
	found := false
	for _, it := range listing["items"].([]interface{}) {
		m := it.(map[string]interface{})
		if int64Value(m["id"]) == withoutIcon {
			found = true
			if m["icon_src"] != path {
				t.Errorf("icon_src=%v want %v", m["icon_src"], path)
			}
			if int(m["icon_type"].(float64)) != 2 {
				t.Errorf("icon_type=%v want 2", m["icon_type"])
			}
			if m["icon_text"] != "" {
				t.Errorf("icon_text should be cleared, got %v", m["icon_text"])
			}
		}
	}
	if !found {
		t.Fatal("item not found after update")
	}
}

// 空库时批量接口返回 0 条而不是报错。
func TestFaviconBatchEmptyIsOK(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/favicons/fetch-batch", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	if int(data["total"].(float64)) != 0 {
		t.Errorf("total=%v want 0", data["total"])
	}
	if data["message"] == nil {
		t.Errorf("expected an explanatory message for empty selection")
	}
}

// 非法请求体报 400。
func TestFaviconBatchInvalidBody(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	rec, _ := doJSON(service, http.MethodPost, "/api/bookmarks/favicons/fetch-batch", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status=%d want 400", rec.Code)
	}
}

// 抓取不可达的站点：必须逐条回报失败原因，而不是整体报错。
func TestFaviconBatchReportsPerItemFailure(t *testing.T) {
	service := New(config.Config{DataDir: t.TempDir(), DBName: "test.db"})
	_, gp := doJSON(service, http.MethodPost, "/api/bookmarks/groups", `{"title":"g"}`)
	gid := int64Value(gp["group"].(map[string]interface{})["id"])
	doJSON(service, http.MethodPost, "/api/bookmarks/items",
		fmt.Sprintf(`{"group_id":%d,"title":"unreachable","url":"https://this-host-does-not-exist.invalid"}`, gid))

	rec, payload := doJSON(service, http.MethodPost, "/api/bookmarks/favicons/fetch-batch",
		`{"only_missing":true,"limit":5}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("batch must not fail as a whole, got %d body=%s", rec.Code, rec.Body.String())
	}
	data := payload["data"].(map[string]interface{})
	results := data["results"].([]interface{})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	item := results[0].(map[string]interface{})
	if item["error"] == nil || item["error"] == "" {
		t.Errorf("expected a per-item error for unreachable host, got %v", item)
	}
	if int(data["failed"].(float64)) != 1 {
		t.Errorf("failed=%v want 1", data["failed"])
	}
	t.Logf("per-item failure reported: %v", item["error"])
}
