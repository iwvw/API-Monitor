package prompt

// SplitByMaxRunes splits a finalized prompt into contiguous rune chunks where
// each segment's rune count does not exceed maxChars. It does not special-case
// role markers; markers are plain text in the DeepSeek web-chat prompt.
func SplitByMaxRunes(prompt string, maxChars int) []string {
	runes := []rune(prompt)
	if maxChars <= 0 || len(runes) <= maxChars {
		return []string{prompt}
	}

	segments := make([]string, 0, (len(runes)+maxChars-1)/maxChars)
	for start := 0; start < len(runes); start += maxChars {
		end := start + maxChars
		if end > len(runes) {
			end = len(runes)
		}
		segments = append(segments, string(runes[start:end]))
	}
	return segments
}

// SplitByRoleBoundary is kept for compatibility with older tests/helpers.
// New expert prompt segmentation should use SplitByMaxRunes.
func SplitByRoleBoundary(prompt string, maxChars int) []string {
	return SplitByMaxRunes(prompt, maxChars)
}
