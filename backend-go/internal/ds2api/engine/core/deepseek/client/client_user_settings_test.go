package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
)

func TestDisableTrainingAllowedSuccess(t *testing.T) {
	var seenURL string
	var seenBody string
	var seenAuth string
	client := &Client{
		regular: doerFunc(func(req *http.Request) (*http.Response, error) {
			seenURL = req.URL.String()
			seenAuth = req.Header.Get("authorization")
			b, _ := io.ReadAll(req.Body)
			seenBody = string(b)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"code":0,"msg":"ok","data":{"biz_code":0,"biz_msg":""}}`)),
				Request:    req,
			}, nil
		}),
	}
	a := &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "tok-abc",
		AccountID:      "acct-1",
	}
	if err := client.DisableTrainingAllowed(context.Background(), a); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if seenURL != dsprotocol.DeepSeekUpdateSettingsURL {
		t.Fatalf("expected url %s, got %s", dsprotocol.DeepSeekUpdateSettingsURL, seenURL)
	}
	if seenAuth != "Bearer tok-abc" {
		t.Fatalf("expected bearer auth header, got %q", seenAuth)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(seenBody), &payload); err != nil {
		t.Fatalf("invalid request body: %v", err)
	}
	if v, ok := payload["training_allowed"].(bool); !ok || v {
		t.Fatalf("expected training_allowed=false, got %v", payload["training_allowed"])
	}
}

func TestDisableTrainingAllowedFailureReturnsError(t *testing.T) {
	client := &Client{
		regular: doerFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"code":50000,"msg":"busy","data":{"biz_code":0,"biz_msg":""}}`)),
				Request:    req,
			}, nil
		}),
	}
	a := &auth.RequestAuth{
		UseConfigToken: true,
		DeepSeekToken:  "tok-abc",
		AccountID:      "acct-1",
	}
	if err := client.DisableTrainingAllowed(context.Background(), a); err == nil {
		t.Fatal("expected error on non-zero code, got nil")
	}
}

func TestDisableTrainingAllowedSkipsEmptyToken(t *testing.T) {
	called := false
	client := &Client{
		regular: doerFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"code":0}`)),
				Request:    req,
			}, nil
		}),
	}
	a := &auth.RequestAuth{UseConfigToken: true, DeepSeekToken: "  ", AccountID: "acct-1"}
	if err := client.DisableTrainingAllowed(context.Background(), a); err != nil {
		t.Fatalf("expected nil error for empty token, got %v", err)
	}
	if called {
		t.Fatal("expected no request when token is empty")
	}
}
