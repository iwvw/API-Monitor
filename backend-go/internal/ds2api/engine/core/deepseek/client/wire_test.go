package client

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

type fakeDoer struct {
	fn func(*http.Request) (*http.Response, error)
}

func (f fakeDoer) Do(req *http.Request) (*http.Response, error) { return f.fn(req) }

type closeTrackingDoer struct {
	fakeDoer
	closed int
}

func (d *closeTrackingDoer) CloseIdleConnections() { d.closed++ }

func TestWireDoerClosesInnerIdleConnections(t *testing.T) {
	inner := &closeTrackingDoer{fakeDoer: fakeDoer{fn: func(*http.Request) (*http.Response, error) {
		return nil, nil
	}}}
	doer := newWireDoer(inner, newCookieJar())
	closer, ok := doer.(interface{ CloseIdleConnections() })
	if !ok {
		t.Fatal("expected wire doer to expose idle connection cleanup")
	}
	closer.CloseIdleConnections()
	if inner.closed != 1 {
		t.Fatalf("expected inner cleanup once, got %d", inner.closed)
	}
}

func ctxForAccount(id string) context.Context {
	return auth.WithAuth(context.Background(), &auth.RequestAuth{
		AccountID: id,
		Account:   config.Account{Email: id},
	})
}

func mustRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://chat.deepseek.com/api/v0/x", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	return req
}

// A client presenting a Chrome UA with Origin and sec-fetch-site: same-origin
// that never returns a cookie contradicts itself. The jar replays only what the
// server actually set.
func TestCookieJarReplaysWhatServerSet(t *testing.T) {
	jar := newCookieJar()
	var sent []string

	doer := newWireDoer(fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		sent = append(sent, req.Header.Get("Cookie"))
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("{}")),
		}
		resp.Header.Add("Set-Cookie", "smidV2=abc123; Path=/")
		resp.Header.Add("Set-Cookie", "session=zzz; Path=/")
		return resp, nil
	}}, jar)

	ctx := ctxForAccount("acct-1")
	for i := 0; i < 2; i++ {
		resp, err := doer.Do(mustRequest(t, ctx))
		if err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}

	if sent[0] != "" {
		t.Errorf("first request should carry no cookie, got %q", sent[0])
	}
	if sent[1] != "session=zzz; smidV2=abc123" {
		t.Errorf("second request cookie = %q, want the two captured cookies in stable order", sent[1])
	}
}

func TestCookieJarIsolatesAccounts(t *testing.T) {
	jar := newCookieJar()
	var lastSent string

	doer := newWireDoer(fakeDoer{fn: func(req *http.Request) (*http.Response, error) {
		lastSent = req.Header.Get("Cookie")
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("{}")),
		}
		resp.Header.Add("Set-Cookie", "who="+accountIDFromRequest(req))
		return resp, nil
	}}, jar)

	first, err := doer.Do(mustRequest(t, ctxForAccount("acct-1")))
	if err != nil {
		t.Fatalf("acct-1: %v", err)
	}
	_ = first.Body.Close()

	// A different account must not inherit acct-1's cookies.
	second, err := doer.Do(mustRequest(t, ctxForAccount("acct-2")))
	if err != nil {
		t.Fatalf("acct-2: %v", err)
	}
	_ = second.Body.Close()
	if lastSent != "" {
		t.Fatalf("acct-2 leaked another account's cookie: %q", lastSent)
	}

	third, err := doer.Do(mustRequest(t, ctxForAccount("acct-1")))
	if err != nil {
		t.Fatalf("acct-1 again: %v", err)
	}
	_ = third.Body.Close()
	if lastSent != "who=acct-1" {
		t.Fatalf("acct-1 cookie = %q, want who=acct-1", lastSent)
	}
}

