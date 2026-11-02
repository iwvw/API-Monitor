package sse

import (
	"strings"
	"testing"
)

func TestTrimContinuationOverlapReturnsSuffixForSnapshotReplay(t *testing.T) {
	existing := "我们被问到：这是一个很长的续答快照前缀，用来验证去重逻辑不会误伤正常 token。"
	incoming := existing + "继续分析"
	got := TrimContinuationOverlap(existing, incoming)
	if got != "继续分析" {
		t.Fatalf("expected suffix only, got %q", got)
	}
}

func TestTrimContinuationOverlapDropsStaleShorterSnapshot(t *testing.T) {
	incoming := "我们被问到：这是一个很长的续答快照前缀，用来验证去重逻辑不会误伤正常 token。"
	existing := incoming + "继续分析"
	got := TrimContinuationOverlap(existing, incoming)
	if got != "" {
		t.Fatalf("expected stale snapshot to be dropped, got %q", got)
	}
}

func TestTrimContinuationOverlapPreservesNormalIncrement(t *testing.T) {
	existing := "我们"
	incoming := "被"
	got := TrimContinuationOverlap(existing, incoming)
	if got != "被" {
		t.Fatalf("expected normal increment unchanged, got %q", got)
	}
}

func TestTrimContinuationOverlapKeepsShortPrefixLikeNormalToken(t *testing.T) {
	existing := "我们被问到"
	incoming := "我们"
	got := TrimContinuationOverlap(existing, incoming)
	if got != "我们" {
		t.Fatalf("expected short token preserved, got %q", got)
	}
}

func TestTrimContinuationOverlapKeepsShortMultibyteChunk(t *testing.T) {
	existing := strings.Repeat("字", 36)
	incoming := strings.Repeat("字", 16)
	got := TrimContinuationOverlap(existing, incoming)
	if got != incoming {
		t.Fatalf("expected short multibyte chunk preserved, got %q", got)
	}
}
