package sse

import (
	"strings"
	"testing"
)

// This file freezes every real DeepSeek web SSE format variant observed in
// production captures (see the variant census). Each case asserts the parsed
// ContentPart type so a future upstream format change is caught immediately.

func parseChunk(t *testing.T, raw string, thinkingEnabled bool, currentType string) ([]ContentPart, string) {
	t.Helper()
	_, _, _, nextType := ParseSSEChunkForContentDetailed(mustChunk(t, raw), thinkingEnabled, currentType)
	parts, _, _, _ := ParseSSEChunkForContentDetailed(mustChunk(t, raw), thinkingEnabled, currentType)
	return parts, nextType
}

func mustChunk(t *testing.T, raw string) map[string]any {
	t.Helper()
	chunk, done, parsed := ParseDeepSeekSSELine([]byte("data: " + raw))
	if !parsed || done {
		t.Fatalf("bad chunk %q", raw)
	}
	return chunk
}

func typesOf(parts []ContentPart) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Type)
		b.WriteByte(',')
	}
	return b.String()
}

// V1: bare chunk {"v":"..."} follows the sticky cursor.
func TestVariantBareChunk(t *testing.T) {
	parts, _ := parseChunk(t, `{"v":"思考内容"}`, true, "thinking")
	if typesOf(parts) != "thinking," {
		t.Fatalf("thinking cursor bare chunk: %s", typesOf(parts))
	}
	parts, _ = parseChunk(t, `{"v":"正文内容"}`, true, "text")
	if typesOf(parts) != "text," {
		t.Fatalf("text cursor bare chunk: %s", typesOf(parts))
	}
}

// V2: absolute fragment content path follows the sticky cursor.
func TestVariantResponseFragmentsContent(t *testing.T) {
	parts, nt := parseChunk(t, `{"p":"response/fragments/-1/content","o":"APPEND","v":" body"}`, true, "text")
	if typesOf(parts) != "text," || nt != "text" {
		t.Fatalf("text cursor: %s next=%q", typesOf(parts), nt)
	}
	parts, nt = parseChunk(t, `{"p":"response/fragments/-1/content","o":"APPEND","v":" thought"}`, true, "thinking")
	if typesOf(parts) != "thinking," || nt != "thinking" {
		t.Fatalf("thinking cursor: %s next=%q", typesOf(parts), nt)
	}
}

// V3: relative "fragments/-1/content" inside a response BATCH follows cursor.
func TestVariantBatchRelativeFragmentsContent(t *testing.T) {
	raw := `{"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":340},{"p":"fragments/-1/content","o":"APPEND","v":" total order"},{"p":"quasi_status","o":"SET","v":"INCOMPLETE"}]}`
	parts, _ := parseChunk(t, raw, true, "thinking")
	if typesOf(parts) != "thinking," {
		t.Fatalf("expected thinking for BATCH relative content, got %s", typesOf(parts))
	}
}

// V4: nested fragments BATCH with "-1/content" APPEND (trailing body code like
// "pass\n```" must NOT be dropped).
func TestVariantNestedFragmentsBatchContent(t *testing.T) {
	raw := "{\"p\":\"response\",\"o\":\"BATCH\",\"v\":[{\"p\":\"accumulated_token_usage\",\"v\":2016},{\"p\":\"fragments\",\"o\":\"BATCH\",\"v\":[{\"p\":\"-1/content\",\"o\":\"APPEND\",\"v\":\"        pass\\n```\"},{\"p\":\"\",\"v\":[{\"id\":4,\"type\":\"TIP\",\"content\":\"提示\",\"style\":\"WARNING\",\"hide_on_wip\":true}]}]},{\"p\":\"quasi_status\",\"o\":\"SET\",\"v\":\"FINISHED\"}]}"
	parts, _ := parseChunk(t, raw, true, "text")
	got := typesOf(parts)
	if !strings.Contains(got, "text,") || !strings.Contains(got, "text") {
		t.Fatalf("expected trailing body text kept, got %s", got)
	}
	joined := ""
	for _, p := range parts {
		joined += p.Text
	}
	if !strings.Contains(joined, "pass\n```") {
		t.Fatalf("expected 'pass\\n```' in output, got %q", joined)
	}
}

