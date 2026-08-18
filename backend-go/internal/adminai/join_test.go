package adminai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// seedSession 建会话行以满足 messages 的外键约束。
func seedSession(t *testing.T, s *Service, id string) {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_sessions (id, source, title, model, write_enabled, created_at, updated_at, last_activity_at) VALUES (?, 'web', '', '', 0, ?, ?, ?)`,
		id, now, now, now)
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// submitMessage 探测：活跃 run 存在时追问应入队（queued=true）而非 409，
// 队列由活跃 run 续跑消费（join 语义，对齐 opencode）。
func TestSubmitMessageJoinsActiveRun(t *testing.T) {
	s := newTestService(t)
	seedSession(t, s, "aas_join")
	s.mu.Lock()
	s.sessionRuns["aas_join"] = "aae_fake_running"
	s.mu.Unlock()

	payload, _ := json.Marshal(map[string]string{"sessionId": "aas_join", "prompt": "装好了吗？"})
	req, _ := http.NewRequest("POST", "/api/admin-ai/messages", strings.NewReader(string(payload)))
	rec := httptest.NewRecorder()
	s.submitMessage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("join submit status=%d body=%s", rec.Code, rec.Body.String())
	}
	var outer struct {
		Data struct {
			Queued bool   `json:"queued"`
			RunID  string `json:"runId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &outer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp := outer.Data
	if !resp.Queued || resp.RunID != "aae_fake_running" {
		t.Fatalf("expected queued=true runId=aae_fake_running, got %+v", resp)
	}

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var count int
	err = db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_messages WHERE session_id = 'aas_join' AND role = 'user'`).Scan(&count)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Fatalf("queued user message not persisted, count=%d", count)
	}
}

// submitMessage 并发入队：会话活跃期间并发追问全部 200 + 落库（无 409、无丢消息）。
func TestSubmitMessageRaceBoundary(t *testing.T) {
	s := newTestService(t)
	seedSession(t, s, "aas_race")
	s.mu.Lock()
	s.sessionRuns["aas_race"] = "aae_race_active"
	s.mu.Unlock()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	var before int
	_ = db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_messages WHERE session_id = 'aas_race' AND role = 'user'`).Scan(&before)
	db.Close()

	start := make(chan struct{})
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func(i int) {
			<-start
			payload, _ := json.Marshal(map[string]string{"sessionId": "aas_race", "prompt": "并发追问"})
			req, _ := http.NewRequest("POST", "/api/admin-ai/messages", strings.NewReader(string(payload)))
			rec := httptest.NewRecorder()
			s.submitMessage(rec, req)
			if rec.Code != http.StatusOK {
				errs <- fmt.Errorf("submit#%d status=%d body=%s", i, rec.Code, rec.Body.String())
				return
			}
			errs <- nil
		}(i)
	}
	close(start)
	for i := 0; i < 8; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("%v", err)
		}
	}
	// 所有提交都以 200 落库（无 409），消息未丢
	db2, _ := s.open(context.Background())
	defer db2.Close()
	var after int
	_ = db2.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM admin_ai_messages WHERE session_id = 'aas_race' AND role = 'user'`).Scan(&after)
	if after != before+8 {
		t.Fatalf("queued messages lost: before=%d after=%d", before, after)
	}
	s.mu.Lock()
	delete(s.sessionRuns, "aas_race")
	s.mu.Unlock()
}