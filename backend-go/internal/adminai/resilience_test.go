package adminai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// --- 事件缓冲：seq 递增、环形覆盖、非增量重放、终态停止 ---

func TestRunEventBufferAppendAndReplay(t *testing.T) {
	b := newRunEventBuffer()
	ev := func(typ, text string) SSEEvent {
		return SSEEvent{Type: typ, Fields: map[string]interface{}{"text": text}}
	}
	// 写入超过容量一半的事件（含增量与非增量）
	for i := 0; i < 6000; i++ {
		typ := "delta"
		payload := fmt.Sprintf("d%d", i)
		if i%10 == 0 {
			typ = "tool_result"
		}
		b.appendSeq(ev(typ, payload))
	}
	var replayed []string
	terminal := false
	b.replayAfter(0, func(seq int64, e SSEEvent) {
		replayed = append(replayed, e.Type)
		if e.Type == "done" {
			terminal = true
		}
	})
	if terminal {
		t.Fatalf("replay should stop at done, got terminal=true")
	}
	// 无 done 时不应有阻断，全部非增量事件可见（delta 跳过）
	for _, typ := range replayed {
		if typ == "delta" {
			t.Fatalf("delta must not be replayed, got %v", typ)
		}
	}
	if len(replayed) != 409 {
		t.Fatalf("expect 409 tool_result events (ring keeps last 4096 of 6000), got %d", len(replayed))
	}

	// 环形覆盖后仍按 seq 升序且带正确 seq
	firstSeq := int64(0)
	b.replayAfter(5900, func(seq int64, e SSEEvent) {
		if firstSeq == 0 {
			firstSeq = seq
		}
	})
	if firstSeq <= 5900 {
		t.Fatalf("replay fromSeq=5900 should start above 5900, got %d", firstSeq)
	}

	// done 提前终止
	b2 := newRunEventBuffer()
	b2.appendSeq(ev("delta", "x"))
	b2.appendSeq(ev("tool_start", "x"))
	b2.appendSeq(ev("done", ""))
	b2.appendSeq(ev("delta", "y")) // 不应被重放（done 后停止）
	var got []string
	b2.replayAfter(0, func(_ int64, e SSEEvent) { got = append(got, e.Type) })
	if len(got) != 2 || got[0] != "tool_start" || got[1] != "done" {
		t.Fatalf("expect [tool_start done], got %v", got)
	}
}

// emit 写入缓冲的事件必须携带 __seq，且 buffer/replay 与通道事件一致。
func TestEmitWritesBufferedSeq(t *testing.T) {
	s := newTestService(t)
	ch := make(chan SSEEvent, eventChBuffer)
	buf := newRunEventBuffer()
	s.mu.Lock()
	s.chToBuf[ch] = buf
	s.mu.Unlock()

	ev := SSEEvent{Type: "delta", Fields: map[string]interface{}{"text": "你好"}}
	s.emit(ch, ev)
	s.emit(ch, SSEEvent{Type: "tool_start", Fields: map[string]interface{}{"toolName": "x"}})

	if got := buf.seq; got != 2 {
		t.Fatalf("buffer seq = %d, want 2", got)
	}
	select {
	case got := <-ch:
		if got.Fields["__seq"] != int64(1) {
			t.Fatalf("channel event seq = %v, want 1", got.Fields["__seq"])
		}
	default:
		t.Fatalf("channel should have events")
	}
	// 缓冲侧事件同样带 __seq
	var seqs []int64
	buf.replayAfter(0, func(seq int64, _ SSEEvent) { seqs = append(seqs, seq) })
	// delta 被跳过，只剩 tool_start（seq=2）
	if len(seqs) != 1 || seqs[0] != 2 {
		t.Fatalf("replay seqs = %v, want [2]", seqs)
	}
}

// 断线重连（resume）HTTP 层：run 结束后按 fromSeq 补发终态，且跳过增量事件。
func TestStreamEventsResumeReplaysTerminal(t *testing.T) {
	s := newTestService(t)
	ch := make(chan SSEEvent, eventChBuffer)
	buf := newRunEventBuffer()
	s.mu.Lock()
	runID := "aae_resume_test"
	s.runs[runID] = ch
	s.runBuffers[runID] = buf
	s.chToBuf[ch] = buf
	s.mu.Unlock()

	s.emit(ch, SSEEvent{Type: "delta", Fields: map[string]interface{}{"text": "增量"}})
	s.emit(ch, SSEEvent{Type: "reasoning", Fields: map[string]interface{}{"text": "思考"}})
	s.emit(ch, SSEEvent{Type: "tool_result", Fields: map[string]interface{}{"toolName": "x", "status": "success"}})
	s.emit(ch, SSEEvent{Type: "done", Fields: map[string]interface{}{"messageId": "m1"}})
	s.mu.Lock()
	if b := s.runBuffers[runID]; b != nil {
		b.markDone()
	}
	s.mu.Unlock()
	s.mu.Lock()
	if c, ok := s.runs[runID]; ok {
		close(c)
		delete(s.runs, runID)
	}
	s.mu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin-ai/messages/stream?runId="+runID+"&resume=1&fromSeq=0", nil)
	s.streamEvents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "增量") || strings.Contains(body, "思考") {
		t.Fatalf("delta/reasoning must be skipped on resume replay")
	}
	if !strings.Contains(body, "event: tool_result") || !strings.Contains(body, "event: done") {
		t.Fatalf("resume replay must contain tool_result and done, got: %s", body)
	}
	if !strings.Contains(body, "id:") {
		t.Fatalf("replay events must carry seq id, got: %s", body)
	}
}

