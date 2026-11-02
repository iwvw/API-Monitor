package prompt

import "unicode/utf8"

// SplitByMaxRunes splits a finalized prompt into contiguous rune chunks where
// each segment's rune count does not exceed maxChars. It does not special-case
// role markers; markers are plain text in the DeepSeek web-chat prompt.
//
// The implementation walks UTF-8 runes directly on the input string instead of
// converting the whole prompt to []rune (which would allocate 4 bytes/rune for
// the entire 160k+ rune prompt and then copy each segment again). Byte-range
// slicing shares the original backing array, so extra memory is only the new
// segments themselves.
func SplitByMaxRunes(prompt string, maxChars int) []string {
	if maxChars <= 0 {
		return []string{prompt}
	}
	if utf8.RuneCountInString(prompt) <= maxChars {
		return []string{prompt}
	}

	segments := make([]string, 0, 2)
	runeCount := 0
	start := 0
	for i, r := range prompt {
		runeCount++
		if runeCount == maxChars {
			segments = append(segments, prompt[start:i+utf8.RuneLen(r)])
			start = i + utf8.RuneLen(r)
			runeCount = 0
		}
	}
	if start < len(prompt) {
		segments = append(segments, prompt[start:])
	}
	return segments
}

// SplitByRoleBoundary is kept for compatibility with older tests/helpers.
// New expert prompt segmentation should use SplitByMaxRunes.
func SplitByRoleBoundary(prompt string, maxChars int) []string {
	return SplitByMaxRunes(prompt, maxChars)
}
