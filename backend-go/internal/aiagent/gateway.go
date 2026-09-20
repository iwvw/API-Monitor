package aiagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	streamTokenTTL = 30 * time.Second
	// streamTokenMaxGrants 限制未消费的一次性凭证上限，避免异常流量下内存无界增长。
	streamTokenMaxGrants = 4096
	// streamTokenMaxPerInstance 限制单个实例的未消费凭证数量，防单实例刷爆映射。
	streamTokenMaxPerInstance = 64
	// gatewayBodyLimit 是网关请求体上限。AI Agent 请求多为小 JSON，
	// 8MB 已足够宽松，避免并发流各自长期持有大请求体放大内存占用。
	gatewayBodyLimit = 8 << 20
	// gatewayStreamIdleTimeout 是流式响应的读空闲上限：SSE 应持续输出，
	// 长时间无数据视为上游或客户端卡死，释放并发槽位。
	gatewayStreamIdleTimeout = 10 * time.Minute
	// gatewayStreamWriteTimeout 是单次写出的上限：慢客户端不读取导致的写阻塞会被此截止时间打断。
	gatewayStreamWriteTimeout = 2 * time.Minute
)

// AgentHTTPRequest 是网关要转发给主机 Agent 的一次 HTTP 请求。
type AgentHTTPRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// AgentHTTPResponse 是主机 Agent 返回的响应。Body 必须是流式读取的，
// 调用方负责 Close；SSE 场景下读到一块就应写出一块。
type AgentHTTPResponse struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
}

// AgentRuntime 是 aiagent 与主机 Agent 之间的抽象。实现放在 server 层做适配，
// 避免本包直接依赖 serveragent 的连接注册表。
type AgentRuntime interface {
	// AgentOnline 报告主机 Agent 控制连接是否在线。
	AgentOnline(serverID string) bool
	// AgentSupportsAIAgentStream 报告主机 Agent 是否支持原生流数据通道能力。
	AgentSupportsAIAgentStream(serverID string) bool
	// AgentSupportsLifecycle 报告主机 Agent 是否支持进程生命周期能力（ADR-0006）。
	AgentSupportsLifecycle(serverID string) bool
	// Probe 让主机 Agent 检测目标 Provider 的进程是否运行、端口是否监听。
	Probe(ctx context.Context, serverID, provider string, port int, processMatch []string) (ProbeResult, error)
	// StartProcess 让主机 Agent 按 Provider 模板启动本地服务（任务 57）。
	StartProcess(ctx context.Context, serverID string, payload LifecycleStartPayload) (LifecycleResult, error)
	// StopProcess 让主机 Agent 停止该实例的托管进程（任务 58）。
	StopProcess(ctx context.Context, serverID, instanceID string) (LifecycleResult, error)
	// ProcessStatus 查询该实例的托管进程状态（任务 59）。
	ProcessStatus(ctx context.Context, serverID, instanceID string) (LifecycleResult, error)
	// Diagnose 让主机 Agent 诊断某 Provider 在主机侧的可用性（任务 60）：
	// 可执行文件是否就绪、端口区间占用与建议空闲端口。
	Diagnose(ctx context.Context, serverID, provider string) (DiagnoseResult, error)
	// RoundTrip 经 Agent 数据通道完成一次 HTTP 往返，响应体为流式。
	RoundTrip(ctx context.Context, serverID string, port int, req AgentHTTPRequest) (AgentHTTPResponse, error)
}

// LifecycleStartPayload 是启动托管进程的载荷。云端只能传 Provider ID 与端口，
// 不能传可执行路径或任意参数（ADR-0006 第 6 条）。
type LifecycleStartPayload struct {
	InstanceID string `json:"instance_id"`
	Provider   string `json:"provider"`
	Port       int    `json:"port"`
}

