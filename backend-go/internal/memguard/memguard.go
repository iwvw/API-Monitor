package memguard

import (
	"bufio"
	"context"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

const (
	defaultLimitRatio     = 0.70
	defaultTriggerRatio   = 0.85
	defaultCheckInterval  = 15 * time.Second
	minContainerLimitByte = 64 * 1024 * 1024
	// maxTrustedCgroupBytes 是可信的 cgroup 内存上限最大值（1TB）：
	// cgroup v1 的 memory.limit_in_bytes 在「未限制」时常返回 LONG_MAX 附近的
	// 巨大值（实测 Fly.io 上读到 6.4EB），直接采用会把内存守卫变成形同虚设。
	maxTrustedCgroupBytes = 1 << 40
)

type Config struct {
	LimitBytes    int64
	TriggerBytes  uint64
	CheckInterval time.Duration
	Source        string
}

func Start(ctx context.Context) Config {
	cfg := resolveConfig()
	if cfg.LimitBytes <= 0 {
		applog.Info(ctx, "memguard", "memory guard disabled")
		return cfg
	}

	debug.SetMemoryLimit(cfg.LimitBytes)
	applog.Info(ctx, "memguard", "memory guard enabled",
		"limit_bytes", cfg.LimitBytes,
		"trigger_bytes", cfg.TriggerBytes,
		"source", cfg.Source,
	)
	// MemTotal 回退只发生在 cgroup 限制不可信/未生效的环境：多租户容器或
	// 共享宿主机上宿主总内存可能远大于容器可用内存，估算值会高估软限。
	// 这类环境建议显式配置 API_MONITOR_MEMORY_LIMIT_MB 锁定真实预算。
	if cfg.Source == "meminfo_MemTotal" {
		applog.Warn(ctx, "memguard", "cgroup 内存上限不可信，已回退宿主 MemTotal 估算；多租户容器建议显式设置 API_MONITOR_MEMORY_LIMIT_MB",
			"limit_bytes", cfg.LimitBytes)
	}

	go run(ctx, cfg)
	return cfg
}

func run(ctx context.Context, cfg Config) {
	ticker := time.NewTicker(cfg.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			rss := readProcessRSSBytes()
			if stats.Sys < cfg.TriggerBytes && stats.HeapAlloc < cfg.TriggerBytes && rss < cfg.TriggerBytes {
				continue
			}

			beforeSys := stats.Sys
			beforeHeap := stats.HeapAlloc
			beforeRSS := rss
			runtime.GC()
			debug.FreeOSMemory()
			runtime.ReadMemStats(&stats)

			applog.Warn(ctx, "memguard", "memory pressure cleanup completed",
				"before_sys_bytes", beforeSys,
				"after_sys_bytes", stats.Sys,
				"before_heap_bytes", beforeHeap,
				"after_heap_bytes", stats.HeapAlloc,
				"before_rss_bytes", beforeRSS,
				"after_rss_bytes", readProcessRSSBytes(),
				"limit_bytes", cfg.LimitBytes,
			)
		}
	}
}

func resolveConfig() Config {
	if isDisabled(os.Getenv("API_MONITOR_MEMORY_GUARD")) {
		return Config{}
	}

	if limitMB := parsePositiveInt(os.Getenv("API_MONITOR_MEMORY_LIMIT_MB")); limitMB > 0 {
		limit := int64(limitMB) * 1024 * 1024
		return configForLimit(limit, "API_MONITOR_MEMORY_LIMIT_MB")
	}

	if limit, source := containerMemoryLimit(); limit >= minContainerLimitByte {
		return configForLimit(int64(float64(limit)*defaultLimitRatio), source)
	}

	return Config{}
}

// containerMemoryLimit 读取容器内存上限：优先 cgroup；cgroup 上限缺失、
// 读取失败或不可信（≥ maxTrustedCgroupBytes，即容器未对该文件设限）时，
// 回退用 /proc/meminfo 的 MemTotal 估算，保证内存守卫在更多环境落地生效。
func containerMemoryLimit() (uint64, string) {
	if limit, source := cgroupMemoryLimit(); trustedCgroupLimit(limit) {
		return limit, source
	}
	if total, ok := hostMemTotalBytes(); ok && total >= minContainerLimitByte {
		return total, "meminfo_MemTotal"
	}
	return 0, ""
}

// trustedCgroupLimit 判断 cgroup 内存上限是否可信可采纳
// （≥64MB 且未超过 1TB；超过即容器对该文件未设真实限制）。
func trustedCgroupLimit(limit uint64) bool {
	return limit >= minContainerLimitByte && limit <= maxTrustedCgroupBytes
}

// hostMemTotalBytes 解析 /proc/meminfo 的 MemTotal（KB → bytes）。
func hostMemTotalBytes() (uint64, bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	return parseMemTotal(string(raw))
}

func parseMemTotal(content string) (uint64, bool) {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, false
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return kb * 1024, true
	}
	return 0, false
}

func configForLimit(limit int64, source string) Config {
	triggerRatio := defaultTriggerRatio
	if raw := strings.TrimSpace(os.Getenv("API_MONITOR_MEMORY_GC_TRIGGER_RATIO")); raw != "" {
		if parsed, err := strconv.ParseFloat(raw, 64); err == nil && parsed > 0 && parsed <= 1 {
			triggerRatio = parsed
		}
	}

	interval := defaultCheckInterval
	if seconds := parsePositiveInt(os.Getenv("API_MONITOR_MEMORY_CHECK_SECONDS")); seconds > 0 {
		interval = time.Duration(seconds) * time.Second
	}

	return Config{
		LimitBytes:    limit,
		TriggerBytes:  uint64(float64(limit) * triggerRatio),
		CheckInterval: interval,
		Source:        source,
	}
}

// cgroupMemoryLimit 读取容器 cgroup 内存上限（v2 优先、v1 兜底），
// 包级变量便于测试替换注入。
var cgroupMemoryLimit = func() (uint64, string) {
	if value, ok := readCgroupLimitFile("/sys/fs/cgroup/memory.max"); ok {
		return value, "cgroup_v2_memory_max"
	}
	if value, ok := readCgroupLimitFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); ok {
		return value, "cgroup_v1_memory_limit"
	}
	return 0, ""
}

// readCgroupLimitFile 读取并解析单个 cgroup 限制文件，包级变量便于测试替换。
var readCgroupLimitFile = func(path string) (uint64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "max" {
		return 0, false
	}
	limit, err := strconv.ParseUint(value, 10, 64)
	if err != nil || limit == 0 || limit > uint64(math.MaxInt64) {
		return 0, false
	}
	return limit, true
}

func parsePositiveInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}

func isDisabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "0", "false", "off", "disabled", "no":
		return true
	default:
		return false
	}
}

func readProcessRSSBytes() uint64 {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kib, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kib * 1024
	}
	return 0
}
