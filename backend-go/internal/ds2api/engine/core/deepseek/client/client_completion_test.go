package client

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
)

func TestCallCompletionDoesNotFallbackForNonIdempotentCompletion(t *testing.T) {
	var fallbackCalled bool
	client := &Client{
		stream: doerFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("ambiguous completion write failure")
		}),
		fallbackS: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			fallbackCalled = true
			return &http.Response{StatusCode: http.StatusOK}, nil
		})},
	}
	_, err := client.CallCompletion(
		context.Background(),
		&auth.RequestAuth{DeepSeekToken: "token"},
		map[string]any{"prompt": "hello"},
		"pow",
		3,
	)
	if err == nil {
		t.Fatal("expected completion error")
	}
	if fallbackCalled {
		t.Fatal("completion fallback should not be called for a non-idempotent request")
	}
}
