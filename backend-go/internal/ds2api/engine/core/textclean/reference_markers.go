package textclean

import "regexp"

var citationReferenceMarkerPattern = regexp.MustCompile(`(?i)\[(citation|reference):\s*\d+\]`)

func StripReferenceMarkers(text string) string {
	if text == "" {
		return text
	}
	return citationReferenceMarkerPattern.ReplaceAllString(text, "")
}

// StripReferenceMarkersEnabled returns the default for streaming surfaces,
// where partial citation/reference markers are hidden before the final
// link metadata is available.
func StripReferenceMarkersEnabled() bool {
	return true
}
