package notification

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEvaluateConditions(t *testing.T) {
	data := map[string]interface{}{
		"monitorName": "API Gateway",
		"server":      map[string]interface{}{"tier": "edge"},
		"cpu":         85,
	}
	cases := []struct {
		name        string
		conditions  map[string]interface{}
		wantAllowed bool
		wantMode    string
	}{
		{"empty conditions allow", map[string]interface{}{}, true, "all"},
		{"flat field equals", map[string]interface{}{"monitorName": "API Gateway"}, true, "all"},
		{"flat field mismatch", map[string]interface{}{"monitorName": "Other"}, false, "all"},
		{"nested path operator", map[string]interface{}{"items": []interface{}{
			map[string]interface{}{"field": "$.server.tier", "operator": "equals", "value": "edge"},
		}}, true, "all"},
		{"all mode one failure", map[string]interface{}{"mode": "all", "items": []interface{}{
			map[string]interface{}{"field": "monitorName", "operator": "equals", "value": "API Gateway"},
			map[string]interface{}{"field": "cpu", "operator": "gt", "value": 90},
		}}, false, "all"},
		{"any mode one pass", map[string]interface{}{"mode": "any", "items": []interface{}{
			map[string]interface{}{"field": "cpu", "operator": "gt", "value": 90},
			map[string]interface{}{"field": "monitorName", "operator": "equals", "value": "API Gateway"},
		}}, true, "any"},
		{"or mode all fail", map[string]interface{}{"mode": "or", "items": []interface{}{
			map[string]interface{}{"field": "cpu", "operator": "gt", "value": 90},
			map[string]interface{}{"field": "monitorName", "operator": "equals", "value": "Other"},
		}}, false, "any"},
		{"rules alias", map[string]interface{}{"rules": []interface{}{
			map[string]interface{}{"field": "cpu", "operator": "gte", "value": 85},
		}}, true, "all"},
		{"conditions alias", map[string]interface{}{"conditions": []interface{}{
			map[string]interface{}{"field": "cpu", "operator": "lte", "value": 100},
		}}, true, "all"},
		{"contains operator", map[string]interface{}{"items": []interface{}{
			map[string]interface{}{"field": "monitorName", "operator": "contains", "value": "API"},
		}}, true, "all"},
		{"exists operator missing field", map[string]interface{}{"items": []interface{}{
			map[string]interface{}{"field": "missing", "operator": "exists"},
		}}, false, "all"},
		{"not_equals operator", map[string]interface{}{"items": []interface{}{
			map[string]interface{}{"field": "monitorName", "operator": "not_equals", "value": "Other"},
		}}, true, "all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := evaluateConditions(tc.conditions, data)
			if result.Allowed != tc.wantAllowed {
				t.Fatalf("Allowed = %v want %v (result=%#v)", result.Allowed, tc.wantAllowed, result)
			}
			if result.Mode != tc.wantMode {
				t.Fatalf("Mode = %q want %q", result.Mode, tc.wantMode)
			}
		})
	}

	detailed := evaluateConditions(map[string]interface{}{"mode": "any", "items": []interface{}{
		map[string]interface{}{"field": "cpu", "operator": "gt", "value": 90},
		map[string]interface{}{"field": "monitorName", "operator": "equals", "value": "API Gateway"},
	}}, data)
	if len(detailed.Results) != 2 {
		t.Fatalf("results length = %d want 2: %#v", len(detailed.Results), detailed.Results)
	}
	if detailed.Results[0]["passed"] != false || detailed.Results[1]["passed"] != true {
		t.Fatalf("unexpected per-item results: %#v", detailed.Results)
	}
	if detailed.Results[0]["field"] != "cpu" || detailed.Results[0]["operator"] != "gt" {
		t.Fatalf("unexpected result metadata: %#v", detailed.Results[0])
	}
}

