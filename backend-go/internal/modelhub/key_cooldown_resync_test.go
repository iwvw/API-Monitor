package modelhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestResyncKeyCooldownsAppliesNewValue 验证「修改冷却秒数立即覆盖生效」：
// 已按 72h 冷却的 key，在端点冷却改成 60s 后必须被收紧到 60s，
// 而不是等原 72h 自然过期。
func TestResyncKeyCooldownsAppliesNewValue(t *testing.T) {
	s := newOpenAIService(t)
	const epID = "ep-resync"
	s.armKeyCooldown(epID, "k1", 72*3600)
	s.armKeyCooldown(epID, "k2", 72*3600)

	if left := time.Until(s.keyCooledAt(epID, "k1")); left < 71*time.Hour {
		t.Fatalf("precondition failed: k1 cooldown = %v, want ~72h", left)
	}

	if changed := s.resyncKeyCooldowns(epID, 60); changed != 2 {
		t.Fatalf("resync should touch 2 cooled keys, got %d", changed)
	}
	for _, k := range []string{"k1", "k2"} {
		left := time.Until(s.keyCooledAt(epID, k))
		if left > 61*time.Second || left < 50*time.Second {
			t.Fatalf("key %q cooldown should be ~60s after resync, got %v", k, left)
		}
	}

	// 再改成 0（关闭冷却）= 立即清空。
	if changed := s.resyncKeyCooldowns(epID, 0); changed != 2 {
		t.Fatalf("resync(0) should release 2 cooled keys, got %d", changed)
	}
	for _, k := range []string{"k1", "k2"} {
		if !s.keyCooledAt(epID, k).IsZero() {
			t.Fatalf("key %q cooldown should be cleared when cooldown disabled", k)
		}
	}
}

// TestResyncKeyCooldownsIgnoresExpiredEntries 验证已自然过期的条目不会被重新拉起：
// 否则把冷却改成 60s 会把早已恢复的 key 又关 60 秒。
func TestResyncKeyCooldownsIgnoresExpiredEntries(t *testing.T) {
	s := newOpenAIService(t)
	const epID = "ep-resync-expired"
	s.keyMu.Lock()
	s.keyStateByEndpoint[epID] = newEndpointKeyState()
	s.keyStateByEndpoint[epID].health = map[string]*keyHealthEntry{
		"stale": {cooldownUntil: time.Now().Add(-time.Hour)},
	}
	s.keyMu.Unlock()

	if changed := s.resyncKeyCooldowns(epID, 3600); changed != 0 {
		t.Fatalf("expired entries must not be re-armed, touched %d", changed)
	}
	if until := s.keyCooledAt(epID, "stale"); time.Now().Before(until) {
		t.Fatalf("stale key should stay uncooled, got %v", until)
	}
}

// TestReleaseEndpointKeyCooldownsRoute 验证「释放所有冷却」接口：
// 清空全部冷却并返回释放条数，随后 key-state 应显示 cooled=false。
func TestReleaseEndpointKeyCooldownsRoute(t *testing.T) {
	s := newOpenAIService(t)

	createPayload := `{
		"name": "Release Cooldowns",
		"baseUrl": "http://127.0.0.1:1",
		"apiKey": "k-cool",
		"apiKeys": ["k-ok"],
		"keyCooldownSeconds": 3600,
		"skipVerify": true
	}`
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(createPayload))
	s.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	id := createRes.Endpoint.ID

	s.armKeyCooldown(id, "k-cool", 3600)

	wState := httptest.NewRecorder()
	rState, _ := http.NewRequest("GET", "/api/modelhub/endpoints/"+id+"/key-state", nil)
	s.ServeHTTP(wState, rState)
	var stateRes struct {
		Keys []endpointKeyRuntimeItem `json:"keys"`
	}
	mustDecode(t, wState.Body.String(), &stateRes)
	cooledCount := 0
	for _, item := range stateRes.Keys {
		if item.Cooled {
			cooledCount++
		}
	}
	if cooledCount != 1 {
		t.Fatalf("precondition: expected 1 cooled key, got %d", cooledCount)
	}

	wRelease := httptest.NewRecorder()
	rRelease, _ := http.NewRequest("POST", "/api/modelhub/endpoints/"+id+"/key-state/release", nil)
	s.ServeHTTP(wRelease, rRelease)
	if wRelease.Code != http.StatusOK {
		t.Fatalf("release status = %d body=%s", wRelease.Code, wRelease.Body.String())
	}
	var releaseRes struct {
		Success  bool `json:"success"`
		Released int  `json:"released"`
	}
	mustDecode(t, wRelease.Body.String(), &releaseRes)
	if !releaseRes.Success || releaseRes.Released != 1 {
		t.Fatalf("release response = %+v, want success with released=1", releaseRes)
	}

	wAfter := httptest.NewRecorder()
	rAfter, _ := http.NewRequest("GET", "/api/modelhub/endpoints/"+id+"/key-state", nil)
	s.ServeHTTP(wAfter, rAfter)
	var afterRes struct {
		Keys []endpointKeyRuntimeItem `json:"keys"`
	}
	mustDecode(t, wAfter.Body.String(), &afterRes)
	for _, item := range afterRes.Keys {
		if item.Cooled {
			t.Fatalf("key %q still cooled after release: %+v", item.Key, item)
		}
	}
}

