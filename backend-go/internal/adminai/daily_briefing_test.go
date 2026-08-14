package adminai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/adminai/channel"
	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

// TestDailyBriefingRouteRegistered 验证 /cron/daily-briefing 已接入 ServeHTTP 分发
// （带 X-Internal-Cron 头、aiCaller 已配置但未配模型时返回模型配置错误而非 404）。
func TestDailyBriefingRouteRegistered(t *testing.T) {
	s := newTestService(t)
	s.aiCaller = func(ctx context.Context, call systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
		return systemmetrics.AICallResponse{StatusCode: http.StatusOK, Body: map[string]interface{}{"data": map[string]interface{}{"ok": true}}}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin-ai/cron/daily-briefing", nil)
	req.Header.Set("X-Internal-Cron", "true")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("daily-briefing 路由未注册: %s", rec.Body.String())
	}
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "简报模型") {
		t.Fatalf("期望模型配置错误(500)，实际 %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDailyBriefingRejectsNonCron 验证缺 X-Internal-Cron 头的外部会话请求一律 403。
func TestDailyBriefingRejectsNonCron(t *testing.T) {
	s := newTestService(t)
	s.aiCaller = func(ctx context.Context, call systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
		return systemmetrics.AICallResponse{StatusCode: http.StatusOK, Body: map[string]interface{}{"data": map[string]interface{}{"ok": true}}}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin-ai/cron/daily-briefing", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("缺 X-Internal-Cron 头期望 403，实际 %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDailyBriefingTargetsDedup 验证简报目标解析：仅收已启用 telegram 频道的绑定用户，且去重。
func TestDailyBriefingTargetsDedup(t *testing.T) {
	s := newTestService(t)
	s.SetupChannels()

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO admin_ai_channels (id, type, name, enabled, config_encrypted, created_at, updated_at) VALUES ('aac_t', 'telegram', '测试频道', 1, 'x', ?, ?)`,
		now, now)
	for _, uid := range []string{"111", "222", "111"} {
		_, _ = db.ExecContext(context.Background(),
			`INSERT OR IGNORE INTO admin_ai_channel_bindings (id, channel_id, channel_user_id, role, created_at) VALUES (?, 'aac_t', ?, 'admin', ?)`,
			randomIDOrPanic(), uid, now)
	}
	db.Close()

	targets, err := s.briefingTargets(context.Background(), "")
	if err != nil {
		t.Fatalf("briefingTargets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("期望去重后 2 个目标，实际 %d: %v", len(targets), targets)
	}

	// 指定 chatId 时单发
	one, err := s.briefingTargets(context.Background(), "999")
	if err != nil {
		t.Fatalf("briefingTargets single: %v", err)
	}
	if len(one) != 1 || one[0] != "999" {
		t.Fatalf("单发目标异常: %v", one)
	}
}

// TestDailyBriefingModelFallback 验证简报模型解析链：briefing 专用键 → 默认模型键 → 环境默认。
func TestDailyBriefingModelFallback(t *testing.T) {
	s := newTestService(t)
	if got := s.getBriefingModel(context.Background()); got != "" {
		t.Fatalf("无配置时期望空，实际 %q", got)
	}

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, _ = db.ExecContext(context.Background(),
		`INSERT INTO system_config (key, value, description, updated_at) VALUES ('admin_ai_briefing_model', 'brief-model', '简报模型', ?)`,
		now)
	db.Close()

	if got := s.getBriefingModel(context.Background()); got != "brief-model" {
		t.Fatalf("期望 brief-model，实际 %q", got)
	}
}

func randomIDOrPanic() string {
	id, err := randomID("aab_")
	if err != nil {
		panic(err)
	}
	return id
}

// TestChannelCommandHelp 验证斜杠命令 /help 被本地处理（返回面板文本，不进入 RunLoop）。
func TestChannelCommandHelp(t *testing.T) {
	s := newTestService(t)
	s.SetupChannels()
	s.aiCaller = func(ctx context.Context, call systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
		return systemmetrics.AICallResponse{StatusCode: http.StatusOK, Body: map[string]interface{}{"data": map[string]interface{}{"ok": true}}}, nil
	}

	env := channel.InboundEnvelope{ChannelID: "aac_t", ChatID: "123", Text: "/帮助"}
	handled, reply := s.handleChannelCommand(env)
	if !handled {
		t.Fatal("/帮助 应被本地命令处理")
	}
	if !strings.Contains(reply, "API Monitor 管理助手") {
		t.Fatalf("/帮助 应返回命令面板，实际: %s", reply)
	}
}

// TestChannelCommandUnknownPassesThrough 验证未知斜杠命令不拦截（交给 AI 对话）。
func TestChannelCommandUnknownPassesThrough(t *testing.T) {
	s := newTestService(t)
	env := channel.InboundEnvelope{ChannelID: "aac_t", ChatID: "123", Text: "/xyz"}
	handled, _ := s.handleChannelCommand(env)
	if handled {
		t.Fatal("/xyz 不应被命令处理，应交给 AI 对话")
	}
}