// LifecycleResult 是主机 Agent 返回的进程生命周期结果。
type LifecycleResult struct {
	Managed bool `json:"managed"`
	Running bool `json:"running"`
	PID     int  `json:"pid,omitempty"`
	// DesiredRunning 是 Agent 侧记录的期望运行标记。
	DesiredRunning bool `json:"desiredRunning"`
	// Crashed 为真表示重启次数用尽，进入终态。
	Crashed bool `json:"crashed"`
	// PortListening / ListenerPID / ListenerMatchesProcess 让托管实例
	// 无需额外探测即可完成端口关联验证。
	PortListening          bool    `json:"portListening"`
	ListenerPID            int     `json:"listenerPid,omitempty"`
	ListenerMatchesProcess bool    `json:"listenerMatchesProcess"`
	UptimeSeconds          int     `json:"uptimeSeconds,omitempty"`
	Restarts               int     `json:"restarts,omitempty"`
	MemoryBytes            uint64  `json:"memoryBytes,omitempty"`
	CPUPercent             float32 `json:"cpuPercent,omitempty"`
}

type streamGrant struct {
	token      string
	userID     string
	instanceID string
	expiresAt  time.Time
	consumed   bool
}

// streamTokenBroker 管理一次性短令牌：用于 EventSource / WebSocket 这类无法
// 自定义请求头的场景，避免长期令牌出现在 URL 与访问日志中。
type streamTokenBroker struct {
	mu     sync.Mutex
	grants map[string]*streamGrant
}

func newStreamTokenBroker() *streamTokenBroker {
	return &streamTokenBroker{grants: make(map[string]*streamGrant)}
}

func (b *streamTokenBroker) issue(userID, instanceID string) string {
	token := randomStreamToken()
	if token == "" {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupLocked(time.Now())
	// 单实例未消费凭证上限：防止弱实例无限签发占满映射。
	if countGrantsForInstance(b.grants, instanceID) >= streamTokenMaxPerInstance {
		evictOldestForInstance(b.grants, instanceID)
	}
	// 全局上限：先尝试淘汰同实例的旧凭证（不挤掉他人）；若本实例没有可淘汰项，
	// 再淘汰全局最旧的一条，保证映射规模确实有界。
	if len(b.grants) >= streamTokenMaxGrants {
		if !evictOldestForInstance(b.grants, instanceID) {
			evictGloballyOldest(b.grants)
		}
	}
	b.grants[token] = &streamGrant{
		token:      token,
		userID:     userID,
		instanceID: instanceID,
		expiresAt:  time.Now().Add(streamTokenTTL),
	}
	return token
}

func countGrantsForInstance(grants map[string]*streamGrant, instanceID string) int {
	count := 0
	for _, grant := range grants {
		if grant.instanceID == instanceID {
			count++
		}
	}
	return count
}

// evictOldestForInstance 淘汰指定实例中最早签发、尚未消费的一条凭证。
// 返回是否真的淘汰了条目。
func evictOldestForInstance(grants map[string]*streamGrant, instanceID string) bool {
	var oldest *streamGrant
	for _, grant := range grants {
		if grant.instanceID != instanceID {
			continue
		}
		if oldest == nil || grant.expiresAt.Before(oldest.expiresAt) {
			oldest = grant
		}
	}
	if oldest == nil {
		return false
	}
	delete(grants, oldest.token)
	return true
}

// evictGloballyOldest 淘汰全局最早签发的一条凭证，作为全局上限的兜底。
func evictGloballyOldest(grants map[string]*streamGrant) {
	var oldest *streamGrant
	for _, grant := range grants {
		if oldest == nil || grant.expiresAt.Before(oldest.expiresAt) {
			oldest = grant
		}
	}
	if oldest != nil {
		delete(grants, oldest.token)
	}
}

func (b *streamTokenBroker) consume(token, instanceID string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupLocked(time.Now())
	grant, ok := b.grants[token]
	if !ok || grant.consumed || grant.instanceID != instanceID || time.Now().After(grant.expiresAt) {
		return "", false
	}
	grant.consumed = true
	delete(b.grants, token)
	return grant.userID, true
}

// peek 校验短令牌但不消费：用于「先确认实例可达再消费」的顺序，避免中途失败
// 白白烧掉一次性凭证（EventSource 重连时会重新换取，但少一次失败更友好）。
func (b *streamTokenBroker) peek(token, instanceID string) (string, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cleanupLocked(time.Now())
	grant, ok := b.grants[token]
	if !ok || grant.consumed || grant.instanceID != instanceID || time.Now().After(grant.expiresAt) {
		return "", false
	}
	return grant.userID, true
}

func (b *streamTokenBroker) cleanupLocked(now time.Time) {
	for token, grant := range b.grants {
		if now.After(grant.expiresAt) {
			delete(b.grants, token)
		}
	}
}

func randomStreamToken() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		// 随机源失败时返回空串：调用方据此拒绝签发，而不是退化成一个可预测的弱令牌。
		return ""
	}
	return hex.EncodeToString(buf)
}

