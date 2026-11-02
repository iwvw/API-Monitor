package util

import "unicode/utf8"

// TruncateRunes trims a string to at most limit Unicode code points.
func TruncateRunes(text string, limit int) (string, bool) {
	if limit < 0 {
		return text, false
	}
	if limit == 0 {
		return "", text != ""
	}

	count := 0
	for i := range text {
		if count == limit {
			return text[:i], true
		}
		count++
	}
	return text, false
}

// TruncateUTF8Bytes trims a string to fit within limit bytes without cutting
// through a UTF-8 code point boundary.
func TruncateUTF8Bytes(text string, limit int) (string, bool) {
	if limit < 0 {
		return text, false
	}
	if len(text) <= limit {
		return text, false
	}
	if limit == 0 {
		return "", true
	}

	raw := []byte(text)
	cut := limit
	if cut > len(raw) {
		cut = len(raw)
	}
	for cut > 0 && cut < len(raw) && !utf8.RuneStart(raw[cut]) {
		cut--
	}
	return string(raw[:cut]), true
}
