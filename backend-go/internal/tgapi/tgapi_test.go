package tgapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type messageResult struct {
	MessageID int64 `json:"message_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}

func TestCallDecodesObjectResult(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(req.URL.Path, "/bottoken-test/sendMessage") {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		body, _ := io.ReadAll(req.Body)
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if payload["chat_id"] != "10001" {
			t.Fatalf("payload chat_id = %#v", payload["chat_id"])
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":{"message_id":42,"chat":{"id":10001}}}`)),
		}, nil
	})}

	env, err := NewClient("token-test", client).Call(context.Background(), "sendMessage", map[string]interface{}{"chat_id": "10001", "text": "hi"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !env.OK {
		t.Fatal("expected ok")
	}
	result, err := DecodeObject[messageResult](env)
	if err != nil {
		t.Fatalf("decode object: %v", err)
	}
	if result.MessageID != 42 || result.Chat.ID != 10001 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCallDecodesArrayResult(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"result":[{"update_id":7},{"update_id":8}]}`)),
		}, nil
	})}

	env, err := NewClient("token-test", client).Call(context.Background(), "getUpdates", map[string]interface{}{"offset": 0})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	updates, err := DecodeArray[map[string]interface{}](env)
	if err != nil {
		t.Fatalf("decode array: %v", err)
	}
	if len(updates) != 2 || int64(updates[0]["update_id"].(float64)) != 7 {
		t.Fatalf("updates = %#v", updates)
	}
}

func TestCallWrapsTransportError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, &http.ProtocolError{ErrorString: "dial timeout"}
	})}
	_, err := NewClient("token-test", client).Call(context.Background(), "getMe", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected transport error")
	}
	if !strings.Contains(err.Error(), "telegram API request failed") {
		t.Fatalf("unexpected transport error: %v", err)
	}
	var urlErr *http.ProtocolError
	if !errors.As(err, &urlErr) {
		t.Fatalf("expected underlying error preserved, got %v", err)
	}
}

func TestCallStatusErrorIncludesDescription(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":false,"description":"method not found"}`)),
		}, nil
	})}
	_, err := NewClient("token-test", client).Call(context.Background(), "sendRichMessage", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for bad status")
	}
	if !strings.Contains(err.Error(), "telegram API status 400") || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("unexpected status error: %v", err)
	}
}

func TestCallNonOKEnvelope(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":false,"description":"bot is blocked"}`)),
		}, nil
	})}
	_, err := NewClient("token-test", client).Call(context.Background(), "sendMessage", map[string]interface{}{})
	if err == nil {
		t.Fatal("expected API error")
	}
	if !strings.Contains(err.Error(), "telegram API error: bot is blocked") {
		t.Fatalf("unexpected API error: %v", err)
	}
}

func TestServerSideIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken-test/getMe" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"username":"test_bot"}}`))
	}))
	defer server.Close()

	client := NewClient("token-test", http.DefaultClient)
	client.Base = server.URL
	env, err := client.Call(context.Background(), "getMe", map[string]interface{}{})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	me, err := DecodeObject[map[string]interface{}](env)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if me["username"] != "test_bot" {
		t.Fatalf("me = %#v", me)
	}
}