package aliyun

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
	fake := fakeAliyunAPI(t)
	defer fake.Close()
	t.Setenv("ALIYUN_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/aliyun/accounts", `{"name":"prod","accessKeyId":"LTAI1234567890","accessKeySecret":"secret","regionId":"cn-hangzhou","description":"primary"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	id := int64Value(createPayload["id"], 0)
	if id == 0 || createPayload["success"] != true {
		t.Fatalf("unexpected create payload: %#v", createPayload)
	}

	res = perform(service, http.MethodGet, "/api/aliyun/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list accounts status=%d body=%s", res.Code, res.Body.String())
	}
	var accounts []map[string]interface{}
	mustDecode(t, res, &accounts)
	if len(accounts) != 1 || accounts[0]["accessKeyId"] == "LTAI1234567890" || accounts[0]["access_key_id"] == "LTAI1234567890" {
		t.Fatalf("unexpected accounts payload: %#v", accounts)
	}
	if accounts[0]["regionId"] != "cn-hangzhou" || accounts[0]["region_id"] != "cn-hangzhou" {
		t.Fatalf("missing compatible region fields: %#v", accounts[0])
	}

	idText := strconv.FormatInt(id, 10)
	res = perform(service, http.MethodPut, "/api/aliyun/accounts/"+idText, `{"name":"prod2","regionId":"cn-shanghai","description":"updated"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update account status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/aliyun/accounts", "")
	mustDecode(t, res, &accounts)
	if accounts[0]["name"] != "prod2" || accounts[0]["regionId"] != "cn-shanghai" {
		t.Fatalf("account did not update compatible fields: %#v", accounts[0])
	}

	res = perform(service, http.MethodGet, "/api/aliyun/accounts/"+idText+"/domains", "")
	if res.Code != http.StatusOK {
		t.Fatalf("domains status=%d body=%s", res.Code, res.Body.String())
	}
	var domains map[string]interface{}
	mustDecode(t, res, &domains)
	if len(arrayAt(domains, "Domains", "Domain")) != 1 || len(arrayValue(domains["domains"])) != 1 {
		t.Fatalf("domains response missing compatibility shapes: %#v", domains)
	}

	res = perform(service, http.MethodGet, "/api/aliyun/accounts/"+idText+"/instances", "")
	if res.Code != http.StatusOK {
		t.Fatalf("instances status=%d body=%s", res.Code, res.Body.String())
	}
	var instances map[string]interface{}
	mustDecode(t, res, &instances)
	ecs := arrayValue(instances["instances"])
	if len(ecs) != 1 || objectValue(ecs[0])["RegionName"] == "" || objectValue(ecs[0])["InstanceTypeFriendly"] == "" {
		t.Fatalf("instances response missing enhanced fields: %#v", instances)
	}

	res = perform(service, http.MethodGet, "/api/aliyun/accounts/"+idText+"/swas", "")
	if res.Code != http.StatusOK {
		t.Fatalf("swas status=%d body=%s", res.Code, res.Body.String())
	}
	var swas map[string]interface{}
	mustDecode(t, res, &swas)
	if len(arrayValue(swas["instances"])) != 1 {
		t.Fatalf("unexpected swas payload: %#v", swas)
	}

	res = perform(service, http.MethodDelete, "/api/aliyun/accounts/"+idText, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete account status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestAliyunProviderActions(t *testing.T) {
	fake := fakeAliyunAPI(t)
	defer fake.Close()
	t.Setenv("ALIYUN_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/aliyun/accounts", `{"name":"prod","accessKeyId":"LTAI1234567890","accessKeySecret":"secret","regionId":"cn-hangzhou"}`)
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
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/metrics", `{"metricName":"CPUUtilization","dimensions":{"instanceId":"i-smoke"}}`},
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/domains", `{"domainName":"example.com"}`},
		{http.MethodGet, "/api/aliyun/accounts/" + id + "/domains/example.com/records", ""},
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/domains/example.com/records", `{"rr":"www","type":"A","value":"127.0.0.1","ttl":600}`},
		{http.MethodPut, "/api/aliyun/accounts/" + id + "/records/rec-smoke", `{"rr":"www","type":"A","value":"127.0.0.2","ttl":600}`},
		{http.MethodPut, "/api/aliyun/accounts/" + id + "/records/rec-smoke/status", `{"status":"Disable"}`},
		{http.MethodDelete, "/api/aliyun/accounts/" + id + "/records/rec-smoke", ""},
		{http.MethodDelete, "/api/aliyun/accounts/" + id + "/domains/example.com", ""},
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/instances/i-smoke/start", `{"regionId":"cn-hangzhou"}`},
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/instances/i-smoke/stop", `{"regionId":"cn-hangzhou","force":true}`},
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/instances/i-smoke/reboot", `{"regionId":"cn-hangzhou"}`},
		{http.MethodGet, "/api/aliyun/accounts/" + id + "/swas/swas-smoke/firewall?regionId=cn-hangzhou", ""},
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/swas/swas-smoke/firewall", `{"regionId":"cn-hangzhou","rule":{"protocol":"TCP","port":"443","remark":"https"}}`},
		{http.MethodDelete, "/api/aliyun/accounts/" + id + "/swas/swas-smoke/firewall/rule-smoke?regionId=cn-hangzhou", ""},
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/swas/swas-smoke/start", `{"regionId":"cn-hangzhou"}`},
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/swas/swas-smoke/stop", `{"regionId":"cn-hangzhou","force":true}`},
		{http.MethodPost, "/api/aliyun/accounts/" + id + "/swas/swas-smoke/reboot", `{"regionId":"cn-hangzhou"}`},
	}
	for _, item := range cases {
		res := perform(service, item.method, item.path, item.body)
		if res.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", item.method, item.path, res.Code, res.Body.String())
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

func fakeAliyunAPI(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("Action")
		if r.URL.Query().Get("AccessKeyId") != "LTAI1234567890" || r.URL.Query().Get("Signature") == "" {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{"Message": "bad signature"})
			return
		}
		switch action {
		case "DescribeDomains":
			writeJSON(w, http.StatusOK, map[string]interface{}{"TotalCount": 1, "Domains": map[string]interface{}{"Domain": []map[string]interface{}{{"DomainId": "domain-smoke", "DomainName": "example.com", "RecordCount": 2}}}})
		case "AddDomain", "DeleteDomain":
			writeJSON(w, http.StatusOK, map[string]interface{}{"RequestId": "req-domain"})
		case "DescribeDomainRecords":
			writeJSON(w, http.StatusOK, map[string]interface{}{"TotalCount": 1, "DomainRecords": map[string]interface{}{"Record": []map[string]interface{}{{"RecordId": "rec-smoke", "RR": "www", "Type": "A", "Value": "127.0.0.1"}}}})
		case "AddDomainRecord", "UpdateDomainRecord", "DeleteDomainRecord", "SetDomainRecordStatus":
			writeJSON(w, http.StatusOK, map[string]interface{}{"RequestId": "req-record"})
		case "DescribeRegions":
			writeJSON(w, http.StatusOK, map[string]interface{}{"Regions": map[string]interface{}{"Region": []map[string]interface{}{{"RegionId": "cn-hangzhou"}}}})
		case "DescribeInstances":
			if r.URL.Query().Get("RegionId") != "cn-hangzhou" {
				writeJSON(w, http.StatusOK, map[string]interface{}{"TotalCount": 0, "Instances": map[string]interface{}{"Instance": []map[string]interface{}{}}})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"TotalCount": 1, "Instances": map[string]interface{}{"Instance": []map[string]interface{}{{"InstanceId": "i-smoke", "InstanceName": "ecs", "Status": "Running", "RegionId": "cn-hangzhou", "InstanceType": "ecs.c2m4.large"}}}})
		case "StartInstance", "StopInstance", "RebootInstance":
			writeJSON(w, http.StatusOK, map[string]interface{}{"RequestId": "req-instance"})
		case "ListRegions":
			writeJSON(w, http.StatusOK, map[string]interface{}{"Regions": []map[string]interface{}{{"RegionId": "cn-hangzhou"}}})
		case "ListInstances":
			if r.URL.Query().Get("RegionId") != "cn-hangzhou" {
				writeJSON(w, http.StatusOK, map[string]interface{}{"Instances": []map[string]interface{}{}})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"Instances": []map[string]interface{}{{"InstanceId": "swas-smoke", "InstanceName": "swas", "Status": "Running", "RegionId": "cn-hangzhou", "PlanId": "swas.c2m2s40b1"}}})
		case "ListFirewallRules":
			writeJSON(w, http.StatusOK, map[string]interface{}{"FirewallRules": []map[string]interface{}{{"RuleId": "rule-smoke", "Port": "443", "RuleProtocol": "TCP"}}})
		case "CreateFirewallRule", "DeleteFirewallRule":
			writeJSON(w, http.StatusOK, map[string]interface{}{"RequestId": "req-firewall"})
		case "DescribeMetricList":
			writeJSON(w, http.StatusOK, map[string]interface{}{"Datapoints": "[]", "Period": "60"})
		default:
			t.Fatalf("unexpected fake Aliyun action %s path=%s", action, r.URL.String())
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
