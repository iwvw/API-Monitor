package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteConfigBytesCreatesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := writeConfigBytes(path, []byte(`{"keys":["secret"]}`)); err != nil {
		t.Fatalf("write config: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if runtime.GOOS == "windows" {
		// Windows does not expose Unix permission bits; the write itself was
		// already verified above.
		return
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("expected config mode 0644, got %o", got)
	}
}