func TestCompareOperators(t *testing.T) {
	cases := []struct {
		name     string
		actual   interface{}
		expected interface{}
		operator string
		want     bool
	}{
		{"default equals int and string", "10", 10, "", true},
		{"equals falls through to sprint compare", "10", 10, "equals", true},
		{"equals same string", "abc", "abc", "equals", true},
		{"not_equals", "10", 11, "not_equals", true},
		{"notEquals alias", "10", 10, "notEquals", false},
		{"ne alias", "10", 11, "ne", true},
		{"contains", "abcdef", "cd", "contains", true},
		{"contains miss", "abcdef", "xyz", "contains", false},
		{"not_contains", "abcdef", "xyz", "not_contains", true},
		{"notContains alias", "abcdef", "cd", "notContains", false},
		{"gt numeric strings", "12.5", "10", "gt", true},
		{"gt equal", "10", "10", "gt", false},
		{"greater_than alias", "12", 10, "greater_than", true},
		{"greaterThan alias", 12, 10, "greaterThan", true},
		{"gte equal", "10", "10", "gte", true},
		{"greater_or_equal alias", "10", 10, "greater_or_equal", true},
		{"lt", "5", "10", "lt", true},
		{"lt equal", "10", "10", "lt", false},
		{"less_than alias", "5", 10, "less_than", true},
		{"lessThan alias", 5, 10, "lessThan", true},
		{"lte equal", "10", "10", "lte", true},
		{"less_or_equal alias", "10", 10, "less_or_equal", true},
		{"exists set", 42, nil, "exists", true},
		{"exists nil", nil, nil, "exists", false},
		{"not_exists nil", nil, nil, "not_exists", true},
		{"not_exists set", 42, nil, "not_exists", false},
		{"notExists alias", nil, nil, "notExists", true},
		{"non-numeric gt falls back to zero", "abc", "10", "gt", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := compare(tc.actual, tc.expected, tc.operator); got != tc.want {
				t.Fatalf("compare(%#v, %#v, %q) = %v want %v", tc.actual, tc.expected, tc.operator, got, tc.want)
			}
		})
	}
}

func TestCheckTimeWindow(t *testing.T) {
	now := time.Now().In(time.UTC)
	current := now.Hour()*60 + now.Minute()
	clock := func(minutes int) string {
		minutes = ((minutes % 1440) + 1440) % 1440
		return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
	}
	cases := []struct {
		name   string
		window map[string]interface{}
		want   bool
	}{
		{"disabled window always allows", map[string]interface{}{"enabled": false}, true},
		{"missing enabled defaults disabled", map[string]interface{}{}, true},
		{"enabled with defaults covers whole day", map[string]interface{}{"enabled": true}, true},
		{"invalid start falls through", map[string]interface{}{"enabled": true, "start": "oops"}, true},
		{"invalid end falls through", map[string]interface{}{"enabled": true, "start": "00:00", "end": "25:99"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := checkTimeWindow(tc.window, time.UTC); got != tc.want {
				t.Fatalf("checkTimeWindow(%#v) = %v want %v", tc.window, got, tc.want)
			}
		})
	}
	if current < 30 || current > 1380 {
		t.Skipf("current time %02d:%02d too close to midnight boundary for relative cases", now.Hour(), now.Minute())
	}
	relative := []struct {
		name  string
		start int
		end   int
		want  bool
	}{
		{"point window covers now", current, current, true},
		{"forward window excludes now", current + 1, current + 30, false},
		{"forward window includes now", current - 30, current, true},
		{"midnight crossing covers end side", current + 1, current, true},
		{"midnight crossing covers start side", current - 30, current - 31, true},
		{"midnight crossing excludes both sides", current + 30, current - 30, false},
	}
	for _, tc := range relative {
		t.Run(tc.name, func(t *testing.T) {
			if now.Hour()*60+now.Minute() != current {
				t.Skipf("minute boundary crossed while building windows")
			}
			window := map[string]interface{}{"enabled": true, "start": clock(tc.start), "end": clock(tc.end)}
			if got := checkTimeWindow(window, time.UTC); got != tc.want {
				t.Fatalf("checkTimeWindow(%v) = %v want %v", window, got, tc.want)
			}
		})
	}
}

func TestGenerateFingerprint(t *testing.T) {
	rule := Rule{SourceModule: "uptime", EventType: "down"}
	serverRule := Rule{SourceModule: "server", EventType: "online"}
	accountRule := Rule{SourceModule: "github", EventType: "action_failed"}
	systemRule := Rule{SourceModule: "system", EventType: "cpu_high"}
	cases := []struct {
		name string
		rule Rule
		data map[string]interface{}
		want string
	}{
		{"monitor id", rule, map[string]interface{}{"monitorId": 7}, "uptime:down:monitor:7"},
		{"monitor id string", rule, map[string]interface{}{"monitorId": "m-1"}, "uptime:down:monitor:m-1"},
		{"server id", serverRule, map[string]interface{}{"serverId": "srv-1"}, "server:online:server:srv-1"},
		{"account id", accountRule, map[string]interface{}{"accountId": "acc-9"}, "github:action_failed:account:acc-9"},
		{"no id global", systemRule, map[string]interface{}{"cpu": 90}, "system:cpu_high:global"},
		{"monitor id wins over server id", rule, map[string]interface{}{"monitorId": 1, "serverId": "srv-1"}, "uptime:down:monitor:1"},
		{"empty data global", systemRule, map[string]interface{}{}, "system:cpu_high:global"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := generateFingerprint(tc.rule, tc.data); got != tc.want {
				t.Fatalf("generateFingerprint() = %q want %q", got, tc.want)
			}
		})
	}
}

