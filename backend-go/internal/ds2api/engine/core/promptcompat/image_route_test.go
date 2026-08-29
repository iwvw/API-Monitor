package promptcompat

import (
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
)

type fakeRouteConfigReader struct {
	aliases         map[string]string
	autoRouteVision bool
}

func (f *fakeRouteConfigReader) ModelAliases() map[string]string {
	return f.aliases
}

func (f *fakeRouteConfigReader) AutoRouteVisionEnabled() bool {
	return f.autoRouteVision
}

func TestVisionModelEquivalent(t *testing.T) {
	cases := []struct {
		model    string
		expected string
	}{
		{"deepseek-v4-flash", "deepseek-v4-vision"},
		{"deepseek-v4-pro", "deepseek-v4-vision"},
		{"deepseek-v4-flash-search", "deepseek-v4-vision"},
		{"deepseek-v4-flash-nothinking", "deepseek-v4-vision-nothinking"},
		{"deepseek-v4-pro-nothinking", "deepseek-v4-vision-nothinking"},
		{"deepseek-v4-vision", ""},
		{"deepseek-v4-vision-nothinking", ""},
		{"gpt-4o", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := VisionModelEquivalent(tc.model)
		if got != tc.expected {
			t.Errorf("VisionModelEquivalent(%q)=%q want %q", tc.model, got, tc.expected)
		}
	}
}

func TestRequestContainsImageContent(t *testing.T) {
	imageURLReq := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "hello"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				},
			},
		},
	}
	if !RequestContainsImageContent(imageURLReq) {
		t.Error("expected image_url block to be detected")
	}

	inputImageReq := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_image", "file_id": "file-123"},
				},
			},
		},
	}
	if !RequestContainsImageContent(inputImageReq) {
		t.Error("expected input_image block to be detected")
	}

	textOnlyReq := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	if RequestContainsImageContent(textOnlyReq) {
		t.Error("expected text-only request to have no images")
	}

	documentReq := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_file", "file_id": "file-456"},
				},
			},
		},
	}
	if RequestContainsImageContent(documentReq) {
		t.Error("expected input_file block not to be treated as image")
	}

	historicalImageReq := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				},
			},
			map[string]any{
				"role":    "assistant",
				"content": "I see the image",
			},
			map[string]any{
				"role":    "user",
				"content": "now just text",
			},
		},
	}
	if RequestContainsImageContent(historicalImageReq) {
		t.Error("expected historical image in earlier user message not to trigger routing")
	}
}

func TestStripImageBlocksFromMessages(t *testing.T) {
	messages := []any{
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "hello"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				map[string]any{"type": "input_image", "file_id": "file-123"},
			},
		},
		map[string]any{
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_file", "file_id": "file-456"},
			},
		},
	}
	stripped := StripImageBlocksFromMessages(messages)
	if len(stripped) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(stripped))
	}
	first, _ := stripped[0].(map[string]any)
	firstContent, _ := first["content"].([]any)
	if len(firstContent) != 1 {
		t.Fatalf("expected 1 content block in first message, got %d", len(firstContent))
	}
	if text := asString(firstContent[0].(map[string]any)["text"]); text != "hello" {
		t.Fatalf("expected remaining text block 'hello', got %q", text)
	}
	second, _ := stripped[1].(map[string]any)
	secondContent, _ := second["content"].([]any)
	if len(secondContent) != 1 {
		t.Fatalf("expected input_file block to be preserved, got %d blocks", len(secondContent))
	}
	if fileID := asString(secondContent[0].(map[string]any)["file_id"]); fileID != "file-456" {
		t.Fatalf("expected preserved file_id 'file-456', got %q", fileID)
	}
}

func TestStripImageBlocksFromMessagesBareContentParts(t *testing.T) {
	messages := []any{
		map[string]any{"type": "input_text", "text": "hello"},
		map[string]any{"type": "input_image", "file_id": "file-123"},
		map[string]any{"type": "input_file", "file_id": "file-456"},
	}
	stripped := StripImageBlocksFromMessages(messages)
	if len(stripped) != 2 {
		t.Fatalf("expected 2 items, got %d", len(stripped))
	}
	first, _ := stripped[0].(map[string]any)
	if asString(first["type"]) != "input_text" {
		t.Fatalf("expected input_text to be preserved, got %v", first)
	}
	second, _ := stripped[1].(map[string]any)
	if asString(second["type"]) != "input_file" {
		t.Fatalf("expected input_file to be preserved, got %v", second)
	}
}

