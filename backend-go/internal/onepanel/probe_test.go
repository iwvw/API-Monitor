package onepanel

import "testing"

func TestParseOnePanelProbe(t *testing.T) {
	cases := []struct {
		name      string
		output    string
		installed bool
		reason    string
	}{
		{"1pctl 命中直接判定", "cmd=1\ndir=0\nproc=0\nport=0\n", true, "1pctl"},
		{"仅目录", "cmd=0\ndir=1\nproc=0\nport=0\n", true, "/opt/1panel"},
		{"仅进程", "cmd=0\ndir=0\nproc=1\nport=0\n", true, "1panel-core"},
		{"仅端口", "cmd=0\ndir=0\nproc=0\nport=1\n", true, "port 8888"},
		{"全部未命中", "cmd=0\ndir=0\nproc=0\nport=0\n", false, ""},
		{"空输出", "", false, ""},
		{"无关行被忽略", "hello\ncmd=0\nunknown=1\n", false, ""},
		{"带空格与回车", "cmd=0\r\ndir= 1 \r\n", true, "/opt/1panel"},
		{"值非 1 不算命中", "cmd=yes\ndir=2\n", false, ""},
	}

	for _, c := range cases {
		installed, reason := parseOnePanelProbe(c.output)
		if installed != c.installed || reason != c.reason {
			t.Fatalf("%s: got (%v,%q), want (%v,%q)", c.name, installed, reason, c.installed, c.reason)
		}
	}
}

func TestParseOnePanelProbePrefersCmd(t *testing.T) {
	// 命令与目录同时命中时，reason 应给出最直接的证据。
	installed, reason := parseOnePanelProbe("dir=1\ncmd=1\n")
	if !installed || reason != "1pctl" {
		t.Fatalf("got (%v,%q), want (true,\"1pctl\")", installed, reason)
	}
}