// V5: nested "-1" BATCH with "content" APPEND follows cursor.
func TestVariantNestedDashOneBatchContent(t *testing.T) {
	raw := `{"p":"response/fragments","o":"BATCH","v":[{"p":"-1","o":"BATCH","v":[{"p":"content","o":"APPEND","v":"body-a"},{"p":"elapsed_secs","o":"SET","v":190.5}]},{"p":"","o":"APPEND","v":[{"id":3,"type":"RESPONSE","content":"下面是","references":[],"stage_id":1}]}]}`
	parts, nt := parseChunk(t, raw, true, "thinking")
	joined := ""
	for _, p := range parts {
		joined += p.Text
	}
	// The "-1" BATCH content belongs to the current (thinking) fragment; the
	// RESPONSE fragment flips to text.
	if !strings.Contains(joined, "body-a") {
		t.Fatalf("expected body-a kept, got %q", joined)
	}
	if nt != "text" {
		t.Fatalf("expected cursor to end on text after RESPONSE, got %q", nt)
	}
}

// V6: TIP fragments are skipped, not emitted as content.
func TestVariantTipFragmentSkipped(t *testing.T) {
	raw := `{"p":"response","o":"BATCH","v":[{"p":"fragments","o":"BATCH","v":[{"p":"","v":[{"id":4,"type":"TIP","content":"本回答由 AI 生成","style":"WARNING","hide_on_wip":true}]}]}]}`
	parts, _ := parseChunk(t, raw, true, "text")
	if len(parts) != 0 {
		t.Fatalf("TIP should be skipped, got %#v", parts)
	}
}

// V7: metadata paths never emit content.
func TestVariantMetadataSkipped(t *testing.T) {
	for _, raw := range []string{
		`{"p":"response/status","o":"SET","v":"FINISHED"}`,
		`{"p":"response/fragments/-1/elapsed_secs","o":"SET","v":571.06}`,
		`{"p":"quasi_status","o":"SET","v":"INCOMPLETE"}`,
		`{"p":"accumulated_token_usage","v":340}`,
	} {
		parts, _ := parseChunk(t, raw, true, "text")
		if len(parts) != 0 {
			t.Fatalf("metadata %s should emit nothing, got %#v", raw, parts)
		}
	}
}

// V8: THINK fragment moves the cursor to thinking only while the message is
// still reasoning; once the body has begun (cursor text) the one-size-fits-all
// rule drops the THINK wholesale and RESPONSE stays text.
func TestVariantThinkResponseFragments(t *testing.T) {
	raw := `{"p":"response/fragments","o":"APPEND","v":[{"type":"THINK","content":"deep thought"},{"type":"RESPONSE","content":"answer"}]}`
	parts, nt := parseChunk(t, raw, true, "text")
	if typesOf(parts) != "text," {
		t.Fatalf("expected dropped THINK + text, got %s", typesOf(parts))
	}
	if nt != "text" {
		t.Fatalf("expected next text, got %q", nt)
	}
}

// V9: response/thinking_content path.
func TestVariantThinkingContentPath(t *testing.T) {
	parts, _ := parseChunk(t, `{"p":"response/thinking_content","o":"APPEND","v":"deep"}`, true, "thinking")
	if typesOf(parts) != "thinking," {
		t.Fatalf("expected thinking, got %s", typesOf(parts))
	}
}

// V10: response envelope with fragments dict (THINK snapshot replay) after the
// body has begun (cursor text) is dropped wholesale by the one-size-fits-all
// rule: it neither rewinds the cursor nor surfaces as thinking.
func TestVariantEnvelopeThinkReplay(t *testing.T) {
	raw := `{"v":{"response":{"message_id":2,"fragments":[{"id":2,"type":"THINK","content":"We need answer."}]}}}`
	parts, nt := parseChunk(t, raw, true, "text")
	if typesOf(parts) != "" {
		t.Fatalf("expected replayed thinking dropped, got %s", typesOf(parts))
	}
	if nt != "text" {
		t.Fatalf("expected cursor to stay text after replay, got %q", nt)
	}
}
