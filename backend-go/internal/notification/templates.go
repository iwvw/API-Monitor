package notification

import (
	"fmt"
	"strings"
	"time"

)

func formatNotificationDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	totalSeconds := int64(duration.Round(time.Second) / time.Second)
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	parts := make([]string, 0, 4)
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%d 天", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%d 小时", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%d 分钟", minutes))
	}
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d 秒", seconds))
	}
	return strings.Join(parts, " ")
}

func formatTitle(rule Rule, data map[string]interface{}) string {
	if rule.TitleTemplate != "" {
		return renderTemplate(rule.TitleTemplate, data)
	}
	icon := notificationEventIcon(rule.EventType, rule.Severity)
	subject := notificationSubject(data)
	if subject != "" {
		return fmt.Sprintf("%s %s - %s", icon, subject, rule.Name)
	}
	return fmt.Sprintf("%s %s", icon, rule.Name)
}

func formatMessage(rule Rule, data map[string]interface{}, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	if rule.MessageTemplate != "" {
		return renderTemplate(rule.MessageTemplate, notificationTemplateData(data, loc))
	}
	lines := []string{}
	add := func(label string, value interface{}) {
		if value != nil && stringValue(value) != "" {
			lines = append(lines, fmt.Sprintf("%s: %v", label, value))
		}
	}

	statusAdded := false
	if statusVal := data["status"]; statusVal != nil {
		add("状态", notificationStatusLabel(stringValue(statusVal)))
		statusAdded = true
	}
	if !statusAdded {
		add("状态", notificationEventStatus(rule.EventType))
	}
	add("事件", notificationEventLabel(rule.EventType))
	add("级别", notificationSeverityLabel(rule.Severity))

	if monitorName := stringValue(data["monitorName"]); monitorName != "" {
		add("监控项", monitorName)
	} else {
		add("主机", firstNonEmpty(stringValue(data["serverName"]), stringValue(data["hostname"])))
	}
	add("仓库", data["repositoryFullName"])
	add("资源", firstNonEmpty(stringValue(data["resourceName"]), stringValue(data["name"])))
	add("任务", data["taskName"])
	add("工作流", data["workflowName"])
	add("结果", data["summary"])
	add("输出", data["output"])
	add("耗时", data["duration"])
	add("触发方式", data["triggerType"])

	if data["url"] != nil {
		add("地址", data["url"])
	} else if data["host"] != nil {
		add("地址", data["host"])
	}

	// 最后活跃时间
	if lastActiveVal := data["lastActive"]; lastActiveVal != nil {
		add("最后活跃", toLocalTimeStr(stringValue(lastActiveVal), loc))
	}

	add("错误原因", data["error"])
	add("延迟 (Ping)", data["ping"])
	add("CPU 使用率", formatNotificationPercent(data["cpu_usage"]))
	add("内存使用率", formatNotificationPercent(data["mem_percent"]))
	add("磁盘使用率", formatNotificationPercent(data["disk_usage"]))
	add("流量使用率", formatNotificationPercent(data["traffic_percent"]))
	add("已用流量", data["traffic_used"])
	add("流量配额", data["traffic_limit"])
	add("报警阈值", formatNotificationPercent(data["threshold"]))
	add("持续时间", data["downDuration"])
	add("证书剩余天数", data["daysLeft"])
	if expiry := stringValue(data["expiry"]); expiry != "" {
		add("证书到期时间", toLocalTimeStr(expiry, loc))
	}

	add("当前值", data["current"])
	add("之前值", data["previous"])
	add("变化量", data["delta"])
	add("Actions 状态", data["conclusion"])
	add("剩余 API 额度", data["rateLimitRemaining"])
	if reset := stringValue(data["rateLimitReset"]); reset != "" {
		add("额度重置时间", toLocalTimeStr(reset, loc))
	}
	add("链接", data["htmlUrl"])
	add("说明", firstNonEmpty(stringValue(data["message"]), stringValue(data["reason"])))

	// 网关告警（openai 模块）字段
	if rateVal, ok := data["error_rate"].(float64); ok {
		add("错误率", fmt.Sprintf("%.1f%%", rateVal))
	}
	add("请求数", data["requests"])
	add("错误数", data["errors"])
	add("统计窗口", data["windowMin"])

	// 数据库备份相关字段
	add("备份 ID", data["backupId"])
	add("备份文件名", data["fileName"])
	if sizeVal := data["size"]; sizeVal != nil {
		var sizeInt int64
		switch v := sizeVal.(type) {
		case int:
			sizeInt = int64(v)
		case int64:
			sizeInt = v
		case float64:
			sizeInt = int64(v)
		}
		if sizeInt > 0 {
			add("文件大小", formatNotificationBytes(sizeInt))
		} else {
			add("文件大小", sizeVal)
		}
	}
	add("存储位置", data["location"])
	add("云端链接", data["remoteUrl"])

	if len(lines) == 0 {
		return jsonString(data)
	}
	lines = append(lines, "", "时间: "+time.Now().In(loc).Format("2006/01/02 15:04:05"))
	return strings.Join(lines, "\n")
}

