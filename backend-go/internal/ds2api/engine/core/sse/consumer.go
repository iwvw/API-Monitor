package sse

import (
	"net/http"
	"strings"

	dsprotocol "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/deepseek/protocol"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/util"
)

// CollectResult holds the aggregated text and thinking content from a
// DeepSeek SSE stream, consumed to completion (non-streaming use case).
type CollectResult struct {
	Text                  string
	Thinking              string
	ToolDetectionThinking string
	ContentFilter         bool
	UpstreamError         string
	CitationLinks         map[int]string
	ResponseMessageID     int
}

// CollectStream fully consumes a DeepSeek SSE response and separates
// thinking content from text content. This replaces the duplicated
// stream-collection logic in openai.handleNonStream, claude.collectDeepSeek,
// and admin.testAccount.
//
// The caller is responsible for closing resp.Body unless closeBody is true.
func CollectStream(resp *http.Response, thinkingEnabled bool, closeBody bool) CollectResult {
	if closeBody {
		defer func() { _ = resp.Body.Close() }()
	}
	text := strings.Builder{}
	thinking := strings.Builder{}
	toolDetectionThinking := strings.Builder{}
	contentFilter := false
	upstreamError := ""
	stopped := false
	collector := newCitationLinkCollector()
	responseMessageID := 0
	var repeatTail = NewRepeatTail(0)
	currentType := "text"
	if thinkingEnabled {
		currentType = "thinking"
	}
	_ = dsprotocol.ScanSSELines(resp, func(line []byte) bool {
		chunk, done, parsed := ParseDeepSeekSSELine(line)
		if parsed && !done {
			collector.ingestChunk(chunk)
			observeResponseMessageID(chunk, &responseMessageID)
		}
		if done {
			return false
		}
		if stopped {
			return true
		}
		result := ParseDeepSeekContentLine(line, thinkingEnabled, currentType)
		currentType = result.NextType
		if !result.Parsed {
			return true
		}
		for _, p := range result.Parts {
			if repeatTail.Observe(p.Text, p.Type == "thinking") {
				upstreamError = "output loop detected"
				return false
			}
		}
		if result.Stop {
			if result.ContentFilter {
				contentFilter = true
			}
			if result.ErrorMessage != "" && upstreamError == "" {
				upstreamError = result.ErrorMessage
			}
			// Keep scanning to collect late-arriving citation metadata lines
			// that can appear after response/status=FINISHED, but stop as soon
			// as [DONE] arrives.
			stopped = true
			return true
		}
		for _, p := range result.Parts {
			if p.Type == "thinking" {
				trimmed := TrimContinuationOverlap(thinking.String(), p.Text)
				thinking.WriteString(trimmed)
			} else {
				trimmed := TrimContinuationOverlap(text.String(), p.Text)
				text.WriteString(trimmed)
			}
		}
		for _, p := range result.ToolDetectionThinkingParts {
			trimmed := TrimContinuationOverlap(toolDetectionThinking.String(), p.Text)
			toolDetectionThinking.WriteString(trimmed)
		}
		return true
	})
	return CollectResult{
		Text:                  text.String(),
		Thinking:              thinking.String(),
		ToolDetectionThinking: toolDetectionThinking.String(),
		ContentFilter:         contentFilter,
		UpstreamError:         upstreamError,
		CitationLinks:         collector.build(),
		ResponseMessageID:     responseMessageID,
	}
}

// observeResponseMessageID extracts the response_message_id from a parsed SSE
// chunk. It mirrors the extraction logic in client_continue.go's observe
// method, checking top-level response_message_id, v.response.message_id, and
// message.response.message_id.
func observeResponseMessageID(chunk map[string]any, out *int) {
	if chunk == nil || out == nil {
		return
	}
	if id := util.IntFrom(chunk["response_message_id"]); id > 0 {
		*out = id
	}
	v, _ := chunk["v"].(map[string]any)
	if response, _ := v["response"].(map[string]any); response != nil {
		if id := util.IntFrom(response["message_id"]); id > 0 {
			*out = id
		}
	}
	if message, _ := chunk["message"].(map[string]any); message != nil {
		if response, _ := message["response"].(map[string]any); response != nil {
			if id := util.IntFrom(response["message_id"]); id > 0 {
				*out = id
			}
		}
	}
}
