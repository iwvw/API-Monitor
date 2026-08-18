package openai

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"golang.org/x/net/proxy"
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
)

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
	inputTokensRegex      = regexp.MustCompile(`"input_tokens"\s*:\s*(\d+)`)
	outputTokensRegex     = regexp.MustCompile(`"output_tokens"\s*:\s*(\d+)`)
	versionPathRegex      = regexp.MustCompile(`(?i)/v\d+/?`)
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
	ModelMappings  map[string]string `json:"modelMappings,omitempty"`
	// Priority 是端点优先级档位：值越大越优先被选中（同模型多端点时先高优先级）。
	// Weight 是同档位内的加权因子：值越大在该档位内被选中的概率越高。
	Priority     int     `json:"priority,omitempty"`
	Weight       int     `json:"weight,omitempty"`
	CreatedAt    string  `json:"createdAt"`
	LastUsed     *string `json:"lastUsed"`
	LastChecked  *string `json:"lastChecked"`
	HealthStatus string  `json:"healthStatus,omitempty"`
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
	analyticsOnce  sync.Once
	analyticsDrop  atomic.Uint64

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
	// alertState 记录告警边沿状态，避免同状态反复触发通知。
	alertMu    sync.Mutex
	alertState gatewayAlertState
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
	Time       time.Time `json:"time"`
	Route      string    `json:"route"`
	Kind       string    `json:"kind"`
	Endpoint   string    `json:"endpoint"`
	EndpointID string    `json:"endpointId"`
	KeyIndex   int       `json:"keyIndex,omitempty"`
	Model      string    `json:"model"`
	Stream     bool      `json:"stream"`
	Proxy      string    `json:"proxy"`
	ClientIP   string    `json:"clientIp"`
	Attempts   int       `json:"attempts"`
	ElapsedMs  int64     `json:"elapsedMs"`
	StatusCode int       `json:"statusCode,omitempty"`
	Upstream   string    `json:"upstream,omitempty"`
	Error      string    `json:"error"`
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
}

// sessionBinding 是某会话在某出口 IP（代理）上的粘性绑定。
// count 为该代理已承载的请求数；超过 sessionProxyRequestLimit 后换新代理并重置。
type sessionBinding struct {
	proxy string
	count int
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
		protocolClients:      map[string]*http.Client{},
		proxyStateByEndpoint: make(map[string]*endpointProxyState),
		keyStateByEndpoint:   make(map[string]*endpointKeyState),
		endpointLatency:      make(map[string]int64),
		endpointLatencyOK:    make(map[string]bool),
		analyticsStreams:     make(map[int]chan map[string]interface{}),
		analyticsQueue:       make(chan analyticsWriteItem, analyticsQueueSize),
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
		SELECT id, base_url, api_key, proxy_pool
		FROM openai_endpoints WHERE enabled = 1 AND proxy_pool IS NOT NULL AND proxy_pool != ''`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, baseURL, apiKey string
		var proxyRaw string
		if err := rows.Scan(&id, &baseURL, &apiKey, &proxyRaw); err != nil {
			continue
		}
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
			s.warmProxyConnection(ctx, id, baseURL, apiKey, proxyURL)
		}
	}
}

// warmProxyConnection 通过指定代理向端点的 models 地址发起一次请求，触发连接建立，
// 并兼作探活：链路可达（拿到任意响应）清除该代理的失败计数与冷却，
// 连接失败则按失败计数冷却代理（与 markProxyFailed 指数退避一致）。
func (s *Service) warmProxyConnection(ctx context.Context, endpointID, baseURL, apiKey, proxyURL string) {
	client, err := s.proxyClient(proxyURL)
	if err != nil {
		return
	}
	fullURL := strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
		fullURL += "/v1"
	}
	fullURL += "/models"

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
			status TEXT DEFAULT 'unknown',
			enabled INTEGER DEFAULT 1,
			models TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			last_used DATETIME,
			last_checked DATETIME,
			sort_order INTEGER DEFAULT 0,
			priority INTEGER DEFAULT 0,
			weight INTEGER DEFAULT 100
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
		`CREATE INDEX IF NOT EXISTS idx_openai_endpoints_status ON openai_endpoints(status)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_health_endpoint ON openai_health_history(endpoint_id, checked_at)`,
		`CREATE TABLE IF NOT EXISTS openai_chat_personas (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			icon TEXT,
			system_prompt TEXT NOT NULL,
			is_default INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS openai_chat_sessions (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			model TEXT,
			endpoint_id TEXT,
			persona_id TEXT,
			system_prompt TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS openai_chat_messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			reasoning TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (session_id) REFERENCES openai_chat_sessions(id) ON DELETE CASCADE
		)`,
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
		`CREATE INDEX IF NOT EXISTS idx_openai_analytics_timestamp ON openai_gateway_analytics(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_openai_gateway_keys_hash ON openai_gateway_keys(key_hash)`,
		`CREATE TABLE IF NOT EXISTS openai_proxy_state (
			endpoint_id TEXT NOT NULL,
			proxy TEXT NOT NULL,
			kind TEXT NOT NULL,
			until DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (endpoint_id, proxy, kind)
		)`,
		`INSERT OR IGNORE INTO openai_chat_personas (id, name, icon, system_prompt, is_default)
		 VALUES ('1', '默认助手', 'fa-robot', '你是一个有用的 AI 助手。', 1)`,
	}
	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("openai ensure schema: %w", err)
		}
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
	if _, err := db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_openai_analytics_gateway_key ON openai_gateway_analytics(gateway_key_id, timestamp)`); err != nil {
		return fmt.Errorf("openai ensure schema: %w", err)
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
	case len(parts) == 1 && parts[0] == "relay-errors" && method == http.MethodGet:
		s.handleRelayErrors(w, r)
	case len(parts) == 1 && parts[0] == "personas" && method == http.MethodGet:
		s.listPersonas(w, r)
	case len(parts) == 1 && parts[0] == "personas" && method == http.MethodPost:
		s.createPersona(w, r)
	case len(parts) == 2 && parts[0] == "personas" && method == http.MethodPut:
		s.updatePersona(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "personas" && method == http.MethodDelete:
		s.deletePersona(w, r, parts[1])
	case len(parts) == 1 && parts[0] == "sessions" && method == http.MethodGet:
		s.listSessions(w, r)
	case len(parts) == 1 && parts[0] == "sessions" && method == http.MethodPost:
		s.createSession(w, r)
	case len(parts) == 1 && parts[0] == "sessions" && method == http.MethodDelete:
		s.clearSessions(w, r)
	case len(parts) == 2 && parts[0] == "sessions" && method == http.MethodPut:
		s.updateSession(w, r, parts[1])
	case len(parts) == 2 && parts[0] == "sessions" && method == http.MethodDelete:
		s.deleteSession(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "messages" && method == http.MethodGet:
		s.listSessionMessages(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "messages" && method == http.MethodPost:
		s.createSessionMessage(w, r, parts[1])
	case len(parts) == 3 && parts[0] == "sessions" && parts[2] == "messages" && method == http.MethodDelete:
		s.clearSessionMessages(w, r, parts[1])
	case len(parts) == 4 && parts[0] == "sessions" && parts[2] == "messages" && method == http.MethodDelete:
		s.deleteSessionMessage(w, r, parts[1], parts[3])
	default:
		response.Error(w, http.StatusNotFound, "openai admin route not found")
	}
}

func (s *Service) resolveEndpointModel(ep Endpoint, requested string) (string, bool) {
	for real, alias := range ep.ModelMappings {
		if alias == requested {
			return real, true
		}
	}
	return requested, false
}

// normalizeReasoningEffort 将 OpenAI 标准枚举之外的 reasoning_effort 值归一到
// 兼容值，避免 failover 到枚举更窄的上游（如部分仅接受 low/medium/high 的
// 服务）时被 400 拒绝。当前仅收敛 max -> high；其余值保持透传，最小侵入。
// 同时兼容 chat.completions（reasoning_effort 顶层字段）与 responses
// （reasoning.effort 嵌套字段）两种请求形态。
func normalizeReasoningEffort(body map[string]interface{}) {
	normalize := func(raw interface{}) interface{} {
		if s, ok := raw.(string); ok && s == "max" {
			return "high"
		}
		return raw
	}
	if raw, ok := body["reasoning_effort"]; ok {
		body["reasoning_effort"] = normalize(raw)
	}
	if reasoning, ok := body["reasoning"].(map[string]interface{}); ok {
		if raw, ok := reasoning["effort"]; ok {
			reasoning["effort"] = normalize(raw)
		}
	}
}

// recordRelayError 记录一次推理转发失败事件：写入内存环形缓冲（供 relay-errors 接口读取），
// 并按严重度输出结构化日志。Proxy 字段必须传脱敏后的 host:port，禁止传完整代理 URL，
// 避免把代理凭据写进日志文件或接口响应。
func (s *Service) recordRelayError(rec RelayErrorRecord) {
	rec.Time = time.Now().UTC()

	s.relayErrMu.Lock()
	s.relayErrors = append(s.relayErrors, rec)
	if len(s.relayErrors) > relayErrorBufferSize {
		s.relayErrors = s.relayErrors[len(s.relayErrors)-relayErrorBufferSize:]
	}
	s.relayErrMu.Unlock()

	logAttrs := []any{
		"route", rec.Route,
		"kind", rec.Kind,
		"endpoint", rec.Endpoint,
		"endpoint_id", rec.EndpointID,
		"key_index", rec.KeyIndex,
		"model", rec.Model,
		"stream", rec.Stream,
		"proxy", rec.Proxy,
		"client_ip", rec.ClientIP,
		"attempts", rec.Attempts,
		"elapsed_ms", rec.ElapsedMs,
		"err", rec.Error,
	}
	if rec.StatusCode > 0 {
		logAttrs = append(logAttrs, "upstream_status", rec.StatusCode)
	}
	if rec.Upstream != "" {
		logAttrs = append(logAttrs, "upstream_body", rec.Upstream)
	}
	switch rec.Kind {
	case "no_endpoint", "config", "gateway", "bad_gateway":
		applog.Error(context.Background(), "openai", "openai relay failed", logAttrs...)
	default:
		applog.Warn(context.Background(), "openai", "openai relay degraded", logAttrs...)
	}
}

// handleRelayErrors 返回最近发生的推理转发失败明细（最新在前），供管理界面与 AI 排障调用。
// limit 参数默认 50，上限与环形缓冲一致。
func (s *Service) handleRelayErrors(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= relayErrorBufferSize {
			limit = n
		}
	}

	s.relayErrMu.Lock()
	defer s.relayErrMu.Unlock()
	total := len(s.relayErrors)
	out := make([]RelayErrorRecord, 0, min(limit, total))
	for i := total - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.relayErrors[i])
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   total,
		"records": out,
	})
}

// truncateForLog 把任意字符串截断到指定字符数，避免上游错误体把日志或缓冲撑爆。
func truncateForLog(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + " ...(truncated)"
}

// errorResponseForLog 决定错误响应体（报错 JSON）是否写入调用日志：
// 仅当请求失败（状态码 >= 400）时返回截断后的报错 JSON，成功请求返回空串。
func errorResponseForLog(body []byte, statusCode int) string {
	if statusCode >= 200 && statusCode < 400 {
		return ""
	}
	return truncateForLog(string(body), relayErrorResponseLimit)
}

// trimErrorDetailRetention 清空超出保留上限（relayErrorResponseRetention）的错误详情：
// 只更新 error_kind/error_message/response_body 列，保留调用日志行（统计不受影响）。
// 最新记录按 timestamp DESC, id DESC 判定（同秒多记录时 id 越新越靠前）。
func (s *Service) trimErrorDetailRetention(ctx context.Context, db *sql.DB) {
	if _, err := db.ExecContext(ctx, `
		UPDATE openai_gateway_analytics
		SET error_kind = '', error_message = '', response_body = ''
		WHERE error_kind IS NOT NULL AND error_kind != ''
		  AND id NOT IN (
			SELECT id FROM (
				SELECT id FROM openai_gateway_analytics
				WHERE error_kind IS NOT NULL AND error_kind != ''
				ORDER BY timestamp DESC, id DESC
				LIMIT ?
			)
		  )
	`, relayErrorResponseRetention); err != nil {
		applog.Error(ctx, "openai", "Failed to trim error detail retention", "error", err.Error())
	}
}

func (s *Service) endpointHasModel(ep Endpoint, requested string) bool {
	for _, m := range ep.Models {
		if m == requested {
			return true
		}
	}
	for _, alias := range ep.ModelMappings {
		if alias == requested {
			return true
		}
	}
	return false
}

func (s *Service) updateModelMappings(w http.ResponseWriter, r *http.Request, endpointID string) {
	ctx := r.Context()
	var payload struct {
		Mappings map[string]string `json:"mappings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	clean := map[string]string{}
	for real, alias := range payload.Mappings {
		real = strings.TrimSpace(real)
		alias = strings.TrimSpace(alias)
		if real != "" && alias != "" {
			clean[real] = alias
		}
	}
	data, _ := json.Marshal(clean)
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "UPDATE openai_endpoints SET model_mappings = ? WHERE id = ?", string(data), endpointID); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "modelMappings": clean})
}

// updateEndpointRouting 保存端点的路由优先级/权重（对齐 model-mappings 的局部更新模式）：
// 只更新 priority 与 weight 两列，不影响端点其他配置；返回更新后的值供前端回填。
func (s *Service) updateEndpointRouting(w http.ResponseWriter, r *http.Request, endpointID string) {
	ctx := r.Context()
	var payload struct {
		Priority *int `json:"priority"`
		Weight   *int `json:"weight"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	if payload.Priority == nil && payload.Weight == nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "priority 或 weight 至少提供一个"})
		return
	}
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var exists int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_endpoints WHERE id = ?", endpointID).Scan(&exists); err != nil || exists == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	var currentPriority, currentWeight int
	_ = db.QueryRowContext(ctx, "SELECT priority, weight FROM openai_endpoints WHERE id = ?", endpointID).Scan(&currentPriority, &currentWeight)
	priority := currentPriority
	if payload.Priority != nil {
		priority = *payload.Priority
	}
	weight := currentWeight
	if weight <= 0 {
		weight = 100
	}
	if payload.Weight != nil {
		weight = *payload.Weight
	}
	if _, err := db.ExecContext(ctx, "UPDATE openai_endpoints SET priority = ?, weight = ? WHERE id = ?", priority, weight, endpointID); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "priority": priority, "weight": weight})
}

func (s *Service) listEndpoints(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, created_at, last_used, last_checked, model_mappings, sort_order, priority, weight FROM openai_endpoints ORDER BY priority DESC, sort_order ASC, created_at ASC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	endpoints := []Endpoint{}
	for rows.Next() {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, batchesRaw, mappingsRaw, protocolRaw, apiKeysRaw sql.NullString
		var created, used, checked sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, sortOrder, priority, weight int

		err := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &batchesRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &created, &used, &checked, &mappingsRaw, &sortOrder, &priority, &weight)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		ep.APIKey = secure.SecureDecrypt(ep.APIKey)
		ep.Enabled = enabledInt == 1
		ep.AutoSwitch = autoSwitchInt == 1
		ep.ProxyEnabled = proxyEnabledInt == 1
		ep.ForceProxy = forceProxyInt == 1
		ep.Protocol = normalizeProtocol(protocolRaw.String)
		ep.Priority = priority
		ep.Weight = weight
		ep.CreatedAt = created.String
		if used.Valid {
			v := used.String
			ep.LastUsed = &v
		}
		if checked.Valid {
			v := checked.String
			ep.LastChecked = &v
		}
		if apiKeysRaw.Valid && apiKeysRaw.String != "" {
			_ = json.Unmarshal([]byte(secure.SecureDecrypt(apiKeysRaw.String)), &ep.APIKeys)
		}
		if mappingsRaw.Valid && mappingsRaw.String != "" {
			_ = json.Unmarshal([]byte(mappingsRaw.String), &ep.ModelMappings)
		}

		ep.Models = []string{}
		if modelsRaw.Valid && modelsRaw.String != "" {
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
		ep.ProxyBatches = []ProxyBatch{}
		if batchesRaw.Valid && batchesRaw.String != "" {
			_ = json.Unmarshal([]byte(batchesRaw.String), &ep.ProxyBatches)
		}
		endpoints = append(endpoints, ep)
	}

	response.JSON(w, http.StatusOK, endpoints)
}

