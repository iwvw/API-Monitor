package serveragent

import (
	"net/http"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/secure"
)

// TestCompareAgentVersion 覆盖版本比较的边界：缺段补 0、前缀 v、预发布后缀。
func TestCompareAgentVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.6.37", "0.6.37", 0},
		{"0.6.38", "0.6.37", 1},
		{"0.6.36", "0.6.37", -1},
		{"0.6.37-beta.1", "0.6.37", 0},
		{"v0.6.37", "0.6.37", 0},
		{"0.7", "0.6.37", 1},
		{"0.6", "0.6.37", -1},
		{"1.0.0", "0.9.99", 1},
	}
	for _, c := range cases {
		if got := compareAgentVersion(c.a, c.b); got != c.want {
			t.Fatalf("compareAgentVersion(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestAgentVersionAtLeastRequiresMetadata 验证没有版本上报时一律判为不支持：
// 宁可回退 SSH，也不能让旧 Agent 静默把文件写进沙箱。
func TestAgentVersionAtLeastRequiresMetadata(t *testing.T) {
	conn := &AgentConnection{Metadata: map[string]interface{}{}}
	if agentVersionAtLeast(conn, minAgentFileRealPathVersion) {
		t.Fatal("missing version must be treated as unsupported")
	}

	conn.SetMetadata("agent_version", "0.6.36")
	if agentVersionAtLeast(conn, minAgentFileRealPathVersion) {
		t.Fatal("0.6.36 must not be treated as supporting realPath")
	}

	conn.SetMetadata("agent_version", minAgentFileRealPathVersion)
	if !agentVersionAtLeast(conn, minAgentFileRealPathVersion) {
		t.Fatalf("%s must be treated as supporting realPath", minAgentFileRealPathVersion)
	}
}

// TestAgentSupportsFileRealPathUsesRegistry 验证门禁走的是在线连接的实际版本。
func TestAgentSupportsFileRealPathUsesRegistry(t *testing.T) {
	service, db := testService(t)
	_ = db

	if service.agentSupportsFileRealPath("not-connected") {
		t.Fatal("offline host must not be treated as supporting realPath")
	}

	conn := service.registry.Register("old-agent", &stubSocket{})
	conn.SetMetadata("agent_version", "0.6.36")
	if service.agentSupportsFileRealPath("old-agent") {
		t.Fatal("old agent must not be treated as supporting realPath")
	}

	conn.SetMetadata("agent_version", "0.6.37")
	if !service.agentSupportsFileRealPath("old-agent") {
		t.Fatal("0.6.37 must be treated as supporting realPath")
	}
}

// TestSFTPWriteRejectsOldAgentWithoutSSH 验证：Agent 版本过低且无 SSH 兜底时，
// 写文件必须直接失败，而不是落到 Agent 沙箱里假装成功。
func TestSFTPWriteRejectsOldAgentWithoutSSH(t *testing.T) {
	service, db := testService(t)
	_, err := db.Exec(`INSERT INTO server_accounts (id, name, host, username, auth_type) VALUES ('legacy-file', 'legacy', '', 'root', 'password')`)
	if err != nil {
		t.Fatalf("insert account: %v", err)
	}

	dispatched := false
	conn := service.registry.Register("legacy-file", &taskReplySocket{
		t:       t,
		service: service,
		reply: func(taskType int, data string) string {
			dispatched = true
			return "文件保存成功"
		},
	})
	conn.SetMetadata("agent_version", "0.6.36")

	res := perform(service, http.MethodPost, "/api/server/sftp/write",
		`{"serverId":"legacy-file","path":"/opt/app/docker-compose.yml","content":"x"}`)
	if res.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s, want 409", res.Code, res.Body.String())
	}
	if dispatched {
		t.Fatal("old agent must not receive the file write task at all")
	}
}

// TestAgentFileModeMatrix 覆盖文件通道选择矩阵：
// 在线+新 Agent→Agent；在线+旧 Agent+有 SSH→SSH；在线+旧 Agent+无 SSH→不可用；
// 离线+有 SSH→SSH。
func TestAgentFileModeMatrix(t *testing.T) {
	service, db := testService(t)

	seed := func(id, host, username, password string) {
		t.Helper()
		enc := ""
		if password != "" {
			var err error
			enc, err = secure.SecureEncrypt(password)
			if err != nil {
				t.Fatalf("encrypt: %v", err)
			}
		}
		if _, err := db.Exec(`INSERT INTO server_accounts (id, name, host, username, auth_type, password) VALUES (?, ?, ?, ?, 'password', ?)`,
			id, id, host, username, enc); err != nil {
			t.Fatalf("insert account: %v", err)
		}
	}

	seed("new-agent", "192.0.2.10", "root", "pw")
	seed("old-agent-ssh", "192.0.2.11", "root", "pw")
	seed("old-agent-no-ssh", "", "", "")
	seed("offline-ssh", "192.0.2.12", "root", "pw")

	service.registry.Register("new-agent", &stubSocket{}).
		SetMetadata("agent_version", minAgentFileRealPathVersion)
	service.registry.Register("old-agent-ssh", &stubSocket{}).
		SetMetadata("agent_version", "0.6.30")
	service.registry.Register("old-agent-no-ssh", &stubSocket{}).
		SetMetadata("agent_version", "0.6.30")

	cases := []struct {
		serverID string
		want     agentFileMode
	}{
		{"new-agent", agentFileViaAgent},
		{"old-agent-ssh", agentFileViaSSH},
		{"old-agent-no-ssh", agentFileUnavailable},
		{"offline-ssh", agentFileViaSSH},
	}
	for _, c := range cases {
		if got := service.resolveAgentFileMode(db, c.serverID); got != c.want {
			t.Fatalf("resolveAgentFileMode(%q) = %v, want %v", c.serverID, got, c.want)
		}
	}
}
