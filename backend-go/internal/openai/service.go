package openai

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

const (
	degradedThreshold        = 3 * time.Second
	healthTimeoutDefault     = 3 * time.Second
	healthConcurrencyDefault = 30
	healthConcurrencyMax     = 200
	// firstTokenTimeout 是流式请求等待首个字节的上限。超时后视为该代理出网链路
	// 不可用，网关会取消当前连接并切换下一个代理（仅在代理池+自动切换启用时生效）。
	// 值需平衡：过高会让慢代理拖长重试（客户端断开回 502），过低会误伤正常的
	// 慢心智模型首字。取 10s，把单次等待减半，同时留给推理首字足够余量。
	firstTokenTimeout = 10 * time.Second
	// streamWriteDeadline 是流式响应的写超时窗口。http.Server 的 WriteTimeout
	// 覆盖整个响应写入时间，长流式对话可能远超该值；每次写前延长 deadline，
	// 使长对话不被 WriteTimeout 掐断，同时保留慢客户端保护（长时间无进展才断）。
	streamWriteDeadline = 5 * time.Minute
	// usageTailLimit 是流式响应尾部保留的最大字节数：usage 信息总在最后一个
	// SSE chunk / response.completed 事件里，只保留尾部即可，避免长对话把整个
	// 流式响应累积在内存中。chat.completions 与 responses 两个流式入口共用。
	usageTailLimit = 64 * 1024
	// streamIdleTimeout 是流式中段空闲保护：上游超过该时长没有新字节（非结束）
	// 即终止流，防止上游停滞时请求无限挂死。正常模型输出不会连续 90s 无字节；
	// 需要长静默的本地推理场景可在端点配置中选用更长超时。
	streamIdleTimeout = 90 * time.Second
	// gatewayBodyDefaultMaxBytes 是网关请求体上限的兜底默认值（未配置
	// GATEWAY_BODY_MAX_MB 时)：全量读入内存（读 body → parsedBody map →
	// 转发体 bytes，约 3 倍峰值），小内存主机上必须封顶，超大（异常/恶意）
	// 请求体直接 413 拒绝，避免瞬时内存尖峰触发 OOM。
	gatewayBodyDefaultMaxBytes = 16 * 1024 * 1024
)

// gatewayBodyLimitBytes 返回当前生效的请求体上限（配置优先，零值回退默认）。
func (s *Service) gatewayBodyLimitBytes() int64 {
	if s.bodyMaxBytes > 0 {
		return s.bodyMaxBytes
	}
	return gatewayBodyDefaultMaxBytes
}

// 热路径正则预编译：chat completion 每请求都会用到，避免逐请求编译。
var (
	localURLRegex         = regexp.MustCompile(`(?i)^https?://(localhost|127\.0\.0\.1|0\.0\.0\.0|192\.168\.|10\.|172\.(1[6-9]|2[0-9]|3[0-1])\.)`)
	promptTokensRegex     = regexp.MustCompile(`"prompt_tokens"\s*:\s*(\d+)`)
	completionTokensRegex = regexp.MustCompile(`"completion_tokens"\s*:\s*(\d+)`)
	totalTokensRegex      = regexp.MustCompile(`"total_tokens"\s*:\s*(\d+)`)
	cachedTokensRegex     = regexp.MustCompile(`"cached_tokens"\s*:\s*(\d+)`)
	// Responses API 的 usage 字段名为 input_tokens/output_tokens（对应 chat 的
	// prompt/completion），缓存与推理分开明细。流式场景 usage 出现在最后的
	// response.completed 事件里，同样用尾部正则提取。
	inputTokensRegex  = regexp.MustCompile(`"input_tokens"\s*:\s*(\d+)`)
	outputTokensRegex = regexp.MustCompile(`"output_tokens"\s*:\s*(\d+)`)
	versionPathRegex  = regexp.MustCompile(`(?i)/v\d+/?`)
)

type HeaderItem struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Endpoint struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	BaseURL        string            `json:"baseUrl"`
	APIKey         string            `json:"apiKey"`
	APIKeys        []string          `json:"apiKeys,omitempty"`
	Notes          string            `json:"notes"`
	Status         string            `json:"status"`
	Enabled        bool              `json:"enabled"`
	Models         []string          `json:"models"`
	Headers        []HeaderItem      `json:"headers,omitempty"`
	DisabledModels []string          `json:"disabledModels,omitempty"`
	ProxyPool      []string          `json:"proxyPool,omitempty"`
	ProxyBatches   []ProxyBatch      `json:"proxyBatches,omitempty"`
	ProxyEnabled   bool              `json:"proxyEnabled"`
	AutoSwitch     bool              `json:"autoSwitch"`
	ForceProxy     bool              `json:"forceProxy"`
	Protocol       string            `json:"protocol,omitempty"`
	// UpstreamType 是端点上游协议类型：空/""/"openai" 表示 OpenAI 兼容上游（默认），
	// "gemini" 表示 Google AI Studio（Generative Language API Interactions API）上游。
	UpstreamType   string            `json:"upstreamType,omitempty"`
	ModelMappings  map[string]string `json:"modelMappings,omitempty"`
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

// defaultKeyRetryRounds 是端点未显式配置时每个 key 在单请求内可被尝试的轮次。
const defaultKeyRetryRounds = 2

// effectiveKeyRetryRounds 返回端点的每 key 单请求尝试次数（0/负值回退默认 2）。
func (ep Endpoint) effectiveKeyRetryRounds() int {
	if ep.KeyRetryRounds >= 1 {
		return ep.KeyRetryRounds
	}
	return defaultKeyRetryRounds
}

