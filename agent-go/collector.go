package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// HostInfo 主机静态信息
type HostInfo struct {
	Platform        string   `json:"platform"`
	PlatformVersion string   `json:"platform_version"`
	CPU             []string `json:"cpu"`
	Cores           int      `json:"cores"`
	GPU             []string `json:"gpu"`
	GPUMemTotal     uint64   `json:"gpu_mem_total"`
	MemTotal        uint64   `json:"mem_total"`
	DiskTotal       uint64   `json:"disk_total"`
	SwapTotal       uint64   `json:"swap_total"`
	Arch            string   `json:"arch"`
	Virtualization  string   `json:"virtualization"`
	BootTime        int64    `json:"boot_time"`
	IP              string   `json:"ip"`
	CountryCode     string   `json:"country_code"`
	AgentVersion    string   `json:"agent_version"`
}

// DockerContainer 容器信息
type DockerContainer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	Status  string `json:"status"`
	Created string `json:"created"`
}

// DockerInfo Docker 信息
type DockerInfo struct {
	Installed  bool              `json:"installed"`
	Running    int               `json:"running"`
	Stopped    int               `json:"stopped"`
	Containers []DockerContainer `json:"containers"`
}

// State 实时状态
type State struct {
	CPU            float64    `json:"cpu"`
	MemUsed        uint64     `json:"mem_used"`
	SwapUsed       uint64     `json:"swap_used"`
	DiskUsed       uint64     `json:"disk_used"`
	NetInTransfer  uint64     `json:"net_in_transfer"`
	NetOutTransfer uint64     `json:"net_out_transfer"`
	NetInSpeed     uint64     `json:"net_in_speed"`
	NetOutSpeed    uint64     `json:"net_out_speed"`
	Uptime         uint64     `json:"uptime"`
	Load1          float64    `json:"load1"`
	Load5          float64    `json:"load5"`
	Load15         float64    `json:"load15"`
	TcpConnCount   int        `json:"tcp_conn_count"`
	UdpConnCount   int        `json:"udp_conn_count"`
	ProcessCount   int        `json:"process_count"`
	Temperatures   []string   `json:"temperatures"`
	GPU            float64    `json:"gpu"`
	GPUMemUsed     uint64     `json:"gpu_mem_used"`
	GPUMemTotal    uint64     `json:"gpu_mem_total"`
	GPUPower       float64    `json:"gpu_power"`
	Docker         DockerInfo `json:"docker"`
}

// Collector 数据采集器
type Collector struct {
	mu             sync.Mutex
	cachedHostInfo *HostInfo
	cachedDiskUsed uint64

	// 网络流量缓存
	lastNetRx   uint64
	lastNetTx   uint64
	lastNetTime time.Time

	// GPU 采集缓存 (节流: 每5秒采集一次)
	lastGPUUsage   float64
	lastGPUMemUsed uint64
	lastGPUPower   float64
	lastGPUTime    time.Time

	// CPU 采集缓存 (保持上次有效值，避免返回 0)
	lastCPUUsage float64
	lastCPUTime  time.Time
}

// NewCollector 创建采集器
func NewCollector() *Collector {
	return &Collector{
		lastNetTime: time.Now(),
		lastGPUTime: time.Now().Add(-1 * time.Hour), // 确保第一次采集立即执行
		lastCPUTime: time.Now().Add(-1 * time.Hour), // 确保第一次采集立即执行
	}
}

