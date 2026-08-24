package adminai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func insertMemoryTest(t *testing.T, s *Service, content string, importance int, pinned bool) MemoryItem {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	it, err := s.insertMemory(context.Background(), db, content, importance, "", pinned, "manual", "")
	if err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	return it
}

// 记忆 CRUD 全链路（含 FTS 索引同步）。
func TestMemoryCRUD(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	it := insertMemoryTest(t, s, "用户偏好：管理面板用中文表格展示回复，简明优先", 8, false)
	if !strings.HasPrefix(it.ID, "aamem_") {
		t.Fatalf("unexpected id prefix: %s", it.ID)
	}

	// 检索命中
	items, err := s.searchMemories(context.Background(), db, "中文表格", 6)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(items) != 1 || items[0].ID != it.ID {
		t.Fatalf("expected hit, got %+v", items)
	}
	// 命中后访问计数与最近访问时间落库
	var accessCount int
	var lastAccessedAt string
	if err := db.QueryRowContext(context.Background(),
		`SELECT access_count, COALESCE(last_accessed_at,'') FROM admin_ai_memories WHERE id = ?`, it.ID).Scan(&accessCount, &lastAccessedAt); err != nil {
		t.Fatalf("query access: %v", err)
	}
	if accessCount != 1 || lastAccessedAt == "" {
		t.Fatalf("access not bumped: count=%d last=%q", accessCount, lastAccessedAt)
	}

	// 更新 content 后旧词不可再命中（FTS 触发器同步）
	patch := memoryPatch{Content: strPtr("用户偏好：通知用 Telegram 推送")}
	upd, err := s.updateMemory(context.Background(), db, it.ID, patch)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.Content != "用户偏好：通知用 Telegram 推送" {
		t.Fatalf("update content mismatch: %s", upd.Content)
	}
	items, _ = s.searchMemories(context.Background(), db, "中文表格", 6)
	if len(items) != 0 {
		t.Fatalf("stale index still hits old content: %+v", items)
	}
	items, err = s.searchMemories(context.Background(), db, "Telegram", 6)
	if err != nil || len(items) != 1 {
		t.Fatalf("expected new content hit, got %v err=%v", items, err)
	}

	// 删除后不可检索
	if _, err := s.executeMemoryDelete(context.Background(), db, map[string]interface{}{"id": it.ID}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.executeMemoryDelete(context.Background(), db, map[string]interface{}{"id": it.ID}); err == nil {
		t.Fatalf("delete twice should fail")
	}
	items, _ = s.searchMemories(context.Background(), db, "Telegram", 6)
	if len(items) != 0 {
		t.Fatalf("deleted memory still searchable: %+v", items)
	}
}

// 中文两字词条（trigram 不支持 <3 字符）应走子串补偿检索。
func TestMemorySearchShortTokenFallback(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	insertMemoryTest(t, s, "用户偏好：回复默认用中文", 5, false)
	items, err := s.searchMemories(context.Background(), db, "中文", 6)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 hit via short-token fallback, got %d", len(items))
	}
}

// 评分排序：pinned 与高重要性应排在普通记忆之前（同相关度时）。
func TestMemorySearchRanking(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	insertMemoryTest(t, s, "网关默认模型是 deepseek-v3", 5, false)
	insertMemoryTest(t, s, "网关默认模型必须保持 deepseek-v3 不动", 9, true)
	items, err := s.searchMemories(context.Background(), db, "deepseek", 6)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(items))
	}
	if !items[0].Pinned {
		t.Fatalf("pinned memory should rank first, got %+v", items[0])
	}
}

// bootstrap 注入：按预算截断、置顶优先、超长条目跳过。
func TestMemoryBootstrapBudget(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	insertMemoryTest(t, s, "偏A：回复用中文", 5, false)
	insertMemoryTest(t, s, "偏B：通知走 Telegram", 9, true)
	block := s.bootstrapMemories(context.Background(), db, 60)
	if block == "" {
		t.Fatalf("expected non-empty bootstrap")
	}
	if !strings.Contains(block, "偏B") {
		t.Fatalf("pinned high-importance memory should be included first: %s", block)
	}
	if len([]rune(block)) > 60 {
		t.Fatalf("bootstrap over budget: %d", len([]rune(block)))
	}
	if s.bootstrapMemories(context.Background(), db, 5) != "" {
		t.Fatalf("tiny budget should yield empty block")
	}
}

