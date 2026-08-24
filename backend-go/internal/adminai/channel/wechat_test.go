package channel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mockILinkServer 模拟 iLink Bot API。
type mockILinkServer struct {
	server      *httptest.Server
	sendCount   atomic.Int32
	sendPayload chan map[string]interface{}
	updates     []interface{}
	typingCount atomic.Int32
}

func newMockILinkServer() *mockILinkServer {
	m := &mockILinkServer{
		sendPayload: make(chan map[string]interface{}, 10),
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ilink/bot/getupdates":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ret":             0,
				"msgs":            m.updates,
				"get_updates_buf": "cur-1",
			})
		case "/ilink/bot/sendmessage":
			m.sendCount.Add(1)
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			m.sendPayload <- body
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ret": 0, "errcode": 0})
		case "/ilink/bot/getconfig":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"ret":           0,
				"typing_ticket": "ticket-1",
			})
		case "/ilink/bot/sendtyping":
			m.typingCount.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"ret": 0, "errcode": 0})
		case "/ilink/bot/get_bot_qrcode":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"qrcode":            "qr-1",
				"qrcode_img_content": base64.StdEncoding.EncodeToString([]byte("png")),
				"url":               "https://weixin.qq.com/qr/qr-1",
			})
		case "/ilink/bot/get_qrcode_status":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    "confirmed",
				"bot_token": "bot-token-1",
				"baseurl":   "https://ilinkai.weixin.qq.com",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	return m
}

func (m *mockILinkServer) Close() {
	m.server.Close()
}

func newTestWeChatChannel(cfg WeChatConfig, reg *Registry) (*WeChatChannel, *mockILinkServer) {
	mock := newMockILinkServer()
	wc := NewWeChatChannel("wechat", cfg, reg)
	wc.SetBaseURL(mock.server.URL)
	return wc, mock
}

func TestWeChatSupportsEditFalse(t *testing.T) {
	wc, mock := newTestWeChatChannel(WeChatConfig{BotToken: "t"}, nil)
	defer mock.Close()
	if wc.SupportsEdit() {
		t.Fatal("wechat 应不支持编辑")
	}
}

func TestWeChatEditDeleteNoop(t *testing.T) {
	wc, mock := newTestWeChatChannel(WeChatConfig{BotToken: "t"}, nil)
	defer mock.Close()
	if err := wc.Edit(context.Background(), "u", "1", OutboundMessage{Text: "x"}); err != nil {
		t.Fatalf("Edit 应为 no-op，实际 %v", err)
	}
	if err := wc.Delete(context.Background(), "u", "1"); err != nil {
		t.Fatalf("Delete 应为 no-op，实际 %v", err)
	}
}

