package completionruntime

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/account"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
)

type fakeDeepSeekCaller struct {
	responses          []*http.Response
	payloads           []map[string]any
	uploads            []dsclient.UploadFileRequest
	completionAccounts []string
	sessionByAccount   bool
	fireAndStopCounter int
	continueCalls      []continueCallInfo
	deletedSessions    []string
}

type continueCallInfo struct {
	SessionID string
	MessageID int
}

type currentInputRuntimeConfig struct{}

func (currentInputRuntimeConfig) CurrentInputFileEnabled() bool { return true }
func (currentInputRuntimeConfig) CurrentInputFileMinChars() int { return 0 }

func (f *fakeDeepSeekCaller) CreateSession(_ context.Context, a *auth.RequestAuth, _ int) (string, error) {
	if f.sessionByAccount && a != nil && a.AccountID != "" {
		return "session-" + a.AccountID, nil
	}
	return "session-1", nil
}

func (f *fakeDeepSeekCaller) GetPow(context.Context, *auth.RequestAuth, int) (string, error) {
	return "pow", nil
}

func (f *fakeDeepSeekCaller) UploadFile(_ context.Context, a *auth.RequestAuth, req dsclient.UploadFileRequest, _ int) (*dsclient.UploadFileResult, error) {
	f.uploads = append(f.uploads, req)
	if a != nil && a.AccountID != "" {
		return &dsclient.UploadFileResult{ID: "file-runtime-" + a.AccountID}, nil
	}
	return &dsclient.UploadFileResult{ID: "file-runtime-1"}, nil
}

func (f *fakeDeepSeekCaller) CallCompletion(_ context.Context, a *auth.RequestAuth, payload map[string]any, _ string, _ int) (*http.Response, error) {
	f.payloads = append(f.payloads, payload)
	if a != nil {
		f.completionAccounts = append(f.completionAccounts, a.AccountID)
	}
	if len(f.responses) == 0 {
		return sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"fallback"}`), nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeDeepSeekCaller) StopStream(_ context.Context, _ *auth.RequestAuth, _ string, _ int) error {
	return nil
}

func (f *fakeDeepSeekCaller) ContinueCompletion(_ context.Context, a *auth.RequestAuth, sessionID string, messageID int, _ string) (*http.Response, error) {
	f.continueCalls = append(f.continueCalls, continueCallInfo{SessionID: sessionID, MessageID: messageID})
	if a != nil {
		f.completionAccounts = append(f.completionAccounts, a.AccountID)
	}
	if len(f.responses) == 0 {
		return sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"resumed-fallback"}`), nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeDeepSeekCaller) FireCompletionAndStop(_ context.Context, a *auth.RequestAuth, payload map[string]any, _ string) (int, error) {
	f.payloads = append(f.payloads, payload)
	if a != nil {
		f.completionAccounts = append(f.completionAccounts, a.AccountID)
	}
	f.fireAndStopCounter++
	return 100 + f.fireAndStopCounter, nil
}

func (f *fakeDeepSeekCaller) DeleteSession(_ context.Context, _ *auth.RequestAuth, sessionID string, _ int) (*dsclient.DeleteSessionResult, error) {
	f.deletedSessions = append(f.deletedSessions, sessionID)
	return &dsclient.DeleteSessionResult{SessionID: sessionID, Success: true}, nil
}

// repairCountingResponse is an SSE body wrapper that increments a counter each
// time the repair completion's body is consumed, so tests can assert a single
// output only triggers a single LLM repair (H1).

