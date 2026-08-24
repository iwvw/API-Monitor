package adminai

import (
	"context"
	"strings"
	"testing"

	systemmetrics "github.com/iwvw/api-monitor/backend-go/internal/system"
)

func TestNormalizeMentions(t *testing.T) {
	if got := normalizeMentions(nil); got != nil {
		t.Fatalf("nil 输入应返回 nil: %v", got)
	}
	raw := []Mention{
		{Type: "zone", ID: "z1"},
		{Type: "zone", ID: "z1"},          // 重复
		{Type: "host", ID: "h1"},
		{Type: "nope", ID: "x1"},          // 白名单外
		{Type: "task", ID: ""},            // 空 id
		{Type: "account", ID: "a1"},
	}
	got := normalizeMentions(raw)
	if len(got) != 3 {
		t.Fatalf("期望 3 条合法引用，实际 %d: %v", len(got), got)
	}
	if got[0].Type != "zone" || got[0].ID != "z1" {
		t.Fatalf("首条应为 zone/z1: %v", got[0])
	}

	many := make([]Mention, 0, 20)
	for i := 0; i < 20; i++ {
		many = append(many, Mention{Type: "host", ID: string(rune('a' + i))})
	}
	if got := normalizeMentions(many); len(got) != mentionMaxCount {
		t.Fatalf("超出上限应裁剪为 %d，实际 %d", mentionMaxCount, len(got))
	}
}

func TestFindMentionArray(t *testing.T) {
	// 精确键
	body := map[string]interface{}{
		"success": true,
		"data":    map[string]interface{}{"zones": []interface{}{map[string]interface{}{"id": "z1"}}},
	}
	if arr := findMentionArray(body, "zones"); len(arr) != 1 {
		t.Fatalf("嵌套精确键未命中: %v", arr)
	}
	// 信封键（data 直接数组）
	body2 := map[string]interface{}{"data": []interface{}{map[string]interface{}{"id": "x"}}}
	if arr := findMentionArray(body2, "tasks"); len(arr) != 1 {
		t.Fatalf("data 信封数组未命中: %v", arr)
	}
	// 兜底任意数组
	body3 := map[string]interface{}{"accounts": []interface{}{map[string]interface{}{"id": "a"}}}
	if arr := findMentionArray(body3, "tasks"); len(arr) != 1 {
		t.Fatalf("任意数组兜底未命中: %v", arr)
	}
	// 标量数组不采纳（避免把 token 数组当资源列表）
	body4 := map[string]interface{}{"data": []interface{}{"str1", "str2"}}
	if arr := findMentionArray(body4, "zones"); arr != nil {
		t.Fatalf("标量数组不应采纳: %v", arr)
	}
}