func TestWeChatChunkText(t *testing.T) {
	chunks := chunkText("abcdefghij", 4)
	if len(chunks) != 3 || chunks[0] != "abcd" || chunks[2] != "ij" {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestWeChatSendText(t *testing.T) {
	reg := NewRegistry()
	wc, mock := newTestWeChatChannel(WeChatConfig{BotToken: "t"}, reg)
	defer mock.Close()

	wc.ctxTokens.Store("wxid_abc@im.wechat", "ctx-1")

	_, err := wc.Send(context.Background(), "wxid_abc@im.wechat", OutboundMessage{Text: "你好"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case payload := <-mock.sendPayload:
		msg, _ := payload["msg"].(map[string]interface{})
		if msg == nil {
			t.Fatalf("payload 缺少 msg: %#v", payload)
		}
		if msg["to_user_id"] != "wxid_abc@im.wechat" {
			t.Fatalf("to_user_id = %v", msg["to_user_id"])
		}
		if msg["context_token"] != "ctx-1" {
			t.Fatalf("context_token = %v", msg["context_token"])
		}
		items, _ := msg["item_list"].([]interface{})
		if len(items) == 0 {
			t.Fatalf("item_list 为空")
		}
		item, _ := items[0].(map[string]interface{})
		textItem, _ := item["text_item"].(map[string]interface{})
		if textItem["text"] != "你好" {
			t.Fatalf("text = %v", textItem["text"])
		}
	default:
		t.Fatal("未收到 sendmessage 调用")
	}
}

func TestWeChatSendNoContextToken(t *testing.T) {
	reg := NewRegistry()
	wc, mock := newTestWeChatChannel(WeChatConfig{BotToken: "t"}, reg)
	defer mock.Close()

	// 无 context_token 缓存
	_, err := wc.Send(context.Background(), "unknown@im.wechat", OutboundMessage{Text: "hi"})
	if err == nil {
		t.Fatal("缺少 context_token 时应返回错误")
	}
}

func TestWeChatHandleMessageText(t *testing.T) {
	reg := NewRegistry()
	wc, mock := newTestWeChatChannel(WeChatConfig{BotToken: "t"}, reg)
	defer mock.Close()

	received := make(chan InboundEnvelope, 1)
	reg.SetOnInbound(func(env InboundEnvelope) {
		received <- env
	})

	wc.handleMessage(map[string]interface{}{
		"message_type":  float64(1),
		"from_user_id":  "wxid_abc@im.wechat",
		"context_token": "ctx-1",
		"item_list": []interface{}{
			map[string]interface{}{
				"type":      float64(1),
				"text_item": map[string]interface{}{"text": "帮我查一下"},
			},
		},
	})

	select {
	case env := <-received:
		if env.ChannelID != "wechat" || env.Text != "帮我查一下" || env.ChatID != "wxid_abc@im.wechat" {
			t.Fatalf("env = %#v", env)
		}
		if env.ChatType != "dm" {
			t.Fatalf("chatType = %q", env.ChatType)
		}
	default:
		t.Fatal("未收到入站回调")
	}

	// context_token 应被缓存
	token, ok := wc.ctxTokens.Load("wxid_abc@im.wechat")
	if !ok || token.(string) != "ctx-1" {
		t.Fatalf("context_token 未缓存: %v/%v", token, ok)
	}
}

func TestWeChatHandleMessageIgnoreBotType(t *testing.T) {
	reg := NewRegistry()
	wc, mock := newTestWeChatChannel(WeChatConfig{BotToken: "t"}, reg)
	defer mock.Close()

	called := false
	reg.SetOnInbound(func(InboundEnvelope) { called = true })

	// message_type != 1（Bot→用户）应忽略
	wc.handleMessage(map[string]interface{}{
		"message_type": float64(2),
		"from_user_id": "wxid_abc@im.wechat",
		"item_list": []interface{}{
			map[string]interface{}{"type": float64(1), "text_item": map[string]interface{}{"text": "hi"}},
		},
	})
	if called {
		t.Fatal("message_type=2 不应触发入站回调")
	}
}

func TestWeChatHandleMessageUnauthorized(t *testing.T) {
	reg := NewRegistry()
	wc, mock := newTestWeChatChannel(WeChatConfig{BotToken: "t"}, reg)
	defer mock.Close()

	called := false
	reg.SetOnInbound(func(InboundEnvelope) { called = true })
	wc.SetAuthorize(func(userID, username, chatType string) bool {
		return false
	})

	wc.handleMessage(map[string]interface{}{
		"message_type":  float64(1),
		"from_user_id":  "wxid_blocked@im.wechat",
		"context_token": "ctx-1",
		"item_list": []interface{}{
			map[string]interface{}{"type": float64(1), "text_item": map[string]interface{}{"text": "hi"}},
		},
	})
	if called {
		t.Fatal("未授权用户不应触发入站回调")
	}
}

func TestWeChatStartRequiresToken(t *testing.T) {
	reg := NewRegistry()
	wc, mock := newTestWeChatChannel(WeChatConfig{}, reg) // 无 bot_token
	defer mock.Close()

	err := wc.Start(context.Background())
	if err == nil {
		t.Fatal("无 bot_token 时 Start 应报错")
	}
	if !strings.Contains(err.Error(), "bot_token") {
		t.Fatalf("错误信息 = %q", err.Error())
	}
}

func TestWeChatStopIdempotent(t *testing.T) {
	wc, mock := newTestWeChatChannel(WeChatConfig{BotToken: "t"}, nil)
	defer mock.Close()

	wc.mu.Lock()
	wc.stop = make(chan struct{})
	wc.state = StateRunning
	wc.mu.Unlock()

	if err := wc.Stop(context.Background()); err != nil {
		t.Fatalf("首次 Stop: %v", err)
	}
	if err := wc.Stop(context.Background()); err != nil {
		t.Fatalf("二次 Stop: %v", err)
	}
	if wc.Status().State != StateStopped {
		t.Fatalf("状态应为 stopped，实际 %s", wc.Status().State)
	}
}

func TestILinkClientQRCode(t *testing.T) {
	mock := newMockILinkServer()
	defer mock.Close()

	client := NewILinkClient("", mock.server.URL)
	qrData, err := client.GetBotQRCode(context.Background())
	if err != nil {
		t.Fatalf("GetBotQRCode: %v", err)
	}
	if qrData["qrcode"] != "qr-1" {
		t.Fatalf("qrcode = %v", qrData["qrcode"])
	}
	if qrData["qrcode_img_content"] == "" {
		t.Fatal("qrcode_img_content 为空")
	}

	statusData, err := client.GetQRCodeStatus(context.Background(), "qr-1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	if statusData["status"] != "confirmed" || statusData["bot_token"] != "bot-token-1" {
		t.Fatalf("status = %#v", statusData)
	}

	// WaitForLogin 应立即返回 confirmed
	token, baseURL, err := client.WaitForLogin(context.Background(), "qr-1", 10*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitForLogin: %v", err)
	}
	if token != "bot-token-1" {
		t.Fatalf("token = %q", token)
	}
	if baseURL == "" {
		t.Fatal("baseURL 为空")
	}
}
