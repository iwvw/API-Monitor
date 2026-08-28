package config

// ConfigProvider 是引擎消费的配置门面。嵌入版由 openaibeta.Service 的
// DB 设置实现，避免原版 config.json/models.json 文件 IO。
type ConfigProvider interface {
	PortAPI() int
	MaxRetries() int
	AdminPassword() string
	ProxyURL() string
	DebugPprof() bool
	DebugMode() bool

	DropMaxTokens() bool

	AggregateStream() bool
	MaxN() int
	MaxRequestMB() int
	MaxSpillMB() int
	RequestTimeout() int
	RaceTimeout() int
	StreamIdleTimeoutSeconds() int
	ModelTurnGuardEnabled() bool

	VertexAPIKey() string
	CountTokensQuerySignature() string

	SafetySettings() map[string]string

	ParallelPoolEnabled() bool
	StickyNodePriority() bool
	ParallelPoolRetryEnabled() bool
	ParallelPoolSize() int
	ParallelPoolDelayDynamic() bool
	ParallelPoolDelayMs() int
	ActiveNodeURI() string
	ParallelNodeTopK() int

	BackgroundImage() string
	FontSize() string
	FontColorType() string
	FontColor() string
	CustomBgPresets() []string
	AutoRefreshLogs() bool
	DefaultImageSize() string
	DefaultResponseModalities() string

	TelemetryEnabled() *bool

	BaseModels() []string
	ModelRegistry() []ModelEntry
	AliasMap() map[string]string
	ResolveModelName(string) string
	LookupModel(string) (ModelEntry, bool)

	ConfigDir() string
	ConfigPath() string
}

type ConfigWriter interface {
	WriteSettings(map[string]any) error
	WriteModels([]string, map[string]string) error
}
