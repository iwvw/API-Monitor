package claude

import (
	"fmt"
	"strings"
)

type claudeToolCallState struct {
	nameByID       map[string]string
	lastIDByName   map[string]string
	callIDSequence int
}

func (s *claudeToolCallState) nextID() string {
	s.callIDSequence++
	return fmt.Sprintf("call_claude_%d", s.callIDSequence)
}

func safeStringValue(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}
