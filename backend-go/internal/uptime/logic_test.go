package uptime

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTransitionState(t *testing.T) {
	baseMonitor := map[string]interface{}{"active": true}
	okResult := probeResult{OK: true, Status: stateUp, LatencyMS: 12, Message: "OK"}
	failResult := probeResult{OK: false, Status: stateDown, LatencyMS: 0, Message: "timeout"}

	cases := []struct {
		name        string
		previous    map[string]interface{}
		monitor     map[string]interface{}
		result      probeResult
		maintenance bool
		wantState   string
		wantAction  string
		wantFail    int
		wantRecover int
		wantError   interface{}
		wantPing    int64
	}{
		{"fresh start succeeds", map[string]interface{}{}, baseMonitor, okResult, false, stateUp, "", 0, 0, nil, 12},
		{"first failure pending", map[string]interface{}{}, map[string]interface{}{"active": true, "downConfirmCount": 3}, failResult, false, statePendingDown, "", 1, 0, "timeout", 0},
		{"third failure opens incident", map[string]interface{}{"state": statePendingDown, "fail_count": 2}, map[string]interface{}{"active": true, "downConfirmCount": 3}, failResult, false, stateDown, "open", 3, 0, "timeout", 0},
		{"instant failure with single confirm", map[string]interface{}{"state": stateUp}, map[string]interface{}{"active": true, "downConfirmCount": 1}, failResult, false, stateDown, "open", 1, 0, "timeout", 0},
		{"down recovery first step", map[string]interface{}{"state": stateDown, "failCount": 3}, map[string]interface{}{"active": true, "upConfirmCount": 3}, okResult, false, statePendingUp, "", 3, 1, nil, 12},
		{"recovery completes", map[string]interface{}{"state": statePendingUp, "recoverCount": 2, "failCount": 3}, map[string]interface{}{"active": true, "upConfirmCount": 3}, okResult, false, stateUp, "resolve", 0, 0, nil, 12},
		{"open incident recovers gradually", map[string]interface{}{"state": stateUp, "activeIncidentId": 9, "failCount": 1}, map[string]interface{}{"active": true, "upConfirmCount": 2}, okResult, false, statePendingUp, "", 1, 1, nil, 12},
		{"failure during maintenance stays maintenance", map[string]interface{}{"state": stateMaintenance}, baseMonitor, failResult, true, stateMaintenance, "", 0, 0, nil, 0},
		{"inactive monitor paused", map[string]interface{}{"state": stateUp}, map[string]interface{}{"active": false}, failResult, false, statePaused, "", 0, 0, nil, 0},
		{"unknown state counts failure", map[string]interface{}{"state": stateUnknown}, map[string]interface{}{"active": true, "downConfirmCount": 1}, failResult, false, stateDown, "open", 1, 0, "timeout", 0},
		{"default confirm counts to three", map[string]interface{}{}, baseMonitor, failResult, false, statePendingDown, "", 1, 0, "timeout", 0},
		{"zero confirm falls back to default", map[string]interface{}{}, map[string]interface{}{"active": true, "downConfirmCount": 0, "upConfirmCount": 0}, failResult, false, statePendingDown, "", 1, 0, "timeout", 0},
		{"pending down repeats failure", map[string]interface{}{"state": statePendingDown, "failCount": 1}, baseMonitor, failResult, false, statePendingDown, "", 2, 0, "timeout", 0},
		{"pending down reaches confirm", map[string]interface{}{"state": statePendingDown, "failCount": 2}, baseMonitor, failResult, false, stateDown, "open", 3, 0, "timeout", 0},
		{"pending up success resets", map[string]interface{}{"state": statePendingUp, "recoverCount": 1}, baseMonitor, okResult, false, statePendingUp, "", 0, 2, nil, 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next, action := transitionState(tc.previous, tc.monitor, tc.result, tc.maintenance)
			if got := stringValue(next["state"], ""); got != tc.wantState {
				t.Fatalf("state = %q want %q (next=%#v)", got, tc.wantState, next)
			}
			if action != tc.wantAction {
				t.Fatalf("action = %q want %q", action, tc.wantAction)
			}
			if got := intValue(next["failCount"], -1); got != tc.wantFail {
				t.Fatalf("failCount = %d want %d", got, tc.wantFail)
			}
			if got := intValue(next["recoverCount"], -1); got != tc.wantRecover {
				t.Fatalf("recoverCount = %d want %d", got, tc.wantRecover)
			}
			if tc.wantError == nil {
				if next["lastError"] != nil {
					t.Fatalf("lastError = %#v want nil", next["lastError"])
				}
			} else if got := stringValue(next["lastError"], ""); got != tc.wantError {
				t.Fatalf("lastError = %q want %q", got, tc.wantError)
			}
			if got := int64Value(next["lastPing"], -1); got != tc.wantPing {
				t.Fatalf("lastPing = %d want %d", got, tc.wantPing)
			}
		})
	}
}

