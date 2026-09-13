package openai

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
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

func newEndpointKeyState() *endpointKeyState {
	return &endpointKeyState{}
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

// resolveEndpointModel 返回请求模型在该端点上实际使用的内部模型名，ok 表示该
// 端点当前可路由此请求。多个内部模型映射到同一外部名时优先返回未被
// disabled_models 禁用的别名，避免随机命中被禁用映射导致端点被整体过滤；
// 命中映射但全部被禁用时不可路由；无映射命中时按请求名本身判定禁用。
func (s *Service) updateEndpoint(w http.ResponseWriter, r *http.Request, id string) {
	var req struct {
		Name                      string        `json:"name"`
		BaseURL                   string        `json:"baseUrl"`
		ModelsURL                 *string       `json:"modelsUrl"`
		APIKey                    *string       `json:"apiKey"`
		APIKeys                   []string      `json:"apiKeys"`
		Notes                     string        `json:"notes"`
		Headers                   *[]HeaderItem `json:"headers"`
		ProxyPool                 *[]string     `json:"proxyPool"`
		ProxyBatches              *[]ProxyBatch `json:"proxyBatches"`
		AutoSwitch                *bool         `json:"autoSwitch"`
		ProxyEnabled              *bool         `json:"proxyEnabled"`
		ForceProxy                *bool         `json:"forceProxy"`
		RateLimitRetryEnabled     *bool         `json:"rateLimitRetryEnabled"`
		RateLimitRetryWaitSeconds *int          `json:"rateLimitRetryWaitSeconds"`
		KeyRetryRounds            *int          `json:"keyRetryRounds"`
		Protocol                  *string       `json:"protocol"`
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

// healthCheckAttempts 是单个模型健康检测的最大尝试轮数。
// 上游限流（429/5xx/超时）波动大，多轮中任一次成功即视为可用，
// 避免一次抖动就把可用模型误判为失败。
const healthCheckAttempts = 2
