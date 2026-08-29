//go:build !windows

package mihomo

import "os/exec"

// hideSubprocessWindow 非 Windows 平台无需处理控制台窗口。
func hideSubprocessWindow(_ *exec.Cmd) {}
