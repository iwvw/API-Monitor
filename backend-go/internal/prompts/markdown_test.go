package prompts

import (
	"strings"
	"testing"
)

func TestGenerateSlug(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{"english lowercased", "Hello World", "hello-world"},
		{"punctuation replaced", "Hello, World! (API)", "hello-world-api"},
		{"cjk preserved", "你好 世界", "你好-世界"},
		{"mixed", "API 监控系统 V2", "api-监控系统-v2"},
		{"trimmed dashes", "-foo-bar-", "foo-bar"},
		{"empty", "", "untitled"},
		{"only separators", "!!!   ???", "untitled"},
		{"spaces trimmed", "   spaced   ", "spaced"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := GenerateSlug(tc.title); got != tc.want {
				t.Fatalf("GenerateSlug(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestGeneratePublicID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := GeneratePublicID()
		if len(id) != 10 {
			t.Fatalf("GeneratePublicID length = %d, want 10", len(id))
		}
		for _, r := range id {
			if !strings.ContainsRune("0123456789abcdef", r) {
				t.Fatalf("GeneratePublicID %q contains non-hex char %q", id, r)
			}
		}
		if seen[id] {
			t.Fatalf("GeneratePublicID duplicated: %q", id)
		}
		seen[id] = true
	}
}

func TestStripMarkdown(t *testing.T) {
	cases := []struct {
		name string
		md   string
		want string
	}{
		{"headers", "# Title\n## Sub", "Title\nSub"},
		{"bold italic", "**加粗**和*斜体*", "加粗和斜体"},
		{"links", "[链接](https://example.com)", "链接"},
		{"images", "![图](https://example.com/a.png)", "!图"},
		{"code block", "```go\ncode here\n```", ""},
		{"inline code", "run `go test` now", "run go test now"},
		{"unordered list", "- item a\n* item b", "item a\nitem b"},
		{"ordered list", "1. one\n2. two", "one\ntwo"},
		{"quote", "> 引用内容", "引用内容"},
		{"horizontal rule", "text\n---\nmore", "text\n\nmore"},
		{"plain text unchanged", "hello world", "hello world"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StripMarkdown(tc.md); got != tc.want {
				t.Fatalf("StripMarkdown(%q) = %q, want %q", tc.md, got, tc.want)
			}
		})
	}
}

func TestExtractExcerpt(t *testing.T) {
	cases := []struct {
		name     string
		md       string
		maxChars int
		want     string
	}{
		{"shorter than limit", "hello", 10, "hello"},
		{"longer than limit", "1234567890", 5, "12345..."},
		{"exact boundary", "12345", 5, "12345"},
		{"markdown stripped first", "# hello world", 5, "hello..."},
		{"empty", "", 10, ""},
		{"zero limit", "abc", 0, "..."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractExcerpt(tc.md, tc.maxChars); got != tc.want {
				t.Fatalf("ExtractExcerpt(%q, %d) = %q, want %q", tc.md, tc.maxChars, got, tc.want)
			}
		})
	}
}

func TestExtractOutline(t *testing.T) {
	outline := ExtractOutline("# 一级\n## 二级\n### 三级\n#  一级带空格  \nplain text")
	if len(outline) != 4 {
		t.Fatalf("outline length = %d, want 4: %#v", len(outline), outline)
	}
	expected := []Heading{
		{Level: 1, Text: "一级"},
		{Level: 2, Text: "二级"},
		{Level: 3, Text: "三级"},
		{Level: 1, Text: "一级带空格"},
	}
	for i, h := range expected {
		if outline[i] != h {
			t.Fatalf("outline[%d] = %#v, want %#v", i, outline[i], h)
		}
	}
	if got := ExtractOutline("no headings here"); len(got) != 0 {
		t.Fatalf("expected empty outline, got %#v", got)
	}
}

func TestExtractVariables(t *testing.T) {
	variables := ExtractVariables("你好 {{name}}，默认 {{role: 管理员}} 结束")
	if len(variables) != 2 {
		t.Fatalf("variables length = %d, want 2: %#v", len(variables), variables)
	}
	if variables[0].Name != "name" || variables[0].DefaultValue != "" {
		t.Fatalf("variables[0] = %#v", variables[0])
	}
	if variables[1].Name != "role" || variables[1].DefaultValue != "管理员" {
		t.Fatalf("variables[1] = %#v", variables[1])
	}

	deduped := ExtractVariables("{{a}}{{b}}{{a: 有默认}}")
	if len(deduped) != 2 || deduped[0].Name != "a" || deduped[0].DefaultValue != "" {
		t.Fatalf("dedupe keeps first occurrence: %#v", deduped)
	}

	if got := ExtractVariables("没有变量"); len(got) != 0 {
		t.Fatalf("expected no variables, got %#v", got)
	}
	if got := ExtractVariables("{{中文变量}}"); len(got) != 0 {
		t.Fatalf("non-ascii names are not matched: %#v", got)
	}
}

func TestComputeChecksum(t *testing.T) {
	a := ComputeChecksum("content")
	b := ComputeChecksum("content")
	c := ComputeChecksum("other")
	if a != b {
		t.Fatalf("checksum not deterministic: %q vs %q", a, b)
	}
	if a == c {
		t.Fatalf("different content produced same checksum %q", a)
	}
	if len(a) != 64 {
		t.Fatalf("checksum length = %d, want 64", len(a))
	}
	if ComputeChecksum("abc") != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("sha256 of abc mismatch: %q", ComputeChecksum("abc"))
	}
}

func TestCountWords(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"basic", "hello world", 2},
		{"empty", "", 0},
		{"extra whitespace", "  a   b  c ", 3},
		{"cjk space separated", "你好 世界", 2},
		{"cjk no space", "你好世界", 1},
		{"newlines", "a\nb\nc", 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CountWords(tc.text); got != tc.want {
				t.Fatalf("CountWords(%q) = %d, want %d", tc.text, got, tc.want)
			}
		})
	}
}