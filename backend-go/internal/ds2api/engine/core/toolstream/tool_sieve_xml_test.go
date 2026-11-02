package toolstream

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
	"strings"
	"testing"
)

func TestProcessToolSieveInterceptsXMLToolCallWithoutLeak(t *testing.T) {
	var state State
	// Simulate a model producing XML tool call output chunk by chunk.
	chunks := []string{
		"<tool_calls>\n",
		`  <invoke name="read_file">` + "\n",
		`    <parameter name="path">README.MD</parameter>` + "\n",
		"  </invoke>\n",
		"</tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(textContent, "<invoke ") {
		t.Fatalf("XML tool call content leaked to text: %q", textContent)
	}
	if strings.Contains(textContent, "read_file") {
		t.Fatalf("tool name leaked to text: %q", textContent)
	}
	if toolCalls == 0 {
		t.Fatal("expected tool calls to be extracted, got none")
	}
}

func TestProcessToolSieveInterceptsEPSEToolCallWithoutLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<|EPSE|tool",
		"_calls>\n",
		`  <|EPSE|invoke name="read_file">` + "\n",
		`    <|EPSE|parameter name="path">README.MD</|EPSE|parameter>` + "\n",
		"  </|EPSE|invoke>\n",
		"</|EPSE|tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		textContent += evt.Content
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(strings.ToLower(textContent), "epse") || strings.Contains(textContent, "read_file") {
		t.Fatalf("EPSE tool call content leaked to text: %q", textContent)
	}
	if toolCalls != 1 {
		t.Fatalf("expected one EPSE tool call, got %d events=%#v", toolCalls, events)
	}
}

func TestProcessToolSieveInterceptsEPSETrailingPipeToolCallWithoutLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<|EPSE|tool_calls| \n",
		`  <|EPSE|invoke name="terminal">` + "\n",
		`    <|EPSE|parameter name="command"><![CDATA[find "/home" -type d]]></|EPSE|parameter>` + "\n",
		`    <|EPSE|parameter name="timeout"><![CDATA[10]]></|EPSE|parameter>` + "\n",
		"  </|EPSE|invoke>\n",
		"</|EPSE|tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"terminal"})...)
	}
	events = append(events, Flush(&state, []string{"terminal"})...)

	var textContent strings.Builder
	var calls []any
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		for _, call := range evt.ToolCalls {
			calls = append(calls, call)
		}
	}
	if text := textContent.String(); strings.Contains(strings.ToLower(text), "epse") || strings.Contains(text, "terminal") {
		t.Fatalf("trailing-pipe EPSE tool call leaked to text: %q events=%#v", text, events)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one trailing-pipe EPSE tool call, got %d events=%#v", len(calls), events)
	}
}

func TestProcessToolSieveInterceptsEPSEControlSeparatorWithoutLeak(t *testing.T) {
	for _, tc := range []struct {
		name string
		sep  string
	}{
		{name: "control_picture", sep: "␂"},
		{name: "raw_stx", sep: "\x02"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sep := tc.sep
			var state State
			chunks := []string{
				"<EPSE" + sep + "tool",
				"_calls>\n",
				`  <EPSE` + sep + `invoke name="Read">` + "\n",
				`    <EPSE` + sep + `parameter name="file_path"><![CDATA[/tmp/input.txt]]></EPSE` + sep + `parameter>` + "\n",
				"  </EPSE" + sep + "invoke>\n",
				"</EPSE" + sep + "tool_calls>",
			}
			var events []Event
			for _, c := range chunks {
				events = append(events, ProcessChunk(&state, c, []string{"Read"})...)
			}
			events = append(events, Flush(&state, []string{"Read"})...)

			var textContent strings.Builder
			var calls []any
			for _, evt := range events {
				textContent.WriteString(evt.Content)
				for _, call := range evt.ToolCalls {
					calls = append(calls, call)
				}
			}
			if text := textContent.String(); strings.Contains(strings.ToLower(text), "epse") || strings.Contains(text, "Read") || strings.Contains(text, sep) {
				t.Fatalf("control-separator EPSE tool call leaked to text: %q events=%#v", text, events)
			}
			if len(calls) != 1 {
				t.Fatalf("expected one control-separator EPSE tool call, got %d events=%#v", len(calls), events)
			}
		})
	}
}

func TestProcessToolSieveInterceptsArbitraryPrefixedToolTagsWithoutLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<proto💥tool",
		"_calls>\n",
		`  <proto💥invoke name="Read">` + "\n",
		`    <proto💥parameter name="file_path"><![CDATA[/tmp/input.txt]]></proto💥parameter>` + "\n",
		"  </proto💥invoke>\n",
		"</proto💥tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"Read"})...)
	}
	events = append(events, Flush(&state, []string{"Read"})...)

	var textContent strings.Builder
	var calls []any
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		for _, call := range evt.ToolCalls {
			calls = append(calls, call)
		}
	}
	if text := textContent.String(); strings.Contains(text, "proto") || strings.Contains(text, "Read") || strings.Contains(text, "💥") {
		t.Fatalf("arbitrary-prefixed tool call leaked to text: %q events=%#v", text, events)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one arbitrary-prefixed tool call, got %d events=%#v", len(calls), events)
	}
}

func TestProcessToolSieveEmitsEmptyEPSEControlSeparatorBlockWithoutLeak(t *testing.T) {
	sep := "␂"
	chunks := []string{
		"<EPSE" + sep + "tool_calls>\n",
		`  <EPSE` + sep + `invoke name="Read">` + "\n",
		`    <EPSE` + sep + `parameter name="file_path"></EPSE` + sep + `parameter>` + "\n",
		"  </EPSE" + sep + "invoke>\n",
		"</EPSE" + sep + "tool_calls>",
	}
	calls := collectToolCallsForChunks(t, chunks, []string{"Read"})
	if len(calls) != 1 {
		t.Fatalf("expected empty control-separator block to produce one call, got %#v", calls)
	}
	if calls[0].Name != "Read" || calls[0].Input["file_path"] != "" {
		t.Fatalf("expected empty file_path parameter to be preserved, got %#v", calls)
	}
}

