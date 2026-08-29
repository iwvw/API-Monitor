package files

import (
	"context"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/shared"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
)

type expertInlineMockStore struct {
	enabled      *bool
	maxFileBytes int
	allowedExts  []string
	modelAliases map[string]string
}

func (m expertInlineMockStore) ModelAliases() map[string]string     { return m.modelAliases }
func (m expertInlineMockStore) ToolcallMode() string                { return "feature_match" }
func (m expertInlineMockStore) ToolcallEarlyEmitConfidence() string { return "high" }
func (m expertInlineMockStore) ResponsesStoreTTLSeconds() int       { return 900 }
func (m expertInlineMockStore) EmbeddingsProvider() string          { return "" }
func (m expertInlineMockStore) AutoDeleteMode() string              { return "none" }
func (m expertInlineMockStore) AutoDeleteSessions() bool            { return false }
func (m expertInlineMockStore) CurrentInputFileEnabled() bool       { return false }
func (m expertInlineMockStore) CurrentInputFileMinChars() int       { return 0 }
func (m expertInlineMockStore) ThinkingInjectionEnabled() bool      { return false }
func (m expertInlineMockStore) ThinkingInjectionPrompt() string     { return "" }
func (m expertInlineMockStore) ExpertPromptSegmentEnabled() bool    { return true }
func (m expertInlineMockStore) ExpertPromptSegmentMaxChars() int    { return 160000 }
func (m expertInlineMockStore) ExpertTextFileInlineEnabled() bool {
	if m.enabled == nil {
		return true
	}
	return *m.enabled
}
func (m expertInlineMockStore) ExpertTextFileInlineMaxFileBytes() int {
	if m.maxFileBytes > 0 {
		return m.maxFileBytes
	}
	return 3 * 1024 * 1024
}
func (m expertInlineMockStore) ExpertTextFileInlineAllowedExtensions() map[string]struct{} {
	return ExtensionSet(m.allowedExts)
}
func (m expertInlineMockStore) AutoRouteVisionEnabled() bool { return false }

type mapContentStore struct {
	data map[string]*storedContent
}

type storedContent struct {
	filename string
	mimeType string
	data     []byte
}

func (m *mapContentStore) Store(id string, filename, mimeType string, data []byte) error {
	if m.data == nil {
		m.data = make(map[string]*storedContent)
	}
	m.data[id] = &storedContent{filename: filename, mimeType: mimeType, data: data}
	return nil
}

func (m *mapContentStore) Read(id string) (string, string, []byte, error) {
	if c, ok := m.data[id]; ok {
		return c.filename, c.mimeType, c.data, nil
	}
	return "", "", nil, ErrFileNotFound
}

func boolPtr(b bool) *bool { return &b }

func TestPreprocessInlineTextFilesForExpert_NonExpert(t *testing.T) {
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "notes.txt", mimeType: "text/plain", data: []byte("hello")},
	}}
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-flash",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "read this"},
					map[string]any{"type": "input_file", "file_id": "file-1"},
				},
			},
		},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected content unchanged, got %d parts", len(content))
	}
	if shared.AsString(content[1].(map[string]any)["file_id"]) != "file-1" {
		t.Errorf("expected file_id to remain")
	}
}

func TestPreprocessInlineTextFilesForExpert_TextFileID(t *testing.T) {
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "notes.txt", mimeType: "text/plain", data: []byte("hello world")},
	}}
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "read this"},
					map[string]any{"type": "input_file", "file_id": "file-1"},
				},
			},
		},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d", len(content))
	}
	second := content[1].(map[string]any)
	if second["type"] != "text" {
		t.Fatalf("expected inlined text block, got type %q", second["type"])
	}
	if second["text"] != "hello world" {
		t.Errorf("expected inlined text %q, got %q", "hello world", second["text"])
	}
}

func TestPreprocessInlineTextFilesForExpert_NonTextFileID(t *testing.T) {
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "image.png", mimeType: "image/png", data: []byte("binary")},
	}}
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_file", "file_id": "file-1"},
				},
			},
		},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
	block := content[0].(map[string]any)
	if block["type"] != "input_file" {
		t.Fatalf("expected non-text file left as input_file, got %v", block)
	}
}

func TestPreprocessInlineTextFilesForExpert_MissingFileID(t *testing.T) {
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: &mapContentStore{}}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_file", "file_id": "missing"},
				},
			},
		},
	}
	err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req)
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("expected unavailable error, got %v", err)
	}
}

func TestPreprocessInlineTextFilesForExpert_FileTooLarge(t *testing.T) {
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "big.txt", mimeType: "text/plain", data: []byte(strings.Repeat("x", 11))},
	}}
	h := &Handler{Store: expertInlineMockStore{maxFileBytes: 10}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_file", "file_id": "file-1"},
				},
			},
		},
	}
	err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req)
	if err == nil {
		t.Fatalf("expected error for too large file")
	}
	inlineErr, ok := err.(*inlineFileUploadError)
	if !ok || inlineErr.status != 413 {
		t.Errorf("expected 413 inlineFileUploadError, got %v", err)
	}
}

