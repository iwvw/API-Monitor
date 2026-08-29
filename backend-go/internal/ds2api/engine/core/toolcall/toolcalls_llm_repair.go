package toolcall

import (
	"context"
	"strings"
)

// ToolCallRepairFixedPrompt is the verbatim instruction appended after the tool
// call format spec when asking an LLM to repair malformed tool-call code. It is
// reproduced byte-for-byte from plan/tool-call-fallback-design-phase3.md §1.
const ToolCallRepairFixedPrompt = "请根据上述说明的工具调用说明，修复以下错误的调用代码。直接输出修复后的格式，无需解释哪里错了，也无需说任何话。注意，工具调用的实际参数已用变量代替。直接原样输出即可。如果你认为接下来的内容并不是工具调用，直接返回 NO TOOL"

// toolCallRepairNoToolSentinel is the literal the repair model must return when
// it judges the fragment is not a tool call (branch ②).
const toolCallRepairNoToolSentinel = "NO TOOL"

// ToolCallRepairInvoker sends the fully assembled repair prompt to an LLM and
// returns its raw text output. Implementations must honor the phase3 §3 hard
// constraints (same account, expert mode, thinking disabled, brand-new session,
// 10s timeout). A non-nil error (including timeout) routes the caller to the
// fallback path (branch ③).
type ToolCallRepairInvoker func(ctx context.Context, prompt string) (string, error)

// BuildToolCallRepairPrompt assembles the repair prompt from three parts, in
// order (plan §1):
//  1. the tool call format spec (verbatim, ToolCallFormatSpec),
//  2. the fixed instruction (ToolCallRepairFixedPrompt),
//  3. the bad tool-call code, with its CDATA contents replaced by DS_SLOT_{idx}
//     placeholders so the model only sees the skeleton.
//
// It returns the prompt plus the slot table / token needed to restore the
// placeholders in the model's output. When the bad code has no closed CDATA the
// returned slots are empty and the skeleton equals badCode.
func BuildToolCallRepairPrompt(badCode string) (prompt string, slots []string, token cdataSlotToken) {
	slotRes := slotCDATAContent(badCode)
	skeleton := slotRes.skeleton
	var b strings.Builder
	b.Grow(len(skeleton) + 4096)
	b.WriteString(ToolCallFormatSpec())
	b.WriteString("\n\n")
	b.WriteString(ToolCallRepairFixedPrompt)
	b.WriteString("\n\n")
	b.WriteString(skeleton)
	return b.String(), slotRes.slots, slotRes.token
}

// RepairToolCallsWithLLM attempts to repair badCode (the residual intent text
// identified by phase 1) into valid tool calls using the provided LLM invoker.
// It implements the three-way branch semantics from plan §4:
//
//	① valid format  → parse into tool calls, return (calls, true)
//	② "NO TOOL"     → return (nil, false); caller emits the fallback verbatim
//	③ unparseable / timeout / error / slot mismatch → (nil, false); fallback
//
// Any failure (invoker error, missing slot, byte mismatch, unparseable output)
// degrades to (nil, false): the caller must never surface half-repaired content
// or leaked DS_SLOT placeholders downstream (hard constraint 3).
func RepairToolCallsWithLLM(ctx context.Context, badCode string, invoke ToolCallRepairInvoker) ([]ParsedToolCall, bool) {
	if invoke == nil || strings.TrimSpace(badCode) == "" {
		return nil, false
	}
	prompt, slots, token := BuildToolCallRepairPrompt(badCode)
	out, err := invoke(ctx, prompt)
	if err != nil {
		// Branch ③: upstream/network error or timeout.
		return nil, false
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return nil, false
	}
	// Branch ②: the model declared the fragment is not a tool call.
	if strings.EqualFold(trimmed, toolCallRepairNoToolSentinel) {
		return nil, false
	}

	// Restore the DS_SLOT placeholders back to the original CDATA bytes. If the
	// model dropped/renamed a placeholder, moved one outside CDATA, or matched
	// it twice, restoreCDATASlots errors and we fall back (branch ③) rather
	// than leaking a placeholder literal.
	restored := out
	if len(slots) > 0 {
		var restoreErr error
		restored, restoreErr = restoreCDATASlots(out, slots, token)
		if restoreErr != nil {
			return nil, false
		}
	}

	parsed := parseToolCallsDetailedXMLOnly(restored)
	if len(parsed.Calls) == 0 {
		// Branch ③: output was not parseable as a tool call.
		return nil, false
	}
	// Branch ①: valid format parsed into one or more tool calls.
	return parsed.Calls, true
}