func TestTelegramEscapeV2(t *testing.T) {
	for _, r := range "\\_*[]()~`>#+-=|{}.!" {
		if got := telegramEscapeV2(string(r)); got != "\\"+string(r) {
			t.Fatalf("telegramEscapeV2(%q) = %q want %q", r, got, "\\"+string(r))
		}
	}
	plain := "abc 中文 emoji 🌐 123"
	if got := telegramEscapeV2(plain); got != plain {
		t.Fatalf("plain text should be unchanged, got %q", got)
	}
	if got := telegramEscapeV2("a_b*c"); got != "a\\_b\\*c" {
		t.Fatalf("telegramEscapeV2(\"a_b*c\") = %q", got)
	}
	if got := telegramEscapeV2(""); got != "" {
		t.Fatalf("empty input = %q", got)
	}
}

func TestTelegramEscapeCode(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"a`b", "a\\`b"},
		{"back\\slash", "back\\\\slash"},
		{"`both\\`", "\\`both\\\\\\`"},
		{"plain *bold_", "plain *bold_"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := telegramEscapeCode(tc.in); got != tc.want {
			t.Fatalf("telegramEscapeCode(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseNotificationMessageLine(t *testing.T) {
	longLabel := strings.Repeat("长", 33) + ": value"
	cases := []struct {
		line   string
		label  string
		value  string
		empty  bool
	}{
		{"", "", "", true},
		{"   ", "", "", true},
		{"plain text line", "", "plain text line", false},
		{"状态: 在线", "状态", "在线", false},
		{"状态：在线", "状态", "在线", false},
		{"地址: https://example.com/x?a=1", "地址", "https://example.com/x?a=1", false},
		{"label: value padded  ", "label", "value padded", false},
		{"a:b：c", "a", "b：c", false},
		{longLabel, "", longLabel, false},
		{":leading colon", "", ":leading colon", false},
		{" :leading colon", "", ":leading colon", false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q", tc.line), func(t *testing.T) {
			field := parseNotificationMessageLine(tc.line)
			if field.Empty != tc.empty {
				t.Fatalf("Empty = %v want %v (field=%#v)", field.Empty, tc.empty, field)
			}
			if !tc.empty {
				if field.Label != tc.label || field.Value != tc.value {
					t.Fatalf("field = %#v want label=%q value=%q", field, tc.label, tc.value)
				}
			}
		})
	}
}

func TestNotificationStatusIcon(t *testing.T) {
	online := []string{"online", "up", "recovered", "success", "在线", "恢复", "已恢复", "成功", " Online ", "UP"}
	offline := []string{"offline", "down", "failed", "failure", "离线", "故障", "失败", "Down"}
	warning := []string{"interrupted", "degraded", "warning", "中断", "采集异常", "告警", "警告"}
	for _, value := range online {
		if got := notificationStatusIcon(value); got != "🟢 " {
			t.Fatalf("notificationStatusIcon(%q) = %q want green", value, got)
		}
	}
	for _, value := range offline {
		if got := notificationStatusIcon(value); got != "🔴 " {
			t.Fatalf("notificationStatusIcon(%q) = %q want red", value, got)
		}
	}
	for _, value := range warning {
		if got := notificationStatusIcon(value); got != "🟠 " {
			t.Fatalf("notificationStatusIcon(%q) = %q want orange", value, got)
		}
	}
	for _, value := range []string{"", "unknown", "partial", "maintenance"} {
		if got := notificationStatusIcon(value); got != "" {
			t.Fatalf("notificationStatusIcon(%q) = %q want empty", value, got)
		}
	}
}

func TestTelegramLifecycleRefreshDue(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name      string
		updatedAt string
		want      bool
	}{
		{"unparseable is due", "not-a-time", true},
		{"empty string is due", "", true},
		{"well under interval", now.UTC().Add(-10 * time.Second).Format(time.RFC3339), false},
		{"exactly at interval", now.UTC().Add(-30 * time.Second).Format(time.RFC3339), true},
		{"over interval", now.UTC().Add(-31 * time.Second).Format(time.RFC3339), true},
		{"future timestamp not due", now.UTC().Add(time.Hour).Format(time.RFC3339), false},
		{"sqlite datetime format over interval", now.UTC().Add(-61 * time.Second).Format("2006-01-02 15:04:05"), true},
		{"sqlite datetime format under interval", now.UTC().Add(-5 * time.Second).Format("2006-01-02 15:04:05"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := telegramLifecycleRefreshDue(tc.updatedAt, now); got != tc.want {
				t.Fatalf("telegramLifecycleRefreshDue(%q, now) = %v want %v", tc.updatedAt, got, tc.want)
			}
		})
	}
}