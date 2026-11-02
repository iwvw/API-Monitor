package workbuddy

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// capturedUpstream 记录上游实际收到的 Content-Encoding 与（解压后的）请求体。
type capturedUpstream struct {
	mu       sync.Mutex
	encoding string
	body     []byte
	calls    int
}

func (c *capturedUpstream) snapshot() (string, []byte, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoding, append([]byte(nil), c.body...), c.calls
}

// newCapturingUpstream 起一个假上游：按 Content-Encoding 解压请求体后记录下来，
// 并回一段最小合法 SSE，让中继能走完整条链路。
func newCapturingUpstream(t *testing.T) *capturedUpstream {
	t.Helper()
	cap := &capturedUpstream{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reader io.Reader = r.Body
		enc := r.Header.Get("Content-Encoding")
		if enc == "gzip" {
			zr, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, "bad gzip: "+err.Error(), http.StatusBadRequest)
				return
			}
			defer zr.Close()
			reader = zr
		}
		body, err := io.ReadAll(reader)
		if err != nil {
			http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
			return
		}
		cap.mu.Lock()
		cap.encoding = enc
		cap.body = body
		cap.calls++
		cap.mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, okSSE)
	}))
	t.Cleanup(srv.Close)
	old := upstreamBaseOverride
	upstreamBaseOverride = srv.URL
	t.Cleanup(func() { upstreamBaseOverride = old })
	return cap
}

// 端到端：中继必须真的把请求体 gzip 压缩后发给上游，且上游解压后内容与
// 客户端原始消息一致（不丢字段、不改内容）。这是「省流量但不改变语义」的核心断言。
func TestRelaySendsGzipAndUpstreamSeesFullContent(t *testing.T) {
	cap := newCapturingUpstream(t)
	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:  true,
		Accounts: []Account{validAccount("u1", "t1")},
	}); err != nil {
		t.Fatal(err)
	}

	// 长上下文：超过 4KB 阈值才会触发压缩。
	longText := strings.Repeat("请逐行解释下面这段代码的作用与边界条件。\n", 200)
	payload, _ := json.Marshal(map[string]any{
		"model":    "hy3",
		"stream":   true,
		"messages": []map[string]string{{"role": "user", "content": longText}},
	})
	rec := serveChat(t, s, string(payload))
	if rec.Code != http.StatusOK {
		t.Fatalf("应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}

	enc, body, calls := cap.snapshot()
	if calls != 1 {
		t.Fatalf("上游应收到 1 次请求，实际 %d", calls)
	}
	if enc != "gzip" {
		t.Fatalf("长请求体应带 Content-Encoding: gzip，实际 %q", enc)
	}
	// 注意：上游解压后的 body 不等于客户端原文——中继会强制 stream:true 并补
	// stream_options，体积略增。这里只断言内容字段完整，不比长度。
	var got struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("上游解压后不是合法 JSON: %v", err)
	}
	if got.Model != "hy3" {
		t.Fatalf("模型名应原样到达上游，得到 %q", got.Model)
	}
	// 中继会给「首条非 system」的请求补一条占位 system（上游硬性要求），
	// 因此这里按 user 消息定位原文，而不是假设只有一条。
	userText := ""
	for _, m := range got.Messages {
		if m.Role == "user" {
			userText = m.Content
			break
		}
	}
	if userText != longText {
		t.Fatalf("消息内容在上游侧不一致（长度 %d vs 原文 %d）", len(userText), len(longText))
	}
}

// 开关关闭时必须发明文，且不带 Content-Encoding —— 保证可就地回退。
func TestRelaySendsPlainBodyWhenCompressionDisabled(t *testing.T) {
	cap := newCapturingUpstream(t)
	s := newTestService(t)
	off := false
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:          true,
		CompressUpstream: &off,
		Accounts:         []Account{validAccount("u1", "t1")},
	}); err != nil {
		t.Fatal(err)
	}

	longText := strings.Repeat("请逐行解释下面这段代码的作用与边界条件。\n", 200)
	payload, _ := json.Marshal(map[string]any{
		"model":    "hy3",
		"stream":   true,
		"messages": []map[string]string{{"role": "user", "content": longText}},
	})
	rec := serveChat(t, s, string(payload))
	if rec.Code != http.StatusOK {
		t.Fatalf("应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}

	enc, body, _ := cap.snapshot()
	if enc != "" {
		t.Fatalf("关闭压缩后不应带 Content-Encoding，实际 %q", enc)
	}
	if string(body) == "" {
		t.Fatal("上游未收到请求体")
	}
	var got struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("上游收到非 JSON: %v", err)
	}
	// 同 TestRelaySendsGzip...：中继会补占位 system，按 user 消息定位原文。
	userText := ""
	for _, m := range got.Messages {
		if m.Role == "user" {
			userText = m.Content
			break
		}
	}
	if userText != longText {
		t.Fatal("关闭压缩后内容仍应完整到达上游")
	}
}

// 小请求不压缩：避免为几百字节多付 gzip 开销。
func TestRelaySkipsCompressionForSmallBody(t *testing.T) {
	cap := newCapturingUpstream(t)
	s := newTestService(t)
	if err := s.SaveSettings(context.Background(), Settings{
		Enabled:  true,
		Accounts: []Account{validAccount("u1", "t1")},
	}); err != nil {
		t.Fatal(err)
	}

	rec := serveChat(t, s, `{"model":"hy3","messages":[{"role":"user","content":"hi"}],"stream":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	enc, _, _ := cap.snapshot()
	if enc != "" {
		t.Fatalf("小请求不应压缩，实际 Content-Encoding=%q", enc)
	}
}
