package uptime

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
)

type fakeAuth struct {
	ok bool
}

func (f fakeAuth) IsAuthenticated(context.Context, *http.Request) (bool, error) {
	return f.ok, nil
}

type fakeNotifier struct {
	mu        sync.Mutex
	events    []string
	refreshes []string
}

func (f *fakeNotifier) RefreshLifecycle(_ context.Context, sourceModule, eventType string, _ map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes = append(f.refreshes, sourceModule+":"+eventType)
	return nil
}

func (f *fakeNotifier) Trigger(_ context.Context, sourceModule, eventType string, _ map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, sourceModule+":"+eventType)
	return nil
}

func (f *fakeNotifier) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.events...)
}

func (f *fakeNotifier) refreshSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.refreshes...)
}

func testService(t *testing.T, authOK bool) (*Service, *fakeNotifier) {
	t.Helper()
	notifier := &fakeNotifier{}
	service := New(config.Config{
		Version: "test",
		Host:    "127.0.0.1",
		Port:    0,
		DataDir: t.TempDir(),
		DBName:  "data.db",
	}, fakeAuth{ok: authOK}, notifier)
	t.Cleanup(service.Stop)
	return service, notifier
}

func performRequest(service *Service, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res := httptest.NewRecorder()
	service.ServeHTTP(res, req)
	return res
}

func TestAuthSplitAndMonitorLifecycle(t *testing.T) {
	unauthenticated, _ := testService(t, false)
	res := performRequest(unauthenticated, http.MethodGet, "/api/uptime/monitors", "")
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d body=%s", res.Code, res.Body.String())
	}
	res = performRequest(unauthenticated, http.MethodPost, "/api/uptime/push/missing", `{}`)
	if res.Code != http.StatusNotFound {
		t.Fatalf("public push status = %d body=%s", res.Code, res.Body.String())
	}

	service, _ := testService(t, true)
	res = performRequest(service, http.MethodPost, "/api/uptime/monitors", `{
		"name":"API",
		"type":"http",
		"url":"http://127.0.0.1:9",
		"active":false,
		"interval":60,
		"timeout":1,
		"tags":["edge"]
	}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create monitor status = %d body=%s", res.Code, res.Body.String())
	}
	var monitor map[string]interface{}
	mustDecode(t, res, &monitor)
	id := int64Value(monitor["id"], 0)
	if id == 0 || boolValue(monitor["active"], true) || stringValue(monitor["type"], "") != "http" {
		t.Fatalf("unexpected monitor: %#v", monitor)
	}

	res = performRequest(service, http.MethodGet, "/api/uptime/monitors", "")
	if res.Code != http.StatusOK {
		t.Fatalf("list monitor status = %d body=%s", res.Code, res.Body.String())
	}
	var monitors []map[string]interface{}
	mustDecode(t, res, &monitors)
	if len(monitors) != 1 || int64Value(monitors[0]["id"], 0) != id {
		t.Fatalf("unexpected monitors: %#v", monitors)
	}

	res = performRequest(service, http.MethodPut, "/api/uptime/monitors/"+stringValue(id, ""), `{"name":"API Updated","active":false}`)
	if res.Code != http.StatusOK {
		t.Fatalf("update monitor status = %d body=%s", res.Code, res.Body.String())
	}
	mustDecode(t, res, &monitor)
	if monitor["name"] != "API Updated" {
		t.Fatalf("unexpected updated monitor: %#v", monitor)
	}

	res = performRequest(service, http.MethodGet, "/api/uptime/monitors/"+stringValue(id, "")+"/history", "")
	if res.Code != http.StatusOK {
		t.Fatalf("history status = %d body=%s", res.Code, res.Body.String())
	}
	var history []map[string]interface{}
	mustDecode(t, res, &history)
	if len(history) != 0 {
		t.Fatalf("expected empty history, got %#v", history)
	}

	res = performRequest(service, http.MethodGet, "/api/uptime/monitors/"+stringValue(id, "")+"/uptime?days=1", "")
	if res.Code != http.StatusOK {
		t.Fatalf("uptime status = %d body=%s", res.Code, res.Body.String())
	}
	var uptimePayload map[string]interface{}
	mustDecode(t, res, &uptimePayload)
	if uptimePayload["uptime"] != "100.000" {
		t.Fatalf("unexpected uptime: %#v", uptimePayload)
	}

	res = performRequest(service, http.MethodPost, "/api/uptime/monitors/batch-delete", `{"ids":[`+stringValue(id, "")+`]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("batch delete status = %d body=%s", res.Code, res.Body.String())
	}
	var deleted map[string]interface{}
	mustDecode(t, res, &deleted)
	if intValue(deleted["count"], 0) != 1 {
		t.Fatalf("unexpected delete payload: %#v", deleted)
	}
}

