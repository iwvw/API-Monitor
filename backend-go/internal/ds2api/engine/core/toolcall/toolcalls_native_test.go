package toolcall

import "testing"

func TestHasNativeToolFrame(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"partial escaped", `<｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view","path":"/workspace/example.md"}`, true},
		{"complete escaped", `<｜｜tool▁calls▁begin｜｜><｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view"}<｜｜tool▁call▁end｜｜><｜｜tool▁calls▁end｜｜>`, true},
		{"third delimiter variant", `<｜｜tool▁calls▁begin｜｜><｜｜tool▁call▁begin｜｜>skill<｜｜tool▁sep｜｜>{"name":"tavern-ui"}<｜｜tool▁call▁end｜｜><｜｜tool▁calls▁end｜｜>`, true},
		{"raw unescaped", `<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>bash<｜tool▁sep｜>{"command":"ls"}<｜tool▁call▁end｜><｜tool▁calls▁end｜>`, true},
		{"sep only", `好的，我先看下目录。<｜｜tool▁sep｜＞{"command":"ls"}`, true},
		{"plain prose mention tool_calls field", `注意：响应里应当包含 tool_calls 字段，以及每个工具的 name 与参数。`, false},
		{"plain prose only", `好的，我先看一下文件内容，然后用 str_replace_editor 改一下。`, false},
		{"empty", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasNativeToolFrame(tc.in); got != tc.want {
				t.Fatalf("HasNativeToolFrame(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestStripNativeToolCallFrames(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"complete wrapper stripped whole", `<｜｜tool▁calls▁begin｜｜><｜｜tool▁call▁begin｜＞bash<｜｜tool▁sep｜＞{"command":"ls"}<｜｜tool▁call▁end｜｜><｜｜tool▁calls▁end｜｜>`, ``},
		{"embedded keeps prose", `好的，我先确认一下目录结构。<｜｜tool▁call▁begin｜＞bash<｜｜tool▁sep｜＞{"command":"ls"} 然后再决定下一步。`, `好的，我先确认一下目录结构。 然后再决定下一步。`},
		{"sep only in prose", `我在看文件。<｜｜tool▁sep｜＞{"command":"ls"}`, `我在看文件。{"command":"ls"}`},
		{"raw unescaped", `<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>bash<｜tool▁sep｜>{"command":"ls"}<｜tool▁call▁end｜><｜tool▁calls▁end｜>`, ``},
		{"plain text untouched", `这是正常回答。`, `这是正常回答。`},
		{"empty", ``, ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripNativeToolCallFrames(tc.in); got != tc.want {
				t.Fatalf("StripNativeToolCallFrames(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsPureNativeTruncatedToolFrame(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"pure partial frame no end", `<｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view"}`, true},
		{"complete frame with end not truncated", `<｜｜tool▁calls▁begin｜｜><｜｜tool▁call▁begin｜＞bash<｜｜tool▁sep｜＞{"command":"ls"}<｜｜tool▁call▁end｜｜><｜｜tool▁calls▁end｜｜>`, false},
		{"leading prose not pure", `我先看一下文件。<｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view"}`, false},
		{"ordinary text", `这是正常回答。`, false},
		{"empty", ``, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPureNativeTruncatedToolFrame(tc.in); got != tc.want {
				t.Fatalf("IsPureNativeTruncatedToolFrame(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestRewriteNativeToolCallFrames(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			"complete single invoke",
			`<｜｜tool▁calls▁begin｜｜><｜｜tool▁call▁begin｜＞bash<｜｜tool▁sep｜＞{"command":"ls"}<｜｜tool▁call▁end｜｜><｜｜tool▁calls▁end｜｜>`,
			`<tool_calls><invoke name="bash">{"input":{"command":"ls"}}</invoke></tool_calls>`,
		},
		{
			"complete multiple invokes",
			`<｜｜tool▁calls▁begin｜｜><｜｜tool▁call▁begin｜＞bash<｜｜tool▁sep｜＞{"command":"ls"}<｜｜tool▁call▁end｜｜><｜｜tool▁call▁begin｜＞bash<｜｜tool▁sep｜＞{"command":"pwd"}<｜｜tool▁call▁end｜｜><｜｜tool▁calls▁end｜｜>`,
			`<tool_calls><invoke name="bash">{"input":{"command":"ls"}}</invoke><invoke name="bash">{"input":{"command":"pwd"}}</invoke></tool_calls>`,
		},
		{
			"raw unescaped",
			`<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>str_replace_editor<｜tool▁sep｜>{"command":"view","path":"/workspace/a.md"}<｜tool▁call▁end｜><｜tool▁calls▁end｜>`,
			`<tool_calls><invoke name="str_replace_editor">{"input":{"command":"view","path":"/workspace/a.md"}}</invoke></tool_calls>`,
		},
		{
			"prose around frame preserved",
			`前置<｜｜tool▁calls▁begin｜｜><｜｜tool▁call▁begin｜＞bash<｜｜tool▁sep｜＞{"command":"ls"}<｜｜tool▁call▁end｜｜><｜｜tool▁calls▁end｜｜>后置`,
			`前置<tool_calls><invoke name="bash">{"input":{"command":"ls"}}</invoke></tool_calls>后置`,
		},
		{
			"partial no end unchanged",
			`<｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view"}`,
			`<｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view"}`,
		},
		{
			"plain prose unchanged",
			`这是正常回答。`,
			`这是正常回答。`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RewriteNativeToolCallFrames(tc.in); got != tc.want {
				t.Fatalf("RewriteNativeToolCallFrames(%q)\n  got  %q\n  want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseStandaloneRecoversNativeFrame(t *testing.T) {
	raw := `<｜｜tool▁calls▁begin｜｜><｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view","path":"/workspace/example.md"}<｜｜tool▁call▁end｜｜><｜｜tool▁calls▁end｜｜>`
	calls := ParseStandaloneToolCalls(raw, nil)
	if len(calls) != 1 {
		t.Fatalf("expected 1 recovered tool call, got %d (raw=%q)", len(calls), raw)
	}
	if calls[0].Name != "str_replace_editor" {
		t.Fatalf("expected name str_replace_editor, got %q", calls[0].Name)
	}
	if calls[0].Input["command"] != "view" || calls[0].Input["path"] != "/workspace/example.md" {
		t.Fatalf("unexpected recovered input: %#v", calls[0].Input)
	}
}

func TestParseStandaloneLeavesPartialNativeFrameUnparsed(t *testing.T) {
	raw := `<｜｜tool▁call▁begin｜＞str_replace_editor<｜｜tool▁sep｜＞{"command":"view","path":"/workspace/example.md"}`
	calls := ParseStandaloneToolCalls(raw, nil)
	if len(calls) != 0 {
		t.Fatalf("expected partial native frame to stay unparsed (retry path), got %d calls", len(calls))
	}
}