// hopByHopHeaders 是转发时必须过滤的逐跳头部。
var hopByHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"host":                true,
}

func filterRequestHeaders(source http.Header) http.Header {
	filtered := make(http.Header, len(source))
	for key, values := range source {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		filtered[key] = append([]string(nil), values...)
	}
	// 网关不向目标 Agent 透传云端口令与浏览器凭据（面板 session cookie 不应
	// 出现在用户登记的外部进程上）。
	filtered.Del("Authorization")
	filtered.Del("Cookie")
	// 网关是真反向代理：剥离客户端伪造的转发/来源头，避免上游基于 IP 的鉴权或
	// 限流被伪造内容欺骗。
	for _, header := range []string{
		"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-Port",
		"X-Real-IP", "CF-Connecting-IP", "True-Client-IP", "X-Original-URL",
	} {
		filtered.Del(header)
	}
	return filtered
}

func filterResponseHeaders(source http.Header) http.Header {
	filtered := make(http.Header, len(source))
	for key, values := range source {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		filtered[key] = append([]string(nil), values...)
	}
	// 面板自身的安全中间件会按白名单设置 CORS 头。上游（本机 Agent 服务）也带
	// 自己的 CORS 头时，两者叠加会产生重复值（ACAO 出现两个值），浏览器会直接
	// 拒绝该响应。网关对浏览器而言是同源反向代理，上游的 CORS 头没有意义，剥离。
	for _, header := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Credentials",
		"Access-Control-Allow-Methods",
		"Access-Control-Allow-Headers",
		"Access-Control-Expose-Headers",
		"Access-Control-Max-Age",
	} {
		filtered.Del(header)
	}
	return filtered
}

// buildTargetPath 按 Provider 的 BasePath 组装目标路径。
func buildTargetPath(provider Provider, rest string) string {
	rest = strings.TrimPrefix(rest, "/")
	base := strings.Trim(provider.BasePath, "/")
	switch {
	case base == "":
		return "/" + rest
	case rest == "":
		return "/" + base
	default:
		return "/" + base + "/" + rest
	}
}

