package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// upstreamBodyLimit 是非流式上游响应体的读取上限：正常 completion 响应远小于
// 该值，超过即视为异常上游（或被劫持的代理）在倾倒超大 body，必须报错拒绝
// 而非静默截断/全量读入，防止内存尖峰打爆网关。
// 用 var 而非 const 以便测试注入更小的值。
var upstreamBodyLimit int64 = 64 << 20

// readUpstreamBodyLimited 读取上游响应体并施加 upstreamBodyLimit 硬上限：
// 超限返回错误（不返回截断数据），调用方据此向客户端回 502。
func readUpstreamBodyLimited(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, upstreamBodyLimit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > upstreamBodyLimit {
		return nil, fmt.Errorf("上游响应体超过 %d MB 上限", upstreamBodyLimit>>20)
	}
	return body, nil
}

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

// rateLimitRetryEnabledAny 判断候选端点中是否有任一开启了「429 等待重试」。
// 任一开启即允许整个请求进入等待重试（预算按全请求计）。
func rateLimitRetryEnabledAny(candidates []Endpoint) bool {
	for i := range candidates {
		if candidates[i].RateLimitRetryEnabled {
			return true
		}
	}
	return false
}

// rateLimitRetryWaitFor 计算一次 429 等待重试应等待的时长：
//   - 优先采用 Google 标准错误 body 的 RetryInfo/ErrorInfo 延迟（Vertex/Gemini 429
//     配额窗口通常只写在 body 里，Retry-After 头常缺失）；
//   - 其次采用最近响应中的 Retry-After 头（配额恢复窗口）；
//   - 无头时采用端点配置缺省秒数（取候选中最短非零配置，避免最慢端点拖住预算）；
//   - 一律钳制到剩余预算内；预算耗尽返回 0（直接收尾不再等待）。
func rateLimitRetryWaitFor(res *relayLoopResult, candidates []Endpoint, budget time.Duration) time.Duration {
	wait := time.Duration(0)
	if res != nil {
		if gw := googleRetryAfterFromBody(res.retryBody); gw != nil {
			wait = *gw
		} else if res.resp != nil {
			if ra := retryAfterFromHeader(res.resp); ra != nil && *ra > 0 {
				wait = *ra
			}
		}
	}
	if wait <= 0 {
		configSeconds := 0
		for i := range candidates {
			if candidates[i].RateLimitRetryEnabled && candidates[i].RateLimitRetryWaitSeconds > 0 {
				if configSeconds == 0 || candidates[i].RateLimitRetryWaitSeconds < configSeconds {
					configSeconds = candidates[i].RateLimitRetryWaitSeconds
				}
			}
		}
		if configSeconds > 0 {
			wait = time.Duration(configSeconds) * time.Second
		}
	}
	if budget <= 0 || wait <= 0 {
		return 0
	}
	if wait > budget {
		wait = budget
	}
	return wait
}

