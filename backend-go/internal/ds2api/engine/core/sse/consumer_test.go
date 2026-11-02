package sse

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCollectStreamDedupesContinueSnapshotReplay(t *testing.T) {
	prefix := "我们被问到：这是一个很长的续答快照前缀，用来验证去重逻辑不会误伤正常 token。"
	body := strings.Join([]string{
		`data: {"v":{"response":{"fragments":[{"id":2,"type":"THINK","content":"` + prefix + `","references":[],"stage_id":1}]}}}`,
		``,
		`data: {"p":"response/status","v":"INCOMPLETE"}`,
		``,
		`data: {"v":{"response":{"fragments":[{"id":2,"type":"THINK","content":"` + prefix + `继续","references":[],"stage_id":1}]}}}`,
		``,
		`data: {"v":"分析"}`,
		``,
		`data: {"p":"response/status","v":"FINISHED"}`,
		``,
	}, "\n")

	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	got := CollectStream(resp, true, true)
	if got.Thinking != prefix+"继续分析" {
		t.Fatalf("unexpected thinking after dedupe: %q", got.Thinking)
	}
}