func TestProcessToolSieveInterceptsExtraLeadingLessThanEPSEToolCallWithoutLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<<|EPSE|tool_calls>\n",
		`  <<|EPSE|invoke name="Bash">` + "\n",
		`    <<|EPSE|parameter name="command"><![CDATA[pwd]]></|EPSE|parameter>` + "\n",
		"  </|EPSE|invoke>\n",
		"</|EPSE|tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"Bash"})...)
	}
	events = append(events, Flush(&state, []string{"Bash"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if text := textContent.String(); strings.Contains(text, "<") || strings.Contains(text, "Bash") {
		t.Fatalf("extra-leading-less-than EPSE tool call leaked to text: %q events=%#v", text, events)
	}
	if toolCalls != 1 {
		t.Fatalf("expected one extra-leading-less-than EPSE tool call, got %d events=%#v", toolCalls, events)
	}
}

func TestProcessToolSieveInterceptsRepeatedEPSEPrefixNoiseWithoutLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<<EPSE|EPSE|tool",
		"_calls>\n",
		`  <<EPSE|EPSE|invoke name="Bash">` + "\n",
		`    <<EPSE|EPSE|parameter name="command"><![CDATA[git status]]></EPSE|EPSE|parameter>` + "\n",
		"  </EPSE|EPSE|invoke>\n",
		"</EPSE|EPSE|tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"Bash"})...)
	}
	events = append(events, Flush(&state, []string{"Bash"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if text := textContent.String(); strings.Contains(strings.ToLower(text), "epse") || strings.Contains(text, "Bash") {
		t.Fatalf("repeated-prefix EPSE tool call leaked to text: %q events=%#v", text, events)
	}
	if toolCalls != 1 {
		t.Fatalf("expected one repeated-prefix EPSE tool call, got %d events=%#v", toolCalls, events)
	}
}

func TestProcessToolSieveHandlesLongXMLToolCall(t *testing.T) {
	var state State
	const toolName = "write_to_file"
	payload := strings.Repeat("x", 4096)
	splitAt := len(payload) / 2
	chunks := []string{
		"<tool_calls>\n  <invoke name=\"" + toolName + "\">\n    <parameter name=\"content\"><![CDATA[",
		payload[:splitAt],
		payload[splitAt:],
		"]]></parameter>\n  </invoke>\n</tool_calls>",
	}

	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{toolName})...)
	}
	events = append(events, Flush(&state, []string{toolName})...)

	var textContent strings.Builder
	toolCalls := 0
	var gotPayload any
	for _, evt := range events {
		if evt.Content != "" {
			textContent.WriteString(evt.Content)
		}
		if len(evt.ToolCalls) > 0 && gotPayload == nil {
			gotPayload = evt.ToolCalls[0].Input["content"]
		}
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 1 {
		t.Fatalf("expected one long XML tool call, got %d events=%#v", toolCalls, events)
	}
	if textContent.Len() != 0 {
		t.Fatalf("expected no leaked text for long XML tool call, got %q", textContent.String())
	}
	got, _ := gotPayload.(string)
	if got != payload {
		t.Fatalf("expected long XML payload to survive intact, got len=%d want=%d", len(got), len(payload))
	}
}

func TestProcessToolSieveKeepsCDATAEmbeddedToolClosingBuffered(t *testing.T) {
	var state State
	payload := strings.Join([]string{
		"# DS2API 4.0 更新内容",
		"",
		strings.Repeat("x", 4096),
		"```xml",
		"<tool_calls>",
		"  <invoke name=\"demo\">",
		"    <parameter name=\"value\">x</parameter>",
		"  </invoke>",
		"</tool_calls>",
		"```",
		"tail",
	}, "\n")
	innerClose := strings.Index(payload, "</tool_calls>") + len("</tool_calls>")
	chunks := []string{
		"<tool_calls>\n  <invoke name=\"Write\">\n    <parameter name=\"content\"><![CDATA[",
		payload[:innerClose],
		payload[innerClose:],
		"]]></parameter>\n    <parameter name=\"file_path\">DS2API-4.0-Release-Notes.md</parameter>\n  </invoke>\n</tool_calls>",
	}

	var events []Event
	for i, c := range chunks {
		next := ProcessChunk(&state, c, []string{"Write"})
		if i <= 1 {
			for _, evt := range next {
				if evt.Content != "" || len(evt.ToolCalls) > 0 {
					t.Fatalf("expected no events before outer closing tag, chunk=%d events=%#v", i, next)
				}
			}
		}
		events = append(events, next...)
	}
	events = append(events, Flush(&state, []string{"Write"})...)

	var textContent strings.Builder
	var gotPayload string
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		if len(evt.ToolCalls) > 0 {
			toolCalls += len(evt.ToolCalls)
			gotPayload, _ = evt.ToolCalls[0].Input["content"].(string)
		}
	}

	if toolCalls != 1 {
		t.Fatalf("expected one parsed tool call, got %d events=%#v", toolCalls, events)
	}
	if textContent.Len() != 0 {
		t.Fatalf("expected no leaked text, got %q", textContent.String())
	}
	if gotPayload != payload {
		t.Fatalf("expected full CDATA payload to survive intact, got len=%d want=%d", len(gotPayload), len(payload))
	}
}

func TestProcessToolSieveKeepsExtremeHereDocCDATAUntilOuterClose(t *testing.T) {
	var state State
	command := strings.Join([]string{
		"cat > docs/project-value.md << 'ENDOFFILE'",
		"# DS2API project value",
		"",
		"```xml",
		`<|EPSE|tool_calls>`,
		`  <|EPSE|invoke name="Bash">`,
		`    <|EPSE|parameter name="command"><![CDATA[grep -E "error|fail" < input.log 2>&1]]></|EPSE|parameter>`,
		`  </|EPSE|invoke>`,
		`</|EPSE|tool_calls>`,
		"```",
		"",
		"Only the literal `]]>` needs special handling.",
		"",
		"ENDOFFILE",
		`echo "Done. Lines: $(wc -l < docs/project-value.md)"`,
	}, "\n")
	innerClose := strings.Index(command, `</|EPSE|tool_calls>`) + len(`</|EPSE|tool_calls>`)
	chunks := []string{
		`<|EPSE|tool_calls>` + "\n",
		`<|EPSE|invoke name="Bash">` + "\n",
		`<|EPSE|parameter name="command"><![CDATA[` + command[:innerClose],
		command[innerClose:],
		`]]></|EPSE|parameter>` + "\n",
		`<|EPSE|parameter name="description"><![CDATA[Write project value doc]]></|EPSE|parameter>` + "\n",
		`</|EPSE|invoke>` + "\n",
		`</|EPSE|tool_calls>`,
	}

	var events []Event
	for i, c := range chunks {
		next := ProcessChunk(&state, c, []string{"Bash"})
		if i <= 2 {
			for _, evt := range next {
				if evt.Content != "" || len(evt.ToolCalls) > 0 {
					t.Fatalf("expected no events before outer close, chunk=%d events=%#v", i, next)
				}
			}
		}
		events = append(events, next...)
	}
	events = append(events, Flush(&state, []string{"Bash"})...)

	var textContent strings.Builder
	var gotCommand string
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		if len(evt.ToolCalls) > 0 {
			toolCalls += len(evt.ToolCalls)
			gotCommand, _ = evt.ToolCalls[0].Input["command"].(string)
		}
	}
	if toolCalls != 1 {
		t.Fatalf("expected one parsed tool call, got %d events=%#v", toolCalls, events)
	}
	if textContent.Len() != 0 {
		t.Fatalf("expected no leaked text, got %q", textContent.String())
	}
	if gotCommand != command {
		t.Fatalf("expected full heredoc command to survive, got len=%d want=%d", len(gotCommand), len(command))
	}
}

