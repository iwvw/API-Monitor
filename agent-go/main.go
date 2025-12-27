package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

const VERSION = "2.0.1"

// Agent 事件类型 (与服务端 protocol.js 保持一致)
const (
	EventAgentConnect    = "agent:connect"
	EventAgentHostInfo   = "agent:host_info"
	EventAgentState      = "agent:state"
	EventAgentTaskResult = "agent:task_result"
	EventDashboardAuthOK = "dashboard:auth_ok"
	EventDashboardAuthFail = "dashboard:auth_fail"
	EventDashboardTask   = "dashboard:task"
)

// Config Agent 配置
type Config struct {
	ServerURL        string `json:"serverUrl"`
	ServerID         string `json:"serverId"`
	AgentKey         string `json:"agentKey"`
	ReportInterval   int    `json:"reportInterval"`   // 毫秒
	HostInfoInterval int    `json:"hostInfoInterval"` // 毫秒
	ReconnectDelay   int    `json:"reconnectDelay"`   // 毫秒
	Debug            bool   `json:"debug"`
}

// SocketIOMessage Socket.IO 消息格式
type SocketIOMessage struct {
	Type      int    // 消息类型
	Namespace string // 命名空间
	Event     string // 事件名
	Data      interface{}
}

// AgentClient Agent 客户端
type AgentClient struct {
	config        *Config
	conn          *websocket.Conn
	authenticated bool
	collector     *Collector
	stopChan      chan struct{}
	mu            sync.Mutex
	reconnecting  bool
}

// NewAgentClient 创建新的 Agent 客户端
func NewAgentClient(config *Config) *AgentClient {
	return &AgentClient{
		config:    config,
		collector: NewCollector(),
		stopChan:  make(chan struct{}),
	}
}

// Start 启动 Agent
func (a *AgentClient) Start() {
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Printf("  API Monitor Agent v%s (Go)\n", VERSION)
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Printf("  Server:   %s\n", a.config.ServerURL)
	fmt.Printf("  ServerID: %s\n", a.config.ServerID)
	fmt.Printf("  Interval: %dms\n", a.config.ReportInterval)
	fmt.Println("═══════════════════════════════════════════════")

	// 预热数据采集 (同步等待完成，确保 GPU 信息已获取)
	log.Println("[Agent] 正在预热数据采集...")
	
	// 第一次采集：建立 CPU 使用率基准
	a.collector.CollectState()
	
	// 等待 1 秒，让 CPU 采集有足够的时间间隔
	time.Sleep(1 * time.Second)
	
	// 并行采集主机信息和第二次状态
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		a.collector.CollectHostInfo()
		log.Println("[Agent] ✓ 主机信息预热完成")
	}()
	go func() {
		defer wg.Done()
		a.collector.CollectState() // 第二次采集，此时 CPU 数据应该准确
		log.Println("[Agent] ✓ 实时状态预热完成")
	}()
	wg.Wait() // 等待预热完成

	// 连接服务器
	a.connect()
}

// connect 连接到服务器
func (a *AgentClient) connect() {
	for {
		select {
		case <-a.stopChan:
			return
		default:
		}

		err := a.dial()
		if err != nil {
			log.Printf("[Agent] 连接失败: %v", err)
			time.Sleep(time.Duration(a.config.ReconnectDelay) * time.Millisecond)
			continue
		}

		// 连接成功，开始消息循环
		a.messageLoop()

		// 连接断开，等待重连
		a.mu.Lock()
		a.authenticated = false
		a.mu.Unlock()

		log.Println("[Agent] 连接断开，准备重连...")
		time.Sleep(time.Duration(a.config.ReconnectDelay) * time.Millisecond)
	}
}

