package stream

import (
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

func TestContentRepeatGuardRequiresPositiveLimit(t *testing.T) {
	if g := newContentRepeatGuard(0); g != nil {
		t.Fatal("expected nil guard for zero limit")
	}
	if g := newContentRepeatGuard(-1); g != nil {
		t.Fatal("expected nil guard for negative limit")
	}
}

// 连续重复：同一段内容反复输出（如模型卡死重复同一大段）。
func TestContentRepeatGuardConsecutiveRepeat(t *testing.T) {
	g := newContentRepeatGuard(3)
	part := []sse.ContentPart{{Text: "这是一个足够长的重复片段用于触发循环检测", Type: "text"}}
	if g.observe(part) {
		t.Fatal("should not trigger on first block")
	}
	if g.observe(part) {
		t.Fatal("should not trigger on second block")
	}
	if !g.observe(part) {
		t.Fatal("should trigger after three identical blocks")
	}
}

// 周期交替：ABAB... 循环，现有「相邻块相同」检测漏掉，新检测应捕获。
func TestContentRepeatGuardAlternatingLoop(t *testing.T) {
	g := newContentRepeatGuard(3)
	// 交替两块 A/B，累积尾部呈现 ABABAB，应判定为周期循环。
	parts := []sse.ContentPart{
		{Text: "alpha段落", Type: "text"},
		{Text: "beta段落", Type: "text"},
	}
	for i := 0; i < 6; i++ {
		if g.observe(parts[i%2 : i%2+1]) {
			return // 检测到即通过
		}
	}
	t.Fatal("alternating A/B loop was not detected")
}

// 增量 delta 流：相邻块各不相同，但累积后出现重复周期（真实循环形态）。
func TestContentRepeatGuardIncrementalDeltaLoop(t *testing.T) {
	g := newContentRepeatGuard(3)
	// 模拟增量流式：每块是「上一块 + 新片段」，但整体循环重复同一个句子。
	sentence := "这是一个足够长的句子用来模拟循环输出的场景"
	// 累积两个周期（每次追加整句），再检查是否触发。
	block := []sse.ContentPart{{Text: sentence, Type: "text"}}
	g.observe(block)
	g.observe(block)
	if !g.observe(block) {
		t.Fatal("incremental delta loop should be detected after three repeats")
	}
}

// 正常输出不误杀：内容持续演进、无重复周期。
func TestContentRepeatGuardNoFalsePositive(t *testing.T) {
	g := newContentRepeatGuard(3)
	lines := []string{
		"第一行是正常的说明文字，内容各不相同",
		"第二行继续展开不同的内容，绝不重复",
		"第三行讲到另一个完全不同的主题方向",
		"第四行补充额外的细节信息用于区分",
	}
	for _, l := range lines {
		if g.observe([]sse.ContentPart{{Text: l, Type: "text"}}) {
			t.Fatalf("normal evolving content falsely flagged as loop: %q", l)
		}
	}
}

// 过短周期不触发：正常 token 合并可能出现短重复词，不应误杀。
func TestContentRepeatGuardShortPeriodIgnored(t *testing.T) {
	g := newContentRepeatGuard(3)
	short := []sse.ContentPart{{Text: "好的好的好的好的", Type: "text"}}
	// 周期 "好的" 只有 6 字节 < repeatMinPeriod，不应触发。
	if g.observe(short) {
		t.Fatal("short-period content should not be flagged as loop")
	}
}

// 单字符块不触发：token 流式合并可能产生长串单字符。
func TestContentRepeatGuardSingleRuneIgnored(t *testing.T) {
	g := newContentRepeatGuard(3)
	// 周期为单字符 "啊"（3 字节 < 8），且重复次数足够，但周期过短应忽略。
	block := []sse.ContentPart{{Text: strings.Repeat("啊", 30), Type: "text"}}
	if g.observe(block) {
		t.Fatal("single-rune block should not be flagged as loop")
	}
}