func TestFetchMentionSnapshots(t *testing.T) {
	s := newTestService(t)
	// 按路径返回对应列表：zone 聚合 /api/cloudflare/zones、主机 /api/server/accounts、
	// 任务 /api/scheduler/tasks、账号 /api/cloudflare/accounts
	s.aiCaller = func(_ context.Context, req systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
		var list []interface{}
		switch req.Path {
		case "/api/cloudflare/zones":
			list = []interface{}{
				map[string]interface{}{"id": "z1", "name": "example.com", "status": "active", "account_id": "acc1"},
				map[string]interface{}{"id": "z2", "name": "example.org", "status": "pending"},
			}
		case "/api/server/accounts":
			list = []interface{}{
				map[string]interface{}{"id": "h1", "name": "web-01", "ip": "1.2.3.4", "region": "nrt"},
				map[string]interface{}{"id": "h2", "name": "db-01", "ip": "5.6.7.8"},
			}
		case "/api/scheduler/tasks":
			list = []interface{}{
				map[string]interface{}{"id": "t1", "name": "夜间备份", "schedule": "0 2 * * *", "enabled": true},
			}
		case "/api/cloudflare/accounts":
			list = []interface{}{map[string]interface{}{"id": "acc1", "name": "主账号", "type": "enterprise"}}
		case "/api/flyio/proxy/apps":
			list = []interface{}{
				map[string]interface{}{"accountName": "salen", "apps": []interface{}{map[string]interface{}{"appName": "apimnt", "region": "nrt", "state": "running", "org": "iwvw"}}},
				map[string]interface{}{"accountName": "other", "apps": []interface{}{map[string]interface{}{"appName": "app2", "region": "sjc", "state": "deployed"}}},
			}
		case "/api/koyeb/data":
			list = []interface{}{
				map[string]interface{}{
					"name": "ssln",
					"projects": []interface{}{
						map[string]interface{}{
							"name": "v2r",
							"services": []interface{}{
								map[string]interface{}{"_id": "srv1", "name": "v2r", "status": "RUNNING", "type": "WEB", "region": "华盛顿"},
							},
						},
					},
				},
			}
		case "/api/scheduler/nodes":
			list = []interface{}{map[string]interface{}{"id": "nd1", "name": "主调度", "hostname": "node-01", "enabled": true}}
		case "/api/notification/channels":
			list = []interface{}{map[string]interface{}{"id": "ch1", "name": "TG 告警", "type": "telegram", "enabled": true}}
		default:
			return systemmetrics.AICallResponse{StatusCode: 404}, nil
		}
		return systemmetrics.AICallResponse{StatusCode: 200, Body: map[string]interface{}{"data": list}}, nil
	}

	snaps, err := s.fetchMentionSnapshots(context.Background(), []Mention{
		{Type: "zone", ID: "z1"},
		{Type: "zone", ID: "zz-missing"},
		{Type: "host", ID: "h2"},
		{Type: "task", ID: "t1"},
		{Type: "account", ID: "acc1"},
		{Type: "flyio", ID: "apimnt"},
		{Type: "flyio", ID: "app2"},
		{Type: "koyeb", ID: "srv1"},
		{Type: "node", ID: "nd1"},
		{Type: "channel", ID: "ch1"},
	})
	if err != nil {
		t.Fatalf("fetchMentionSnapshots 失败: %v", err)
	}
	if len(snaps) != 10 {
		t.Fatalf("期望 10 条快照，实际 %d", len(snaps))
	}
	findSnap := func(tp, id string) *MentionSnapshot {
		for i := range snaps {
			if snaps[i].Type == tp && snaps[i].ID == id {
				return &snaps[i]
			}
		}
		return nil
	}
	// zone 命中：快照含名称、status 与专属 records 接口（account_id 嵌套归一）
	zSnap := findSnap("zone", "z1")
	if zSnap == nil || !strings.Contains(zSnap.Text, "example.com") || !strings.Contains(zSnap.Text, "status=active") {
		t.Fatalf("zone 快照字段缺失: %+v", zSnap)
	}
	if !strings.Contains(zSnap.Text, "/api/cloudflare/accounts/acc1/zones/z1/records") {
		t.Fatalf("zone 专属接口缺失: %q", zSnap.Text)
	}
	// 未找到：显式标注
	mSnap := findSnap("zone", "zz-missing")
	if mSnap == nil || !strings.Contains(mSnap.Text, "引用未找到") {
		t.Fatalf("未找到引用应标注: %+v", mSnap)
	}
	// host 快照
	hSnap := findSnap("host", "h2")
	if hSnap == nil || !strings.Contains(hSnap.Text, "db-01") || !strings.Contains(hSnap.Text, "ip=5.6.7.8") {
		t.Fatalf("host 快照字段缺失: %+v", hSnap)
	}
	// task 布尔字段 + 专属执行接口
	tSnap := findSnap("task", "t1")
	if tSnap == nil || !strings.Contains(tSnap.Text, "enabled=true") || !strings.Contains(tSnap.Text, "/api/scheduler/tasks/t1/run") {
		t.Fatalf("task 快照字段/专属接口缺失: %+v", tSnap)
	}
	// flyio：appName 定位 + 专属 update-image 接口（第二账号应用同样命中，验证跨账号合并）
	fSnap := findSnap("flyio", "apimnt")
	if fSnap == nil || !strings.Contains(fSnap.Text, "apimnt") || !strings.Contains(fSnap.Text, "/api/flyio/apps/apimnt/update-image") {
		t.Fatalf("flyio 快照/专属接口缺失: %+v", fSnap)
	}
	if s2 := findSnap("flyio", "app2"); s2 == nil || !strings.Contains(s2.Text, "app2") {
		t.Fatalf("flyio 跨账号应用未命中（未合并全部账号）: %+v", s2)
	}
	// koyeb：嵌套 projects[].services 穿透 + 专属 pause 接口
	kSnap := findSnap("koyeb", "srv1")
	if kSnap == nil || !strings.Contains(kSnap.Text, "v2r") || !strings.Contains(kSnap.Text, "POST /api/koyeb/services/srv1/pause") {
		t.Fatalf("koyeb 快照/专属接口缺失: %+v", kSnap)
	}
	// node：专属更新接口
	nSnap := findSnap("node", "nd1")
	if nSnap == nil || !strings.Contains(nSnap.Text, "nd1") || !strings.Contains(nSnap.Text, "PUT /api/scheduler/nodes/nd1") {
		t.Fatalf("node 快照/专属接口缺失: %+v", nSnap)
	}
	// channel：专属测试消息接口
	cSnap := findSnap("channel", "ch1")
	if cSnap == nil || !strings.Contains(cSnap.Text, "TG 告警") || !strings.Contains(cSnap.Text, "/api/notification/channels/ch1/test") {
		t.Fatalf("channel 快照/专属接口缺失: %+v", cSnap)
	}
	// 总预算截断：全部注入不大于上限
	block := buildMentionBlock(snaps)
	if len(block) > mentionTotalCap+len("## 本次会话引用的资源（实时快照）\n")+400 {
		t.Fatalf("注入块超过预算: %d", len(block))
	}
	if !strings.Contains(block, "example.com") || !strings.Contains(block, "引用未找到") {
		t.Fatalf("注入块内容缺失:\n%s", block)
	}
}

