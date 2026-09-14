package m365

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	_ "modernc.org/sqlite"
)

func TestM365Lifecycle(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")
	testToken := fakeJWT(`{"roles":["User.Read.All","User.ReadWrite.All","Organization.Read.All","LicenseAssignment.Read.All","LicenseAssignment.ReadWrite.All","Group.Create","GroupMember.ReadWrite.All","Files.Read.All"]}`)

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"` + testToken + `"}`))
		case r.URL.Path == "/organization":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":[{"displayName":"Contoso","verifiedDomains":[{"name":"contoso.com","isDefault":true},{"name":"campus.contoso.com","isDefault":false}]}]}`))
		case strings.HasPrefix(r.URL.Path, "/users") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"@odata.count":1,"value":[{"id":"user-1","displayName":"Alice","userPrincipalName":"alice@contoso.com","accountEnabled":true,"assignedLicenses":[{"skuId":"c42b9cae-ea4f-4ab7-9717-81576235ccac"}]}]}`))
		case strings.HasPrefix(r.URL.Path, "/users") && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"new-user","displayName":"New User"}`))
		case r.URL.Path == "/subscribedSkus":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":[{"skuId":"sku-1","skuPartNumber":"ENTERPRISEPACK","consumedUnits":2,"subscriptionIds":["sub-1"]} ]}`))
		case r.URL.Path == "/directory/subscriptions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":[{"id":"sub-1","skuId":"sku-1","skuPartNumber":"ENTERPRISEPACK","status":"Enabled","isTrial":false,"nextLifecycleDateTime":"2027-01-15T00:00:00Z"}]}`))
		case strings.HasPrefix(r.URL.Path, "/groups") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":[{"id":"group-1","displayName":"Engineering","securityEnabled":true}]}`))
		case strings.Contains(r.URL.Path, "/members") && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":[{"id":"user-1","displayName":"Alice","userPrincipalName":"alice@contoso.com"}]}`))
		case strings.Contains(r.URL.Path, "/assignLicense"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"updated-object"}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer mock.Close()

	t.Setenv("M365_GRAPH_BASE_URL", mock.URL)
	t.Setenv("M365_LOGIN_BASE_URL", mock.URL)

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createRes := performM365(service, http.MethodPost, "/api/m365/accounts", `{"name":"Contoso","tenantId":"tenant-1","clientId":"client-1","clientSecret":"secret-1"}`)
	if createRes.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", createRes.Code, createRes.Body.String())
	}

	listRes := performM365(service, http.MethodGet, "/api/m365/accounts", "")
	if listRes.Code != http.StatusOK || !strings.Contains(listRes.Body.String(), `"name":"Contoso"`) {
		t.Fatalf("list account status=%d body=%s", listRes.Code, listRes.Body.String())
	}

	verifyRes := performM365(service, http.MethodPost, "/api/m365/accounts/1/verify", "")
	if verifyRes.Code != http.StatusOK || !strings.Contains(verifyRes.Body.String(), `"displayName":"Contoso"`) {
		t.Fatalf("verify status=%d body=%s", verifyRes.Code, verifyRes.Body.String())
	}
	verifiedListRes := performM365(service, http.MethodGet, "/api/m365/accounts", "")
	if verifiedListRes.Code != http.StatusOK || !strings.Contains(verifiedListRes.Body.String(), `"verifiedDomains":["contoso.com","campus.contoso.com"]`) {
		t.Fatalf("verified account list status=%d body=%s", verifiedListRes.Code, verifiedListRes.Body.String())
	}

	permissionsRes := performM365(service, http.MethodGet, "/api/m365/accounts/1/permissions", "")
	if permissionsRes.Code != http.StatusOK || !strings.Contains(permissionsRes.Body.String(), `"grantedCount":8`) || !strings.Contains(permissionsRes.Body.String(), `"Files.Read.All"`) {
		t.Fatalf("permissions status=%d body=%s", permissionsRes.Code, permissionsRes.Body.String())
	}

	usersRes := performM365(service, http.MethodGet, "/api/m365/accounts/1/users?top=10", "")
	if usersRes.Code != http.StatusOK || !strings.Contains(usersRes.Body.String(), `"Alice"`) || !strings.Contains(usersRes.Body.String(), `"assignedLicenses"`) {
		t.Fatalf("users status=%d body=%s", usersRes.Code, usersRes.Body.String())
	}

	createUserRes := performM365(service, http.MethodPost, "/api/m365/accounts/1/users", `{"displayName":"New User","mailNickname":"newuser","userPrincipalName":"newuser@contoso.com","password":"TempPassw0rd!"}`)
	if createUserRes.Code != http.StatusOK || !strings.Contains(createUserRes.Body.String(), `"new-user"`) {
		t.Fatalf("create user status=%d body=%s", createUserRes.Code, createUserRes.Body.String())
	}

	skuRes := performM365(service, http.MethodGet, "/api/m365/accounts/1/licenses/skus", "")
	if skuRes.Code != http.StatusOK || !strings.Contains(skuRes.Body.String(), `"ENTERPRISEPACK"`) || !strings.Contains(skuRes.Body.String(), `"nextLifecycleDateTime":"2027-01-15T00:00:00Z"`) {
		t.Fatalf("sku status=%d body=%s", skuRes.Code, skuRes.Body.String())
	}

	groupRes := performM365(service, http.MethodGet, "/api/m365/accounts/1/groups", "")
	if groupRes.Code != http.StatusOK || !strings.Contains(groupRes.Body.String(), `"Engineering"`) {
		t.Fatalf("groups status=%d body=%s", groupRes.Code, groupRes.Body.String())
	}
}