// waitForRateLimitRetry 在 429 等待重试期间等待 wait 时长（ctx 可取消）。
// 返回 false 表示客户端已断开或预算中断，调用方应停止重试。
func waitForRateLimitRetry(ctx context.Context, wait time.Duration) bool {
	if wait <= 0 {
		return true
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Service) relayLoop(p relayLoopParams) *relayLoopResult {
	res := &relayLoopResult{
		statusCode:   http.StatusBadGateway,
		realModel:    p.realModel,
		egressIP:     s.egressOutbound(),
		startTime:    time.Now(),
		stepKeyIndex: -1,
	}
	ctx := p.ctx
	selected := p.selected
	stream := p.stream

	// 代理池选择 + 限流自动切换：最多尝试 len(pool) 个代理。
	// 代理开关未开启或池为空时只尝试一次（重试只是对同一链路的重复请求，
	// 首字超时重发反而放大慢响应，见 effectiveProxyAttempts）。
	maxProxyAttempts := effectiveProxyAttempts(selected)
	// 多 key 时保证每个 key 至少有 keyRetryRounds 次尝试机会（默认 2 = 循环两遍，
	// 覆盖 401 冻结后自动切 key 再试的场景）。单 key 端点不放大（重试同 key 无意义）。
	// 预算比 keyCount*keyMaxTries 多 1：预留一个「耗尽检测位」，让全部 key 达到上限后
	// 的下一轮 pickKey 有机会返回 ("", -1) 触发端点级切换，而不是因预算耗尽静默退出
	// （后者会把最后一跳已关闭的响应体当最终结果透传，产生 502 而不是干净的耗尽错误）。
	keyMaxTries := 1
	if keyCount := len(cleanKeyList(selected.AllKeys())); keyCount > 1 {
		keyMaxTries = selected.effectiveKeyRetryRounds()
		if need := keyCount*keyMaxTries + 1; maxProxyAttempts < need {
			maxProxyAttempts = need
		}
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
	// triedKeys 记录本轮请求内每个 key 已尝试失败的次数（key 永不冻结，仅请求内计数），
	// 未达到 keyMaxTries 的 key 仍会被 pickKey 选中重试，避免单 key 场景 401 后对同一 key 无限重试。
	triedKeys := map[string]int{}
	// 不同出口 IP 的 429 计数：达到 proxyRateLimitPicks 视为上游限流已扩散到
	// 整池，提前收尾（单 IP 被限时组冻结已让候选自动跳到其他 IP，不在此计数）。
	observed429IPs := map[string]bool{}
	bump429 := func(proxy string) bool {
		// 优先用探测到的出口公网 IP 区分；未探测（冷启动）时退化为按 slot 计。
		key := s.proxyExitIPOf(selected.ID, proxy)
		if key == "" {
			key = proxy
		}
		observed429IPs[key] = true
		return len(observed429IPs) >= proxyRateLimitPicks
	}
	// triedProxies 记录本轮请求内已尝试过（含 429）的代理，随机换出口时绝不会
	// 重复抽到已试出口；retryProxy 由 429 分支随机选定后强制作为下一跳。
	triedProxies := map[string]bool{}
	retryProxy := ""

	for attempt = 0; attempt < maxProxyAttempts; attempt++ {
		// 客户端已断开（ctx 取消/超时）：立即结束尝试循环，不再发起新的
		// 网络连接。无显式检查时，连接失败路径虽也会因 attemptCtx 取消而快速
		// 返回，但在 clientForEndpoint 选择阶段仍可能空转；这里在每轮最前面
		// 提前终止，杜绝客户端断开后的无效重试（对应网关 502 的常见成因）。
		if err := ctx.Err(); err != nil {
			if lastErr == nil {
				lastErr = err
			}
			if errors.Is(err, context.Canceled) {
				res.clientCancelled = true
			}
			res.attempt = attempt
			break
		}
		attemptCtx, cancel := context.WithCancel(ctx)
		res.cancel = cancel
		var client *http.Client
		var currentProxy string
		var clientErr error
		if retryProxy != "" {
			// 429 后的随机换出口：强制使用随机选定的下一跳，不再走择优（已试出口
			// 与已冻结出口都会被跳过，绝不再打同一个 IP）。
			client, clientErr = s.proxyClient(retryProxy)
			currentProxy = retryProxy
		} else {
			client, currentProxy, clientErr = s.clientForEndpoint(selected.ID, selected.ProxyPool, selected.ProxyEnabled, selected.ForceProxy, p.sessionKey, selected.Protocol, selected.ProxyPoolID)
		}
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
		triedProxies[currentProxy] = true

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
			}, p.realModel)
			return res
		}
		httpReq.Header.Set("Content-Type", "application/json")
		if stream {
			httpReq.Header.Set("Accept", "text/event-stream")
		} else {
			httpReq.Header.Set("Accept", "application/json")
		}
		applyCustomHeaders(httpReq, selected.Headers)

		// 多 API Key 选择：轮询选一个 key（key 永不冻结，未达单请求最大尝试次数的
		// key 会被继续选中，已耗尽的 key 被跳过）。
		keys := selected.AllKeys()
		currentKey, currentKeyIndex := s.pickKey(selected.ID, keys, triedKeys, keyMaxTries)
		res.stepKeyIndex = currentKeyIndex
		if currentKey == "" {
			// 本轮全部 key 均已达到单请求最大尝试次数：本端点不可用，标记该端点后尝试下一个候选端点。
			cancel()
			s.markProxyFailed(selected.ID, currentProxy)
			res.endpointExhausted = true
			lastErr = fmt.Errorf("端点 %s 本轮全部 API Key 均已尝试 %d 次失败", selected.Name, keyMaxTries)
			lastProxy = currentProxy
			break
		}
		if isGoogleAPIKeyUpstream(selected) {
			httpReq.Header.Set("X-Goog-Api-Key", currentKey)
		} else {
			httpReq.Header.Set("Authorization", "Bearer "+currentKey)
		}

		// 逐跳响应头等待上限：仍有可切换出口时，代理在 attemptHeaderTimeout 内
		// 不返回响应头即视为该出口链路不可用，立即切下一个，避免挂死代理拖住
		// 整条回退链（非流式 transport 的 ResponseHeaderTimeout 高达 180s）。
		canSwitch := selected.AutoSwitch && attempt+1 < maxProxyAttempts && currentProxy != ""
		if canSwitch {
			type doRes struct {
				resp *http.Response
				err  error
			}
			ch := make(chan doRes, 1)
			go func() {
				r, e := client.Do(httpReq)
				ch <- doRes{r, e}
			}()
			select {
			case d := <-ch:
				resp, lastErr = d.resp, d.err
			case <-time.After(attemptHeaderTimeout):
				cancel()
				// 吞掉晚到的 Do 结果：若其已成功打开响应体，ctx 取消会关闭连接。
				select {
				case d := <-ch:
					if d.resp != nil {
						d.resp.Body.Close()
					}
				default:
				}
				s.markProxyFailed(selected.ID, currentProxy)
				res.retryableUpstream = true
				lastErr = fmt.Errorf("上游响应头超时（超过 %s）", attemptHeaderTimeout)
				resp = nil
				if attempt+1 < maxProxyAttempts {
					continue
				}
				break
			}
		} else {
			resp, lastErr = client.Do(httpReq)
		}
		if lastErr != nil {
			// 连接失败（例如该代理不可用）：key 不冻结，只标记代理失败，若有池则切下一个。
			s.markProxyFailed(selected.ID, currentProxy)
			if s.externalPoolInUse(selected.ProxyPoolID) {
				s.reportExternalPoolResult(selected.ProxyPoolID, currentProxy, false, false, nil)
			}
			cancel()
			if errors.Is(lastErr, context.Canceled) {
				res.clientCancelled = true
			}
			if currentProxy != "" && attempt+1 < maxProxyAttempts {
				continue
			}
			// 直连（代理池全部冻结回退）没有可切换的出口，重试只是重复打同一条链路；
			// 但端点级还有别的候选可用，连接层失败同样交给 failover 尝试下一个端点，
			// 而不是直接把 502 返回给客户端。
			res.retryableUpstream = true
			break
		}

		// 401/403 鉴权失败：key 本身失效，但不冻结；本轮请求内该 key 的失败次数
		// 计入 triedKeys（达到 keyMaxTries 后不再被选中），继续尝试下一个 key
		// （或由 pickKey 耗尽后切换端点）。
		// 不消耗代理切换次数，因为 key 问题是凭据级非代理级。
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			triedKeys[currentKey]++
			resp.Body.Close()
			cancel()
			continue
		}

		// 非流式：读取正文判断限流或 5xx，失败时在循环内重试。
		// 上游限流/5xx 不是代理的错：只切换出口重试，不惩罚代理（不冷却），
		// 避免上游故障污染整个代理池。但限流（429）会累计计数，
		// 达到阈值后临时禁用该代理（IP 级限流下继续选择只会反复 429）。
		if !stream && selected.AutoSwitch && attempt+1 < maxProxyAttempts {
			// 非流式重试判定：施加 upstreamBodyLimit 硬上限，避免超大上游响应
			// 被完整读入内存（与请求体 :284 的 readUpstreamBodyLimited 一致）。
			bodyBytesRead, readErr := readUpstreamBodyLimited(resp.Body)
			resp.Body.Close()
			cancel()
			if readErr != nil || isRetryableUpstreamResponse(resp, bodyBytesRead) {
				is429 := isRateLimitResponse(resp, bodyBytesRead)
				if is429 {
					res.retryBody = bodyBytesRead
					s.markProxy429(selected.ID, currentProxy, retryAfterFromHeader(resp))
					// 随机换出口：已试出口与组冻结出口都会跳过，绝不重复打同一 IP。
					if bump429(currentProxy) {
						// 5 个不同出口 IP 均 429：限流已扩散到整池，提前收尾
						// （保留已读正文原样交给统一判定记录，以 429 退出）。
						resp.Body = io.NopCloser(bytes.NewReader(bodyBytesRead))
						res.retryableUpstream = true
						lastErr = fmt.Errorf("上游按出口限流（连续 %d 个出口 429）", proxyRateLimitPicks)
						break
					}
					if currentProxy != "" {
						var next string
						if s.externalPoolInUse(selected.ProxyPoolID) {
							// 独立代理池：反馈 429 冻结当前出口后重选池内下一出口。
							next = s.externalPoolNextProxy(ctx, selected.ProxyPoolID, currentProxy, retryAfterFromHeader(resp))
						} else {
							next = s.pickRandomAvailableProxy(selected.ID, cleanProxyPool(selected.ProxyPool), triedProxies, currentProxy)
						}
						if next == "" {
							// 全部出口已试/已冻结：无可换出口，提前收尾。
							resp.Body = io.NopCloser(bytes.NewReader(bodyBytesRead))
							res.retryableUpstream = true
							lastErr = fmt.Errorf("上游按出口限流（出口已耗尽，%d 个出口均 429）", proxyRateLimitPicks)
							break
						}
						lastProxy = next
						res.egressIP = proxyEndpointAddr(next)
						retryProxy = next
						s.clearSessionBinding(selected.ID, p.sessionKey)
						continue
					}
				}
				s.clearSessionBinding(selected.ID, p.sessionKey)
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

		// 流式：仅按状态码判断，限流或 5xx 时切换代理重试一次（429 立即组冻结出口）。
		if stream && selected.AutoSwitch && attempt+1 < maxProxyAttempts && isRetryableUpstreamResponse(resp, nil) {
			is429 := isRateLimitResponse(resp, nil)
			if is429 {
				s.markProxy429(selected.ID, currentProxy, retryAfterFromHeader(resp))
			}
			resp.Body.Close()
			cancel()
			if is429 && currentProxy != "" {
				// 随机换出口：已试出口与组冻结出口都会跳过，绝不重复打同一 IP。
				if bump429(currentProxy) {
					// 5 个不同出口 IP 均 429：上游限流已扩散到整池，提前收尾。
					res.retryableUpstream = true
					lastErr = fmt.Errorf("上游按出口限流（连续 %d 个出口 429）", proxyRateLimitPicks)
					break
				}
				var next string
				if s.externalPoolInUse(selected.ProxyPoolID) {
					// 独立代理池：反馈 429 冻结当前出口后重选池内下一出口。
					next = s.externalPoolNextProxy(ctx, selected.ProxyPoolID, currentProxy, retryAfterFromHeader(resp))
				} else {
					next = s.pickRandomAvailableProxy(selected.ID, cleanProxyPool(selected.ProxyPool), triedProxies, currentProxy)
				}
				if next == "" {
					// 全部出口已试/已冻结：无可换出口，提前收尾。
					res.retryableUpstream = true
					lastErr = fmt.Errorf("上游按出口限流（出口已耗尽，%d 个出口均 429）", proxyRateLimitPicks)
					break
				}
				lastProxy = next
				res.egressIP = proxyEndpointAddr(next)
				retryProxy = next
				s.clearSessionBinding(selected.ID, p.sessionKey)
				continue
			}
			s.clearSessionBinding(selected.ID, p.sessionKey)
			if currentProxy != "" {
				// 非 429 的可重试错误（5xx/首字超时外的连接类）：交给择优换下一个出口。
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
		if errors.Is(lastErr, context.Canceled) {
			res.clientCancelled = true
		}
		if !res.retryableUpstream && !res.clientCancelled {
			// 不可重试的终局失败（配置/网关侧错误）在此记录并直接返回。
			// 客户端已断开（context canceled）是用户主动停止，不视为故障：
			// 静默收尾、不记账，避免把中止请求记成网关 502。
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
			}, p.realModel)
		} else {
			// 可重试失败（429/5xx/首字或响应头超时/连接耗尽）：循环内不逐次记日志，
			// 也不在此记账——端点级 failover 聚合会按「最终结果」记一条（含尝试次数
			// 与出口代理），保证整条回退链只落一条终局日志。
		}
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
		// 读取 429 正文以解析 Google RetryInfo/ErrorInfo 延迟（仅非流式；流式
		// 错误体不读，避免消耗流）。读取后重建 body，保证调用方仍可消费。
		if !stream {
			if bodyBytesRead, readErr := readUpstreamBodyLimited(resp.Body); readErr == nil {
				res.retryBody = bodyBytesRead
				resp.Body = io.NopCloser(bytes.NewReader(bodyBytesRead))
			}
		}
		s.markProxy429(selected.ID, lastProxy, retryAfterFromHeader(resp))
		if s.externalPoolInUse(selected.ProxyPoolID) {
			s.reportExternalPoolResult(selected.ProxyPoolID, lastProxy, false, true, retryAfterFromHeader(resp))
		}
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
		// 不在此记账：失败按「最终结果」由端点级 failover 聚合为一条，
		// 保证整条回退链在 relay-errors 中只出现一次（Attempts 体现尝试次数）。
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
	// 成功转发：记录池级粘性出口（健康且 TTFB<10s 的代理持续复用，直到被冷却/冻结才换）。
	if resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 400 && res.lastProxy != "" {
		stickyTTFB := res.ttfbMs
		if stickyTTFB <= 0 {
			stickyTTFB = time.Since(res.startTime).Milliseconds()
		}
		s.recordActiveProxy(selected.ID, res.lastProxy, stickyTTFB)
	}
	return res
}

// relayCancelOnCloseBody 在正文关闭时连带释放 attempt context，供正文由调用方
// 消费的入口（/v1/messages）使用：避免在正文未读完时提前 cancel 掐断响应。
type relayCancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}
