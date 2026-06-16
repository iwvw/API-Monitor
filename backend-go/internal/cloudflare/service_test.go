package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

func TestAccountLifecycleAndTokenCompatibility(t *testing.T) {
	fake := fakeCloudflareAPI(t)
	defer fake.Close()
	t.Setenv("CLOUDFLARE_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/cloudflare/accounts", `{"name":"prod","apiToken":"smoke-cf-token","email":""}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	account := objectValue(createPayload["account"])
	id := stringValue(account["id"], "")
	if createPayload["success"] != true || id == "" {
		t.Fatalf("unexpected create payload: %#v", createPayload)
	}

	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	stored, err := loadAccount(ctx, db, id)
	_ = db.Close()
	if err != nil {
		t.Fatalf("load stored account: %v", err)
	}
	rawToken := stringValue(stored["api_token"], "")
	if rawToken == "smoke-cf-token" || !secure.IsEncrypted(rawToken) {
		t.Fatalf("expected encrypted token in db, got %q", rawToken)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list accounts status=%d body=%s", res.Code, res.Body.String())
	}
	var accounts []map[string]interface{}
	mustDecode(t, res, &accounts)
	if len(accounts) != 1 || accounts[0]["hasToken"] != true {
		t.Fatalf("unexpected accounts payload: %#v", accounts)
	}
	if _, ok := accounts[0]["apiToken"]; ok {
		t.Fatalf("safe account leaked token: %#v", accounts[0])
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+id+"/token", "")
	if res.Code != http.StatusOK {
		t.Fatalf("token status=%d body=%s", res.Code, res.Body.String())
	}
	var tokenPayload map[string]interface{}
	mustDecode(t, res, &tokenPayload)
	if tokenPayload["apiToken"] != "smoke-cf-token" {
		t.Fatalf("unexpected token payload: %#v", tokenPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/export", "")
	if res.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", res.Code, res.Body.String())
	}
	var exportPayload map[string]interface{}
	mustDecode(t, res, &exportPayload)
	exported := arrayValue(exportPayload["accounts"])
	if len(exported) != 1 || objectValue(exported[0])["apiToken"] != "smoke-cf-token" {
		t.Fatalf("unexpected export payload: %#v", exportPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+id+"/verify", "")
	if res.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", res.Code, res.Body.String())
	}
	var verifyPayload map[string]interface{}
	mustDecode(t, res, &verifyPayload)
	if verifyPayload["valid"] != true || verifyPayload["status"] != "active" {
		t.Fatalf("unexpected verify payload: %#v", verifyPayload)
	}

	res = perform(service, http.MethodPut, "/api/cloudflare/accounts/"+id, `{"name":"prod2","apiToken":"smoke-cf-token-2"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+id+"/token", "")
	mustDecode(t, res, &tokenPayload)
	if tokenPayload["apiToken"] != "smoke-cf-token-2" {
		t.Fatalf("unexpected updated token payload: %#v", tokenPayload)
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+id, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestCreateAccountRejectsInvalidToken(t *testing.T) {
	fake := fakeCloudflareAPI(t)
	defer fake.Close()
	t.Setenv("CLOUDFLARE_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/cloudflare/accounts", `{"name":"bad","apiToken":"bad-token"}`)
	if res.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status=%d body=%s", res.Code, res.Body.String())
	}
	var payload map[string]interface{}
	mustDecode(t, res, &payload)
	if !strings.Contains(stringValue(payload["error"], ""), "Token") {
		t.Fatalf("unexpected invalid token payload: %#v", payload)
	}
}

func TestZonesAndDNSRecordsLifecycle(t *testing.T) {
	fake := fakeCloudflareAPI(t)
	defer fake.Close()
	t.Setenv("CLOUDFLARE_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/cloudflare/accounts", `{"name":"prod","apiToken":"smoke-cf-token"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	accountID := stringValue(objectValue(createPayload["account"])["id"], "")
	if accountID == "" {
		t.Fatalf("missing account id: %#v", createPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/record-types", "")
	if res.Code != http.StatusOK {
		t.Fatalf("record types status=%d body=%s", res.Code, res.Body.String())
	}
	var recordTypes []string
	mustDecode(t, res, &recordTypes)
	if len(recordTypes) == 0 || recordTypes[0] != "A" {
		t.Fatalf("unexpected record types: %#v", recordTypes)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/zones", "")
	if res.Code != http.StatusOK {
		t.Fatalf("zones status=%d body=%s", res.Code, res.Body.String())
	}
	var zonesPayload map[string]interface{}
	mustDecode(t, res, &zonesPayload)
	zones := arrayValue(zonesPayload["zones"])
	if len(zones) != 1 || objectValue(zones[0])["nameServers"] == nil {
		t.Fatalf("unexpected zones payload: %#v", zonesPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/zones", `{"name":"example.net","jumpStart":true}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create zone status=%d body=%s", res.Code, res.Body.String())
	}
	var createZonePayload map[string]interface{}
	mustDecode(t, res, &createZonePayload)
	if createZonePayload["success"] != true || objectValue(createZonePayload["zone"])["id"] != "zone_new" {
		t.Fatalf("unexpected create zone payload: %#v", createZonePayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/records", "")
	if res.Code != http.StatusOK {
		t.Fatalf("records status=%d body=%s", res.Code, res.Body.String())
	}
	var recordsPayload map[string]interface{}
	mustDecode(t, res, &recordsPayload)
	records := arrayValue(recordsPayload["records"])
	if len(records) != 1 || objectValue(records[0])["createdOn"] == nil {
		t.Fatalf("unexpected records payload: %#v", recordsPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/records", `{"type":"A","name":"api","content":"2.2.2.2","ttl":1,"proxied":false}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create record status=%d body=%s", res.Code, res.Body.String())
	}
	var createRecordPayload map[string]interface{}
	mustDecode(t, res, &createRecordPayload)
	recordID := stringValue(objectValue(createRecordPayload["record"])["id"], "")
	if createRecordPayload["success"] != true || recordID == "" {
		t.Fatalf("unexpected create record payload: %#v", createRecordPayload)
	}

	res = perform(service, http.MethodPut, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/records/"+recordID, `{"type":"A","name":"api","content":"3.3.3.3","ttl":1,"proxied":true}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update record status=%d body=%s", res.Code, res.Body.String())
	}
	var updateRecordPayload map[string]interface{}
	mustDecode(t, res, &updateRecordPayload)
	if objectValue(updateRecordPayload["record"])["content"] != "3.3.3.3" {
		t.Fatalf("unexpected update record payload: %#v", updateRecordPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/switch", `{"type":"A","name":"www","newContent":"4.4.4.4"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("switch status=%d body=%s", res.Code, res.Body.String())
	}
	var switchPayload map[string]interface{}
	mustDecode(t, res, &switchPayload)
	if switchPayload["updated"] != float64(1) {
		t.Fatalf("unexpected switch payload: %#v", switchPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/batch", `{"records":[{"type":"TXT","name":"_test","content":"ok"},{"type":"CNAME","name":"app","content":"example.com"}]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("batch status=%d body=%s", res.Code, res.Body.String())
	}
	var batchPayload map[string]interface{}
	mustDecode(t, res, &batchPayload)
	if batchPayload["success"] != true || batchPayload["created"] != float64(2) {
		t.Fatalf("unexpected batch payload: %#v", batchPayload)
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/records/"+recordID, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete record status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/zones/zone_new", "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete zone status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestZoneResourcesTemplatesAndImports(t *testing.T) {
	fake := fakeCloudflareAPI(t)
	defer fake.Close()
	t.Setenv("CLOUDFLARE_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/cloudflare/accounts", `{"name":"prod","apiToken":"smoke-cf-token"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	accountID := stringValue(objectValue(createPayload["account"])["id"], "")
	if accountID == "" {
		t.Fatalf("missing account id: %#v", createPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/export/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("legacy export status=%d body=%s", res.Code, res.Body.String())
	}
	var rawAccounts []map[string]interface{}
	mustDecode(t, res, &rawAccounts)
	if len(rawAccounts) != 1 || rawAccounts[0]["apiToken"] != "smoke-cf-token" || rawAccounts[0]["id"] == "" {
		t.Fatalf("unexpected raw export payload: %#v", rawAccounts)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/import/accounts", `{"accounts":[{"name":"imported","apiToken":"smoke-cf-token"}]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("import accounts status=%d body=%s", res.Code, res.Body.String())
	}
	var importAccountsPayload map[string]interface{}
	mustDecode(t, res, &importAccountsPayload)
	if importAccountsPayload["success"] != true || importAccountsPayload["count"] != float64(1) {
		t.Fatalf("unexpected import accounts payload: %#v", importAccountsPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/purge", `{"purge_everything":true}`)
	if res.Code != http.StatusOK {
		t.Fatalf("purge status=%d body=%s", res.Code, res.Body.String())
	}
	var purgePayload map[string]interface{}
	mustDecode(t, res, &purgePayload)
	if purgePayload["success"] != true || purgePayload["message"] != "缓存已清除" {
		t.Fatalf("unexpected purge payload: %#v", purgePayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/ssl", "")
	if res.Code != http.StatusOK {
		t.Fatalf("ssl status=%d body=%s", res.Code, res.Body.String())
	}
	var sslPayload map[string]interface{}
	mustDecode(t, res, &sslPayload)
	ssl := objectValue(sslPayload["ssl"])
	if ssl["mode"] != "full" || len(arrayValue(ssl["certificates"])) != 1 {
		t.Fatalf("unexpected ssl payload: %#v", sslPayload)
	}

	res = perform(service, http.MethodPatch, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/ssl", `{"mode":"strict"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("ssl patch status=%d body=%s", res.Code, res.Body.String())
	}
	mustDecode(t, res, &sslPayload)
	if objectValue(sslPayload["ssl"])["mode"] != "strict" {
		t.Fatalf("unexpected ssl patch payload: %#v", sslPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/analytics?timeRange=24h", "")
	if res.Code != http.StatusOK {
		t.Fatalf("analytics status=%d body=%s", res.Code, res.Body.String())
	}
	var analyticsPayload map[string]interface{}
	mustDecode(t, res, &analyticsPayload)
	analytics := objectValue(analyticsPayload["analytics"])
	if analytics["requests"] != float64(100) || analytics["cacheHitRate"] != float64(40) || len(arrayValue(analytics["timeseries"])) != 1 {
		t.Fatalf("unexpected analytics payload: %#v", analyticsPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/templates", `{"name":"web","records":[{"type":"A","name":"www","content":"5.5.5.5","ttl":1,"proxied":false}]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create template status=%d body=%s", res.Code, res.Body.String())
	}
	var createTemplatePayload map[string]interface{}
	mustDecode(t, res, &createTemplatePayload)
	templateID := stringValue(objectValue(createTemplatePayload["template"])["id"], "")
	if createTemplatePayload["success"] != true || templateID == "" {
		t.Fatalf("unexpected create template payload: %#v", createTemplatePayload)
	}

	res = perform(service, http.MethodPut, "/api/cloudflare/templates/"+templateID, `{"description":"updated","records":[{"type":"TXT","name":"_check","content":"ok"}]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update template status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/templates", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list templates status=%d body=%s", res.Code, res.Body.String())
	}
	var templates []map[string]interface{}
	mustDecode(t, res, &templates)
	if len(templates) != 1 || templates[0]["description"] != "updated" {
		t.Fatalf("unexpected templates payload: %#v", templates)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/templates/"+templateID+"/apply", `{"accountId":"`+accountID+`","zoneId":"zone_1","recordName":"_applied"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("apply template status=%d body=%s", res.Code, res.Body.String())
	}
	var applyPayload map[string]interface{}
	mustDecode(t, res, &applyPayload)
	if applyPayload["success"] != true || applyPayload["created"] != float64(1) {
		t.Fatalf("unexpected apply payload: %#v", applyPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/import/templates", `{"templates":[{"name":"imported","records":[{"type":"CNAME","name":"app","content":"example.com"}]}]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("import templates status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/templates/"+templateID, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete template status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestWorkersRoutesDomainsAndAnalytics(t *testing.T) {
	fake := fakeCloudflareAPI(t)
	defer fake.Close()
	t.Setenv("CLOUDFLARE_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/cloudflare/accounts", `{"name":"prod","apiToken":"smoke-cf-token"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	accountID := stringValue(objectValue(createPayload["account"])["id"], "")
	if accountID == "" {
		t.Fatalf("missing account id: %#v", createPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/cf-account-id", "")
	if res.Code != http.StatusOK {
		t.Fatalf("cf account id status=%d body=%s", res.Code, res.Body.String())
	}
	var cfIDPayload map[string]interface{}
	mustDecode(t, res, &cfIDPayload)
	if cfIDPayload["cfAccountId"] != "cf-account-1" {
		t.Fatalf("unexpected cf account id payload: %#v", cfIDPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/workers", "")
	if res.Code != http.StatusOK {
		t.Fatalf("workers status=%d body=%s", res.Code, res.Body.String())
	}
	var workersPayload map[string]interface{}
	mustDecode(t, res, &workersPayload)
	workers := arrayValue(workersPayload["workers"])
	if len(workers) != 1 || objectValue(workers[0])["name"] != "worker-smoke" || workersPayload["subdomain"] != "smoke-workers" {
		t.Fatalf("unexpected workers payload: %#v", workersPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/workers/worker-smoke", "")
	if res.Code != http.StatusOK {
		t.Fatalf("worker get status=%d body=%s", res.Code, res.Body.String())
	}
	var workerPayload map[string]interface{}
	mustDecode(t, res, &workerPayload)
	if !strings.Contains(stringValue(objectValue(workerPayload["worker"])["script"], ""), "export default") {
		t.Fatalf("unexpected worker payload: %#v", workerPayload)
	}

	workerScript := `export default { async fetch() { return new Response("new"); } };`
	res = perform(service, http.MethodPut, "/api/cloudflare/accounts/"+accountID+"/workers/worker-new", `{"script":`+quoteJSON(workerScript)+`}`)
	if res.Code != http.StatusOK {
		t.Fatalf("worker put status=%d body=%s", res.Code, res.Body.String())
	}
	mustDecode(t, res, &workerPayload)
	if workerPayload["success"] != true || objectValue(workerPayload["worker"])["id"] != "worker-new" {
		t.Fatalf("unexpected worker put payload: %#v", workerPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/workers/worker-new", "")
	if res.Code != http.StatusOK {
		t.Fatalf("worker get new status=%d body=%s", res.Code, res.Body.String())
	}
	mustDecode(t, res, &workerPayload)
	if !strings.Contains(stringValue(objectValue(workerPayload["worker"])["script"], ""), `Response("new")`) {
		t.Fatalf("unexpected worker new payload: %#v", workerPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/workers/worker-new/toggle", `{"enabled":true}`)
	if res.Code != http.StatusOK {
		t.Fatalf("worker toggle status=%d body=%s", res.Code, res.Body.String())
	}
	var togglePayload map[string]interface{}
	mustDecode(t, res, &togglePayload)
	if objectValue(togglePayload["result"])["enabled"] != true {
		t.Fatalf("unexpected toggle payload: %#v", togglePayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/workers/routes", "")
	if res.Code != http.StatusOK {
		t.Fatalf("worker routes status=%d body=%s", res.Code, res.Body.String())
	}
	var routesPayload map[string]interface{}
	mustDecode(t, res, &routesPayload)
	if len(arrayValue(routesPayload["routes"])) != 1 {
		t.Fatalf("unexpected routes payload: %#v", routesPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/workers/routes", `{"pattern":"api.example.com/*","script":"worker-new"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("worker route create status=%d body=%s", res.Code, res.Body.String())
	}
	mustDecode(t, res, &routesPayload)
	routeID := stringValue(objectValue(routesPayload["route"])["id"], "")
	if routesPayload["success"] != true || routeID == "" {
		t.Fatalf("unexpected route create payload: %#v", routesPayload)
	}

	res = perform(service, http.MethodPut, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/workers/routes/"+routeID, `{"pattern":"new.example.com/*","script":"worker-new"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("worker route update status=%d body=%s", res.Code, res.Body.String())
	}
	mustDecode(t, res, &routesPayload)
	if objectValue(routesPayload["route"])["pattern"] != "new.example.com/*" {
		t.Fatalf("unexpected route update payload: %#v", routesPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/workers/worker-smoke/domains", "")
	if res.Code != http.StatusOK {
		t.Fatalf("worker domains status=%d body=%s", res.Code, res.Body.String())
	}
	var domainsPayload map[string]interface{}
	mustDecode(t, res, &domainsPayload)
	if len(arrayValue(domainsPayload["domains"])) != 1 || objectValue(arrayValue(domainsPayload["domains"])[0])["zoneId"] != "zone_1" {
		t.Fatalf("unexpected domains payload: %#v", domainsPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/workers/worker-new/domains", `{"hostname":"api.example.com","environment":"production"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("worker domain create status=%d body=%s", res.Code, res.Body.String())
	}
	mustDecode(t, res, &domainsPayload)
	domainID := stringValue(objectValue(domainsPayload["domain"])["id"], "")
	if domainsPayload["success"] != true || domainID == "" {
		t.Fatalf("unexpected domain create payload: %#v", domainsPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/workers/worker-new/analytics", "")
	if res.Code != http.StatusOK {
		t.Fatalf("worker analytics status=%d body=%s", res.Code, res.Body.String())
	}
	var analyticsPayload map[string]interface{}
	mustDecode(t, res, &analyticsPayload)
	if objectValue(analyticsPayload["analytics"])["requests"] != float64(12) {
		t.Fatalf("unexpected analytics payload: %#v", analyticsPayload)
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/workers/worker-new/domains/"+domainID, "")
	if res.Code != http.StatusOK {
		t.Fatalf("worker domain delete status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/zones/zone_1/workers/routes/"+routeID, "")
	if res.Code != http.StatusOK {
		t.Fatalf("worker route delete status=%d body=%s", res.Code, res.Body.String())
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/workers/worker-new", "")
	if res.Code != http.StatusOK {
		t.Fatalf("worker delete status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestPagesProjectsDeploymentsAndDomains(t *testing.T) {
	fake := fakeCloudflareAPI(t)
	defer fake.Close()
	t.Setenv("CLOUDFLARE_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/cloudflare/accounts", `{"name":"prod","apiToken":"smoke-cf-token"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	accountID := stringValue(objectValue(createPayload["account"])["id"], "")
	if accountID == "" {
		t.Fatalf("missing account id: %#v", createPayload)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/pages", "")
	if res.Code != http.StatusOK {
		t.Fatalf("pages status=%d body=%s", res.Code, res.Body.String())
	}
	var pagesPayload map[string]interface{}
	mustDecode(t, res, &pagesPayload)
	projects := arrayValue(pagesPayload["projects"])
	if len(projects) != 1 || objectValue(projects[0])["name"] != "pages-smoke" || pagesPayload["cfAccountId"] != "cf-account-1" {
		t.Fatalf("unexpected pages payload: %#v", pagesPayload)
	}
	latest := objectValue(objectValue(projects[0])["latestDeployment"])
	if latest["status"] != "success" {
		t.Fatalf("unexpected latest deployment mapping: %#v", latest)
	}

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/pages/pages-smoke/deployments", "")
	if res.Code != http.StatusOK {
		t.Fatalf("pages deployments status=%d body=%s", res.Code, res.Body.String())
	}
	var deploymentsPayload map[string]interface{}
	mustDecode(t, res, &deploymentsPayload)
	deployments := arrayValue(deploymentsPayload["deployments"])
	if deploymentsPayload["success"] != true || len(deployments) != 1 || objectValue(deployments[0])["status"] != "success" {
		t.Fatalf("unexpected deployments payload: %#v", deploymentsPayload)
	}
	deploymentID := stringValue(objectValue(deployments[0])["id"], "")

	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/pages/pages-smoke/domains", "")
	if res.Code != http.StatusOK {
		t.Fatalf("pages domains status=%d body=%s", res.Code, res.Body.String())
	}
	var domainsPayload map[string]interface{}
	mustDecode(t, res, &domainsPayload)
	domains := arrayValue(domainsPayload["domains"])
	if domainsPayload["success"] != true || len(domains) != 1 || objectValue(domains[0])["validationStatus"] != "active" {
		t.Fatalf("unexpected domains payload: %#v", domainsPayload)
	}

	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/pages/pages-smoke/domains", `{"domain":"app.example.com"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("add pages domain status=%d body=%s", res.Code, res.Body.String())
	}
	var addDomainPayload map[string]interface{}
	mustDecode(t, res, &addDomainPayload)
	if addDomainPayload["success"] != true || objectValue(addDomainPayload["domain"])["name"] != "app.example.com" {
		t.Fatalf("unexpected add domain payload: %#v", addDomainPayload)
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/pages/pages-smoke/domains/app.example.com", "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete pages domain status=%d body=%s", res.Code, res.Body.String())
	}
	var deleteDomainPayload map[string]interface{}
	mustDecode(t, res, &deleteDomainPayload)
	if deleteDomainPayload["success"] != true {
		t.Fatalf("unexpected delete domain payload: %#v", deleteDomainPayload)
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/pages/pages-smoke/deployments/"+deploymentID, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete pages deployment status=%d body=%s", res.Code, res.Body.String())
	}
	var deleteDeploymentPayload map[string]interface{}
	mustDecode(t, res, &deleteDeploymentPayload)
	if deleteDeploymentPayload["success"] != true {
		t.Fatalf("unexpected delete deployment payload: %#v", deleteDeploymentPayload)
	}

	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/pages/pages-smoke", "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete pages project status=%d body=%s", res.Code, res.Body.String())
	}
	var deleteProjectPayload map[string]interface{}
	mustDecode(t, res, &deleteProjectPayload)
	if deleteProjectPayload["success"] != true {
		t.Fatalf("unexpected delete project payload: %#v", deleteProjectPayload)
	}
}

func TestR2AndTunnelsLifecycle(t *testing.T) {
	fake := fakeCloudflareAPI(t)
	defer fake.Close()
	t.Setenv("CLOUDFLARE_API_BASE_URL", fake.URL)

	service := testService(t)
	res := perform(service, http.MethodPost, "/api/cloudflare/accounts", `{"name":"prod","apiToken":"smoke-cf-token"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	var createPayload map[string]interface{}
	mustDecode(t, res, &createPayload)
	accountID := stringValue(objectValue(createPayload["account"])["id"], "")

	// 1. R2 Buckets list
	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/r2/buckets", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list buckets status=%d body=%s", res.Code, res.Body.String())
	}
	var bucketsPayload map[string]interface{}
	mustDecode(t, res, &bucketsPayload)
	if bucketsPayload["success"] != true || len(arrayValue(bucketsPayload["buckets"])) != 1 {
		t.Fatalf("unexpected buckets payload: %#v", bucketsPayload)
	}

	// 2. R2 Bucket create
	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/r2/buckets", `{"name":"test-bucket"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create bucket status=%d body=%s", res.Code, res.Body.String())
	}
	var createBucketPayload map[string]interface{}
	mustDecode(t, res, &createBucketPayload)
	if createBucketPayload["success"] != true || objectValue(createBucketPayload["bucket"])["name"] != "test-bucket" {
		t.Fatalf("unexpected create bucket payload: %#v", createBucketPayload)
	}

	// 3. R2 Bucket objects list
	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/r2/buckets/test-bucket/objects", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list objects status=%d body=%s", res.Code, res.Body.String())
	}
	var objectsPayload map[string]interface{}
	mustDecode(t, res, &objectsPayload)
	if objectsPayload["success"] != true || len(arrayValue(objectsPayload["objects"])) != 1 {
		t.Fatalf("unexpected objects payload: %#v", objectsPayload)
	}

	// 4. R2 Object Download Info
	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/r2/buckets/test-bucket/objects/test%2Fkey.txt/download-info", "")
	if res.Code != http.StatusOK {
		t.Fatalf("download info status=%d body=%s", res.Code, res.Body.String())
	}
	var downloadPayload map[string]interface{}
	mustDecode(t, res, &downloadPayload)
	if downloadPayload["success"] != true || downloadPayload["publicUrl"] != "https://pub-r2.example.com/test/key.txt" {
		t.Fatalf("unexpected download info payload: %#v", downloadPayload)
	}

	// 5. R2 Object Delete
	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/r2/buckets/test-bucket/objects/test%2Fkey.txt", "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete object status=%d body=%s", res.Code, res.Body.String())
	}
	var deleteObjPayload map[string]interface{}
	mustDecode(t, res, &deleteObjPayload)
	if deleteObjPayload["success"] != true {
		t.Fatalf("unexpected delete object payload: %#v", deleteObjPayload)
	}

	// 6. R2 Bucket delete
	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/r2/buckets/test-bucket", "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete bucket status=%d body=%s", res.Code, res.Body.String())
	}
	var deleteBucketPayload map[string]interface{}
	mustDecode(t, res, &deleteBucketPayload)
	if deleteBucketPayload["success"] != true {
		t.Fatalf("unexpected delete bucket payload: %#v", deleteBucketPayload)
	}

	// 7. Tunnel List
	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/tunnels", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list tunnels status=%d body=%s", res.Code, res.Body.String())
	}
	var tunnelsPayload map[string]interface{}
	mustDecode(t, res, &tunnelsPayload)
	if tunnelsPayload["success"] != true || len(arrayValue(tunnelsPayload["tunnels"])) != 1 {
		t.Fatalf("unexpected tunnels payload: %#v", tunnelsPayload)
	}

	// 8. Tunnel Create
	res = perform(service, http.MethodPost, "/api/cloudflare/accounts/"+accountID+"/tunnels", `{"name":"test-tunnel"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create tunnel status=%d body=%s", res.Code, res.Body.String())
	}
	var createTunnelPayload map[string]interface{}
	mustDecode(t, res, &createTunnelPayload)
	if createTunnelPayload["success"] != true || objectValue(createTunnelPayload["tunnel"])["name"] != "test-tunnel" {
		t.Fatalf("unexpected create tunnel payload: %#v", createTunnelPayload)
	}
	tunnelID := stringValue(objectValue(createTunnelPayload["tunnel"])["id"], "")

	// 9. Tunnel Detail
	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/tunnels/"+tunnelID, "")
	if res.Code != http.StatusOK {
		t.Fatalf("get tunnel status=%d body=%s", res.Code, res.Body.String())
	}
	var tunnelPayload map[string]interface{}
	mustDecode(t, res, &tunnelPayload)
	if tunnelPayload["success"] != true || objectValue(tunnelPayload["tunnel"])["name"] != "t-smoke" {
		t.Fatalf("unexpected tunnel payload: %#v", tunnelPayload)
	}

	// 10. Tunnel configuration read
	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/tunnels/"+tunnelID+"/configuration", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get configuration status=%d body=%s", res.Code, res.Body.String())
	}
	var configPayload map[string]interface{}
	mustDecode(t, res, &configPayload)
	if configPayload["success"] != true || len(arrayValue(objectValue(configPayload["config"])["ingress"])) != 1 {
		t.Fatalf("unexpected config payload: %#v", configPayload)
	}

	// 11. Tunnel configuration update
	res = perform(service, http.MethodPut, "/api/cloudflare/accounts/"+accountID+"/tunnels/"+tunnelID+"/configuration", `{"config":{"ingress":[{"service":"http://localhost:9090"}]}}`)
	if res.Code != http.StatusOK {
		t.Fatalf("put configuration status=%d body=%s", res.Code, res.Body.String())
	}
	var updateConfigPayload map[string]interface{}
	mustDecode(t, res, &updateConfigPayload)
	if updateConfigPayload["success"] != true {
		t.Fatalf("unexpected update config payload: %#v", updateConfigPayload)
	}

	// 12. Tunnel Token
	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/tunnels/"+tunnelID+"/token", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get token status=%d body=%s", res.Code, res.Body.String())
	}
	var tokenPayload map[string]interface{}
	mustDecode(t, res, &tokenPayload)
	if tokenPayload["success"] != true || tokenPayload["token"] != "token-smoke" {
		t.Fatalf("unexpected token payload: %#v", tokenPayload)
	}

	// 13. Tunnel Connections
	res = perform(service, http.MethodGet, "/api/cloudflare/accounts/"+accountID+"/tunnels/"+tunnelID+"/connections", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get connections status=%d body=%s", res.Code, res.Body.String())
	}
	var connsPayload map[string]interface{}
	mustDecode(t, res, &connsPayload)
	if connsPayload["success"] != true || len(arrayValue(connsPayload["connections"])) != 1 {
		t.Fatalf("unexpected connections payload: %#v", connsPayload)
	}

	// 14. Tunnel Update name
	res = perform(service, http.MethodPatch, "/api/cloudflare/accounts/"+accountID+"/tunnels/"+tunnelID, `{"name":"renamed-tunnel"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("patch tunnel status=%d body=%s", res.Code, res.Body.String())
	}
	var patchPayload map[string]interface{}
	mustDecode(t, res, &patchPayload)
	if patchPayload["success"] != true || objectValue(patchPayload["tunnel"])["name"] != "renamed-tunnel" {
		t.Fatalf("unexpected patch payload: %#v", patchPayload)
	}

	// 15. Tunnel Delete
	res = perform(service, http.MethodDelete, "/api/cloudflare/accounts/"+accountID+"/tunnels/"+tunnelID, "")
	if res.Code != http.StatusOK {
		t.Fatalf("delete tunnel status=%d body=%s", res.Code, res.Body.String())
	}
	var deleteTunnelPayload map[string]interface{}
	mustDecode(t, res, &deleteTunnelPayload)
	if deleteTunnelPayload["success"] != true {
		t.Fatalf("unexpected delete tunnel payload: %#v", deleteTunnelPayload)
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

func quoteJSON(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func fakeCloudflareAPI(t *testing.T) *httptest.Server {
	t.Helper()
	zones := []map[string]interface{}{{
		"id":           "zone_1",
		"name":         "example.com",
		"status":       "active",
		"paused":       false,
		"type":         "full",
		"name_servers": []string{"ada.ns.cloudflare.com", "bob.ns.cloudflare.com"},
		"created_on":   "2026-01-01T00:00:00Z",
		"modified_on":  "2026-01-02T00:00:00Z",
	}}
	recordsByZone := map[string][]map[string]interface{}{
		"zone_1": {{
			"id":          "rec_1",
			"type":        "A",
			"name":        "www",
			"content":     "1.1.1.1",
			"proxied":     false,
			"ttl":         1,
			"created_on":  "2026-01-01T00:00:00Z",
			"modified_on": "2026-01-02T00:00:00Z",
		}},
	}
	nextRecord := 2
	workers := []map[string]interface{}{{
		"id":          "worker-smoke",
		"created_on":  "2026-01-01T00:00:00Z",
		"modified_on": "2026-01-02T00:00:00Z",
		"etag":        "etag-smoke",
	}}
	workerScripts := map[string]string{
		"worker-smoke": `export default { async fetch() { return new Response("ok"); } };`,
	}
	workerRoutes := []map[string]interface{}{{
		"id":      "wr_1",
		"pattern": "example.com/*",
		"script":  "worker-smoke",
	}}
	workerDomains := []map[string]interface{}{{
		"id":          "wd_1",
		"hostname":    "worker.example.com",
		"service":     "worker-smoke",
		"environment": "production",
		"zone_id":     "zone_1",
		"zone_name":   "example.com",
	}}
	nextWorkerRoute := 2
	nextWorkerDomain := 2
	pagesProjects := []map[string]interface{}{{
		"name":              "pages-smoke",
		"subdomain":         "pages-smoke.pages.dev",
		"domains":           []string{"pages.example.com"},
		"created_on":        "2026-01-01T00:00:00Z",
		"production_branch": "main",
		"latest_deployment": map[string]interface{}{
			"id":           "pd_1",
			"url":          "https://pages-smoke.pages.dev",
			"created_on":   "2026-01-02T00:00:00Z",
			"latest_stage": map[string]interface{}{"status": "success"},
		},
	}}
	pagesDeployments := map[string][]map[string]interface{}{
		"pages-smoke": {{
			"id":           "pd_1",
			"url":          "https://pages-smoke.pages.dev",
			"environment":  "production",
			"latest_stage": map[string]interface{}{"status": "success"},
			"created_on":   "2026-01-02T00:00:00Z",
			"source":       map[string]interface{}{"type": "github"},
			"build_config": map[string]interface{}{"build_command": "npm run build"},
		}},
	}
	pagesDomains := map[string][]map[string]interface{}{
		"pages-smoke": {{
			"id":              "pdom_1",
			"name":            "pages.example.com",
			"status":          "active",
			"validation_data": map[string]interface{}{"status": "active"},
			"created_on":      "2026-01-03T00:00:00Z",
		}},
	}
	nextPagesDomain := 2
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.URL.Path == "/client/v4/user" {
			token = r.Header.Get("X-Auth-Key")
		}
		if token != "smoke-cf-token" && token != "smoke-cf-token-2" {
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success": false,
				"errors":  []map[string]interface{}{{"message": "bad token"}},
			})
			return
		}
		switch r.URL.Path {
		case "/client/v4/user/tokens/verify":
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result":  map[string]interface{}{"status": "active", "expires_on": "2030-01-01T00:00:00Z"},
			})
		case "/client/v4/user":
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result":  map[string]interface{}{"id": "user-smoke"},
			})
		case "/client/v4/accounts":
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result":  []map[string]interface{}{{"id": "cf-account-1", "name": "Smoke Account"}},
			})
		case "/client/v4/graphql":
			writeJSON(w, http.StatusOK, fakeGraphQLAnalytics())
		case "/client/v4/zones":
			switch r.Method {
			case http.MethodGet:
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success":     true,
					"result":      zones,
					"result_info": map[string]interface{}{"page": 1, "per_page": 50, "count": len(zones), "total_count": len(zones)},
				})
			case http.MethodPost:
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				zone := map[string]interface{}{
					"id":           "zone_new",
					"name":         body["name"],
					"status":       "pending",
					"paused":       false,
					"type":         "full",
					"name_servers": []string{"new.ns.cloudflare.com"},
					"created_on":   "2026-01-03T00:00:00Z",
				}
				zones = append(zones, zone)
				recordsByZone["zone_new"] = []map[string]interface{}{}
				writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": zone})
			default:
				t.Fatalf("unexpected fake Cloudflare method %s %s", r.Method, r.URL.Path)
			}
		default:
			if strings.HasPrefix(r.URL.Path, "/client/v4/accounts/cf-account-1/pages/projects") {
				handleFakePagesPath(t, w, r, &pagesProjects, pagesDeployments, pagesDomains, &nextPagesDomain)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/client/v4/accounts/cf-account-1/workers/") {
				handleFakeWorkerPath(t, w, r, &workers, workerScripts, &workerDomains, &nextWorkerDomain)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/client/v4/accounts/cf-account-1/r2") {
				handleFakeR2Path(t, w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/client/v4/accounts/cf-account-1/cfd_tunnel") {
				handleFakeTunnelPath(t, w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/client/v4/zones/") {
				handleFakeZonePath(t, w, r, &zones, recordsByZone, &nextRecord, &workerRoutes, &nextWorkerRoute)
				return
			}
			t.Fatalf("unexpected fake Cloudflare path %s", r.URL.Path)
		}
	}))
}

func handleFakeWorkerPath(t *testing.T, w http.ResponseWriter, r *http.Request, workers *[]map[string]interface{}, workerScripts map[string]string, workerDomains *[]map[string]interface{}, nextWorkerDomain *int) {
	t.Helper()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/client/v4/accounts/cf-account-1/workers/"), "/")
	if len(parts) == 1 && parts[0] == "scripts" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": *workers})
		return
	}
	if len(parts) == 1 && parts[0] == "subdomain" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"subdomain": "smoke-workers"}})
		return
	}
	if len(parts) >= 1 && parts[0] == "domains" {
		switch {
		case len(parts) == 1 && r.Method == http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": *workerDomains})
		case len(parts) == 1 && r.Method == http.MethodPut:
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			domain := map[string]interface{}{
				"id":          fmt.Sprintf("wd_%d", *nextWorkerDomain),
				"hostname":    body["hostname"],
				"service":     body["service"],
				"environment": body["environment"],
				"zone_id":     body["zone_id"],
				"zone_name":   "example.com",
			}
			*nextWorkerDomain = *nextWorkerDomain + 1
			*workerDomains = append(*workerDomains, domain)
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": domain})
		case len(parts) == 2 && r.Method == http.MethodDelete:
			domainID := parts[1]
			filtered := (*workerDomains)[:0]
			for _, domain := range *workerDomains {
				if stringValue(domain["id"], "") != domainID {
					filtered = append(filtered, domain)
				}
			}
			*workerDomains = filtered
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"id": domainID}})
		default:
			t.Fatalf("unexpected fake Cloudflare worker domain path %s", r.URL.Path)
		}
		return
	}
	if len(parts) < 2 || parts[0] != "scripts" {
		t.Fatalf("unexpected fake Cloudflare worker path %s", r.URL.Path)
	}
	scriptName := parts[1]
	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		script, ok := workerScripts[scriptName]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "errors": []map[string]interface{}{{"message": "script not found"}}})
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(script))
	case len(parts) == 2 && r.Method == http.MethodPut:
		script := extractFakeWorkerScript(t, r)
		workerScripts[scriptName] = script
		updated := map[string]interface{}{
			"id":          scriptName,
			"created_on":  "2026-01-04T00:00:00Z",
			"modified_on": "2026-01-04T00:00:00Z",
			"etag":        "etag-updated",
		}
		filtered := (*workers)[:0]
		for _, worker := range *workers {
			if stringValue(worker["id"], "") != scriptName {
				filtered = append(filtered, worker)
			}
		}
		*workers = append(filtered, updated)
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": updated})
	case len(parts) == 2 && r.Method == http.MethodDelete:
		delete(workerScripts, scriptName)
		filtered := (*workers)[:0]
		for _, worker := range *workers {
			if stringValue(worker["id"], "") != scriptName {
				filtered = append(filtered, worker)
			}
		}
		*workers = filtered
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"id": scriptName}})
	case len(parts) == 3 && parts[2] == "subdomain" && r.Method == http.MethodPost:
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"enabled": boolValue(body["enabled"])}})
	case len(parts) == 3 && parts[2] == "analytics" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"requests": 12, "errors": 1, "scriptName": scriptName}})
	default:
		t.Fatalf("unexpected fake Cloudflare worker path %s", r.URL.Path)
	}
}

func handleFakePagesPath(t *testing.T, w http.ResponseWriter, r *http.Request, projects *[]map[string]interface{}, deployments map[string][]map[string]interface{}, domains map[string][]map[string]interface{}, nextDomain *int) {
	t.Helper()
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/client/v4/accounts/cf-account-1/pages/projects"), "/")
	parts := []string{}
	if trimmed != "" {
		parts = strings.Split(trimmed, "/")
	}
	if len(parts) == 0 && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": *projects})
		return
	}
	if len(parts) == 0 {
		t.Fatalf("unexpected fake Cloudflare pages path %s", r.URL.Path)
	}
	projectName := parts[0]
	switch {
	case len(parts) == 1 && r.Method == http.MethodDelete:
		filtered := (*projects)[:0]
		for _, project := range *projects {
			if stringValue(project["name"], "") != projectName {
				filtered = append(filtered, project)
			}
		}
		*projects = filtered
		delete(deployments, projectName)
		delete(domains, projectName)
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"name": projectName}})
	case len(parts) == 2 && parts[1] == "deployments" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": deployments[projectName]})
	case len(parts) == 3 && parts[1] == "deployments" && r.Method == http.MethodDelete:
		deploymentID := parts[2]
		filtered := deployments[projectName][:0]
		for _, deployment := range deployments[projectName] {
			if stringValue(deployment["id"], "") != deploymentID {
				filtered = append(filtered, deployment)
			}
		}
		deployments[projectName] = filtered
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"id": deploymentID}})
	case len(parts) == 2 && parts[1] == "domains" && r.Method == http.MethodGet:
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": domains[projectName]})
	case len(parts) == 2 && parts[1] == "domains" && r.Method == http.MethodPost:
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		domain := map[string]interface{}{
			"id":              fmt.Sprintf("pdom_%d", *nextDomain),
			"name":            body["name"],
			"status":          "pending",
			"validation_data": map[string]interface{}{"status": "pending"},
			"created_on":      "2026-01-04T00:00:00Z",
		}
		*nextDomain = *nextDomain + 1
		domains[projectName] = append(domains[projectName], domain)
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": domain})
	case len(parts) == 3 && parts[1] == "domains" && r.Method == http.MethodDelete:
		domainName := parts[2]
		filtered := domains[projectName][:0]
		for _, domain := range domains[projectName] {
			if stringValue(domain["name"], "") != domainName {
				filtered = append(filtered, domain)
			}
		}
		domains[projectName] = filtered
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"name": domainName}})
	default:
		t.Fatalf("unexpected fake Cloudflare pages path %s", r.URL.Path)
	}
}

func extractFakeWorkerScript(t *testing.T, r *http.Request) string {
	t.Helper()
	_, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse worker multipart content type: %v", err)
	}
	reader := multipart.NewReader(r.Body, params["boundary"])
	for {
		part, err := reader.NextPart()
		if err != nil {
			t.Fatalf("read worker multipart: %v", err)
		}
		if strings.Contains(part.Header.Get("Content-Type"), "javascript") || strings.HasSuffix(part.FileName(), ".js") {
			raw, _ := io.ReadAll(part)
			return string(raw)
		}
	}
}

func handleFakeZonePath(t *testing.T, w http.ResponseWriter, r *http.Request, zones *[]map[string]interface{}, recordsByZone map[string][]map[string]interface{}, nextRecord *int, workerRoutes *[]map[string]interface{}, nextWorkerRoute *int) {
	t.Helper()
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/client/v4/zones/"), "/")
	zoneID := parts[0]
	if len(parts) == 1 && r.Method == http.MethodDelete {
		filtered := (*zones)[:0]
		for _, zone := range *zones {
			if stringValue(zone["id"], "") != zoneID {
				filtered = append(filtered, zone)
			}
		}
		*zones = filtered
		delete(recordsByZone, zoneID)
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"id": zoneID}})
		return
	}
	if len(parts) == 2 && parts[1] == "purge_cache" && r.Method == http.MethodPost {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"id": zoneID}})
		return
	}
	if len(parts) == 3 && parts[1] == "settings" && parts[2] == "ssl" {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": map[string]interface{}{
					"value":       "full",
					"modified_on": "2026-01-05T00:00:00Z",
					"editable":    true,
				},
			})
			return
		}
		if r.Method == http.MethodPatch {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": map[string]interface{}{
					"value":       body["value"],
					"modified_on": "2026-01-06T00:00:00Z",
				},
			})
			return
		}
	}
	if len(parts) == 3 && parts[1] == "ssl" && parts[2] == "certificate_packs" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"result": []map[string]interface{}{{
				"id":                    "cert_1",
				"type":                  "universal",
				"hosts":                 []string{"example.com", "*.example.com"},
				"status":                "active",
				"validity_days":         90,
				"certificate_authority": "google",
				"primary":               true,
			}},
		})
		return
	}
	if len(parts) == 3 && parts[1] == "ssl" && parts[2] == "verification" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true,
			"result":  []map[string]interface{}{{"certificate_status": "active"}},
		})
		return
	}
	if len(parts) >= 2 && parts[1] == "workers" {
		if len(parts) == 3 && parts[2] == "routes" {
			switch r.Method {
			case http.MethodGet:
				writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": *workerRoutes})
			case http.MethodPost:
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				route := map[string]interface{}{
					"id":      fmt.Sprintf("wr_%d", *nextWorkerRoute),
					"pattern": body["pattern"],
					"script":  body["script"],
				}
				*nextWorkerRoute = *nextWorkerRoute + 1
				*workerRoutes = append(*workerRoutes, route)
				writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": route})
			default:
				t.Fatalf("unexpected fake Cloudflare worker route method %s %s", r.Method, r.URL.Path)
			}
			return
		}
		if len(parts) == 4 && parts[2] == "routes" {
			routeID := parts[3]
			switch r.Method {
			case http.MethodPut:
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				for index, route := range *workerRoutes {
					if stringValue(route["id"], "") == routeID {
						route["pattern"] = body["pattern"]
						route["script"] = body["script"]
						(*workerRoutes)[index] = route
						writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": route})
						return
					}
				}
				writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "errors": []map[string]interface{}{{"message": "route not found"}}})
			case http.MethodDelete:
				filtered := (*workerRoutes)[:0]
				for _, route := range *workerRoutes {
					if stringValue(route["id"], "") != routeID {
						filtered = append(filtered, route)
					}
				}
				*workerRoutes = filtered
				writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"id": routeID}})
			default:
				t.Fatalf("unexpected fake Cloudflare worker route method %s %s", r.Method, r.URL.Path)
			}
			return
		}
		t.Fatalf("unexpected fake Cloudflare worker route path %s", r.URL.Path)
	}
	if len(parts) < 2 || parts[1] != "dns_records" {
		t.Fatalf("unexpected fake Cloudflare zone path %s", r.URL.Path)
	}
	records := recordsByZone[zoneID]
	if len(parts) == 2 {
		switch r.Method {
		case http.MethodGet:
			filtered := []map[string]interface{}{}
			for _, record := range records {
				if queryType := r.URL.Query().Get("type"); queryType != "" && stringValue(record["type"], "") != queryType {
					continue
				}
				if queryName := r.URL.Query().Get("name"); queryName != "" && stringValue(record["name"], "") != queryName {
					continue
				}
				filtered = append(filtered, record)
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success":     true,
				"result":      filtered,
				"result_info": map[string]interface{}{"page": 1, "per_page": 100, "count": len(filtered), "total_count": len(filtered)},
			})
		case http.MethodPost:
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			id := fmt.Sprintf("rec_%d", *nextRecord)
			*nextRecord = *nextRecord + 1
			record := map[string]interface{}{
				"id":          id,
				"type":        body["type"],
				"name":        body["name"],
				"content":     body["content"],
				"proxied":     body["proxied"],
				"ttl":         body["ttl"],
				"priority":    body["priority"],
				"created_on":  "2026-01-03T00:00:00Z",
				"modified_on": "2026-01-03T00:00:00Z",
			}
			recordsByZone[zoneID] = append(records, record)
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": record})
		default:
			t.Fatalf("unexpected fake Cloudflare method %s %s", r.Method, r.URL.Path)
		}
		return
	}
	recordID := parts[2]
	switch r.Method {
	case http.MethodPatch:
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		for index, record := range records {
			if stringValue(record["id"], "") == recordID {
				for key, value := range body {
					record[key] = value
				}
				record["modified_on"] = "2026-01-04T00:00:00Z"
				records[index] = record
				recordsByZone[zoneID] = records
				writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": record})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"success": false, "errors": []map[string]interface{}{{"message": "record not found"}}})
	case http.MethodDelete:
		filtered := records[:0]
		for _, record := range records {
			if stringValue(record["id"], "") != recordID {
				filtered = append(filtered, record)
			}
		}
		recordsByZone[zoneID] = filtered
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "result": map[string]interface{}{"id": recordID}})
	default:
		t.Fatalf("unexpected fake Cloudflare method %s %s", r.Method, r.URL.Path)
	}
}

func fakeGraphQLAnalytics() map[string]interface{} {
	return map[string]interface{}{
		"data": map[string]interface{}{
			"viewer": map[string]interface{}{
				"zones": []map[string]interface{}{{
					"totals": []map[string]interface{}{{
						"sum": map[string]interface{}{
							"requests":       100,
							"bytes":          2048,
							"cachedRequests": 40,
							"cachedBytes":    1024,
							"threats":        1,
							"pageViews":      90,
						},
						"uniq": map[string]interface{}{"uniques": 30},
					}},
					"series": []map[string]interface{}{{
						"dimensions": map[string]interface{}{"datetime": "2026-01-01T00:00:00Z"},
						"sum": map[string]interface{}{
							"requests":       100,
							"bytes":          2048,
							"cachedRequests": 40,
							"cachedBytes":    1024,
							"threats":        1,
							"pageViews":      90,
						},
						"uniq": map[string]interface{}{"uniques": 30},
					}},
				}},
			},
		},
	}
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

func handleFakeR2Path(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.EscapedPath(), "/client/v4/accounts/cf-account-1/r2"), "/")
	parts := []string{}
	if trimmed != "" {
		parts = strings.Split(trimmed, "/")
	}
	if len(parts) == 0 {
		t.Fatalf("unexpected fake Cloudflare r2 path %s", r.URL.Path)
	}
	if parts[0] == "buckets" {
		if len(parts) == 1 {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"result": map[string]interface{}{
						"buckets": []map[string]interface{}{{
							"name": "buckets-smoke",
							"creation_date": "2026-01-01T00:00:00Z",
						}},
					},
				})
				return
			}
			if r.Method == http.MethodPost {
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"result": map[string]interface{}{
						"name": body["name"],
						"creation_date": "2026-01-01T00:00:00Z",
					},
				})
				return
			}
		}
		if len(parts) == 2 {
			bucketName := parts[1]
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"result": map[string]interface{}{
						"name": bucketName,
						"public_url_base": "https://pub-r2.example.com",
					},
				})
				return
			}
			if r.Method == http.MethodDelete {
				writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
				return
			}
		}
		if len(parts) == 3 && parts[2] == "objects" {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"result": []map[string]interface{}{{
						"key": "objects-smoke.txt",
						"size": 100,
					}},
					"result_info": map[string]interface{}{
						"delimited": []interface{}{},
						"cursor": nil,
					},
				})
				return
			}
		}
		if len(parts) == 4 && parts[2] == "objects" {
			if r.Method == http.MethodDelete {
				writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
				return
			}
		}
	}
	t.Fatalf("unexpected fake Cloudflare r2 path %s", r.URL.Path)
}

func handleFakeTunnelPath(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/client/v4/accounts/cf-account-1/cfd_tunnel"), "/")
	parts := []string{}
	if trimmed != "" {
		parts = strings.Split(trimmed, "/")
	}
	if len(parts) == 0 {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": []map[string]interface{}{{
					"id": "t1",
					"name": "t-smoke",
					"status": "healthy",
					"created_at": "2026-01-01T00:00:00Z",
				}},
			})
			return
		}
		if r.Method == http.MethodPost {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": map[string]interface{}{
					"id": "t-new",
					"name": body["name"],
					"status": "inactive",
					"created_at": "2026-01-01T00:00:00Z",
				},
			})
			return
		}
	}
	if len(parts) == 1 {
		tunnelId := parts[0]
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": map[string]interface{}{
					"id": tunnelId,
					"name": "t-smoke",
					"status": "healthy",
				},
			})
			return
		}
		if r.Method == http.MethodDelete {
			writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
			return
		}
		if r.Method == http.MethodPatch {
			var body map[string]interface{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": map[string]interface{}{
					"id": tunnelId,
					"name": body["name"],
				},
			})
			return
		}
	}
	if len(parts) == 2 {
		tunnelId := parts[0]
		_ = tunnelId
		if parts[1] == "configurations" {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"result": map[string]interface{}{
						"config": map[string]interface{}{
							"ingress": []interface{}{
								map[string]interface{}{"service": "http://localhost:8080"},
							},
						},
					},
				})
				return
			}
			if r.Method == http.MethodPut {
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"result": body,
				})
				return
			}
		}
		if parts[1] == "token" && r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"result": "token-smoke",
			})
			return
		}
		if parts[1] == "connections" {
			if r.Method == http.MethodGet {
				writeJSON(w, http.StatusOK, map[string]interface{}{
					"success": true,
					"result": []map[string]interface{}{{
						"id": "conn1",
						"client_version": "2026.1.0",
					}},
				})
				return
			}
			if r.Method == http.MethodDelete {
				writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
				return
			}
		}
	}
	t.Fatalf("unexpected fake Cloudflare tunnel path %s", r.URL.Path)
}
