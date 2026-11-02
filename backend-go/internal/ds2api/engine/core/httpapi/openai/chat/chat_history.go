package chat

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	openaifmt "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/format/openai"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/prompt"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/usagestats"
)

type chatHistorySession struct {
	store       *chathistory.Store
	entryID     string
	startedAt   time.Time
	lastPersist time.Time
	finalPrompt string
	startParams chathistory.StartParams
	disabled    bool
}

func startChatHistory(store *chathistory.Store, r *http.Request, a *auth.RequestAuth, stdReq promptcompat.StandardRequest) *chatHistorySession {
	if store == nil || r == nil || a == nil {
		return nil
	}
	if !shouldCaptureChatHistory(r) {
		return nil
	}
	startParams := chathistory.StartParams{
		CallerID:      strings.TrimSpace(a.CallerID),
		AccountID:     strings.TrimSpace(a.AccountID),
		Surface:       "openai.chat_completions",
		Model:         strings.TrimSpace(stdReq.ResponseModel),
		ResolvedModel: strings.TrimSpace(stdReq.ResolvedModel),
		Stream:        stdReq.Stream,
		UserInput:     extractSingleUserInput(stdReq.Messages),
		Messages:      extractAllMessages(stdReq.Messages),
		HistoryText:   stdReq.HistoryText,
		FinalPrompt:   stdReq.FinalPrompt,
	}
	now := time.Now()
	if !store.Enabled() {
		return &chatHistorySession{
			store:       store,
			startedAt:   now,
			lastPersist: now,
			finalPrompt: stdReq.FinalPrompt,
			startParams: startParams,
			disabled:    true,
		}
	}
	entry, err := store.Start(startParams)
	session := &chatHistorySession{
		store:       store,
		entryID:     entry.ID,
		startedAt:   now,
		lastPersist: now,
		finalPrompt: stdReq.FinalPrompt,
		startParams: startParams,
	}
	if err != nil {
		if entry.ID == "" {
			config.Logger.Warn("[chat_history] start failed", "error", err)
			return nil
		}
		config.Logger.Warn("[chat_history] start persisted in memory after write failure", "error", err)
	}
	return session
}

func shouldCaptureChatHistory(r *http.Request) bool {
	if r == nil {
		return false
	}
	if isVercelStreamPrepareRequest(r) {
		return false
	}
	return true
}

func extractSingleUserInput(messages []any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
		if role != "user" {
			continue
		}
		if normalized := strings.TrimSpace(prompt.NormalizeContent(msg["content"])); normalized != "" {
			return normalized
		}
	}
	return ""
}

func extractAllMessages(messages []any) []chathistory.Message {
	out := make([]chathistory.Message, 0, len(messages))
	for _, raw := range messages {
		msg, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(asString(msg["role"])))
		content := strings.TrimSpace(prompt.NormalizeContent(msg["content"]))
		if role == "" || content == "" {
			continue
		}
		out = append(out, chathistory.Message{
			Role:    role,
			Content: content,
		})
	}
	return out
}

func (s *chatHistorySession) progress(thinking, content string) {
	if s == nil || s.store == nil || s.disabled {
		return
	}
	now := time.Now()
	if now.Sub(s.lastPersist) < 250*time.Millisecond {
		return
	}
	s.lastPersist = now
	s.persistUpdate(chathistory.UpdateParams{
		Status:           "streaming",
		ReasoningContent: thinking,
		Content:          content,
		StatusCode:       http.StatusOK,
		ElapsedMs:        time.Since(s.startedAt).Milliseconds(),
	})
}

func (s *chatHistorySession) success(statusCode int, thinking, content, finishReason string, usage map[string]any) {
	if s == nil {
		return
	}
	s.recordUsage(usage)
	if s.store == nil || s.disabled {
		return
	}
	s.persistUpdate(chathistory.UpdateParams{
		Status:           "success",
		ReasoningContent: thinking,
		Content:          content,
		StatusCode:       statusCode,
		ElapsedMs:        time.Since(s.startedAt).Milliseconds(),
		FinishReason:     finishReason,
		Usage:            usage,
		Completed:        true,
	})
}

