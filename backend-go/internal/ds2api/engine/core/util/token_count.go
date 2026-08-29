package util

const (
	defaultTokenizerModel = "gpt-4o"
	claudeTokenizerModel  = "claude"
)

func CountPromptTokens(text, model string) int {
	base := maxTokenCount(
		EstimateTokens(text),
		countWithTokenizer(text, model),
	)
	if base <= 0 {
		return 0
	}
	return base + conservativePromptPadding(base)
}

func CountOutputTokens(text, model string) int {
	base := maxTokenCount(
		EstimateTokens(text),
		countWithTokenizer(text, model),
	)
	if base <= 0 {
		return 0
	}
	return base
}

func conservativePromptPadding(base int) int {
	padding := base / 50
	if padding < 4 {
		padding = 4
	}
	return padding
}

func maxTokenCount(values ...int) int {
	best := 0
	for _, v := range values {
		if v > best {
			best = v
		}
	}
	return best
}
