package serveragent

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// 验证 token 模式创建转发：明文令牌只在创建响应返回一次，库内存的是密文
func TestCreateManagedForwardTokenMode(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host-t','测试主机','192.0.2.10','root','password')`); err != nil {
		t.Fatal(err)
	}

	body := `{"name":"token-fwd","server_id":"host-t","local_host":"127.0.0.1","local_port":9999,"protocol":"tcp","transport":"cloudflare_tunnel","access_mode":"token"}`
	req := httptest.NewRequest("POST", "/api/server/forward", strings.NewReader(body))
	rec := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec, req, db, nil)

	if rec.Code != 201 {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	resp := rec.Body.String()
	if !strings.Contains(resp, `"access_token"`) {
		t.Fatalf("response missing plaintext access_token: %s", resp)
	}
	// 明文令牌 32 字符 hex
	idx := strings.Index(resp, `"access_token":"`)
	if idx < 0 {
		t.Fatal("unreachable")
	}
	token := resp[idx+len(`"access_token":"`) : idx+len(`"access_token":"`)+32]
	if len(token) != 32 {
		t.Fatalf("token length = %d, want 32 (%q)", len(token), token)
	}
	for _, c := range token {
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHex {
			t.Fatalf("token %q contains non-hex char %q", token, c)
		}
	}

	// 列表/详情不应泄露明文令牌
	var stored string
	if err := db.QueryRowContext(context.Background(), `SELECT access_token FROM managed_forwards WHERE name='token-fwd'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "" {
		t.Fatal("access_token not persisted")
	}
	if stored == token {
		t.Fatal("stored token is plaintext, expected ciphertext")
	}
	if !strings.Contains(resp, `"has_token":true`) {
		t.Fatalf("list payload should flag has_token=true: %s", resp)
	}
}

// public 模式不生成令牌
func TestCreateManagedForwardPublicModeHasNoToken(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host-t','测试主机','192.0.2.10','root','password')`); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"public-fwd","server_id":"host-t","local_port":8080,"protocol":"http","transport":"cloudflare_tunnel","access_mode":"public"}`
	req := httptest.NewRequest("POST", "/api/server/forward", strings.NewReader(body))
	rec := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec, req, db, nil)
	if rec.Code != 201 {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"access_token"`) {
		t.Fatalf("public mode must not return access_token: %s", rec.Body.String())
	}
}

// 同一源主机允许多条转发规则共用同一本地端口（如 5173 同时走 CF Tunnel 与 tcp_relay）
func TestCreateManagedForwardAllowsSharedLocalPort(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host-t','测试主机','192.0.2.10','root','password'),('relay-id','中继','relay.example.com','root','password')`); err != nil {
		t.Fatal(err)
	}
	first := `{"name":"fwd-http","server_id":"host-t","local_host":"127.0.0.1","local_port":5173,"protocol":"http","transport":"cloudflare_tunnel","access_mode":"public"}`
	rec := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec, httptest.NewRequest("POST", "/api/server/forward", strings.NewReader(first)), db, nil)
	if rec.Code != 201 {
		t.Fatalf("first create status = %d body=%s", rec.Code, rec.Body.String())
	}
	second := `{"name":"fwd-tcp","server_id":"host-t","local_host":"127.0.0.1","local_port":5173,"protocol":"tcp","transport":"tcp_relay","relay_server_id":"relay-id","access_mode":"public"}`
	rec2 := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec2, httptest.NewRequest("POST", "/api/server/forward", strings.NewReader(second)), db, nil)
	if rec2.Code != 201 {
		t.Fatalf("second create with shared local port status = %d body=%s", rec2.Code, rec2.Body.String())
	}
	// 预检不再把「端口无冲突」当作冲突项
	pre := httptest.NewRecorder()
	service.handleManagedForwardRoutes(pre, httptest.NewRequest("POST", "/api/server/forward/preflight", strings.NewReader(`{"server_id":"host-t","local_host":"127.0.0.1","local_port":5173,"transport":"cloudflare_tunnel"}`)), db, []string{"preflight"})
	if pre.Code != 200 {
		t.Fatalf("preflight status = %d body=%s", pre.Code, pre.Body.String())
	}
	if strings.Contains(pre.Body.String(), `"端口无冲突"`) {
		t.Fatalf("preflight should no longer include port-conflict check: %s", pre.Body.String())
	}
	// 仅剩与冲突无关的检查项（源主机在线 / CF Tunnel 已就绪）
	if !strings.Contains(pre.Body.String(), `"源主机在线"`) || !strings.Contains(pre.Body.String(), `"CF Tunnel 已就绪"`) {
		t.Fatalf("preflight unexpected checks: %s", pre.Body.String())
	}
}

