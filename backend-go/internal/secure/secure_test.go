package secure

import (
	"strings"
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
