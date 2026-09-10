package workbuddy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 「实际发往下游的字节」必须留痕 —— 这是定位"插件有值、网关为 0"这类问题的唯一直接证据。
func TestCaptureEmittedUsage(t *testing.T) {
	s := &Service{}
	if got := s.diagnosticsSnapshot()["emittedUsage"]; got != "" {
		t.Fatalf("初始应为空，得到 %v", got)
	}

	// 模拟上游发字符串、归一化后应为裸数字的 chunk
	cleaned := cleanChunkJSON(`{"choices":[],"usage":{"prompt_tokens":100,"prompt_cache_hit_tokens":"80"}}`)
	s.captureEmittedUsage(cleaned)

	emitted, _ := s.diagnosticsSnapshot()["emittedUsage"].(string)
	if !strings.Contains(emitted, `"cached_tokens":80`) {
		t.Fatalf("留痕里没有裸数字 cached_tokens，网关仍会读不到: %s", emitted)
	}
	if strings.Contains(emitted, `"cached_tokens":"80"`) {
		t.Fatalf("留痕里是带引号的数，正则读不到: %s", emitted)
	}
}

// 不含 usage 的 chunk 不该覆盖留痕。
func TestCaptureEmittedUsageIgnoresNonUsageChunks(t *testing.T) {
	s := &Service{}
	s.captureEmittedUsage(cleanChunkJSON(`{"choices":[],"usage":{"prompt_tokens":10,"cached_tokens":5}}`))
	first, _ := s.diagnosticsSnapshot()["emittedUsage"].(string)
	if first == "" {
		t.Fatal("应先留痕")
	}
	s.captureEmittedUsage(`{"choices":[{"delta":{"content":"hi"}}]}`)
	again, _ := s.diagnosticsSnapshot()["emittedUsage"].(string)
	if again != first {
		t.Errorf("无 usage 的 chunk 不应覆盖留痕\n got=%s\nwant=%s", again, first)
	}
}

// /api/workbuddy/v1/_diag 是回环专用的排障面（中继前缀 = AuthInternal，无需凭据）。
func TestDiagEndpointExposesBothSides(t *testing.T) {
	s := newTestService(t)

	// 造一份上游原始快照 + 一份发往下游的字节
	s.observeUsage(`{"choices":[],"usage":{"prompt_tokens":100,"prompt_cache_hit_tokens":"80","credit":0.03}}`)
	s.captureEmittedUsage(cleanChunkJSON(`{"choices":[],"usage":{"prompt_tokens":100,"prompt_cache_hit_tokens":"80","credit":0.03}}`))

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/workbuddy/v1/_diag", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("应返回 200，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var doc struct {
		Live struct {
			UpstreamKeys  []string          `json:"upstreamKeys"`
			UpstreamTypes map[string]string `json:"upstreamTypes"`
			EmittedUsage  string            `json:"emittedUsage"`
		} `json:"live"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Live.UpstreamKeys) == 0 {
		t.Error("应给出上游原始 usage 的字段名")
	}
	if doc.Live.UpstreamTypes["prompt_cache_hit_tokens"] != "string" {
		t.Errorf("应给出上游字段类型，得到 %v", doc.Live.UpstreamTypes)
	}
	if !strings.Contains(doc.Live.EmittedUsage, `"cached_tokens":80`) {
		t.Errorf("应给出归一化后实际发出的 usage，得到 %q", doc.Live.EmittedUsage)
	}

	// 非 GET 拒绝
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/workbuddy/v1/_diag", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("非 GET 应返回 405，得到 %d", rec.Code)
	}
}
