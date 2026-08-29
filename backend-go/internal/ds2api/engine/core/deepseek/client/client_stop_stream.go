package client

import (
	"bufio"
	"context"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
)

const (
	segmentContentWaitTimeout = 20 * time.Second
	segmentDrainTimeout       = 5 * time.Second
	segmentSettleDelay        = 1 * time.Second
)

func (c *Client) StopStream(ctx context.Context, a *auth.RequestAuth, sessionID string, messageID int) error {
	if strings.TrimSpace(sessionID) == "" || messageID <= 0 {
		return errors.New("missing stop_stream identifiers")
	}
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken, a.Account.Locale)
	payload := map[string]any{
		"chat_session_id": sessionID,
		"message_id":      messageID,
	}
	applySessionReferer(headers, payload)
	resp, status, err := c.postJSONWithStatus(ctx, clients.regular, clients.fallback, dsprotocol.DeepSeekStopStreamURL, headers, payload)
	if err != nil {
		config.Logger.Warn("[stop_stream] request error", "session_id", sessionID, "message_id", messageID, "account", a.AccountID, "error", err)
		return err
	}
	code, bizCode, msg, bizMsg := extractResponseStatus(resp)
	if status != http.StatusOK || code != 0 || bizCode != 0 {
		config.Logger.Warn("[stop_stream] non-success response", "session_id", sessionID, "message_id", messageID, "account", a.AccountID, "status", status, "code", code, "biz_code", bizCode, "msg", msg, "biz_msg", bizMsg, "resp", fmt.Sprintf("%v", resp))
		return fmt.Errorf("stop_stream failed: status=%d code=%d biz_code=%d msg=%s biz_msg=%s", status, code, bizCode, msg, bizMsg)
	}
	config.Logger.Info("[stop_stream] ok", "session_id", sessionID, "message_id", messageID, "account", a.AccountID)
	return nil
}

