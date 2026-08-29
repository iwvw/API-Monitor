package config

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"
)

type Config struct {
	Keys                 []string                   `json:"keys,omitempty"`
	APIKeys              []APIKey                   `json:"api_keys,omitempty"`
	Accounts             []Account                  `json:"accounts,omitempty"`
	Proxies              []Proxy                    `json:"proxies,omitempty"`
	ModelAliases         map[string]string          `json:"model_aliases,omitempty"`
	Admin                AdminConfig                `json:"admin,omitempty"`
	Runtime              RuntimeConfig              `json:"runtime,omitempty"`
	Responses            ResponsesConfig            `json:"responses,omitempty"`
	Embeddings           EmbeddingsConfig           `json:"embeddings,omitempty"`
	AutoDelete           AutoDeleteConfig           `json:"auto_delete"`
	CurrentInputFile     CurrentInputFileConfig     `json:"current_input_file,omitempty"`
	ThinkingInjection    ThinkingInjectionConfig    `json:"thinking_injection,omitempty"`
	ExpertTextFileInline ExpertTextFileInlineConfig `json:"expert_text_file_inline,omitempty"`
	ExpertPromptSegment  ExpertPromptSegmentConfig  `json:"expert_prompt_segment,omitempty"`
	AutoRouteVision      AutoRouteVisionConfig      `json:"auto_route_vision,omitempty"`
	ElasticPool          ElasticPoolConfig          `json:"elastic_pool,omitempty"`
	Mihomo               MihomoConfig               `json:"mihomo,omitempty"`
	Vercel               VercelConfig               `json:"vercel,omitempty"`
	VercelSyncHash       string                     `json:"_vercel_sync_hash,omitempty"`
	VercelSyncTime       int64                      `json:"_vercel_sync_time,omitempty"`
	AdditionalFields     map[string]any             `json:"-"`
}

type Account struct {
	Name           string  `json:"name,omitempty"`
	Remark         string  `json:"remark,omitempty"`
	Email          string  `json:"email,omitempty"`
	Mobile         string  `json:"mobile,omitempty"`
	Password       string  `json:"password,omitempty"`
	Token          string  `json:"token,omitempty"`
	DeviceID       string  `json:"device_id,omitempty"`
	ProxyID        string  `json:"proxy_id,omitempty"`
	PoolType       string  `json:"pool_type,omitempty"`
	Locale         string  `json:"locale,omitempty"`
	Disabled       bool    `json:"disabled,omitempty"`
	DisabledReason string  `json:"disabled_reason,omitempty"`
	Banned         bool    `json:"banned,omitempty"`
	MutedUntil     float64 `json:"muted_until,omitempty"`
	// CooldownUntil 是本地风控冷却到期时间（Unix 秒）。
	// 与 MutedUntil 不同：MutedUntil 由上游下发，CooldownUntil 是我们自己
	// 在收到验证码挑战后主动设置的——挑战意味着风控已经盯上这个账号，
	// 继续用它只会把情况变得更糟。
	CooldownUntil float64 `json:"cooldown_until,omitempty"`
	// NodeCooldownUntil 是坏节点自动安全转移后账号的换号冷却到期时间（Unix 秒）。
	// 冷却期内自动调度不会再次切换该账号的节点，避免故障节点间反复横跳。
	NodeCooldownUntil float64 `json:"node_cooldown_until,omitempty"`
	// NoProxy 表示账号被显式解绑、强制走直连（不走代理）。置位后自动调度
	// （auto_bind 补位 / 一键分配）不再给该账号分配节点，直到用户显式重新绑定。
	NoProxy bool `json:"no_proxy,omitempty"`
}

type APIKey struct {
	Key    string `json:"key"`
	Name   string `json:"name,omitempty"`
	Remark string `json:"remark,omitempty"`
}

type Proxy struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

func NormalizeProxy(p Proxy) Proxy {
	p.ID = strings.TrimSpace(p.ID)
	p.Name = strings.TrimSpace(p.Name)
	p.Type = strings.ToLower(strings.TrimSpace(p.Type))
	p.Host = strings.TrimSpace(p.Host)
	p.Username = strings.TrimSpace(p.Username)
	p.Password = strings.TrimSpace(p.Password)
	if p.ID == "" {
		p.ID = StableProxyID(p)
	}
	if p.Name == "" && p.Host != "" && p.Port > 0 {
		p.Name = fmt.Sprintf("%s:%d", p.Host, p.Port)
	}
	return p
}

func StableProxyID(p Proxy) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(p.Type)) + "|" + strings.ToLower(strings.TrimSpace(p.Host)) + "|" + fmt.Sprintf("%d", p.Port) + "|" + strings.TrimSpace(p.Username)))
	return "proxy_" + hex.EncodeToString(sum[:6])
}

func (c *Config) ClearAccountTokens() {
	if c == nil {
		return
	}
	for i := range c.Accounts {
		c.Accounts[i].Token = ""
	}
}

