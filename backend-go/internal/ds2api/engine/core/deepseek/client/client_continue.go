package client

import (
	"bufio"
	"bytes"
	"context"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

// defaultAutoContinueLimit caps the number of automatic continuation rounds
// spliced onto one completion. Long code outputs regularly exceed a single
// upstream response, so the cap must be generous (each round is one upstream
// response of a few thousand tokens).
const defaultAutoContinueLimit = 32

type continueOpenFunc func(context.Context, string, int) (*http.Response, error)

type continueState struct {
	sessionID         string
	responseMessageID int
	lastStatus        string
	finished          bool
	sawContent        bool
	sawDone           bool
}

// wrapCompletionWithAutoContinue wraps the completion response body so that
// if the upstream indicates the response is incomplete (INCOMPLETE /
// AUTO_CONTINUE), ds2api will automatically call the DeepSeek continue
// endpoint and splice the continuation SSE stream onto the original.
// The caller sees a single, seamless SSE stream.
func (c *Client) wrapCompletionWithAutoContinue(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string, resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}
	sessionID, _ := payload["chat_session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return resp
	}
	config.Logger.Debug("[auto_continue] wrapping completion response", "session_id", sessionID)
	resp.Body = newAutoContinueBody(ctx, resp.Body, sessionID, defaultAutoContinueLimit, func(ctx context.Context, sessionID string, responseMessageID int) (*http.Response, error) {
		return c.callContinue(ctx, a, sessionID, responseMessageID, powResp)
	})
	return resp
}

// callContinue sends a continue request to DeepSeek to resume generation.
func (c *Client) callContinue(ctx context.Context, a *auth.RequestAuth, sessionID string, responseMessageID int, powResp string) (*http.Response, error) {
	if strings.TrimSpace(sessionID) == "" || responseMessageID <= 0 {
		return nil, errors.New("missing continue identifiers")
	}
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken, a.Account.Locale)
	headers["x-ds-pow-response"] = powResp
	payload := map[string]any{
		"chat_session_id":    sessionID,
		"message_id":         responseMessageID,
		"fallback_to_resume": true,
	}
	applySessionReferer(headers, payload)
	config.Logger.Info("[auto_continue] calling continue", "session_id", sessionID, "message_id", responseMessageID)
	captureSession := c.capture.Start("deepseek_continue", dsprotocol.DeepSeekContinueURL, a.AccountID, payload)
	resp, err := c.streamPost(ctx, clients.stream, dsprotocol.DeepSeekContinueURL, headers, payload)
	if err != nil {
		return nil, err
	}
	if captureSession != nil {
		resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errors.New("continue failed")
	}
	return resp, nil
}

// ContinueCompletion resumes a partially generated message on the upstream via
// the continue endpoint. The response body is wrapped with the same automatic
// continuation machinery as CallCompletion so further INCOMPLETE / interrupted
// rounds are spliced transparently. It backs the runtime-level continue-resume
// retry for streams that were interrupted after producing partial output.
func (c *Client) ContinueCompletion(ctx context.Context, a *auth.RequestAuth, sessionID string, messageID int, powResp string) (*http.Response, error) {
	if c == nil {
		return nil, errors.New("client is nil")
	}
	resp, err := c.callContinue(ctx, a, sessionID, messageID, powResp)
	if err != nil {
		return nil, err
	}
	return c.wrapCompletionWithAutoContinue(ctx, a, map[string]any{"chat_session_id": sessionID}, powResp, resp), nil
}

// newAutoContinueBody returns a new ReadCloser that transparently pumps
// continuation rounds via an io.Pipe.
func newAutoContinueBody(ctx context.Context, initial io.ReadCloser, sessionID string, maxRounds int, openContinue continueOpenFunc) io.ReadCloser {
	if initial == nil || strings.TrimSpace(sessionID) == "" || openContinue == nil {
		return initial
	}
	if maxRounds <= 0 {
		maxRounds = defaultAutoContinueLimit
	}
	pr, pw := io.Pipe()
	go pumpAutoContinue(ctx, pw, initial, continueState{sessionID: sessionID}, maxRounds, openContinue)
	return pr
}