func (s *Service) createEndpoint(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string       `json:"name"`
		BaseURL      string       `json:"baseUrl"`
		APIKey       string       `json:"apiKey"`
		APIKeys      []string     `json:"apiKeys"`
		Notes        string       `json:"notes"`
		Headers      []HeaderItem `json:"headers"`
		ProxyPool    []string     `json:"proxyPool"`
		ProxyBatches []ProxyBatch `json:"proxyBatches"`
		AutoSwitch   bool         `json:"autoSwitch"`
		ProxyEnabled bool         `json:"proxyEnabled"`
		ForceProxy   bool         `json:"forceProxy"`
		Protocol     string       `json:"protocol"`
		SkipVerify   bool         `json:"skipVerify"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Name == "" || req.BaseURL == "" || req.APIKey == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "名称、API 地址和 API Key 必填"})
		return
	}

	normalizedURL := s.normalizeBaseURL(req.BaseURL)
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	headersJSON, _ := json.Marshal(cleanHeaders(req.Headers))
	batchesJSON, _ := json.Marshal(cleanProxyBatches(req.ProxyBatches))
	// 运行时只消费 proxy_pool：无论客户端是否已合并，都保证池 = 手动代理 ∪ 全部批次代理。
	proxyJSON, _ := json.Marshal(mergeProxyPoolWithBatches(req.ProxyPool, req.ProxyBatches))
	autoSwitchInt := boolToInt(req.AutoSwitch)
	protocol := normalizeProtocol(req.Protocol)

	id := fmt.Sprintf("oai_%d_%s", time.Now().UnixNano(), s.randString(9))
	status := "unknown"
	modelsList := []string{}
	var verification map[string]interface{}

	if !req.SkipVerify {
		vOk, count, err := s.verifyAPIKeyRaw(ctx, normalizedURL, req.APIKey, id, cleanProxyPool(req.ProxyPool), cleanHeaders(req.Headers))
		if err == nil && vOk {
			status = "valid"
			verification = map[string]interface{}{
				"valid":       true,
				"modelsCount": count,
			}
			mList, mErr := s.listModelsRaw(ctx, normalizedURL, req.APIKey, id, cleanProxyPool(req.ProxyPool), cleanHeaders(req.Headers))
			if mErr == nil {
				modelsList = mList
			}
		} else {
			status = "invalid"
			errMsg := "API Key 验证失败"
			if err != nil {
				errMsg = err.Error()
			}
			verification = map[string]interface{}{
				"valid": false,
				"error": errMsg,
			}
		}
	}

	modelsJSON, _ := json.Marshal(modelsList)
	createdAt := time.Now().Format(time.RFC3339)
	var lastCheckedVal interface{} = nil
	if !req.SkipVerify {
		lastCheckedVal = createdAt
	}

	encryptedKey, err := secure.SecureEncrypt(req.APIKey)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "数据加密失败"})
		return
	}
	apiKeysJSON, _ := json.Marshal(req.APIKeys)
	encryptedAPIKeys, err := secure.SecureEncrypt(string(apiKeysJSON))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "扩展 Key 加密失败"})
		return
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO openai_endpoints (id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, created_at, last_checked, sort_order)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.Name, normalizedURL, encryptedKey, encryptedAPIKeys, string(headersJSON), "[]", string(proxyJSON), string(batchesJSON), autoSwitchInt, boolToInt(req.ProxyEnabled), boolToInt(req.ForceProxy), protocol, status, 1, string(modelsJSON), createdAt, lastCheckedVal, time.Now().UnixMilli())
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var checkedStr *string
	if !req.SkipVerify {
		checkedStr = &createdAt
	}

	resEndpoint := Endpoint{
		ID:           id,
		Name:         req.Name,
		BaseURL:      normalizedURL,
		APIKey:       req.APIKey,
		Notes:        req.Notes,
		Headers:      cleanHeaders(req.Headers),
		ProxyPool:    mergeProxyPoolWithBatches(req.ProxyPool, req.ProxyBatches),
		ProxyBatches: cleanProxyBatches(req.ProxyBatches),
		AutoSwitch:   req.AutoSwitch,
		Protocol:     protocol,
		Status:       status,
		Enabled:      true,
		Models:       modelsList,
		CreatedAt:    createdAt,
		LastChecked:  checkedStr,
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":      true,
		"endpoint":     resEndpoint,
		"verification": verification,
	})
}

func (s *Service) updateEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Name         string        `json:"name"`
		BaseURL      string        `json:"baseUrl"`
		APIKey       string        `json:"apiKey"`
		APIKeys      []string      `json:"apiKeys"`
		Notes        string        `json:"notes"`
		Headers      *[]HeaderItem `json:"headers"`
		ProxyPool    *[]string     `json:"proxyPool"`
		ProxyBatches *[]ProxyBatch `json:"proxyBatches"`
		AutoSwitch   *bool         `json:"autoSwitch"`
		ProxyEnabled *bool         `json:"proxyEnabled"`
		ForceProxy   *bool         `json:"forceProxy"`
		Protocol     *string       `json:"protocol"`
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
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key FROM openai_endpoints WHERE id = ?", id).Scan(&currentBaseURL, &currentAPIKey)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	currentAPIKey = secure.SecureDecrypt(currentAPIKey)

	targetBaseURL := currentBaseURL
	if req.BaseURL != "" {
		targetBaseURL = s.normalizeBaseURL(req.BaseURL)
	}
	targetAPIKey := currentAPIKey
	if req.APIKey != "" {
		targetAPIKey = req.APIKey
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
	protocol := ""
	protocolChanged := false
	if req.Protocol != nil {
		protocol = normalizeProtocol(*req.Protocol)
		protocolChanged = true
	}

	if req.APIKey != "" || req.BaseURL != "" {
		status := "unknown"
		modelsList := []string{}

		verifyHeaders := []HeaderItem(nil)
		if req.Headers != nil {
			verifyHeaders = cleanHeaders(*req.Headers)
		}
		verifyPool := []string(nil)
		if req.ProxyPool != nil {
			verifyPool = cleanProxyPool(*req.ProxyPool)
		}

		vOk, _, err := s.verifyAPIKeyRaw(ctx, targetBaseURL, targetAPIKey, id, verifyPool, verifyHeaders)
		if err == nil && vOk {
			status = "valid"
			mList, mErr := s.listModelsRaw(ctx, targetBaseURL, targetAPIKey, id, verifyPool, verifyHeaders)
			if mErr == nil {
				modelsList = mList
			}
		} else {
			status = "invalid"
		}

		modelsJSON, _ := json.Marshal(modelsList)
		encryptedKey, err := secure.SecureEncrypt(targetAPIKey)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "数据加密失败"})
			return
		}
		lastChecked := time.Now().Format(time.RFC3339)

		_, err = db.ExecContext(ctx, `
			UPDATE openai_endpoints
			SET name = ?, base_url = ?, api_key = ?, api_keys = ?, headers = ?, proxy_pool = ?, proxy_batches = ?, auto_switch = ?, proxy_enabled = ?, force_proxy = ?, protocol = ?, status = ?, models = ?, last_checked = ?
			WHERE id = ?`,
			req.Name, targetBaseURL, encryptedKey, string(apiKeysJSON), string(headersJSON), string(proxyJSON), string(batchesJSON), autoSwitchInt, proxyEnabledInt, forceProxyInt, protocol, status, string(modelsJSON), lastChecked, id)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else if headersChanged || proxyChanged || batchesChanged || autoSwitchChanged || proxyEnabledChanged || forceProxyChanged || protocolChanged {
		_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET name = ?, api_keys = ?, headers = ?, proxy_pool = ?, proxy_batches = ?, auto_switch = ?, proxy_enabled = ?, force_proxy = ?, protocol = ? WHERE id = ?",
			req.Name, string(apiKeysJSON), string(headersJSON), string(proxyJSON), string(batchesJSON), autoSwitchInt, proxyEnabledInt, forceProxyInt, protocol, id)
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
func effectiveProxyAttempts(ep Endpoint) int {
	if ep.AutoSwitch && ep.ProxyEnabled {
		if n := len(cleanProxyPool(ep.ProxyPool)); n > 0 {
			if n > proxyAttemptCap {
				return proxyAttemptCap
			}
			return n
		}
	}
	return 1
}

func cleanProxyPool(pool []string) []string {
	out := make([]string, 0, len(pool))
	seen := make(map[string]bool, len(pool))
	for _, raw := range pool {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if seen[entry] {
			continue
		}
		seen[entry] = true
		out = append(out, entry)
	}
	return out
}

// cleanProxyBatches 清洗代理批次：剔除无 ID / 无名称 / 无代理的批次，并清洗每条代理 URL。
func cleanProxyBatches(batches []ProxyBatch) []ProxyBatch {
	out := make([]ProxyBatch, 0, len(batches))
	for _, b := range batches {
		if strings.TrimSpace(b.ID) == "" || strings.TrimSpace(b.Name) == "" {
			continue
		}
		cleaned := cleanProxyPool(b.Proxies)
		if len(cleaned) == 0 {
			continue
		}
		b.Proxies = cleaned
		out = append(out, b)
	}
	return out
}

// mergeProxyPoolWithBatches 返回「手动代理 ∪ 全部批次代理」的去重并集，
// 保证运行时 proxy_pool 始终包含批次成员（客户端可能只提交其一）。
func mergeProxyPoolWithBatches(pool []string, batches []ProxyBatch) []string {
	merged := cleanProxyPool(pool)
	for _, b := range cleanProxyBatches(batches) {
		merged = cleanProxyPool(append(merged, b.Proxies...))
	}
	return merged
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// proxyCooldown 是一个代理被标记失败后的基准冷却间隔；连续失败按指数退避放大，
// 避免瞬时抖动把坏节点快速洗回池内。达到封顶后由预热探活（成功）恢复。
const (
	proxyCooldown      = 60 * time.Second
	proxyCooldownMax   = 30 * time.Minute
	proxyCooldownShift = 5
)

// proxy429BanThreshold 是同一代理累计 429 次数的阈值；达到后禁用 proxy429BanDuration。// opencode.ai/zen 按出口 IP 限流：连续 429 说明该 IP 已被上游限死，
// 若仍把它留在候选池内，重试循环会反复打到同一个 IP，白白消耗尝试次数。
// 禁用到期后自动释放回池（时间判断，无需主动清理）。
const (
	proxy429BanThreshold = 3
	proxy429BanDuration  = 30 * time.Minute
)

// proxyAllFrozenRetryInterval 是「全部出口禁用时自动解冻全体代理」的节流间隔：
// 距上次自动解冻不足该间隔时仍回退直连，避免上游 IP 级限流未恢复时反复
// 「解冻→又全部冻结→又解冻」打满全池的限流风暴。
const proxyAllFrozenRetryInterval = 10 * time.Minute

// proxyAttemptCap 是单次转发最多尝试的代理数量上限。代理池可容纳数千条
// （文件批量导入），但一次请求串行扫完整池会拖死请求：限流时每个出口都要
// 等一次完整往返。封顶后大池只轮询前 cap 个出口，配合 429 累计冻结，
// 被限死的出口会逐渐退出候选，剩余流量自动向健康出口集中。
// 与 firstTokenTimeout 配合构成单次转发最坏耗时预算：10s × cap，避免
// 池过大 / 出口过慢时把请求拖到客户端超时断开（回 502）。
const proxyAttemptCap = 8

// endpointRetryRounds 是全部候选端点均失败后，网关在内部重试整轮候选的
// 最大次数。对齐 New API 的 RetryTimes 语义：让客户端保持等待状态，网关
// 内部有耐心地反复重试所有候选，期间上游可能恢复。
// 0 = 不重试（试完即返回），3 = 试完候选后等待 500ms 再试，最多 3 轮。
const endpointRetryRounds = 3

var endpointRetryDelay = 500 * time.Millisecond

// sessionProxyRequestLimit 是同一会话在同一个出口 IP 上的请求数上限：
// 达到上限后主动轮换到下一个代理。opencode 等上游按出口 IP 限额时，
// 提前轮换可避免被限额后再重试（限额前主动换 IP）。
// 用 var 而非 const 以便测试注入更小的值。
var sessionProxyRequestLimit = 50

// recordEndpointLatency 记录端点最近一次转发延迟（毫秒），供延迟加权分流使用。
func (s *Service) recordEndpointLatency(endpointID string, latencyMs int64) {
	if endpointID == "" || latencyMs <= 0 {
		return
	}
	s.latencyMu.Lock()
	s.endpointLatency[endpointID] = latencyMs
	s.endpointLatencyOK[endpointID] = true
	s.latencyMu.Unlock()
}

// getEndpointLatency 读取端点最近转发延迟；无记录时返回 (0, false)。
func (s *Service) getEndpointLatency(endpointID string) (int64, bool) {
	s.latencyMu.RLock()
	defer s.latencyMu.RUnlock()
	ok := s.endpointLatencyOK[endpointID]
	return s.endpointLatency[endpointID], ok
}

// weightedEndpointPick 在可服务同一模型的端点中按延迟加权随机选择：
// 权重 = 1 + (maxLatency - latency) / 200，延迟越低的端点权重越高，
// 健康快的端点被选中概率更高；尚无延迟记录的端点按中等延迟（maxLatency）
// 参与，保证首次使用也有机会被选中。返回选中下标。
func weightedEndpointPick(latencies []int64, known []bool) int {
	maxLatency := int64(0)
	for i, latency := range latencies {
		if known[i] && latency > maxLatency {
			maxLatency = latency
		}
	}
	if maxLatency == 0 {
		maxLatency = 1000
	}

	total := int64(0)
	weights := make([]int64, len(latencies))
	for i, latency := range latencies {
		effective := latency
		if !known[i] {
			// 无记录端点视为中等延迟，避免被饿死。
			effective = maxLatency
		}
		weight := int64(1) + (maxLatency-effective)/200
		if weight < 1 {
			weight = 1
		}
		weights[i] = weight
		total += weight
	}
	if total <= 0 {
		return 0
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(total))
	acc := int64(0)
	for i, w := range weights {
		acc += w
		if n.Int64() < acc {
			return i
		}
	}
	return len(latencies) - 1
}

// randIntN 返回 [0, n) 内的安全随机整数；n <= 0 时返回 0。
// 用于并发请求需要打散到不同出口的场景（如全池解冻后分散起点）。
func randIntN(n int) int {
	if n <= 0 {
		return 0
	}
	bigN, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(bigN.Int64())
}

// normalizeProtocol 规范化端点连接协议设置：
//   - "" / auto：自动协商（HTTP/2 优先，服务端不支持时回退 HTTP/1.1），即默认行为
//   - http1：强制 HTTP/1.1（对齐主流 AI SDK / 官方客户端的传输层）
//   - h2：偏好 HTTP/2（标准库仅做 ALPN 协商，服务端不支持时仍回退 HTTP/1.1）
//
// 未知值一律回退 auto，避免旧配置 / 脏数据导致转发失败。
func normalizeProtocol(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "http1", "http/1.1", "http1.1", "h1":
		return "http1"
	case "h2", "http2", "http/2":
		return "h2"
	default:
		return "auto"
	}
}

// clientForProtocol 返回绑定指定连接协议的直连客户端。
// 客户端按协议名缓存，同一协议共享连接池；auto 与 h2 使用同一传输层配置
// （ForceAttemptHTTP2 开启、ALPN 协商），http1 关闭 HTTP/2 升级。
func (s *Service) clientForProtocol(protocol string) *http.Client {
	key := normalizeProtocol(protocol)
	s.protocolMu.Lock()
	defer s.protocolMu.Unlock()
	if c, ok := s.protocolClients[key]; ok {
		return c
	}
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
	if key == "http1" {
		// 关闭 HTTP/2 升级：既不尝试 h2 也不在 ALPN 中声明 h2，
		// 与 node fetch / curl 等 HTTP/1.1 客户端的传输行为一致。
		tr.ForceAttemptHTTP2 = false
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	c := &http.Client{Transport: tr}
	s.protocolClients[key] = c
	return c
}

// newEndpointProxyState 创建端点的代理池运行时状态。
func newEndpointProxyState() *endpointProxyState {
	return &endpointProxyState{
		cursor:          0,
		cooldown:        make(map[string]time.Time),
		failures:        make(map[string]int),
		sessionBindings: make(map[string]*sessionBinding),
		lastTTFB:        make(map[string]int64),
		rate429:         make(map[string]int),
		rateLimited:     make(map[string]time.Time),
		sunk:            make(map[string]time.Time),
		lastExitIP:      make(map[string]string),
		lastProbeAt:     make(map[string]time.Time),
	}
}

// clientForEndpoint 按端点代理池选择下一个可用代理，返回绑定该代理的 http.Client。
// 规则：
//   - proxyEnabled 关闭：忽略代理池，返回按端点 protocol 配置的直连客户端
//   - proxyEnabled 开启且池为空：forceProxy 开启时报错（禁止直连），否则回退直连
//   - proxyEnabled 开启且有池：按池选择代理；非空 sessionKey 时优先复用
//     会话粘性绑定的代理（同一会话固定出口，请求数达 sessionProxyRequestLimit 后
//     主动轮换下一个出口，规避上游按出口 IP 的限额）
func (s *Service) clientForEndpoint(endpointID string, pool []string, proxyEnabled, forceProxy bool, sessionKey, protocol string) (*http.Client, string, error) {
	if !proxyEnabled {
		return s.clientForProtocol(protocol), "", nil
	}
	cleaned := cleanProxyPool(pool)
	if len(cleaned) == 0 {
		if forceProxy {
			return nil, "", fmt.Errorf("端点配置为强制走代理，但代理池为空")
		}
		return s.client, "", nil
	}
	now := time.Now()

	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointProxyState()
		s.proxyStateByEndpoint[endpointID] = state
	}
	if state.cursor >= len(cleaned) || state.cursor < 0 {
		state.cursor = 0
	}

	// 会话粘性：绑定代理仍在池内、未冷却、未处于 429 禁用期、未被沉淀为坏代理、
	// 计数未达上限时直接复用，保持同一会话的出口 IP 稳定（配额感知轮换的前提）。
	if sessionKey != "" {
		if binding, bound := state.sessionBindings[sessionKey]; bound {
			bindingOK := false
			if until, cooled := state.cooldown[binding.proxy]; !cooled || now.After(until) {
				if !proxyRateLimited(state, binding.proxy, now) {
					if _, sunk := state.sunk[binding.proxy]; !sunk || now.After(state.sunk[binding.proxy]) {
						if binding.count < sessionProxyRequestLimit {
							if _, inPool := poolIndex(binding.proxy, cleaned); inPool {
								binding.count++
								bindingOK = true
							}
						}
					}
				}
			}
			if bindingOK {
				selectedProxy := binding.proxy
				state.cursor = (indexOfProxy(binding.proxy, cleaned) + 1) % len(cleaned)
				s.proxyMu.Unlock()
				client, err := s.proxyClient(selectedProxy)
				if err != nil {
					return s.client, selectedProxy, err
				}
				return client, selectedProxy, nil
			}
			// 绑定失效（冷却/429 禁用/沉淀/计数满/移出池）：解除并走择优换新出口。
			delete(state.sessionBindings, sessionKey)
		}
	}

	// 可用代理：未在冷却中、且未处于 429 禁用期、未被沉淀为坏代理的代理集合。
	candidates := []proxyCandidate{}
	for i := range cleaned {
		if until, cooled := state.cooldown[cleaned[i]]; cooled && !now.After(until) {
			continue
		}
		if proxyRateLimited(state, cleaned[i], now) {
			continue
		}
		if until, sunk := state.sunk[cleaned[i]]; sunk && now.Before(until) {
			continue
		}
		ttfb, known := state.lastTTFB[cleaned[i]]
		candidates = append(candidates, proxyCandidate{idx: i, ttfb: ttfb, known: known})
	}

	// 从候选集中选一个：优先让没有延迟记录的代理进入轮询（探索），
	// 全部已知后按延迟加权选择，避免单个最快代理独占全部流量。
	selectedIdx := -1
	switch {
	case len(candidates) == 0:
		// 全部冷却/禁用：退化为 cursor 轮询，先满足请求再说
		// （跳过 429 禁用中的代理，避免反复打同一个被限死的 IP）。
		for i := 0; i < len(cleaned); i++ {
			idx := (state.cursor + i) % len(cleaned)
			if proxyRateLimited(state, cleaned[idx], now) {
				continue
			}
			if until, sunk := state.sunk[cleaned[idx]]; sunk && now.Before(until) {
				continue
			}
			selectedIdx = idx
			break
		}
		if selectedIdx == -1 {
			// 全部出口都处于 429 冻结/坏代理沉淀：IP 级限流已把整个池锁死，硬选冻结代理只会
			// 反复 429（老行为：selectedIdx = cursor % len(pool) 直接选回被冻 IP，
			// 形成「全部冻结 → 每请求全池扫一遍 → 又全部冻结」的限流风暴）。
			// 自动解冻全体代理：清除全部冷却/429 冻结/沉淀状态，让池子重新获得
			// 出网机会（可能刚解冻即恢复）；但带节流，避免上游未恢复时反复解冻
			// 导致每请求都全池扫一遍。节流窗口内仍回退直连兜底。
			if s.autoUnfreezeAllLocked(endpointID, cleaned, now) {
				// 解冻成功：本次请求直接复用解冻后的池子，重新构建候选再择优。
				// 解冻后所有代理均可用（冷却/冻结/沉淀已清空），从 cursor 起取一个。
				// 解冻时刻会有大量并发请求同时涌入，若都从 cursor 起点开始会全部
				// 命中同一批代理，形成「刚解冻就被再次打爆」的雪崩。从 cursor 起加
				// 一个随机偏移，让并发请求散开到池内不同出口，避免集中重打。
				offset := 0
				if len(cleaned) > 1 {
					offset = randIntN(len(cleaned))
				}
				selectedIdx = (state.cursor + offset) % len(cleaned)
				break
			}
			s.logProxyPoolFrozen(endpointID, cleaned, now)
			s.proxyMu.Unlock()
			return s.client, "", nil
		}
	case len(candidates) == 1:
		selectedIdx = candidates[0].idx
	default:
		unknownAny := false
		for _, c := range candidates {
			if !c.known {
				unknownAny = true
				break
			}
		}
		if unknownAny {
			// 探索：在候选（未冷却）集合内按 cursor 轮询，绝不选中冷却代理。
			cursorPos := state.cursor % len(candidates)
			selectedIdx = candidates[cursorPos].idx
		} else {
			// 全部已知：延迟加权随机。延迟越低权重越高，但保留次优代理出现的机会，
			// 兼顾「选快代理」与「多代理分摊流量」。
			selectedIdx = weightedProxyPick(candidates)
		}
	}
	state.cursor = (selectedIdx + 1) % len(cleaned)
	selectedProxy := cleaned[selectedIdx]

	// 新出口绑定到会话（从 1 次计数开始），后续请求在此计数内保持同一出口。
	if sessionKey != "" {
		state.sessionBindings[sessionKey] = &sessionBinding{proxy: selectedProxy, count: 1}
	}
	s.proxyMu.Unlock()

	client, err := s.proxyClient(selectedProxy)
	if err != nil {
		return s.client, selectedProxy, err
	}
	return client, selectedProxy, nil
}

// poolIndex 返回 proxy 在 cleaned 池中的下标；不在池中返回 -1。
func poolIndex(proxy string, cleaned []string) (int, bool) {
	for i, p := range cleaned {
		if p == proxy {
			return i, true
		}
	}
	return -1, false
}

// indexOfProxy 返回 proxy 在池中的下标；不在池中时回退到 0（仅用于游标推进，无害）。
func indexOfProxy(proxy string, cleaned []string) int {
	if i, ok := poolIndex(proxy, cleaned); ok {
		return i
	}
	return 0
}

// clearSessionBinding 解除某端点下会话与出口 IP 的粘性绑定。
// 收到上游 429/5xx 切换出口时调用，使下一次请求重新绑定新出口（配额感知轮换）。
func (s *Service) clearSessionBinding(endpointID, sessionKey string) {
	if sessionKey == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	if state, ok := s.proxyStateByEndpoint[endpointID]; ok {
		delete(state.sessionBindings, sessionKey)
	}
}

// resolveSessionKey 从请求头或请求体中提取会话标识：
// 依次取 X-OpenCode-Session-ID / X-Opencode-Session-ID / X-Relay-Session-ID / X-Session-ID，
// 再回退到请求体 user 字段；都没有时返回空串（退化为池内轮询）。
func resolveSessionKey(r *http.Request, parsedBody map[string]interface{}) string {
	for _, h := range []string{"X-OpenCode-Session-ID", "X-Opencode-Session-ID", "X-Relay-Session-ID", "X-Session-ID"} {
		if v := strings.TrimSpace(r.Header.Get(h)); v != "" {
			return v
		}
	}
	if user, ok := parsedBody["user"].(string); ok {
		if user = strings.TrimSpace(user); user != "" {
			return "user:" + user
		}
	}
	return ""
}

// auxClientForPool 为辅助请求（验证、模型列表、健康检测等）选择代理 client。
// 与 clientForEndpoint 的区别：不推进端点的游标、不写 TTFB、不写冷却，
// 只读取冷却状态做跳过，避免辅助请求污染真实转发的择优状态。
func (s *Service) auxClientForPool(endpointID string, pool []string) (*http.Client, string) {
	if len(pool) == 0 {
		return s.client, ""
	}
	cleaned := cleanProxyPool(pool)
	if len(cleaned) == 0 {
		return s.client, ""
	}

	now := time.Now()
	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	selectedProxy := ""
	for _, candidate := range cleaned {
		if ok && state.cooldown[candidate] != (time.Time{}) && !now.After(state.cooldown[candidate]) {
			continue
		}
		if ok && proxyRateLimited(state, candidate, now) {
			continue
		}
		if ok {
			if until, sunk := state.sunk[candidate]; sunk && now.Before(until) {
				continue
			}
		}
		selectedProxy = candidate
		break
	}
	if selectedProxy == "" {
		// 全部冷却/禁用：退化为池内第一个，先满足请求再说。
		selectedProxy = cleaned[0]
	}
	s.proxyMu.Unlock()

	client, err := s.proxyClient(selectedProxy)
	if err != nil {
		return s.client, selectedProxy
	}
	return client, selectedProxy
}

// proxyCandidate 是择优时的候选代理：idx 为 cleaned 池中的下标，ttfb 为最近一次
// 首字耗时（毫秒），known 表示是否已产生过 TTFB 记录。
type proxyCandidate struct {
	idx   int
	ttfb  int64
	known bool
}

// weightedProxyPick 在全部已知延迟的候选代理中做加权选择：
// 权重 = 1 + (maxTTFB - ttfb) / 200，延迟越低的代理权重越高。
// 权重差按 200ms 为一档，既能让几百毫秒的快慢差异被感知，又不会让
// 极端慢代理彻底失去机会，从而兼顾「优先选快代理」与「多代理分摊流量」。
func weightedProxyPick(candidates []proxyCandidate) int {
	maxTTFB := int64(0)
	for _, c := range candidates {
		if c.ttfb > maxTTFB {
			maxTTFB = c.ttfb
		}
	}
	total := int64(0)
	weights := make([]int64, len(candidates))
	for i, c := range candidates {
		weight := int64(1) + (maxTTFB-c.ttfb)/200
		if weight < 1 {
			weight = 1
		}
		weights[i] = weight
		total += weight
	}
	if total <= 0 {
		return candidates[0].idx
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(total))
	acc := int64(0)
	for i, w := range weights {
		acc += w
		if n.Int64() < acc {
			return candidates[i].idx
		}
	}
	return candidates[len(candidates)-1].idx
}

// recordProxyTTFB 记录某端点下某代理的一次首字耗时，供后续请求择优。
func (s *Service) recordProxyTTFB(endpointID, proxy string, ttfbMs int64) {
	if endpointID == "" || proxy == "" || ttfbMs <= 0 {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointProxyState()
		s.proxyStateByEndpoint[endpointID] = state
	}
	state.lastTTFB[proxy] = ttfbMs
}

// markProxyFailed 将某个代理标记为冷却，后续选择会跳过它。
// 冷却时长按连续失败次数指数退避：1min << min(failures-1, 5)，封顶 30min。
// 只应在「传输层/链路」失败时调用；上游 429/5xx 不是代理的错，不应惩罚代理
// （否则上游故障会污染整个代理池）。
func (s *Service) markProxyFailed(endpointID, proxy string) {
	if proxy == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		return
	}
	state.failures[proxy]++
	fails := state.failures[proxy]
	shift := fails - 1
	if shift > proxyCooldownShift {
		shift = proxyCooldownShift
	}
	cooldown := proxyCooldown << shift
	if cooldown > proxyCooldownMax {
		cooldown = proxyCooldownMax
	}
	state.cooldown[proxy] = time.Now().Add(cooldown)
	s.persistProxyState(endpointID, proxy, "cooldown", state.cooldown[proxy])
}

// markProxySuccess 清除代理的失败计数与冷却（探活/预热成功时调用），使之立即恢复可选。
func (s *Service) markProxySuccess(endpointID, proxy string) {
	if proxy == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		return
	}
	delete(state.failures, proxy)
	delete(state.cooldown, proxy)
	s.persistProxyState(endpointID, proxy, "cooldown", time.Time{})
	s.persistProxyState(endpointID, proxy, "rate_limited", time.Time{})
	s.persistProxyState(endpointID, proxy, "sunk", time.Time{})
}

// markProxy429 记录代理的一次上游 429。与 markProxyFailed 的区别：
// 429 是上游按出口 IP 的限流，单次不惩罚代理（避免上游故障污染整个池）；
// 但同一代理累计 proxy429BanThreshold 次 429 说明该 IP 已被上游限死，
// 继续把它留在候选池只会让重试反复打同一个 IP，故临时禁用 proxy429BanDuration，
// 到期自动释放回池。成功转发不解除禁用；触发禁用时清零累计计数（重新累计下一轮）。
// retryAfter 非 nil 时优先用上游给出的 Retry-After 时长作为禁用期（封顶
// proxy429BanDuration），更贴合上游的配额恢复窗口；nil 时退回默认禁用期。
// 触发禁用时打 WARN 日志（此前冻结完全静默，难以确认熔断是否生效）。
func (s *Service) markProxy429(endpointID, proxy string, retryAfter *time.Duration) {
	if proxy == "" {
		return
	}
	s.proxyMu.Lock()
	defer s.proxyMu.Unlock()
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		// 辅助请求（健康检测/验证/模型列表）也会累计 429，首次出现时补建状态。
		state = newEndpointProxyState()
		s.proxyStateByEndpoint[endpointID] = state
	}
	state.rate429[proxy]++
	if state.rate429[proxy] >= proxy429BanThreshold {
		duration := proxy429BanDuration
		if retryAfter != nil && *retryAfter > 0 {
			if *retryAfter < duration {
				duration = *retryAfter
			}
		}
		state.rateLimited[proxy] = time.Now().Add(duration)
		delete(state.rate429, proxy)
		applog.Warn(context.Background(), "openai",
			"proxy frozen after repeated upstream 429s",
			"endpoint_id", endpointID,
			"proxy", hostFromProxyURL(proxy),
			"duration", duration.String(),
		)
		s.persistProxyState(endpointID, proxy, "rate_limited", state.rateLimited[proxy])
	}
}

// loadProxyState 启动时从 openai_proxy_state 表恢复代理池的持久化状态
// （429 冻结 / 连接失败冷却 / 坏代理沉淀）。只恢复尚未过期的记录；
// 过期记录在恢复时顺手清理，避免表无限增长。
// 幂等：重复调用只是再次把未过期状态写回内存（各 map 均为覆盖语义）。
func (s *Service) loadProxyState(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `
		SELECT endpoint_id, proxy, kind, until FROM openai_proxy_state`)
	if err != nil {
		return
	}
	defer rows.Close()
	now := time.Now()
	var stale [][3]string
	for rows.Next() {
		var endpointID, proxy, kind, untilRaw string
		if err := rows.Scan(&endpointID, &proxy, &kind, &untilRaw); err != nil {
			continue
		}
		until, err := time.Parse(time.RFC3339, untilRaw)
		if err != nil {
			continue
		}
		if !until.After(now) {
			stale = append(stale, [3]string{endpointID, proxy, kind})
			continue
		}
		s.proxyMu.Lock()
		state, ok := s.proxyStateByEndpoint[endpointID]
		if !ok {
			state = newEndpointProxyState()
			s.proxyStateByEndpoint[endpointID] = state
		}
		switch kind {
		case "rate_limited":
			state.rateLimited[proxy] = until
		case "cooldown":
			state.cooldown[proxy] = until
		case "sunk":
			state.sunk[proxy] = until
		}
		s.proxyMu.Unlock()
	}
	if len(stale) > 0 {
		for _, key := range stale {
			_, _ = db.ExecContext(ctx,
				"DELETE FROM openai_proxy_state WHERE endpoint_id=? AND proxy=? AND kind=?",
				key[0], key[1], key[2])
		}
	}
}

// proxyStateWriteDedup 是代理池状态持久化的写入去重表：
// 同一 (endpoint, proxy, kind) 在 proxyStateWriteDedupWindow 内只触发一次实际写库，
// 避免连接失败等高频事件把 DB 写入打爆（期间状态的最终值由补写时的 latest 决定，
// 慢一点覆盖没关系，只关心当前是否该恢复/清除）。
var proxyStateWriteDedup sync.Map

// proxyStateWriteWG 追踪代理池状态持久化的在途 goroutine，供测试在 TempDir
// 清理前等待落盘完成，避免 RemoveAll 竞态失败（Windows 下目录非空）。
var proxyStateWriteWG sync.WaitGroup

// proxyStateWriteDedupWindow 是同一键持久化去重的窗口时长。
const proxyStateWriteDedupWindow = 30 * time.Second

// persistProxyState 把代理池的一条运行时状态异步持久化到 openai_proxy_state：
// until 为零值时表示清除该条记录（代理已恢复）。
// 使用独立短连接与 goroutine，避免阻塞转发热路径；同一键的并发写由
// SQLite 的 UPSERT 语义自然收敛为最终值。写入带去重窗口，低频高频均安全。
func (s *Service) persistProxyState(endpointID, proxy, kind string, until time.Time) {
	if endpointID == "" || proxy == "" || kind == "" {
		return
	}
	key := endpointID + "\x00" + proxy + "\x00" + kind
	now := time.Now()
	if v, ok := proxyStateWriteDedup.Load(key); ok {
		if last, _ := v.(time.Time); now.Sub(last) < proxyStateWriteDedupWindow {
			return
		}
	}
	proxyStateWriteDedup.Store(key, now)
	proxyStateWriteWG.Add(1)
	go func() {
		defer proxyStateWriteWG.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		db, err := s.open(ctx)
		if err != nil {
			return
		}
		defer db.Close()
		if until.IsZero() {
			_, _ = db.ExecContext(ctx,
				"DELETE FROM openai_proxy_state WHERE endpoint_id=? AND proxy=? AND kind=?",
				endpointID, proxy, kind)
			return
		}
		_, _ = db.ExecContext(ctx, `
			INSERT INTO openai_proxy_state(endpoint_id, proxy, kind, until, created_at)
			VALUES(?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(endpoint_id, proxy, kind) DO UPDATE SET until=excluded.until`,
			endpointID, proxy, kind, until.UTC().Format(time.RFC3339))
	}()
}

// retryAfterFromHeader 解析上游响应的 Retry-After 头为时长。
// 仅支持秒数形式（RFC 7231 的 HTTP-date 形式较少见，且与配额窗口语义不符）；
// 头缺失或解析失败返回 nil。禁用期上限由调用方与 proxy429BanDuration 封顶。
func retryAfterFromHeader(resp *http.Response) *time.Duration {
	if resp == nil {
		return nil
	}
	raw := strings.TrimSpace(resp.Header.Get("Retry-After"))
	if raw == "" {
		return nil
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds <= 0 {
		return nil
	}
	d := time.Duration(seconds) * time.Second
	return &d
}

// logProxyPoolFrozen 在全部出口因 429 冻结、回退直连时记录 WARN。
// 同一端点 10 分钟内只记一次，避免并发请求刷屏。调用方需持有 proxyMu。
func (s *Service) logProxyPoolFrozen(endpointID string, pool []string, now time.Time) {
	state, ok := s.proxyStateByEndpoint[endpointID]
	if ok && now.Sub(state.lastAllFrozenLog) < 10*time.Minute {
		return
	}
	if ok {
		state.lastAllFrozenLog = now
	}
	sample := ""
	if len(pool) > 0 {
		sample = hostFromProxyURL(pool[0])
	}
	applog.Warn(context.Background(), "openai",
		"proxy pool fully frozen by upstream 429s, falling back to direct connection",
		"endpoint_id", endpointID,
		"pool_size", len(pool),
		"sample_proxy", sample,
		"until", now.Add(proxy429BanDuration).Format(time.RFC3339),
	)
}

// autoUnfreezeAllLocked 在全部出口被禁用（429 冻结/坏代理沉淀）时自动解冻全体代理：
// 清除池内全部出口的冷却、429 冻结与沉淀状态，使池子重新可选。带节流：
// 距上次自动解冻不足 proxyAllFrozenRetryInterval 时不执行（返回 false，调用方回退直连）。
// 调用方需持有 proxyMu。
func (s *Service) autoUnfreezeAllLocked(endpointID string, pool []string, now time.Time) bool {
	state, ok := s.proxyStateByEndpoint[endpointID]
	if !ok {
		return false
	}
	if !state.lastAllUnfrozen.IsZero() && now.Sub(state.lastAllUnfrozen) < proxyAllFrozenRetryInterval {
		return false
	}
	state.lastAllUnfrozen = now
	for _, proxy := range pool {
		delete(state.cooldown, proxy)
		delete(state.rateLimited, proxy)
		delete(state.rate429, proxy)
		delete(state.sunk, proxy)
		delete(state.failures, proxy)
		s.persistProxyState(endpointID, proxy, "cooldown", time.Time{})
		s.persistProxyState(endpointID, proxy, "rate_limited", time.Time{})
		s.persistProxyState(endpointID, proxy, "sunk", time.Time{})
	}
	applog.Warn(context.Background(), "openai",
		"proxy pool fully disabled, auto-unfroze all proxies",
		"endpoint_id", endpointID,
		"pool_size", len(pool),
	)
	return true
}

// proxyRateLimited 判断代理是否处于 429 累计触发的禁用期（禁用中不可被选中）。
func proxyRateLimited(state *endpointProxyState, proxy string, now time.Time) bool {
	until, banned := state.rateLimited[proxy]
	return banned && now.Before(until)
}

// pickKey 从端点全部 key 中按轮询选出一个 key，返回 (key, index)。
// key 永不冻结：triedKeys 记录本次请求内已尝试失败的 key，跳过它们避免无限重试；
// 全部 key 均已在本轮尝试失败时返回 ("", -1)，由调用方触发端点级切换。
// 429 绝不冻结 key，只靠轮询天然分散 RPM 压力。
func (s *Service) pickKey(endpointID string, keys []string, triedKeys map[string]bool) (string, int) {
	cleaned := cleanKeyList(keys)
	if len(cleaned) == 0 {
		return "", -1
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	state, ok := s.keyStateByEndpoint[endpointID]
	if !ok {
		state = newEndpointKeyState()
		s.keyStateByEndpoint[endpointID] = state
	}
	if state.cursor < 0 || state.cursor >= len(cleaned) {
		state.cursor = 0
	}
	start := state.cursor
	for i := 0; i < len(cleaned); i++ {
		idx := (start + i) % len(cleaned)
		if triedKeys != nil && triedKeys[cleaned[idx]] {
			continue
		}
		state.cursor = (idx + 1) % len(cleaned)
		return cleaned[idx], idx
	}
	return "", -1
}

// cleanKeyList 清洗并去重 API Key 列表（保留顺序，剔除空串）。
func cleanKeyList(keys []string) []string {
	out := make([]string, 0, len(keys))
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// normalizeProxyURL 校验并规范化代理 URL：
//   - socks://socks5://socks5h:// 与裸 host:port 统一为 socks5（远端解析域名）
//   - 仅接受 socks5 与 http/https 代理，其余协议报错
func normalizeProxyURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	switch u.Scheme {
	case "socks5", "socks", "socks5h":
		// socks:// 是常见订阅节点前缀，socks5h 表示远端解析域名，均按 SOCKS5 处理。
		u.Scheme = "socks5"
	case "":
		// 裸地址（host:port）默认按 socks5 处理，便于直接粘贴节点地址。
		u = &url.URL{Scheme: "socks5", Host: strings.TrimSpace(raw)}
	default:
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("不支持的代理协议: %s", u.Scheme)
		}
	}
	if u.Host == "" {
		return nil, fmt.Errorf("代理地址缺少 host:port: %s", raw)
	}
	return u, nil
}

// configureProxyTransport 把代理绑定到 transport（复刻 New API 的渠道代理隔离做法）：
//   - socks5：用 x/net/proxy 构造支持 context 取消的拨号器，替代标准库不支持的 http.ProxyURL
//   - http/https：直接使用 http.ProxyURL
//
// 返回的 transport 在启用代理后不依赖环境变量（HTTP_PROXY/HTTPS_PROXY），
// 保证出口严格落在显式配置的代理上，避免「代理池外 IP」出现。
func configureProxyTransport(tr *http.Transport, u *url.URL) error {
	switch u.Scheme {
	case "http", "https":
		tr.Proxy = http.ProxyURL(u)
		return nil
	case "socks5":
		tr.Proxy = nil
		forwardDialer := &net.Dialer{
			Timeout:   4 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		dialer, err := proxy.FromURL(u, forwardDialer)
		if err != nil {
			return err
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return fmt.Errorf("SOCKS5 代理拨号器不支持 context 取消")
		}
		tr.DialContext = contextDialer.DialContext
		return nil
	default:
		return fmt.Errorf("不支持的代理协议: %s", u.Scheme)
	}
}

// proxyClients 按代理 URL 缓存 http.Client，避免每次请求重建 transport。
var proxyClients = struct {
	sync.Mutex
	m map[string]*http.Client
}{m: make(map[string]*http.Client)}

func (s *Service) proxyClient(proxyURL string) (*http.Client, error) {
	u, err := normalizeProxyURL(proxyURL)
	if err != nil {
		return nil, err
	}
	proxyClients.Lock()
	defer proxyClients.Unlock()
	if c, ok := proxyClients.m[proxyURL]; ok {
		return c, nil
	}
	tr := &http.Transport{
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
		// 兜底限制「等待响应头」的时间；排队的上游（免费模型高峰）可能 30s+ 才
		// 返回响应头，固定 30s 会误杀「慢但最终成功」的请求，故放宽到 180s。
		// 首字失败切换由转发循环的 firstTokenTimeout（收到响应头后等首块）控制。
		ResponseHeaderTimeout: 180 * time.Second,
	}
	if err := configureProxyTransport(tr, u); err != nil {
		return nil, err
	}
	c := &http.Client{Transport: tr}
	proxyClients.m[proxyURL] = c
	return c, nil
}

// readWithIdleTimeout 为阻塞式上游读加中段空闲超时：idle 内无任何字节到达
// 则返回 errStreamIdleTimeout，避免上游流中途停滞时请求无限挂死。
// 超时后遗留的读取 goroutine 会在上游数据到达或连接关闭后自行退出。
func readWithIdleTimeout(ctx context.Context, r io.Reader, p []byte, idle time.Duration) (int, error) {
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		n, err := r.Read(p)
		select {
		case ch <- readResult{n: n, err: err}:
		case <-ctx.Done():
		}
	}()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(idle):
		return 0, errStreamIdleTimeout
	}
}

var errStreamIdleTimeout = errors.New("upstream stream idle timeout")

// isRateLimitResponse 判断上游响应是否命中限流（429/439 或正文含限流关键词）。
// 注意：503/529 是上游过载/停机信号，不属于客户端限流，不应计入「连续 429 冻结
// 代理」的累计（否则瞬时过载会把一个健康代理冻结 30 分钟）；正文关键词仍能覆盖
// 携带过载语义的 503 响应。
func isRateLimitResponse(resp *http.Response, body []byte) bool {
	switch resp.StatusCode {
	case http.StatusTooManyRequests, 439:
		return true
	}
	if len(body) > 0 {
		lower := strings.ToLower(string(body))
		for _, keyword := range []string{"rate limit", "rate_limit", "too many requests", "overloaded", "throttled"} {
			if strings.Contains(lower, keyword) {
				return true
			}
		}
	}
	return false
}

// unavailableStatusCode 在所有候选端点均失败后，根据各端点失败码聚合决定返回给客户端的
// 状态码：全一致（如全部 429）→ 透传该码；不一致 → 503。供调用方在自己写响应时使用。
func unavailableStatusCode(model string, failCodes []int) int {
	if len(failCodes) > 0 {
		first := failCodes[0]
		allSame := true
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		if allSame && first >= 400 && first < 600 {
			return first
		}
	}
	return http.StatusServiceUnavailable
}

// writeRelayUnavailable 在所有候选端点均失败后，聚合各端点失败状态码决定返回给客户端的错误：
//   - 所有端点失败码一致（如全部 429）→ 透传该码，并说明网关无可用渠道。
//   - 失败码不一致或不在 4xx/5xx 内 → 返回 503 网关无可用渠道。
//
// 这类「所有渠道耗尽」属于网关自身状态，不额外写入调用日志（各尝试已在 relayLoop 内记录）。
func writeRelayUnavailable(w http.ResponseWriter, model string, failCodes []int) {
	if len(failCodes) > 0 {
		first := failCodes[0]
		allSame := true
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		if allSame && first >= 400 && first < 600 {
			msg := fmt.Sprintf("网关无可用渠道（模型 %s）：所有端点均返回 HTTP %d", model, first)
			response.JSON(w, first, map[string]interface{}{
				"error": map[string]string{"message": msg, "type": "service_unavailable"},
			})
			return
		}
	}
	response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
		"error": map[string]string{
			"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
			"type":    "service_unavailable",
		},
	})
}

// isRetryableUpstreamResponse 判断上游响应是否值得切换到下一个代理重试：
// 限流（429/439/503/529 或限流关键词）与常见 5xx 服务器错误（500/502/504/599）。
// 501/505 等表示请求本身语义问题，重试无意义，不纳入。
func isRetryableUpstreamResponse(resp *http.Response, body []byte) bool {
	if isRateLimitResponse(resp, body) {
		return true
	}
	switch resp.StatusCode {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout,
		http.StatusServiceUnavailable, 529, 599:
		return true
	}
	return false
}

// cleanHeaders 过滤掉空名称的条目，其余原样保留。
func cleanHeaders(headers []HeaderItem) []HeaderItem {
	out := make([]HeaderItem, 0, len(headers))
	for _, h := range headers {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			continue
		}
		out = append(out, HeaderItem{Name: name, Value: h.Value})
	}
	return out
}

// isModelDisabled 判断给定模型是否在端点禁用的模型列表中。
func isModelDisabled(disabled []string, model string) bool {
	for _, m := range disabled {
		if m == model {
			return true
		}
	}
	return false
}

// decodeEndpointHeaders 从数据库的 headers JSON 列还原自定义请求头。
func decodeEndpointHeaders(raw sql.NullString) []HeaderItem {
	headers := []HeaderItem{}
	if raw.Valid && raw.String != "" {
		_ = json.Unmarshal([]byte(raw.String), &headers)
	}
	return cleanHeaders(headers)
}

// decodeProxyPool 从数据库读取端点代理池（JSON 字符串数组）。
func decodeProxyPool(raw sql.NullString) []string {
	pool := []string{}
	if raw.Valid && raw.String != "" {
		_ = json.Unmarshal([]byte(raw.String), &pool)
	}
	return cleanProxyPool(pool)
}

// applyCustomHeaders 把端点配置的自定义请求头写入待发请求。
// 网关自身的鉴权（Authorization 等）在调用方设置，自定义头允许覆盖非鉴权头。
func applyCustomHeaders(req *http.Request, headers []HeaderItem) {
	for _, h := range headers {
		name := strings.TrimSpace(h.Name)
		if name == "" {
			continue
		}
		req.Header.Set(name, h.Value)
	}
}

func (s *Service) toggleEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Enabled bool `json:"enabled"`
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

	var exists int
	err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_endpoints WHERE id = ?", id).Scan(&exists)
	if err != nil || exists == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	enabledVal := 0
	if req.Enabled {
		enabledVal = 1
	}

	_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET enabled = ? WHERE id = ?", enabledVal, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "enabled": req.Enabled})
}

func (s *Service) deleteEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	res, err := db.ExecContext(ctx, "DELETE FROM openai_endpoints WHERE id = ?", id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	rows, err := res.RowsAffected()
	if err != nil || rows == 0 {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// reorderEndpoints 保存端点拖拽排序结果：按传入顺序重写 sort_order。
// 使用事务保证全部成功或全部失败；仅校验 id 存在，不要求全部端点都在列表内。
func (s *Service) reorderEndpoints(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EndpointIDs []string `json:"endpointIds"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if len(req.EndpointIDs) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "endpointIds 不能为空"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	for idx, endpointID := range req.EndpointIDs {
		res, err := tx.ExecContext(ctx,
			"UPDATE openai_endpoints SET sort_order = ? WHERE id = ?",
			(idx+1)*1000, endpointID)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			response.JSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("端点不存在: %s", endpointID)})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) verifyEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	// 验证类请求使用独立短超时，避免上游无响应时拖住整个操作。
	verifyCtx, verifyCancel := context.WithTimeout(ctx, 10*time.Second)
	defer verifyCancel()

	var name, baseURL, apiKey string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT name, base_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&name, &baseURL, &apiKey, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)
	headers := decodeEndpointHeaders(headersRaw)
	pool := decodeProxyPool(proxyRaw)

	startTime := time.Now()
	status := "invalid"
	modelsList := []string{}
	var errMsg string

	vOk, _, vErr := s.verifyAPIKeyRaw(verifyCtx, baseURL, apiKey, id, pool, headers)
	responseTime := time.Since(startTime).Milliseconds()

	if vErr == nil && vOk {
		status = "valid"
		mList, mErr := s.listModelsRaw(verifyCtx, baseURL, apiKey, id, pool, headers)
		if mErr == nil {
			modelsList = mList
		}
	} else if vErr != nil {
		errMsg = vErr.Error()
	}

	checkedAt := time.Now().Format(time.RFC3339)

	// 验证失败时保留旧的模型列表：一次超时/临时网络故障不应清空已获取的模型。
	modelsJSON := "[]"
	if status == "valid" && len(modelsList) > 0 {
		modelsJSONBytes, _ := json.Marshal(modelsList)
		modelsJSON = string(modelsJSONBytes)
	} else if status == "valid" {
		// 验证成功但返回空列表：视为真实空（首次接入或上游确实无模型）。
		modelsJSON = "[]"
	}

	_, err = db.ExecContext(ctx, `
		UPDATE openai_endpoints
		SET status = ?, models = ?, last_checked = ?
		WHERE id = ?`,
		status, modelsJSON, checkedAt, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	res := map[string]interface{}{
		"status":       status,
		"responseTime": responseTime,
		"modelsCount":  len(modelsList),
		"models":       modelsList,
		"checkedAt":    checkedAt,
		"valid":        status == "valid",
	}
	if errMsg != "" {
		res["error"] = errMsg
	}

	response.JSON(w, http.StatusOK, res)
}