// CollectHostInfo 采集主机静态信息 (变化慢，10分钟采集一次)
func (c *Collector) CollectHostInfo() *HostInfo {
	c.mu.Lock()
	defer c.mu.Unlock()

	info := &HostInfo{
		Platform:     runtime.GOOS,
		Arch:         runtime.GOARCH,
		AgentVersion: VERSION,
	}

	// 平台信息
	if hostInfo, err := host.Info(); err == nil {
		info.Platform = hostInfo.Platform
		info.PlatformVersion = fmt.Sprintf("%s %s", hostInfo.PlatformFamily, hostInfo.PlatformVersion)
		info.BootTime = int64(hostInfo.BootTime)
		info.Virtualization = hostInfo.VirtualizationSystem
	}

	// CPU 信息
	logicalCores, _ := cpu.Counts(true)
	if logicalCores == 0 {
		logicalCores = runtime.NumCPU()
	}

	if cpuInfo, err := cpu.Info(); err == nil && len(cpuInfo) > 0 {
		cpuDesc := fmt.Sprintf("%s %s %d Core(s)", cpuInfo[0].VendorID, cpuInfo[0].ModelName, logicalCores)
		info.CPU = []string{strings.TrimSpace(cpuDesc)}
	} else {
		// Fallback for Windows (using PowerShell since wmic might be missing)
		cpuName := ""
		if runtime.GOOS == "windows" {
			// Get-CimInstance Win32_Processor | Select-Object -ExpandProperty Name
			cmd := exec.Command("powershell", "-NoProfile", "-Command", "Get-CimInstance Win32_Processor | Select-Object -ExpandProperty Name")
			hideWindow(cmd)
			if out, err := cmd.Output(); err == nil {
				cpuName = strings.TrimSpace(string(out))
			}
		}

		if cpuName != "" {
			info.CPU = []string{fmt.Sprintf("%s %d Core(s)", cpuName, logicalCores)}
		} else {
			info.CPU = []string{fmt.Sprintf("Unknown CPU %d Core(s)", logicalCores)}
		}
	}
	info.Cores = logicalCores
	fmt.Printf("[Collector] Detected %d cores, Platform: %s\n", logicalCores, info.Platform)

	// 内存信息
	if memInfo, err := mem.VirtualMemory(); err == nil {
		info.MemTotal = memInfo.Total
	}

	// Swap 信息
	if swapInfo, err := mem.SwapMemory(); err == nil {
		info.SwapTotal = swapInfo.Total
	}

	// 磁盘信息
	if partitions, err := disk.Partitions(false); err == nil {
		var totalSize uint64
		for _, p := range partitions {
			if usage, err := disk.Usage(p.Mountpoint); err == nil {
				totalSize += usage.Total
			}
		}
		info.DiskTotal = totalSize
	}

	// 公网 IP
	info.IP = getPublicIP()

	// GPU
	gpuModels, gpuMemTotal := c.collectGPUMetadata()
	info.GPU = gpuModels
	info.GPUMemTotal = gpuMemTotal

	c.cachedHostInfo = info
	return info
}

// CollectState 采集实时状态 (变化快，1-2秒采集一次)
func (c *Collector) CollectState() *State {
	state := &State{
		Temperatures: []string{},
	}

	// CPU 使用率 (带缓存：如果本次采集返回 0 且距上次采集不足 500ms，使用缓存值)
	if cpuPercent, err := cpu.Percent(0, false); err == nil && len(cpuPercent) > 0 {
		currentCPU := cpuPercent[0]
		now := time.Now()
		
		// 如果返回 0 但距上次有效采集不足 3 秒，使用缓存值
		if currentCPU < 0.1 && time.Since(c.lastCPUTime) < 3*time.Second && c.lastCPUUsage > 0 {
			state.CPU = c.lastCPUUsage
		} else {
			state.CPU = currentCPU
			// 只有非零值才更新缓存
			if currentCPU >= 0.1 {
				c.mu.Lock()
				c.lastCPUUsage = currentCPU
				c.lastCPUTime = now
				c.mu.Unlock()
			}
		}
	} else if c.lastCPUUsage > 0 {
		// 采集失败时使用缓存值
		state.CPU = c.lastCPUUsage
	}

	// 内存
	if memInfo, err := mem.VirtualMemory(); err == nil {
		state.MemUsed = memInfo.Used
	}

	// Swap
	if swapInfo, err := mem.SwapMemory(); err == nil {
		state.SwapUsed = swapInfo.Used
	}

	// 磁盘使用 (异步更新缓存)
	go func() {
		if partitions, err := disk.Partitions(false); err == nil {
			var usedSize uint64
			for _, p := range partitions {
				if usage, err := disk.Usage(p.Mountpoint); err == nil {
					usedSize += usage.Used
				}
			}
			c.mu.Lock()
			c.cachedDiskUsed = usedSize
			c.mu.Unlock()
		}
	}()
	c.mu.Lock()
	state.DiskUsed = c.cachedDiskUsed
	c.mu.Unlock()

	// 网络流量
	if netIO, err := net.IOCounters(false); err == nil && len(netIO) > 0 {
		state.NetInTransfer = netIO[0].BytesRecv
		state.NetOutTransfer = netIO[0].BytesSent

		// 计算速度
		c.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(c.lastNetTime).Seconds()
		if elapsed > 0 && c.lastNetTime.Unix() > 0 {
			if netIO[0].BytesRecv >= c.lastNetRx {
				state.NetInSpeed = uint64(float64(netIO[0].BytesRecv-c.lastNetRx) / elapsed)
			}
			if netIO[0].BytesSent >= c.lastNetTx {
				state.NetOutSpeed = uint64(float64(netIO[0].BytesSent-c.lastNetTx) / elapsed)
			}
		}
		c.lastNetRx = netIO[0].BytesRecv
		c.lastNetTx = netIO[0].BytesSent
		c.lastNetTime = now
		c.mu.Unlock()
	}

	// 运行时长
	if hostInfo, err := host.Info(); err == nil {
		state.Uptime = hostInfo.Uptime
	}

	// 负载 (Windows 不支持，使用 CPU 模拟)
	if runtime.GOOS != "windows" {
		if loadAvg, err := load.Avg(); err == nil {
			state.Load1 = loadAvg.Load1
			state.Load5 = loadAvg.Load5
			state.Load15 = loadAvg.Load15
		}
	} else {
		// Windows: 使用 CPU 使用率模拟
		cpuCount := float64(runtime.NumCPU())
		state.Load1 = state.CPU / 100 * cpuCount
		state.Load5 = state.Load1
		state.Load15 = state.Load1
	}

	// TCP/UDP 连接数
	if conns, err := net.Connections("all"); err == nil {
		for _, conn := range conns {
			switch conn.Type {
			case 1: // TCP
				state.TcpConnCount++
			case 2: // UDP
				state.UdpConnCount++
			}
		}
	}

	// Docker 信息采集
	state.Docker = c.collectDockerInfo()
	
	// GPU 使用率、显存与功耗采集 (节流: 每5秒实际采集一次，但如果缓存为0则立即重采)
	shouldCollectGPU := time.Since(c.lastGPUTime) > 5*time.Second || 
		(c.lastGPUUsage < 0.1 && c.lastGPUMemUsed == 0 && time.Since(c.lastGPUTime) > 1*time.Second)
	
	if shouldCollectGPU {
		gpuUsage, gpuMemUsed, gpuPower := c.collectGPUState()
		// 只有采集到有效数据才更新缓存
		if gpuUsage > 0 || gpuMemUsed > 0 || gpuPower > 0 {
			c.lastGPUUsage = gpuUsage
			c.lastGPUMemUsed = gpuMemUsed
			c.lastGPUPower = gpuPower
			c.lastGPUTime = time.Now()
		}

		// 补救措施：如果显存总量为 0，尝试重新获取静态信息
		if c.cachedHostInfo != nil && c.cachedHostInfo.GPUMemTotal == 0 {
			go func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				// 再次检查，防止并发重复
				if c.cachedHostInfo.GPUMemTotal == 0 {
					models, total := c.collectGPUMetadata()
					if total > 0 {
						c.cachedHostInfo.GPU = models
						c.cachedHostInfo.GPUMemTotal = total
						fmt.Printf("[Collector] GPU metadata refreshed: %d MiB\n", total/1024/1024)
					}
				}
			}()
		}
	}
	state.GPU = c.lastGPUUsage
	state.GPUMemUsed = c.lastGPUMemUsed
	state.GPUMemTotal = 0
	if c.cachedHostInfo != nil {
		state.GPUMemTotal = c.cachedHostInfo.GPUMemTotal
	}
	state.GPUPower = c.lastGPUPower

	return state
}

