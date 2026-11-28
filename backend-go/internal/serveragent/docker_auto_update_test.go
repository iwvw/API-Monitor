package serveragent

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestNormalizeDockerAutoUpdatePolicy(t *testing.T) {
	got := normalizeDockerAutoUpdatePolicy(DockerAutoUpdatePolicy{})
	if got.Enabled {
		t.Fatal("默认必须关闭")
	}
	if got.Schedule != dockerAutoUpdateDefaultSchedule {
		t.Fatalf("默认计划 = %q", got.Schedule)
	}
	if got.MaxConcurrency != dockerAutoUpdateDefaultConcurrency {
		t.Fatalf("默认并发 = %d", got.MaxConcurrency)
	}
	if got.PerContainerTimeoutSeconds != dockerAutoUpdateDefaultTimeoutSec {
		t.Fatalf("默认单容器超时 = %d", got.PerContainerTimeoutSeconds)
	}
	if !defaultDockerAutoUpdatePolicy().NotifyOnFailure {
		t.Fatal("默认开启失败通知")
	}

	clamped := normalizeDockerAutoUpdatePolicy(DockerAutoUpdatePolicy{MaxConcurrency: 999, PerContainerTimeoutSeconds: 1})
	if clamped.MaxConcurrency != dockerAutoUpdateMaxConcurrency {
		t.Fatalf("并发上限 = %d", clamped.MaxConcurrency)
	}
	if clamped.PerContainerTimeoutSeconds != dockerAutoUpdateMinTimeoutSec {
		t.Fatalf("超时下限 = %d", clamped.PerContainerTimeoutSeconds)
	}
}

