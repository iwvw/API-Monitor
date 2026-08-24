package drawio

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
)

func TestParsePageInfo(t *testing.T) {
	xml := `<mxfile><diagram id="p1" name="首页"/><diagram id="p2" name="架构"/></mxfile>`
	pages, coverID, coverName := ParsePageInfo(xml)
	if len(pages) != 2 {
		t.Fatalf("pages = %#v, want 2", pages)
	}
	if pages[0] != (PageInfo{ID: "p1", Name: "首页"}) || pages[1] != (PageInfo{ID: "p2", Name: "架构"}) {
		t.Fatalf("pages = %#v", pages)
	}
	if coverID != "p1" || coverName != "首页" {
		t.Fatalf("cover = %q/%q, want p1/首页", coverID, coverName)
	}

	pages, coverID, coverName = ParsePageInfo("not xml at all")
	if len(pages) != 0 || coverID != "" || coverName != "" {
		t.Fatalf("invalid xml: pages=%#v cover=%q/%q", pages, coverID, coverName)
	}

	pages, _, _ = ParsePageInfo(`<mxfile></mxfile>`)
	if len(pages) != 0 {
		t.Fatalf("no diagram pages = %#v", pages)
	}
}

func TestComputeXMLHash(t *testing.T) {
	a := ComputeXMLHash("<mxfile/>")
	if a != ComputeXMLHash("<mxfile/>") {
		t.Fatal("hash not deterministic")
	}
	if a == ComputeXMLHash("<mxfile/> ") {
		t.Fatal("different content produced same hash")
	}
	if len(a) != 64 {
		t.Fatalf("hash length = %d, want 64", len(a))
	}
}

func TestNormalizeXML(t *testing.T) {
	plain := `<mxfile><diagram id="p1" name="Page-1"><mxGraphModel><root></root></mxGraphModel></diagram></mxfile>`
	normalized, err := NormalizeXML(plain)
	if err != nil || normalized != plain {
		t.Fatalf("plain normalize = %q err=%v", normalized, err)
	}

	withDecl := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n" + plain
	normalized, err = NormalizeXML(withDecl)
	if err != nil || normalized != plain {
		t.Fatalf("xml declaration stripped = %q err=%v", normalized, err)
	}

	bom := "\ufeff" + plain
	normalized, err = NormalizeXML(bom)
	if err != nil || normalized != plain {
		t.Fatalf("bom stripped = %q err=%v", normalized, err)
	}

	page := `<mxGraphModel><root><mxCell id="0"/></root></mxGraphModel>`
	wrapped, err := NormalizeXML(page)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(wrapped, "<mxfile") || !strings.Contains(wrapped, "<diagram id=\"page-1\" name=\"Page-1\">") || !strings.Contains(wrapped, page) {
		t.Fatalf("single model wrapped = %q", wrapped)
	}

	if _, err := NormalizeXML("plain text without xml"); err == nil {
		t.Fatal("expected error for unrecognized format")
	}
	if _, err := NormalizeXML("<mxfile>"); err == nil {
		t.Fatal("expected error for invalid mxfile xml")
	}
	if _, err := NormalizeXML("<mxfile><diagram id=\"p1\">!!!not-base64!!!</diagram></mxfile>"); err == nil {
		t.Fatal("expected error for invalid compressed payload")
	}
}

func TestNormalizeXMLCompressedBodies(t *testing.T) {
	page := `<mxGraphModel><root><mxCell id="0"/></root></mxGraphModel>`
	var compressed bytes.Buffer
	w, _ := flate.NewWriter(&compressed, flate.DefaultCompression)
	w.Write([]byte(url.QueryEscape(page)))
	w.Close()
	raw := `<mxfile><diagram id="p1" name="Page-1">` + base64.StdEncoding.EncodeToString(compressed.Bytes()) + `</diagram></mxfile>`

	normalized, err := NormalizeXML(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normalized, "<mxGraphModel") || !strings.Contains(normalized, "Page-1") {
		t.Fatalf("compressed body not inflated: %q", normalized)
	}
}