func TestPreprocessInlineTextFilesForExpert_Base64TextInline(t *testing.T) {
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: &mapContentStore{}}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "read this"},
					map[string]any{"type": "input_file", "filename": "notes.txt", "file_data": "data:text/plain;base64,aGVsbG8="},
				},
			},
		},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(content))
	}
	second := content[1].(map[string]any)
	if second["type"] != "text" || second["text"] != "hello" {
		t.Errorf("unexpected inlined block: %v", second)
	}
}

func TestPreprocessInlineTextFilesForExpert_Disabled(t *testing.T) {
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "notes.txt", mimeType: "text/plain", data: []byte("hello")},
	}}
	h := &Handler{Store: expertInlineMockStore{enabled: boolPtr(false)}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_file", "file_id": "file-1"},
				},
			},
		},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
	block := content[0].(map[string]any)
	if block["file_id"] != "file-1" {
		t.Errorf("expected file unchanged when disabled, got %v", block)
	}
}

func TestPreprocessInlineTextFilesForExpert_TopLevelFileIDs(t *testing.T) {
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "notes.txt", mimeType: "text/plain", data: []byte("top level content")},
		"file-2": {filename: "image.png", mimeType: "image/png", data: []byte("binary")},
	}}
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "summarize"},
		},
		"file_ids": []any{"file-1", "file-2"},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts (text + inlined file), got %d: %#v", len(content), content)
	}
	first := content[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "summarize" {
		t.Fatalf("expected original text preserved, got %#v", first)
	}
	second := content[1].(map[string]any)
	if second["type"] != "text" || second["text"] != "top level content" {
		t.Fatalf("expected inlined top-level file content, got %#v", second)
	}
}

func TestPreprocessInlineTextFilesForExpert_TopLevelRefFileIDsWithArrayContent(t *testing.T) {
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "notes.txt", mimeType: "text/plain", data: []byte("ref content")},
	}}
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": []any{map[string]any{"type": "text", "text": "read this"}},
			},
		},
		"ref_file_ids": []string{"file-1"},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected 2 content parts, got %d: %#v", len(content), content)
	}
	if got := content[1].(map[string]any)["text"]; got != "ref content" {
		t.Fatalf("expected inlined ref content, got %#v", got)
	}
}

func TestPreprocessInlineTextFilesForExpert_TopLevelFileIDsMissing(t *testing.T) {
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: &mapContentStore{}}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "summarize"},
		},
		"file_ids": []any{"missing"},
	}
	err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req)
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("expected unavailable error, got %v", err)
	}
}

func TestPreprocessInlineTextFilesForExpert_TopLevelFileIDsNotAppliedToToolMessages(t *testing.T) {
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "notes.txt", mimeType: "text/plain", data: []byte("content")},
	}}
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "system", "content": "instructions"},
			map[string]any{"role": "assistant", "content": "thinking"},
			map[string]any{"role": "user", "content": "last user"},
		},
		"file_ids": []any{"file-1"},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	messages := req["messages"].([]any)
	last := messages[2].(map[string]any)
	content := last["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("expected inlined content on last user message, got %d: %#v", len(content), content)
	}
}

func TestPreprocessInlineTextFilesForExpert_InvalidUTF8(t *testing.T) {
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "notes.txt", mimeType: "text/plain", data: []byte("hello \xff world")},
	}}
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "input_file", "file_id": "file-1"},
				},
			},
		},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	content := req["messages"].([]any)[0].(map[string]any)["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "\ufffd") {
		t.Errorf("expected replacement char for invalid UTF-8, got %q", text)
	}
}

func TestPreprocessInlineTextFilesForExpert_LongTextTriggersSegment(t *testing.T) {
	longText := strings.Repeat("x", 200000)
	store := &mapContentStore{data: map[string]*storedContent{
		"file-1": {filename: "long.txt", mimeType: "text/plain", data: []byte(longText)},
	}}
	h := &Handler{Store: expertInlineMockStore{}, ContentStore: store}
	req := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "read this"},
					map[string]any{"type": "input_file", "file_id": "file-1"},
				},
			},
		},
	}
	if err := h.PreprocessInlineTextFilesForExpert(context.Background(), &auth.RequestAuth{}, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stdReq, err := promptcompat.NormalizeOpenAIChatRequest(h.Store, req, "trace-1")
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if !strings.Contains(stdReq.FinalPrompt, longText) {
		t.Fatalf("FinalPrompt should contain inlined long text")
	}
	if len([]rune(stdReq.FinalPrompt)) <= 160000 {
		t.Fatalf("expected FinalPrompt to exceed default segment threshold, got %d runes", len([]rune(stdReq.FinalPrompt)))
	}
}
