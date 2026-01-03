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

const VERSION = "0.1.2"

// Agent 事件类型 (与服务端 protocol.js 保持一致)
const (
	EventAgentConnect    = "agent:connect"
	EventAgentHostInfo   = "agent:host_info"
	EventAgentState      = "agent:state"
	EventAgentTaskResult = "agent:task_result"
	EventDashboardAuthOK = "dashboard:auth_ok"
	EventDashboardAuthFail = "dashboard:auth_fail"
	EventDashboardTask   = "dashboard:task"
	EventDashboardPtyInput = "dashboard:pty_input"
	EventDashboardPtyResize = "dashboard:pty_resize"
	EventAgentPtyData    = "agent:pty_data"
)

// Task Types
const (
	TaskTypePtyStart = 12
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
	ptySessions   map[string]IPty      // taskId -> IPty
	taskProgress  map[string]*TaskProgress // taskId -> 进度
	progressMu    sync.RWMutex
}

// TaskProgress 任务进度
type TaskProgress struct {
	TaskID     string `json:"task_id"`
	Name       string `json:"name"`       // 任务名称
	Percentage int    `json:"percentage"` // 进度百分比 0-100
	Message    string `json:"message"`    // 当前步骤
	DetailMsg  string `json:"detail_msg"` // 详细信息
	IsDone     bool   `json:"is_done"`    // 是否完成
	IsError    bool   `json:"is_error"`   // 是否出错
}

// IPty PTY 接口实现抽象
type IPty interface {
	io.ReadWriteCloser
	Resize(cols, rows uint32) error
}

type PTYResizeData struct {
	Cols uint32 `json:"cols"`
	Rows uint32 `json:"rows"`
}

