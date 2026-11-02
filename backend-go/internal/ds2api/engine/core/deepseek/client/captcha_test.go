package client

import (
	"testing"
)

func TestDetectCaptchaChallengeNilForCleanResponse(t *testing.T) {
	resp := map[string]any{
		"code": 0,
		"data": map[string]any{
			"biz_code": 0,
			"biz_data": map[string]any{
				"challenge": map[string]any{
					"algorithm": "DeepSeekHashV1",
					"challenge": "abc",
				},
			},
		},
	}
	if ch := DetectCaptchaChallenge(resp); ch != nil {
		t.Fatalf("expected nil for clean response, got %+v", ch)
	}
}

func TestDetectCaptchaChallengeWithImageAndInstruction(t *testing.T) {
	resp := map[string]any{
		"code": 500,
		"data": map[string]any{
			"biz_code": 500,
			"biz_data": map[string]any{
				"detail": map[string]any{
					"bg":          "//castatic.fengkongcloud.cn/captcha.png",
					"order":       "请按顺序点击",
					"rid":         "test-rid-123",
					"captchaUuid": "uuid-abc",
				},
			},
		},
	}
	ch := DetectCaptchaChallenge(resp)
	if ch == nil {
		t.Fatal("expected captcha challenge, got nil")
	}
	if ch.ImageURL == "" {
		t.Fatal("expected image url to be populated")
	}
	if ch.Instruction == "" {
		t.Fatal("expected instruction to be populated")
	}
	if ch.Rid != "test-rid-123" {
		t.Fatalf("unexpected rid: %q", ch.Rid)
	}
	if ch.CaptchaUUID != "uuid-abc" {
		t.Fatalf("unexpected captcha uuid: %q", ch.CaptchaUUID)
	}
}

func TestDetectCaptchaChallengeWithKeywordAndFailureCode(t *testing.T) {
	resp := map[string]any{
		"code": 1,
		"msg":  "verification required",
		"data": map[string]any{
			"biz_code": 1001,
			"biz_msg":  "需要验证码",
		},
	}
	ch := DetectCaptchaChallenge(resp)
	if ch == nil {
		t.Fatal("expected captcha challenge from keyword+failure code, got nil")
	}
}

func TestDetectCaptchaChallengeNoKeywordNoImageReturnsNil(t *testing.T) {
	resp := map[string]any{
		"code": 1,
		"data": map[string]any{
			"biz_code": 1001,
			"biz_msg":  "some other error",
		},
	}
	if ch := DetectCaptchaChallenge(resp); ch != nil {
		t.Fatalf("expected nil for non-captcha failure, got %+v", ch)
	}
}

func TestDetectCaptchaChallengeNestedDeep(t *testing.T) {
	resp := map[string]any{
		"data": map[string]any{
			"biz_data": map[string]any{
				"items": []any{
					map[string]any{
						"nested": map[string]any{
							"detail": map[string]any{
								"image": "https://example.com/captcha.jpg",
								"rid":   "deep-rid",
							},
						},
					},
				},
			},
		},
	}
	ch := DetectCaptchaChallenge(resp)
	if ch == nil {
		t.Fatal("expected captcha challenge from nested structure, got nil")
	}
	if ch.Rid != "deep-rid" {
		t.Fatalf("unexpected rid from nested: %q", ch.Rid)
	}
}
