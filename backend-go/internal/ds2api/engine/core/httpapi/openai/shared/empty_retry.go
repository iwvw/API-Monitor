package shared

import "strings"

const EmptyOutputRetrySuffix = "Previous reply had no visible output. Please regenerate the visible final answer or tool call now."

func EmptyOutputRetryEnabled() bool {
	return true
}

// EmptyOutputRetryMaxAttempts bounds how many times a stream that died without
// a resumable outcome is retried. It also caps the continue-resume rounds for
// a stream interrupted after partial output (see completionruntime
// ExecuteStreamWithRetry): the budget counts every interrupted round, so a
// value of 1 means one interrupted stream gets one continue attempt and a
// second interruption surfaces a hard 502. Upstream chat.deepseek.com streams
// occasionally die mid-body under heavy contexts (large tool outputs), so the
// budget is 3 to let the same message be resumed a couple of times before
// giving up. Each continue round is a fresh upstream request, so this only
// costs latency on already-failing streams, never on the happy path.
func EmptyOutputRetryMaxAttempts() int {
	return 3
}

func ClonePayloadWithEmptyOutputRetryPrompt(payload map[string]any) map[string]any {
	return ClonePayloadForEmptyOutputRetry(payload, 0)
}

// ClonePayloadForEmptyOutputRetry creates a retry payload with the suffix
// appended and, if parentMessageID > 0, sets parent_message_id so the
// retry is submitted as a proper follow-up turn in the same DeepSeek
// session rather than a disconnected root message.
func ClonePayloadForEmptyOutputRetry(payload map[string]any, parentMessageID int) map[string]any {
	clone := make(map[string]any, len(payload))
	for k, v := range payload {
		clone[k] = v
	}
	original, _ := payload["prompt"].(string)
	clone["prompt"] = AppendEmptyOutputRetrySuffix(original)
	if parentMessageID > 0 {
		clone["parent_message_id"] = parentMessageID
	}
	return clone
}

func AppendEmptyOutputRetrySuffix(prompt string) string {
	prompt = strings.TrimRight(prompt, "\r\n\t ")
	if prompt == "" {
		return EmptyOutputRetrySuffix
	}
	return prompt + "\n\n" + EmptyOutputRetrySuffix
}

func UsagePromptWithEmptyOutputRetry(originalPrompt string, retryAttempts int) string {
	if retryAttempts <= 0 {
		return originalPrompt
	}
	parts := make([]string, 0, retryAttempts+1)
	parts = append(parts, originalPrompt)
	next := originalPrompt
	for i := 0; i < retryAttempts; i++ {
		next = AppendEmptyOutputRetrySuffix(next)
		parts = append(parts, next)
	}
	return strings.Join(parts, "\n")
}
