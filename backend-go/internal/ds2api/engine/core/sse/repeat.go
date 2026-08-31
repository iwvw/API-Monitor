package sse

import (
	"bytes"
	"strings"
)

const (
	// RepeatWindowBytes 是周期检测的尾部窗口大小：只在最近这么多字节内检测
	// 重复周期，兼顾内存占用与实时性（循环一旦开始，尾部会持续保持周期形态）。
	RepeatWindowBytes = 768
	// RepeatMinPeriod 是最短可判定为循环的周期长度（字节）。过短的周期在正常
	// token 流式合并里可能高频出现（如代码块、重复短词），需排除，避免误杀。
	RepeatMinPeriod = 8
)

// RepeatTail 维护最近累积可见文本的尾部窗口，用于检测输出循环。
// 它按 text / thinking 分别累积，避免一种流的循环污染另一种的周期检测。
type RepeatTail struct {
	runs  int
	text  []byte
	think []byte
}

// NewRepeatTail 构造带指定重复次数阈值的检测器。runs <= 0 时用默认 3。
func NewRepeatTail(runs int) RepeatTail {
	if runs <= 0 {
		runs = 3
	}
	return RepeatTail{runs: runs}
}

// Observe 追加一段内容并返回是否检测到循环（尾部出现重复周期）。
func (t *RepeatTail) Observe(text string, isThinking bool) bool {
	if text = strings.TrimSpace(text); text == "" {
		return false
	}
	if isThinking {
		t.think = appendRepeatTail(t.think, text)
		return hasTrailingPeriod(t.think, t.runs)
	}
	t.text = appendRepeatTail(t.text, text)
	return hasTrailingPeriod(t.text, t.runs)
}

// appendRepeatTail 追加 s 并只保留尾部 RepeatWindowBytes 字节。
func appendRepeatTail(buf []byte, s string) []byte {
	buf = append(buf, s...)
	if len(buf) > RepeatWindowBytes {
		buf = buf[len(buf)-RepeatWindowBytes:]
	}
	return buf
}

// hasTrailingPeriod 判断 buf 尾部是否以重复周期结尾：尾部可分解为至少 runs 段
// 相同的、长度 >= RepeatMinPeriod 的子串。覆盖连续重复（AAAA）与周期交替
// （ABABAB）。周期需含至少 2 个不同 rune，避免把单字符长串误判。
func hasTrailingPeriod(buf []byte, runs int) bool {
	if runs < 2 {
		runs = 3
	}
	n := len(buf)
	if n < RepeatMinPeriod*runs {
		return false
	}
	for period := RepeatMinPeriod; period <= n/runs; period++ {
		tail := buf[n-period:]
		if distinctRuneCount(string(tail)) < 2 {
			continue
		}
		matched := true
		for i := 1; i < runs; i++ {
			start := n - (i+1)*period
			if !bytes.Equal(buf[start:start+period], tail) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// distinctRuneCount 返回 s 中不同 rune 的数量。
func distinctRuneCount(s string) int {
	seen := make(map[rune]struct{})
	for _, r := range s {
		seen[r] = struct{}{}
	}
	return len(seen)
}
