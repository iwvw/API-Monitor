package serveragent

import (
	"fmt"
	"testing"
	"time"
)

// ---- 指标广播按需订阅（issue #77）----

func newTestHubClient(id string) *MetricsHubClient {
	return &MetricsHubClient{
		sessionID: id,
		connAt:    time.Now(),
	}
}

// 未订阅（旧前端）仍按全量推送，保持向后兼容。
func TestMetricsHubClientWantsWithoutSubscription(t *testing.T) {
	client := newTestHubClient("c1")
	if !client.wants("any-server") {
		t.Fatal("未订阅的客户端应全量接收（兼容旧前端）")
	}
}

// 订阅后只关心集合内的主机。
func TestMetricsHubClientWantsAfterSubscribe(t *testing.T) {
	hub := NewMetricsHub()
	client := newTestHubClient("c2")
	hub.clients["c2"] = client

	hub.Subscribe("c2", []string{"srv-a", " srv-b "})

	if !client.wants("srv-a") || !client.wants("srv-b") {
		t.Fatal("订阅内的主机应接收")
	}
	if client.wants("srv-c") {
		t.Fatal("订阅外的主机不应接收")
	}
}

// 空集合表示「明确不关心任何主机」，与「从未订阅」语义不同。
func TestMetricsHubSubscribeEmptyMeansNothing(t *testing.T) {
	hub := NewMetricsHub()
	client := newTestHubClient("c3")
	hub.clients["c3"] = client

	hub.Subscribe("c3", []string{})

	if client.wants("srv-a") {
		t.Fatal("空订阅集合不应接收任何主机")
	}
	if client.subscription == nil {
		t.Fatal("空订阅应是非 nil 的空集合，而不是「未订阅」")
	}
}

// 订阅不存在的会话不得 panic、也不得凭空创建客户端。
func TestMetricsHubSubscribeUnknownSessionIsNoop(t *testing.T) {
	hub := NewMetricsHub()
	hub.Subscribe("nobody", []string{"srv-a"})
	if hub.ClientCount() != 0 {
		t.Fatalf("不应凭空创建客户端, count=%d", hub.ClientCount())
	}
}

// 空白 serverId 要被过滤掉，避免把 "" 当成一台主机订阅。
func TestMetricsHubSubscribeTrimsAndSkipsBlanks(t *testing.T) {
	hub := NewMetricsHub()
	client := newTestHubClient("c4")
	hub.clients["c4"] = client

	hub.Subscribe("c4", []string{"", "  ", "srv-a"})

	if len(client.subscription) != 1 {
		t.Fatalf("订阅大小 = %d, want 1", len(client.subscription))
	}
	if _, ok := client.subscription["srv-a"]; !ok {
		t.Fatal("srv-a 应在订阅内")
	}
}

// BroadcastMetrics 只分发给关心该主机的客户端。
//
// 直接断言「哪些客户端被选中」：hub.broadcastEvent 会真的往 websocket 写，
// 测试里用无 session 的客户端会在写阶段被跳过，这里只验证筛选逻辑。
func TestBroadcastMetricsFiltersBySubscription(t *testing.T) {
	hub := NewMetricsHub()
	interested := newTestHubClient("a")
	interested.subscription = map[string]struct{}{"srv-1": {}}
	bystander := newTestHubClient("b")
	bystander.subscription = map[string]struct{}{"srv-2": {}}
	legacy := newTestHubClient("c") // 未订阅，应全量接收
	hub.clients["a"] = interested
	hub.clients["b"] = bystander
	hub.clients["c"] = legacy

	var selected []string
	hub.mu.RLock()
	for _, c := range hub.clients {
		if c.wants("srv-1") {
			selected = append(selected, c.sessionID)
		}
	}
	hub.mu.RUnlock()

	got := map[string]bool{}
	for _, id := range selected {
		got[id] = true
	}
	if !got["a"] {
		t.Fatal("订阅了 srv-1 的客户端应被选中")
	}
	if got["b"] {
		t.Fatal("只订阅 srv-2 的客户端不应收到 srv-1 的指标")
	}
	if !got["c"] {
		t.Fatal("未订阅的旧前端应继续全量接收")
	}
}

// 订阅可被覆盖：前端切换看板时重复调用应生效，而不是累加。
func TestMetricsHubSubscribeReplacesPrevious(t *testing.T) {
	hub := NewMetricsHub()
	client := newTestHubClient("c5")
	hub.clients["c5"] = client

	hub.Subscribe("c5", []string{"srv-a", "srv-b"})
	hub.Subscribe("c5", []string{"srv-c"})

	if client.wants("srv-a") {
		t.Fatal("覆盖后不应再关心旧主机")
	}
	if !client.wants("srv-c") {
		t.Fatal("覆盖后应关心新主机")
	}
}

// 订阅集合要有上限：/metrics 命名空间匿名可达，无上限会被塞入巨量 id。
func TestMetricsHubSubscribeCapsSize(t *testing.T) {
	hub := NewMetricsHub()
	client := newTestHubClient("c-cap")
	hub.clients["c-cap"] = client

	ids := make([]string, maxSubscriptionSize+500)
	for i := range ids {
		ids[i] = fmt.Sprintf("srv-%d", i)
	}
	hub.Subscribe("c-cap", ids)

	if got := len(client.subscription); got != maxSubscriptionSize {
		t.Fatalf("订阅大小 = %d, want %d（超限应截断）", got, maxSubscriptionSize)
	}
}
