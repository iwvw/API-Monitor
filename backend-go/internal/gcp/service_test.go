package gcp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func testRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func testService(t *testing.T) *Service {
	t.Helper()
	return New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})
}

func sampleSAJSON(t *testing.T) string {
	t.Helper()
	key := testRSAKey(t)
	priv := pemEncodePKCS8(t, key)
	return `{"type":"service_account","project_id":"smoke-project","private_key_id":"k1","private_key":` +
		strconvQuote(priv) +
		`,"client_email":"svc@smoke-project.iam.gserviceaccount.com","client_id":"cid","token_uri":"https://oauth2.googleapis.com/token"}`
}

func sampleSAWithBadJSON() string {
	return `{"type":"service_account","project_id":"p1","private_key":"not a pem"`
}

func perform(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func mustDecode(t *testing.T, res *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.NewDecoder(res.Body).Decode(target); err != nil {
		t.Fatalf("decode response: %v body=%s", err, res.Body.String())
	}
}

func TestSchemaIdempotent(t *testing.T) {
	service := testService(t)
	for i := 0; i < 2; i++ {
		db, err := service.open(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err := ensureSchema(t.Context(), db); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM sqlite_master WHERE type='table' AND name='gcp_accounts'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if count != 1 {
			t.Fatalf("gcp_accounts table not present: count=%d", count)
		}
	}
}

func TestAccountLifecycleEncryptsSAJSON(t *testing.T) {
	service := testService(t)
	sa := sampleSAJSON(t)
	body := `{"name":"prod","serviceAccountJson":` + strconvQuote(sa) + `,"description":"primary"}`
	res := perform(service, http.MethodPost, "/api/gcp/accounts", body)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}

	db, err := service.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var encrypted string
	if err := db.QueryRowContext(t.Context(), `SELECT service_account_json_encrypted FROM gcp_accounts WHERE name = 'prod'`).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(encrypted, "private_key") || !secure.IsEncrypted(encrypted) {
		t.Fatalf("SA JSON was not encrypted: %q", encrypted)
	}

	res = perform(service, http.MethodGet, "/api/gcp/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list accounts status=%d body=%s", res.Code, res.Body.String())
	}
	var listPayload struct {
		Success bool                     `json:"success"`
		Data    []map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &listPayload)
	if !listPayload.Success || len(listPayload.Data) != 1 {
		t.Fatalf("unexpected list payload: %#v", listPayload)
	}
	if _, ok := listPayload.Data[0]["serviceAccountJson"]; ok {
		t.Fatalf("SA JSON leaked in list payload: %#v", listPayload.Data[0])
	}
	if listPayload.Data[0]["hasServiceAccountJson"] != true {
		t.Fatalf("missing hasServiceAccountJson flag: %#v", listPayload.Data[0])
	}
	if listPayload.Data[0]["clientEmail"] != "svc@smoke-project.iam.gserviceaccount.com" {
		t.Fatalf("client_email not exposed: %#v", listPayload.Data[0])
	}
	if listPayload.Data[0]["defaultProjectId"] != "smoke-project" {
		t.Fatalf("default project not derived from JSON: %#v", listPayload.Data[0])
	}

	id := int64(listPayload.Data[0]["id"].(float64))
	t.Logf("created account id=%d", id)
}

