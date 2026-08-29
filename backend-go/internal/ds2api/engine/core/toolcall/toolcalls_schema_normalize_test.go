package toolcall

import (
	"reflect"
	"testing"
)

func TestNormalizeParsedToolCallsForSchemasCoercesDeclaredStringFieldsRecursively(t *testing.T) {
	toolsRaw := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "TaskUpdate",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"taskId": map[string]any{"type": "string"},
						"payload": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"content": map[string]any{"type": "string"},
								"tags": map[string]any{
									"type":  "array",
									"items": map[string]any{"type": "string"},
								},
								"count": map[string]any{"type": "number"},
							},
						},
					},
				},
			},
		},
	}
	calls := []ParsedToolCall{{
		Name: "TaskUpdate",
		Input: map[string]any{
			"taskId": 1,
			"payload": map[string]any{
				"content": map[string]any{"text": "hello"},
				"tags":    []any{1, true, map[string]any{"k": "v"}},
				"count":   2,
			},
		},
	}}

	got := NormalizeParsedToolCallsForSchemas(calls, toolsRaw)
	if len(got) != 1 {
		t.Fatalf("expected one normalized call, got %#v", got)
	}
	if got[0].Input["taskId"] != "1" {
		t.Fatalf("expected taskId coerced to string, got %#v", got[0].Input["taskId"])
	}
	payload, ok := got[0].Input["payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected payload object, got %#v", got[0].Input["payload"])
	}
	if payload["content"] != `{"text":"hello"}` {
		t.Fatalf("expected nested content coerced to json string, got %#v", payload["content"])
	}
	if payload["count"] != 2 {
		t.Fatalf("expected non-string count unchanged, got %#v", payload["count"])
	}
	tags, ok := payload["tags"].([]any)
	if !ok {
		t.Fatalf("expected tags slice, got %#v", payload["tags"])
	}
	wantTags := []any{"1", "true", `{"k":"v"}`}
	if !reflect.DeepEqual(tags, wantTags) {
		t.Fatalf("unexpected normalized tags: got %#v want %#v", tags, wantTags)
	}
}

func TestNormalizeParsedToolCallsForSchemasSupportsDirectToolSchemaShape(t *testing.T) {
	toolsRaw := []any{
		map[string]any{
			"name": "Write",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
				},
			},
		},
	}
	calls := []ParsedToolCall{{Name: "Write", Input: map[string]any{"content": []any{"a", 1}}}}
	got := NormalizeParsedToolCallsForSchemas(calls, toolsRaw)
	if got[0].Input["content"] != `["a",1]` {
		t.Fatalf("expected direct-schema content coerced to string, got %#v", got[0].Input["content"])
	}
}

func TestNormalizeParsedToolCallsForSchemasLeavesAmbiguousUnionUnchanged(t *testing.T) {
	toolsRaw := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name": "TaskUpdate",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"taskId": map[string]any{"type": []any{"string", "integer"}},
					},
				},
			},
		},
	}
	calls := []ParsedToolCall{{Name: "TaskUpdate", Input: map[string]any{"taskId": 1}}}
	got := NormalizeParsedToolCallsForSchemas(calls, toolsRaw)
	if got[0].Input["taskId"] != 1 {
		t.Fatalf("expected ambiguous union to stay unchanged, got %#v", got[0].Input["taskId"])
	}
}

func TestNormalizeParsedToolCallsForSchemasSupportsCamelCaseInputSchema(t *testing.T) {
	toolsRaw := []any{
		map[string]any{
			"name": "Write",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{"type": "string"},
				},
			},
		},
	}
	calls := []ParsedToolCall{{Name: "Write", Input: map[string]any{"content": map[string]any{"message": "hi"}}}}
	got := NormalizeParsedToolCallsForSchemas(calls, toolsRaw)
	if got[0].Input["content"] != `{"message":"hi"}` {
		t.Fatalf("expected camelCase inputSchema content coercion, got %#v", got[0].Input["content"])
	}
}

func TestNormalizeParsedToolCallsForSchemasPreservesArrayWhenSchemaSaysArray(t *testing.T) {
	toolsRaw := []any{
		map[string]any{
			"name": "todowrite",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"todos": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"content":  map[string]any{"type": "string"},
								"status":   map[string]any{"type": "string"},
								"priority": map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
	}
	todos := []any{map[string]any{"content": "x", "status": "pending", "priority": "high"}}
	calls := []ParsedToolCall{{Name: "todowrite", Input: map[string]any{"todos": todos}}}
	got := NormalizeParsedToolCallsForSchemas(calls, toolsRaw)
	if !reflect.DeepEqual(got[0].Input["todos"], todos) {
		t.Fatalf("expected todos array preserved, got %#v want %#v", got[0].Input["todos"], todos)
	}
}
