package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/aiagent"
)

// agentReqForTest 造一个最小的网关→Agent 请求。
func agentReqForTest(path string) aiagent.AgentHTTPRequest {
	return aiagent.AgentHTTPRequest{
		Method: "GET",
		Path:   path,
		Header: map[string][]string{},
	}
}

// fakeAgentTunnel 模拟主机 Agent 的复用隧道行为：
//
//   - 每条连接只服务一次「读请求头 → 回响应」，之后循环读下一条请求（keep-alive）；
//   - 它**不**支持同一连接上的流水线（HTTP/1.1 无流水线），因此并发请求必须由
//     不同的 net.Conn 承载。
//
// 这是线上真实形态：网关的 http.Transport 复用连接池，每条请求占一条 conn。
type fakeAgentTunnel struct {
	ln net.Listener

	mu       sync.Mutex
	dials    int
	requests int
	conns    []net.Conn
}

// dropAll 强制断开所有已建立的连接，模拟 Agent 重启/网络中断：
// 池子里残留的连接会变成「可写但读不到响应」的半死状态。
func (f *fakeAgentTunnel) dropAll() {
	f.mu.Lock()
	conns := append([]net.Conn(nil), f.conns...)
	f.conns = nil
	f.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

func newFakeAgentTunnel(t *testing.T) *fakeAgentTunnel {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeAgentTunnel{ln: ln}
	go f.serve()
	t.Cleanup(func() { _ = ln.Close() })
	return f
}

func (f *fakeAgentTunnel) dialCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dials
}

func (f *fakeAgentTunnel) connCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.conns)
}

func (f *fakeAgentTunnel) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeAgentTunnel) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.dials++
		f.conns = append(f.conns, conn)
		f.mu.Unlock()
		go f.handle(conn)
	}
}

func (f *fakeAgentTunnel) handle(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		// 读请求头（到空行）
		path := ""
		contentLength := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if strings.HasPrefix(line, "GET ") || strings.HasPrefix(line, "POST ") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					path = parts[1]
				}
			}
			if lower := strings.ToLower(line); strings.HasPrefix(lower, "content-length:") {
				_, _ = fmt.Sscanf(strings.TrimSpace(line[len("content-length:"):]), "%d", &contentLength)
			}
		}
		if contentLength > 0 {
			if _, err := io.CopyN(io.Discard, reader, int64(contentLength)); err != nil {
				return
			}
		}
		f.mu.Lock()
		f.requests++
		f.mu.Unlock()

		body := fmt.Sprintf(`{"path":%q}`, path)
		resp := fmt.Sprintf(
			"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: keep-alive\r\n\r\n%s",
			len(body), body,
		)
		if _, err := conn.Write([]byte(resp)); err != nil {
			return
		}
	}
}

// aiagentRuntimeWithDial 构造一个用假隧道做 DialContext 的 runtime。
func aiagentRuntimeWithDial(f *fakeAgentTunnel) *aiagentRuntime {
	r := &aiagentRuntime{}
	r.dialOverride = func(_ context.Context, _ string, _ int) (net.Conn, error) {
		return net.Dial("tcp", f.ln.Addr().String())
	}
	return r
}

// 并发请求必须各自拿到**独立连接**：复用隧道不支持流水线，若两条请求挤在同一
// 条 conn 上，响应会串行化甚至错位。
//
// 回归背景：线上表现为「并发一上来，轻量投影就永久失效」——因为共享的
// http.Transport 把并发请求塞进同一条隧道，隧道响应边界被打乱后，
// 后续所有请求都退化成裸透传。
func TestPooledTransportServesConcurrentRequestsOnSeparateConns(t *testing.T) {
	f := newFakeAgentTunnel(t)
	r := aiagentRuntimeWithDial(f)

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, err := r.RoundTrip(context.Background(), "srv-1", 4097, agentReqForTest("/ping/"+fmt.Sprint(idx)))
			if err != nil {
				errs[idx] = err
				return
			}
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != 200 {
				errs[idx] = fmt.Errorf("status %d", resp.StatusCode)
				return
			}
			want := "/ping/" + fmt.Sprint(idx)
			if !strings.Contains(string(body), want) {
				errs[idx] = fmt.Errorf("响应串台：想要 %s，得到 %s", want, body)
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("请求 %d 失败: %v", i, err)
		}
	}
	if got := f.requestCount(); got != n {
		t.Fatalf("隧道应处理 %d 条请求，实际 %d", n, got)
	}
}

