package m365

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

func TestPublicRegisterConcurrentSameCodeReservesOnce(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key")
	testToken := fakeJWT(`{"roles":["User.ReadWrite.All","LicenseAssignment.ReadWrite.All"]}`)

	var createUserCalls int32
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/oauth2/v2.0/token"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"` + testToken + `"}`))
		case r.URL.Path == "/users" && r.Method == http.MethodPost:
			atomic.AddInt32(&createUserCalls, 1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"registered-user","displayName":"Student"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer mock.Close()

	t.Setenv("M365_GRAPH_BASE_URL", mock.URL)
	t.Setenv("M365_LOGIN_BASE_URL", mock.URL)

	service := New(config.Config{DataDir: t.TempDir(), DBName: "data.db"})

	if res := performM365(service, http.MethodPost, "/api/m365/accounts", `{"name":"Contoso","tenantId":"tenant-1","clientId":"client-1","clientSecret":"secret-1","defaultDomain":"contoso.com"}`); res.Code != http.StatusOK {
		t.Fatalf("create account status=%d body=%s", res.Code, res.Body.String())
	}
	if res := performM365(service, http.MethodPost, "/api/m365/public-pages", `{"name":"2026 批次","accountIds":[1],"domains":["contoso.com"]}`); res.Code != http.StatusOK {
		t.Fatalf("create public page status=%d body=%s", res.Code, res.Body.String())
	}
	createInvite := performM365(service, http.MethodPost, "/api/m365/invite-codes", `{"publicPageId":1,"quantity":1}`)
	if createInvite.Code != http.StatusOK {
		t.Fatalf("create invite status=%d body=%s", createInvite.Code, createInvite.Body.String())
	}
	inviteData := decodeEnvelopeData(t, createInvite)
	codes := stringArray(inviteData["codes"])
	if len(codes) != 1 {
		t.Fatalf("expected 1 invite code body=%s", createInvite.Body.String())
	}
	code := strings.TrimSpace(codes[0])

	const workers = 6
	start := make(chan struct{})
	statuses := make([]int, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			res := performM365(service, http.MethodPost, "/api/m365/public/register", `{"code":"`+code+`","mailNickname":"student`+strconv.Itoa(index)+`","displayName":"Student","domain":"contoso.com"}`)
			statuses[index] = res.Code
		}(i)
	}
	close(start)
	wg.Wait()

	success := 0
	for _, status := range statuses {
		switch status {
		case http.StatusOK:
			success++
		case http.StatusConflict, http.StatusBadRequest:
		default:
			t.Fatalf("unexpected concurrent register status=%d statuses=%v", status, statuses)
		}
	}
	if success != 1 {
		t.Fatalf("expected exactly one successful registration, got %d statuses=%v", success, statuses)
	}
	if got := atomic.LoadInt32(&createUserCalls); got != 1 {
		t.Fatalf("expected exactly one graph user creation, got %d", got)
	}

	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var usedCount int64
	if err := db.QueryRowContext(context.Background(), `SELECT used_count FROM m365_invite_codes WHERE id = 1`).Scan(&usedCount); err != nil {
		t.Fatal(err)
	}
	if usedCount != 1 {
		t.Fatalf("expected invite used_count=1, got %d", usedCount)
	}

	var registrationCount int64
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM m365_public_page_registrations`).Scan(&registrationCount); err != nil {
		t.Fatal(err)
	}
	if registrationCount != 1 {
		t.Fatalf("expected 1 registration record, got %d", registrationCount)
	}
}