// NewAgentClient 创建新的 Agent 客户端
func NewAgentClient(config *Config) *AgentClient {
	return &AgentClient{
		config:       config,
		collector:    NewCollector(),
		stopChan:     make(chan struct{}),
		ptySessions:  make(map[string]IPty),
		taskProgress: make(map[string]*TaskProgress),
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

	case EventDashboardPtyInput:
		var input struct {
			ID   string `json:"id"`
			Data string `json:"data"`
		}
		if err := json.Unmarshal(data, &input); err == nil {
			a.mu.Lock()
			pty, ok := a.ptySessions[input.ID]
			a.mu.Unlock()
			if ok {
				pty.Write([]byte(input.Data))
			}
		}

	case EventDashboardPtyResize:
		var resize struct {
			ID   string `json:"id"`
			Cols uint32 `json:"cols"`
			Rows uint32 `json:"rows"`
		}
		if err := json.Unmarshal(data, &resize); err == nil {
			a.mu.Lock()
			pty, ok := a.ptySessions[resize.ID]
			a.mu.Unlock()
			if ok {
				pty.Resize(resize.Cols, resize.Rows)
			}
		}
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
	case 1: // COMMAND - 执行命令
		output, err := a.executeCommand(data, timeout)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
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
	case 11: // DOCKER_CHECK_UPDATE
		output, err := a.handleDockerCheckUpdate(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 13: // DOCKER_IMAGES - 镜像列表
		output, err := a.handleDockerImages(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 14: // DOCKER_IMAGE_ACTION - 镜像操作
		output, err := a.handleDockerImageAction(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 15: // DOCKER_NETWORKS - 网络列表
		output, err := a.handleDockerNetworks(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 16: // DOCKER_NETWORK_ACTION - 网络操作
		output, err := a.handleDockerNetworkAction(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 17: // DOCKER_VOLUMES - Volume 列表
		output, err := a.handleDockerVolumes(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 18: // DOCKER_VOLUME_ACTION - Volume 操作
		output, err := a.handleDockerVolumeAction(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 19: // DOCKER_LOGS - 容器日志
		output, err := a.handleDockerLogs(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 20: // DOCKER_STATS - 容器资源统计
		output, err := a.handleDockerStats(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 21: // DOCKER_COMPOSE_LIST - Compose 项目列表
		output, err := a.handleDockerComposeList(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 22: // DOCKER_COMPOSE_ACTION - Compose 操作
		output, err := a.handleDockerComposeAction(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 23: // DOCKER_CREATE_CONTAINER - 创建容器
		output, err := a.handleDockerCreateContainer(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 24: // DOCKER_UPDATE_CONTAINER - 容器一键更新
		go a.handleDockerContainerUpdate(id, data)
		result["successful"] = true
		result["data"] = "容器更新任务已启动"
		return // 异步任务，通过进度事件反馈
	case 25: // DOCKER_RENAME_CONTAINER - 容器重命名
		output, err := a.handleDockerRenameContainer(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 26: // DOCKER_TASK_PROGRESS - 查询任务进度
		output, err := a.getTaskProgress(data)
		if err != nil {
			result["data"] = err.Error()
		} else {
			result["successful"] = true
			result["data"] = output
		}
	case 5: // UPGRADE
		go a.handleUpgrade(id)
		result["successful"] = true
		result["data"] = "正在通过后台进程执行升级..."
	case TaskTypePtyStart: // 启动 PTY
		go a.handlePTYTask(id, data)
		return // PTY 任务是长连接，不立刻返回结果
	default:
		result["data"] = fmt.Sprintf("不支持的任务类型: %d", taskType)
	}

	result["delay"] = time.Since(startTime).Milliseconds()

	a.emit(EventAgentTaskResult, result)
	log.Printf("[Agent] 任务完成: %s", id)
}

// executeCommand 执行命令并返回输出
func (a *AgentClient) executeCommand(command string, timeout int) (string, error) {
	if command == "" {
		return "", fmt.Errorf("命令不能为空")
	}

	log.Printf("[Agent] 执行命令: %s", command)

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}

	// 设置超时
	timeoutDuration := 60 * time.Second
	if timeout > 0 {
		timeoutDuration = time.Duration(timeout) * time.Second
	}

	// 使用 context 实现超时
	done := make(chan error, 1)
	var output []byte
	var cmdErr error

	go func() {
		output, cmdErr = cmd.CombinedOutput()
		done <- cmdErr
	}()

	select {
	case <-time.After(timeoutDuration):
		// 超时，杀死进程
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		return "", fmt.Errorf("命令执行超时 (%d秒)", timeout)
	case err := <-done:
		if err != nil {
			// 命令执行失败但有输出，返回输出内容
			if len(output) > 0 {
				return string(output), fmt.Errorf("命令执行失败: %v\n%s", err, string(output))
			}
			return "", fmt.Errorf("命令执行失败: %v", err)
		}
		return string(output), nil
	}
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

// DockerCheckUpdateRequest 检查更新请求
type DockerCheckUpdateRequest struct {
	ContainerID string `json:"container_id"` // 容器 ID 或名称，留空则检查所有容器
}

// DockerImageUpdateStatus 镜像更新状态
type DockerImageUpdateStatus struct {
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Image         string `json:"image"`
	CurrentDigest string `json:"current_digest"`
	LatestDigest  string `json:"latest_digest"`
	HasUpdate     bool   `json:"has_update"`
	Error         string `json:"error,omitempty"`
}

// handleDockerCheckUpdate 处理 Docker 镜像更新检测
func (a *AgentClient) handleDockerCheckUpdate(data string) (string, error) {
	var req DockerCheckUpdateRequest
	if data != "" {
		json.Unmarshal([]byte(data), &req)
	}

	var containers []string

	if req.ContainerID != "" {
		// 检查指定容器
		containers = []string{req.ContainerID}
	} else {
		// 获取所有运行中的容器
		cmd := exec.Command("docker", "ps", "-q")
		output, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("获取容器列表失败: %v", err)
		}
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, line := range lines {
			if line != "" {
				containers = append(containers, line)
			}
		}
	}

	if len(containers) == 0 {
		return "[]", nil
	}

	var results []DockerImageUpdateStatus

	for _, containerID := range containers {
		status := a.checkContainerImageUpdate(containerID)
		results = append(results, status)
	}

	jsonResult, _ := json.Marshal(results)
	return string(jsonResult), nil
}

// checkContainerImageUpdate 检查单个容器的镜像更新
func (a *AgentClient) checkContainerImageUpdate(containerID string) DockerImageUpdateStatus {
	status := DockerImageUpdateStatus{
		ContainerID: containerID,
	}

	// 1. 获取容器信息 (Name 和 Image)
	inspectCmd := exec.Command("docker", "inspect", "--format",
		"{{.Name}}|{{.Config.Image}}",
		containerID)
	output, err := inspectCmd.Output()
	if err != nil {
		status.Error = fmt.Sprintf("获取容器信息失败: %v", err)
		return status
	}

	parts := strings.SplitN(strings.TrimSpace(string(output)), "|", 2)
	if len(parts) < 2 {
		status.Error = "解析容器信息失败"
		return status
	}

	status.ContainerName = strings.TrimPrefix(parts[0], "/")
	status.Image = parts[1]

	// 2. 从镜像获取本地 Digest
	localDigest := ""
	imgInspect := exec.Command("docker", "image", "inspect", "--format",
		"{{index .RepoDigests 0}}", status.Image)
	imgOutput, err := imgInspect.Output()
	if err == nil && strings.TrimSpace(string(imgOutput)) != "" && strings.TrimSpace(string(imgOutput)) != "<no value>" {
		if idx := strings.Index(string(imgOutput), "@"); idx != -1 {
			localDigest = strings.TrimSpace(string(imgOutput)[idx+1:])
		}
	}

	status.CurrentDigest = localDigest

	// 3. 解析镜像名获取 registry、repo、tag
	registry, repo, tag := parseImageName(status.Image)

	// 4. 获取远程 Digest
	remoteDigest, err := getRemoteDigest(registry, repo, tag)
	if err != nil {
		status.Error = fmt.Sprintf("获取远程镜像信息失败: %v", err)
		return status
	}

	status.LatestDigest = remoteDigest
	status.HasUpdate = localDigest != "" && remoteDigest != "" && localDigest != remoteDigest

	return status
}

// parseImageName 解析镜像名称为 registry、repo、tag
func parseImageName(image string) (registry, repo, tag string) {
	// 默认值
	registry = "registry-1.docker.io"
	tag = "latest"

	// 移除可能的 digest 后缀
	if idx := strings.Index(image, "@"); idx != -1 {
		image = image[:idx]
	}

	// 分离 tag
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		// 检查是否是端口号 (例如 localhost:5000/image)
		slashIdx := strings.LastIndex(image, "/")
		if idx > slashIdx {
			tag = image[idx+1:]
			image = image[:idx]
		}
	}

	// 判断是否包含 registry
	parts := strings.Split(image, "/")
	if len(parts) == 1 {
		// 例如 "nginx" -> "library/nginx"
		repo = "library/" + parts[0]
	} else if len(parts) == 2 {
		// 检查第一部分是否是 registry (包含 . 或 :)
		if strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") {
			registry = parts[0]
			repo = parts[1]
		} else {
			// 例如 "user/image" -> "user/image"
			repo = image
		}
	} else if len(parts) >= 3 {
		// 例如 "ghcr.io/user/image"
		registry = parts[0]
		repo = strings.Join(parts[1:], "/")
	}

	return registry, repo, tag
}

// getRemoteDigest 从 Registry 获取远程镜像的 Digest
// 参考 dockerCopilot 实现
func getRemoteDigest(registry, repo, tag string) (string, error) {
	// Docker Hub 加速器列表 (当直连失败时尝试)
	accelerators := []string{
		"registry-1.docker.io", // 原始地址优先
		"docker.m.daocloud.io",
		"docker.1panel.live",
		"hub.rat.dev",
	}

	// 非 Docker Hub 暂不支持
	if registry != "registry-1.docker.io" && registry != "docker.io" {
		return "", fmt.Errorf("暂不支持的 Registry: %s", registry)
	}

	var lastErr error
	for _, host := range accelerators {
		digest, err := tryGetDigestFromHost(host, repo, tag)
		if err == nil && digest != "" {
			return digest, nil
		}
		lastErr = err
		log.Printf("[Docker] 尝试 %s 失败: %v, 切换下一个", host, err)
	}

	return "", fmt.Errorf("所有镜像源均失败: %v", lastErr)
}

// tryGetDigestFromHost 从指定 host 获取 digest
func tryGetDigestFromHost(host, repo, tag string) (string, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}

	// 1. 先获取 challenge
	challengeURL := fmt.Sprintf("https://%s/v2/", host)
	challengeReq, _ := http.NewRequest("GET", challengeURL, nil)
	challengeResp, err := client.Do(challengeReq)
	if err != nil {
		return "", fmt.Errorf("challenge 请求失败: %v", err)
	}
	defer challengeResp.Body.Close()

	// 2. 解析 WWW-Authenticate header 获取 token URL
	wwwAuth := challengeResp.Header.Get("WWW-Authenticate")
	token := ""
	if strings.HasPrefix(strings.ToLower(wwwAuth), "bearer") {
		token, err = getBearerToken(wwwAuth, repo, client)
		if err != nil {
			return "", fmt.Errorf("获取 token 失败: %v", err)
		}
	}

	// 3. 使用 HEAD 请求获取 manifest digest
	manifestURL := fmt.Sprintf("https://%s/v2/%s/manifests/%s", host, repo, tag)
	req, _ := http.NewRequest("HEAD", manifestURL, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// 参考 dockerCopilot 的 Accept headers
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
	req.Header.Add("Accept", "application/vnd.docker.distribution.manifest.v1+json")
	req.Header.Add("Accept", "application/vnd.oci.image.index.v1+json")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("manifest 请求失败: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("registry 返回 %d", resp.StatusCode)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("响应中未包含 Docker-Content-Digest")
	}

	return digest, nil
}

// getBearerToken 从 WWW-Authenticate 解析并获取 bearer token
func getBearerToken(wwwAuth, repo string, client *http.Client) (string, error) {
	// 解析格式: Bearer realm="xxx",service="xxx",scope="xxx"
	raw := strings.TrimPrefix(strings.ToLower(wwwAuth), "bearer ")
	params := make(map[string]string)
	
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, "="); idx != -1 {
			key := strings.TrimSpace(part[:idx])
			val := strings.Trim(strings.TrimSpace(part[idx+1:]), `"`)
			params[key] = val
		}
	}

	realm := params["realm"]
	service := params["service"]
	if realm == "" {
		return "", fmt.Errorf("无法解析 realm")
	}

	// 构建 token URL
	tokenURL := fmt.Sprintf("%s?service=%s&scope=repository:%s:pull", realm, service, repo)
	
	resp, err := client.Get(tokenURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	return tokenResp.Token, nil
}

// ==================== Docker 镜像管理 ====================

// DockerImage 镜像信息
type DockerImage struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size"`
	Created    string `json:"created"`
}

// handleDockerImages 列出 Docker 镜像
func (a *AgentClient) handleDockerImages(data string) (string, error) {
	cmd := exec.Command("docker", "images", "--format", "{{.ID}}|{{.Repository}}|{{.Tag}}|{{.Size}}|{{.CreatedSince}}")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("获取镜像列表失败: %v", err)
	}

	var images []DockerImage
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 5)
		if len(parts) >= 5 {
			images = append(images, DockerImage{
				ID:         parts[0],
				Repository: parts[1],
				Tag:        parts[2],
				Size:       parts[3],
				Created:    parts[4],
			})
		}
	}

	jsonResult, _ := json.Marshal(images)
	return string(jsonResult), nil
}

// DockerImageActionRequest 镜像操作请求
type DockerImageActionRequest struct {
	Action string `json:"action"` // pull, remove, prune
	Image  string `json:"image"`  // 镜像名 (pull/remove 时使用)
}

// handleDockerImageAction 镜像操作
func (a *AgentClient) handleDockerImageAction(data string) (string, error) {
	var req DockerImageActionRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	var cmd *exec.Cmd
	var actionDesc string

	switch req.Action {
	case "pull":
		if req.Image == "" {
			return "", fmt.Errorf("缺少镜像名")
		}
		cmd = exec.Command("docker", "pull", req.Image)
		actionDesc = "拉取镜像"
	case "remove":
		if req.Image == "" {
			return "", fmt.Errorf("缺少镜像 ID")
		}
		cmd = exec.Command("docker", "rmi", req.Image)
		actionDesc = "删除镜像"
	case "prune":
		cmd = exec.Command("docker", "image", "prune", "-f")
		actionDesc = "清理未使用镜像"
	default:
		return "", fmt.Errorf("不支持的操作: %s", req.Action)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s失败: %s", actionDesc, string(output))
	}

	return fmt.Sprintf("%s成功\n%s", actionDesc, string(output)), nil
}

// ==================== Docker 网络管理 ====================

// DockerNetwork 网络信息
type DockerNetwork struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Driver  string `json:"driver"`
	Scope   string `json:"scope"`
	Subnet  string `json:"subnet"`
	Gateway string `json:"gateway"`
}

// handleDockerNetworks 列出 Docker 网络
func (a *AgentClient) handleDockerNetworks(data string) (string, error) {
	cmd := exec.Command("docker", "network", "ls", "--format", "{{.ID}}|{{.Name}}|{{.Driver}}|{{.Scope}}")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("获取网络列表失败: %v", err)
	}

	var networks []DockerNetwork
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 4)
		if len(parts) >= 4 {
			network := DockerNetwork{
				ID:     parts[0],
				Name:   parts[1],
				Driver: parts[2],
				Scope:  parts[3],
			}

			// 获取网络详情 (子网和网关)
			inspectCmd := exec.Command("docker", "network", "inspect", parts[0], "--format", "{{range .IPAM.Config}}{{.Subnet}}|{{.Gateway}}{{end}}")
			inspectOut, _ := inspectCmd.Output()
			if inspectParts := strings.SplitN(strings.TrimSpace(string(inspectOut)), "|", 2); len(inspectParts) >= 2 {
				network.Subnet = inspectParts[0]
				network.Gateway = inspectParts[1]
			}

			networks = append(networks, network)
		}
	}

	jsonResult, _ := json.Marshal(networks)
	return string(jsonResult), nil
}

// DockerNetworkActionRequest 网络操作请求
type DockerNetworkActionRequest struct {
	Action  string `json:"action"`  // create, remove, connect, disconnect
	Name    string `json:"name"`    // 网络名
	Driver  string `json:"driver"`  // 驱动 (bridge, host, overlay)
	Subnet  string `json:"subnet"`  // 子网 (可选)
	Gateway string `json:"gateway"` // 网关 (可选)
	Container string `json:"container"` // 容器 ID (connect/disconnect 时使用)
}

// handleDockerNetworkAction 网络操作
func (a *AgentClient) handleDockerNetworkAction(data string) (string, error) {
	var req DockerNetworkActionRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	var cmd *exec.Cmd
	var actionDesc string

	switch req.Action {
	case "create":
		if req.Name == "" {
			return "", fmt.Errorf("缺少网络名")
		}
		args := []string{"network", "create"}
		if req.Driver != "" {
			args = append(args, "--driver", req.Driver)
		}
		if req.Subnet != "" {
			args = append(args, "--subnet", req.Subnet)
		}
		if req.Gateway != "" {
			args = append(args, "--gateway", req.Gateway)
		}
		args = append(args, req.Name)
		cmd = exec.Command("docker", args...)
		actionDesc = "创建网络"
	case "remove":
		if req.Name == "" {
			return "", fmt.Errorf("缺少网络名")
		}
		cmd = exec.Command("docker", "network", "rm", req.Name)
		actionDesc = "删除网络"
	case "connect":
		if req.Name == "" || req.Container == "" {
			return "", fmt.Errorf("缺少网络名或容器 ID")
		}
		cmd = exec.Command("docker", "network", "connect", req.Name, req.Container)
		actionDesc = "连接容器到网络"
	case "disconnect":
		if req.Name == "" || req.Container == "" {
			return "", fmt.Errorf("缺少网络名或容器 ID")
		}
		cmd = exec.Command("docker", "network", "disconnect", req.Name, req.Container)
		actionDesc = "断开容器与网络"
	default:
		return "", fmt.Errorf("不支持的操作: %s", req.Action)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s失败: %s", actionDesc, string(output))
	}

	return fmt.Sprintf("%s成功", actionDesc), nil
}

// ==================== Docker Volume 管理 ====================

// DockerVolume Volume 信息
type DockerVolume struct {
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Mountpoint string `json:"mountpoint"`
	Size       string `json:"size"`
}

// handleDockerVolumes 列出 Docker Volumes
func (a *AgentClient) handleDockerVolumes(data string) (string, error) {
	cmd := exec.Command("docker", "volume", "ls", "--format", "{{.Name}}|{{.Driver}}|{{.Mountpoint}}")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("获取 Volume 列表失败: %v", err)
	}

	var volumes []DockerVolume
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) >= 3 {
			volumes = append(volumes, DockerVolume{
				Name:       parts[0],
				Driver:     parts[1],
				Mountpoint: parts[2],
			})
		}
	}

	jsonResult, _ := json.Marshal(volumes)
	return string(jsonResult), nil
}

