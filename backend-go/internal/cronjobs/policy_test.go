package cronjobs

import (
	"encoding/json"
	"testing"
)

// AI 任务策略归一化：历史非法值 "standard" 与空/缺失必须收敛为 "allow"
// （cron 无头执行下唯一可写的策略），readonly 与 allow 保持原样。
func TestNormalizeAIPolicyConfig(t *testing.T) {
	cases := []struct {
		name   string
		config string
		want   string // 期望的 policy 值；"" 表示不应被改写
	}{
		{"非法值 standard → allow", `{"model":"m","policy":"standard","channelId":"c"}`, "allow"},
		{"大写 STANDARD → allow", `{"policy":"STANDARD"}`, "allow"},
		{"空 policy → allow", `{"model":"m","policy":""}`, "allow"},
		{"缺失 policy → allow", `{"model":"m"}`, "allow"},
		{"readonly 保持", `{"policy":"readonly"}`, "readonly"},
		{"allow 保持", `{"policy":"allow"}`, "allow"},
		{"非法 JSON 不处理", `not json`, ""},
	}
	for _, c := range cases {
		out, changed := normalizeAIPolicyConfig(c.config)
		if c.want == "" {
			if changed {
				t.Fatalf("%s: 不应改写, got %s", c.name, out)
			}
			continue
		}
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(out), &raw); err != nil {
			t.Fatalf("%s: 输出不是合法 JSON: %s", c.name, out)
		}
		if got, _ := raw["policy"].(string); got != c.want {
			t.Fatalf("%s: policy = %q, want %q (out=%s)", c.name, got, c.want, out)
		}
		if c.config == out && raw["policy"] != c.want {
			t.Fatalf("%s: 未触发改写: %s", c.name, out)
		}
	}
}

// 经 buildSchedulerTask 保存 AI 任务时，config 中的非法 policy 被归一化。
func TestBuildSchedulerTaskNormalizesAIPolicy(t *testing.T) {
	svc := newCronService(t)
	_ = svc
	payload := schedulerTaskPayload{
		Name:    strPtr("巡检"),
		Command: strPtr("执行巡检"),
		Type:    strPtr("ai"),
		Config:  strPtr(`{"model":"m","policy":"standard","channelId":"c"}`),
	}
	task, err := buildSchedulerTask(payload, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(task.Config), &raw); err != nil {
		t.Fatalf("config not json: %s", task.Config)
	}
	if got, _ := raw["policy"].(string); got != "allow" {
		t.Fatalf("AI 任务保存后 policy 应归一化 allow, got %q (config=%s)", got, task.Config)
	}
}

func strPtr(v string) *string { return &v }