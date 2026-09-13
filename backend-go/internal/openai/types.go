package openai

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

type HeaderItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Endpoint struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	BaseURL        string       `json:"baseUrl"`
	APIKey         string       `json:"apiKey"`
	APIKeys        []string     `json:"apiKeys,omitempty"`
	Notes          string       `json:"notes"`
	Status         string       `json:"status"`
	Enabled        bool         `json:"enabled"`
	Models         []string     `json:"models"`
	Headers        []HeaderItem `json:"headers,omitempty"`
	DisabledModels []string     `json:"disabledModels,omitempty"`
	ProxyPool      []string     `json:"proxyPool,omitempty"`
	ProxyBatches   []ProxyBatch `json:"proxyBatches,omitempty"`
	ProxyEnabled   bool         `json:"proxyEnabled"`
	AutoSwitch     bool         `json:"autoSwitch"`
	ForceProxy     bool         `json:"forceProxy"`
	Protocol       string       `json:"protocol,omitempty"`
	// UpstreamType 是端点上游协议类型：空/""/"openai" 表示 OpenAI 兼容上游（默认），
	// "gemini" 表示 Google AI Studio（Generative Language API Interactions API）上游。
	UpstreamType  string            `json:"upstreamType,omitempty"`
	ModelMappings map[string]string `json:"modelMappings,omitempty"`
	// ModelsURL 覆盖模型列表拉取地址（默认 {baseURL}/models）。用于模型列表不在
	// 标准 /models 路径的上游（如 Cline 的 /recommended-models 独立端点）。
	ModelsURL string `json:"modelsUrl,omitempty"`
	// Pricing 保存上游 /models 接口返回的模型定价（按模型 id 索引），用于按量计费。
	Pricing PricingMap `json:"pricing,omitempty"`
	// RateLimitRetryEnabled 开启「429 等待重试」：收到 429/439 后在 Retry-After
	// （缺省 RateLimitRetryWaitSeconds 秒）内等待配额恢复并重试，适用低 RPM 端点。
	RateLimitRetryEnabled bool `json:"rateLimitRetryEnabled"`
	// RateLimitRetryWaitSeconds 是无 Retry-After 响应头时的缺省等待秒数。
	RateLimitRetryWaitSeconds int `json:"rateLimitRetryWaitSeconds"`
	// KeyRetryRounds 是单个请求内每个 API Key 最多可被尝试的次数（默认 2）。
	// 多 key 轮询时，每个 key 在「同一请求」内最多被尝试 KeyRetryRounds 次
	// （0/负值回退默认 2），而不是只试一次即换端点。单 key 端点不受影响（仍只试一次）。
	KeyRetryRounds int `json:"keyRetryRounds,omitempty"`
	// Priority 是端点优先级档位：值越大越优先被选中（同模型多端点时先高优先级）。
	// Weight 是同档位内的加权因子：值越大在该档位内被选中的概率越高。
	Priority     int     `json:"priority,omitempty"`
	Weight       int     `json:"weight,omitempty"`
	CreatedAt    string  `json:"createdAt"`
	LastUsed     *string `json:"lastUsed"`
	LastChecked  *string `json:"lastChecked"`
	HealthStatus string  `json:"healthStatus,omitempty"`
	// PluginID 标记该端点由哪个插件注册（如独立插件），普通端点为空。
	PluginID string `json:"pluginId,omitempty"`
	// ProxyPoolID 引用独立代理池插件（/api/proxypool）中的池；非空时转发出口
	// 从该池选择（忽略内联 proxyPool）。用于让网关端点复用插件管理的代理池。
	ProxyPoolID string `json:"proxyPoolId,omitempty"`
}

// ProxyBatch 是一次文件/文本导入形成的代理批次，便于按来源批量管理大代理池。
// Proxies 只记录该批次新增（导入时池中尚不存在）的代理；proxy_pool 是全部
// 批次的并集与手动添加代理的展开结果，运行时只消费 proxy_pool。
type ProxyBatch struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	CreatedAt string   `json:"createdAt"`
	Proxies   []string `json:"proxies"`
}

type HealthRecord struct {
	Model      string `json:"model"`
	Status     string `json:"status"`
	Latency    int64  `json:"latency"`
	StatusCode int    `json:"statusCode"`
	Error      string `json:"error,omitempty"`
	CheckedAt  string `json:"checkedAt"`
}

type HealthSummary struct {
	TotalModels   int            `json:"totalModels"`
	Operational   int            `json:"operational"`
	Degraded      int            `json:"degraded"`
	Failed        int            `json:"failed"`
	OverallStatus string         `json:"overallStatus"`
	Results       []HealthRecord `json:"results"`
	CheckedAt     string         `json:"checkedAt"`
}