func (c *Client) FireCompletionAndStop(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string) (int, error) {
	sessionID, _ := payload["chat_session_id"].(string)
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken, a.Account.Locale)
	headers["x-ds-pow-response"] = powResp
	applySessionReferer(headers, payload)
	captureSession := c.capture.Start("deepseek_completion", dsprotocol.DeepSeekCompletionURL, a.AccountID, payload)
	resp, err := c.streamPostOnce(ctx, clients.stream, dsprotocol.DeepSeekCompletionURL, headers, payload)
	if err != nil {
		config.Logger.Warn("[fire_completion_and_stop] completion request failed", "session_id", sessionID, "account", a.AccountID, "error", err)
		return 0, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			config.Logger.Warn("[fire_completion_and_stop] response body close failed", "error", err)
		}
	}()
	if captureSession != nil {
		resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		config.Logger.Warn("[fire_completion_and_stop] completion returned non-200", "session_id", sessionID, "account", a.AccountID, "status", resp.StatusCode)
		return 0, fmt.Errorf("completion returned HTTP %d", resp.StatusCode)
	}

	newBody, muted, muteUntil, err := detectMutedCompletion(resp.Body)
	if err != nil {
		config.Logger.Warn("[fire_completion_and_stop] mute detection failed", "session_id", sessionID, "account", a.AccountID, "error", err)
		return 0, err
	}
	if muted {
		config.Logger.Warn("[fire_completion_and_stop] account muted", "session_id", sessionID, "account", a.AccountID)
		c.persistMutedUntil(a.AccountID, muteUntil)
		return 0, &RequestFailure{Op: "completion", Kind: FailureMuted, Message: "user is muted"}
	}
	if newBody != nil {
		resp.Body = newBody
	}

	responseMessageID := 0
	requestMessageID := 0
	hadContent := false
	streamEnded := false
	var ssePreview strings.Builder
	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			trimmed := strings.TrimSpace(string(line))
			if trimmed == "" || !strings.HasPrefix(trimmed, "data:") {
				if ssePreview.Len() < 2000 && trimmed != "" {
					if ssePreview.Len() > 0 {
						ssePreview.WriteByte('\n')
					}
					ssePreview.WriteString(trimmed)
				}
			} else {
				if ssePreview.Len() < 2000 {
					if ssePreview.Len() > 0 {
						ssePreview.WriteByte('\n')
					}
					ssePreview.WriteString(trimmed)
				}
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data != "[DONE]" {
					var chunk map[string]any
					if json.Unmarshal([]byte(data), &chunk) == nil {
						if id := intFrom(chunk["request_message_id"]); id > 0 && requestMessageID == 0 {
							requestMessageID = id
						}
						extractResponseMessageID(chunk, &responseMessageID)
						if !hadContent {
							parts, _, _, _ := sse.ParseSSEChunkForContentDetailed(chunk, true, "text")
							if len(parts) > 0 {
								hadContent = true
							}
						}
					}
				}
			}
			if responseMessageID > 0 {
				break
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				streamEnded = true
				break
			}
			config.Logger.Warn("[fire_completion_and_stop] SSE scan error", "session_id", sessionID, "account", a.AccountID, "error", readErr)
			return 0, readErr
		}
	}
	if responseMessageID <= 0 {
		previewStr := ssePreview.String()
		if strings.HasPrefix(strings.TrimSpace(previewStr), "{") {
			var errResp map[string]any
			if json.Unmarshal([]byte(previewStr), &errResp) == nil {
				code := intFrom(errResp["code"])
				msg, _ := errResp["msg"].(string)
				if msg == "" {
					if data, _ := errResp["data"].(map[string]any); data != nil {
						msg, _ = data["biz_msg"].(string)
					}
				}
				if code != 0 || msg != "" {
					config.Logger.Warn("[fire_completion_and_stop] upstream JSON error", "session_id", sessionID, "account", a.AccountID, "parent_message_id", payload["parent_message_id"], "code", code, "msg", msg, "raw", previewStr)
					return 0, fmt.Errorf("completion upstream error: code=%d msg=%s", code, msg)
				}
			}
		}
		config.Logger.Warn("[fire_completion_and_stop] response_message_id not received before stream ended", "session_id", sessionID, "account", a.AccountID, "parent_message_id", payload["parent_message_id"], "request_message_id", requestMessageID, "sse_preview", previewStr)
		return 0, errors.New("response_message_id not received before stream ended")
	}
	config.Logger.Info("[fire_completion_and_stop] captured ids", "session_id", sessionID, "account", a.AccountID, "request_message_id", requestMessageID, "response_message_id", responseMessageID, "had_content", hadContent)

	if streamEnded && !hadContent {
		config.Logger.Info("[fire_completion_and_stop] stream ended before content, no stop needed", "session_id", sessionID, "account", a.AccountID, "request_message_id", requestMessageID, "response_message_id", responseMessageID)
		return responseMessageID, nil
	}

	contentCh := make(chan struct{}, 1)
	drainDone := make(chan struct{})
	closeCh := make(chan struct{})
	go func() {
		closedSignaled := false
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				if lineHasContent(line) {
					select {
					case contentCh <- struct{}{}:
					default:
					}
				}
				if !closedSignaled && lineIsEventClose(line) {
					closedSignaled = true
					select {
					case closeCh <- struct{}{}:
					default:
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					config.Logger.Warn("[fire_completion_and_stop] drain stream error", "session_id", sessionID, "account", a.AccountID, "error", err)
				}
				break
			}
		}
		close(drainDone)
	}()

	if !hadContent {
		select {
		case <-contentCh:
			hadContent = true
		case <-drainDone:
			config.Logger.Info("[fire_completion_and_stop] stream ended before content, no stop needed", "session_id", sessionID, "account", a.AccountID, "request_message_id", requestMessageID, "response_message_id", responseMessageID)
			return responseMessageID, nil
		case <-time.After(segmentContentWaitTimeout):
			config.Logger.Warn("[fire_completion_and_stop] content wait timed out, aborting segment", "session_id", sessionID, "account", a.AccountID, "request_message_id", requestMessageID, "response_message_id", responseMessageID, "wait", segmentContentWaitTimeout)
			return 0, fmt.Errorf("segment content wait timed out after %s (session=%s, response_message_id=%d)", segmentContentWaitTimeout, sessionID, responseMessageID)
		case <-ctx.Done():
			config.Logger.Warn("[fire_completion_and_stop] context cancelled while waiting for content", "session_id", sessionID, "account", a.AccountID, "error", ctx.Err())
			return responseMessageID, ctx.Err()
		}
	}

	stopCalledAt := time.Now()
	config.Logger.Info("[fire_completion_and_stop] content detected, calling stop_stream", "session_id", sessionID, "account", a.AccountID, "request_message_id", requestMessageID, "response_message_id", responseMessageID)
	if err := c.StopStream(ctx, a, sessionID, responseMessageID); err != nil {
		config.Logger.Warn("[fire_completion_and_stop] stop_stream failed", "session_id", sessionID, "message_id", responseMessageID, "error", err)
		return 0, err
	}

	// After stop, wait for upstream's event: close confirmation (message committed)
	// or drain completion, with a timeout fallback.
	select {
	case <-closeCh:
		config.Logger.Info("[fire_completion_and_stop] upstream close confirmed", "session_id", sessionID, "account", a.AccountID, "response_message_id", responseMessageID)
	case <-drainDone:
		config.Logger.Info("[fire_completion_and_stop] drain completed", "session_id", sessionID, "account", a.AccountID, "response_message_id", responseMessageID)
	case <-time.After(segmentDrainTimeout):
		config.Logger.Warn("[fire_completion_and_stop] drain stream timed out, forcing close", "session_id", sessionID, "account", a.AccountID)
	}

	// Brief settle after upstream close confirmation to let the session
	// finish committing the previous message before sending the next segment.
	select {
	case <-time.After(segmentSettleDelay):
	case <-ctx.Done():
		config.Logger.Warn("[fire_completion_and_stop] context cancelled during settle delay", "session_id", sessionID, "account", a.AccountID, "error", ctx.Err())
		return responseMessageID, ctx.Err()
	}

	config.Logger.Info("[fire_completion_and_stop] segment sent and stopped", "session_id", sessionID, "response_message_id", responseMessageID, "request_message_id", requestMessageID, "drain_after_stop", time.Since(stopCalledAt), "had_content", hadContent, "account", a.AccountID)
	return responseMessageID, nil
}

func lineHasContent(line []byte) bool {
	result := sse.ParseDeepSeekContentLine(line, true, "text")
	if !result.Parsed || result.Stop {
		return false
	}
	return len(result.Parts) > 0 || len(result.ToolDetectionThinkingParts) > 0
}

func lineIsEventClose(line []byte) bool {
	return strings.HasPrefix(strings.TrimSpace(string(line)), "event: close")
}

func extractResponseMessageID(chunk map[string]any, out *int) {
	if chunk == nil || out == nil {
		return
	}
	if id := intFrom(chunk["response_message_id"]); id > 0 {
		*out = id
	}
	if v, ok := chunk["v"].(map[string]any); ok {
		if response, _ := v["response"].(map[string]any); response != nil {
			if id := intFrom(response["message_id"]); id > 0 {
				*out = id
			}
		}
	}
	if message, _ := chunk["message"].(map[string]any); message != nil {
		if response, _ := message["response"].(map[string]any); response != nil {
			if id := intFrom(response["message_id"]); id > 0 {
				*out = id
			}
		}
	}
}