// dial 建立 WebSocket 连接
func (a *AgentClient) dial() error {
	// 构建 Socket.IO 握手 URL
	u, err := url.Parse(a.config.ServerURL)
	if err != nil {
		return fmt.Errorf("无效的服务器地址: %v", err)
	}

	// Socket.IO 需要先进行 HTTP 握手获取 sid
	scheme := "ws"
	if u.Scheme == "https" {
		scheme = "wss"
	}

	// Socket.IO v4 握手
	handshakeURL := fmt.Sprintf("%s://%s/socket.io/?EIO=4&transport=polling", u.Scheme, u.Host)
	resp, err := http.Get(handshakeURL)
	if err != nil {
		return fmt.Errorf("握手失败: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	// Socket.IO 响应格式: 0{"sid":"xxx",...}
	bodyStr := string(body)
	if len(bodyStr) < 2 {
		return fmt.Errorf("无效的握手响应")
	}

	var handshake struct {
		SID string `json:"sid"`
	}
	if err := json.Unmarshal([]byte(bodyStr[1:]), &handshake); err != nil {
		return fmt.Errorf("解析握手响应失败: %v", err)
	}

	// 升级到 WebSocket
	wsURL := fmt.Sprintf("%s://%s/socket.io/?EIO=4&transport=websocket&sid=%s", scheme, u.Host, handshake.SID)
	log.Printf("[Agent] 正在连接: %s", wsURL)

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("WebSocket 连接失败: %v", err)
	}

	a.conn = conn

	// 发送 Socket.IO 升级确认
	if err := conn.WriteMessage(websocket.TextMessage, []byte("2probe")); err != nil {
		return err
	}

	// 等待服务器确认
	_, msg, err := conn.ReadMessage()
	if err != nil || string(msg) != "3probe" {
		return fmt.Errorf("升级确认失败")
	}

	// 发送升级完成
	if err := conn.WriteMessage(websocket.TextMessage, []byte("5")); err != nil {
		return err
	}

	// 连接到 /agent 命名空间
	if err := conn.WriteMessage(websocket.TextMessage, []byte("40/agent,")); err != nil {
		return err
	}

	// 等待命名空间确认 (40/agent,{...})
	_, nsMsg, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("命名空间确认失败: %v", err)
	}
	nsStr := string(nsMsg)
	if !strings.HasPrefix(nsStr, "40/agent") {
		// 可能是 ping 消息，继续读取
		if nsStr == "2" {
			conn.WriteMessage(websocket.TextMessage, []byte("3"))
			_, nsMsg, err = conn.ReadMessage()
			if err != nil {
				return fmt.Errorf("命名空间确认失败: %v", err)
			}
			nsStr = string(nsMsg)
		}
	}

	log.Printf("[Agent] 命名空间已确认: %s", nsStr)
	log.Println("[Agent] 已连接，正在认证...")

	// 发送认证
	a.authenticate()

	return nil
}

// authenticate 发送认证请求
func (a *AgentClient) authenticate() {
	hostname, _ := os.Hostname()
	authData := map[string]interface{}{
		"server_id": a.config.ServerID,
		"key":       a.config.AgentKey,
		"hostname":  hostname,
		"version":   VERSION,
	}
	a.emit(EventAgentConnect, authData)
}

// emit 发送事件
func (a *AgentClient) emit(event string, data interface{}) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.conn == nil {
		return fmt.Errorf("未连接")
	}

	// Socket.IO 事件格式: 42/namespace,["event", data]
	payload := []interface{}{event, data}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	msg := fmt.Sprintf("42/agent,%s", string(jsonData))
	return a.conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

// messageLoop 消息处理循环
func (a *AgentClient) messageLoop() {
	// 启动心跳
	go a.heartbeat()

	for {
		select {
		case <-a.stopChan:
			return
		default:
		}

		_, message, err := a.conn.ReadMessage()
		if err != nil {
			log.Printf("[Agent] 读取消息失败: %v", err)
			return
		}

		msg := string(message)
		// 调试日志：显示收到的消息（排除心跳）
		if msg != "2" && msg != "3" {
			log.Printf("[Agent] 收到消息: %s", msg)
		}

		a.handleMessage(msg)
	}
}

// handleMessage 处理消息
func (a *AgentClient) handleMessage(msg string) {
	// Socket.IO 消息格式解析
	if len(msg) < 1 {
		return
	}

	// 服务端发送的 ping，需要立即回复 pong
	if msg == "2" {
		a.mu.Lock()
		if a.conn != nil {
			a.conn.WriteMessage(websocket.TextMessage, []byte("3"))
		}
		a.mu.Unlock()
		return
	}

	// 心跳响应 (服务端回复的 pong)
	if msg == "3" {
		return
	}

	// 命名空间确认
	if strings.HasPrefix(msg, "40/agent") {
		return
	}

	// 事件消息: 42/agent,["event", data]
	if strings.HasPrefix(msg, "42/agent,") {
		jsonStr := msg[9:] // 移除 "42/agent,"

		var payload []json.RawMessage
		if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
			log.Printf("[Agent] 解析消息失败: %v", err)
			return
		}

		if len(payload) < 1 {
			return
		}

		var event string
		json.Unmarshal(payload[0], &event)

		var data json.RawMessage
		if len(payload) > 1 {
			data = payload[1]
		}

		a.handleEvent(event, data)
	}
}

