package onepanel

import (
	"net/http"
	"testing"
)

func TestCertificateNameMatches(t *testing.T) {
	cases := []struct {
		cert, query string
		want        bool
	}{
		{"*.dmuk.org", "occ.dmuk.org", true},
		{"*.dmuk.org", "dmuk.org", true},
		{"*.dmuk.org", "*.dmuk.org", true},
		{"*.dmuk.org", "dmuk.com", false},
		{"dsukhub.com", "dsukhub.com", true},
		{"dsukhub.com", "www.dsukhub.com", false},
		{"", "x.com", false},
		{"x.com", "", false},
	}
	for _, tc := range cases {
		if got := certificateNameMatches(tc.cert, tc.query); got != tc.want {
			t.Fatalf("certificateNameMatches(%q,%q)=%v want %v", tc.cert, tc.query, got, tc.want)
		}
	}
}

// TestResolveSSLIDByDomain 验证按泛域名解析到正确的证书 id。
func TestResolveSSLIDByDomain(t *testing.T) {
	service := newTestService(t)
	runner := &scriptedRunner{output: `{"code":200,"data":{"items":[
		{"id":19,"primaryDomain":"*.dmuk.org","domains":"","description":"","provider":"dnsAccount"}
	]}}`}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/ssl/resolve-id?domain=occ.dmuk.org", "")
	if res.Code != http.StatusOK {
		t.Fatalf("resolve ssl status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Matched bool   `json:"matched"`
			ID      any    `json:"id"`
			Reason  string `json:"matchedBy"`
		} `json:"data"`
	}
	mustDecode(t, res, &payload)
	if !payload.Success || !payload.Data.Matched || payload.Data.Reason == "" {
		t.Fatalf("unexpected resolve payload: %s", res.Body.String())
	}
	if id, ok := payload.Data.ID.(float64); !ok || int(id) != 19 {
		t.Fatalf("expected id 19, got %v", payload.Data.ID)
	}
}

// TestResolveSSLIDNotFound 验证无覆盖证书时返回 404 并带提示。
func TestResolveSSLIDNotFound(t *testing.T) {
	service := newTestService(t)
	runner := &scriptedRunner{output: `{"code":200,"data":{"items":[
		{"id":4,"primaryDomain":"dsukhub.com","domains":""}
	]}}`}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/ssl/resolve-id?domain=occ.dmuk.org", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing cert should 404, got %d body=%s", res.Code, res.Body.String())
	}
}

// TestResolveSSLIDRequiresDomain 验证缺少 domain 参数时 400。
func TestResolveSSLIDRequiresDomain(t *testing.T) {
	service := newTestService(t)
	service.SetAgentRunner(&scriptedRunner{output: `{"code":200,"data":{"items":[]}}`})
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/ssl/resolve-id", "")
	if res.Code != http.StatusBadRequest {
		t.Fatalf("missing domain should 400, got %d body=%s", res.Code, res.Body.String())
	}
}

// TestResolveWebsiteIDByDomain 验证按域名解析网站 id。
func TestResolveWebsiteIDByDomain(t *testing.T) {
	service := newTestService(t)
	runner := &scriptedRunner{output: `{"code":200,"data":{"items":[
		{"id":14,"primaryDomain":"occ.dmuk.org","alias":"OCC 控制台"},
		{"id":24,"primaryDomain":"th.dsukhub.com","alias":""}
	]}}`}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/websites/resolve-id?domain=occ.dmuk.org", "")
	if res.Code != http.StatusOK {
		t.Fatalf("resolve website status = %d body=%s", res.Code, res.Body.String())
	}
	var payload struct {
		Data struct {
			Matched bool `json:"matched"`
			ID      any  `json:"id"`
		} `json:"data"`
	}
	mustDecode(t, res, &payload)
	if !payload.Data.Matched {
		t.Fatalf("expected match: %s", res.Body.String())
	}
	if id, ok := payload.Data.ID.(float64); !ok || int(id) != 14 {
		t.Fatalf("expected website id 14, got %v", payload.Data.ID)
	}
}

// TestResolveUpstreamErrorSurfaced 验证解析接口在 1Panel 失败时把错误带出(走统一错误收敛)。
func TestResolveUpstreamErrorSurfaced(t *testing.T) {
	service := newTestService(t)
	runner := &scriptedRunner{output: `{"code":500,"message":"服务错误: record not found","data":null}`}
	service.SetAgentRunner(runner)
	perform(service, http.MethodPost, "/api/onepanel/config", `{"serverId":"srv-arm","apiKey":"secret-key-1"}`)

	res := perform(service, http.MethodGet, "/api/onepanel/srv-arm/ssl/resolve-id?domain=x.com", "")
	if res.Code != http.StatusBadGateway {
		t.Fatalf("upstream failure should 502, got %d body=%s", res.Code, res.Body.String())
	}
}