func TestCachedInfoDockerInstalled(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`{"docker":{"installed":true}}`, true},
		{`{"docker":{"installed":false}}`, false},
		{`{"docker":{}}`, false},
		{`{}`, false},
		{"", false},
		{"not json", false},
	}
	for _, c := range cases {
		if got := cachedInfoDockerInstalled(c.in); got != c.want {
			t.Fatalf("cachedInfoDockerInstalled(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseContainerLabel(t *testing.T) {
	key, value, ok := parseContainerLabel("auto-update=true")
	if !ok || key != "auto-update" || value != "true" {
		t.Fatalf("got %q %q %v", key, value, ok)
	}
	key, value, ok = parseContainerLabel("auto-update")
	if !ok || key != "auto-update" || value != "" {
		t.Fatalf("got %q %q %v", key, value, ok)
	}
	if _, _, ok := parseContainerLabel("   "); ok {
		t.Fatal("空表达式不应命中")
	}
}

func containerSummary(name, image, state string, labels map[string]interface{}) map[string]interface{} {
	raw := map[string]interface{}{
		"id":    name + "-id",
		"name":  name,
		"image": image,
		"state": state,
	}
	if labels != nil {
		raw["labels"] = labels
	}
	return raw
}

func TestFilterDockerAutoUpdateCandidates(t *testing.T) {
	containers := []map[string]interface{}{
		containerSummary("web", "nginx:latest", "running", map[string]interface{}{"auto-update": "true"}),
		containerSummary("db", "postgres:16", "running", nil),
		containerSummary("app-1", "ghcr.io/acme/app:1", "running", map[string]interface{}{
			"auto-update":                "true",
			"com.docker.compose.project": "app",
		}),
		containerSummary("local", "sha256:deadbeef", "running", map[string]interface{}{"auto-update": "true"}),
		containerSummary("stopped", "nginx:latest", "exited", map[string]interface{}{"auto-update": "true"}),
	}

	// 默认:标签白名单 + 跳过 compose + 跳过裸 sha256 + 只处理运行中。
	policy := normalizeDockerAutoUpdatePolicy(DockerAutoUpdatePolicy{ContainerLabel: "auto-update=true"})
	got := filterDockerAutoUpdateCandidates(containers, policy)
	if len(got) != 1 || got[0].Name != "web" {
		t.Fatalf("默认筛选 = %#v", got)
	}

	// 开启 compose 后,compose 容器进入候选(裸 sha256 仍被跳过)。
	policy.IncludeCompose = true
	got = filterDockerAutoUpdateCandidates(containers, policy)
	names := map[string]bool{}
	for _, c := range got {
		names[c.Name] = true
	}
	if !names["web"] || !names["app-1"] || names["local"] || names["stopped"] {
		t.Fatalf("含 compose 筛选 = %#v", got)
	}

	// 容器名单(包含匹配)优先于 label 白名单。
	policy = normalizeDockerAutoUpdatePolicy(DockerAutoUpdatePolicy{ContainerNames: "d"})
	got = filterDockerAutoUpdateCandidates(containers, policy)
	if len(got) != 1 || got[0].Name != "db" {
		t.Fatalf("名单筛选 = %#v", got)
	}
}

func TestRunDockerAutoUpdateUpdatesFlaggedContainers(t *testing.T) {
	service, db := testService(t)
	notifier := &recordingNotifier{}
	service.SetNotifier(notifier)

	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type,tags,order_index,cached_info) VALUES('host-1','prod','192.0.2.9','root','password','["prod"]',0,'{"docker":{"installed":true}}')`); err != nil {
		t.Fatal(err)
	}

	containers := []map[string]interface{}{
		containerSummary("web", "nginx:latest", "running", map[string]interface{}{"auto-update": "true"}),
		containerSummary("db", "postgres:16", "running", nil),
	}
	containersJSON, _ := json.Marshal(containers)
	checks := []map[string]interface{}{
		{"container_id": "web-id", "container_name": "web", "image": "nginx:latest", "has_update": true},
		{"container_id": "db-id", "container_name": "db", "image": "postgres:16", "has_update": false},
	}
	checksJSON, _ := json.Marshal(checks)

	var updated []string
	service.registry.Register("host-1", &taskReplySocket{
		t:       t,
		service: service,
		reply: func(taskType int, data string) string {
			switch taskType {
			case 27:
				return string(containersJSON)
			case 11:
				return string(checksJSON)
			case 24:
				updated = append(updated, data)
				return "更新成功"
			default:
				t.Fatalf("unexpected task type %d", taskType)
			}
			return ""
		},
	})

	ctx := context.Background()
	policy := DockerAutoUpdatePolicy{Enabled: true, HostSelector: "prod", ContainerLabel: "auto-update=true"}
	if err := saveDockerAutoUpdatePolicy(ctx, db, policy); err != nil {
		t.Fatal(err)
	}

	summary, err := service.RunDockerAutoUpdate(ctx, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if summary.Hosts != 1 || summary.Scanned != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.Updated != 1 || summary.Failed != 0 {
		t.Fatalf("summary = %#v", summary)
	}
	if len(updated) != 1 || !strings.Contains(updated[0], `"container_name":"web"`) {
		t.Fatalf("updated payload = %#v", updated)
	}

	results, err := service.loadDockerAutoUpdateResults(ctx, db, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ContainerName != "web" || results[0].Status != "updated" || results[0].ServerName != "prod" {
		t.Fatalf("results = %#v", results)
	}
	if events, _ := notifier.snapshot(); len(events) != 0 {
		t.Fatalf("成功不应触发失败通知: %#v", events)
	}
}

func TestRunDockerAutoUpdateDisabledIsNoop(t *testing.T) {
	service, db := testService(t)
	summary, err := service.RunDockerAutoUpdate(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Status != "disabled" {
		t.Fatalf("未开启时应直接返回 disabled, got %#v", summary)
	}
	_ = db
}

func TestDockerAutoUpdatePolicyRoutes(t *testing.T) {
	service, _ := testService(t)

	res := perform(service, http.MethodGet, "/api/server/v2/docker/auto-update/policy", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get policy status=%d body=%s", res.Code, res.Body.String())
	}
	payload := decodePayload(t, res)
	data, _ := payload["data"].(map[string]interface{})
	policy, _ := data["policy"].(map[string]interface{})
	if enabled, _ := policy["enabled"].(bool); enabled {
		t.Fatal("默认策略必须关闭")
	}

	res = perform(service, http.MethodPut, "/api/server/v2/docker/auto-update/policy", `{"enabled":true,"schedule":"0 5 * * *","containerLabel":"auto-update=true","maxConcurrency":3}`)
	if res.Code != http.StatusOK {
		t.Fatalf("put policy status=%d body=%s", res.Code, res.Body.String())
	}
	payload = decodePayload(t, res)
	data, _ = payload["data"].(map[string]interface{})
	policy, _ = data["policy"].(map[string]interface{})
	if enabled, _ := policy["enabled"].(bool); !enabled {
		t.Fatalf("policy not saved: %#v", policy)
	}

	res = perform(service, http.MethodGet, "/api/server/v2/docker/auto-update/results", "")
	if res.Code != http.StatusOK {
		t.Fatalf("get results status=%d body=%s", res.Code, res.Body.String())
	}
}

func TestDockerAutoUpdateScheduleReflectsPolicy(t *testing.T) {
	service, db := testService(t)
	ctx := context.Background()
	if service.DockerAutoUpdateEnabled(ctx) {
		t.Fatal("默认应关闭")
	}
	if err := saveDockerAutoUpdatePolicy(ctx, db, DockerAutoUpdatePolicy{Enabled: true, Schedule: "0 6 * * *"}); err != nil {
		t.Fatal(err)
	}
	if !service.DockerAutoUpdateEnabled(ctx) {
		t.Fatal("保存后应开启")
	}
	if got := service.DockerAutoUpdateSchedule(ctx); got != "0 6 * * *" {
		t.Fatalf("schedule = %q", got)
	}
}

func TestStartDockerAutoUpdateRunRejectsOverlap(t *testing.T) {
	service, _ := testService(t)
	if !dockerAutoUpdateRunGuard.CompareAndSwap(false, true) {
		t.Fatal("precondition")
	}
	if service.StartDockerAutoUpdateRun(true) {
		t.Fatal("并发启动应被拒绝")
	}
	dockerAutoUpdateRunGuard.Store(false)
}

func TestRunDockerAutoUpdateNotifiesFailure(t *testing.T) {
	service, db := testService(t)
	notifier := &recordingNotifier{}
	service.SetNotifier(notifier)

	if _, err := db.Exec(`INSERT INTO server_accounts(id,name,host,username,auth_type,tags,order_index,cached_info) VALUES('host-2','edge','192.0.2.10','root','password','[]',0,'{"docker":{"installed":true}}')`); err != nil {
		t.Fatal(err)
	}

	containersJSON, _ := json.Marshal([]map[string]interface{}{
		containerSummary("web", "nginx:latest", "running", map[string]interface{}{"auto-update": "true"}),
	})
	checksJSON, _ := json.Marshal([]map[string]interface{}{
		{"container_id": "web-id", "container_name": "web", "image": "nginx:latest", "has_update": true},
	})
	service.registry.Register("host-2", &dockerTaskSocket{
		t:          t,
		service:    service,
		containers: string(containersJSON),
		checks:     string(checksJSON),
		failType:   24,
	})

	ctx := context.Background()
	if err := saveDockerAutoUpdatePolicy(ctx, db, DockerAutoUpdatePolicy{Enabled: true, ContainerLabel: "auto-update=true", NotifyOnFailure: true}); err != nil {
		t.Fatal(err)
	}
	summary, err := service.RunDockerAutoUpdate(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Failed != 1 || summary.Status != "failed" {
		t.Fatalf("summary = %#v", summary)
	}
	events, data := notifier.snapshot()
	if len(events) != 1 || events[0] != "container.update.failed" {
		t.Fatalf("events = %#v", events)
	}
	if data[0]["containerName"] != "web" {
		t.Fatalf("event data = %#v", data[0])
	}
}

type dockerTaskSocket struct {
	t          *testing.T
	service    *Service
	containers string
	checks     string
	failType   int
}

func (s *dockerTaskSocket) WriteMessage(_ int, data []byte) error {
	raw := string(data)
	if !strings.HasPrefix(raw, "42") {
		s.t.Fatalf("unexpected socket frame: %s", raw)
	}
	var frame []interface{}
	if err := json.Unmarshal([]byte(raw[2:]), &frame); err != nil {
		s.t.Fatalf("decode socket frame: %v", err)
	}
	payload, ok := frame[1].(map[string]interface{})
	if !ok {
		s.t.Fatalf("unexpected socket payload: %#v", frame[1])
	}
	taskID, _ := payload["id"].(string)
	taskType := int(payload["type"].(float64))
	if taskType == s.failType {
		go s.service.taskRegistry.Fail(taskID, "agent reported failure")
		return nil
	}
	switch taskType {
	case 27:
		go s.service.taskRegistry.Complete(taskID, s.containers)
	case 11:
		go s.service.taskRegistry.Complete(taskID, s.checks)
	case 24:
		go s.service.taskRegistry.Complete(taskID, "更新成功")
	default:
		s.t.Fatalf("unexpected task type %d", taskType)
	}
	return nil
}
