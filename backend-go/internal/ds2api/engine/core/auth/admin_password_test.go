package auth

import (
	"strings"
	"testing"
)

func TestGenerateAdminPassword_LengthAndCharset(t *testing.T) {
	p, err := GenerateAdminPassword()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(p) < 16 {
		t.Fatalf("password too short: %d", len(p))
	}
	// 只含安全字符集（字母数字，无易混淆字符）
	for _, r := range p {
		if !strings.ContainsRune(adminPasswordCharset, r) {
			t.Fatalf("unexpected char: %c", r)
		}
	}
}

func TestGenerateAdminPassword_Randomness(t *testing.T) {
	a, err := GenerateAdminPassword()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	b, err := GenerateAdminPassword()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if a == b {
		t.Fatalf("two generated passwords identical")
	}
}