func TestDefaultBlankMXFile(t *testing.T) {
	blank := DefaultBlankMXFile()
	for _, marker := range []string{"<mxfile", "host=\"api-monitor\"", `<diagram id="page-1" name="Page-1">`, "<mxGraphModel", "<mxCell id=\"0\""} {
		if !strings.Contains(blank, marker) {
			t.Fatalf("blank file missing %q: %s", marker, blank)
		}
	}
}

func TestExtractExternalAssets(t *testing.T) {
	xml := `<mxfile>
  <diagram>
    <mxCell style="image;image=https://cdn.example.com/a.png" value="" />
    <mxCell style="image;image=http://cdn.example.com/b.jpg" value="" />
    <mxCell style="image;image=https://cdn.example.com/a.png" value="" />
    <mxCell style="image;image=https://cdn.example.com/c.svg&x=1" value="" />
  </diagram>
</mxfile>`
	assets := ExtractExternalAssets(xml)
	if len(assets) != 3 {
		t.Fatalf("assets = %#v, want 3 (dedupe)", assets)
	}
	if assets[0].URL != "https://cdn.example.com/a.png" || assets[0].Domain != "cdn.example.com" || assets[0].AssetType != "image" {
		t.Fatalf("assets[0] = %#v", assets[0])
	}
	if assets[1].URL != "http://cdn.example.com/b.jpg" || assets[1].Domain != "cdn.example.com" {
		t.Fatalf("assets[1] = %#v", assets[1])
	}
	if assets[2].URL != "https://cdn.example.com/c.svg" {
		t.Fatalf("url stops at & : %#v", assets[2])
	}

	if got := ExtractExternalAssets("no urls here"); len(got) != 0 {
		t.Fatalf("no urls = %#v", got)
	}
}

func TestExtractExternalAssetsDomainWithPort(t *testing.T) {
	assets := ExtractExternalAssets(`<mxCell style="image;image=https://cdn.example.com:8443/x.png" />`)
	if len(assets) != 1 || assets[0].Domain != "cdn.example.com:8443" {
		t.Fatalf("assets = %#v", assets)
	}
}

func TestIsPrivateNetworkURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"localhost", "http://localhost", true},
		{"localhost path", "http://localhost:8080/admin", true},
		{"loopback ipv4", "https://127.0.0.1", true},
		{"loopback other", "http://127.0.0.2/x", true},
		{"ipv4 mapped loopback", "http://[::ffff:127.0.0.1]", true},
		{"ipv6 loopback", "http://[::1]", true},
		{"ipv6 loopback port", "https://[::1]:8443/x", true},
		{"unspecified", "http://0.0.0.0", true},
		{"private 10", "http://10.0.0.5", true},
		{"private 172 low", "http://172.16.0.1", true},
		{"private 172 high", "http://172.31.255.255", true},
		{"private 192.168", "http://192.168.1.1", true},
		{"link local ipv4", "http://169.254.169.254", true},
		{"cloud metadata", "https://169.254.169.254/latest/meta-data/", true},
		{"link local ipv6", "http://[fe80::1]", true},
		{"no scheme", "example.com", true},
		{"unsupported scheme", "ftp://example.com", true},
		{"no host", "http://", true},
		{"space in host", "http://exa mple.com", true},

		{"public hostname", "https://example.com", false},
		{"public hostname path", "https://example.com/path?q=1", false},
		{"public hostname port", "http://cdn.example.com:8080/x", false},
		{"public ipv4", "http://8.8.8.8", false},
		{"public ipv4 path", "https://1.1.1.1/path?q=1", false},
		{"public ipv6", "https://[2606:4700:4700::1111]", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPrivateNetworkURL(tc.url); got != tc.want {
				t.Fatalf("IsPrivateNetworkURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}