func TestAcceptedStatus(t *testing.T) {
	cases := []struct {
		raw    string
		status int
		want   bool
	}{
		{"", 200, true},
		{"", 199, false},
		{"", 299, true},
		{"", 300, false},
		{"", 301, false},
		{"200", 200, true},
		{"200", 201, false},
		{"200, 404", 404, true},
		{"200, 404", 302, false},
		{"200-299", 250, true},
		{"200-299", 199, false},
		{"200-299", 300, false},
		{"201, 404-500", 201, true},
		{"201, 404-500", 450, true},
		{"201, 404-500", 200, false},
		{" 200 , 201 ", 201, true},
		{"200,,300", 300, true},
	}
	for _, tc := range cases {
		name := fmt.Sprintf("%q/%d", tc.raw, tc.status)
		t.Run(name, func(t *testing.T) {
			if got := acceptedStatus(tc.raw, tc.status); got != tc.want {
				t.Fatalf("acceptedStatus(%q, %d) = %v want %v", tc.raw, tc.status, got, tc.want)
			}
		})
	}
}

func TestJSONPathValue(t *testing.T) {
	source := map[string]interface{}{
		"a":     map[string]interface{}{"b": 42},
		"users": []interface{}{map[string]interface{}{"name": "alice"}, map[string]interface{}{"name": "bob"}},
		"tags":  []interface{}{"x", "y"},
		"plain": "value",
	}
	cases := []struct {
		path string
		want interface{}
		ok   bool
	}{
		{"", source, true},
		{"$", source, true},
		{"$.a.b", 42, true},
		{"$.users[0].name", "alice", true},
		{"$.users[1].name", "bob", true},
		{"$.tags[1]", "y", true},
		{"$.missing", nil, false},
		{"$.a.b.c", nil, false},
		{"$.users[9].name", nil, false},
		{"$.users[-1].name", nil, false},
		{"$.a..b", 42, true},
		{"$.plain", "value", true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := jsonPathValue(source, tc.path)
			if tc.ok && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("jsonPathValue(%q) = %#v want %#v", tc.path, got, tc.want)
			}
			if !tc.ok && got != nil {
				t.Fatalf("jsonPathValue(%q) = %#v want nil", tc.path, got)
			}
		})
	}
}

func TestJSONPathValueScalarSourceFails(t *testing.T) {
	if got := jsonPathValue("not-a-map", "$.a"); got != nil {
		t.Fatalf("scalar source path traversal = %#v want nil", got)
	}
	if got := jsonPathValue([]interface{}{1, 2}, "$.first"); got != nil {
		t.Fatalf("array source object lookup = %#v want nil", got)
	}
}

func TestCompareValues(t *testing.T) {
	cases := []struct {
		actual   interface{}
		expected interface{}
		operator string
		want     bool
	}{
		{"10", 10, "equals", true},
		{"10", 10, "eq", true},
		{"10", 10, "", true},
		{"10", 11, "equals", false},
		{"10", 11, "not_equals", true},
		{"10", 10, "notEquals", false},
		{"10", 10, "ne", false},
		{"abcdef", "cd", "contains", true},
		{"abcdef", "xyz", "contains", false},
		{"abcdef", "cd", "not_contains", false},
		{"abcdef", "xyz", "notContains", true},
		{"12.5", "10", "gt", true},
		{"10", "10", "gt", false},
		{12.5, 10, "greater_than", true},
		{"10", "10", "gte", true},
		{"9", "10", "greater_or_equal", false},
		{"5", "10", "lt", true},
		{"10", "5", "less_than", false},
		{"10", "10", "lte", true},
		{"11", "10", "less_or_equal", false},
		{42, nil, "exists", true},
		{nil, nil, "exists", false},
		{nil, nil, "not_exists", true},
		{42, nil, "notExists", false},
		{"true", true, "equals", true},
		{"unknown-op", "unknown-op", "bogus", true},
		{"10", 8, "bogus", false},
	}
	for _, tc := range cases {
		name := fmt.Sprintf("%v %s %v", tc.actual, tc.operator, tc.expected)
		t.Run(name, func(t *testing.T) {
			if got := compareValues(tc.actual, tc.expected, tc.operator); got != tc.want {
				t.Fatalf("compareValues(%#v, %#v, %q) = %v want %v", tc.actual, tc.expected, tc.operator, got, tc.want)
			}
		})
	}
}

