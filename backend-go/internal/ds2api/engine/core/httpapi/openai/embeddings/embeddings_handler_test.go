package embeddings

import (
	"testing"
)

func TestExtractEmbeddingInputs(t *testing.T) {
	// String input
	res := ExtractEmbeddingInputs("hello world")
	if len(res) != 1 || res[0] != "hello world" {
		t.Fatalf("unexpected string input extraction: %#v", res)
	}

	// Empty string
	if res := ExtractEmbeddingInputs("   "); len(res) != 0 {
		t.Fatalf("expected empty for whitespace string, got %#v", res)
	}

	// Array input
	res = ExtractEmbeddingInputs([]any{"text1", "text2", "text3"})
	if len(res) != 3 || res[0] != "text1" || res[1] != "text2" || res[2] != "text3" {
		t.Fatalf("unexpected slice input extraction: %#v", res)
	}

	// Nil input
	if res := ExtractEmbeddingInputs(nil); len(res) != 0 {
		t.Fatalf("expected empty for nil, got %#v", res)
	}
}

func TestDeterministicEmbedding(t *testing.T) {
	vec1 := DeterministicEmbedding("test")
	vec2 := DeterministicEmbedding("test")
	vec3 := DeterministicEmbedding("different")

	if len(vec1) != 64 {
		t.Fatalf("expected embedding dimension 64, got %d", len(vec1))
	}

	// Deterministic equality
	for i := range vec1 {
		if vec1[i] != vec2[i] {
			t.Fatalf("expected deterministic equality at %d", i)
		}
	}

	// Different input should yield different vector
	diff := false
	for i := range vec1 {
		if vec1[i] != vec3[i] {
			diff = true
			break
		}
	}
	if !diff {
		t.Fatalf("expected different vector for different input")
	}
}
