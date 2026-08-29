package openai

import "testing"

func TestReplaceCitationMarkersWithLinks(t *testing.T) {
	raw := "这是一条更新[citation:1]，更多信息见[citation:2]。"
	links := map[int]string{
		1: "https://example.com/news-1",
		2: "https://example.com/news-2",
	}

	got := replaceCitationMarkersWithLinks(raw, links)
	want := "这是一条更新[1](https://example.com/news-1)，更多信息见[2](https://example.com/news-2)。"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReplaceCitationMarkersWithLinksKeepsUnknownIndex(t *testing.T) {
	raw := "只有一个来源[citation:1]，未知来源[citation:3]。"
	links := map[int]string{1: "https://example.com/a"}

	got := replaceCitationMarkersWithLinks(raw, links)
	want := "只有一个来源[1](https://example.com/a)，未知来源[citation:3]。"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReplaceCitationMarkersWithLinksSupportsReferenceMarker(t *testing.T) {
	raw := "新闻摘要[reference:1]，详情[reference:2]。"
	links := map[int]string{
		1: "https://example.com/r1",
		2: "https://example.com/r2",
	}

	got := replaceCitationMarkersWithLinks(raw, links)
	want := "新闻摘要[1](https://example.com/r1)，详情[2](https://example.com/r2)。"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReplaceCitationMarkersWithLinksSupportsReferenceZeroBased(t *testing.T) {
	raw := "来源[reference:0] 与 [reference:1]。"
	links := map[int]string{
		1: "https://example.com/first",
		2: "https://example.com/second",
	}

	got := replaceCitationMarkersWithLinks(raw, links)
	want := "来源[0](https://example.com/first) 与 [1](https://example.com/second)。"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReplaceCitationMarkersWithLinksKeepsCitationOneBasedWithZeroBasedReference(t *testing.T) {
	raw := "引用[citation:1]，来源[reference:0]，后续[reference:1]。"
	links := map[int]string{
		1: "https://example.com/first",
		2: "https://example.com/second",
	}

	got := replaceCitationMarkersWithLinks(raw, links)
	want := "引用[1](https://example.com/first)，来源[0](https://example.com/first)，后续[1](https://example.com/second)。"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestReplaceCitationMarkersWithLinksDetectsSpacedReferenceZeroBased(t *testing.T) {
	raw := "来源[reference: 0] 与 [reference: 1]。"
	links := map[int]string{
		1: "https://example.com/first",
		2: "https://example.com/second",
	}

	got := replaceCitationMarkersWithLinks(raw, links)
	want := "来源[0](https://example.com/first) 与 [1](https://example.com/second)。"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