// pumpAutoContinue is the goroutine that drives the auto-continue loop.
// It reads the initial SSE body, checks whether a continue is required,
// and if so opens a new continue stream and splices it onto the pipe writer.
// A mid-body read error (upstream reset / EOF without terminal status) is
// itself treated as an interruption signal: when the response already has a
// message id, one more continue round is attempted so a truncated stream is
// resumed instead of silently delivered as a "complete" response.
func pumpAutoContinue(ctx context.Context, pw *io.PipeWriter, initial io.ReadCloser, state continueState, maxRounds int, openContinue continueOpenFunc) {
	defer func() { _ = pw.Close() }()
	current := initial
	rounds := 0
	for {
		hadDone, err := streamBodyWithContinueState(ctx, pw, current, &state)
		_ = current.Close()
		if err != nil {
			if ctx.Err() != nil {
				_ = pw.CloseWithError(err)
				return
			}
			config.Logger.Warn("[auto_continue] upstream stream interrupted mid-body", "error", err, "session_id", state.sessionID, "response_message_id", state.responseMessageID, "round", rounds)
			// Only resume when the upstream had NOT already signalled
			// completion via [DONE] — a post-[DONE] connection teardown is a
			// normal FIN/RST, not a truncation worth resuming.
			if !state.sawDone && state.responseMessageID > 0 && rounds < maxRounds {
				rounds++
				nextResp, nextErr := openContinue(ctx, state.sessionID, state.responseMessageID)
				if nextErr != nil {
					config.Logger.Warn("[auto_continue] interrupted resume continue failed", "round", rounds, "error", nextErr, "session_id", state.sessionID)
					_ = pw.CloseWithError(fmt.Errorf("upstream stream interrupted (%v); continue resume failed: %w", err, nextErr))
					return
				}
				config.Logger.Info("[auto_continue] resumed after mid-body interruption", "round", rounds, "session_id", state.sessionID, "response_message_id", state.responseMessageID)
				current = nextResp.Body
				state.prepareForNextRound()
				continue
			}
			_ = pw.CloseWithError(err)
			return
		}
		if state.shouldContinue() && rounds < maxRounds {
			rounds++
			config.Logger.Info("[auto_continue] continuing", "round", rounds, "session_id", state.sessionID, "message_id", state.responseMessageID, "status", state.lastStatus)
			nextResp, err := openContinue(ctx, state.sessionID, state.responseMessageID)
			if err != nil {
				config.Logger.Warn("[auto_continue] continue request failed", "round", rounds, "error", err)
				_ = pw.CloseWithError(err)
				return
			}
			current = nextResp.Body
			state.prepareForNextRound()
			continue
		}
		// Emit the final [DONE] sentinel if the upstream had one.
		if hadDone {
			if _, err := io.Copy(pw, bytes.NewBufferString("data: [DONE]\n")); err != nil {
				_ = pw.CloseWithError(err)
			}
		}
		return
	}
}

// streamBodyWithContinueState scans an SSE body line-by-line, writing each
// line through to pw while observing state signals. Intermediate [DONE]
// sentinels are consumed (not forwarded) so that the downstream only sees
// one final [DONE] at the very end.
func streamBodyWithContinueState(ctx context.Context, pw *io.PipeWriter, body io.Reader, state *continueState) (bool, error) {
	reader := bufio.NewReaderSize(body, 64*1024)
	hadDone := false
	for {
		select {
		case <-ctx.Done():
			return hadDone, ctx.Err()
		default:
		}
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if err == io.EOF {
				return hadDone, nil
			}
			return hadDone, err
		}
		trimmed := strings.TrimSpace(string(line))
		if trimmed != "" {
			if strings.HasPrefix(trimmed, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data == "[DONE]" {
					hadDone = true
					state.sawDone = true
					if err != nil && err != io.EOF {
						return hadDone, err
					}
					if err == io.EOF {
						return hadDone, nil
					}
					continue
				}
				state.observe(data)
			}
			if !strings.HasSuffix(string(line), "\n") {
				line = append(line, '\n')
			}
			if _, copyErr := io.Copy(pw, bytes.NewReader(line)); copyErr != nil {
				return hadDone, copyErr
			}
		}
		if err != nil {
			if err == io.EOF {
				return hadDone, nil
			}
			return hadDone, err
		}
	}
}

// observe extracts continue-relevant signals from an SSE JSON chunk.
func (s *continueState) observe(data string) {
	if s == nil || strings.TrimSpace(data) == "" {
		return
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		return
	}
	if chunkHasVisibleContent(chunk) {
		s.sawContent = true
	}
	// Top-level response_message_id
	if id := intFrom(chunk["response_message_id"]); id > 0 {
		s.responseMessageID = id
	}
	s.observeDirectPatch(asString(chunk["p"]), chunk["v"])
	if p, _ := chunk["p"].(string); p == "response" {
		s.observeBatchPatches("response", chunk["v"])
	} else {
		s.observeBatchPatches("", chunk["v"])
	}
	if v, _ := chunk["v"].(map[string]any); v != nil {
		s.observeResponseObject(v["response"])
	}
	if message, _ := chunk["message"].(map[string]any); message != nil {
		s.observeResponseObject(message["response"])
	}
}

func (s *continueState) observeDirectPatch(path string, value any) {
	if s == nil {
		return
	}
	switch strings.Trim(strings.TrimSpace(path), "/") {
	case "response/status", "status", "response/quasi_status", "quasi_status":
		s.setStatus(asString(value))
	case "response/auto_continue", "auto_continue":
		if v, ok := value.(bool); ok && v {
			s.lastStatus = "AUTO_CONTINUE"
		}
	}
}

func (s *continueState) observeResponseObject(raw any) {
	if s == nil {
		return
	}
	response, _ := raw.(map[string]any)
	if response == nil {
		return
	}
	if id := intFrom(response["message_id"]); id > 0 {
		s.responseMessageID = id
	}
	s.setStatus(asString(response["status"]))
	if autoContinue, ok := response["auto_continue"].(bool); ok && autoContinue {
		s.lastStatus = "AUTO_CONTINUE"
	}
}

