package secure

import (
	"log"
	"strings"
	"sync"
	"testing"
)

func TestIsEncrypted(t *testing.T) {
	valid := "00112233445566778899aabbccddeeff:00112233445566778899aabbccddeeff:00ff"
	if !IsEncrypted(valid) {
		t.Fatalf("IsEncrypted(%q) = false, want true", valid)
	}
	invalid := map[string]bool{
		"":                                    false,
		"plaintext":                           false,
		"abc:def:ghi":                         false,
		"zz:00:00":                            false,
		"00:00":                               false,
		"00:00:00:00":                         false,
		"00:00:00:":                           false,
		"00:00:00  ":                          false,
		"00112233445566778899aabbccddeeff:00": false,
	}
	for value, want := range invalid {
		if got := IsEncrypted(value); got != want {
			t.Errorf("IsEncrypted(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "unit-test-key-0123456789abcdef")
	secret := "super-secret-api-token"
	encrypted, err := SecureEncrypt(secret)
	if err != nil {
		t.Fatalf("SecureEncrypt failed: %v", err)
	}
	if encrypted == secret {
		t.Fatal("SecureEncrypt returned plaintext")
	}
	if !IsEncrypted(encrypted) {
		t.Fatalf("encrypted value %q not recognized as encrypted", encrypted)
	}
	got := SecureDecrypt(encrypted)
	if got != secret {
		t.Fatalf("SecureDecrypt got %q, want %q", got, secret)
	}
}

func TestSecureEncryptIdempotentForEmptyAndAlreadyEncrypted(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "unit-test-key-0123456789abcdef")

	if got, err := SecureEncrypt(""); err != nil || got != "" {
		t.Fatalf("SecureEncrypt(\"\") = %q, %v; want \"\", nil", got, err)
	}

	encrypted, err := SecureEncrypt("already-encrypted-value")
	if err != nil {
		t.Fatal(err)
	}
	again, err := SecureEncrypt(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if again != encrypted {
		t.Fatalf("SecureEncrypt on encrypted value changed it: got %q want %q", again, encrypted)
	}
}

func TestSecureEncryptProducesUniqueCiphertext(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "unit-test-key-0123456789abcdef")
	a, err := SecureEncrypt("same-value")
	if err != nil {
		t.Fatal(err)
	}
	b, err := SecureEncrypt("same-value")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two encryptions of the same value produced identical ciphertext (IV reuse?)")
	}
	if SecureDecrypt(a) != SecureDecrypt(b) {
		t.Fatal("encryptions of the same value did not decrypt to the same plaintext")
	}
}

func TestSecureDecryptNonEncryptedPassthrough(t *testing.T) {
	if got := SecureDecrypt("plaintext"); got != "plaintext" {
		t.Fatalf("SecureDecrypt(plaintext) = %q, want passthrough", got)
	}
	if got := SecureDecrypt(""); got != "" {
		t.Fatalf("SecureDecrypt(\"\") = %q, want \"\"", got)
	}
}

func TestSecureDecryptInvalidEncryptedValue(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "unit-test-key-0123456789abcdef")
	// Proper format but garbage hex that will fail to decrypt.
	invalid := "00000000000000000000000000000000:00000000000000000000000000000000:0000000000000000000000000000000000000000"
	if got := SecureDecrypt(invalid); got != "" {
		t.Fatalf("SecureDecrypt(invalid) = %q, want empty string on decryption failure", got)
	}
}

func TestEncryptJSONRoundTrip(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "unit-test-key-0123456789abcdef")
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	input := payload{Name: "test", Count: 42}
	encrypted, err := EncryptJSON(input)
	if err != nil {
		t.Fatalf("EncryptJSON failed: %v", err)
	}
	var out payload
	if err := DecryptJSON(encrypted, &out); err != nil {
		t.Fatalf("DecryptJSON failed: %v", err)
	}
	if out != input {
		t.Fatalf("DecryptJSON got %+v, want %+v", out, input)
	}
}

func TestDecryptJSONInvalidInput(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "unit-test-key-0123456789abcdef")
	var out map[string]interface{}
	// Plain text that isn't a valid JSON object.
	if err := DecryptJSON("not-json", &out); err == nil {
		t.Fatal("DecryptJSON with non-JSON plaintext should error")
	}
	// Malformed encrypted format.
	if err := DecryptJSON("bad:format", &out); err == nil {
		t.Fatal("DecryptJSON with malformed encrypted value should error")
	}
}

