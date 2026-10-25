package antigravity

import (
	"strings"
	"testing"

	engineag "github.com/iwvw/api-monitor/backend-go/internal/antigravity/engine/pkg/antigravity"
)

func TestForwardBaseURLAlwaysUsesDaily(t *testing.T) {
	// 旧实现按 planType 在 prod / daily 之间切；上游已确认消费级凭据统一落在
	// daily，且跨端点会拿到互不兼容的模型授权。这里钉住「不按套餐改变端点」。
	cases := []string{"", "Free", "Pro", "Ultra", "unknown"}
	first := forwardBaseURL(&Account{})
	if first == "" {
		t.Fatal("forwardBaseURL 不应返回空")
	}

	for _, plan := range cases {
		acc := &Account{PlanType: plan}
		if got := forwardBaseURL(acc); got != first {
			t.Errorf("forwardBaseURL(plan=%q) = %q, want %q", plan, got, first)
		}
	}

	// 必须是 daily 端点。
	if !strings.Contains(first, "daily-cloudcode-pa") {
		t.Errorf("forwardBaseURL = %q, want daily endpoint", first)
	}
}

func TestForwardBaseURLNilAccount(t *testing.T) {
	if got := forwardBaseURL(nil); got == "" {
		t.Fatal("forwardBaseURL(nil) 不应返回空")
	}
}

func TestBaseURLsPreferDaily(t *testing.T) {
	// 端点顺序是 daily 优先，prod 兜底：新模型/配额接口只在 daily 可用。
	urls := engineag.ForwardBaseURLs()
	if len(urls) < 2 {
		t.Fatalf("expected at least 2 base URLs, got %d", len(urls))
	}
	if !strings.Contains(urls[0], "daily-cloudcode-pa") {
		t.Errorf("first base URL = %q, want daily", urls[0])
	}
	if !strings.Contains(urls[1], "cloudcode-pa.googleapis.com") || strings.Contains(urls[1], "daily") {
		t.Errorf("second base URL = %q, want prod", urls[1])
	}
}
