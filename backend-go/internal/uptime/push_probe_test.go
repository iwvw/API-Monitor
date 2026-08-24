package uptime

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestPushProbeAfterHeartbeat(t *testing.T) {
	service, _ := testService(t, true)

	res := performRequest(service, http.MethodPost, "/api/uptime/monitors", `{"name":"Push","type":"push","active":false,"pushGraceSeconds":120}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create push monitor status = %d body=%s", res.Code, res.Body.String())
	}
	var monitor map[string]interface{}
	mustDecode(t, res, &monitor)
	id := int64Value(monitor["id"], 0)
	token := stringValue(monitor["pushToken"], "")
	if id == 0 || token == "" {
		t.Fatalf("unexpected push monitor: %#v", monitor)
	}

	res = performRequest(service, http.MethodPost, "/api/uptime/push/"+token, `{"source":"unit"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("push heartbeat status = %d body=%s", res.Code, res.Body.String())
	}

	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	last, err := getLastHeartbeat(ctx, db, id)
	if err != nil || last == nil {
		t.Fatalf("getLastHeartbeat err=%v last=%v", err, last)
	}
	if intValue(last["status"], 0) != 1 {
		t.Fatalf("expected last heartbeat up, got %#v", last)
	}

	loaded, ok, err := loadMonitor(ctx, db, id)
	if err != nil || !ok {
		t.Fatalf("loadMonitor err=%v ok=%v", err, ok)
	}
	t.Logf("monitor id=%v type=%v pushToken=%v grace=%v", loaded["id"], loaded["type"], loaded["pushToken"], loaded["pushGraceSeconds"])

	result, err := service.probe(ctx, db, loaded)
	if err != nil {
		t.Fatalf("pushProbe failed after fresh heartbeat: %v", err)
	}
	if !result.OK || result.Status != stateUp {
		t.Fatalf("pushProbe not up: %+v", result)
	}
}

func TestPushProbeViaCheckNow(t *testing.T) {
	service, _ := testService(t, true)

	res := performRequest(service, http.MethodPost, "/api/uptime/monitors", `{"name":"Push2","type":"push","active":false,"pushGraceSeconds":120}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create push monitor status = %d body=%s", res.Code, res.Body.String())
	}
	var monitor map[string]interface{}
	mustDecode(t, res, &monitor)
	id := int64Value(monitor["id"], 0)
	token := stringValue(monitor["pushToken"], "")

	res = performRequest(service, http.MethodPost, "/api/uptime/push/"+token, `{"source":"unit2"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("push heartbeat status = %d body=%s", res.Code, res.Body.String())
	}

	res = performRequest(service, http.MethodPost, "/api/uptime/monitors/"+stringValue(id, "")+"/check-now", "")
	if res.Code != http.StatusOK {
		t.Fatalf("check-now status = %d body=%s", res.Code, res.Body.String())
	}
	body := map[string]interface{}{}
	mustDecode(t, res, &body)
	data, _ := body["data"].(map[string]interface{})
	if intValue(data["status"], -1) != 1 {
		t.Fatalf("check-now should report up after fresh push heartbeat, got %#v", body)
	}
}

func TestPushProbeGraceOverdue(t *testing.T) {
	service, _ := testService(t, true)

	res := performRequest(service, http.MethodPost, "/api/uptime/monitors", `{"name":"Push3","type":"push","active":false,"pushGraceSeconds":1}`)
	if res.Code != http.StatusOK {
		t.Fatalf("create push monitor status = %d body=%s", res.Code, res.Body.String())
	}
	var monitor map[string]interface{}
	mustDecode(t, res, &monitor)
	id := int64Value(monitor["id"], 0)
	token := stringValue(monitor["pushToken"], "")

	res = performRequest(service, http.MethodPost, "/api/uptime/push/"+token, `{"source":"unit3"}`)
	if res.Code != http.StatusOK {
		t.Fatalf("push heartbeat status = %d body=%s", res.Code, res.Body.String())
	}

	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	loaded, ok, err := loadMonitor(ctx, db, id)
	if err != nil || !ok {
		t.Fatalf("loadMonitor err=%v ok=%v", err, ok)
	}

	result, err := service.probe(ctx, db, loaded)
	if err != nil {
		t.Fatalf("fresh probe: %v", err)
	}
	if !result.OK {
		t.Fatalf("fresh probe should be up: %+v", result)
	}

	// 心跳写入后 sleep 超过 1s 宽限，探测应判定 overdue
	beatTime := time.Now().UTC().Add(-3 * time.Second)
	if _, err := db.ExecContext(ctx, `
		UPDATE uptime_heartbeats SET created_at = ?
		WHERE monitor_id = ? AND id = (SELECT MAX(id) FROM uptime_heartbeats WHERE monitor_id = ?)
	`, beatTime.Format(time.RFC3339), id, id); err != nil {
		t.Fatal(err)
	}
	result, err = service.probe(ctx, db, loaded)
	if err == nil || !strings.Contains(err.Error(), "overdue") {
		t.Fatalf("expected overdue error, got result=%+v err=%v", result, err)
	}
}