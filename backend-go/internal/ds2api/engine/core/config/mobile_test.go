package config

import "testing"

func TestNormalizeMobileForStorageChinaMainlandAddsPlus86(t *testing.T) {
	if got := NormalizeMobileForStorage("13800138000"); got != "+8613800138000" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeMobileForStorageChinaWithCountryCode(t *testing.T) {
	if got := NormalizeMobileForStorage("8613800138000"); got != "+8613800138000" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeMobileForStorageKeepsExistingCountryCode(t *testing.T) {
	if got := NormalizeMobileForStorage(" +1 (415) 555-2671 "); got != "+14155552671" {
		t.Fatalf("got %q", got)
	}
}

func TestCanonicalMobileKeyMatchesChinaAliases(t *testing.T) {
	a := CanonicalMobileKey("+8613800138000")
	b := CanonicalMobileKey("13800138000")
	c := CanonicalMobileKey("86 13800138000")
	if a == "" || a != b || b != c {
		t.Fatalf("alias mismatch: a=%q b=%q c=%q", a, b, c)
	}
}

func TestCanonicalMobileKeyEmptyForInvalidInput(t *testing.T) {
	if got := CanonicalMobileKey("() --"); got != "" {
		t.Fatalf("got %q", got)
	}
}