func TestCookieJarDropsExpiredCookies(t *testing.T) {
	jar := newCookieJar()
	jar.capture("acct-1", &http.Response{Header: http.Header{
		"Set-Cookie": []string{"keep=1", "gone=1"},
	}})
	jar.capture("acct-1", &http.Response{Header: http.Header{
		"Set-Cookie": []string{"gone=1; Max-Age=0; Expires=Thu, 01 Jan 1970 00:00:00 GMT"},
	}})

	req := mustRequest(t, ctxForAccount("acct-1"))
	jar.apply("acct-1", req)
	if got := req.Header.Get("Cookie"); got != "keep=1" {
		t.Fatalf("cookie = %q, want keep=1", got)
	}
}

func TestDecompressResponseHandlesEveryAdvertisedEncoding(t *testing.T) {
	const payload = `{"hello":"world"}`

	cases := map[string]func() []byte{
		"gzip": func() []byte {
			var buf bytes.Buffer
			zw := gzip.NewWriter(&buf)
			_, _ = zw.Write([]byte(payload))
			_ = zw.Close()
			return buf.Bytes()
		},
		"br": func() []byte {
			var buf bytes.Buffer
			bw := brotli.NewWriter(&buf)
			_, _ = bw.Write([]byte(payload))
			_ = bw.Close()
			return buf.Bytes()
		},
		"zstd": func() []byte {
			var buf bytes.Buffer
			zw, err := zstd.NewWriter(&buf)
			if err != nil {
				t.Fatalf("zstd writer: %v", err)
			}
			_, _ = zw.Write([]byte(payload))
			_ = zw.Close()
			return buf.Bytes()
		},
	}

	for encoding, compress := range cases {
		t.Run(encoding, func(t *testing.T) {
			body := compress()
			resp := &http.Response{
				StatusCode:    http.StatusOK,
				Header:        http.Header{"Content-Encoding": {encoding}, "Content-Length": {"999"}},
				Body:          io.NopCloser(bytes.NewReader(body)),
				ContentLength: int64(len(body)),
			}
			if err := decompressResponse(resp); err != nil {
				t.Fatalf("decompressResponse: %v", err)
			}
			got, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if string(got) != payload {
				t.Fatalf("body = %q, want %q", got, payload)
			}
			if resp.Header.Get("Content-Encoding") != "" {
				t.Error("Content-Encoding should be cleared once decoded")
			}
			if resp.Header.Get("Content-Length") != "" || resp.ContentLength != -1 {
				t.Error("Content-Length is meaningless after decoding and must be cleared")
			}
		})
	}
}

// The completion endpoint is Server-Sent Events. If decompression buffered the
// whole response, every token would arrive at once at the end of generation.
func TestDecompressedSSEStreamsIncrementally(t *testing.T) {
	pr, pw := io.Pipe()
	zw := gzip.NewWriter(pw)
	secondWritten := make(chan struct{})

	go func() {
		_, _ = zw.Write([]byte("data: first\n\n"))
		_ = zw.Flush() // emit a partial gzip flush, as a streaming server would
		<-secondWritten
		_, _ = zw.Write([]byte("data: second\n\n"))
		_ = zw.Close()
		_ = pw.Close()
	}()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Encoding": {"gzip"}},
		Body:       pr,
	}
	if err := decompressResponse(resp); err != nil {
		t.Fatalf("decompressResponse: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	type readResult struct {
		text string
		err  error
	}
	firstChunk := make(chan readResult, 1)
	go func() {
		buf := make([]byte, len("data: first\n\n"))
		n, err := io.ReadFull(resp.Body, buf)
		firstChunk <- readResult{text: string(buf[:n]), err: err}
	}()

	select {
	case got := <-firstChunk:
		if got.err != nil {
			t.Fatalf("reading first event: %v", got.err)
		}
		if got.text != "data: first\n\n" {
			t.Fatalf("first event = %q", got.text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first SSE event never arrived: the stream is being buffered, not streamed")
	}

	close(secondWritten)
	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading rest: %v", err)
	}
	if string(rest) != "data: second\n\n" {
		t.Fatalf("remaining stream = %q", rest)
	}
}

func TestDecompressResponseLeavesIdentityAlone(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("plain")),
	}
	if err := decompressResponse(resp); err != nil {
		t.Fatalf("decompressResponse: %v", err)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "plain" {
		t.Fatalf("body = %q, want plain", got)
	}
}
