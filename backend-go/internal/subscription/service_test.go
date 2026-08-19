package subscription

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

func TestIsLinuxPlatformRecognizesDistributions(t *testing.T) {
	tests := []struct {
		platform string
		version  string
		want     bool
	}{
		{platform: "Linux", version: "Ubuntu 24.04 LTS", want: true},
		{platform: "Ubuntu", want: true},
		{platform: "Debian GNU/Linux", want: true},
		{platform: "", version: "Alpine Linux v3.20", want: true},
		{platform: "", version: "", want: true},
		{platform: "Windows", version: "11", want: false},
		{platform: "Darwin", version: "15", want: false},
	}
	for _, test := range tests {
		if got := isLinuxPlatform(test.platform, test.version); got != test.want {
			t.Errorf("isLinuxPlatform(%q, %q) = %v, want %v", test.platform, test.version, got, test.want)
		}
	}
}

func TestManagedSubscriptionNodesApplyPreferredAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	raw := "vless://00000000-0000-4000-8000-000000000001@origin.example.com:443?security=tls&type=ws&sni=origin.example.com&host=origin.example.com&path=%2Fedge#node"
	encrypted, err := secure.SecureEncrypt(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_preferences(id,name,address,port,enabled,is_default) VALUES('pref','优选','saas.sin.fan',443,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,enabled,publishable,apply_status,access_mode,tunnel_hostname) VALUES('managed-one','server-one','节点一','vless-reality','origin.example.com',45654,'tcp','',?,1,1,'running','cloudflare_tunnel','origin.example.com')`, encrypted); err != nil {
		t.Fatal(err)
	}
	nodes, err := loadManagedSubscriptionNodes(ctx, db, Subscription{})
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || !strings.Contains(nodes[0].Raw, "@saas.sin.fan:443") {
		t.Fatalf("preferred address was not published: %#v", nodes)
	}
	if nodes[0].TrafficServerID != "server-one" {
		t.Fatalf("managed node traffic host missing: %#v", nodes[0])
	}
	if !strings.Contains(nodes[0].Raw, "sni=origin.example.com") {
		t.Fatalf("Tunnel SNI must remain owned hostname: %s", nodes[0].Raw)
	}
	if nodes[0].Stable {
		t.Fatal("managed nodes must not enter the stable group without an explicit stability policy")
	}
}

func TestResetTokenRotatesNodeCredentialsAndQueuesRuntimeSync(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE server_accounts(id TEXT PRIMARY KEY,name TEXT,host TEXT,username TEXT,auth_type TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts(id,name,host,username,auth_type) VALUES('host','主机','192.0.2.1','root','password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,enabled,publishable,apply_status) VALUES('node','host','节点','vless-reality','sing-box','192.0.2.1',45654,'tcp','{}','',1,1,'running')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_plans(id,name,enabled,total_bytes,cycle_type,cycle_day,selection_mode,include_internal_nodes,include_external_nodes) VALUES('plan','套餐',1,0,'none',1,'explicit',1,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES('plan','node','internal')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions(id,profile_id,plan_id,name,public_token,vless_uuid,hysteria2_password,enabled) VALUES('sub','sub','plan','订阅','old-token','00000000-0000-4000-8000-000000000001','old-password',1)`); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/subscriptions/sub/reset-token", nil)
	responseRecorder := httptest.NewRecorder()
	(&Service{}).resetToken(responseRecorder, request, db, "sub")
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", responseRecorder.Code, responseRecorder.Body.String())
	}
	var token, uuid, password string
	if err := db.QueryRowContext(ctx, `SELECT public_token,vless_uuid,hysteria2_password FROM subscription_subscriptions WHERE id='sub'`).Scan(&token, &uuid, &password); err != nil {
		t.Fatal(err)
	}
	if token == "old-token" || uuid == "00000000-0000-4000-8000-000000000001" || password == "old-password" {
		t.Fatalf("credentials were not rotated: token=%q uuid=%q password=%q", token, uuid, password)
	}
	var state, reason string
	if err := db.QueryRowContext(ctx, `SELECT state,reason FROM subscription_runtime_reconcile WHERE node_id='node'`).Scan(&state, &reason); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || reason != "subscription credentials rotated" {
		t.Fatalf("unexpected reconcile job: state=%q reason=%q", state, reason)
	}
}

func TestRotateAddressOnlyRotatesTokenWithoutCredentialsOrSync(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions(id,profile_id,plan_id,name,public_token,vless_uuid,hysteria2_password,enabled) VALUES('sub','sub','','订阅','old-token','00000000-0000-4000-8000-000000000001','old-password',1)`); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/subscriptions/sub/rotate-address", nil)
	responseRecorder := httptest.NewRecorder()
	(&Service{}).rotateAddress(responseRecorder, request, db, "sub")
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", responseRecorder.Code, responseRecorder.Body.String())
	}
	var token, uuid, password string
	if err := db.QueryRowContext(ctx, `SELECT public_token,vless_uuid,hysteria2_password FROM subscription_subscriptions WHERE id='sub'`).Scan(&token, &uuid, &password); err != nil {
		t.Fatal(err)
	}
	if token == "old-token" {
		t.Fatalf("token was not rotated: token=%q", token)
	}
	if uuid != "00000000-0000-4000-8000-000000000001" || password != "old-password" {
		t.Fatalf("node credentials must stay untouched: uuid=%q password=%q", uuid, password)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_runtime_reconcile`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rotate-address must not enqueue runtime reconciliation, found %d jobs", count)
	}
}

func TestSubscriptionFormatFromUAInfersClientFormat(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Windows NT 10.0) ClashForWindows/0.20.0": "clash",
		"Mihomo 1.18.0 (darwin)":                                "clash",
		"Stash/2.0":                                             "clash",
		"v2rayN/6.25":                                           "base64",
		"NekoBox/1.3":                                           "base64",
		"Quantumult X/1.4":                                      "base64",
		"Shadowrocket/2023":                                     "base64",
		"SFA/1.0":                                               "base64",
		"sing-box 1.11":                                         "base64",
		"random-unknown-client/1.0":                             "",
		"":                                                      "",
	}
	for ua, want := range cases {
		if got := subscriptionFormatFromUA(ua); got != want {
			t.Errorf("subscriptionFormatFromUA(%q)=%q want %q", ua, got, want)
		}
	}
}

func TestWantsBrowserInfoPageDetectsBrowsersOnly(t *testing.T) {
	browser := httptest.NewRequest(http.MethodGet, "/sub/x", nil)
	browser.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36")
	browser.Header.Set("Accept", "text/html,application/xhtml+xml")
	if !wantsBrowserInfoPage(browser) {
		t.Fatal("browser request should get the info page")
	}
	browserNoHTML := httptest.NewRequest(http.MethodGet, "/sub/x", nil)
	browserNoHTML.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36")
	browserNoHTML.Header.Set("Accept", "*/*")
	if wantsBrowserInfoPage(browserNoHTML) {
		t.Fatal("browser-style UA without text/html Accept must not get the info page")
	}
	noAccept := httptest.NewRequest(http.MethodGet, "/sub/x", nil)
	noAccept.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	if wantsBrowserInfoPage(noAccept) {
		t.Fatal("Mozilla UA with no Accept header must not get the info page")
	}
	client := httptest.NewRequest(http.MethodGet, "/sub/x", nil)
	client.Header.Set("User-Agent", "Mihomo 1.18.0")
	if wantsBrowserInfoPage(client) {
		t.Fatal("proxy client must not get the info page")
	}
	client2 := httptest.NewRequest(http.MethodGet, "/sub/x", nil)
	client2.Header.Set("User-Agent", "Mozilla/5.0 SFA/1.0")
	if wantsBrowserInfoPage(client2) {
		t.Fatal("sing-box client must not get the info page")
	}
}