// available-ports：已占用端口被剔除
func TestAvailablePortsExcludesOccupied(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host-t','测试主机','192.0.2.10','root','password'),('host-src','源主机','192.0.2.11','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,relay_server_id,access_mode,desired_status,apply_status,remote_port) VALUES('fwda','占用55655','host-src','127.0.0.1',5000,'tcp','tcp_relay','host-t','public','running','running',55655)`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/server/forward/available-ports?server_id=host-t", nil)
	rec := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec, req, db, []string{"available-ports"})
	if rec.Code != 200 {
		t.Fatalf("available-ports status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	var payload struct {
		Data struct {
			Available []int `json:"available"`
			Used      int   `json:"used"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("bad json: %v body=%s", err, body[:min(len(body), 300)])
	}
	for _, port := range payload.Data.Available {
		if port == 55655 {
			t.Fatalf("occupied port 55655 should be excluded")
		}
	}
	found := false
	for _, port := range payload.Data.Available {
		if port == 55656 {
			found = true
		}
	}
	if !found || payload.Data.Used != 1 {
		t.Fatalf("expected free 55656 present and used=1, got used=%d", payload.Data.Used)
	}
}

// status 端点：存在与不存在两种情况
func TestForwardStatusEndpoint(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host-t','测试主机','192.0.2.10','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,access_mode,desired_status,apply_status,last_error) VALUES('fwdst','状态查询','host-t','127.0.0.1',7000,'tcp','cloudflare_tunnel','public','running','running','probe timeout')`); err != nil {
		t.Fatal(err)
	}
	// 存在
	req := httptest.NewRequest("GET", "/api/server/forward/fwdst/status", nil)
	rec := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec, req, db, []string{"fwdst", "status"})
	if rec.Code != 200 {
		t.Fatalf("status code = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"apply_status":"running"`) || !strings.Contains(rec.Body.String(), "probe timeout") {
		t.Fatalf("status payload incomplete: %s", rec.Body.String())
	}
	// 不存在
	req2 := httptest.NewRequest("GET", "/api/server/forward/nope/status", nil)
	rec2 := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec2, req2, db, []string{"nope", "status"})
	if rec2.Code != 404 {
		t.Fatalf("missing forward status = %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// token/panel 访问控制：仅已落地组合（tcp_relay+token、CF 隧道(http/https)+token）通过守卫，
// 其余必须拒绝，防止「声称有鉴权实则公开」。
func TestDeployRejectsNonPublicAccessMode(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host-t','测试主机','192.0.2.10','root','password')`); err != nil {
		t.Fatal(err)
	}
	// CF 隧道 + token + tcp：tcp 无鉴权代理可用，必须拒绝并提示改 http/https
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,access_mode,desired_status,apply_status) VALUES('fwdtk','token-fwd-tcp','host-t','127.0.0.1',9000,'tcp','cloudflare_tunnel','token','running','pending')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/server/forward/fwdtk/deploy", nil)
	rec := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec, req, db, []string{"fwdtk", "deploy"})
	if rec.Code != 422 {
		t.Fatalf("CF token tcp deploy status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "http/https") {
		t.Fatalf("CF token tcp rejection should mention http/https: %s", rec.Body.String())
	}

	// CF 隧道 + panel：守卫已放开，部署应进入传输层（测试环境无 agent → 报 agent 离线，而非访问控制拒绝）
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,access_mode,desired_status,apply_status) VALUES('fwdp','panel-fwd','host-t','127.0.0.1',9001,'http','cloudflare_tunnel','panel','running','pending')`); err != nil {
		t.Fatal(err)
	}
	req2 := httptest.NewRequest("POST", "/api/server/forward/fwdp/deploy", nil)
	rec2 := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec2, req2, db, []string{"fwdp", "deploy"})
	if strings.Contains(rec2.Body.String(), "改为 public") {
		t.Fatalf("panel mode should no longer be rejected by the access guard: %s", rec2.Body.String())
	}
}

// tcp_relay 部署必须使用中继服务器 host，而不是面板内部 UUID。
func TestDeployTCPRelayRequiresRelayHost(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('relay-id','中继','','root','password'),('host-src','源','192.0.2.11','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,relay_server_id,access_mode,desired_status,apply_status) VALUES('fwdrt','relay-fwd','host-src','127.0.0.1',9100,'tcp','tcp_relay','relay-id','public','running','pending')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/server/forward/fwdrt/deploy", nil)
	rec := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec, req, db, []string{"fwdrt", "deploy"})
	// 中继主机 host 为空 → 422，明确拒绝而非把 UUID 当下发地址
	if rec.Code != 422 {
		t.Fatalf("deploy without relay host status = %d body=%s", rec.Code, rec.Body.String())
	}
}

// tcp_relay 列表返回中继 host，access_url 使用真实 host。
func TestListTCPRelayExposesRelayHost(t *testing.T) {
	service, db := testService(t)
	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('relay-id','中继','relay.example.com','root','password'),('host-src','源','192.0.2.11','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_forwards(id,name,server_id,local_host,local_port,protocol,transport,relay_server_id,remote_port,access_mode,desired_status,apply_status) VALUES('fwdrt','relay-fwd','host-src','127.0.0.1',9200,'tcp','tcp_relay','relay-id',55660,'public','running','running')`); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/server/forward?limit=20", nil)
	rec := httptest.NewRecorder()
	service.handleManagedForwardRoutes(rec, req, db, nil)
	if rec.Code != 200 {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"relay_server_host":"relay.example.com"`) {
		t.Fatalf("list missing relay host: %s", body)
	}
	if !strings.Contains(body, `"access_url":"tcp://relay.example.com:55660"`) {
		t.Fatalf("access_url should use host not server id: %s", body)
	}
}
