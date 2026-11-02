package memguard

import (
	"strings"
	"testing"
)

// 小内存主机（≤512MB）用更保守的 GOMEMLIMIT 比率，大主机保持默认 0.70。
func TestLimitRatioForSmallHost(t *testing.T) {
	cases := []struct {
		name  string
		limit uint64
		want  float64
	}{
		{"200MB 主机（本次加固目标）", 200 << 20, smallHostLimitRatio},
		{"512MB 边界", 512 << 20, smallHostLimitRatio},
		{"513MB", 513 << 20, defaultLimitRatio},
		{"1GB", 1 << 30, defaultLimitRatio},
	}
	for _, c := range cases {
		if got := limitRatioFor(c.limit); got != c.want {
			t.Fatalf("%s: limitRatioFor(%d) = %v, want %v", c.name, c.limit, got, c.want)
		}
	}
}

// cgroup 正常值（容器设了 256MB 上限）→ 可信采纳。
func TestTrustedCgroupLimit(t *testing.T) {
	cases := []struct {
		name   string
		limit  uint64
		want   bool
	}{
		{"256MB", 256 * 1024 * 1024, true},
		{"64MB 最小值", 64 * 1024 * 1024, true},
		{"1TB 边界", 1 << 40, true},
		{"0", 0, false},
		{"低于最小值", 32 * 1024 * 1024, false},
		{"未限制（Fly.io 实测 6.4EB）", 6456360425798339584, false},
		{"LONG_MAX 附近", 9223372036854771712, false},
	}
	for _, c := range cases {
		if got := trustedCgroupLimit(c.limit); got != c.want {
			t.Fatalf("%s: trustedCgroupLimit(%d) = %v, want %v", c.name, c.limit, got, c.want)
		}
	}
}

// MemTotal 解析：标准 Linux 输出。
func TestParseMemTotal(t *testing.T) {
	content := `
MemTotal:       262144 kB
MemFree:        100000 kB
MemAvailable:   200000 kB
`
	total, ok := parseMemTotal(content)
	if !ok || total != 262144*1024 {
		t.Fatalf("parseMemTotal = %d, %v; want %d, true", total, ok, 262144*1024)
	}
}

// MemTotal 缺失或格式异常 → 返回 not-ok（调用方回退 disabled）。
func TestParseMemTotalMissing(t *testing.T) {
	if _, ok := parseMemTotal("nothing here"); ok {
		t.Fatal("应解析失败")
	}
	if _, ok := parseMemTotal("MemTotal: not-a-number kB\n"); ok {
		t.Fatal("非数字应解析失败")
	}
}

// containerMemoryLimit 的组合逻辑：cgroup 不可信 → 尝试 MemTotal 兜底；
// cgroup 可信（256MB）→ 优先采纳 cgroup。
// 注：兜底读取真实 /proc/meminfo，仅 Linux 容器环境可全链路验证；Windows 上
// /proc 不存在即回退 0，属预期行为（本包守护在部署环境为 Linux/Fly 容器）。
func TestContainerMemoryLimitFallback(t *testing.T) {
	orig := cgroupMemoryLimit
	origRead := readCgroupLimitFile
	defer func() {
		cgroupMemoryLimit = orig
		readCgroupLimitFile = origRead
	}()

	var (
		lastSource string
		lastOK     bool
	)
	// cgroup 读到未限制的巨大值（Fly.io 实测）→ 不可信，进入 MemTotal 兜底
	readCgroupLimitFile = func(path string) (uint64, bool) {
		return 6456360425798339584, true
	}
	cgroupMemoryLimit = func() (uint64, string) {
		v, ok := readCgroupLimitFile("")
		lastSource, lastOK = "cgroup_v1_memory_limit", ok
		return v, "cgroup_v1_memory_limit"
	}

	limit, source := containerMemoryLimit()
	// cgroup 返回值不可信（应被 trustedCgroupLimit 拒绝）；
	// 有 /proc 的 Linux 上应回退 MemTotal 且 ≥64MB，无 /proc 的 Windows 上为 0。
	if lastSource != "cgroup_v1_memory_limit" {
		t.Fatal("应优先读取 cgroup")
	}
	// 有 /proc 的 Linux 上回退 MemTotal（≥64MB 且来源标注 meminfo）；
	// 无 /proc 的环境（Windows）回退失败为 0，属预期。
	if limit > 0 && (limit < 64*1024*1024 || !strings.Contains(source, "meminfo")) {
		t.Fatalf("cgroup 不可信时应回退 meminfo，got limit=%d source=%q", limit, source)
	}

	// cgroup 可信时（256MB）优先采纳 cgroup，不回退
	readCgroupLimitFile = func(path string) (uint64, bool) {
		return 256 * 1024 * 1024, true
	}
	limit, source = containerMemoryLimit()
	if limit != 256*1024*1024 || source != "cgroup_v1_memory_limit" {
		t.Fatalf("cgroup 可信应优先采纳: limit=%d source=%s", limit, source)
	}
	_ = lastOK
}