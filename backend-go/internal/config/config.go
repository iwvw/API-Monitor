package config

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Version       string
	Host          string
	Port          int
	LegacyBaseURL string
	DistDir       string
	PublicDir     string
	DataDir       string
	DBName        string
}

func Load(version string) Config {
	root := repoRoot()
	return Config{
		Version:       version,
		Host:          envString("GO_HOST", "0.0.0.0"),
		Port:          envInt("GO_PORT", envInt("PORT", 3000)),
		LegacyBaseURL: strings.TrimRight(os.Getenv("NODE_LEGACY_URL"), "/"),
		DistDir:       envPath(root, "DIST_DIR", filepath.Join(root, "dist")),
		PublicDir:     envPath(root, "PUBLIC_DIR", filepath.Join(root, "public")),
		DataDir:       envPath(root, "DATA_DIR", filepath.Join(root, "data")),
		DBName:        envString("DB_NAME", "data.db"),
	}
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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
