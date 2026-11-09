package raccoon

import (
	"strings"
	"testing"
)

// TestOnboardingStatusFromLedger 验证「已结算」完全由台账决定（零上游）。
func TestOnboardingStatusFromLedger(t *testing.T) {
	acc := Account{ID: "a1"}
	status := onboardingStatusOf(acc)
	if status.Total != 6000 {
		t.Errorf("Total = %d, want 6000（两条各 3000）", status.Total)
	}
	if status.Settled {
		t.Error("空台账不应判为已结清")
	}
	if len(status.Tasks) != 2 {
		t.Fatalf("任务数 = %d, want 2", len(status.Tasks))
	}

	// 只结算一条。
	acc.OnboardingGrants = map[string]string{"desktop_login_grant": "2026-11-09T00:00:00Z"}
	status = onboardingStatusOf(acc)
	if status.Settled {
		t.Error("只结算一条时不应判为已结清")
	}
	if status.Earned != 3000 {
		t.Errorf("Earned = %d, want 3000", status.Earned)
	}

	// 两条都结算。
	acc.OnboardingGrants["mobile_login_grant"] = "2026-11-09T00:00:00Z"
	status = onboardingStatusOf(acc)
	if !status.Settled {
		t.Error("两条都结算后应判为已结清")
	}
	if status.Earned != 6000 {
		t.Errorf("Earned = %d, want 6000", status.Earned)
	}
}

// TestOnboardingTasksCoverBothPlatforms 验证任务表覆盖桌面端与手机端，
// 且手机端使用 app-android 身份（与桌面端端点互不共享额度）。
func TestOnboardingTasksCoverBothPlatforms(t *testing.T) {
	var desktop, mobile bool
	for _, task := range onboardingTasks {
		switch task.Key {
		case "desktop_login_grant":
			desktop = true
			if task.Mobile {
				t.Error("桌面端任务不应标为 mobile")
			}
			if !strings.Contains(task.Path, "/desktop/") {
				t.Errorf("桌面端路径 = %q", task.Path)
			}
		case "mobile_login_grant":
			mobile = true
			if !task.Mobile {
				t.Error("手机端任务应标为 mobile")
			}
			if !strings.Contains(task.Path, "/mobile/") {
				t.Errorf("手机端路径 = %q", task.Path)
			}
		}
	}
	if !desktop || !mobile {
		t.Errorf("任务表应同时包含桌面端与手机端（desktop=%v mobile=%v）", desktop, mobile)
	}
	if onboardingTotal() != 6000 {
		t.Errorf("onboardingTotal = %d, want 6000", onboardingTotal())
	}
}

// TestOnboardingSettledIgnoresBlank 空串台账值视为未结算。
func TestOnboardingSettledIgnoresBlank(t *testing.T) {
	acc := Account{OnboardingGrants: map[string]string{"desktop_login_grant": "   "}}
	if onboardingSettled(acc, "desktop_login_grant") {
		t.Error("空白台账值应视为未结算")
	}
	if onboardingSettled(acc, "missing") {
		t.Error("不存在的键应视为未结算")
	}
}
