package totp

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 / RFC 4226 test vectors. The shared secret is the ASCII bytes
// "12345678901234567890", base32 = GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ.
const rfcSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

func TestHotpCodeRFC4226Vectors(t *testing.T) {
	cases := []struct {
		counter uint64
		want    string
	}{
		{0, "755224"},
		{1, "287082"},
		{2, "359152"},
		{3, "969429"},
		{4, "338314"},
		{5, "254676"},
		{6, "287922"},
		{7, "162583"},
		{8, "399871"},
		{9, "520489"},
	}
	for _, tc := range cases {
		got, err := hotpCode(rfcSecret, tc.counter, 6, "SHA1")
		if err != nil {
			t.Fatalf("hotpCode(%d) unexpected error: %v", tc.counter, err)
		}
		if got != tc.want {
			t.Errorf("hotpCode(%d) = %s, want %s", tc.counter, got, tc.want)
		}
	}
}

func TestTotpCodeRFC6238Vectors(t *testing.T) {
	cases := []struct {
		now  int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
	}
	for _, tc := range cases {
		now := time.Unix(tc.now, 0)
		got, err := totpCode(rfcSecret, now, 8, 30, "SHA1")
		if err != nil {
			t.Fatalf("totpCode(%d) unexpected error: %v", tc.now, err)
		}
		if got != tc.want {
			t.Errorf("totpCode(%d) = %s, want %s", tc.now, got, tc.want)
		}
	}
}

func TestTotpCodeDefaultsAndClamps(t *testing.T) {
	now := time.Unix(59, 0)
	if got, err := totpCode(rfcSecret, now, 0, 0, "SHA1"); err != nil || got != "287082" {
		t.Errorf("totpCode with zero digits/period = %q, err=%v; want 287082", got, err)
	}
	if got, err := hotpCode(rfcSecret, 1, 0, "SHA1"); err != nil || got != "287082" {
		t.Errorf("hotpCode with zero digits = %q, err=%v; want 287082", got, err)
	}
	if got, err := hotpCode(rfcSecret, 0, 11, "SHA1"); err != nil || len(got) != 10 {
		t.Errorf("hotpCode digits clamped to 10 = %q (len %d), err=%v", got, len(got), err)
	}
	if _, err := hotpCode("!!!invalid!!!", 0, 6, "SHA1"); err == nil {
		t.Error("hotpCode should reject a non-base32 secret")
	}
}

func TestVerifyTOTPSkewWindow(t *testing.T) {
	now := time.Unix(1000, 0)
	cur, err := totpCode(rfcSecret, now, 6, 30, "SHA1")
	if err != nil {
		t.Fatal(err)
	}
	prev, err := totpCode(rfcSecret, now.Add(-30*time.Second), 6, 30, "SHA1")
	if err != nil {
		t.Fatal(err)
	}
	next, err := totpCode(rfcSecret, now.Add(30*time.Second), 6, 30, "SHA1")
	if err != nil {
		t.Fatal(err)
	}
	farFuture, err := totpCode(rfcSecret, now.Add(60*time.Second), 6, 30, "SHA1")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyTOTP(rfcSecret, cur, 6, 30, "SHA1", now) {
		t.Error("verifyTOTP should accept the current step token")
	}
	if !verifyTOTP(rfcSecret, prev, 6, 30, "SHA1", now) {
		t.Error("verifyTOTP should accept the previous step token")
	}
	if !verifyTOTP(rfcSecret, next, 6, 30, "SHA1", now) {
		t.Error("verifyTOTP should accept the next step token")
	}
	if verifyTOTP(rfcSecret, farFuture, 6, 30, "SHA1", now) {
		t.Error("verifyTOTP should reject a token two steps away")
	}
	if verifyTOTP(rfcSecret, "000000", 6, 30, "SHA1", now) {
		t.Error("verifyTOTP should reject a wrong token")
	}
	if verifyTOTP("!!!invalid!!!", cur, 6, 30, "SHA1", now) {
		t.Error("verifyTOTP should reject an invalid base32 secret")
	}
}