func TestNodeGCMRoundTrip(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "unit-test-key-0123456789abcdef")
	plain := "node-gcm-secret"
	encrypted, err := EncryptNodeGCM(plain)
	if err != nil {
		t.Fatalf("EncryptNodeGCM failed: %v", err)
	}
	decrypted, err := DecryptNodeGCM(encrypted)
	if err != nil {
		t.Fatalf("DecryptNodeGCM failed: %v", err)
	}
	if decrypted != plain {
		t.Fatalf("DecryptNodeGCM got %q, want %q", decrypted, plain)
	}
}

func TestDecryptNodeGCMRejectsMalformed(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "unit-test-key-0123456789abcdef")
	bad := map[string]string{
		"no-colons":   "too few parts",
		"00:00:00:00": "too many parts",
		"zz:00:00":    "invalid iv hex",
		"00:zz:00":    "invalid auth tag hex",
		"00:00:zz":    "invalid ciphertext hex",
	}
	for value, desc := range bad {
		if _, err := DecryptNodeGCM(value); err == nil {
			t.Errorf("DecryptNodeGCM(%q) (%s) should error", value, desc)
		}
	}
}

func TestDecryptNodeGCMTamperDetection(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "unit-test-key-0123456789abcdef")
	encrypted, err := EncryptNodeGCM("tamper-me")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(encrypted, ":")
	// Flip the first nibble of the ciphertext.
	flip := []byte(parts[2])
	flip[0] ^= 0x01
	parts[2] = string(flip)
	tampered := strings.Join(parts, ":")
	if _, err := DecryptNodeGCM(tampered); err == nil {
		t.Fatal("DecryptNodeGCM accepted tampered ciphertext")
	}
}

func TestEncryptionKeyDerivation(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "custom-key-material")
	withEnv := encryptionKey()
	t.Setenv("ENCRYPTION_KEY", "different-key-material")
	withOther := encryptionKey()
	if string(withEnv) == string(withOther) {
		t.Fatal("encryptionKey should derive different keys from different sources")
	}
	// Default key fallback.
	t.Setenv("ENCRYPTION_KEY", "")
	fallback := encryptionKey()
	if len(fallback) != 32 {
		t.Fatalf("encryptionKey len = %d, want 32 (sha256)", len(fallback))
	}
}

func TestAllowInsecureDefaultKey(t *testing.T) {
	t.Setenv("API_MONITOR_ALLOW_INSECURE_DEFAULT_KEY", "1")
	if !AllowInsecureDefaultKey() {
		t.Fatal("expected 1 to enable the insecure default key")
	}
	t.Setenv("API_MONITOR_ALLOW_INSECURE_DEFAULT_KEY", "true")
	if !AllowInsecureDefaultKey() {
		t.Fatal("expected true to enable the insecure default key")
	}
	t.Setenv("API_MONITOR_ALLOW_INSECURE_DEFAULT_KEY", "0")
	if AllowInsecureDefaultKey() {
		t.Fatal("expected 0 to disable the insecure default key")
	}
	t.Setenv("API_MONITOR_ALLOW_INSECURE_DEFAULT_KEY", "")
	if AllowInsecureDefaultKey() {
		t.Fatal("expected empty to disable the insecure default key")
	}
}

func TestStrictEncryptionRequired(t *testing.T) {
	t.Setenv("API_MONITOR_STRICT_ENCRYPTION", "1")
	if !StrictEncryptionRequired() {
		t.Fatal("expected 1 to require strict encryption")
	}
	t.Setenv("API_MONITOR_STRICT_ENCRYPTION", "true")
	if !StrictEncryptionRequired() {
		t.Fatal("expected true to require strict encryption")
	}
	t.Setenv("API_MONITOR_STRICT_ENCRYPTION", "")
	if StrictEncryptionRequired() {
		t.Fatal("expected empty to not require strict encryption")
	}
}

func TestEncryptionKeyWarnsOnceForDefaultKey(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "")
	t.Setenv("API_MONITOR_ALLOW_INSECURE_DEFAULT_KEY", "")

	var buf strings.Builder
	previous := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(previous) })

	// 告警由进程级 sync.Once 把关,同包其他测试可能已触发过;先重置以便独立断言「只打一次」。
	defaultKeyWarnOnce = sync.Once{}

	for i := 0; i < 3; i++ {
		if key := encryptionKey(); len(key) != 32 {
			t.Fatalf("encryptionKey len = %d, want 32", len(key))
		}
	}
	if got := strings.Count(buf.String(), "SECURITY WARNING"); got != 1 {
		t.Fatalf("expected exactly one warning for repeated default-key use, got %d: %q", got, buf.String())
	}
}
