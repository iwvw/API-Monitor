package toolcall

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestBuildToolCallRepairPromptStructure(t *testing.T) {
	bad := `<|EPSE|call name="Bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|call>`
	prompt, slots, token := BuildToolCallRepairPrompt(bad)

	if !strings.HasPrefix(prompt, ToolCallFormatSpec()) {
		t.Fatalf("prompt must begin with the verbatim format spec")
	}
	if !strings.Contains(prompt, ToolCallRepairFixedPrompt) {
		t.Fatalf("prompt must contain the fixed instruction verbatim")
	}
	// The CDATA content must be slotted; the raw value must not be visible.
	if len(slots) != 1 || slots[0] != "pwd" {
		t.Fatalf("expected one slot 'pwd', got %#v", slots)
	}
	if !strings.Contains(prompt, token.format(0)) {
		t.Fatalf("prompt should carry the DS_SLOT placeholder, got: %s", prompt)
	}
	// Format spec then fixed prompt then skeleton, in order.
	idxSpec := strings.Index(prompt, "工具调用格式规范")
	idxFixed := strings.Index(prompt, ToolCallRepairFixedPrompt)
	idxSkeleton := strings.Index(prompt, `<|EPSE|call name="Bash">`)
	if idxSpec >= idxFixed || idxFixed >= idxSkeleton {
		t.Fatalf("prompt parts out of order: spec=%d fixed=%d skeleton=%d", idxSpec, idxFixed, idxSkeleton)
	}
}

func TestRepairToolCallsWithLLMValidFormat(t *testing.T) {
	bad := `<|EPSE|call name="Bash"><|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter></|EPSE|call>`
	invoke := func(_ context.Context, prompt string) (string, error) {
		// Model returns the same shape but with the correct invoke local name.
		// It must reproduce the DS_SLOT placeholder verbatim.
		if !strings.Contains(prompt, "DS_SLOT_0") {
			t.Fatalf("prompt missing placeholder")
		}
		return `<|EPSE|tool_calls><|EPSE|invoke name="Bash"><|EPSE|parameter name="command"><![CDATA[DS_SLOT_0]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`, nil
	}
	calls, ok := RepairToolCallsWithLLM(context.Background(), bad, invoke)
	if !ok || len(calls) != 1 {
		t.Fatalf("expected one repaired call, got ok=%v calls=%#v", ok, calls)
	}
	if calls[0].Name != "Bash" {
		t.Fatalf("expected Bash call, got %q", calls[0].Name)
	}
	if got, _ := calls[0].Input["command"].(string); got != "pwd" {
		t.Fatalf("expected restored command 'pwd', got %q", got)
	}
}

func TestRepairToolCallsWithLLMNoTool(t *testing.T) {
	bad := `<|EPSE|call name="x">not really a tool</|EPSE|call>`
	invoke := func(_ context.Context, _ string) (string, error) {
		return "NO TOOL", nil
	}
	calls, ok := RepairToolCallsWithLLM(context.Background(), bad, invoke)
	if ok || len(calls) != 0 {
		t.Fatalf("expected NO TOOL branch to yield no calls, got ok=%v calls=%#v", ok, calls)
	}
}

func TestRepairToolCallsWithLLMNoToolCaseInsensitive(t *testing.T) {
	invoke := func(_ context.Context, _ string) (string, error) {
		return "  no tool  ", nil
	}
	if calls, ok := RepairToolCallsWithLLM(context.Background(), `<|EPSE|call>x</|EPSE|call>`, invoke); ok || len(calls) != 0 {
		t.Fatalf("expected trimmed/case-insensitive NO TOOL to be honored")
	}
}

func TestRepairToolCallsWithLLMTimeoutError(t *testing.T) {
	invoke := func(_ context.Context, _ string) (string, error) {
		return "", errors.New("timeout")
	}
	if calls, ok := RepairToolCallsWithLLM(context.Background(), `<|EPSE|call>x</|EPSE|call>`, invoke); ok || len(calls) != 0 {
		t.Fatalf("expected error/timeout to route to fallback branch ③")
	}
}

func TestRepairToolCallsWithLLMUnparseableOutput(t *testing.T) {
	invoke := func(_ context.Context, _ string) (string, error) {
		return "Here is the fixed code: <not-a-tool-call>", nil
	}
	if calls, ok := RepairToolCallsWithLLM(context.Background(), `<|EPSE|call>x</|EPSE|call>`, invoke); ok || len(calls) != 0 {
		t.Fatalf("expected unparseable output to route to fallback branch ③")
	}
}

func TestRepairToolCallsWithLLMPlaceholderRewrittenDegrades(t *testing.T) {
	bad := `<|EPSE|call name="Bash"><|EPSE|parameter name="command"><![CDATA[secret payload]]></|EPSE|parameter></|EPSE|call>`
	invoke := func(_ context.Context, _ string) (string, error) {
		// The model corrupts the placeholder: restore must fail and we must NOT
		// leak the placeholder literal or emit a half-repaired call.
		return `<|EPSE|tool_calls><|EPSE|invoke name="Bash"><|EPSE|parameter name="command"><![CDATA[DS_SLOT_999]]></|EPSE|parameter></|EPSE|invoke></|EPSE|tool_calls>`, nil
	}
	if calls, ok := RepairToolCallsWithLLM(context.Background(), bad, invoke); ok || len(calls) != 0 {
		t.Fatalf("expected placeholder mismatch to degrade to fallback, got ok=%v calls=%#v", ok, calls)
	}
}

func TestRepairToolCallsWithLLMNilInvokerOrEmpty(t *testing.T) {
	if calls, ok := RepairToolCallsWithLLM(context.Background(), `<|EPSE|call>x</|EPSE|call>`, nil); ok || len(calls) != 0 {
		t.Fatalf("nil invoker must yield no calls")
	}
	invoke := func(_ context.Context, _ string) (string, error) { return "x", nil }
	if calls, ok := RepairToolCallsWithLLM(context.Background(), "   ", invoke); ok || len(calls) != 0 {
		t.Fatalf("empty bad code must yield no calls")
	}
}

func TestToolCallFormatSpecStableForBuildInstructions(t *testing.T) {
	// The spec returned standalone must be a byte-identical prefix of the full
	// instruction block so the repair model sees exactly the injected rules.
	full := BuildToolCallInstructions([]string{"Bash"})
	if !strings.HasPrefix(full, ToolCallFormatSpec()) {
		t.Fatalf("ToolCallFormatSpec must be a verbatim prefix of BuildToolCallInstructions")
	}
	spec := ToolCallFormatSpec()
	for _, want := range []string{
		"工具调用格式规范 — 请严格遵照执行：",
		"切勿省略起始标签 <|EPSE|tool_calls>",
		"请记住：使用工具的唯一正确方式是在回复末尾使用 <|EPSE|tool_calls>...</|EPSE|tool_calls> 代码块。",
	} {
		if !strings.Contains(spec, want) {
			t.Fatalf("format spec missing %q", want)
		}
	}
}
