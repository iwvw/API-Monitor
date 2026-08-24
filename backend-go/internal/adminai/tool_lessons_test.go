package adminai

import (
	"context"
	"strings"
	"testing"
)

// 教训沉淀：同接口先失败后成功（参数差异）→ 落库为长期记忆。
func TestCaptureToolLessonsPersists(t *testing.T) {
	s := newTestService(t)
	seedSession(t, s, "aas_lesson")
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	tracker := &toolLessonTracker{}
	tracker.record("call_api", map[string]interface{}{"method": "POST", "path": "/api/server/action", "body": map[string]interface{}{"action": "restart"}}, "unsupported action: 400", false)
	tracker.record("call_api", map[string]interface{}{"method": "POST", "path": "/api/server/action", "body": map[string]interface{}{"action": "stop"}}, "", true)

	s.captureToolLessons(context.Background(), db, "aas_lesson", tracker)

	var content, source string
	err = db.QueryRowContext(context.Background(),
		`SELECT content, source FROM admin_ai_memories ORDER BY created_at DESC LIMIT 1`).Scan(&content, &source)
	if err != nil {
		t.Fatalf("query memory: %v", err)
	}
	if source != lessonSource {
		t.Fatalf("expected source=lesson, got %q", source)
	}
	for _, want := range []string{"/api/server/action", "unsupported action", "body="} {
		if !strings.Contains(content, want) {
			t.Fatalf("lesson content missing %q: %s", want, content)
		}
	}
}

// 失败后没有同接口成功 → 不沉淀（避免无修正的经验垃圾）。
func TestCaptureToolLessonsSkipsWithoutFix(t *testing.T) {
	s := newTestService(t)
	seedSession(t, s, "aas_lesson2")
	db, _ := s.open(context.Background())
	defer db.Close()

	tracker := &toolLessonTracker{}
	tracker.record("call_api", map[string]interface{}{"method": "GET", "path": "/api/server/status"}, "boom", false)

	s.captureToolLessons(context.Background(), db, "aas_lesson2", tracker)

	var count int
	_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM admin_ai_memories`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no lessons, got %d", count)
	}
}

// 参数完全相同（非修正场景，如瞬时网络错误）→ 不沉淀。
func TestCaptureToolLessonsSkipsSameArgs(t *testing.T) {
	s := newTestService(t)
	seedSession(t, s, "aas_lesson3")
	db, _ := s.open(context.Background())
	defer db.Close()

	tracker := &toolLessonTracker{}
	args := map[string]interface{}{"method": "GET", "path": "/api/hosts"}
	tracker.record("call_api", args, "timeout", false)
	tracker.record("call_api", args, "", true)

	s.captureToolLessons(context.Background(), db, "aas_lesson3", tracker)

	var count int
	_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM admin_ai_memories`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no lessons for same-args retry, got %d", count)
	}
}

// 去重：同一接口同一错误已存在 → 不再重复插入。
func TestCaptureToolLessonsDedup(t *testing.T) {
	s := newTestService(t)
	seedSession(t, s, "aas_lesson4")
	db, _ := s.open(context.Background())
	defer db.Close()

	tracker := func() *toolLessonTracker {
		tr := &toolLessonTracker{}
		tr.record("call_api", map[string]interface{}{"method": "POST", "path": "/api/server/action", "body": map[string]interface{}{"action": "x"}}, "unsupported action", false)
		tr.record("call_api", map[string]interface{}{"method": "POST", "path": "/api/server/action", "body": map[string]interface{}{"action": "y"}}, "", true)
		return tr
	}
	s.captureToolLessons(context.Background(), db, "aas_lesson4", tracker())
	s.captureToolLessons(context.Background(), db, "aas_lesson4", tracker())

	var count int
	_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM admin_ai_memories`).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 deduped lesson, got %d", count)
	}
}

// get_route 换 path 属「换接口」而非修正，不沉淀（保守：只记同接口参数修正）。
func TestCaptureToolLessonsGetRoute(t *testing.T) {
	s := newTestService(t)
	seedSession(t, s, "aas_lesson5")
	db, _ := s.open(context.Background())
	defer db.Close()

	tracker := &toolLessonTracker{}
	tracker.record("get_route", map[string]interface{}{"path": "/api/cron/tasks"}, "route not found", false)
	tracker.record("get_route", map[string]interface{}{"path": "/api/scheduler/tasks"}, "", true)

	s.captureToolLessons(context.Background(), db, "aas_lesson5", tracker)

	var count int
	_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM admin_ai_memories`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no lesson for different-path fallback, got %d", count)
	}
}

