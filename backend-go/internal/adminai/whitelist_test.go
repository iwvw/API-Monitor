package adminai

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/adminai/channel"
)

// TestAuthorizeByWhitelist 验证白名单判定语义：
// 无白名单条目 = 开放（任何人可对话）；有条目后仅命中者放行。
func TestAuthorizeByWhitelist(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	seed := func(channelID string) {
		_, _ = db.ExecContext(context.Background(),
			`INSERT INTO admin_ai_channels (id, type, name, enabled, config_encrypted, created_at, updated_at) VALUES (?, 'telegram', '测试', 1, 'x', ?, ?)`,
			channelID, now, now)
	}

	// 1) 空白名单 = 开放
	seed("aac_open")
	if !authorizeByWhitelist(db, "aac_open", "anyone") {
		t.Fatal("空白名单应开放所有用户")
	}

	// 2) 有白名单后仅命中者放行
	seed("aac_restricted")
	for _, uid := range []string{"111", "222"} {
		_, _ = db.ExecContext(context.Background(),
			`INSERT INTO admin_ai_channel_bindings (id, channel_id, channel_user_id, role, created_at) VALUES (?, 'aac_restricted', ?, 'admin', ?)`,
			randomIDOrPanic(), uid, now)
	}
	if authorizeByWhitelist(db, "aac_restricted", "999") {
		t.Fatal("白名单外的用户应被拒绝")
	}
	if !authorizeByWhitelist(db, "aac_restricted", "111") {
		t.Fatal("白名单内的用户应放行")
	}
	if !authorizeByWhitelist(db, "aac_restricted", "222") {
		t.Fatal("白名单内的第二个用户应放行")
	}

	// 3) 不同频道互不影响：open 频道仍开放
	if !authorizeByWhitelist(db, "aac_open", "999") {
		t.Fatal("开放频道的判定不应受其他频道白名单影响")
	}
}

// TestSourceChannelInUse 验证同源频道去重：同一通知渠道只能被一个 AI 频道引用。
func TestSourceChannelInUse(t *testing.T) {
	s := newTestService(t)
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_channels (id, type, name, enabled, config_encrypted, notification_channel_id, created_at, updated_at) VALUES ('aac_a', 'telegram', 'A', 1, 'x', 'notif_x', ?, ?)`,
		now, now)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_channels (id, type, name, enabled, config_encrypted, notification_channel_id, created_at, updated_at) VALUES ('aac_b', 'telegram', 'B', 1, 'x', '', ?, ?)`,
		now, now)
	db.Close()

	inUse, err := s.sourceChannelInUse(context.Background(), "notif_x", "aac_b")
	if err != nil {
		t.Fatalf("sourceChannelInUse: %v", err)
	}
	if !inUse {
		t.Fatal("notif_x 已被 aac_a 引用，应判定 inUse")
	}
	// 排除自身：更新 aac_a 时不应因自己而拦截
	inUse, err = s.sourceChannelInUse(context.Background(), "notif_x", "aac_a")
	if err != nil {
		t.Fatalf("sourceChannelInUse self: %v", err)
	}
	if inUse {
		t.Fatal("排除自身后 notif_x 不应判定 inUse")
	}
	// 未使用的来源
	inUse, err = s.sourceChannelInUse(context.Background(), "notif_y", "aac_a")
	if err != nil {
		t.Fatalf("sourceChannelInUse free: %v", err)
	}
	if inUse {
		t.Fatal("notif_y 未被引用，不应判定 inUse")
	}
}

// TestResolveBotToken 验证 token 解析：无来源时回退旧配置 token。
func TestResolveBotToken(t *testing.T) {
	s := newTestService(t)

	token, err := s.resolveBotToken(context.Background(), "", channel.TelegramConfig{BotToken: "legacy-token"})
	if err != nil || token != "legacy-token" {
		t.Fatalf("legacy 回退期望 legacy-token，实际 %q err=%v", token, err)
	}
	if _, err := s.resolveBotToken(context.Background(), "", channel.TelegramConfig{}); err == nil || !strings.Contains(err.Error(), "旧 Token") {
		t.Fatalf("无来源且无旧 token 应报错，实际 err=%v", err)
	}
}
