package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadDotEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "# 注释\nENCRYPTION_KEY=api-mock-key-32-bytes-long-1234\n\nPORT=\"3100\"\nJWT_SECRET='secret-value'\nINVALID_LINE_NO_EQ\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	values := readDotEnvFile(path)
	if values["ENCRYPTION_KEY"] != "api-mock-key-32-bytes-long-1234" {
		t.Errorf("ENCRYPTION_KEY = %q", values["ENCRYPTION_KEY"])
	}
	if values["PORT"] != "3100" {
		t.Errorf("PORT = %q (quotes should be stripped)", values["PORT"])
	}
	if values["JWT_SECRET"] != "secret-value" {
		t.Errorf("JWT_SECRET = %q (single quotes should be stripped)", values["JWT_SECRET"])
	}
	if _, ok := values["INVALID_LINE_NO_EQ"]; ok {
		t.Error("line without '=' must be ignored")
	}
	if _, ok := values["# 注释"]; ok {
		t.Error("comment line must be ignored")
	}
}

func TestLoadDotEnvFillsOnlyUnset(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("DOTENV_A=from-file\nDOTENV_B=from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOTENV_B", "from-process")

	LoadDotEnv(dir)
	if got := os.Getenv("DOTENV_A"); got != "from-file" {
		t.Errorf("DOTENV_A = %q, want from-file (unset var should be filled)", got)
	}
	if got := os.Getenv("DOTENV_B"); got != "from-process" {
		t.Errorf("DOTENV_B = %q, want from-process (existing env must win)", got)
	}
}

// Windows 记事本保存的 .env 首行带 UTF-8 BOM：不剥离会让首个键名带上
// 不可见前缀而静默失效；超长行（>64KB 默认扫描上限）不得截断后续内容。
func TestReadDotEnvFileBOMAndLongLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	longValue := strings.Repeat("v", 256*1024)
	content := "\uFEFFFIRST_KEY=first\nLONG_KEY=" + longValue + "\nLAST_KEY=last\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	values := readDotEnvFile(path)
	if values["FIRST_KEY"] != "first" {
		t.Errorf("FIRST_KEY = %q (BOM must be stripped from first key)", values["FIRST_KEY"])
	}
	if values["LONG_KEY"] != longValue {
		t.Errorf("LONG_KEY length = %d, want %d (long line must not be truncated)", len(values["LONG_KEY"]), len(longValue))
	}
	if values["LAST_KEY"] != "last" {
		t.Errorf("LAST_KEY = %q (lines after a long line must be parsed)", values["LAST_KEY"])
	}
}