// --- 设置 clamp：超大值收敛，防止 context_window 误配导致上下文膨胀 ---

func TestClampAISetting(t *testing.T) {
	cases := []struct {
		key, in, want string
	}{
		{"admin_ai_context_window", "1000000", "1000000"},
		{"admin_ai_context_window", "2000000", "1000000"},
		{"admin_ai_context_window", "128000", "128000"},
		{"admin_ai_context_window", "100", "4000"},
		{"admin_ai_timeout_seconds", "300", "300"},
		{"admin_ai_timeout_seconds", "10", "30"},
		{"admin_ai_timeout_seconds", "99999", "3600"},
		{"admin_ai_tool_call_limit", "50", "50"},
		{"admin_ai_tool_call_limit", "500", "100"},
		{"admin_ai_default_model", "gemini-x", "gemini-x"},
	}
	for _, c := range cases {
		if got := clampAISetting(c.key, c.in); got != c.want {
			t.Errorf("clampAISetting(%q, %q) = %q, want %q", c.key, c.in, got, c.want)
		}
	}
}

// --- LLM 首字护栏：网关挂起不发数据时按 firstTokenTimeout 中止 ---

func TestCallLLMStreamFirstTokenTimeout(t *testing.T) {
	orig := firstTokenTimeout
	firstTokenTimeout = 200 * time.Millisecond
	defer func() { firstTokenTimeout = orig }()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port
	s := newTestService(t)
	s.cfg.Port = port

	start := time.Now()
	_, err := s.callLLMStream(context.Background(), "test-model", []map[string]interface{}{{"role": "user", "content": "hi"}}, make(chan SSEEvent, 8), "aam_test_1")
	if err == nil {
		t.Fatalf("expected first-token timeout error")
	}
	if !strings.Contains(err.Error(), "未收到首个数据块") {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("first-token guard too slow: %v", elapsed)
	}
}

// --- 摘要模型多候选回退：首个失败自动尝试下一个；逗号解析去空白 ---

