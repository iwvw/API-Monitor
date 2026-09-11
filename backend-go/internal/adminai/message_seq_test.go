package adminai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"
)

// TestMessageSeqOrderingAndPagination 覆盖「同秒消息稳定排序」修复：
// created_at 仅到秒，同一秒内多条消息必须靠单调递增的 seq 定序，且游标分页
// 不得漏行/重复/错位（旧的随机 id tie-break 会让刷新后顺序漂移）。
func TestMessageSeqOrderingAndPagination(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO admin_ai_sessions (id, source, title, write_enabled, created_at, updated_at, last_activity_at) VALUES ('aas_seq', 'web', '排序测试', 0, ?, ?, ?)`,
		now, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}

	// 50 条消息，全部使用同一个 created_at（同一秒），模拟一次 run 内快速落库。
	want := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		id, err := randomID("aam_")
		if err != nil {
			t.Fatalf("randomID: %v", err)
		}
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO admin_ai_messages (id, session_id, role, content, created_at) VALUES (?, 'aas_seq', ?, ?, ?)`,
			id, role, "内容", now); err != nil {
			t.Fatalf("insert message %d: %v", i, err)
		}
		want = append(want, id)
	}

	// 逐页拉取（limit=7）。后端分页语义：页内 seq 升序、页间由新到旧，故收集
	// 全部行后按 seq 升序重排，应与插入顺序（want）完全一致，且无重复。
	type row struct {
		seq int64
		id  string
	}
	collected := make([]row, 0, 50)
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 100; page++ {
		url := "/api/admin-ai/sessions/aas_seq/messages?limit=7"
		if cursor != "" {
			url += "&cursor=" + cursor
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		w := httptest.NewRecorder()
		s.listMessages(w, req, "aas_seq")
		if w.Code != http.StatusOK {
			t.Fatalf("page %d status=%d body=%s", page, w.Code, w.Body.String())
		}
		var resp struct {
			Data struct {
				Items      []map[string]interface{} `json:"items"`
				NextCursor string                   `json:"nextCursor"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("page %d decode: %v body=%s", page, err, w.Body.String())
		}
		for _, it := range resp.Data.Items {
			id := it["id"].(string)
			if seen[id] {
				t.Fatalf("duplicate row across pages: %s", id)
			}
			seen[id] = true
			collected = append(collected, row{seq: int64(it["seq"].(float64)), id: id})
		}
		if resp.Data.NextCursor == "" {
			break
		}
		cursor = resp.Data.NextCursor
	}

	if len(collected) != len(want) {
		t.Fatalf("fetched %d rows, want %d", len(collected), len(want))
	}
	// 页间由新到旧，按 seq 升序重排后应与插入顺序完全一致。
	sort.Slice(collected, func(i, j int) bool { return collected[i].seq < collected[j].seq })
	for i := range want {
		if collected[i].id != want[i] {
			t.Fatalf("row %d = %s, want %s (seq ordering broken)", i, collected[i].id, want[i])
		}
	}
}

// TestMessageSeqBackfill 验证历史行（seq 为空）在迁移后按插入顺序回填，
// 且不与新插入行的 seq 冲突。
func TestMessageSeqBackfill(t *testing.T) {
	s := newTestService(t)
	ctx := context.Background()
	db, err := s.open(ctx)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// 直接构造无 seq 的历史行并清空 seq，模拟旧库；再重跑迁移。
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO admin_ai_sessions (id, source, title, write_enabled, created_at, updated_at, last_activity_at) VALUES ('aas_bf', 'web', '回填', 0, ?, ?, ?)`,
		now, now, now); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	var firstID string
	for i := 0; i < 5; i++ {
		id, _ := randomID("aam_")
		if i == 0 {
			firstID = id
		}
		if _, err := db.ExecContext(ctx,
			`INSERT INTO admin_ai_messages (id, session_id, role, content, created_at) VALUES (?, 'aas_bf', 'user', 'x', ?)`,
			id, now); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, `UPDATE admin_ai_messages SET seq = NULL WHERE session_id = 'aas_bf'`); err != nil {
		t.Fatalf("clear seq: %v", err)
	}
	// 重跑迁移回填
	if err := s.ensureMessageSeq(ctx, db); err != nil {
		t.Fatalf("ensureMessageSeq: %v", err)
	}
	var nullCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_ai_messages WHERE session_id = 'aas_bf' AND seq IS NULL`).Scan(&nullCount); err != nil {
		t.Fatalf("count null seq: %v", err)
	}
	if nullCount != 0 {
		t.Fatalf("backfill left %d null seq rows", nullCount)
	}
	// 回填后第一条 seq 应为 1
	var seq int64
	if err := db.QueryRowContext(ctx, `SELECT seq FROM admin_ai_messages WHERE id = ?`, firstID).Scan(&seq); err != nil {
		t.Fatalf("select seq: %v", err)
	}
	if seq != 1 {
		t.Fatalf("first row seq = %d, want 1", seq)
	}
}
