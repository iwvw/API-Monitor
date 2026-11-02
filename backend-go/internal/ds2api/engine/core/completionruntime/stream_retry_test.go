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
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/shared"
)

func TestExecuteStreamWithRetryUsesSharedRetryPayloadAndUsagePrompt(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"ok"}`),
	}}
	initial := sseHTTPResponse(http.StatusOK, `data: {"response_message_id":77,"p":"response/thinking_content","v":"plan"}`)
	payload := map[string]any{"prompt": "original prompt"}
	attemptsSeen := 0
	retryPrompt := ""

	ExecuteStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, initial, payload, "pow", StreamRetryOptions{
		Surface:      "test.stream",
		Stream:       true,
		RetryEnabled: true,
		UsagePrompt:  "original prompt",
	}, StreamRetryHooks{
		ConsumeAttempt: func(resp *http.Response, allowDeferEmpty bool) ConsumeAttemptResult {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Fatalf("close failed: %v", err)
				}
			}()
			_, _ = io.ReadAll(resp.Body)
			attemptsSeen++
			return ConsumeAttemptResult{Terminal: attemptsSeen == 2, Retryable: attemptsSeen == 1 && allowDeferEmpty}
		},
		ParentMessageID: func() int {
			return 77
		},
		OnRetryPrompt: func(prompt string) {
			retryPrompt = prompt
		},
	})

	if attemptsSeen != 2 {
		t.Fatalf("expected two stream attempts, got %d", attemptsSeen)
	}
	if len(ds.payloads) != 1 {
		t.Fatalf("expected one retry completion call, got %d", len(ds.payloads))
	}
	if got := ds.payloads[0]["parent_message_id"]; got != 77 {
		t.Fatalf("retry parent_message_id mismatch: %#v", got)
	}
	if prompt, _ := ds.payloads[0]["prompt"].(string); !strings.Contains(prompt, shared.EmptyOutputRetrySuffix) {
		t.Fatalf("expected retry suffix in payload prompt, got %q", prompt)
	}
	if !strings.Contains(retryPrompt, shared.EmptyOutputRetrySuffix) {
		t.Fatalf("expected retry suffix in usage prompt, got %q", retryPrompt)
	}
}

func TestExecuteStreamWithRetryFallsBackToPayloadParentWhenAttemptHasNoMessageID(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		sseHTTPResponse(http.StatusOK, `data: {"p":"response/content","v":"ok"}`),
	}}
	initial := sseHTTPResponse(http.StatusOK, `data: {"p":"response/thinking_content","v":"plan"}`)
	payload := map[string]any{"prompt": "original prompt", "parent_message_id": 44}
	attemptsSeen := 0

	ExecuteStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, initial, payload, "pow", StreamRetryOptions{
		Surface:      "test.stream",
		Stream:       true,
		RetryEnabled: true,
		UsagePrompt:  "original prompt",
	}, StreamRetryHooks{
		ConsumeAttempt: func(resp *http.Response, allowDeferEmpty bool) ConsumeAttemptResult {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Fatalf("close failed: %v", err)
				}
			}()
			_, _ = io.ReadAll(resp.Body)
			attemptsSeen++
			return ConsumeAttemptResult{Terminal: attemptsSeen == 2, Retryable: attemptsSeen == 1 && allowDeferEmpty}
		},
		ParentMessageID: func() int {
			return 0
		},
	})

	if len(ds.payloads) != 1 {
		t.Fatalf("expected one retry completion call, got %d", len(ds.payloads))
	}
	if got := ds.payloads[0]["parent_message_id"]; got != 44 {
		t.Fatalf("retry parent_message_id mismatch: %#v", got)
	}
}

func TestExecuteStreamWithRetrySwitchesManagedAccountBeforeFinal429(t *testing.T) {
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
			sseHTTPResponse(http.StatusOK, `data: {"response_message_id":12,"p":"response/thinking_content","v":"retry empty"}`),
			sseHTTPResponse(http.StatusOK, `data: {"response_message_id":21,"p":"response/content","v":"ok from second account"}`),
		},
	}
	initial := sseHTTPResponse(http.StatusOK, `data: {"response_message_id":11,"p":"response/thinking_content","v":"first empty"}`)
	payload := map[string]any{"prompt": "original prompt", "chat_session_id": "session-acc1@test.com"}
	attemptsSeen := 0
	switchedSession := ""

	ExecuteStreamWithRetry(context.Background(), ds, a, initial, payload, "pow", StreamRetryOptions{
		Surface:          "test.stream",
		Stream:           true,
		RetryEnabled:     true,
		RetryMaxAttempts: 1,
		UsagePrompt:      "original prompt",
	}, StreamRetryHooks{
		ConsumeAttempt: func(resp *http.Response, allowDeferEmpty bool) ConsumeAttemptResult {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					t.Fatalf("close failed: %v", err)
				}
			}()
			body, _ := io.ReadAll(resp.Body)
			attemptsSeen++
			if strings.Contains(string(body), "ok from second account") {
				return ConsumeAttemptResult{Terminal: true}
			}
			if !allowDeferEmpty {
				t.Fatalf("expected empty attempt %d to be deferred before final 429", attemptsSeen)
			}
			return ConsumeAttemptResult{Retryable: true}
		},
		ParentMessageID: func() int {
			return 11 + attemptsSeen
		},
		OnAccountSwitch: func(sessionID string) {
			switchedSession = sessionID
		},
	})

	if attemptsSeen != 3 {
		t.Fatalf("expected three stream attempts, got %d", attemptsSeen)
	}
	if switchedSession != "session-acc2@test.com" {
		t.Fatalf("expected switched session id, got %q", switchedSession)
	}
	wantAccounts := []string{"acc1@test.com", "acc2@test.com"}
	if len(ds.completionAccounts) != len(wantAccounts) {
		t.Fatalf("completion accounts mismatch: got %v want %v", ds.completionAccounts, wantAccounts)
	}
	for i, want := range wantAccounts {
		if ds.completionAccounts[i] != want {
			t.Fatalf("completion account %d = %q want %q (all=%v)", i, ds.completionAccounts[i], want, ds.completionAccounts)
		}
	}
	if got := ds.payloads[1]["chat_session_id"]; got != "session-acc2@test.com" {
		t.Fatalf("switched payload session mismatch: %#v", got)
	}
	if prompt, _ := ds.payloads[1]["prompt"].(string); strings.Contains(prompt, shared.EmptyOutputRetrySuffix) {
		t.Fatalf("expected switched-account prompt without empty-output suffix, got %q", prompt)
	}
}

func TestExecuteStreamWithRetryResumesInterruptedStreamViaContinue(t *testing.T) {
	ds := &fakeDeepSeekCaller{responses: []*http.Response{
		sseHTTPResponse(http.StatusOK, `data: {"response_message_id":77,"p":"response/content","v":"resumed-part"}`),
	}}
	initial := sseHTTPResponse(http.StatusOK, `data: {"response_message_id":77,"p":"response/content","v":"partial"}`)
	payload := map[string]any{"prompt": "original prompt", "chat_session_id": "session-1"}
	attempts := 0

	ExecuteStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, initial, payload, "pow", StreamRetryOptions{
		Surface:      "test.stream",
		Stream:       true,
		RetryEnabled: true,
		UsagePrompt:  "original prompt",
	}, StreamRetryHooks{
		ConsumeAttempt: func(resp *http.Response, _ bool) ConsumeAttemptResult {
			defer func() { _ = resp.Body.Close() }()
			body, _ := io.ReadAll(resp.Body)
			attempts++
			if strings.Contains(string(body), "resumed-part") {
				return ConsumeAttemptResult{Terminal: true}
			}
			return ConsumeAttemptResult{Retryable: true, ResumeContinue: true}
		},
		ParentMessageID: func() int {
			return 77
		},
	})

	if attempts != 2 {
		t.Fatalf("expected two stream attempts, got %d", attempts)
	}
	if len(ds.continueCalls) != 1 {
		t.Fatalf("expected exactly one continue resume call, got %d", len(ds.continueCalls))
	}
	if got := ds.continueCalls[0].SessionID; got != "session-1" {
		t.Fatalf("continue resume session id mismatch: %q", got)
	}
	if got := ds.continueCalls[0].MessageID; got != 77 {
		t.Fatalf("continue resume message id mismatch: %d", got)
	}
	if len(ds.payloads) != 0 {
		t.Fatalf("expected no synthetic empty-output completion call for continue resume, got %d", len(ds.payloads))
	}
}

func TestExecuteStreamWithRetrySurfacesExhaustedContinueResume(t *testing.T) {
	ds := &fakeDeepSeekCaller{}
	initial := sseHTTPResponse(http.StatusOK, `data: {"response_message_id":77,"p":"response/content","v":"partial"}`)
	payload := map[string]any{"prompt": "original prompt", "chat_session_id": "session-1"}
	attempts := 0
	var failStatus int
	var failCode string

	ExecuteStreamWithRetry(context.Background(), ds, &auth.RequestAuth{}, initial, payload, "pow", StreamRetryOptions{
		Surface:          "test.stream",
		Stream:           true,
		RetryEnabled:     true,
		RetryMaxAttempts: 1,
		UsagePrompt:      "original prompt",
	}, StreamRetryHooks{
		ConsumeAttempt: func(resp *http.Response, _ bool) ConsumeAttemptResult {
			defer func() { _ = resp.Body.Close() }()
			attempts++
			return ConsumeAttemptResult{Retryable: true, ResumeContinue: true}
		},
		ParentMessageID: func() int {
			return 77
		},
		OnRetryFailure: func(status int, _ string, code string) {
			failStatus = status
			failCode = code
		},
	})

	if attempts != 2 {
		t.Fatalf("expected first attempt plus one exhausted resume round, got %d attempts", attempts)
	}
	if len(ds.continueCalls) != 1 {
		t.Fatalf("expected one continue resume call before exhaustion, got %d", len(ds.continueCalls))
	}
	if failStatus != http.StatusBadGateway || failCode != "upstream_interrupted" {
		t.Fatalf("expected 502 upstream_interrupted failure, got status=%d code=%q", failStatus, failCode)
	}
}