// DockerVolumeActionRequest Volume 操作请求
type DockerVolumeActionRequest struct {
	Action string `json:"action"` // create, remove, prune
	Name   string `json:"name"`   // Volume 名
	Driver string `json:"driver"` // 驱动 (可选)
}

// handleDockerVolumeAction Volume 操作
func (a *AgentClient) handleDockerVolumeAction(data string) (string, error) {
	var req DockerVolumeActionRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	var cmd *exec.Cmd
	var actionDesc string

	switch req.Action {
	case "create":
		if req.Name == "" {
			return "", fmt.Errorf("缺少 Volume 名")
		}
		args := []string{"volume", "create"}
		if req.Driver != "" {
			args = append(args, "--driver", req.Driver)
		}
		args = append(args, req.Name)
		cmd = exec.Command("docker", args...)
		actionDesc = "创建 Volume"
	case "remove":
		if req.Name == "" {
			return "", fmt.Errorf("缺少 Volume 名")
		}
		cmd = exec.Command("docker", "volume", "rm", req.Name)
		actionDesc = "删除 Volume"
	case "prune":
		cmd = exec.Command("docker", "volume", "prune", "-f")
		actionDesc = "清理未使用 Volume"
	default:
		return "", fmt.Errorf("不支持的操作: %s", req.Action)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s失败: %s", actionDesc, string(output))
	}

	return fmt.Sprintf("%s成功\n%s", actionDesc, string(output)), nil
}

