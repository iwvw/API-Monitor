package shared

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var citationMarkerPattern = regexp.MustCompile(`(?i)\[(citation|reference):\s*(\d+)\]`)

func ReplaceCitationMarkersWithLinks(text string, links map[int]string) string {
	if strings.TrimSpace(text) == "" || len(links) == 0 {
		return text
	}
	zeroBasedReference := hasZeroBasedReferenceMarker(text)
	return citationMarkerPattern.ReplaceAllStringFunc(text, func(match string) string {
		sub := citationMarkerPattern.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		idx, err := strconv.Atoi(strings.TrimSpace(sub[2]))
		if err != nil || idx < 0 {
			return match
		}
		lookupIdx := idx
		if strings.EqualFold(sub[1], "reference") && zeroBasedReference {
			lookupIdx = idx + 1
		}
		url := strings.TrimSpace(links[lookupIdx])
		if url == "" {
			return match
		}
		return fmt.Sprintf("[%d](%s)", idx, url)
	})
}

func hasZeroBasedReferenceMarker(text string) bool {
	for _, sub := range citationMarkerPattern.FindAllStringSubmatch(text, -1) {
		if len(sub) < 3 || !strings.EqualFold(sub[1], "reference") {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(sub[2]))
		if err == nil && idx == 0 {
			return true
		}
	}
	return false
}
