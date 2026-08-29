package responsehistory

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/assistantturn"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/chathistory"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/prompt"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/promptcompat"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/usagestats"
)

type Session struct {
	store       *chathistory.Store
	entryID     string
	startedAt   time.Time
	lastPersist time.Time
	startParams chathistory.StartParams
	disabled    bool
}

const progressPersistInterval = time.Second

func progressPersistDue(now, last time.Time) bool {
	return last.IsZero() || now.Sub(last) >= progressPersistInterval
}

type StartParams struct {
	Store    *chathistory.Store
	Request  *http.Request
	Auth     *auth.RequestAuth
	Surface  string
	Standard promptcompat.StandardRequest
}

func Start(params StartParams) *Session {
	if params.Store == nil || params.Request == nil || params.Auth == nil {
		return nil
	}
	if !shouldCapture(params.Request) {
		return nil
	}
	startParams := chathistory.StartParams{
		CallerID:      strings.TrimSpace(params.Auth.CallerID),
		AccountID:     strings.TrimSpace(params.Auth.AccountID),
		Surface:       strings.TrimSpace(params.Surface),
		Model:         strings.TrimSpace(params.Standard.ResponseModel),
		ResolvedModel: strings.TrimSpace(params.Standard.ResolvedModel),
		Stream:        params.Standard.Stream,
		UserInput:     ExtractSingleUserInput(params.Standard.Messages),
		Messages:      ExtractAllMessages(params.Standard.Messages),
		HistoryText:   params.Standard.HistoryText,
		FinalPrompt:   params.Standard.FinalPrompt,
	}
	now := time.Now()
	if !params.Store.Enabled() {
		return &Session{
			store:       params.Store,
			startedAt:   now,
			lastPersist: now,
			startParams: startParams,
			disabled:    true,
		}
	}
	entry, err := params.Store.Start(startParams)
	session := &Session{
		store:       params.Store,
		entryID:     entry.ID,
		startedAt:   now,
		lastPersist: now,
		startParams: startParams,
	}
	if err != nil {
		if entry.ID == "" {
			config.Logger.Warn("[response_history] start failed", "surface", startParams.Surface, "error", err)
			return nil
		}
		config.Logger.Warn("[response_history] start persisted in memory after write failure", "surface", startParams.Surface, "error", err)
	}
	return session
}

func shouldCapture(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	if strings.TrimSpace(r.URL.Query().Get("__stream_prepare")) == "1" {
		return false
	}
	if strings.TrimSpace(r.URL.Query().Get("__stream_release")) == "1" {
		return false
	}
	return true
}

func ExtractSingleUserInput(messages []any) string {
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

func ExtractAllMessages(messages []any) []chathistory.Message {
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

func (s *Session) Progress(thinking, content string) {
	if s == nil || s.store == nil || s.disabled {
		return
	}
	now := time.Now()
	if !progressPersistDue(now, s.lastPersist) {
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

func (s *Session) Success(statusCode int, thinking, content, finishReason string, usage map[string]any) {
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

func (s *Session) recordUsage(usage map[string]any) {
	if s == nil {
		return
	}
	model := s.startParams.ResolvedModel
	if model == "" {
		model = s.startParams.Model
	}
	usagestats.Record(model, s.startParams.CallerID, usage)
}

func (s *Session) Error(statusCode int, message, finishReason, thinking, content string) {
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

func (s *Session) Discard() {
	if s == nil || s.store == nil || s.disabled {
		return
	}
	if err := s.store.Delete(s.entryID); err != nil {
		if !isMissingError(err) && !errors.Is(err, chathistory.ErrDisabled) {
			config.Logger.Warn("[response_history] discard failed", "surface", s.startParams.Surface, "error", err)
		}
	}
}

func (s *Session) SuccessTurn(statusCode int, turn assistantturn.Turn, usage map[string]any) {
	outcome := assistantturn.FinalizeTurn(turn, assistantturn.FinalizeOptions{})
	s.Success(
		statusCode,
		ThinkingForArchive(turn.RawThinking, turn.DetectionThinking, turn.Thinking),
		TextForArchive(turn.RawText, turn.Text),
		outcome.FinishReason,
		usage,
	)
}

func (s *Session) ErrorTurn(statusCode int, message, finishReason string, turn assistantturn.Turn) {
	s.Error(
		statusCode,
		message,
		finishReason,
		ThinkingForArchive(turn.RawThinking, turn.DetectionThinking, turn.Thinking),
		TextForArchive(turn.RawText, turn.Text),
	)
}

func TextForArchive(raw, visible string) string {
	if strings.TrimSpace(raw) != "" {
		return raw
	}
	return visible
}

func ThinkingForArchive(raw, detection, visible string) string {
	if strings.TrimSpace(raw) != "" {
		return raw
	}
	if strings.TrimSpace(detection) != "" {
		return detection
	}
	return visible
}

func GenericUsage(turn assistantturn.Turn) map[string]any {
	return map[string]any{
		"input_tokens":     turn.Usage.InputTokens,
		"output_tokens":    turn.Usage.OutputTokens,
		"reasoning_tokens": turn.Usage.ReasoningTokens,
		"total_tokens":     turn.Usage.TotalTokens,
	}
}

func (s *Session) retryMissingEntry() bool {
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
			config.Logger.Warn("[response_history] recreate missing entry failed", "surface", s.startParams.Surface, "error", err)
		}
		return false
	}
	s.entryID = entry.ID
	if err != nil {
		config.Logger.Warn("[response_history] recreate missing entry persisted in memory after write failure", "surface", s.startParams.Surface, "error", err)
	}
	return true
}

func (s *Session) persistUpdate(params chathistory.UpdateParams) {
	if s == nil || s.store == nil || s.disabled {
		return
	}
	if _, err := s.store.Update(s.entryID, params); err != nil {
		s.handlePersistError(params, err)
	}
}

func (s *Session) handlePersistError(params chathistory.UpdateParams, err error) {
	if err == nil || s == nil {
		return
	}
	if errors.Is(err, chathistory.ErrDisabled) {
		s.disabled = true
		return
	}
	if isMissingError(err) {
		if s.retryMissingEntry() {
			if _, retryErr := s.store.Update(s.entryID, params); retryErr != nil {
				if errors.Is(retryErr, chathistory.ErrDisabled) || isMissingError(retryErr) {
					s.disabled = true
					return
				}
				config.Logger.Warn("[response_history] retry after missing entry failed", "surface", s.startParams.Surface, "error", retryErr)
			}
			return
		}
		s.disabled = true
		return
	}
	config.Logger.Warn("[response_history] update failed", "surface", s.startParams.Surface, "error", err)
}

func isMissingError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		return strings.TrimSpace(prompt.NormalizeContent(x))
	}
}