func TestParseModelList(t *testing.T) {
	cases := []struct{ in string; want []string }{
		{"gemini-3.1-flash-lite,gpt-oss-120b", []string{"gemini-3.1-flash-lite", "gpt-oss-120b"}},
		{"  a , b ,, c ", []string{"a", "b", "c"}},
		{"", nil},
		{"single", []string{"single"}},
	}
	for _, c := range cases {
		got := parseModelList(c.in)
		if len(got) != len(c.want) {
			t.Errorf("parseModelList(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseModelList(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

// summarizeReasoning 回退：mock 网关按模型名返回不同结果（首个 500，第二个成功）。
func TestSummarizeReasoningFallback(t *testing.T) {
	s := newTestService(t)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			var req struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(body, &req)
			if req.Model == "slow-model" {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"状态总览"}}]}`)
			return
		}
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer ts.Close()
	s.cfg.Port = ts.Listener.Addr().(*net.TCPAddr).Port

	text := s.summarizeReasoning(context.Background(), []string{"slow-model", "fast-model"}, "这是一段足够长的推理内容用于生成标题式摘要，超过了四十个字符的门槛，应该可以被处理。")
	if text != "状态总览" {
		t.Fatalf("fallback summary = %q, want 状态总览", text)
	}
	// 全部失败 → 空串
	text2 := s.summarizeReasoning(context.Background(), []string{"slow-model"}, "这是一段足够长的推理内容用于生成标题式摘要，超过了四十个字符的门槛，应该可以被处理。")
	if text2 != "" {
		t.Fatalf("all-fail summary = %q, want empty", text2)
	}
}

// --- 会话标题长度治理：≤16 保留；超长在收尾词完整处截断，避免半截词 ---

func TestTrimTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"并行查询系统及AI接口状态", "并行查询系统及AI接口状态"},     // 13 字 ≤16 原样
		{"查询所有云服务器CPU使用率", "查询所有云服务器CPU使用率"},     // 14 字 ≤16 原样
		{"ABCDEFGHIJKLMNOP状态详情报告", "ABCDEFGHIJKLMNOP状态"},   // 22 字，截断点后即收尾词"状态"：保留完整词尾（18 字）
		{"今天下午三点检查所有域名解析记录和证书到期状态做个全面检查汇总", "今天下午三点检查所有域名解析记录"}, // 31 字，无词表命中硬切 16
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := trimTitle(c.in); got != c.want {
			t.Errorf("trimTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- 只读工具并行 + 写操作串行：同轮 3 个慢 GET 并发执行，POST 在其后 ---

func TestParallelReadonlyToolsThenWrite(t *testing.T) {
	s := newTestService(t)

	var mu sync.Mutex
	var getFinished []time.Time
	var postStarted time.Time
	var getConcurrency int32
	var maxConcurrency int32
	mockCaller := func(_ context.Context, req systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
		if req.Method == http.MethodGet && strings.HasPrefix(req.Path, "/read/") {
			cur := atomic.AddInt32(&getConcurrency, 1)
			defer atomic.AddInt32(&getConcurrency, -1)
			for {
				old := atomic.LoadInt32(&maxConcurrency)
				if cur <= old || atomic.CompareAndSwapInt32(&maxConcurrency, old, cur) {
					break
				}
			}
			time.Sleep(300 * time.Millisecond)
			mu.Lock()
			getFinished = append(getFinished, time.Now())
			mu.Unlock()
			return systemmetrics.AICallResponse{StatusCode: 200, Body: []byte(`{"data":{"ok":true}}`)}, nil
		}
		if req.Method == http.MethodPost {
			mu.Lock()
			postStarted = time.Now()
			mu.Unlock()
			return systemmetrics.AICallResponse{StatusCode: 200, Body: []byte(`{"data":{"created":true}}`)}, nil
		}
		return systemmetrics.AICallResponse{StatusCode: 200, Body: []byte(`{}`)}, nil
	}
	s.SetAICaller(mockCaller)

	// 写操作免审批（模拟运行环境），否则 POST 会走审批链被拒
	dbCfg, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := dbCfg.ExecContext(context.Background(),
		`INSERT OR REPLACE INTO system_config (key, value, updated_at) VALUES ('admin_ai_auto_approve', 'true', ?)`,
		time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("config: %v", err)
	}
	dbCfg.Close()

// mock LLM 网关：仅主流程推理请求（携带 tools）走轮次控制；会话标题/摘要等
	// 辅助调用（无 tools）统一返回普通文本，避免抢占轮次计数。
	var round int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/chat/completions" {
body, _ := io.ReadAll(r.Body)
			hasTools := strings.Contains(string(body), `"tools"`)
			w.Header().Set("Content-Type", "text/event-stream")
			fl := w.(http.Flusher)
			var payload string
			if !hasTools {
				payload = `{"choices":[{"delta":{"content":"辅助调用"}}]}`
			} else if atomic.AddInt32(&round, 1) == 1 {
				payload = `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"call_api","arguments":"{\"method\":\"GET\",\"path\":\"/read/1\"}"}},{"index":1,"id":"c2","type":"function","function":{"name":"call_api","arguments":"{\"method\":\"GET\",\"path\":\"/read/2\"}"}},{"index":2,"id":"c3","type":"function","function":{"name":"call_api","arguments":"{\"method\":\"GET\",\"path\":\"/read/3\"}"}},{"index":3,"id":"c4","type":"function","function":{"name":"call_api","arguments":"{\"method\":\"POST\",\"path\":\"/write\"}"}}]}}]}`
			} else {
				payload = `{"choices":[{"delta":{"content":"并行测试完成"}}]}`
			}
			fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", payload)
			fl.Flush()
			return
		}
		http.Error(w, "no", http.StatusNotFound)
	}))
	defer ts.Close()

	port := ts.Listener.Addr().(*net.TCPAddr).Port
	s.cfg.Port = port

	runID, err := s.RunLoop(context.Background(), "web", "aas_parallel", "并行执行三个只读查询后执行一次写操作", "", "", "")
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	_ = runID

	// 等待 run 收尾（LLM 两轮 + 工具执行，最长容 15s）
	deadline := time.Now().Add(15 * time.Second)
	for {
		s.mu.Lock()
		_, running := s.sessionRuns["aas_parallel"]
		s.mu.Unlock()
		if !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not finish in time")
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(getFinished) != 3 {
		t.Fatalf("GET calls = %d, want 3", len(getFinished))
	}
	if maxConcurrency < 2 {
		t.Fatalf("GET calls were not parallel (max concurrency %d, want >=2)", maxConcurrency)
	}
	// 串行版墙钟约 900ms+；并行版（300ms 假延迟）应远小于串行总和
	spread := getFinished[2].Sub(getFinished[0])
	if spread > 700*time.Millisecond {
		t.Fatalf("parallel GETs too slow spread=%v", spread)
	}
	if postStarted.IsZero() {
		t.Fatalf("POST call not recorded")
	}
	// POST（写）应晚于所有 GET（串行段在并行段之后执行）
	for _, ft := range getFinished {
		if postStarted.Before(ft) {
			t.Fatalf("write POST must run after all readonly GETs, postStarted=%v getFinished=%v", postStarted, ft)
		}
	}
}