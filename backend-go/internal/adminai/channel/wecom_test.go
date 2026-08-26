package channel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// mockWeComServer 模拟企微智能机器人长连接服务端。
// 关键协议行为：订阅/心跳响应的 req_id 只保持前缀、不原样回显；
// send 类命令回执原样回显 req_id。
type mockWeComServer struct {
	server *httptest.Server

	mu         sync.Mutex
	conns      []*websocket.Conn
	sendMsgs   []map[string]interface{} // 收到的 aibot_send_msg body
	responds   []map[string]interface{} // 收到的 aibot_respond_msg body
	authOK     bool
	subCount   int
	pingCount  int
}

func newMockWeComServer(authOK bool) *mockWeComServer {
	m := &mockWeComServer{authOK: authOK}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		m.mu.Lock()
		m.conns = append(m.conns, conn)
		m.mu.Unlock()
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var frame wecomFrame
			if json.Unmarshal(data, &frame) != nil {
				continue
			}
			switch frame.Cmd {
			case "aibot_subscribe":
				m.mu.Lock()
				m.subCount++
				ok := m.authOK
				m.mu.Unlock()
				// 关键：响应 req_id 与请求不同，仅保持前缀
				resp := wecomFrame{
					Headers: wecomFrameHeaders{ReqID: frame.Headers.ReqID + "_resp"},
					Errcode: map[bool]int{true: 0, false: 91001}[ok],
					Errmsg:  map[bool]string{true: "", false: "invalid secret"}[ok],
				}
				_ = conn.WriteJSON(resp)
			case "ping":
				m.mu.Lock()
				m.pingCount++
				m.mu.Unlock()
				resp := wecomFrame{
					Headers: wecomFrameHeaders{ReqID: "pong_" + fmt.Sprint(m.pingCount)},
					Errcode: 0,
				}
				_ = conn.WriteJSON(resp)
			case "aibot_send_msg":
				var body map[string]interface{}
				_ = json.Unmarshal(frame.Body, &body)
				m.mu.Lock()
				m.sendMsgs = append(m.sendMsgs, body)
				m.mu.Unlock()
				// send 回执原样回显 req_id
				resp := wecomFrame{
					Cmd:     "aibot_send_msg",
					Headers: wecomFrameHeaders{ReqID: frame.Headers.ReqID},
					Errcode: 0,
				}
				_ = conn.WriteJSON(resp)
			case "aibot_respond_msg":
				var body map[string]interface{}
				_ = json.Unmarshal(frame.Body, &body)
				m.mu.Lock()
				m.responds = append(m.responds, body)
				m.mu.Unlock()
				// respond 回执原样回显回调的 req_id
				resp := wecomFrame{
					Headers: wecomFrameHeaders{ReqID: frame.Headers.ReqID},
					Errcode: 0,
				}
				_ = conn.WriteJSON(resp)
			}
		}
	}))
	// 转 wss 不必要：gorilla Dialer 支持 ws://，测试注入 ws:// 地址即可
	return m
}

func (m *mockWeComServer) wsURL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

func (m *mockWeComServer) close() { m.server.Close() }

func (m *mockWeComServer) sentMessages() []map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]interface{}, len(m.sendMsgs))
	copy(out, m.sendMsgs)
	return out
}

func (m *mockWeComServer) sentResponds() []map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]map[string]interface{}, len(m.responds))
	copy(out, m.responds)
	return out
}

func newTestWeComChannel(cfg WeComConfig, reg *Registry) (*WeComChannel, *mockWeComServer) {
	mock := newMockWeComServer(true)
	cfg.WSURL = mock.wsURL()
	wc := NewWeComChannel("wecom", cfg, reg)
	return wc, mock
}