// KeyCheckResult 描述一次 API Key 校验结果：status 取值 valid/invalid/overdue/error。
type KeyCheckResult struct {
	Index      int    `json:"index"`
	Key        string `json:"key"`
	Status     string `json:"status"`
	StatusCode int    `json:"statusCode,omitempty"`
	Message    string `json:"message,omitempty"`
}

// healthCheckKeysRoute 对端点配置的多个 API Key 逐个做有效性检测（GET /models）。
// 用于端点编辑弹窗里的 key 管理：进入弹窗时自动刷新状态，快速识别失效/欠费 key。
func (s *Service) healthCheckKeysRoute(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Keys    []string `json:"keys"`
		Timeout int      `json:"timeout"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	keys := make([]string, 0, len(req.Keys))
	for _, k := range req.Keys {
		k = strings.TrimSpace(k)
		if k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "keys 不能为空"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	timeout := 8 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Millisecond
	}
	pool := decodeProxyPool(proxyRaw)
	headers := decodeEndpointHeaders(headersRaw)

	results := make([]KeyCheckResult, len(keys))
	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Add(1)
		go func(idx int, k string) {
			defer wg.Done()
			results[idx] = s.checkAPIKeyStatus(ctx, baseURL, k, id, timeout, pool, headers, idx)
		}(i, key)
	}
	wg.Wait()

	response.JSON(w, http.StatusOK, map[string]interface{}{"results": results})
}

// checkAPIKeyStatus 用 GET {baseURL}/models 检测单个 key 的有效性。
// 2xx=valid；401/403=invalid（鉴权失败）；402=overdue（欠费）；其余/网络错误=error。
func (s *Service) checkAPIKeyStatus(ctx context.Context, baseURL, key, endpointID string, timeout time.Duration, pool []string, headers []HeaderItem, index int) KeyCheckResult {
	result := KeyCheckResult{Index: index, Key: key}
	childCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	reqURL := fmt.Sprintf("%s/models", strings.TrimSuffix(baseURL, "/"))
	httpReq, err := http.NewRequestWithContext(childCtx, "GET", reqURL, nil)
	if err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return result
	}
	httpReq.Header.Set("Authorization", "Bearer "+key)
	applyCustomHeaders(httpReq, headers)

	client := s.client
	if len(pool) > 0 {
		if poolClient, _ := s.auxClientForPool(endpointID, pool); poolClient != nil {
			client = poolClient
		}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		result.Status = "error"
		result.Message = err.Error()
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		result.Status = "valid"
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		result.Status = "invalid"
		result.Message = "鉴权失败"
	case resp.StatusCode == http.StatusPaymentRequired:
		result.Status = "overdue"
		result.Message = "欠费"
	default:
		result.Status = "error"
		result.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return result
}

func (s *Service) getEndpointModels(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	// 模型列表拉取使用独立短超时，避免上游无响应时拖住整个操作。
	modelsCtx, modelsCancel := context.WithTimeout(ctx, 10*time.Second)
	defer modelsCancel()

	var baseURL, apiKey string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)

	modelsList, err := s.listModelsRaw(modelsCtx, baseURL, apiKey, id, decodeProxyPool(proxyRaw), decodeEndpointHeaders(headersRaw))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	modelsJSON, _ := json.Marshal(modelsList)
	checkedAt := time.Now().Format(time.RFC3339)

	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET models = ?, last_checked = ? WHERE id = ?", string(modelsJSON), checkedAt, id)
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"models":  modelsList,
	})
}

func (s *Service) toggleEndpointModel(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model   string `json:"model"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "模型名称必填"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var disabledRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT disabled_models FROM openai_endpoints WHERE id = ?", id).Scan(&disabledRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	disabled := []string{}
	if disabledRaw.Valid && disabledRaw.String != "" {
		_ = json.Unmarshal([]byte(disabledRaw.String), &disabled)
	}

	disabledSet := make(map[string]bool, len(disabled)+1)
	for _, m := range disabled {
		disabledSet[m] = true
	}
	if req.Enabled {
		delete(disabledSet, model)
	} else {
		disabledSet[model] = true
	}

	next := make([]string, 0, len(disabledSet))
	for m := range disabledSet {
		next = append(next, m)
	}
	sort.Strings(next)

	disabledJSON, _ := json.Marshal(next)
	_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET disabled_models = ? WHERE id = ?", string(disabledJSON), id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"model":          model,
		"enabled":        req.Enabled,
		"disabledModels": next,
	})
}

// toggleEndpointModelsBatch 批量启用/停用端点上的多个模型。
// 单次「读-改-写」原子完成，避免前端并发逐个 toggle 时互相覆盖丢失。
func (s *Service) toggleEndpointModelsBatch(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Models  []string `json:"models"`
		Enabled bool     `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cleaned := make([]string, 0, len(req.Models))
	seen := make(map[string]bool, len(req.Models))
	for _, m := range req.Models {
		trimmed := strings.TrimSpace(m)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		cleaned = append(cleaned, trimmed)
	}
	if len(cleaned) == 0 {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "模型列表不能为空"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var disabledRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT disabled_models FROM openai_endpoints WHERE id = ?", id).Scan(&disabledRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	disabledSet := make(map[string]bool)
	if disabledRaw.Valid && disabledRaw.String != "" {
		var existing []string
		if err := json.Unmarshal([]byte(disabledRaw.String), &existing); err == nil {
			for _, m := range existing {
				disabledSet[m] = true
			}
		}
	}
	for _, m := range cleaned {
		if req.Enabled {
			delete(disabledSet, m)
		} else {
			disabledSet[m] = true
		}
	}

	next := make([]string, 0, len(disabledSet))
	for m := range disabledSet {
		next = append(next, m)
	}
	sort.Strings(next)

	disabledJSON, _ := json.Marshal(next)
	_, err = db.ExecContext(ctx, "UPDATE openai_endpoints SET disabled_models = ? WHERE id = ?", string(disabledJSON), id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"enabled":        req.Enabled,
		"disabledModels": next,
	})
}

// subscriptionProxy 描述一个可从订阅节点导入到端点代理池的 socks/http 出口。
type subscriptionProxy struct {
	NodeID   string `json:"nodeId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Proxy    string `json:"proxy"`
	Location string `json:"location,omitempty"`
}

