package serveragent

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"
)

// 回归：文件删除/改名/改权限/删目录必须与 read/write/mkdir 一样，
// 在 Agent 因文件白名单拒绝路径时回退到 SSH/SFTP。
//
// 历史缺陷：handleSFTPDelete / handleSFTPRmdir / handleSFTPRename /
// handleSFTPChmod 在 Agent 返回错误后直接 writeSFTPError 返回，不回退 SSH。
// 结果是同一个目录（例如 Agent 安装目录 /opt/api-monitor-agent）在面板
// 文件管理里「能浏览、能写入、能新建目录，却删不掉」——因为
// list/write/mkdir 都会回退，只有这几个不会。用户只看到
// 「路径不在 Agent 文件白名单内」，无从判断为什么写入反而成功。
//
// 用例不依赖在线 Agent 或真实 SFTP 服务：主机配置了 SSH 凭据但地址不可达，
// 因此只要响应里不再出现「不在 Agent 文件白名单内」，就证明代码已经
// 进入 SSH 回退分支（回退后的连接失败属于预期结果）。
func TestSFTPDestructiveOpsDoNotReturnAgentWhitelistError(t *testing.T) {
	cases := []struct {
		name string
		call func(*Service, *sql.DB, string) *httptest.ResponseRecorder
	}{
		{
			name: "delete",
			call: func(s *Service, db *sql.DB, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/server/sftp/delete",
					strings.NewReader(`{"serverId":"`+id+`","path":"/opt/api-monitor-agent/ROLLBACK.txt"}`))
				s.handleSFTPDelete(rec, req, db)
				return rec
			},
		},
		{
			name: "rmdir",
			call: func(s *Service, db *sql.DB, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/server/sftp/rmdir",
					strings.NewReader(`{"serverId":"`+id+`","path":"/opt/api-monitor-agent/runtimes","recursive":false}`))
				s.handleSFTPRmdir(rec, req, db)
				return rec
			},
		},
		{
			name: "rename",
			call: func(s *Service, db *sql.DB, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/server/sftp/rename",
					strings.NewReader(`{"serverId":"`+id+`","oldPath":"/opt/api-monitor-agent/a","newPath":"/opt/api-monitor-agent/b"}`))
				s.handleSFTPRename(rec, req, db)
				return rec
			},
		},
		{
			name: "chmod",
			call: func(s *Service, db *sql.DB, id string) *httptest.ResponseRecorder {
				rec := httptest.NewRecorder()
				req := httptest.NewRequest("POST", "/api/server/sftp/chmod",
					strings.NewReader(`{"serverId":"`+id+`","path":"/opt/api-monitor-agent/ROLLBACK.txt","mode":"644"}`))
				s.handleSFTPChmod(rec, req, db)
				return rec
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, db := testService(t)
			const id = "host-fallback"
			if _, err := db.Exec(
				`INSERT INTO server_accounts(id,name,host,port,username,auth_type,password)
				 VALUES(?,?,?,?,?,?,?)`,
				id, "回退测试", "192.0.2.1", 22, "root", "password", "secret",
			); err != nil {
				t.Fatal(err)
			}

			rec := tc.call(service, db, id)
			body := rec.Body.String()
			if strings.Contains(body, "不在 Agent 文件白名单内") {
				t.Fatalf("%s 未回退 SSH，仍直接返回 Agent 白名单错误: %s", tc.name, body)
			}
		})
	}
}

// 对照用例：同一路径的写入本来就会回退 SSH（不受本次修改影响），
// 用来证明「同一目录写能成、删不能」这个差异确实存在且已被拉平。
func TestSFTPWriteDoesNotReturnAgentWhitelistError(t *testing.T) {
	service, db := testService(t)
	const id = "host-write"
	if _, err := db.Exec(
		`INSERT INTO server_accounts(id,name,host,port,username,auth_type,password)
		 VALUES(?,?,?,?,?,?,?)`,
		id, "写测试", "192.0.2.1", 22, "root", "password", "secret",
	); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/server/sftp/write",
		strings.NewReader(`{"serverId":"`+id+`","path":"/opt/api-monitor-agent/x","content":"x"}`))
	service.handleSFTPWrite(rec, req, db)
	if strings.Contains(rec.Body.String(), "不在 Agent 文件白名单内") {
		t.Fatalf("write 不应报白名单错误: %s", rec.Body.String())
	}
}