// collectDockerInfo 采集 Docker 容器信息
func (c *Collector) collectDockerInfo() DockerInfo {
	info := DockerInfo{
		Installed:  false,
		Running:    0,
		Stopped:    0,
		Containers: []DockerContainer{},
	}

	// 检查 Docker 是否可用
	if _, err := exec.LookPath("docker"); err != nil {
		return info
	}

	// 尝试执行 docker ps 命令
	cmd := exec.Command("docker", "ps", "-a", "--format", "{{json .}}")
	hideWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		// Docker 可能已安装但无权限或未运行
		return info
	}

	info.Installed = true

	// 解析容器列表
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		var container struct {
			ID      string `json:"ID"`
			Names   string `json:"Names"`
			Image   string `json:"Image"`
			State   string `json:"State"`
			Status  string `json:"Status"`
			Created string `json:"CreatedAt"`
		}

		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue
		}

		dc := DockerContainer{
			ID:      container.ID[:12], // 短 ID
			Name:    container.Names,
			Image:   container.Image,
			Status:  container.Status,
			Created: container.Created,
		}

		info.Containers = append(info.Containers, dc)

		// 统计运行/停止状态
		if container.State == "running" {
			info.Running++
		} else {
			info.Stopped++
		}
	}

	return info
}

// getPublicIP 获取公网 IP
func getPublicIP() string {
	endpoints := []string{
		"http://ip.sb",
		"https://api.ipify.org",
		"https://icanhazip.com",
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, endpoint := range endpoints {
		resp, err := client.Get(endpoint)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		ip := strings.TrimSpace(string(body))
		if ip != "" {
			return ip
		}
	}

	return ""
}

// GetHostname 获取主机名
func GetHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// collectGPUMetadata 采集 GPU 型号和显存总量
func (c *Collector) collectGPUMetadata() ([]string, uint64) {
	// 1. 尝试使用 nvidia-smi
	nvidiaSmi := c.getNvidiaSmiPath()
	if nvidiaSmi != "" {
		cmd := exec.Command(nvidiaSmi, "--query-gpu=name,memory.total", "--format=csv,noheader,nounits")
		hideWindow(cmd)
		output, err := cmd.Output()
		if err == nil {
			lines := strings.Split(strings.TrimSpace(string(output)), "\n")
			var models []string
			var totalMem uint64

			for _, line := range lines {
				parts := strings.Split(line, ",")
				if len(parts) >= 2 {
					models = append(models, strings.TrimSpace(parts[0]))
					mem, _ := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
					totalMem += mem * 1024 * 1024 // MiB 转为 Bytes
				}
			}
			if len(models) > 0 {
				return models, totalMem
			}
		} else {
			fmt.Printf("[Collector] nvidia-smi failed: %v\n", err)
		}
	}

	// 2. Windows 下回退到 PowerShell (CIM/WMI)
	if runtime.GOOS == "windows" {
		return c.collectGPUInfoWindows()
	}

	return []string{}, 0
}

// collectGPUInfoWindows Windows 下采集 GPU 信息 (PowerShell)
func (c *Collector) collectGPUInfoWindows() ([]string, uint64) {
	// 使用 PowerShell 获取，避免依赖 wmic
	psCmd := "Get-CimInstance Win32_VideoController | Select-Object Name, AdapterRAM | ForEach-Object { $_.Name + ',' + $_.AdapterRAM }"
	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	hideWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("[Collector] PowerShell GPU info failed: %v\n", err)
		return []string{}, 0
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var models []string
	var totalMem uint64

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, ",")
		// PowerShell Output: Name,AdapterRAM
		// 注意: 某些虚拟显卡可能没有 AdapterRAM，导致 split 后可能只有1部分或空值
		if len(parts) >= 1 {
			name := strings.TrimSpace(parts[0])
			if name != "" {
				models = append(models, name)
			}
			
			if len(parts) >= 2 {
				mem, _ := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
				totalMem += mem
			}
		}
	}
	return models, totalMem
}

