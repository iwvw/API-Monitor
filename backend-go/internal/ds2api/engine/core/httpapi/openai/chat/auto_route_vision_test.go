package chat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func TestChatCompletionsAutoRouteVisionUsesVisionModelTypeAndPreservesResponseModel(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	ds := &inlineUploadDSStub{
		completionResp: makeOpenAISSEHTTPResponse(
			`data: {"p":"response/content","v":"vision response"}`,
			`data: [DONE]`,
		),
	}
	h := &Handler{
		Store: mockOpenAIConfig{
			aliases:         config.DefaultModelAliases(),
			autoRouteVision: true,
		},
		Auth:        streamStatusAuthStub{},
		DS:          ds,
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 inline upload, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].ModelType != "vision" {
		t.Fatalf("expected upload model_type=vision, got %q", ds.uploadCalls[0].ModelType)
	}
	payload := ds.completionReq
	if payload == nil {
		t.Fatal("expected completion payload to be captured")
	}
	if modelType := payload["model_type"]; modelType != "vision" {
		t.Fatalf("expected completion model_type=vision, got %v", modelType)
	}

	out := decodeJSONBody(t, rec.Body.String())
	if got := out["model"]; got != "deepseek-v4-flash" {
		t.Fatalf("expected response model deepseek-v4-flash, got %v", got)
	}
}

func TestChatCompletionsAutoRouteVisionDisabledKeepsOriginalModel(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	ds := &inlineUploadDSStub{
		completionResp: makeOpenAISSEHTTPResponse(
			`data: {"p":"response/content","v":"flash response"}`,
			`data: [DONE]`,
		),
	}
	h := &Handler{
		Store: mockOpenAIConfig{
			aliases:         config.DefaultModelAliases(),
			autoRouteVision: false,
		},
		Auth:        streamStatusAuthStub{},
		DS:          ds,
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 inline upload, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].ModelType != "default" {
		t.Fatalf("expected upload model_type=default, got %q", ds.uploadCalls[0].ModelType)
	}
	payload := ds.completionReq
	if payload == nil {
		t.Fatal("expected completion payload to be captured")
	}
	if modelType := payload["model_type"]; modelType != "default" {
		t.Fatalf("expected completion model_type=default, got %v", modelType)
	}
}

func TestChatCompletionsAutoRouteVisionNoImageKeepsOriginalModel(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	ds := &inlineUploadDSStub{
		completionResp: makeOpenAISSEHTTPResponse(
			`data: {"p":"response/content","v":"flash response"}`,
			`data: [DONE]`,
		),
	}
	h := &Handler{
		Store: mockOpenAIConfig{
			aliases:         config.DefaultModelAliases(),
			autoRouteVision: true,
		},
		Auth:        streamStatusAuthStub{},
		DS:          ds,
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 0 {
		t.Fatalf("expected no inline uploads, got %d", len(ds.uploadCalls))
	}
	payload := ds.completionReq
	if payload == nil {
		t.Fatal("expected completion payload to be captured")
	}
	if modelType := payload["model_type"]; modelType != "default" {
		t.Fatalf("expected completion model_type=default, got %v", modelType)
	}
}

func TestChatCompletionsAutoRouteVisionStripsImageBlocks(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	ds := &inlineUploadDSStub{
		completionResp: makeOpenAISSEHTTPResponse(
			`data: {"p":"response/content","v":"vision response"}`,
			`data: [DONE]`,
		),
	}
	h := &Handler{
		Store: mockOpenAIConfig{
			aliases:         config.DefaultModelAliases(),
			autoRouteVision: true,
		},
		Auth:        streamStatusAuthStub{},
		DS:          ds,
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	snapshot, err := historyStore.Snapshot()
	if err != nil {
		t.Fatalf("snapshot failed: %v", err)
	}
	if len(snapshot.Items) != 1 {
		t.Fatalf("expected one history item, got %d", len(snapshot.Items))
	}
	full, err := historyStore.Get(snapshot.Items[0].ID)
	if err != nil {
		t.Fatalf("expected detail item, got %v", err)
	}
	if len(full.Messages) != 1 {
		t.Fatalf("expected one message persisted after stripping image, got %d", len(full.Messages))
	}
}

func TestChatCompletionsAutoRouteVisionPreservesNoThinking(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	ds := &inlineUploadDSStub{
		completionResp: makeOpenAISSEHTTPResponse(
			`data: {"p":"response/content","v":"vision response"}`,
			`data: [DONE]`,
		),
	}
	h := &Handler{
		Store: mockOpenAIConfig{
			aliases:         config.DefaultModelAliases(),
			autoRouteVision: true,
		},
		Auth:        streamStatusAuthStub{},
		DS:          ds,
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"deepseek-v4-pro-nothinking","messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	payload := ds.completionReq
	if payload == nil {
		t.Fatal("expected completion payload to be captured")
	}
	if payload["model_type"] != "vision" {
		t.Fatalf("expected completion model_type=vision, got %v", payload["model_type"])
	}
	if payload["thinking_enabled"] != false {
		t.Fatalf("expected thinking_enabled=false for nothinking, got %v", payload["thinking_enabled"])
	}
	out := decodeJSONBody(t, rec.Body.String())
	if got := out["model"]; got != "deepseek-v4-pro-nothinking" {
		t.Fatalf("expected response model deepseek-v4-pro-nothinking, got %v", got)
	}
}

func TestChatCompletionsAutoRouteVisionAlias(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	ds := &inlineUploadDSStub{
		completionResp: makeOpenAISSEHTTPResponse(
			`data: {"p":"response/content","v":"vision response"}`,
			`data: [DONE]`,
		),
	}
	h := &Handler{
		Store: mockOpenAIConfig{
			aliases:         config.DefaultModelAliases(),
			autoRouteVision: true,
		},
		Auth:        streamStatusAuthStub{},
		DS:          ds,
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	payload := ds.completionReq
	if payload["model_type"] != "vision" {
		t.Fatalf("expected completion model_type=vision, got %v", payload["model_type"])
	}
	out := decodeJSONBody(t, rec.Body.String())
	if got := out["model"]; got != "gpt-4o" {
		t.Fatalf("expected response model gpt-4o, got %v", got)
	}
}

func TestChatCompletionsAutoRouteVisionFollowUpWithoutImageReturnsToOriginalModel(t *testing.T) {
	historyStore := newTestChatHistoryStore(t)
	ds := &inlineUploadDSStub{
		completionResp: makeOpenAISSEHTTPResponse(
			`data: {"p":"response/content","v":"flash follow-up"}`,
			`data: [DONE]`,
		),
	}
	h := &Handler{
		Store: mockOpenAIConfig{
			aliases:         config.DefaultModelAliases(),
			autoRouteVision: true,
		},
		Auth:        streamStatusAuthStub{},
		DS:          ds,
		ChatHistory: historyStore,
	}

	reqBody := `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]},{"role":"assistant","content":"I see"},{"role":"user","content":"now text only"}],"stream":false}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ChatCompletions(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	payload := ds.completionReq
	if payload == nil {
		t.Fatal("expected completion payload to be captured")
	}
	if payload["model_type"] != "default" {
		t.Fatalf("expected completion model_type=default for follow-up, got %v", payload["model_type"])
	}
	out := decodeJSONBody(t, rec.Body.String())
	if got := out["model"]; got != "deepseek-v4-flash" {
		t.Fatalf("expected response model deepseek-v4-flash, got %v", got)
	}
}
