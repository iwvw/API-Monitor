package serveragent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRemoteDesktopManagerLongPollReceivesAgentSignal(t *testing.T) {
	manager := newRemoteDesktopManager()
	session := manager.create("windows-1", struct{}{})
	result := make(chan []remoteDesktopSignal, 1)
	go func() {
		signals, _, ok := manager.waitSignals(session.ID, 0, time.Second)
		if !ok {
			result <- nil
			return
		}
		result <- signals
	}()

	time.Sleep(20 * time.Millisecond)
	if !manager.appendAgentSignal(session.ID, "windows-1", json.RawMessage(`{"kind":"answer"}`)) {
		t.Fatal("agent signal should be accepted")
	}
	select {
	case signals := <-result:
		if len(signals) != 1 || signals[0].ID != 1 {
			t.Fatalf("unexpected signals: %#v", signals)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long poll was not notified")
	}
}

func TestCreateRemoteDesktopSessionRequiresWindowsCapabilityAndForwardsOffer(t *testing.T) {
	registry := NewConnectionRegistry()
	t.Cleanup(registry.Stop)
	engineSession := &EngineIOSession{}
	connection := registry.Register("windows-1", engineSession)
	connection.SetMetadata("platform", "windows")
	connection.UpdateCapabilities(map[string]bool{"remote_desktop_v1": true})
	service := &Service{registry: registry, remoteDesktop: newRemoteDesktopManager()}

	req := httptest.NewRequest(http.MethodPost, "/api/server/remote-desktop/sessions", strings.NewReader(`{
		"serverId":"windows-1",
		"offer":{"type":"offer","sdp":"test-sdp"}
	}`))
	res := httptest.NewRecorder()
	service.createRemoteDesktopSession(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	engineSession.mu.RLock()
	pending := append([]string(nil), engineSession.PendingMessages...)
	engineSession.mu.RUnlock()
	if len(pending) != 1 || !strings.Contains(pending[0], `dashboard:rd_start`) || !strings.Contains(pending[0], `test-sdp`) {
		t.Fatalf("offer was not forwarded to Agent: %#v", pending)
	}

	connection.UpdateCapabilities(map[string]bool{})
	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/server/remote-desktop/sessions", strings.NewReader(`{"serverId":"windows-1","offer":{"type":"offer","sdp":"test"}}`))
	service.createRemoteDesktopSession(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("Agent without capability status = %d, want %d", res.Code, http.StatusConflict)
	}
}

// 同一主机只允许一个远程桌面会话：新会话建立即接管，旧会话必须被标记为
// superseded，使旧页签能明确区分「被接管」与「超时回收」，从而停在专门的
// 接管提示而不是被当成终态错误要求刷新。
func TestRemoteDesktopCreateSupersedesExistingSessionForSameServer(t *testing.T) {
	manager := newRemoteDesktopManager()
	first := manager.create("windows-1", struct{}{})
	if manager.isSuperseded(first.ID) {
		t.Fatal("a freshly created session must not be marked superseded")
	}

	second := manager.create("windows-1", struct{}{})
	if second.ID == first.ID {
		t.Fatal("takeover must allocate a new session id")
	}
	if !manager.isSuperseded(first.ID) {
		t.Fatal("the replaced session must be marked superseded")
	}
	if manager.isSuperseded(second.ID) {
		t.Fatal("the new session must not be marked superseded")
	}
	if _, ok := manager.get(first.ID); ok {
		t.Fatal("the replaced session must no longer be retrievable")
	}
	if _, ok := manager.get(second.ID); !ok {
		t.Fatal("the new session must be retrievable")
	}
	// 不同主机互不影响：接管只针对同一 serverID。
	other := manager.create("windows-2", struct{}{})
	if manager.isSuperseded(other.ID) {
		t.Fatal("a session on another host must not be superseded")
	}
	if _, ok := manager.get(second.ID); !ok {
		t.Fatal("creating a session on another host must not evict this one")
	}
}

// 被接管会话的 signals 端点必须返回 409 + reason=superseded，而不是含糊的 404：
// 前端据此停止轮询并展示「重新接管」，避免落入死页。
func TestRemoteDesktopSupersededSessionSignalsReportConflict(t *testing.T) {
	manager := newRemoteDesktopManager()
	service := &Service{remoteDesktop: manager}
	first := manager.create("windows-1", struct{}{})
	manager.create("windows-1", struct{}{})

	req := httptest.NewRequest(http.MethodGet, "/api/server/remote-desktop/sessions/"+first.ID+"/signals?wait=0", nil)
	res := httptest.NewRecorder()
	service.getRemoteDesktopSignals(res, req, first.ID)
	if res.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d (body=%s)", res.Code, http.StatusConflict, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), `"reason":"superseded"`) {
		t.Fatalf("body must carry reason=superseded: %s", res.Body.String())
	}

	// 未接管过的不存在会话仍是 404，避免把普通失效误报为接管。
	req = httptest.NewRequest(http.MethodGet, "/api/server/remote-desktop/sessions/missing/signals?wait=0", nil)
	res = httptest.NewRecorder()
	service.getRemoteDesktopSignals(res, req, "missing")
	if res.Code != http.StatusNotFound {
		t.Fatalf("unknown session status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

// 接管记录只保留判定窗口，超窗后不得继续把普通失效当成接管。
func TestRemoteDesktopSupersededMarkerExpires(t *testing.T) {
	manager := newRemoteDesktopManager()
	session := manager.create("windows-1", struct{}{})
	manager.create("windows-1", struct{}{})

	manager.mu.Lock()
	manager.superseded[session.ID] = time.Now().Add(-remoteDesktopSupersededTTL - time.Second)
	manager.mu.Unlock()
	if manager.isSuperseded(session.ID) {
		t.Fatal("superseded marker must expire after its window")
	}

	// 过期记录由 cleanupLocked 连带清理，避免无界增长。
	manager.mu.Lock()
	manager.cleanupLocked(time.Now())
	_, stillThere := manager.superseded[session.ID]
	manager.mu.Unlock()
	if stillThere {
		t.Fatal("expired superseded marker must be pruned")
	}
}

func TestRemoteDesktopAgentSignalIsScopedToServer(t *testing.T) {
	service := &Service{remoteDesktop: newRemoteDesktopManager()}
	session := service.remoteDesktop.create("windows-1", struct{}{})
	payload := json.RawMessage(`{"session_id":"` + session.ID + `","signal":{"kind":"ice","candidate":{"candidate":"candidate:1"}},"state":"signaling"}`)
	service.handleRemoteDesktopAgentSignal("windows-2", payload)
	signals, _, _ := service.remoteDesktop.signals(session.ID, 0)
	if len(signals) != 0 {
		t.Fatal("signal from another Agent must be rejected")
	}
	service.handleRemoteDesktopAgentSignal("windows-1", payload)
	signals, state, _ := service.remoteDesktop.signals(session.ID, 0)
	if len(signals) != 1 || state != "signaling" {
		t.Fatalf("signal/state = %#v/%q", signals, state)
	}
}

func TestRemoteDesktopSessionExpiresOnAbsoluteDeadline(t *testing.T) {
	manager := newRemoteDesktopManager()
	session := manager.create("windows-1", struct{}{})
	// 绝对上限优先于空闲刷新：直接把绝对截止时间推到达即应回收。
	session.ExpiresAt = time.Now().Add(-time.Second)
	if _, ok := manager.get(session.ID); ok {
		t.Fatal("session past its absolute deadline must be reclaimed")
	}
}

func TestRemoteDesktopSessionExpiresWhenIdle(t *testing.T) {
	manager := newRemoteDesktopManager()
	session := manager.create("windows-1", struct{}{})
	session.LastActivity = time.Now().Add(-remoteDesktopIdleTTL - time.Minute)
	if _, ok := manager.get(session.ID); ok {
		t.Fatal("session idle past the idle TTL must be reclaimed")
	}
}

func TestRemoteDesktopPollingRefreshesIdleTimer(t *testing.T) {
	manager := newRemoteDesktopManager()
	session := manager.create("windows-1", struct{}{})
	// 客户端轮询代表会话仍在使用，应刷新空闲时间，避免活跃会话被误回收。
	stale := time.Now().Add(-remoteDesktopIdleTTL + time.Minute)
	session.LastActivity = stale
	if _, _, ok := manager.signals(session.ID, 0); !ok {
		t.Fatal("session should still be alive before the idle TTL")
	}
	if !session.LastActivity.After(stale) {
		t.Fatal("polling must refresh LastActivity so an active session is not reclaimed")
	}
}

func TestRemoteDesktopSignalCapAndSinceCursor(t *testing.T) {
	manager := newRemoteDesktopManager()
	session := manager.create("windows-1", struct{}{})
	total := remoteDesktopMaxSignals + 40
	for i := 0; i < total; i++ {
		if !manager.appendAgentSignal(session.ID, "windows-1", json.RawMessage(`{"kind":"ice"}`)) {
			t.Fatalf("signal %d should be accepted", i)
		}
	}
	all, _, ok := manager.signals(session.ID, 0)
	if !ok {
		t.Fatal("session should exist")
	}
	if len(all) != remoteDesktopMaxSignals {
		t.Fatalf("retained signals = %d, want %d", len(all), remoteDesktopMaxSignals)
	}
	lastID := all[len(all)-1].ID
	if lastID != int64(total) {
		t.Fatalf("last signal id = %d, want %d", lastID, total)
	}
	// since 游标只返回严格大于游标的信号。
	tail, _, _ := manager.signals(session.ID, lastID-1)
	if len(tail) != 1 || tail[0].ID != lastID {
		t.Fatalf("since cursor tail = %#v", tail)
	}
	if empty, _, _ := manager.signals(session.ID, lastID); len(empty) != 0 {
		t.Fatalf("since cursor at head must be empty, got %#v", empty)
	}
}

func TestRemoteDesktopRemoveWakesLongPoll(t *testing.T) {
	manager := newRemoteDesktopManager()
	session := manager.create("windows-1", struct{}{})
	done := make(chan bool, 1)
	go func() {
		_, _, ok := manager.waitSignals(session.ID, 0, 20*time.Second)
		done <- ok
	}()
	time.Sleep(20 * time.Millisecond)
	manager.remove(session.ID)
	select {
	case ok := <-done:
		if ok {
			t.Fatal("waitSignals should report the session as gone after removal")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("removing a session must wake pending long polls instead of waiting for the timeout")
	}
}

func TestRemoteDesktopCloseForServer(t *testing.T) {
	manager := newRemoteDesktopManager()
	first := manager.create("windows-1", struct{}{})
	second := manager.create("windows-2", struct{}{})
	if removed := manager.closeForServer("windows-1"); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, ok := manager.get(first.ID); ok {
		t.Fatal("session on the disconnected server must be removed")
	}
	if _, ok := manager.get(second.ID); !ok {
		t.Fatal("session on another server must be preserved")
	}
}

func TestCloseRemoteDesktopSessionHandler(t *testing.T) {
	manager := newRemoteDesktopManager()
	service := &Service{registry: NewConnectionRegistry(), remoteDesktop: manager}
	t.Cleanup(service.registry.Stop)
	session := manager.create("windows-1", struct{}{})

	req := httptest.NewRequest(http.MethodDelete, "/api/server/remote-desktop/sessions/"+session.ID, nil)
	res := httptest.NewRecorder()
	service.handleRemoteDesktopRoutes(res, req, []string{"sessions", session.ID})
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", res.Code, res.Body.String())
	}
	if _, ok := manager.get(session.ID); ok {
		t.Fatal("session must be gone after DELETE")
	}

	// 删除一个不存在的会话也应幂等返回成功。
	res = httptest.NewRecorder()
	service.handleRemoteDesktopRoutes(res, httptest.NewRequest(http.MethodDelete, "/x", nil), []string{"sessions", "missing"})
	if res.Code != http.StatusOK {
		t.Fatalf("deleting a missing session status = %d, want 200", res.Code)
	}
}

func TestSplitRemoteDesktopNatIPs(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"203.0.113.7", []string{"203.0.113.7"}},
		{"203.0.113.7, 198.51.100.9", []string{"203.0.113.7", "198.51.100.9"}},
		{"203.0.113.7;198.51.100.9\n192.0.2.1", []string{"203.0.113.7", "198.51.100.9", "192.0.2.1"}},
		{" 203.0.113.7 ,, ", []string{"203.0.113.7"}},
	}
	for _, tc := range cases {
		got := splitRemoteDesktopNatIPs(tc.raw)
		if len(got) != len(tc.want) {
			t.Fatalf("split(%q) = %#v, want %#v", tc.raw, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("split(%q)[%d] = %q, want %q", tc.raw, i, got[i], tc.want[i])
			}
		}
	}
}

func TestNormalizeRemoteDesktopNatType(t *testing.T) {
	cases := map[string]string{
		"":        "srflx",
		"host":    "host",
		"HOST":    "host",
		" srflx ": "srflx",
		// webrtc-rs rejects prflx/relay for 1:1 NAT; fall back to srflx.
		"prflx": "srflx",
		"relay": "srflx",
		"bogus": "srflx",
	}
	for input, want := range cases {
		if got := normalizeRemoteDesktopNatType(input); got != want {
			t.Fatalf("normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRemoteDesktopNatConfigWithoutStore(t *testing.T) {
	// 精简构造的 Service 没有 store，应安全返回「未配置」而非 panic。
	service := &Service{}
	if ips, kind, ok := service.remoteDesktopNatConfig(context.Background(), "windows-1"); ok || ips != nil || kind != "" {
		t.Fatalf("expected no config without store, got %#v %q %v", ips, kind, ok)
	}
}
