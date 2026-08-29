package completionruntime

import (
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
)

type mockExpertSegmentConfig struct {
	enabled  bool
	maxChars int
}

func (m mockExpertSegmentConfig) ExpertPromptSegmentEnabled() bool { return m.enabled }
func (m mockExpertSegmentConfig) ExpertPromptSegmentMaxChars() int { return m.maxChars }

func TestShouldSegmentExpertPrompt_DisabledReturnsNil(t *testing.T) {
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "deepseek-v4-pro",
		FinalPrompt:   stringRepeat("X", 200000),
	}
	opts := Options{ExpertPromptSegment: mockExpertSegmentConfig{enabled: false, maxChars: 120000}}
	if segs := shouldSegmentExpertPrompt(stdReq, opts); segs != nil {
		t.Fatalf("expected nil when disabled, got %d segments", len(segs))
	}
}

func TestShouldSegmentExpertPrompt_NonExpertReturnsNil(t *testing.T) {
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "deepseek-v4-flash",
		FinalPrompt:   stringRepeat("X", 200000),
	}
	opts := Options{ExpertPromptSegment: mockExpertSegmentConfig{enabled: true, maxChars: 120000}}
	if segs := shouldSegmentExpertPrompt(stdReq, opts); segs != nil {
		t.Fatalf("expected nil for non-expert model, got %d segments", len(segs))
	}
}

func TestShouldSegmentExpertPrompt_UnderThresholdReturnsNil(t *testing.T) {
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "deepseek-v4-pro",
		FinalPrompt:   "short prompt",
	}
	opts := Options{ExpertPromptSegment: mockExpertSegmentConfig{enabled: true, maxChars: 120000}}
	if segs := shouldSegmentExpertPrompt(stdReq, opts); segs != nil {
		t.Fatalf("expected nil for short prompt, got %d segments", len(segs))
	}
}

func TestShouldSegmentExpertPrompt_OverThresholdReturnsSegments(t *testing.T) {
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "deepseek-v4-pro",
		FinalPrompt:   "<User>:" + stringRepeat("X", 200000) + "<Assistant>:",
	}
	opts := Options{ExpertPromptSegment: mockExpertSegmentConfig{enabled: true, maxChars: 1000}}
	segs := shouldSegmentExpertPrompt(stdReq, opts)
	if segs == nil {
		t.Fatalf("expected segments for over-threshold expert prompt, got nil")
	}
	if len(segs) < 2 {
		t.Fatalf("expected at least 2 segments, got %d", len(segs))
	}
}

func TestShouldSegmentExpertPrompt_NoReaderReturnsNil(t *testing.T) {
	stdReq := promptcompat.StandardRequest{
		ResolvedModel: "deepseek-v4-pro",
		FinalPrompt:   stringRepeat("X", 200000),
	}
	opts := Options{}
	if segs := shouldSegmentExpertPrompt(stdReq, opts); segs != nil {
		t.Fatalf("expected nil when no reader provided, got %d segments", len(segs))
	}
}

func stringRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