func TestAccountSecretStoredEncrypted(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")
	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	res := performM365(service, http.MethodPost, "/api/m365/accounts", `{"name":"Contoso","tenantId":"tenant-1","clientId":"client-1","clientSecret":"secret-1"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var secret string
	if err := db.QueryRowContext(context.Background(), `SELECT client_secret FROM m365_accounts WHERE id = 1`).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if secret == "secret-1" {
		t.Fatalf("expected encrypted secret, got plaintext")
	}
}

func TestM365AccountExportImport(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")
	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createRes := performM365(service, http.MethodPost, "/api/m365/accounts", `{"name":"Contoso","tenantId":"tenant-1","clientId":"client-1","clientSecret":"secret-1","defaultDomain":"contoso.com","verifiedDomains":["contoso.com","campus.contoso.com"],"organization":"Contoso Org","enabled":true}`)
	if createRes.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", createRes.Code, createRes.Body.String())
	}

	exportRes := performM365(service, http.MethodGet, "/api/m365/export/accounts", "")
	if exportRes.Code != http.StatusOK {
		t.Fatalf("export accounts status=%d body=%s", exportRes.Code, exportRes.Body.String())
	}
	if !strings.Contains(exportRes.Body.String(), `"clientSecret":"secret-1"`) || !strings.Contains(exportRes.Body.String(), `"verifiedDomains":["contoso.com","campus.contoso.com"]`) {
		t.Fatalf("unexpected export payload body=%s", exportRes.Body.String())
	}

	importRes := performM365(service, http.MethodPost, "/api/m365/import/accounts", `{"overwrite":true,"accounts":[{"name":"Fabrikam","tenantId":"tenant-2","clientId":"client-2","clientSecret":"secret-2","defaultDomain":"fabrikam.com","verifiedDomains":["fabrikam.com"],"organization":"Fabrikam Org","enabled":false}]}`)
	if importRes.Code != http.StatusOK {
		t.Fatalf("import accounts status=%d body=%s", importRes.Code, importRes.Body.String())
	}

	listRes := performM365(service, http.MethodGet, "/api/m365/accounts", "")
	if listRes.Code != http.StatusOK {
		t.Fatalf("list accounts status=%d body=%s", listRes.Code, listRes.Body.String())
	}
	if strings.Contains(listRes.Body.String(), `"tenantId":"tenant-1"`) || !strings.Contains(listRes.Body.String(), `"tenantId":"tenant-2"`) || !strings.Contains(listRes.Body.String(), `"enabled":false`) {
		t.Fatalf("unexpected accounts after import body=%s", listRes.Body.String())
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var secret string
	if err := db.QueryRowContext(context.Background(), `SELECT client_secret FROM m365_accounts WHERE tenant_id = 'tenant-2' AND client_id = 'client-2'`).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if secret == "secret-2" {
		t.Fatalf("expected imported secret to be encrypted, got plaintext")
	}
}

func TestUserPatchSupportsPasswordReset(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")
	testToken := fakeJWT(`{"roles":["User.ReadWrite.All"]}`)

	var patchBody string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"` + testToken + `"}`))
		case r.URL.Path == "/users/user-1" && r.Method == http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			patchBody = string(body)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer mock.Close()

	t.Setenv("M365_GRAPH_BASE_URL", mock.URL)
	t.Setenv("M365_LOGIN_BASE_URL", mock.URL)

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createRes := performM365(service, http.MethodPost, "/api/m365/accounts", `{"name":"Contoso","tenantId":"tenant-1","clientId":"client-1","clientSecret":"secret-1"}`)
	if createRes.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", createRes.Code, createRes.Body.String())
	}

	patchRes := performM365(service, http.MethodPatch, "/api/m365/accounts/1/users/user-1", `{"displayName":"Alice Updated","password":"TempPassw0rd!","forceChangePasswordNextSignIn":false}`)
	if patchRes.Code != http.StatusOK {
		t.Fatalf("patch user status=%d body=%s", patchRes.Code, patchRes.Body.String())
	}
	if !strings.Contains(patchBody, `"passwordProfile"`) || !strings.Contains(patchBody, `"password":"TempPassw0rd!"`) {
		t.Fatalf("expected passwordProfile in graph patch body, got %s", patchBody)
	}
	if !strings.Contains(patchBody, `"forceChangePasswordNextSignIn":false`) {
		t.Fatalf("expected forceChangePasswordNextSignIn in graph patch body, got %s", patchBody)
	}
}

