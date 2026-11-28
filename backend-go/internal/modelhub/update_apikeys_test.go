package modelhub

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func readStoredAPIKeys(t *testing.T, s *Service, id string) []string {
	t.Helper()
	db, err := s.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var raw sql.NullString
	if err := db.QueryRow("SELECT api_keys FROM modelhub_endpoints WHERE id = ?", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var keys []string
	_ = json.Unmarshal([]byte(secure.SecureDecrypt(raw.String)), &keys)
	return keys
}

// TestUpdateEndpointPersistsApiKeysWithoutOtherChanges 回归:仅提交 apiKeys、其余
// 字段与现值相同或未提交时,必须写入 api_keys。此前这种请求会落到「只更新 name」
// 的 else 分支,apiKeys 被静默丢弃(见网关问题清单第 1 条)。
func TestUpdateEndpointPersistsApiKeysWithoutOtherChanges(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[{"id":"m1","object":"model"}]}`))
	}))
	defer upstream.Close()

	service := newOpenAIService(t)
	createPayload := fmt.Sprintf(`{"name":"TH","baseUrl":"%s","apiKey":"k1","skipVerify":true}`, upstream.URL)
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

	// 与实测同形:name/baseUrl/apiKey 都提交但与现值相同,apiKeys 为新值。
	update := fmt.Sprintf(`{"name":"TH","baseUrl":"%s","apiKey":"k1","apiKeys":["k2","k3"]}`, upstream.URL)
	wUpdate := httptest.NewRecorder()
	rUpdate, _ := http.NewRequest("PUT", "/api/modelhub/endpoints/"+id, strings.NewReader(update))
	service.ServeHTTP(wUpdate, rUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", wUpdate.Code, wUpdate.Body.String())
	}

	if got := readStoredAPIKeys(t, service, id); len(got) != 2 || got[0] != "k2" || got[1] != "k3" {
		t.Fatalf("apiKeys 应写入 [k2 k3],实际 %v", got)
	}

	// 仅提交 apiKeys(其余字段完全不提交)也必须落库。
	update2 := `{"name":"TH","apiKeys":["k4"]}`
	wUpdate2 := httptest.NewRecorder()
	rUpdate2, _ := http.NewRequest("PUT", "/api/modelhub/endpoints/"+id, strings.NewReader(update2))
	service.ServeHTTP(wUpdate2, rUpdate2)
	if wUpdate2.Code != http.StatusOK {
		t.Fatalf("update2 status=%d body=%s", wUpdate2.Code, wUpdate2.Body.String())
	}
	if got := readStoredAPIKeys(t, service, id); len(got) != 1 || got[0] != "k4" {
		t.Fatalf("apiKeys 应写入 [k4],实际 %v", got)
	}
}
