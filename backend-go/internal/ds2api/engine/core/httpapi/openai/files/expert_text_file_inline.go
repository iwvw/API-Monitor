package files

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/httpapi/openai/shared"
)

// ExpertTextFileInlineConfigReader exposes the subset of config needed to
// decide which text files should be inlined for expert models.
type ExpertTextFileInlineConfigReader interface {
	ExpertTextFileInlineEnabled() bool
	ExpertTextFileInlineMaxFileBytes() int
	ExpertTextFileInlineAllowedExtensions() map[string]struct{}
}

// PreprocessInlineTextFilesForExpert walks the incoming request and replaces
// text file references (input_file blocks with file_id, or base64 inline files)
// with plain text blocks so that the content becomes part of the prompt.
// It is only active for expert models because non-expert models can forward
// file references upstream normally.
func (h *Handler) PreprocessInlineTextFilesForExpert(ctx context.Context, a *auth.RequestAuth, req map[string]any) error {
	if h == nil || len(req) == 0 {
		return nil
	}
	modelType := "default"
	if requestedModel, ok := req["model"].(string); ok {
		if resolvedModel, ok := config.ResolveModel(h.Store, requestedModel); ok {
			if resolvedType, ok := config.GetModelType(resolvedModel); ok {
				modelType = resolvedType
			}
		}
	}
	if modelType != "expert" {
		return nil
	}
	if h.Store == nil || !h.Store.ExpertTextFileInlineEnabled() {
		return nil
	}
	state := &expertTextInlineState{
		ctx:          ctx,
		handler:      h,
		auth:         a,
		store:        h.ContentStore,
		maxFileBytes: h.Store.ExpertTextFileInlineMaxFileBytes(),
		allowedExts:  h.Store.ExpertTextFileInlineAllowedExtensions(),
	}
	for _, key := range []string{"messages", "input", "attachments"} {
		if raw, ok := req[key]; ok {
			updated, err := state.walk(raw)
			if err != nil {
				return err
			}
			req[key] = updated
		}
	}
	// Top-level ref_file_ids/file_ids carry request-wide file references
	// (OpenAI-compatible clients, including the admin tester, send attachments
	// this way). The expert completion payload drops ref_file_ids, so any text
	// files referenced here must be inlined into the last user message as well.
	// Non-text references are left untouched: they are dropped by payload
	// assembly anyway.
	if err := state.inlineTopLevelRefs(req); err != nil {
		return err
	}
	return nil
}

// inlineTopLevelRefs appends the content of text files referenced by top-level
// ref_file_ids/file_ids to the last user message, mirroring the inlining done
// for input_file blocks. Files not cached locally produce an error so the
// caller learns that the content cannot be forwarded to the expert model.
func (s *expertTextInlineState) inlineTopLevelRefs(req map[string]any) error {
	if s.store == nil {
		return nil
	}
	ids := collectTopLevelFileIDs(req)
	if len(ids) == 0 {
		return nil
	}
	userIdx := lastUserMessageIndex(req)
	if userIdx < 0 {
		return nil
	}
	messages, _ := req["messages"].([]any)
	msg, _ := messages[userIdx].(map[string]any)
	if msg == nil {
		return nil
	}
	blocks := normalizeContentBlocks(msg["content"])
	appended := 0
	for _, fileID := range ids {
		filename, mimeType, data, err := s.store.Read(fileID)
		if err != nil {
			if err == ErrFileTooLarge {
				return &inlineFileUploadError{
					status:  http.StatusRequestEntityTooLarge,
					message: fmt.Sprintf("text file %q exceeds max inline size (%d bytes)", fileID, s.maxFileBytes),
					err:     err,
				}
			}
			return &inlineFileUploadError{
				status:  http.StatusBadRequest,
				message: fmt.Sprintf("text file content unavailable for %q", fileID),
				err:     err,
			}
		}
		if !IsTextFile(filename, mimeType, s.allowedExts) {
			continue
		}
		if err := s.checkSize(len(data), filename); err != nil {
			return err
		}
		blocks = append(blocks, textBlock(data))
		appended++
	}
	if appended == 0 {
		return nil
	}
	msg["content"] = blocks
	return nil
}

func collectTopLevelFileIDs(req map[string]any) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, key := range []string{"ref_file_ids", "file_ids"} {
		raw, ok := req[key]
		if !ok {
			continue
		}
		var items []any
		switch x := raw.(type) {
		case []any:
			items = x
		case []string:
			items = make([]any, len(x))
			for i, item := range x {
				items[i] = item
			}
		default:
			continue
		}
		for _, item := range items {
			fileID := strings.TrimSpace(shared.AsString(item))
			if fileID == "" {
				continue
			}
			key := strings.ToLower(fileID)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, fileID)
		}
	}
	return out
}

