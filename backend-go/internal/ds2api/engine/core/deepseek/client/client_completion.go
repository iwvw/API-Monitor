package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"io"
	"net/http"
	"strings"

	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/auth"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/config"
	trans "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/transport"
)

func (c *Client) CallCompletion(ctx context.Context, a *auth.RequestAuth, payload map[string]any, powResp string, maxAttempts int) (*http.Response, error) {
	_ = maxAttempts
	clients := c.requestClientsForAuth(ctx, a)
	headers := c.authHeaders(a.DeepSeekToken, a.Account.Locale)
	headers["x-ds-pow-response"] = powResp
	applySessionReferer(headers, payload)
	captureSession := c.capture.Start("deepseek_completion", dsprotocol.DeepSeekCompletionURL, a.AccountID, payload)
	resp, err := c.streamPostOnce(ctx, clients.stream, dsprotocol.DeepSeekCompletionURL, headers, payload)
	if err != nil {
		return nil, err
	}
	if captureSession != nil {
		resp.Body = captureSession.WrapBody(resp.Body, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusOK {
		newBody, muted, muteUntil, err := detectMutedCompletion(resp.Body)
		if err != nil {
			config.Logger.Warn("[deepseek_completion] failed to inspect response body for mute detection", "account", a.AccountID, "error", err)
		}
		if muted {
			_ = resp.Body.Close()
			c.persistMutedUntil(a.AccountID, muteUntil)
			return nil, &RequestFailure{Op: "completion", Kind: FailureMuted, Message: "user is muted"}
		}
		if newBody != nil {
			resp.Body = newBody
		}
		resp = c.wrapCompletionWithAutoContinue(ctx, a, payload, powResp, resp)
	}
	return resp, nil
}

func (c *Client) streamPost(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (*http.Response, error) {
	return c.streamPostWithFallback(ctx, doer, url, headers, payload, true)
}

func (c *Client) streamPostOnce(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any) (*http.Response, error) {
	return c.streamPostWithFallback(ctx, doer, url, headers, payload, false)
}

func (c *Client) streamPostWithFallback(ctx context.Context, doer trans.Doer, url string, headers map[string]string, payload any, allowFallback bool) (*http.Response, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	headers = c.jsonHeaders(headers)
	clients := c.requestClientsFromContext(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := doer.Do(req)
	if err != nil {
		if allowFallback {
			config.Logger.Warn("[deepseek] fingerprint stream request failed, fallback to std transport", "url", url, "error", err)
			req2, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
			if reqErr != nil {
				return nil, reqErr
			}
			for k, v := range headers {
				req2.Header.Set(k, v)
			}
			return clients.fallbackS.Do(req2)
		}
		return nil, err
	}
	return resp, nil
}

// detectMutedCompletion peeks the response body to determine whether the
// upstream returned a muted-account JSON error instead of an SSE stream.
// DeepSeek returns HTTP 200 with a plain JSON body (not text/event-stream)
// when the account is muted: {"code":0,"data":{"biz_code":5,"biz_msg":"user is muted",...}}.
// Returns (restoredBody, muted, muteUntil, error).
// When muted is true, restoredBody is nil and the caller should close the original body.
// When muted is false, restoredBody contains the unread body for downstream SSE parsing.
func detectMutedCompletion(body io.ReadCloser) (io.ReadCloser, bool, float64, error) {
	if body == nil {
		return nil, false, 0, nil
	}
	br := bufio.NewReader(body)
	b, err := br.Peek(1)
	if err != nil && err != io.EOF {
		return io.NopCloser(br), false, 0, nil
	}
	if len(b) == 0 || b[0] != '{' {
		return io.NopCloser(br), false, 0, nil
	}
	// Muted/error responses are small JSON payloads. Reading the whole body
	// here for a giant (non-SSE) response would blow up memory, so cap the
	// inspection at 1 MiB — enough for any real mute/error envelope.
	capped, err := io.ReadAll(io.LimitReader(br, 1<<20))
	if err != nil {
		return io.NopCloser(bytes.NewReader(nil)), false, 0, err
	}
	// Rebuild the untouched stream for the downstream SSE parser: the data we
	// already consumed plus whatever remains in br. When the body fits under
	// the cap, br is at EOF so this is lossless; when we hit the cap it is
	// still exactly the original byte order.
	replay := io.NopCloser(io.MultiReader(bytes.NewReader(capped), br))
	if len(capped) >= 1<<20 {
		return replay, false, 0, nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(capped, &parsed); err != nil {
		return replay, false, 0, nil
	}
	if isMutedJSONResponse(parsed) {
		return nil, true, extractMuteUntil(parsed), nil
	}
	return io.NopCloser(bytes.NewReader(capped)), false, 0, nil
}

// isMutedJSONResponse checks whether a parsed JSON response indicates a muted account.
// Matches on biz_code==5, biz_msg containing "muted", or is_muted==1 in biz_data.
func isMutedJSONResponse(resp map[string]any) bool {
	if resp == nil {
		return false
	}
	data, _ := resp["data"].(map[string]any)
	if data == nil {
		return false
	}
	bizCode := intFrom(data["biz_code"])
	bizMsg := strings.ToLower(strings.TrimSpace(getStringFromMap(data, "biz_msg")))
	if bizCode == 5 || strings.Contains(bizMsg, "muted") {
		return true
	}
	bizData, _ := data["biz_data"].(map[string]any)
	if bizData != nil {
		if isMuted, _ := bizData["is_muted"].(float64); isMuted == 1 {
			return true
		}
	}
	return false
}

// extractMuteUntil extracts the mute_until timestamp from a muted JSON response.
func extractMuteUntil(resp map[string]any) float64 {
	if resp == nil {
		return 0
	}
	data, _ := resp["data"].(map[string]any)
	if data == nil {
		return 0
	}
	bizData, _ := data["biz_data"].(map[string]any)
	if bizData != nil {
		if muteUntil, ok := bizData["mute_until"].(float64); ok {
			return muteUntil
		}
	}
	return 0
}

func getStringFromMap(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// applySessionReferer points Referer at the conversation page the message
// belongs to. A browser sending a message is sitting on /a/chat/s/<id>, so a
// bare site root on every request is a mismatch between the header and the
// action it accompanies.
func applySessionReferer(headers map[string]string, payload map[string]any) {
	if headers == nil || payload == nil {
		return
	}
	sessionID := getStringFromMap(payload, "chat_session_id")
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	headers["Referer"] = dsprotocol.ChatSessionReferer(sessionID)
}