func TestProcessToolSieveKeepsCompactCDATAWithImmediateFencedEPSE(t *testing.T) {
	var state State
	content := strings.Join([]string{
		"```xml",
		`<|EPSE|tool_calls>`,
		`  <|EPSE|invoke name="Bash">`,
		`    <|EPSE|parameter name="command"><![CDATA[echo compact]]></|EPSE|parameter>`,
		`  </|EPSE|invoke>`,
		`</|EPSE|tool_calls>`,
		"```",
		"tail",
	}, "\n")
	chunks := []string{
		`<tool_calls><invoke name="Write"><parameter name="content"><![CDATA[` + content[:len("```xml\n")],
		content[len("```xml\n"):],
		`]]></parameter></invoke></tool_calls>`,
	}

	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"Write"})...)
	}
	events = append(events, Flush(&state, []string{"Write"})...)

	var textContent strings.Builder
	var gotContent string
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		if len(evt.ToolCalls) > 0 {
			toolCalls += len(evt.ToolCalls)
			gotContent, _ = evt.ToolCalls[0].Input["content"].(string)
		}
	}
	if toolCalls != 1 {
		t.Fatalf("expected one compact CDATA tool call, got %d events=%#v", toolCalls, events)
	}
	if textContent.Len() != 0 {
		t.Fatalf("expected no leaked text, got %q", textContent.String())
	}
	if gotContent != content {
		t.Fatalf("expected compact CDATA content to survive, got len=%d want=%d", len(gotContent), len(content))
	}
}

func TestProcessToolSieveFallsBackWhenCDATANeverCloses(t *testing.T) {
	var state State
	chunks := []string{
		"<tool_calls>\n  <invoke name=\"Write\">\n    <parameter name=\"content\"><![CDATA[",
		"hello world",
		"</parameter>\n  </invoke>\n</tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"Write"})...)
	}
	events = append(events, Flush(&state, []string{"Write"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		if evt.Content != "" {
			textContent.WriteString(evt.Content)
		}
		toolCalls += len(evt.ToolCalls)
		if len(evt.ToolCalls) > 0 {
			if got, _ := evt.ToolCalls[0].Input["content"].(string); got != "hello world" {
				t.Fatalf("expected recovered CDATA payload, got %q", got)
			}
		}
	}

	if toolCalls != 1 {
		t.Fatalf("expected unclosed CDATA payload to still parse, got %d tool calls events=%#v", toolCalls, events)
	}
	if textContent.Len() != 0 {
		t.Fatalf("expected no leaked text, got %q", textContent.String())
	}
}

func TestProcessToolSieveXMLWithLeadingText(t *testing.T) {
	var state State
	// Model outputs some prose then an XML tool call.
	chunks := []string{
		"Let me check the file.\n",
		"<tool_calls>\n  <invoke name=\"read_file\">\n",
		`    <parameter name="path">go.mod</parameter>` + "\n  </invoke>\n</tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
		toolCalls += len(evt.ToolCalls)
	}

	// Leading text should be emitted.
	if !strings.Contains(textContent, "Let me check the file.") {
		t.Fatalf("expected leading text to be emitted, got %q", textContent)
	}
	// The XML itself should NOT leak.
	if strings.Contains(textContent, "<invoke ") {
		t.Fatalf("XML tool call content leaked to text: %q", textContent)
	}
	if toolCalls == 0 {
		t.Fatal("expected tool calls to be extracted, got none")
	}
}

func TestProcessToolSievePassesThroughNonToolXMLBlock(t *testing.T) {
	var state State
	chunk := `<tool><title>示例 XML</title><body>plain text xml payload</body></tool>`
	events := ProcessChunk(&state, chunk, []string{"read_file"})
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 0 {
		t.Fatalf("expected no tool calls for plain XML payload, got %d events=%#v", toolCalls, events)
	}
	if textContent.String() != chunk {
		t.Fatalf("expected XML payload to pass through unchanged, got %q", textContent.String())
	}
}

func TestProcessToolSieveNonToolXMLKeepsSuffixForToolParsing(t *testing.T) {
	var state State
	chunk := `<tool><title>plain xml</title></tool><tool_calls><invoke name="read_file"><parameter name="path">README.MD</parameter></invoke></tool_calls>`
	events := ProcessChunk(&state, chunk, []string{"read_file"})
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if !strings.Contains(textContent.String(), `<tool><title>plain xml</title></tool>`) {
		t.Fatalf("expected leading non-tool XML to be preserved, got %q", textContent.String())
	}
	if strings.Contains(textContent.String(), `<tool_calls><invoke`) {
		t.Fatalf("expected invoke tool XML to be intercepted, got %q", textContent.String())
	}
	if toolCalls != 1 {
		t.Fatalf("expected exactly one parsed tool call from suffix, got %d events=%#v", toolCalls, events)
	}
}

func TestProcessToolSieveReleasesMalformedExecutableXMLBlock(t *testing.T) {
	var state State
	chunk := `<tool_calls><invoke name="read_file"><param>{"path":"README.md"}</param></invoke></tool_calls>`
	events := ProcessChunk(&state, chunk, []string{"read_file"})
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected malformed executable-looking XML not to become a tool call, got %d events=%#v", toolCalls, events)
	}
	if textContent.String() != chunk {
		t.Fatalf("expected malformed executable-looking XML to be released as text, got %q", textContent.String())
	}
}

func TestProcessToolSieveEmitsAllEmptyEPSEToolBlock(t *testing.T) {
	chunk := strings.Join([]string{
		`<|EPSE|tool_calls>`,
		`<|EPSE|invoke name="Bash">`,
		`<|EPSE|parameter name="command"></|EPSE|parameter>`,
		`<|EPSE|parameter name="description">   </|EPSE|parameter>`,
		`<|EPSE|parameter name="timeout"></|EPSE|parameter>`,
		`</|EPSE|invoke>`,
		`</|EPSE|tool_calls>`,
	}, "\n")
	calls := collectToolCallsForChunks(t, []string{chunk}, []string{"Bash"})
	if len(calls) != 1 {
		t.Fatalf("expected all-empty EPSE block to produce one tool call, got %#v", calls)
	}
	if calls[0].Input["command"] != "" || calls[0].Input["description"] != "" || calls[0].Input["timeout"] != "" {
		t.Fatalf("expected empty parameters to be preserved, got %#v", calls[0].Input)
	}
}

func TestProcessToolSieveEmitsChunkedAllEmptyArbitraryPrefixedToolBlock(t *testing.T) {
	chunk := strings.Join([]string{
		`<T|EPSE|tool_calls>`,
		`  <T|EPSE|invoke name="TaskOutput">`,
		`  <T|EPSE|parameter name="task_id"></T|EPSE|parameter>`,
		`  <T|EPSE|parameter name="block"></T|EPSE|parameter>`,
		`  <T|EPSE|parameter name="timeout"></T|EPSE|parameter>`,
		`  </T|EPSE|invoke>`,
		`  </T|EPSE|tool_calls>`,
	}, "\n")
	calls := collectToolCallsForChunks(t, splitEveryNRBytes(chunk, 8), []string{"TaskOutput"})
	if len(calls) != 1 {
		t.Fatalf("expected chunked all-empty arbitrary-prefixed block to produce one tool call, got %#v", calls)
	}
	if calls[0].Name != "TaskOutput" || calls[0].Input["task_id"] != "" || calls[0].Input["block"] != "" || calls[0].Input["timeout"] != "" {
		t.Fatalf("expected empty TaskOutput parameters to be preserved, got %#v", calls)
	}
}

func collectToolCallsForChunks(t *testing.T, chunks []string, toolNames []string) []toolcall.ParsedToolCall {
	t.Helper()
	var state State
	var events []Event
	for _, chunk := range chunks {
		events = append(events, ProcessChunk(&state, chunk, toolNames)...)
	}
	events = append(events, Flush(&state, toolNames)...)

	var textContent strings.Builder
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		calls = append(calls, evt.ToolCalls...)
	}
	if textContent.Len() != 0 {
		t.Fatalf("expected tool block not to leak as text, got %q", textContent.String())
	}
	return calls
}

