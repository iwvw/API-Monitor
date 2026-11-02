package promptcompat

import "testing"

func TestRequestBodyHasTools(t *testing.T) {
	cases := []struct {
		name string
		req  map[string]any
		want bool
	}{
		{name: "nil request", req: nil, want: false},
		{name: "no tools key", req: map[string]any{"model": "x"}, want: false},
		{name: "tools nil", req: map[string]any{"tools": nil}, want: false},
		{name: "tools empty slice", req: map[string]any{"tools": []any{}}, want: false},
		{name: "tools wrong type", req: map[string]any{"tools": "search"}, want: false},
		{
			name: "tools present",
			req: map[string]any{"tools": []any{
				map[string]any{"type": "function", "function": map[string]any{"name": "search"}},
			}},
			want: true,
		},
		{
			name: "system message mentions tools keyword",
			req: map[string]any{"messages": []any{
				map[string]any{"role": "system", "content": "Available tools: workspace_read_file, workspace_write_file"},
				map[string]any{"role": "user", "content": "read the file"},
			}},
			want: true,
		},
		{
			name: "system message mentions tool keyword (singular)",
			req: map[string]any{"messages": []any{
				map[string]any{"role": "system", "content": "You may invoke a Tool when needed."},
			}},
			want: true,
		},
		{
			name: "system message mentions functions keyword",
			req: map[string]any{"messages": []any{
				map[string]any{"role": "system", "content": "The following FUNCTIONS are available."},
			}},
			want: true,
		},
		{
			name: "system message content parts mention tools",
			req: map[string]any{"messages": []any{
				map[string]any{"role": "system", "content": []any{
					map[string]any{"type": "text", "text": "Use the available tools to help."},
				}},
			}},
			want: true,
		},
		{
			name: "system message without tool keyword",
			req: map[string]any{"messages": []any{
				map[string]any{"role": "system", "content": "You are a helpful assistant."},
				map[string]any{"role": "user", "content": "hello"},
			}},
			want: false,
		},
		{
			name: "tool keyword only in user message is ignored",
			req: map[string]any{"messages": []any{
				map[string]any{"role": "user", "content": "what tools do you have?"},
			}},
			want: false,
		},
		{
			name: "empty tools array falls back to system scan hit",
			req: map[string]any{
				"tools": []any{},
				"messages": []any{
					map[string]any{"role": "system", "content": "Available tools: read_file"},
				},
			},
			want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RequestBodyHasTools(c.req); got != c.want {
				t.Fatalf("RequestBodyHasTools(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}
