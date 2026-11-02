package client

import (
	"context"
	"testing"
)

// ─── toFloat64 edge cases ────────────────────────────────────────────

func TestToFloat64FromFloat64(t *testing.T) {
	if got := toFloat64(float64(3.14), 0); got != 3.14 {
		t.Fatalf("expected 3.14, got %f", got)
	}
}

func TestToFloat64FromInt(t *testing.T) {
	if got := toFloat64(42, 0); got != 42.0 {
		t.Fatalf("expected 42.0, got %f", got)
	}
}

func TestToFloat64FromInt64(t *testing.T) {
	if got := toFloat64(int64(100), 0); got != 100.0 {
		t.Fatalf("expected 100.0, got %f", got)
	}
}

func TestToFloat64FromStringDefault(t *testing.T) {
	if got := toFloat64("42", 99.0); got != 99.0 {
		t.Fatalf("expected default 99.0, got %f", got)
	}
}

func TestToFloat64FromNilDefault(t *testing.T) {
	if got := toFloat64(nil, 5.5); got != 5.5 {
		t.Fatalf("expected default 5.5, got %f", got)
	}
}

func TestToFloat64FromBoolDefault(t *testing.T) {
	if got := toFloat64(true, 1.0); got != 1.0 {
		t.Fatalf("expected default 1.0, got %f", got)
	}
}

// ─── toInt64 edge cases ──────────────────────────────────────────────

func TestToInt64FromFloat64(t *testing.T) {
	if got := toInt64(float64(42.9), 0); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestToInt64FromInt(t *testing.T) {
	if got := toInt64(42, 0); got != 42 {
		t.Fatalf("expected 42, got %d", got)
	}
}

func TestToInt64FromInt64(t *testing.T) {
	if got := toInt64(int64(100), 0); got != 100 {
		t.Fatalf("expected 100, got %d", got)
	}
}

func TestToInt64FromStringDefault(t *testing.T) {
	if got := toInt64("42", 99); got != 99 {
		t.Fatalf("expected default 99, got %d", got)
	}
}

func TestToInt64FromNilDefault(t *testing.T) {
	if got := toInt64(nil, 7); got != 7 {
		t.Fatalf("expected default 7, got %d", got)
	}
}

// ─── BuildPowHeader edge cases ───────────────────────────────────────

func TestBuildPowHeaderBasicChallenge(t *testing.T) {
	challenge := map[string]any{
		"algorithm":   "DeepSeekHashV1",
		"challenge":   "abc123",
		"salt":        "salt456",
		"signature":   "sig789",
		"target_path": "/path",
	}
	result, err := BuildPowHeader(challenge, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestBuildPowHeaderEmptyChallenge(t *testing.T) {
	result, err := BuildPowHeader(map[string]any{}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should produce a base64 encoded JSON with nil values
	if result == "" {
		t.Fatal("expected non-empty result for empty challenge")
	}
}

// ─── NewClient ───────────────────────────────────────────────────────

func TestNewClientInitialState(t *testing.T) {
	client := NewClient(nil, nil)
	if client == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestNewClientPreloadPowIdempotent(t *testing.T) {
	client := NewClient(nil, nil)
	if err := client.PreloadPow(context.Background()); err != nil {
		t.Fatalf("first preload failed: %v", err)
	}
	if err := client.PreloadPow(context.Background()); err != nil {
		t.Fatalf("second preload failed: %v", err)
	}
}