// handleEvent 处理事件
func (a *AgentClient) handleEvent(event string, data json.RawMessage) {
	switch event {
	case EventDashboardAuthOK:
		log.Println("[Agent] ✅ 认证成功")
		a.mu.Lock()
		a.authenticated = true
		a.mu.Unlock()

		// 稍微延迟后再发送数据，避免与 ping/pong 竞争
		go func() {
			time.Sleep(100 * time.Millisecond)
			// 发送主机信息
			a.reportHostInfo()
			// 启动上报循环
			a.reportLoop()
		}()

	case EventDashboardAuthFail:
		var failData struct {
			Reason string `json:"reason"`
		}
		json.Unmarshal(data, &failData)
		log.Printf("[Agent] ❌ 认证失败: %s", failData.Reason)
		os.Exit(1)

	case EventDashboardTask:
		var task struct {
			ID      string `json:"id"`
			Type    int    `json:"type"`
			Data    string `json:"data"`
			Timeout int    `json:"timeout"`
		}
		json.Unmarshal(data, &task)
		go a.handleTask(task.ID, task.Type, task.Data, task.Timeout)
	}
}

// reportHostInfo 上报主机信息
func (a *AgentClient) reportHostInfo() {
	hostInfo := a.collector.CollectHostInfo()
	if err := a.emit(EventAgentHostInfo, hostInfo); err != nil {
		log.Printf("[Agent] 上报主机信息失败: %v", err)
	} else if a.config.Debug {
		log.Println("[Agent] 已上报主机信息")
	}
}

// reportState 上报实时状态
func (a *AgentClient) reportState() {
	a.mu.Lock()
	auth := a.authenticated
	a.mu.Unlock()

	if !auth {
		return
	}

	state := a.collector.CollectState()
	if err := a.emit(EventAgentState, state); err != nil {
		log.Printf("[Agent] 状态上报失败: %v", err)
	} else if a.config.Debug {
		log.Printf("[Agent] 状态上报: CPU=%.1f%%, MEM=%.1fGB, GPU=%.1f%%, Power=%.1fW",
			state.CPU, float64(state.MemUsed)/1024/1024/1024, state.GPU, state.GPUPower)
	}
}

// reportLoop 定时上报循环
func (a *AgentClient) reportLoop() {
	// 立即上报一次
	a.reportState()

	stateTicker := time.NewTicker(time.Duration(a.config.ReportInterval) * time.Millisecond)
	hostInfoTicker := time.NewTicker(time.Duration(a.config.HostInfoInterval) * time.Millisecond)

	defer stateTicker.Stop()
	defer hostInfoTicker.Stop()

	for {
		select {
		case <-a.stopChan:
			return
		case <-stateTicker.C:
			a.reportState()
		case <-hostInfoTicker.C:
			a.reportHostInfo()
		}

		a.mu.Lock()
		auth := a.authenticated
		a.mu.Unlock()
		if !auth {
			return
		}
	}
}

// heartbeat 心跳监控 - 只处理停止信号，ping响应在handleMessage中处理
func (a *AgentClient) heartbeat() {
	// Socket.IO 中只有服务端发送 ping (2)，客户端只需响应 pong (3)
	// 我们在 handleMessage 中已经处理了 ping 响应
	// 这个 goroutine 只是为了监听 stopChan
	<-a.stopChan
}