// listSubscriptionSocksProxies 读取订阅板块中的 socks/http 协议节点，转换为可直接使用的代理 URL。
func (s *Service) listSubscriptionSocksProxies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `SELECT id, COALESCE(name,''), COALESCE(type,''), COALESCE(server,''), COALESCE(port,0), COALESCE(location,''), COALESCE(raw_encrypted,'') FROM subscription_nodes WHERE enabled = 1`)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	proxies := []subscriptionProxy{}
	seen := make(map[string]bool)
	for rows.Next() {
		var item subscriptionProxy
		var rawEnc string
		if err := rows.Scan(&item.NodeID, &item.Name, &item.Type, &item.Server, &item.Port, &item.Location, &rawEnc); err != nil {
			continue
		}
		item.Type = strings.ToLower(strings.TrimSpace(item.Type))
		if item.Type != "socks" && item.Type != "socks5" && item.Type != "http" && item.Type != "https" {
			continue
		}
		raw := secure.SecureDecrypt(rawEnc)
		proxy, name, ok := convertNodeToProxy(item.Type, raw, item.Server, item.Port, item.Name)
		if !ok || proxy == "" {
			continue
		}
		item.Proxy = proxy
		item.Name = name
		if seen[proxy] {
			continue
		}
		seen[proxy] = true
		proxies = append(proxies, item)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": proxies})
}

// importProxyListRoute 解析用户上传的代理列表文本（例如 .txt 文件内容，每行一个代理），
// 清洗并去重后返回可直接写入端点代理池的代理 URL 列表及统计。
// 支持 http(s)://、socks5:// 与裸 host:port；也兼容 base64 编码的订阅文本。
func (s *Service) importProxyListRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	const maxImportBytes = 16 * 1024 * 1024
	if len(req.Text) > maxImportBytes {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "代理列表过大（上限 16MB）"})
		return
	}

	proxies := parseSubscriptionProxyText(req.Text)
	urls := make([]string, 0, len(proxies))
	for _, p := range proxies {
		urls = append(urls, p.Proxy)
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"total":   len(urls),
		"proxies": urls,
	})
}

// resolveSubscriptionProxies 拉取用户粘贴的订阅链接，解析其中的 socks/http 节点，
// 转换为可直接写入端点代理池的代理 URL。
func (s *Service) resolveSubscriptionProxies(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败"})
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "请填写订阅链接"})
		return
	}
	if !strings.HasPrefix(strings.ToLower(req.URL), "http://") && !strings.HasPrefix(strings.ToLower(req.URL), "https://") {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "订阅链接必须以 http:// 或 https:// 开头"})
		return
	}

	ctx := r.Context()
	fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, req.URL, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "构造请求失败: " + err.Error()})
		return
	}
	httpReq.Header.Set("User-Agent", "API-Monitor-OpenAI/1.0")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		response.JSON(w, http.StatusBadGateway, map[string]string{"error": "拉取订阅失败: " + err.Error()})
		return
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		response.JSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("订阅源返回 HTTP %d", resp.StatusCode)})
		return
	}

	proxies := parseSubscriptionProxyText(string(body))
	if len(proxies) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"proxies": []subscriptionProxy{},
			"message": "订阅内容中没有找到 socks/http 节点",
		})
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": proxies})
}

// parseSubscriptionProxyText 解析订阅内容（可能是 base64 节点列表或纯文本），
// 提取其中的 socks/socks5/http/https 节点为代理 URL。
func parseSubscriptionProxyText(content string) []subscriptionProxy {
	text := strings.TrimSpace(content)
	lines := []string{}
	// 尝试 base64 解码：订阅常见格式是 base64 编码的每行一个节点 URI。
	decoded := decodeBase64Text(text)
	if decoded != "" {
		text = decoded
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}

	proxies := []subscriptionProxy{}
	seen := make(map[string]bool)
	for _, line := range lines {
		proxy, name, ok := convertNodeToProxy("", line, "", 0, "")
		if !ok || proxy == "" {
			continue
		}
		// 仅接受 socks/http 出口；裸 server:port 也归为 socks5 出口。
		scheme := proxy
		if idx := strings.Index(proxy, "://"); idx >= 0 {
			scheme = strings.ToLower(proxy[:idx])
		}
		if scheme != "socks5" && scheme != "http" && scheme != "https" {
			continue
		}
		if seen[proxy] {
			continue
		}
		seen[proxy] = true
		proxies = append(proxies, subscriptionProxy{
			Name:   name,
			Type:   scheme,
			Proxy:  proxy,
			Server: hostFromProxyURL(proxy),
		})
	}
	return proxies
}

// decodeBase64Text 尝试将内容按 base64 解码；成功且结果可读时返回解码文本。
func decodeBase64Text(text string) string {
	compact := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, text)
	raw, err := base64.StdEncoding.DecodeString(compact)
	if err != nil {
		return ""
	}
	decoded := string(raw)
	if strings.Contains(decoded, "://") || strings.Contains(decoded, "vmess://") || strings.Contains(decoded, "trojan://") || strings.Contains(decoded, "ss://") {
		return decoded
	}
	return ""
}

// hostFromProxyURL 从代理 URL 中提取 host 部分用于展示。
func hostFromProxyURL(proxy string) string {
	u, err := url.Parse(proxy)
	if err != nil {
		return proxy
	}
	return u.Host
}

// convertNodeToProxy 把订阅节点 raw URI 转换为网关可用的 socks5/http 代理 URL。
// 优先复用 raw 中的用户凭据；raw 无法解析时回退为 server:port。
func convertNodeToProxy(nodeType, raw, server string, port int, fallbackName string) (proxy, name string, ok bool) {
	name = strings.TrimSpace(fallbackName)
	if name == "" {
		name = server
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(trimmed), "socks://") ||
		strings.HasPrefix(strings.ToLower(trimmed), "socks5://") ||
		strings.HasPrefix(strings.ToLower(trimmed), "http://") ||
		strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		u, err := url.Parse(trimmed)
		if err == nil && u.Host != "" {
			scheme := u.Scheme
			if scheme == "socks" || scheme == "socks5" {
				scheme = "socks5"
			}
			u.Scheme = scheme
			// 去掉 fragment（节点名常放在 # 后）。
			u.Fragment = ""
			if fragName := parseNodeFragment(trimmed); fragName != "" {
				name = fragName
			}
			return u.String(), name, true
		}
	}
	// 无 raw 或解析失败：直接用 server:port 构造成 socks5。
	if server == "" || port < 1 || port > 65535 {
		return "", name, false
	}
	return fmt.Sprintf("socks5://%s", net.JoinHostPort(server, strconv.Itoa(port))), name, true
}

func parseNodeFragment(raw string) string {
	idx := strings.LastIndex(raw, "#")
	if idx < 0 || idx+1 >= len(raw) {
		return ""
	}
	name := strings.TrimSpace(raw[idx+1:])
	name = strings.Trim(name, "\"")
	return name
}

func (s *Service) testEndpointChat(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	modelName := req.Model
	if modelName == "" {
		modelName = "gpt-3.5-turbo"
	}

	ctx := r.Context()
	// 端点测试使用独立短超时，避免上游无响应时拖住整个操作。
	testCtx, testCancel := context.WithTimeout(ctx, 10*time.Second)
	defer testCancel()

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)
	pool := decodeProxyPool(proxyRaw)

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	chatPayload := map[string]interface{}{
		"model": modelName,
		"messages": []map[string]string{
			{"role": "user", "content": "Say \"Hello, API test successful!\" in exactly those words."},
		},
		"max_tokens": 50,
	}
	bodyBytes, _ := json.Marshal(chatPayload)

	reqURL := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(baseURL, "/"))
	httpReq, err := http.NewRequestWithContext(testCtx, "POST", reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	applyCustomHeaders(httpReq, decodeEndpointHeaders(headersRaw))

	client := s.client
	selectedProxy := ""
	if len(pool) > 0 {
		poolClient, proxy := s.auxClientForPool(id, pool)
		if poolClient != nil {
			client = poolClient
			selectedProxy = proxy
		}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(id, selectedProxy, retryAfterFromHeader(resp))
		}
		respBytes, _ := io.ReadAll(resp.Body)
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBytes))})
		return
	}

	var chatResponse struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage interface{} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&chatResponse); err != nil {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": false, "error": "无法解析响应 JSON"})
		return
	}

	reply := ""
	if len(chatResponse.Choices) > 0 {
		reply = chatResponse.Choices[0].Message.Content
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"response": reply,
		"usage":    chatResponse.Usage,
	})
}

func (s *Service) healthCheckModelRoute(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Model   string `json:"model"`
		Timeout int    `json:"timeout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Model == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "模型名称必填"})
		return
	}

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	pool := decodeProxyPool(proxyRaw)
	result := s.healthCheckSingleModel(ctx, id, baseURL, apiKey, req.Model, timeoutDuration, pool, decodeEndpointHeaders(headersRaw))

	// Save check to health history
	var errMsg sql.NullString
	if result.Error != "" {
		errMsg.Valid = true
		errMsg.String = result.Error
	}
	_, _ = db.ExecContext(ctx, `
		INSERT INTO openai_health_history (endpoint_id, status, response_time, error_message, checked_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, result.Status, result.Latency, errMsg, result.CheckedAt)

	response.JSON(w, http.StatusOK, result)
}

func (s *Service) healthCheckAllModelsRoute(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Timeout     int `json:"timeout"`
		Concurrency int `json:"concurrency"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey, modelsRaw string
	var headersRaw, proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, models, headers, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &modelsRaw, &headersRaw, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	apiKey = secure.SecureDecrypt(apiKey)
	endpointHeaders := decodeEndpointHeaders(headersRaw)
	endpointPool := decodeProxyPool(proxyRaw)

	var models []string
	if modelsRaw != "" {
		models = parseModelIDsFromRaw(modelsRaw)
	}

	if len(models) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"success":     true,
			"totalModels": 0,
			"message":     "该端点没有模型可供检测",
		})
		return
	}

	// Touch endpoint
	_, _ = db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), id)

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	concurrency := healthConcurrencyDefault
	if req.Concurrency > 0 {
		concurrency = req.Concurrency
	}
	concurrency = min(max(concurrency, 1), healthConcurrencyMax)

	summary := s.runBatchHealthCheck(ctx, id, baseURL, apiKey, models, timeoutDuration, concurrency, endpointPool, endpointHeaders)

	// Save check results to db history
	for _, result := range summary.Results {
		var errMsg sql.NullString
		if result.Error != "" {
			errMsg.Valid = true
			errMsg.String = result.Error
		}
		_, _ = db.ExecContext(ctx, `
			INSERT INTO openai_health_history (endpoint_id, status, response_time, error_message, checked_at)
			VALUES (?, ?, ?, ?, ?)`,
			id, result.Status, result.Latency, errMsg, result.CheckedAt)
	}

	// Update endpoint status
	_, _ = db.ExecContext(ctx, `
		UPDATE openai_endpoints
		SET status = ?, last_checked = ?
		WHERE id = ?`,
		summary.OverallStatus, summary.CheckedAt, id)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"summary": summary,
	})
}

// proxyRuntimeStateItem 是单个代理的运行时禁用状态，供前端在代理池管理里红底展示。
type proxyRuntimeStateItem struct {
	Proxy            string `json:"proxy"`
	CooldownUntil    string `json:"cooldownUntil,omitempty"`
	RateLimitedUntil string `json:"rateLimitedUntil,omitempty"`
	SunkUntil        string `json:"sunkUntil,omitempty"`
	Failures         int    `json:"failures"`
	Rate429          int    `json:"rate429"`
	LastTTFB         int64  `json:"lastTTFB,omitempty"`
	LastExitIP       string `json:"lastExitIP,omitempty"`
	LastProbeAt      string `json:"lastProbeAt,omitempty"`
}

// getEndpointProxyStateRoute 返回端点代理池各出口的运行时禁用状态：
//   - cooldownUntil：连接失败冷却到期时间（指数退避，1min~30min）
//   - rateLimitedUntil：上游 429 累计冻结到期时间（30min）
//   - sunkUntil：连续失败被判定的坏代理沉淀到期时间（6h）
//
// 前端据此把被冷却/冻结/沉淀的代理 IP 标红，便于发现「正在被禁用的出口」。
func (s *Service) getEndpointProxyStateRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	pool := decodeProxyPool(proxyRaw)

	now := time.Now()
	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[id]
	items := make([]proxyRuntimeStateItem, 0, len(pool))
	for _, proxy := range pool {
		item := proxyRuntimeStateItem{Proxy: proxy}
		if ok {
			item.Failures = state.failures[proxy]
			item.Rate429 = state.rate429[proxy]
			item.LastTTFB = state.lastTTFB[proxy]
			item.LastExitIP = state.lastExitIP[proxy]
			if probeAt, probed := state.lastProbeAt[proxy]; probed && !probeAt.IsZero() {
				item.LastProbeAt = probeAt.Format(time.RFC3339)
			}
			if until, cooled := state.cooldown[proxy]; cooled && now.Before(until) {
				item.CooldownUntil = until.Format(time.RFC3339)
			}
			if until, banned := state.rateLimited[proxy]; banned && now.Before(until) {
				item.RateLimitedUntil = until.Format(time.RFC3339)
			}
			if until, sunk := state.sunk[proxy]; sunk && now.Before(until) {
				item.SunkUntil = until.Format(time.RFC3339)
			}
		}
		items = append(items, item)
	}
	s.proxyMu.Unlock()

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "proxies": items})
}

// unbanEndpointProxies 一键解封端点代理池全部出口：清除冷却、429 冻结与坏代理沉淀
// 状态，使被临时/长期禁用的代理立即恢复可选。代理池的禁用都是运行时内存状态，
// 不修改配置，故解封无需写库。
func (s *Service) unbanEndpointProxies(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	pool := decodeProxyPool(proxyRaw)

	now := time.Now()
	s.proxyMu.Lock()
	state, ok := s.proxyStateByEndpoint[id]
	cleared := 0
	if ok {
		for _, proxy := range pool {
			clearedFrom := false
			if until, cooled := state.cooldown[proxy]; cooled && now.Before(until) {
				delete(state.cooldown, proxy)
				clearedFrom = true
			}
			if until, banned := state.rateLimited[proxy]; banned && now.Before(until) {
				delete(state.rateLimited, proxy)
				delete(state.rate429, proxy)
				clearedFrom = true
			}
			if until, sunk := state.sunk[proxy]; sunk && now.Before(until) {
				delete(state.sunk, proxy)
				delete(state.failures, proxy)
				clearedFrom = true
			}
			if clearedFrom {
				cleared++
				s.persistProxyState(id, proxy, "cooldown", time.Time{})
				s.persistProxyState(id, proxy, "rate_limited", time.Time{})
				s.persistProxyState(id, proxy, "sunk", time.Time{})
			}
		}
	}
	s.proxyMu.Unlock()

	applog.Info(ctx, "openai", "proxy pool unbanned",
		"endpoint_id", id,
		"cleared", cleared,
		"pool_size", len(pool),
	)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "cleared": cleared})
}

// probeEndpointProxies 立即对端点代理池全体出口做一次手动探活：
//   1. 经代理向端点 /models 发起请求，判定链路连通性（成功清冷却/沉淀，失败按
//      失败计数指数冷却且连续失败达阈值沉淀为坏代理）
//   2. 经代理访问 ipify 记录出口公网 IP
// 并发执行（上限 20），响应返回每个代理的探测结果（成功后记入运行时状态）。
// 用于前端「批量测试」：探活结果随后通过 /proxy-state 读取。
func (s *Service) probeEndpointProxies(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var baseURL, apiKey string
	var proxyRaw sql.NullString
	err = db.QueryRowContext(ctx, "SELECT base_url, api_key, proxy_pool FROM openai_endpoints WHERE id = ?", id).Scan(&baseURL, &apiKey, &proxyRaw)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}
	pool := decodeProxyPool(proxyRaw)
	if len(pool) == 0 {
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "probed": 0, "reachable": 0})
		return
	}

	sem := make(chan struct{}, 20)
	var probe sync.WaitGroup
	var okMu sync.Mutex
	reachable := 0
	for _, proxyURL := range pool {
		proxyURL := proxyURL
		probe.Add(1)
		go func() {
			defer probe.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if s.probeEndpointProxyOnce(ctx, id, baseURL, apiKey, proxyURL) {
				okMu.Lock()
				reachable++
				okMu.Unlock()
			}
		}()
	}
	probe.Wait()

	applog.Info(ctx, "openai", "proxy pool manually probed",
		"endpoint_id", id,
		"pool_size", len(pool),
		"reachable", reachable,
	)
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "probed": len(pool), "reachable": reachable})
}

// probeEndpointProxyOnce 对单个代理做一次手动探活，返回是否链路可达。
// 链路可达：清冷却/沉淀，记录出口 IP；不可达：指数冷却 + 连续失败沉淀。
func (s *Service) probeEndpointProxyOnce(ctx context.Context, endpointID, baseURL, apiKey, proxyURL string) bool {
	client, err := s.proxyClient(proxyURL)
	if err != nil {
		return false
	}
	fullURL := strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
		fullURL += "/v1"
	}
	fullURL += "/models"

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, fullURL, nil)
	if err != nil {
		return false
	}
	if apiKey != "" && apiKey != "public" {
		req.Header.Set("Authorization", "Bearer "+secure.SecureDecrypt(apiKey))
	}
	resp, err := client.Do(req)
	if err != nil {
		s.markProxyFailed(endpointID, proxyURL)
		if s.proxyFailCount(endpointID, proxyURL) >= proxySinkThreshold {
			s.sinkProxy(endpointID, proxyURL)
		}
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
	resp.Body.Close()
	if resp.StatusCode != 0 && resp.StatusCode >= 300 {
		// 上游 4xx/5xx：链路可达但上游拒绝；不沉淀代理（可能是 key/额度问题）。
		s.markProxySuccess(endpointID, proxyURL)
		s.unsinkProxy(endpointID, proxyURL)
		s.probeProxyExitIP(endpointID, proxyURL)
		return true
	}
	s.markProxySuccess(endpointID, proxyURL)
	s.unsinkProxy(endpointID, proxyURL)
	s.probeProxyExitIP(endpointID, proxyURL)
	return true
}

func (s *Service) getEndpointHealthRoute(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	var status, lastChecked sql.NullString
	err = db.QueryRowContext(ctx, "SELECT status, last_checked FROM openai_endpoints WHERE id = ?", id).Scan(&status, &lastChecked)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "端点不存在"})
		return
	}

	// Fetch health history per model
	rows, err := db.QueryContext(ctx, `
		SELECT h.status, h.response_time, h.error_message, h.checked_at
		FROM openai_health_history h
		WHERE h.endpoint_id = ?
		ORDER BY h.checked_at DESC
		LIMIT 100`, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	history := []map[string]interface{}{}
	for rows.Next() {
		var hStatus, checked string
		var respTime sql.NullInt64
		var errMsg sql.NullString
		if err := rows.Scan(&hStatus, &respTime, &errMsg, &checked); err == nil {
			item := map[string]interface{}{
				"status":    hStatus,
				"checkedAt": checked,
			}
			if respTime.Valid {
				item["responseTime"] = respTime.Int64
			}
			if errMsg.Valid {
				item["errorMessage"] = errMsg.String
			}
			history = append(history, item)
		}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"endpointId":      id,
		"healthStatus":    status.String,
		"lastHealthCheck": lastChecked.String,
		"history":         history,
	})
}

func (s *Service) refreshAllEndpointsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, headers, proxy_pool FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type item struct {
		id, name, url, key string
		headers            []HeaderItem
		pool               []string
	}
	items := []item{}
	for rows.Next() {
		var it item
		var headersRaw, proxyRaw sql.NullString
		if err := rows.Scan(&it.id, &it.name, &it.url, &it.key, &headersRaw, &proxyRaw); err == nil {
			it.key = secure.SecureDecrypt(it.key)
			it.headers = decodeEndpointHeaders(headersRaw)
			it.pool = decodeProxyPool(proxyRaw)
			items = append(items, it)
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := []map[string]interface{}{}

	for _, it := range items {
		wg.Add(1)
		go func(it item) {
			defer wg.Done()
			status := "invalid"
			modelsList := []string{}
			var errStr string

			vOk, _, err := s.verifyAPIKeyRaw(ctx, it.url, it.key, it.id, it.pool, it.headers)
			if err == nil && vOk {
				status = "valid"
				mList, mErr := s.listModelsRaw(ctx, it.url, it.key, it.id, it.pool, it.headers)
				if mErr == nil {
					modelsList = mList
				}
			} else if err != nil {
				errStr = err.Error()
			}

			checkedAt := time.Now().Format(time.RFC3339)
			// 验证失败时保留旧模型列表：一次超时/临时故障不应清空已获取的模型。
			modelsJSON := "[]"
			if status == "valid" && len(modelsList) > 0 {
				modelsJSONBytes, _ := json.Marshal(modelsList)
				modelsJSON = string(modelsJSONBytes)
			}

			// Update in DB
			if dbConn, dbErr := s.open(ctx); dbErr == nil {
				defer dbConn.Close()
				_, _ = dbConn.ExecContext(ctx, `
					UPDATE openai_endpoints
					SET status = ?, models = ?, last_checked = ?
					WHERE id = ?`,
					status, modelsJSON, checkedAt, it.id)
			}

			mu.Lock()
			res := map[string]interface{}{
				"id":          it.id,
				"name":        it.name,
				"success":     status == "valid",
				"modelsCount": len(modelsList),
			}
			if errStr != "" {
				res["error"] = errStr
			}
			results = append(results, res)
			mu.Unlock()
		}(it)
	}

	wg.Wait()
	s.invalidateRouteCache()
	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "results": results})
}

func (s *Service) healthCheckAllRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Timeout     int `json:"timeout"`
		Concurrency int `json:"concurrency"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, models, headers, proxy_pool FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type item struct {
		id, name, url, key, modelsRaw string
		headers                       []HeaderItem
		pool                          []string
	}
	items := []item{}
	for rows.Next() {
		var it item
		var headersRaw, proxyRaw sql.NullString
		if err := rows.Scan(&it.id, &it.name, &it.url, &it.key, &it.modelsRaw, &headersRaw, &proxyRaw); err == nil {
			it.key = secure.SecureDecrypt(it.key)
			it.headers = decodeEndpointHeaders(headersRaw)
			it.pool = decodeProxyPool(proxyRaw)
			items = append(items, it)
		}
	}

	timeoutDuration := healthTimeoutDefault
	if req.Timeout > 0 {
		timeoutDuration = time.Duration(req.Timeout) * time.Millisecond
	}

	concurrency := healthConcurrencyDefault
	if req.Concurrency > 0 {
		concurrency = req.Concurrency
	}
	concurrency = min(max(concurrency, 1), healthConcurrencyMax)

	type endpointTarget struct {
		item
		models []string
	}
	targets := make([]endpointTarget, 0, len(items))
	for _, it := range items {
		models := parseModelIDsFromRaw(it.modelsRaw)
		targets = append(targets, endpointTarget{item: it, models: models})
	}

	resultsByEndpoint := make(map[string][]HealthRecord, len(targets))
	var resultsMu sync.Mutex
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, target := range targets {
		for _, model := range target.models {
			wg.Add(1)
			go func(target endpointTarget, model string) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				defer func() { <-sem }()

				result := s.healthCheckSingleModel(ctx, target.id, target.url, target.key, model, timeoutDuration, target.pool, target.headers)
				resultsMu.Lock()
				resultsByEndpoint[target.id] = append(resultsByEndpoint[target.id], result)
				resultsMu.Unlock()
			}(target, model)
		}
	}
	wg.Wait()

	results := make([]map[string]interface{}, 0, len(targets))
	for _, target := range targets {
		if len(target.models) == 0 {
			results = append(results, map[string]interface{}{
				"endpointId":  target.id,
				"name":        target.name,
				"totalModels": 0,
				"skipped":     true,
			})
			continue
		}

		summary := summarizeHealthResults(target.models, resultsByEndpoint[target.id])
		for _, result := range summary.Results {
			var errMsg sql.NullString
			if result.Error != "" {
				errMsg.Valid = true
				errMsg.String = result.Error
			}
			_, _ = db.ExecContext(ctx, `
				INSERT INTO openai_health_history (endpoint_id, status, response_time, error_message, checked_at)
				VALUES (?, ?, ?, ?, ?)`,
				target.id, result.Status, result.Latency, errMsg, result.CheckedAt)
		}
		_, _ = db.ExecContext(ctx, `
			UPDATE openai_endpoints
			SET status = ?, last_checked = ?
			WHERE id = ?`,
			summary.OverallStatus, summary.CheckedAt, target.id)

		results = append(results, map[string]interface{}{
			"endpointId":  target.id,
			"name":        target.name,
			"totalModels": summary.TotalModels,
			"operational": summary.Operational,
			"degraded":    summary.Degraded,
			"failed":      summary.Failed,
			"results":     summary.Results,
		})
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"checkedAt": time.Now().Format(time.RFC3339),
		"endpoints": results,
	})
}

