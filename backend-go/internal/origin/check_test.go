package origin

import (
	"net/url"
	"testing"
)

func TestIsDevelopmentOriginHost(t *testing.T) {
	allowed := []string{"localhost", "LOCALHOST", "host.docker.internal", "127.0.0.1", "[::1]", "192.168.10.3", "10.0.0.5", "172.16.1.1"}
	for _, host := range allowed {
		if !IsDevelopmentOriginHost(host) {
			t.Errorf("expected %q to be a development origin host", host)
		}
	}
	denied := []string{"", "null", "attacker.example", "zcode", "8.8.8.8", "app"}
	for _, host := range denied {
		if IsDevelopmentOriginHost(host) {
			t.Errorf("expected %q to NOT be a development origin host", host)
		}
	}
}

func TestIsEmbeddedWrapperOrigin(t *testing.T) {
	allowed := []string{"null", "app://zcode", "zcode://app", "file://index.html", "about:blank", ""}
	for _, origin := range allowed {
		if !IsEmbeddedWrapperOrigin(origin) {
			t.Errorf("expected %q to be an embedded wrapper origin", origin)
		}
	}
	denied := []string{"http://attacker.example", "https://attacker.example", "http://localhost:5173", "http://192.168.10.3:3000"}
	for _, origin := range denied {
		if IsEmbeddedWrapperOrigin(origin) {
			t.Errorf("expected %q to NOT be an embedded wrapper origin", origin)
		}
	}
}

func TestIsLoopbackClient(t *testing.T) {
	if !IsLoopbackClient("127.0.0.1:3000") {
		t.Error("127.0.0.1:3000 should be loopback")
	}
	if !IsLoopbackClient("[::1]:3000") {
		t.Error("[::1]:3000 should be loopback")
	}
	if IsLoopbackClient("192.168.10.3:3000") {
		t.Error("192.168.10.3:3000 should not be loopback")
	}
	if IsLoopbackClient("") {
		t.Error("empty remote addr should not be loopback")
	}
}

func TestAllowedByConfig(t *testing.T) {
	list := []string{"http://localhost:5173", "https://wrapper.example/"}
	if !AllowedByConfig(list, "http://localhost:5173") {
		t.Error("exact origin should be allowed")
	}
	if !AllowedByConfig(list, "https://wrapper.example") {
		t.Error("origin with trailing slash should be normalized")
	}
	if AllowedByConfig(list, "https://attacker.example") {
		t.Error("unlisted origin should be denied")
	}
	if AllowedByConfig(nil, "https://wrapper.example") {
		t.Error("empty list should deny")
	}
}

func TestOriginParsingRoundTrip(t *testing.T) {
	for _, raw := range []string{"http://localhost:3000", "http://127.0.0.1:3000/", "null"} {
		if _, err := url.Parse(raw); err != nil {
			t.Fatalf("unexpected parse error for %q: %v", raw, err)
		}
	}
}