func TestNormalizeMonitorConfig(t *testing.T) {
	got := normalizeMonitorConfig(map[string]interface{}{
		"config_json":       `{"jsonQueryPath":"$.a","timeout":5}`,
		"config":            map[string]interface{}{"jsonQueryOperator": "gte", "dropEmpty": ""},
		"jsonExpectedValue": "42",
		"pushGraceSeconds":  90,
	}, map[string]interface{}{"existingKey": "keep"})
	checks := map[string]interface{}{
		"existingKey":        "keep",
		"jsonQueryPath":      "$.a",
		"timeout":            float64(5),
		"jsonQueryOperator":  "gte",
		"jsonExpectedValue":  "42",
		"graceSeconds":       90,
		"dropEmpty":          nil,
	}
	for key, want := range checks {
		if got[key] != want {
			t.Fatalf("normalizeMonitorConfig[%q] = %#v want %#v", key, got[key], want)
		}
	}

	empty := normalizeMonitorConfig(map[string]interface{}{}, nil)
	if len(empty) != 0 {
		t.Fatalf("empty config should normalize to empty map, got %#v", empty)
	}
	pruned := normalizeMonitorConfig(map[string]interface{}{"config": map[string]interface{}{"keep": "v", "drop": ""}}, nil)
	if pruned["keep"] != "v" {
		t.Fatalf("non-empty values should survive pruning: %#v", pruned)
	}
	if _, exists := pruned["drop"]; exists {
		t.Fatalf("empty values should be pruned: %#v", pruned)
	}
}

func TestShouldUpdateConfig(t *testing.T) {
	cases := []struct {
		data map[string]interface{}
		want bool
	}{
		{map[string]interface{}{"config": map[string]interface{}{}}, true},
		{map[string]interface{}{"config_json": "{}"}, true},
		{map[string]interface{}{"jsonQueryPath": "$.a"}, true},
		{map[string]interface{}{"jsonQueryOperator": "gte"}, true},
		{map[string]interface{}{"jsonExpectedValue": "1"}, true},
		{map[string]interface{}{"expectedValue": "1"}, true},
		{map[string]interface{}{"pushGraceSeconds": 5}, true},
		{map[string]interface{}{"push_grace_seconds": 5}, true},
		{map[string]interface{}{"name": "x"}, false},
		{map[string]interface{}{}, false},
		{nil, false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%#v", tc.data), func(t *testing.T) {
			if got := shouldUpdateConfig(tc.data); got != tc.want {
				t.Fatalf("shouldUpdateConfig(%#v) = %v want %v", tc.data, got, tc.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "0s"},
		{1000, "1s"},
		{59999, "59s"},
		{60000, "1m0s"},
		{65000, "1m5s"},
		{3599999, "59m59s"},
		{3600000, "1h0m"},
		{90000000, "25h0m"},
		{3661000, "1h1m"},
	}
	for _, tc := range cases {
		name := fmt.Sprintf("%d", tc.ms)
		t.Run(name, func(t *testing.T) {
			if got := formatDuration(tc.ms); got != tc.want {
				t.Fatalf("formatDuration(%d) = %q want %q", tc.ms, got, tc.want)
			}
		})
	}
}

func TestCalculateUptime(t *testing.T) {
	service, _ := testService(t, true)
	ctx := context.Background()
	db, err := service.open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	formatTime := func(at time.Time) string {
		return at.UTC().Format(time.RFC3339Nano)
	}
	hourAgo := formatTime(now.Add(-time.Hour))
	twoHoursAgo := formatTime(now.Add(-2 * time.Hour))
	threeHoursAgo := formatTime(now.Add(-3 * time.Hour))
	hourAhead := formatTime(now.Add(time.Hour))
	nearNow := formatTime(now.Add(-100 * time.Millisecond))

	cases := []struct {
		name      string
		days      int
		started   string
		resolved  string
		want      string
		wantStrict bool
	}{
		{"no incidents", 1, "", "", "100.000", true},
		{"zero days defaults to one", 0, "", "", "100.000", true},
		{"full range outage clamped to window", 1, formatTime(now.Add(-48 * time.Hour)), nearNow, "0.000", true},
		{"one hour outage", 1, threeHoursAgo, twoHoursAgo, "95.833", true},
		{"open incident clamped to now", 1, hourAgo, "", "95.83", false},
		{"future resolved clamped to now", 1, hourAgo, hourAhead, "95.833", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := db.Exec(`INSERT INTO uptime_monitors (name, type, url) VALUES ('probe-target', 'http', 'http://127.0.0.1:9')`)
			if err != nil {
				t.Fatal(err)
			}
			monitorID, _ := result.LastInsertId()
			if tc.started != "" {
				var resolvedValue interface{}
				if tc.resolved != "" {
					resolvedValue = tc.resolved
				}
				if _, err := db.Exec(`INSERT INTO uptime_incidents (monitor_id, started_at, resolved_at) VALUES (?, ?, ?)`, monitorID, tc.started, resolvedValue); err != nil {
					t.Fatalf("insert incident: %v", err)
				}
			}
			got, err := calculateUptime(ctx, db, monitorID, tc.days)
			if err != nil {
				t.Fatalf("calculateUptime: %v", err)
			}
			if tc.wantStrict {
				if got != tc.want {
					t.Fatalf("calculateUptime(days=%d) = %q want %q", tc.days, got, tc.want)
				}
			} else if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("calculateUptime(days=%d) = %q want prefix %q", tc.days, got, tc.want)
			}
		})
	}
}