package openai

import "testing"

// 这段契约测试锁定「网关如何统计缓存命中」这件事，供插件侧对齐。
//
// 背景：插件（如 workbuddy）把上游 usage 透传给网关，而网关只从上游字节流里用
// cachedTokensRegex 捞缓存命中数。上游对「缓存命中 token」的命名各家不同
// （CodeBuddy 实测用 DeepSeek 风格的 prompt_cache_hit_tokens），网关不会认。
// 因此插件侧必须把别名归一到标准形状 usage.prompt_tokens_details.cached_tokens。
//
// 这里把两件事都钉住：标准形状必须被识别；非标准命名必须不被识别（否则插件侧
// 的归一化就变成"可有可无"，日后被人删掉也不会有人发现）。
func TestCachedTokensRegexContract(t *testing.T) {
	extract := func(chunk string) (int, bool) {
		m := cachedTokensRegex.FindStringSubmatch(chunk)
		if len(m) <= 1 {
			return 0, false
		}
		n := 0
		for _, c := range m[1] {
			n = n*10 + int(c-'0')
		}
		return n, true
	}

	cases := []struct {
		name  string
		chunk string
		want  int
		found bool
	}{
		{
			name:  "标准形状（插件归一化后的目标）",
			chunk: `{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105,"prompt_tokens_details":{"cached_tokens":80}}}`,
			want:  80,
			found: true,
		},
		{
			name:  "带空格的紧凑差异也应命中",
			chunk: `{"usage":{"prompt_tokens_details":{"cached_tokens": 31020}}}`,
			want:  31020,
			found: true,
		},
		{
			name:  "DeepSeek/CodeBuddy 原生命名：网关认不出，必须由插件归一",
			chunk: `{"usage":{"prompt_tokens":100,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`,
			found: false,
		},
		{
			name:  "腾讯协议文档命名：同样认不出",
			chunk: `{"usage":{"prompt_tokens":100,"cache_read_tokens":80}}`,
			found: false,
		},
		{
			name:  "写入缓存不算命中（即使出现也不该被当成命中键名）",
			chunk: `{"usage":{"prompt_tokens":100,"cache_write_tokens":80}}`,
			found: false,
		},
		{
			name: "上游常见形状：顶层占位 0 出现在前、真值在后 —— 正则只会取到 0",
			chunk: `{"usage":{"cached_tokens":0,"prompt_cache_hit_tokens":131712,` +
				`"prompt_tokens_details":{"cached_tokens":129664}}}`,
			want:  0,
			found: true,
		},
		{
			name:  "带引号的数：正则不认，插件侧必须转成裸数字",
			chunk: `{"usage":{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":"80"}}}`,
			found: false,
		},
		{
			name:  "没有任何缓存字段",
			chunk: `{"usage":{"prompt_tokens":100,"completion_tokens":5,"total_tokens":105}}`,
			found: false,
		},
	}

	for _, c := range cases {
		got, found := extract(c.chunk)
		if found != c.found {
			t.Errorf("%s: 命中判定=%v want=%v", c.name, found, c.found)
			continue
		}
		if found && got != c.want {
			t.Errorf("%s: 提取值=%d want=%d", c.name, got, c.want)
		}
	}
}