// AllKeys 返回端点全部可用 API Key（主 key + 扩展 key，去重）。
func (ep Endpoint) AllKeys() []string {
	seen := map[string]bool{ep.APIKey: true}
	keys := []string{ep.APIKey}
	for _, k := range ep.APIKeys {
		k = strings.TrimSpace(k)
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	return keys
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

type Service struct {
	cfg        config.Config
	store      *database.Store
	client     *http.Client
	apiKeys    *apikeys.Manager
	schemaOnce sync.Once
	schemaErr  error

	// protocolClients 按连接协议（auto/h2/http1）缓存的直连客户端，
	// 保证同一端点的协议设置能复用连接池（keep-alive / h2 流复用）。
	protocolMu      sync.Mutex
	protocolClients map[string]*http.Client

	// proxyStateByEndpoint 记录每个端点的代理池运行时状态（当前游标、冷却中的代理）。
	proxyMu              sync.Mutex
	proxyStateByEndpoint map[string]*endpointProxyState

	// keyStateByEndpoint 记录每个端点的多 API Key 运行时状态（轮询游标、冻结/冷却）。
	keyMu              sync.Mutex
	keyStateByEndpoint map[string]*endpointKeyState

	// endpointLatency 记录每个端点最近一次转发的响应延迟（毫秒），
	// 供多端点同模型时的延迟加权分流（健康快的端点被选中概率更高）。
	latencyMu         sync.RWMutex
	endpointLatency   map[string]int64
	endpointLatencyOK map[string]bool

	// analyticsStreams 是网关日志实时推送的订阅者集合（SSE）。
	analyticsStreamMu   sync.Mutex
	analyticsStreams    map[int]chan map[string]interface{}
	analyticsStreamNext int

	// analyticsQueue 是网关调用日志的异步落库队列：请求路径只投递，
	// 由常驻 worker 批量写入，避免高流量下每次请求都在请求线程内
	// 同步 INSERT 造成写锁竞争。队列满时丢弃并计数（日志页短暂缺行）。
	analyticsQueue chan analyticsWriteItem
	// analyticsStartMu/started/done 管理常驻 worker 的生命周期：
	// 首条记录或显式 Shutdown 时启动，Shutdown 关闭队列后等 worker 退出，
	// 避免测试 TempDir 清理时后台线程仍占用 SQLite 文件。
	analyticsStartMu sync.Mutex
	analyticsStarted bool
	analyticsDone    chan struct{}
	shutdownOnce     sync.Once
	analyticsDrop    atomic.Uint64

	// relayErrors 是推理转发失败事件的环形缓冲，供排障接口与详细日志排查。
	relayErrMu  sync.Mutex
	relayErrors []RelayErrorRecord

	// routeCache 缓存「已启用端点配置」列表（模型/映射/代理池等），避免每次转发
	// 请求都全表扫描 + JSON 解析。短 TTL 到期自动重建，端点配置变更最多延迟
	// routeCacheTTL 生效，无需逐处失效。
	routeCacheMu    sync.Mutex
	routeCache      []Endpoint
	routeCacheAt    time.Time
	routeCacheReady bool

	// bodyMaxBytes 是网关请求体上限（来自配置 GATEWAY_BODY_MAX_MB，
	// 零值时回退 gatewayBodyDefaultMaxBytes，兼容测试直构 Service 的空配置）。
	bodyMaxBytes int64

	// routeModelIndex 是「模型名 → 候选端点下标」的内存倒排索引，随 routeCache
	// 一并重建，避免每次转发请求都遍历全部端点做模型匹配（Ability 物化索引）。
	// key 同时收录端点自身模型与 modelMappings 的别名，命中即能服务该模型。
	routeModelIndex map[string][]int

	// channelAffinity 记录会话键（X-*Session-ID / user 字段）最近一次成功使用的
	// 端点。后续同一会话的请求优先复用该端点（命中上游上下文缓存），失败后由
	// failover 正常换端；记录带 TTL，超时自动遗忘避免粘死在坏端点上。
	affinityMu      sync.Mutex
	channelAffinity map[string]channelAffinityEntry

	// warmupOnce 保护预热 goroutine 只启动一次。
	warmupOnce sync.Once

	// notifier 用于上报网关健康/配额告警（由 server 注入；nil 时静默忽略）。
	notifier Notifier
	// alertOnce 保护告警监测 goroutine 只启动一次。
	alertOnce sync.Once
	// modelRefreshOnce 保护上游模型自动刷新 goroutine 只启动一次。
	modelRefreshOnce sync.Once
	// alertState 记录告警边沿状态，避免同状态反复触发通知。
	alertMu    sync.Mutex
	alertState gatewayAlertState

	// externalPool 是独立代理池插件（proxypool）的选择器；由 server 注入。
	// 端点在配置了 proxy_pool_id 时，转发出口经它选择（复用插件健康数据）。
	externalPool ProxyPoolSelector
}

// ProxyPoolSelector 供 openai 网关复用独立代理池插件（proxypool）的选择能力。
// 由 server 注入实现，避免包级循环依赖。
type ProxyPoolSelector interface {
	SelectProxy(ctx context.Context, poolID, sessionKey string) (string, error)
	ReportResult(ctx context.Context, poolID, proxy string, ok, ratelimit bool, retryAfter *time.Duration) error
}

// SetProxyPoolSelector 注入独立代理池选择器（server 组装时调用）。
func (s *Service) SetProxyPoolSelector(sel ProxyPoolSelector) {
	s.externalPool = sel
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

func newEndpointKeyState() *endpointKeyState {
	return &endpointKeyState{}
}

// safeUploadPathJoin 将 /uploads/ 开头的图片 URL 解析为 DataDir/uploads 内的绝对路径。
// 返回 false 表示路径穿越或越界（拒绝内联），防止读取 DataDir 之外的任意文件。
func safeUploadPathJoin(dataDir, imgURL string) (string, bool) {
	if dataDir == "" || !strings.HasPrefix(imgURL, "/uploads/") {
		return "", false
	}
	uploadsRoot := filepath.Clean(filepath.Join(filepath.Clean(dataDir), "uploads"))
	joined := filepath.Clean(filepath.Join(uploadsRoot, strings.TrimPrefix(imgURL, "/uploads/")))
	expected := uploadsRoot
	if !strings.HasSuffix(expected, string(os.PathSeparator)) {
		expected += string(os.PathSeparator)
	}
	if joined != uploadsRoot && !strings.HasPrefix(joined, expected) {
		return "", false
	}
	return joined, true
}

func (s *Service) inlineLocalUploadImage(imgURLMap map[string]interface{}, dataDir string) {
	imgURL, ok := imgURLMap["url"].(string)
	if !ok {
		return
	}
	filePath, ok := safeUploadPathJoin(dataDir, imgURL)
	if !ok {
		return
	}
	if fileBytes, err := os.ReadFile(filePath); err == nil {
		ext := strings.ToLower(filepath.Ext(filePath))
		mimeType := "image/jpeg"
		switch ext {
		case ".png":
			mimeType = "image/png"
		case ".webp":
			mimeType = "image/webp"
		case ".gif":
			mimeType = "image/gif"
		}
		b64 := base64.StdEncoding.EncodeToString(fileBytes)
		imgURLMap["url"] = fmt.Sprintf("data:%s;base64,%s", mimeType, b64)
		imgURLMap["_original_url"] = imgURL
	}
}

func New(cfg config.Config) *Service {
	tr := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   4 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          500,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// 兜底限制「等待响应头」的时间；快速切换由 headerTimeoutPerAttempt 在转发循环内控制，
		// 故此处放宽到 180s，避免误杀「慢但最终成功」的非流式请求（推理模型思考阶段可能超过 60s），
		// 也不限制流式响应体时长。
		ResponseHeaderTimeout: 180 * time.Second,
	}
	s := &Service{
		cfg:                  cfg,
		store:                database.New(cfg),
		client:               &http.Client{Transport: tr},
		apiKeys:              apikeys.New(cfg),
		bodyMaxBytes:         cfg.GatewayBodyMaxBytes,
		protocolClients:      map[string]*http.Client{},
		proxyStateByEndpoint: make(map[string]*endpointProxyState),
		keyStateByEndpoint:   make(map[string]*endpointKeyState),
		endpointLatency:      make(map[string]int64),
		endpointLatencyOK:    make(map[string]bool),
		analyticsStreams:     make(map[int]chan map[string]interface{}),
		analyticsQueue:       make(chan analyticsWriteItem, analyticsQueueSize),
		analyticsDone:        make(chan struct{}),
		relayErrors:          make([]RelayErrorRecord, 0, relayErrorBufferSize),
		routeModelIndex:      make(map[string][]int),
		channelAffinity:      make(map[string]channelAffinityEntry),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if db, err := s.open(ctx); err == nil {
		s.loadProxyState(ctx, db)
		db.Close()
	}
	return s
}

// warmupInterval 是代理池预热的保活周期。
const warmupInterval = 10 * time.Minute

// SetNotifier 注入告警通知器（由 server 组装时调用）。notifier 为 nil 时告警监测静默跳过。
func (s *Service) SetNotifier(notifier Notifier) {
	s.notifier = notifier
}

// StartWarmup 在后台周期性地对启用了代理池的端点发起轻量 /models 请求，
// 预建立 SOCKS5/TLS 连接并复用连接池，避免首次请求承受完整的冷启动握手。
// 进程结束前保持运行；端点或代理池为空时自动跳过。
func (s *Service) StartWarmup(ctx context.Context) {
	s.warmupOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(warmupInterval)
			defer ticker.Stop()
			s.warmupOnceNow(ctx)
			for {
				select {
				case <-ticker.C:
					s.warmupOnceNow(ctx)
				case <-ctx.Done():
					return
				}
			}
		}()
	})
}

// StartAlertMonitor 在后台周期性评估网关健康并触发通知告警：
//   - 网关错误率过高（最近 10 分钟 ≥50% 且样本 ≥20）：触发 gateway_error_high，
//     恢复（<25%）时触发 gateway_error_normal（成对事件便于通知生命周期）。
//
// 由 server 在启动 openai 服务后调用；notifier 未注入时静默跳过。
// 错误率按开表计算，避免引入运行时计数器带来的并发复杂度。
func (s *Service) StartAlertMonitor(ctx context.Context) {
	s.alertOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(gatewayAlertInterval)
			defer ticker.Stop()
			s.alertOnceNow(ctx)
			for {
				select {
				case <-ticker.C:
					s.alertOnceNow(ctx)
				case <-ctx.Done():
					return
				}
			}
		}()
	})
}

// modelAutoRefreshInterval 是上游模型列表自动刷新的周期。后台默认开启、无需前端
// 展示：每隔一小时重取所有启用端点的 /v1/models 并写库，让上游新增模型自动可用。
const modelAutoRefreshInterval = time.Hour

// modelAutoRefreshCycleTimeout 是单轮自动刷新的整体预算：超时即中止，不阻塞下一轮。
const modelAutoRefreshCycleTimeout = 6 * time.Minute

