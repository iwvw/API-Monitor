package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv 在读取配置前加载 .env 文件（若有），仅填充尚未设置的环境变量
// （进程环境优先），支持注释行与引号。
//
// 背景：面板在 Node 时代通过 dotenv 读取 .env（如 ENCRYPTION_KEY、PORT、
// ADMIN_PASSWORD），Go 主程序此前不加载导致这些配置静默失效；
// 例如 ENCRYPTION_KEY 失效会让旧密文端点密钥解密失败、面板显示为空。
func LoadDotEnv(dirs ...string) {
	if len(dirs) == 0 {
		dirs = dotEnvCandidates()
	}
	loaded := 0
	for _, dir := range dirs {
		values := readDotEnvFile(filepath.Join(dir, ".env"))
		for key, value := range values {
			if os.Getenv(key) == "" {
				_ = os.Setenv(key, value)
				loaded++
			}
		}
		if loaded > 0 {
			return
		}
	}
}

// dotEnvCandidates 按优先级返回 .env 的候选目录：
// 1. 当前工作目录（开发时 go run 所在目录）
// 2. 仓库根目录（repoRoot 标记：package.json + backend-go）
// 3. 可执行文件所在目录（发布形态）
func dotEnvCandidates() []string {
	dirs := make([]string, 0, 3)
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	dirs = append(dirs, repoRoot())
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}
	return dedupeStrings(dirs)
}

func dedupeStrings(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// readDotEnvFile 解析简单 KEY=VALUE 格式（兼容 Node 侧 dotenv 常用子集）：
// 忽略空行/注释行；键值两侧空白与首尾引号被去除。
func readDotEnvFile(path string) map[string]string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	// 行缓冲上限调大到 1MB：默认 64KB 会在超长行处静默截断后续内容。
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 剥离 UTF-8 BOM（Windows 记事本保存的 .env 首行带 BOM，
		// 不剥会导致首个键名带不可见前缀而静默失效）。
		line = strings.TrimPrefix(line, "\uFEFF")
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		values[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	}
	// 读取中途的 I/O 错误只告警不中断：已解析的键值仍然生效。
	if err := scanner.Err(); err != nil {
		log.Printf("[config] 读取 .env 文件出错（已解析内容仍然生效） %s: %v", path, err)
	}
	return values
}
