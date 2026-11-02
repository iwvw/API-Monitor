package openai

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// warmupInterval 是代理池预热的保活周期。
const warmupInterval = 10 * time.Minute

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
