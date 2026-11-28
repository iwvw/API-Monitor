package modelhub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestEndpointKeyStateRoute 验证 key-state 接口把内存里的回避冷却真实暴露出来：
// 冷却中的 key 标 cooled=true 并带截止时间，未冷却的 key 保持 false。
func TestEndpointKeyStateRoute(t *testing.T) {
	s := newOpenAIService(t)

	mainKey := "sk-kcool-aaaa1111"
	okKey := "sk-kok-bbbb2222"
	coolKey := "sk-kcool-cccc3333"
	createPayload := `{
		"name": "Key State",
		"baseUrl": "http://127.0.0.1:1",
		"apiKey": "` + mainKey + `",
		"apiKeys": ["` + okKey + `", "` + coolKey + `"],
		"keyCooldownSeconds": 3600,
		"skipVerify": true
	}`
	wCreate := httptest.NewRecorder()
	rCreate, _ := http.NewRequest("POST", "/api/modelhub/endpoints", strings.NewReader(createPayload))
	s.ServeHTTP(wCreate, rCreate)
	if wCreate.Code != http.StatusOK {
		t.Fatalf("create status = %d body=%s", wCreate.Code, wCreate.Body.String())
	}
	var createRes struct {
		Endpoint Endpoint `json:"endpoint"`
	}
	mustDecode(t, wCreate.Body.String(), &createRes)
	id := createRes.Endpoint.ID
	if id == "" {
		t.Fatal("created endpoint has empty id")
	}

	s.armKeyCooldown(id, mainKey, 3600)
	s.armKeyCooldown(id, coolKey, 3600)

	wState := httptest.NewRecorder()
	rState, _ := http.NewRequest("GET", "/api/modelhub/endpoints/"+id+"/key-state", nil)
	s.ServeHTTP(wState, rState)
	if wState.Code != http.StatusOK {
		t.Fatalf("key-state status = %d body=%s", wState.Code, wState.Body.String())
	}
	var stateRes struct {
		Success bool                     `json:"success"`
		Keys    []endpointKeyRuntimeItem `json:"keys"`
	}
	mustDecode(t, wState.Body.String(), &stateRes)
	if !stateRes.Success {
		t.Fatalf("key-state not successful: %s", wState.Body.String())
	}
	if len(stateRes.Keys) != 3 {
		t.Fatalf("expect 3 keys, got %d: %+v", len(stateRes.Keys), stateRes.Keys)
	}
	cooled := map[string]endpointKeyRuntimeItem{}
	for _, item := range stateRes.Keys {
		cooled[item.Key] = item
	}
	if !cooled[mainKey].Cooled || cooled[mainKey].CooldownUntil == "" {
		t.Fatalf("main key should be cooled with deadline, got %+v", cooled[mainKey])
	}
	if !cooled[coolKey].Cooled {
		t.Fatalf("extra cooled key should be cooled, got %+v", cooled[coolKey])
	}
	if cooled[okKey].Cooled {
		t.Fatalf("ok key should not be cooled, got %+v", cooled[okKey])
	}

	// 成功转发清除冷却后接口应同步归零，避免前端一直标红。
	s.markKeySuccess(id, mainKey)
	wAfter := httptest.NewRecorder()
	rAfter, _ := http.NewRequest("GET", "/api/modelhub/endpoints/"+id+"/key-state", nil)
	s.ServeHTTP(wAfter, rAfter)
	if wAfter.Code != http.StatusOK {
		t.Fatalf("key-state after success status = %d", wAfter.Code)
	}
	var afterRes struct {
		Keys []endpointKeyRuntimeItem `json:"keys"`
	}
	mustDecode(t, wAfter.Body.String(), &afterRes)
	for _, item := range afterRes.Keys {
		if item.Key == mainKey && item.Cooled {
			t.Fatalf("main key cooldown should be cleared after markKeySuccess, got %+v", item)
		}
	}
}

// TestEndpointKeyStateRouteMissingEndpoint 验证不存在端点返回 404。
func TestEndpointKeyStateRouteMissingEndpoint(t *testing.T) {
	s := newOpenAIService(t)
	w := httptest.NewRecorder()
	r, _ := http.NewRequest("GET", "/api/modelhub/endpoints/does-not-exist/key-state", nil)
	s.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing endpoint should be 404, got %d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}