func (s *Service) exportEndpointsRoute(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, created_at, last_used, last_checked, model_mappings, priority, weight FROM openai_endpoints ORDER BY sort_order ASC, created_at ASC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	endpoints := []Endpoint{}
	for rows.Next() {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, proxyBatchesRaw, mappingsRaw, apiKeysRaw, protocolRaw sql.NullString
		var created, used, checked sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, priority, weight int

		err := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &proxyBatchesRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &created, &used, &checked, &mappingsRaw, &priority, &weight)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		ep.APIKey = secure.SecureDecrypt(ep.APIKey)
		ep.Enabled = enabledInt == 1
		ep.AutoSwitch = autoSwitchInt == 1
		ep.ProxyEnabled = proxyEnabledInt == 1
		ep.ForceProxy = forceProxyInt == 1
		ep.Protocol = normalizeProtocol(protocolRaw.String)
		ep.Priority = priority
		ep.Weight = weight
		if mappingsRaw.Valid && mappingsRaw.String != "" {
			_ = json.Unmarshal([]byte(mappingsRaw.String), &ep.ModelMappings)
		}
		if apiKeysRaw.Valid && apiKeysRaw.String != "" {
			_ = json.Unmarshal([]byte(secure.SecureDecrypt(apiKeysRaw.String)), &ep.APIKeys)
		}
		ep.CreatedAt = created.String
		if used.Valid {
			v := used.String
			ep.LastUsed = &v
		}
		if checked.Valid {
			v := checked.String
			ep.LastChecked = &v
		}

		ep.Models = []string{}
		if modelsRaw.Valid && modelsRaw.String != "" {
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
		ep.ProxyBatches = []ProxyBatch{}
		if proxyBatchesRaw.Valid && proxyBatchesRaw.String != "" {
			_ = json.Unmarshal([]byte(proxyBatchesRaw.String), &ep.ProxyBatches)
		}
		endpoints = append(endpoints, ep)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"endpoints":  endpoints,
		"exportedAt": time.Now().Format(time.RFC3339),
	})
}

func (s *Service) importEndpointsRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Endpoints []Endpoint `json:"endpoints"`
		Overwrite bool       `json:"overwrite"`
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

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	if req.Overwrite {
		_, err = tx.ExecContext(ctx, "DELETE FROM openai_endpoints")
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	importedCount := 0
	skippedCount := 0

	for _, ep := range req.Endpoints {
		if ep.Name == "" || ep.BaseURL == "" || ep.APIKey == "" {
			skippedCount++
			continue
		}

		if !req.Overwrite {
			var exists int
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM openai_endpoints WHERE base_url = ?", ep.BaseURL).Scan(&exists)
			if exists > 0 {
				skippedCount++
				continue
			}
		}

		id := ep.ID
		if id == "" {
			id = fmt.Sprintf("oai_%d_%s", time.Now().UnixNano(), s.randString(9))
		}

		enabledInt := 0
		if ep.Enabled {
			enabledInt = 1
		}
		status := ep.Status
		if status == "" {
			status = "unknown"
		}
		createdAt := ep.CreatedAt
		if createdAt == "" {
			createdAt = time.Now().Format(time.RFC3339)
		}
		var modelsJSON []byte
		if len(ep.Models) > 0 {
			modelsJSON, _ = json.Marshal(ep.Models)
		} else {
			modelsJSON = []byte("[]")
		}

		var encryptedKey, err_enc = secure.SecureEncrypt(ep.APIKey)
		if err_enc != nil {
			skippedCount++
			continue
		}
		headersJSON, _ := json.Marshal([]HeaderItem{})
		if len(ep.Headers) > 0 {
			headersJSON, _ = json.Marshal(cleanHeaders(ep.Headers))
		}
		disabledJSON, _ := json.Marshal([]string{})
		if len(ep.DisabledModels) > 0 {
			disabledJSON, _ = json.Marshal(ep.DisabledModels)
		}
		proxyJSON, _ := json.Marshal([]string{})
		if len(ep.ProxyPool) > 0 {
			proxyJSON, _ = json.Marshal(cleanProxyPool(ep.ProxyPool))
		}
		batchesJSON, _ := json.Marshal([]ProxyBatch{})
		if len(ep.ProxyBatches) > 0 {
			batchesJSON, _ = json.Marshal(ep.ProxyBatches)
		}
		mappingsJSON, _ := json.Marshal(map[string]string{})
		if len(ep.ModelMappings) > 0 {
			mappingsJSON, _ = json.Marshal(ep.ModelMappings)
		}
		apiKeysJSON, _ := json.Marshal([]string{})
		if len(ep.APIKeys) > 0 {
			encryptedKeys := make([]string, 0, len(ep.APIKeys))
			for _, k := range ep.APIKeys {
				if enc, encErr := secure.SecureEncrypt(strings.TrimSpace(k)); encErr == nil {
					encryptedKeys = append(encryptedKeys, enc)
				}
			}
			apiKeysJSON, _ = json.Marshal(encryptedKeys)
		}
		autoSwitchInt := boolToInt(ep.AutoSwitch)
		proxyEnabledInt := boolToInt(ep.ProxyEnabled)
		forceProxyInt := boolToInt(ep.ForceProxy)
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO openai_endpoints (id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, proxy_batches, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, model_mappings, created_at, last_used, last_checked, priority, weight)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, ep.Name, ep.BaseURL, encryptedKey, string(apiKeysJSON), string(headersJSON), string(disabledJSON), string(proxyJSON), string(batchesJSON), autoSwitchInt, proxyEnabledInt, forceProxyInt, normalizeProtocol(ep.Protocol), status, enabledInt, string(modelsJSON), string(mappingsJSON), createdAt, ep.LastUsed, ep.LastChecked, ep.Priority, ep.Weight)
		if err != nil {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		importedCount++
	}

	if err := tx.Commit(); err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.invalidateRouteCache()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"imported": importedCount,
		"skipped":  skippedCount,
		"total":    importedCount + skippedCount,
	})
}

// resolveTargetEndpoint 仅在管理面板入口（/api/openai 前缀，会话鉴权）读取
// x-endpoint-id 强制指定端点，用于调试/聊天测试；外部统一出口（/v1）忽略该头，
// 保证模型池路由不外泄、外部无法锁定特定上游。
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
		SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, model_mappings, sort_order, priority, weight
		FROM openai_endpoints WHERE enabled = 1
		ORDER BY priority DESC, sort_order ASC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ep Endpoint
		var headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw sql.NullString
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt, sortOrder, priority, weight int
		if errScan := rows.Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &mappingsRaw, &sortOrder, &priority, &weight); errScan == nil {
			ep.APIKey = secure.SecureDecrypt(ep.APIKey)
			ep.Enabled = enabledInt == 1
			ep.AutoSwitch = autoSwitchInt == 1
			ep.ProxyEnabled = proxyEnabledInt == 1
			ep.ForceProxy = forceProxyInt == 1
			ep.Protocol = normalizeProtocol(protocolRaw.String)
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

	loadEndpoint := func(ep *Endpoint, headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw sql.NullString, enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt int) {
		ep.APIKey = secure.SecureDecrypt(ep.APIKey)
		ep.Enabled = enabledInt == 1
		ep.AutoSwitch = autoSwitchInt == 1
		ep.ProxyEnabled = proxyEnabledInt == 1
		ep.ForceProxy = forceProxyInt == 1
		ep.Protocol = normalizeProtocol(protocolRaw.String)
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
		var enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt int
		err := db.QueryRowContext(ctx, `
			SELECT id, name, base_url, api_key, api_keys, headers, disabled_models, proxy_pool, auto_switch, proxy_enabled, force_proxy, protocol, status, enabled, models, model_mappings
			FROM openai_endpoints WHERE id = ? AND enabled = 1`, targetEndpointID).
			Scan(&ep.ID, &ep.Name, &ep.BaseURL, &ep.APIKey, &apiKeysRaw, &headersRaw, &disabledRaw, &proxyRaw, &autoSwitchInt, &proxyEnabledInt, &forceProxyInt, &protocolRaw, &ep.Status, &enabledInt, &modelsRaw, &mappingsRaw)
		if err == nil {
			loadEndpoint(&ep, headersRaw, modelsRaw, disabledRaw, proxyRaw, mappingsRaw, protocolRaw, apiKeysRaw, enabledInt, autoSwitchInt, proxyEnabledInt, forceProxyInt)
			if s.endpointHasModel(ep, model) && !isModelDisabled(ep.DisabledModels, model) {
				selectedModel, _ = s.resolveEndpointModel(ep, model)
				return []Endpoint{ep}, ep, 0, selectedModel, true
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
			real, _ := s.resolveEndpointModel(ep, model)
			if !isModelDisabled(ep.DisabledModels, real) {
				candidates = append(candidates, ep)
			}
		}
	}
	if len(candidates) == 0 {
		// 兜底：索引未命中（模型列表尚未刷新/模型名未收录）时按原逻辑遍历全部端点。
		for _, ep := range endpoints {
			real, _ := s.resolveEndpointModel(ep, model)
			if s.endpointHasModel(ep, model) && !isModelDisabled(ep.DisabledModels, real) {
				candidates = append(candidates, ep)
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
	chosen = candidates[chosenIndex]
	selectedModel, _ = s.resolveEndpointModel(chosen, model)
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
	lastErr           error
	endpointExhausted bool // 本轮全部 API Key 尝试失败，应切换到下一个候选端点
	retryableUpstream bool // 上游返回 429/5xx 且代理重试耗尽，应切换到下一个候选端点
	firstChunk        []byte
	firstWritten      bool
	ttfbMs            int64
	lastProxy         string
	lastKeyIndex      int
	attempt           int
	egressIP          string // 请求实际从哪个出口/代理发出（随循环内选中的代理更新）
	startTime         time.Time
	cancel            context.CancelFunc
}

// relayLoop 执行带代理择优与重试的上游转发循环（三个转发入口共用）。语义与
// 说明见调用方注释：上游限流/5xx 不是代理的错，只切换出口重试不冷却代理；
// 但 429 会累计计数，达到阈值后临时禁用该代理（IP 级限流下继续选择只会反复 429）。
func (s *Service) relayLoop(p relayLoopParams) *relayLoopResult {
	res := &relayLoopResult{
		statusCode: http.StatusBadGateway,
		egressIP:   s.egressOutbound(),
		startTime:  time.Now(),
	}
	ctx := p.ctx
	selected := p.selected
	stream := p.stream

	// 代理池选择 + 限流自动切换：最多尝试 len(pool) 个代理。
	// 代理开关未开启或池为空时只尝试一次（重试只是对同一链路的重复请求，
	// 首字超时重发反而放大慢响应，见 effectiveProxyAttempts）。
	maxProxyAttempts := effectiveProxyAttempts(selected)
	// 多 key 时保证每个 key 至少有一次尝试机会（覆盖 401 冻结后自动切 key 的场景）。
	if keyCount := len(cleanKeyList(selected.AllKeys())); keyCount > 1 && maxProxyAttempts < keyCount {
		maxProxyAttempts = keyCount
	}

	var resp *http.Response
	var lastErr error
	var attempt int
	// lastProxy 保存最终成功使用的代理（用于 TTFB 择优记录与日志）。
	lastProxy := ""
	// lastKeyIndex 保存最终成功后使用的 API Key 序号（用于日志 key pill）。
	lastKeyIndex := -1
	// firstChunk 保存流式首字等待阶段读到的首个数据块；无切换机会时首字在循环内读取。
	var firstChunk []byte
	var ttfbMs int64
	firstWritten := false
	// triedKeys 记录本轮请求内已尝试失败的 key（key 永不冻结，仅请求内去重），
	// 避免单 key 场景 401 后对同一 key 无限重试。
	triedKeys := map[string]bool{}
	// finalUpstreamRecorded 标记最后一次尝试的限流/5xx 是否已写入转发失败明细，
	// 避免流式无切换路径与循环结束统一判定处重复记录同一次失败。
	finalUpstreamRecorded := false

	for attempt = 0; attempt < maxProxyAttempts; attempt++ {
		// 客户端已断开（ctx 取消/超时）：立即结束尝试循环，不再发起新的
		// 网络连接。无显式检查时，连接失败路径虽也会因 attemptCtx 取消而快速
		// 返回，但在 clientForEndpoint 选择阶段仍可能空转；这里在每轮最前面
		// 提前终止，杜绝客户端断开后的无效重试（对应网关 502 的常见成因）。
		if err := ctx.Err(); err != nil {
			if lastErr == nil {
				lastErr = err
			}
			res.attempt = attempt
			break
		}
		attemptCtx, cancel := context.WithCancel(ctx)
		res.cancel = cancel
		client, currentProxy, clientErr := s.clientForEndpoint(selected.ID, selected.ProxyPool, selected.ProxyEnabled, selected.ForceProxy, p.sessionKey, selected.Protocol)
		if clientErr != nil {
			cancel()
			lastErr = clientErr
			s.recordRelayError(RelayErrorRecord{
				Route: p.route, Kind: "config",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
				ClientIP: p.clientIP, Attempts: attempt + 1,
				ElapsedMs: time.Since(res.startTime).Milliseconds(),
				Error:     clientErr.Error(),
			})
			break
		}
		if currentProxy != "" {
			lastProxy = currentProxy
			res.egressIP = proxyEndpointAddr(currentProxy)
		}

		httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, p.fullURL, bytes.NewReader(p.body))
		if err != nil {
			cancel()
			res.statusCode = http.StatusInternalServerError
			res.lastErr = err
			res.attempt = attempt
			s.recordRelayError(RelayErrorRecord{
				Route: p.route, Kind: "gateway",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
				ClientIP: p.clientIP, Attempts: attempt + 1,
				ElapsedMs: time.Since(p.requestStarted).Milliseconds(),
				Error:     "build upstream request failed: " + err.Error(),
			})
			errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
			s.recordAnalyticsKey(ctx, p.route, selected.ID, p.model, http.StatusInternalServerError, time.Since(p.requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), boolToInt(res.lastProxy != ""), p.clientIP, res.egressIP, res.lastKeyIndex, "", &AnalyticsError{
				Kind:     "gateway",
				Message:  "build upstream request failed: " + err.Error(),
				Response: errorResponseForLog(errBody, http.StatusInternalServerError),
			})
			return res
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if stream {
			httpReq.Header.Set("Accept", "text/event-stream")
		} else {
			httpReq.Header.Set("Accept", "application/json")
		}
		applyCustomHeaders(httpReq, selected.Headers)

		// 多 API Key 选择：轮询选一个 key（key 永不冻结，本轮已尝试失败的 key 会被跳过）。
		keys := selected.AllKeys()
		currentKey, currentKeyIndex := s.pickKey(selected.ID, keys, triedKeys)
		if currentKey == "" {
			// 本轮全部 key 均已尝试失败：本端点不可用，标记该端点后尝试下一个候选端点。
			cancel()
			s.markProxyFailed(selected.ID, currentProxy)
			res.endpointExhausted = true
			lastErr = fmt.Errorf("端点 %s 本轮全部 API Key 均尝试失败", selected.Name)
			lastProxy = currentProxy
			break
		}
		httpReq.Header.Set("Authorization", "Bearer "+currentKey)

		resp, lastErr = client.Do(httpReq)
		if lastErr != nil {
			// 连接失败（例如该代理不可用）：key 不冻结，只标记代理失败，若有池则切下一个。
			s.markProxyFailed(selected.ID, currentProxy)
			s.recordRelayError(RelayErrorRecord{
				Route: p.route, Kind: "dial",
				Endpoint: selected.Name, EndpointID: selected.ID, KeyIndex: currentKeyIndex,
				Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
				ClientIP: p.clientIP, Attempts: attempt + 1,
				ElapsedMs: time.Since(res.startTime).Milliseconds(),
				Error:     lastErr.Error(),
			})
			cancel()
			if currentProxy != "" && attempt+1 < maxProxyAttempts {
				continue
			}
			// 直连（代理池全部冻结回退）没有可切换的出口，重试只是重复打同一条链路。
			break
		}

		// 401/403 鉴权失败：key 本身失效，但不冻结；本轮请求内已尝试的 key 记入
		// triedKeys，避免无限重试，继续尝试下一个 key（或由 pickKey 耗尽后切换端点）。
		// 不消耗代理切换次数，因为 key 问题是凭据级非代理级。
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			triedKeys[currentKey] = true
			resp.Body.Close()
			cancel()
			continue
		}

		// 非流式：读取正文判断限流或 5xx，失败时在循环内重试。
		// 上游限流/5xx 不是代理的错：只切换出口重试，不惩罚代理（不冷却），
		// 避免上游故障污染整个代理池。但限流（429）会累计计数，
		// 达到阈值后临时禁用该代理（IP 级限流下继续选择只会反复 429）。
		if !stream && selected.AutoSwitch && attempt+1 < maxProxyAttempts {
			bodyBytesRead, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			if readErr != nil || isRetryableUpstreamResponse(resp, bodyBytesRead) {
				if isRateLimitResponse(resp, bodyBytesRead) {
					s.markProxy429(selected.ID, currentProxy, retryAfterFromHeader(resp))
				}
				s.clearSessionBinding(selected.ID, p.sessionKey)
				s.recordRelayError(RelayErrorRecord{
					Route: p.route, Kind: "upstream",
					Endpoint: selected.Name, EndpointID: selected.ID, KeyIndex: currentKeyIndex,
					Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
					ClientIP: p.clientIP, Attempts: attempt + 1,
					ElapsedMs:  time.Since(res.startTime).Milliseconds(),
					StatusCode: resp.StatusCode,
					Upstream:   truncateForLog(string(bodyBytesRead), relayErrorBodyLimit),
					Error:      "retryable upstream response",
				})
				if currentProxy != "" {
					// 直连（池全部冻结回退）无处可切：保留已读正文原样返回给客户端。
					continue
				}
				res.retryableUpstream = true
				lastErr = fmt.Errorf("上游返回 %d（限流/服务端错误，重试耗尽）", resp.StatusCode)
				resp.Body = io.NopCloser(bytes.NewReader(bodyBytesRead))
				break
			}
			// 不是限流：重建带正文的响应继续处理。
			resp.Body = io.NopCloser(bytes.NewReader(bodyBytesRead))
			lastKeyIndex = currentKeyIndex
			break
		}

		// 流式：仅按状态码判断，限流或 5xx 时切换代理重试一次（同样不惩罚代理，
		// 但限流会累计计数，达到阈值后禁用该代理）。
		if stream && selected.AutoSwitch && attempt+1 < maxProxyAttempts && isRetryableUpstreamResponse(resp, nil) {
			if isRateLimitResponse(resp, nil) {
				s.markProxy429(selected.ID, currentProxy, retryAfterFromHeader(resp))
			}
			resp.Body.Close()
			cancel()
			s.clearSessionBinding(selected.ID, p.sessionKey)
			s.recordRelayError(RelayErrorRecord{
				Route: p.route, Kind: "upstream",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
				ClientIP: p.clientIP, Attempts: attempt + 1,
				ElapsedMs:  time.Since(res.startTime).Milliseconds(),
				StatusCode: resp.StatusCode,
				Error:      "retryable upstream response",
			})
			if currentProxy != "" {
				continue
			}
			// 直连（代理池全部冻结回退）无处可切：标记上游可重试错误，交给端点级 failover。
			res.retryableUpstream = true
			lastErr = fmt.Errorf("上游返回 %d（限流/服务端错误，重试耗尽）", resp.StatusCode)
			break
		}

		if stream {
			// 首字等待：若还有可切换的代理，则带超时等待首个字节，
			// 超时或上游提前断流时标记该代理失败并切换下一个。
			// 直连（池全部冻结回退）没有可切换出口，直接阻塞读首块。
			waitForFirst := selected.AutoSwitch && attempt+1 < maxProxyAttempts && currentProxy != ""
			if waitForFirst {
				type readRes struct {
					n   int
					err error
				}
				ch := make(chan readRes, 1)
				tmp := make([]byte, 4096)
				go func() {
					n, err := resp.Body.Read(tmp)
					ch <- readRes{n, err}
				}()
				var r readRes
				select {
				case r = <-ch:
				case <-time.After(firstTokenTimeout):
					cancel()
					resp.Body.Close()
					s.markProxyFailed(selected.ID, currentProxy)
					s.recordRelayError(RelayErrorRecord{
						Route: p.route, Kind: "timeout",
						Endpoint: selected.Name, EndpointID: selected.ID,
						Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
						ClientIP: p.clientIP, Attempts: attempt + 1,
						ElapsedMs: time.Since(res.startTime).Milliseconds(),
						Error:     fmt.Sprintf("no first byte within %s", firstTokenTimeout),
					})
					if currentProxy != "" && attempt+1 < maxProxyAttempts {
						continue
					}
					lastErr = fmt.Errorf("上游首字超时（超过 %s）", firstTokenTimeout)
					// 首字超时属上游问题：代理重试已耗尽，交给端点级 failover 尝试下一个候选端点。
					res.retryableUpstream = true
					break
				}
				if r.n > 0 {
					firstChunk = append([]byte(nil), tmp[:r.n]...)
					firstWritten = true
					ttfbMs = time.Since(res.startTime).Milliseconds()
					lastKeyIndex = currentKeyIndex
					break
				}
				cancel()
				resp.Body.Close()
				s.markProxyFailed(selected.ID, currentProxy)
				if currentProxy != "" && attempt+1 < maxProxyAttempts {
					continue
				}
				if lastErr == nil {
					lastErr = r.err
					if lastErr == nil {
						lastErr = io.EOF
					}
					s.recordRelayError(RelayErrorRecord{
						Route: p.route, Kind: "stream_closed",
						Endpoint: selected.Name, EndpointID: selected.ID,
						Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
						ClientIP: p.clientIP, Attempts: attempt + 1,
						ElapsedMs: time.Since(res.startTime).Milliseconds(),
						Error:     "upstream closed stream before first byte: " + lastErr.Error(),
					})
				}
				break
			}

			// 无切换机会：直接阻塞读首块，读取结果留给下方流式循环继续消费。
			// 但若上游返回的是限流/5xx 错误（非真正 SSE 数据），不应标记
			// firstWritten（否则末尾统一判定跳过，retryableUpstream 不被设置，
			// 导致 failover 循环直接把 429 透传）。
			tmp := make([]byte, 4096)
			n, err := resp.Body.Read(tmp)
			if n > 0 {
				// 429/5xx 错误体不是 SSE 首块，不设 firstWritten。
				if isRetryableUpstreamResponse(resp, nil) {
					res.retryableUpstream = true
					if lastErr == nil {
						lastErr = fmt.Errorf("上游返回 %d（限流/服务端错误，无切换机会）", resp.StatusCode)
					}
					s.recordRelayError(RelayErrorRecord{
						Route: p.route, Kind: "upstream",
						Endpoint: selected.Name, EndpointID: selected.ID, KeyIndex: currentKeyIndex,
						Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
						ClientIP: p.clientIP, Attempts: attempt + 1,
						ElapsedMs:  time.Since(res.startTime).Milliseconds(),
						StatusCode: resp.StatusCode,
						Error:      "retryable upstream response",
					})
					finalUpstreamRecorded = true
					break
				}
				firstChunk = append([]byte(nil), tmp[:n]...)
				firstWritten = true
				ttfbMs = time.Since(res.startTime).Milliseconds()
				lastKeyIndex = currentKeyIndex
				break
			}
			cancel()
			lastErr = err
			if lastErr == nil {
				lastErr = io.EOF
			}
			s.recordRelayError(RelayErrorRecord{
				Route: p.route, Kind: "stream_closed",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: p.model, Stream: stream, Proxy: hostFromProxyURL(currentProxy),
				ClientIP: p.clientIP, Attempts: attempt + 1,
				ElapsedMs: time.Since(res.startTime).Milliseconds(),
				Error:     "upstream closed stream before first byte: " + lastErr.Error(),
			})
			break
		}
		break
	}

	res.resp = resp
	res.lastProxy = lastProxy
	res.lastKeyIndex = lastKeyIndex
	res.firstChunk = firstChunk
	res.firstWritten = firstWritten
	res.ttfbMs = ttfbMs
	res.attempt = attempt

	if lastErr != nil && resp == nil {
		res.lastErr = lastErr
		s.recordRelayError(RelayErrorRecord{
			Route: p.route, Kind: "bad_gateway",
			Endpoint: selected.Name, EndpointID: selected.ID,
			Model: p.model, Stream: stream, Proxy: hostFromProxyURL(lastProxy),
			ClientIP: p.clientIP, Attempts: attempt + 1,
			ElapsedMs: time.Since(res.startTime).Milliseconds(),
			Error:     lastErr.Error(),
		})
		errBody, _ := json.Marshal(map[string]string{"error": lastErr.Error()})
		s.recordAnalyticsKey(ctx, p.route, selected.ID, p.model, http.StatusBadGateway, time.Since(res.startTime).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), boolToInt(res.lastProxy != ""), p.clientIP, res.egressIP, res.lastKeyIndex, "", &AnalyticsError{
			Kind:     "bad_gateway",
			Message:  lastErr.Error(),
			Response: errorResponseForLog(errBody, http.StatusBadGateway),
		})
		return res
	}
	// 防御：正常退出循环但未拿到响应（理论上只会在极端路径发生），兜底为 502，
	// 避免调用方对 nil resp / nil lastErr 做解引用。
	if resp == nil {
		res.lastErr = lastErr
		if res.lastErr == nil {
			res.lastErr = fmt.Errorf("上游转发未返回响应（重试耗尽）")
		}
		res.statusCode = http.StatusBadGateway
		res.endpointExhausted = true
		s.recordRelayError(RelayErrorRecord{
			Route: p.route, Kind: "bad_gateway",
			Endpoint: selected.Name, EndpointID: selected.ID,
			Model: p.model, Stream: stream, Proxy: hostFromProxyURL(lastProxy),
			ClientIP: p.clientIP, Attempts: attempt + 1,
			ElapsedMs: time.Since(res.startTime).Milliseconds(),
			Error:     res.lastErr.Error(),
		})
		return res
	}
	// 最后一次尝试（无重试机会）返回限流：同样累计计数，供 429 熔断使用。
	if resp != nil && isRateLimitResponse(resp, nil) {
		s.markProxy429(selected.ID, lastProxy, retryAfterFromHeader(resp))
	}
	// 统一判定「上游可重试错误」：无论是否启用 AutoSwitch / 是否有代理池，
	// 只要最终响应是限流或 5xx（且流式尚未写出首字节），都交给端点级 failover
	// 尝试下一个候选端点，尽最大可能提供可用渠道。成功（2xx）或客户端 4xx 不触发。
	// 若最后一次尝试的失败事件尚未写入明细（非流式循环内未逐次记录的最后一跳、
	// 或直连无切换机会），在此补齐一条，保证「最终导致失败的那一跳」也能排障追溯。
	if resp != nil && !firstWritten && isRetryableUpstreamResponse(resp, nil) {
		res.retryableUpstream = true
		if lastErr == nil {
			lastErr = fmt.Errorf("上游返回 %d（限流/服务端错误）", resp.StatusCode)
		}
		if !finalUpstreamRecorded {
			s.recordRelayError(RelayErrorRecord{
				Route: p.route, Kind: "upstream",
				Endpoint: selected.Name, EndpointID: selected.ID, KeyIndex: lastKeyIndex,
				Model: p.model, Stream: stream, Proxy: hostFromProxyURL(lastProxy),
				ClientIP: p.clientIP, Attempts: attempt + 1,
				ElapsedMs:  time.Since(res.startTime).Milliseconds(),
				StatusCode: resp.StatusCode,
				Error:      "retryable upstream response",
			})
		}
	}
	res.lastErr = lastErr
	// 单 key 健康统计：成功（2xx）清连续失败；发送了 key 但上游返回限流/错误不冻结，
	// 仅记录失败（供前端展示 key 健康状态）。401/403 记失败便于观察 key 是否失效。
	keys := cleanKeyList(selected.AllKeys())
	if resp != nil && lastKeyIndex >= 0 && lastKeyIndex < len(keys) {
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			s.markKeySuccess(selected.ID, keys[lastKeyIndex])
		} else if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			s.markKeyFailure(selected.ID, keys[lastKeyIndex], resp.StatusCode, "401/403 auth failed")
		}
	}
	_, _ = p.db.ExecContext(ctx, "UPDATE openai_endpoints SET last_used = ? WHERE id = ?", time.Now().Format(time.RFC3339), selected.ID)
	res.statusCode = resp.StatusCode
	return res
}

