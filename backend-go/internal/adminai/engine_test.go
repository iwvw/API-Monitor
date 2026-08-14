package adminai

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func insertTestSession(t *testing.T, s *Service, id string) {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(context.Background(),
		`INSERT OR IGNORE INTO admin_ai_sessions (id, source, title, write_enabled, created_at, updated_at, last_activity_at) VALUES (?, 'test', '测试会话', 0, ?, ?, ?)`,
		id, now, now, now)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func insertTestMsg(t *testing.T, s *Service, id, sessionID, role, content, toolMeta, toolCallID string) {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	if toolCallID != "" {
		_, err = db.ExecContext(context.Background(),
			`INSERT INTO admin_ai_messages (id, session_id, role, content, tool_call_meta, tool_call_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, sessionID, role, content, toolMeta, toolCallID, now)
	} else {
		_, err = db.ExecContext(context.Background(),
			`INSERT INTO admin_ai_messages (id, session_id, role, content, tool_call_meta, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, sessionID, role, content, toolMeta, now)
	}
	if err != nil {
		t.Fatalf("insert msg %s: %v", id, err)
	}
}

func countTestMsgs(t *testing.T, s *Service, sessionID string) int {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM admin_ai_messages WHERE session_id = ?`, sessionID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func newToolCall(id, name, args string) toolCall {
	var tc toolCall
	tc.ID = id
	tc.Type = "function"
	tc.Function.Name = name
	tc.Function.Arguments = args
	return tc
}

func TestRestoreSessionHistoryParallelToolCalls(t *testing.T) {
	s := newTestService(t)
	session := "aas_restore_parallel"
	insertTestSession(t, s, session)

	// 新格式：一个 assistant 行携带全部 tool_calls（JSON 数组），tool 行各自带 tool_call_id
	meta, _ := json.Marshal([]toolCall{
		newToolCall("call_1", "get_system_status", "{}"),
		newToolCall("call_2", "get_route", `{"path":"/api/system/ai-access"}`),
	})
	insertTestMsg(t, s, "aam_01", session, "user", "看看系统状态", "", "")
	insertTestMsg(t, s, "aam_02", session, "assistant", "", string(meta), "")
	insertTestMsg(t, s, "aam_03", session, "tool", "cpu 正常", "", "call_1")
	insertTestMsg(t, s, "aam_04", session, "tool", "路由已读取", "", "call_2")
	insertTestMsg(t, s, "aam_05", session, "assistant", "系统状态正常", "", "")

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	msgs, err := s.restoreSessionHistory(context.Background(), db, session)
	db.Close()
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("期望 5 条消息，实际 %d", len(msgs))
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 2 {
		t.Fatalf("assistant 应携带 2 个 tool_calls，实际 %d", len(msgs[1].ToolCalls))
	}
	if msgs[1].ToolCalls[0].ID != "call_1" || msgs[1].ToolCalls[1].ID != "call_2" {
		t.Fatalf("tool_calls 顺序/ID 不符: %s,%s", msgs[1].ToolCalls[0].ID, msgs[1].ToolCalls[1].ID)
	}
	// 并行 tool 结果必须按各自 tool_call_id 配对
	if msgs[2].ToolCallID != "call_1" || msgs[3].ToolCallID != "call_2" {
		t.Fatalf("tool 结果配对错误: %s,%s", msgs[2].ToolCallID, msgs[3].ToolCallID)
	}
	// 不应误删任何行
	if got := countTestMsgs(t, s, session); got != 5 {
		t.Fatalf("不应删除历史行，实际剩 %d", got)
	}
}

func TestRestoreSessionHistoryLegacyParallelRows(t *testing.T) {
	s := newTestService(t)
	session := "aas_restore_legacy"
	insertTestSession(t, s, session)

	// 旧格式：并行轮每个 tool_call 各占一个 assistant 行（单条 tool_call_meta），
	// tool 行无 tool_call_id 列，按顺序回填配对。
	meta1, _ := json.Marshal(newToolCall("call_1", "get_system_status", "{}"))
	meta2, _ := json.Marshal(newToolCall("call_2", "get_route", `{"path":"/api/system/ai-access"}`))
	insertTestMsg(t, s, "aam_01", session, "user", "看看状态", "", "")
	insertTestMsg(t, s, "aam_02", session, "assistant", "", string(meta1), "")
	insertTestMsg(t, s, "aam_03", session, "assistant", "", string(meta2), "")
	insertTestMsg(t, s, "aam_04", session, "tool", "cpu 正常", "", "")
	insertTestMsg(t, s, "aam_05", session, "tool", "路由已读取", "", "")

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	msgs, err := s.restoreSessionHistory(context.Background(), db, session)
	db.Close()
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	// 旧格式并行轮合并为一个 assistant（含 2 tool_calls）+ 2 tool 结果 → 共 4 条
	if len(msgs) != 4 {
		t.Fatalf("期望 4 条消息（合并后 1 个 assistant + 2 tool），实际 %d", len(msgs))
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 2 {
		t.Fatalf("旧格式并行轮应合并为 1 个 assistant + 2 tool_calls，实际 %d", len(msgs[1].ToolCalls))
	}
	// 缺失 tool_call_id 的行按顺序回填
	if msgs[2].ToolCallID != "call_1" || msgs[3].ToolCallID != "call_2" {
		t.Fatalf("旧格式 tool 行回填配对错误: %s,%s", msgs[2].ToolCallID, msgs[3].ToolCallID)
	}
	if got := countTestMsgs(t, s, session); got != 5 {
		t.Fatalf("旧格式合法行不应被删除，实际剩 %d", got)
	}
}

func TestRestoreSessionHistoryDropsInterruptedRound(t *testing.T) {
	s := newTestService(t)
	session := "aas_restore_interrupted"
	insertTestSession(t, s, session)

	meta, _ := json.Marshal([]toolCall{
		newToolCall("call_1", "call_api", "{}"),
		newToolCall("call_2", "call_api", "{}"),
	})
	insertTestMsg(t, s, "aam_01", session, "user", "删除资源", "", "")
	// 中断轮：assistant 声明 2 个调用，但只有 1 个 tool 结果（缺 call_2）
	insertTestMsg(t, s, "aam_02", session, "assistant", "", string(meta), "")
	insertTestMsg(t, s, "aam_03", session, "tool", "已删除", "", "call_1")
	// 完整轮（后续正常执行留下的历史）
	meta2, _ := json.Marshal(newToolCall("call_3", "get_system_status", "{}"))
	insertTestMsg(t, s, "aam_04", session, "assistant", "", string(meta2), "")
	insertTestMsg(t, s, "aam_05", session, "tool", "cpu 正常", "", "call_3")
	insertTestMsg(t, s, "aam_06", session, "assistant", "完成", "", "")

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	msgs, err := s.restoreSessionHistory(context.Background(), db, session)
	db.Close()
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	// 中断轮（assistant + 残留 tool）被整体丢弃，用户消息保留；仅剩完整轮
	if len(msgs) != 4 {
		t.Fatalf("期望丢弃中断轮后剩 4 条，实际 %d", len(msgs))
	}
	if msgs[1].Role != "assistant" || len(msgs[1].ToolCalls) != 1 || msgs[1].ToolCalls[0].ID != "call_3" {
		t.Fatalf("剩余轮应为完整轮 call_3，实际 %+v", msgs[1])
	}
	if got := countTestMsgs(t, s, session); got != 4 {
		t.Fatalf("中断轮行应从库中删除，实际剩 %d", got)
	}
}

func TestRestoreSessionHistoryDropsOrphanTool(t *testing.T) {
	s := newTestService(t)
	session := "aas_restore_orphan"
	insertTestSession(t, s, session)

	insertTestMsg(t, s, "aam_01", session, "user", "你好", "", "")
	insertTestMsg(t, s, "aam_02", session, "assistant", "好的", "", "")
	// 无前置 assistant tool_calls 的孤儿 tool 行
	insertTestMsg(t, s, "aam_03", session, "tool", "孤儿结果", "", "call_x")

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	msgs, err := s.restoreSessionHistory(context.Background(), db, session)
	db.Close()
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("期望孤儿 tool 行被丢弃后剩 2 条，实际 %d", len(msgs))
	}
	if got := countTestMsgs(t, s, session); got != 2 {
		t.Fatalf("孤儿行应从库中删除，实际剩 %d", got)
	}
}
