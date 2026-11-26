package modelhub

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestKeyCooldownSecondsZeroIsSerialized 锁住「冷却秒数设为 0（关闭冷却）」这个合法值
// 必须出现在 JSON 里。
//
// 回归背景：Endpoint.KeyCooldownSeconds 曾带 `omitempty`，导致值为 0 时整个字段被省略；
// 前端 openEditEndpointModal 读到 undefined 后 `endpoint.keyCooldownSeconds ?? 60`
// 回退成 60，用户看到的症状是「冷却时间无法设为 0」，且保存后又被写成 60。
func TestKeyCooldownSecondsZeroIsSerialized(t *testing.T) {
	raw, err := json.Marshal(Endpoint{KeyCooldownSeconds: 0})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"keyCooldownSeconds":0`) {
		t.Fatalf("keyCooldownSeconds=0 必须出现在 JSON 中，实际: %s", raw)
	}

	// 非 0 值照常序列化。
	raw, err = json.Marshal(Endpoint{KeyCooldownSeconds: 120})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"keyCooldownSeconds":120`) {
		t.Fatalf("keyCooldownSeconds=120 必须出现在 JSON 中，实际: %s", raw)
	}

	// 反序列化不受影响：缺字段仍是 0（关闭），显式 0 也是 0。
	var ep Endpoint
	if err := json.Unmarshal([]byte(`{}`), &ep); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ep.KeyCooldownSeconds != 0 {
		t.Fatalf("缺字段时应为 0，实际 %d", ep.KeyCooldownSeconds)
	}
	if err := json.Unmarshal([]byte(`{"keyCooldownSeconds":0}`), &ep); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ep.KeyCooldownSeconds != 0 {
		t.Fatalf("显式 0 时应为 0，实际 %d", ep.KeyCooldownSeconds)
	}
}
