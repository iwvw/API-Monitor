//go:build windows

package mihomo

import (
	"os/exec"
	"syscall"
)

// hideSubprocessWindow 让 mihomo 子进程在 Windows 上不弹出控制台窗口。
func hideSubprocessWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
