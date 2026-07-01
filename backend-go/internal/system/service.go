package system

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/config"
	"github.com/iwvw/api-monitor/backend-go/internal/database"
	"github.com/iwvw/api-monitor/backend-go/internal/response"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
)

type Notifier interface {
	Trigger(ctx context.Context, sourceModule, eventType string, eventData map[string]interface{}) error
}

type alertState struct {
	cpuHigh    bool
	memoryHigh bool
	diskHigh   bool
}

type Service struct {
	cfg        config.Config
	startedAt  time.Time
	store      *database.Store

	mu         sync.Mutex
	statsCache map[string]*APICounters
	stopChan   chan struct{}
	wg         sync.WaitGroup
	notifier   Notifier
	alertState alertState
}

type APICounters struct {
	Audit int64 `json:"audit"`
	Ops   int64 `json:"ops"`
}

func (s *Service) SetNotifier(n Notifier) {
	s.notifier = n
}

func New(cfg config.Config) *Service {
	s := &Service{
		cfg:        cfg,
		startedAt:  time.Now(),
		store:      database.New(cfg),
		statsCache: make(map[string]*APICounters),
		stopChan:   make(chan struct{}),
	}

	s.wg.Add(1)
	go s.runFlushLoop()

	s.wg.Add(1)
	go s.runHostMonitorLoop()

	return s
}

func (s *Service) runFlushLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.flushToDB()
		case <-s.stopChan:
			s.flushToDB()
			return
		}
	}
}

func (s *Service) runHostMonitorLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.checkHostAlerts()
		case <-s.stopChan:
			return
		}
	}
}

