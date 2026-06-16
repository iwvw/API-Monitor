package tencent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestAccountLifecycleAndFrontendShapes(t *testing.T) {
	fake := fakeTencentAPI(t)
	defer fake.Close()
	t.Setenv("TENCENT_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/tencent/accounts", `{"name":"prod","secretId":"AKID1234567890","secretKey":"secret","regionId":"ap-guangzhou","description":"primary"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	id := int64Value(createPayload["id"], 0)
	if id == 0 || createPayload["success"] != true {
		t.Fatalf("unexpected create payload: %#v", createPayload)
	}

	res = perform(service, http.MethodGet, "/api/tencent/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list accounts status=%d body=%s", res.Code, res.Body.String())
	}
	var accounts []map[string]interface{}
	mustDecode(t, res, &accounts)
	if len(accounts) != 1 || accounts[0]["secret_id"] == "AKID1234567890" || accounts[0]["secretId"] == "AKID1234567890" {
		t.Fatalf("unexpected accounts payload: %#v", accounts)
	}
	if accounts[0]["region_id"] != "ap-guangzhou" || accounts[0]["regionId"] != "ap-guangzhou" {
		t.Fatalf("missing compatible region fields: %#v", accounts[0])
	}

	idText := strconv.FormatInt(id, 10)
	res = perform(service, http.MethodPut, "/api/tencent/accounts/"+idText, `{"name":"prod2","secretId":"AKID1234****7890","regionId":"ap-shanghai","description":"updated"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update account status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/tencent/accounts", "")
	mustDecode(t, res, &accounts)
	if accounts[0]["name"] != "prod2" || accounts[0]["region_id"] != "ap-shanghai" {
		t.Fatalf("account did not update compatible fields: %#v", accounts[0])
	}

	res = perform(service, http.MethodGet, "/api/tencent/accounts/"+idText+"/domains", "")
	if res.Code != http.StatusOK {
		t.Fatalf("domains status=%d body=%s", res.Code, res.Body.String())
	}
	var domains map[string]interface{}
	mustDecode(t, res, &domains)
	if len(arrayValue(domains["Domains"])) != 1 || len(arrayValue(domains["domains"])) != 1 {
		t.Fatalf("domains response missing compatibility shapes: %#v", domains)
	}

	res = perform(service, http.MethodGet, "/api/tencent/accounts/"+idText+"/cvm", "")
	if res.Code != http.StatusOK {
		t.Fatalf("cvm status=%d body=%s", res.Code, res.Body.String())
	}
	var cvm map[string]interface{}
	mustDecode(t, res, &cvm)
	cvmInstances := arrayValue(cvm["instances"])
	if len(cvmInstances) != 1 || objectValue(cvmInstances[0])["_Region"] != "ap-guangzhou" || objectValue(cvmInstances[0])["RegionName"] == "" {
		t.Fatalf("cvm response missing frontend fields: %#v", cvm)
	}

	res = perform(service, http.MethodGet, "/api/tencent/accounts/"+idText+"/lighthouse", "")
	if res.Code != http.StatusOK {
		t.Fatalf("lighthouse status=%d body=%s", res.Code, res.Body.String())
	}
	var lighthouse map[string]interface{}
	mustDecode(t, res, &lighthouse)
	lightInstances := arrayValue(lighthouse["instances"])
	if len(lightInstances) != 1 || objectValue(lightInstances[0])["_Region"] != "ap-guangzhou" {
		t.Fatalf("lighthouse response missing frontend fields: %#v", lighthouse)
	}

	res = perform(service, http.MethodDelete, "/api/tencent/accounts/"+idText, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete account status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestTencentProviderActions(t *testing.T) {
	fake := fakeTencentAPI(t)
	defer fake.Close()
	t.Setenv("TENCENT_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/tencent/accounts", `{"name":"prod","secretId":"AKID1234567890","secretKey":"secret","regionId":"ap-guangzhou"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	id := strconv.FormatInt(int64Value(createPayload["id"], 0), 10)

	cases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/tencent/accounts/" + id + "/domains", `{"domain":"example.com"}`},
		{http.MethodGet, "/api/tencent/accounts/" + id + "/domains/example.com/records", ""},
		{http.MethodPost, "/api/tencent/accounts/" + id + "/domains/example.com/records", `{"subDomain":"www","recordType":"A","recordLine":"默认","value":"127.0.0.1","ttl":600}`},
		{http.MethodPut, "/api/tencent/accounts/" + id + "/domains/example.com/records/123", `{"subDomain":"www","recordType":"A","value":"127.0.0.2","ttl":600}`},
		{http.MethodPatch, "/api/tencent/accounts/" + id + "/domains/example.com/records/123/status", `{"status":"DISABLE"}`},
		{http.MethodDelete, "/api/tencent/accounts/" + id + "/domains/example.com/records/123", ""},
		{http.MethodDelete, "/api/tencent/accounts/" + id + "/domains/example.com", ""},
		{http.MethodPost, "/api/tencent/accounts/" + id + "/cvm/ins-smoke/control", `{"action":"START","region":"ap-guangzhou"}`},
		{http.MethodPost, "/api/tencent/accounts/" + id + "/cvm/ins-smoke/control", `{"action":"STOP","region":"ap-guangzhou"}`},
		{http.MethodPost, "/api/tencent/accounts/" + id + "/cvm/ins-smoke/control", `{"action":"REBOOT","region":"ap-guangzhou"}`},
		{http.MethodPost, "/api/tencent/accounts/" + id + "/lighthouse/lhins-smoke/control", `{"action":"START","region":"ap-guangzhou"}`},
		{http.MethodPost, "/api/tencent/accounts/" + id + "/lighthouse/lhins-smoke/control", `{"action":"STOP","region":"ap-guangzhou"}`},
		{http.MethodPost, "/api/tencent/accounts/" + id + "/lighthouse/lhins-smoke/control", `{"action":"REBOOT","region":"ap-guangzhou"}`},
	}
	for _, item := range cases {
		res := perform(service, item.method, item.path, item.body)
		if res.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", item.method, item.path, res.Code, res.Body.String())
		}
		var payload map[string]interface{}
		mustDecode(t, res, &payload)
		if item.method != http.MethodGet && payload["success"] != true {
			t.Fatalf("%s %s expected success payload, got %#v", item.method, item.path, payload)
		}
	}
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

func perform(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func fakeTencentAPI(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("X-TC-Action")
		if !strings.Contains(r.Header.Get("Authorization"), "AKID1234567890") || r.Header.Get("X-TC-Timestamp") == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"Response": map[string]interface{}{"Error": map[string]interface{}{"Code": "AuthFailure", "Message": "bad signature"}}})
			return
		}
		switch action {
		case "DescribeDomainList":
			writeJSON(w, http.StatusOK, map[string]interface{}{"Response": map[string]interface{}{"RequestId": "req-domain-list", "DomainCount": 1, "DomainList": []map[string]interface{}{{"DomainId": 1001, "Name": "example.com", "Status": "ENABLE", "RecordCount": 2, "Expiration": "2030-01-01"}}}})
		case "CreateDomain", "DeleteDomain":
			writeJSON(w, http.StatusOK, map[string]interface{}{"Response": map[string]interface{}{"RequestId": "req-domain"}})
		case "DescribeRecordList":
			writeJSON(w, http.StatusOK, map[string]interface{}{"Response": map[string]interface{}{"RequestId": "req-record-list", "RecordCount": 1, "RecordList": []map[string]interface{}{{"RecordId": 123, "Name": "www", "Type": "A", "Value": "127.0.0.1"}}}})
		case "CreateRecord", "ModifyRecord", "ModifyRecordStatus", "DeleteRecord":
			writeJSON(w, http.StatusOK, map[string]interface{}{"Response": map[string]interface{}{"RequestId": "req-record"}})
		case "DescribeInstances":
			region := r.Header.Get("X-TC-Region")
			if region != "ap-guangzhou" {
				writeJSON(w, http.StatusOK, map[string]interface{}{"Response": map[string]interface{}{"RequestId": "req-empty", "InstanceSet": []map[string]interface{}{}}})
				return
			}
			if r.Header.Get("X-TC-Version") == lighthouseVersion {
				writeJSON(w, http.StatusOK, map[string]interface{}{"Response": map[string]interface{}{"RequestId": "req-lighthouse", "InstanceSet": []map[string]interface{}{{"InstanceId": "lhins-smoke", "InstanceName": "lighthouse", "InstanceState": "RUNNING", "Zone": "ap-guangzhou-3", "PublicAddresses": []string{"203.0.113.2"}, "CPU": 2, "Memory": 2, "ExpiredTime": "2030-01-01T00:00:00Z"}}}})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"Response": map[string]interface{}{"RequestId": "req-cvm", "InstanceSet": []map[string]interface{}{{"InstanceId": "ins-smoke", "InstanceName": "cvm", "InstanceState": "RUNNING", "Placement": map[string]interface{}{"Zone": "ap-guangzhou-3"}, "PublicIpAddresses": []string{"203.0.113.1"}, "CPU": 2, "Memory": 4, "OsName": "Linux"}}}})
		case "StartInstances", "StopInstances", "RebootInstances":
			writeJSON(w, http.StatusOK, map[string]interface{}{"Response": map[string]interface{}{"RequestId": "req-action"}})
		default:
			t.Fatalf("unexpected fake Tencent action %s", action)
		}
	}))
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func mustDecode(t *testing.T, res *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %T: %v body=%s", target, err, res.Body.String())
	}
}

func int64Value(value interface{}, fallback int64) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case float64:
		return int64(typed)
	case string:
		if parsed, err := strconv.ParseInt(typed, 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}
