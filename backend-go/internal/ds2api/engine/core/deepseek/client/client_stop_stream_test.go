package client

import "testing"

func TestLineHasContent(t *testing.T) {
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"empty", "", false},
		{"non-data line", "event: message", false},
		{"done sentinel", "data: [DONE]", false},
		{"invalid json", "data: {bad", false},
		{"response/content string", `data: {"p":"response/content","v":"hi"}`, true},
		{"response/content empty", `data: {"p":"response/content","v":""}`, false},
		{"response/thinking_content", `data: {"p":"response/thinking_content","v":"think"}`, true},
		{"response/fragments append", `data: {"p":"response/fragments","o":"APPEND","v":[{"type":"THINK","content":"思"}]}`, true},
		{"response/fragments append empty", `data: {"p":"response/fragments","o":"APPEND","v":[]}`, false},
		{"response/fragments/-1/content", `data: {"p":"response/fragments/-1/content","v":"案"}`, true},
		{"response/fragments/-1/content empty", `data: {"p":"response/fragments/-1/content","v":""}`, false},
		{"response/status incomplete", `data: {"p":"response/status","v":"INCOMPLETE"}`, false},
		{"response/status finished", `data: {"p":"response/status","v":"FINISHED"}`, false},
		{"top-level string content", `data: {"v":"你好"}`, true},
		{"error hint", `data: {"type":"error","content":"内容超长"}`, false},
		{"content_filter", `data: {"code":"content_filter"}`, false},
		{"response_message_id only no content", `data: {"response_message_id":42,"p":"response/status","v":"INCOMPLETE"}`, false},
		{"response_message_id with content", `data: {"response_message_id":42,"p":"response/content","v":"ok"}`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lineHasContent([]byte(c.line))
			if got != c.want {
				t.Fatalf("lineHasContent(%q) = %v, want %v", c.line, got, c.want)
			}
		})
	}
}