func TestServePublicSubscriptionInfoExposesDisplayDataOnly(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions(id,profile_id,plan_id,name,public_token,vless_uuid,hysteria2_password,enabled) VALUES('sub','sub','','信息订阅','public-token','00000000-0000-4000-8000-000000000001','secret-password',1)`); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/subscription/public/public-token", nil)
	responseRecorder := httptest.NewRecorder()
	(&Service{}).servePublicSubscriptionInfo(responseRecorder, request, db, "public-token")
	if responseRecorder.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", responseRecorder.Code, responseRecorder.Body.String())
	}
	body := responseRecorder.Body.String()
	for _, want := range []string{`"name":"信息订阅"`, `"public_token":"public-token"`, `"status":"active"`} {
		if !strings.Contains(body, want) {
			t.Errorf("public info payload missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, "secret-password") || strings.Contains(body, "00000000-0000-4000-8000-000000000001") {
		t.Fatal("public info payload must not leak node credentials")
	}
}

func TestPublishedNodeNamesAreUnique(t *testing.T) {
	nodes := ensureUniquePublishedNodeNames([]Node{
		{Name: "🇸🇬 新加坡", Raw: "vless://a@example.com:443#old", ConfigJSON: `{"name":"old","type":"vless"}`},
		{Name: "🇸🇬 新加坡", Raw: "trojan://b@example.net:443#old", ConfigJSON: `{"name":"old","type":"trojan"}`},
	})
	if nodes[0].Name != "🇸🇬 新加坡" || nodes[1].Name != "🇸🇬 新加坡 · 2" {
		t.Fatalf("unexpected published names: %#v", nodes)
	}
	if !strings.Contains(nodes[1].Raw, "%C2%B7%202") || !strings.Contains(nodes[1].ConfigJSON, "新加坡 · 2") {
		t.Fatalf("renamed node was not propagated: %#v", nodes[1])
	}
}

func TestDefaultMihomoOutputUsesClientCompatibleFingerprintAndUniqueNames(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	nodes := ensureUniquePublishedNodeNames([]Node{
		{Name: "香港月抛", Type: "vless", Server: "one.example.com", Port: 443, ConfigJSON: `{"name":"香港月抛","type":"vless","server":"one.example.com","port":443,"uuid":"one","tls":true,"client-fingerprint":"chrome"}`, Enabled: true},
		{Name: "香港月抛", Type: "trojan", Server: "two.example.com", Port: 443, ConfigJSON: `{"name":"香港月抛","type":"trojan","server":"two.example.com","port":443,"password":"two","tls":true,"client-fingerprint":"chrome"}`, Enabled: true},
	})
	body, _, err := renderOutput(ctx, db, Subscription{Name: "测试"}, nodes, "clash", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "global-client-fingerprint") {
		t.Fatal("deprecated global-client-fingerprint must not be emitted")
	}
	var output struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(body), &output); err != nil {
		t.Fatalf("invalid mihomo yaml: %v", err)
	}
	if len(output.Proxies) != 2 || output.Proxies[0]["name"] == output.Proxies[1]["name"] {
		t.Fatalf("proxy names are not unique: %#v", output.Proxies)
	}
	for _, proxy := range output.Proxies {
		if proxy["client-fingerprint"] != "chrome" {
			t.Fatalf("proxy fingerprint missing: %#v", proxy)
		}
	}
}

func TestMihomoOutputUsesManagedNodeNameOverStaleConfigName(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	nodes := []Node{
		{Name: "🇭🇰 香港月抛", Type: "vless", Server: "one.example.com", Port: 443, ConfigJSON: `{"name":"旧节点名","type":"vless","server":"one.example.com","port":443,"uuid":"one"}`, Enabled: true},
		{Name: "🇭🇰 香港", Type: "vless", Server: "two.example.com", Port: 443, ConfigJSON: `{"name":"旧节点名","type":"vless","server":"two.example.com","port":443,"uuid":"two"}`, Enabled: true},
	}
	body, _, err := renderOutput(ctx, db, Subscription{Name: "测试"}, nodes, "clash", false)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(body), &output); err != nil {
		t.Fatalf("invalid mihomo yaml: %v", err)
	}
	if len(output.Proxies) != 2 {
		t.Fatalf("proxy count = %d, want 2", len(output.Proxies))
	}
	if output.Proxies[0]["name"] != "🇭🇰 香港月抛" || output.Proxies[1]["name"] != "🇭🇰 香港" {
		t.Fatalf("managed names were not propagated: %#v", output.Proxies)
	}
}

func TestMihomoOutputFallsBackToRawURIWhenStoredConfigIsIncomplete(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	nodes := []Node{{
		Name:       "🇯🇵 CF ET",
		Type:       "vless",
		Server:     "saas.sin.fan",
		Port:       443,
		Raw:        "vless://0119068b-0148-47bf-875b-2145040b8174@saas.sin.fan:443?security=tls&type=ws&path=%2Fvless-argo%3Fed%3D2560#%F0%9F%87%AF%F0%9F%87%B5%20CF%20ET",
		ConfigJSON: `{"name":"🇯🇵 CF ET","type":"vless","server":"saas.sin.fan","port":443}`,
		Enabled:    true,
	}}
	body, _, err := renderOutput(ctx, db, Subscription{Name: "测试"}, nodes, "clash", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "0119068b-0148-47bf-875b-2145040b8174") {
		t.Fatalf("raw URI credentials were not used for incomplete config:\n%s", body)
	}
	if err := validateMihomoOutput(body); err != nil {
		t.Fatalf("fallback output is invalid: %v\n%s", err, body)
	}
}

func TestMihomoOutputFallsBackWhenCustomTemplateDuplicatesGeneratedProxyName(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	custom := `proxies:
{{ proxies_yaml }}
  - name: HK 香港月抛
    type: trojan
    server: static.example.com
    port: 443
    password: static-password
proxy-groups:
  - name: 手动选择
    type: select
    proxies:
{{ proxy_names_yaml | indent 6 }}`
	if _, err := db.Exec(`INSERT INTO subscription_templates(id,name,format,content) VALUES('custom','冲突模板','clash',?)`, custom); err != nil {
		t.Fatal(err)
	}
	nodes := []Node{{Name: "HK 香港月抛", Type: "trojan", Server: "node.example.com", Port: 443, ConfigJSON: `{"name":"HK 香港月抛","type":"trojan","server":"node.example.com","port":443,"password":"node-password"}`, Enabled: true}}
	body, _, err := renderOutput(ctx, db, Subscription{Name: "测试", TemplateID: "custom"}, nodes, "clash", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "static.example.com") {
		t.Fatalf("invalid custom template should fall back to the built-in template:\n%s", body)
	}
	if err := validateMihomoOutput(body); err != nil {
		t.Fatalf("fallback output remains invalid: %v\n%s", err, body)
	}
}

func TestMihomoOutputFallsBackWhenPersistedBuiltinTemplateReferencesRemovedNode(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	stale := `proxies:
{{ proxies_yaml }}
proxy-groups:
  - name: 手动
    type: select
    proxies:
      - 🇯🇵 已删除节点`
	if _, err := db.Exec(`INSERT INTO subscription_templates(id,name,format,content,builtin) VALUES('builtin_stale','旧内置模板','clash',?,1)`, stale); err != nil {
		t.Fatal(err)
	}
	nodes := []Node{{Name: "🇭🇰 香港", Type: "vless", Server: "node.example.com", Port: 443, ConfigJSON: `{"name":"🇭🇰 香港","type":"vless","server":"node.example.com","port":443,"uuid":"node"}`, Enabled: true}}
	body, _, err := renderOutput(ctx, db, Subscription{Name: "测试", TemplateID: "builtin_stale"}, nodes, "clash", false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "🇯🇵 已删除节点") {
		t.Fatalf("stale persisted template was not replaced:\n%s", body)
	}
	if err := validateMihomoOutput(body); err != nil {
		t.Fatalf("fallback output is invalid: %v\n%s", err, body)
	}
}

func TestTemplateWriteRejectsInvalidMihomoDefinition(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}

	service := New(config.Config{})
	for name, body := range map[string]string{
		"unknown placeholder":    `{"name":"错误模板","format":"clash","content":"proxies:\n{{ unknown_nodes }}"}`,
		"broken group reference": `{"name":"错误模板","format":"clash","content":"proxies: []\nproxy-groups:\n  - name: 手动\n    type: select\n    proxies: [不存在的节点]"}`,
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/subscription/templates", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			service.createTemplate(rec, req, db)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestJSONServerMetadataDoesNotTurnMissingValuesIntoNilText(t *testing.T) {
	values := map[string]interface{}{"platform": nil, "os": "Ubuntu"}
	if got := jsonString(values, "platform"); got != "" {
		t.Fatalf("platform = %q, want empty", got)
	}
	if got := firstNonEmpty(jsonString(values, "platform"), jsonString(values, "os")); got != "Ubuntu" {
		t.Fatalf("fallback platform = %q, want Ubuntu", got)
	}
}

func TestListServersIncludesLinuxDistributionsAndPreservesPlatform(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE server_accounts (id TEXT PRIMARY KEY, name TEXT NOT NULL, host TEXT NOT NULL, username TEXT, auth_type TEXT, cached_info TEXT, resolved_country TEXT, country TEXT, traffic_limit_bytes INTEGER, status TEXT, last_check_time TEXT, order_index INTEGER DEFAULT 0, created_at TEXT DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct{ id, name, cached string }{
		{"ubuntu", "Ubuntu 主机", `{"platform":"Ubuntu","platform_version":"24.04"}`},
		{"debian", "Debian 主机", `{"platform":"Debian GNU/Linux","platform_version":"12"}`},
		{"windows", "Windows 主机", `{"platform":"Windows","platform_version":"11"}`},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id,name,host,username,auth_type,cached_info) VALUES (?,?,?,'root','password',?)`, row.id, row.name, row.id+".example.com", row.cached); err != nil {
			t.Fatal(err)
		}
		var saved string
		if err := db.QueryRowContext(ctx, `SELECT cached_info FROM server_accounts WHERE id=?`, row.id).Scan(&saved); err != nil || saved != row.cached {
			t.Fatalf("cached_info for %s = %q, err=%v", row.id, saved, err)
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/subscription/servers", nil)
	New(config.Config{}).listServers(recorder, request, db)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"name":"Ubuntu 主机"`) || !strings.Contains(body, `"platform":"Ubuntu"`) || !strings.Contains(body, `"platform_version":"24.04"`) {
		t.Fatalf("Ubuntu host or platform metadata missing: %s", body)
	}
	if !strings.Contains(body, `"name":"Debian 主机"`) {
		t.Fatalf("Debian host missing: %s", body)
	}
	if strings.Contains(body, `"name":"Windows 主机"`) {
		t.Fatalf("Windows host must not be included: %s", body)
	}
}

func TestLoadSubscriptionsDoesNotQueryWhileRowsOpen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions (id, name, public_token, enabled, include_internal_nodes, include_external_nodes) VALUES ('sub_test', '测试订阅', 'token_test', 1, 0, 1)`); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_plans (id, name, total_bytes, cycle_type, cycle_day, rate_limit_enabled, rate_limit_per_minute, selection_mode, include_internal_nodes, include_external_nodes) VALUES ('plan_test', '测试套餐', 4096, 'monthly', 6, 1, 20, 'all', 0, 1)`); err != nil {
		t.Fatalf("insert plan: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE subscription_subscriptions SET plan_id = 'plan_test' WHERE id = 'sub_test'`); err != nil {
		t.Fatalf("bind plan: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_nodes (id, subscription_id, name, enabled) VALUES ('node_test', 'sub_test', '测试节点', 1)`); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_access_logs (subscription_id, public_token, success, status_code) VALUES ('sub_test', 'token_test', 1, 200)`); err != nil {
		t.Fatalf("insert access log: %v", err)
	}

	started := time.Now()
	items, err := loadSubscriptions(ctx, db, "")
	if err != nil {
		t.Fatalf("load subscriptions: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("load subscriptions took %s, likely blocked on nested sqlite queries", elapsed)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].NodeCount != 1 {
		t.Fatalf("NodeCount = %d, want 1", items[0].NodeCount)
	}
	if items[0].AccessCountToday != 1 {
		t.Fatalf("AccessCountToday = %d, want 1", items[0].AccessCountToday)
	}
	if items[0].TotalBytes != 4096 || items[0].CycleDay != 6 {
		t.Fatalf("plan policy was not applied: %#v", items[0])
	}
}

func TestLoadNodesDoesNotQueryQualityWhileRowsOpen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := insertNode(ctx, tx, Node{ID: "node_bound", SubscriptionID: "profile_one", ProfileID: "profile_one", Name: "绑定主机节点", Enabled: true, TrafficServerID: "server_one"}); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	started := time.Now()
	nodes, err := loadNodes(ctx, db, "profile_one", true)
	if err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("load nodes took %s, likely blocked on nested sqlite queries", elapsed)
	}
	if len(nodes) != 1 || nodes[0].TrafficServerID != "server_one" {
		t.Fatalf("nodes = %#v, want bound server id preserved", nodes)
	}
}