func TestAccountRejectsBadJSON(t *testing.T) {
	service := testService(t)
	res := perform(service, http.MethodPost, "/api/gcp/accounts", `{"name":"bad","serviceAccountJson":"`+strconvQuote(sampleSAWithBadJSON())+`"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("create bad-json account status=%d body=%s, want 400", res.Code, res.Body.String())
	}
}

func TestAccountUpdateKeepsEncryptedWhenBlank(t *testing.T) {
	service := testService(t)
	sa := sampleSAJSON(t)
	res := perform(service, http.MethodPost, "/api/gcp/accounts", `{"name":"prod","serviceAccountJson":`+strconvQuote(sa)+`}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}

	db, err := service.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var firstEncrypted string
	if err := db.QueryRowContext(t.Context(), `SELECT service_account_json_encrypted FROM gcp_accounts WHERE name = 'prod'`).Scan(&firstEncrypted); err != nil {
		t.Fatal(err)
	}
	db.Close()

	res = perform(service, http.MethodPut, "/api/gcp/accounts/1", `{"name":"prod2","description":"updated"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update account status=%d body=%s", res.Code, res.Body.String())
	}

	db, err = service.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var secondEncrypted string
	if err := db.QueryRowContext(t.Context(), `SELECT service_account_json_encrypted FROM gcp_accounts WHERE id = 1`).Scan(&secondEncrypted); err != nil {
		t.Fatal(err)
	}
	if secondEncrypted != firstEncrypted {
		t.Fatalf("encrypted SA JSON changed on blank update")
	}
	var clientEmail string
	if err := db.QueryRowContext(t.Context(), `SELECT client_email FROM gcp_accounts WHERE id = 1`).Scan(&clientEmail); err != nil {
		t.Fatal(err)
	}
	if clientEmail != "svc@smoke-project.iam.gserviceaccount.com" {
		t.Fatalf("client_email not preserved on name-only update: %q", clientEmail)
	}
}

func TestParseServiceAccountValidation(t *testing.T) {
	if _, err := parseServiceAccount("not json"); err == nil {
		t.Fatal("expected invalid JSON error")
	}
	if _, err := parseServiceAccount(`{"type":"service_account","client_email":"a@b","private_key":""}`); err == nil {
		t.Fatal("expected missing private_key error")
	}
	if _, err := parseServiceAccount(`{"type":"service_account","project_id":"p","private_key":"x","private_key_id":"k","client_email":"a@b","token_uri":""}`); err != nil {
		t.Fatalf("token_uri should default, got %v", err)
	}
}

func TestBuildJWTAssertionShape(t *testing.T) {
	key := testRSAKey(t)
	sa := serviceAccountJSON{
		ClientEmail: "svc@proj.iam.gserviceaccount.com",
		TokenURI:    "https://oauth2.googleapis.com/token",
		PrivateKey:  pemEncodePKCS8(t, key),
	}
	assertion, err := buildJWTAssertion(sa, scopeFull)
	if err != nil {
		t.Fatalf("buildJWTAssertion error: %v", err)
	}
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt must be header.payload.signature, got %d parts", len(parts))
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Iss   string `json:"iss"`
		Scope string `json:"scope"`
		Aud   string `json:"aud"`
		Exp   int64  `json:"exp"`
		Iat   int64  `json:"iat"`
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Iss != sa.ClientEmail || claims.Aud != "https://oauth2.googleapis.com/token" || claims.Scope != scopeFull {
		t.Fatalf("unexpected claims: %#v", claims)
	}
	if claims.Exp-claims.Iat != jwtTTLSeconds {
		t.Fatalf("jwt ttl = %d, want %d", claims.Exp-claims.Iat, jwtTTLSeconds)
	}
	_ = key
}

func TestSignRS256WithBadKeyFails(t *testing.T) {
	if _, err := signRS256("not a pem", "input"); err == nil {
		t.Fatal("expected invalid key error")
	}
}

func TestTokenExchangeMockServer(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("token request method = %s", r.Method)
		}
		r.ParseForm()
		if r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("assertion") == "" {
			t.Errorf("assertion missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fake-token","token_type":"Bearer","expires_in":3600}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	sa := serviceAccountJSON{
		ClientEmail: "svc@proj.iam.gserviceaccount.com",
		TokenURI:    server.URL + "/token",
		PrivateKey:  pemEncodePKCS8(t, testRSAKey(t)),
	}
	t.Setenv("GCP_TOKEN_BASE_URL", server.URL+"/token")
	cache := newTokenCache()
	provider := &tokenProvider{httpClient: &http.Client{Timeout: 5 * time.Second}, cache: cache, sa: sa, scope: scopeFull, key: "acc:scope"}
	token, err := provider.AccessToken(t.Context())
	if err != nil {
		t.Fatalf("token exchange failed: %v", err)
	}
	if token != "fake-token" {
		t.Fatalf("unexpected token %q", token)
	}
	// 二次调用应命中缓存（不再请求 mock；用开关验证一次即可）
	if got, ok := cache.get("acc:scope"); !ok || got != "fake-token" {
		t.Fatalf("token not cached: %v %q", ok, got)
	}
}

func TestTokenExchangeRejected(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Invalid JWT Signature"}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	sa := serviceAccountJSON{ClientEmail: "a@b", TokenURI: server.URL + "/token", PrivateKeyID: "k", PrivateKey: pemEncodePKCS8(t, testRSAKey(t))}
	provider := &tokenProvider{httpClient: &http.Client{Timeout: 5 * time.Second}, cache: newTokenCache(), sa: sa, scope: scopeFull, key: "k"}
	if _, err := provider.AccessToken(t.Context()); err == nil {
		t.Fatal("expected token exchange rejection error")
	}
}

func TestInstanceNormalization(t *testing.T) {
	raw := `{
		"id":"12345",
		"name":"my-instance",
		"zone":"zones/us-central1-a",
		"machineType":"https://compute.googleapis.com/compute/v1/projects/p/zones/us-central1-a/machineTypes/e2-micro",
		"status":"RUNNING",
		"creationTimestamp":"2024-01-01T00:00:00.000-08:00",
		"labels":{"env":"prod"},
		"networkInterfaces":[{
			"name":"nic0",
			"network":"https://.../global/networks/default",
			"subnetwork":"https://.../regions/us-central1/subnetworks/default",
			"networkIP":"10.128.0.5",
			"accessConfigs":[{"name":"external-nat","type":"ONE_TO_ONE_NAT","natIP":"34.123.45.67"}]
		}],
		"disks":[{"type":"PERSISTENT","mode":"READ_WRITE","source":"https://.../zones/us-central1-a/disks/my-instance","deviceName":"my-instance","boot":true,"autoDelete":true,"diskSizeGb":20}],
		"metadata":{"items":[{"key":"ssh-keys","value":"user:key"}]}
	}`
	item := instanceFromRaw(json.RawMessage(raw))
	if item.Name != "my-instance" || item.Zone != "us-central1-a" || item.State != "RUNNING" {
		t.Fatalf("unexpected basic fields: %#v", item)
	}
	if item.MachineType != "e2-micro" {
		t.Fatalf("machineType not shortened: %q", item.MachineType)
	}
	if item.PublicIP != "34.123.45.67" || item.PrivateIP != "10.128.0.5" {
		t.Fatalf("unexpected ips: %#v", item)
	}
	if len(item.NetworkInterfaces) != 1 || len(item.NetworkInterfaces[0].AccessConfigs) != 1 {
		t.Fatalf("network interfaces not normalized: %#v", item.NetworkInterfaces)
	}
	if len(item.Disks) != 1 || !item.Disks[0].Boot {
		t.Fatalf("disks not normalized: %#v", item.Disks)
	}
	if item.Image == "" || item.Image != "my-instance" {
		t.Fatalf("boot image source not captured: %q", item.Image)
	}
}

func TestDiskNormalization(t *testing.T) {
	raw := `{
		"id":"d1","name":"disk-a","zone":"zones/asia-east1-b",
		"type":"https://.../zones/asia-east1-b/diskTypes/pd-standard","sizeGb":50,
		"status":"READY","creationTimestamp":"2024-05-05T00:00:00Z","users":["https://.../instances/i1"]
	}`
	disk := diskFromRaw(json.RawMessage(raw))
	if disk.Name != "disk-a" || disk.Zone != "asia-east1-b" || disk.SizeGB != 50 || disk.Status != "READY" {
		t.Fatalf("unexpected normalized disk: %#v", disk)
	}
	if disk.Type != "pd-standard" {
		t.Fatalf("disk type not shortened: %q", disk.Type)
	}
}

func TestRouteDispatchAccounts(t *testing.T) {
	service := testService(t)
	res := perform(service, http.MethodGet, "/api/gcp/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("GET accounts status=%d", res.Code)
	}
	res = perform(service, http.MethodDelete, "/api/gcp/accounts/999999", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing account status=%d body=%s", res.Code, res.Body.String())
	}
	// 未实现/未知路由
	res = perform(service, http.MethodGet, "/api/gcp/nope/xyz", "")
	if res.Code != http.StatusNotFound {
		t.Fatalf("unknown route status=%d, want 404", res.Code)
	}
}

func TestRouteDispatchRequiresZoneForInstanceAction(t *testing.T) {
	service := testService(t)
	sa := sampleSAJSON(t)
	perform(service, http.MethodPost, "/api/gcp/accounts", `{"name":"a","serviceAccountJson":`+strconvQuote(sa)+`}`)
	res := perform(service, http.MethodPost, "/api/gcp/accounts/1/projects/p1/instances/i1/actions", `{"action":"start"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("action without zone status=%d body=%s, want 400", res.Code, res.Body.String())
	}
}

func TestInstanceActionValidation(t *testing.T) {
	service := testService(t)
	sa := sampleSAJSON(t)
	perform(service, http.MethodPost, "/api/gcp/accounts", `{"name":"a","serviceAccountJson":`+strconvQuote(sa)+`}`)
	res := perform(service, http.MethodPost, "/api/gcp/accounts/1/projects/p1/instances/i1/actions?zone=us-central1-a", `{"action":"fly"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("invalid action status=%d body=%s, want 400", res.Code, res.Body.String())
	}
}

func TestBuildInstanceBody(t *testing.T) {
	body := buildInstanceBody(instanceCreatePayload{
		Name:         "vm-1",
		Zone:         "us-central1-a",
		MachineType:  "e2-small",
		Image:        "projects/ubuntu-os-cloud/global/images/family/ubuntu-minimal-2204-lts",
		BootDiskSizeGB: 30,
		Network:      "https://.../global/networks/default",
		Subnetwork:   "https://.../regions/us-central1/subnetworks/default",
		Labels:       map[string]string{"env": "test"},
	})
	if body["name"] != "vm-1" {
		t.Fatalf("name missing: %#v", body)
	}
	disks := body["disks"].([]interface{})
	if len(disks) != 1 {
		t.Fatalf("expected single boot disk: %#v", body)
	}
	interfaces := body["networkInterfaces"].([]interface{})
	if len(interfaces) != 1 {
		t.Fatalf("expected one network interface: %#v", body)
	}
	if _, ok := body["labels"].(map[string]string); !ok {
		t.Fatalf("labels not passed through: %#v", body)
	}
}

func TestSafeAccountNeverIncludesSAJSON(t *testing.T) {
	account := Account{
		ID:                  7,
		Name:                "x",
		ClientEmail:         "svc@proj.iam.gserviceaccount.com",
		ServiceAccountJSON:  `{"private_key":"secret"}`,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
	safe := safeAccount(account)
	_, hasJSON := safe["serviceAccountJson"]
	if hasJSON {
		t.Fatalf("safeAccount leaked serviceAccountJson: %#v", safe)
	}
}

func TestNullEmpty(t *testing.T) {
	if v := nullEmpty(""); v != nil {
		t.Fatalf("nullEmpty empty got %#v", v)
	}
	if v := nullEmpty("  "); v != nil {
		t.Fatalf("nullEmpty whitespace got %#v", v)
	}
	if v := nullEmpty("ok"); v != "ok" {
		t.Fatalf("nullEmpty value got %#v", v)
	}
}

func TestGetAccountMissing(t *testing.T) {
	service := testService(t)
	db, err := service.open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := getAccount(t.Context(), db, 1); err == nil || err != sql.ErrNoRows {
		t.Fatalf("expected ErrNoRows, got %v", err)
	}
}

// ==================== 测试工具 ====================

func pemEncodePKCS8(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return string(pem.EncodeToMemory(block))
}

func strconvQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}