// HTTP 层：列表/新增/更新/删除接口全链路。
func TestMemoryHTTPRoutes(t *testing.T) {
	s := newTestService(t)

	// POST 新增
	body := strings.NewReader(`{"content":"用户偏好：磁盘告警阈值 80%","importance":7}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin-ai/memories", body)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created MemoryItem
	var envelope struct {
		Data MemoryItem `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	created = envelope.Data
	if created.Importance != 7 || created.Source != "manual" {
		t.Fatalf("unexpected created: %+v", created)
	}

	// 参数校验：空内容 400
	req = httptest.NewRequest(http.MethodPost, "/api/admin-ai/memories", strings.NewReader(`{"content":"  "}`))
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty content should 400, got %d", rec.Code)
	}

	// GET 列表 + 搜索
	req = httptest.NewRequest(http.MethodGet, "/api/admin-ai/memories", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var list struct {
		Data struct {
			Items []MemoryItem `json:"items"`
			Total int          `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || list.Data.Total != 1 {
		t.Fatalf("list decode: %v items=%+v", err, list)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin-ai/memories?q=告警", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || list.Data.Total != 1 {
		t.Fatalf("search list decode: %v items=%+v", err, list)
	}

	// PUT 更新
	req = httptest.NewRequest(http.MethodPut, "/api/admin-ai/memories/"+created.ID, strings.NewReader(`{"pinned":true}`))
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	var updEnvelope struct {
		Data MemoryItem `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updEnvelope); err != nil || !updEnvelope.Data.Pinned {
		t.Fatalf("update decode: %v item=%+v", err, updEnvelope)
	}
	var updated MemoryItem
	updated = updEnvelope.Data
	if !updated.Pinned {
		t.Fatalf("update item: %+v", updated)
	}

	// DELETE 删除
	req = httptest.NewRequest(http.MethodDelete, "/api/admin-ai/memories/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d", rec.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/admin-ai/memories/"+created.ID, nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete twice should 404, got %d", rec.Code)
	}

	// 未登记路径 404
	req = httptest.NewRequest(http.MethodGet, "/api/admin-ai/memories/unknown/extra", nil)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path should 404, got %d", rec.Code)
	}
}

// AI 工具执行：memory_search / memory_add / memory_delete 的入参校验。
func TestMemoryTools(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	if _, err := s.executeMemorySearch(context.Background(), db, map[string]interface{}{}); err == nil {
		t.Fatalf("empty query should fail")
	}
	added, err := s.executeMemoryAdd(context.Background(), db, map[string]interface{}{"content": "测试记忆", "importance": 9.0, "triggers": "测试"}, "aas_1")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	addedMap := added.(map[string]interface{})
	if addedMap["importance"] != 9 {
		t.Fatalf("importance mismatch: %v", addedMap["importance"])
	}
	res, err := s.executeMemorySearch(context.Background(), db, map[string]interface{}{"query": "测试记忆", "limit": float64(10)})
	if err != nil {
		t.Fatalf("search tool: %v", err)
	}
	resMap := res.(map[string]interface{})
	if resMap["count"] != 1 {
		t.Fatalf("expected 1 result, got %v", resMap["count"])
	}
	results := resMap["results"].([]memorySearchResult)
	if results[0].Content != "测试记忆" {
		t.Fatalf("content mismatch: %v", results[0])
	}
	if _, err := s.executeMemoryDelete(context.Background(), db, map[string]interface{}{"id": "nope"}); err == nil {
		t.Fatalf("delete missing id should fail")
	}
}

// importance 越界钳制。
func TestMemoryClampImportance(t *testing.T) {
	if clampImportance(0) != 1 || clampImportance(99) != 10 || clampImportance(5) != 5 {
		t.Fatalf("clampImportance broken")
	}
}

// FTS 查询短语化：特殊字符不炸查询（转义后安全）。
func TestMemoryBuildFTSQuery(t *testing.T) {
	q := buildMemoryFTSQuery(memoryTokens(`a"b 中文词 短`))
	if q != `"中文词"` {
		t.Fatalf("unexpected fts query: %s", q)
	}
	if buildMemoryFTSQuery(memoryTokens("短")) != "" {
		t.Fatalf("short-only tokens should yield empty fts query")
	}
}

func strPtr(s string) *string { return &s }