func TestVerifyTOTPNormalizesTokenInput(t *testing.T) {
	now := time.Unix(1000, 0)
	valid, err := totpCode(rfcSecret, now, 6, 30, "SHA1")
	if err != nil {
		t.Fatal(err)
	}
	if !verifyTOTP(rfcSecret, "  "+valid+"  ", 6, 30, "SHA1", now) {
		t.Error("verifyTOTP should trim surrounding whitespace")
	}
	spaced := valid[0:3] + " " + valid[3:6]
	if !verifyTOTP(rfcSecret, spaced, 6, 30, "SHA1", now) {
		t.Error("verifyTOTP should strip inner spaces")
	}
}

func TestVerifyHOTPCounterAdvance(t *testing.T) {
	matched, next := verifyHOTP(rfcSecret, "755224", 0, 6, "SHA1")
	if !matched || next != 1 {
		t.Errorf("verifyHOTP counter 0 match = %v, next=%d; want true, 1", matched, next)
	}
	matched, next = verifyHOTP(rfcSecret, "520489", 5, 6, "SHA1")
	if !matched || next != 10 {
		t.Errorf("verifyHOTP counter 9 match = %v, next=%d; want true, 10", matched, next)
	}
	matched, next = verifyHOTP(rfcSecret, "000000", 0, 6, "SHA1")
	if matched || next != 0 {
		t.Errorf("verifyHOTP no match = %v, next=%d; want false, 0", matched, next)
	}
	matched, next = verifyHOTP(rfcSecret, "755224", 100, 6, "SHA1")
	if matched || next != 100 {
		t.Errorf("verifyHOTP out-of-window match = %v, next=%d; want false, 100", matched, next)
	}
}

func TestDecodeBase32(t *testing.T) {
	decoded, err := decodeBase32("JBSWY3DPEHPK3PXP")
	if err != nil || string(decoded) != "Hello!\xde\xad\xbe\xef" {
		t.Fatalf("decodeBase32(JBSWY3DPEHPK3PXP) = %q, %v", decoded, err)
	}
	if _, err := decodeBase32(" jbswy3dpehpk3pxp "); err != nil {
		t.Errorf("decodeBase32 should clean spaces and lowercase, got %v", err)
	}
	if _, err := decodeBase32("JBSWY3DPEHPK3PXP=="); err != nil {
		t.Errorf("decodeBase32 should tolerate padding, got %v", err)
	}
	if _, err := decodeBase32("JBSWY3!INVALID!"); err == nil {
		t.Error("decodeBase32 should reject non-base32 characters")
	}
}

func TestHashFactory(t *testing.T) {
	if hashFactory("") == nil || hashFactory("sha-1") == nil || hashFactory("SHA1") == nil {
		t.Error("hashFactory should map empty/dashed/uppercase values to sha1")
	}
	if hashFactory("sha256") == nil || hashFactory("sha512") == nil {
		t.Error("hashFactory should support sha256 and sha512")
	}
}

func TestGenerateSecret(t *testing.T) {
	secret := generateSecret()
	if len(secret) != 32 {
		t.Errorf("generateSecret length = %d, want 32", len(secret))
	}
	if _, err := decodeBase32(secret); err != nil {
		t.Errorf("generateSecret output is not valid base32: %v", err)
	}
}

func TestGenerateURI(t *testing.T) {
	cases := []struct {
		name    string
		input   accountInput
		mustEnd string
	}{
		{"totp defaults", accountInput{Secret: "JBSWY3DPEHPK3PXP", Issuer: "Issuer", Account: "user"}, "otpauth://totp/Issuer:user?algorithm=SHA1&issuer=Issuer&secret=JBSWY3DPEHPK3PXP"},
		{"hotp counter", accountInput{OTPType: "hotp", Secret: "JBSWY3DPEHPK3PXP", Issuer: "Issuer", Account: "user", Counter: 42}, "otpauth://hotp/Issuer:user?algorithm=SHA1&counter=42&issuer=Issuer&secret=JBSWY3DPEHPK3PXP"},
		{"custom digits period", accountInput{Secret: "JBSWY3DPEHPK3PXP", Issuer: "Issuer", Account: "user", Digits: 8, Period: 60, Algorithm: "SHA256"}, "digits=8&period=60&algorithm=SHA256"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uri := generateURI(tc.input)
			for _, part := range strings.Split(tc.mustEnd, "&") {
				if !strings.Contains(uri, part) {
					t.Errorf("generateURI = %s, missing %s", uri, part)
				}
			}
			if !strings.HasPrefix(uri, "otpauth://") {
				t.Errorf("generateURI = %s, want otpauth:// scheme", uri)
			}
		})
	}
}

