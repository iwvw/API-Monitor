package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
)

// failingTransport 让 ReverseProxy 在写出响应头之后、拷贝响应体时失败，
// 触发 ReverseProxy 的 panic(http.ErrAbortHandler) 路径。
type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(&explodingReader{}),
	}, nil
}

// explodingReader 首次读取即返回错误，模拟上游流中途断开。
type explodingReader struct{}

func (e *explodingReader) Read([]byte) (int, error) {
	return 0, errors.New("upstream connection reset")
}

// brokenPipeWriter 让 statusOnlyWriter 的写出失败（模拟客户端已断开），
// 这是真实场景里触发 ReverseProxy 中止的最常见原因。
type brokenPipeWriter struct{}

func (brokenPipeWriter) Write([]byte) (int, error) {
	return 0, errors.New("client gone")
}

// TestServeAgentProxyRecoversAbort 回归：ReverseProxy 在另行启动的 goroutine 中执行，
// 它抛出的 panic(http.ErrAbortHandler) 不会被 net/http 兜住，必须由 serveAgentProxy
// 接住，否则客户端中途断开 SSE 会直接终止整个后端进程；finalize 必须始终执行。
//
// 注意：ReverseProxy 仅在「运行于 HTTP server 之下」时才真的 panic（见
// shouldPanicOnCopyError 对 http.ServerContextKey 的判断）。这里在请求上下文里
// 注入该键以复现线上由 net/http server 触发的场景。
func TestServeAgentProxyRecoversAbort(t *testing.T) {
	target, err := url.Parse("http://127.0.0.1:0")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}

	proxy := &httputil.ReverseProxy{
		Transport:     failingTransport{},
		FlushInterval: -1,
		Rewrite: func(proxyReq *httputil.ProxyRequest) {
			proxyReq.Out.URL.Scheme = target.Scheme
			proxyReq.Out.URL.Host = target.Host
		},
	}

	recorder := &statusOnlyWriter{pipe: nil, header: make(http.Header)}
	outbound, err := http.NewRequest(http.MethodGet, target.String(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	outbound = outbound.WithContext(context.WithValue(context.Background(), http.ServerContextKey, &http.Server{}))

	finalized := false
	abortedSeen := false
	// 不 recover：若 serveAgentProxy 未接住 panic，本测试会失败而非崩溃整个测试进程。
	serveAgentProxy(proxy, recorder, outbound, func(aborted bool) {
		finalized = true
		abortedSeen = aborted
	})

	if !finalized {
		t.Fatal("finalize must run even when the proxy aborts")
	}
	if !abortedSeen {
		t.Fatal("expected the abort to be reported to finalize")
	}
}

// TestServeAgentProxyReraisesUnexpectedPanic 确保非中止类 panic 不会被吞掉。
func TestServeAgentProxyReraisesUnexpectedPanic(t *testing.T) {
	target, err := url.Parse("http://127.0.0.1:0")
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	proxy := &httputil.ReverseProxy{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			panic("boom")
		}),
		Rewrite: func(proxyReq *httputil.ProxyRequest) {
			proxyReq.Out.URL.Scheme = target.Scheme
			proxyReq.Out.URL.Host = target.Host
		},
	}
	recorder := &statusOnlyWriter{pipe: nil, header: make(http.Header)}
	outbound, err := http.NewRequest(http.MethodGet, target.String(), strings.NewReader(""))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	finalized := false
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected the unexpected panic to be re-raised")
		}
		if !finalized {
			t.Fatal("finalize must run before re-raising")
		}
	}()
	serveAgentProxy(proxy, recorder, outbound, func(bool) { finalized = true })
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestCloseAgentPipePrioritisesErrors 回归：截断响应必须以错误关闭，不能被当成干净 EOF。
func TestCloseAgentPipePrioritisesErrors(t *testing.T) {
	cases := []struct {
		name      string
		writeErr  error
		proxyErr  error
		aborted   bool
		wantError bool
	}{
		{name: "write error wins", writeErr: errors.New("client gone"), wantError: true},
		{name: "proxy error", proxyErr: errors.New("upstream reset"), wantError: true},
		{name: "aborted without detail", aborted: true, wantError: true},
		{name: "clean success", wantError: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pr, pw := io.Pipe()
			closeAgentPipe(pw, tc.writeErr, tc.proxyErr, tc.aborted)

			_, readErr := io.ReadAll(pr)
			if tc.wantError && readErr == nil {
				t.Fatal("expected the reader to observe an error, got clean EOF")
			}
			if !tc.wantError && readErr != nil {
				t.Fatalf("expected clean EOF, got %v", readErr)
			}
		})
	}
}