// relayCancelOnCloseBody 在正文关闭时连带释放 attempt context，供正文由调用方
// 消费的入口（/v1/messages）使用：避免在正文未读完时提前 cancel 掐断响应。
type relayCancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *relayCancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	if b.cancel != nil {
		b.cancel()
	}
	return err
}

func (s *Service) proxyChatCompletions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestStarted := time.Now()
	clientIP := s.resolveClientIP(r)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		// 请求体读取失败（如客户端上传超时中断）：这是网关侧问题，用 502 表达，
		// 避免与「请求体格式错误」的 400 语义混淆。
		s.recordRelayError(RelayErrorRecord{
			Route: "chat.completions", Kind: "gateway",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body read failed: " + err.Error(),
		})
		// 网关拦截（未到达上游）不写入调用日志。
		response.JSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	var parsedBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsedBody); err != nil {
		s.recordRelayError(RelayErrorRecord{
			Route: "chat.completions", Kind: "bad_request",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body is not valid JSON: " + err.Error(),
		})
		// 网关拦截（未到达上游）也写入调用日志（含报错信息），便于日志与 AI 排障。
		errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
		s.recordAnalyticsKey(ctx, "chat.completions", "", "", http.StatusBadRequest, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, 0, 0, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "bad_request",
			Message:  "request body is not valid JSON: " + err.Error(),
			Response: errorResponseForLog(errBody, http.StatusBadRequest),
		})
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	model, _ := parsedBody["model"].(string)
	stream, _ := parsedBody["stream"].(bool)
	targetEndpointID := s.resolveTargetEndpoint(r)
	sessionKey := resolveSessionKey(r, parsedBody)

	db, err := s.open(ctx)
	if err != nil {
		// 网关侧数据库故障，未进入转发；不写入调用日志。
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	endpointCandidates, selected, _, _, found := s.selectEndpointCandidates(ctx, db, model, targetEndpointID, sessionKey)
	if !found {
		s.recordRelayError(RelayErrorRecord{
			Route: "chat.completions", Kind: "no_endpoint",
			Model: model, Stream: stream, ClientIP: clientIP,
			ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error:     fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", model, targetEndpointID),
		})
		// 候选池为空属网关自身状态，仍写入调用日志（含报错信息），便于日志与 AI 排障。
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
				"type":    "service_unavailable",
			},
		})
		s.recordAnalyticsKey(ctx, "chat.completions", "", model, http.StatusServiceUnavailable, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), 0, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "no_endpoint",
			Message:  fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", model, targetEndpointID),
			Response: errorResponseForLog(errBody, http.StatusServiceUnavailable),
		})
		response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
				"type":    "service_unavailable",
			},
		})
		return
	}

	// 记录是否经由本端点配置的代理池出网。先于网关密钥限制等分支计算，
	// 使这些早退路径的调用日志也能正确标注代理。
	viaProxy := 0
	if len(selected.ProxyPool) > 0 {
		viaProxy = 1
	}

	// 网关密钥限制：模型白名单 / 端点白名单 / token 配额。
	if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
		if limitErr := s.enforceGatewayKeyLimits(ctx, keyIdentity, model, selected.ID); limitErr != "" {
			s.recordRelayError(RelayErrorRecord{
				Route: "chat.completions", Kind: "blocked",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: model, Stream: stream, ClientIP: clientIP,
				ElapsedMs: time.Since(requestStarted).Milliseconds(),
				Error:     limitErr,
			})
			errBody, _ := json.Marshal(map[string]interface{}{
				"error": map[string]string{"message": limitErr, "type": "forbidden"},
			})
			s.recordAnalyticsKey(ctx, "chat.completions", selected.ID, model, http.StatusForbidden, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "", -1, "", &AnalyticsError{
				Kind:     "blocked",
				Message:  limitErr,
				Response: errorResponseForLog(errBody, http.StatusForbidden),
			})
			response.JSON(w, http.StatusForbidden, map[string]interface{}{
				"error": map[string]string{
					"message": limitErr,
					"type":    "forbidden",
				},
			})
			return
		}
	}

	// 本地端点判断：只在前端填入的 base_url 上判定（首个候选），
	// 决定是否启用 /uploads/ 相对路径的本地图片内联。
	primaryURL := strings.TrimSuffix(selected.BaseURL, "/")
	if !strings.HasSuffix(strings.ToLower(primaryURL), "/v1") && !strings.Contains(strings.ToLower(primaryURL), "/v1/") {
		primaryURL += "/v1"
	}
	primaryURL += "/chat/completions"
	isLocal := localURLRegex.MatchString(primaryURL)

	if !isLocal {
		if messages, ok := parsedBody["messages"].([]interface{}); ok {
			for _, msg := range messages {
				if msgMap, ok := msg.(map[string]interface{}); ok {
					if contentArr, ok := msgMap["content"].([]interface{}); ok {
						for _, part := range contentArr {
							if partMap, ok := part.(map[string]interface{}); ok {
								if partMap["type"] == "image_url" {
									if imgURLMap, ok := partMap["image_url"].(map[string]interface{}); ok {
										s.inlineLocalUploadImage(imgURLMap, s.cfg.DataDir)
									}
								}
							}
						}
					}
				}
			}
		}
	}

	// 归一化 Anthropic/Claude 风格的消息 content 数组为 OpenAI 标准格式。
	// PI / 部分 agent 客户端以 openai-completions 协议发请求时，assistant 历史
	// 消息的 content 可能是 content blocks 数组（[{type:"thinking",...},
	// {type:"text",...},{type:"toolCall",...}] 或 Claude 的 tool_use/tool_result），
	// 而 zen 等上游的 chat.completions 只接受字符串或 OpenAI 标准 parts。
	// 这里把 thinking block 提取为顶层 reasoning_content、text 合并为字符串、
	// toolCall/tool_use 转为标准 tool_calls，否则上游直接 400 break（见本地网关
	// 透传后 opencode.ai/zen 的 "Input should be a valid string" 错误）。
	normalizeChatContentBlocks(parsedBody)

	// 多端点 failover：按侧栏 sort_order 顺序逐个尝试候选端点。
	// 端点「不可用」时切换到下一个候选，保证单端点故障不影响可用性，包括：
	//   - endpointExhausted：本轮全部 API Key 尝试失败（key 问题）
	//   - retryableUpstream：上游 429/5xx/首字超时且代理重试耗尽（上游问题）
	// 2xx/4xx（含流式已写首字节）视为该端点已给出最终响应，直接 break。
	// 注意：模型名改写（将对外别名还原为真实模型名）必须在循环内对每个候选
	// 独立执行，因为各候选的 modelMappings 可能不同，复用同一 body 会导致
	// 错误的模型名被发送到不匹配的端点（如 opencode 的内部名发到日日新）。
	// 对齐 New API 的 RetryTimes：全部候选失败后不立即返回，等待 interval 后
	// 重试整轮，最多 endpointRetryRounds 轮，期间客户端保持等待状态。
	var res *relayLoopResult
	failCodes := []int{}
	var lastRes *relayLoopResult
	retryRoundFinished := false
	// failoverSteps 记录本轮请求逐个尝试过的端点与状态码，前端据此展示迁移趋势。
	var failoverSteps []map[string]interface{}
	for retryRound := 0; retryRound <= endpointRetryRounds; retryRound++ {
		// 每轮独立收集失败码；上一轮的失败响应体需关闭，避免连接泄漏。
		if lastRes != nil && lastRes.resp != nil {
			_ = lastRes.resp.Body.Close()
			lastRes = nil
		}
		failCodes = failCodes[:0]
		retryRoundCancelled := false
		for ci, cand := range endpointCandidates {
			// 每个候选独立解析模型映射，避免加权选中的端点映射污染其他候选。
			candModel, _ := s.resolveEndpointModel(cand, model)
			// 需要独立副本的情形：模型映射改写（写 model 字段）或 failover
			// 候选归一化（写 reasoning_effort）。首个候选不复制、保持原样透传；
			// 后续候选复制后再归一化，避免把 max 这类非标准值发给枚举更窄的上游。
			candBody := parsedBody
			needCopy := ci > 0 || (candModel != model && candModel != "")
			if needCopy {
				cp := make(map[string]interface{}, len(parsedBody))
				for k, v := range parsedBody {
					cp[k] = v
				}
				candBody = cp
			}
			if candModel != model && candModel != "" {
				candBody["model"] = candModel
			}
			if ci > 0 {
				normalizeReasoningEffort(candBody)
			}
			upstreamBodyBytes, _ := json.Marshal(candBody)

			fullURL := strings.TrimSuffix(cand.BaseURL, "/")
			if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
				fullURL += "/v1"
			}
			fullURL += "/chat/completions"
			res = s.relayLoop(relayLoopParams{
				route:          "chat.completions",
				ctx:            ctx,
				db:             db,
				selected:       cand,
				endpoints:      endpointCandidates,
				model:          model,
				fullURL:        fullURL,
				body:           upstreamBodyBytes,
				stream:         stream,
				sessionKey:     sessionKey,
				clientIP:       clientIP,
				requestStarted: requestStarted,
			})
			// 记录该候选的尝试结果（端点名 + 状态码），供前端展示迁移趋势。
			stepStatus := res.statusCode
			if stepStatus == 0 && res.resp != nil {
				stepStatus = res.resp.StatusCode
			}
			failoverSteps = append(failoverSteps, map[string]interface{}{"endpoint": cand.Name, "status": stepStatus})
			if res.resp != nil && !res.retryableUpstream && !res.endpointExhausted {
				selected = cand
				// 会话亲和：仅当上游返回 2xx/3xx（真正成功）时记录该会话最近使用的端点，
				// 后续同会话请求优先复用；4xx 客户端错误不记录，避免把会话钉死在
				// 无法服务该请求的端点上。
				if res.resp.StatusCode >= 200 && res.resp.StatusCode < 400 {
					s.recordChannelAffinity(sessionKey, cand.ID)
				}
				retryRoundFinished = true
				break
			}
			// 端点不可用（key 耗尽或上游可重试错误）：收集失败码后尝试下一个候选端点。
			if res.statusCode > 0 {
				failCodes = append(failCodes, res.statusCode)
			}
			if ci+1 < len(endpointCandidates) {
				selected = endpointCandidates[ci+1]
				failReason := "上游转发失败"
				if res.lastErr != nil {
					failReason = res.lastErr.Error()
				}
				s.recordRelayError(RelayErrorRecord{
					Route: "chat.completions", Kind: "endpoint_failover",
					Endpoint: cand.Name, EndpointID: cand.ID, Model: model,
					Stream: stream, ClientIP: clientIP,
					Attempts:  res.attempt + 1,
					ElapsedMs: time.Since(requestStarted).Milliseconds(),
					Error:     failReason,
				})
			}
		}
		if retryRoundFinished {
			break
		}
		// 全部候选均已失败（本轮）。继续下一轮前，等待间隔并检查客户端是否断开。
		lastRes = res
		if retryRound < endpointRetryRounds {
			select {
			case <-ctx.Done():
				retryRoundCancelled = true
			case <-time.After(endpointRetryDelay):
			}
		}
		if retryRoundCancelled {
			break
		}
	}
	// 全部候选端点均已失败（重试轮耗尽或客户端断开）：聚合错误决定返回给客户端的状态码。
	if len(endpointCandidates) > 0 && len(failCodes) == len(endpointCandidates) {
		failStatus := http.StatusServiceUnavailable
		allSame := true
		first := failCodes[0]
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		msg := fmt.Sprintf("网关无可用渠道（模型 %s）", model)
		if allSame && first >= 400 && first < 600 {
			failStatus = first
			msg = fmt.Sprintf("网关无可用渠道（模型 %s）：所有端点均返回 HTTP %d", model, first)
		}
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{"message": msg, "type": "service_unavailable"},
		})
		s.recordAnalyticsKey(ctx, "chat.completions", "", model, failStatus, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "upstream",
			Message:  msg,
			Response: errorResponseForLog(errBody, failStatus),
		})
		writeRelayUnavailable(w, model, failCodes)
		return
	}
	if lastRes != nil && lastRes.resp != nil {
		_ = lastRes.resp.Body.Close()
	}
	if res.lastErr != nil && res.resp == nil {
		// 失败原因与统计已在 relayLoop 内记录，这里仅按状态码写回响应。
		if res.statusCode == http.StatusInternalServerError {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": res.lastErr.Error()})
		} else {
			response.JSON(w, res.statusCode, map[string]interface{}{"error": map[string]string{"message": res.lastErr.Error(), "type": "proxy_error"}})
		}
		return
	}
	// 正文处理完/关闭后再释放 attempt context（defer 逆序：先关 Body 再 cancel）。
	if res.cancel != nil {
		defer res.cancel()
	}
	defer res.resp.Body.Close()

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(res.resp.StatusCode)

		sw := newSSEStreamWriter(w)
		buf := make([]byte, 4096)
		tail := make([]byte, 0, usageTailLimit)

		// 每次写前延长写超时，避免 http.Server.WriteTimeout 掐断长流式响应。
		extendStreamDeadline := func() {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(streamWriteDeadline))
		}

		// SSE ping 保活：上游长时间不吐流时向客户端发送注释行，穿透 NAT 空闲超时。
		stopPing := sw.startPing(ctx)
		defer stopPing()

		// 首字等待阶段已读到的数据块，直接作为流式响应的首批内容写回。
		if res.firstWritten && len(res.firstChunk) > 0 {
			extendStreamDeadline()
			sw.write(res.firstChunk)
			tail = append(tail, res.firstChunk...)
		}

		for {
			// 上游流中段停滞保护：idle 内无数据则终止流，防止请求无限挂死。
			n, err := readWithIdleTimeout(ctx, res.resp.Body, buf, streamIdleTimeout)
			if n > 0 {
				extendStreamDeadline()
				sw.write(buf[:n])
				tail = append(tail, buf[:n]...)
				if len(tail) > usageTailLimit {
					tail = tail[len(tail)-usageTailLimit:]
				}
			}
			if err != nil {
				break
			}
		}
		// 对齐 new-api：首字节后的流式中断也静默收尾，绝不向客户端报错。
		// 若上游未发送结束标记（[DONE]），补发收尾，保证前端对话正常结束。
		if !bytes.Contains(tail, []byte("[DONE]")) {
			extendStreamDeadline()
			sw.write([]byte("data: [DONE]\n\n"))
		}
		latencyMs := time.Since(res.startTime).Milliseconds()

		promptTokens := 0
		completionTokens := 0
		totalTokens := 0
		cachedTokens := 0

		accumulatedStr := string(tail)
		if matches := promptTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			promptTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := completionTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			completionTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := totalTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			totalTokens, _ = strconv.Atoi(matches[1])
		} else if promptTokens > 0 || completionTokens > 0 {
			totalTokens = promptTokens + completionTokens
		}
		if matches := cachedTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			cachedTokens, _ = strconv.Atoi(matches[1])
		}

		s.recordProxyTTFB(selected.ID, res.lastProxy, res.ttfbMs)
		fp, _ := json.Marshal(failoverSteps)
		var errInfo *AnalyticsError
		if res.resp.StatusCode >= 400 {
			errInfo = &AnalyticsError{
				Kind:    "upstream",
				Message: fmt.Sprintf("upstream returned HTTP %d (stream)", res.resp.StatusCode),
			}
		}
		s.recordAnalyticsKey(ctx, "chat.completions", selected.ID, model, res.resp.StatusCode, latencyMs, res.ttfbMs, promptTokens, completionTokens, totalTokens, cachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(totalTokens))
		}
	} else {
		respBodyBytes, _ := io.ReadAll(res.resp.Body)
		latencyMs := time.Since(res.startTime).Milliseconds()

		var usageInfo struct {
			Usage struct {
				PromptTokens        int `json:"prompt_tokens"`
				CompletionTokens    int `json:"completion_tokens"`
				TotalTokens         int `json:"total_tokens"`
				PromptTokensDetails struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(respBodyBytes, &usageInfo)

		s.recordProxyTTFB(selected.ID, res.lastProxy, latencyMs)
		fp, _ := json.Marshal(failoverSteps)
		var errInfo *AnalyticsError
		if res.resp.StatusCode >= 400 {
			errInfo = &AnalyticsError{
				Kind:     "upstream",
				Message:  upstreamErrorMessage(respBodyBytes),
				Response: errorResponseForLog(respBodyBytes, res.resp.StatusCode),
			}
		}
		s.recordAnalyticsKey(ctx, "chat.completions", selected.ID, model, res.resp.StatusCode, latencyMs, 0, usageInfo.Usage.PromptTokens, usageInfo.Usage.CompletionTokens, usageInfo.Usage.TotalTokens, usageInfo.Usage.PromptTokensDetails.CachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(usageInfo.Usage.TotalTokens))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.resp.StatusCode)
		_, _ = w.Write(respBodyBytes)
	}
}

// normalizeResponsesTools 为缺失 name 的工具补充 name（取值等于 type）。
// 上游 zen 用 serde flatten 解析 tools，要求每个工具都带顶层 name；
// OpenAI 官方的 web_search 等工具本身没有 name 字段，补齐避免反序列化失败。
func normalizeResponsesTools(body map[string]interface{}) {
	tools, ok := body["tools"].([]interface{})
	if !ok {
		return
	}
	for _, item := range tools {
		tool, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if _, has := tool["name"]; has {
			continue
		}
		if t, ok := tool["type"].(string); ok && t != "" {
			tool["name"] = t
		}
	}
}

// normalizeResponsesInput 规范化 Responses 请求的 input 列表，兼容 zen 的转换缺陷：
//  1. assistant 消息的 content 数组（output_text 块）在 zen 转 chat 时不被识别，
//     需提取文本为字符串，否则报 "Invalid assistant message: content or tool_calls must be set"。
//  2. input 以 function_call_output 结尾时，zen 转成 chat 的 tool 消息后无后续 user，
//     报 "reasoning_content in the thinking mode must be passed back"，
//     末尾补一条空 user 消息即可通过。
//  3. 独立 function_call items 归并到相邻 assistant 消息的 tool_calls（chat 风格）。
//     zen 对独立 function_call item 的归并不稳定（同样的请求时而 200 时而 400
//     "An assistant message with 'tool_calls' must be followed by tool messages responding
//     to each 'tool_call_id'"），显式归并后可稳定通过。
//  4. assistant 已自带 tool_calls 但后续 function_call_output 不足时（codex 多轮
//     并行工具分步回传：历史 tool_calls 仍含全部 call_id，但部分工具结果尚未返回），
//     zen 转 chat 会报 "insufficient tool messages following tool_calls message"。
//     对未被任何 function_call_output 回应的 tool_call 做防御性剔除，让校验通过。
func normalizeResponsesInput(body map[string]interface{}) {
	input, ok := body["input"].([]interface{})
	if !ok {
		return
	}
	// responded 记录已被 function_call_output 回应的 call_id。归并时从独立
	// function_call item 取 call_id（call_id 优先，回退 id）；assistant 自带
	// tool_calls 的 call_id 也在最终校验阶段核对。
	responded := map[string]bool{}
	normalized := make([]interface{}, 0, len(input))
	var lastAssistant map[string]interface{}
	for _, item := range input {
		msg, ok := item.(map[string]interface{})
		if !ok {
			normalized = append(normalized, item)
			continue
		}
		switch msg["type"] {
		case "function_call":
			// 归并到相邻 assistant 消息的 tool_calls，并丢弃独立 item。
			if lastAssistant == nil {
				// 防御：无前驱 assistant 时原样透传，避免静默丢弃。
				normalized = append(normalized, item)
				continue
			}
			name, _ := msg["name"].(string)
			args, _ := msg["arguments"].(string)
			callID, _ := msg["call_id"].(string)
			if callID == "" {
				callID, _ = msg["id"].(string)
			}
			if name != "" {
				toolCalls, _ := lastAssistant["tool_calls"].([]interface{})
				lastAssistant["tool_calls"] = append(toolCalls, map[string]interface{}{
					"id":   callID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      name,
						"arguments": args,
					},
				})
			}
			continue
		case "function_call_output":
			if callID, _ := msg["call_id"].(string); callID != "" {
				responded[callID] = true
			}
		}
		normalized = append(normalized, item)
		if msg["type"] == "message" {
			if role, _ := msg["role"].(string); role == "assistant" {
				lastAssistant = msg
			} else {
				lastAssistant = nil
			}
		}
	}

	// assistant 消息的 content 数组提取为字符串。
	for _, item := range normalized {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if msg["type"] != "message" {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		contentArr, ok := msg["content"].([]interface{})
		if !ok || len(contentArr) == 0 {
			continue
		}
		var text strings.Builder
		hasText := false
		for _, part := range contentArr {
			partMap, ok := part.(map[string]interface{})
			if !ok {
				continue
			}
			if partMap["type"] != "output_text" && partMap["type"] != "input_text" {
				continue
			}
			if t, ok := partMap["text"].(string); ok {
				if hasText {
					text.WriteString("\n")
				}
				text.WriteString(t)
				hasText = true
			}
		}
		if hasText {
			msg["content"] = text.String()
		}
	}

	// 防御性剔除：assistant 已声明但未被任何 function_call_output 回应的 tool_call
	// 会触发 zen 的 "insufficient tool messages following tool_calls message"。codex
	// 多轮并行工具分步回传时历史 tool_calls 含全部 call_id，但部分 output 尚未返回，
	// 这类未回应的调用本轮无法执行，剔除后既满足 zen 校验也不改变对话语义。
	for _, item := range normalized {
		msg, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if msg["type"] != "message" {
			continue
		}
		if role, _ := msg["role"].(string); role != "assistant" {
			continue
		}
		toolCalls, ok := msg["tool_calls"].([]interface{})
		if !ok || len(toolCalls) == 0 {
			continue
		}
		kept := toolCalls[:0]
		for _, tc := range toolCalls {
			tcMap, ok := tc.(map[string]interface{})
			if !ok {
				kept = append(kept, tc)
				continue
			}
			callID, _ := tcMap["id"].(string)
			if callID != "" && !responded[callID] {
				// 无对应 function_call_output：剔除。
				continue
			}
			kept = append(kept, tc)
		}
		if len(kept) == 0 {
			delete(msg, "tool_calls")
		} else {
			msg["tool_calls"] = kept
		}
	}

	// 末尾补齐：若最后一条是 function_call_output，追加空 user 消息。
	if len(normalized) > 0 {
		if last, ok := normalized[len(normalized)-1].(map[string]interface{}); ok {
			if t, _ := last["type"].(string); t == "function_call_output" {
				normalized = append(normalized, map[string]interface{}{
					"type":    "message",
					"role":    "user",
					"content": "",
				})
			}
		}
	}
	body["input"] = normalized
}

