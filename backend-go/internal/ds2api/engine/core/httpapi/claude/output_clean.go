package claude

import textclean "github.com/iwvw/api-monitor/backend-go/internal/ds2api/engine/core/textclean"

func cleanVisibleOutput(text string, stripReferenceMarkers bool) string {
	if text == "" {
		return text
	}
	if stripReferenceMarkers {
		text = textclean.StripReferenceMarkers(text)
	}
	return text
}
