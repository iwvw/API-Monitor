//go:build 386 || arm || mips || mipsle || wasm

package util

func countWithTokenizer(_, _ string) int {
	return 0
}