func TestStatusPagesMaintenanceExportImportAndPush(t *testing.T) {
	service, _ := testService(t, true)
	res := performRequest(service, http.MethodPost, "/api/uptime/monitors", `{"name":"Push","type":"push","active":false}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create push status = %d body=%s", res.Code, res.Body.String())
	}
	var monitor map[string]interface{}
	mustDecode(t, res, &monitor)
	id := int64Value(monitor["id"], 0)
	token := stringValue(monitor["pushToken"], "")
	if token == "" {
		t.Fatalf("expected push token: %#v", monitor)
	}

	res = performRequest(service, http.MethodPost, "/api/uptime/push/"+token, `{"source":"unit"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("record push status = %d body=%s", res.Code, res.Body.String())
	}

	res = performRequest(service, http.MethodPost, "/api/uptime/status-pages", `{"title":"Public Status","slug":"public-status","monitorIds":[`+stringValue(id, "")+`]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create status page status = %d body=%s", res.Code, res.Body.String())
	}
	var pageEnvelope struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &pageEnvelope)
	if !pageEnvelope.Success || pageEnvelope.Data["slug"] != "public-status" {
		t.Fatalf("unexpected status page: %#v", pageEnvelope)
	}

	res = performRequest(service, http.MethodGet, "/api/uptime/public/status-pages/public-status", "")
	if res.Code != http.StatusOK {
		t.Fatalf("public status page status = %d body=%s", res.Code, res.Body.String())
	}
	var publicEnvelope struct {
		Success bool `json:"success"`
		Data    struct {
			Monitors []map[string]interface{} `json:"monitors"`
		} `json:"data"`
	}
	mustDecode(t, res, &publicEnvelope)
	if !publicEnvelope.Success || len(publicEnvelope.Data.Monitors) != 1 {
		t.Fatalf("unexpected public status page: %#v", publicEnvelope)
	}

	res = performRequest(service, http.MethodGet, "/api/uptime/public/badge/"+stringValue(id, ""), "")
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "<svg") {
		t.Fatalf("badge status=%d body=%s", res.Code, res.Body.String())
	}

	res = performRequest(service, http.MethodPost, "/api/uptime/maintenance", `{"title":"Global","targets":[{"type":"global"}]}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create maintenance status = %d body=%s", res.Code, res.Body.String())
	}
	var maintenanceEnvelope struct {
		Success bool `json:"success"`
		Data    struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	mustDecode(t, res, &maintenanceEnvelope)
	if !maintenanceEnvelope.Success || maintenanceEnvelope.Data.ID == 0 {
		t.Fatalf("unexpected maintenance: %#v", maintenanceEnvelope)
	}

	res = performRequest(service, http.MethodGet, "/api/uptime/export", "")
	if res.Code != http.StatusOK {
		t.Fatalf("export status = %d body=%s", res.Code, res.Body.String())
	}
	var exportEnvelope struct {
		Success bool                   `json:"success"`
		Data    map[string]interface{} `json:"data"`
	}
	mustDecode(t, res, &exportEnvelope)
	if exportEnvelope.Data["type"] != "api-monitor-uptime-export" {
		t.Fatalf("unexpected export: %#v", exportEnvelope)
	}

	importBody, _ := json.Marshal(map[string]interface{}{"data": exportEnvelope.Data})
	res = performRequest(service, http.MethodPost, "/api/uptime/import/preview", string(importBody))
	if res.Code != http.StatusOK {
		t.Fatalf("import preview status = %d body=%s", res.Code, res.Body.String())
	}
}

func TestProbesAndStateNotifications(t *testing.T) {
	ok := false
	httpTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !ok {
			http.Error(w, "down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","nested":{"value":42}}`))
	}))
	defer httpTarget.Close()

	service, notifier := testService(t, true)
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	monitor, err := service.createMonitor(context.Background(), db, map[string]interface{}{
		"name":             "API",
		"type":             "json",
		"url":              httpTarget.URL,
		"active":           true,
		"downConfirmCount": 1,
		"upConfirmCount":   1,
		"config": map[string]interface{}{
			"jsonQueryPath":     "$.nested.value",
			"jsonQueryOperator": "equals",
			"jsonExpectedValue": 42,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := service.check(context.Background(), db, monitor); err != nil {
		t.Fatalf("down check: %v", err)
	}
	state, _ := loadState(context.Background(), db, int64Value(monitor["id"], 0))
	if state["state"] != stateDown {
		t.Fatalf("expected down state, got %#v", state)
	}
	if _, err := service.check(context.Background(), db, monitor); err != nil {
		t.Fatalf("continuing down check: %v", err)
	}
	if refreshes := notifier.refreshSnapshot(); len(refreshes) != 1 || refreshes[0] != "uptime:down" {
		t.Fatalf("unexpected lifecycle refreshes: %#v", refreshes)
	}

	ok = true
	if _, err := service.check(context.Background(), db, monitor); err != nil {
		t.Fatalf("up check: %v", err)
	}
	state, _ = loadState(context.Background(), db, int64Value(monitor["id"], 0))
	if state["state"] != stateUp {
		t.Fatalf("expected up state, got %#v", state)
	}
	events := notifier.snapshot()
	if len(events) != 2 || events[0] != "uptime:down" || events[1] != "uptime:up" {
		t.Fatalf("unexpected notification events: %#v", events)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	if _, err := tcpProbe(context.Background(), map[string]interface{}{"hostname": host, "port": port, "timeout": 1}); err != nil {
		t.Fatalf("tcp probe: %v", err)
	}
}

func TestPublicPageIconID(t *testing.T) {
	service, _ := testService(t, true)
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO uptime_status_pages (slug, domain, title, description, public, cache_seconds, config_json)
		VALUES ('fav-slug', 'fav.example.com', 'Fav Test', '', 1, 300, '{"publicIconId":"site-custom"}')`); err != nil {
		t.Fatalf("insert status page: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO uptime_status_pages (slug, title, public, cache_seconds, config_json)
		VALUES ('plain-slug', 'Plain', 1, 300, '{}')`); err != nil {
		t.Fatalf("insert plain status page: %v", err)
	}

	iconID, found, err := service.PublicPageIconID(ctx, "fav-slug", false)
	if err != nil || !found || iconID != "site-custom" {
		t.Fatalf("custom slug lookup = (%q, %v, %v), want (site-custom, true, nil)", iconID, found, err)
	}
	iconID, found, err = service.PublicPageIconID(ctx, "fav.example.com", true)
	if err != nil || !found || iconID != "site-custom" {
		t.Fatalf("custom domain lookup = (%q, %v, %v), want (site-custom, true, nil)", iconID, found, err)
	}
	iconID, found, err = service.PublicPageIconID(ctx, "plain-slug", false)
	if err != nil || !found || iconID != "" {
		t.Fatalf("plain slug lookup = (%q, %v, %v), want ('', true, nil)", iconID, found, err)
	}
	iconID, found, err = service.PublicPageIconID(ctx, "missing-slug", false)
	if err != nil || found {
		t.Fatalf("missing slug lookup = (%q, %v, %v), want ('', false, nil)", iconID, found, err)
	}
}

func mustDecode(t *testing.T, res *httptest.ResponseRecorder, target interface{}) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %T: %v body=%s", target, err, res.Body.String())
	}
}

func TestConcurrentChecksDoNotDuplicateIncidents(t *testing.T) {
	service, _ := testService(t, true)
	db, err := service.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	res := performRequest(service, http.MethodPost, "/api/uptime/monitors", `{
		"name":"race",
		"type":"push",
		"active":true,
		"interval":60,
		"push_token":"tok",
		"down_confirm_count":1,
		"up_confirm_count":1
	}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create monitor status = %d body=%s", res.Code, res.Body.String())
	}
	var created map[string]interface{}
	mustDecode(t, res, &created)
	id := int64Value(created["id"], 0)
	if id == 0 {
		t.Fatalf("no monitor id: %#v", created)
	}
	service.stopMonitor(id)

	monitor, ok, err := loadMonitor(context.Background(), db, id)
	if err != nil || !ok {
		t.Fatalf("load monitor: ok=%v err=%v", ok, err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = service.check(context.Background(), db, monitor)
		}()
	}
	wg.Wait()

	var openCount, total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM uptime_incidents WHERE monitor_id = ? AND resolved_at IS NULL`, id).Scan(&openCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM uptime_incidents WHERE monitor_id = ?`, id).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if openCount != 1 || total != 1 {
		t.Fatalf("concurrent checks must create exactly one incident, open=%d total=%d", openCount, total)
	}
}
func TestMaintenanceRowActiveEvaluatesCron(t *testing.T) {
	at := time.Date(2026, 8, 18, 10, 30, 0, 0, time.UTC)

	cases := []struct {
		name string
		row  map[string]interface{}
		want bool
	}{
		{"once window always active", map[string]interface{}{"cron": nil}, true},
		{"cron hits minute", map[string]interface{}{"cron": "30 * * * *", "timezone": "UTC"}, true},
		{"cron misses minute", map[string]interface{}{"cron": "0 * * * *", "timezone": "UTC"}, false},
		{"cron in recurrence json", map[string]interface{}{"recurrence_json": `{"cron":"30 * * * *"}`, "timezone": "UTC"}, true},
		{"invalid cron never active", map[string]interface{}{"cron": "not a cron", "timezone": "UTC"}, false},
	}
	for _, tc := range cases {
		got, err := maintenanceRowActive(tc.row, at)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: active=%v want %v", tc.name, got, tc.want)
		}
	}
}