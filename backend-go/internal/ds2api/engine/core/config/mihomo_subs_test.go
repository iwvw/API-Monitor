package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMihomoConfigMarshalOmitsSubscriptions(t *testing.T) {
	cfg := Config{
		Mihomo: MihomoConfig{
			Enabled:    true,
			BinaryPath: "/usr/bin/mihomo",
			BasePort:   DefaultMihomoBasePort,
			APIPort:    DefaultMihomoAPIPort,
			Subscriptions: []MihomoSubscription{
				{ID: "sub-1", Name: "机场", URL: "https://example.com/sub", Nodes: []MihomoNode{{Name: "节点", Type: "ss"}}},
			},
			PortMap: map[string]int{"sub-1::节点": 10801},
		},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	raw := string(b)
	if strings.Contains(raw, "subscriptions") || strings.Contains(raw, "port_map") {
		t.Fatalf("expected subscriptions/port_map omitted from config.json serialization, got: %s", raw)
	}
	if !strings.Contains(raw, `"enabled":true`) {
		t.Fatalf("expected enabled=true kept in serialization, got: %s", raw)
	}

	// Unmarshal 仍兼容旧式 subscriptions/port_map。
	var decoded Config
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if len(decoded.Mihomo.Subscriptions) != 0 {
		t.Fatalf("expected no subscriptions after roundtrip (omitted), got %+v", decoded.Mihomo.Subscriptions)
	}

	legacy := `{"mihomo":{"enabled":true,"subscriptions":[{"id":"sub-1","url":"https://example.com/sub"}],"port_map":{"sub-1::x":10801}}}`
	var legacyCfg Config
	if err := json.Unmarshal([]byte(legacy), &legacyCfg); err != nil {
		t.Fatalf("unmarshal legacy error: %v", err)
	}
	if len(legacyCfg.Mihomo.Subscriptions) != 1 || legacyCfg.Mihomo.Subscriptions[0].ID != "sub-1" {
		t.Fatalf("legacy subscriptions not read: %+v", legacyCfg.Mihomo.Subscriptions)
	}
	if legacyCfg.Mihomo.PortMap["sub-1::x"] != 10801 {
		t.Fatalf("legacy port_map not read: %+v", legacyCfg.Mihomo.PortMap)
	}
}

func TestMihomoAutoBindRoundTripsThroughSerialization(t *testing.T) {
	cfg := Config{
		Mihomo: MihomoConfig{
			Enabled:  true,
			AutoBind: true,
		},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if !strings.Contains(string(b), `"auto_bind":true`) {
		t.Fatalf("expected auto_bind=true kept in serialization, got: %s", string(b))
	}
	var decoded Config
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !decoded.Mihomo.AutoBind {
		t.Fatal("auto_bind lost after roundtrip")
	}
	clone := decoded.Clone()
	if !clone.Mihomo.AutoBind {
		t.Fatal("clone dropped mihomo.auto_bind")
	}
}

func TestMihomoSubscriptionsPersistToSeparateFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("DS2API_CONFIG_JSON", "")
	t.Setenv("DS2API_CONFIG_PATH", path)

	store := LoadStore()
	if err := store.Update(func(c *Config) error {
		c.Mihomo.Subscriptions = []MihomoSubscription{
			{ID: "sub-1", Name: "机场", URL: "https://example.com/sub", Nodes: []MihomoNode{{Name: "节点", Type: "ss"}}},
		}
		c.Mihomo.PortMap = map[string]int{"sub-1::节点": DefaultMihomoBasePort}
		return nil
	}); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// config.json 不应包含订阅。
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(content), "subscriptions") {
		t.Fatalf("config.json should not contain subscriptions, got: %s", content)
	}

	// 独立文件应包含订阅与端口映射。
	subsPath := filepath.Join(dir, "mihomo_subscriptions.json")
	subsContent, err := os.ReadFile(subsPath)
	if err != nil {
		t.Fatalf("read subscriptions file: %v", err)
	}
	if !strings.Contains(string(subsContent), "sub-1") || !strings.Contains(string(subsContent), "port_map") {
		t.Fatalf("subscriptions file content mismatch: %s", subsContent)
	}

	// 重新加载后订阅仍可用。
	reloaded := LoadStore()
	snap := reloaded.Snapshot()
	if len(snap.Mihomo.Subscriptions) != 1 || snap.Mihomo.Subscriptions[0].ID != "sub-1" {
		t.Fatalf("subscriptions lost after reload: %+v", snap.Mihomo.Subscriptions)
	}
	if snap.Mihomo.PortMap["sub-1::节点"] != DefaultMihomoBasePort {
		t.Fatalf("port_map lost after reload: %+v", snap.Mihomo.PortMap)
	}
}