// 顺序请求应复用同一条连接（keep-alive 生效），避免每次重握手。
func TestPooledTransportReusesConnSequentially(t *testing.T) {
	f := newFakeAgentTunnel(t)
	r := aiagentRuntimeWithDial(f)

	for i := 0; i < 5; i++ {
		resp, err := r.RoundTrip(context.Background(), "srv-1", 4097, agentReqForTest("/seq"))
		if err != nil {
			t.Fatalf("第 %d 次请求失败: %v", i, err)
		}
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	if got := f.dialCount(); got != 1 {
		t.Fatalf("顺序请求应只建 1 条连接（keep-alive 复用），实际 %d 条", got)
	}
	if got := f.requestCount(); got != 5 {
		t.Fatalf("应处理 5 条请求，实际 %d", got)
	}
}

// 删除实例 / Agent 断线后，池子必须作废，下一次请求要新建连接。
func TestInvalidateForcesFreshConnection(t *testing.T) {
	f := newFakeAgentTunnel(t)
	r := aiagentRuntimeWithDial(f)

	do := func() {
		resp, err := r.RoundTrip(context.Background(), "srv-1", 4097, agentReqForTest("/x"))
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		_, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
	}

	do()
	if got := f.dialCount(); got != 1 {
		t.Fatalf("首次应建 1 条连接，实际 %d", got)
	}

	r.invalidatePooledTransport("srv-1", 4097)
	do()
	if got := f.dialCount(); got != 2 {
		t.Fatalf("失效后应重新建连，实际 %d 条", got)
	}
}

// 并发 + 复用混合压测：先并发一批，再顺序一批，全部应成功且不串台。
func TestPooledTransportMixedLoad(t *testing.T) {
	f := newFakeAgentTunnel(t)
	r := aiagentRuntimeWithDial(f)

	var wg sync.WaitGroup
	for round := 0; round < 3; round++ {
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func(rnd, idx int) {
				defer wg.Done()
				path := fmt.Sprintf("/r%d-%d", rnd, idx)
				resp, err := r.RoundTrip(context.Background(), "srv-1", 4097, agentReqForTest(path))
				if err != nil {
					t.Errorf("混合负载请求失败: %v", err)
					return
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)
				if !strings.Contains(string(body), path) {
					t.Errorf("响应串台：想要 %s，得到 %s", path, body)
				}
			}(round, i)
		}
		// 每轮之间留一点时间让连接回池
		time.Sleep(20 * time.Millisecond)
	}
	wg.Wait()
}

// 池中连接失效后，RoundTrip 必须自动作废池子并重试一次，而不是把错误抛给调用方
// 让该实例永久退化。
//
// 回归背景：线上表现为「并发一上来/Agent 重启后，该实例的消息投影永久失效，
// 必须重启网关才恢复」——因为失效连接留在池里，每次请求都复用它。
func TestRoundTripSelfHealsAfterStalePooledConnection(t *testing.T) {
	f := newFakeAgentTunnel(t)
	r := aiagentRuntimeWithDial(f)

	// 先建立一条池中连接
	resp, err := r.RoundTrip(context.Background(), "srv-1", 4097, agentReqForTest("/first"))
	if err != nil {
		t.Fatalf("首次请求失败: %v", err)
	}
	_, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if got := f.dialCount(); got != 1 {
		t.Fatalf("首次应建 1 条连接，实际 %d", got)
	}

	// 模拟 Agent 重启：服务端强制断开所有已建立的连接。
	f.dropAll()

	// 下一次请求应自愈：作废池子 → 新建连接 → 成功
	resp2, err := r.RoundTrip(context.Background(), "srv-1", 4097, agentReqForTest("/second"))
	if err != nil {
		t.Fatalf("失效连接后应自动重试成功，实际失败: %v", err)
	}
	defer resp2.Body.Close()
	body, _ := io.ReadAll(resp2.Body)
	if !strings.Contains(string(body), "/second") {
		t.Fatalf("重试响应内容不对: %s", body)
	}
	if got := f.dialCount(); got < 2 {
		t.Fatalf("自愈应新建连接，实际 dial 次数 %d", got)
	}
}

// 流式请求（SSE/WebSocket）不做重试：重试会重复发起有副作用的请求。
func TestStreamingRequestIsNotRetried(t *testing.T) {
	f := newFakeAgentTunnel(t)
	r := aiagentRuntimeWithDial(f)

	req := agentReqForTest("/global/event")
	req.Header = map[string][]string{"Accept": {"text/event-stream"}}

	// 假隧道只会正常响应；这里验证的是「流式请求不会因为一次失败而被重放」。
	// 用一个必然失败的 dial 覆盖来观察调用次数。
	calls := 0
	r.dialOverride = func(_ context.Context, _ string, _ int) (net.Conn, error) {
		calls++
		return nil, fmt.Errorf("模拟反连失败")
	}

	if _, err := r.RoundTrip(context.Background(), "srv-1", 4097, req); err == nil {
		t.Fatal("反连失败时应返回错误")
	}
	if calls != 1 {
		t.Fatalf("流式请求不应重试，dial 调用次数应为 1，实际 %d", calls)
	}

	// 对照：非流式请求应重试一次（共 2 次 dial）
	calls = 0
	if _, err := r.RoundTrip(context.Background(), "srv-1", 4097, agentReqForTest("/plain")); err == nil {
		t.Fatal("反连失败时应返回错误")
	}
	if calls != 2 {
		t.Fatalf("非流式请求应重试一次（共 2 次 dial），实际 %d", calls)
	}
	_ = f
}
