package client

import (
	"io"
	"net/http"
	"strings"
)

// maxControlResponseBytes caps fully-buffered control-plane responses
// (login, session, PoW, upload). These are small JSON envelopes; the cap is a
// hard memory guard so an oversized upstream response can never be fully
// buffered on a small-memory host (200 MB total).
const maxControlResponseBytes = 2 << 20

// readResponseBody reads a fully-buffered response body.
//
// Decompression normally already happened in wireDoer, which strips
// Content-Encoding. The switch here is a defensive fallback for any response
// that did not pass through that decorator.
func readResponseBody(resp *http.Response) ([]byte, error) {
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	switch encoding {
	case "", "identity":
		return io.ReadAll(io.LimitReader(resp.Body, maxControlResponseBytes))
	}
	decoded, err := decompressReader(resp.Body, encoding)
	if err != nil {
		return nil, err
	}
	if decoded == nil {
		return io.ReadAll(io.LimitReader(resp.Body, maxControlResponseBytes))
	}
	defer func() { _ = decoded.Close() }()
	return io.ReadAll(io.LimitReader(decoded, maxControlResponseBytes))
}

func preview(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		return s[:160]
	}
	return s
}

func (c *Client) jsonHeaders(headers map[string]string) map[string]string {
	out := cloneStringMap(headers)
	out["Content-Type"] = "application/json"
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