// ==================== Docker 日志 ====================

// DockerLogsRequest 日志请求
type DockerLogsRequest struct {
	ContainerID string `json:"container_id"`
	Tail        int    `json:"tail"` // 获取最后 N 行, 默认 100
	Since       string `json:"since"` // 时间过滤, 如 "1h", "30m"
}

// handleDockerLogs 获取容器日志
func (a *AgentClient) handleDockerLogs(data string) (string, error) {
	var req DockerLogsRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.ContainerID == "" {
		return "", fmt.Errorf("缺少容器 ID")
	}

	args := []string{"logs"}
	if req.Tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", req.Tail))
	} else {
		args = append(args, "--tail", "100") // 默认 100 行
	}
	if req.Since != "" {
		args = append(args, "--since", req.Since)
	}
	args = append(args, req.ContainerID)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("获取日志失败: %s", string(output))
	}

	return string(output), nil
}

// ==================== Docker 资源统计 ====================

// DockerContainerStats 容器资源统计
type DockerContainerStats struct {
	ContainerID string  `json:"container_id"`
	Name        string  `json:"name"`
	CPUPercent  string  `json:"cpu_percent"`
	MemUsage    string  `json:"mem_usage"`
	MemPercent  string  `json:"mem_percent"`
	NetIO       string  `json:"net_io"`
	BlockIO     string  `json:"block_io"`
}

