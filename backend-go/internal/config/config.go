package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Version              string
	Environment          string
	Host                 string
	Port                 int
	LegacyBaseURL        string
	DistDir              string
	PublicDir            string
	DataDir              string
	DBName               string
	SecureCookies        bool
	AllowLocalShellTasks bool
	CORSAllowedOrigins   []string
	TrustedProxyCIDRs    []string
	AdminAIDefaultModel  string
	// GatewayBodyMaxBytes 是模型网关（模型网关转发入口）可接受的请求体上限；
	// 小内存主机上全量读入 body 会放大 3 倍内存，超限请求直接 413。
	// 环境变量 GATEWAY_BODY_MAX_MB（默认 16）配置。
	GatewayBodyMaxBytes int64
}

func Load(version string) Config {
	root := repoRoot()
	environment := strings.ToLower(envString("APP_ENV", envString("NODE_ENV", "development")))
	return Config{
		Version:              version,
		Environment:          environment,
		Host:                 envString("GO_HOST", "0.0.0.0"),
		Port:                 envInt("GO_PORT", envInt("PORT", 3000)),
		LegacyBaseURL:        strings.TrimRight(os.Getenv("NODE_LEGACY_URL"), "/"),
		DistDir:              envPath(root, "DIST_DIR", filepath.Join(root, "dist")),
		PublicDir:            envPath(root, "PUBLIC_DIR", filepath.Join(root, "public")),
		DataDir:              envPath(root, "DATA_DIR", filepath.Join(root, "data")),
		DBName:               envString("DB_NAME", "data.db"),
		SecureCookies:        envBool("SECURE_COOKIES", environment == "production"),
		AllowLocalShellTasks: envBool("ALLOW_LOCAL_SHELL_TASKS", environment != "production"),
		CORSAllowedOrigins:   envList("CORS_ALLOWED_ORIGINS"),
		TrustedProxyCIDRs:    trustedProxyCIDRs(),
		AdminAIDefaultModel:  envString("ADMIN_AI_DEFAULT_MODEL", ""),
		GatewayBodyMaxBytes:  int64(envInt("GATEWAY_BODY_MAX_MB", 16)) * 1024 * 1024,
	}
}

func (c Config) IsProduction() bool {
	return strings.EqualFold(strings.TrimSpace(c.Environment), "production")
}

func (c Config) ValidateSecurity() error {
	if !c.IsProduction() {
		return nil
	}
	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "ENCRYPTION_KEY", value: os.Getenv("ENCRYPTION_KEY")},
		{name: "JWT_SECRET", value: os.Getenv("JWT_SECRET")},
	} {
		if len(strings.TrimSpace(item.value)) < 32 {
			return fmt.Errorf("production requires %s with at least 32 characters", item.name)
		}
	}
	return nil
}

func (c Config) LocalShellTasksAllowed() bool {
	// Config literals used by tests and local embedders predate Environment.
	// Preserve their development behavior while loaded production config is explicit.
	if strings.TrimSpace(c.Environment) == "" {
		return true
	}
	return c.AllowLocalShellTasks
}

func (c Config) ListenAddress() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func (c Config) DatabasePath() string {
	return filepath.Join(c.DataDir, c.DBName)
}

func repoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}

	for {
		// 检查项目根目录标记：package.json + backend-go/
		if exists(filepath.Join(wd, "package.json")) && exists(filepath.Join(wd, "backend-go")) {
			return wd
		}
		// 运行时镜像只保留 /app/api-monitor 和 /app/dist，没有源码树标记。
		if exists(filepath.Join(wd, "dist", "index.html")) {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			return wd
		}
		wd = parent
	}
}

func envString(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envPath(root, name, fallback string) string {
	value := envString(name, fallback)
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(root, value)
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envList(name string) []string {
	values := strings.Split(os.Getenv(name), ",")
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, strings.TrimRight(value, "/"))
		}
	}
	return result
}

// defaultTrustedProxyCIDRs 是 TRUSTED_PROXY_CIDRS 未显式配置时的回退集合。
// 目标部署形态是「反向代理/容器网关 → Go 后端」，代理与后端总是经回环或
// 私有网段直连；若这里为空，requestClientIP 会退回到代理自身的地址，导致
// 所有真实客户端被视为同一来源（登录限流退化为全局单桶，任何人都能锁死
// 登录）。显式配置该变量会完全覆盖这些默认值；配置为 none 表示不信任任何
// 代理（仅适用于后端直接暴露、无前置代理的场景）。
var defaultTrustedProxyCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
	"10.0.0.0/8",
	"172.16.0.0/12",
	"192.168.0.0/16",
	"fc00::/7",
}

func trustedProxyCIDRs() []string {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if raw == "" {
		return append([]string(nil), defaultTrustedProxyCIDRs...)
	}
	if strings.EqualFold(raw, "none") {
		return nil
	}
	return envList("TRUSTED_PROXY_CIDRS")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
