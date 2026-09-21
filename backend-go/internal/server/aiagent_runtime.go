package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/aiagent"
	"github.com/iwvw/api-monitor/backend-go/internal/serveragent"
)

// aiagentRuntime 把 serveragent 的 Agent 能力适配成 aiagent 模块所需的
// AgentRuntime 抽象：探测走 Agent 任务，转发走原生流通道 + 反向代理。
type aiagentRuntime struct {
	server *serveragent.Service
}

// errAgentStreamAborted 表示上游在响应体传输中途中止（无更具体的错误可用）。
var errAgentStreamAborted = errors.New("agent stream aborted")

func newAIAgentRuntime(server *serveragent.Service) *aiagentRuntime {
	return &aiagentRuntime{server: server}
}

func (r *aiagentRuntime) AgentOnline(serverID string) bool {
	return r.server.HasAgentConnection(serverID)
}

func (r *aiagentRuntime) AgentSupportsAIAgentStream(serverID string) bool {
	return r.server.AgentSupportsAIAgentStream(serverID)
}

func (r *aiagentRuntime) AgentSupportsLifecycle(serverID string) bool {
	return r.server.AgentSupportsAIAgentLifecycle(serverID)
}

// StartProcess 下发任务 57 启动托管进程。云端只能传 Provider ID 与端口，
// 可执行路径与参数由 Agent 侧模板决定（ADR-0006 第 6 条）。
func (r *aiagentRuntime) StartProcess(ctx context.Context, serverID string, payload aiagent.LifecycleStartPayload) (aiagent.LifecycleResult, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return aiagent.LifecycleResult{}, err
	}
	result, err := r.server.RunAIAgentStartTaskAndWaitCtx(ctx, serverID, string(raw))
	if err != nil {
		return aiagent.LifecycleResult{}, err
	}
	return decodeLifecycleResult(result)
}

// StopProcess 下发任务 58 停止托管进程。
func (r *aiagentRuntime) StopProcess(ctx context.Context, serverID, instanceID string) (aiagent.LifecycleResult, error) {
	raw, err := json.Marshal(map[string]string{"instance_id": instanceID})
	if err != nil {
		return aiagent.LifecycleResult{}, err
	}
	result, err := r.server.RunAIAgentStopTaskAndWaitCtx(ctx, serverID, string(raw))
	if err != nil {
		return aiagent.LifecycleResult{}, err
	}
	return decodeLifecycleResult(result)
}

// ProcessStatus 下发任务 59 查询托管进程状态。
func (r *aiagentRuntime) ProcessStatus(ctx context.Context, serverID, instanceID string) (aiagent.LifecycleResult, error) {
	raw, err := json.Marshal(map[string]string{"instance_id": instanceID})
	if err != nil {
		return aiagent.LifecycleResult{}, err
	}
	result, err := r.server.RunAIAgentStatusTaskAndWaitCtx(ctx, serverID, string(raw))
	if err != nil {
		return aiagent.LifecycleResult{}, err
	}
	return decodeLifecycleResult(result)
}

// Diagnose 下发任务 60 诊断某 Provider 在主机侧的可用性：
// exe 是否就绪、端口区间占用与建议空闲端口。
func (r *aiagentRuntime) Diagnose(ctx context.Context, serverID, provider string) (aiagent.DiagnoseResult, error) {
	raw, err := json.Marshal(map[string]string{"provider": provider})
	if err != nil {
		return aiagent.DiagnoseResult{}, err
	}
	result, err := r.server.RunAIAgentDiagnoseTaskAndWaitCtx(ctx, serverID, string(raw))
	if err != nil {
		return aiagent.DiagnoseResult{}, err
	}
	return decodeDiagnoseResult(result)
}

