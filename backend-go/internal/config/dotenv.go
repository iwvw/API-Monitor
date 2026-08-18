package config

import (
	"bufio"
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
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		values[strings.TrimSpace(parts[0])] = strings.Trim(strings.TrimSpace(parts[1]), `"'`)
	}
	return values
}