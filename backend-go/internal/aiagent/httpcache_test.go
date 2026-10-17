package aiagent

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestETagForIsStableAndContentSensitive(t *testing.T) {
	a := etagFor("k1", "2026-01-01T00:00:00Z", `{"v":1}`)
	b := etagFor("k1", "2026-01-01T00:00:00Z", `{"v":1}`)
	if a != b {
		t.Fatalf("same content must yield same etag: %q vs %q", a, b)
	}
	if etagFor("k1", "2026-01-01T00:00:00Z", `{"v":2}`) == a {
		t.Fatal("changed value must change etag")
	}
	if etagFor("k2", "2026-01-01T00:00:00Z", `{"v":1}`) == a {
		t.Fatal("changed key must change etag")
	}
	if etagFor("k1", "2026-01-02T00:00:00Z", `{"v":1}`) == a {
		t.Fatal("changed timestamp must change etag")
	}
	// 分隔符必须防拼接歧义："ab"+"c" 与 "a"+"bc" 不能撞。
	if etagFor("ab", "c") == etagFor("a", "bc") {
		t.Fatal("concatenation ambiguity: parts must be delimited")
	}
}

func TestIfNoneMatchMatches(t *testing.T) {
	const etag = `"abc123"`
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{etag, true},
		{"*", true},
		{`"other"`, false},
		{`"other", ` + etag, true},
		{`W/` + etag, true},
		{`"a", "b", W/"abc123"`, true},
		{`  "abc123"  `, true},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if tc.header != "" {
			req.Header.Set("If-None-Match", tc.header)
		}
		if got := ifNoneMatchMatches(req, etag); got != tc.want {
			t.Fatalf("ifNoneMatchMatches(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

func TestWriteNotModifiedSetsHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	writeNotModified(rec, `"etag"`)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rec.Code)
	}
	if got := rec.Header().Get("ETag"); got != `"etag"` {
		t.Fatalf("ETag = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("304 must have empty body, got %d bytes", rec.Body.Len())
	}
}