func (s *continueState) observeBatchPatches(parentPath string, raw any) {
	if s == nil {
		return
	}
	patches, ok := raw.([]any)
	if !ok {
		return
	}
	for _, patch := range patches {
		m, ok := patch.(map[string]any)
		if !ok {
			continue
		}
		path := strings.TrimSpace(asString(m["p"]))
		if path == "" {
			continue
		}
		fullPath := path
		if parent := strings.Trim(strings.TrimSpace(parentPath), "/"); parent != "" && !strings.Contains(path, "/") {
			fullPath = parent + "/" + path
		}
		switch strings.Trim(strings.TrimSpace(fullPath), "/") {
		case "response/status", "status", "response/quasi_status", "quasi_status":
			s.setStatus(asString(m["v"]))
		case "response/auto_continue", "auto_continue":
			if v, ok := m["v"].(bool); ok && v {
				s.lastStatus = "AUTO_CONTINUE"
			}
		}
	}
}

func (s *continueState) setStatus(status string) {
	if s == nil {
		return
	}
	normalized := strings.TrimSpace(status)
	if normalized == "" {
		return
	}
	s.lastStatus = normalized
	if strings.EqualFold(normalized, "FINISHED") || strings.EqualFold(normalized, "CONTENT_FILTER") {
		s.finished = true
	}
}

// shouldContinue returns true when the upstream explicitly indicates the
// response is incomplete and we have enough information to issue a continue
// request. Plain WIP is not sufficient because normal streams begin in WIP.
// As a fallback, a stream that ended (EOF) without reaching a terminal status
// after producing content is treated as interrupted and also resumed, so an
// upstream that truncates a long response without an INCOMPLETE signal does
// not silently degrade into a partial answer.
func (s *continueState) shouldContinue() bool {
	if s == nil {
		return false
	}
	if s.finished || s.responseMessageID <= 0 || strings.TrimSpace(s.sessionID) == "" {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(s.lastStatus)) {
	case "INCOMPLETE", "AUTO_CONTINUE":
		return true
	default:
		// Relaxed interrupted-stream detection: an EOF without a terminal
		// status that produced content is a truncation candidate — unless the
		// upstream already signalled completion with [DONE], in which case a
		// following teardown is a normal connection close, not a truncation.
		return s.sawContent && !s.sawDone
	}
}

// prepareForNextRound resets ephemeral state before processing the next
// continuation stream.
func (s *continueState) prepareForNextRound() {
	if s == nil {
		return
	}
	s.finished = false
	s.lastStatus = ""
	s.sawContent = false
	s.sawDone = false
}

// chunkHasVisibleContent reports whether an SSE JSON chunk carries visible
// content (text, thinking or tool fragments) as opposed to envelope, status or
// heartbeat frames. It drives the interrupted-stream resume heuristic: a
// stream that produced visible content before ending without a terminal status
// is a truncation candidate worth resuming.
func chunkHasVisibleContent(chunk map[string]any) bool {
	if chunk == nil {
		return false
	}
	path, _ := chunk["p"].(string)
	if path != "" && sse.SkipPath(path) {
		return false
	}
	v, ok := chunk["v"]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case string:
		trimmed := strings.TrimSpace(x)
		if trimmed == "" {
			return false
		}
		switch strings.ToUpper(trimmed) {
		case "FINISHED", "WIP", "INCOMPLETE", "AUTO_CONTINUE", "CONTENT_FILTER":
			return false
		}
		return true
	case []any:
		for _, item := range x {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			itemPath, _ := m["p"].(string)
			if itemPath != "" {
				if sse.SkipPath(itemPath) || isContinueMetadataPath(itemPath) {
					continue
				}
				if strings.Contains(itemPath, "content") {
					return true
				}
			}
			if _, hasContent := m["content"]; hasContent {
				return true
			}
			if _, hasV := m["v"]; hasV {
				return true
			}
		}
		return false
	case map[string]any:
		if _, hasResponse := x["response"]; hasResponse {
			return false // initial envelope, not content
		}
		if text, _ := x["text"].(string); strings.TrimSpace(text) != "" {
			return true
		}
		if content, _ := x["content"].(string); strings.TrimSpace(content) != "" {
			return true
		}
		if frags, ok := x["fragments"].([]any); ok && len(frags) > 0 {
			return true
		}
		return false
	}
	return false
}

// isContinueMetadataPath lists patch paths that are session bookkeeping rather
// than visible content (status transitions, usage, mode switches, ids).
func isContinueMetadataPath(path string) bool {
	switch strings.TrimSpace(path) {
	case "response/status", "status",
		"response/quasi_status", "quasi_status",
		"response/auto_continue", "auto_continue",
		"conversation_mode", "accumulated_token_usage",
		"response/message_id", "message_id",
		"request_message_id", "response/request_message_id",
		"response/has_pending_fragment", "has_pending_fragment",
		"response/search_status":
		return true
	}
	return false
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		s := strings.TrimSpace(strings.ReplaceAll(strings.TrimSpace(fmt.Sprint(v)), "\u0000", ""))
		if s == "<nil>" {
			return ""
		}
		return s
	}
}