func TestMihomoLatencyPersistsOnNode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("DS2API_CONFIG_JSON", "")
	t.Setenv("DS2API_CONFIG_PATH", path)

	store := LoadStore()
	if err := store.Update(func(c *Config) error {
		c.Mihomo.Subscriptions = []MihomoSubscription{
			{ID: "sub-1", Name: "机场", URL: "https://example.com/sub", Nodes: []MihomoNode{
				{Name: "香港 01", Type: "ss", Status: "ok", LatencyMS: 88, TestedAt: 123},
			}},
		}
		return nil
	}); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// 延迟直接挂在节点上，随独立文件持久化；config.json 不应出现测速字段。
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(content), "latency_ms") {
		t.Fatalf("config.json should not contain latency, got: %s", content)
	}
	subsContent, err := os.ReadFile(filepath.Join(dir, "mihomo_subscriptions.json"))
	if err != nil {
		t.Fatalf("read subscriptions file: %v", err)
	}
	if !strings.Contains(string(subsContent), "latency_ms") {
		t.Fatalf("subscriptions file should contain latency on node, got: %s", subsContent)
	}

	// 重新加载后节点延迟可用。
	reloaded := LoadStore()
	nodes := reloaded.Snapshot().Mihomo.Subscriptions[0].Nodes
	if len(nodes) != 1 || nodes[0].Status != "ok" || nodes[0].LatencyMS != 88 || nodes[0].TestedAt != 123 {
		t.Fatalf("latency lost after reload: %+v", nodes)
	}
	// 深拷贝应保留节点延迟。
	clone := reloaded.Snapshot().Clone()
	if clone.Mihomo.Subscriptions[0].Nodes[0].LatencyMS != 88 {
		t.Fatal("clone dropped node latency")
	}
}

func TestMihomoSubscriptionsMigratedFromLegacyConfigJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	legacy := `{
		"keys": ["k1"],
		"mihomo": {
			"enabled": false,
			"subscriptions": [{"id": "sub-legacy", "name": "旧机场", "url": "https://example.com/sub", "nodes": [{"name": "香港", "type": "ss"}]}],
			"port_map": {"sub-legacy::香港": 10801}
		}
	}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}
	t.Setenv("DS2API_CONFIG_JSON", "")
	t.Setenv("DS2API_CONFIG_PATH", path)

	store := LoadStore()
	snap := store.Snapshot()
	if len(snap.Mihomo.Subscriptions) != 1 || snap.Mihomo.Subscriptions[0].ID != "sub-legacy" {
		t.Fatalf("legacy subscriptions not loaded: %+v", snap.Mihomo.Subscriptions)
	}

	// 触发一次保存（模拟增删订阅后的迁移写入）。
	if err := store.Update(func(c *Config) error {
		c.Mihomo.Subscriptions[0].Name = "机场（已迁移）"
		return nil
	}); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(content), "subscriptions") {
		t.Fatalf("legacy subscriptions should be migrated out of config.json, got: %s", content)
	}
	if _, err := os.Stat(filepath.Join(dir, "mihomo_subscriptions.json")); err != nil {
		t.Fatalf("migrated subscriptions file missing: %v", err)
	}

	reloaded := LoadStore()
	if got := reloaded.Snapshot().Mihomo.Subscriptions[0].Name; got != "机场（已迁移）" {
		t.Fatalf("migrated name not preserved, got %q", got)
	}
}

func TestMihomoOnlyChangesSkipUnrelatedFileWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	subsPath := filepath.Join(dir, "mihomo_subscriptions.json")
	t.Setenv("DS2API_CONFIG_JSON", "")
	t.Setenv("DS2API_CONFIG_PATH", path)
	t.Setenv("DS2API_MIHOMO_SUBSCRIPTIONS_PATH", subsPath)

	store := LoadStore()
	if err := store.Update(func(c *Config) error {
		c.Keys = []string{"k1"}
		c.Mihomo.Subscriptions = []MihomoSubscription{{
			ID: "sub-1", URL: "https://example.com/sub",
			Nodes: []MihomoNode{{Name: "节点", Type: "ss"}},
		}}
		return nil
	}); err != nil {
		t.Fatalf("seed update failed: %v", err)
	}

	// config.json 置只读：纯 mihomo 数据变更不应重写它。
	if err := os.Chmod(path, 0o444); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	if err := store.Update(func(c *Config) error {
		c.Mihomo.Subscriptions[0].Nodes[0].LatencyMS = 88
		return nil
	}); err != nil {
		_ = os.Chmod(path, 0o644)
		t.Fatalf("mihomo-only update must not rewrite config.json: %v", err)
	}
	_ = os.Chmod(path, 0o644)

	// 订阅文件置只读：非 mihomo 配置变更不应重写它。
	if err := os.Chmod(subsPath, 0o444); err != nil {
		t.Fatalf("chmod subs: %v", err)
	}
	if err := store.Update(func(c *Config) error {
		c.Keys = append(c.Keys, "k2")
		return nil
	}); err != nil {
		_ = os.Chmod(subsPath, 0o644)
		t.Fatalf("keys-only update must not rewrite subscriptions file: %v", err)
	}
	_ = os.Chmod(subsPath, 0o644)

	// 数据一致性：订阅文件应已持久化延迟字段。
	content, err := os.ReadFile(subsPath)
	if err != nil {
		t.Fatalf("read subs file: %v", err)
	}
	if !strings.Contains(string(content), "latency_ms") {
		t.Fatalf("subs file should persist latency on node: %s", content)
	}
}

func TestMihomoSubscriptionsFileRemovedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv("DS2API_CONFIG_JSON", "")
	t.Setenv("DS2API_CONFIG_PATH", path)

	store := LoadStore()
	if err := store.Update(func(c *Config) error {
		c.Mihomo.Subscriptions = []MihomoSubscription{{ID: "sub-1", URL: "https://example.com/sub"}}
		return nil
	}); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	subsPath := filepath.Join(dir, "mihomo_subscriptions.json")
	if _, err := os.Stat(subsPath); err != nil {
		t.Fatalf("subscriptions file should exist: %v", err)
	}

	if err := store.Update(func(c *Config) error {
		c.Mihomo.Subscriptions = nil
		c.Mihomo.PortMap = nil
		return nil
	}); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	if _, err := os.Stat(subsPath); !os.IsNotExist(err) {
		t.Fatalf("subscriptions file should be removed when empty, err=%v", err)
	}
}
