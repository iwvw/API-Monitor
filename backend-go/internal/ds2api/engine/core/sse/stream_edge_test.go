package sse

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestStartParsedLinePumpEmptyBody(t *testing.T) {
	body := strings.NewReader("")
	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	collected := make([]LineResult, 0)
	for r := range results {
		collected = append(collected, r)
	}
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collected) != 0 {
		t.Fatalf("expected no results for empty body, got %d", len(collected))
	}
}

func TestStartParsedLinePumpMultipleLines(t *testing.T) {
	body := strings.NewReader(
		"data: {\"p\":\"response/thinking_content\",\"v\":\"think\"}\n" +
			"data: {\"p\":\"response/content\",\"v\":\"text\"}\n" +
			"data: [DONE]\n",
	)
	results, done := StartParsedLinePump(context.Background(), body, true, "thinking")

	collected := make([]LineResult, 0)
	for r := range results {
		collected = append(collected, r)
	}
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(collected) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(collected))
	}
	hasThinking := false
	for _, r := range collected {
		for _, p := range r.Parts {
			if p.Type == "thinking" {
				hasThinking = true
			}
		}
	}
	if !hasThinking {
		t.Fatal("expected thinking part in results")
	}
	last := collected[len(collected)-1]
	if !last.Stop {
		t.Fatal("expected last result to be stop")
	}
}

func TestStartParsedLinePumpTypeTracking(t *testing.T) {
	body := strings.NewReader(
		"data: {\"p\":\"response/fragments\",\"o\":\"APPEND\",\"v\":[{\"type\":\"THINK\",\"content\":\"思\"}]}\n" +
			"data: {\"p\":\"response/fragments/-1/content\",\"v\":\"考\"}\n" +
			"data: {\"p\":\"response/fragments\",\"o\":\"APPEND\",\"v\":[{\"type\":\"RESPONSE\",\"content\":\"答\"}]}\n" +
			"data: {\"p\":\"response/fragments/-1/content\",\"v\":\"案\"}\n" +
			"data: [DONE]\n",
	)
	// Reasoning-enabled messages start in the thinking segment, so a THINK
	// fragment stays thinking and the RESPONSE flips to text.
	results, done := StartParsedLinePump(context.Background(), body, true, "thinking")

	types := make([]string, 0)
	for r := range results {
		for _, p := range r.Parts {
			types = append(types, p.Type)
		}
	}
	<-done

	if len(types) == 0 {
		t.Fatal("expected some parts, got none")
	}
	hasThinking := false
	hasText := false
	for _, tp := range types {
		if tp == "thinking" {
			hasThinking = true
		}
		if tp == "text" {
			hasText = true
		}
	}
	if !hasThinking {
		t.Fatalf("expected thinking type in results, got %v", types)
	}
	if !hasText {
		t.Fatalf("expected text type in results, got %v", types)
	}
}

func TestStartParsedLinePumpContextCancellation(t *testing.T) {
	pr, pw := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	results, done := StartParsedLinePump(ctx, pr, false, "text")

	go func() {
		_, _ = io.WriteString(pw, "data: {\"p\":\"response/content\",\"v\":\"hello\"}\n")
		time.Sleep(50 * time.Millisecond)
		_ = pw.Close()
	}()

	r := <-results
	if !r.Parsed || len(r.Parts) == 0 {
		t.Fatalf("expected first parsed result, got %#v", r)
	}

	cancel()

	for range results {
	}

	err := <-done
	if err != nil && err != context.Canceled {
		t.Fatalf("expected context.Canceled or nil error, got %v", err)
	}
}

func TestStartParsedLinePumpOnlyDONE(t *testing.T) {
	body := strings.NewReader("data: [DONE]\n")
	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	collected := make([]LineResult, 0)
	for r := range results {
		collected = append(collected, r)
	}
	<-done

	if len(collected) != 1 {
		t.Fatalf("expected 1 result, got %d", len(collected))
	}
	if !collected[0].Stop {
		t.Fatal("expected stop on [DONE]")
	}
}

func TestStartParsedLinePumpNonSSELines(t *testing.T) {
	body := strings.NewReader(
		"event: update\n" +
			": comment line\n" +
			"data: {\"p\":\"response/content\",\"v\":\"valid\"}\n" +
			"data: [DONE]\n",
	)
	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	var validCount int
	for r := range results {
		if r.Parsed && len(r.Parts) > 0 {
			validCount++
		}
	}
	<-done

	if validCount != 1 {
		t.Fatalf("expected 1 valid result, got %d", validCount)
	}
}

func TestStartParsedLinePumpThinkingDisabled(t *testing.T) {
	body := strings.NewReader(
		"data: {\"p\":\"response/fragments\",\"o\":\"APPEND\",\"v\":[{\"type\":\"THINK\",\"content\":\"思\"}]}\n" +
			"data: {\"p\":\"response/fragments/-1/content\",\"v\":\"考\"}\n" +
			"data: {\"v\":\"隐藏\"}\n" +
			"data: {\"p\":\"response/fragments\",\"o\":\"APPEND\",\"v\":[{\"type\":\"RESPONSE\",\"content\":\"答\"}]}\n" +
			"data: {\"p\":\"response/content\",\"v\":\"response\"}\n" +
			"data: [DONE]\n",
	)
	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	var parts []ContentPart
	for r := range results {
		parts = append(parts, r.Parts...)
	}
	<-done

	got := strings.Builder{}
	for _, p := range parts {
		if p.Type != "text" {
			t.Fatalf("expected only text parts with thinking disabled, got %#v", parts)
		}
		got.WriteString(p.Text)
	}
	if got.String() != "答response" {
		t.Fatalf("expected hidden thinking to be dropped, got %q from %#v", got.String(), parts)
	}
}

func TestStartParsedLinePumpAccumulatesSmallChunks(t *testing.T) {
	body := strings.NewReader(
		"data: {\"p\":\"response/content\",\"v\":\"h\"}\n" +
			"data: {\"p\":\"response/content\",\"v\":\"i\"}\n" +
			"data: [DONE]\n",
	)

	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	collected := make([]LineResult, 0)
	for r := range results {
		collected = append(collected, r)
	}
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	last := collected[len(collected)-1]
	if !last.Stop {
		t.Fatal("expected last result to stop")
	}

	allText := strings.Builder{}
	for _, r := range collected {
		for _, p := range r.Parts {
			allText.WriteString(p.Text)
		}
	}
	if allText.String() != "hi" {
		t.Fatalf("expected accumulated text 'hi', got %q", allText.String())
	}
}

func TestStartParsedLinePumpFirstFlushImmediate(t *testing.T) {
	body := strings.NewReader(
		"data: {\"p\":\"response/content\",\"v\":\"Hi\"}\n" +
			"data: [DONE]\n",
	)

	results, done := StartParsedLinePump(context.Background(), body, false, "text")

	collected := make([]LineResult, 0)
	for r := range results {
		collected = append(collected, r)
	}
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	hasContent := false
	for _, r := range collected {
		for _, p := range r.Parts {
			if p.Text == "Hi" {
				hasContent = true
			}
		}
	}
	if !hasContent {
		t.Fatal("expected 'Hi' content in results")
	}
}