func TestImportedNodeDefaultsToExternalUnmanagedTrafficUnavailable(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNode(ctx, tx, Node{ID: "external", SubscriptionID: "profile", Name: "external", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	nodes, err := loadNodes(ctx, db, "profile", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Ownership != "external" || nodes[0].Management != "unmanaged" || nodes[0].TrafficReporting != "unavailable" {
		t.Fatalf("unexpected imported node classification: %#v", nodes)
	}
}

func TestLoadQualityUsesDailyAverageLatency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `CREATE TABLE server_network_quality_samples (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id TEXT NOT NULL,
		target_name TEXT NOT NULL,
		success INTEGER DEFAULT 0,
		latency_ms REAL,
		checked_at TEXT DEFAULT (datetime('now'))
	)`); err != nil {
		t.Fatalf("create quality samples: %v", err)
	}
	statements := []string{
		`INSERT INTO server_network_quality_samples (server_id, target_name, success, latency_ms, checked_at) VALUES ('server_one', '联通', 1, 100, datetime('now', '-5 minutes'))`,
		`INSERT INTO server_network_quality_samples (server_id, target_name, success, latency_ms, checked_at) VALUES ('server_one', '联通', 1, 200, datetime('now', '-10 minutes'))`,
		`INSERT INTO server_network_quality_samples (server_id, target_name, success, latency_ms, checked_at) VALUES ('server_one', '联通', 0, 900, datetime('now', '-15 minutes'))`,
	}
	for _, statement := range statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("insert quality sample: %v", err)
		}
	}

	quality := loadQuality(ctx, db, "server_one")
	if len(quality) != 1 {
		t.Fatalf("quality = %#v, want one target", quality)
	}
	if quality[0].LatencyMS != 150 || quality[0].AvgLatencyMS != 150 {
		t.Fatalf("latency = %.1f avg = %.1f, want daily average 150", quality[0].LatencyMS, quality[0].AvgLatencyMS)
	}
	if quality[0].LossRate < 33 || quality[0].LossRate > 34 {
		t.Fatalf("loss rate = %.1f, want about 33.3", quality[0].LossRate)
	}
}

func TestParseImportTextDecodesBase64Subscription(t *testing.T) {
	raw := "vmess://example-one#节点一\ntrojan://password@example.com:443#节点二\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(raw))

	nodes := parseImportText(encoded)
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2", len(nodes))
	}
	if nodes[0].Type != "vmess" || nodes[1].Type != "trojan" {
		t.Fatalf("types = %q, %q", nodes[0].Type, nodes[1].Type)
	}
	if nodes[1].Server != "example.com" || nodes[1].Port != 443 {
		t.Fatalf("trojan endpoint = %s:%d", nodes[1].Server, nodes[1].Port)
	}
}

func TestParseImportTextPreservesURIClientFields(t *testing.T) {
	raw := strings.Join([]string{
		"vless://0119068b-0148-47bf-875b-2145040b8174@saas.sin.fan:443?security=tls&type=ws&fp=chrome&path=/group/live/intro&encryption=none&headerType=none&sni=egtu.pages.dev#JP%20CF%20ET",
		"hysteria2://3Aq8A2om3bNPoY7C@189.1.217.109:1005/?insecure=1&sni=cdn.jsdelivr.net#HK%20香港",
		"trojan://YPMVpLJMZxDHoMChEzY5-nU3xQPALdyV@tjw.rois.eu.org:56501?type=tcp&sni=tjw.rois.eu.orgh3,h2,http/1.1#US%20洛杉矶",
	}, "\n")

	nodes := parseImportText(raw)
	if len(nodes) != 3 {
		t.Fatalf("len(nodes) = %d, want 3", len(nodes))
	}
	if nodes[0].CountryCode != "JP" || nodes[0].Name != "JP CF ET" {
		t.Fatalf("vless name/country not normalized: %#v", nodes[0])
	}
	if nodes[1].CountryCode != "HK" || nodes[1].Name != "HK 香港" {
		t.Fatalf("hysteria2 name/country not normalized: %#v", nodes[1])
	}

	var vless map[string]interface{}
	if err := json.Unmarshal([]byte(nodes[0].ConfigJSON), &vless); err != nil {
		t.Fatalf("vless config json: %v", err)
	}
	if vless["uuid"] != "0119068b-0148-47bf-875b-2145040b8174" || vless["network"] != "ws" || vless["tls"] != true || vless["servername"] != "egtu.pages.dev" || vless["sni"] != "egtu.pages.dev" {
		t.Fatalf("vless config lost fields: %#v", vless)
	}
	wsOpts, ok := vless["ws-opts"].(map[string]interface{})
	if !ok || wsOpts["path"] != "/group/live/intro" {
		t.Fatalf("vless ws opts lost fields: %#v", vless["ws-opts"])
	}
	headers, ok := wsOpts["headers"].(map[string]interface{})
	if !ok || headers["Host"] != "egtu.pages.dev" {
		t.Fatalf("vless ws host header lost fields: %#v", wsOpts["headers"])
	}

	var hy2 map[string]interface{}
	if err := json.Unmarshal([]byte(nodes[1].ConfigJSON), &hy2); err != nil {
		t.Fatalf("hysteria2 config json: %v", err)
	}
	if hy2["password"] != "3Aq8A2om3bNPoY7C" || hy2["sni"] != "cdn.jsdelivr.net" || hy2["skip-cert-verify"] != true {
		t.Fatalf("hysteria2 config lost fields: %#v", hy2)
	}

	var trojan map[string]interface{}
	if err := json.Unmarshal([]byte(nodes[2].ConfigJSON), &trojan); err != nil {
		t.Fatalf("trojan config json: %v", err)
	}
	if trojan["password"] != "YPMVpLJMZxDHoMChEzY5-nU3xQPALdyV" || trojan["sni"] != "tjw.rois.eu.org" || trojan["tls"] != true || trojan["network"] != "tcp" {
		t.Fatalf("trojan config lost fields: %#v", trojan)
	}
	if alpn, ok := trojan["alpn"].([]interface{}); !ok || len(alpn) != 3 {
		t.Fatalf("trojan alpn lost fields: %#v", trojan["alpn"])
	}
}

func TestParseImportTextPreservesUnescapedFragmentNames(t *testing.T) {
	raw := strings.Join([]string{
		"hysteria2://3Aq8A2om3bNPoY7C@189.1.217.109:1005/?insecure=1&sni=cdn.jsdelivr.net#🇭🇰 香港",
		"hysteria2://yNbE8RV2zdIojLVS@45.32.157.55:4534/?insecure=1&sni=cdn.jsdelivr.net#🇩🇪 法兰克福",
		"vless://65f52614-1632-45c6-9809-72c30c4e16bd@saas.sin.fan:443?security=tls&type=ws&fp=chrome&path=/vless-argo?ed=2560&encryption=none&headerType=none#🇯🇵 日本",
	}, "\n")

	nodes := parseImportText(raw)
	if len(nodes) != 3 {
		t.Fatalf("len(nodes) = %d, want 3: %#v", len(nodes), nodes)
	}
	wants := []struct {
		code string
		name string
	}{
		{"HK", "🇭🇰 香港"},
		{"DE", "🇩🇪 法兰克福"},
		{"JP", "🇯🇵 日本"},
	}
	for i, want := range wants {
		if nodes[i].CountryCode != want.code || nodes[i].Name != want.name {
			t.Fatalf("node %d = country %q name %q, want country %q name %q", i, nodes[i].CountryCode, nodes[i].Name, want.code, want.name)
		}
	}
}

func TestParseImportTextReadsClashProxiesOnly(t *testing.T) {
	raw := `
proxies:
  - {"name":"JP CF","type":"vless","server":"saas.example.com","port":443,"uuid":"0119068b-0148-47bf-875b-2145040b8174","network":"ws","ws-opts":{"path":"/group/live/intro","headers":{"Host":"edge.example.com"}}}
  # - {"name":"Disabled","type":"trojan","server":"disabled.example.com","port":443,"password":"secret"}
  - name: London
    type: hysteria2
    server: 185.168.194.90
    port: 5427
    password: secret
rule-providers:
  reject:
    type: http
    url: https://raw.example.com/clash-rules/reject.txt
`

	nodes := parseImportText(raw)
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2: %#v", len(nodes), nodes)
	}
	if nodes[0].Name != "JP CF" || nodes[0].Type != "vless" || nodes[0].Server != "saas.example.com" || nodes[0].Port != 443 {
		t.Fatalf("first node = %#v", nodes[0])
	}
	if nodes[1].Name != "London" || nodes[1].Type != "hysteria2" || nodes[1].Server != "185.168.194.90" || nodes[1].Port != 5427 {
		t.Fatalf("second node = %#v", nodes[1])
	}
	for _, node := range nodes {
		if strings.Contains(node.Server, "raw.example.com") || strings.Contains(node.Raw, "raw.example.com") {
			t.Fatalf("rule provider URL was parsed as node: %#v", node)
		}
		if node.ConfigJSON == "" {
			t.Fatalf("ConfigJSON is empty for %#v", node)
		}
	}
}

func TestParseClashProxyNodesPreservesEmojiNamesAndClientFields(t *testing.T) {
	raw := `
proxies:
  - {"name":"🇯🇵 CF ET","type":"vless","server":"saas.sin.fan","port":443,"uuid":"0119068b-0148-47bf-875b-2145040b8174","encryption":"none","tls":true,"sni":"egtu.pages.dev","client-fingerprint":"chrome","network":"ws","ws-opts":{"path":"/group/live/intro","headers":{"Host":"egtu.pages.dev"}}}
  - {"name":"🇺🇸 洛杉矶","type":"trojan","server":"tjw.rois.eu.org","port":56501,"network":"tcp","udp":true,"password":"secret","tls":true,"sni":"tjw.rois.eu.org","alpn":["h3","h2","http/1.1"],"client-fingerprint":"chrome"}
`

	nodes := parseImportText(raw)
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2: %#v", len(nodes), nodes)
	}
	if nodes[0].CountryCode != "JP" || nodes[0].Name != "🇯🇵 CF ET" {
		t.Fatalf("first node country/name = %q/%q", nodes[0].CountryCode, nodes[0].Name)
	}
	if nodes[1].CountryCode != "US" || nodes[1].Name != "🇺🇸 洛杉矶" {
		t.Fatalf("second node country/name = %q/%q", nodes[1].CountryCode, nodes[1].Name)
	}

	body := "proxies:\n" + proxiesYAML(nodes, 2) + "\n"
	var parsed map[string][]map[string]interface{}
	if err := yaml.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("exported yaml should parse: %v\n%s", err, body)
	}
	proxies := parsed["proxies"]
	if len(proxies) != 2 {
		t.Fatalf("len(proxies) = %d, want 2: %#v", len(proxies), proxies)
	}
	wsOpts, _ := proxies[0]["ws-opts"].(map[string]interface{})
	headers, _ := wsOpts["headers"].(map[string]interface{})
	if proxies[0]["name"] != "🇯🇵 CF ET" || proxies[0]["sni"] != "egtu.pages.dev" || headers["Host"] != "egtu.pages.dev" {
		t.Fatalf("exported vless yaml lost display/client fields: %#v", proxies[0])
	}
	if proxies[1]["name"] != "🇺🇸 洛杉矶" || proxies[1]["tls"] != true || proxies[1]["client-fingerprint"] != "chrome" {
		t.Fatalf("exported trojan yaml lost display/client fields: %#v", proxies[1])
	}
}

