// Package commandguard 集中托管「危险命令」检测规则，供 Agent 命令接口、
// 主机任务接口与定时任务调度共用，避免各入口重复维护或遗漏。
package commandguard

import (
	"regexp"
	"strings"
)

type Pattern struct {
	Pattern *regexp.Regexp
	Reason  string
}

var patterns = []Pattern{
	{regexp.MustCompile(`(?i)\brm\s+-[^\n;|&]*r[^\n;|&]*f\b`), "递归强制删除文件"},
	// 单参数内 r/f 任意组合（-fr / -rfv / -vfr 等）
	{regexp.MustCompile(`(?i)\brm\s+(?:-[a-z]*[rR][a-z]*f[a-z]*|-[a-z]*f[a-z]*[rR][a-z]*)`), "递归强制删除文件"},
	// GNU 长参数形式（--recursive + --force，顺序任意）
	{regexp.MustCompile(`(?i)\brm\s+--(?:recursive|force)\b[^\n]*(?:--(?:recursive|force)\b)`), "递归强制删除文件"},
	// 短参数与长参数混合（-r --force / -f --recursive，顺序任意）
	{regexp.MustCompile(`(?i)\brm\s+(?:-[a-z]*r\b[^\n;|&]*--force\b|--force\b[^\n;|&]*-[a-z]*r\b|-[a-z]*f\b[^\n;|&]*--recursive\b|--recursive\b[^\n;|&]*-[a-z]*f\b)`), "递归强制删除文件"},
	{regexp.MustCompile(`(?i)\bdd\s+if=.*\bof=`), "直接写入磁盘或块设备"},
	{regexp.MustCompile(`(?i)\bmkfs(?:\.[a-z0-9]+)?\b`), "格式化文件系统"},
	// 只拦截作为命令名的关机/重启（wsl --shutdown 等选项参数形态不算，前面是连字符）
	{regexp.MustCompile(`(?i)(^|[^-\w])(shutdown|reboot|poweroff|halt)\b`), "重启或关闭主机"},
	{regexp.MustCompile(`(?i)\bdocker\s+(?:system\s+prune|rm|rmi|volume\s+rm)\b`), "删除 Docker 资源"},
	{regexp.MustCompile(`(?i)\bkubectl\s+delete\b`), "删除 Kubernetes 资源"},
	{regexp.MustCompile(`(?i)\bDROP\s+(?:DATABASE|TABLE)\b`), "删除数据库对象"},
	{regexp.MustCompile(`(?i)\bRemove-Item\b[^\n;|]*\s-(?:Recurse|r)\b`), "PowerShell 递归删除"},
	{regexp.MustCompile(`(?i)\b(Stop-Computer|Restart-Computer)\b`), "重启或关闭 Windows 主机"},
}

// Result 是危险命令检测的结果。
type Result struct {
	Dangerous bool     `json:"dangerous"`
	Reasons   []string `json:"reasons"`
}

// Detect 检测命令是否命中危险模式，返回命中的原因列表（去重、保序）。
func Detect(command string) Result {
	var reasons []string
	seen := make(map[string]bool)
	for _, p := range patterns {
		if p.Pattern.MatchString(command) {
			if !seen[p.Reason] {
				seen[p.Reason] = true
				reasons = append(reasons, p.Reason)
			}
		}
	}
	if reasons == nil {
		reasons = []string{}
	}
	return Result{
		Dangerous: len(reasons) > 0,
		Reasons:   reasons,
	}
}

// JoinReasons 将原因列表整理为便于日志/错误展示的字符串。
func JoinReasons(reasons []string) string {
	return strings.Join(reasons, ", ")
}
