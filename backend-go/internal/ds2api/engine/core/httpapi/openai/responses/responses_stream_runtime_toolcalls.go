package responses

import (
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolcall"
	"github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/toolstream"
	"encoding/json"
	"strings"

	openaifmt "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/format/openai"

	"github.com/google/uuid"
)

func (s *responsesStreamRuntime) allocateOutputIndex() int {
	idx := s.nextOutputID
	s.nextOutputID++
	return idx
}

func (s *responsesStreamRuntime) ensureMessageItemID() string {
	if strings.TrimSpace(s.messageItemID) != "" {
		return s.messageItemID
	}
	s.messageItemID = "msg_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return s.messageItemID
}

func (s *responsesStreamRuntime) ensureMessageOutputIndex() int {
	if s.messageOutputID >= 0 {
		return s.messageOutputID
	}
	s.messageOutputID = s.allocateOutputIndex()
	return s.messageOutputID
}

func (s *responsesStreamRuntime) ensureMessageItemAdded() {
	if s.messageAdded {
		return
	}
	itemID := s.ensureMessageItemID()
	item := map[string]any{
		"id":     itemID,
		"type":   "message",
		"role":   "assistant",
		"status": "in_progress",
	}
	s.sendEvent(
		"response.output_item.added",
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, itemID, s.ensureMessageOutputIndex(), item),
	)
	s.messageAdded = true
}

func (s *responsesStreamRuntime) ensureMessageContentPartAdded() {
	if s.messagePartAdded {
		return
	}
	s.ensureMessageItemAdded()
	s.sendEvent(
		"response.content_part.added",
		openaifmt.BuildResponsesContentPartAddedPayload(
			s.responseID,
			s.ensureMessageItemID(),
			s.ensureMessageOutputIndex(),
			0,
			map[string]any{"type": "output_text", "text": ""},
		),
	)
	s.messagePartAdded = true
}

func (s *responsesStreamRuntime) ensureReasoningItemAdded() {
	if s.reasoningItemAdded {
		return
	}
	s.reasoningItemID = "rsn_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	s.reasoningOutputIndex = s.allocateOutputIndex()
	item := map[string]any{
		"id":      s.reasoningItemID,
		"type":    "reasoning",
		"summary": []any{},
		"status":  "in_progress",
	}
	s.sendEvent(
		"response.output_item.added",
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, s.reasoningItemID, s.reasoningOutputIndex, item),
	)
	s.reasoningItemAdded = true
}

func (s *responsesStreamRuntime) ensureReasoningContentPartAdded() {
	if s.reasoningPartAdded {
		return
	}
	s.ensureReasoningItemAdded()
	s.sendEvent(
		"response.content_part.added",
		openaifmt.BuildResponsesContentPartAddedPayload(
			s.responseID,
			s.reasoningItemID,
			s.reasoningOutputIndex,
			0,
			map[string]any{"type": "reasoning_text", "text": ""},
		),
	)
	s.reasoningPartAdded = true
}

func (s *responsesStreamRuntime) emitReasoningDelta(content string) {
	if content == "" {
		return
	}
	s.ensureReasoningContentPartAdded()
	s.visibleReasoning.WriteString(content)
	s.sendEvent(
		"response.reasoning_text.delta",
		openaifmt.BuildResponsesReasoningTextDeltaPayload(
			s.responseID,
			s.reasoningItemID,
			s.reasoningOutputIndex,
			0,
			content,
		),
	)
}

func (s *responsesStreamRuntime) closeReasoningItem() {
	if !s.reasoningItemAdded || s.reasoningClosed {
		return
	}
	s.reasoningClosed = true
	text := s.visibleReasoning.String()
	s.sendEvent(
		"response.reasoning_text.done",
		openaifmt.BuildResponsesReasoningTextDonePayload(
			s.responseID,
			s.reasoningItemID,
			s.reasoningOutputIndex,
			0,
			text,
		),
	)
	if s.reasoningPartAdded {
		s.sendEvent(
			"response.content_part.done",
			openaifmt.BuildResponsesContentPartDonePayload(
				s.responseID,
				s.reasoningItemID,
				s.reasoningOutputIndex,
				0,
				map[string]any{"type": "reasoning_text", "text": text},
			),
		)
		s.reasoningPartAdded = false
	}
	item := map[string]any{
		"id":   s.reasoningItemID,
		"type": "reasoning",
		"summary": []map[string]any{
			{"type": "summary_text", "text": text},
		},
		"content": []map[string]any{
			{"type": "reasoning_text", "text": text},
		},
		"status": "completed",
	}
	s.sendEvent(
		"response.output_item.done",
		openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, s.reasoningItemID, s.reasoningOutputIndex, item),
	)
}

func (s *responsesStreamRuntime) emitTextDelta(content string) {
	if content == "" {
		return
	}
	s.closeReasoningItem()
	s.ensureMessageContentPartAdded()
	s.visibleText.WriteString(content)
	s.sendEvent(
		"response.output_text.delta",
		openaifmt.BuildResponsesTextDeltaPayload(
			s.responseID,
			s.ensureMessageItemID(),
			s.ensureMessageOutputIndex(),
			0,
			content,
		),
	)
}