func splitEveryNRBytes(s string, n int) []string {
	if n <= 0 {
		return []string{s}
	}
	out := make([]string, 0, len(s)/n+1)
	for len(s) > 0 {
		if len(s) <= n {
			out = append(out, s)
			break
		}
		out = append(out, s[:n])
		s = s[n:]
	}
	return out
}

func TestProcessToolSievePassesThroughFencedXMLToolCallExamples(t *testing.T) {
	var state State
	input := strings.Join([]string{
		"Before first example.\n```",
		"xml\n<tool_calls><invoke name=\"read_file\"><parameter name=\"path\">README.md</parameter></invoke></tool_calls>\n```\n",
		"Between examples.\n```xml\n",
		"<tool_calls><invoke name=\"search\"><parameter name=\"q\">golang</parameter></invoke></tool_calls>\n",
		"```\nAfter examples.",
	}, "")

	chunks := []string{
		"Before first example.\n```",
		"xml\n<tool_calls><invoke name=\"read_file\"><parameter name=\"path\">README.md</parameter></invoke></tool_calls>\n```\n",
		"Between examples.\n```xml\n",
		"<tool_calls><invoke name=\"search\"><parameter name=\"q\">golang</parameter></invoke></tool_calls>\n",
		"```\nAfter examples.",
	}

	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file", "search"})...)
	}
	events = append(events, Flush(&state, []string{"read_file", "search"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		if evt.Content != "" {
			textContent.WriteString(evt.Content)
		}
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected fenced XML examples to stay text, got %d tool calls events=%#v", toolCalls, events)
	}
	if textContent.String() != input {
		t.Fatalf("expected fenced XML examples to pass through unchanged, got %q", textContent.String())
	}
}

func TestProcessToolSieveKeepsPartialXMLTagInsideFencedExample(t *testing.T) {
	var state State
	input := strings.Join([]string{
		"Example:\n```xml\n<tool_ca",
		"lls><invoke name=\"read_file\"><parameter name=\"path\">README.md</parameter></invoke></tool_calls>\n```\n",
		"Done.",
	}, "")

	chunks := []string{
		"Example:\n```xml\n<tool_ca",
		"lls><invoke name=\"read_file\"><parameter name=\"path\">README.md</parameter></invoke></tool_calls>\n```\n",
		"Done.",
	}

	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		if evt.Content != "" {
			textContent.WriteString(evt.Content)
		}
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected partial fenced XML to stay text, got %d tool calls events=%#v", toolCalls, events)
	}
	if textContent.String() != input {
		t.Fatalf("expected partial fenced XML to pass through unchanged, got %q", textContent.String())
	}
}

func TestProcessToolSievePartialXMLTagHeldBack(t *testing.T) {
	var state State
	// Chunk ends with a partial XML tool tag.
	events := ProcessChunk(&state, "Hello <too", []string{"read_file"})

	var textContent string
	for _, evt := range events {
		textContent += evt.Content
	}

	// "Hello " should be emitted, but "<too" should be held back.
	if strings.Contains(textContent, "<too") {
		t.Fatalf("partial XML tag should not be emitted, got %q", textContent)
	}
	if !strings.Contains(textContent, "Hello") {
		t.Fatalf("expected 'Hello' text to be emitted, got %q", textContent)
	}
}

