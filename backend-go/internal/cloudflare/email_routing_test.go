package cloudflare

import "testing"

func TestMapEmailRuleForward(t *testing.T) {
	rule := map[string]interface{}{
		"id":      "rule_1",
		"name":    "",
		"enabled": true,
		"matchers": []interface{}{
			map[string]interface{}{"type": "all"},
		},
		"actions": []interface{}{
			map[string]interface{}{"type": "forward", "value": []interface{}{"a@example.com"}},
		},
		"priority": float64(2147483647),
	}
	out := mapEmailRule(rule)
	if out["matcher"] != "全部匹配" {
		t.Fatalf("expected 全部匹配, got %v", out["matcher"])
	}
	if out["matcherValue"] != "" {
		t.Fatalf("expected empty matcherValue, got %v", out["matcherValue"])
	}
	actions, ok := out["actions"].([]string)
	if !ok || len(actions) != 1 || actions[0] != "转发至 a@example.com" {
		t.Fatalf("unexpected actions: %#v", out["actions"])
	}
	if out["enabled"] != true {
		t.Fatalf("expected enabled true, got %v", out["enabled"])
	}
}

func TestMapEmailRuleLiteralAndWorker(t *testing.T) {
	rule := map[string]interface{}{
		"id":   "rule_2",
		"name": "verify",
		"matchers": []interface{}{
			map[string]interface{}{"type": "literal", "value": "code@dmuk.org"},
		},
		"actions": []interface{}{
			map[string]interface{}{"type": "worker", "value": []interface{}{"posthog-code-verifier"}},
		},
		"enabled": false,
	}
	out := mapEmailRule(rule)
	if out["matcher"] != "收件人精确匹配" {
		t.Fatalf("unexpected matcher: %v", out["matcher"])
	}
	if out["matcherValue"] != "code@dmuk.org" {
		t.Fatalf("unexpected matcherValue: %v", out["matcherValue"])
	}
	actions := out["actions"].([]string)
	if len(actions) != 1 || actions[0] != "发往 Worker posthog-code-verifier" {
		t.Fatalf("unexpected actions: %#v", actions)
	}
	if out["enabled"] != false {
		t.Fatalf("expected enabled false, got %v", out["enabled"])
	}
}

func TestMapAddressVerification(t *testing.T) {
	verified := mapAddress(map[string]interface{}{
		"id":       "addr_1",
		"email":    "a@example.com",
		"verified": "2026-07-02T23:43:51.076781Z",
		"created":  "2026-07-02T23:43:51.076781Z",
	})
	if verified["verified"] != true {
		t.Fatalf("expected verified true, got %v", verified["verified"])
	}
	if verified["verifiedAt"] != "2026-07-02T23:43:51.076781Z" {
		t.Fatalf("unexpected verifiedAt: %v", verified["verifiedAt"])
	}

	pending := mapAddress(map[string]interface{}{
		"id":       "addr_2",
		"email":    "b@example.com",
		"verified": "pending",
	})
	if pending["verified"] != false {
		t.Fatalf("expected verified false, got %v", pending["verified"])
	}
	if pending["verifiedAt"] != "" {
		t.Fatalf("expected empty verifiedAt, got %v", pending["verifiedAt"])
	}
}

func TestZoneNameByID(t *testing.T) {
	zones := []interface{}{
		map[string]interface{}{"id": "zone_1", "name": "a.example.com"},
		map[string]interface{}{"id": "zone_2", "name": "dmuk.org"},
	}
	if got := zoneNameByID(zones, "zone_2"); got != "dmuk.org" {
		t.Fatalf("expected dmuk.org, got %q", got)
	}
	if got := zoneNameByID(zones, "missing"); got != "" {
		t.Fatalf("expected empty for missing, got %q", got)
	}
}