func TestStableProxyNamesOnlyIncludesExplicitStableNodes(t *testing.T) {
	nodes := []Node{
		{Name: "🇺🇸 洛杉矶", Enabled: true, Stable: true},
		{Name: "🇯🇵 日本", Enabled: true, Stable: false},
	}

	body := stableProxyNamesYAML(nodes, 0)
	var names []string
	if err := yaml.Unmarshal([]byte(strings.TrimSpace(body)), &names); err != nil {
		t.Fatalf("stable proxy names should parse: %v\n%s", err, body)
	}
	joined := strings.Join(names, "\n")
	if !strings.Contains(joined, "🇺🇸 洛杉矶") {
		t.Fatalf("stable node missing from stable proxy names: %#v", names)
	}
	if strings.Contains(joined, "🇯🇵 日本") {
		t.Fatalf("non-stable node should not be in stable proxy names:\n%s", body)
	}
}

func TestStableProxyNamesFallsBackToDirect(t *testing.T) {
	body := stableProxyNamesYAML([]Node{{Name: "普通节点", Enabled: true}}, 0)
	var names []string
	if err := yaml.Unmarshal([]byte(strings.TrimSpace(body)), &names); err != nil {
		t.Fatalf("stable proxy names should parse: %v\n%s", err, body)
	}
	if len(names) != 1 || names[0] != "DIRECT" {
		t.Fatalf("stable group without explicitly stable nodes = %#v, want DIRECT", names)
	}
}

func TestProxiesYAMLRendersClientCompatibleList(t *testing.T) {
	nodes := parseImportText("vless://0119068b-0148-47bf-875b-2145040b8174@saas.sin.fan:443?security=tls&type=ws&fp=chrome&path=/group/live/intro&encryption=none&sni=egtu.pages.dev#JP%20CF%20ET")
	if len(nodes) != 1 {
		t.Fatalf("len(nodes) = %d, want 1", len(nodes))
	}
	body := "proxies:\n" + proxiesYAML(nodes, 2) + "\n"
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("rendered proxies yaml is invalid: %v\n%s", err, body)
	}
	proxies, ok := parsed["proxies"].([]interface{})
	if !ok || len(proxies) != 1 {
		t.Fatalf("proxies = %#v", parsed["proxies"])
	}
	proxy := normalizeStringMap(proxies[0]).(map[string]interface{})
	if proxy["uuid"] != "0119068b-0148-47bf-875b-2145040b8174" || proxy["network"] != "ws" {
		t.Fatalf("rendered proxy lost fields: %#v", proxy)
	}
}

func TestEnsureSchemaMigratesLegacySubscriptionsToProfiles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	legacyStatements := []string{
		`CREATE TABLE subscription_subscriptions (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			remark TEXT,
			enabled INTEGER DEFAULT 1,
			public_token TEXT NOT NULL UNIQUE,
			template_id TEXT DEFAULT 'builtin_mihomo_default',
			traffic_source TEXT DEFAULT 'manual',
			traffic_server_id TEXT,
			upstream_url TEXT,
			upstream_enabled INTEGER DEFAULT 0,
			upstream_refresh_hours INTEGER DEFAULT 24,
			upstream_status TEXT,
			upstream_last_error TEXT,
			upstream_last_refresh_at TEXT,
			upstream_userinfo TEXT,
			total_bytes INTEGER DEFAULT 0,
			manual_upload_bytes INTEGER DEFAULT 0,
			manual_download_bytes INTEGER DEFAULT 0,
			expire_at TEXT,
			cycle_type TEXT DEFAULT 'none',
			cycle_day INTEGER DEFAULT 1,
			cycle_start TEXT,
			cycle_end TEXT,
			baseline_upload_bytes INTEGER DEFAULT 0,
			baseline_download_bytes INTEGER DEFAULT 0,
			rate_limit_enabled INTEGER DEFAULT 1,
			rate_limit_per_minute INTEGER DEFAULT 30,
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`CREATE TABLE subscription_nodes (
			id TEXT PRIMARY KEY,
			subscription_id TEXT NOT NULL,
			name TEXT NOT NULL,
			type TEXT,
			server TEXT,
			port INTEGER DEFAULT 0,
			country_code TEXT,
			location TEXT,
			tags TEXT,
			enabled INTEGER DEFAULT 1,
			stable INTEGER DEFAULT 0,
			sort_order INTEGER DEFAULT 0,
			raw_encrypted TEXT,
			config_encrypted TEXT,
			fingerprint TEXT,
			source TEXT DEFAULT 'manual',
			created_at TEXT DEFAULT (datetime('now')),
			updated_at TEXT DEFAULT (datetime('now'))
		)`,
		`INSERT INTO subscription_subscriptions (id, name, public_token, upstream_url, upstream_enabled, upstream_userinfo, total_bytes) VALUES ('sub_legacy', '旧订阅', 'tok_legacy', 'https://example.com/sub', 1, 'upload=1; download=2; total=3; expire=4', 100)`,
		`INSERT INTO subscription_nodes (id, subscription_id, name, enabled) VALUES ('node_legacy', 'sub_legacy', '旧节点', 1)`,
	}
	for _, statement := range legacyStatements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("exec legacy statement: %v", err)
		}
	}

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	var profileID string
	if err := db.QueryRowContext(ctx, `SELECT profile_id FROM subscription_subscriptions WHERE id = 'sub_legacy'`).Scan(&profileID); err != nil {
		t.Fatalf("load subscription profile id: %v", err)
	}
	if profileID != "sub_legacy" {
		t.Fatalf("profileID = %q, want sub_legacy", profileID)
	}

	var profileName string
	if err := db.QueryRowContext(ctx, `SELECT name FROM subscription_profiles WHERE id = 'sub_legacy'`).Scan(&profileName); err != nil {
		t.Fatalf("load migrated profile: %v", err)
	}
	if profileName != "旧订阅" {
		t.Fatalf("profileName = %q, want 旧订阅", profileName)
	}

	var upstreamURL, userinfo string
	if err := db.QueryRowContext(ctx, `SELECT url, userinfo FROM subscription_upstreams WHERE profile_id = 'sub_legacy'`).Scan(&upstreamURL, &userinfo); err != nil {
		t.Fatalf("load migrated upstream: %v", err)
	}
	if upstreamURL != "https://example.com/sub" || !strings.Contains(userinfo, "total=3") {
		t.Fatalf("upstream = %q userinfo = %q", upstreamURL, userinfo)
	}

	var nodeProfileID string
	if err := db.QueryRowContext(ctx, `SELECT profile_id FROM subscription_nodes WHERE id = 'node_legacy'`).Scan(&nodeProfileID); err != nil {
		t.Fatalf("load node profile id: %v", err)
	}
	if nodeProfileID != "sub_legacy" {
		t.Fatalf("nodeProfileID = %q, want sub_legacy", nodeProfileID)
	}
}

func TestEnsureSchemaAddsProfileOwnershipColumnsToLegacyDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.ExecContext(ctx, `CREATE TABLE subscription_profiles (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		remark TEXT,
		enabled INTEGER DEFAULT 1,
		template_id TEXT DEFAULT 'builtin_mihomo_default',
		traffic_source TEXT DEFAULT 'manual',
		traffic_server_id TEXT,
		total_bytes INTEGER DEFAULT 0,
		manual_upload_bytes INTEGER DEFAULT 0,
		manual_download_bytes INTEGER DEFAULT 0,
		expire_at TEXT,
		cycle_type TEXT DEFAULT 'none',
		cycle_day INTEGER DEFAULT 1,
		cycle_start TEXT,
		cycle_end TEXT,
		baseline_upload_bytes INTEGER DEFAULT 0,
		baseline_download_bytes INTEGER DEFAULT 0,
		rate_limit_enabled INTEGER DEFAULT 1,
		rate_limit_per_minute INTEGER DEFAULT 30,
		node_filter_tags TEXT DEFAULT '',
		sort_order INTEGER DEFAULT 0,
		created_at TEXT DEFAULT (datetime('now')),
		updated_at TEXT DEFAULT (datetime('now'))
	)`)
	if err != nil {
		t.Fatalf("create legacy subscription_profiles: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_profiles (id, name) VALUES ('legacy_pool', '旧节点池')`); err != nil {
		t.Fatalf("insert legacy profile: %v", err)
	}

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	var ownership, management, trafficReporting string
	if err := db.QueryRowContext(ctx, `SELECT ownership, management, traffic_reporting FROM subscription_profiles WHERE id='legacy_pool'`).Scan(&ownership, &management, &trafficReporting); err != nil {
		t.Fatalf("load migrated profile ownership: %v", err)
	}
	if ownership != "external" || management != "unmanaged" || trafficReporting != "unavailable" {
		t.Fatalf("profile ownership defaults = %q/%q/%q", ownership, management, trafficReporting)
	}
}