// handleGateway 处理 ANY /api/aiagent/gw/{instanceId}/{rest...}。
func (s *Service) handleGateway(w http.ResponseWriter, r *http.Request, instanceID, rest string) {
	if s.runtime == nil {
		s.writeGatewayError(w, http.StatusServiceUnavailable, CodeChannelError, "agent runtime not configured")
		return
	}
	auth, shortToken, code, message := s.resolveGatewayAuth(w, r, instanceID)
	if message != "" {
		// 鉴权失败（含反复尝试一次性令牌）是审计要点，记 denied 后再返回。
		s.logGatewayDenied(r, instanceID, auth, http.StatusText(code), message)
		s.writeGatewayError(w, code, errorCodeForStatus(code), message)
		return
	}
	// 并发闸门在鉴权通过后获取：否则未鉴权的请求也能占满槽位（拒绝路径还要写审计、
	// 串行等唯一的 SQLite 连接），从而瞬时限流到正常租户。
	select {
	case s.gatewaySlots <- struct{}{}:
		defer func() { <-s.gatewaySlots }()
	case <-r.Context().Done():
		return
	default:
		s.writeGatewayError(w, http.StatusTooManyRequests, CodeRateLimited, "too many concurrent gateway streams")
		return
	}
	db, err := s.store.Open(r.Context())
	if err != nil {
		s.writeGatewayError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
		return
	}

	instance, err := s.getInstance(r.Context(), db, instanceID)
	if err != nil {
		db.Close()
		s.logGatewayDenied(r, instanceID, auth, "not_found", "instance not found")
		s.writeGatewayError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return
	}
	if !auth.IsAdmin {
		granted, grantErr := s.instanceGrantedTo(r.Context(), db, instance.ID, auth.UserID)
		if grantErr != nil {
			db.Close()
			s.logGatewayDenied(r, instanceID, auth, "forbidden", "grant lookup failed")
			s.writeGatewayError(w, http.StatusInternalServerError, CodeChannelError, "database unavailable")
			return
		}
		if !granted {
			db.Close()
			// 统一按「不存在」返回，避免暴露实例存在性/归属/启用状态。
			s.logGatewayDenied(r, instanceID, auth, "forbidden", "instance not granted to caller")
			s.writeGatewayError(w, http.StatusNotFound, CodeNotFound, "instance not found")
			return
		}
	}
	if !instance.Enabled {
		db.Close()
		s.logGatewayDenied(r, instanceID, auth, "forbidden", "instance disabled")
		s.writeGatewayError(w, http.StatusNotFound, CodeNotFound, "instance not found")
		return
	}
	// 实例信息已取到，立即归还唯一的 SQLite 连接：后面的流式转发可能持续很久，
	// 若在此处持有连接（连接池上限为 1）会拖垮所有并发会话。
	db.Close()

	provider, ok := LookupProvider(instance.Provider)
	if !ok {
		s.logGatewayDenied(r, instanceID, auth, "invalid", "unknown provider")
		s.writeGatewayError(w, http.StatusBadRequest, CodeInvalid, "unknown provider")
		return
	}
	if !s.runtime.AgentOnline(instance.ServerID) {
		s.logGatewayDenied(r, instanceID, auth, "host_offline", "host agent offline")
		s.writeGatewayError(w, http.StatusConflict, CodeHostOffline, "host agent offline")
		return
	}
	// 旧版 Agent 不支持数据通道：直接返回明确错误，避免每个请求白等 12 秒后再 502。
	if !s.runtime.AgentSupportsAIAgentStream(instance.ServerID) {
		s.logGatewayDenied(r, instanceID, auth, "agent_outdated", "agent does not support the stream channel")
		s.writeGatewayError(w, http.StatusServiceUnavailable, CodeAgentOutdated,
			"host agent does not support the native stream channel; please upgrade the agent")
		return
	}

	body, err := readLimitedBody(r)
	if err != nil {
		s.logGatewayDenied(r, instanceID, auth, "invalid", err.Error())
		s.writeGatewayError(w, http.StatusRequestEntityTooLarge, CodeInvalid, err.Error())
		return
	}

	// 短令牌在这里才真正消费：实例/归属/启用/在线/能力校验与请求体读取全部通过后，
	// 才把一次性凭证用掉，避免这些可预期的失败白白烧掉它（客户端无需重新换取）。
	if shortToken != "" {
		// 与 Bearer 路径一致：复核归属用户当前是否可用，避免用户刚被禁用/删除后
		// 仍能用未过期的短令牌继续访问（最长 30 秒窗口）。
		if err := s.assertUserActive(r.Context(), auth.UserID); err != nil {
			s.logGatewayDenied(r, instanceID, auth, "user_inactive", "user disabled or removed")
			s.writeGatewayError(w, http.StatusForbidden, CodeForbidden, "account disabled")
			return
		}
		if _, consumed := s.streamTokens.consume(shortToken, instanceID); !consumed {
			s.logGatewayDenied(r, instanceID, auth, "unauthorized", "invalid or expired stream token")
			s.writeGatewayError(w, http.StatusUnauthorized, CodeUnauthorized, "invalid or expired stream token")
			return
		}
	}

	req := AgentHTTPRequest{
		Method: r.Method,
		Path:   buildTargetPath(provider, rest),
		Header: filterRequestHeaders(r.Header),
		Body:   body,
	}
	// 透传查询参数（去掉网关自用的一次性令牌 st），否则依赖 query 的 Agent 接口会失效。
	if values := r.URL.Query(); len(values) > 0 {
		values.Del("st")
		if encoded := values.Encode(); encoded != "" {
			req.Path += "?" + encoded
		}
	}

	// 只做取消传播，不设绝对超时：长会话（含等待用户输入的 SSE）可能远超任何
	// 固定墙钟上限。连接/响应头阶段由 runtime 的 ResponseHeaderTimeout 兜底，
	// 流式阶段依赖客户端断开或上游关闭来结束。
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	started := time.Now()
	resp, err := s.runtime.RoundTrip(ctx, instance.ServerID, instance.Port, req)
	if err != nil {
		s.writeAccessLog(r.Context(), AccessLog{
			UserID:     auth.UserID,
			TokenID:    auth.TokenID,
			InstanceID: instance.ID,
			Action:     "gw",
			Result:     "error",
			Error:      err.Error(),
			IP:         s.clientIP(r),
			UserAgent:  r.UserAgent(),
		})
		// 面向客户端返回通用错误，细节只留在服务端访问日志，避免泄漏内部信息。
		s.writeGatewayError(w, http.StatusBadGateway, CodeChannelError, "upstream agent request failed")
		return
	}
	defer resp.Body.Close()

	for key, values := range filterResponseHeaders(resp.Header) {
		// 不把目标 Agent 的 Set-Cookie 透传到面板域，避免被外部进程在面板域名下种 Cookie。
		if strings.EqualFold(key, "Set-Cookie") {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Del("Content-Length")
	w.WriteHeader(resp.StatusCode)

	bytesWritten, copyErr := copyStream(r.Context(), w, resp.Body, func() { _ = resp.Body.Close() })
	// 审计写入由 writeAccessLog 内部脱离请求上下文，客户端断开也能落地。
	s.writeAccessLog(r.Context(), AccessLog{
		UserID:     auth.UserID,
		TokenID:    auth.TokenID,
		InstanceID: instance.ID,
		Action:     fmt.Sprintf("gw %s %s (%s, %d bytes)", sanitizeLogToken(r.Method), sanitizeLogToken(req.Path), time.Since(started).Round(time.Millisecond), bytesWritten),
		Result:     resultFor(resp.StatusCode, copyErr),
		StatusCode: resp.StatusCode,
		Error:      errorText(copyErr),
		IP:         s.clientIP(r),
		UserAgent:  r.UserAgent(),
	})
}

// streamChunk 是常驻读取 goroutine 向上游传递的一块数据或错误。
type streamChunk struct {
	data []byte
	err  error
}

// copyStream 逐块拷贝并在每次写入后 flush，保证 SSE 不被整体缓冲。
// 受请求上下文与「读空闲/写截止」约束：客户端断开、长期不读取导致写阻塞、
// 或上游长时间无数据都会中止，避免卡死的长连接一直占用并发槽位。
// 使用一个常驻读取 goroutine（而非每次 Read 起一个），超时/取消时通过
// closeSource 关闭上游响应体，使阻塞中的 Read 立即返回。
func copyStream(ctx context.Context, w http.ResponseWriter, source io.Reader, closeSource func()) (int64, error) {
	flusher, _ := w.(http.Flusher)
	chunks := make(chan streamChunk)
	go func() {
		defer close(chunks)
		buf := make([]byte, 32*1024)
		for {
			read, readErr := source.Read(buf)
			if read > 0 {
				// 必须拷贝：无缓冲 channel 只保证「接收方收到」，不保证它已写完。
				// 直接发送 buf[:read] 会让读取方在下一轮 Read 覆盖这块内存，而主循环
				// 可能仍在 Write 同一片数据，导致响应体错位（大响应 gzip 流损坏，
				// 浏览器报 ERR_CONTENT_DECODING_FAILED）。
				chunk := make([]byte, read)
				copy(chunk, buf[:read])
				select {
				case chunks <- streamChunk{data: chunk}:
				case <-ctx.Done():
					return
				}
				continue
			}
			if readErr != nil {
				select {
				case chunks <- streamChunk{err: readErr}:
				case <-ctx.Done():
				}
				return
			}
		}
	}()

	stop := func() {
		if closeSource != nil {
			closeSource()
		}
	}

	var total int64
	idle := time.NewTimer(gatewayStreamIdleTimeout)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			stop()
			return total, ctx.Err()
		case <-idle.C:
			// 读空闲上限：SSE 正常会持续输出，长时间无数据视为对端卡死。
			stop()
			return total, fmt.Errorf("upstream stream idle for %s", gatewayStreamIdleTimeout)
		case chunk, ok := <-chunks:
			if !ok {
				return total, nil
			}
			// 收到数据即重置空闲窗口。
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(gatewayStreamIdleTimeout)
			if chunk.err != nil {
				if chunk.err == io.EOF {
					return total, nil
				}
				return total, chunk.err
			}
			// 写出受写截止时间约束：慢客户端不读取会造成 TCP 背压，令 Write 永久阻塞，
			// ctx 取消也无法打断阻塞中的 Write。用连接写截止时间兜底（HTTP/2/httptest
			// 等不支持时返回 ErrNotSupported，视为可继续写）。
			if err := http.NewResponseController(w).SetWriteDeadline(time.Now().Add(gatewayStreamWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
				stop()
				return total, err
			}
			written, writeErr := w.Write(chunk.data)
			total += int64(written)
			if flusher != nil {
				flusher.Flush()
			}
			if writeErr != nil {
				stop()
				return total, writeErr
			}
		}
	}
}

func resultFor(status int, err error) string {
	if err != nil {
		return "error"
	}
	if status >= 400 {
		return "error"
	}
	return "ok"
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func readLimitedBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	limited := io.LimitReader(r.Body, gatewayBodyLimit+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > gatewayBodyLimit {
		return nil, fmt.Errorf("request body exceeds %d bytes", gatewayBodyLimit)
	}
	return body, nil
}

// resolveGatewayAuth 解析网关鉴权：优先 Bearer 长期令牌，其次一次性短令牌。
// 短令牌此处只做 peek（不消费），由调用方在实例校验通过后再最终消费。
func (s *Service) resolveGatewayAuth(w http.ResponseWriter, r *http.Request, instanceID string) (authContext, string, int, string) {
	if bearer := bearerToken(r); bearer != "" {
		db, err := s.store.Open(r.Context())
		if err != nil {
			return authContext{}, "", http.StatusInternalServerError, "database unavailable"
		}
		defer db.Close()
		auth, authErr := s.authenticateToken(r.Context(), db, bearer, s.clientIP(r))
		if authErr != nil {
			// 对外只返回通用错误，避免暴露内部哨兵错误（过期/吊销/禁用等）的差异。
			return authContext{}, "", http.StatusUnauthorized, "invalid or expired token"
		}
		return auth, "", 0, ""
	}
	short := strings.TrimSpace(r.URL.Query().Get("st"))
	if short == "" {
		return authContext{}, "", http.StatusUnauthorized, "missing credential"
	}
	userID, ok := s.streamTokens.peek(short, instanceID)
	if !ok {
		return authContext{}, "", http.StatusUnauthorized, "invalid or expired stream token"
	}
	return authContext{UserID: userID}, short, 0, ""
}

// sanitizeLogToken 去掉日志字段中的控制字符/换行，避免审计日志注入。
func sanitizeLogToken(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r < 0x20 {
			return -1
		}
		return r
	}, value)
}

func (s *Service) writeGatewayError(w http.ResponseWriter, status int, code, message string) {
	writeError(w, status, code, message)
}

// logGatewayDenied 记录网关在转发前拒绝的请求，便于审计反复的令牌尝试与跨租户探测。
func (s *Service) logGatewayDenied(r *http.Request, instanceID string, auth authContext, reason, detail string) {
	s.writeAccessLog(r.Context(), AccessLog{
		UserID:     auth.UserID,
		TokenID:    auth.TokenID,
		InstanceID: instanceID,
		Action:     fmt.Sprintf("gw %s denied (%s)", sanitizeLogToken(r.Method), sanitizeLogToken(reason)),
		Result:     "denied",
		Error:      sanitizeLogToken(detail),
		IP:         s.clientIP(r),
		UserAgent:  r.UserAgent(),
	})
}

func errorCodeForStatus(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeConflict
	}
	if status >= 500 {
		return CodeChannelError
	}
	return CodeInvalid
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return ""
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