func (c *Config) NormalizeCredentials() {
	if c == nil {
		return
	}
	normalizedAPIKeys := normalizeAPIKeys(c.APIKeys)
	if len(normalizedAPIKeys) > 0 {
		c.APIKeys = normalizedAPIKeys
		c.Keys = apiKeysToStrings(c.APIKeys)
	} else {
		c.Keys = normalizeKeys(c.Keys)
		c.APIKeys = apiKeysFromStrings(c.Keys, nil)
	}

	for i := range c.Accounts {
		c.Accounts[i].Name = strings.TrimSpace(c.Accounts[i].Name)
		c.Accounts[i].Remark = strings.TrimSpace(c.Accounts[i].Remark)
		c.Accounts[i].DeviceID = strings.TrimSpace(c.Accounts[i].DeviceID)
		c.Accounts[i].Locale = strings.TrimSpace(c.Accounts[i].Locale)
		c.Accounts[i].PoolType = NormalizePoolType(c.Accounts[i].PoolType)
	}

	c.Vercel = NormalizeVercelConfig(c.Vercel)
	c.Mihomo = NormalizeMihomoConfig(c.Mihomo)
	c.normalizeModelAliases()
}

// DropInvalidAccounts removes accounts that cannot be addressed by admin APIs
// (no email and no normalizable mobile). This prevents legacy token-only
// records from becoming orphaned empty entries after token stripping.
func (c *Config) DropInvalidAccounts() {
	if c == nil || len(c.Accounts) == 0 {
		return
	}
	kept := make([]Account, 0, len(c.Accounts))
	for _, acc := range c.Accounts {
		if acc.Identifier() == "" {
			continue
		}
		kept = append(kept, acc)
	}
	c.Accounts = kept
}

func (c *Config) normalizeModelAliases() {
	if c == nil {
		return
	}

	aliases := map[string]string{}
	for k, v := range c.ModelAliases {
		key := strings.TrimSpace(lower(k))
		val := strings.TrimSpace(lower(v))
		if key == "" || val == "" {
			continue
		}
		aliases[key] = val
	}
	if len(aliases) == 0 {
		c.ModelAliases = nil
	} else {
		c.ModelAliases = aliases
	}
}

type AdminConfig struct {
	PasswordHash      string `json:"password_hash,omitempty"`
	JWTExpireHours    int    `json:"jwt_expire_hours,omitempty"`
	JWTValidAfterUnix int64  `json:"jwt_valid_after_unix,omitempty"`
}

type RuntimeConfig struct {
	AccountMaxInflight        int `json:"account_max_inflight,omitempty"`
	AccountMaxQueue           int `json:"account_max_queue,omitempty"`
	GlobalMaxInflight         int `json:"global_max_inflight,omitempty"`
	TokenRefreshIntervalHours int `json:"token_refresh_interval_hours,omitempty"`
}

type ResponsesConfig struct {
	StoreTTLSeconds int `json:"store_ttl_seconds,omitempty"`
}

type EmbeddingsConfig struct {
	Provider string `json:"provider,omitempty"`
}

type AutoDeleteConfig struct {
	Mode     string `json:"mode,omitempty"`
	Sessions bool   `json:"sessions,omitempty"`
}

type CurrentInputFileConfig struct {
	Enabled  *bool `json:"enabled,omitempty"`
	MinChars int   `json:"min_chars,omitempty"`
}

type ThinkingInjectionConfig struct {
	Enabled *bool  `json:"enabled,omitempty"`
	Prompt  string `json:"prompt,omitempty"`
}

type ExpertPromptSegmentConfig struct {
	Enabled  *bool `json:"enabled,omitempty"`
	MaxChars int   `json:"max_chars,omitempty"`
}

type ExpertTextFileInlineConfig struct {
	Enabled           *bool    `json:"enabled,omitempty"`
	MaxFileBytes      int      `json:"max_file_bytes,omitempty"`
	AllowedExtensions []string `json:"allowed_extensions,omitempty"`
}

type AutoRouteVisionConfig struct {
	Enabled *bool `json:"enabled,omitempty"`
}

// ElasticPoolConfig 控制弹性号池行为。
// 开启后按 config.Accounts 原始顺序在未禁言账号中启用前 N 个，
// 其余禁用；封号(muted)账号一律禁用且不占名额。
// PerPool=false 时所有账号共用 GlobalCount；PerPool=true 时
// default/no_tools/tools_only 三种号池类型分别使用各自的 Count。
type ElasticPoolConfig struct {
	Enabled        bool `json:"enabled,omitempty"`
	PerPool        bool `json:"per_pool,omitempty"`
	GlobalCount    int  `json:"global_count,omitempty"`
	DefaultCount   int  `json:"default_count,omitempty"`
	NoToolsCount   int  `json:"no_tools_count,omitempty"`
	ToolsOnlyCount int  `json:"tools_only_count,omitempty"`
}

type VercelConfig struct {
	Token     string `json:"token,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	TeamID    string `json:"team_id,omitempty"`
}

func NormalizeVercelConfig(v VercelConfig) VercelConfig {
	return VercelConfig{
		Token:     strings.TrimSpace(v.Token),
		ProjectID: strings.TrimSpace(v.ProjectID),
		TeamID:    strings.TrimSpace(v.TeamID),
	}
}

func (c *Config) ClearVercelCredentials() {
	if c == nil {
		return
	}
	c.Vercel = VercelConfig{}
}