func TestUserPatchOmitsBlankOptionalProfileFields(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")
	testToken := fakeJWT(`{"roles":["User.ReadWrite.All"]}`)

	var patchBody string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"` + testToken + `"}`))
		case r.URL.Path == "/users/user-1" && r.Method == http.MethodPatch:
			body, _ := io.ReadAll(r.Body)
			patchBody = string(body)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer mock.Close()

	t.Setenv("M365_GRAPH_BASE_URL", mock.URL)
	t.Setenv("M365_LOGIN_BASE_URL", mock.URL)

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createRes := performM365(service, http.MethodPost, "/api/m365/accounts", `{"name":"Contoso","tenantId":"tenant-1","clientId":"client-1","clientSecret":"secret-1"}`)
	if createRes.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", createRes.Code, createRes.Body.String())
	}

	patchRes := performM365(service, http.MethodPatch, "/api/m365/accounts/1/users/user-1", `{"displayName":"Alice Updated","department":"","jobTitle":" ","officeLocation":"","usageLocation":" ","accountEnabled":true}`)
	if patchRes.Code != http.StatusOK {
		t.Fatalf("patch user status=%d body=%s", patchRes.Code, patchRes.Body.String())
	}
	if strings.Contains(patchBody, `"department"`) {
		t.Fatalf("expected blank department to be omitted from graph patch body, got %s", patchBody)
	}
	if strings.Contains(patchBody, `"jobTitle"`) {
		t.Fatalf("expected blank jobTitle to be omitted from graph patch body, got %s", patchBody)
	}
	if strings.Contains(patchBody, `"officeLocation"`) {
		t.Fatalf("expected blank officeLocation to be omitted from graph patch body, got %s", patchBody)
	}
	if strings.Contains(patchBody, `"usageLocation"`) {
		t.Fatalf("expected blank usageLocation to be omitted from graph patch body, got %s", patchBody)
	}
	if !strings.Contains(patchBody, `"displayName":"Alice Updated"`) || !strings.Contains(patchBody, `"accountEnabled":true`) {
		t.Fatalf("expected non-blank fields to remain in graph patch body, got %s", patchBody)
	}
}

func TestInviteRegistrationLifecycle(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")
	testToken := fakeJWT(`{"roles":["User.ReadWrite.All","LicenseAssignment.ReadWrite.All"]}`)

	var createUserBody string
	var assignLicenseBody string
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"` + testToken + `"}`))
		case r.URL.Path == "/users" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			createUserBody = string(body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"registered-user","displayName":"Student One"}`))
		case r.URL.Path == "/users/registered-user/assignLicense" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			assignLicenseBody = string(body)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer mock.Close()

	t.Setenv("M365_GRAPH_BASE_URL", mock.URL)
	t.Setenv("M365_LOGIN_BASE_URL", mock.URL)

	service := New(config.Config{
		DataDir: t.TempDir(),
		DBName:  "data.db",
	})

	createAccountRes := performM365(service, http.MethodPost, "/api/m365/accounts", `{"name":"Contoso","tenantId":"tenant-1","clientId":"client-1","clientSecret":"secret-1","defaultDomain":"contoso.com"}`)
	if createAccountRes.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", createAccountRes.Code, createAccountRes.Body.String())
	}
	createSecondAccountRes := performM365(service, http.MethodPost, "/api/m365/accounts", `{"name":"Fabrikam","tenantId":"tenant-2","clientId":"client-2","clientSecret":"secret-2","defaultDomain":"fabrikam.com"}`)
	if createSecondAccountRes.Code != http.StatusOK {
		t.Fatalf("create second account status=%d body=%s", createSecondAccountRes.Code, createSecondAccountRes.Body.String())
	}

	createPageRes := performM365(service, http.MethodPost, "/api/m365/public-pages", `{"name":"2026 新生批次","accountIds":[1,2],"domains":["contoso.com","fabrikam.com"],"skuIds":["sku-1"],"forceChangePasswordNextSignIn":true}`)
	if createPageRes.Code != http.StatusOK {
		t.Fatalf("create public page status=%d body=%s", createPageRes.Code, createPageRes.Body.String())
	}

	createInviteRes := performM365(service, http.MethodPost, "/api/m365/invite-codes", `{"publicPageId":1,"quantity":2}`)
	if createInviteRes.Code != http.StatusOK {
		t.Fatalf("create invite codes status=%d body=%s", createInviteRes.Code, createInviteRes.Body.String())
	}
	createInviteData := decodeEnvelopeData(t, createInviteRes)
	codes := stringArray(createInviteData["codes"])
	batchID := strings.TrimSpace(stringValue(createInviteData["batchId"], ""))
	if len(codes) != 2 {
		t.Fatalf("expected 2 generated invite codes, body=%s", createInviteRes.Body.String())
	}
	if batchID == "" {
		t.Fatalf("expected generated batchId, body=%s", createInviteRes.Body.String())
	}
	code := strings.TrimSpace(codes[0])
	if code == "" || strings.TrimSpace(codes[1]) == "" {
		t.Fatalf("expected generated invite code, body=%s", createInviteRes.Body.String())
	}
	if numberValue(createInviteData["createdCount"]) != 2 {
		t.Fatalf("expected createdCount=2 body=%s", createInviteRes.Body.String())
	}

	listPageRes := performM365(service, http.MethodGet, "/api/m365/public-pages", "")
	if listPageRes.Code != http.StatusOK || !strings.Contains(listPageRes.Body.String(), `"2026 新生批次"`) || !strings.Contains(listPageRes.Body.String(), `"inviteCodeCount":2`) {
		t.Fatalf("list public pages status=%d body=%s", listPageRes.Code, listPageRes.Body.String())
	}

	listCodeRes := performM365(service, http.MethodGet, "/api/m365/invite-codes", "")
	if listCodeRes.Code != http.StatusOK || !strings.Contains(listCodeRes.Body.String(), `"code":"`+code+`"`) || !strings.Contains(listCodeRes.Body.String(), `"used":false`) {
		t.Fatalf("list invite codes status=%d body=%s", listCodeRes.Code, listCodeRes.Body.String())
	}

	publicInviteRes := performM365(service, http.MethodGet, "/api/m365/public/invites/"+code, "")
	if publicInviteRes.Code != http.StatusOK || !strings.Contains(publicInviteRes.Body.String(), `"domains":["contoso.com","fabrikam.com"]`) || !strings.Contains(publicInviteRes.Body.String(), `"targetCount":2`) {
		t.Fatalf("public invite status=%d body=%s", publicInviteRes.Code, publicInviteRes.Body.String())
	}

	registerDescriptorRes := performM365(service, http.MethodGet, "/api/m365/public/register?code="+code, "")
	if registerDescriptorRes.Code != http.StatusOK || !strings.Contains(registerDescriptorRes.Body.String(), `"method":"POST"`) || !strings.Contains(registerDescriptorRes.Body.String(), `"targetCount":2`) {
		t.Fatalf("register descriptor status=%d body=%s", registerDescriptorRes.Code, registerDescriptorRes.Body.String())
	}

	registerRes := performM365(service, http.MethodPost, "/api/m365/public/register", `{"code":"`+code+`","domain":"fabrikam.com","mailNickname":"student1","displayName":"Student One"}`)
	if registerRes.Code != http.StatusOK || !strings.Contains(registerRes.Body.String(), `"status":"success"`) || !strings.Contains(registerRes.Body.String(), `"userPrincipalName":"student1@fabrikam.com"`) {
		t.Fatalf("register status=%d body=%s", registerRes.Code, registerRes.Body.String())
	}
	registerData := decodeEnvelopeData(t, registerRes)
	initialPassword := strings.TrimSpace(stringValue(registerData["initialPassword"], ""))
	if initialPassword == "" {
		t.Fatalf("expected generated initialPassword, body=%s", registerRes.Body.String())
	}
	if !strings.Contains(createUserBody, `"userPrincipalName":"student1@fabrikam.com"`) || !strings.Contains(createUserBody, `"usageLocation":"CN"`) {
		t.Fatalf("unexpected create user body: %s", createUserBody)
	}
	if !strings.Contains(createUserBody, `"forceChangePasswordNextSignIn":true`) {
		t.Fatalf("expected forceChangePasswordNextSignIn in body: %s", createUserBody)
	}
	if !strings.Contains(createUserBody, `"password":"`+initialPassword+`"`) {
		t.Fatalf("expected generated password in create user body: %s", createUserBody)
	}
	if !strings.Contains(assignLicenseBody, `"skuId":"sku-1"`) {
		t.Fatalf("unexpected assign license body: %s", assignLicenseBody)
	}

	registrationRes := performM365(service, http.MethodGet, "/api/m365/registrations", "")
	if registrationRes.Code != http.StatusOK || !strings.Contains(registrationRes.Body.String(), `"status":"success"`) || !strings.Contains(registrationRes.Body.String(), `"Student One"`) || !strings.Contains(registrationRes.Body.String(), `"accountId":2`) || !strings.Contains(registrationRes.Body.String(), `"publicPageName":"2026 新生批次"`) || !strings.Contains(registrationRes.Body.String(), `"inviteCode":"`+code+`"`) {
		t.Fatalf("registrations status=%d body=%s", registrationRes.Code, registrationRes.Body.String())
	}
	registrationData := decodeEnvelopeData(t, registrationRes)
	registrationItems := objectArray(registrationData["items"])
	if len(registrationItems) != 1 {
		t.Fatalf("expected 1 registration item, body=%s", registrationRes.Body.String())
	}
	registrationID := numberValue(registrationItems[0]["id"])
	if registrationID <= 0 {
		t.Fatalf("expected valid registration id, body=%s", registrationRes.Body.String())
	}

	deleteRegistrationRes := performM365(service, http.MethodDelete, "/api/m365/registrations", fmt.Sprintf(`{"ids":[%d]}`, registrationID))
	if deleteRegistrationRes.Code != http.StatusOK || !strings.Contains(deleteRegistrationRes.Body.String(), `"deletedCount":1`) {
		t.Fatalf("delete registrations status=%d body=%s", deleteRegistrationRes.Code, deleteRegistrationRes.Body.String())
	}

	registrationAfterDeleteRes := performM365(service, http.MethodGet, "/api/m365/registrations", "")
	if registrationAfterDeleteRes.Code != http.StatusOK || strings.Contains(registrationAfterDeleteRes.Body.String(), `"Student One"`) {
		t.Fatalf("registrations after delete status=%d body=%s", registrationAfterDeleteRes.Code, registrationAfterDeleteRes.Body.String())
	}

	usedInviteRes := performM365(service, http.MethodGet, "/api/m365/public/invites/"+code, "")
	if usedInviteRes.Code != http.StatusOK || !strings.Contains(usedInviteRes.Body.String(), `"available":false`) || !strings.Contains(usedInviteRes.Body.String(), `"availabilityReason":"used"`) {
		t.Fatalf("used invite status=%d body=%s", usedInviteRes.Code, usedInviteRes.Body.String())
	}

	batchDescriptorRes := performM365(service, http.MethodGet, "/api/m365/public/register?batch="+batchID, "")
	if batchDescriptorRes.Code != http.StatusOK || !strings.Contains(batchDescriptorRes.Body.String(), `"inviteCount":2`) || !strings.Contains(batchDescriptorRes.Body.String(), `"availableCount":1`) {
		t.Fatalf("batch register descriptor status=%d body=%s", batchDescriptorRes.Code, batchDescriptorRes.Body.String())
	}

	batchRegisterRes := performM365(service, http.MethodPost, "/api/m365/public/register", `{"batch":"`+batchID+`","domain":"contoso.com","mailNickname":"student2","displayName":"Student Two"}`)
	if batchRegisterRes.Code != http.StatusOK || !strings.Contains(batchRegisterRes.Body.String(), `"status":"success"`) || !strings.Contains(batchRegisterRes.Body.String(), `"userPrincipalName":"student2@contoso.com"`) {
		t.Fatalf("batch register status=%d body=%s", batchRegisterRes.Code, batchRegisterRes.Body.String())
	}
	batchRegisterData := decodeEnvelopeData(t, batchRegisterRes)
	batchInitialPassword := strings.TrimSpace(stringValue(batchRegisterData["initialPassword"], ""))
	if batchInitialPassword == "" {
		t.Fatalf("expected generated initialPassword for batch registration, body=%s", batchRegisterRes.Body.String())
	}
	if !strings.Contains(createUserBody, `"userPrincipalName":"student2@contoso.com"`) {
		t.Fatalf("unexpected batch create user body: %s", createUserBody)
	}
	if !strings.Contains(createUserBody, `"password":"`+batchInitialPassword+`"`) {
		t.Fatalf("expected generated batch password in create user body: %s", createUserBody)
	}

	registrationAfterBatchRes := performM365(service, http.MethodGet, "/api/m365/registrations", "")
	if registrationAfterBatchRes.Code != http.StatusOK || !strings.Contains(registrationAfterBatchRes.Body.String(), `"Student Two"`) || !strings.Contains(registrationAfterBatchRes.Body.String(), `"accountId":1`) {
		t.Fatalf("registrations after batch register status=%d body=%s", registrationAfterBatchRes.Code, registrationAfterBatchRes.Body.String())
	}

	updatePageRes := performM365(service, http.MethodPut, "/api/m365/public-pages/1", `{"name":"已关闭批次","enabled":false,"accountIds":[1,2],"domains":["contoso.com","fabrikam.com"]}`)
	if updatePageRes.Code != http.StatusOK {
		t.Fatalf("update public page status=%d body=%s", updatePageRes.Code, updatePageRes.Body.String())
	}

	updatedPublicInviteRes := performM365(service, http.MethodGet, "/api/m365/public/invites/"+code, "")
	if updatedPublicInviteRes.Code != http.StatusOK || !strings.Contains(updatedPublicInviteRes.Body.String(), `"publicPageEnabled":false`) {
		t.Fatalf("updated public invite status=%d body=%s", updatedPublicInviteRes.Code, updatedPublicInviteRes.Body.String())
	}

	deleteInviteRes := performM365(service, http.MethodDelete, "/api/m365/invite-codes", `{"publicPageId":1,"batchId":"`+batchID+`"}`)
	if deleteInviteRes.Code != http.StatusOK || !strings.Contains(deleteInviteRes.Body.String(), `"deletedCount":2`) {
		t.Fatalf("delete invite batch status=%d body=%s", deleteInviteRes.Code, deleteInviteRes.Body.String())
	}

	listInviteAfterDeleteRes := performM365(service, http.MethodGet, "/api/m365/invite-codes", "")
	if listInviteAfterDeleteRes.Code != http.StatusOK || strings.Contains(listInviteAfterDeleteRes.Body.String(), `"batchId":"`+batchID+`"`) {
		t.Fatalf("list invite codes after batch delete status=%d body=%s", listInviteAfterDeleteRes.Code, listInviteAfterDeleteRes.Body.String())
	}
}

func TestResolveInviteTargetsUsesVerifiedDomains(t *testing.T) {
	accounts := []accountRecord{
		{
			ID:              1,
			Name:            "Contoso",
			DefaultDomain:   "contoso.com",
			VerifiedDomains: []string{"contoso.com", "campus.contoso.com"},
		},
	}

	targets := resolveInviteTargetsFromAccounts(inviteRecord{
		AccountIDs: []int64{1},
	}, accounts)
	if len(targets) != 2 {
		t.Fatalf("expected 2 targets for verified domains, got %#v", targets)
	}

	filteredTargets := resolveInviteTargetsFromAccounts(inviteRecord{
		AccountIDs: []int64{1},
		Domains:    []string{"campus.contoso.com"},
	}, accounts)
	if len(filteredTargets) != 1 || filteredTargets[0].Domain != "campus.contoso.com" {
		t.Fatalf("expected campus.contoso.com target, got %#v", filteredTargets)
	}
}

func TestResolveInviteTargetsPreservesExplicitDomainsForSingleAccount(t *testing.T) {
	accounts := []accountRecord{
		{
			ID:              1,
			Name:            "Contoso",
			DefaultDomain:   "contoso.com",
			VerifiedDomains: []string{"contoso.com"},
		},
	}

	targets := resolveInviteTargetsFromAccounts(inviteRecord{
		AccountIDs: []int64{1},
		Domains:    []string{"085014.xyz", "dsukme.onmicrosoft.com", "dsuk.top"},
	}, accounts)

	if len(targets) != 3 {
		t.Fatalf("expected explicit domains to be preserved for single account, got %#v", targets)
	}

	domainSet := map[string]bool{}
	for _, target := range targets {
		domainSet[target.Domain] = true
	}
	for _, expected := range []string{"085014.xyz", "dsukme.onmicrosoft.com", "dsuk.top"} {
		if !domainSet[expected] {
			t.Fatalf("expected domain %s in targets %#v", expected, targets)
		}
	}
}

func TestEnsureSchemaAddsVerifiedDomainsToLegacyAccountsTable(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")
	dataDir := t.TempDir()
	dbPath := filepath.Join(dataDir, "data.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), `CREATE TABLE m365_accounts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		tenant_id TEXT NOT NULL,
		client_id TEXT NOT NULL,
		client_secret TEXT NOT NULL,
		description TEXT,
		default_domain TEXT,
		organization_name TEXT,
		enabled INTEGER DEFAULT 1,
		last_verified_at DATETIME,
		last_verified_error TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		t.Fatal(err)
	}

	service := New(config.Config{
		DataDir: dataDir,
		DBName:  "data.db",
	})

	opened, err := service.open(context.Background())
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	defer opened.Close()

	res := performM365(service, http.MethodGet, "/api/m365/accounts", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list legacy accounts status=%d body=%s", res.Code, res.Body.String())
	}

	rows, err := opened.QueryContext(context.Background(), `PRAGMA table_info(m365_accounts)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal interface{}
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "verified_domains" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected verified_domains column to be added to legacy m365_accounts table")
	}
}

func TestOneDriveQuotaAggregation(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")
	testToken := fakeJWT(`{"roles":["Files.Read.All"]}`)

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			_, _ = w.Write([]byte(`{"access_token":"` + testToken + `"}`))
		case r.URL.Path == "/users/normal/drive":
			_, _ = w.Write([]byte(`{"id":"drive-normal","driveType":"business","quota":{"deleted":1048576,"remaining":549755813888,"state":"normal","total":1099511627776,"used":549755814400}}`))
		case r.URL.Path == "/users/over/drive":
			_, _ = w.Write([]byte(`{"id":"drive-over","driveType":"business","quota":{"remaining":0,"state":"exceeded","total":10737418240,"used":15456973383}}`))
		case r.URL.Path == "/users/missing/drive":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"ResourceNotFound","message":"User's mysite not found."}}`))
		case r.URL.Path == "/users/legacy/drive":
			_, _ = w.Write([]byte(`{"id":"drive-legacy","driveType":"business","quota":{"deleted":0,"remaining":549755813888,"state":"normal","total":1099511627776}}`))
		case r.URL.Path == "/users/broken/drive":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"InternalServerError","message":"boom"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer mock.Close()

	t.Setenv("M365_GRAPH_BASE_URL", mock.URL)
	t.Setenv("M365_LOGIN_BASE_URL", mock.URL)

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})
	createRes := performM365(service, http.MethodPost, "/api/m365/accounts", `{"name":"Contoso","tenantId":"tenant-1","clientId":"client-1","clientSecret":"secret-1"}`)
	if createRes.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", createRes.Code, createRes.Body.String())
	}

	batchRes := performM365(service, http.MethodGet, "/api/m365/accounts/1/usage/onedrive?userIds=normal,over,missing,legacy,broken,normal", "")
	if batchRes.Code != http.StatusOK {
		t.Fatalf("batch usage status=%d body=%s", batchRes.Code, batchRes.Body.String())
	}
	data := decodeEnvelopeData(t, batchRes)
	items, _ := data["items"].([]interface{})
	if len(items) != 5 {
		t.Fatalf("expected duplicate id to be deduped to 5 items, got %d body=%s", len(items), batchRes.Body.String())
	}
	if data["provisioned"] != float64(3) || data["notProvisioned"] != float64(1) || data["failed"] != float64(1) {
		t.Fatalf("unexpected counters: %#v", data)
	}
	if data["overQuota"] != float64(1) {
		t.Fatalf("expected 1 over-quota user, got %v", data["overQuota"])
	}
	if data["totalUsedBytes"] != float64(549755814400+15456973383+(1099511627776-549755813888)) {
		t.Fatalf("unexpected totalUsedBytes: %v", data["totalUsedBytes"])
	}

	byID := map[string]map[string]interface{}{}
	for _, raw := range items {
		entry, _ := raw.(map[string]interface{})
		byID[fmt.Sprint(entry["userId"])] = entry
	}
	if byID["missing"]["provisioned"] != false {
		t.Fatalf("expected missing user marked unprovisioned, got %#v", byID["missing"])
	}
	if byID["broken"]["error"] == nil {
		t.Fatalf("expected broken user to carry an error, got %#v", byID["broken"])
	}
	if byID["over"]["overQuota"] != true || byID["over"]["usedBytes"] != float64(15456973383) {
		t.Fatalf("expected over-quota user to keep used > total, got %#v", byID["over"])
	}
	if byID["legacy"]["usedBytes"] != float64(1099511627776-549755813888) {
		t.Fatalf("expected used derived from total-remaining when quota.used absent, got %#v", byID["legacy"])
	}
	if byID["normal"]["usedBytes"] != float64(549755814400) {
		t.Fatalf("expected quota.used to take precedence, got %#v", byID["normal"])
	}

	noIDsRes := performM365(service, http.MethodGet, "/api/m365/accounts/1/usage/onedrive", "")
	if noIDsRes.Code != http.StatusBadRequest {
		t.Fatalf("expected missing userIds to be rejected, got %d body=%s", noIDsRes.Code, noIDsRes.Body.String())
	}

	singleRes := performM365(service, http.MethodGet, "/api/m365/accounts/1/users/normal/drive-quota", "")
	if singleRes.Code != http.StatusOK {
		t.Fatalf("drive quota status=%d body=%s", singleRes.Code, singleRes.Body.String())
	}
	singleData := decodeEnvelopeData(t, singleRes)
	if singleData["totalBytes"] != float64(1099511627776) || singleData["usedBytes"] != float64(549755814400) {
		t.Fatalf("unexpected single quota payload: %#v", singleData)
	}
	if singleData["deletedBytes"] != float64(1048576) || singleData["quotaState"] != "normal" {
		t.Fatalf("unexpected single quota detail: %#v", singleData)
	}

	missingSingleRes := performM365(service, http.MethodGet, "/api/m365/accounts/1/users/missing/drive-quota", "")
	if missingSingleRes.Code != http.StatusOK {
		t.Fatalf("expected unprovisioned single lookup to succeed, got %d body=%s", missingSingleRes.Code, missingSingleRes.Body.String())
	}
	missingData := decodeEnvelopeData(t, missingSingleRes)
	if missingData["provisioned"] != false {
		t.Fatalf("expected provisioned=false for unprovisioned user, got %#v", missingData)
	}

	failedSingleRes := performM365(service, http.MethodGet, "/api/m365/accounts/1/users/broken/drive-quota", "")
	if failedSingleRes.Code != http.StatusBadGateway {
		t.Fatalf("expected real graph failure to surface as 502, got %d body=%s", failedSingleRes.Code, failedSingleRes.Body.String())
	}
}

func performM365(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	service.ServeHTTP(rec, req)
	return rec
}

func fakeJWT(payload string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	body := base64.RawURLEncoding.EncodeToString([]byte(payload))
	return header + "." + body + ".signature"
}

func decodeEnvelopeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var envelope struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
		Error   string                 `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v body=%s", err, rec.Body.String())
	}
	if !envelope.Success {
		t.Fatalf("expected success envelope, got error=%s body=%s", envelope.Error, rec.Body.String())
	}
	return envelope.Data
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