func (s *Service) checkHostAlerts() {
	if s.notifier == nil {
		return
	}
	metrics, err := s.hostMetrics()
	if err != nil {
		return
	}

	cpuInfo, _ := metrics["cpu"].(map[string]interface{})
	cpuVal, _ := cpuInfo["usage"].(float64)

	memInfo, _ := metrics["memory"].(map[string]interface{})
	memVal, _ := memInfo["usage"].(float64)

	diskInfo, _ := metrics["disk"].(map[string]interface{})
	diskVal, _ := diskInfo["usage"].(float64)

	hostname, _ := metrics["hostname"].(string)
	if hostname == "" {
		hostname = "local-host"
	}

	eventData := map[string]interface{}{
		"serverId":   "local-host",
		"serverName": hostname,
		"host":       hostname,
		"hostname":   hostname,
		"cpu_usage":  cpuVal,
		"mem_percent": memVal,
		"disk_usage": diskVal,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	// CPU
	if cpuVal >= 90 {
		if !s.alertState.cpuHigh {
			s.alertState.cpuHigh = true
			eventData["eventType"] = "cpu_high"
			_ = s.notifier.Trigger(ctx, "system", "cpu_high", eventData)
		}
	} else if cpuVal < 85 {
		if s.alertState.cpuHigh {
			s.alertState.cpuHigh = false
			eventData["eventType"] = "cpu_normal"
			_ = s.notifier.Trigger(ctx, "system", "cpu_normal", eventData)
		}
	}

	// Memory
	if memVal >= 90 {
		if !s.alertState.memoryHigh {
			s.alertState.memoryHigh = true
			eventData["eventType"] = "memory_high"
			_ = s.notifier.Trigger(ctx, "system", "memory_high", eventData)
		}
	} else if memVal < 85 {
		if s.alertState.memoryHigh {
			s.alertState.memoryHigh = false
			eventData["eventType"] = "memory_normal"
			_ = s.notifier.Trigger(ctx, "system", "memory_normal", eventData)
		}
	}

	// Disk
	if diskVal >= 90 {
		if !s.alertState.diskHigh {
			s.alertState.diskHigh = true
			eventData["eventType"] = "disk_high"
			_ = s.notifier.Trigger(ctx, "system", "disk_high", eventData)
		}
	} else if diskVal < 85 {
		if s.alertState.diskHigh {
			s.alertState.diskHigh = false
			eventData["eventType"] = "disk_normal"
			_ = s.notifier.Trigger(ctx, "system", "disk_normal", eventData)
		}
	}
}

func (s *Service) flushToDB() {
	s.mu.Lock()
	if len(s.statsCache) == 0 {
		s.mu.Unlock()
		return
	}

	// Copy counters to release the lock quickly
	statsToSave := make(map[string]*APICounters)
	for k, v := range s.statsCache {
		statsToSave[k] = &APICounters{Audit: v.Audit, Ops: v.Ops}
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := s.store.Open(ctx)
	if err != nil {
		return
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO system_api_stats (date, audit_count, ops_count)
		VALUES (?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			audit_count = audit_count + excluded.audit_count,
			ops_count = ops_count + excluded.ops_count,
			updated_at = CURRENT_TIMESTAMP
	`)
	if err != nil {
		return
	}
	defer stmt.Close()

	for date, counters := range statsToSave {
		if _, err := stmt.ExecContext(ctx, date, counters.Audit, counters.Ops); err != nil {
			return
		}
	}

	if err := tx.Commit(); err != nil {
		return
	}

	// Subtract the successfully written values
	s.mu.Lock()
	for date, saved := range statsToSave {
		if current, exists := s.statsCache[date]; exists {
			current.Audit -= saved.Audit
			current.Ops -= saved.Ops
			if current.Audit <= 0 && current.Ops <= 0 {
				delete(s.statsCache, date)
			}
		}
	}
	s.mu.Unlock()
}

func (s *Service) RecordAPICall(method string, path string) {
	// Filter high-frequency heartbeat and stats queries
	if path == "/api/system/host-metrics" || path == "/api/system/api-stats" || path == "/health" {
		return
	}

	date := time.Now().Format("2006-01-02")

	s.mu.Lock()
	defer s.mu.Unlock()

	counters, exists := s.statsCache[date]
	if !exists {
		counters = &APICounters{}
		s.statsCache[date] = counters
	}

	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		counters.Audit++
	} else {
		counters.Ops++
	}
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	switch r.URL.Path {
	case "/api/system/host-metrics":
		payload, err := s.hostMetrics()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, payload)
	case "/api/system/api-stats":
		payload, err := s.apiStats()
		if err != nil {
			response.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		response.OK(w, payload)
	default:
		response.Error(w, http.StatusNotFound, "system route not implemented")
	}
}

func (s *Service) apiStats() (map[string]interface{}, error) {
	now := time.Now()
	startDateStr := now.AddDate(0, 0, -6).Format("2006-01-02")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := s.store.Open(ctx)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, `
		SELECT date, audit_count, ops_count FROM system_api_stats
		WHERE date >= ? ORDER BY date ASC
	`, startDateStr)
	if err != nil {
		return nil, fmt.Errorf("query api stats: %w", err)
	}
	defer rows.Close()

	dbData := make(map[string]*APICounters)
	for rows.Next() {
		var date string
		var audit, ops int64
		if err := rows.Scan(&date, &audit, &ops); err != nil {
			return nil, err
		}
		dbData[date] = &APICounters{Audit: audit, Ops: ops}
	}

	trend := make([]map[string]interface{}, 0, 7)
	var totalAudit, totalOps int64

	s.mu.Lock()
	for i := 0; i < 7; i++ {
		day := now.AddDate(0, 0, -6+i).Format("2006-01-02")

		var auditVal, opsVal int64
		if dbVal, exists := dbData[day]; exists {
			auditVal += dbVal.Audit
			opsVal += dbVal.Ops
		}
		if memVal, exists := s.statsCache[day]; exists {
			auditVal += memVal.Audit
			opsVal += memVal.Ops
		}

		totalAudit += auditVal
		totalOps += opsVal

		trend = append(trend, map[string]interface{}{
			"bucket": day,
			"audit":  auditVal,
			"ops":    opsVal,
			"total":  auditVal + opsVal,
		})
	}
	s.mu.Unlock()

	return map[string]interface{}{
		"total": map[string]interface{}{
			"audit": totalAudit,
			"ops":   totalOps,
			"all":   totalAudit + totalOps,
		},
		"trend": trend,
	}, nil
}

func (s *Service) Shutdown() {
	close(s.stopChan)
	s.wg.Wait()
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

func isVirtualFS(fstype string) bool {
	virtualFS := map[string]bool{
		"tmpfs":        true,
		"devtmpfs":     true,
		"sysfs":        true,
		"proc":         true,
		"devpts":       true,
		"cgroup":       true,
		"overlay":      true,
		"squashfs":     true,
		"iso9660":      true,
		"udf":          true,
		"configfs":     true,
		"debugfs":      true,
		"tracefs":      true,
		"securityfs":   true,
		"pstore":       true,
		"bpf":          true,
		"fusectl":      true,
		"mqueue":       true,
		"hugetlbfs":    true,
		"autofs":       true,
		"binfmt_misc":  true,
		"devfs":        true,
		"fdescfs":      true,
		"linprocfs":    true,
		"linsysfs":     true,
		"procfs":       true,
		"sysctlfs":     true,
	}
	return virtualFS[fstype]
}

func readDiskUsageSingle() map[string]interface{} {
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

func readDiskUsage() map[string]interface{} {
	parts, err := disk.Partitions(false)
	if err != nil {
		return readDiskUsageSingle()
	}

	var total, used, free uint64
	var roots []string
	seenDevices := make(map[string]bool)

	for _, p := range parts {
		if isVirtualFS(p.Fstype) {
			continue
		}
		if p.Device == "" || seenDevices[p.Device] {
			continue
		}

		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}
		if usage.Total == 0 {
			continue
		}

		seenDevices[p.Device] = true
		total += usage.Total
		used += usage.Used
		free += usage.Free
		roots = append(roots, p.Mountpoint)
	}

	if len(roots) == 0 {
		return readDiskUsageSingle()
	}

	var usagePercent float64
	if total > 0 {
		usagePercent = float64(used) / float64(total) * 100
	}

	return map[string]interface{}{
		"root":  strings.Join(roots, ", "),
		"total": total,
		"used":  used,
		"free":  free,
		"usage": clampPercent(usagePercent),
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