func TestSubscriptionTokenRendersNodesFromProfile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions (id, profile_id, name, public_token, enabled, template_id) VALUES ('link_one', 'profile_one', '公开链接', 'token_one', 1, 'builtin_raw_uri')`); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := insertNode(ctx, tx, Node{SubscriptionID: "profile_one", ProfileID: "profile_one", Name: "节点一", Type: "trojan", Server: "example.com", Port: 443, Raw: "trojan://password@example.com:443#node-one", Enabled: true}); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	subs, err := loadSubscriptionByToken(ctx, db, "token_one")
	if err != nil {
		t.Fatalf("load subscription by token: %v", err)
	}
	if len(subs) != 1 || subs[0].ProfileID != "profile_one" {
		t.Fatalf("loaded subscription = %#v", subs)
	}
	nodes, err := loadNodes(ctx, db, subs[0].ProfileID, true)
	if err != nil {
		t.Fatalf("load profile nodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("len(nodes) = %d, want 1", len(nodes))
	}
	body, contentType, err := renderOutput(ctx, db, subs[0], nodes, "raw", false)
	if err != nil {
		t.Fatalf("render output: %v", err)
	}
	if contentType != "text/plain; charset=utf-8" || !strings.Contains(body, "trojan://password@example.com:443#node-one") {
		t.Fatalf("rendered %q as %q", body, contentType)
	}
}

func TestPublicSubscriptionIncludesClientProfileNameHeaders(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dataDir := t.TempDir()
	svc := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	db, err := svc.open(ctx)
	if err != nil {
		t.Fatalf("open service db: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions (id, profile_id, name, public_token, enabled, template_id) VALUES ('link_one', 'profile_one', '香港 订阅', 'token_one', 1, 'builtin_mihomo_default')`); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := insertNode(ctx, tx, Node{SubscriptionID: "profile_one", ProfileID: "profile_one", Name: "HK 节点一", Type: "trojan", Server: "example.com", Port: 443, Raw: "trojan://password@example.com:443#node-one", Enabled: true}); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/sub/token_one", nil)
	rec := httptest.NewRecorder()
	svc.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	wantTitle := "base64:" + base64.StdEncoding.EncodeToString([]byte("香港 订阅"))
	if got := rec.Header().Get("Profile-Title"); got != wantTitle {
		t.Fatalf("Profile-Title = %q, want %q", got, wantTitle)
	}
	contentDisposition := rec.Header().Get("Content-Disposition")
	if !strings.Contains(contentDisposition, "attachment") || !strings.Contains(contentDisposition, "filename*=") {
		t.Fatalf("unexpected Content-Disposition: %q", contentDisposition)
	}
	if strings.Contains(contentDisposition, ".yaml") || strings.Contains(contentDisposition, ".txt") {
		t.Fatalf("Content-Disposition should not include a file extension: %q", contentDisposition)
	}
}

func TestMihomoRenderFallsBackWhenBoundTemplateIsEmpty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_templates (id, name, format, content) VALUES ('empty_tpl', '空模板', 'clash', '')`); err != nil {
		t.Fatalf("insert template: %v", err)
	}
	sub := Subscription{ID: "link_one", Name: "DSUK", TemplateID: "empty_tpl", Enabled: true}
	nodes := []Node{{Name: "HK 节点一", Type: "trojan", Server: "example.com", Port: 443, Raw: "trojan://password@example.com:443#node-one", Enabled: true}}

	body, contentType, err := renderOutput(ctx, db, sub, nodes, "clash", false)
	if err != nil {
		t.Fatalf("render output: %v", err)
	}
	if contentType != "text/yaml; charset=utf-8" {
		t.Fatalf("contentType = %q", contentType)
	}
	if !strings.Contains(body, "proxies:") || !strings.Contains(body, "HK 节点一") {
		t.Fatalf("rendered body did not fall back to default template:\n%s", body)
	}
}

func TestMihomoRenderUsesEmptyProxyListWhenNoNodes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	sub := Subscription{ID: "link_one", Name: "DSUK", TemplateID: defaultTemplateID, Enabled: true}

	body, _, err := renderOutput(ctx, db, sub, nil, "clash", false)
	if err != nil {
		t.Fatalf("render output: %v", err)
	}
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("rendered invalid yaml: %v\n%s", err, body)
	}
	proxies, ok := parsed["proxies"].([]interface{})
	if !ok {
		t.Fatalf("proxies = %#v, want empty list", parsed["proxies"])
	}
	if len(proxies) != 0 {
		t.Fatalf("len(proxies) = %d, want 0", len(proxies))
	}
}

func TestRenderOutputSkipsDuplicateAndBrokenNodes(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	nodes := []Node{
		{Name: "节点一", Type: "trojan", Server: "example.com", Port: 443, Raw: "trojan://password@example.com:443#节点一", Enabled: true},
		{Name: "节点一-重复", Type: "trojan", Server: "example.com", Port: 443, Raw: "trojan://password@example.com:443#节点一-重复", Enabled: true},
		{Name: "坏节点", Type: "trojan", Raw: "not-a-uri", Enabled: true},
	}
	rawBody, _, err := renderOutput(ctx, db, Subscription{Name: "测试", TemplateID: rawTemplateID}, nodes, "raw", false)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(rawBody), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "trojan://password@example.com:443#") {
		t.Fatalf("raw output still contains duplicates or invalid nodes: %q", rawBody)
	}
	clashBody, _, err := renderOutput(ctx, db, Subscription{Name: "测试"}, nodes, "clash", false)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(clashBody), &parsed); err != nil {
		t.Fatalf("invalid yaml: %v\n%s", err, clashBody)
	}
	if len(parsed.Proxies) != 1 {
		t.Fatalf("proxies = %#v, want single valid proxy", parsed.Proxies)
	}
}

func TestBlockedRenderReturnsEmptySubscriptionWithoutWarningComment(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	body, _, err := renderOutput(ctx, db, Subscription{Name: "测试"}, []Node{
		{Name: "节点一", Type: "trojan", Server: "example.com", Port: 443, Raw: "trojan://password@example.com:443#节点一", Enabled: true},
	}, "clash", true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "Nodes are hidden") || strings.Contains(body, "Subscription is") {
		t.Fatalf("blocked output should not include warning comment:\n%s", body)
	}
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("invalid blocked yaml: %v\n%s", err, body)
	}
	proxies, ok := parsed["proxies"].([]interface{})
	if !ok || len(proxies) != 0 {
		t.Fatalf("blocked subscription should expose no proxies: %#v", parsed["proxies"])
	}
}

func TestPublicSubscriptionHidesNodesWhenHostQuotaIsExhausted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dataDir := t.TempDir()
	svc := New(config.Config{DataDir: dataDir, DBName: "data.db"})
	db, err := svc.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	raw := "vless://00000000-0000-4000-8000-000000000001@origin.example.com:443?security=reality&pbk=pubkey123&sid=abcd&sni=origin.example.com#节点一"
	encrypted, err := secure.SecureEncrypt(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE server_accounts (id TEXT PRIMARY KEY, name TEXT, host TEXT, cached_info TEXT, traffic_limit_bytes INTEGER DEFAULT 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, cached_info, traffic_limit_bytes) VALUES ('server-one', 'Server One', 'one.example.com', '{"network":{"rx_total_bytes":600,"tx_total_bytes":500}}', 1000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions (id, profile_id, name, public_token, enabled, template_id, include_internal_nodes, include_external_nodes) VALUES ('link_one', 'profile_one', '公开链接', 'token_one', 1, 'builtin_raw_uri', 1, 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,enabled,publishable,apply_status,access_mode,tunnel_hostname) VALUES('managed-one','server-one','节点一','vless-reality','origin.example.com',443,'tcp','',?,1,1,'running','direct','')`, encrypted); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/sub/token_one", nil)
	rec := httptest.NewRecorder()
	svc.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "" {
		t.Fatalf("exhausted host nodes should be hidden from raw subscription: %q", rec.Body.String())
	}
}

