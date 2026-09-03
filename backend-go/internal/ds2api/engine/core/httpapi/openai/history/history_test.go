package history

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
)

func TestMapError(t *testing.T) {
	err := errors.New("generic failure")
	code, msg := MapError(err)
	if code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", code)
	}
	if msg != "generic failure" {
		t.Fatalf("unexpected message: %s", msg)
	}
}

func TestService_NilStoreOrDisabled(t *testing.T) {
	svc := Service{}
	req := promptcompat.StandardRequest{
		ResolvedModel: "deepseek-chat",
	}
	res, err := svc.ApplyCurrentInputFile(context.Background(), nil, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ResolvedModel != "deepseek-chat" {
		t.Fatalf("expected model deepseek-chat, got %s", res.ResolvedModel)
	}
}