// handleDockerStats 获取容器资源统计
func (a *AgentClient) handleDockerStats(data string) (string, error) {
	// 获取所有运行中容器的资源统计 (非阻塞模式)
	cmd := exec.Command("docker", "stats", "--no-stream", "--format",
		"{{.ID}}|{{.Name}}|{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}|{{.NetIO}}|{{.BlockIO}}")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("获取资源统计失败: %v", err)
	}

	var stats []DockerContainerStats
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 7)
		if len(parts) >= 7 {
			stats = append(stats, DockerContainerStats{
				ContainerID: parts[0],
				Name:        parts[1],
				CPUPercent:  parts[2],
				MemUsage:    parts[3],
				MemPercent:  parts[4],
				NetIO:       parts[5],
				BlockIO:     parts[6],
			})
		}
	}

	jsonResult, _ := json.Marshal(stats)
	return string(jsonResult), nil
}

// ==================== Docker Compose 管理 ====================

// DockerComposeProject Compose 项目信息
type DockerComposeProject struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	ConfigFile string `json:"config_file"`
}

// handleDockerComposeList 列出 Docker Compose 项目
func (a *AgentClient) handleDockerComposeList(data string) (string, error) {
	// 使用 docker compose ls 命令列出所有项目
	cmd := exec.Command("docker", "compose", "ls", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		// 尝试使用 docker-compose (旧版)
		cmd = exec.Command("docker-compose", "ls", "--format", "json")
		output, err = cmd.Output()
		if err != nil {
			return "[]", nil // 没有 compose 项目或命令不可用
		}
	}

	// 直接返回 JSON 格式输出
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return "[]", nil
	}

	return trimmed, nil
}

// DockerComposeActionRequest Compose 操作请求
type DockerComposeActionRequest struct {
	Action    string `json:"action"`     // up, down, restart, pull
	Project   string `json:"project"`    // 项目名称
	ConfigDir string `json:"config_dir"` // compose 文件所在目录
}

// handleDockerComposeAction Compose 操作
func (a *AgentClient) handleDockerComposeAction(data string) (string, error) {
	var req DockerComposeActionRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.Project == "" {
		return "", fmt.Errorf("缺少项目名称")
	}

	var args []string
	var actionDesc string

	switch req.Action {
	case "up":
		args = []string{"compose", "-p", req.Project, "up", "-d"}
		actionDesc = "启动项目"
	case "down":
		args = []string{"compose", "-p", req.Project, "down"}
		actionDesc = "停止项目"
	case "restart":
		args = []string{"compose", "-p", req.Project, "restart"}
		actionDesc = "重启项目"
	case "pull":
		args = []string{"compose", "-p", req.Project, "pull"}
		actionDesc = "更新镜像"
	default:
		return "", fmt.Errorf("不支持的操作: %s", req.Action)
	}

	cmd := exec.Command("docker", args...)
	if req.ConfigDir != "" {
		cmd.Dir = req.ConfigDir
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s失败: %s", actionDesc, string(output))
	}

	return fmt.Sprintf("%s成功\n%s", actionDesc, string(output)), nil
}

// ==================== Docker 容器创建 ====================