func notificationSubject(data map[string]interface{}) string {
	return firstNonEmpty(
		stringValue(data["monitorName"]), stringValue(data["serverName"]),
		stringValue(data["repositoryFullName"]), stringValue(data["fileName"]),
		stringValue(data["resourceName"]),
	)
}

func notificationEventIcon(eventType, severity string) string {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	if eventType == "up" || eventType == "online" || eventType == "action_recovered" || strings.HasSuffix(eventType, "_normal") {
		return "🟢"
	}
	if eventType == "database.backup" || eventType == "database.import" || eventType == "release_published" || strings.HasSuffix(eventType, ".created") || strings.HasSuffix(eventType, ".exported") || strings.HasSuffix(eventType, ".imported") {
		return "✅"
	}
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return "🚨"
	case "warning":
		return "🟠"
	default:
		return "ℹ️"
	}
}

func notificationEventStatus(eventType string) string {
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	switch {
	case eventType == "up", eventType == "online", eventType == "action_recovered", strings.HasSuffix(eventType, "_normal"):
		return "已恢复"
	case eventType == "down", eventType == "offline":
		return "故障"
	case eventType == "interrupted":
		return "中断"
	case eventType == "degraded":
		return "采集异常"
	case strings.HasSuffix(eventType, "_high"), strings.Contains(eventType, "failed"), eventType == "ssl_expiry":
		return "告警"
	case strings.HasSuffix(eventType, ".created"), strings.HasSuffix(eventType, ".imported"), strings.HasSuffix(eventType, ".exported"), eventType == "database.backup", eventType == "database.import":
		return "成功"
	default:
		return "已触发"
	}
}

func notificationStatusLabel(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "online", "up":
		return "在线"
	case "offline", "down":
		return "离线"
	case "interrupted":
		return "中断"
	case "degraded":
		return "采集异常"
	case "success":
		return "成功"
	case "failed", "failure":
		return "失败"
	default:
		return status
	}
}

func notificationSeverityLabel(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return "严重"
	case "warning":
		return "警告"
	case "info":
		return "信息"
	default:
		return severity
	}
}

