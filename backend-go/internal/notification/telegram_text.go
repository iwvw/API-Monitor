package notification

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/iwvw/api-monitor/backend-go/internal/applog"
)

func cloneNotificationData(data map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(data)+8)
	for key, value := range data {
		result[key] = value
	}
	return result
}

func notificationDataChanges(previous, current map[string]interface{}) map[string]interface{} {
	changes := map[string]interface{}{}
	keys := map[string]struct{}{}
	for key := range previous {
		if !strings.HasPrefix(key, "lifecycle") && key != "downDuration" {
			keys[key] = struct{}{}
		}
	}
	for key := range current {
		if !strings.HasPrefix(key, "lifecycle") && key != "downDuration" {
			keys[key] = struct{}{}
		}
	}
	for key := range keys {
		before, beforeOK := previous[key]
		after, afterOK := current[key]
		if beforeOK == afterOK && jsonString(before) == jsonString(after) {
			continue
		}
		changes[key] = map[string]interface{}{"from": before, "to": after}
	}
	return changes
}

func parseLifecycleStateTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		parsed, err := time.ParseInLocation(layout, value, time.UTC)
		if err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// telegramMessageText 组装 Rich Markdown（GFM）消息体：标题加粗、键值行用
// **label：** 粗体标签，行间用 GFM 硬换行（行尾两个空格）保证富文本渲染真正换行
// （普通 LF 在 GFM 中属于软换行，会合并为一行）。
func telegramMessageText(title, message string) string {
	lines := strings.Split(strings.TrimSpace(message), "\n")
	formatted := make([]string, 0, len(lines))
	for _, line := range lines {
		rendered := telegramMessageLine(line)
		if rendered == "" {
			continue
		}
		formatted = append(formatted, rendered+"  ")
	}
	body := strings.Join(formatted, "\n")
	head := title
	if body == "" {
		return normalizeRichTextColons(head)
	}
	return normalizeRichTextColons(head + "\n\n" + body + "\n\nAPI Monitor")
}

// normalizeRichTextColons 富文本（sendRichMessage markdown）渲染在部分客户端会把
// 全角冒号（U+FF1A）显示为替换字符（��），发送前统一转为半角冒号避免乱码。
func normalizeRichTextColons(text string) string {
	return strings.ReplaceAll(text, "：", ":")
}

func telegramMessageLine(line string) string {
	applog.Info(context.Background(), "notification", "telegram-line-in", "lineHex", fmt.Sprintf("%x", []byte(line)))
	field := parseNotificationMessageLine(line)
	if field.Empty {
		return ""
	}
	applog.Info(context.Background(), "notification", "telegram-line-parse", "labelHex", fmt.Sprintf("%x", []byte(field.Label)), "valueHex", fmt.Sprintf("%x", []byte(field.Value)))
	if field.Label == "" {
		return field.Value
	}
	value := field.Value
	if field.Label == "状态" || strings.EqualFold(field.Label, "status") {
		value = notificationStatusIcon(field.Value) + value
	}
	if isNotificationCodeField(field.Label) {
		value = "`" + field.Value + "`"
	}
	return field.Label + ": " + value
}