// handleTask 处理任务
func (a *AgentClient) handleTask(id string, taskType int, data string, timeout int) {
	log.Printf("[Agent] 收到任务: %s (type=%d)", id, taskType)

	result := map[string]interface{}{
		"id":         id,
		"type":       taskType,
		"successful": false,
		"data":       "",
		"delay":      0,
	}

	startTime := time.Now()

	switch taskType {
	case 6: // REPORT_HOST_INFO
		a.reportHostInfo()
		result["successful"] = true
	case 7: // KEEPALIVE
		result["successful"] = true
	case 10: // DOCKER_ACTION
		output, err := a.handleDockerAction(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	default:
		result["data"] = fmt.Sprintf("不支持的任务类型: %d", taskType)
	}

	result["delay"] = time.Since(startTime).Milliseconds()

	a.emit(EventAgentTaskResult, result)
	log.Printf("[Agent] 任务完成: %s", id)
}

// DockerActionRequest Docker 操作请求
type DockerActionRequest struct {
	Action      string `json:"action"`       // start, stop, restart, pause, unpause, update
	ContainerID string `json:"container_id"` // 容器 ID 或名称
	Image       string `json:"image"`        // 更新时使用的镜像
}

// handleDockerAction 处理 Docker 操作
func (a *AgentClient) handleDockerAction(data string) (string, error) {
	var req DockerActionRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.ContainerID == "" {
		return "", fmt.Errorf("缺少容器 ID")
	}

	var cmd *exec.Cmd
	var actionDesc string

	switch req.Action {
	case "start":
		cmd = exec.Command("docker", "start", req.ContainerID)
		actionDesc = "启动"
	case "stop":
		cmd = exec.Command("docker", "stop", req.ContainerID)
		actionDesc = "停止"
	case "restart":
		cmd = exec.Command("docker", "restart", req.ContainerID)
		actionDesc = "重启"
	case "pause":
		cmd = exec.Command("docker", "pause", req.ContainerID)
		actionDesc = "暂停"
	case "unpause":
		cmd = exec.Command("docker", "unpause", req.ContainerID)
		actionDesc = "恢复"
	case "update":
		// 更新流程: pull 新镜像 -> stop -> rm -> run
		return a.handleDockerUpdate(req)
	case "pull":
		// 仅拉取镜像
		image := req.Image
		if image == "" {
			// 获取容器的镜像
			inspectCmd := exec.Command("docker", "inspect", "--format", "{{.Config.Image}}", req.ContainerID)
			output, err := inspectCmd.Output()
			if err != nil {
				return "", fmt.Errorf("获取容器镜像失败: %v", err)
			}
			image = strings.TrimSpace(string(output))
		}
		cmd = exec.Command("docker", "pull", image)
		actionDesc = "拉取镜像"
	default:
		return "", fmt.Errorf("不支持的操作: %s", req.Action)
	}

	log.Printf("[Docker] %s容器: %s", actionDesc, req.ContainerID)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s失败: %s", actionDesc, string(output))
	}

	return fmt.Sprintf("%s成功", actionDesc), nil
}

// handleDockerUpdate 处理 Docker 容器更新
func (a *AgentClient) handleDockerUpdate(req DockerActionRequest) (string, error) {
	// 1. 获取容器信息
	inspectCmd := exec.Command("docker", "inspect", "--format",
		"{{.Config.Image}}|{{.HostConfig.RestartPolicy.Name}}|{{json .HostConfig.PortBindings}}|{{json .Config.Env}}|{{json .HostConfig.Binds}}|{{.Name}}",
		req.ContainerID)
	output, err := inspectCmd.Output()
	if err != nil {
		return "", fmt.Errorf("获取容器信息失败: %v", err)
	}

	parts := strings.SplitN(strings.TrimSpace(string(output)), "|", 6)
	if len(parts) < 6 {
		return "", fmt.Errorf("解析容器信息失败")
	}

	image := parts[0]
	containerName := strings.TrimPrefix(parts[5], "/")

	log.Printf("[Docker] 更新容器: %s (镜像: %s)", containerName, image)

	// 2. 拉取最新镜像
	pullCmd := exec.Command("docker", "pull", image)
	if pullOutput, err := pullCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("拉取镜像失败: %s", string(pullOutput))
	}

	// 3. 停止旧容器
	stopCmd := exec.Command("docker", "stop", req.ContainerID)
	stopCmd.Run()

	// 4. 重命名旧容器 (备份)
	backupName := containerName + "_backup_" + time.Now().Format("20060102150405")
	renameCmd := exec.Command("docker", "rename", req.ContainerID, backupName)
	renameCmd.Run()

	// 5. 使用相同配置启动新容器
	// 注意：这是简化实现，完整实现需要解析并重建所有参数
	runArgs := []string{"run", "-d", "--name", containerName}
	
	// 解析 restart policy
	if parts[1] != "" && parts[1] != "no" {
		runArgs = append(runArgs, "--restart", parts[1])
	}

	runArgs = append(runArgs, image)
	
	runCmd := exec.Command("docker", runArgs...)
	if runOutput, err := runCmd.CombinedOutput(); err != nil {
		// 恢复旧容器
		exec.Command("docker", "rename", backupName, containerName).Run()
		exec.Command("docker", "start", containerName).Run()
		return "", fmt.Errorf("启动新容器失败: %s", string(runOutput))
	}

	// 6. 删除备份容器
	exec.Command("docker", "rm", backupName).Run()

	return fmt.Sprintf("容器 %s 更新成功", containerName), nil
}

