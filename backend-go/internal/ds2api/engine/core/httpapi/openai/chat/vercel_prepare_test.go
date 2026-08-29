package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/files"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
)

func TestIsVercelStreamPrepareRequest(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions?__stream_prepare=1", nil)
	if !isVercelStreamPrepareRequest(req) {
		t.Fatalf("expected prepare request to be detected")
	}

	req2 := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	if isVercelStreamPrepareRequest(req2) {
		t.Fatalf("expected non-prepare request")
	}
}

func TestIsVercelStreamReleaseRequest(t *testing.T) {
	req := httptest.NewRequest("POST", "/v1/chat/completions?__stream_release=1", nil)
	if !isVercelStreamReleaseRequest(req) {
		t.Fatalf("expected release request to be detected")
	}

	req2 := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	if isVercelStreamReleaseRequest(req2) {
		t.Fatalf("expected non-release request")
	}
}

func TestVercelInternalSecret(t *testing.T) {
	t.Run("prefer explicit secret", func(t *testing.T) {
		t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")
		t.Setenv("DS2API_ADMIN_KEY", "admin-fallback")
		if got := vercelInternalSecret(); got != "stream-secret" {
			t.Fatalf("expected explicit secret, got %q", got)
		}
	})

	t.Run("fallback to admin key", func(t *testing.T) {
		t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "")
		t.Setenv("DS2API_ADMIN_KEY", "admin-fallback")
		if got := vercelInternalSecret(); got != "admin-fallback" {
			t.Fatalf("expected admin key fallback, got %q", got)
		}
	})

	t.Run("default admin when env missing", func(t *testing.T) {
		t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "")
		t.Setenv("DS2API_ADMIN_KEY", "")
		if got := vercelInternalSecret(); got != "admin" {
			t.Fatalf("expected default admin fallback, got %q", got)
		}
	})
}

func TestStreamLeaseLifecycle(t *testing.T) {
	h := &Handler{}
	leaseID := h.holdStreamLease(&auth.RequestAuth{UseConfigToken: false}, promptcompat.StandardRequest{}, "test-session-id")
	if leaseID == "" {
		t.Fatalf("expected non-empty lease id")
	}
	if lease, ok := h.releaseStreamLease(leaseID); !ok {
		t.Fatalf("expected lease release success")
	} else if lease.SessionID != "test-session-id" {
		t.Fatalf("expected released session id, got %q", lease.SessionID)
	}
	if _, ok := h.releaseStreamLease(leaseID); ok {
		t.Fatalf("expected duplicate release to fail")
	}
}

func TestStreamLeaseTTL(t *testing.T) {
	t.Setenv("DS2API_VERCEL_STREAM_LEASE_TTL_SECONDS", "120")
	if got := streamLeaseTTL(); got != 120*time.Second {
		t.Fatalf("expected ttl=120s, got %v", got)
	}
	t.Setenv("DS2API_VERCEL_STREAM_LEASE_TTL_SECONDS", "invalid")
	if got := streamLeaseTTL(); got != 15*time.Minute {
		t.Fatalf("expected default ttl on invalid value, got %v", got)
	}
}