// TestReleaseEndpointKeyCooldownsMissingEndpoint 验证不存在端点返回 404。
func TestReleaseEndpointKeyCooldownsMissingEndpoint(t *testing.T) {
	s := newOpenAIService(t)
	w := httptest.NewRecorder()
	r, _ := http.NewRequest("POST", "/api/modelhub/endpoints/does-not-exist/key-state/release", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing endpoint should be 404, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestUpdateEndpointCooldownAppliesImmediately 验证端到端：通过更新接口把冷却
// 从 72h 改成 60s 后，存量冷却立即被收紧（而不是等 72h 过期）。
func TestUpdateEndpointCooldownAppliesImmediately(t *testing.T) {
	s := newOpenAIService(t)

	createPayload := `{
		"name": "Cooldown Update",
		"baseUrl": "http://127.0.0.1:1",
		"apiKey": "k1",
		"apiKeys": ["k2"],
		"keyCooldownSeconds": 259200,
		"skipVerify": true
	}`
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(createPayload))
	s.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	id := createRes.Endpoint.ID

	s.armKeyCooldown(id, "k1", 259200)

	wUpdate := httptest.NewRecorder()
	rUpdate, _ := http.NewRequest("PUT", "/api/modelhub/endpoints/"+id, strings.NewReader(`{"name": "Cooldown Update", "keyCooldownSeconds": 60}`))
	s.ServeHTTP(wUpdate, rUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("update status = %d body=%s", wUpdate.Code, wUpdate.Body.String())
	}

	left := time.Until(s.keyCooledAt(id, "k1"))
	if left > 61*time.Second {
		t.Fatalf("changing cooldown 259200s -> 60s must tighten existing cooldown immediately, still %v", left)
	}
	if left <= 0 {
		t.Fatalf("existing cooldown should still be active for ~60s, got %v", left)
	}

	db, err := s.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	var stored int
	err = db.QueryRowContext(context.Background(),
		"SELECT key_cooldown_seconds FROM modelhub_endpoints WHERE id = ?", id).Scan(&stored)
	db.Close()
	if err != nil {
		t.Fatalf("read key_cooldown_seconds: %v", err)
	}
	if stored != 60 {
		t.Fatalf("persisted key_cooldown_seconds = %d, want 60", stored)
	}

	wOff := httptest.NewRecorder()
	rOff, _ := http.NewRequest("PUT", "/api/modelhub/endpoints/"+id, strings.NewReader(`{"name": "Cooldown Update", "keyCooldownSeconds": 0}`))
	s.ServeHTTP(wOff, rOff)
	if wOff.Code != http.StatusOK {
		t.Fatalf("update(0) status = %d body=%s", wOff.Code, wOff.Body.String())
	}
	if until := s.keyCooledAt(id, "k1"); !until.IsZero() && time.Now().Before(until) {
		t.Fatalf("setting cooldown to 0 must clear existing cooldowns, still active until %v", until)
	}
}
