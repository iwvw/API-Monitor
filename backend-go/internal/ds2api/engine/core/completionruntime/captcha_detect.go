package completionruntime

import (
	"encoding/json"

	dsclient "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/client"
)

// TryDetectCaptchaFromBody parses an HTTP error body as JSON and checks for a
// shumei/captcha risk-control challenge. Returns a non-empty detail string when
// a challenge is detected, allowing the caller to map the failure to a 429 for
// account-switch retry.
func TryDetectCaptchaFromBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	ch := dsclient.DetectCaptchaChallenge(resp)
	if ch == nil {
		return ""
	}
	if ch.Instruction != "" {
		return ch.Instruction
	}
	if ch.ImageURL != "" {
		return ch.ImageURL
	}
	return "captcha_required"
}

// tryDetectCaptchaFromBody is an internal alias for completionruntime use.
func tryDetectCaptchaFromBody(body []byte) string {
	return TryDetectCaptchaFromBody(body)
}