func TestHandleVercelStreamPrepareAppliesCurrentInputFile(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	ds := &inlineUploadDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{
			currentInputEnabled: true,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 current input upload, got %d", len(ds.uploadCalls))
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	payload, _ := body["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("expected payload object, got %#v", body["payload"])
	}
	promptText, _ := payload["prompt"].(string)
	if !strings.Contains(promptText, "继续会话") {
		t.Fatalf("expected continuation prompt, got %s", promptText)
	}
	if strings.Contains(promptText, "first user turn") || strings.Contains(promptText, "latest user turn") {
		t.Fatalf("expected original turns hidden from prompt, got %s", promptText)
	}
	refIDs, _ := payload["ref_file_ids"].([]any)
	if len(refIDs) == 0 || refIDs[0] != "file-inline-1" {
		t.Fatalf("expected uploaded history file first in ref_file_ids, got %#v", payload["ref_file_ids"])
	}
}

func TestHandleVercelStreamPrepareUsesHalfwidthEPSEToolPrompt(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	h := &Handler{
		Store: mockOpenAIConfig{},
		Auth:  streamStatusAuthStub{},
		DS:    &inlineUploadDSStub{},
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "search docs"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "search docs",
					"parameters": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query": map[string]any{"type": "string"},
						},
						"required": []any{"query"},
					},
				},
			},
		},
		"stream": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	finalPrompt, _ := body["final_prompt"].(string)
	payload, _ := body["payload"].(map[string]any)
	payloadPrompt, _ := payload["prompt"].(string)
	for label, promptText := range map[string]string{"final_prompt": finalPrompt, "payload.prompt": payloadPrompt} {
		if !strings.Contains(promptText, "<|EPSE|tool_calls>") || !strings.Contains(promptText, `标签语法中所允许使用的标点符号字符集仅限 ASCII 的 < > / = " 以及半角竖线 |。`) {
			t.Fatalf("expected %s to contain halfwidth EPSE tool instructions, got %q", label, promptText)
		}
		if strings.Contains(promptText, "\uff5c") || strings.Contains(promptText, "full"+"width vertical bar") {
			t.Fatalf("expected %s not to contain legacy pipe guidance, got %q", label, promptText)
		}
	}
	toolNames, _ := body["tool_names"].([]any)
	if len(toolNames) != 1 || toolNames[0] != "search" {
		t.Fatalf("expected prepared tool names to align with request tools, got %#v", body["tool_names"])
	}
}

type vercelReleaseAutoDeleteDSStub struct {
	resp             *http.Response
	deleteCallCount  int
	deletedSessionID string
	deletedToken     string
	deleteErr        error
	events           *[]string
}

func (m *vercelReleaseAutoDeleteDSStub) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "session-id", nil
}

func (m *vercelReleaseAutoDeleteDSStub) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "pow", nil
}

func (m *vercelReleaseAutoDeleteDSStub) UploadFile(_ context.Context, _ *auth.RequestAuth, _ dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	return &dsclient.UploadFileResult{ID: "file-id", Filename: "file.txt", Bytes: 1, Status: "uploaded"}, nil
}

func (m *vercelReleaseAutoDeleteDSStub) CallCompletion(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ string, _ int) (*http.Response, error) {
	return m.resp, nil
}

func (m *vercelReleaseAutoDeleteDSStub) StopStream(_ context.Context, _ *auth.RequestAuth, _ string, _ int) error {
	return nil
}

func (m *vercelReleaseAutoDeleteDSStub) FireCompletionAndStop(_ context.Context, _ *auth.RequestAuth, _ map[string]any, _ string) (int, error) {
	return 0, nil
}

func (m *vercelReleaseAutoDeleteDSStub) DeleteSessionForToken(_ context.Context, token string, sessionID string) (*dsclient.DeleteSessionResult, error) {
	if m.events != nil {
		*m.events = append(*m.events, "delete")
	}
	m.deleteCallCount++
	m.deletedSessionID = sessionID
	m.deletedToken = token
	if m.deleteErr != nil {
		return nil, m.deleteErr
	}
	return &dsclient.DeleteSessionResult{SessionID: sessionID, Success: true}, nil
}

func (m *vercelReleaseAutoDeleteDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

type vercelReleaseAuthStub struct {
	events *[]string
}

func (a *vercelReleaseAuthStub) Determine(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{DeepSeekToken: "test-token", AccountID: "test-account"}, nil
}

func (a *vercelReleaseAuthStub) DetermineCaller(_ *http.Request) (*auth.RequestAuth, error) {
	return &auth.RequestAuth{DeepSeekToken: "test-token", AccountID: "test-account"}, nil
}

func (a *vercelReleaseAuthStub) Release(_ *auth.RequestAuth) {
	if a.events != nil {
		*a.events = append(*a.events, "release")
	}
}

func (a *vercelReleaseAuthStub) SetAccountMutedUntil(_ *auth.RequestAuth, _ float64) {}

func (a *vercelReleaseAuthStub) SetAccountBanned(_ *auth.RequestAuth, _ string) {}

func TestHandleVercelStreamReleaseTriggersAutoDelete(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	events := []string{}
	ds := &vercelReleaseAutoDeleteDSStub{events: &events}
	h := &Handler{
		Store: mockOpenAIConfig{
			autoDeleteMode: "single",
		},
		Auth: &vercelReleaseAuthStub{events: &events},
		DS:   ds,
	}

	leaseID := h.holdStreamLease(&auth.RequestAuth{DeepSeekToken: "test-token", AccountID: "test-account"}, promptcompat.StandardRequest{}, "session-to-delete")
	if leaseID == "" {
		t.Fatalf("expected non-empty lease id")
	}

	reqBody := map[string]any{"lease_id": leaseID}
	reqJSON, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_release=1", strings.NewReader(string(reqJSON)))
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.handleVercelStreamRelease(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ds.deleteCallCount != 1 {
		t.Fatalf("expected auto delete call count=1, got %d", ds.deleteCallCount)
	}
	if ds.deletedSessionID != "session-to-delete" {
		t.Fatalf("expected deleted session id=session-to-delete, got %q", ds.deletedSessionID)
	}
	if got, want := strings.Join(events, ","), "delete,release"; got != want {
		t.Fatalf("expected auto-delete before auth release, got %s", got)
	}
}

func TestHandleVercelStreamPrepareUploadsToolsSeparately(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	ds := &inlineUploadDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{currentInputEnabled: true},
		Auth:  streamStatusAuthStub{},
		DS:    ds,
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "search docs"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "search docs",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
		"stream": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 2 {
		t.Fatalf("expected history and tools uploads, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].Filename != "HISTORY.txt" || ds.uploadCalls[1].Filename != "TOOLS.txt" {
		t.Fatalf("unexpected upload filenames: %#v", ds.uploadCalls)
	}
	if strings.Contains(string(ds.uploadCalls[0].Data), "Description: search docs") {
		t.Fatalf("history transcript should not embed tool descriptions, got %q", string(ds.uploadCalls[0].Data))
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	finalPrompt, _ := body["final_prompt"].(string)
	payload, _ := body["payload"].(map[string]any)
	payloadPrompt, _ := payload["prompt"].(string)
	for label, promptText := range map[string]string{"final_prompt": finalPrompt, "payload.prompt": payloadPrompt} {
		if !strings.Contains(promptText, "继续会话") || !strings.Contains(promptText, "工具调用格式规范") {
			t.Fatalf("expected %s to retain continuation and tool instructions, got %q", label, promptText)
		}
		if strings.Contains(promptText, "TOOLS.txt") {
			t.Fatalf("expected %s not to reference tools file by filename, got %q", label, promptText)
		}
		if strings.Contains(promptText, "Description: search docs") {
			t.Fatalf("expected %s not to inline tool descriptions, got %q", label, promptText)
		}
	}
	refIDs, _ := payload["ref_file_ids"].([]any)
	if len(refIDs) < 2 || refIDs[0] != "file-inline-1" || refIDs[1] != "file-inline-2" {
		t.Fatalf("expected history and tools ref ids first, got %#v", payload["ref_file_ids"])
	}
}

func TestHandleVercelStreamPrepareAutoRouteVision(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	ds := &inlineUploadDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{
			aliases:         config.DefaultModelAliases(),
			autoRouteVision: true,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "describe this"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
			}},
		},
		"stream": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 inline upload, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].ModelType != "vision" {
		t.Fatalf("expected upload model_type=vision, got %q", ds.uploadCalls[0].ModelType)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got := body["model"]; got != "deepseek-v4-flash" {
		t.Fatalf("expected response model deepseek-v4-flash, got %v", got)
	}
	payload, _ := body["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("expected payload object, got %#v", body["payload"])
	}
	if modelType := payload["model_type"]; modelType != "vision" {
		t.Fatalf("expected payload model_type=vision, got %v", modelType)
	}
}

func TestHandleVercelStreamPrepareAutoRouteVisionDisabledKeepsOriginalModel(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	ds := &inlineUploadDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{
			aliases:         config.DefaultModelAliases(),
			autoRouteVision: false,
		},
		Auth: streamStatusAuthStub{},
		DS:   ds,
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "describe this"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
			}},
		},
		"stream": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 1 {
		t.Fatalf("expected 1 inline upload, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].ModelType != "default" {
		t.Fatalf("expected upload model_type=default, got %q", ds.uploadCalls[0].ModelType)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got := body["model"]; got != "deepseek-v4-flash" {
		t.Fatalf("expected response model deepseek-v4-flash, got %v", got)
	}
	payload, _ := body["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("expected payload object, got %#v", body["payload"])
	}
	if modelType := payload["model_type"]; modelType != "default" {
		t.Fatalf("expected payload model_type=default, got %v", modelType)
	}
}

func TestHandleVercelStreamPrepareMapsCurrentInputFileManagedAuthFailureTo401(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	ds := &inlineUploadDSStub{
		uploadErr: &dsclient.RequestFailure{Op: "upload file", Kind: dsclient.FailureManagedUnauthorized, Message: "expired token"},
	}
	h := &Handler{
		Store: mockOpenAIConfig{
			currentInputEnabled: true,
		},
		Auth: streamStatusManagedAuthStub{},
		DS:   ds,
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model":    "deepseek-v4-flash",
		"messages": historySplitTestMessages(),
		"stream":   true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer managed-key")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Please re-login the account in admin") {
		t.Fatalf("expected managed auth error message, got %s", rec.Body.String())
	}
}

func TestHandleVercelStreamSwitchReuploadsCurrentInputFile(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"acc1@test.com","password":"pwd"},
			{"email":"acc2@test.com","password":"pwd"}
		]
	}`)
	store := config.LoadStore()
	resolver := auth.NewResolver(store, account.NewPool(store), func(_ context.Context, acc config.Account) (string, error) {
		return "token-" + acc.Identifier(), nil
	})
	authReq := httptest.NewRequest(http.MethodPost, "/", nil)
	authReq.Header.Set("Authorization", "Bearer managed-key")
	a, err := resolver.Determine(authReq)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer resolver.Release(a)

	ds := &inlineUploadDSStub{}
	h := &Handler{
		Store: mockOpenAIConfig{currentInputEnabled: true},
		Auth:  resolver,
		DS:    ds,
	}
	stdReq := promptcompat.StandardRequest{
		RequestedModel:          "deepseek-v4-flash",
		ResolvedModel:           "deepseek-v4-flash",
		ResponseModel:           "deepseek-v4-flash",
		FinalPrompt:             "继续会话 使用工具时请参照说明与格式要求，仅使用所列出的工具",
		PromptTokenText:         "# HISTORY.txt\n\n=== 1. USER ===\nhello\n\n# TOOLS.txt\nAvailable tool descriptions and parameter schemas for this request.\n\nYou have access to these tools:\n\nTool: search\nDescription: search docs\nParameters: {\"type\":\"object\"}\n",
		HistoryText:             "# HISTORY.txt\n\n=== 1. USER ===\nhello\n",
		CurrentInputFileApplied: true,
		CurrentInputFileID:      "file-old",
		CurrentToolsFileID:      "file-old-tools",
		ToolsRaw: []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "search",
					"description": "search docs",
					"parameters":  map[string]any{"type": "object"},
				},
			},
		},
		RefFileIDs: []string{"file-old", "file-old-tools", "client-file"},
		Thinking:   true,
	}
	leaseID := h.holdStreamLease(a, stdReq, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_switch=1", strings.NewReader(`{"lease_id":"`+leaseID+`"}`))
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamSwitch(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.uploadCalls) != 2 {
		t.Fatalf("expected current input and tools reupload on switched account, got %d", len(ds.uploadCalls))
	}
	if ds.uploadCalls[0].Filename != "HISTORY.txt" || ds.uploadCalls[1].Filename != "TOOLS.txt" {
		t.Fatalf("unexpected reupload filenames: %#v", ds.uploadCalls)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if body["deepseek_token"] != "token-acc2@test.com" {
		t.Fatalf("expected switched account token, got %#v", body["deepseek_token"])
	}
	payload, _ := body["payload"].(map[string]any)
	refIDs, _ := payload["ref_file_ids"].([]any)
	if len(refIDs) != 3 || refIDs[0] != "file-inline-1" || refIDs[1] != "file-inline-2" || refIDs[2] != "client-file" {
		t.Fatalf("expected reuploaded current input ref plus client ref, got %#v", payload["ref_file_ids"])
	}
	promptText, _ := payload["prompt"].(string)
	if !strings.Contains(promptText, "继续会话") || !strings.Contains(promptText, "使用工具时请参照说明") {
		t.Fatalf("expected switched payload prompt to retain continuation and tool guidance, got %q", promptText)
	}
	if strings.Contains(promptText, "TOOLS.txt") {
		t.Fatalf("switched payload prompt should not reference tools file by filename, got %q", promptText)
	}
}

func TestHandleVercelStreamPrepareInlinesTextFilesForExpert(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	store := files.NewMemoryContentStore(100<<20, time.Hour)
	if err := store.Store("file-text-1", "notes.txt", "text/plain", []byte("inlined expert text")); err != nil {
		t.Fatalf("store failed: %v", err)
	}

	enabled := true
	h := &Handler{
		Store: mockOpenAIConfig{
			expertTextInlineEnabled: &enabled,
		},
		Auth:         streamStatusAuthStub{},
		DS:           &inlineUploadDSStub{},
		ContentStore: store,
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "read this"},
					map[string]any{"type": "input_file", "file_id": "file-text-1"},
				},
			},
		},
		"stream": true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	finalPrompt, _ := body["final_prompt"].(string)
	if !strings.Contains(finalPrompt, "inlined expert text") {
		t.Fatalf("expected final_prompt to contain inlined file text, got %q", finalPrompt)
	}
	payload, _ := body["payload"].(map[string]any)
	refIDs, _ := payload["ref_file_ids"].([]any)
	if len(refIDs) != 0 {
		t.Fatalf("expected expert payload ref_file_ids empty, got %#v", refIDs)
	}
}

type vercelSegmentDSStub struct {
	fireCalls   []map[string]any
	callPayload map[string]any
	createCalls int
}

func (m *vercelSegmentDSStub) CreateSession(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	m.createCalls++
	return fmt.Sprintf("session-%d", m.createCalls), nil
}

func (m *vercelSegmentDSStub) GetPow(_ context.Context, _ *auth.RequestAuth, _ int) (string, error) {
	return "pow", nil
}

func (m *vercelSegmentDSStub) UploadFile(_ context.Context, _ *auth.RequestAuth, _ dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	return &dsclient.UploadFileResult{ID: "file-uploaded", Filename: "f.txt", Bytes: 1, Status: "uploaded"}, nil
}

func (m *vercelSegmentDSStub) CallCompletion(_ context.Context, _ *auth.RequestAuth, payload map[string]any, _ string, _ int) (*http.Response, error) {
	m.callPayload = payload
	return makeOpenAISSEHTTPResponse(
		`data: {"p":"response/content","v":"ok"}`,
		`data: [DONE]`,
	), nil
}

func (m *vercelSegmentDSStub) StopStream(_ context.Context, _ *auth.RequestAuth, _ string, _ int) error {
	return nil
}

func (m *vercelSegmentDSStub) FireCompletionAndStop(_ context.Context, _ *auth.RequestAuth, payload map[string]any, _ string) (int, error) {
	m.fireCalls = append(m.fireCalls, payload)
	return 42, nil
}

func (m *vercelSegmentDSStub) DeleteSessionForToken(_ context.Context, _ string, _ string) (*dsclient.DeleteSessionResult, error) {
	return &dsclient.DeleteSessionResult{Success: true}, nil
}

func (m *vercelSegmentDSStub) DeleteAllSessionsForToken(_ context.Context, _ string) error {
	return nil
}

func bigExpertTextFileRequest(t *testing.T, contentStore *files.MemoryContentStore) string {
	t.Helper()
	bigText := strings.Repeat("这是很长的一段文件内容，用于模拟拆分上传的大文件。", 20000)
	if err := contentStore.Store("file-big-1", "big.txt", "text/plain", []byte(bigText)); err != nil {
		t.Fatalf("store failed: %v", err)
	}
	reqBody, err := json.Marshal(map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "read this"},
					map[string]any{"type": "input_file", "file_id": "file-big-1"},
				},
			},
		},
		"stream": true,
	})
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	return string(reqBody)
}

// TestHandleVercelStreamPrepareSegmentsExpertPrompt verifies that oversized
// expert prompts (e.g. after text file inlining) are split into segments on the
// Vercel prepare path: all but the last segment are fired-and-stopped in Go and
// only the final segment payload is handed to the Node stream layer.
func TestHandleVercelStreamPrepareSegmentsExpertPrompt(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")
	t.Setenv("DS2API_CONFIG_JSON", `{"keys":["k"],"accounts":[{"email":"a@b.c","password":"p"}]}`)
	store := config.LoadStore()

	contentStore := files.NewMemoryContentStore(100<<20, time.Hour)
	ds := &vercelSegmentDSStub{}
	h := &Handler{
		Store:        store,
		Auth:         streamStatusAuthStub{},
		DS:           ds,
		ContentStore: contentStore,
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(bigExpertTextFileRequest(t, contentStore)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if len(ds.fireCalls) == 0 {
		t.Fatalf("expected segmented expert prompt to fire-and-stop earlier segments")
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	payload, _ := body["payload"].(map[string]any)
	promptText, _ := payload["prompt"].(string)
	if len([]rune(promptText)) > store.ExpertPromptSegmentMaxChars() {
		t.Fatalf("expected final segment payload prompt within max_chars, got %d runes", len([]rune(promptText)))
	}
	if parentID, _ := payload["parent_message_id"].(float64); parentID != 42 {
		t.Fatalf("expected final segment payload parent_message_id=42, got %#v", payload["parent_message_id"])
	}
	if len(ds.fireCalls) > 1 {
		for i, fire := range ds.fireCalls {
			if i == 0 {
				continue
			}
			if got := payloadParentMessageID(fire); got != 42 {
				t.Fatalf("expected non-first fired segments to chain parent_message_id=42, got %#v", fire["parent_message_id"])
			}
		}
	}
}

func payloadParentMessageID(payload map[string]any) float64 {
	switch v := payload["parent_message_id"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

// TestHandleVercelStreamSwitchSegmentsExpertPrompt verifies that the Vercel
// switch path re-segments the stored expert request on the new account and
// returns the final segment payload with a fresh session.
func TestHandleVercelStreamSwitchSegmentsExpertPrompt(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")
	t.Setenv("DS2API_CONFIG_JSON", `{
		"keys":["managed-key"],
		"accounts":[
			{"email":"acc1@test.com","password":"pwd"},
			{"email":"acc2@test.com","password":"pwd"}
		]
	}`)
	store := config.LoadStore()
	resolver := auth.NewResolver(store, account.NewPool(store), func(_ context.Context, acc config.Account) (string, error) {
		return "token-" + acc.Identifier(), nil
	})
	authReq := httptest.NewRequest(http.MethodPost, "/", nil)
	authReq.Header.Set("Authorization", "Bearer managed-key")
	a, err := resolver.Determine(authReq)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer resolver.Release(a)

	contentStore := files.NewMemoryContentStore(100<<20, time.Hour)
	prepDS := &vercelSegmentDSStub{}
	h := &Handler{
		Store:        store,
		Auth:         resolver,
		DS:           prepDS,
		ContentStore: contentStore,
	}

	prep := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(bigExpertTextFileRequest(t, contentStore)))
	prep.Header.Set("Authorization", "Bearer managed-key")
	prep.Header.Set("Content-Type", "application/json")
	prep.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()
	h.handleVercelStreamPrepare(rec, prep)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected prepare 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var prepBody map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&prepBody); err != nil {
		t.Fatalf("prepare decode failed: %v", err)
	}
	leaseID, _ := prepBody["lease_id"].(string)
	if leaseID == "" {
		t.Fatalf("expected lease_id in prepare response")
	}
	prepareFireCount := len(prepDS.fireCalls)
	if prepareFireCount == 0 {
		t.Fatalf("expected segmented prepare to fire earlier segments")
	}

	switchDS := &vercelSegmentDSStub{}
	h.DS = switchDS
	switchReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_switch=1", strings.NewReader(`{"lease_id":"`+leaseID+`"}`))
	switchReq.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec2 := httptest.NewRecorder()
	h.handleVercelStreamSwitch(rec2, switchReq)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected switch 200, got %d body=%s", rec2.Code, rec2.Body.String())
	}
	if len(switchDS.fireCalls) != prepareFireCount {
		t.Fatalf("expected switch to re-fire %d segments on the new account, got %d", prepareFireCount, len(switchDS.fireCalls))
	}
	var switchBody map[string]any
	if err := json.NewDecoder(rec2.Body).Decode(&switchBody); err != nil {
		t.Fatalf("switch decode failed: %v", err)
	}
	preparedToken, _ := prepBody["deepseek_token"].(string)
	switchedToken, _ := switchBody["deepseek_token"].(string)
	if switchedToken == "" || switchedToken == preparedToken {
		t.Fatalf("expected switch to move to a different account, prepared=%q switched=%q", preparedToken, switchedToken)
	}
	if switchedToken != "token-acc1@test.com" && switchedToken != "token-acc2@test.com" {
		t.Fatalf("unexpected switched account token %q", switchedToken)
	}
	payload, _ := switchBody["payload"].(map[string]any)
	promptText, _ := payload["prompt"].(string)
	if len([]rune(promptText)) > store.ExpertPromptSegmentMaxChars() {
		t.Fatalf("expected switched final segment prompt within max_chars, got %d runes", len([]rune(promptText)))
	}
	if parentID, _ := payload["parent_message_id"].(float64); parentID != 42 {
		t.Fatalf("expected switched final segment parent_message_id=42, got %#v", payload["parent_message_id"])
	}
}

func TestHandleVercelStreamPrepareInlinesTopLevelFileIDsForExpert(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DS2API_VERCEL_INTERNAL_SECRET", "stream-secret")

	store := files.NewMemoryContentStore(100<<20, time.Hour)
	if err := store.Store("file-top-1", "notes.txt", "text/plain", []byte("top level inlined content")); err != nil {
		t.Fatalf("store failed: %v", err)
	}

	enabled := true
	h := &Handler{
		Store: mockOpenAIConfig{
			expertTextInlineEnabled: &enabled,
		},
		Auth:         streamStatusAuthStub{},
		DS:           &inlineUploadDSStub{},
		ContentStore: store,
	}

	reqBody, _ := json.Marshal(map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "summarize"},
		},
		"file_ids": []any{"file-top-1"},
		"stream":   true,
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions?__stream_prepare=1", strings.NewReader(string(reqBody)))
	req.Header.Set("Authorization", "Bearer direct-token")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ds2-Internal-Token", "stream-secret")
	rec := httptest.NewRecorder()

	h.handleVercelStreamPrepare(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	finalPrompt, _ := body["final_prompt"].(string)
	if !strings.Contains(finalPrompt, "top level inlined content") {
		t.Fatalf("expected final_prompt to contain top-level file content, got %q", finalPrompt)
	}
}
