package transport

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	httpcloak "github.com/sardanioss/httpcloak/client"
)

// mergeHeaderOrder overlays a DeepSeek-specific order on top of a browser
// preset. Headers listed by customOrder keep their requested order; headers
// present only in baseOrder are appended in their original order. This lets us
// retain x-ds-pow-response without depending on httpcloak's preset internals.
func mergeHeaderOrder(baseOrder, customOrder []string) []string {
	seen := make(map[string]struct{}, len(baseOrder)+len(customOrder))
	out := make([]string, 0, len(baseOrder)+len(customOrder))
	appendUnique := func(values []string) {
		for _, value := range values {
			value = strings.ToLower(strings.TrimSpace(value))
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			out = append(out, value)
		}
	}
	appendUnique(customOrder)
	appendUnique(baseOrder)
	return out
}

type httpCloakDoer struct {
	client    *httpcloak.Client
	streaming bool
}

func newHTTPCloakDoer(timeout time.Duration, proxyURL string, streaming bool) *httpCloakDoer {
	return &httpCloakDoer{
		client:    newHTTPCloakClient(timeout, proxyURL),
		streaming: streaming,
	}
}

func newHTTPCloakClient(timeout time.Duration, proxyURL string) *httpcloak.Client {
	if timeout <= 0 {
		// httpcloak turns its configured timeout into a context deadline. A zero
		// deadline would cancel SSE immediately; streaming callers still retain
		// cancellation through req.Context().
		timeout = 24 * time.Hour
	}
	opts := []httpcloak.Option{
		httpcloak.WithTimeout(timeout),
		// Keep the first migration comparable with ds2api's current H2-only
		// transport. H3/ECH can be introduced in a separately measured change.
		httpcloak.WithForceHTTP2(),
		httpcloak.WithTLSOnly(),
	}
	if proxyURL != "" {
		opts = append(opts, httpcloak.WithTCPProxy(proxyURL))
	}
	client := httpcloak.NewClient("chrome-150-windows", opts...)
	client.SetHeaderOrder(mergeHeaderOrder(nil, chromeHeaderOrder))
	return client
}

func (d *httpCloakDoer) Do(req *http.Request) (*http.Response, error) {
	if d == nil || d.client == nil {
		return nil, fmt.Errorf("httpcloak client is nil")
	}
	request := toHTTPCloakRequest(req)
	if d.streaming {
		resp, err := d.client.DoStream(req.Context(), request)
		if err != nil {
			return nil, err
		}
		return fromHTTPCloakStreamResponse(resp, req)
	}
	resp, err := d.client.Do(req.Context(), request)
	if err != nil {
		return nil, err
	}
	return fromHTTPCloakResponse(resp, req)
}

// CloseIdleConnections releases pooled httpcloak connections when an account
// proxy or client bundle is replaced.
func (d *httpCloakDoer) CloseIdleConnections() {
	if d != nil && d.client != nil {
		d.client.Close()
	}
}

func toHTTPCloakRequest(req *http.Request) *httpcloak.Request {
	headers := make(map[string][]string, len(req.Header))
	for key, values := range req.Header {
		headers[key] = append([]string(nil), values...)
	}
	return &httpcloak.Request{
		Method:  req.Method,
		URL:     req.URL.String(),
		Headers: headers,
		Body:    req.Body,
	}
}

func fromHTTPCloakResponse(resp *httpcloak.Response, req *http.Request) (*http.Response, error) {
	if resp == nil {
		return nil, fmt.Errorf("httpcloak returned nil response")
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		StatusCode:    resp.StatusCode,
		Header:        normalizedHTTPCloakHeaders(resp.Headers),
		Body:          resp.Body,
		ContentLength: -1,
		Uncompressed:  true,
		Request:       req,
	}, nil
}

func fromHTTPCloakStreamResponse(resp *httpcloak.StreamResponse, req *http.Request) (*http.Response, error) {
	if resp == nil {
		return nil, fmt.Errorf("httpcloak returned nil stream response")
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", resp.StatusCode, http.StatusText(resp.StatusCode)),
		StatusCode:    resp.StatusCode,
		Header:        normalizedHTTPCloakHeaders(resp.Headers),
		Body:          resp,
		ContentLength: -1,
		Uncompressed:  true,
		Request:       req,
	}, nil
}

func httpCloakHeaders(headers map[string][]string) http.Header {
	out := make(http.Header, len(headers))
	for key, values := range headers {
		for _, value := range values {
			out.Add(key, value)
		}
	}
	return out
}

// httpcloak transparently decodes advertised content encodings, but retains
// the original Content-Encoding/Content-Length metadata. wireDoer performs
// the legacy ds2api decompression step, so forwarding those headers would make
// it decode an already-decoded body a second time.
func normalizedHTTPCloakHeaders(headers map[string][]string) http.Header {
	out := httpCloakHeaders(headers)
	out.Del("Content-Encoding")
	out.Del("Content-Length")
	return out
}

var _ Doer = (*httpCloakDoer)(nil)
