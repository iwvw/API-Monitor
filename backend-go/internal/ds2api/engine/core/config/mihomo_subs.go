package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// MihomoSubscriptionsFileVersion 是订阅独立存储文件的结构版本。
const MihomoSubscriptionsFileVersion = 1

// MihomoSubscriptionsFile 描述订阅与端口映射的独立持久化文件。
// 配置迁移后，机场订阅从 config.json 移出，单独存放于此，
// 避免 config.json 因订阅节点缓存频繁变化而抖动。
type MihomoSubscriptionsFile struct {
	Version       int                  `json:"version"`
	Subscriptions []MihomoSubscription `json:"subscriptions,omitempty"`
	PortMap       map[string]int       `json:"port_map,omitempty"`
}

// MihomoSubscriptionsPath 返回订阅独立存储文件的路径。
// 默认与 config.json 同目录；可用环境变量 DS2API_MIHOMO_SUBSCRIPTIONS_PATH 覆盖。
func MihomoSubscriptionsPath() string {
	if raw := strings.TrimSpace(os.Getenv("DS2API_MIHOMO_SUBSCRIPTIONS_PATH")); raw != "" {
		return ResolvePath("DS2API_MIHOMO_SUBSCRIPTIONS_PATH", "mihomo_subscriptions.json")
	}
	cfgPath := ConfigPath()
	dir := filepath.Dir(cfgPath)
	if dir == "." || dir == "" {
		return "mihomo_subscriptions.json"
	}
	return filepath.Join(dir, "mihomo_subscriptions.json")
}

// loadMihomoSubscriptionsFile 从独立文件加载订阅与端口映射并合并进配置。
// 文件不存在时保留 config.json 中的旧式订阅（兼容读取）；
// 文件解析失败时仅返回错误，调用方告警后继续，不阻断启动。
func loadMihomoSubscriptionsFile(cfg *Config) error {
	path := MihomoSubscriptionsPath()
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var file MihomoSubscriptionsFile
	if err := json.Unmarshal(content, &file); err != nil {
		return err
	}
	cfg.Mihomo.Subscriptions = file.Subscriptions
	cfg.Mihomo.PortMap = file.PortMap
	return nil
}

// configFileHasLegacyMihomoSubs 判断 config.json 原始内容里 mihomo 段是否仍
// 内嵌旧式 subscriptions/port_map（需要在下一次保存时迁移到独立文件）。
func configFileHasLegacyMihomoSubs(path string) bool {
	content, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var doc struct {
		Mihomo struct {
			Subscriptions json.RawMessage `json:"subscriptions"`
			PortMap       json.RawMessage `json:"port_map"`
		} `json:"mihomo"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return false
	}
	return len(doc.Mihomo.Subscriptions) > 0 || len(doc.Mihomo.PortMap) > 0
}

// saveMihomoSubscriptionsFile 把订阅与端口映射写入独立文件。
// 无订阅且无端口映射时删除该文件（最后一次删除订阅后自动清理）。
func saveMihomoSubscriptionsFile(m MihomoConfig) error {
	path := MihomoSubscriptionsPath()
	if len(m.Subscriptions) == 0 && len(m.PortMap) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			Logger.Warn("[config] remove mihomo subscriptions file failed", "path", path, "error", err)
			return err
		}
		return nil
	}
	file := MihomoSubscriptionsFile{
		Version:       MihomoSubscriptionsFileVersion,
		Subscriptions: m.Subscriptions,
		PortMap:       m.PortMap,
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := writeConfigBytes(path, b); err != nil {
		Logger.Warn("[config] write mihomo subscriptions file failed", "path", path, "error", err)
		return err
	}
	return nil
}
