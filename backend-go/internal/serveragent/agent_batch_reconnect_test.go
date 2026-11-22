package serveragent

import (
	"strings"
	"testing"
	"time"
)

func reconnectTestItem() *AgentBatchItem {
	return &AgentBatchItem{ServerID: "host-1", ServerName: "host-1"}
}

// reconnectTestAfter 返回一个早于 connection.AuthenticatedAt 的时间戳,
// 让「重连」判定成立。
func reconnectTestAfter() int64 {
	return time.Now().Add(-time.Minute).UnixNano() / int64(time.Millisecond)
}

// TestWaitForAgentReconnectAlreadyLatestSucceeds 回归:Agent 已是最新版本时,自升级脚本
// 重装同版本二进制、版本号不变,仍应判成功;且循环内关键日志只记录一次,不刷屏。
func TestWaitForAgentReconnectAlreadyLatestSucceeds(t *testing.T) {
	// 只搭最小 Service(仅 registry):避免走 New() 触发的后台 release 资产解析等全局副作用。
	service := &Service{registry: NewConnectionRegistry()}
	const serverID = "host-latest"
	conn := service.registry.Register(serverID, &stubSocket{})
	conn.SetMetadata("agent_version", "0.6.55")
	conn.SetMetadata("upgrade_status", map[string]interface{}{"state": "succeeded", "version": "0.6.55"})

	item := reconnectTestItem()
	ok, reason := service.waitForAgentReconnectWithLog(item, serverID, reconnectTestAfter(), 3*time.Second, "0.6.55")
	if !ok {
		t.Fatalf("already-latest reconnect should succeed, got reason=%q", reason)
	}
	if got := countContaining(item.Log, "Agent 已确认更新完成"); got != 1 {
		t.Fatalf("update-complete log should appear exactly once, got %d: %v", got, item.Log)
	}
	if got := countContaining(item.Log, "Agent 已重新上线"); got != 1 {
		t.Fatalf("reconnect log should appear exactly once, got %d: %v", got, item.Log)
	}
	if !hasContaining(item.Log, "已是最新版本: 0.6.55") {
		t.Fatalf("expected already-latest log, got %v", item.Log)
	}
}

// TestWaitForAgentReconnectVersionChangedSucceeds 版本真正变化时明确记录 from/to。
func TestWaitForAgentReconnectVersionChangedSucceeds(t *testing.T) {
	// 只搭最小 Service(仅 registry):避免走 New() 触发的后台 release 资产解析等全局副作用。
	service := &Service{registry: NewConnectionRegistry()}
	const serverID = "host-changed"
	conn := service.registry.Register(serverID, &stubSocket{})
	conn.SetMetadata("agent_version", "0.6.55")
	conn.SetMetadata("upgrade_status", map[string]interface{}{"state": "succeeded", "version": "0.6.55"})

	item := reconnectTestItem()
	ok, reason := service.waitForAgentReconnectWithLog(item, serverID, reconnectTestAfter(), 3*time.Second, "0.6.53")
	if !ok {
		t.Fatalf("version change should succeed, got reason=%q", reason)
	}
	if !hasContaining(item.Log, "从 0.6.53 更新到 0.6.55") {
		t.Fatalf("expected version-change log, got %v", item.Log)
	}
}

// TestWaitForAgentReconnectSameVersionWithoutSignalFails 无自升级成功信号且版本未变时,
// 超时后仍判「升级未生效」。
func TestWaitForAgentReconnectSameVersionWithoutSignalFails(t *testing.T) {
	// 只搭最小 Service(仅 registry):避免走 New() 触发的后台 release 资产解析等全局副作用。
	service := &Service{registry: NewConnectionRegistry()}
	const serverID = "host-stale"
	conn := service.registry.Register(serverID, &stubSocket{})
	conn.SetMetadata("agent_version", "0.6.55")

	item := reconnectTestItem()
	ok, reason := service.waitForAgentReconnectWithLog(item, serverID, reconnectTestAfter(), 300*time.Millisecond, "0.6.55")
	if ok {
		t.Fatal("same version without success signal should not succeed")
	}
	if !strings.Contains(reason, "版本未变化") {
		t.Fatalf("expected version-unchanged reason, got %q", reason)
	}
}

func countContaining(lines []string, sub string) int {
	n := 0
	for _, line := range lines {
		if strings.Contains(line, sub) {
			n++
		}
	}
	return n
}

func hasContaining(lines []string, sub string) bool {
	return countContaining(lines, sub) > 0
}