// TestExecuteNonStreamWithRetryRepairsOnce verifies H1: a non-stream request
// that hits "intent true + 0 calls" runs the LLM repair exactly once, not
// twice. The repair invoker is the deepseek-v4-pro completion, which we detect
// by the toolcall.repair session creation via a dedicated response.
func TestExecuteNonStreamWithRetryRepairsOnce(t *testing.T) {
	bad := `<|EPSE|call name=\"Bash\"><|EPSE|parameter name=\"command\"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|call>`
	repaired := `<|EPSE|tool_calls><|EPSE|invoke name=\"Bash\"><|EPSE|parameter name=\"command\"><![CDATA[DS_SLOT_0]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		// main completion emits bad tool-call text
		sseHTTPResponse(http.StatusOK, `data: {"response_message_id":7,"p":"response/content","v":"`+bad+`"}`),
		// first (and only expected) repair completion
		sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"`+repaired+`"}`),
		// a second repair completion would be consumed here if H1 regressed
		sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"`+repaired+`"}`),
	}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
		ToolNames:       []string{"Bash"},
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{
		ToolCallRepairEnabled: true,
	})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if len(result.Turn.ToolCalls) != 1 || result.Turn.ToolCalls[0].Name != "Bash" {
		t.Fatalf("expected one repaired Bash call, got %#v", result.Turn.ToolCalls)
	}
	// Exactly 2 completions: the main one + a single repair. A third completion
	// (second repair) means H1 regressed.
	if len(ds.payloads) != 2 {
		t.Fatalf("expected 2 completions (main + single repair), got %d", len(ds.payloads))
	}
	// The single repair session must be deleted (M2).
	if len(ds.deletedSessions) != 1 {
		t.Fatalf("expected 1 deleted repair session, got %d (%v)", len(ds.deletedSessions), ds.deletedSessions)
	}
}

func TestExecuteNonStreamWithRetryBuildsCanonicalTurn(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(
		http.StatusOK,
		`data: {"response_message_id":42,"p":"response/content","v":"<tool_calls><invoke name=\"Write\"><parameter name=\"content\">{\"x\":1}</parameter></invoke></tool_calls>"}`,
	)}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
		ToolNames:       []string{"Write"},
		ToolsRaw: []any{map[string]any{
			"name": "Write",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
				},
			},
		}},
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if result.SessionID != "session-1" {
		t.Fatalf("session mismatch: %q", result.SessionID)
	}
	if got := result.Turn.ResponseMessageID; got != 42 {
		t.Fatalf("response message id mismatch: %d", got)
	}
	if len(result.Turn.ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(result.Turn.ToolCalls))
	}
	if _, ok := result.Turn.ToolCalls[0].Input["content"].(string); !ok {
		t.Fatalf("expected schema-normalized string argument, got %#v", result.Turn.ToolCalls[0].Input["content"])
	}
	if result.Turn.Usage.InputTokens == 0 || result.Turn.Usage.TotalTokens == 0 {
		t.Fatalf("expected usage to be populated, got %#v", result.Turn.Usage)
	}
}

func TestExecuteNonStreamWithRetrySwitchesManagedAccountBeforeFinal429(t *testing.T) {
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
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	a, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer resolver.Release(a)

	ds := &fakeDeepSeekCaller{
		sessionByAccount: true,
		responses: []*http.Response{
			sseHTTPResponse(http.StatusOK, `data: {"response_message_id":11,"p":"response/thinking_content","v":"first empty"}`),
			sseHTTPResponse(http.StatusOK, `data: {"response_message_id":12,"p":"response/thinking_content","v":"retry empty"}`),
			sseHTTPResponse(http.StatusOK, `data: {"response_message_id":21,"p":"response/content","v":"ok from second account"}`),
		},
	}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
		Thinking:        true,
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, a, stdReq, Options{RetryEnabled: true})
	if outErr != nil {
		t.Fatalf("unexpected output error after account switch retry: %#v", outErr)
	}
	if result.Turn.Text != "ok from second account" {
		t.Fatalf("text mismatch after switch retry: %q", result.Turn.Text)
	}
	if result.SessionID != "session-acc2@test.com" {
		t.Fatalf("expected switched account session, got %q", result.SessionID)
	}
	wantAccounts := []string{"acc1@test.com", "acc1@test.com", "acc2@test.com"}
	if len(ds.completionAccounts) != len(wantAccounts) {
		t.Fatalf("completion account count mismatch: got %v want %v", ds.completionAccounts, wantAccounts)
	}
	for i, want := range wantAccounts {
		if ds.completionAccounts[i] != want {
			t.Fatalf("completion account %d = %q want %q (all=%v)", i, ds.completionAccounts[i], want, ds.completionAccounts)
		}
	}
	if got := ds.payloads[2]["chat_session_id"]; got != "session-acc2@test.com" {
		t.Fatalf("switched payload session mismatch: %#v", got)
	}
	if prompt, _ := ds.payloads[2]["prompt"].(string); strings.Contains(prompt, "Previous reply had no visible output") {
		t.Fatalf("expected fresh switched-account prompt without empty-output suffix, got %q", prompt)
	}
}

func TestExecuteNonStreamWithRetryReuploadsCurrentInputFileAfterAccountSwitch(t *testing.T) {
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
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer managed-key")
	a, err := resolver.Determine(req)
	if err != nil {
		t.Fatalf("determine failed: %v", err)
	}
	defer resolver.Release(a)

	ds := &fakeDeepSeekCaller{
		sessionByAccount: true,
		responses: []*http.Response{
			sseHTTPResponse(http.StatusOK, `data: {"response_message_id":11,"p":"response/thinking_content","v":"first empty"}`),
			sseHTTPResponse(http.StatusOK, `data: {"response_message_id":12,"p":"response/thinking_content","v":"retry empty"}`),
			sseHTTPResponse(http.StatusOK, `data: {"response_message_id":21,"p":"response/content","v":"ok from second account"}`),
		},
	}
	stdReq := promptcompat.StandardRequest{
		Surface:        "test",
		RequestedModel: "deepseek-v4-flash",
		ResolvedModel:  "deepseek-v4-flash",
		ResponseModel:  "deepseek-v4-flash",
		Messages: []any{
			map[string]any{"role": "user", "content": "large current input"},
		},
		PromptTokenText: "large current input",
		FinalPrompt:     "large current input",
		Thinking:        true,
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, a, stdReq, Options{
		RetryEnabled:     true,
		CurrentInputFile: currentInputRuntimeConfig{},
	})
	if outErr != nil {
		t.Fatalf("unexpected output error after account switch retry: %#v", outErr)
	}
	if result.Turn.Text != "ok from second account" {
		t.Fatalf("text mismatch after switch retry: %q", result.Turn.Text)
	}
	if len(ds.uploads) != 2 {
		t.Fatalf("expected current input file uploaded once per account, got %d", len(ds.uploads))
	}
	refIDs, _ := ds.payloads[2]["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-runtime-acc2@test.com" {
		t.Fatalf("expected switched account ref_file_ids to use reuploaded file, got %#v", ds.payloads[2]["ref_file_ids"])
	}
}

func TestExecuteNonStreamWithRetryUsesParentMessageForEmptyRetry(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		sseHTTPResponse(http.StatusOK, `data: {"response_message_id":77,"p":"response/thinking_content","v":"plan"}`),
		sseHTTPResponse(http.StatusOK, `data: {"response_message_id":78,"p":"response/content","v":"ok"}`),
	}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{RetryEnabled: true})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if result.Attempts != 1 {
		t.Fatalf("expected one retry, got %d", result.Attempts)
	}
	if len(ds.payloads) != 2 {
		t.Fatalf("expected two completion calls, got %d", len(ds.payloads))
	}
	if got := ds.payloads[1]["parent_message_id"]; got != 77 {
		t.Fatalf("retry parent_message_id mismatch: %#v", got)
	}
	if result.Turn.Text != "ok" {
		t.Fatalf("retry text mismatch: %q", result.Turn.Text)
	}
}

func TestExecuteNonStreamWithRetryFallsBackToPayloadParentWhenAttemptHasNoMessageID(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		sseHTTPResponse(http.StatusOK, `data: {"p":"response/thinking_content","v":"plan"}`),
		sseHTTPResponse(http.StatusOK, `data: {"response_message_id":45,"p":"response/content","v":"ok"}`),
	}}
	start := StartResult{
		SessionID: "session-1",
		Payload:   map[string]any{"prompt": "final prompt", "parent_message_id": 44},
		Pow:       "pow",
		Response:  ds.responses[0],
		Request: promptcompat.StandardRequest{
			Surface:         "test",
			ResponseModel:   "deepseek-v4-flash",
			PromptTokenText: "prompt",
			FinalPrompt:     "final prompt",
		},
	}
	ds.responses = ds.responses[1:]

	result, outErr := ExecuteNonStreamStartedWithRetry(context.Background(), ds, &auth.RequestAuth{}, start, Options{RetryEnabled: true})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if result.Attempts != 1 {
		t.Fatalf("expected one retry, got %d", result.Attempts)
	}
	if len(ds.payloads) != 1 {
		t.Fatalf("expected one retry completion call, got %d", len(ds.payloads))
	}
	if got := ds.payloads[0]["parent_message_id"]; got != 44 {
		t.Fatalf("retry parent_message_id mismatch: %#v", got)
	}
	if result.Turn.Text != "ok" {
		t.Fatalf("retry text mismatch: %q", result.Turn.Text)
	}
}

func TestExecuteNonStreamWithRetryConvertsReferenceMarkers(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(
		http.StatusOK,
		`data: {"p":"response/content","v":"答案[reference:0]。","citation":{"cite_index":0,"url":"https://example.com/ref"}}`,
	)}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test",
		ResponseModel:   "deepseek-v4-flash-search",
		PromptTokenText: "prompt",
		FinalPrompt:     "final prompt",
		Search:          true,
	}

	result, outErr := ExecuteNonStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, stdReq, Options{})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	want := "答案[0](https://example.com/ref)。"
	if result.Turn.Text != want {
		t.Fatalf("text mismatch: got %q want %q", result.Turn.Text, want)
	}
}

func TestStartCompletionAppliesCurrentInputFileGlobally(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"ok"}`)}}
	stdReq := promptcompat.StandardRequest{
		Surface:         "test_adapter",
		RequestedModel:  "deepseek-v4-flash",
		ResolvedModel:   "deepseek-v4-flash",
		ResponseModel:   "deepseek-v4-flash",
		PromptTokenText: "first user turn",
		FinalPrompt:     "first user turn",
		Messages: []any{
			map[string]any{"role": "user", "content": "first user turn"},
		},
	}

	start, outErr := StartCompletion(context.Background(), ds, &auth.RequestAuth{DeepSeekToken: "token"}, stdReq, Options{
		CurrentInputFile: currentInputRuntimeConfig{},
	})
	if outErr != nil {
		t.Fatalf("unexpected output error: %#v", outErr)
	}
	if len(ds.uploads) != 1 {
		t.Fatalf("expected current input upload, got %d", len(ds.uploads))
	}
	if got := ds.uploads[0].Filename; got != "HISTORY.txt" {
		t.Fatalf("upload filename=%q want HISTORY.txt", got)
	}
	if len(ds.payloads) != 1 {
		t.Fatalf("expected one completion payload, got %d", len(ds.payloads))
	}
	refIDs, _ := ds.payloads[0]["ref_file_ids"].([]any)
	if len(refIDs) != 1 || refIDs[0] != "file-runtime-1" {
		t.Fatalf("expected uploaded file id in ref_file_ids, got %#v", ds.payloads[0]["ref_file_ids"])
	}
	prompt, _ := ds.payloads[0]["prompt"].(string)
	if !strings.Contains(prompt, "继续会话") {
		t.Fatalf("expected continuation prompt, got %q", prompt)
	}
	if !start.Request.CurrentInputFileApplied || !strings.Contains(start.Request.PromptTokenText, "# HISTORY.txt") {
		t.Fatalf("expected prepared request to carry current input file state, got %#v", start.Request)
	}
}

func sseHTTPResponse(status int, lines ...string) *http.Response {
	body := strings.Join(lines, "\n")
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
