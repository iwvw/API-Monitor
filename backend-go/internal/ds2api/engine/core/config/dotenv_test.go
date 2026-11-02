package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnvLoadsWorkingDirectoryFileWithoutOverridingExistingEnv(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	const newKey = "DS2API_TEST_DOTENV_NEW"
	const keepKey = "DS2API_TEST_DOTENV_KEEP"
	const quotedKey = "DS2API_TEST_DOTENV_QUOTED"

	unsetEnv(t, newKey)
	unsetEnv(t, quotedKey)
	t.Setenv(keepKey, "from-env")

	content := "DS2API_TEST_DOTENV_NEW=from-file\n" +
		"DS2API_TEST_DOTENV_KEEP=from-file\n" +
		"DS2API_TEST_DOTENV_QUOTED=\"line1\\nline2\"\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := LoadDotEnv(); err != nil {
		t.Fatalf("LoadDotEnv() error: %v", err)
	}

	if got := os.Getenv(newKey); got != "from-file" {
		t.Fatalf("expected %s from .env, got %q", newKey, got)
	}
	if got := os.Getenv(keepKey); got != "from-env" {
		t.Fatalf("expected existing env to win, got %q", got)
	}
	if got := os.Getenv(quotedKey); got != "line1\nline2" {
		t.Fatalf("expected quoted newline decoding, got %q", got)
	}
}

func TestLoadDotEnvIgnoresMissingFile(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	if err := LoadDotEnv(); err != nil {
		t.Fatalf("expected missing .env to be ignored, got %v", err)
	}
}

func TestLoadDotEnvStripsInlineCommentsFromUnquotedValues(t *testing.T) {
	dir := t.TempDir()
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	const plainKey = "DS2API_TEST_DOTENV_PLAIN"
	const hashKey = "DS2API_TEST_DOTENV_HASH"
	const quotedKey = "DS2API_TEST_DOTENV_QUOTED_COMMENT"
	const exportKey = "DS2API_TEST_DOTENV_EXPORT"

	unsetEnv(t, plainKey)
	unsetEnv(t, hashKey)
	unsetEnv(t, quotedKey)
	unsetEnv(t, exportKey)

	content := strings.Join([]string{
		plainKey + "=5001 # local",
		hashKey + "=5001#local",
		quotedKey + `="5001 # local" # keep the inner hash`,
		"export " + exportKey + "=enabled # exported",
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(content), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	if err := LoadDotEnv(); err != nil {
		t.Fatalf("LoadDotEnv() error: %v", err)
	}

	if got := os.Getenv(plainKey); got != "5001" {
		t.Fatalf("expected inline comment to be stripped, got %q", got)
	}
	if got := os.Getenv(hashKey); got != "5001#local" {
		t.Fatalf("expected hash without preceding whitespace to remain, got %q", got)
	}
	if got := os.Getenv(quotedKey); got != "5001 # local" {
		t.Fatalf("expected quoted value to preserve hash text, got %q", got)
	}
	if got := os.Getenv(exportKey); got != "enabled" {
		t.Fatalf("expected export syntax to load, got %q", got)
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("unset %s: %v", key, err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
			return
		}
		_ = os.Unsetenv(key)
	})
}
