package modelhub

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func readStoredEndpointAPIKey(t *testing.T, s *Service, id string) string {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw string
	if err := db.QueryRow("SELECT api_key FROM modelhub_endpoints WHERE id = ?", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	return secure.SecureDecrypt(raw)
}

// TestUpdateEndpointIgnoresMaskedKeys 回归:列表响应只回掩码后,前端把掩码原样
// 回填保存时,updateEndpoint 必须把掩码当作「未修改」(还原为原明文),绝不能把
// 掩码加密写库。
func TestUpdateEndpointIgnoresMaskedKeys(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1","object":"model"}]}`))
	}))
	defer upstream.Close()

	service := newOpenAIService(t)
	mainKey := "sk-main-abcdefghijklmnop"
	extraOne := "sk-extra-111111111111"
	extraTwo := "sk-extra-222222222222"
	createPayload := fmt.Sprintf(`{"name":"Mask Guard","baseUrl":"%s","apiKey":%q,"apiKeys":[%q,%q],"skipVerify":true}`, upstream.URL, mainKey, extraOne, extraTwo)

	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(createPayload))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	id := createRes.Endpoint.ID
	if createRes.Endpoint.APIKey != mainKey {
		t.Fatalf("create response must echo plaintext apiKey, got %q", createRes.Endpoint.APIKey)
	}

	wList := httptest.NewRecorder()
	rList, _ := http.NewRequest("GET", "/api/modelhub/endpoints", nil)
	service.ServeHTTP(wList, rList)
	var listed []Endpoint
	mustDecode(t, wList.Body.String(), &listed)
	if len(listed) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(listed))
	}
	mainMask := maskSecretValue(mainKey)
	if listed[0].APIKey != mainMask {
		t.Fatalf("list must return masked apiKey, got %q", listed[0].APIKey)
	}
	for _, raw := range []string{mainKey, extraOne, extraTwo} {
		if strings.Contains(wList.Body.String(), raw) {
			t.Fatalf("list must not leak plaintext key %q: %s", raw, wList.Body.String())
		}
	}

	// modelsUrl 变更强制走「写入 api_key」的全量分支,确保防呆覆盖真正落库路径。
	updatePayload := fmt.Sprintf(`{"name":"Mask Guard Renamed","apiKey":%q,"apiKeys":[%q,%q],"modelsUrl":"%s"}`, listed[0].APIKey, listed[0].APIKeys[0], listed[0].APIKeys[1], upstream.URL)
	wUpdate := httptest.NewRecorder()
	rUpdate, _ := http.NewRequest("PUT", "/api/modelhub/endpoints/"+id, strings.NewReader(updatePayload))
	service.ServeHTTP(wUpdate, rUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", wUpdate.Code, wUpdate.Body.String())
	}

	stored := append([]string{readStoredEndpointAPIKey(t, service, id)}, readStoredAPIKeys(t, service, id)...)
	if stored[0] != mainKey {
		t.Fatalf("masked apiKey must not be persisted, stored=%q want=%q", stored[0], mainKey)
	}
	if len(stored) != 3 || stored[1] != extraOne || stored[2] != extraTwo {
		t.Fatalf("masked apiKeys must resolve to originals, stored=%v", stored)
	}
	for _, value := range stored {
		if isMaskedSecretValue(value) {
			t.Fatalf("stored key must not be a mask: %q", value)
		}
	}

	// 掩码与真 key 混合提交:掩码还原为原值,新 key 正常写入并保持顺序。
	wMixed := httptest.NewRecorder()
	rMixed, _ := http.NewRequest("PUT", "/api/modelhub/endpoints/"+id, strings.NewReader(fmt.Sprintf(`{"name":"Mask Guard Mixed","apiKeys":[%q,"sk-new-999999999999"]}`, maskSecretValue(extraOne))))
	service.ServeHTTP(wMixed, rMixed)
	if wMixed.Code != http.StatusOK {
		t.Fatalf("mixed update status=%d body=%s", wMixed.Code, wMixed.Body.String())
	}
	if got := readStoredAPIKeys(t, service, id); len(got) != 2 || got[0] != extraOne || got[1] != "sk-new-999999999999" {
		t.Fatalf("mixed apiKeys resolve wrong, stored=%v", got)
	}
}

// TestGatewayKeyCreateAndRotateEchoPlaintext 固化网关密钥响应契约:创建与轮换
// 一次性返回明文 apiKey,列表只回掩码,二者不互相污染。
func TestGatewayKeyCreateAndRotateEchoPlaintext(t *testing.T) {
	service := newOpenAIService(t)

	wCreate := httptest.NewRecorder()
	rCreate := httptest.NewRequest(http.MethodPost, "/api/modelhub/keys", strings.NewReader(`{"name":"mask guard"}`))
	service.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var created struct {
		APIKey string `json:"apiKey"`
		Key    struct {
			ID string `json:"id"`
		} `json:"key"`
	}
	mustDecode(t, wCreate.Body.String(), &created)
	if !strings.HasPrefix(created.APIKey, "sk-am-") || created.Key.ID == "" {
		t.Fatalf("create must echo plaintext apiKey and key id, got %+v", created)
	}

	wRotate := httptest.NewRecorder()
	rRotate := httptest.NewRequest(http.MethodPost, "/api/modelhub/keys/"+created.Key.ID+"/rotate", nil)
	service.ServeHTTP(wRotate, rRotate)
	if wRotate.Code != http.StatusOK {
		t.Fatalf("rotate status=%d body=%s", wRotate.Code, wRotate.Body.String())
	}
	var rotated struct {
		APIKey string `json:"apiKey"`
	}
	mustDecode(t, wRotate.Body.String(), &rotated)
	if !strings.HasPrefix(rotated.APIKey, "sk-am-") || rotated.APIKey == created.APIKey {
		t.Fatalf("rotate must echo a fresh plaintext apiKey, got %q", rotated.APIKey)
	}

	wList := httptest.NewRecorder()
	service.ServeHTTP(wList, httptest.NewRequest(http.MethodGet, "/api/modelhub/keys", nil))
	if strings.Contains(wList.Body.String(), rotated.APIKey) {
		t.Fatalf("list must not leak rotated plaintext: %s", wList.Body.String())
	}
	var listed []GatewayKey
	mustDecode(t, wList.Body.String(), &listed)
	if len(listed) != 1 || listed[0].APIKey == "" || listed[0].APIKey == rotated.APIKey {
		t.Fatalf("list must return a mask, got %+v", listed)
	}
}