// TestWeComAuthPrefixMatch 认证响应 req_id 仅前缀相同时也应认证成功（回归：完全匹配导致超时）。
func TestWeComAuthPrefixMatch(t *testing.T) {
	wc, mock := newTestWeComChannel(WeComConfig{BotID: "bot", Secret: "sec"}, nil)
	defer mock.close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- wc.Start(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if wc.Status().State == StateRunning {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if st := wc.Status().State; st != StateRunning {
		t.Fatalf("频道未进入 running 状态: %s (err=%s)", st, wc.Status().Error)
	}

	mock.mu.Lock()
	subs := mock.subCount
	mock.mu.Unlock()
	if subs < 1 {
		t.Fatal("未收到订阅帧")
	}

	cancel()
	select {
	case <-errCh:
	case <-time.After(3 * time.Second):
		t.Fatal("Start 未在 Stop 后退出")
	}
}

// TestWeComAuthRejected 密钥错误时 Start 应报认证失败。
func TestWeComAuthRejected(t *testing.T) {
	mock := newMockWeComServer(false)
	defer mock.close()

	cfg := WeComConfig{BotID: "bot", Secret: "bad", WSURL: mock.wsURL()}
	wc := NewWeComChannel("wecom", cfg, nil)
	err := wc.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "企微认证失败") {
		t.Fatalf("应报认证失败，实际 %v", err)
	}
}

// TestWeComInboundAndSend 入站消息分发 + Send 发送 markdown。
func TestWeComInboundAndSend(t *testing.T) {
	reg := NewRegistry()
	received := make(chan InboundEnvelope, 1)
	reg.SetOnInbound(func(env InboundEnvelope) { received <- env })

	wc, mock := newTestWeComChannel(WeComConfig{BotID: "bot", Secret: "sec"}, reg)
	defer mock.close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() { _ = wc.Start(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && wc.Status().State != StateRunning {
		time.Sleep(50 * time.Millisecond)
	}
	if wc.Status().State != StateRunning {
		t.Fatalf("频道未运行: %+v", wc.Status())
	}

	// 模拟服务端推送一条单聊文本消息
	inboundBody := mustJSON(map[string]interface{}{
		"msgid":    "m1",
		"msgtype":  "text",
		"chattype": "single",
		"from":     map[string]interface{}{"userid": "zhangsan"},
		"text":     map[string]interface{}{"content": "你好"},
	})
	pushFrame := wecomFrame{Cmd: "aibot_msg_callback", Headers: wecomFrameHeaders{ReqID: "cb_1"}, Body: inboundBody}
	mock.mu.Lock()
	conn := mock.conns[len(mock.conns)-1]
	mock.mu.Unlock()
	_ = conn.WriteJSON(pushFrame)

	select {
	case env := <-received:
		if env.ChatID != "zhangsan" || env.UserID != "zhangsan" || env.Text != "你好" || env.ChannelID != "wecom" {
			t.Fatalf("env = %#v", env)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("未收到入站回调")
	}

	// Send 完整消息：入站后缓存了 req_id → 走被动回复通道（aibot_respond_msg）markdown
	if _, err := wc.Send(ctx, "zhangsan", OutboundMessage{Text: "回复内容"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	responds := mock.sentResponds()
	if len(responds) == 0 {
		t.Fatal("未收到 respond 帧")
	}
	last := responds[len(responds)-1]
	if last["msgtype"] != "markdown" {
		t.Fatalf("msgtype = %v", last["msgtype"])
	}
	md, _ := last["markdown"].(map[string]interface{})
	if md == nil || md["content"] != "回复内容" {
		t.Fatalf("markdown = %#v", md)
	}
}

// TestWeComStreamSendEdit 流式走被动回复通道（aibot_respond_msg）：
// 先推送入站消息缓存 req_id，Send(Stream) 与 Edit 均应经 respond 通道覆盖同一 stream.id。
func TestWeComStreamSendEdit(t *testing.T) {
	reg := NewRegistry()
	received := make(chan InboundEnvelope, 1)
	reg.SetOnInbound(func(env InboundEnvelope) { received <- env })

	wc, mock := newTestWeComChannel(WeComConfig{BotID: "bot", Secret: "sec"}, reg)
	defer mock.close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() { _ = wc.Start(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && wc.Status().State != StateRunning {
		time.Sleep(50 * time.Millisecond)
	}
	if wc.Status().State != StateRunning {
		t.Fatalf("频道未运行: %+v", wc.Status())
	}
	if !wc.SupportsEdit() {
		t.Fatal("初始应支持流式编辑")
	}

	// 推送入站消息（缓存回调 req_id）
	mock.mu.Lock()
	conn := mock.conns[len(mock.conns)-1]
	mock.mu.Unlock()
	_ = conn.WriteJSON(wecomFrame{
		Cmd:     "aibot_msg_callback",
		Headers: wecomFrameHeaders{ReqID: "callback-req-1"},
		Body: mustJSON(map[string]interface{}{
			"msgid": "m-s1", "msgtype": "text", "chattype": "single",
			"from": map[string]interface{}{"userid": "u1"}, "text": map[string]interface{}{"content": "hi"},
		}),
	})
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("未收到入站回调")
	}

	msgID, err := wc.Send(ctx, "u1", OutboundMessage{Text: "⏳ 正在处理中…", Stream: true})
	if err != nil {
		t.Fatalf("stream Send: %v", err)
	}
	if msgID == "" {
		t.Fatal("流式 Send 应返回 stream_id")
	}

	if err := wc.Edit(ctx, "u1", msgID, OutboundMessage{Text: "部分回答"}); err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if err := wc.Delete(ctx, "u1", msgID); err != nil {
		t.Fatalf("Delete(finish): %v", err)
	}

	responds := mock.sentResponds()
	if len(responds) < 3 {
		t.Fatalf("respond 帧数量 = %d, want >=3（占位+编辑+finish）", len(responds))
	}
	first := responds[0]
	if first["msgtype"] != "stream" {
		t.Fatalf("首帧 msgtype = %v", first["msgtype"])
	}
	stream, _ := first["stream"].(map[string]interface{})
	if stream == nil || stream["id"] != msgID || stream["finish"] != false {
		t.Fatalf("首帧 stream = %#v, want id=%s finish=false", stream, msgID)
	}
	final := responds[len(responds)-1]
	fstream, _ := final["stream"].(map[string]interface{})
	if fstream == nil || fstream["id"] != msgID || fstream["finish"] != true {
		t.Fatalf("末帧 stream = %#v, want finish=true", fstream)
	}
	// 流式不应走主动发送通道（官方 send_msg 不支持 stream）
	for _, m := range mock.sentMessages() {
		if m["msgtype"] == "stream" {
			t.Fatalf("stream 不应通过 aibot_send_msg 发送: %#v", m)
		}
	}
}

// TestWeComNoDoubleFinish 已定稿的流绝不二次 finish（重推会被企微当作新消息，导致上一条回复整段重复）。
func TestWeComNoDoubleFinish(t *testing.T) {
	reg := NewRegistry()
	received := make(chan InboundEnvelope, 1)
	reg.SetOnInbound(func(env InboundEnvelope) { received <- env })

	wc, mock := newTestWeComChannel(WeComConfig{BotID: "bot", Secret: "sec"}, reg)
	defer mock.close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() { _ = wc.Start(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && wc.Status().State != StateRunning {
		time.Sleep(50 * time.Millisecond)
	}

	mock.mu.Lock()
	conn := mock.conns[len(mock.conns)-1]
	mock.mu.Unlock()
	_ = conn.WriteJSON(wecomFrame{
		Cmd:     "aibot_msg_callback",
		Headers: wecomFrameHeaders{ReqID: "cb-df"},
		Body: mustJSON(map[string]interface{}{
			"msgid": "m-df", "msgtype": "text", "chattype": "single",
			"from": map[string]interface{}{"userid": "u2"}, "text": map[string]interface{}{"content": "hi"},
		}),
	})
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("未收到入站回调")
	}

	msgID, err := wc.Send(ctx, "u2", OutboundMessage{Text: "回答内容", Stream: true})
	if err != nil || msgID == "" {
		t.Fatalf("stream Send: %v / %q", err, msgID)
	}

	// 第一次 Delete：finish=true；后续多次 finishStream/Delete 不应再发任何 finish 帧
	if err := wc.Delete(ctx, "u2", msgID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_ = wc.Delete(ctx, "u2", msgID)
	wc.finishStream("u2")
	wc.finishStream("u2")

	finishCount := 0
	for _, r := range mock.sentResponds() {
		if s, ok := r["stream"].(map[string]interface{}); ok && s["finish"] == true {
			finishCount++
			if s["id"] != msgID {
				t.Fatalf("finish 帧 id 不匹配: %#v", s)
			}
		}
	}
	if finishCount != 1 {
		t.Fatalf("finish=true 帧数量 = %d, want 1（重复 finish 会重建上一条消息）", finishCount)
	}
}

// TestWeComDedup 同一 msgid 的重复推送只入站一次。
func TestWeComDedup(t *testing.T) {
	reg := NewRegistry()
	count := make(chan InboundEnvelope, 8)
	reg.SetOnInbound(func(env InboundEnvelope) { count <- env })

	wc, mock := newTestWeComChannel(WeComConfig{BotID: "bot", Secret: "sec"}, reg)
	defer mock.close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go func() { _ = wc.Start(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && wc.Status().State != StateRunning {
		time.Sleep(50 * time.Millisecond)
	}

	mock.mu.Lock()
	conn := mock.conns[len(mock.conns)-1]
	mock.mu.Unlock()
	frame := wecomFrame{Cmd: "aibot_msg_callback", Headers: wecomFrameHeaders{ReqID: "cb_x"}, Body: mustJSON(map[string]interface{}{
		"msgid": "dup-1", "msgtype": "text", "chattype": "single",
		"from": map[string]interface{}{"userid": "u"}, "text": map[string]interface{}{"content": "hi"},
	})}
	_ = conn.WriteJSON(frame)
	_ = conn.WriteJSON(frame)

	select {
	case <-count:
	case <-time.After(3 * time.Second):
		t.Fatal("未收到首次入站")
	}
	select {
	case extra := <-count:
		t.Fatalf("重复消息不应入站: %#v", extra)
	case <-time.After(500 * time.Millisecond):
	}
}
