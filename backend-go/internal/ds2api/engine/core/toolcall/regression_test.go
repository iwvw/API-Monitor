package toolcall

import (
	"reflect"
	"testing"
)

func TestRegression_RobustXMLAndCDATA(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []ParsedToolCall
	}{
		{
			name:     "Standard JSON scalar parameters (Regression)",
			text:     `<tool_calls><invoke name="foo"><parameter name="a">1</parameter></invoke></tool_calls>`,
			expected: []ParsedToolCall{{Name: "foo", Input: map[string]any{"a": float64(1)}}},
		},
		{
			name:     "XML tags parameters (Regression)",
			text:     `<tool_calls><invoke name="foo"><parameter name="arg1">hello</parameter></invoke></tool_calls>`,
			expected: []ParsedToolCall{{Name: "foo", Input: map[string]any{"arg1": "hello"}}},
		},
		{
			name: "CDATA parameters (New Feature)",
			text: `<tool_calls><invoke name="write_file"><parameter name="content"><![CDATA[line 1
line 2 with <tags> and & symbols]]></parameter></invoke></tool_calls>`,
			expected: []ParsedToolCall{{
				Name:  "write_file",
				Input: map[string]any{"content": "line 1\nline 2 with <tags> and & symbols"},
			}},
		},
		{
			name: "Nested XML with repeated parameters (New Feature)",
			text: `<tool_calls><invoke name="write_file"><parameter name="path">script.sh</parameter><parameter name="content"><![CDATA[#!/bin/bash
echo "hello"
]]></parameter><parameter name="item">first</parameter><parameter name="item">second</parameter></invoke></tool_calls>`,
			expected: []ParsedToolCall{{
				Name: "write_file",
				Input: map[string]any{
					"path":    "script.sh",
					"content": "#!/bin/bash\necho \"hello\"\n",
					"item":    []any{"first", "second"},
				},
			}},
		},
		{
			name: "Dirty XML with unescaped symbols (Robustness Improvement)",
			text: `<tool_calls><invoke name="bash"><parameter name="command">echo "hello" > out.txt && cat out.txt</parameter></invoke></tool_calls>`,
			expected: []ParsedToolCall{{
				Name:  "bash",
				Input: map[string]any{"command": "echo \"hello\" > out.txt && cat out.txt"},
			}},
		},
		{
			name: "Mixed JSON inside CDATA (New Hybrid Case)",
			text: `<tool_calls><invoke name="foo"><parameter name="json_param"><![CDATA[works]]></parameter></invoke></tool_calls>`,
			expected: []ParsedToolCall{{
				Name:  "foo",
				Input: map[string]any{"json_param": "works"},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseToolCalls(tt.text, []string{"foo", "write_file", "bash"})
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %d calls, got %d", len(tt.expected), len(got))
			}
			for i := range got {
				if got[i].Name != tt.expected[i].Name {
					t.Errorf("expected name %q, got %q", tt.expected[i].Name, got[i].Name)
				}
				if !reflect.DeepEqual(got[i].Input, tt.expected[i].Input) {
					t.Errorf("expected input %#v, got %#v", tt.expected[i].Input, got[i].Input)
				}
			}
		})
	}
}
