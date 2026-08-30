package toolcall

import "testing"

func TestHasUnclosedToolCallMarkup(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"truncated EPSE bash", `<|EPSE|tool_calls> <|EPSE|invoke name="Bash"> <|EPSE|parameter name="command"></|EPSE|parameter> </|EPSE|invoke> </|EPSE>`, true},
		{"truncated canonical", "<tool_calls>\n<invoke name=\"read_file\">\n<parameter name=\"path\">README.md</parameter>\n", true},
		{"complete EPSE", "<|EPSE|tool_calls>\n<|EPSE|invoke name=\"Bash\">\n<|EPSE|parameter name=\"command\"><![CDATA[git status]]></|EPSE|parameter>\n</|EPSE|invoke>\n</|EPSE|tool_calls>", false},
		{"complete canonical", `<tool_calls><invoke name="Write"><parameter name="content">x</parameter></invoke></tool_calls>`, false},
		{"plain prose mention", "See <tool_calls> docs and its closing tag</tool_calls> above", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasUnclosedToolCallMarkup(tc.in); got != tc.want {
				t.Fatalf("HasUnclosedToolCallMarkup(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestIsPureTruncatedToolCallText(t *testing.T) {
	frag := `<|EPSE|tool_calls> <|EPSE|invoke name="Bash"> <|EPSE|parameter name="command"></|EPSE|parameter> </|EPSE|invoke> </|EPSE>`
	if !IsPureTruncatedToolCallText(frag) {
		t.Fatalf("expected pure truncated fragment to be detected")
	}
	withProse := "最后一块拼图是 relay.go 里的 resolveEndpointModel。\n" + frag
	if IsPureTruncatedToolCallText(withProse) {
		t.Fatalf("fragment with leading prose must not count as pure")
	}
	if IsPureTruncatedToolCallText("ordinary answer text") {
		t.Fatalf("ordinary answer must not be truncated")
	}
}

func TestStripTruncatedToolFragment(t *testing.T) {
	frag := `<|EPSE|tool_calls> <|EPSE|invoke name="Bash"> <|EPSE|parameter name="command"></|EPSE|parameter> </|EPSE|invoke> </|EPSE>`
	if got := StripTruncatedToolFragment(frag); got != "" {
		t.Fatalf("pure fragment should strip to empty, got %q", got)
	}
	withProse := "我先把上下文读清楚。\n" + frag
	if got := StripTruncatedToolFragment(withProse); got != "我先把上下文读清楚。\n" {
		t.Fatalf("expected prose to survive, got %q", got)
	}
	if got := StripTruncatedToolFragment("keep me"); got != "keep me" {
		t.Fatalf("plain text must pass through untouched, got %q", got)
	}
}