// collectGPUState 采集 GPU 使用率、显存占用和功耗 (带超时保护)
func (c *Collector) collectGPUState() (float64, uint64, float64) {
	nvidiaSmi := c.getNvidiaSmiPath()
	if nvidiaSmi == "" {
		return 0, 0, 0
	}

	// 使用 context 添加超时保护 (2秒)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 获取使用率、显存已用量和功耗
	cmd := exec.CommandContext(ctx, nvidiaSmi, "--query-gpu=utilization.gpu,memory.used,power.draw", "--format=csv,noheader,nounits")
	hideWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("[Collector] GPU state collection failed: %v\n", err)
		return 0, 0, 0
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 {
		return 0, 0, 0
	}

	var totalUsage float64
	var totalUsedMem uint64
	var totalPower float64
	var count int

	for _, line := range lines {
		parts := strings.Split(line, ",")
		if len(parts) >= 3 {
			usage, _ := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			used, _ := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
			power, _ := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
			totalUsage += usage
			totalUsedMem += used * 1024 * 1024 // MiB 转为 Bytes
			totalPower += power
			count++
		}
	}

	if count == 0 {
		return 0, 0, 0
	}
	
	avgUsage := totalUsage / float64(count)
	if avgUsage > 0 || totalUsedMem > 0 {
		// 仅在有意义时打印日志
		// fmt.Printf("[Collector] GPU: %.1f%%, Mem: %d MiB, Power: %.1f W\n", avgUsage, totalUsedMem/1024/1024, totalPower)
	}
	
	return avgUsage, totalUsedMem, totalPower
}

func (c *Collector) getNvidiaSmiPath() string {
	if runtime.GOOS == "windows" {
		possiblePaths := []string{
			"C:\\Windows\\System32\\nvidia-smi.exe",
			"C:\\Program Files\\NVIDIA Corporation\\NVSMI\\nvidia-smi.exe",
			"nvidia-smi",
		}
		for _, p := range possiblePaths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
			if p != "nvidia-smi" {
				if _, err := exec.LookPath(p); err == nil {
					return p
				}
			}
		}
		// 最后尝试直接从 PATH 查找
		if p, err := exec.LookPath("nvidia-smi"); err == nil {
			return p
		}
	} else {
		if p, err := exec.LookPath("nvidia-smi"); err == nil {
			return p
		}
	}
	return ""
}

// 废弃旧方法
func (c *Collector) collectGPUUsage() float64 {
	u, _, _ := c.collectGPUState()
	return u
}
