package system

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type Service struct {
	startedAt time.Time
}

func New() *Service {
	return &Service{startedAt: time.Now()}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || r.URL.Path != "/api/system/host-metrics" {
		response.Error(w, http.StatusNotFound, "system route not implemented")
		return
	}

	payload, err := s.hostMetrics()
	if err != nil {
		response.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	response.OK(w, payload)
}

func (s *Service) hostMetrics() (map[string]interface{}, error) {
	hostInfo, _ := host.Info()
	cpuPercent := readCPUPercent()
	cpuInfo := readCPUInfo()
	virtualMemory := readVirtualMemory()
	diskUsage := readDiskUsage()
	currentProcess := readProcessInfo(s.startedAt)

	return map[string]interface{}{
		"hostname":      hostname(),
		"platform":      nodePlatformName(runtime.GOOS),
		"platformLabel": platformLabel(hostInfo),
		"uptime":        systemUptimeSeconds(hostInfo),
		"cpu": map[string]interface{}{
			"usage":       cpuPercent,
			"cores":       runtime.NumCPU(),
			"model":       cpuInfo.model,
			"loadAverage": readLoadAverage(),
		},
		"memory":    virtualMemory,
		"disk":      diskUsage,
		"process":   currentProcess,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

type cpuDetails struct {
	model string
}

func readCPUPercent() float64 {
	percentages, err := cpu.Percent(120*time.Millisecond, false)
	if err != nil || len(percentages) == 0 {
		return 0
	}
	return clampPercent(percentages[0])
}

func readCPUInfo() cpuDetails {
	info, err := cpu.Info()
	if err != nil || len(info) == 0 {
		return cpuDetails{}
	}
	return cpuDetails{model: info[0].ModelName}
}

func readVirtualMemory() map[string]interface{} {
	stats, err := mem.VirtualMemory()
	if err != nil {
		return map[string]interface{}{
			"total": uint64(0),
			"used":  uint64(0),
			"free":  uint64(0),
			"usage": float64(0),
			"error": err.Error(),
		}
	}
	return map[string]interface{}{
		"total": stats.Total,
		"used":  stats.Used,
		"free":  stats.Free,
		"usage": clampPercent(stats.UsedPercent),
	}
}

func readDiskUsage() map[string]interface{} {
	root := rootPath()
	usage, err := disk.Usage(root)
	if err != nil {
		return map[string]interface{}{
			"root":  root,
			"total": uint64(0),
			"used":  uint64(0),
			"free":  uint64(0),
			"usage": float64(0),
			"error": err.Error(),
		}
	}
	return map[string]interface{}{
		"root":  usage.Path,
		"total": usage.Total,
		"used":  usage.Used,
		"free":  usage.Free,
		"usage": clampPercent(usage.UsedPercent),
	}
}

func readLoadAverage() []float64 {
	avg, err := load.Avg()
	if err != nil || avg == nil {
		return []float64{0, 0, 0}
	}
	return []float64{avg.Load1, avg.Load5, avg.Load15}
}

func readProcessInfo(startedAt time.Time) map[string]interface{} {
	memoryRSS := uint64(0)
	if current, err := process.NewProcess(int32(os.Getpid())); err == nil {
		if info, err := current.MemoryInfo(); err == nil && info != nil {
			memoryRSS = info.RSS
		}
	}
	return map[string]interface{}{
		"uptime":    time.Since(startedAt).Seconds(),
		"memoryRss": memoryRSS,
	}
}

func systemUptimeSeconds(info *host.InfoStat) uint64 {
	if info != nil && info.Uptime > 0 {
		return info.Uptime
	}
	return 0
}

func platformLabel(info *host.InfoStat) string {
	osType := osTypeName(runtime.GOOS)
	release := ""
	if info != nil {
		release = info.KernelVersion
		if release == "" {
			release = info.PlatformVersion
		}
	}
	if release == "" {
		return osType
	}
	return fmt.Sprintf("%s %s", osType, release)
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

func rootPath() string {
	wd, err := os.Getwd()
	if err != nil {
		if runtime.GOOS == "windows" {
			return `C:\`
		}
		return "/"
	}
	if runtime.GOOS == "windows" {
		volume := filepath.VolumeName(wd)
		if volume == "" {
			return `C:\`
		}
		return volume + `\`
	}
	volume := filepath.VolumeName(wd)
	if volume != "" {
		return volume + string(os.PathSeparator)
	}
	return string(os.PathSeparator)
}

func nodePlatformName(goos string) string {
	switch goos {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	case "linux":
		return "linux"
	default:
		return goos
	}
}

func osTypeName(goos string) string {
	switch goos {
	case "windows":
		return "Windows_NT"
	case "darwin":
		return "Darwin"
	case "linux":
		return "Linux"
	default:
		return goos
	}
}

func clampPercent(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