func lastUserMessageIndex(req map[string]any) int {
	messages, ok := req["messages"].([]any)
	if !ok {
		return -1
	}
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(shared.AsString(msg["role"])), "user") {
			return i
		}
	}
	return -1
}

func normalizeContentBlocks(content any) []any {
	if raw, ok := content.([]any); ok {
		out := make([]any, 0, len(raw)+4)
		out = append(out, raw...)
		return out
	}
	text := strings.TrimSpace(shared.AsString(content))
	if text == "" {
		return nil
	}
	return []any{map[string]any{"type": "text", "text": text}}
}

type expertTextInlineState struct {
	ctx          context.Context
	handler      *Handler
	auth         *auth.RequestAuth
	store        ContentStore
	maxFileBytes int
	allowedExts  map[string]struct{}
	inlineCount  int
	inlineBytes  int
}

func (s *expertTextInlineState) walk(raw any) (any, error) {
	switch x := raw.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			updated, err := s.walk(item)
			if err != nil {
				return nil, err
			}
			out[i] = updated
		}
		return out, nil
	case map[string]any:
		if replacement, replaced, err := s.tryInlineTextBlock(x); replaced || err != nil {
			return replacement, err
		}
		for _, key := range []string{"messages", "input", "attachments", "content", "files", "items", "data", "source", "file", "image_url"} {
			if nested, ok := x[key]; ok {
				updated, err := s.walk(nested)
				if err != nil {
					return nil, err
				}
				x[key] = updated
			}
		}
		return x, nil
	default:
		return raw, nil
	}
}

func (s *expertTextInlineState) tryInlineTextBlock(block map[string]any) (map[string]any, bool, error) {
	if block == nil {
		return nil, false, nil
	}
	// Existing uploaded file referenced by id.
	if fileID := strings.TrimSpace(shared.AsString(block["file_id"])); fileID != "" {
		return s.inlineStoredFile(fileID, block)
	}
	// Fresh inline base64 / data-url file payload.
	decoded, ok, err := decodeOpenAIInlineFileBlock(block)
	if err != nil {
		return nil, true, &inlineFileUploadError{status: http.StatusBadRequest, message: err.Error(), err: err}
	}
	if !ok {
		return nil, false, nil
	}
	if !IsTextFile(decoded.Filename, decoded.ContentType, s.allowedExts) {
		// Non-text inline files are left untouched for expert models; they
		// cannot be forwarded as references and will be ignored by prompt
		// normalization, but removing them here is not our concern.
		return nil, false, nil
	}
	if err := s.checkSize(len(decoded.Data), decoded.Filename); err != nil {
		return nil, true, err
	}
	return textBlock(decoded.Data), true, nil
}

func (s *expertTextInlineState) inlineStoredFile(fileID string, block map[string]any) (map[string]any, bool, error) {
	if s.store == nil {
		return nil, false, nil
	}
	filename, mimeType, data, err := s.store.Read(fileID)
	if err != nil {
		if err == ErrFileTooLarge {
			return nil, true, &inlineFileUploadError{
				status:  http.StatusRequestEntityTooLarge,
				message: fmt.Sprintf("text file %q exceeds max inline size (%d bytes)", fileID, s.maxFileBytes),
				err:     err,
			}
		}
		return nil, true, &inlineFileUploadError{
			status:  http.StatusBadRequest,
			message: fmt.Sprintf("text file content unavailable for %q", fileID),
			err:     err,
		}
	}
	if !IsTextFile(filename, mimeType, s.allowedExts) {
		// Non-text uploaded files are left untouched; they will be dropped from
		// the expert payload later because ref_file_ids is cleared.
		return nil, false, nil
	}
	if err := s.checkSize(len(data), filename); err != nil {
		return nil, true, err
	}
	return textBlock(data), true, nil
}

func (s *expertTextInlineState) checkSize(n int, filename string) error {
	if s.maxFileBytes > 0 && n > s.maxFileBytes {
		return &inlineFileUploadError{
			status:  http.StatusRequestEntityTooLarge,
			message: fmt.Sprintf("text file %q exceeds max inline size (%d bytes)", filename, s.maxFileBytes),
		}
	}
	s.inlineCount++
	s.inlineBytes += n
	return nil
}

func textBlock(data []byte) map[string]any {
	return map[string]any{
		"type": "text",
		"text": sanitizeFileText(data),
	}
}

func sanitizeFileText(data []byte) string {
	return strings.ToValidUTF8(string(data), "\ufffd")
}