func TestFindToolSegmentStartDetectsXMLToolCalls(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"tool_calls_tag", "some text <tool_calls>\n", 10},
		{"epse_trailing_pipe_tag", "some text <|EPSE|tool_calls| \n", 10},
		{"epse_extra_leading_less_than", "some text <<|EPSE|tool_calls>\n", 10},
		{"invoke_tag_missing_wrapper", "some text <invoke name=\"read_file\">\n", 10},
		{"bare_tool_call_text", "prefix <tool_call>\n", -1},
		{"xml_inside_code_fence", "```xml\n<tool_calls><invoke name=\"read_file\"></invoke></tool_calls>\n```", -1},
		{"no_xml", "just plain text", -1},
		{"gemini_json_no_detect", `some text {"functionCall":{"name":"search"}}`, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findToolSegmentStart(nil, tc.input)
			if got != tc.want {
				t.Fatalf("findToolSegmentStart(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestFindPartialXMLToolTagStart(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"partial_tool_calls", "Hello <tool_ca", 6},
		{"partial_epse_trailing_pipe", "Hello <|EPSE|tool_calls|", 6},
		{"partial_epse_extra_leading_less_than", "Hello <<|EPSE|tool_calls", 6},
		{"partial_arbitrary_prefix_before_epse", "Hello <T|EP", 6},
		{"partial_arbitrary_prefix_after_epse_pipe", "Hello <T|EPSE|", 6},
		{"partial_invoke", "Hello <inv", 6},
		{"bare_tool_call_not_held", "Hello <tool_name", -1},
		{"partial_lt_only", "Text <", 5},
		{"complete_tag", "Text <tool_calls>done", -1},
		{"no_lt", "plain text", -1},
		{"closed_lt", "a < b > c", -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findPartialXMLToolTagStart(tc.input)
			if got != tc.want {
				t.Fatalf("findPartialXMLToolTagStart(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestHasOpenXMLToolTag(t *testing.T) {
	if !hasOpenXMLToolTag("<tool_calls>\n<invoke name=\"foo\">") {
		t.Fatal("should detect open XML tool tag without closing tag")
	}
	if hasOpenXMLToolTag("<tool_calls>\n<invoke name=\"foo\"></invoke>\n</tool_calls>") {
		t.Fatal("should return false when closing tag is present")
	}
	if hasOpenXMLToolTag("plain text without any XML") {
		t.Fatal("should return false for plain text")
	}
}

// Test the EXACT scenario the user reports: token-by-token streaming where
// <tool_calls> tag arrives in small pieces.
func TestProcessToolSieveTokenByTokenXMLNoLeak(t *testing.T) {
	var state State
	// Simulate DeepSeek model generating tokens one at a time.
	chunks := []string{
		"<",
		"tool",
		"_ca",
		"lls",
		">\n",
		"  <in",
		"voke",
		` name="`,
		"read",
		"_file",
		`">` + "\n",
		"    <para",
		`meter name="path">`,
		"README.MD",
		"</parameter>\n",
		"  </invoke>\n",
		"</",
		"tool_calls",
		">",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(textContent, "<invoke ") {
		t.Fatalf("XML tool call content leaked to text in token-by-token mode: %q", textContent)
	}
	if strings.Contains(textContent, "tool_calls>") {
		t.Fatalf("closing tag fragment leaked to text: %q", textContent)
	}
	if strings.Contains(textContent, "read_file") {
		t.Fatalf("tool name leaked to text: %q", textContent)
	}
	if toolCalls == 0 {
		t.Fatal("expected tool calls to be extracted, got none")
	}
}

// Test that Flush on incomplete XML falls back to raw text.
func TestFlushToolSieveIncompleteXMLFallsBackToText(t *testing.T) {
	var state State
	// XML block starts but stream ends before completion.
	chunks := []string{
		"<tool_calls>\n",
		"  <invoke name=\"read_file\">\n",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	// Stream ends abruptly - flush should NOT dump raw XML.
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent string
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
	}

	if textContent != strings.Join(chunks, "") {
		t.Fatalf("expected incomplete XML to fall back to raw text, got %q", textContent)
	}
}

// Test that the opening tag "<tool_calls>\n  " is NOT emitted as text content.
func TestOpeningXMLTagNotLeakedAsContent(t *testing.T) {
	var state State
	// First chunk is the opening tag - should be held, not emitted.
	evts1 := ProcessChunk(&state, "<tool_calls>\n  ", []string{"read_file"})
	for _, evt := range evts1 {
		if strings.Contains(evt.Content, "<tool_calls>") {
			t.Fatalf("opening tag leaked on first chunk: %q", evt.Content)
		}
	}

	// Remaining content arrives.
	evts2 := ProcessChunk(&state, "<invoke name=\"read_file\">\n    <parameter name=\"path\">README.MD</parameter>\n  </invoke>\n</tool_calls>", []string{"read_file"})
	evts2 = append(evts2, Flush(&state, []string{"read_file"})...)

	var textContent string
	var toolCalls int
	allEvents := append(evts1, evts2...)
	for _, evt := range allEvents {
		if evt.Content != "" {
			textContent += evt.Content
		}
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(textContent, "<invoke ") {
		t.Fatalf("XML content leaked: %q", textContent)
	}
	if toolCalls == 0 {
		t.Fatal("expected tool calls to be extracted")
	}
}

func TestProcessToolSieveFallsBackToRawAttemptCompletion(t *testing.T) {
	var state State
	// Simulate an agent outputting attempt_completion XML tag.
	// If it does not parse as a tool call, it should fall back to raw text.
	chunks := []string{
		"Done with task.\n",
		"<attempt_completion>\n",
		"  <result>Here is the answer</result>\n",
		"</attempt_completion>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"attempt_completion"})...)
	}
	events = append(events, Flush(&state, []string{"attempt_completion"})...)

	var textContent string
	for _, evt := range events {
		if evt.Content != "" {
			textContent += evt.Content
		}
	}

	if !strings.Contains(textContent, "Done with task.\n") {
		t.Fatalf("expected leading text to be emitted, got %q", textContent)
	}

	if textContent != strings.Join(chunks, "") {
		t.Fatalf("expected agent XML to fall back to raw text, got %q", textContent)
	}
}

func TestProcessToolSievePassesThroughBareToolCallAsText(t *testing.T) {
	var state State
	chunk := `<invoke name="read_file"><parameter name="path">README.md</parameter></invoke>`
	events := ProcessChunk(&state, chunk, []string{"read_file"})
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected bare invoke to remain text, got %d events=%#v", toolCalls, events)
	}
	if textContent.String() != chunk {
		t.Fatalf("expected bare invoke to pass through unchanged, got %q", textContent.String())
	}
}

func TestProcessToolSieveBareInvokeInlineProseDoesNotStall(t *testing.T) {
	var state State
	chunk := "Use `<invoke name=\"read_file\">` as plain documentation text."
	events := ProcessChunk(&state, chunk, []string{"read_file"})

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected inline invoke prose to remain text, got %d events=%#v", toolCalls, events)
	}
	if textContent.String() != chunk {
		t.Fatalf("expected inline invoke prose to stream immediately, got %q", textContent.String())
	}
	if state.capturing {
		t.Fatal("expected inline invoke prose not to leave stream capture open")
	}
}

func TestProcessToolSieveBareInvokeExampleReleasesWhenNotRepairable(t *testing.T) {
	var state State
	chunks := []string{
		`Example: <invoke name="read_file"><parameter name="path">README.md</parameter>`,
		"</invoke> then continue.",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 0 {
		t.Fatalf("expected non-repairable bare invoke to remain text, got %d events=%#v", toolCalls, events)
	}
	if textContent.String() != strings.Join(chunks, "") {
		t.Fatalf("expected non-repairable bare invoke to pass through, got %q", textContent.String())
	}
	if state.capturing {
		t.Fatal("expected non-repairable bare invoke not to leave stream capture open")
	}
}

func TestProcessToolSieveRepairsMissingOpeningWrapperWithoutLeakingInvokeText(t *testing.T) {
	var state State
	chunks := []string{
		"<invoke name=\"read_file\">\n",
		"  <parameter name=\"path\">README.md</parameter>\n",
		"</invoke>\n",
		"</tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}

	if toolCalls != 1 {
		t.Fatalf("expected repaired missing-wrapper stream to emit one tool call, got %d events=%#v", toolCalls, events)
	}
	if strings.Contains(textContent.String(), "<invoke") || strings.Contains(textContent.String(), "</tool_calls>") {
		t.Fatalf("expected repaired missing-wrapper stream not to leak xml text, got %q", textContent.String())
	}
}

// Test escaped U+FF5C pipe variant: <\uff5ctool_calls> should be buffered and parsed.
func TestProcessToolSieveFullwidthPipeVariantDoesNotLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<\uff5ctool_calls>\n",
		"<invoke name=\"execute_command\">\n",
		"<parameter name=\"command\">git status</parameter>\n",
		"</invoke>\n",
		"</\uff5ctool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"execute_command"})...)
	}
	events = append(events, Flush(&state, []string{"execute_command"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		textContent += evt.Content
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(textContent, "invoke") || strings.Contains(textContent, "execute_command") {
		t.Fatalf("escaped U+FF5C pipe variant leaked to text: %q", textContent)
	}
	if toolCalls != 1 {
		t.Fatalf("expected one tool call from escaped U+FF5C pipe variant, got %d events=%#v", toolCalls, events)
	}
}

// Test <|EPSE|tool_calls> with EPSE invoke/parameter tags should buffer the
// wrapper instead of leaking it before the block is complete.
func TestProcessToolSieveFullwidthEPSEPrefixVariantDoesNotLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<|EPSE|tool",
		"_calls>\n",
		"<|EPSE|invoke name=\"Bash\">\n",
		"<|EPSE|parameter name=\"command\"><![CDATA[ls -la /Users/aq/Desktop/myproject/ds2api/]]></|EPSE|parameter>\n",
		"<|EPSE|parameter name=\"description\"><![CDATA[List project root contents]]></|EPSE|parameter>\n",
		"</|EPSE|invoke>\n",
		"<|EPSE|invoke name=\"Bash\">\n",
		"<|EPSE|parameter name=\"command\"><![CDATA[cat /Users/aq/Desktop/myproject/ds2api/package.json 2>/dev/null || echo \"No package.json found\"]]></|EPSE|parameter>\n",
		"<|EPSE|parameter name=\"description\"><![CDATA[Check for existing package.json]]></|EPSE|parameter>\n",
		"</|EPSE|invoke>\n",
		"</|EPSE|tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"Bash"})...)
	}
	events = append(events, Flush(&state, []string{"Bash"})...)

	var textContent strings.Builder
	var toolCalls int
	var names []string
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		for _, call := range evt.ToolCalls {
			toolCalls++
			names = append(names, call.Name)
		}
	}

	if toolCalls != 2 {
		t.Fatalf("expected two tool calls from fullwidth EPSE prefix variant, got %d events=%#v", toolCalls, events)
	}
	if len(names) != 2 || names[0] != "Bash" || names[1] != "Bash" {
		t.Fatalf("expected two Bash tool calls, got %v", names)
	}
	if textContent.Len() != 0 {
		t.Fatalf("expected fullwidth EPSE prefix variant not to leak text, got %q", textContent.String())
	}
}

// Test <EPSE|tool_calls> with <|EPSE|invoke> (EPSE prefix without leading pipe on wrapper).
func TestProcessToolSieveEPSEPrefixVariantDoesNotLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<EPSE|tool_calls>\n",
		"  <|EPSE|invoke name=\"execute_command\">\n",
		"    <|EPSE|parameter name=\"command\"><![CDATA[git status]]></|EPSE|parameter>\n",
		"  </|EPSE|invoke>\n",
		"</EPSE|tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"execute_command"})...)
	}
	events = append(events, Flush(&state, []string{"execute_command"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		textContent += evt.Content
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(strings.ToLower(textContent), "epse") || strings.Contains(textContent, "execute_command") {
		t.Fatalf("EPSE prefix variant leaked to text: %q", textContent)
	}
	if toolCalls != 1 {
		t.Fatalf("expected one tool call from EPSE prefix variant, got %d events=%#v", toolCalls, events)
	}
}

// Test <EPSE|tool_calls> with <EPSE|invoke> (no pipe anywhere) should be buffered and parsed.
func TestProcessToolSieveEPSEBarePrefixVariantDoesNotLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<EPSE|tool_calls>\n",
		"<EPSE|invoke name=\"execute_command\">\n",
		"<EPSE|parameter name=\"command\"><![CDATA[git status]]></EPSE|parameter>\n",
		"</EPSE|invoke>\n",
		"</EPSE|tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"execute_command"})...)
	}
	events = append(events, Flush(&state, []string{"execute_command"})...)

	var textContent string
	var toolCalls int
	for _, evt := range events {
		textContent += evt.Content
		toolCalls += len(evt.ToolCalls)
	}

	if strings.Contains(strings.ToLower(textContent), "epse") || strings.Contains(textContent, "execute_command") {
		t.Fatalf("EPSE bare prefix variant leaked to text: %q", textContent)
	}
	if toolCalls != 1 {
		t.Fatalf("expected one tool call from EPSE bare prefix variant, got %d events=%#v", toolCalls, events)
	}
}

func TestProcessToolSieveCJKAngleEPSDriftDoesNotLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<EPS|tool_calls>\n",
		"<EPS|invoke name=\"Bash\">\n",
		"<EPS|parameter name=\"description\"|>〈![CDATA[Check tracking branch status]]〉〈/EPS|parameter〉\n",
		"<EPS|parameter name=\"command\"|>〈![CDATA[git status -b --short]]〉〈/EPS|parameter〉\n",
		"〈/EPS|invoke〉\n",
		"〈/EPS|tool_calls〉",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"Bash"})...)
	}
	events = append(events, Flush(&state, []string{"Bash"})...)

	var textContent string
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent += evt.Content
		calls = append(calls, evt.ToolCalls...)
	}

	if strings.Contains(textContent, "EPS") || strings.Contains(textContent, "git status") {
		t.Fatalf("CJK-angle EPS drift leaked to text: %q events=%#v", textContent, events)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one CJK-angle EPS drift tool call, got %d events=%#v", len(calls), events)
	}
	if calls[0].Name != "Bash" || calls[0].Input["command"] != "git status -b --short" {
		t.Fatalf("unexpected CJK-angle EPS drift call: %#v", calls[0])
	}
}