// decodeDiagnoseResult 解析 Agent 返回的诊断 JSON。字段缺失视为合法
// （如 usedPorts 为空数组、suggestedPort 缺省为 0）。
func decodeDiagnoseResult(raw string) (aiagent.DiagnoseResult, error) {
	var decoded struct {
		Provider     string `json:"provider"`
		Executable   struct {
			Path  string `json:"path"`
			Found bool   `json:"found"`
		} `json:"executable"`
		PortRange struct {
			Min int `json:"min"`
			Max int `json:"max"`
		} `json:"portRange"`
		UsedPorts     []int `json:"usedPorts"`
		SuggestedPort int    `json:"suggestedPort"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return aiagent.DiagnoseResult{}, fmt.Errorf("invalid agent diagnose response: %w", err)
	}
	return aiagent.DiagnoseResult{
		Provider:        decoded.Provider,
		Executable:      decoded.Executable.Path,
		ExecutableFound: decoded.Executable.Found,
		PortRange: aiagent.PortRange{
			Min: decoded.PortRange.Min,
			Max: decoded.PortRange.Max,
		},
		UsedPorts:     decoded.UsedPorts,
		SuggestedPort: decoded.SuggestedPort,
	}, nil
}

// decodeLifecycleResult 解析 Agent 返回的进程状态 JSON。
// Agent 用同构 JSON 表达成功与失败（running=false 也是合法结果），
// 因此解析失败才是错误，字段缺失不算。
func decodeLifecycleResult(raw string) (aiagent.LifecycleResult, error) {
	var decoded struct {
		Managed                bool    `json:"managed"`
		Running                bool    `json:"running"`
		PID                    int     `json:"pid"`
		DesiredRunning         bool    `json:"desiredRunning"`
		Crashed                bool    `json:"crashed"`
		PortListening          bool    `json:"portListening"`
		ListenerPID            int     `json:"listenerPid"`
		ListenerMatchesProcess bool    `json:"listenerMatchesProcess"`
		UptimeSeconds          int     `json:"uptimeSeconds"`
		Restarts               int     `json:"restarts"`
		MemoryBytes            uint64  `json:"memoryBytes"`
		CPUPercent             float32 `json:"cpuPercent"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return aiagent.LifecycleResult{}, fmt.Errorf("invalid agent lifecycle response: %w", err)
	}
	return aiagent.LifecycleResult{
		Managed:                decoded.Managed,
		Running:                decoded.Running,
		PID:                    decoded.PID,
		DesiredRunning:         decoded.DesiredRunning,
		Crashed:                decoded.Crashed,
		PortListening:          decoded.PortListening,
		ListenerPID:            decoded.ListenerPID,
		ListenerMatchesProcess: decoded.ListenerMatchesProcess,
		UptimeSeconds:          decoded.UptimeSeconds,
		Restarts:               decoded.Restarts,
		MemoryBytes:            decoded.MemoryBytes,
		CPUPercent:             decoded.CPUPercent,
	}, nil
}

