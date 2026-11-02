package openai

import "strings"

func BuildResponsesCreatedPayload(responseID, model string) map[string]any {
	return map[string]any{
		"type":        "response.created",
		"id":          responseID,
		"response_id": responseID,
		"object":      "response",
		"model":       model,
		"status":      "in_progress",
	}
}

func BuildResponsesOutputItemAddedPayload(responseID, itemID string, outputIndex int, item map[string]any) map[string]any {
	return map[string]any{
		"type":         "response.output_item.added",
		"id":           responseID,
		"response_id":  responseID,
		"output_index": outputIndex,
		"item_id":      itemID,
		"item":         item,
	}
}

func BuildResponsesOutputItemDonePayload(responseID, itemID string, outputIndex int, item map[string]any) map[string]any {
	return map[string]any{
		"type":         "response.output_item.done",
		"id":           responseID,
		"response_id":  responseID,
		"output_index": outputIndex,
		"item_id":      itemID,
		"item":         item,
	}
}

func BuildResponsesContentPartAddedPayload(responseID, itemID string, outputIndex, contentIndex int, part map[string]any) map[string]any {
	return map[string]any{
		"type":          "response.content_part.added",
		"id":            responseID,
		"response_id":   responseID,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
		"part":          part,
	}
}

func BuildResponsesContentPartDonePayload(responseID, itemID string, outputIndex, contentIndex int, part map[string]any) map[string]any {
	return map[string]any{
		"type":          "response.content_part.done",
		"id":            responseID,
		"response_id":   responseID,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
		"part":          part,
	}
}

func BuildResponsesTextDeltaPayload(responseID, itemID string, outputIndex, contentIndex int, delta string) map[string]any {
	return map[string]any{
		"type":          "response.output_text.delta",
		"id":            responseID,
		"response_id":   responseID,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
		"delta":         delta,
	}
}

func BuildResponsesTextDonePayload(responseID, itemID string, outputIndex, contentIndex int, text string) map[string]any {
	return map[string]any{
		"type":          "response.output_text.done",
		"id":            responseID,
		"response_id":   responseID,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
		"text":          text,
	}
}

func BuildResponsesReasoningTextDeltaPayload(responseID, itemID string, outputIndex, contentIndex int, delta string) map[string]any {
	return map[string]any{
		"type":          "response.reasoning_text.delta",
		"id":            responseID,
		"response_id":   responseID,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
		"delta":         delta,
	}
}

func BuildResponsesReasoningTextDonePayload(responseID, itemID string, outputIndex, contentIndex int, text string) map[string]any {
	return map[string]any{
		"type":          "response.reasoning_text.done",
		"id":            responseID,
		"response_id":   responseID,
		"item_id":       itemID,
		"output_index":  outputIndex,
		"content_index": contentIndex,
		"text":          text,
	}
}

func BuildResponsesFunctionCallArgumentsDeltaPayload(responseID, itemID string, outputIndex int, callID, delta string) map[string]any {
	return map[string]any{
		"type":         "response.function_call_arguments.delta",
		"id":           responseID,
		"response_id":  responseID,
		"item_id":      itemID,
		"output_index": outputIndex,
		"call_id":      callID,
		"delta":        delta,
	}
}

func BuildResponsesFunctionCallArgumentsDonePayload(responseID, itemID string, outputIndex int, callID, name, arguments string) map[string]any {
	return map[string]any{
		"type":         "response.function_call_arguments.done",
		"id":           responseID,
		"response_id":  responseID,
		"item_id":      itemID,
		"output_index": outputIndex,
		"call_id":      callID,
		"name":         name,
		"arguments":    normalizeJSONString(arguments),
	}
}

func BuildResponsesFailedPayload(responseID, model string, status int, message, code string) map[string]any {
	code = strings.TrimSpace(code)
	if code == "" {
		code = "api_error"
	}
	return map[string]any{
		"type":        "response.failed",
		"id":          responseID,
		"response_id": responseID,
		"object":      "response",
		"model":       model,
		"status":      "failed",
		"status_code": status,
		"error": map[string]any{
			"message": message,
			"type":    responsesErrorType(status),
			"code":    code,
			"param":   nil,
		},
	}
}

func responsesErrorType(status int) string {
	switch status {
	case 400, 404, 422:
		return "invalid_request_error"
	case 401:
		return "authentication_error"
	case 403:
		return "permission_error"
	case 429:
		return "rate_limit_error"
	case 503:
		return "service_unavailable_error"
	default:
		if status >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

func BuildResponsesCompletedPayload(response map[string]any) map[string]any {
	responseID, _ := response["id"].(string)
	return map[string]any{
		"type":        "response.completed",
		"response_id": responseID,
		"response":    response,
	}
}