func TestProcessToolSieveFullwidthBangEPSEDriftDoesNotLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<！EPSE！tool_calls>\n",
		"  <！EPSE！invoke name=“Bash”>\n",
		"  <！EPSE！parameter name=“command”><！[CDATA[lsof -i :4321 -t]]><！/EPSE！parameter>\n",
		"  <！EPSE！parameter name=“description”><！[CDATA[Verify port 4321 is free]]><！/EPSE！parameter>\n",
		"  <！/EPSE！invoke>\n",
		"  <！/EPSE！tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"Bash"})...)
	}
	events = append(events, Flush(&state, []string{"Bash"})...)

	var textContent string
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent += evt.Content
		calls = append(calls, evt.ToolCalls...)
	}

	if strings.Contains(textContent, "EPSE") || strings.Contains(textContent, "lsof") {
		t.Fatalf("fullwidth-bang EPSE drift leaked to text: %q events=%#v", textContent, events)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one fullwidth-bang EPSE drift tool call, got %d events=%#v", len(calls), events)
	}
	if calls[0].Name != "Bash" || calls[0].Input["command"] != "lsof -i :4321 -t" {
		t.Fatalf("unexpected fullwidth-bang EPSE drift call: %#v", calls[0])
	}
}

func TestProcessToolSieveIdeographicCommaEPSEDriftDoesNotLeak(t *testing.T) {
	var state State
	chunks := []string{
		"<、EPSE、tool_calls>\n",
		"  <、EPSE、invoke name=\"Bash\">\n",
		"    <、EPSE、parameter name=\"command\"><、[CDATA[git commit -m \"$(cat <<'EOF'\n",
		"feat: expand fullwidth bang separator and curly quote tolerance in EPSE tool parsing\n\n",
		"Co-Authored-By: Claude Opus 4.6 noreply@anthropic.com\n",
		"EOF\n",
		")\"]]><、/EPSE、parameter>\n",
		"    <、EPSE、parameter name=\"description\"><、[CDATA[Create commit with staged changes]]><、/EPSE、parameter>\n",
		"  <、/EPSE、invoke>\n",
		"<、/EPSE、tool_calls>",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"Bash"})...)
	}
	events = append(events, Flush(&state, []string{"Bash"})...)

	var textContent string
	var calls []toolcall.ParsedToolCall
	for _, evt := range events {
		textContent += evt.Content
		calls = append(calls, evt.ToolCalls...)
	}

	if strings.Contains(textContent, "EPSE") || strings.Contains(textContent, "git commit") {
		t.Fatalf("ideographic-comma EPSE drift leaked to text: %q events=%#v", textContent, events)
	}
	if len(calls) != 1 {
		t.Fatalf("expected one ideographic-comma EPSE drift tool call, got %d events=%#v", len(calls), events)
	}
	command, _ := calls[0].Input["command"].(string)
	if calls[0].Name != "Bash" || !strings.Contains(command, "git commit -m") {
		t.Fatalf("unexpected ideographic-comma EPSE drift call: %#v", calls[0])
	}
}

