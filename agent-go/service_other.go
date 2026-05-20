// +build !windows

package main

import "fmt"

// IsRunningAsService 非 Windows 平台始终返回 false
func IsRunningAsService() bool {
	return false
}

// RunAsService 非 Windows 平台不支持服务模式
func RunAsService() {
	fmt.Println("Windows 服务模式仅在 Windows 平台可用")
}

// InstallService 非 Windows 平台不支持
func InstallService() error {
	return fmt.Errorf("Windows 服务模式仅在 Windows 平台可用")
}

// UninstallService 非 Windows 平台不支持
func UninstallService() error {
	return fmt.Errorf("Windows 服务模式仅在 Windows 平台可用")
}

// StartService 非 Windows 平台不支持
func StartService() error {
	return fmt.Errorf("Windows 服务模式仅在 Windows 平台可用")
}

// StopService 非 Windows 平台不支持
func StopService() error {
	return fmt.Errorf("Windows 服务模式仅在 Windows 平台可用")
}

// StopUserAgent 非 Windows 平台暂不支持或无需特殊处理
func StopUserAgent() {
	// 非 Windows 平台无需处理（由安装脚本或进程信号管理）
}

// InstallUserStartup 非 Windows 平台暂不支持
func InstallUserStartup() error {
	return fmt.Errorf("用户级注册表自启模式仅在 Windows 平台可用")
}

// UninstallUserStartup 非 Windows 平台暂不支持
func UninstallUserStartup() error {
	return fmt.Errorf("用户级注册表自启模式仅在 Windows 平台可用")
}