func (r *aiagentRuntime) Probe(ctx context.Context, serverID, provider string, port int, processMatch []string) (aiagent.ProbeResult, error) {
	payload, err := json.Marshal(map[string]interface{}{
		"provider":      provider,
		"port":          port,
		"process_match": processMatch,
	})
	if err != nil {
		return aiagent.ProbeResult{}, err
	}
	raw, err := r.server.RunAIAgentProbeTaskAndWaitCtx(ctx, serverID, string(payload))
	if err != nil {
		return aiagent.ProbeResult{}, err
	}
	var decoded struct {
		ProcessRunning         bool    `json:"processRunning"`
		PortListening          bool    `json:"portListening"`
		PID                    int     `json:"pid"`
		ListenerPID            int     `json:"listenerPid"`
		ListenerMatchesProcess bool    `json:"listenerMatchesProcess"`
		MemoryBytes            uint64  `json:"memoryBytes"`
		CPUPercent             float32 `json:"cpuPercent"`
		Detail                 string  `json:"detail"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return aiagent.ProbeResult{}, fmt.Errorf("invalid agent probe response: %w", err)
	}
	return aiagent.ProbeResult{
		ProcessRunning:         decoded.ProcessRunning,
		PortListening:          decoded.PortListening,
		PID:                    decoded.PID,
		ListenerPID:            decoded.ListenerPID,
		ListenerMatchesProcess: decoded.ListenerMatchesProcess,
		MemoryBytes:            decoded.MemoryBytes,
		CPUPercent:             decoded.CPUPercent,
		Detail:                 decoded.Detail,
	}, nil
}

// aiagentServerOptions 适配可登记主机列表。
type aiagentServerOptions struct {
	server *serveragent.Service
}

func (o *aiagentServerOptions) ListServerOptions(ctx context.Context) []aiagent.ServerOption {
	options := o.server.ListAgentServerOptions(ctx)
	result := make([]aiagent.ServerOption, 0, len(options))
	for _, option := range options {
		result = append(result, aiagent.ServerOption{
			ID:     option.ID,
			Name:   option.Name,
			Host:   option.Host,
			Online: option.Online,
		})
	}
	return result
}

// OpenStream 打开一条到目标主机端口的原始字节流连接（数据通道）。
// 直接复用 serveragent.OpenAIAgentPortStream：它返回的 net.Conn 实现了
// 完整双向读写，可承载 WebSocket 升级后的全双工帧。
func (r *aiagentRuntime) OpenStream(ctx context.Context, serverID string, port int) (io.ReadWriteCloser, error) {
	return r.server.OpenAIAgentPortStream(ctx, serverID, port)
}

// RoundTrip 经原生流通道完成一次 HTTP 往返。使用 http.Transport 自定义
// DialContext（每个请求一条数据通道），并通过 ReverseProxy 逐块转发以支持 SSE。
func (r *aiagentRuntime) RoundTrip(ctx context.Context, serverID string, port int, req aiagent.AgentHTTPRequest) (aiagent.AgentHTTPResponse, error) {
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:0"}

	transport := &http.Transport{
		DialContext: func(dialCtx context.Context, _, _ string) (net.Conn, error) {
			return r.server.OpenAIAgentPortStream(dialCtx, serverID, port)
		},
		DisableKeepAlives:   true,
		MaxIdleConns:        0,
		IdleConnTimeout:     0,
		TLSHandshakeTimeout: 5 * time.Second,
		// 目标 Agent 只接受连接却不返回响应头时，尽快释放通道，而不是挂到网关超时。
		ResponseHeaderTimeout: 30 * time.Second,
	}

	var statusCode int
	var header http.Header
	pr, pw := io.Pipe()
	headersReady := make(chan struct{})
	var headersOnce sync.Once
	var proxyErr error
	var proxyErrMu sync.Mutex

	outbound, err := http.NewRequestWithContext(ctx, req.Method, target.String(), bytes.NewReader(req.Body))
	if err != nil {
		return aiagent.AgentHTTPResponse{}, err
	}
	outbound.Header = req.Header.Clone()
	// ReverseProxy 用 Out.URL 的 path/query 发起请求。req.Path 可能形如
	// "/x?y=1"，必须解析后分别写入 Path 与 RawQuery，否则 '?' 会被转义、
	// query 被重复拼接。
	if parsed, parseErr := url.Parse(req.Path); parseErr == nil {
		outbound.URL.Path = parsed.Path
		outbound.URL.RawQuery = parsed.RawQuery
	} else {
		outbound.URL.Path = req.Path
	}

	proxy := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1, // 立即 flush：SSE 流式响应不被缓冲
		ModifyResponse: func(response *http.Response) error {
			statusCode = response.StatusCode
			header = response.Header.Clone()
			// 包一层 body：上游读取中途失败时记录错误。ReverseProxy 只在「响应头之前」
			// 的传输错误走 ErrorHandler，头之后的 body 读取错误会被它自己吞掉，
			// 导致截断响应被当成干净 EOF。
			response.Body = &recordingBody{inner: response.Body, record: func(err error) {
				proxyErrMu.Lock()
				if proxyErr == nil {
					proxyErr = err
				}
				proxyErrMu.Unlock()
			}}
			headersOnce.Do(func() { close(headersReady) })
			return nil
		},
		ErrorHandler: func(_ http.ResponseWriter, _ *http.Request, proxyError error) {
			proxyErrMu.Lock()
			proxyErr = proxyError
			proxyErrMu.Unlock()
			headersOnce.Do(func() { close(headersReady) })
			_ = pw.CloseWithError(proxyError)
		},
		Rewrite: func(proxyReq *httputil.ProxyRequest) {
			// 只改 scheme/host，保留 outbound 上已设置的 path 与 query。
			proxyReq.Out.URL.Scheme = target.Scheme
			proxyReq.Out.URL.Host = target.Host
			proxyReq.Out.Host = target.Host
		},
	}

	// statusOnlyWriter.Write 把响应体逐块写入 pipe：ReverseProxy 自己是唯一的
	// body 读取方（避免两个 goroutine 并发读同一 response.Body），调用方从 pr 流式消费。
	recorder := &statusOnlyWriter{pipe: pw}
	go serveAgentProxy(proxy, recorder, outbound, func(aborted bool) {
		proxyErrMu.Lock()
		streamFailed := proxyErr
		proxyErrMu.Unlock()
		closeAgentPipe(pw, recorder.writeErr(), streamFailed, aborted)
		headersOnce.Do(func() { close(headersReady) })
	})

	// 等响应头到达即可返回：SSE 场景下响应体随后持续流出，不能被整体等待。
	select {
	case <-headersReady:
	case <-ctx.Done():
		_ = pw.CloseWithError(ctx.Err())
		return aiagent.AgentHTTPResponse{}, ctx.Err()
	}

	proxyErrMu.Lock()
	responseErr := proxyErr
	proxyErrMu.Unlock()
	if responseErr != nil {
		return aiagent.AgentHTTPResponse{}, responseErr
	}
	if header == nil {
		return aiagent.AgentHTTPResponse{}, fmt.Errorf("agent stream closed before response")
	}
	return aiagent.AgentHTTPResponse{
		StatusCode: statusCode,
		Header:     header,
		Body:       pr,
	}, nil
}

// serveAgentProxy 在独立 goroutine 中执行 ReverseProxy，并接住它在中止流时抛出的
// panic(http.ErrAbortHandler)。net/http 只会 recover 它自己启动的 handler goroutine，
// 这里若不接住，客户端中途断开 SSE 就会终止整个后端进程。finalize 始终执行（含 panic
// 路径），参数表示本次是否因中止而结束。
func serveAgentProxy(proxy *httputil.ReverseProxy, writer http.ResponseWriter, req *http.Request, finalize func(aborted bool)) {
	aborted := false
	defer func() {
		recovered := recover()
		switch {
		case recovered == nil:
		case recovered == http.ErrAbortHandler:
			aborted = true
		default:
			finalize(false)
			panic(recovered) // 非中止类 panic 照常抛出，避免掩盖真实缺陷
		}
		finalize(aborted)
	}()
	proxy.ServeHTTP(writer, req)
}

// closeAgentPipe 按优先级关闭 pipe 写入端：写出错误 > 代理错误 > 中止 > 成功。
// 前三种都以错误关闭，避免截断的响应被调用方当成干净 EOF。
func closeAgentPipe(pw *io.PipeWriter, writeErr, proxyErr error, aborted bool) {
	switch {
	case writeErr != nil:
		_ = pw.CloseWithError(writeErr)
	case proxyErr != nil:
		_ = pw.CloseWithError(proxyErr)
	case aborted:
		_ = pw.CloseWithError(errAgentStreamAborted)
	default:
		// 成功路径同样要关闭写入端，否则调用方读不到 EOF（有限响应也会挂住）。
		_ = pw.Close()
	}
}

// recordingBody 包装上游响应体：读取错误通过 record 上报，供调用方识别
// 「响应头之后」的中途失败（ReverseProxy 不会把这类错误交给 ErrorHandler）。
type recordingBody struct {
	inner  io.ReadCloser
	record func(error)
}

func (b *recordingBody) Read(target []byte) (int, error) {
	n, err := b.inner.Read(target)
	if err != nil && err != io.EOF {
		b.record(err)
	}
	return n, err
}

func (b *recordingBody) Close() error {
	return b.inner.Close()
}

// statusOnlyWriter 丢弃状态行/头部，但把响应体转发到 pipe，使调用方能流式读取；
// 同时记录写出错误，供调用方以错误关闭 pipe（识别中途断开）。
type statusOnlyWriter struct {
	header http.Header
	pipe   *io.PipeWriter
	errMu  sync.Mutex
	err    error
}

func (w *statusOnlyWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *statusOnlyWriter) Write(payload []byte) (int, error) {
	if w.pipe == nil {
		return len(payload), nil
	}
	if _, err := w.pipe.Write(payload); err != nil {
		w.errMu.Lock()
		if w.err == nil {
			w.err = err
		}
		w.errMu.Unlock()
		return 0, err
	}
	return len(payload), nil
}

func (w *statusOnlyWriter) writeErr() error {
	w.errMu.Lock()
	defer w.errMu.Unlock()
	return w.err
}

func (w *statusOnlyWriter) WriteHeader(int) {}