func (s *chatHistorySession) error(statusCode int, message, finishReason, thinking, content string) {
	if s == nil || s.store == nil || s.disabled {
		return
	}
	s.persistUpdate(chathistory.UpdateParams{
		Status:           "error",
		ReasoningContent: thinking,
		Content:          content,
		Error:            message,
		StatusCode:       statusCode,
		ElapsedMs:        time.Since(s.startedAt).Milliseconds(),
		FinishReason:     finishReason,
		Completed:        true,
	})
}

func (s *chatHistorySession) stopped(thinking, content, finishReason string) {
	if s == nil {
		return
	}
	usage := openaifmt.BuildChatUsage(s.finalPrompt, thinking, content)
	s.recordUsage(usage)
	if s.store == nil || s.disabled {
		return
	}
	s.persistUpdate(chathistory.UpdateParams{
		Status:           "stopped",
		ReasoningContent: thinking,
		Content:          content,
		StatusCode:       http.StatusOK,
		ElapsedMs:        time.Since(s.startedAt).Milliseconds(),
		FinishReason:     finishReason,
		Usage:            usage,
		Completed:        true,
	})
}

func (s *chatHistorySession) discard() {
	if s == nil || s.store == nil || s.disabled {
		return
	}
	if err := s.store.Delete(s.entryID); err != nil {
		if !isChatHistoryMissingError(err) && !errors.Is(err, chathistory.ErrDisabled) {
			config.Logger.Warn("[chat_history] discard failed", "error", err)
		}
	}
}

func (s *chatHistorySession) recordUsage(usage map[string]any) {
	if s == nil {
		return
	}
	model := s.startParams.ResolvedModel
	if model == "" {
		model = s.startParams.Model
	}
	usagestats.Record(model, s.startParams.CallerID, usage)
}

func historyTextForArchive(raw, visible string) string {
	if strings.TrimSpace(raw) != "" {
		return raw
	}
	return visible
}

func historyThinkingForArchive(raw, detection, visible string) string {
	if strings.TrimSpace(raw) != "" {
		return raw
	}
	if strings.TrimSpace(detection) != "" {
		return detection
	}
	return visible
}

func (s *chatHistorySession) retryMissingEntry() bool {
	if s == nil || s.store == nil || s.disabled {
		return false
	}
	entry, err := s.store.Start(s.startParams)
	if errors.Is(err, chathistory.ErrDisabled) {
		s.disabled = true
		return false
	}
	if entry.ID == "" {
		if err != nil {
			config.Logger.Warn("[chat_history] recreate missing entry failed", "error", err)
		}
		return false
	}
	s.entryID = entry.ID
	if err != nil {
		config.Logger.Warn("[chat_history] recreate missing entry persisted in memory after write failure", "error", err)
	}
	return true
}

func (s *chatHistorySession) persistUpdate(params chathistory.UpdateParams) {
	if s == nil || s.store == nil || s.disabled {
		return
	}
	if _, err := s.store.Update(s.entryID, params); err != nil {
		s.handlePersistError(params, err)
	}
}

func (s *chatHistorySession) handlePersistError(params chathistory.UpdateParams, err error) {
	if err == nil || s == nil {
		return
	}
	if errors.Is(err, chathistory.ErrDisabled) {
		s.disabled = true
		return
	}
	if isChatHistoryMissingError(err) {
		if s.retryMissingEntry() {
			if _, retryErr := s.store.Update(s.entryID, params); retryErr != nil {
				if errors.Is(retryErr, chathistory.ErrDisabled) || isChatHistoryMissingError(retryErr) {
					s.disabled = true
					return
				}
				config.Logger.Warn("[chat_history] retry after missing entry failed", "error", retryErr)
			}
			return
		}
		s.disabled = true
		return
	}
	config.Logger.Warn("[chat_history] update failed", "error", err)
}

func isChatHistoryMissingError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}
