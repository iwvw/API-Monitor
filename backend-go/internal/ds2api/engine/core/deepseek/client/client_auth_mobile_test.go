package client

import "testing"

func TestNormalizeMobileForLogin_ChinaWithPlus86(t *testing.T) {
	mobile, areaCode := normalizeMobileForLogin("+8613800138000")
	if mobile != "13800138000" {
		t.Fatalf("unexpected mobile: %q", mobile)
	}
	if areaCode != "+86" {
		t.Fatalf("expected +86 areaCode, got %q", areaCode)
	}
}

func TestNormalizeMobileForLogin_ChinaWith86Prefix(t *testing.T) {
	mobile, areaCode := normalizeMobileForLogin("8613800138000")
	if mobile != "13800138000" {
		t.Fatalf("unexpected mobile: %q", mobile)
	}
	if areaCode != "+86" {
		t.Fatalf("expected +86 areaCode, got %q", areaCode)
	}
}

func TestNormalizeMobileForLogin_KeepPlainDigits(t *testing.T) {
	mobile, areaCode := normalizeMobileForLogin("13800138000")
	if mobile != "13800138000" {
		t.Fatalf("unexpected mobile: %q", mobile)
	}
	if areaCode != "+86" {
		t.Fatalf("expected +86 areaCode, got %q", areaCode)
	}
}
