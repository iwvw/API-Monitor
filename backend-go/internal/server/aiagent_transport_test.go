package server

import (
	"net/http"
	"testing"
)

// 分流决策是本次改动的核心：非流式请求走复用连接池（省去每次约 700-900ms 的
// 反向握手），流式请求必须继续独占新建通道。
//
// 这里锁住的是「哪些请求会被判为流式」——判错的代价是流式响应被复用连接搞乱，
// 属于难以复现的线上故障，因此逐个形态都要有回归。
func TestIsStreamingRequest(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
		want   bool
	}{
		{
			name:   "无 Accept 头视为非流式",
			header: http.Header{},
			want:   false,
		},
		{
			name:   "普通 JSON 请求非流式",
			header: http.Header{"Accept": []string{"application/json"}},
			want:   false,
		},
		{
			name:   "通配 Accept 非流式",
			header: http.Header{"Accept": []string{"*/*"}},
			want:   false,
		},
		{
			name:   "纯 event-stream 是流式",
			header: http.Header{"Accept": []string{"text/event-stream"}},
			want:   true,
		},
		{
			name:   "复合 Accept 含 event-stream 也是流式",
			header: http.Header{"Accept": []string{"text/event-stream, application/json"}},
			want:   true,
		},
		{
			name:   "大小写不敏感",
			header: http.Header{"Accept": []string{"TEXT/EVENT-STREAM"}},
			want:   true,
		},
		{
			name:   "websocket 升级是流式",
			header: http.Header{"Upgrade": []string{"websocket"}},
			want:   true,
		},
		{
			name:   "websocket 大小写不敏感",
			header: http.Header{"Upgrade": []string{"WebSocket"}},
			want:   true,
		},
		{
			name:   "event-stream 带参数也是流式",
			header: http.Header{"Accept": []string{"text/event-stream; charset=utf-8"}},
			want:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStreamingRequest(tc.header); got != tc.want {
				t.Fatalf("isStreamingRequest(%v) = %v, want %v", tc.header, got, tc.want)
			}
		})
	}
}

// 池子按 (server, port) 分片，且同一组合应返回同一个实例（复用的前提）。
func TestPooledTransportIsSharedPerInstance(t *testing.T) {
	r := &aiagentRuntime{}

	a := r.pooledTransportFor("srv-1", 4097)
	b := r.pooledTransportFor("srv-1", 4097)
	if a != b {
		t.Fatal("同一 (server,port) 应复用同一个 Transport")
	}

	c := r.pooledTransportFor("srv-1", 4098)
	if a == c {
		t.Fatal("不同 port 不应共用同一个 Transport")
	}

	d := r.pooledTransportFor("srv-2", 4097)
	if a == d {
		t.Fatal("不同 server 不应共用同一个 Transport")
	}

	// 池子必须开启 keep-alive，否则复用无从谈起
	if a.DisableKeepAlives {
		t.Fatal("复用池不应禁用 keep-alive")
	}
	if a.MaxIdleConnsPerHost < 8 {
		t.Fatalf("空闲连接上限过小（%d），并发短请求会排队等连接", a.MaxIdleConnsPerHost)
	}
}

// 失效后应拿到新的 Transport，避免复用已断开 Agent 的旧连接。
func TestInvalidatePooledTransport(t *testing.T) {
	r := &aiagentRuntime{}

	before := r.pooledTransportFor("srv-1", 4097)
	r.invalidatePooledTransport("srv-1", 4097)
	after := r.pooledTransportFor("srv-1", 4097)

	if before == after {
		t.Fatal("失效后应重建 Transport，而不是复用旧的")
	}
}
