package onepanel

import (
	"encoding/json"
	"testing"
)

func buildPanelEnvelope(t *testing.T, sites []map[string]interface{}) string {
	t.Helper()
	buf, err := json.Marshal(map[string]interface{}{"code": 200, "message": "", "data": sites})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return string(buf)
}

func TestBuildReverseProxyTargetsGroupsByUpstreamPort(t *testing.T) {
	raw := buildPanelEnvelope(t, []map[string]interface{}{
		{
			"id": 16, "primaryDomain": "resin.dsukhub.com", "type": "proxy", "protocol": "HTTPS",
			"status": "Running", "proxy": "http://127.0.0.1:2260",
			"domains": []map[string]interface{}{{"domain": "resin.dsukhub.com", "ssl": true, "port": 80}},
		},
		{
			"id": 9, "primaryDomain": "salen.eu.org", "type": "proxy", "protocol": "HTTPS",
			"status": "Running", "proxy": "http://127.0.0.1:5244",
			"domains": []map[string]interface{}{{"domain": "salen.eu.org", "port": 80}},
		},
		{
			"id": 15, "primaryDomain": "ol.dsuk.top", "type": "proxy", "protocol": "HTTPS",
			"status": "Running", "proxy": "http://127.0.0.1:5244",
			"domains": []map[string]interface{}{
				{"domain": "ol.dsuk.top", "port": 80},
				{"domain": "files.example.com", "port": 80},
			},
		},
	})

	targets := buildReverseProxyTargets(raw)
	if len(targets) != 2 {
		t.Fatalf("targets = %d, want 2: %+v", len(targets), targets)
	}
	if targets[0].UpstreamPort != 2260 || targets[1].UpstreamPort != 5244 {
		t.Fatalf("ports not sorted: %d, %d", targets[0].UpstreamPort, targets[1].UpstreamPort)
	}
	if got := targets[0].Domains[0]; got.URL != "https://resin.dsukhub.com" {
		t.Fatalf("https site url = %q", got.URL)
	}
	// 同端口两个站点都要保留，且域名按字母序排列。
	shared := targets[1].Domains
	if len(shared) != 2 {
		t.Fatalf("port 5244 domains = %d, want 2", len(shared))
	}
	if shared[0].Domain != "ol.dsuk.top" || shared[1].Domain != "salen.eu.org" {
		t.Fatalf("domains not sorted: %q, %q", shared[0].Domain, shared[1].Domain)
	}
	if len(shared[0].Domains) != 2 {
		t.Fatalf("aliases not merged: %+v", shared[0].Domains)
	}
}

func TestBuildReverseProxyTargetsSkipsNonProxyAndPortless(t *testing.T) {
	raw := buildPanelEnvelope(t, []map[string]interface{}{
		{"id": 1, "primaryDomain": "static.example.com", "type": "static", "proxy": ""},
		{"id": 2, "primaryDomain": "noproxy.example.com", "type": "proxy", "proxy": ""},
		{"id": 3, "primaryDomain": "runtime.example.com", "type": "runtime", "proxy": "http://127.0.0.1:9000"},
		{"id": 4, "primaryDomain": "ok.example.com", "type": "proxy", "proxy": "http://127.0.0.1:9001"},
	})
	targets := buildReverseProxyTargets(raw)
	if len(targets) != 1 || targets[0].UpstreamPort != 9001 {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	if targets[0].Domains[0].URL != "http://ok.example.com" {
		t.Fatalf("expected http scheme for non-HTTPS site, got %q", targets[0].Domains[0].URL)
	}
}

func TestBuildReverseProxyTargetsHandlesBadInput(t *testing.T) {
	for _, raw := range []string{"", "not-json", `{"code":200,"data":null}`, `{"code":200,"data":{}}`} {
		if targets := buildReverseProxyTargets(raw); len(targets) != 0 {
			t.Fatalf("raw %q should yield no targets, got %+v", raw, targets)
		}
	}
}

func TestParseUpstreamHostPort(t *testing.T) {
	cases := []struct {
		raw  string
		port int
		ok   bool
		host string
	}{
		{"http://127.0.0.1:8081", 8081, true, "127.0.0.1"},
		{"http://127.0.0.1:8081/", 8081, true, "127.0.0.1"},
		{"https://app.internal:8443", 8443, true, "app.internal"},
		{"http://app.internal", 80, true, "app.internal"},
		{"https://app.internal", 443, true, "app.internal"},
		{"", 0, false, ""},
		{"nonsense", 0, false, ""},
		{"http://127.0.0.1:99999", 0, false, ""},
	}
	for _, c := range cases {
		port, ok, host := parseUpstreamHostPort(c.raw)
		if ok != c.ok || (c.ok && port != c.port) {
			t.Fatalf("parseUpstreamHostPort(%q) = (%d,%v), want (%d,%v)", c.raw, port, ok, c.port, c.ok)
		}
		if host != c.host {
			t.Fatalf("parseUpstreamHostPort(%q) host = %q, want %q", c.raw, host, c.host)
		}
	}
}
