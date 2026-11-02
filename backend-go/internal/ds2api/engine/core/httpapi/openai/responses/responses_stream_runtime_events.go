package responses

import (
	"encoding/json"

	openaifmt "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/format/openai"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/sse"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolstream"
)

func (s *responsesStreamRuntime) nextSequence() int {
	s.sequence++
	return s.sequence
}

func (s *responsesStreamRuntime) sendEvent(event string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["sequence_number"]; !ok {
		payload["sequence_number"] = s.nextSequence()
	}
	b, _ := json.Marshal(payload)
	_, _ = s.w.Write([]byte("event: " + event + "\n"))
	_, _ = s.w.Write([]byte("data: "))
	_, _ = s.w.Write(b)
	_, _ = s.w.Write([]byte("\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *responsesStreamRuntime) sendCreated() {
	s.sendEvent("response.created", openaifmt.BuildResponsesCreatedPayload(s.responseID, s.model))
}

func (s *responsesStreamRuntime) sendDone() {
	_, _ = s.w.Write([]byte("data: [DONE]\n\n"))
	if s.canFlush {
		_ = s.rc.Flush()
	}
}

func (s *responsesStreamRuntime) processToolStreamEvents(events []toolstream.Event, emitContent bool, resetAfterToolCalls bool) {
	for _, evt := range events {
		if emitContent && evt.Content != "" && !s.toolCallsEmitted {
			// 已发出 tool_calls 后，工具调用块之后的尾巴正文不再透传，
			// 避免工具调用最后几行结果泄漏到正文。
			cleaned := cleanVisibleOutput(evt.Content, s.stripReferenceMarkers)
			if cleaned != "" && (!s.searchEnabled || !sse.IsCitation(cleaned)) {
				s.emitTextDelta(cleaned)
			}
		}
		if len(evt.ToolCallDeltas) > 0 {
			if !s.emitEarlyToolDeltas {
				continue
			}
			filtered := filterIncrementalToolCallDeltasByAllowed(evt.ToolCallDeltas, s.functionNames)
			if len(filtered) == 0 {
				continue
			}
			s.emitFunctionCallDeltaEvents(filtered)
		}
		if len(evt.ToolCalls) > 0 {
			s.emitFunctionCallDoneEvents(evt.ToolCalls)
			if resetAfterToolCalls {
				s.resetStreamToolCallState()
			}
		}
	}
}
