package client

import (
	"compress/flate"
	"compress/gzip"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	trans "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/transport"
)

// wireDoer decorates a transport with the two behaviours that have to apply to
// every upstream request regardless of which endpoint issued it:
//
//   - replaying per-account cookies, so the session looks like a browser that
//     accepted what the server set rather than one that never stores anything;
//   - decompressing responses, which the caller must now do itself because we
//     advertise Chrome's full Accept-Encoding instead of letting the HTTP stack
//     inject a gzip-only header it would transparently unwrap.
type wireDoer struct {
	inner trans.Doer
	jar   *cookieJar
}

func newWireDoer(inner trans.Doer, jar *cookieJar) trans.Doer {
	if inner == nil {
		return nil
	}
	return wireDoer{inner: inner, jar: jar}
}

func (d wireDoer) Do(req *http.Request) (*http.Response, error) {
	accountID := accountIDFromRequest(req)
	d.jar.apply(accountID, req)

	resp, err := d.inner.Do(req)
	if err != nil {
		return nil, err
	}

	d.jar.capture(accountID, resp)
	if err := decompressResponse(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

// CloseIdleConnections 把空闲连接清理转发到底层 transport，
// 供代理配置变更后丢弃已缓存的连接池。
func (d wireDoer) CloseIdleConnections() {
	type idleCloser interface{ CloseIdleConnections() }
	if closer, ok := d.inner.(idleCloser); ok {
		closer.CloseIdleConnections()
	}
}

func accountIDFromRequest(req *http.Request) string {
	if req == nil {
		return ""
	}
	a, ok := auth.FromContext(req.Context())
	if !ok || a == nil {
		return ""
	}
	return strings.TrimSpace(a.AccountID)
}

// cookieJar keeps cookies per account, in memory only.
//
// A client that presents a Chrome User-Agent, Origin and sec-fetch-site:
// same-origin but never sends a single Cookie is contradicting itself at the
// application layer. Replaying what the server sets removes that tell without
// changing any request semantics: nothing is invented, only echoed back.
type cookieJar struct {
	mu sync.RWMutex
	m  map[string]map[string]string // accountID -> cookie name -> value
}

func newCookieJar() *cookieJar {
	return &cookieJar{m: map[string]map[string]string{}}
}

func (j *cookieJar) apply(accountID string, req *http.Request) {
	if j == nil || req == nil || accountID == "" {
		return
	}
	// Never overwrite a Cookie header a caller set deliberately.
	if req.Header.Get("Cookie") != "" {
		return
	}

	j.mu.RLock()
	jar := j.m[accountID]
	names := make([]string, 0, len(jar))
	for name := range jar {
		names = append(names, name)
	}
	pairs := make([]string, 0, len(jar))
	sort.Strings(names) // stable ordering; a browser does not shuffle its cookies
	for _, name := range names {
		pairs = append(pairs, name+"="+jar[name])
	}
	j.mu.RUnlock()

	if len(pairs) == 0 {
		return
	}
	req.Header.Set("Cookie", strings.Join(pairs, "; "))
}

func (j *cookieJar) capture(accountID string, resp *http.Response) {
	if j == nil || resp == nil || accountID == "" {
		return
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return
	}

	j.mu.Lock()
	defer j.mu.Unlock()
	jar := j.m[accountID]
	if jar == nil {
		jar = map[string]string{}
		j.m[accountID] = jar
	}
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		if expired(c) {
			delete(jar, c.Name)
			continue
		}
		jar[c.Name] = c.Value
	}
}

func expired(c *http.Cookie) bool {
	if c.MaxAge < 0 {
		return true
	}
	if c.MaxAge == 0 && !c.Expires.IsZero() && c.Expires.Before(time.Now()) {
		return true
	}
	return false
}

// forgetAccountCookies drops an account's cookies, e.g. after re-login so a
// stale session cookie is not replayed alongside a fresh token.
func (j *cookieJar) forget(accountID string) {
	if j == nil || accountID == "" {
		return
	}
	j.mu.Lock()
	delete(j.m, accountID)
	j.mu.Unlock()
}

// decompressResponse replaces resp.Body with a streaming decompressing reader
// and clears the now-meaningless Content-Encoding/Content-Length metadata.
//
// The readers are all incremental, so Server-Sent Events keep streaming rather
// than buffering until the response completes.
func decompressResponse(resp *http.Response) error {
	if resp == nil || resp.Body == nil {
		return nil
	}
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	switch encoding {
	case "", "identity":
		return nil
	}

	decoded, err := decompressReader(resp.Body, encoding)
	if err != nil {
		return err
	}
	if decoded == nil {
		return nil // unknown encoding: leave the body untouched
	}

	resp.Body = decoded
	resp.Header.Del("Content-Encoding")
	resp.Header.Del("Content-Length")
	resp.ContentLength = -1
	resp.Uncompressed = true
	return nil
}

// decompressReader returns a reader that decodes encoding, or nil when the
// encoding is not one we handle.
func decompressReader(body io.ReadCloser, encoding string) (io.ReadCloser, error) {
	switch encoding {
	case "gzip":
		gz, err := gzip.NewReader(body)
		if err != nil {
			return nil, err
		}
		return chainCloser{Reader: gz, closers: []io.Closer{gz, body}}, nil
	case "deflate":
		fl := flate.NewReader(body)
		return chainCloser{Reader: fl, closers: []io.Closer{fl, body}}, nil
	case "br":
		return chainCloser{Reader: brotli.NewReader(body), closers: []io.Closer{body}}, nil
	case "zstd":
		zr, err := zstd.NewReader(body)
		if err != nil {
			return nil, err
		}
		return chainCloser{Reader: zr.IOReadCloser(), closers: []io.Closer{zr.IOReadCloser(), body}}, nil
	default:
		return nil, nil
	}
}

// chainCloser closes the decompressor and the underlying body together.
type chainCloser struct {
	io.Reader
	closers []io.Closer
}

func (c chainCloser) Close() error {
	var firstErr error
	for _, closer := range c.closers {
		if err := closer.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