// 快照拉取接口整体失败：注入兜底文案而非中断。
func TestFetchMentionSnapshotsListError(t *testing.T) {
	s := newTestService(t)
	s.aiCaller = func(_ context.Context, req systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error) {
		if req.Path == "/api/cloudflare/accounts" {
			return systemmetrics.AICallResponse{StatusCode: 200, Body: map[string]interface{}{
				"success": false, "error": "未配置 Cloudflare 账号",
			}}, nil
		}
		return systemmetrics.AICallResponse{StatusCode: 200, Body: map[string]interface{}{
			"data": []interface{}{map[string]interface{}{"id": "h1", "name": "web-01"}},
		}}, nil
	}
	snaps, err := s.fetchMentionSnapshots(context.Background(), []Mention{{Type: "account", ID: "acc1"}, {Type: "host", ID: "h1"}})
	if err != nil {
		t.Fatalf("不应整体失败: %v", err)
	}
	if len(snaps) != 2 {
		t.Fatalf("期望 2 条（1 成功 1 失败标注），实际 %d", len(snaps))
	}
	var accSnap, hostSnap *MentionSnapshot
	for i := range snaps {
		if snaps[i].Type == "account" {
			accSnap = &snaps[i]
		}
		if snaps[i].Type == "host" {
			hostSnap = &snaps[i]
		}
	}
	if accSnap == nil || !strings.Contains(accSnap.Text, "拉取失败") {
		t.Fatalf("失败应标注拉取失败: %+v", accSnap)
	}
	if hostSnap == nil || !strings.Contains(hostSnap.Text, "web-01") {
		t.Fatalf("host 快照应正常: %+v", hostSnap)
	}
}