package commandguard

import "testing"

func TestDetectDangerousVariants(t *testing.T) {
	shouldBlock := []string{
		"rm -rf /tmp/x",
		"rm -fr /tmp/x",
		"rm -rfv /tmp/x",
		"rm --recursive --force /tmp/x",
		"shutdown -h now",
		"reboot",
		"/sbin/poweroff",
		"Stop-Computer -Force",
		"dd if=/dev/zero of=/dev/sda",
		"mkfs.ext4 /dev/sdb1",
		"docker system prune -a",
		"kubectl delete pod x",
		"DROP TABLE users",
	}
	shouldAllow := []string{
		"echo hello",
		"rm -f /tmp/single.log",
		"rm -r /tmp/emptydir",
		"ls -la",
		"systemctl status sshd",
		"wsl --shutdown",
		"docker ps",
	}
	for _, c := range shouldBlock {
		if !Detect(c).Dangerous {
			t.Errorf("should block: %q", c)
		}
	}
	for _, c := range shouldAllow {
		if Detect(c).Dangerous {
			t.Errorf("should allow: %q (reasons=%v)", c, Detect(c).Reasons)
		}
	}
}

func TestDetectReasonsDedup(t *testing.T) {
	res := Detect("rm -rf /a && rm -rf /b")
	if !res.Dangerous {
		t.Fatal("expected dangerous")
	}
	if len(res.Reasons) != 1 {
		t.Fatalf("expected reasons deduped to 1, got %v", res.Reasons)
	}
}

func TestJoinReasons(t *testing.T) {
	if got := JoinReasons([]string{"a", "b"}); got != "a, b" {
		t.Fatalf("unexpected join: %q", got)
	}
}
