package translatorcliproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

// OpenAIStreamTranslatorWriter translates OpenAI SSE output to another client format in real-time.
type OpenAIStreamTranslatorWriter struct {
	dst           http.ResponseWriter
	target        sdktranslator.Format
	model         string
	originalReq   []byte
	translatedReq []byte
	param         any
	statusCode    int
	headersSent   bool
	lineBuf       bytes.Buffer
}

func NewOpenAIStreamTranslatorWriter(dst http.ResponseWriter, target sdktranslator.Format, model string, originalReq, translatedReq []byte) *OpenAIStreamTranslatorWriter {
	return &OpenAIStreamTranslatorWriter{
		dst:           dst,
		target:        target,
		model:         model,
		originalReq:   originalReq,
		translatedReq: translatedReq,
		statusCode:    http.StatusOK,
	}
}

func (w *OpenAIStreamTranslatorWriter) Header() http.Header {
	return w.dst.Header()
}

func (w *OpenAIStreamTranslatorWriter) WriteHeader(statusCode int) {
	if w.headersSent {
		return
	}
	w.statusCode = statusCode
	w.headersSent = true
	w.dst.WriteHeader(statusCode)
}

func (w *OpenAIStreamTranslatorWriter) Write(p []byte) (int, error) {
	if !w.headersSent {
		w.WriteHeader(http.StatusOK)
	}
	if w.statusCode < 200 || w.statusCode >= 300 {
		return w.dst.Write(p)
	}
	w.lineBuf.Write(p)
	for {
		line, ok := w.readOneLine()
		if !ok {
			break
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if bytes.HasPrefix(trimmed, []byte(":")) {
			if _, err := w.dst.Write(trimmed); err != nil {
				return len(p), err
			}
			if _, err := w.dst.Write([]byte("\n\n")); err != nil {
				return len(p), err
			}
			if f, ok := w.dst.(http.Flusher); ok {
				f.Flush()
			}
			continue
		}
		if !bytes.HasPrefix(trimmed, []byte("data:")) {
			continue
		}
		usage, hasUsage := extractOpenAIUsage(trimmed)
		chunks := sdktranslator.TranslateStream(context.Background(), sdktranslator.FormatOpenAI, w.target, w.model, w.originalReq, w.translatedReq, trimmed, &w.param)
		if hasUsage {
			for i := range chunks {
				chunks[i] = injectStreamUsageMetadata(chunks[i], w.target, usage)
			}
		}
		for i := range chunks {
			if len(chunks[i]) == 0 {
				continue
			}
			if _, err := w.dst.Write(chunks[i]); err != nil {
				return len(p), err
			}
			if !bytes.HasSuffix(chunks[i], []byte("\n")) {
				if _, err := w.dst.Write([]byte("\n")); err != nil {
					return len(p), err
				}
			}
		}
		if f, ok := w.dst.(http.Flusher); ok {
			f.Flush()
		}
	}
	return len(p), nil
}

func (w *OpenAIStreamTranslatorWriter) Flush() {
	if f, ok := w.dst.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *OpenAIStreamTranslatorWriter) Unwrap() http.ResponseWriter {
	return w.dst
}

func (w *OpenAIStreamTranslatorWriter) readOneLine() ([]byte, bool) {
	b := w.lineBuf.Bytes()
	idx := bytes.IndexByte(b, '\n')
	if idx < 0 {
		return nil, false
	}
	line := append([]byte(nil), b[:idx]...)
	w.lineBuf.Next(idx + 1)
	return line, true
}

type openAIUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func extractOpenAIUsage(line []byte) (openAIUsage, bool) {
	raw := strings.TrimSpace(strings.TrimPrefix(string(line), "data:"))
	if raw == "" || raw == "[DONE]" {
		return openAIUsage{}, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return openAIUsage{}, false
	}
	usageObj, _ := payload["usage"].(map[string]any)
	if usageObj == nil {
		return openAIUsage{}, false
	}
	p := toInt(usageObj["prompt_tokens"])
	c := toInt(usageObj["completion_tokens"])
	t := toInt(usageObj["total_tokens"])
	if p <= 0 {
		p = toInt(usageObj["input_tokens"])
	}
	if c <= 0 {
		c = toInt(usageObj["output_tokens"])
	}
	if p <= 0 && c <= 0 && t <= 0 {
		return openAIUsage{}, false
	}
	if t <= 0 {
		t = p + c
	}
	return openAIUsage{PromptTokens: p, CompletionTokens: c, TotalTokens: t}, true
}

func injectStreamUsageMetadata(chunk []byte, target sdktranslator.Format, usage openAIUsage) []byte {
	if target != sdktranslator.FormatGemini {
		return chunk
	}
	suffix := ""
	switch {
	case bytes.HasSuffix(chunk, []byte("\n\n")):
		suffix = "\n\n"
	case bytes.HasSuffix(chunk, []byte("\n")):
		suffix = "\n"
	}
	text := strings.TrimSpace(string(chunk))
	if text == "" {
		return chunk
	}
	var (
		hasDataPrefix bool
		jsonText      = text
	)
	if strings.HasPrefix(jsonText, "data:") {
		hasDataPrefix = true
		jsonText = strings.TrimSpace(strings.TrimPrefix(jsonText, "data:"))
	}
	if jsonText == "" || jsonText == "[DONE]" {
		return chunk
	}
	obj := map[string]any{}
	if err := json.Unmarshal([]byte(jsonText), &obj); err != nil {
		return chunk
	}
	if _, ok := obj["candidates"]; !ok {
		return chunk
	}
	obj["usageMetadata"] = map[string]any{
		"promptTokenCount":     usage.PromptTokens,
		"candidatesTokenCount": usage.CompletionTokens,
		"totalTokenCount":      usage.TotalTokens,
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return chunk
	}
	if hasDataPrefix {
		return []byte("data: " + string(b) + suffix)
	}
	if suffix != "" {
		return append(b, []byte(suffix)...)
	}
	return b
}

func toInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}