func notificationEventLabel(eventType string) string {
	labels := map[string]string{
		"down": "服务不可用", "up": "服务恢复", "pending": "状态待确认", "ssl_expiry": "SSL 证书即将到期",
		"offline": "主机离线", "online": "主机上线", "interrupted": "连接中断", "degraded": "采集异常",
		"cpu_high": "CPU 使用率过高", "cpu_normal": "CPU 恢复正常", "memory_high": "内存使用率过高", "memory_normal": "内存恢复正常",
		"disk_high": "磁盘使用率过高", "disk_normal": "磁盘恢复正常", "traffic_high": "流量使用率过高", "traffic_normal": "流量恢复正常",
		"action_failed": "GitHub Actions 执行失败", "action_recovered": "GitHub Actions 恢复正常", "release_published": "GitHub 新版本发布",
		"star_spike": "GitHub Star 激增", "issue_opened": "GitHub Issue 新增", "pull_request_opened": "GitHub PR 新增",
		"repository_unreachable": "GitHub 仓库无法访问", "token_invalid": "GitHub Token 已失效", "rate_limit_low": "GitHub API 额度偏低",
		"webhook_delivery_failed": "GitHub Webhook 投递失败", "webhook_ping": "GitHub Webhook 连通成功",
		"database.backup": "数据库备份", "database.import": "数据库恢复", "log.cleanup": "日志清理", "migration.failed": "数据库迁移失败",
		"resource.created": "资源已创建", "resource.updated": "资源已更新", "resource.deleted": "资源已删除", "cleanup": "清理任务",
		"security.revealed": "敏感信息已查看", "backup.imported": "备份已导入", "backup.exported": "备份已导出",
		"gateway_error_high": "网关错误率过高", "gateway_error_normal": "网关错误率恢复正常",
		"quota_window_refreshed": "Antigravity 配额窗口已刷新",
		"task.completed": "定时任务执行完成", "task.failed": "定时任务执行失败",
		"workflow.completed": "工作流执行完成", "workflow.failed": "工作流执行失败",
		"asset_expiry": "资产即将到期",
	}
	if label := labels[strings.ToLower(strings.TrimSpace(eventType))]; label != "" {
		return label
	}
	return eventType
}

func notificationTemplateData(data map[string]interface{}, loc *time.Location) map[string]interface{} {
	if loc == nil {
		loc = time.Local
	}
	result := make(map[string]interface{}, len(data)+3)
	for key, value := range data {
		result[key] = value
	}
	result["time"] = time.Now().In(loc).Format("2006/01/02 15:04:05")
	result["timeZone"] = loc.String()
	if lastActive := stringValue(data["lastActive"]); lastActive != "" {
		result["lastActiveLocal"] = toLocalTimeStr(lastActive, loc)
	}
	return result
}

func toLocalTimeStr(utcStr string, loc *time.Location) string {
	if utcStr == "" {
		return ""
	}
	if loc == nil {
		loc = time.Local
	}
	var t time.Time
	var err error
	t, err = time.ParseInLocation("2006-01-02 15:04:05", utcStr, time.UTC)
	if err != nil {
		t, err = time.Parse(time.RFC3339, utcStr)
	}
	if err != nil {
		return utcStr
	}
	return t.In(loc).Format("2006/01/02 15:04:05")
}

func formatNotificationBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

func formatNotificationPercent(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(stringValue(value))
	if text == "" || strings.HasSuffix(text, "%") {
		return text
	}
	return text + "%"
}

// renderTemplate 渲染通知模板。用游标式单遍扫描替换占位符：
// 缺失 key 的占位符原样保留（便于用户定位），不重复扫描、不阻塞后续
// 正常占位符，保证渲染必然终止（旧实现重建相同占位符导致死循环）。
func renderTemplate(template string, data map[string]interface{}) string {
	const maxKeys = 100
	var b strings.Builder
	b.Grow(len(template) + 32)
	rest := template
	for i := 0; i < maxKeys; i++ {
		start := strings.Index(rest, "{{")
		if start < 0 {
			b.WriteString(rest)
			return normalizeTemplateNewlines(b.String())
		}
		end := strings.Index(rest[start+2:], "}}")
		if end < 0 {
			b.WriteString(rest)
			return normalizeTemplateNewlines(b.String())
		}
		end += start + 2
		b.WriteString(rest[:start])
		key := strings.TrimSpace(rest[start+2 : end])
		if value, ok := data[key]; ok {
			b.WriteString(stringValue(value))
		} else {
			b.WriteString("{{" + key + "}}")
		}
		rest = rest[end+2:]
	}
	b.WriteString(rest)
	return normalizeTemplateNewlines(b.String())
}

// normalizeTemplateNewlines 把模板里以字面量存储的 \n（反斜杠+n 两个字符，常见于
// 旧版编辑器/转义序列落库）统一还原为真实换行，保证行级解析（telegramMessageLine
// 等按行切分状态/指标）不会把整段消息当成一行导致格式错乱。
func normalizeTemplateNewlines(text string) string {
	return strings.ReplaceAll(text, `\n`, "\n")
}