// normalizeChatContentBlocks 把 Anthropic/Claude 或 agent 客户端发送的 content
// blocks 数组归一化为 OpenAI chat.completions 标准格式。上游 zen 的 chat.completions
// 只接受 content 为字符串或 OpenAI 标准 parts，若传入含 {type:"thinking",
// signature:"reasoning_content"} / {type:"toolCall"} / {type:"tool_use"} 等块会直接
// 400。归一化规则：
//   - thinking / reasoning / redacted_thinking：提取 thinking 文本累积到消息顶层
//     reasoning_content，并丢弃该块（避免把 Anthropic signature 传给 zen）。
//   - toolCall / tool-call / tool_use block：转化为标准 tool_calls（id/type/function）。
//     arguments 优先（PI 用对象或字符串），其次 input（Anthropic 用结构化对象）。
//   - text：合并为 content 字符串。
//   - image / image_url：保留为 OpenAI 图片 parts。
//   - tool_result：随 keptParts 原样保留（对应消息已是 role=tool 时由 zen 直接处理）。
//
// 仅当 content 为非空数组且含可识别块时才改写；纯普通图片数组（image_url）不动。
func normalizeChatContentBlocks(body map[string]interface{}) {
	messages, ok := body["messages"].([]interface{})
	if !ok {
		return
	}
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		contentArr, ok := msg["content"].([]interface{})
		if !ok || len(contentArr) == 0 {
			continue
		}
		role, _ := msg["role"].(string)
		var text strings.Builder
		hasText := false
		var reasoning strings.Builder
		hasReasoning := false
		var toolCalls []interface{}
		var keptParts []interface{}
		needsRewrite := false
		for _, part := range contentArr {
			pm, ok := part.(map[string]interface{})
			if !ok {
				keptParts = append(keptParts, part)
				continue
			}
			ptype, _ := pm["type"].(string)
			switch ptype {
			case "text":
				if t, ok := pm["text"].(string); ok {
					if hasText {
						text.WriteString("\n")
					}
					text.WriteString(t)
					hasText = true
				}
				needsRewrite = true
			case "thinking", "reasoning", "redacted_thinking":
				if t := chatContentThinkingText(pm); t != "" {
					if hasReasoning {
						reasoning.WriteString("\n")
					}
					reasoning.WriteString(t)
					hasReasoning = true
				}
				// 丢弃 thinking 块，reasoning 转顶层字段。
				needsRewrite = true
			case "toolCall", "tool-call", "tool_use":
				name, _ := pm["name"].(string)
				callID, _ := pm["id"].(string)
				argsStr := chatContentToolArguments(pm)
				if name != "" {
					toolCalls = append(toolCalls, map[string]interface{}{
						"id":   callID,
						"type": "function",
						"function": map[string]interface{}{
							"name":      name,
							"arguments": argsStr,
						},
					})
				}
				needsRewrite = true
			case "image", "image_url":
				// 保持 OpenAI 图片 part 原样。
				keptParts = append(keptParts, part)
			default:
				keptParts = append(keptParts, part)
			}
		}

		if !needsRewrite {
			continue
		}

		var content interface{}
		switch {
		case hasText:
			// 文本合并为首个或唯一 part；若同时含图片/其余 part，则文本作为
			// ContentTextPart 后接其余 part，保证 zen 接受的 OpenAI parts 结构。
			var merged []interface{}
			if len(keptParts) == 0 {
				content = text.String()
			} else {
				merged = append(merged, map[string]interface{}{
					"type": "text",
					"text": text.String(),
				})
				content = append(merged, keptParts...)
			}
		case len(keptParts) > 0:
			content = keptParts
		default:
			if role == "assistant" && len(toolCalls) > 0 {
				content = ""
			} else {
				content = text.String()
			}
		}

		msg["content"] = content
		if hasReasoning {
			msg["reasoning_content"] = reasoning.String()
		}
		if len(toolCalls) > 0 && role == "assistant" {
			msg["tool_calls"] = toolCalls
		}
	}

	// zen 的 thinking 模式下，assistant 消息一旦在 tool 循环中开启思考，之后每轮
	// toolCall 轮次的 assistant 消息都必须携带 reasoning_content（可为空串），否则
	// 上游返回 400 "The `reasoning_content` in the thinking mode must be passed back
	// to the API"。PI 等 agent 客户端在多轮工具调用时可能漏发 thinking 块，这里做
	// 兜底：记录 thinking 是否已开启，对后续缺失 reasoning_content 的 assistant
	// toolCall 消息补空串，满足 zen 的连续传回要求。
	thinkingActive := false
	for _, m := range messages {
		msg, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if _, hasRC := msg["reasoning_content"]; hasRC {
			thinkingActive = true
		}
		if role == "user" || role == "system" {
			// 新一轮用户请求重置思考状态：新的对话轮不要求续传上一轮 reasoning。
			thinkingActive = false
		}
		if thinkingActive && role == "assistant" {
			if _, hasRC := msg["reasoning_content"]; !hasRC {
				if _, hasTC := msg["tool_calls"]; hasTC {
					msg["reasoning_content"] = ""
				}
			}
		}
	}
}

// chatContentThinkingText 提取 thinking/reasoning block 中的文本。PI 用
// {type:"thinking", thinking, signature:"reasoning_content"}，部分 agent 用
// {type:"reasoning", text}；统一兼容 thinking/reasoning_content/text/content。
func chatContentThinkingText(pm map[string]interface{}) string {
	for _, key := range []string{"thinking", "reasoning_content", "text", "content"} {
		if t, ok := pm[key].(string); ok && t != "" {
			return t
		}
	}
	return ""
}

// chatContentToolArguments 提取 toolCall block 的参数并序列化为 JSON 字符串。
// PI 用 arguments（对象或字符串），Anthropic 用 input（结构化对象）。
func chatContentToolArguments(pm map[string]interface{}) string {
	if v, ok := pm["arguments"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	if v, ok := pm["input"]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return "{}"
}

// sseDataJSON 提取 SSE 事件块中 data: 行的内容。
func sseDataJSON(block []byte) (string, bool) {
	for _, line := range strings.Split(string(block), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "data:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
		}
	}
	return "", false
}

// responsesStreamNormalizer 把上游精简的 Responses 流式事件补全为 Codex 可解析的标准事件序列。
// 部分上游（如 zen）直接发 output_text.delta / function_call_arguments.delta，缺少
// output_item.added（message 前导）与 output_item.done（完整 item）事件；Codex 在
// active item 缺失时收到文本 delta 会直接报错（OutputTextDelta without active item），
// 且工具调用的完整 arguments 只从 output_item.done 中读取，缺失会导致调用永远不执行。
// 网关在此补全：文本 delta 前注入 message item 的 added，item 切换与 completed 前注入 done。
type responsesStreamNormalizer struct {
	model       string
	respID      string
	createdSent bool
	msgOpen     bool
	msgID       string
	msgText     strings.Builder
	fnOpen      bool
	fnID        string
	fnName      string
	fnCallID    string
	fnArgs      strings.Builder
}

func newResponsesStreamNormalizer(model string) *responsesStreamNormalizer {
	return &responsesStreamNormalizer{model: model}
}

// sseEventType 解析 SSE 事件块中的 data JSON 的 type 字段。
func sseEventType(block []byte) (string, map[string]interface{}) {
	dataJSON, ok := sseDataJSON(block)
	if !ok {
		return "", nil
	}
	var ev map[string]interface{}
	if err := json.Unmarshal([]byte(dataJSON), &ev); err != nil {
		return "", nil
	}
	t, _ := ev["type"].(string)
	return t, ev
}

func sseEventBlock(eventType string, payload interface{}) []byte {
	payloadJSON, _ := json.Marshal(payload)
	out := append([]byte("event: "+eventType+"\ndata: "), payloadJSON...)
	return append(out, []byte("\n\n")...)
}

// transform 处理一个上游事件块，返回需要写出的一个或多个事件块。
func (n *responsesStreamNormalizer) transform(block []byte) [][]byte {
	eventType, ev := sseEventType(block)
	if eventType == "" {
		return [][]byte{block}
	}

	var outs [][]byte

	// 首事件前注入 response.created（若上游未发）。
	if !n.createdSent {
		n.createdSent = true
		if eventType != "response.created" {
			respID := n.respID
			if respID == "" {
				respID = uuid.NewString()
			}
			n.respID = respID
			outs = append(outs, sseEventBlock("response.created", map[string]interface{}{
				"type": "response.created",
				"response": map[string]interface{}{
					"id":         respID,
					"object":     "response",
					"created_at": time.Now().Unix(),
					"status":     "in_progress",
					"model":      n.model,
					"output":     []interface{}{},
					"usage":      nil,
				},
			}))
		}
	}

	switch eventType {
	case "response.created":
		if n.respID == "" {
			if resp, ok := ev["response"].(map[string]interface{}); ok {
				if id, ok := resp["id"].(string); ok {
					n.respID = id
				}
			}
		}
	case "response.output_text.delta":
		// Codex 需要 message item 先建立（active item）才能挂文本 delta。
		if !n.msgOpen {
			n.msgOpen = true
			n.msgID = "msg_" + uuid.NewString()
			outs = append(outs, sseEventBlock("response.output_item.added", map[string]interface{}{
				"type":         "response.output_item.added",
				"output_index": 0,
				"item": map[string]interface{}{
					"id":      n.msgID,
					"type":    "message",
					"status":  "in_progress",
					"role":    "assistant",
					"content": []interface{}{},
				},
			}))
		}
		if delta, ok := ev["delta"].(string); ok {
			n.msgText.WriteString(delta)
		}
	case "response.output_item.added":
		if item, ok := ev["item"].(map[string]interface{}); ok {
			itemType, _ := item["type"].(string)
			switch itemType {
			case "message":
				n.msgOpen = true
				if id, ok := item["id"].(string); ok {
					n.msgID = id
				}
			case "function_call":
				// 切换 item 前先关闭未完成的 message 与上一个 function_call
				// （上游并行工具调用时会连续发多个 function_call 的 added，
				// 不关闭会导致参数拼进同一个 arguments 变成非法 JSON）。
				outs = append(outs, n.closeMessageIfOpen()...)
				outs = append(outs, n.closeFunctionIfOpen()...)
				n.fnOpen = true
				if id, ok := item["id"].(string); ok {
					n.fnID = id
				}
				if name, ok := item["name"].(string); ok {
					n.fnName = name
				}
				if callID, ok := item["call_id"].(string); ok {
					n.fnCallID = callID
				}
			}
		}
	case "response.function_call_arguments.delta":
		if delta, ok := ev["delta"].(string); ok {
			n.fnArgs.WriteString(delta)
		}
	case "response.output_item.done":
		if item, ok := ev["item"].(map[string]interface{}); ok {
			if itemType, _ := item["type"].(string); itemType == "message" {
				n.msgOpen = false
				n.msgText.Reset()
			} else if itemType == "function_call" {
				n.fnOpen = false
				n.fnArgs.Reset()
			}
		}
	case "response.completed":
		outs = append(outs, n.closeFunctionIfOpen()...)
		outs = append(outs, n.closeMessageIfOpen()...)
	}

	outs = append(outs, block)
	return outs
}

// closeMessageIfOpen 关闭未完成的 message item（补齐 output_item.done）。
func (n *responsesStreamNormalizer) closeMessageIfOpen() [][]byte {
	if !n.msgOpen {
		return nil
	}
	n.msgOpen = false
	content := []interface{}{}
	if n.msgText.Len() > 0 {
		content = append(content, map[string]interface{}{
			"type": "output_text",
			"text": n.msgText.String(),
		})
	}
	done := sseEventBlock("response.output_item.done", map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": map[string]interface{}{
			"id":      n.msgID,
			"type":    "message",
			"status":  "completed",
			"role":    "assistant",
			"content": content,
		},
	})
	n.msgText.Reset()
	return [][]byte{done}
}

// closeFunctionIfOpen 关闭未完成的 function_call item（补齐 output_item.done 与完整 arguments）。
func (n *responsesStreamNormalizer) closeFunctionIfOpen() [][]byte {
	if !n.fnOpen {
		return nil
	}
	n.fnOpen = false
	callID := n.fnCallID
	if callID == "" {
		callID = n.fnID
	}
	done := sseEventBlock("response.output_item.done", map[string]interface{}{
		"type":         "response.output_item.done",
		"output_index": 0,
		"item": map[string]interface{}{
			"id":        n.fnID,
			"type":      "function_call",
			"status":    "completed",
			"name":      n.fnName,
			"arguments": n.fnArgs.String(),
			"call_id":   callID,
		},
	})
	n.fnArgs.Reset()
	return [][]byte{done}
}

// readSSEBlock 从流中读取一个完整 SSE 事件块（到空行结束，含结尾空行）。
func readSSEBlock(br *bufio.Reader) ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				if buf.Len() > 0 {
					buf.WriteString(line)
					return buf.Bytes(), nil
				}
				if err != nil {
					return nil, err
				}
				continue
			}
			buf.WriteString(line)
		}
		if err != nil {
			if buf.Len() > 0 {
				return buf.Bytes(), nil
			}
			return nil, err
		}
	}
}