// Stop 停止 Agent
func (a *AgentClient) Stop() {
	close(a.stopChan)

	a.mu.Lock()
	if a.conn != nil {
		a.conn.Close()
	}
	a.mu.Unlock()

	log.Println("[Agent] 已关闭")
}

// ==================== 主程序 ====================

func main() {
	// 检查是否以 Windows 服务方式运行
	if IsRunningAsService() {
		RunAsService()
		return
	}

	// 检查服务管理命令
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			if err := InstallService(); err != nil {
				fmt.Println("❌ 安装失败:", err)
				os.Exit(1)
			}
			return
		case "uninstall", "remove":
			if err := UninstallService(); err != nil {
				fmt.Println("❌ 卸载失败:", err)
				os.Exit(1)
			}
			return
		case "start":
			if err := StartService(); err != nil {
				fmt.Println("❌ 启动失败:", err)
				os.Exit(1)
			}
			return
		case "stop":
			if err := StopService(); err != nil {
				fmt.Println("❌ 停止失败:", err)
				os.Exit(1)
			}
			return
		case "service":
			// 直接以服务模式运行（由 Windows SCM 调用）
			RunAsService()
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	// 命令行参数
	serverURL := flag.String("s", "", "Dashboard 地址")
	serverID := flag.String("id", "", "主机 ID")
	agentKey := flag.String("k", "", "Agent 密钥")
	interval := flag.Int("i", 1500, "上报间隔 (毫秒)")
	debug := flag.Bool("d", false, "调试模式")
	flag.Parse()

	// 加载配置
	config := &Config{
		ServerURL:        "http://localhost:3000",
		ReportInterval:   1500,
		HostInfoInterval: 600000,
		ReconnectDelay:   4000,
	}

	// 从配置文件加载（使用可执行文件所在目录）
	exePath, _ := os.Executable()
	configPath := filepath.Join(filepath.Dir(exePath), "config.json")
	if data, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(data, config)
		log.Println("[Config] 已加载配置文件:", configPath)
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

	// 命令行参数覆盖
	if *serverURL != "" {
		config.ServerURL = *serverURL
	}
	if *serverID != "" {
		config.ServerID = *serverID
	}
	if *agentKey != "" {
		config.AgentKey = *agentKey
	}
	if *interval > 0 {
		config.ReportInterval = *interval
	}
	if *debug {
		config.Debug = true
	}

	// 验证配置
	if config.ServerID == "" {
		log.Fatal("[Config] 错误: 缺少 serverId，使用 --id 指定")
	}
	if config.AgentKey == "" {
		log.Fatal("[Config] 错误: 缺少 agentKey，使用 -k 指定")
	}

	// 创建并启动 Agent
	agent := NewAgentClient(config)

	// 优雅退出
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("\n[Agent] 收到退出信号...")
		agent.Stop()
		os.Exit(0)
	}()

	agent.Start()
}

func init() {
	// 设置日志格式
	log.SetFlags(log.Ltime)
	
	// 设置最大可用 CPU
	runtime.GOMAXPROCS(runtime.NumCPU())
}

// printUsage 打印使用帮助
func printUsage() {
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Printf("  API Monitor Agent v%s (Go)\n", VERSION)
	fmt.Println("═══════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("使用方法:")
	fmt.Println("  api-monitor-agent [命令] [选项]")
	fmt.Println()
	fmt.Println("服务管理命令 (需要管理员权限):")
	fmt.Println("  install     安装为 Windows 服务 (开机自启)")
	fmt.Println("  uninstall   卸载 Windows 服务")
	fmt.Println("  start       启动服务")
	fmt.Println("  stop        停止服务")
	fmt.Println()
	fmt.Println("直接运行选项:")
	fmt.Println("  -s <url>    Dashboard 地址")
	fmt.Println("  -id <id>    主机 ID")
	fmt.Println("  -k <key>    Agent 密钥")
	fmt.Println("  -i <ms>     上报间隔 (毫秒, 默认 1500)")
	fmt.Println("  -d          调试模式")
	fmt.Println()
	fmt.Println("配置文件:")
	fmt.Println("  将 config.json 放在程序同目录下")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  api-monitor-agent install           # 安装服务")
	fmt.Println("  api-monitor-agent start             # 启动服务")  
	fmt.Println("  api-monitor-agent -s https://xxx -id abc -k key123")
}