// 路径修正配对：猜错聚合前缀路径（404）→ 改用清单内真实子路由成功，
// 应沉淀「勿猜聚合路径」教训（否则同一 404 会在每次执行里重犯）。
func TestCaptureToolLessonsPathCorrection(t *testing.T) {
	s := newTestService(t)
	seedSession(t, s, "aas_lesson6")
	db, _ := s.open(context.Background())
	defer db.Close()

	tracker := &toolLessonTracker{}
	tracker.record("call_api", map[string]interface{}{"method": "GET", "path": "/api/scheduler"}, "scheduler route not implemented (HTTP 404)", false)
	tracker.record("call_api", map[string]interface{}{"method": "GET", "path": "/api/scheduler/tasks"}, "", true)

	s.captureToolLessons(context.Background(), db, "aas_lesson6", tracker)

	var content string
	err := db.QueryRowContext(context.Background(),
		`SELECT content FROM admin_ai_memories ORDER BY created_at DESC LIMIT 1`).Scan(&content)
	if err != nil {
		t.Fatalf("query memory: %v", err)
	}
	for _, want := range []string{"/api/scheduler", "/api/scheduler/tasks", "勿再猜测"} {
		if !strings.Contains(content, want) {
			t.Fatalf("lesson content missing %q: %s", want, content)
		}
	}
}

// 路径修正配对的保守边界：错误不是「路径不存在」类、成功路径不是失败路径的
// 子路由、或失败为 get_route 时，都不沉淀。
func TestCaptureToolLessonsPathCorrectionSkips(t *testing.T) {
	s := newTestService(t)
	seedSession(t, s, "aas_lesson7")
	db, _ := s.open(context.Background())
	defer db.Close()

	// 错误类型不符（业务失败而非 404）→ 不沉淀
	tr := &toolLessonTracker{}
	tr.record("call_api", map[string]interface{}{"method": "GET", "path": "/api/scheduler/status"}, "business failed: quota", false)
	tr.record("call_api", map[string]interface{}{"method": "GET", "path": "/api/scheduler/tasks"}, "", true)
	s.captureToolLessons(context.Background(), db, "aas_lesson7", tr)

	// 失败 get_route + 成功 call_api（跨工具）→ 不沉淀
	tr2 := &toolLessonTracker{}
	tr2.record("get_route", map[string]interface{}{"path": "/api/scheduler"}, "route not found", false)
	tr2.record("call_api", map[string]interface{}{"method": "GET", "path": "/api/scheduler/tasks"}, "", true)
	s.captureToolLessons(context.Background(), db, "aas_lesson7", tr2)

	var count int
	_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM admin_ai_memories`).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no lessons, got %d", count)
	}
}

// argsDiff：只列出差异字段。
func TestArgsDiff(t *testing.T) {
	diff := argsDiff(
		map[string]interface{}{"method": "POST", "path": "/api/x", "body": map[string]interface{}{"action": "restart"}, "id": "1"},
		map[string]interface{}{"method": "POST", "path": "/api/x", "body": map[string]interface{}{"action": "stop"}, "id": "1"},
	)
	if !strings.Contains(diff, "body=") || strings.Contains(diff, "id=") {
		t.Fatalf("unexpected diff: %s", diff)
	}
	if strings.Contains(diff, "restart") {
		t.Fatalf("diff should contain corrected value, got: %s", diff)
	}
}