// DockerCreateContainerRequest 创建容器请求
type DockerCreateContainerRequest struct {
	Name        string            `json:"name"`        // 容器名称
	Image       string            `json:"image"`       // 镜像名称
	Ports       []string          `json:"ports"`       // 端口映射，如 ["8080:80", "443:443"]
	Volumes     []string          `json:"volumes"`     // 卷映射，如 ["/host/path:/container/path"]
	Env         map[string]string `json:"env"`         // 环境变量
	Network     string            `json:"network"`     // 网络名称
	Restart     string            `json:"restart"`     // 重启策略: no, always, unless-stopped, on-failure
	Privileged  bool              `json:"privileged"`  // 特权模式
	ExtraArgs   []string          `json:"extra_args"`  // 额外的 docker run 参数
}

// handleDockerCreateContainer 创建新容器
func (a *AgentClient) handleDockerCreateContainer(data string) (string, error) {
	var req DockerCreateContainerRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.Image == "" {
		return "", fmt.Errorf("缺少镜像名称")
	}

	// 构建 docker run 命令参数
	args := []string{"run", "-d"}

	// 容器名称
	if req.Name != "" {
		args = append(args, "--name", req.Name)
	}

	// 端口映射
	for _, port := range req.Ports {
		args = append(args, "-p", port)
	}

	// 卷映射
	for _, vol := range req.Volumes {
		args = append(args, "-v", vol)
	}

	// 环境变量
	for k, v := range req.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// 网络
	if req.Network != "" {
		args = append(args, "--network", req.Network)
	}

	// 重启策略
	if req.Restart != "" {
		args = append(args, "--restart", req.Restart)
	}

	// 特权模式
	if req.Privileged {
		args = append(args, "--privileged")
	}

	// 额外参数
	args = append(args, req.ExtraArgs...)

	// 最后添加镜像名
	args = append(args, req.Image)

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("创建容器失败: %s", string(output))
	}

	containerId := strings.TrimSpace(string(output))
	return fmt.Sprintf("容器创建成功\nID: %s", containerId), nil
}

// handleUpgrade 执行 Agent 自我升级
func (a *AgentClient) handleUpgrade(taskId string) {
	// 稍微延迟，确保 Ack 消息先发送出去
	time.Sleep(1 * time.Second)

	log.Printf("[Upgrade] 开始执行升级流程...")

	var cmd *exec.Cmd

	if runtime.GOOS == "windows" {
		// Windows: 使用 PowerShell 下载并执行脚本
		installUrl := fmt.Sprintf("%s/api/server/agent/install/win/%s", a.config.ServerURL, a.config.ServerID)
		psCommand := fmt.Sprintf("irm %s | iex", installUrl)
		
		// 使用 Start-Process 启动一个独立的 PowerShell 窗口执行升级，确保不会因为 Agent 停止而被杀掉
		// 注意：服务中运行已经有 System 权限，不需要 (也不能) 使用 RunAs，否则 Session 0 会失败
		cmd = exec.Command("powershell", "-Command", "Start-Process", "powershell", "-ArgumentList", fmt.Sprintf("'-NoProfile -ExecutionPolicy Bypass -Command \"%s\"'", psCommand), "-WindowStyle", "Hidden")
	} else {
		// Linux/MacOS: 使用 curl | bash
		installUrl := fmt.Sprintf("%s/api/server/agent/install/linux/%s", a.config.ServerURL, a.config.ServerID)
		shellCommand := fmt.Sprintf("curl -fsSL %s | sudo bash", installUrl)
		
		// 使用 nohup 后台执行
		cmd = exec.Command("sh", "-c", fmt.Sprintf("nohup sh -c '%s' > /tmp/agent_upgrade.log 2>&1 &", shellCommand))
	}

	if err := cmd.Start(); err != nil {
		log.Printf("[Upgrade] 启动升级进程失败: %v", err)
	} else {
		log.Printf("[Upgrade] 升级进程已启动，Agent 即将重启...")
	}
}

// handlePTYTask 处理 PTY 任务
func (a *AgentClient) handlePTYTask(taskId string, data string) {
	log.Printf("[Agent] 启动 PTY 会话: %s", taskId)

	// 解析初始尺寸
	var resize PTYResizeData
	if err := json.Unmarshal([]byte(data), &resize); err != nil {
		resize.Cols = 80
		resize.Rows = 24
	}
	if resize.Cols == 0 {
		resize.Cols = 80
	}
	if resize.Rows == 0 {
		resize.Rows = 24
	}

	// 启动 PTY
	pty, err := StartPTY(resize.Cols, resize.Rows)
	if err != nil {
		log.Printf("[Agent] 启动 PTY 失败: %v", err)
		return
	}

	// 注册会话
	a.mu.Lock()
	a.ptySessions[taskId] = pty
	a.mu.Unlock()

	// 清理函数
	defer func() {
		a.mu.Lock()
		delete(a.ptySessions, taskId)
		a.mu.Unlock()
		pty.Close()
		log.Printf("[Agent] PTY 会话已关闭: %s", taskId)
	}()

	// 读取 PTY 输出并发送到服务器
	buf := make([]byte, 8192)
	for {
		n, err := pty.Read(buf)
		if n > 0 {
			if a.config.Debug {
				log.Printf("[Agent] PTY 读取到数据: %d 字节", n)
			}
			// 发送实时数据
			a.emit(EventAgentPtyData, map[string]interface{}{
				"id":   taskId,
				"data": string(buf[:n]),
			})
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("[Agent] PTY 读取错误: %v", err)
			}
			break
		}
	}
}