// StartModelAutoRefresh 在后台每小时刷新一次所有启用端点的上游模型列表。
// 默认开启、无前端开关；端点验证/取模型失败时保留旧模型列表（对齐刷新路由语义）。
// 进程结束（ctx 取消）时退出。
func (s *Service) StartModelAutoRefresh(ctx context.Context) {
	s.modelRefreshOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(modelAutoRefreshInterval)
			defer ticker.Stop()
			s.modelAutoRefreshOnceNow(ctx)
			for {
				select {
				case <-ticker.C:
					s.modelAutoRefreshOnceNow(ctx)
				case <-ctx.Done():
					return
				}
			}
		}()
	})
}

// modelAutoRefreshOnceNow 执行一轮上游模型列表刷新，并在轮询间隙记录失败数（不打扰通知）。
func (s *Service) modelAutoRefreshOnceNow(ctx context.Context) {
	cycleCtx, cancel := context.WithTimeout(ctx, modelAutoRefreshCycleTimeout)
	defer cancel()
	results, _ := s.refreshAllModels(cycleCtx)
	s.invalidateRouteCache()
	if len(results) == 0 {
		return
	}
	failed := 0
	for _, r := range results {
		if ok, _ := r["success"].(bool); !ok {
			failed++
		}
	}
	applog.Info(cycleCtx, "openai", "model auto refresh finished", "endpoints", len(results), "failed", failed)
}

// gatewayAlertInterval 是网关健康告警的评估周期。
const gatewayAlertInterval = 5 * time.Minute

// gatewayErrorRateHigh / Normal 是错误率告警的触发与恢复阈值（百分比）。
const (
	gatewayErrorRateHigh   = 50
	gatewayErrorRateNormal = 25
)

// gatewayErrorSampleMin 是错误率判定所需的最少样本数，避免冷启动误报。
const gatewayErrorSampleMin = 20

// alertOnceNow 执行一次网关健康告警评估。
func (s *Service) alertOnceNow(ctx context.Context) {
	if s.notifier == nil {
		return
	}
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	since := time.Now().UTC().Add(-10 * time.Minute).Format("2006-01-02 15:04:05")
	var total, errors int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END), 0)
		FROM openai_gateway_analytics WHERE timestamp >= ? AND route != 'models'`, since).Scan(&total, &errors)
	if err != nil {
		return
	}

	s.alertMu.Lock()
	defer s.alertMu.Unlock()
	high := total >= gatewayErrorSampleMin && float64(errors)/float64(total)*100 >= gatewayErrorRateHigh
	if high && !s.alertState.errorRateHigh {
		s.alertState.errorRateHigh = true
		s.triggerGatewayAlert(ctx, "gateway_error_high", map[string]interface{}{
			"requests":   total,
			"errors":     errors,
			"error_rate": float64(errors) / float64(total) * 100,
			"windowMin":  10,
			"event_type": "gateway_error_high",
		})
	} else if !high && s.alertState.errorRateHigh {
		s.alertState.errorRateHigh = false
		s.triggerGatewayAlert(ctx, "gateway_error_normal", map[string]interface{}{
			"requests":   total,
			"errors":     errors,
			"error_rate": float64(errors) / float64(total) * 100,
			"event_type": "gateway_error_normal",
		})
	}
}

// triggerGatewayAlert 向通知系统触发 openai 模块告警；发送失败仅记日志。
func (s *Service) triggerGatewayAlert(ctx context.Context, eventType string, data map[string]interface{}) {
	if s.notifier == nil {
		return
	}
	triggerCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.notifier.Trigger(triggerCtx, "openai", eventType, data); err != nil {
		applog.Warn(triggerCtx, "openai", "failed to trigger gateway alert",
			"event_type", eventType,
			"error", err.Error(),
		)
	}
}

// warmupOnceNow 对每个启用了代理池的端点，尝试通过每个代理建连一次。
func (s *Service) warmupOnceNow(ctx context.Context) {
	db, err := s.open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT id, base_url, models_url, api_key, proxy_pool
		FROM openai_endpoints WHERE enabled = 1 AND proxy_pool IS NOT NULL AND proxy_pool != ''`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, baseURL, apiKey string
		var modelsURLRaw sql.NullString
		var proxyRaw string
		if err := rows.Scan(&id, &baseURL, &modelsURLRaw, &apiKey, &proxyRaw); err != nil {
			continue
		}
		modelsURL := modelsURLRaw.String
		var pool []string
		if err := json.Unmarshal([]byte(proxyRaw), &pool); err != nil {
			continue
		}
		pool = cleanProxyPool(pool)
		if len(pool) == 0 {
			continue
		}
		for _, proxyURL := range pool {
			if ctx.Err() != nil {
				return
			}
			s.warmProxyConnection(ctx, id, baseURL, modelsURL, apiKey, proxyURL)
		}
	}
}