// ProxyPoolSelector 供 openai 网关复用独立代理池插件（proxypool）的选择能力。
// 由 server 注入实现，避免包级循环依赖。
type ProxyPoolSelector interface {
	SelectProxy(ctx context.Context, poolID, sessionKey string) (string, error)
	ReportResult(ctx context.Context, poolID, proxy string, ok, ratelimit bool, retryAfter *time.Duration) error
}

// Notifier 是 openai 网关向外部通知系统上报告警的最小接口。
// 各模块自行声明同构接口，避免包级循环依赖；由 server 注入实现。
type Notifier interface {
	Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error
}

// gatewayAlertState 记录网关健康告警的去重边沿状态。
type gatewayAlertState struct {
	// lastProxyFrozenAt 记录最近一次「代理池全冻结」告警时间，避免反复刷屏。
	lastProxyFrozenAt time.Time
	// errorRateHigh 标记当前是否处于「错误率过高」告警态（触发->恢复成对）。
	errorRateHigh bool
}

// relayErrorBufferSize 是转发失败事件环形缓冲的上限。
const relayErrorBufferSize = 200

// relayErrorBodyLimit 是上游错误响应体写入日志/缓冲的最大字符数。
const relayErrorBodyLimit = 300

// relayErrorResponseLimit 是报错 JSON（错误响应体）写入调用日志的最大字符数。
// 仅在请求失败（状态码 >= 400）时记录，成功请求不占空间。
const relayErrorResponseLimit = 65536

// relayErrorResponseRetention 是保留报错 JSON 的失败记录条数（按时间最新）。
// 超出部分自动清空 response_body，只留下调用日志行与统计，控制表体积。
const relayErrorResponseRetention = 50

// RelayErrorRecord 是一条推理转发失败事件的明细记录。
// Proxy 只存放脱敏后的 host:port，绝不包含代理 URL 中的凭据。
// KeyIndex 是本次请求使用的 API Key 序号（0 = 主 key），用于日志定位。
type RelayErrorRecord struct {
	Time  time.Time `json:"time"`
	Route string    `json:"route"`
	Kind  string    `json:"kind"`
	// Outcome 是失败结果的粗粒度分类（对齐 opencode2api 上游账本语义）：
	//   transport_error  — 传输层失败（dial/首字或响应头超时/流中断）
	//   retryable_failure — 上游可重试错误（429/5xx/限流）
	//   rejected         — 上游或网关确定性拒绝（no_endpoint/blocked/bad_request/
	//                      配置错误等，重试无意义）
	// 成功请求不产生记录（relay-errors 只承载失败）。
	Outcome    string `json:"outcome,omitempty"`
	Endpoint   string `json:"endpoint"`
	EndpointID string `json:"endpointId"`
	KeyIndex   int    `json:"keyIndex,omitempty"`
	Model      string `json:"model"`
	Stream     bool   `json:"stream"`
	Proxy      string `json:"proxy"`
	ClientIP   string `json:"clientIp"`
	Attempts   int    `json:"attempts"`
	ElapsedMs  int64  `json:"elapsedMs"`
	StatusCode int    `json:"statusCode,omitempty"`
	Upstream   string `json:"upstream,omitempty"`
	Error      string `json:"error"`
}

type endpointProxyState struct {
	cursor   int
	cooldown map[string]time.Time
	// failures 记录每个代理的连续失败次数，驱动指数退避冷却（1min << min(f-1, 5)）。
	failures map[string]int
	// sessionBindings 记录会话与代理的粘性绑定与请求计数，达到 sessionProxyRequestLimit 后轮换。
	sessionBindings map[string]*sessionBinding
	// lastTTFB 记录每个代理最近一次请求的首字耗时（毫秒），用于择优选择延迟最低的代理。
	// 尚未产生记录的代理按 cursor 轮询，保证首次使用可测出延迟。
	lastTTFB map[string]int64
	// rate429 记录每个代理累计的上游 429 次数；达到 proxy429BanThreshold 后
	// 触发 rateLimited 禁用（IP 级限流时该出口已被上游限死，继续选择只会反复 429）。
	rate429 map[string]int
	// rateLimited 记录因累计 429 被临时禁用的代理及解禁时间；到期自动释放回池。
	rateLimited map[string]time.Time
	// sunk 记录因连续失败被判定为「坏代理」的沉淀标记及沉淀到期时间；沉淀期内
	// 不参与选择，到期自动放回（暂态故障恢复后可重新加入）。
	sunk map[string]time.Time
	// lastExitIP 记录每个代理最近一次探活拿到的出口公网 IP（经代理出网），
	// 用于前端排查「代理能用但出口被封 / 出口雷同」等代理级问题。
	lastExitIP map[string]string
	// lastProbeAt 记录每个代理最近一次成功探活的时间（UTC），前端展示探活新鲜度。
	lastProbeAt map[string]time.Time
	// lastAllFrozenLog 记录最近一次「全部出口冻结回退直连」的告警时间，用于节流日志。
	lastAllFrozenLog time.Time
	// lastAllUnfrozen 记录最近一次「全部出口禁用时自动解冻全体代理」的时间，
	// 用于节流：防止上游限流未恢复时出现「解冻→再全部冻结→又解冻」的限流风暴。
	lastAllUnfrozen time.Time
	// activeProxy 记录该端点最近一次成功转发的代理（池级粘性出口）。
	// 不带会话 ID 的请求优先复用它：有效就一直用，直到被冷却/429 冻结/沉淀才换，
	// 减少每请求换 IP 的冷启动与随机撞限。不持久化（进程重启后重新学习）。
	activeProxy string
}

