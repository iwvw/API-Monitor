package adminai

import (
	"strings"
	"testing"
)

// 工具失败回喂的修正引导：参数类错误必须给可执行动作，审批等待不给（正常流程）。
func TestToolErrorHint(t *testing.T) {
	cases := []struct {
		name    string
		tool    string
		msg     string
		wantSub string // 期望出现的子串
		noHint  bool
	}{
		{"call_api 4xx → 契约引导", "call_api", "接口返回 HTTP 404: not found", "get_route", false},
		{"call_api 5xx → 临时故障", "call_api", "接口返回 HTTP 502: bad gateway", "临时故障", false},
		{"call_api 业务失败 → 业务校验", "call_api", "接口返回 HTTP 200 但业务失败: cron 格式错误", "业务校验未通过", false},
		{"call_api unsupported action → 直达正确通道", "call_api", "接口返回 HTTP 400 但业务失败: unsupported action", "/api/server/agent/command/{id}", false},
		{"审批等待 → 不加引导", "call_api", "写操作等待用户审批", "", true},
		{"未启用 → 不加引导", "call_api", "AI 写操作功能未启用", "", true},
		{"未知工具 → 提示用清单内工具", "no_such_tool", "未知工具: no_such_tool", "清单中的工具名", false},
		{"通用工具错误 → schema 引导", "memory_add", "参数解析失败", "工具 schema", false},
	}
	for _, c := range cases {
		got := toolErrorHint(c.tool, nil, c.msg)
		if strings.Contains(got, "[修正引导]") != !c.noHint {
			t.Fatalf("%s: hint 出现与否与预期不符: noHint=%v got=%q", c.name, c.noHint, got)
		}
		if c.noHint {
			continue
		}
		if !strings.Contains(got, c.wantSub) {
			t.Fatalf("%s: 期望包含 %q, got=%q", c.name, c.wantSub, got)
		}
		if !strings.HasPrefix(got, c.msg) {
			t.Fatalf("%s: 必须保留原始错误文本", c.name)
		}
	}
}

// 大列表结果：压缩后保 id/name、显式标注总条数，输出在预算内。
func TestSummarizeToolResultLargeList(t *testing.T) {
	var list []interface{}
	for i := 0; i < 1000; i++ {
		list = append(list, map[string]interface{}{
			"id":   strings.Repeat("z", 200) + string(rune('0'+i/100)) + string(rune('0'+(i%100)/10)) + string(rune('0'+i%10)),
			"name": "zone-001",
			"p1":   strings.Repeat("x", 300),
			"p2":   strings.Repeat("y", 300),
		})
	}
	out := summarizeToolResult(list)
	if len(out) > contentSizeLimit {
		t.Fatalf("summary 超过预算: %d > %d", len(out), contentSizeLimit)
	}
	if !strings.Contains(out, "[已截断：共 1000 条") {
		t.Fatalf("缺少总条数标注: %q...", out[:200])
	}
	if strings.Contains(out, "p1") {
		t.Fatalf("冗余大字段应被压缩省略: %q...", out[:200])
	}
	if !strings.Contains(out, `"id"`) {
		t.Fatalf("必须保留 id 标识字段: %q...", out[:200])
	}
}

// 大对象结果：值压缩保留标识键并标注省略，JSON 保持可解析。
func TestTrimToolJSONCompactEntry(t *testing.T) {
	entry := map[string]interface{}{
		"id":      "res-42",
		"name":    "我的资源",
		"details": strings.Repeat("d", contentSizeLimit),
	}
	out := summarizeToolResult(entry)
	if len(out) > contentSizeLimit+64 {
		t.Fatalf("summary 超过预算+标注余量: %d", len(out))
	}
	if !strings.Contains(out, `"id":"res-42"`) {
		t.Fatalf("必须保留 id: %q...", out[:200])
	}
	if !strings.Contains(out, "_truncated") {
		t.Fatalf("压缩必须显式标注: %q...", out[:200])
	}
}

// 小结果原样返回，不做任何加工。
func TestSummarizeToolResultSmall(t *testing.T) {
	got := summarizeToolResult(map[string]interface{}{"id": "a", "name": "b"})
	if !strings.Contains(got, `"id":"a"`) || strings.Contains(got, "[已截断") {
		t.Fatalf("小结果不应被加工: %q", got)
	}
}