func TestProcessToolSieveParsesFullwidthClosingSlashAndKeepsSuffixText(t *testing.T) {
	var state State
	chunk := `<|EPSE|tool_calls><|EPSE|invoke name="execute_code"><|EPSE|parameter name="code"><![CDATA[print("hi")]]></|EPSE|parameter></|EPSE|invoke><／EPSE|tool_calls> sao cụm này lại đc trả là 1 message`
	events := ProcessChunk(&state, chunk, []string{"execute_code"})
	events = append(events, Flush(&state, []string{"execute_code"})...)

	var textContent strings.Builder
	toolCalls := 0
	var parsed Event
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		if len(evt.ToolCalls) > 0 {
			parsed = evt
		}
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 1 {
		t.Fatalf("expected exactly one parsed tool call from fullwidth closing slash block, got %d events=%#v", toolCalls, events)
	}
	if parsed.ToolCalls[0].Name != "execute_code" || parsed.ToolCalls[0].Input["code"] != `print("hi")` {
		t.Fatalf("unexpected parsed call from fullwidth closing slash block: %#v", parsed.ToolCalls[0])
	}
	if got := textContent.String(); got != " sao cụm này lại đc trả là 1 message" {
		t.Fatalf("expected suffix text to be preserved, got %q", got)
	}
}

