package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

func TestToAccountMissingFieldsRemainEmpty(t *testing.T) {
	acc := toAccount(map[string]any{
		"email":    "user@example.com",
		"password": "secret",
	})
	if acc.Email != "user@example.com" {
		t.Fatalf("unexpected email: %q", acc.Email)
	}
	if acc.Mobile != "" {
		t.Fatalf("expected empty mobile, got %q", acc.Mobile)
	}
	if acc.Token != "" {
		t.Fatalf("expected empty token, got %q", acc.Token)
	}
}

func TestFieldStringNilToEmpty(t *testing.T) {
	if got := fieldString(map[string]any{"token": nil}, "token"); got != "" {
		t.Fatalf("expected empty string for nil field, got %q", got)
	}
	if got := fieldString(map[string]any{}, "token"); got != "" {
		t.Fatalf("expected empty string for missing field, got %q", got)
	}
}

func TestMaskSecretPreviewKeepsOnlyFirstAndLastTwoChars(t *testing.T) {
	cases := map[string]string{
		"":         "",
		"a":        "*",
		"ab":       "**",
		"abcd":     "****",
		"abcdef":   "ab****ef",
		"abc12345": "ab****45",
	}

	for input, want := range cases {
		if got := maskSecretPreview(input); got != want {
			t.Fatalf("maskSecretPreview(%q)=%q want %q", input, got, want)
		}
	}
}

func TestGetConfigMasksAccountTokenPreview(t *testing.T) {
	h := newAdminTestHandler(t, `{
		"accounts":[{"email":"u@example.com","password":"pwd"}]
	}`)
	if err := h.Store.UpdateAccountToken("u@example.com", "abcdefgh"); err != nil {
		t.Fatalf("seed runtime token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/config", nil)
	rec := httptest.NewRecorder()
	h.getConfig(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	accounts, _ := payload["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accounts))
	}
	first, _ := accounts[0].(map[string]any)
	if got, _ := first["token_preview"].(string); got != "ab****gh" {
		t.Fatalf("expected masked token preview, got %q", got)
	}
}

func TestRunAccountTestsConcurrentlyKeepsInputOrder(t *testing.T) {
	accounts := []config.Account{
		{Email: "a@example.com"},
		{Email: "b@example.com"},
		{Email: "c@example.com"},
	}
	results := runAccountTestsConcurrently(accounts, 2, func(idx int, acc config.Account) map[string]any {
		return map[string]any{
			"idx":     idx,
			"account": acc.Identifier(),
		}
	})
	if len(results) != len(accounts) {
		t.Fatalf("unexpected result length: got %d want %d", len(results), len(accounts))
	}
	for i := range accounts {
		gotIdx, _ := results[i]["idx"].(int)
		if gotIdx != i {
			t.Fatalf("result index mismatch at %d: got %d", i, gotIdx)
		}
		gotID, _ := results[i]["account"].(string)
		if gotID != accounts[i].Identifier() {
			t.Fatalf("result order mismatch at %d: got %q want %q", i, gotID, accounts[i].Identifier())
		}
	}
}

func TestRunAccountTestsConcurrentlyRespectsLimit(t *testing.T) {
	const limit = 3
	accounts := []config.Account{
		{Email: "1@example.com"},
		{Email: "2@example.com"},
		{Email: "3@example.com"},
		{Email: "4@example.com"},
		{Email: "5@example.com"},
		{Email: "6@example.com"},
	}
	var current int32
	var maxSeen int32
	_ = runAccountTestsConcurrently(accounts, limit, func(_ int, _ config.Account) map[string]any {
		c := atomic.AddInt32(&current, 1)
		for {
			m := atomic.LoadInt32(&maxSeen)
			if c <= m || atomic.CompareAndSwapInt32(&maxSeen, m, c) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&current, -1)
		return map[string]any{"success": true}
	})
	if maxSeen > limit {
		t.Fatalf("concurrency exceeded limit: got %d > %d", maxSeen, limit)
	}
	if maxSeen < 2 {
		t.Fatalf("expected concurrent execution, max seen %d", maxSeen)
	}
}