// Stop 停止 Agent
func (a *AgentClient) Stop() {
	close(a.stopChan)

	a.mu.Lock()
	if a.conn != nil {
		a.conn.Close()
	}
	// 关闭并清理所有 PTY 会话
	for id, pty := range a.ptySessions {
		pty.Close()
		delete(a.ptySessions, id)
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
	background := flag.Bool("b", false, "后台模式 (隐藏控制台窗口)")
	flag.Parse()

	// 初始化日志文件 (无论是否后台模式)
	exePath, _ := os.Executable()
	logPath := filepath.Join(filepath.Dir(exePath), "agent.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		// 同时输出到文件和控制台 (如果是服务模式，控制台不可见，但这没关系)
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
		log.Println("==================================================")
		log.Printf("[Agent] 启动时间: %s", time.Now().Format(time.RFC3339))
	} else {
		fmt.Printf("无法创建日志文件: %v\n", err)
	}

	// 后台模式：隐藏控制台窗口
	if *background {
		HideConsoleWindow()
	}

	// 加载配置
	config := &Config{
		ServerURL:        "http://localhost:3000",
		ReportInterval:   1500,
		HostInfoInterval: 600000,
		ReconnectDelay:   4000,
	}

	// 从配置文件加载（使用可执行文件所在目录）
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
	fmt.Println("  -b          后台模式 (隐藏控制台窗口, Windows)")
	fmt.Println()
	fmt.Println("配置文件:")
	fmt.Println("  将 config.json 放在程序同目录下")
	fmt.Println()
	fmt.Println("示例:")
	fmt.Println("  api-monitor-agent install           # 安装为 Windows 服务 (推荐)")
	fmt.Println("  api-monitor-agent start             # 启动服务")
	fmt.Println("  api-monitor-agent -b                # 后台模式运行 (隐藏窗口)")
	fmt.Println("  api-monitor-agent -s https://xxx -id abc -k key123")
}

// ==================== 容器一键更新与进度跟踪 ====================

// DockerContainerUpdateRequest 容器更新请求
type DockerContainerUpdateRequest struct {
	ContainerID   string `json:"container_id"`
	ContainerName string `json:"container_name"`
	Image         string `json:"image"` // 新镜像 (可选，不填则用原镜像)
}

// DockerRenameRequest 容器重命名请求
type DockerRenameRequest struct {
	ContainerID string `json:"container_id"`
	NewName     string `json:"new_name"`
}

// updateProgress 更新任务进度
func (a *AgentClient) updateProgress(taskID string, progress *TaskProgress) {
	a.progressMu.Lock()
	a.taskProgress[taskID] = progress
	a.progressMu.Unlock()

	// 通过 WebSocket 发送进度事件
	a.emit("agent:task_progress", progress)
}

// getTaskProgress 获取任务进度
func (a *AgentClient) getTaskProgress(data string) (string, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", err
	}

	a.progressMu.RLock()
	progress, exists := a.taskProgress[req.TaskID]
	a.progressMu.RUnlock()

	if !exists {
		return "", fmt.Errorf("任务不存在: %s", req.TaskID)
	}

	result, _ := json.Marshal(progress)
	return string(result), nil
}

// handleDockerRenameContainer 处理容器重命名
func (a *AgentClient) handleDockerRenameContainer(data string) (string, error) {
	var req DockerRenameRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		return "", fmt.Errorf("解析请求失败: %v", err)
	}

	if req.ContainerID == "" || req.NewName == "" {
		return "", fmt.Errorf("缺少必要参数")
	}

	cmd := exec.Command("docker", "rename", req.ContainerID, req.NewName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("重命名失败: %s - %v", string(output), err)
	}

	return fmt.Sprintf("容器已重命名为: %s", req.NewName), nil
}

