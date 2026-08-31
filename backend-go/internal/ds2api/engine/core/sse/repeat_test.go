package sse

import (
	"strings"
	"testing"
)

func TestRepeatTailConsecutiveRepeat(t *testing.T) {
	var tail RepeatTail
	text := "这是一个足够长的重复片段用于触发循环检测"
	if tail.Observe(text, false) {
		t.Fatal("should not trigger on first block")
	}
	if tail.Observe(text, false) {
		t.Fatal("should not trigger on second block")
	}
	if !tail.Observe(text, false) {
		t.Fatal("should trigger after three identical blocks")
	}
}

func TestRepeatTailAlternatingLoop(t *testing.T) {
	var tail RepeatTail
	blocks := []string{"alpha段落", "beta段落"}
	for i := 0; i < 6; i++ {
		if tail.Observe(blocks[i%2], false) {
			return
		}
	}
	t.Fatal("alternating A/B loop was not detected")
}

func TestRepeatTailThinkingSeparate(t *testing.T) {
	var tail RepeatTail
	// thinking 循环应被检测到（返回 true），且不污染 text 独立窗口。
	detected := false
	for i := 0; i < 6; i++ {
		if tail.Observe("思考循环段落", true) {
			detected = true
			break
		}
	}
	if !detected {
		t.Fatal("thinking loop should be detected")
	}
	// 正常 text 演进不应误杀（text 窗口独立于 thinking）。
	lines := []string{"正常第一行", "正常第二行", "正常第三行"}
	for _, l := range lines {
		if tail.Observe(l, false) {
			t.Fatal("normal text falsely flagged")
		}
	}
}

func TestRepeatTailSingleRuneIgnored(t *testing.T) {
	var tail RepeatTail
	if tail.Observe(strings.Repeat("啊", 30), false) {
		t.Fatal("single-rune block should not be flagged as loop")
	}
}

func TestRepeatTailShortPeriodIgnored(t *testing.T) {
	var tail RepeatTail
	if tail.Observe("好的好的好的好的", false) {
		t.Fatal("short-period content should not be flagged as loop")
	}
}
