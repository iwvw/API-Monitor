package prompts

import (
	"strings"
	"testing"
)

func TestHashIP(t *testing.T) {
	withPort := HashIP("203.0.113.7:8080")
	withoutPort := HashIP("203.0.113.7")
	if withPort != withoutPort {
		t.Fatalf("HashIP with port %q != without port %q", withPort, withoutPort)
	}

	ipv6WithPort := HashIP("[2001:db8::1]:443")
	ipv6Bare := HashIP("2001:db8::1")
	if ipv6WithPort != ipv6Bare {
		t.Fatalf("HashIP ipv6 with port %q != bare %q", ipv6WithPort, ipv6Bare)
	}

	if withPort == HashIP("203.0.113.8") {
		t.Fatal("different IPs produced same hash")
	}

	if len(withPort) != 16 {
		t.Fatalf("HashIP length = %d, want 16", len(withPort))
	}
	for _, r := range withPort {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Fatalf("HashIP %q contains non-hex char %q", withPort, r)
		}
	}

	if strings.Contains(withPort, "203.0.113.7") {
		t.Fatal("hash leaks the raw IP address")
	}

	if HashIP("") != HashIP("") {
		t.Fatal("empty input should be deterministic")
	}
}