func (s *responsesStreamRuntime) closeMessageItem() {
	if !s.messageAdded {
		return
	}
	itemID := s.ensureMessageItemID()
	outputIndex := s.ensureMessageOutputIndex()
	text := s.visibleText.String()
	if s.messagePartAdded {
		s.sendEvent(
			"response.output_text.done",
			openaifmt.BuildResponsesTextDonePayload(
				s.responseID,
				itemID,
				outputIndex,
				0,
				text,
			),
		)
		s.sendEvent(
			"response.content_part.done",
			openaifmt.BuildResponsesContentPartDonePayload(
				s.responseID,
				itemID,
				outputIndex,
				0,
				map[string]any{"type": "output_text", "text": text},
			),
		)
		s.messagePartAdded = false
	}
	item := map[string]any{
		"id":     itemID,
		"type":   "message",
		"role":   "assistant",
		"status": "completed",
		"content": []map[string]any{
			{
				"type": "output_text",
				"text": text,
			},
		},
	}
	s.sendEvent(
		"response.output_item.done",
		openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, outputIndex, item),
	)
}

func (s *responsesStreamRuntime) ensureFunctionItemID(callIndex int) string {
	if id, ok := s.functionItemIDs[callIndex]; ok && strings.TrimSpace(id) != "" {
		return id
	}
	id := "fc_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	s.functionItemIDs[callIndex] = id
	return id
}

func (s *responsesStreamRuntime) ensureToolCallID(callIndex int) string {
	if id, ok := s.streamToolCallIDs[callIndex]; ok && strings.TrimSpace(id) != "" {
		return id
	}
	id := "call_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	s.streamToolCallIDs[callIndex] = id
	return id
}

func (s *responsesStreamRuntime) resetStreamToolCallState() {
	s.streamToolCallIDs = map[int]string{}
	s.functionItemIDs = map[int]string{}
	s.functionOutputIDs = map[int]int{}
	s.functionArgs = map[int]string{}
	s.functionDone = map[int]bool{}
	s.functionAdded = map[int]bool{}
	s.functionNames = map[int]string{}
}

func (s *responsesStreamRuntime) ensureFunctionOutputIndex(callIndex int) int {
	if idx, ok := s.functionOutputIDs[callIndex]; ok {
		return idx
	}
	idx := s.allocateOutputIndex()
	s.functionOutputIDs[callIndex] = idx
	return idx
}

func (s *responsesStreamRuntime) ensureFunctionItemAdded(callIndex int, name string) {
	if strings.TrimSpace(name) != "" {
		s.functionNames[callIndex] = strings.TrimSpace(name)
	}
	if s.functionAdded[callIndex] {
		return
	}
	s.closeReasoningItem()
	fnName := strings.TrimSpace(s.functionNames[callIndex])
	if fnName == "" {
		return
	}
	outputIndex := s.ensureFunctionOutputIndex(callIndex)
	itemID := s.ensureFunctionItemID(callIndex)
	callID := s.ensureToolCallID(callIndex)
	item := map[string]any{
		"id":        itemID,
		"type":      "function_call",
		"call_id":   callID,
		"name":      fnName,
		"arguments": "",
		"status":    "in_progress",
	}
	s.sendEvent(
		"response.output_item.added",
		openaifmt.BuildResponsesOutputItemAddedPayload(s.responseID, itemID, outputIndex, item),
	)
	s.functionAdded[callIndex] = true
	s.toolCallsEmitted = true
}

func (s *responsesStreamRuntime) emitFunctionCallDeltaEvents(deltas []toolstream.ToolCallDelta) {
	for _, d := range deltas {
		s.ensureFunctionItemAdded(d.Index, d.Name)
		if strings.TrimSpace(d.Arguments) == "" {
			continue
		}
		s.functionArgs[d.Index] += d.Arguments
		outputIndex := s.ensureFunctionOutputIndex(d.Index)
		itemID := s.ensureFunctionItemID(d.Index)
		callID := s.ensureToolCallID(d.Index)
		s.sendEvent(
			"response.function_call_arguments.delta",
			openaifmt.BuildResponsesFunctionCallArgumentsDeltaPayload(s.responseID, itemID, outputIndex, callID, d.Arguments),
		)
	}
}

func (s *responsesStreamRuntime) emitFunctionCallDoneEvents(calls []toolcall.ParsedToolCall) {
	normalizedCalls := toolcall.NormalizeParsedToolCallsForSchemas(calls, s.toolsRaw)
	for idx, tc := range normalizedCalls {
		if strings.TrimSpace(tc.Name) == "" {
			continue
		}
		s.ensureFunctionItemAdded(idx, tc.Name)
		if s.functionDone[idx] {
			continue
		}
		outputIndex := s.ensureFunctionOutputIndex(idx)
		itemID := s.ensureFunctionItemID(idx)
		callID := s.ensureToolCallID(idx)
		argsBytes, _ := json.Marshal(tc.Input)
		args := string(argsBytes)
		s.functionArgs[idx] = args
		s.sendEvent(
			"response.function_call_arguments.done",
			openaifmt.BuildResponsesFunctionCallArgumentsDonePayload(s.responseID, itemID, outputIndex, callID, tc.Name, args),
		)
		item := map[string]any{
			"id":        itemID,
			"type":      "function_call",
			"call_id":   callID,
			"name":      tc.Name,
			"arguments": args,
			"status":    "completed",
		}
		s.sendEvent(
			"response.output_item.done",
			openaifmt.BuildResponsesOutputItemDonePayload(s.responseID, itemID, outputIndex, item),
		)
		s.functionDone[idx] = true
		s.toolCallsDoneEmitted = true
	}
}
