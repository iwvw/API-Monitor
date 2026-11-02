package config

import (
	"strings"
	"time"
)

const (
	// PoolTypeDefault 允许无工具调用和含工具调用的请求使用此账号。
	PoolTypeDefault = "default"
	// PoolTypeNoTools 仅允许无工具调用的请求使用此账号。
	// 携带工具定义（请求 body 的 tools 非空）的请求不会调度到此账号。
	PoolTypeNoTools = "no_tools"
	// PoolTypeToolsOnly 仅允许含工具调用的请求使用此账号。
	// 不携带工具定义（请求 body 的 tools 为空）的请求不会调度到此账号。
	PoolTypeToolsOnly = "tools_only"
)

func (a Account) Identifier() string {
	if strings.TrimSpace(a.Email) != "" {
		return strings.TrimSpace(a.Email)
	}
	if mobile := NormalizeMobileForStorage(a.Mobile); mobile != "" {
		return mobile
	}
	return ""
}

// IsEnabled reports whether the account is eligible for scheduling.
// Disabled accounts are skipped by the pool.
func (a Account) IsEnabled() bool {
	return !a.Disabled
}

// IsMuted reports whether the account is currently muted (banned from chatting).
// Returns true only when MutedUntil is set and has not yet expired.
func (a Account) IsMuted() bool {
	if a.MutedUntil <= 0 {
		return false
	}
	return a.MutedUntil > float64(time.Now().Unix())
}

// IsBanned reports whether the account has been suspended by upstream
// (USER_IS_BANNED). A banned account stays out of the pool even if an admin
// manually re-enables it; only a successful token refresh can clear the flag.
func (a Account) IsBanned() bool {
	return a.Banned
}

// IsCoolingDown reports whether the account is in a local risk-control cooldown
// after a captcha challenge. Like IsMuted this expires on its own, so the pool
// picks the account back up without any sweeper.
func (a Account) IsCoolingDown() bool {
	if a.CooldownUntil <= 0 {
		return false
	}
	return a.CooldownUntil > float64(time.Now().Unix())
}

// IsSchedulable reports whether the pool may hand this account to a request.
func (a Account) IsSchedulable() bool {
	return a.IsEnabled() && !a.IsMuted() && !a.IsBanned() && !a.IsCoolingDown()
}

// NormalizePoolType 规范化账号号池类型，空值视为 default。
func NormalizePoolType(poolType string) string {
	switch strings.ToLower(strings.TrimSpace(poolType)) {
	case PoolTypeNoTools:
		return PoolTypeNoTools
	case PoolTypeToolsOnly:
		return PoolTypeToolsOnly
	default:
		return PoolTypeDefault
	}
}

// MatchesPoolType 判断账号是否可被指定工具开关的请求调用。
//   - no_tools 账号仅匹配不含工具定义（toolsPresent=false）的请求
//   - tools_only 账号仅匹配含工具定义（toolsPresent=true）的请求
//   - default / 未知 账号总是匹配
//
// toolsPresent 表示本次请求 body 是否携带非空的 tools 字段，由请求链路在
// 标准化前判定。
func (a Account) MatchesPoolType(toolsPresent bool) bool {
	switch NormalizePoolType(a.PoolType) {
	case PoolTypeNoTools:
		return !toolsPresent
	case PoolTypeToolsOnly:
		return toolsPresent
	default:
		return true
	}
}