// warmProxyConnection 通过指定代理向端点的 models 地址发起一次请求，触发连接建立，
// 并兼作探活：链路可达（拿到任意响应）清除该代理的失败计数与冷却，
// 连接失败则按失败计数冷却代理（与 markProxyFailed 指数退避一致）。
func (s *Service) warmProxyConnection(ctx context.Context, endpointID, baseURL, modelsURL, apiKey, proxyURL string) {
	client, err := s.proxyClient(proxyURL)
	if err != nil {
		return
	}
	start := time.Now()
	fullURL := modelListURL(baseURL, modelsURL)

	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fullURL, nil)
	if err != nil {
		return
	}
	if apiKey != "" && apiKey != "public" {
		req.Header.Set("Authorization", "Bearer "+secure.SecureDecrypt(apiKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		// 链路不可达：按失败计数指数退避冷却（预热同样消费探活闭环）。
		s.markProxyFailed(endpointID, proxyURL)
		// 连续失败达到阈值则沉淀为坏代理（长期排除），避免坏代理反复拖累转发。
		if s.proxyFailCount(endpointID, proxyURL) >= proxySinkThreshold {
			s.sinkProxy(endpointID, proxyURL)
		}
		return
	}
	// 读取并关闭响应，让连接回到空闲连接池供后续复用；任意状态码都视为链路可达。
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
	resp.Body.Close()
	s.markProxySuccess(endpointID, proxyURL)
	s.unsinkProxy(endpointID, proxyURL)
	// 记录该代理到该端点的首字耗时，让池子速度过一次预热即可按延迟择优，
	// 首个真实请求不必再走未知延迟的探索轮询。
	s.recordProxyTTFB(endpointID, proxyURL, time.Since(start).Milliseconds())
	s.probeProxyExitIP(endpointID, proxyURL)
}

// proxyFailCount 返回端点下某代理的连续失败计数（供探活判定沉降阈值）。
func (s *Service) proxyFailCount(endpointID, proxy string) int {
	if proxy == "" {
		return 0
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	if state, ok := s.proxyStateByEndpoint[endpointID]; ok {
		return state.failures[proxy]
	}
	return 0
}

// probeProxyExitIP 经指定代理访问 ipify 获取该代理出口的公网 IP 并记录到
// 端点代理状态，供前端展示。失败静默忽略（仅为观测，不参与可用性判定）。
// 独立超时与短连接，避免探活拖长预热循环。
func (s *Service) probeProxyExitIP(endpointID, proxyURL string) {
	if endpointID == "" || proxyURL == "" {
		return
	}
	client, err := s.proxyClient(proxyURL)
	if err != nil {
		return
	}
	reqCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}
	ip := strings.TrimSpace(string(body))
	if ip == "" {
		return
	}
	s.proxyMu.Lock()
	if state, ok := s.proxyStateByEndpoint[endpointID]; ok {
		state.lastExitIP[proxyURL] = ip
		state.lastProbeAt[proxyURL] = time.Now()
	}
	s.proxyMu.Unlock()
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	// schema 幂等且启动后不变，进程内只执行一次，避免每次打开连接都重放 DDL。
	s.schemaOnce.Do(func() {
		s.schemaErr = ensureSchema(ctx, db)
	})
	if s.schemaErr != nil {
		db.Close()
		return nil, s.schemaErr
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS openai_endpoints (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL,
			headers TEXT,
			disabled_models TEXT,
			proxy_pool TEXT,
			proxy_batches TEXT,
			auto_switch INTEGER DEFAULT 0,
			proxy_enabled INTEGER DEFAULT 0,
			force_proxy INTEGER DEFAULT 0,
			rate_limit_retry_enabled INTEGER DEFAULT 1,
			rate_limit_retry_wait_seconds INTEGER DEFAULT 10,
			status TEXT DEFAULT 'unknown',
			enabled INTEGER DEFAULT 1,
			models TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME,
			last_checked DATETIME,
			sort_order INTEGER DEFAULT 0,
			priority INTEGER DEFAULT 0,
			weight INTEGER DEFAULT 100,
			key_retry_rounds INTEGER DEFAULT 2,
			upstream_type TEXT DEFAULT 'openai'
		)`,
		`CREATE TABLE IF NOT EXISTS openai_health_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			endpoint_id TEXT NOT NULL,
			status TEXT NOT NULL,
			response_time INTEGER,
			error_message TEXT,
			checked_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (endpoint_id) REFERENCES openai_endpoints(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS openai_endpoint_name_archive (
			endpoint_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			deleted_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_endpoints_status ON openai_endpoints(status)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_archive_endpoint ON openai_endpoint_name_archive(endpoint_id)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_health_endpoint ON openai_health_history(endpoint_id, checked_at)`,
		`CREATE TABLE IF NOT EXISTS openai_gateway_analytics (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			endpoint_id TEXT,
			gateway_key_id TEXT,
			route TEXT NOT NULL DEFAULT 'chat.completions',
			model TEXT NOT NULL,
			status_code INTEGER NOT NULL,
			latency_ms INTEGER NOT NULL,
			ttfb_ms INTEGER DEFAULT 0,
			prompt_tokens INTEGER DEFAULT 0,
			completion_tokens INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			cached_tokens INTEGER DEFAULT 0,
			client_ip TEXT,
			upstream_ip TEXT,
			stream INTEGER DEFAULT 0,
			via_proxy INTEGER DEFAULT 0,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS openai_gateway_keys (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_cipher TEXT,
			key_prefix TEXT NOT NULL,
			key_suffix TEXT NOT NULL,
			enabled INTEGER DEFAULT 1,
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME,
			expires_at DATETIME,
			request_count INTEGER DEFAULT 0,
			allowed_models TEXT,
			allowed_endpoints TEXT,
			max_tokens_quota INTEGER DEFAULT 0,
			total_tokens_used INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS openai_gateway_stats_hourly (
			hour TEXT NOT NULL,
			endpoint_id TEXT NOT NULL DEFAULT '',
			gateway_key_id TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			route TEXT NOT NULL DEFAULT '',
			requests INTEGER NOT NULL DEFAULT 0,
			errors INTEGER NOT NULL DEFAULT 0,
			latency_sum INTEGER NOT NULL DEFAULT 0,
			ttfb_sum INTEGER NOT NULL DEFAULT 0,
			ttfb_count INTEGER NOT NULL DEFAULT 0,
			prompt_tokens INTEGER NOT NULL DEFAULT 0,
			completion_tokens INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0,
			cached_tokens INTEGER NOT NULL DEFAULT 0,
			cost REAL NOT NULL DEFAULT 0,
			cost_currency TEXT NOT NULL DEFAULT '',
			PRIMARY KEY (hour, endpoint_id, gateway_key_id, model, route, cost_currency)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_analytics_timestamp ON openai_gateway_analytics(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_stats_hourly_hour ON openai_gateway_stats_hourly(hour)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_gateway_keys_hash ON openai_gateway_keys(key_hash)`,
		`CREATE TABLE IF NOT EXISTS openai_proxy_state (
			endpoint_id TEXT NOT NULL,
			proxy TEXT NOT NULL,
			kind TEXT NOT NULL,
			until DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (endpoint_id, proxy, kind)
		)`,
	}
	// 看板聚合表首次建表时，把存量网关调用日志聚合回填进 openai_gateway_stats_hourly，
	// 避免升级后看板历史空白。仅在建表当次执行一次：若用户后续手动清空了看板历史
	// （聚合表仍存在但为空），重启不会从日志表重新回填。
	statsTableExists := false
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM sqlite_master WHERE type='table' AND name='openai_gateway_stats_hourly'
	)`).Scan(&statsTableExists); err != nil {
		return fmt.Errorf("openai check stats table: %w", err)
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("openai ensure schema: %w", err)
		}
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "real_model", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "gateway_key_id", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "failover_path", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "response_body", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "error_kind", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "error_message", "TEXT"); err != nil {
		return err
	}
	// 错误明细保留清理（trimErrorDetailRetention）按 error_kind 定位待清空行：
	// 部分索引只覆盖有错误详情的行，避免失败请求落库时全表扫描。
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_openai_gateway_analytics_error_kind ON openai_gateway_analytics(error_kind) WHERE error_kind IS NOT NULL AND error_kind != ''`); err != nil {
		return fmt.Errorf("openai ensure schema error_kind index: %w", err)
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "route", "TEXT NOT NULL DEFAULT 'chat.completions'"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "ttfb_ms", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "cached_tokens", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "cost", "REAL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "cost_currency", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "client_ip", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "upstream_ip", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "stream", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "via_proxy", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_analytics", "key_index", "INTEGER DEFAULT -1"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_keys", "key_cipher", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_keys", "is_default", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_keys", "allowed_models", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_keys", "allowed_endpoints", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_keys", "max_tokens_quota", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_gateway_keys", "total_tokens_used", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "headers", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "pricing", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "disabled_models", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "proxy_pool", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "proxy_batches", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "auto_switch", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "model_mappings", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "sort_order", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "force_proxy", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "proxy_enabled", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "protocol", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "api_keys", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "priority", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "weight", "INTEGER DEFAULT 100"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "key_retry_rounds", "INTEGER DEFAULT 2"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "rate_limit_retry_enabled", "INTEGER DEFAULT 1"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "rate_limit_retry_wait_seconds", "INTEGER DEFAULT 10"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "models_url", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "plugin_id", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "proxy_pool_id", "TEXT"); err != nil {
		return err
	}
	if err := ensureSQLiteColumn(ctx, db, "openai_endpoints", "upstream_type", "TEXT"); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_openai_analytics_gateway_key ON openai_gateway_analytics(gateway_key_id, timestamp)`); err != nil {
		return fmt.Errorf("openai ensure schema: %w", err)
	}

	// 首次建表回填：从存量原始日志聚合出小时桶写入看板聚合表。
	// 只回填非 models 路由（与看板查询口径一致）。
	if !statsTableExists {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO openai_gateway_stats_hourly (hour, endpoint_id, gateway_key_id, model, route, requests, errors, latency_sum, ttfb_sum, ttfb_count, prompt_tokens, completion_tokens, total_tokens, cached_tokens, cost, cost_currency)
			SELECT
				strftime('%Y-%m-%d %H:00:00', timestamp) as hour,
				COALESCE(endpoint_id, ''),
				COALESCE(gateway_key_id, ''),
				COALESCE(model, ''),
				COALESCE(route, ''),
				COUNT(*),
				SUM(CASE WHEN status_code >= 400 THEN 1 ELSE 0 END),
				COALESCE(SUM(latency_ms), 0),
				COALESCE(SUM(CASE WHEN ttfb_ms > 0 THEN ttfb_ms ELSE 0 END), 0),
				COALESCE(SUM(CASE WHEN ttfb_ms > 0 THEN 1 ELSE 0 END), 0),
				COALESCE(SUM(prompt_tokens), 0),
				COALESCE(SUM(completion_tokens), 0),
				COALESCE(SUM(total_tokens), 0),
				COALESCE(SUM(cached_tokens), 0),
				COALESCE(SUM(cost), 0),
				COALESCE(cost_currency, '')
			FROM openai_gateway_analytics
			WHERE route != 'models'
			GROUP BY strftime('%Y-%m-%d %H:00:00', timestamp), COALESCE(endpoint_id, ''), COALESCE(gateway_key_id, ''), COALESCE(model, ''), COALESCE(route, ''), COALESCE(cost_currency, '')
		`); err != nil {
			return fmt.Errorf("openai ensure schema backfill stats: %w", err)
		}
	}
	return nil
}

func ensureSQLiteColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
	rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
	return err
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method

	// Completions proxy (intelligent routing or specific load balancer)
	if method == http.MethodPost && (path == "/v1/chat/completions" || path == "/chat/completions" || path == "/api/openai" || path == "/api/openai/v1/chat/completions" || path == "/api/openai/chat/completions") {
		s.proxyChatCompletions(w, r)
		return
	}

	// Responses proxy (OpenAI Responses API)
	if method == http.MethodPost && (path == "/v1/responses" || path == "/responses") {
		s.proxyResponses(w, r)
		return
	}

	// Anthropic Messages proxy (Anthropic Messages API)
	if method == http.MethodPost && (path == "/v1/messages" || path == "/messages") {
		s.proxyAnthropicMessages(w, r)
		return
	}

	// Models proxy
	if method == http.MethodGet && (path == "/v1/models" || path == "/models" || path == "/api/openai/v1/models" || path == "/api/openai/models") {
		s.proxyModels(w, r)
		return
	}

	// Admin CRUD prefix
	adminPath := strings.TrimPrefix(path, "/api/openai")
	adminPath = strings.Trim(adminPath, "/")
	parts := []string{}
	if adminPath != "" {
		parts = strings.Split(adminPath, "/")
	}

	switch {
	case len(parts) == 1 && parts[0] == "endpoints" && method == http.MethodGet:
		s.listEndpoints(w, r)
	case len(parts) == 1 && parts[0] == "endpoints" && method == http.MethodPost:
		s.createEndpoint(w, r)
	case len(parts) == 2 && parts[0] == "endpoints" && method == http.MethodPut:
		s.updateEndpoint(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "toggle" && method == http.MethodPost:
		s.toggleEndpoint(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "endpoints" && method == http.MethodDelete:
		s.deleteEndpoint(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "verify" && method == http.MethodPost:
		s.verifyEndpoint(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "models" && method == http.MethodGet:
		s.getEndpointModels(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "models" && parts[3] == "toggle" && method == http.MethodPost:
		s.toggleEndpointModel(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "models" && parts[3] == "toggle-batch" && method == http.MethodPost:
		s.toggleEndpointModelsBatch(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "models" && parts[3] == "add" && method == http.MethodPost:
		s.addEndpointModels(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "model-mappings" && method == http.MethodPut:
		s.updateModelMappings(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "routing" && method == http.MethodPut:
		s.updateEndpointRouting(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "test" && method == http.MethodPost:
		s.testEndpointChat(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "health-check" && method == http.MethodPost:
		s.healthCheckModelRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "health-check-all" && method == http.MethodPost:
		s.healthCheckAllModelsRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "key-check" && method == http.MethodPost:
		s.healthCheckKeysRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "health" && method == http.MethodGet:
		s.getEndpointHealthRoute(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "endpoints" && parts[2] == "proxy-state" && method == http.MethodGet:
		s.getEndpointProxyStateRoute(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "proxy-state" && parts[3] == "unban" && method == http.MethodPost:
		s.unbanEndpointProxies(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "endpoints" && parts[2] == "proxy-state" && parts[3] == "probe" && method == http.MethodPost:
		s.probeEndpointProxies(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "endpoints" && (parts[1] == "refresh" || parts[1] == "refresh-all") && method == http.MethodPost:
		s.refreshAllEndpointsRoute(w, r)
	case len(parts) == 2 && parts[0] == "endpoints" && parts[1] == "reorder" && method == http.MethodPost:
		s.reorderEndpoints(w, r)
	case len(parts) == 1 && parts[0] == "health-check-all" && method == http.MethodPost:
		s.healthCheckAllRoute(w, r)
	case len(parts) == 1 && parts[0] == "keys" && method == http.MethodGet:
		s.listGatewayKeys(w, r)
	case len(parts) == 1 && parts[0] == "keys" && method == http.MethodPost:
		s.createGatewayKey(w, r)
	case len(parts) == 2 && parts[0] == "keys" && method == http.MethodPut:
		s.updateGatewayKey(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "keys" && method == http.MethodDelete:
		s.deleteGatewayKey(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "keys" && parts[2] == "toggle" && method == http.MethodPost:
		s.toggleGatewayKey(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "keys" && parts[2] == "rotate" && method == http.MethodPost:
		s.rotateGatewayKey(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "keys" && parts[2] == "default" && method == http.MethodPut:
		s.setDefaultGatewayKey(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "export" && method == http.MethodGet:
		s.exportEndpointsRoute(w, r)
	case len(parts) == 1 && parts[0] == "import" && method == http.MethodPost:
		s.importEndpointsRoute(w, r)
	case len(parts) == 2 && parts[0] == "proxies" && parts[1] == "subscription-nodes" && method == http.MethodGet:
		s.listSubscriptionSocksProxies(w, r)
	case len(parts) == 2 && parts[0] == "proxies" && parts[1] == "resolve-subscription" && method == http.MethodPost:
		s.resolveSubscriptionProxies(w, r)
	case len(parts) == 2 && parts[0] == "proxies" && parts[1] == "import-list" && method == http.MethodPost:
		s.importProxyListRoute(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "summary" && method == http.MethodGet:
		s.getAnalyticsSummary(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "charts" && method == http.MethodGet:
		s.getAnalyticsCharts(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "logs" && method == http.MethodGet:
		s.getAnalyticsLogs(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "stream" && method == http.MethodGet:
		s.analyticsEventStream(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "clear" && method == http.MethodPost:
		s.clearAnalyticsLogs(w, r)
	case len(parts) == 2 && parts[0] == "analytics" && parts[1] == "clear-history" && method == http.MethodPost:
		s.clearAnalyticsHistory(w, r)
	case len(parts) == 1 && parts[0] == "relay-errors" && method == http.MethodGet:
		s.handleRelayErrors(w, r)
	default:
		response.Error(w, http.StatusNotFound, "openai admin route not found")
	}
}

// resolveEndpointModel 返回请求模型在该端点上实际使用的内部模型名，ok 表示该
// 端点当前可路由此请求。多个内部模型映射到同一外部名时优先返回未被
// disabled_models 禁用的别名，避免随机命中被禁用映射导致端点被整体过滤；
// 命中映射但全部被禁用时不可路由；无映射命中时按请求名本身判定禁用。
func (s *Service) updateEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Name         string        `json:"name"`
		BaseURL      string        `json:"baseUrl"`
		ModelsURL    *string       `json:"modelsUrl"`
		APIKey       *string       `json:"apiKey"`
		APIKeys      []string      `json:"apiKeys"`
		Notes        string        `json:"notes"`
		Headers      *[]HeaderItem `json:"headers"`
		ProxyPool    *[]string     `json:"proxyPool"`
		ProxyBatches *[]ProxyBatch `json:"proxyBatches"`
		AutoSwitch   *bool         `json:"autoSwitch"`
		ProxyEnabled *bool         `json:"proxyEnabled"`
		ForceProxy   *bool         `json:"forceProxy"`
		RateLimitRetryEnabled *bool `json:"rateLimitRetryEnabled"`
		RateLimitRetryWaitSeconds *int `json:"rateLimitRetryWaitSeconds"`
		KeyRetryRounds      *int   `json:"keyRetryRounds"`
		Protocol     *string       `json:"protocol"`
		// UpstreamType 端点上游协议类型（openai/gemini）；nil 表示未变更。
		UpstreamType *string `json:"upstreamType"`
		// ProxyPoolID 引用独立代理池插件（/api/proxypool）中的池；空串表示不引用。
		ProxyPoolID *string `json:"proxyPoolId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

		var currentBaseURL, currentAPIKey string
	var currentModelsURLRaw, currentProxyPoolIDRaw, currentUpstreamTypeRaw sql.NullString
	var currentKeyRetryRounds int
	// models_url 为 NULL（插件注册等历史行）时按空串处理，避免 NULL→string 扫描报错误判「端点不存在」。
	err = db.QueryRowContext(ctx, "SELECT base_url, models_url, api_key, proxy_pool_id, key_retry_rounds, upstream_type FROM openai_endpoints WHERE id = ?", id).Scan(&currentBaseURL, &currentModelsURLRaw, &currentAPIKey, &currentProxyPoolIDRaw, &currentKeyRetryRounds, &currentUpstreamTypeRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	currentModelsURL := currentModelsURLRaw.String
	currentAPIKey = secure.SecureDecrypt(currentAPIKey)

	// 上游协议类型：未提交时保留存量（全量保存不应把已配置的 gemini 冲回 openai）。
	upstreamType := ""
	upstreamTypeChanged := false
	if req.UpstreamType != nil {
		upstreamType = normalizeUpstreamType(*req.UpstreamType)
		upstreamTypeChanged = true
	} else if currentUpstreamTypeRaw.Valid {
		upstreamType = normalizeUpstreamType(currentUpstreamTypeRaw.String)
	}

	targetBaseURL := currentBaseURL
	if req.BaseURL != "" {
		// Gemini / Vertex 上游 baseURL 不追加 OpenAI 风格的 /v1 版本路径：
		// Gemini 在 /v1beta 下，Vertex 的 baseURL 已含 /v1/publishers/google。
		if upstreamType == upstreamTypeGemini {
			targetBaseURL = normalizeGeminiBaseURL(req.BaseURL)
		} else if upstreamType == upstreamTypeVertex {
			targetBaseURL = normalizeVertexBaseURL(req.BaseURL)
		} else {
			targetBaseURL = s.normalizeBaseURL(req.BaseURL)
		}
	}
	// 模型列表 URL 用指针区分「未提交」与「显式清空」：未提交保留存量，空串即清除覆盖。
	targetModelsURL := currentModelsURL
	if req.ModelsURL != nil {
		targetModelsURL = strings.TrimSpace(*req.ModelsURL)
	}
	targetAPIKey := currentAPIKey
	keyChanged := false
	if req.APIKey != nil {
		targetAPIKey = *req.APIKey
		// 仅当提交值与当前存储值不同才算「Key 变化」：前端保存时总是把表单里的
		// 原 key 原样发回，若按是否提供字段判定，纯改代理池/开关也会触发上游
		// 验证（大池端点保存慢的主要成因其一）。同值提交视为未变更，不验证。
		keyChanged = *req.APIKey != currentAPIKey
	}
	headersJSON, _ := json.Marshal([]HeaderItem{})
	headersChanged := false
	if req.Headers != nil {
		headersJSON, _ = json.Marshal(cleanHeaders(*req.Headers))
		headersChanged = true
	}
	proxyJSON, _ := json.Marshal([]string{})
	proxyChanged := false
	if req.ProxyPool != nil {
		// 运行时只消费 proxy_pool：确保池 = 手动代理 ∪ 全部批次代理。
		batchesForMerge := []ProxyBatch(nil)
		if req.ProxyBatches != nil {
			batchesForMerge = *req.ProxyBatches
		}
		proxyJSON, _ = json.Marshal(mergeProxyPoolWithBatches(*req.ProxyPool, batchesForMerge))
		proxyChanged = true
	}
	batchesJSON, _ := json.Marshal([]ProxyBatch{})
	batchesChanged := false
	if req.ProxyBatches != nil {
		batchesJSON, _ = json.Marshal(cleanProxyBatches(*req.ProxyBatches))
		batchesChanged = true
	} else {
		// 未提交批次时保留存量：老客户端/局部更新（仅改名字或池）不应清空批次数据。
		var batchesRaw sql.NullString
		_ = db.QueryRowContext(ctx, "SELECT proxy_batches FROM openai_endpoints WHERE id = ?", id).Scan(&batchesRaw)
		if batchesRaw.Valid && batchesRaw.String != "" {
			batchesJSON = []byte(batchesRaw.String)
		}
	}
	var apiKeysJSON []byte
	if req.APIKeys == nil {
		// 未提交备用 key 时保留存量：老客户端/局部更新（仅改开关或池）不应清空多 key。
		var apiKeysRaw sql.NullString
		_ = db.QueryRowContext(ctx, "SELECT api_keys FROM openai_endpoints WHERE id = ?", id).Scan(&apiKeysRaw)
		if apiKeysRaw.Valid && apiKeysRaw.String != "" {
			apiKeysJSON = []byte(apiKeysRaw.String)
			if string(apiKeysJSON) == "null" {
				apiKeysJSON = []byte("[]")
			}
		} else {
			apiKeysJSON = []byte("[]")
		}
	} else {
		apiKeysJSON, _ = json.Marshal(req.APIKeys)
		encryptedAPIKeys, err := secure.SecureEncrypt(string(apiKeysJSON))
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "扩展 Key 加密失败"})
			return
		}
		apiKeysJSON = []byte(encryptedAPIKeys)
	}
	autoSwitchInt := 0
	autoSwitchChanged := false
	if req.AutoSwitch != nil {
		autoSwitchInt = boolToInt(*req.AutoSwitch)
		autoSwitchChanged = true
	}
	proxyEnabledInt := 0
	proxyEnabledChanged := false
	if req.ProxyEnabled != nil {
		proxyEnabledInt = boolToInt(*req.ProxyEnabled)
		proxyEnabledChanged = true
	}
	forceProxyInt := 0
	forceProxyChanged := false
	if req.ForceProxy != nil {
		forceProxyInt = boolToInt(*req.ForceProxy)
		forceProxyChanged = true
	}
	rateLimitRetryInt := 1
	rateLimitRetryChanged := false
	if req.RateLimitRetryEnabled != nil {
		rateLimitRetryInt = boolToInt(*req.RateLimitRetryEnabled)
		rateLimitRetryChanged = true
	}
	rateLimitRetryWaitSeconds := 10
	rateLimitRetryWaitChanged := false
	if req.RateLimitRetryWaitSeconds != nil {
		rateLimitRetryWaitSeconds = *req.RateLimitRetryWaitSeconds
		if rateLimitRetryWaitSeconds < 1 {
			rateLimitRetryWaitSeconds = 10
		}
		rateLimitRetryWaitChanged = true
	}
	keyRetryRounds := 0
	keyRetryRoundsChanged := false
	if req.KeyRetryRounds != nil && *req.KeyRetryRounds >= 1 {
		keyRetryRounds = *req.KeyRetryRounds
		keyRetryRoundsChanged = true
	}
	// 全量更新分支总是写 key_retry_rounds：未变更时保留存量，避免全量保存把已配置值冲掉。
	targetKeyRetryRounds := currentKeyRetryRounds
	if keyRetryRoundsChanged {
		targetKeyRetryRounds = keyRetryRounds
	}
	protocol := ""
	protocolChanged := false
	if req.Protocol != nil {
		protocol = normalizeProtocol(*req.Protocol)
		protocolChanged = true
	}
	proxyPoolID := ""
	proxyPoolIDChanged := false
	if req.ProxyPoolID != nil {
		proxyPoolID = strings.TrimSpace(*req.ProxyPoolID)
		proxyPoolIDChanged = true
	} else {
		// 未提交代理池引用时保留存量：局部更新（仅改 key/开关等）不应解绑已选池。
		if currentProxyPoolIDRaw.Valid {
			proxyPoolID = currentProxyPoolIDRaw.String
		}
	}

	// 仅当 API Key 或地址实际变化时才重新验证/拉模型：纯改代理池、开关等局部
	// 保存不应触发上游请求（大池端点保存慢的主要成因其一）。前端全量提交时
	// baseUrl 必填，不能以「是否提交了 baseUrl」判定（恒真），必须比较归一化
	// 后的目标地址与当前存储值。
	if keyChanged || targetBaseURL != currentBaseURL || targetModelsURL != currentModelsURL {
		status := "unknown"
		// 验证或拉模型失败时保留旧模型列表：一次超时/临时网络故障不应清空
		// 已获取的模型（对齐 verifyEndpoint/refreshAllModels 的失败保留语义）。
		modelsList := []string{}
		pricing := PricingMap{}
		var currentModelsRaw, currentPricingRaw sql.NullString
		if err := db.QueryRowContext(ctx, "SELECT models, pricing FROM openai_endpoints WHERE id = ?", id).Scan(&currentModelsRaw, &currentPricingRaw); err == nil {
			if currentModelsRaw.Valid && currentModelsRaw.String != "" {
				_ = json.Unmarshal([]byte(currentModelsRaw.String), &modelsList)
			}
			if currentPricingRaw.Valid && currentPricingRaw.String != "" {
				_ = json.Unmarshal([]byte(currentPricingRaw.String), &pricing)
			}
		}

		verifyHeaders := []HeaderItem(nil)
		if req.Headers != nil {
			verifyHeaders = cleanHeaders(*req.Headers)
		}
		verifyPool := []string(nil)
		if req.ProxyPool != nil {
			verifyPool = cleanProxyPool(*req.ProxyPool)
		}

		// 空 API Key（新建或显式清空）无法验证：状态置 unknown，不发起上游请求。
		// 验证与拉取模型加总超时：挂死的出口/上游不能把保存拖成「等超时」。
		if targetAPIKey != "" {
			verifyCtx, cancelVerify := context.WithTimeout(ctx, endpointVerifyTimeout)
			vOk, _, err := s.verifyAPIKeyRaw(verifyCtx, targetBaseURL, targetAPIKey, id, verifyPool, targetModelsURL, upstreamType, verifyHeaders)
			if err == nil && vOk {
				status = "valid"
				mList, mPrice, mErr := s.listModelsWithPricing(verifyCtx, targetBaseURL, targetAPIKey, id, verifyPool, targetModelsURL, upstreamType, verifyHeaders)
				if mErr == nil {
					// Vertex AI 不提供模型列表端点，models 全部来自手动添加：
					// 保存触发重验证时不覆盖手动模型（拉取结果为空列表）。
					if upstreamType != upstreamTypeVertex {
						modelsList = mList
						pricing = mPrice
					}
				}
			} else {
				status = "invalid"
			}
			cancelVerify()
		}

		modelsJSON, _ := json.Marshal(modelsList)
		pricingJSON, _ := json.Marshal(pricing)
		encryptedKey, err := secure.SecureEncrypt(targetAPIKey)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "数据加密失败"})
			return
		}
		lastChecked := time.Now().Format(time.RFC3339)

		_, err = db.ExecContext(ctx, `
			UPDATE openai_endpoints
			SET name = ?, base_url = ?, models_url = ?, api_key = ?, api_keys = ?, headers = ?, proxy_pool = ?, proxy_batches = ?, auto_switch = ?, proxy_enabled = ?, force_proxy = ?, rate_limit_retry_enabled = ?, rate_limit_retry_wait_seconds = ?, protocol = ?, status = ?, models = ?, pricing = ?, last_checked = ?, proxy_pool_id = ?, key_retry_rounds = ?, upstream_type = ?
			WHERE id = ?`,
			req.Name, targetBaseURL, targetModelsURL, encryptedKey, string(apiKeysJSON), string(headersJSON), string(proxyJSON), string(batchesJSON), autoSwitchInt, proxyEnabledInt, forceProxyInt, rateLimitRetryInt, rateLimitRetryWaitSeconds, protocol, status, string(modelsJSON), string(pricingJSON), lastChecked, proxyPoolID, targetKeyRetryRounds, upstreamType, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else if headersChanged || proxyChanged || batchesChanged || autoSwitchChanged || proxyEnabledChanged || forceProxyChanged || protocolChanged || rateLimitRetryChanged || rateLimitRetryWaitChanged || proxyPoolIDChanged || keyRetryRoundsChanged || upstreamTypeChanged {
		_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET name = ?, api_keys = ?, headers = ?, proxy_pool = ?, proxy_batches = ?, auto_switch = ?, proxy_enabled = ?, force_proxy = ?, rate_limit_retry_enabled = ?, rate_limit_retry_wait_seconds = ?, protocol = ?, proxy_pool_id = ?, key_retry_rounds = ?, upstream_type = ? WHERE id = ?",
			req.Name, string(apiKeysJSON), string(headersJSON), string(proxyJSON), string(batchesJSON), autoSwitchInt, proxyEnabledInt, forceProxyInt, rateLimitRetryInt, rateLimitRetryWaitSeconds, protocol, proxyPoolID, targetKeyRetryRounds, upstreamType, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET name = ? WHERE id = ?", req.Name, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// effectiveProxyAttempts 计算一次转发最多尝试的次数。
// 只有「代理开关开启 + 代理池非空 + 自动切换」同时满足时，重试才可能换到不同的出口；
// 否则重试只是对同一条链路的重复请求——流式首字超时（firstTokenTimeout）后的重发
// 会把一次慢响应放大成两次串行等待，反而更慢，故此时固定只尝试一次。
// 池条目数超过 proxyAttemptCap 时按 cap 封顶，避免巨池把单次请求拖成串行扫库。
func (s *Service) resolveTargetEndpoint(r *http.Request) string {
	if !strings.HasPrefix(r.URL.Path, "/api/openai") {
		return ""
	}
	return r.Header.Get("x-endpoint-id")
}

// routeCacheTTL 是已启用端点配置缓存的过期时间。端点配置变更最多延迟该时长生效，
// 无需在每条写路径逐处失效，避免遗漏导致的陈旧路由。
const routeCacheTTL = 2 * time.Second

// enabledEndpointsCached 返回已启用端点的完整配置列表（按 sort_order 升序），
// 结果缓存在内存中，TTL 到期或配置变更后自动重建。调用方不得修改返回的端点。
func (s *Service) enabledEndpointsCached(ctx context.Context, db *sql.DB) ([]Endpoint, error) {
	s.routeCacheMu.Lock()
	if s.routeCacheReady && time.Since(s.routeCacheAt) < routeCacheTTL {
		cached := s.routeCache
		s.routeCacheMu.Unlock()
		return cached, nil
	}
	s.routeCacheMu.Unlock()

	endpoints := []Endpoint{}
	rows, err := db.QueryContext(ctx, `
		SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, auto_switch, proxy_enabled, force_proxy, rate_limit_retry_enabled, rate_limit_retry_wait_seconds, protocol, status, enabled, models, model_mappings, sort_order, priority, weight, key_retry_rounds, proxy_pool_id, upstream_type
		FROM openai_endpoints WHERE enabled = 1
		ORDER BY priority DESC, sort_order ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw, proxyPoolIDRaw, upstreamTypeRaw sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, rateLimitRetryInt, rateLimitRetryWaitSeconds, sortOrder, priority, weight, keyRetryRounds int
		if errScan := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &rateLimitRetryInt, &rateLimitRetryWaitSeconds, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &mappingsRaw, &sortOrder, &priority, &weight, &keyRetryRounds, &proxyPoolIDRaw, &upstreamTypeRaw); errScan == nil {
			ep.ProxyPoolID = proxyPoolIDRaw.String
			ep.UpstreamType = normalizeUpstreamType(upstreamTypeRaw.String)
			ep.APIKey = secure.SecureDecrypt(ep.APIKey)
			ep.Enabled = enabledInt == 1
			ep.AutoSwitch = autoSwitchInt == 1
			ep.ProxyEnabled = proxyEnabledInt == 1
			ep.ForceProxy = forceProxyInt == 1
			ep.RateLimitRetryEnabled = rateLimitRetryInt == 1
			ep.RateLimitRetryWaitSeconds = rateLimitRetryWaitSeconds
			ep.Protocol = normalizeProtocol(protocolRaw.String)
			ep.KeyRetryRounds = keyRetryRounds
			if apiKeysRaw.Valid && apiKeysRaw.String != "" {
				_ = json.Unmarshal([]byte(secure.SecureDecrypt(apiKeysRaw.String)), &ep.APIKeys)
			}
			if mappingsRaw.Valid && mappingsRaw.String != "" {
				_ = json.Unmarshal([]byte(mappingsRaw.String), &ep.ModelMappings)
			}
			if modelsRaw.Valid {
				_ = json.Unmarshal([]byte(modelsRaw.String), &ep.Models)
			}
			ep.Headers = []HeaderItem{}
			if headersRaw.Valid && headersRaw.String != "" {
				_ = json.Unmarshal([]byte(headersRaw.String), &ep.Headers)
			}
			ep.DisabledModels = []string{}
			if disabledRaw.Valid && disabledRaw.String != "" {
				_ = json.Unmarshal([]byte(disabledRaw.String), &ep.DisabledModels)
			}
			ep.ProxyPool = []string{}
			if proxyRaw.Valid && proxyRaw.String != "" {
				_ = json.Unmarshal([]byte(proxyRaw.String), &ep.ProxyPool)
			}
			ep.Priority = priority
			ep.Weight = weight
			ep.HealthStatus = ""
			endpoints = append(endpoints, ep)
		}
	}

	s.routeCacheMu.Lock()
	s.routeCache = endpoints
	s.routeModelIndex = buildRouteModelIndex(endpoints)
	s.routeCacheAt = time.Now()
	s.routeCacheReady = true
	s.routeCacheMu.Unlock()
	return endpoints, nil
}

// invalidateRouteCache 主动清空路由缓存，供端点配置变更路径调用，
// 使新配置立即生效而无需等待 TTL 到期。
func (s *Service) invalidateRouteCache() {
	s.routeCacheMu.Lock()
	s.routeCacheReady = false
	s.routeCache = nil
	s.routeCacheMu.Unlock()
}

// enabledEndpointSnapshot 返回已启用端点列表与其对应的模型倒排索引，二者在
// 同一次锁持有内读取，保证下标与索引一致（避免缓存重建窗口下选择到错位端点）。
func (s *Service) enabledEndpointSnapshot(ctx context.Context, db *sql.DB) ([]Endpoint, map[string][]int, error) {
	s.routeCacheMu.Lock()
	if s.routeCacheReady && time.Since(s.routeCacheAt) < routeCacheTTL {
		eps := s.routeCache
		idx := s.routeModelIndex
		s.routeCacheMu.Unlock()
		return eps, idx, nil
	}
	s.routeCacheMu.Unlock()

	eps, err := s.enabledEndpointsCached(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	// enabledEndpointsCached 在同一锁内写入 routeCache 与 routeModelIndex，
	// 此处再次加锁读取索引即可保证与刚返回的切片同代。
	s.routeCacheMu.Lock()
	idx := s.routeModelIndex
	s.routeCacheMu.Unlock()
	return eps, idx, nil
}

// selectEndpointCandidates 根据模型名返回能服务该模型的全部候选端点（已按侧栏
// sort_order 升序排序；同 order 内按创建时间稳定）。优先使用管理面板 x-endpoint-id
// 指定的端点（若它也服务该模型则仅返回它一个，强制指定时不参与 failover）。
// sessionKey 非空时优先复用该会话最近成功使用的端点（Channel Affinity）。
// chosen 为调用方实际首选使用的端点（主 key 端），index 为 chosen 在返回切片中的下标。
func (s *Service) selectEndpointCandidates(ctx context.Context, db *sql.DB, model, targetEndpointID, sessionKey string) (candidates []Endpoint, chosen Endpoint, chosenIndex int, selectedModel string, found bool) {
	selectedModel = model

	loadEndpoint := func(ep *Endpoint, headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw sql.NullString, enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, keyRetryRounds int) {
		ep.APIKey = secure.SecureDecrypt(ep.APIKey)
		ep.Enabled = enabledInt == 1
		ep.AutoSwitch = autoSwitchInt == 1
		ep.ProxyEnabled = proxyEnabledInt == 1
		ep.ForceProxy = forceProxyInt == 1
		ep.Protocol = normalizeProtocol(protocolRaw.String)
		ep.KeyRetryRounds = keyRetryRounds
		if apiKeysRaw.Valid && apiKeysRaw.String != "" {
			_ = json.Unmarshal([]byte(secure.SecureDecrypt(apiKeysRaw.String)), &ep.APIKeys)
		}
		if mappingsRaw.Valid && mappingsRaw.String != "" {
			_ = json.Unmarshal([]byte(mappingsRaw.String), &ep.ModelMappings)
		}
		if modelsRaw.Valid {
			_ = json.Unmarshal([]byte(modelsRaw.String), &ep.Models)
		}
		ep.Headers = []HeaderItem{}
		if headersRaw.Valid && headersRaw.String != "" {
			_ = json.Unmarshal([]byte(headersRaw.String), &ep.Headers)
		}
		ep.DisabledModels = []string{}
		if disabledRaw.Valid && disabledRaw.String != "" {
			_ = json.Unmarshal([]byte(disabledRaw.String), &ep.DisabledModels)
		}
		ep.ProxyPool = []string{}
		if proxyRaw.Valid && proxyRaw.String != "" {
			_ = json.Unmarshal([]byte(proxyRaw.String), &ep.ProxyPool)
		}
	}

	if targetEndpointID != "" {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, keyRetryRounds int
		err := db.QueryRowContext(ctx, `
			SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, model_mappings, key_retry_rounds
			FROM openai_endpoints WHERE id = ? AND enabled = 1`, targetEndpointID).
			Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &mappingsRaw, &keyRetryRounds)
		if err == nil {
			loadEndpoint(&ep, headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw, enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, keyRetryRounds)
			if s.endpointHasModel(ep, model) {
				if real, routable := s.resolveEndpointModel(ep, model); routable {
					selectedModel = real
					return []Endpoint{ep}, ep, 0, selectedModel, true
				}
			}
		}
	}

	// Enabled is the administrator's routing decision; status is the latest verification result.
	endpoints, routeModelIndex, err := s.enabledEndpointSnapshot(ctx, db)
	if err != nil {
		return nil, chosen, -1, selectedModel, false
	}

	// 使用预构建的内存倒排索引（Ability）：按模型名直接定位候选端点下标，
	// 避免每请求遍历全部已启用端点。索引中的端点可能已被禁用模型（disabled_models），
	// 仍需二次过滤，故在候选组装时统一做禁用模型检查。
	modelIdx := make([]int, 0, 4)
	if routeModelIndex != nil {
		modelIdx = append(modelIdx, routeModelIndex[model]...)
	}

	if len(modelIdx) > 0 {
		for _, idx := range modelIdx {
			if idx < 0 || idx >= len(endpoints) {
				continue
			}
			ep := endpoints[idx]
			if _, routable := s.resolveEndpointModel(ep, model); routable {
				candidates = append(candidates, ep)
			}
		}
	}
	if len(candidates) == 0 {
		// 兜底：索引未命中（模型列表尚未刷新/模型名未收录）时按原逻辑遍历全部端点。
		for _, ep := range endpoints {
			if s.endpointHasModel(ep, model) {
				if _, routable := s.resolveEndpointModel(ep, model); routable {
					candidates = append(candidates, ep)
				}
			}
		}
	}
	if len(candidates) == 0 {
		return nil, chosen, -1, selectedModel, false
	}

	// 会话亲和（Channel Affinity）：同一会话上次成功使用的端点优先复用（若仍在候选池）。
	if sessionKey != "" {
		if affinityID := s.preferredAffinityEndpoint(sessionKey); affinityID != "" {
			if idx := affinityEndpointIndex(affinityID, candidates); idx > 0 {
				cand := candidates[idx]
				candidates = append(candidates[:idx], candidates[idx+1:]...)
				candidates = append([]Endpoint{cand}, candidates...)
			}
		}
	}

	// 按侧栏顺序稳定的候选列表；但首选在健康端中按延迟加权挑选，
	// 以保持原有「快的端点优先」行为，同时整体列表仍按 sort_order 渐变。
	latencies := make([]int64, len(candidates))
	known := make([]bool, len(candidates))
	weights := make([]int64, len(candidates))
	for i, ep := range candidates {
		latencies[i], known[i] = s.getEndpointLatency(ep.ID)
		weights[i] = endpointWeight(ep)
	}
	chosenIndex = weightedEndpointPickWeighted(latencies, known, weights)
	// 测试专用确定性选路钩子（生产恒为 nil）：覆盖延迟加权随机，
	// 供依赖「端点 A 先被选中」的 failover 测试消除 flake。
	if endpointPickOverride != nil {
		if i := endpointPickOverride(candidates); i >= 0 && i < len(candidates) {
			chosenIndex = i
		}
	}
	chosen = candidates[chosenIndex]
	if real, routable := s.resolveEndpointModel(chosen, model); routable {
		selectedModel = real
	}
	return candidates, chosen, chosenIndex, selectedModel, true
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
}

// relayLoop 执行带代理择优与重试的上游转发循环（三个转发入口共用）。语义与
// 说明见调用方注释：上游限流/5xx 不是代理的错，只切换出口重试不冷却代理；
// 但 429 会累计计数，达到阈值后临时禁用该代理（IP 级限流下继续选择只会反复 429）。
func (b *relayCancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	if b.cancel != nil {
		b.cancel()
	}
	return err
}

func parseModelIDsFromRaw(raw string) []string {
	if raw == "" {
		return nil
	}
	var strList []string
	if err := json.Unmarshal([]byte(raw), &strList); err == nil && len(strList) > 0 {
		out := make([]string, 0, len(strList))
		for _, s := range strList {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				out = append(out, trimmed)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	var anyList []interface{}
	if err := json.Unmarshal([]byte(raw), &anyList); err == nil {
		out := make([]string, 0, len(anyList))
		for _, item := range anyList {
			switch v := item.(type) {
			case string:
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					out = append(out, trimmed)
				}
			case map[string]interface{}:
				if id, ok := v["id"].(string); ok {
					if trimmed := strings.TrimSpace(id); trimmed != "" {
						out = append(out, trimmed)
					}
				} else if name, ok := v["name"].(string); ok {
					if trimmed := strings.TrimSpace(name); trimmed != "" {
						out = append(out, trimmed)
					}
				}
			}
		}
		return out
	}
	return nil
}

// healthCheckAttempts 是单个模型健康检测的最大尝试轮数。
// 上游限流（429/5xx/超时）波动大，多轮中任一次成功即视为可用，
// 避免一次抖动就把可用模型误判为失败。
const healthCheckAttempts = 2

// healthCheckFastFailStatuses 是无需重试的确定性失败状态码：
// 权限/不存在等错误不会因重试而好转，直接判定失败以节省时间。
func (s *Service) randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			applog.Warn(context.Background(), "openai", "secure random failed, using timestamp fallback", "error", err.Error())
			b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
			continue
		}
		b[i] = letters[num.Int64()]
	}
	return string(b)
}
