package openai

import (
	"context"
	"crypto/rand"
	"database/sql"
	"math/big"
	"net"
	"net/http"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/apikeys"
	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
)

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

// SetNotifier 注入告警通知器（由 server 组装时调用）。notifier 为 nil 时告警监测静默跳过。
func (s *Service) SetNotifier(notifier Notifier) {
	s.notifier = notifier
}

// SetProxyPoolSelector 注入独立代理池选择器（server 组装时调用）。
func (s *Service) SetProxyPoolSelector(sel ProxyPoolSelector) {
	s.externalPool = sel
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