// proxyResponses 代理 OpenAI Responses API（POST /v1/responses）。
// 请求体按不透明 JSON 透传（Responses 的 input/instructions 结构与 chat 不同，
// 网关不做改写），仅复用端点的模型路由、代理池与首字超时切换能力。
func (s *Service) proxyResponses(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	requestStarted := time.Now()
	clientIP := s.resolveClientIP(r)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		// 请求体读取失败（如客户端上传超时中断）：这是网关侧问题，用 502 表达，
		// 避免与「请求体格式错误」的 400 语义混淆。
		s.recordRelayError(RelayErrorRecord{
			Route: "responses", Kind: "gateway",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body read failed: " + err.Error(),
		})
		// 网关拦截（未到达上游）不写入调用日志。
		response.JSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	var parsedBody map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsedBody); err != nil {
		s.recordRelayError(RelayErrorRecord{
			Route: "responses", Kind: "bad_request",
			ClientIP: clientIP, ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error: "request body is not valid JSON: " + err.Error(),
		})
		// 网关拦截（未到达上游）也写入调用日志（含报错信息），便于日志与 AI 排障。
		errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
		s.recordAnalyticsKey(ctx, "responses", "", "", http.StatusBadRequest, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, 0, 0, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "bad_request",
			Message:  "request body is not valid JSON: " + err.Error(),
			Response: errorResponseForLog(errBody, http.StatusBadRequest),
		})
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	model, _ := parsedBody["model"].(string)
	stream, _ := parsedBody["stream"].(bool)
	targetEndpointID := s.resolveTargetEndpoint(r)
	sessionKey := resolveSessionKey(r, parsedBody)

	db, err := s.open(ctx)
	if err != nil {
		// 网关侧数据库故障，未进入转发；不写入调用日志。
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	endpointCandidates, selected, _, _, found := s.selectEndpointCandidates(ctx, db, model, targetEndpointID, sessionKey)
	if !found {
		s.recordRelayError(RelayErrorRecord{
			Route: "responses", Kind: "no_endpoint",
			Model: model, Stream: stream, ClientIP: clientIP,
			ElapsedMs: time.Since(requestStarted).Milliseconds(),
			Error:     fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", model, targetEndpointID),
		})
		// 候选池为空属网关自身状态，仍写入调用日志（含报错信息），便于日志与 AI 排障。
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
				"type":    "service_unavailable",
			},
		})
		s.recordAnalyticsKey(ctx, "responses", "", model, http.StatusServiceUnavailable, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), 0, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "no_endpoint",
			Message:  fmt.Sprintf("no enabled endpoint serves model %q (target_endpoint=%q)", model, targetEndpointID),
			Response: errorResponseForLog(errBody, http.StatusServiceUnavailable),
		})
		response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error": map[string]string{
				"message": fmt.Sprintf("网关无可用渠道（模型 %s）", model),
				"type":    "service_unavailable",
			},
		})
		return
	}

	// 记录是否经由本端点配置的代理池出网。先于网关密钥限制等分支计算，
	// 使这些早退路径的调用日志也能正确标注代理。
	viaProxy := 0
	if len(selected.ProxyPool) > 0 {
		viaProxy = 1
	}

	// 网关密钥限制：模型白名单 / 端点白名单 / token 配额。
	if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
		if limitErr := s.enforceGatewayKeyLimits(ctx, keyIdentity, model, selected.ID); limitErr != "" {
			s.recordRelayError(RelayErrorRecord{
				Route: "responses", Kind: "blocked",
				Endpoint: selected.Name, EndpointID: selected.ID,
				Model: model, Stream: stream, ClientIP: clientIP,
				ElapsedMs: time.Since(requestStarted).Milliseconds(),
				Error:     limitErr,
			})
			errBody, _ := json.Marshal(map[string]interface{}{
				"error": map[string]string{
					"message": limitErr,
					"type":    "forbidden",
				},
			})
			s.recordAnalyticsKey(ctx, "responses", selected.ID, model, http.StatusForbidden, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "", -1, "", &AnalyticsError{
				Kind:     "blocked",
				Message:  limitErr,
				Response: errorResponseForLog(errBody, http.StatusForbidden),
			})
			response.JSON(w, http.StatusForbidden, map[string]interface{}{
				"error": map[string]string{
					"message": limitErr,
					"type":    "forbidden",
				},
			})
			return
		}
	}

	// Responses API 的路径为 /responses。
	fullURL := strings.TrimSuffix(selected.BaseURL, "/")
	if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
		fullURL += "/v1"
	}
	fullURL += "/responses"

	// 若请求模型名是对外别名，转发到上游时还原为真实模型名。
	// 注意：必须在循环内对每个候选独立执行，因为各候选的 modelMappings 可能不同。
	normalizeResponsesTools(parsedBody)
	normalizeResponsesInput(parsedBody)

	// 对齐 New API 的 RetryTimes：全部候选失败后不立即返回，等待 interval 后
	// 重试整轮，最多 endpointRetryRounds 轮，期间客户端保持等待状态。
	var res *relayLoopResult
	failCodes := []int{}
	var lastRes *relayLoopResult
	retryRoundFinished := false
	var failoverSteps []map[string]interface{}
	for retryRound := 0; retryRound <= endpointRetryRounds; retryRound++ {
		// 每轮独立收集失败码；上一轮的失败响应体需关闭，避免连接泄漏。
		if lastRes != nil && lastRes.resp != nil {
			_ = lastRes.resp.Body.Close()
			lastRes = nil
		}
		failCodes = failCodes[:0]
		retryRoundCancelled := false
		for ci, cand := range endpointCandidates {
			// 每个候选独立解析模型映射，避免加权选中的端点映射污染其他候选。
			candModel, _ := s.resolveEndpointModel(cand, model)
			// 需要独立副本的情形：模型映射改写（写 model 字段）或 failover
			// 候选归一化（写 reasoning.effort）。首个候选不复制、保持原样透传；
			// 后续候选复制后再归一化，避免把 max 这类非标准值发给枚举更窄的上游。
			candBody := parsedBody
			needCopy := ci > 0 || (candModel != model && candModel != "")
			if needCopy {
				cp := make(map[string]interface{}, len(parsedBody))
				for k, v := range parsedBody {
					cp[k] = v
				}
				candBody = cp
			}
			if candModel != model && candModel != "" {
				candBody["model"] = candModel
			}
			if ci > 0 {
				normalizeReasoningEffort(candBody)
			}
			upstreamBodyBytes, _ := json.Marshal(candBody)

			fullURL := strings.TrimSuffix(cand.BaseURL, "/")
			if !strings.HasSuffix(strings.ToLower(fullURL), "/v1") && !strings.Contains(strings.ToLower(fullURL), "/v1/") {
				fullURL += "/v1"
			}
			fullURL += "/responses"
			res = s.relayLoop(relayLoopParams{
				route:          "responses",
				ctx:            ctx,
				db:             db,
				selected:       cand,
				endpoints:      endpointCandidates,
				model:          model,
				fullURL:        fullURL,
				body:           upstreamBodyBytes,
				stream:         stream,
				sessionKey:     sessionKey,
				clientIP:       clientIP,
				requestStarted: requestStarted,
			})
			// 记录该候选的尝试结果（端点名 + 状态码），供前端展示迁移趋势。
			stepStatus := res.statusCode
			if stepStatus == 0 && res.resp != nil {
				stepStatus = res.resp.StatusCode
			}
			failoverSteps = append(failoverSteps, map[string]interface{}{"endpoint": cand.Name, "status": stepStatus})
			if res.resp != nil && !res.retryableUpstream && !res.endpointExhausted {
				selected = cand
				// 会话亲和：仅当上游返回 2xx/3xx（真正成功）时记录该会话最近使用的端点，
				// 4xx 客户端错误不记录，避免把会话钉死在无法服务该请求的端点上。
				if res.resp.StatusCode >= 200 && res.resp.StatusCode < 400 {
					s.recordChannelAffinity(sessionKey, cand.ID)
				}
				retryRoundFinished = true
				break
			}
			// 端点不可用（key 耗尽或上游可重试错误）：收集失败码后尝试下一个候选端点。
			if res.statusCode > 0 {
				failCodes = append(failCodes, res.statusCode)
			}
			if ci+1 < len(endpointCandidates) {
				selected = endpointCandidates[ci+1]
				failReason := "上游转发失败"
				if res.lastErr != nil {
					failReason = res.lastErr.Error()
				}
				s.recordRelayError(RelayErrorRecord{
					Route: "responses", Kind: "endpoint_failover",
					Endpoint: cand.Name, EndpointID: cand.ID, Model: model,
					Stream: stream, ClientIP: clientIP,
					Attempts:  res.attempt + 1,
					ElapsedMs: time.Since(requestStarted).Milliseconds(),
					Error:     failReason,
				})
			}
		}
		if retryRoundFinished {
			break
		}
		// 全部候选均已失败（本轮）。继续下一轮前，等待间隔并检查客户端是否断开。
		lastRes = res
		if retryRound < endpointRetryRounds {
			select {
			case <-ctx.Done():
				retryRoundCancelled = true
			case <-time.After(endpointRetryDelay):
			}
		}
		if retryRoundCancelled {
			break
		}
	}
	// 全部候选端点均已失败（重试轮耗尽或客户端断开）：聚合错误决定返回给客户端的状态码。
	if len(endpointCandidates) > 0 && len(failCodes) == len(endpointCandidates) {
		failStatus := http.StatusServiceUnavailable
		allSame := true
		first := failCodes[0]
		for _, c := range failCodes[1:] {
			if c != first {
				allSame = false
				break
			}
		}
		msg := fmt.Sprintf("网关无可用渠道（模型 %s）", model)
		if allSame && first >= 400 && first < 600 {
			failStatus = first
			msg = fmt.Sprintf("网关无可用渠道（模型 %s）：所有端点均返回 HTTP %d", model, first)
		}
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]string{"message": msg, "type": "service_unavailable"},
		})
		s.recordAnalyticsKey(ctx, "responses", "", model, failStatus, time.Since(requestStarted).Milliseconds(), 0, 0, 0, 0, 0, boolToInt(stream), viaProxy, clientIP, "", -1, "", &AnalyticsError{
			Kind:     "upstream",
			Message:  msg,
			Response: errorResponseForLog(errBody, failStatus),
		})
		writeRelayUnavailable(w, model, failCodes)
		return
	}
	if lastRes != nil && lastRes.resp != nil {
		_ = lastRes.resp.Body.Close()
	}
	if res.lastErr != nil && res.resp == nil {
		// 失败原因与统计已在 relayLoop 内记录，这里仅按状态码写回响应。
		if res.statusCode == http.StatusInternalServerError {
			response.JSON(w, http.StatusInternalServerError, map[string]string{"error": res.lastErr.Error()})
		} else {
			response.JSON(w, res.statusCode, map[string]interface{}{"error": map[string]string{"message": res.lastErr.Error(), "type": "proxy_error"}})
		}
		return
	}
	// 正文处理完/关闭后再释放 attempt context（defer 逆序：先关 Body 再 cancel）。
	if res.cancel != nil {
		defer res.cancel()
	}
	defer res.resp.Body.Close()

	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(res.resp.StatusCode)

		sw := newSSEStreamWriter(w)
		// 每次写前延长写超时，避免 http.Server.WriteTimeout 掐断长流式响应。
		extendStreamDeadline := func() {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(streamWriteDeadline))
		}
		// SSE ping 保活：上游长时间不吐流时向客户端发送注释行，穿透 NAT 空闲超时。
		stopPing := sw.startPing(ctx)
		defer stopPing()
		// 部分上游（如 zen）的 Responses 流缺少 response.created / output_item 容器事件，
		// Codex 等 SDK 依赖它们初始化响应与挂载文本/工具参数，缺失会导致空白回。
		// 用状态机逐事件补全后转发。
		normalizer := newResponsesStreamNormalizer(model)
		streamReader := bufio.NewReader(res.resp.Body)
		if res.firstWritten && len(res.firstChunk) > 0 {
			streamReader = bufio.NewReader(io.MultiReader(bytes.NewReader(res.firstChunk), res.resp.Body))
		}
		// usage 信息总在最后的 response.completed 事件里，只保留流尾部即可，
		// 避免长对话把整个流式响应累积在内存中。
		tail := make([]byte, 0, usageTailLimit)
		for {
			block, readErr := readSSEBlock(streamReader)
			if len(block) > 0 {
				tail = append(tail, block...)
				if len(tail) > usageTailLimit {
					tail = tail[len(tail)-usageTailLimit:]
				}
				for _, out := range normalizer.transform(block) {
					extendStreamDeadline()
					sw.write(out)
				}
			}
			if readErr != nil {
				break
			}
		}
		latencyMs := time.Since(res.startTime).Milliseconds()

		// 从尾部 response.completed 事件解析 usage（Responses 用 input/output_tokens）。
		promptTokens := 0
		completionTokens := 0
		totalTokens := 0
		cachedTokens := 0
		accumulatedStr := string(tail)
		if matches := inputTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			promptTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := outputTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			completionTokens, _ = strconv.Atoi(matches[1])
		}
		if matches := totalTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			totalTokens, _ = strconv.Atoi(matches[1])
		} else if promptTokens > 0 || completionTokens > 0 {
			totalTokens = promptTokens + completionTokens
		}
		if matches := cachedTokensRegex.FindStringSubmatch(accumulatedStr); len(matches) > 1 {
			cachedTokens, _ = strconv.Atoi(matches[1])
		}

		s.recordProxyTTFB(selected.ID, res.lastProxy, res.ttfbMs)
		fp, _ := json.Marshal(failoverSteps)
		var errInfo *AnalyticsError
		if res.resp.StatusCode >= 400 {
			errInfo = &AnalyticsError{
				Kind:    "upstream",
				Message: fmt.Sprintf("upstream returned HTTP %d (stream)", res.resp.StatusCode),
			}
		}
		s.recordAnalyticsKey(ctx, "responses", selected.ID, model, res.resp.StatusCode, latencyMs, res.ttfbMs, promptTokens, completionTokens, totalTokens, cachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(totalTokens))
		}
	} else {
		respBodyBytes, _ := io.ReadAll(res.resp.Body)
		latencyMs := time.Since(res.startTime).Milliseconds()

		var usageInfo struct {
			Usage struct {
				InputTokens         int `json:"input_tokens"`
				OutputTokens        int `json:"output_tokens"`
				TotalTokens         int `json:"total_tokens"`
				InputTokensDetails  struct {
					CachedTokens int `json:"cached_tokens"`
				} `json:"input_tokens_details"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(respBodyBytes, &usageInfo)

		s.recordProxyTTFB(selected.ID, res.lastProxy, latencyMs)
		fp, _ := json.Marshal(failoverSteps)
		var errInfo *AnalyticsError
		if res.resp.StatusCode >= 400 {
			errInfo = &AnalyticsError{
				Kind:     "upstream",
				Message:  upstreamErrorMessage(respBodyBytes),
				Response: errorResponseForLog(respBodyBytes, res.resp.StatusCode),
			}
		}
		s.recordAnalyticsKey(ctx, "responses", selected.ID, model, res.resp.StatusCode, latencyMs, 0, usageInfo.Usage.InputTokens, usageInfo.Usage.OutputTokens, usageInfo.Usage.TotalTokens, usageInfo.Usage.InputTokensDetails.CachedTokens, boolToInt(stream), boolToInt(res.lastProxy != ""), clientIP, res.egressIP, res.lastKeyIndex, string(fp), errInfo)
		s.recordEndpointLatency(selected.ID, latencyMs)
		if keyIdentity := gatewayKeyFromContext(ctx); keyIdentity.ID != "" {
			s.consumeGatewayKeyTokens(ctx, keyIdentity, int64(usageInfo.Usage.TotalTokens))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(res.resp.StatusCode)
		_, _ = w.Write(respBodyBytes)
	}
}

func (s *Service) GetModelsList(ctx context.Context, anonymizeOwner bool) ([]map[string]interface{}, error) {
	db, err := s.open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT name, enabled, status, models, disabled_models, model_mappings FROM openai_endpoints WHERE enabled = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	modelMap := make(map[string]map[string]interface{})
	for rows.Next() {
		var name, status, modelsRaw string
		var enabledInt int
		var disabledRaw, mappingsRaw sql.NullString
		if err := rows.Scan(&name, &enabledInt, &status, &modelsRaw, &disabledRaw, &mappingsRaw); err == nil {
			var models []string
			if modelsRaw != "" {
				_ = json.Unmarshal([]byte(modelsRaw), &models)
			}
			disabled := []string{}
			if disabledRaw.Valid && disabledRaw.String != "" {
				_ = json.Unmarshal([]byte(disabledRaw.String), &disabled)
			}
			mappings := map[string]string{}
			if mappingsRaw.Valid && mappingsRaw.String != "" {
				_ = json.Unmarshal([]byte(mappingsRaw.String), &mappings)
			}
			for _, mID := range models {
				if isModelDisabled(disabled, mID) {
					continue
				}
				// 对外名称：存在映射时使用别名。
				externalID := mID
				if alias := mappings[mID]; alias != "" {
					externalID = alias
				}
				if _, ok := modelMap[externalID]; !ok {
					owner := name
					if anonymizeOwner {
						// 外部统一出口不泄漏内部端点名。
						owner = "api-monitor-gateway"
					}
					modelMap[externalID] = map[string]interface{}{
						"id":       externalID,
						"object":   "model",
						"created":  time.Now().Unix(),
						"owned_by": owner,
					}
				}
			}
		}
	}

	modelList := []map[string]interface{}{}
	for _, m := range modelMap {
		modelList = append(modelList, m)
	}
	return modelList, nil
}

func (s *Service) proxyModels(w http.ResponseWriter, r *http.Request) {
	// 外部统一出口（/v1）匿名化 owned_by，不泄漏内部端点名；
	// 管理面板入口（/api/openai）保留端点归属用于按端点筛选模型。
	anonymize := !strings.HasPrefix(r.URL.Path, "/api/openai")
	modelList, err := s.GetModelsList(r.Context(), anonymize)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// 网关密钥白名单：不在白名单里的模型不暴露给该密钥。
	if keyIdentity := gatewayKeyFromContext(r.Context()); keyIdentity.ID != "" {
		modelList = filterModelsByKey(keyIdentity, modelList)
	}

	sort.Slice(modelList, func(i, j int) bool {
		idI, _ := modelList[i]["id"].(string)
		idJ, _ := modelList[j]["id"].(string)
		return idI < idJ
	})

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   modelList,
	})
}

// ==================== Helper methods ====================

func (s *Service) normalizeBaseURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(u, "/")

	stripSuffixes := []string{"/chat/completions", "/completions", "/models", "/embeddings"}
	for _, suffix := range stripSuffixes {
		if strings.HasSuffix(strings.ToLower(u), suffix) {
			u = u[:len(u)-len(suffix)]
			u = strings.TrimSuffix(u, "/")
		}
	}

	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		u = "https://" + u
	}

	// Append version path if missing
	hasVersion := false
	if reg := versionPathRegex; reg.MatchString(u) {
		hasVersion = true
	}
	if !hasVersion {
		u += "/v1"
	}

	return u
}

// verifyAPIKeyRaw 校验上游 API Key（GET /models）。endpointID 用于把 429 限流
// 累计到对应端点的代理池状态（辅助请求也参与 429 熔断，避免半死出口无人标记）。
func (s *Service) verifyAPIKeyRaw(ctx context.Context, u, key, endpointID string, pool []string, headers ...[]HeaderItem) (bool, int, error) {
	reqURL := fmt.Sprintf("%s/models", strings.TrimSuffix(u, "/"))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if len(headers) > 0 {
		applyCustomHeaders(req, headers[0])
	}

	client := s.client
	selectedProxy := ""
	if len(pool) > 0 {
		poolClient, proxy := s.auxClientForPool(endpointID, pool)
		if poolClient != nil {
			client = poolClient
			selectedProxy = proxy
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(endpointID, selectedProxy, retryAfterFromHeader(resp))
		}
		return false, 0, fmt.Errorf("verify failed: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, 0, err
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		if dataArr, ok := parsed["data"].([]interface{}); ok {
			return true, len(dataArr), nil
		}
	}

	var parsedArr []interface{}
	if err := json.Unmarshal(bodyBytes, &parsedArr); err == nil {
		return true, len(parsedArr), nil
	}

	return true, 0, nil
}

// listModelsRaw 拉取上游模型列表（GET /models）。endpointID 用于把 429 限流
// 累计到对应端点的代理池状态（见 verifyAPIKeyRaw）。
func (s *Service) listModelsRaw(ctx context.Context, u, key, endpointID string, pool []string, headers ...[]HeaderItem) ([]string, error) {
	reqURL := fmt.Sprintf("%s/models", strings.TrimSuffix(u, "/"))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if len(headers) > 0 {
		applyCustomHeaders(req, headers[0])
	}

	client := s.client
	selectedProxy := ""
	if len(pool) > 0 {
		poolClient, proxy := s.auxClientForPool(endpointID, pool)
		if poolClient != nil {
			client = poolClient
			selectedProxy = proxy
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(endpointID, selectedProxy, retryAfterFromHeader(resp))
		}
		return nil, fmt.Errorf("list models failed: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	models := []string{}
	var parsed map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &parsed); err == nil {
		// OpenAI structure
		if dataArr, ok := parsed["data"].([]interface{}); ok {
			for _, item := range dataArr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if id, ok := itemMap["id"].(string); ok {
						models = append(models, id)
					}
				}
			}
			return models, nil
		}
		// Custom models key
		if modelsArr, ok := parsed["models"].([]interface{}); ok {
			for _, item := range modelsArr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					if id, ok := itemMap["id"].(string); ok {
						models = append(models, id)
					} else if name, ok := itemMap["name"].(string); ok {
						models = append(models, name)
					}
				}
			}
			return models, nil
		}
	}

	var parsedArr []interface{}
	if err := json.Unmarshal(bodyBytes, &parsedArr); err == nil {
		for _, item := range parsedArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				if id, ok := itemMap["id"].(string); ok {
					models = append(models, id)
				}
			}
		}
		return models, nil
	}

	return nil, fmt.Errorf("unexpected models structure")
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
func healthCheckFastFailStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusBadRequest, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	return false
}

func (s *Service) healthCheckSingleModel(ctx context.Context, endpointID, baseURL, apiKey, model string, timeout time.Duration, pool []string, headers ...[]HeaderItem) HealthRecord {
	startTime := time.Now()
	record := HealthRecord{
		Model:     model,
		Status:    "failed",
		CheckedAt: startTime.Format(time.RFC3339),
	}

	reqURL := fmt.Sprintf("%s/chat/completions", strings.TrimSuffix(baseURL, "/"))

	// 端点配置了代理池时，健康检测与真实转发走同一出口（池内代理），
	// 避免检测请求从网关本机直连出口（出口 IP 不属于代理池）。
	client := s.client
	selectedProxy := ""
	if len(pool) > 0 {
		poolClient, proxy := s.auxClientForPool(endpointID, pool)
		if poolClient != nil {
			client = poolClient
			selectedProxy = proxy
		}
	}

	// 请求体模拟真实客户端常用字段，降低上游对缺字段请求的兼容性误判。
	payload := map[string]interface{}{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": "Reply with any short non-empty text."}},
		"stream":      false,
		"max_tokens":  1,
		"temperature": 0,
	}
	bodyBytes, _ := json.Marshal(payload)

	lastLatency := int64(0)
	var lastError string
	for attempt := 0; attempt < healthCheckAttempts; attempt++ {
		if attempt > 0 {
			// 重试前等待一小段退避，避免在限流窗口内反复撞墙。
			select {
			case <-ctx.Done():
				break
			case <-time.After(300 * time.Millisecond):
			}
		}

		childCtx, cancel := context.WithTimeout(ctx, timeout)
		httpReq, err := http.NewRequestWithContext(childCtx, "POST", reqURL, bytes.NewReader(bodyBytes))
		if err != nil {
			cancel()
			record.Error = err.Error()
			record.Latency = time.Since(startTime).Milliseconds()
			return record
		}
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json, text/event-stream")
		if len(headers) > 0 {
			applyCustomHeaders(httpReq, headers[0])
		}

		resp, err := client.Do(httpReq)
		if err != nil {
			cancel()
			lastLatency = time.Since(startTime).Milliseconds()
			if childCtx.Err() != nil {
				lastError = "超时"
			} else {
				lastError = err.Error()
			}
			continue
		}
		lastLatency = time.Since(startTime).Milliseconds()
		record.StatusCode = resp.StatusCode

		// 健康检测也累计上游限流：半死出口（已限死的 IP）在健康检查里反复
		// 429 时同样触发冻结，避免只有真实流量才能发现问题代理。
		if isRateLimitResponse(resp, nil) && selectedProxy != "" {
			s.markProxy429(endpointID, selectedProxy, retryAfterFromHeader(resp))
		}

		// 状态码优先：2xx 视为可用（仅校验显式 error 结构），
		// 4xx 确定性失败立即返回，其余（429/5xx）进入下一轮重试。
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// 2xx 响应只校验是否携带显式 error 结构（部分上游会返回 200 + {"error": ...}）。
			// 空响应体、SSE、纯文本等一律视为有效，避免误伤兼容端点。
			// 只读前 4KB 判定，不等待完整响应体（生成慢的模型等完整 body 会浪费数秒）。
			sniff, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			cancel()
			if errMsg := healthCheckBodyError(sniff); errMsg != "" {
				lastError = errMsg
				continue
			}
			record.Latency = lastLatency
			if time.Duration(lastLatency)*time.Millisecond <= degradedThreshold {
				record.Status = "operational"
			} else {
				record.Status = "degraded"
			}
			return record
		}

		resp.Body.Close()
		cancel()
		if healthCheckFastFailStatus(resp.StatusCode) {
			record.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
			record.Latency = lastLatency
			return record
		}
		lastError = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	record.Error = lastError
	record.Latency = lastLatency
	return record
}

// healthCheckBodyError 在 2xx 响应中探测明确的错误结构（如 {"error": {"message": "..."}}），
// 未发现错误时返回空字符串。兼容 JSON、SSE（data: 行）与纯文本响应。
func healthCheckBodyError(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "data:") {
		for _, line := range strings.Split(trimmed, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			if msg := healthCheckBodyError([]byte(payload)); msg != "" {
				return msg
			}
		}
		return ""
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return ""
	}
	switch errValue := parsed["error"].(type) {
	case map[string]interface{}:
		if message, ok := errValue["message"].(string); ok && strings.TrimSpace(message) != "" {
			return strings.TrimSpace(message)
		}
	case string:
		if strings.TrimSpace(errValue) != "" {
			return strings.TrimSpace(errValue)
		}
	}
	return ""
}

func (s *Service) runBatchHealthCheck(ctx context.Context, endpointID, baseURL, apiKey string, models []string, timeout time.Duration, concurrency int, pool []string, headers ...[]HeaderItem) HealthSummary {
	var mu sync.Mutex
	results := []HealthRecord{}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for _, model := range models {
		wg.Add(1)
		go func(m string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res := s.healthCheckSingleModel(ctx, endpointID, baseURL, apiKey, m, timeout, pool, headers...)

			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(model)
	}

	wg.Wait()

	return summarizeHealthResults(models, results)
}

func summarizeHealthResults(models []string, results []HealthRecord) HealthSummary {
	sort.Slice(results, func(i, j int) bool {
		return results[i].Model < results[j].Model
	})

	operationalCount := 0
	degradedCount := 0
	failedCount := 0

	for _, r := range results {
		switch r.Status {
		case "operational":
			operationalCount++
		case "degraded":
			degradedCount++
		default:
			failedCount++
		}
	}

	overall := "unknown"
	if len(results) > 0 {
		if failedCount == len(results) {
			overall = "failed"
		} else if operationalCount == len(results) {
			overall = "operational"
		} else {
			overall = "degraded"
		}
	}

	return HealthSummary{
		TotalModels:   len(models),
		Operational:   operationalCount,
		Degraded:      degradedCount,
		Failed:        failedCount,
		OverallStatus: overall,
		Results:       results,
		CheckedAt:     time.Now().Format(time.RFC3339),
	}
}

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

// Personas Handlers
func (s *Service) listPersonas(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, name, icon, system_prompt, is_default, created_at FROM openai_chat_personas ORDER BY is_default DESC, created_at DESC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Persona struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Icon         string `json:"icon"`
		SystemPrompt string `json:"system_prompt"`
		IsDefault    int    `json:"is_default"`
		CreatedAt    string `json:"created_at"`
	}

	var list []Persona
	for rows.Next() {
		var p Persona
		var icon sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &icon, &p.SystemPrompt, &p.IsDefault, &p.CreatedAt); err == nil {
			p.Icon = icon.String
			list = append(list, p)
		}
	}
	response.JSON(w, http.StatusOK, list)
}

func (s *Service) createPersona(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		Icon         string `json:"icon"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.Name == "" || body.SystemPrompt == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "name and system_prompt are required"})
		return
	}

	id := body.ID
	if id == "" {
		id = uuid.NewString()
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	createdAt := time.Now().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, "INSERT INTO openai_chat_personas (id, name, icon, system_prompt, is_default, created_at) VALUES (?, ?, ?, ?, 0, ?)",
		id, body.Name, body.Icon, body.SystemPrompt, createdAt)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (s *Service) updatePersona(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	var body struct {
		Name         string `json:"name"`
		Icon         string `json:"icon"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.Name == "" || body.SystemPrompt == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "name and system_prompt are required"})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "UPDATE openai_chat_personas SET name = ?, icon = ?, system_prompt = ? WHERE id = ?",
		body.Name, body.Icon, body.SystemPrompt, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deletePersona(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	if id == "1" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete default persona"})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_personas WHERE id = ?", id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Sessions Handlers
func (s *Service) listSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, title, model, endpoint_id, persona_id, system_prompt, created_at, updated_at FROM openai_chat_sessions ORDER BY updated_at DESC")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Session struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Model        string `json:"model"`
		EndpointID   string `json:"endpoint_id"`
		PersonaID    string `json:"persona_id"`
		SystemPrompt string `json:"system_prompt"`
		CreatedAt    string `json:"created_at"`
		UpdatedAt    string `json:"updated_at"`
	}

	var list []Session
	for rows.Next() {
		var s Session
		var model, epID, persID, sysPrompt sql.NullString
		if err := rows.Scan(&s.ID, &s.Title, &model, &epID, &persID, &sysPrompt, &s.CreatedAt, &s.UpdatedAt); err == nil {
			s.Model = model.String
			s.EndpointID = epID.String
			s.PersonaID = persID.String
			s.SystemPrompt = sysPrompt.String
			list = append(list, s)
		}
	}
	response.JSON(w, http.StatusOK, list)
}

func (s *Service) createSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var body struct {
		ID           string `json:"id"`
		Title        string `json:"title"`
		Model        string `json:"model"`
		EndpointID   string `json:"endpoint_id"`
		PersonaID    string `json:"persona_id"`
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	id := body.ID
	if id == "" {
		id = uuid.NewString()
	}

	title := body.Title
	if title == "" {
		title = "新对话"
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	now := time.Now().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, "INSERT INTO openai_chat_sessions (id, title, model, endpoint_id, persona_id, system_prompt, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, title, body.Model, body.EndpointID, body.PersonaID, body.SystemPrompt, now, now)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (s *Service) updateSession(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	var body struct {
		Title        *string `json:"title"`
		Model        *string `json:"model"`
		EndpointID   *string `json:"endpoint_id"`
		PersonaID    *string `json:"persona_id"`
		SystemPrompt *string `json:"system_prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	// Fetch current session values first
	var currentTitle, currentModel, currentEpID, currentPersID, currentSysPrompt string
	err = db.QueryRowContext(ctx, "SELECT title, model, endpoint_id, persona_id, system_prompt FROM openai_chat_sessions WHERE id = ?", id).
		Scan(&currentTitle, &currentModel, &currentEpID, &currentPersID, &currentSysPrompt)
	if err != nil {
		response.JSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	title := currentTitle
	if body.Title != nil {
		title = *body.Title
	}
	model := currentModel
	if body.Model != nil {
		model = *body.Model
	}
	epID := currentEpID
	if body.EndpointID != nil {
		epID = *body.EndpointID
	}
	persID := currentPersID
	if body.PersonaID != nil {
		persID = *body.PersonaID
	}
	sysPrompt := currentSysPrompt
	if body.SystemPrompt != nil {
		sysPrompt = *body.SystemPrompt
	}

	now := time.Now().Format(time.RFC3339)
	_, err = db.ExecContext(ctx, "UPDATE openai_chat_sessions SET title = ?, model = ?, endpoint_id = ?, persona_id = ?, system_prompt = ?, updated_at = ? WHERE id = ?",
		title, model, epID, persID, sysPrompt, now, id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) deleteSession(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_sessions WHERE id = ?", id)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) clearSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_sessions")
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Messages Handlers
func (s *Service) listSessionMessages(w http.ResponseWriter, r *http.Request, sessionId string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, "SELECT id, role, content, reasoning, timestamp FROM openai_chat_messages WHERE session_id = ? ORDER BY timestamp ASC", sessionId)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	type Message struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		Content   string `json:"content"`
		Reasoning string `json:"reasoning,omitempty"`
		Timestamp string `json:"timestamp"`
	}

	var list []Message
	for rows.Next() {
		var m Message
		var reasoning sql.NullString
		if err := rows.Scan(&m.ID, &m.Role, &m.Content, &reasoning, &m.Timestamp); err == nil {
			m.Reasoning = reasoning.String
			list = append(list, m)
		}
	}
	response.JSON(w, http.StatusOK, list)
}

func (s *Service) createSessionMessage(w http.ResponseWriter, r *http.Request, sessionId string) {
	ctx := r.Context()
	var body struct {
		ID        string `json:"id"`
		Role      string `json:"role"`
		Content   string `json:"content"`
		Reasoning string `json:"reasoning"`
		Timestamp string `json:"timestamp"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if body.Role == "" || body.Content == "" {
		response.JSON(w, http.StatusBadRequest, map[string]string{"error": "role and content are required"})
		return
	}

	id := body.ID
	if id == "" {
		id = uuid.NewString()
	}

	timestamp := body.Timestamp
	if timestamp == "" {
		timestamp = time.Now().Format(time.RFC3339)
	}

	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	// Insert message
	_, err = db.ExecContext(ctx, "INSERT INTO openai_chat_messages (id, session_id, role, content, reasoning, timestamp) VALUES (?, ?, ?, ?, ?, ?)",
		id, sessionId, body.Role, body.Content, body.Reasoning, timestamp)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Update session updated_at timestamp
	now := time.Now().Format(time.RFC3339)
	_, _ = db.ExecContext(ctx, "UPDATE openai_chat_sessions SET updated_at = ? WHERE id = ?", now, sessionId)

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "id": id})
}

func (s *Service) deleteSessionMessage(w http.ResponseWriter, r *http.Request, sessionId, msgId string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_messages WHERE session_id = ? AND id = ?", sessionId, msgId)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Service) clearSessionMessages(w http.ResponseWriter, r *http.Request, sessionId string) {
	ctx := r.Context()
	db, err := s.open(ctx)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, "DELETE FROM openai_chat_messages WHERE session_id = ?", sessionId)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