func TestParseURI(t *testing.T) {
	parsed, ok := parseURI("otpauth://totp/Issuer:user?secret=JBSWY3DPEHPK3PXP&issuer=Issuer&algorithm=SHA1")
	if !ok {
		t.Fatal("parseURI should accept a standard otpauth URI")
	}
	if parsed.OTPType != "totp" || parsed.Issuer != "Issuer" || parsed.Account != "user" ||
		parsed.Secret != "JBSWY3DPEHPK3PXP" || parsed.Algorithm != "SHA1" ||
		parsed.Digits != 6 || parsed.Period != 30 {
		t.Errorf("parseURI = %#v", parsed)
	}

	hotp, ok := parseURI("otpauth://hotp/Issuer:user?secret=JBSWY3DPEHPK3PXP&counter=42")
	if !ok || hotp.OTPType != "hotp" || hotp.Counter != 42 {
		t.Errorf("parseURI hotp = %#v, ok=%v", hotp, ok)
	}

	explicit, ok := parseURI("otpauth://totp/Group:user?secret=JBSWY3DPEHPK3PXP&issuer=RealIssuer&digits=8&period=60")
	if !ok || explicit.Issuer != "RealIssuer" || explicit.Digits != 8 || explicit.Period != 60 {
		t.Errorf("parseURI with explicit issuer/digits/period = %#v", explicit)
	}

	colonAccount, ok := parseURI("otpauth://totp/Issuer:user:extra?secret=JBSWY3DPEHPK3PXP")
	if !ok || colonAccount.Issuer != "Issuer" || colonAccount.Account != "user:extra" {
		t.Errorf("parseURI with multi-colon label = %#v", colonAccount)
	}

	if _, ok := parseURI("http://totp/Issuer:user?secret=JBSWY3DPEHPK3PXP"); ok {
		t.Error("parseURI should reject non-otpauth schemes")
	}
}

func TestCleanSecretAndIsValid(t *testing.T) {
	if got := cleanSecret(" jbswy3d pehpk3pxp "); got != "JBSWY3DPEHPK3PXP" {
		t.Errorf("cleanSecret = %q", got)
	}
	if !isValidBase32Secret("JBSWY3DPEHPK3PXP") {
		t.Error("isValidBase32Secret should accept a valid secret")
	}
	if isValidBase32Secret("JBSWY3DPEHPK3PXP1") {
		t.Error("isValidBase32Secret should reject a secret with bad length")
	}
	if isValidBase32Secret("JBSWY3!!!") {
		t.Error("isValidBase32Secret should reject non-base32 characters")
	}
	if isValidBase32Secret("") {
		t.Error("isValidBase32Secret should reject an empty secret")
	}
}

func TestGenerateCodeForInputDefaults(t *testing.T) {
	now := time.Unix(59, 0)
	input := accountInput{Secret: rfcSecret}
	code, err := generateCodeForInput(input, now)
	if err != nil || code != "287082" {
		t.Errorf("generateCodeForInput totp defaults = %q, err=%v; want 287082", code, err)
	}
	hotpInput := accountInput{OTPType: "hotp", Secret: rfcSecret, Counter: 1}
	code, err = generateCodeForInput(hotpInput, now)
	if err != nil || code != "287082" {
		t.Errorf("generateCodeForInput hotp = %q, err=%v; want 287082", code, err)
	}
}