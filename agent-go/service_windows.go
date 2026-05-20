// +build windows

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	user32           = syscall.NewLazyDLL("user32.dll")
	getConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	showWindow       = user32.NewProc("ShowWindow")
)

const SW_HIDE = 0

// HideConsoleWindow 隐藏控制台窗口 (用于非服务模式的后台运行)
func HideConsoleWindow() {
	hwnd, _, _ := getConsoleWindow.Call()
	if hwnd != 0 {
		showWindow.Call(hwnd, SW_HIDE)
	}
}

const serviceName = "APIMonitorAgent"
const serviceDisplayName = "API Monitor Agent"
const serviceDescription = "API Monitor 服务器监控代理，用于采集和上报服务器指标"

// AgentService 实现 Windows 服务接口
type AgentService struct {
	agent *AgentClient
}

// Execute 是 Windows 服务的主入口点
func (s *AgentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const acceptedCmds = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	// 初始化日志到事件查看器
	elog, err := eventlog.Open(serviceName)
	if err == nil {
		defer elog.Close()
		elog.Info(1, fmt.Sprintf("%s 服务正在启动...", serviceName))
	}

	// 加载配置并创建 Agent
	config := loadServiceConfig()
	if config == nil {
		if elog != nil {
			elog.Error(1, "加载配置失败")
		}
		return false, 1
	}

	s.agent = NewAgentClient(config)

	// 在后台启动 Agent
	go s.agent.Start()

	changes <- svc.Status{State: svc.Running, Accepts: acceptedCmds}

	if elog != nil {
		elog.Info(1, fmt.Sprintf("%s 服务已启动", serviceName))
	}

loop:
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				if elog != nil {
					elog.Info(1, fmt.Sprintf("%s 服务正在停止...", serviceName))
				}
				break loop
			default:
				if elog != nil {
					elog.Warning(1, fmt.Sprintf("收到未知控制请求: %d", c.Cmd))
				}
			}
		}
	}

	changes <- svc.Status{State: svc.StopPending}
	s.agent.Stop()

	if elog != nil {
		elog.Info(1, fmt.Sprintf("%s 服务已停止", serviceName))
	}

	return false, 0
}

// loadServiceConfig 从配置文件或注册表加载配置
func loadServiceConfig() *Config {
	config := &Config{
		ServerURL:        "http://localhost:3000",
		ReportInterval:   1500,
		HostInfoInterval: 600000,
		ReconnectDelay:   4000,
	}

	// 获取可执行文件所在目录
	exePath, err := os.Executable()
	if err == nil {
		configPath := filepath.Join(filepath.Dir(exePath), "config.json")
		if data, err := os.ReadFile(configPath); err == nil {
			json.Unmarshal(data, config)
		}
	}

	// 环境变量覆盖
	if env := os.Getenv("API_MONITOR_SERVER"); env != "" {
		config.ServerURL = env
	}
	if env := os.Getenv("API_MONITOR_SERVER_ID"); env != "" {
		config.ServerID = env
	}
	if env := os.Getenv("API_MONITOR_KEY"); env != "" {
		config.AgentKey = env
	}

	// 验证必要配置
	if config.ServerID == "" || config.AgentKey == "" {
		return nil
	}

	return config
}

// RunAsService 以 Windows 服务方式运行
func RunAsService() {
	err := svc.Run(serviceName, &AgentService{})
	if err != nil {
		log.Fatalf("服务运行失败: %v", err)
	}
}

// InstallService 安装 Windows 服务
func InstallService() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取程序路径失败: %v", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接服务管理器失败: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err == nil {
		s.Close()
		return fmt.Errorf("服务已存在")
	}

	s, err = m.CreateService(serviceName, exePath, mgr.Config{
		DisplayName: serviceDisplayName,
		Description: serviceDescription,
		StartType:   mgr.StartAutomatic,
	}, "service")
	if err != nil {
		return fmt.Errorf("创建服务失败: %v", err)
	}
	defer s.Close()

	// 自动注入安装用户的环境上下文，解决 LocalSystem 账户下命令或配置缺失问题
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\`+serviceName, registry.SET_VALUE)
	if err == nil {
		defer k.Close()
		var envs []string
		keys := []string{"PATH", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "HOMEDRIVE", "HOMEPATH"}
		for _, key := range keys {
			if val := os.Getenv(key); val != "" {
				envs = append(envs, key+"="+val)
			}
		}
		if len(envs) > 0 {
			if rErr := k.SetStringsValue("Environment", envs); rErr != nil {
				log.Printf("写入服务环境变量失败: %v", rErr)
			} else {
				log.Printf("成功将安装用户的环境变量 (%v) 注入服务注册表", keys)
			}
		}
	} else {
		log.Printf("打开服务注册表项失败: %v", err)
	}

	// 配置服务恢复选项：失败后自动重启
	err = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 86400) // 24小时后重置失败计数
	if err != nil {
		log.Printf("设置恢复选项失败: %v", err)
	}

	// 安装事件日志源
	err = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	if err != nil {
		log.Printf("安装事件日志源失败: %v", err)
	}

	fmt.Println("✅ 服务安装成功!")
	fmt.Println("   服务名称:", serviceName)
	fmt.Println("   启动类型: 自动")
	fmt.Println()
	fmt.Println("使用以下命令管理服务:")
	fmt.Println("   启动: sc start", serviceName)
	fmt.Println("   停止: sc stop", serviceName)
	fmt.Println("   状态: sc query", serviceName)

	return nil
}

// UninstallService 卸载 Windows 服务
func UninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接服务管理器失败: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("服务不存在: %v", err)
	}
	defer s.Close()

	// 先停止服务
	s.Control(svc.Stop)
	time.Sleep(2 * time.Second)

	err = s.Delete()
	if err != nil {
		return fmt.Errorf("删除服务失败: %v", err)
	}

	// 移除事件日志源
	eventlog.Remove(serviceName)

	fmt.Println("✅ 服务已卸载")
	return nil
}

// StartService 启动 Windows 服务
func StartService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接服务管理器失败: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("打开服务失败: %v", err)
	}
	defer s.Close()

	err = s.Start()
	if err != nil {
		return fmt.Errorf("启动服务失败: %v", err)
	}

	fmt.Println("✅ 服务已启动")
	return nil
}

// StopService 停止 Windows 服务
func StopService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("连接服务管理器失败: %v", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("打开服务失败: %v", err)
	}
	defer s.Close()

	_, err = s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("停止服务失败: %v", err)
	}

	fmt.Println("✅ 服务已停止")
	return nil
}

// IsRunningAsService 检查是否作为 Windows 服务运行
func IsRunningAsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return isService
}
