package protocol

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestScanSSELinesHandlesLongSingleLine(t *testing.T) {
	payload := strings.Repeat("x", 2*1024*1024+4096)
	body := "data: {\"p\":\"response/content\",\"v\":\"" + payload + "\"}\n"
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}

	var got string
	err := ScanSSELines(resp, func(line []byte) bool {
		got = string(line)
		return true
	})
	if err != nil {
		t.Fatalf("ScanSSELines returned error: %v", err)
	}
	if !strings.Contains(got, payload) {
		t.Fatalf("long SSE line was not preserved: got len=%d want payload len=%d", len(got), len(payload))
	}
}
