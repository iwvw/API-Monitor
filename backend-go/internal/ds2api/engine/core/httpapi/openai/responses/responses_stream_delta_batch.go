package responses

import (
	"strings"
)

type responsesDeltaBatch struct {
	runtime *responsesStreamRuntime
	kind    string
	text    strings.Builder
}

func (b *responsesDeltaBatch) append(kind, text string) {
	if text == "" {
		return
	}
	if b.kind != "" && b.kind != kind {
		b.flush()
	}
	b.kind = kind
	b.text.WriteString(text)
}

func (b *responsesDeltaBatch) flush() {
	if b.kind == "" || b.text.Len() == 0 {
		return
	}
	text := b.text.String()
	switch b.kind {
	case "reasoning":
		b.runtime.emitReasoningDelta(text)
	case "text":
		b.runtime.emitTextDelta(text)
	}
	b.kind = ""
	b.text.Reset()
}
