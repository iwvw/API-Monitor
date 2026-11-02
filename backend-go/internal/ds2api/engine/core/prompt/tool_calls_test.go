package prompt

import "testing"

func TestStringifyToolCallArgumentsPreservesConcatenatedJSON(t *testing.T) {
	got := StringifyToolCallArguments(`{}{"query":"测试工具调用"}`)
	if got != `{}{"query":"测试工具调用"}` {
		t.Fatalf("expected raw concatenated JSON to be preserved, got %q", got)
	}
}

func TestFormatToolCallsForPromptEPSE(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"id": "call_1",
			"function": map[string]any{
				"name":      "search_web",
				"arguments": map[string]any{"query": "latest"},
			},
		},
	})
	if got == "" {
		t.Fatal("expected non-empty formatted tool calls")
	}
	if got != "<|EPSE|tool_calls>\n  <|EPSE|invoke name=\"search_web\">\n    <|EPSE|parameter name=\"query\"><![CDATA[latest]]></|EPSE|parameter>\n  </|EPSE|invoke>\n</|EPSE|tool_calls>" {
		t.Fatalf("unexpected formatted tool call EPSE: %q", got)
	}
}

func TestFormatToolCallsForPromptEscapesXMLEntities(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"name":      "search<&>",
			"arguments": `{"q":"a < b && c > d"}`,
		},
	})
	want := "<|EPSE|tool_calls>\n  <|EPSE|invoke name=\"search&lt;&amp;&gt;\">\n    <|EPSE|parameter name=\"q\"><![CDATA[a < b && c > d]]></|EPSE|parameter>\n  </|EPSE|invoke>\n</|EPSE|tool_calls>"
	if got != want {
		t.Fatalf("unexpected escaped tool call XML: %q", got)
	}
}

func TestFormatToolCallsForPromptUsesCDATAForMultilineContent(t *testing.T) {
	got := FormatToolCallsForPrompt([]any{
		map[string]any{
			"name": "write_file",
			"arguments": map[string]any{
				"path":    "script.sh",
				"content": "#!/bin/bash\nprintf \"hello\"\n",
			},
		},
	})
	want := "<|EPSE|tool_calls>\n  <|EPSE|invoke name=\"write_file\">\n    <|EPSE|parameter name=\"content\"><![CDATA[#!/bin/bash\nprintf \"hello\"\n]]></|EPSE|parameter>\n    <|EPSE|parameter name=\"path\"><![CDATA[script.sh]]></|EPSE|parameter>\n  </|EPSE|invoke>\n</|EPSE|tool_calls>"
	if got != want {
		t.Fatalf("unexpected multiline cdata tool call XML: %q", got)
	}
}