func TestLoadProfilesReturnsLibrariesWithCountsAndUpstream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := upsertProfile(ctx, db, "profile_one", Subscription{Name: "DSUK", Enabled: true, UpstreamURL: "https://example.com/sub", UpstreamEnabled: true, RateLimitEnabled: true}, defaultTemplateID, "manual", "none", 1, 30, "explicit", false); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	if err := upsertDefaultUpstream(ctx, db, "profile_one", Subscription{UpstreamURL: "https://example.com/sub", UpstreamEnabled: true}, 12); err != nil {
		t.Fatalf("upsert upstream: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions (id, profile_id, name, public_token, enabled) VALUES ('link_one', 'profile_one', '公开链接', 'token_one', 1)`); err != nil {
		t.Fatalf("insert subscription link: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := insertNode(ctx, tx, Node{SubscriptionID: "profile_one", ProfileID: "profile_one", Name: "节点一", Type: "trojan", Server: "example.com", Port: 443, Enabled: true}); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	items, err := loadProfiles(ctx, db, "")
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ID != "profile_one" || items[0].Name != "DSUK" {
		t.Fatalf("profile = %#v", items[0])
	}
	if items[0].NodeCount != 1 || items[0].SubscriptionCount != 1 {
		t.Fatalf("counts = nodes %d subscriptions %d, want 1/1", items[0].NodeCount, items[0].SubscriptionCount)
	}
	if items[0].UpstreamURL != "https://example.com/sub" || items[0].UpstreamRefreshHours != 12 || !items[0].UpstreamEnabled {
		t.Fatalf("upstream = %#v", items[0])
	}
}

func TestDeleteProfileBlocksWhenNodesOrLinksExist(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if err := upsertProfile(ctx, db, "profile_busy", Subscription{Name: "繁忙节点库", Enabled: true}, defaultTemplateID, "manual", "none", 1, 30, "explicit", false); err != nil {
		t.Fatalf("upsert profile: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions (id, profile_id, name, public_token, enabled) VALUES ('link_busy', 'profile_busy', '公开链接', 'token_busy', 1)`); err != nil {
		t.Fatalf("insert subscription link: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_nodes (id, subscription_id, profile_id, name, enabled) VALUES ('node_busy', 'link_busy', 'profile_busy', '繁忙节点', 1)`); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_access_logs (subscription_id, public_token, success, status_code) VALUES ('link_busy', 'token_busy', 1, 200)`); err != nil {
		t.Fatalf("insert access log: %v", err)
	}

	service := &Service{}
	req := httptest.NewRequest(http.MethodDelete, "/api/subscription/profiles/profile_busy", nil)
	rec := httptest.NewRecorder()
	service.deleteProfile(rec, req, db, "profile_busy")

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_profiles WHERE id = 'profile_busy'`).Scan(&count); err != nil {
		t.Fatalf("count profile: %v", err)
	}
	if count != 1 {
		t.Fatalf("profile count = %d, want 1", count)
	}

	forceReq := httptest.NewRequest(http.MethodDelete, "/api/subscription/profiles/profile_busy?force=1", nil)
	forceRec := httptest.NewRecorder()
	service.deleteProfile(forceRec, forceReq, db, "profile_busy")
	if forceRec.Code != http.StatusOK {
		t.Fatalf("force status = %d, want %d body=%s", forceRec.Code, http.StatusOK, forceRec.Body.String())
	}
	for _, check := range []struct {
		name  string
		query string
	}{
		{"profiles", `SELECT COUNT(*) FROM subscription_profiles WHERE id = 'profile_busy'`},
		{"links", `SELECT COUNT(*) FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = 'profile_busy'`},
		{"nodes", `SELECT COUNT(*) FROM subscription_nodes WHERE COALESCE(profile_id, subscription_id) = 'profile_busy'`},
		{"logs", `SELECT COUNT(*) FROM subscription_access_logs WHERE subscription_id = 'link_busy'`},
	} {
		var remaining int
		if err := db.QueryRowContext(ctx, check.query).Scan(&remaining); err != nil {
			t.Fatalf("count %s: %v", check.name, err)
		}
		if remaining != 0 {
			t.Fatalf("%s remaining = %d, want 0", check.name, remaining)
		}
	}
}

func TestComputeTrafficDoesNotApplyExternalUpstreamUsageToSubscription(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_upstreams (id, profile_id, name, url, enabled, userinfo) VALUES ('up_profile_one', 'profile_one', '默认上游', 'https://example.com/sub', 1, 'upload=11; download=22; total=100; expire=1234')`); err != nil {
		t.Fatalf("insert upstream: %v", err)
	}

	info := computeTraffic(ctx, db, Subscription{ID: "link_one", ProfileID: "profile_one", TrafficSource: "upstream"})
	if info.Upload != 0 || info.Download != 0 || info.Total != 0 || info.Source != "panel" || info.MeteringStatus != "unavailable" {
		t.Fatalf("traffic = %#v", info)
	}
}

func TestPlanOverridesSubscriptionPolicy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO subscription_plans(id,name,total_bytes,cycle_type,cycle_day,rate_limit_enabled,rate_limit_per_minute,selection_mode,include_internal_nodes,include_external_nodes) VALUES('plan_one','基础套餐',1000,'monthly',8,1,12,'explicit',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES('internal_one','host_one','内部节点','vless-reality','internal.example',45654,'tcp','','')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO subscription_nodes(id,subscription_id,name,type,server,port) VALUES('external_one','library_one','外部节点','vless','external.example',443)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES('plan_one','internal_one','internal'),('plan_one','external_one','external')`); err != nil {
		t.Fatal(err)
	}
	sub := Subscription{PlanID: "plan_one", TotalBytes: 9, CycleType: "none", NodeFilterIDs: []string{"wrong"}}
	applyPlanToSubscription(ctx, db, &sub)
	if sub.TotalBytes != 1000 || sub.CycleType != "monthly" || sub.CycleDay != 8 || sub.RateLimitPerMinute != 12 || len(sub.NodeFilterIDs) != 2 || !sub.IncludeInternalNodes || !sub.IncludeExternalNodes {
		t.Fatalf("plan policy not applied: %#v", sub)
	}
}

func TestPlanNodeMigrationDropsDeletedLegacyReferences(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted) VALUES('managed_last','host_one','最后节点','vless-reality','node.example',45654,'tcp','','')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_plans(id,name,node_ids,selection_mode) VALUES('plan_legacy','历史套餐','["managed_last","already_deleted"]','explicit')`); err != nil {
		t.Fatal(err)
	}

	if err := migratePlanNodeRelations(ctx, db); err != nil {
		t.Fatalf("initial legacy migration: %v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_plan_nodes WHERE plan_id='plan_legacy'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migrated relation count=%d err=%v", count, err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM managed_proxy_nodes WHERE id='managed_last'`); err != nil {
		t.Fatal(err)
	}
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("schema must remain available after deleting the last plan node: %v", err)
	}
	var legacy string
	if err := db.QueryRowContext(ctx, `SELECT node_ids FROM subscription_plans WHERE id='plan_legacy'`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	if legacy != "" {
		t.Fatalf("legacy node_ids were not retired: %q", legacy)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_plan_nodes WHERE plan_id='plan_legacy'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted node relation count=%d err=%v", count, err)
	}
}

func TestExplicitPlanWithNoNodesPublishesNoNodes(t *testing.T) {
	nodes := []Node{{ID: "one", Name: "节点一"}}
	if got := filterPlanNodesByIDsForSource(nodes, nil, "explicit"); len(got) != 0 {
		t.Fatalf("explicit empty selection published nodes: %#v", got)
	}
	if got := filterPlanNodesByIDsForSource(nodes, nil, "all"); len(got) != 1 {
		t.Fatalf("all selection did not publish all nodes: %#v", got)
	}
}

func TestDeleteExternalNodeRemovesPlanMembership(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plans(id,name) VALUES('plan','套餐')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_nodes(id,subscription_id,name) VALUES('external','library','外部节点')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES('plan','external','external')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	New(config.Config{}).deleteNode(rec, httptest.NewRequest(http.MethodDelete, "/api/subscription/nodes/external", nil), db, "external")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_plan_nodes WHERE node_id='external'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("membership count=%d err=%v", count, err)
	}
}

func TestReplacingImportedNodePreservesPlanMembershipWhenEndpointChanges(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_profiles(id,name) VALUES('library','节点库')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plans(id,name) VALUES('plan','套餐')`); err != nil {
		t.Fatal(err)
	}
	old := Node{ID: "external", SubscriptionID: "library", ProfileID: "library", Name: "香港节点", Type: "vless", Server: "old.example.com", Port: 443, Raw: "vless://old@example.com:443#香港节点", Enabled: true, Source: "manual"}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := insertNode(ctx, tx, old); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES('plan','external','external')`); err != nil {
		t.Fatal(err)
	}

	body := `{"subscription_id":"library","replace":true,"nodes":[{"name":"香港节点","type":"vless","server":"new.example.com","port":8443,"raw":"vless://new@example.com:8443#香港节点","enabled":true}]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/subscription/import/commit", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	New(config.Config{}).importCommit(rec, req, db)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var nodeID, server string
	var port int
	if err := db.QueryRow(`SELECT id,server,port FROM subscription_nodes WHERE COALESCE(profile_id,subscription_id)='library'`).Scan(&nodeID, &server, &port); err != nil {
		t.Fatal(err)
	}
	if nodeID != "external" || server != "new.example.com" || port != 8443 {
		t.Fatalf("replacement node id=%q server=%q port=%d", nodeID, server, port)
	}
	var relationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_plan_nodes WHERE plan_id='plan' AND node_id='external' AND source='external'`).Scan(&relationCount); err != nil || relationCount != 1 {
		t.Fatalf("membership count=%d err=%v", relationCount, err)
	}
}

func TestRefreshingManagedSourcePreservesPlanMembershipWhenEndpointChanges(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_profiles(id,name) VALUES('library','节点库')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plans(id,name) VALUES('plan','套餐')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	old := Node{ID: "managed", SubscriptionID: "library", ProfileID: "library", Name: "新加坡节点", Type: "trojan", Server: "old.example.com", Port: 443, Raw: "trojan://old@example.com:443#新加坡节点", Enabled: true, Source: "managed"}
	if err := insertNode(ctx, tx, old); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES('plan','managed','external')`); err != nil {
		t.Fatal(err)
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	incoming := []Node{{Name: "新加坡节点", Type: "trojan", Server: "new.example.com", Port: 8443, Raw: "trojan://new@example.com:8443#新加坡节点", Enabled: true}}
	if err := mergeManagedNodes(ctx, tx, "library", incoming); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var nodeID, server string
	if err := db.QueryRow(`SELECT id,server FROM subscription_nodes WHERE COALESCE(profile_id,subscription_id)='library'`).Scan(&nodeID, &server); err != nil {
		t.Fatal(err)
	}
	if nodeID != "managed" || server != "new.example.com" {
		t.Fatalf("refreshed node id=%q server=%q", nodeID, server)
	}
	var relationCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_plan_nodes WHERE plan_id='plan' AND node_id='managed'`).Scan(&relationCount); err != nil || relationCount != 1 {
		t.Fatalf("membership count=%d err=%v", relationCount, err)
	}
}

func TestEnsureBuiltinsRefreshesStaleBuiltinTemplate(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_templates(id,name,format,content,builtin) VALUES(?,?,?,?,1)`, defaultTemplateID, "旧模板", "clash", "global-client-fingerprint: chrome"); err != nil {
		t.Fatal(err)
	}
	if err := ensureBuiltins(ctx, db, false); err != nil {
		t.Fatal(err)
	}
	var content string
	if err := db.QueryRow(`SELECT content FROM subscription_templates WHERE id=?`, defaultTemplateID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(content, "global-client-fingerprint") || !strings.Contains(content, "{{ proxies_yaml }}") {
		t.Fatalf("builtin template was not refreshed: %q", content)
	}
}

func TestDeleteTemplateRejectsReferences(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_templates(id,name,format,content) VALUES('custom','自定义','clash','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_profiles(id,name,template_id) VALUES('profile','节点池','custom')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	New(config.Config{}).deleteTemplate(rec, httptest.NewRequest(http.MethodDelete, "/api/subscription/templates/custom", nil), db, "custom")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_templates WHERE id='custom'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("referenced template count=%d err=%v", count, err)
	}
}

func TestCreateSubscriptionStoresOnlyIdentityFieldsFromPlan(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plans(id,name,total_bytes,cycle_type,cycle_day,rate_limit_enabled,rate_limit_per_minute,selection_mode) VALUES('plan','套餐',4096,'monthly',9,1,20,'explicit')`); err != nil {
		t.Fatal(err)
	}
	body := `{"name":"客户 A","plan_id":"plan","enabled":true,"expire_at":"2030-01-01","total_bytes":999,"node_filter_ids":["stale"]}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/subscription/subscriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	New(config.Config{}).createSubscription(rec, req, db)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var profileID, planID, nodeIDs, cycleType string
	var total int64
	if err := db.QueryRow(`SELECT profile_id,plan_id,total_bytes,node_filter_ids,cycle_type FROM subscription_subscriptions WHERE name='客户 A'`).Scan(&profileID, &planID, &total, &nodeIDs, &cycleType); err != nil {
		t.Fatal(err)
	}
	if profileID != defaultNodeLibrary || planID != "plan" || total != 0 || nodeIDs != "" || cycleType != "none" {
		t.Fatalf("stored subscription snapshot profile=%q plan=%q total=%d nodes=%q cycle=%q", profileID, planID, total, nodeIDs, cycleType)
	}
	items, err := loadSubscriptions(ctx, db, "")
	if err != nil || len(items) != 1 || items[0].TotalBytes != 4096 || items[0].CycleDay != 9 {
		t.Fatalf("effective plan policy items=%#v err=%v", items, err)
	}
}

func TestManagedNodesUseSubscriptionSpecificCredentials(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	vlessRaw := "vless://legacy@node.example.com:45654?security=reality&pbk=public&sid=abcd&sni=www.cloudflare.com#VLESS"
	hy2Raw := "hysteria2://legacy@hy2.example.com:45655?sni=hy2.example.com&insecure=1#HY2"
	for id, raw := range map[string]string{"vless-node": vlessRaw, "hy2-node": hy2Raw} {
		encrypted, err := secure.SecureEncrypt(raw)
		if err != nil {
			t.Fatal(err)
		}
		protocol := "vless-reality"
		transport := "tcp"
		if id == "hy2-node" {
			protocol, transport = "hysteria2", "udp"
		}
		if _, err := db.Exec(`INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,enabled,publishable,apply_status) VALUES(?,?,?,?,?,?,?,?,?,1,1,'running')`, id, "host", id, protocol, "node.example.com", 45654, transport, "", encrypted); err != nil {
			t.Fatal(err)
		}
	}
	sub := Subscription{VLESSUUID: "00000000-0000-4000-8000-000000000099", Hysteria2Password: "subscription-password"}
	nodes, err := loadManagedSubscriptionNodes(ctx, db, sub)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes=%#v", nodes)
	}
	for _, node := range nodes {
		if strings.Contains(node.Raw, "legacy") {
			t.Fatalf("shared node credential leaked into subscription output: %s", node.Raw)
		}
		if node.Type == "vless" && !strings.Contains(node.Raw, sub.VLESSUUID+"@") {
			t.Fatalf("VLESS subscription credential missing: %s", node.Raw)
		}
		if node.Type == "hysteria2" && !strings.Contains(node.Raw, sub.Hysteria2Password+"@") {
			t.Fatalf("Hysteria2 subscription credential missing: %s", node.Raw)
		}
	}
}

func TestBindSubscriptionCredentialSocksAndHTTPUseUserPassword(t *testing.T) {
	sub := Subscription{
		VLESSUUID:         "00000000-0000-4000-8000-000000000099",
		Hysteria2Password: "hy2-password",
	}
	cases := []struct {
		protocol string
		raw      string
		wantUser string
		wantPass string
	}{
		{"socks", "socks://bootstrap:bootstrap@edge.example.com:45654#NODE", "00000000-0000-4000-8000-000000000099", "hy2-password"},
		{"http", "http://bootstrap:bootstrap@edge.example.com:45654#NODE", "00000000-0000-4000-8000-000000000099", "hy2-password"},
	}
	for _, tc := range cases {
		got := bindSubscriptionCredential(tc.raw, tc.protocol, sub)
		if !strings.Contains(got, tc.wantUser+":"+tc.wantPass+"@edge.example.com") {
			t.Fatalf("%s bound URI missing credential: %s", tc.protocol, got)
		}
	}
	rawVless := bindSubscriptionCredential("vless://bootstrap@edge.example.com:45654#NODE", "vless-reality", sub)
	if !strings.Contains(rawVless, sub.VLESSUUID+"@edge.example.com") {
		t.Fatalf("vless bound URI missing uuid: %s", rawVless)
	}
}

func TestSubscriptionUsageReportsAreIdempotentAndCycleScoped(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plans(id,name,total_bytes,cycle_type,cycle_day) VALUES('plan','套餐',1000,'monthly',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_subscriptions(id,profile_id,plan_id,name,enabled,public_token,vless_uuid,hysteria2_password) VALUES('sub','pool','plan','用户',1,'token','00000000-0000-4000-8000-000000000099','hy2-password')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS server_accounts(id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO server_accounts(id) VALUES('host')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO managed_proxy_nodes(id,server_id,name,protocol,runtime,public_host,assigned_port,transport,config_encrypted,client_uri_encrypted,enabled,publishable,apply_status) VALUES('node','host','节点','vless-reality','sing-box','127.0.0.1',45654,'tcp','{}','',1,1,'running')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plan_nodes(plan_id,node_id,source) VALUES('plan','node','internal')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE subscription_plans SET include_internal_nodes=1 WHERE id='plan'`); err != nil {
		t.Fatal(err)
	}
	report := subscriptionUsageReport{ServerID: "host", NodeID: "node", CredentialID: "00000000-0000-4000-8000-000000000099", BootID: "boot", Sequence: 7, UploadBytes: 300, DownloadBytes: 400}
	accepted, err := recordSubscriptionUsage(ctx, db, report)
	if err != nil || !accepted {
		t.Fatalf("first report accepted=%v err=%v", accepted, err)
	}
	accepted, err = recordSubscriptionUsage(ctx, db, report)
	if err != nil || accepted {
		t.Fatalf("duplicate report accepted=%v err=%v", accepted, err)
	}
	items, err := loadSubscriptions(ctx, db, "sub")
	if err != nil || len(items) != 1 {
		t.Fatalf("subscriptions=%#v err=%v", items, err)
	}
	if items[0].Traffic.Upload != 300 || items[0].Traffic.Download != 400 || items[0].Traffic.MeteringStatus != "available" {
		t.Fatalf("traffic=%#v", items[0].Traffic)
	}
}

func TestPlanCycleWindowUsesPlanResetDay(t *testing.T) {
	start, end := planCycleWindow(time.Date(2026, time.July, 22, 12, 0, 0, 0, time.UTC), "monthly", 9)
	if start != "2026-07-09T00:00:00Z" || end != "2026-08-09T00:00:00Z" {
		t.Fatalf("cycle window=%q..%q", start, end)
	}
	start, end = planCycleWindow(time.Date(2026, time.February, 15, 12, 0, 0, 0, time.UTC), "monthly", 31)
	if start != "2026-01-31T00:00:00Z" || end != "2026-02-28T00:00:00Z" {
		t.Fatalf("clamped cycle window=%q..%q", start, end)
	}
}

func TestPlanAndSubscriptionEnabledPatchKeepPolicyCentralized(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plans(id,name,enabled,total_bytes,cycle_type,cycle_day) VALUES('plan','套餐',1,4096,'monthly',9)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_subscriptions(id,profile_id,plan_id,name,public_token,enabled,expire_at) VALUES('sub','subscription_external_pool','plan','订阅','token',1,'2030-01-01')`); err != nil {
		t.Fatal(err)
	}
	service := New(config.Config{})
	planRecorder := httptest.NewRecorder()
	service.handlePlans(planRecorder, httptest.NewRequest(http.MethodPatch, "/api/subscription/plans/plan", strings.NewReader(`{"enabled":false}`)), db, "plan")
	if planRecorder.Code != http.StatusOK {
		t.Fatalf("disable plan status=%d body=%s", planRecorder.Code, planRecorder.Body.String())
	}
	items, err := loadSubscriptions(ctx, db, "sub")
	if err != nil || len(items) != 1 {
		t.Fatalf("load subscription items=%#v err=%v", items, err)
	}
	if items[0].PlanEnabled || items[0].ExpireAt != "" || items[0].TotalBytes != 4096 || items[0].CycleEnd == "" {
		t.Fatalf("effective policy=%#v", items[0])
	}
	subRecorder := httptest.NewRecorder()
	service.setSubscriptionEnabled(subRecorder, httptest.NewRequest(http.MethodPatch, "/api/subscription/subscriptions/sub", strings.NewReader(`{"enabled":false}`)), db, "sub")
	if subRecorder.Code != http.StatusOK {
		t.Fatalf("disable subscription status=%d body=%s", subRecorder.Code, subRecorder.Body.String())
	}
}

func TestDeleteSubscriptionNeverDeletesSharedNodes(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_profiles(id,name) VALUES('pool','外部节点池')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_subscriptions(id,profile_id,name,public_token) VALUES('link','pool','订阅','token')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_usage_reports(server_id,node_id,subscription_id,credential_id,boot_id,sequence) VALUES('host','managed-node','link','link','boot',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_usage_report_keys(server_id,node_id,subscription_id,boot_id,sequence) VALUES('host','managed-node','link','boot',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_usage_hourly(server_id,node_id,subscription_id,hour,upload_bytes,download_bytes) VALUES('host','managed-node','link','2026-07-01T12:00:00Z',1,2)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_usage_cycles(subscription_id,cycle_start,upload_bytes,download_bytes) VALUES('link','2026-07-01T00:00:00Z',10,20)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_nodes(id,subscription_id,profile_id,name) VALUES('node','pool','pool','节点')`); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	New(config.Config{}).deleteSubscription(rec, httptest.NewRequest(http.MethodDelete, "/api/subscription/subscriptions/link", nil), db, "link")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM subscription_nodes WHERE id='node'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("shared node count=%d err=%v", count, err)
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM subscription_usage_reports WHERE subscription_id='link'`,
		`SELECT COUNT(*) FROM subscription_usage_report_keys WHERE subscription_id='link'`,
		`SELECT COUNT(*) FROM subscription_usage_hourly WHERE subscription_id='link'`,
		`SELECT COUNT(*) FROM subscription_usage_cycles WHERE subscription_id='link'`,
	} {
		if err := db.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("deleted subscription ledger query %q count=%d err=%v", query, count, err)
		}
	}
}

func TestExternalNodeTrafficIsNotPartOfPlanUsage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	info := computeTraffic(ctx, db, Subscription{PlanID: "plan_one", TotalBytes: 2048, IncludeExternalNodes: true, TrafficSource: "upstream", ManualUploadBytes: 999})
	if info.Upload != 0 || info.Download != 0 || info.Total != 2048 || info.Source != "panel" {
		t.Fatalf("external usage leaked into plan traffic: %#v", info)
	}
}

func TestRefreshUpstreamPreservesBoundNodeProperties(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	upstreamRaw := "vless://0119068b-0148-47bf-875b-2145040b8174@saas.sin.fan:443?security=tls&type=ws&path=/group/live/intro&encryption=none#%F0%9F%87%AD%F0%9F%87%B0%20%E9%A6%99%E6%B8%AF"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=4")
		_, _ = w.Write([]byte(upstreamRaw))
	}))
	defer server.Close()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_profiles (id, name, enabled) VALUES ('profile_one', '节点库', 1)`); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions (id, profile_id, name, public_token, enabled) VALUES ('link_one', 'profile_one', '公开链接', 'token_one', 1)`); err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_upstreams (id, profile_id, name, url, enabled) VALUES ('up_one', 'profile_one', '默认上游', ?, 1)`, server.URL); err != nil {
		t.Fatalf("insert upstream: %v", err)
	}

	nodes := parseImportText(upstreamRaw)
	if len(nodes) != 1 {
		t.Fatalf("parsed nodes = %d, want 1", len(nodes))
	}
	nodes[0].ID = "node_existing"
	nodes[0].SubscriptionID = "profile_one"
	nodes[0].ProfileID = "profile_one"
	nodes[0].Source = "managed"
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if err := insertNode(ctx, tx, nodes[0]); err != nil {
		t.Fatalf("insert node: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE subscription_nodes SET name = '自定义香港', country_code = 'SG', location = '自定义地区', tags = 'stable,premium', traffic_server_id = 'server_one', enabled = 0, stable = 1, sort_order = 7 WHERE id = 'node_existing'`); err != nil {
		t.Fatalf("customize node: %v", err)
	}

	if err := New(config.Config{}).refreshUpstreamNow(ctx, db, "link_one"); err != nil {
		t.Fatalf("refresh upstream: %v", err)
	}

	loaded, err := loadNodes(ctx, db, "profile_one", true)
	if err != nil {
		t.Fatalf("load nodes: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("len(nodes) = %d, want 1", len(loaded))
	}
	node := loaded[0]
	if node.ID != "node_existing" || node.Name != "自定义香港" || node.TrafficServerID != "server_one" || node.Enabled || !node.Stable || node.SortOrder != 7 {
		t.Fatalf("node local fields not preserved: %#v", node)
	}
	if node.CountryCode != "SG" || node.Location != "自定义地区" || node.Tags != "stable,premium" {
		t.Fatalf("node custom labels not preserved: %#v", node)
	}
	if node.Type != "vless" || node.Server != "saas.sin.fan" || node.Port != 443 || !strings.Contains(node.Raw, "vless://") {
		t.Fatalf("node upstream fields not refreshed: %#v", node)
	}
}

