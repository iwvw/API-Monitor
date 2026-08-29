package mihomo

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

// newFakeController 模拟 mihomo external-controller 的延迟测试接口
// （GET /proxies/{name}/delay）。名为 "超时节点" 的节点返回 500（模拟失败）；
// 其余节点返回 delays 中指定的延迟，未指定则返回默认正延迟。
// proxy 名带 "subID::" 前缀（运行时限定名），按前缀后的节点名匹配 delays。
func newFakeController(delays map[string]int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "proxies" || parts[2] != "delay" {
			http.NotFound(w, r)
			return
		}
		name, _ := url.PathUnescape(parts[1])
		if idx := strings.Index(name, "::"); idx >= 0 {
			name = name[idx+2:]
		}
		if name == "超时节点" {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"request timeout"}`))
			return
		}
		delay, ok := delays[name]
		if !ok {
			delay = 50
		}
		_ = json.NewEncoder(w).Encode(map[string]int64{"delay": delay})
	}))
}

// newDelayTestStore 构造启用状态、api_port 指向假 controler 的内存配置仓库。
func newDelayTestStore(t *testing.T, apiPort int) *config.Store {
	t.Helper()
	t.Setenv("DS2API_CONFIG_JSON", fmt.Sprintf(`{
		"keys": ["k1"],
		"accounts": [
			{"email": "a@test.com", "password": "x"},
			{"email": "b@test.com", "password": "x"}
		],
		"mihomo": {
			"enabled": true,
			"api_port": %d,
			"subscriptions": [{
				"id": "sub-1",
				"name": "测试机场",
				"url": "https://example.com/sub",
				"nodes": [
					{"name": "香港 01", "type": "ss", "raw": {"name": "香港 01", "type": "ss", "server": "hk.example.com", "port": 8388, "cipher": "aes-128-gcm", "password": "pw"}},
					{"name": "日本 01", "type": "vmess", "raw": {"name": "日本 01", "type": "vmess", "server": "jp.example.com", "port": 443, "uuid": "u-u-i-d"}},
					{"name": "美国 01", "type": "trojan", "raw": {"name": "美国 01", "type": "trojan", "server": "us.example.com", "port": 443, "password": "pw"}},
					{"name": "超时节点", "type": "ss", "raw": {"name": "超时节点", "type": "ss", "server": "slow.example.com", "port": 8388, "cipher": "aes-128-gcm", "password": "pw"}}
				]
			}]
		}
	}`, apiPort))
	return config.LoadStore()
}

// runningMgr 构造一个 running=true 的 Manager（避免真实拉起 mihomo 子进程）。
func runningMgr(store *config.Store) *Manager {
	mgr := NewManager(store, nil)
	mgr.mu.Lock()
	mgr.running = true
	mgr.mu.Unlock()
	return mgr
}

func TestTestLatencySortsResults(t *testing.T) {
	srv := newFakeController(map[string]int64{
		"日本 01": 80,
		"香港 01": 120,
		"美国 01": 200,
	})
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	mgr := runningMgr(newDelayTestStore(t, port))
	results, err := mgr.TestLatency(context.Background())
	if err != nil {
		t.Fatalf("TestLatency failed: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}
	wantOrder := []string{"日本 01", "香港 01", "美国 01", "超时节点"}
	for i, want := range wantOrder {
		got, _ := results[i]["name"].(string)
		if got != want {
			t.Fatalf("result[%d] = %q, want %q", i, got, want)
		}
	}
	if results[3]["error"] == nil || results[3]["error"] == "" {
		t.Fatalf("expected last result to carry an error: %v", results[3])
	}
	// 成功项应全部携带延迟。
	for i := 0; i < 3; i++ {
		if results[i]["delay_ms"] == nil {
			t.Fatalf("result[%d] missing delay_ms: %v", i, results[i])
		}
	}
}

func TestTestLatencyBatchesOver60(t *testing.T) {
	srv := newFakeController(nil)
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	store := newDelayTestStore(t, port)
	const extra = 130
	if err := store.Update(func(c *config.Config) error {
		for i := 1; i <= extra; i++ {
			name := fmt.Sprintf("批量节点 %03d", i)
			c.Mihomo.Subscriptions[0].Nodes = append(c.Mihomo.Subscriptions[0].Nodes, config.MihomoNode{
				Name: name,
				Type: "ss",
				Raw:  map[string]any{"name": name, "type": "ss", "server": "batch.example.com", "port": 8388, "cipher": "aes-128-gcm", "password": "pw"},
			})
		}
		return nil
	}); err != nil {
		t.Fatalf("seed extra nodes failed: %v", err)
	}

	mgr := runningMgr(store)
	results, err := mgr.TestLatency(context.Background())
	if err != nil {
		t.Fatalf("TestLatency failed: %v", err)
	}
	if len(results) != 4+extra {
		t.Fatalf("expected %d results, got %d", 4+extra, len(results))
	}
	failed := 0
	for _, r := range results {
		if r["error"] != nil && r["error"] != "" {
			if r["name"] != "超时节点" {
				t.Fatalf("unexpected error result: %v", r)
			}
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("expected exactly 1 failed node (超时节点), got %d", failed)
	}
}

func TestTestLatencyRequiresRunningBridge(t *testing.T) {
	store := newDelayTestStore(t, 19090)
	mgr := NewManager(store, nil)
	if _, err := mgr.TestLatency(context.Background()); err == nil {
		t.Fatal("expected error when bridge is not running")
	}
}