func TestProcessToolSieveParsesSentencePieceSeparatorAndFullwidthTerminator(t *testing.T) {
	var state State
	chunk := `<|EPSE▁tool_calls|><|EPSE▁invoke▁name="execute_code"><|EPSE▁parameter▁name="code"><![CDATA[print("hi")]]></|EPSE▁parameter></|EPSE▁invoke></|EPSE▁tool_calls＞ suffix`
	events := ProcessChunk(&state, chunk, []string{"execute_code"})
	events = append(events, Flush(&state, []string{"execute_code"})...)

	var textContent strings.Builder
	toolCalls := 0
	var parsed Event
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		if len(evt.ToolCalls) > 0 {
			parsed = evt
		}
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 1 {
		t.Fatalf("expected exactly one parsed tool call from sentencepiece/fullwidth-terminator block, got %d events=%#v", toolCalls, events)
	}
	if parsed.ToolCalls[0].Name != "execute_code" || parsed.ToolCalls[0].Input["code"] != `print("hi")` {
		t.Fatalf("unexpected parsed call from sentencepiece/fullwidth-terminator block: %#v", parsed.ToolCalls[0])
	}
	if got := textContent.String(); got != " suffix" {
		t.Fatalf("expected suffix text to be preserved, got %q", got)
	}
}

func TestProcessToolSieveParsesFullwidthOpeningDelimiterAndUnicodeAttributes(t *testing.T) {
	var state State
	chunk := `＜|EPSE　tool_calls＞＜|EPSE　invoke　name＝“execute_code”＞＜|EPSE　parameter　name＝“code”＞<![CDATA[print("hi")]]>＜／EPSE|parameter＞＜／EPSE|invoke＞＜／EPSE|tool_calls＞ suffix`
	events := ProcessChunk(&state, chunk, []string{"execute_code"})
	events = append(events, Flush(&state, []string{"execute_code"})...)

	var textContent strings.Builder
	toolCalls := 0
	var parsed Event
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		if len(evt.ToolCalls) > 0 {
			parsed = evt
		}
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 1 {
		t.Fatalf("expected exactly one parsed tool call from fullwidth-opening/Unicode-attr block, got %d events=%#v", toolCalls, events)
	}
	if parsed.ToolCalls[0].Name != "execute_code" || parsed.ToolCalls[0].Input["code"] != `print("hi")` {
		t.Fatalf("unexpected parsed call from fullwidth-opening/Unicode-attr block: %#v", parsed.ToolCalls[0])
	}
	if got := textContent.String(); got != " suffix" {
		t.Fatalf("expected suffix text to be preserved, got %q", got)
	}
}

func TestProcessToolSieveParsesConfusableCandidateShellAndKeepsSuffixText(t *testing.T) {
	var state State
	chunk := "<|\u200b\uff24\u0405\u039cL|to\u03bfl\uff3fcalls><|\ufeffEPSE|inv\u03bfk\u0435 n\u0430me\uff1d\u201cexecute_code\u201d><|\u200bEPSE|par\u0430meter n\u0430me\uff1d\u201ccode\u201d><![\ufeff\u0421D\u0410T\u0410[print(\"hi\")]]></|\u200bEPSE|par\u0430meter></|\u200bEPSE|inv\u03bfk\u0435></|\u200b\uff24\u0405\u039cL|to\u03bfl\uff3fcalls> suffix"
	events := ProcessChunk(&state, chunk, []string{"execute_code"})
	events = append(events, Flush(&state, []string{"execute_code"})...)

	var textContent strings.Builder
	toolCalls := 0
	var parsed Event
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		if len(evt.ToolCalls) > 0 {
			parsed = evt
		}
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 1 {
		t.Fatalf("expected exactly one parsed tool call from confusable-shell block, got %d events=%#v", toolCalls, events)
	}
	if parsed.ToolCalls[0].Name != "execute_code" || parsed.ToolCalls[0].Input["code"] != `print("hi")` {
		t.Fatalf("unexpected parsed call from confusable-shell block: %#v", parsed.ToolCalls[0])
	}
	if got := textContent.String(); got != " suffix" {
		t.Fatalf("expected suffix text to be preserved, got %q", got)
	}
}

func TestProcessToolSieveRepairsConfusableMissingWrapperAndKeepsSuffixText(t *testing.T) {
	var state State
	chunks := []string{
		"<inv\u03bfk\u0435 n\u0430me=\"read_file\">\n",
		"  <par\u0430meter n\u0430me=\"path\"><![\u200b\u0421D\u0410T\u0410[README.md]]></par\u0430meter>\n",
		"</inv\u03bfk\u0435>\n",
		"</to\u03bfl_calls> trailing prose",
	}
	var events []Event
	for _, c := range chunks {
		events = append(events, ProcessChunk(&state, c, []string{"read_file"})...)
	}
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	var parsed Event
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		if len(evt.ToolCalls) > 0 {
			parsed = evt
		}
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 1 {
		t.Fatalf("expected repaired confusable missing-wrapper stream to emit one tool call, got %d events=%#v", toolCalls, events)
	}
	if parsed.ToolCalls[0].Name != "read_file" || parsed.ToolCalls[0].Input["path"] != "README.md" {
		t.Fatalf("unexpected parsed call from repaired confusable missing-wrapper block: %#v", parsed.ToolCalls[0])
	}
	if got := textContent.String(); got != " trailing prose" {
		t.Fatalf("expected suffix prose to be preserved, got %q", got)
	}
}

func TestProcessToolSieveKeepsConfusableNearMissWrapperAsText(t *testing.T) {
	var state State
	chunk := "<to\u03bfl_callz><inv\u03bfke name=\"read_file\"><parameter name=\"path\">README.md</parameter></inv\u03bfke></to\u03bfl_callz>"
	events := ProcessChunk(&state, chunk, []string{"read_file"})
	events = append(events, Flush(&state, []string{"read_file"})...)

	var textContent strings.Builder
	toolCalls := 0
	for _, evt := range events {
		textContent.WriteString(evt.Content)
		toolCalls += len(evt.ToolCalls)
	}
	if toolCalls != 0 {
		t.Fatalf("expected confusable near-miss wrapper to remain text, got %d events=%#v", toolCalls, events)
	}
	if got := textContent.String(); got != chunk {
		t.Fatalf("expected confusable near-miss wrapper to pass through unchanged, got %q", got)
	}
}
