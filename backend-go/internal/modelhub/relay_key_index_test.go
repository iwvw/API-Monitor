package modelhub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newSuccessChatUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNonStreamRelayRecordsKeyIndex(t *testing.T) {
	upstream := newSuccessChatUpstream(t)

	service := newOpenAIService(t)
	endpointID := newStrategyEndpoint(t, service, upstream.URL, []string{"k0"}, keyStrategyRoundRobin, 1)

	if code := sendStrategyChat(t, service, endpointID); code != http.StatusOK {
		t.Fatalf("chat status = %d, want 200", code)
	}
	service.flushAnalyticsQueue(5 * time.Second)

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var keyIndex int
	if err := db.QueryRow(`SELECT key_index FROM modelhub_gateway_analytics ORDER BY id DESC LIMIT 1`).Scan(&keyIndex); err != nil {
		t.Fatalf("query key_index: %v", err)
	}
	if keyIndex != 0 {
		t.Fatalf("non-stream key_index = %d, want 0 (primary key)", keyIndex)
	}
}
