package version

import "testing"

func TestNormalizeAndTag(t *testing.T) {
	if got := normalize("v2.3.5"); got != "2.3.5" {
		t.Fatalf("normalize failed: %q", got)
	}
	if got := Tag("2.3.5"); got != "v2.3.5" {
		t.Fatalf("tag failed: %q", got)
	}
}

func TestCompare(t *testing.T) {
	if Compare("2.3.5", "2.3.5") != 0 {
		t.Fatal("expected equal")
	}
	if Compare("2.3.5", "2.3.6") >= 0 {
		t.Fatal("expected less")
	}
	if Compare("v2.10.0", "2.3.9") <= 0 {
		t.Fatal("expected greater")
	}
}

func TestTagKeepsPreviewStyle(t *testing.T) {
	if got := Tag("preview-dev.abcd123"); got != "preview-dev.abcd123" {
		t.Fatalf("expected preview tag unchanged, got %q", got)
	}
}

func TestVersionFromVercelEnv(t *testing.T) {
	t.Setenv("VERCEL_GIT_COMMIT_TAG", "")
	t.Setenv("VERCEL_GIT_COMMIT_REF", "dev")
	t.Setenv("VERCEL_GIT_COMMIT_SHA", "abcdef123456")
	if got := versionFromVercelEnv(); got != "preview-dev.abcdef1" {
		t.Fatalf("unexpected vercel preview version: %q", got)
	}
}
