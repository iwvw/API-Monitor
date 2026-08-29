package config

import "strings"

// NormalizeMobileForStorage normalizes user input to a stable storage format.
// It keeps existing country codes and auto-prefixes mainland China numbers with +86.
func NormalizeMobileForStorage(raw string) string {
	digits, hasPlus := extractMobileDigits(raw)
	if digits == "" {
		return ""
	}
	if hasPlus {
		return "+" + digits
	}
	if isChinaMobileWithCountryCode(digits) {
		return "+86" + digits[2:]
	}
	if isChinaMainlandMobileDigits(digits) {
		return "+86" + digits
	}
	// For non-China numbers without a leading +, preserve semantics by adding it.
	return "+" + digits
}

// CanonicalMobileKey returns the comparison key used by dedupe/matching logic.
func CanonicalMobileKey(raw string) string {
	return NormalizeMobileForStorage(raw)
}

func extractMobileDigits(raw string) (digits string, hasPlus bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}

	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			goto collect
		case isMobileSeparator(r):
			continue
		case r == '+':
			hasPlus = true
			goto collect
		default:
			goto collect
		}
	}

collect:
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String(), hasPlus
}

func isChinaMainlandMobileDigits(digits string) bool {
	if len(digits) != 11 || digits[0] != '1' {
		return false
	}
	return digits[1] >= '3' && digits[1] <= '9'
}

func isChinaMobileWithCountryCode(digits string) bool {
	if len(digits) != 13 || !strings.HasPrefix(digits, "86") {
		return false
	}
	return isChinaMainlandMobileDigits(digits[2:])
}

func isMobileSeparator(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '-', '(', ')', '.', '/':
		return true
	default:
		return false
	}
}
