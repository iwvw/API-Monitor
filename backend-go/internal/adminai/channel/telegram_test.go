package channel

import (
	"context"
	"strings"
	"testing"
)

func TestEscapeV2(t *testing.T) {
	specials := `\ _ * [ ] ( ) ~ ` + "`" + ` > # + - = | { } . !`
	escaped := EscapeV2(specials)
	if escaped != `\\ \_ \* \[ \] \( \) \~ \`+"`"+` \> \# \+ \- \= \| \{ \} \. \!` {
		t.Fatalf("EscapeV2(%q) = %q", specials, escaped)
	}

	if got := EscapeV2("plain text 123"); got != "plain text 123" {
		t.Fatalf("EscapeV2 plain = %q", got)
	}
	if got := EscapeV2("你好 world"); got != "你好 world" {
		t.Fatalf("EscapeV2 unicode = %q", got)
	}
	if got := EscapeV2("a_b"); got != `a\_b` {
		t.Fatalf("EscapeV2 underscore = %q", got)
	}
	if got := EscapeV2(""); got != "" {
		t.Fatalf("EscapeV2 empty = %q", got)
	}
	if EscapeBold("a*b") != `a\*b` {
		t.Fatalf("EscapeBold = %q", EscapeBold("a*b"))
	}
}

func TestEscapeCode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello", "hello"},
		{"`tick`", "\\`tick\\`"},
		{`a\b`, `a\\b`},
		{"mix `and\\", "mix \\`and\\\\"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := EscapeCode(tc.in); got != tc.want {
			t.Fatalf("EscapeCode(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestEscapeLinkURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://a.com/x", "https://a.com/x"},
		{"https://a.com/x)", "https://a.com/x\\)"},
		{`https://a.com/x\y`, `https://a.com/x\\y`},
		{"a(b)", "a(b\\)"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := EscapeLinkURL(tc.in); got != tc.want {
			t.Fatalf("EscapeLinkURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestChunkText(t *testing.T) {
	defaultChannel := NewTelegramChannel("test", TelegramConfig{}, nil)
	if defaultChannel.cfg.TextChunkLimit != 4000 {
		t.Fatalf("default chunk limit = %d, want 4000", defaultChannel.cfg.TextChunkLimit)
	}

	t.Run("default limit single chunk", func(t *testing.T) {
		chunks := defaultChannel.chunkText("short")
		if len(chunks) != 1 || chunks[0] != "short" {
			t.Fatalf("chunks = %#v", chunks)
		}
	})

	channel := NewTelegramChannel("test", TelegramConfig{TextChunkLimit: 4}, nil)

	t.Run("hard cut without newline", func(t *testing.T) {
		chunks := channel.chunkText("abcdefghij")
		want := []string{"abcd", "efgh", "ij"}
		if len(chunks) != len(want) {
			t.Fatalf("chunks = %#v, want %#v", chunks, want)
		}
		for i := range want {
			if chunks[i] != want[i] {
				t.Fatalf("chunks[%d] = %q, want %q", i, chunks[i], want[i])
			}
		}
	})

	t.Run("cut at newline near boundary", func(t *testing.T) {
		chunks := channel.chunkText("xyz\nabcdefgh")
		want := []string{"xyz\n", "abcd", "efgh"}
		if len(chunks) != len(want) {
			t.Fatalf("chunks = %#v, want %#v", chunks, want)
		}
		for i := range want {
			if chunks[i] != want[i] {
				t.Fatalf("chunks[%d] = %q, want %q", i, chunks[i], want[i])
			}
		}
	})

	t.Run("newline before midpoint falls back to hard cut", func(t *testing.T) {
		chunks := channel.chunkText("a\nbcdefghijkl")
		want := []string{"a\nbc", "defg", "hijk", "l"}
		if len(chunks) != len(want) {
			t.Fatalf("chunks = %#v, want %#v", chunks, want)
		}
		for i := range want {
			if chunks[i] != want[i] {
				t.Fatalf("chunks[%d] = %q, want %q", i, chunks[i], want[i])
			}
		}
	})

	t.Run("exact boundary", func(t *testing.T) {
		chunks := channel.chunkText("abcd")
		if len(chunks) != 1 || chunks[0] != "abcd" {
			t.Fatalf("chunks = %#v", chunks)
		}
	})

	t.Run("empty text", func(t *testing.T) {
		chunks := channel.chunkText("")
		if len(chunks) != 1 || chunks[0] != "" {
			t.Fatalf("chunks = %#v", chunks)
		}
	})
}

func TestRenderBlocks(t *testing.T) {
	channel := NewTelegramChannel("test", TelegramConfig{}, nil)
	out := channel.renderBlocks([]OutboundBlock{
		{Type: "code", Code: "x`y"},
		{Type: "error", Title: "出错", Text: "a_b"},
		{Type: "text", Text: "普通文本"},
	}, "开场白")

	for _, want := range []string{"开场白", "```\nx\\`y\n```", "⚠️ *出错*: a\\_b", "普通文本"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderBlocks output missing %q: %q", want, out)
		}
	}
}

func TestNormalizeRichMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"single line", "hello", "hello"},
		{"adjacent plain lines", "line1\nline2", "line1  \nline2"},
		{"blank line separator", "para1\n\npara2", "para1\n\npara2"},
		{"heading keeps structure", "# head\nline", "# head\nline"},
		{"list keeps structure", "- item\n- item2", "- item\n- item2"},
		{"code fence untouched", "```\ncode\n```", "```\ncode\n```"},
		{"quote untouched", "> quote\n> more", "> quote\n> more"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeRichMarkdown(tc.in); got != tc.want {
				t.Fatalf("NormalizeRichMarkdown(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEnvelopeFromMessage(t *testing.T) {
	channel := NewTelegramChannel("telegram", TelegramConfig{}, nil)
	channel.botName = "monitor_bot"

	env := channel.envelopeFromMessage(map[string]interface{}{
		"message_id": float64(42),
		"chat":       map[string]interface{}{"id": float64(-100123), "type": "supergroup"},
		"from":       map[string]interface{}{"id": float64(12345), "username": "alice"},
		"text":       "hello @monitor_bot world  ",
	})
	if env.ChannelID != "telegram" || env.ChatID != "-100123" || env.ChatType != "group" {
		t.Fatalf("env = %#v", env)
	}
	if env.UserID != "12345" || env.Username != "alice" || !env.IsMention || env.Text != "hello  world" {
		t.Fatalf("env = %#v", env)
	}
	if env.MessageID != 42 || len(env.Raw) == 0 {
		t.Fatalf("env = %#v", env)
	}

	privateEnv := channel.envelopeFromMessage(map[string]interface{}{
		"chat": map[string]interface{}{"id": float64(777), "type": "private"},
		"from": map[string]interface{}{"id": float64(777)},
		"text": "hi",
	})
	if privateEnv.ChatType != "private" || privateEnv.IsMention {
		t.Fatalf("private env = %#v", privateEnv)
	}

	emptyEnv := channel.envelopeFromMessage(map[string]interface{}{})
	if emptyEnv.Text != "" {
		t.Fatalf("empty env = %#v", emptyEnv)
	}
}

// Stop 幂等：重复调用不得二次 close 同一通道（修复前第二次 Stop panic）。
func TestTelegramStopIdempotent(t *testing.T) {
	tg := NewTelegramChannel("telegram", TelegramConfig{BotToken: "x"}, nil)
	// Start 需要真实 Bot API，直接模拟 Start 已创建 stop 通道的运行态
	tg.mu.Lock()
	tg.stop = make(chan struct{})
	tg.state = StateRunning
	tg.mu.Unlock()

	if err := tg.Stop(context.Background()); err != nil {
		t.Fatalf("首次 Stop: %v", err)
	}
	if err := tg.Stop(context.Background()); err != nil {
		t.Fatalf("二次 Stop: %v", err)
	}
	if tg.Status().State != StateStopped {
		t.Fatalf("Stop 后状态应为 stopped，实际 %s", tg.Status().State)
	}

	// 未 Start 过（stop 为 nil）的实例：Stop 同样安全
	fresh := NewTelegramChannel("telegram2", TelegramConfig{BotToken: "x"}, nil)
	if err := fresh.Stop(context.Background()); err != nil {
		t.Fatalf("未启动实例 Stop: %v", err)
	}
	if err := fresh.Stop(context.Background()); err != nil {
		t.Fatalf("未启动实例二次 Stop: %v", err)
	}
}