func TestComputeTrafficFromNodeBoundServers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE server_accounts (id TEXT PRIMARY KEY, name TEXT, host TEXT, cached_info TEXT, traffic_limit_bytes INTEGER DEFAULT 0)`); err != nil {
		t.Fatalf("create server accounts: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO server_accounts (id, name, host, cached_info, traffic_limit_bytes) VALUES
		('srv_one', 'Server One', 'one.example.com', '{"network":{"rx_total_bytes":1000,"tx_total_bytes":2000}}', 10000),
		('srv_two', 'Server Two', 'two.example.com', '{"net_in_transfer":3000,"net_out_transfer":4000}', 20000)`); err != nil {
		t.Fatalf("insert servers: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	nodes := []Node{
		{ID: "node_one", SubscriptionID: "profile_one", ProfileID: "profile_one", Name: "节点一", Enabled: true, TrafficServerID: "srv_one"},
		{ID: "node_two", SubscriptionID: "profile_one", ProfileID: "profile_one", Name: "节点二", Enabled: true, TrafficServerID: "srv_two"},
		{ID: "node_three", SubscriptionID: "profile_one", ProfileID: "profile_one", Name: "节点三", Enabled: false, TrafficServerID: "srv_one"},
	}
	for _, node := range nodes {
		if err := insertNode(ctx, tx, node); err != nil {
			t.Fatalf("insert node: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	info := computeTraffic(ctx, db, Subscription{ID: "link_one", ProfileID: "profile_one", TrafficSource: "node_servers", TotalBytes: 50000})
	if info.Upload != 0 || info.Download != 0 || info.Total != 50000 || info.Source != "panel" || info.MeteringStatus != "unavailable" {
		t.Fatalf("traffic = %#v, host NIC totals must not be treated as subscription usage", info)
	}

	filtered := computeTraffic(ctx, db, Subscription{ID: "link_one", ProfileID: "profile_one", TrafficSource: "node_servers", NodeFilterIDs: []string{"node_two"}, TotalBytes: 50000})
	if filtered.Upload != 0 || filtered.Download != 0 || filtered.Total != 50000 || filtered.Source != "panel" || filtered.MeteringStatus != "unavailable" {
		t.Fatalf("filtered traffic = %#v, node filters must not turn host NIC totals into subscription usage", filtered)
	}
}

func TestDeleteSubscriptionKeepsSharedProfile(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_profiles (id, name, enabled) VALUES ('profile_shared', '共享档案', 1)`); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_upstreams (id, profile_id, name, url, enabled) VALUES ('up_profile_shared', 'profile_shared', '默认上游', 'https://example.com/sub', 1)`); err != nil {
		t.Fatalf("insert upstream: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO subscription_subscriptions (id, profile_id, name, public_token, enabled) VALUES ('link_one', 'profile_shared', '链接一', 'token_one', 1), ('link_two', 'profile_shared', '链接二', 'token_two', 1)`); err != nil {
		t.Fatalf("insert links: %v", err)
	}

	profileID := firstNonEmpty(profileIDForSubscription(ctx, db, "link_one"), "link_one")
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	_, _ = tx.ExecContext(ctx, `DELETE FROM subscription_nodes WHERE subscription_id = ?`, "link_one")
	_, _ = tx.ExecContext(ctx, `DELETE FROM subscription_nodes WHERE profile_id = ?`, profileID)
	_, _ = tx.ExecContext(ctx, `DELETE FROM subscription_access_logs WHERE subscription_id = ?`, "link_one")
	if _, err := tx.ExecContext(ctx, `DELETE FROM subscription_subscriptions WHERE id = ?`, "link_one"); err != nil {
		t.Fatalf("delete link: %v", err)
	}
	var remainingLinks int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_subscriptions WHERE COALESCE(profile_id, id) = ?`, profileID).Scan(&remainingLinks)
	if remainingLinks == 0 {
		_, _ = tx.ExecContext(ctx, `DELETE FROM subscription_upstreams WHERE profile_id = ?`, profileID)
		_, _ = tx.ExecContext(ctx, `DELETE FROM subscription_profiles WHERE id = ?`, profileID)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	var profileCount, upstreamCount int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_profiles WHERE id = 'profile_shared'`).Scan(&profileCount)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscription_upstreams WHERE profile_id = 'profile_shared'`).Scan(&upstreamCount)
	if profileCount != 1 || upstreamCount != 1 {
		t.Fatalf("profileCount=%d upstreamCount=%d, want both 1", profileCount, upstreamCount)
	}
}

func TestImportCommitReadsSettingsThroughOpenTransaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := ensureSchema(ctx, db); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback()

	var refreshHours int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(default_refresh_hours, 24) FROM subscription_settings WHERE id = 1`).Scan(&refreshHours); err != nil {
		t.Fatalf("query settings through tx: %v", err)
	}
	if refreshHours != 24 {
		t.Fatalf("refreshHours = %d, want 24", refreshHours)
	}
}

// TestGetSubscriptionUsage 验证订阅流量明细接口：周期累计（cycles，面板口径）+
// 逐日明细（hourly）一并返回，percent 按套餐总量计算。
func TestGetSubscriptionUsage(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ensureSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	// ledger 表由 subscriptionledger.EnsureSchema 建，测试按同构列自行建。
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS subscription_usage_cycles (
		subscription_id TEXT NOT NULL, cycle_start TEXT NOT NULL, cycle_end TEXT NOT NULL DEFAULT '',
		upload_bytes INTEGER NOT NULL DEFAULT 0, download_bytes INTEGER NOT NULL DEFAULT 0,
		updated_at TEXT NOT NULL DEFAULT '', PRIMARY KEY(subscription_id, cycle_start))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS subscription_usage_hourly (
		server_id TEXT NOT NULL, node_id TEXT NOT NULL, subscription_id TEXT NOT NULL, hour TEXT NOT NULL,
		upload_bytes INTEGER NOT NULL DEFAULT 0, download_bytes INTEGER NOT NULL DEFAULT 0,
		reported_at TEXT NOT NULL DEFAULT '', PRIMARY KEY(server_id,node_id,subscription_id,hour))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_plans(id,name,enabled,total_bytes,cycle_type,cycle_day,selection_mode,include_internal_nodes,include_external_nodes)
		VALUES('plan-u','UsagePlan',1,500,'monthly',1,'explicit',1,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_subscriptions(id,profile_id,plan_id,name,public_token,vless_uuid,hysteria2_password,enabled,created_at,traffic_source)
		VALUES('sub-u','sub_default_nodes','plan-u','UsageSub','tok-u','uuid-u','pwd-u',1,'2026-08-01 00:00:00','panel')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO subscription_usage_cycles(subscription_id,cycle_start,cycle_end,upload_bytes,download_bytes,updated_at)
		VALUES('sub-u','2026-08-01T00:00:00Z','2026-09-01T00:00:00Z',10,20,datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	hour := time.Now().UTC().Truncate(time.Hour).Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO subscription_usage_hourly(server_id,node_id,subscription_id,hour,upload_bytes,download_bytes,reported_at)
		VALUES('srv','node','sub-u',?,5,7,datetime('now'))`, hour); err != nil {
		t.Fatal(err)
	}

	service := New(config.Config{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/subscription/subscriptions/sub-u/usage?days=30", nil)
	service.getSubscriptionUsage(rec, req, db, "sub-u")
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Success bool `json:"success"`
		Data    struct {
			CycleUploadBytes   int64   `json:"cycleUploadBytes"`
			CycleDownloadBytes int64   `json:"cycleDownloadBytes"`
			CycleUsedBytes     int64   `json:"cycleUsedBytes"`
			TotalBytes         int64   `json:"totalBytes"`
			Percent            float64 `json:"percent"`
			Granularity        string  `json:"granularity"`
			Points             []struct {
				Bucket        string `json:"bucket"`
				UploadBytes   int64  `json:"uploadBytes"`
				DownloadBytes int64  `json:"downloadBytes"`
			} `json:"points"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success {
		t.Fatalf("success=false body=%s", rec.Body.String())
	}
	if resp.Data.CycleUsedBytes != 30 {
		t.Fatalf("cycle used = %d, want 30", resp.Data.CycleUsedBytes)
	}
	if resp.Data.CycleUploadBytes != 10 || resp.Data.CycleDownloadBytes != 20 {
		t.Fatalf("cycle components = %d/%d, want 10/20", resp.Data.CycleUploadBytes, resp.Data.CycleDownloadBytes)
	}
	if resp.Data.TotalBytes != 500 {
		t.Fatalf("total = %d, want 500", resp.Data.TotalBytes)
	}
	if resp.Data.Percent <= 0 {
		t.Fatalf("percent = %v, want > 0", resp.Data.Percent)
	}
	if resp.Data.Granularity != "day" {
		t.Fatalf("granularity = %q, want day", resp.Data.Granularity)
	}
	if len(resp.Data.Points) != 1 {
		t.Fatalf("points len = %d, want 1", len(resp.Data.Points))
	}
	if resp.Data.Points[0].UploadBytes != 5 || resp.Data.Points[0].DownloadBytes != 7 {
		t.Fatalf("hourly point mismatch: %+v", resp.Data.Points[0])
	}
}
