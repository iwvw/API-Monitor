package prompt

import (
	"strings"
	"testing"
)

func TestSplitByMaxRunesNoSplitWhenUnderThreshold(t *testing.T) {
	prompt := "<System>:hello<User>:world<Assistant>:"
	segments := SplitByMaxRunes(prompt, 1000)
	if len(segments) != 1 || segments[0] != prompt {
		t.Fatalf("expected single segment, got %d segments: %v", len(segments), segments)
	}
}

func TestSplitByMaxRunesCutsAtConfiguredLength(t *testing.T) {
	prompt := strings.Repeat("A", 50) + "<User>:[Start a new chat]" + strings.Repeat("B", 50)
	segments := SplitByMaxRunes(prompt, 60)
	if len(segments) != 3 {
		t.Fatalf("expected 3 segments, got %d: %v", len(segments), segments)
	}
	joined := strings.Join(segments, "")
	if joined != prompt {
		t.Fatalf("segments do not reconstruct original prompt:\njoined:  %q\noriginal:%q", joined, prompt)
	}
	for i, seg := range segments {
		if len([]rune(seg)) > 60 {
			t.Fatalf("segment %d exceeds maxChars: %d > 60", i, len([]rune(seg)))
		}
	}
}

func TestSplitByMaxRunesDoesNotCreateRoleMarkerOnlySegment(t *testing.T) {
	prefix := strings.Repeat("A", 55)
	marker := "<User>:[Start a new chat]"
	suffix := strings.Repeat("B", 20)
	prompt := prefix + marker + suffix

	segments := SplitByMaxRunes(prompt, 60)
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d: %v", len(segments), segments)
	}
	for i, seg := range segments {
		if seg == marker {
			t.Fatalf("segment %d contains only marker text: %q", i, seg)
		}
	}
	if joined := strings.Join(segments, ""); joined != prompt {
		t.Fatalf("segments do not reconstruct original prompt")
	}
}

func TestSplitByMaxRunesSingleBlockNoMarkers(t *testing.T) {
	longText := strings.Repeat("X", 200)
	segments := SplitByMaxRunes(longText, 50)
	if len(segments) != 4 {
		t.Fatalf("expected 4 segments, got %d", len(segments))
	}
	joined := strings.Join(segments, "")
	if joined != longText {
		t.Fatalf("segments do not reconstruct original text")
	}
}

func TestSplitByMaxRunesEmptyPrompt(t *testing.T) {
	segments := SplitByMaxRunes("", 100)
	if len(segments) != 1 || segments[0] != "" {
		t.Fatalf("expected single empty segment, got %v", segments)
	}
}

func TestSplitByMaxRunesExactThreshold(t *testing.T) {
	prompt := "<User>:hello<Assistant>:"
	segments := SplitByMaxRunes(prompt, len([]rune(prompt)))
	if len(segments) != 1 {
		t.Fatalf("expected single segment when at exact threshold, got %d", len(segments))
	}
}

func TestSplitByRoleBoundaryUsesMaxRuneSplitting(t *testing.T) {
	prompt := strings.Repeat("A", 55) + "<User>:short" + strings.Repeat("B", 10)
	segments := SplitByRoleBoundary(prompt, 60)
	if len(segments) != 2 {
		t.Fatalf("expected compatibility wrapper to split by max runes, got %d", len(segments))
	}
	if strings.Join(segments, "") != prompt {
		t.Fatalf("segments do not reconstruct original prompt")
	}
}