// sessionBinding 是某会话在某出口 IP（代理）上的粘性绑定。
// count 为该代理已承载的请求数；超过 sessionProxyRequestLimit 后换新代理并重置。
// updatedAt 为最近一次使用时间，驱动空闲过期清理（见 sessionBindingTTL）。
type sessionBinding struct {
	proxy     string
	count     int
	updatedAt time.Time
}

// endpointKeyState 记录端点多 API Key 的轮询游标。
// 设计语义（与代理池区分）：
//   - key 在任何时候都不冻结：401/403、连接失败都不会让 key 进入跨请求冻结状态，
//     只在本请求内对已尝试失败的 key 去重，保证每个 key 在每次请求都有机会被尝试。
//   - 429 是「限速」不是「故障」：单次 429 只切换下一个 key 分担 RPM，绝不冻结。
//   - 仅当本轮全部 key 均已尝试仍失败时才触发端点级切换（见 relayLoop 端点循环）。
type endpointKeyState struct {
	cursor int
	// health 记录每个 API Key 的独立健康状态（连续失败/最近失败原因/时间）。
	// key 永不冻结，健康信息仅供排障展示与前端展示。
	health map[string]*keyHealthEntry
}

// relayLoopParams 描述一次上游转发重试循环的输入。chat.completions / responses /
// messages 三个转发入口共用同一重试语义：代理择优 → 限流/5xx 自动切换 → 首字超时
// 轮换 → 429 累计熔断。正文改写、响应写回与 token 统计等差异留在调用方。
// endpoints 为候选端点列表（按 sort_order 排序），用于端点级 failover。
type relayLoopParams struct {
	route          string // 统计与日志路由名
	ctx            context.Context
	db             *sql.DB
	selected       Endpoint
	endpoints      []Endpoint
	model          string
	realModel      string // 命中模型映射时的上游真实模型名（model 为对外别名），随日志落库
	fullURL        string
	body           []byte
	stream         bool
	sessionKey     string
	clientIP       string
	requestStarted time.Time
}

// relayLoopResult relayLoop 的结果。resp 非 nil 表示已拿到上游响应，调用方直接消费
// 正文；resp 为 nil 时 lastErr 携带失败原因，statusCode 为应回给客户端的状态码
// （500=构建请求失败，502=代理重试耗尽/配置错误）。cancel 为成功路径最近一次尝试的
// context 取消函数，调用方在读完正文（或关闭 resp.Body）后调用以释放 attempt context。
// 停止条件：resp 非空（成功拿到上游响应）或 endpointExhausted（本轮全部 key 尝试
// 失败）或 retryableUpstream（上游返回限流/5xx 且代理重试耗尽）。后两者都应切换到
// 下一个候选端点继续尝试，保证「尽最大可能提供可用渠道」。
type relayLoopResult struct {
	resp              *http.Response
	statusCode        int
	realModel         string // 本候选解析出的上游真实模型名（透传给调用方落库）
	lastErr           error
	endpointExhausted bool // 本轮全部 API Key 尝试失败，应切换到下一个候选端点
	retryableUpstream bool // 上游返回 429/5xx 且代理重试耗尽，应切换到下一个候选端点
	clientCancelled   bool // 客户端已断开（请求上下文被取消）：应静默收尾，不记账、不回写错误
	firstChunk        []byte
	firstWritten      bool
	ttfbMs            int64
	lastProxy         string
	lastKeyIndex      int
	stepKeyIndex      int // 本候选最近一次尝试实际使用的 key 序号（含失败尝试），供 failover 路径每步展示
	attempt           int
	egressIP          string // 请求实际从哪个出口/代理发出（随循环内选中的代理更新）
	startTime         time.Time
	cancel            context.CancelFunc
	retryBody         []byte // 最近一次 429 的响应体，供 rateLimitRetryWaitFor 解析 Google RetryInfo 延迟
}