// telegramEscapeV2 转义 Telegram MarkdownV2 特殊字符。
// MarkdownV2 规定除 pre/code（仅 ` 与 \）和链接 URL（仅 ) 与 \）外，
// 全部保留字符 _ * [ ] ( ) ~ ` > # + - = | { } . ! 在普通文本与实体内部都必须转义，
// 否则 Telegram 返回 "can't parse entities"。
func telegramEscapeV2(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', '_', '*', '[', ']', '(', ')', '~', '`', '>', '#', '+', '-', '=', '|', '{', '}', '.', '!':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// telegramEscapeBold 转义 MarkdownV2 加粗实体内部内容（同全量转义）。
func telegramEscapeBold(s string) string { return telegramEscapeV2(s) }


// telegramEscapeCode 转义 MarkdownV2 行内代码内部（仅 ` 与 \ 需转义）。
func telegramEscapeCode(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '`' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func parseNotificationMessageLine(line string) notificationMessageField {
	line = strings.TrimSpace(line)
	if line == "" {
		return notificationMessageField{Empty: true}
	}
	separator := strings.Index(line, ":")
	separatorLen := 1
	if chineseSeparator := strings.Index(line, "："); chineseSeparator >= 0 && (separator < 0 || chineseSeparator < separator) {
		// 全角冒号为 3 字节（U+FF1A），value 切片必须跳过完整分隔符，
		// 否则残留后两字节（BC 9A）导致 Telegram 端显示乱码。
		separator = chineseSeparator
		separatorLen = 3
	}
	if separator <= 0 {
		return notificationMessageField{Value: line}
	}
	label := strings.TrimSpace(line[:separator])
	if len([]rune(label)) > 32 {
		return notificationMessageField{Value: line}
	}
	return notificationMessageField{Label: label, Value: strings.TrimSpace(line[separator+separatorLen:])}
}

func isNotificationCodeField(label string) bool {
	return label == "地址" || label == "链接" || label == "云端链接" || strings.EqualFold(label, "url") ||
		strings.EqualFold(label, "host") || strings.EqualFold(label, "address")
}

func notificationStatusIcon(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "online", "up", "recovered", "success", "在线", "恢复", "已恢复", "成功":
		return "🟢 "
	case "offline", "down", "failed", "failure", "离线", "故障", "失败":
		return "🔴 "
	case "interrupted", "degraded", "warning", "中断", "采集异常", "告警", "警告":
		return "🟠 "
	default:
		return ""
	}
}

func emailMessageHTML(title, message string) string {
	accent := "#3b82f6"
	statusBackground := "#eff6ff"
	statusText := "#1d4ed8"
	lowerMessage := strings.ToLower(message)
	if strings.Contains(lowerMessage, "状态: 离线") || strings.Contains(lowerMessage, "状态: 故障") || strings.Contains(lowerMessage, "status: offline") || strings.Contains(lowerMessage, "status: down") {
		accent, statusBackground, statusText = "#dc2626", "#fef2f2", "#b91c1c"
	} else if strings.Contains(lowerMessage, "状态: 在线") || strings.Contains(lowerMessage, "状态: 已恢复") || strings.Contains(lowerMessage, "status: online") || strings.Contains(lowerMessage, "status: recovered") {
		accent, statusBackground, statusText = "#16a34a", "#f0fdf4", "#15803d"
	} else if strings.Contains(lowerMessage, "状态: 告警") || strings.Contains(lowerMessage, "状态: 中断") || strings.Contains(lowerMessage, "status: warning") {
		accent, statusBackground, statusText = "#d97706", "#fffbeb", "#b45309"
	}

	var rows strings.Builder
	for _, line := range strings.Split(strings.TrimSpace(message), "\n") {
		field := parseNotificationMessageLine(line)
		switch {
		case field.Empty:
			rows.WriteString(`<tr><td colspan="2" style="height:8px"></td></tr>`)
		case field.Label == "":
			rows.WriteString(`<tr><td colspan="2" style="padding:5px 0;color:#334155;font-size:14px;line-height:1.6">`)
			rows.WriteString(html.EscapeString(field.Value))
			rows.WriteString(`</td></tr>`)
		default:
			value := html.EscapeString(field.Value)
			if field.Label == "状态" || strings.EqualFold(field.Label, "status") {
				value = notificationStatusIcon(field.Value) + value
			}
			if isNotificationCodeField(field.Label) {
				value = `<code style="font-family:Consolas,monospace;font-size:12px;color:#0f172a;word-break:break-all">` + value + `</code>`
			}
			rows.WriteString(`<tr><td style="width:104px;padding:5px 12px 5px 0;color:#64748b;font-size:13px;vertical-align:top">`)
			rows.WriteString(html.EscapeString(field.Label))
			rows.WriteString(`</td><td style="padding:5px 0;color:#0f172a;font-size:14px;font-weight:600;line-height:1.5">`)
			rows.WriteString(value)
			rows.WriteString(`</td></tr>`)
		}
	}

	return `<!doctype html><html><body style="margin:0;padding:24px;background:#f1f5f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#0f172a">` +
		`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr><td align="center">` +
		`<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:620px;background:#ffffff;border:1px solid #e2e8f0;border-radius:8px;overflow:hidden">` +
		`<tr><td style="height:5px;background:` + accent + `"></td></tr>` +
		`<tr><td style="padding:24px 28px 10px"><div style="font-size:12px;font-weight:700;color:` + statusText + `;text-transform:uppercase">API Monitor</div>` +
		`<h1 style="margin:8px 0 0;font-size:20px;line-height:1.4;color:#0f172a">` + html.EscapeString(title) + `</h1></td></tr>` +
		`<tr><td style="padding:12px 28px 26px"><div style="padding:14px 16px;border-left:3px solid ` + accent + `;background:` + statusBackground + `;border-radius:4px">` +
		`<table role="presentation" width="100%" cellspacing="0" cellpadding="0">` + rows.String() + `</table></div></td></tr>` +
		`</table></td></tr></table></body></html>`
}