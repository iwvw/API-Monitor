package openaibeta

import (
	engineconfig "github.com/iwvw/api-monitor/backend-go/internal/openaibeta/engine/config"
)

// defaultAnonAPIKey 与上游 vertex2api 内置匿名 key 保持一致。
const defaultAnonAPIKey = "AIzaSyCI-zsRP85UVOi0DjtiCwWBwQ1djDy741g"

// defaultCountTokensQuerySig 与上游内置签名保持一致。
const defaultCountTokensQuerySig = "2/mENOSldfC+HZM+tGhVuJLrl8M6gEyK3HRjUKuA5AM58="

// settingsProvider 实现 engine/config.ConfigProvider，读取 Service 当前设置。
// 模型注册表/别名经 engineconfig 包内存 store 读取（syncModelStore 已同步）。
type settingsProvider struct {
	s *Service
}

func (p settingsProvider) PortAPI() int          { return 0 }
func (p settingsProvider) MaxRetries() int       { return p.s.Settings().MaxRetries }
func (p settingsProvider) AdminPassword() string { return "" }
// ProxyURL 走 via 直连代理入口（每次请求显式传入 proxyURI），此处恒返回空。
func (p settingsProvider) ProxyURL() string { return "" }
func (p settingsProvider) DebugPprof() bool         { return false }
func (p settingsProvider) DebugMode() bool          { return p.s.Settings().DebugMode }
func (p settingsProvider) DropMaxTokens() bool      { return p.s.Settings().DropMaxTokens }
func (p settingsProvider) AggregateStream() bool    { return p.s.Settings().AggregateStream }
func (p settingsProvider) MaxN() int                { return p.s.Settings().MaxN }
func (p settingsProvider) MaxRequestMB() int        { return 0 }
func (p settingsProvider) MaxSpillMB() int          { return 2048 }
func (p settingsProvider) RequestTimeout() int      { return p.s.Settings().RequestTimeout }
func (p settingsProvider) RaceTimeout() int         { return 0 }
func (p settingsProvider) StreamIdleTimeoutSeconds() int {
	return p.s.Settings().StreamIdleTimeoutSeconds
}
func (p settingsProvider) ModelTurnGuardEnabled() bool { return p.s.Settings().ModelTurnGuardEnabled }
func (p settingsProvider) VertexAPIKey() string {
	if k := p.s.Settings().VertexAPIKey; k != "" {
		return k
	}
	return defaultAnonAPIKey
}
func (p settingsProvider) CountTokensQuerySignature() string {
	if k := p.s.Settings().CountTokensQuerySignature; k != "" {
		return k
	}
	return defaultCountTokensQuerySig
}
func (p settingsProvider) SafetySettings() map[string]string {
	return map[string]string{}
}
func (p settingsProvider) ParallelPoolEnabled() bool      { return false }
func (p settingsProvider) StickyNodePriority() bool       { return false }
func (p settingsProvider) ParallelPoolRetryEnabled() bool { return false }
func (p settingsProvider) ParallelPoolSize() int          { return 1 }
func (p settingsProvider) ParallelPoolDelayDynamic() bool { return false }
func (p settingsProvider) ParallelPoolDelayMs() int       { return 0 }
func (p settingsProvider) ActiveNodeURI() string          { return "" }
func (p settingsProvider) ParallelNodeTopK() int          { return 0 }
func (p settingsProvider) BackgroundImage() string        { return "" }
func (p settingsProvider) FontSize() string               { return "" }
func (p settingsProvider) FontColorType() string          { return "" }
func (p settingsProvider) FontColor() string              { return "" }
func (p settingsProvider) CustomBgPresets() []string      { return nil }
func (p settingsProvider) AutoRefreshLogs() bool          { return true }
func (p settingsProvider) DefaultImageSize() string       { return "1K" }
func (p settingsProvider) DefaultResponseModalities() string { return "图文" }
func (p settingsProvider) TelemetryEnabled() *bool        { return nil }
func (p settingsProvider) BaseModels() []string           { return engineconfig.BaseModels() }
func (p settingsProvider) ModelRegistry() []engineconfig.ModelEntry {
	return engineconfig.ModelRegistry()
}
func (p settingsProvider) AliasMap() map[string]string { return engineconfig.AliasMap() }
func (p settingsProvider) ResolveModelName(s string) string {
	return engineconfig.ResolveModelName(s)
}
func (p settingsProvider) LookupModel(s string) (engineconfig.ModelEntry, bool) {
	return engineconfig.LookupModel(s)
}
func (p settingsProvider) ConfigDir() string  { return "" }
func (p settingsProvider) ConfigPath() string { return "" }
