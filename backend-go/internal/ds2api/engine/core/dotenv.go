package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv loads environment variables from .env in the current working
// directory without overriding variables that are already set.
func LoadDotEnv() error {
	return loadDotEnvFromPath(filepath.Join(BaseDir(), ".env"))
}

func loadDotEnvFromPath(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	for i, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if i == 0 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d invalid env assignment", path, i+1)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("%s:%d empty env key", path, i+1)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, normalizeDotEnvValue(trimDotEnvValue(strings.TrimSpace(value)))); err != nil {
			return fmt.Errorf("%s:%d set env %q: %w", path, i+1, key, err)
		}
	}

	return nil
}

// Preserve quoted values, but drop Compose-style inline comments from unquoted values.
func trimDotEnvValue(raw string) string {
	if raw == "" {
		return raw
	}

	switch raw[0] {
	case '"':
		if trimmed, ok := trimQuotedDotEnvValue(raw, '"'); ok {
			return trimmed
		}
	case '\'':
		if trimmed, ok := trimQuotedDotEnvValue(raw, '\''); ok {
			return trimmed
		}
	default:
		if idx := inlineDotEnvCommentStart(raw); idx >= 0 {
			return strings.TrimSpace(raw[:idx])
		}
	}

	return raw
}

func trimQuotedDotEnvValue(raw string, quote byte) (string, bool) {
	escaped := false
	for i := 1; i < len(raw); i++ {
		ch := raw[i]
		if quote == '"' && escaped {
			escaped = false
			continue
		}
		if quote == '"' && ch == '\\' {
			escaped = true
			continue
		}
		if ch == quote {
			return strings.TrimSpace(raw[:i+1]), true
		}
	}
	return raw, false
}

func inlineDotEnvCommentStart(raw string) int {
	for i := 1; i < len(raw); i++ {
		if raw[i] == '#' && isDotEnvCommentSpacer(raw[i-1]) {
			return i
		}
	}
	return -1
}

func isDotEnvCommentSpacer(b byte) bool {
	return b == ' ' || b == '\t'
}

func normalizeDotEnvValue(raw string) string {
	if len(raw) < 2 {
		return raw
	}
	first := raw[0]
	last := raw[len(raw)-1]
	if (first != '"' || last != '"') && (first != '\'' || last != '\'') {
		return raw
	}

	raw = raw[1 : len(raw)-1]
	if first == '\'' {
		return raw
	}

	replacer := strings.NewReplacer(
		`\\`, `\`,
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
		`\"`, `"`,
	)
	return replacer.Replace(raw)
}