// handleDockerContainerUpdate 处理容器一键更新 (异步)
func (a *AgentClient) handleDockerContainerUpdate(taskID string, data string) {
	var req DockerContainerUpdateRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		a.sendTaskError(taskID, "解析请求失败: "+err.Error())
		return
	}

	progress := &TaskProgress{
		TaskID:     taskID,
		Name:       "更新容器: " + req.ContainerName,
		Percentage: 0,
		Message:    "正在准备...",
	}
	a.updateProgress(taskID, progress)

	// 1. 获取容器当前配置
	progress.Percentage = 5
	progress.Message = "获取容器配置..."
	a.updateProgress(taskID, progress)

	inspectCmd := exec.Command("docker", "inspect", "--format", "{{json .}}", req.ContainerID)
	inspectOutput, err := inspectCmd.Output()
	if err != nil {
		a.finishWithError(taskID, progress, "获取容器配置失败: "+err.Error())
		return
	}

	var containerInfo map[string]interface{}
	if err := json.Unmarshal(inspectOutput, &containerInfo); err != nil {
		a.finishWithError(taskID, progress, "解析容器配置失败: "+err.Error())
		return
	}

	// 获取镜像名
	imageName := req.Image
	if imageName == "" {
		if config, ok := containerInfo["Config"].(map[string]interface{}); ok {
			if img, ok := config["Image"].(string); ok {
				imageName = img
			}
		}
	}
	if imageName == "" {
		a.finishWithError(taskID, progress, "无法确定镜像名称")
		return
	}

	// 2. 拉取新镜像
	progress.Percentage = 10
	progress.Message = "正在拉取镜像: " + imageName
	a.updateProgress(taskID, progress)

	pullCmd := exec.Command("docker", "pull", imageName)
	pullOutput, err := pullCmd.CombinedOutput()
	if err != nil {
		a.finishWithError(taskID, progress, "拉取镜像失败: "+string(pullOutput))
		return
	}

	progress.Percentage = 40
	progress.Message = "镜像拉取完成"
	progress.DetailMsg = string(pullOutput)
	a.updateProgress(taskID, progress)

	// 3. 停止旧容器
	progress.Percentage = 50
	progress.Message = "正在停止容器..."
	a.updateProgress(taskID, progress)

	stopCmd := exec.Command("docker", "stop", req.ContainerID)
	if _, err := stopCmd.CombinedOutput(); err != nil {
		a.finishWithError(taskID, progress, "停止容器失败: "+err.Error())
		return
	}

	// 4. 重命名旧容器
	progress.Percentage = 60
	progress.Message = "正在备份旧容器..."
	a.updateProgress(taskID, progress)

	backupName := req.ContainerName + "-backup-" + time.Now().Format("20060102-150405")
	renameCmd := exec.Command("docker", "rename", req.ContainerID, backupName)
	if _, err := renameCmd.CombinedOutput(); err != nil {
		a.finishWithError(taskID, progress, "备份容器失败: "+err.Error())
		return
	}

	// 5. 使用 docker run 创建新容器 (简化版，复用旧配置)
	progress.Percentage = 70
	progress.Message = "正在创建新容器..."
	a.updateProgress(taskID, progress)

	// 构建 docker run 命令
	runArgs := a.buildDockerRunArgs(containerInfo, imageName, req.ContainerName)
	runCmd := exec.Command("docker", runArgs...)
	runOutput, err := runCmd.CombinedOutput()
	if err != nil {
		// 创建失败，恢复旧容器
		exec.Command("docker", "rename", backupName, req.ContainerName).Run()
		exec.Command("docker", "start", req.ContainerName).Run()
		a.finishWithError(taskID, progress, "创建新容器失败: "+string(runOutput))
		return
	}

	// 6. 删除旧容器
	progress.Percentage = 90
	progress.Message = "正在清理旧容器..."
	a.updateProgress(taskID, progress)

	exec.Command("docker", "rm", backupName).Run()

	// 完成
	progress.Percentage = 100
	progress.Message = "更新完成"
	progress.DetailMsg = "容器已成功更新到最新版本"
	progress.IsDone = true
	a.updateProgress(taskID, progress)

	// 发送最终结果
	a.emit(EventAgentTaskResult, map[string]interface{}{
		"id":         taskID,
		"successful": true,
		"data":       "容器更新完成",
	})
}

// buildDockerRunArgs 从容器配置构建 docker run 参数
func (a *AgentClient) buildDockerRunArgs(containerInfo map[string]interface{}, imageName, containerName string) []string {
	args := []string{"run", "-d", "--name", containerName}

	// 获取 HostConfig
	hostConfig, _ := containerInfo["HostConfig"].(map[string]interface{})
	config, _ := containerInfo["Config"].(map[string]interface{})

	// 端口映射
	if portBindings, ok := hostConfig["PortBindings"].(map[string]interface{}); ok {
		for containerPort, bindings := range portBindings {
			if bindList, ok := bindings.([]interface{}); ok && len(bindList) > 0 {
				if bind, ok := bindList[0].(map[string]interface{}); ok {
					hostPort := bind["HostPort"].(string)
					args = append(args, "-p", hostPort+":"+strings.Split(containerPort, "/")[0])
				}
			}
		}
	}

	// 卷挂载
	if mounts, ok := containerInfo["Mounts"].([]interface{}); ok {
		for _, m := range mounts {
			if mount, ok := m.(map[string]interface{}); ok {
				source := mount["Source"].(string)
				dest := mount["Destination"].(string)
				args = append(args, "-v", source+":"+dest)
			}
		}
	}

	// 环境变量
	if env, ok := config["Env"].([]interface{}); ok {
		for _, e := range env {
			if envStr, ok := e.(string); ok {
				// 过滤掉一些自动生成的环境变量
				if !strings.HasPrefix(envStr, "PATH=") && !strings.HasPrefix(envStr, "HOME=") {
					args = append(args, "-e", envStr)
				}
			}
		}
	}

	// 网络模式
	if networkMode, ok := hostConfig["NetworkMode"].(string); ok && networkMode != "default" && networkMode != "bridge" {
		args = append(args, "--network", networkMode)
	}

	// 重启策略
	if restartPolicy, ok := hostConfig["RestartPolicy"].(map[string]interface{}); ok {
		if name, ok := restartPolicy["Name"].(string); ok && name != "" && name != "no" {
			args = append(args, "--restart", name)
		}
	}

	// 特权模式
	if privileged, ok := hostConfig["Privileged"].(bool); ok && privileged {
		args = append(args, "--privileged")
	}

	args = append(args, imageName)
	return args
}

// finishWithError 完成任务并标记错误
func (a *AgentClient) finishWithError(taskID string, progress *TaskProgress, errMsg string) {
	progress.Message = "失败: " + errMsg
	progress.DetailMsg = errMsg
	progress.IsDone = true
	progress.IsError = true
	a.updateProgress(taskID, progress)

	a.emit(EventAgentTaskResult, map[string]interface{}{
		"id":         taskID,
		"successful": false,
		"data":       errMsg,
	})
}

// sendTaskError 发送任务错误
func (a *AgentClient) sendTaskError(taskID string, errMsg string) {
	a.emit(EventAgentTaskResult, map[string]interface{}{
		"id":         taskID,
		"successful": false,
		"data":       errMsg,
	})
}
