package adminai

import (
	"strings"
	"testing"

	"github.com/iwvw/api-monitor/backend-go/internal/adminai/channel"
)

func TestNormalizeRichMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "plain-text-hard-break",
			in:   "普通段落第一行\n这是第二行\n第三行",
			want: []string{"普通段落第一行  \n这是第二行  \n第三行"},
		},
		{
			name: "blank-line-paragraph-preserved",
			in:   "段落一\n\n段落二",
			want: []string{"段落一\n\n段落二"},
		},
		{
			name: "list-preserved",
			in:   "- 项 A\n- 项 B\n  - 子项",
			want: []string{"- 项 A\n- 项 B\n  - 子项"},
		},
		{
			name: "ordered-list-preserved",
			in:   "1. 第一步\n2. 第二步",
			want: []string{"1. 第一步\n2. 第二步"},
		},
		{
			name: "table-preserved",
			in:   "| a | b |\n| --- | --- |\n| 1 | 2 |",
			want: []string{"| a | b |\n| --- | --- |\n| 1 | 2 |"},
		},
		{
			name: "code-block-preserved",
			in:   "```go\nfmt.Println(\"x\")\n```",
			want: []string{"```go\nfmt.Println(\"x\")\n```"},
		},
		{
			name: "quote-preserved",
			in:   "> 引用\n> 继续",
			want: []string{"> 引用\n> 继续"},
		},
		{
			name: "heading-and-plain-body",
			in:   "# 标题\n普通正文第一行\n第二行",
			// 标题行与正文是不同块级元素，GFM 自然分行；仅连续普通文本行之间补硬换行。
			want: []string{"# 标题\n普通正文第一行  \n第二行"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := channel.NormalizeRichMarkdown(c.in)
			for _, w := range c.want {
				if got != w {
					t.Errorf("NormalizeRichMarkdown(%q)\n got: %q\nwant: %q", c.in, got, w)
				}
			}
		})
	}
}

func TestNormalizeRichMarkdownNoMarkdownV2Leftover(t *testing.T) {
	in := "### 🖥️ 系统状态\n*   **CPU**: 13th Gen Intel i7，使用率 **31.8%**。"
	got := channel.NormalizeRichMarkdown(in)
	if strings.Contains(got, "\x00") {
		t.Errorf("still contains NUL placeholder: %q", got)
	}
	if strings.Contains(got, "M2:") {
		t.Errorf("still contains placeholder marker: %q", got)
	}
}
