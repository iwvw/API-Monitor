package serveragent

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf16"
)

// decodeEncodedCommand 解开 -EncodedCommand 的 UTF-16LE base64，用于断言改写结果。
func decodeEncodedCommand(command string) (string, bool) {
	idx := strings.LastIndex(command, "-EncodedCommand ")
	if idx < 0 {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(command[idx+len("-EncodedCommand "):]))
	if err != nil {
		return "", false
	}
	units := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		units = append(units, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	return string(utf16.Decode(units)), true
}

// mustDecodeEncodedScript 断言命令已被改写为 -EncodedCommand 形式并返回解码后的脚本。
func mustDecodeEncodedScript(t *testing.T, command string) string {
	t.Helper()
	script, ok := decodeEncodedCommand(command)
	if !ok {
		t.Fatalf("expected -EncodedCommand form, got: %s", command)
	}
	return script
}

// TestNormalizeExecCommandPowerShellWithQuotesAndPipe 锁住改写初衷：管道与引号
// 经 base64 下发，避免 cmd /C 把元字符拆坏。
func TestNormalizeExecCommandPowerShellWithQuotesAndPipe(t *testing.T) {
	in := `powershell -NoProfile -Command "Get-Process api-monitor-agent | Select-Object Id | Format-List"`
	out := normalizeExecCommand(in)
	script := mustDecodeEncodedScript(t, out)
	if want := `Get-Process api-monitor-agent | Select-Object Id | Format-List`; script != want {
		t.Fatalf("script = %q, want %q", script, want)
	}
	if !strings.HasPrefix(out, "powershell -NoProfile -NonInteractive -OutputFormat Text -ExecutionPolicy Bypass -EncodedCommand ") {
		t.Fatalf("unexpected invocation: %s", out)
	}
	if !strings.Contains(out, "-OutputFormat Text") {
		t.Fatalf("应有 -OutputFormat Text 抑制 CLIXML 噪声: %s", out)
	}
}

// TestNormalizeExecCommandPowerShellBareTokens 无引号的裸 token 脚本。
func TestNormalizeExecCommandPowerShellBareTokens(t *testing.T) {
	script := mustDecodeEncodedScript(t, normalizeExecCommand(`powershell -NoProfile -Command Get-Date -Format yyyy-MM-dd`))
	if want := `Get-Date -Format yyyy-MM-dd`; script != want {
		t.Fatalf("script = %q, want %q", script, want)
	}
}

// TestNormalizeExecCommandChineseScript 中文脚本需按 UTF-16LE 正确编码。
func TestNormalizeExecCommandChineseScript(t *testing.T) {
	script := mustDecodeEncodedScript(t, normalizeExecCommand(`powershell -Command "Write-Host '中文测试' | Out-Host"`))
	if want := `Write-Host '中文测试' | Out-Host`; script != want {
		t.Fatalf("script = %q, want %q", script, want)
	}
}

// TestNormalizeExecCommandNonPowerShellUnchanged 非 PowerShell 前缀必须原样透传。
func TestNormalizeExecCommandNonPowerShellUnchanged(t *testing.T) {
	cases := []string{
		`echo hello`,
		`dir %TEMP%`,
		`tasklist | findstr api-monitor`,
		`powershell -File C:\x.ps1`, // 无 -Command，保持原样
	}
	for _, in := range cases {
		if out := normalizeExecCommand(in); out != in {
			t.Fatalf("command %q was rewritten to %q", in, out)
		}
	}
}

// TestNormalizeExecCommandPwshShortC pwsh 与 -c 短参数同样要改写。
func TestNormalizeExecCommandPwshShortC(t *testing.T) {
	script := mustDecodeEncodedScript(t, normalizeExecCommand(`pwsh -c "Get-Process | Select Id"`))
	if want := `Get-Process | Select Id`; script != want {
		t.Fatalf("script = %q, want %q", script, want)
	}
}

// TestNormalizeExecCommandEscapedQuotesInsideScript 回归：脚本内出现 \" 时，
// 旧实现只找第一个结尾引号，会误判外壳未闭合而整串保留引号，
// 导致 PowerShell 把脚本当成字符串字面量原样输出（静默不执行）。
func TestNormalizeExecCommandEscapedQuotesInsideScript(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   `powershell -NoProfile -Command "1..200 | ForEach-Object { \"L$_\" }"`,
			want: `1..200 | ForEach-Object { "L$_" }`,
		},
		{
			in:   `powershell -Command "Get-ChildItem | ForEach-Object { \"$($_.Name)\" }"`,
			want: `Get-ChildItem | ForEach-Object { "$($_.Name)" }`,
		},
	}
	for _, c := range cases {
		if got := mustDecodeEncodedScript(t, normalizeExecCommand(c.in)); got != c.want {
			t.Fatalf("in %q\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// TestNormalizeExecCommandPreservesInnerSpacing 回归：引号内连续空白不能被压缩。
func TestNormalizeExecCommandPreservesInnerSpacing(t *testing.T) {
	in := `powershell -Command "$a=1;  echo   '$a'"`
	if got := mustDecodeEncodedScript(t, normalizeExecCommand(in)); got != `$a=1;  echo   '$a'` {
		t.Fatalf("inner spacing lost: %q", got)
	}
}

// TestNormalizeExecCommandNonASCIIArgs 大写折叠不得改变字节位置（'İ' 折叠后长度变化）。
func TestNormalizeExecCommandNonASCIIArgs(t *testing.T) {
	in := `powershell -NoProfile -Command "Write-Host 'İstanbul 测试'"`
	if got := mustDecodeEncodedScript(t, normalizeExecCommand(in)); got != `Write-Host 'İstanbul 测试'` {
		t.Fatalf("script = %q", got)
	}
}

// TestNormalizeExecCommandDoubledQuoteEscape PowerShell 的 "" 双写转义。
func TestNormalizeExecCommandDoubledQuoteEscape(t *testing.T) {
	in := `powershell -Command "Write-Host ""hello world"""`
	got := mustDecodeEncodedScript(t, normalizeExecCommand(in))
	if !strings.Contains(got, `hello world`) {
		t.Fatalf("script = %q", got)
	}
}

// TestAllowDangerousFromAdminAI 锁住「完全批准模式下危险命令放行」的安全边界：
// 只有经服务端内部 ai_caller 写入 request context 的标记才放行；普通 HTTP 请求
// （无该 context 值）一律不放行，杜绝外部登录用户伪造头绕过危险命令拦截。
func TestAllowDangerousFromAdminAI(t *testing.T) {
	s := &Service{}
	plainReq := httptest.NewRequest("POST", "/api/server/agent/command/x", nil)
	fullReq := httptest.NewRequest("POST", "/api/server/agent/command/x", nil).WithContext(WithAdminAIFullApprove(context.Background()))

	if s.allowDangerousFromAdminAI(plainReq) {
		t.Errorf("普通 HTTP 请求不应放行危险命令")
	}
	if !s.allowDangerousFromAdminAI(fullReq) {
		t.Errorf("管理 AI 完全批准 context 应放行危险命令")
	}

	// context 值不可由 HTTP 头伪造：即使外部携带 X-AI-Agent / X-Admin-AI-Full-Approve，
	// 也不会让普通请求获得放行（这些头只是透传，不写入 context）。
	spoofReq := httptest.NewRequest("POST", "/api/server/agent/command/x", nil)
	spoofReq.Header.Set("X-AI-Agent", "api-monitor")
	spoofReq.Header.Set("X-Admin-AI-Full-Approve", "true")
	if s.allowDangerousFromAdminAI(spoofReq) {
		t.Errorf("伪造头部不应放行危险命令")
	}
}
