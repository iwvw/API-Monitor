package files

import "testing"

func TestIsTextFile_Extensions(t *testing.T) {
	cases := []struct {
		filename string
		want     bool
	}{
		{"notes.txt", true},
		{"README.md", true},
		{"data.csv", true},
		{"config.json", true},
		{"main.py", true},
		{"main.go", true},
		{"main.c", true},
		{"main.cpp", true},
		{"index.html", true},
		{"style.css", true},
		{"app.ts", true},
		{"app.tsx", true},
		{"archive.zip", false},
		{"photo.png", false},
		{"doc.pdf", false},
		{"binary.exe", false},
		{"unknown", false},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			if got := IsTextFile(tc.filename, "", nil); got != tc.want {
				t.Errorf("IsTextFile(%q, \"\", nil) = %v, want %v", tc.filename, got, tc.want)
			}
		})
	}
}

func TestIsTextFile_MimeTypes(t *testing.T) {
	cases := []struct {
		filename    string
		contentType string
		want        bool
	}{
		{"", "text/plain", true},
		{"", "text/markdown", true},
		{"", "application/json", true},
		{"", "application/xml", true},
		{"", "application/javascript", true},
		{"", "application/x-yaml", true},
		{"", "application/x-www-form-urlencoded", true},
		{"", "application/pdf", false},
		{"", "image/png", false},
		{"", "application/octet-stream", false},
		{"unknown", "text/plain; charset=utf-8", true},
		{"unknown", "application/json; charset=utf-8", true},
	}
	for _, tc := range cases {
		t.Run(tc.contentType, func(t *testing.T) {
			if got := IsTextFile(tc.filename, tc.contentType, nil); got != tc.want {
				t.Errorf("IsTextFile(%q, %q, nil) = %v, want %v", tc.filename, tc.contentType, got, tc.want)
			}
		})
	}
}

func TestIsTextFile_AllowedExtensionsOverride(t *testing.T) {
	allowed := ExtensionSet([]string{"custom", ".foo"})
	if !IsTextFile("x.custom", "", allowed) {
		t.Errorf("expected custom extension to be allowed")
	}
	if !IsTextFile("x.foo", "", allowed) {
		t.Errorf("expected foo extension to be allowed")
	}
	if IsTextFile("x.txt", "", allowed) {
		t.Errorf("expected default extension to be rejected when override is provided")
	}
	if IsTextFile("x", "text/plain", allowed) {
		t.Errorf("expected MIME fallback to be disabled when override is provided")
	}
}

func TestExtensionSet(t *testing.T) {
	set := ExtensionSet([]string{"txt", " .md ", "", "XML"})
	if len(set) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(set))
	}
	for _, ext := range []string{".txt", ".md", ".xml"} {
		if _, ok := set[ext]; !ok {
			t.Errorf("expected %q in set", ext)
		}
	}
}
