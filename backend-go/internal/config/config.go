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
		TrustedProxyCIDRs:    envList("TRUSTED_PROXY_CIDRS"),
		AdminAIDefaultModel:  envString("ADMIN_AI_DEFAULT_MODEL", ""),
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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