func TestMaybeAutoRouteVision(t *testing.T) {
	store := &fakeRouteConfigReader{
		aliases:         config.DefaultModelAliases(),
		autoRouteVision: true,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				},
			},
		},
	}
	originalModel, rerouted := MaybeAutoRouteVision(req, store)
	if !rerouted {
		t.Fatal("expected reroute")
	}
	if originalModel != "deepseek-v4-flash" {
		t.Fatalf("expected original model deepseek-v4-flash, got %q", originalModel)
	}
	if req["model"] != "deepseek-v4-vision" {
		t.Fatalf("expected model rewritten to deepseek-v4-vision, got %q", req["model"])
	}
}

func TestMaybeAutoRouteVisionPreservesNoThinking(t *testing.T) {
	store := &fakeRouteConfigReader{
		aliases:         config.DefaultModelAliases(),
		autoRouteVision: true,
	}
	req := map[string]any{
		"model": "deepseek-v4-pro-nothinking",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				},
			},
		},
	}
	_, rerouted := MaybeAutoRouteVision(req, store)
	if !rerouted {
		t.Fatal("expected reroute")
	}
	if req["model"] != "deepseek-v4-vision-nothinking" {
		t.Fatalf("expected model rewritten to deepseek-v4-vision-nothinking, got %q", req["model"])
	}
}

func TestMaybeAutoRouteVisionDisabled(t *testing.T) {
	store := &fakeRouteConfigReader{
		aliases:         config.DefaultModelAliases(),
		autoRouteVision: false,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				},
			},
		},
	}
	_, rerouted := MaybeAutoRouteVision(req, store)
	if rerouted {
		t.Fatal("expected no reroute when disabled")
	}
	if req["model"] != "deepseek-v4-flash" {
		t.Fatalf("expected model unchanged, got %q", req["model"])
	}
}

func TestMaybeAutoRouteVisionNoImage(t *testing.T) {
	store := &fakeRouteConfigReader{
		aliases:         config.DefaultModelAliases(),
		autoRouteVision: true,
	}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{"role": "user", "content": "hello"},
		},
	}
	_, rerouted := MaybeAutoRouteVision(req, store)
	if rerouted {
		t.Fatal("expected no reroute without images")
	}
}

func TestMaybeAutoRouteVisionAlreadyVision(t *testing.T) {
	store := &fakeRouteConfigReader{
		aliases:         config.DefaultModelAliases(),
		autoRouteVision: true,
	}
	req := map[string]any{
		"model": "deepseek-v4-vision",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				},
			},
		},
	}
	_, rerouted := MaybeAutoRouteVision(req, store)
	if rerouted {
		t.Fatal("expected no reroute when already vision")
	}
}

func TestMaybeAutoRouteVisionAlias(t *testing.T) {
	store := &fakeRouteConfigReader{
		aliases:         config.DefaultModelAliases(),
		autoRouteVision: true,
	}
	req := map[string]any{
		"model": "gpt-4o",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				},
			},
		},
	}
	originalModel, rerouted := MaybeAutoRouteVision(req, store)
	if !rerouted {
		t.Fatal("expected reroute for alias")
	}
	if originalModel != "gpt-4o" {
		t.Fatalf("expected original model gpt-4o, got %q", originalModel)
	}
	if req["model"] != "deepseek-v4-vision" {
		t.Fatalf("expected model rewritten to deepseek-v4-vision, got %q", req["model"])
	}
}

func TestStripImageBlocksFromRequest(t *testing.T) {
	req := map[string]any{
		"model": "deepseek-v4-vision",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "hello"},
					map[string]any{"type": "input_image", "file_id": "file-123"},
				},
			},
		},
		"input": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
				},
			},
		},
	}
	StripImageBlocksFromRequest(req)
	msgs, _ := req["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	input, _ := req["input"].([]any)
	if len(input) != 0 {
		t.Fatalf("expected input stripped to 0 messages, got %d", len(input))
	}
}
