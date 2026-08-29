package client

import (
	"io"
	"net/http"
	"strings"
)

// readResponseBody reads a fully-buffered response body.
//
// Decompression normally already happened in wireDoer, which strips
// Content-Encoding. The switch here is a defensive fallback for any response
// that did not pass through that decorator.
func readResponseBody(resp *http.Response) ([]byte, error) {
	encoding := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Encoding")))
	switch encoding {
	case "", "identity":
		return io.ReadAll(resp.Body)
	}
	decoded, err := decompressReader(resp.Body, encoding)
	if err != nil {
		return nil, err
	}
	if decoded == nil {
		return io.ReadAll(resp.Body)
	}
	defer func() { _ = decoded.Close() }()
	return io.ReadAll(decoded)
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
