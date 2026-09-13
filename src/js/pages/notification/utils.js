export const formatHistoryDate = (raw) => {
  if (!raw) return '';
  const iso = String(raw).includes(' ') ? String(raw).replace(' ', 'T') : String(raw);
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? String(raw) : date.toLocaleString();
};

export const buildSampleEventData = (rule = {}) => {
  if (rule.source_module === 'cron') {
    const isWorkflow = (rule.event_type || '').startsWith('workflow');
    return {
      severity: rule.severity || 'info',
      eventType: rule.event_type || 'task.completed',
      taskId: 12,
      taskName: '每日 Token 用量统计',
      workflowId: 8,
      workflowName: '每日Token用量分析',
      status: 'success',
      summary: '成功 3，失败 0，跳过 0',
      output: '读取 GET /api/openai/analytics/summary 完成，总量 1,283,990 tokens',
      duration: 11,
      triggerType: 'cron',
      time: new Date().toLocaleString('zh-CN'),
      ...(isWorkflow ? { workflowId: 8, workflowName: '每日Token用量分析' } : { taskId: 12, taskName: '每日 Token 用量统计' }),
    };
  }
  return {
    severity: rule.severity || 'warning',
    eventType: rule.event_type || 'down',
    monitorName: 'API Gateway',
    serverName: 'prod-node-01',
    url: 'https://api.example.com/health',
    host: 'prod-node-01',
    hostname: 'prod-node-01',
    error: 'Connection timeout',
    ping: 128,
    cpu_usage: 92,
    mem_percent: 84,
    disk_usage: 91,
    traffic_percent: 86.35,
    traffic_used: '863.5 GB',
    traffic_limit: '1 TB',
    threshold: 90,
    downDuration: '3 分钟',
  };
};

export const parseNotificationPreviewLine = (line = '') => {
  const trimmed = line.trim();
  if (!trimmed) return { empty: true };
  const asciiIndex = trimmed.indexOf(':');
  const chineseIndex = trimmed.indexOf('：');
  const separator = asciiIndex < 0
    ? chineseIndex
    : (chineseIndex < 0 ? asciiIndex : Math.min(asciiIndex, chineseIndex));
  if (separator <= 0) return { value: trimmed };
  const label = trimmed.slice(0, separator).trim();
  const rawValue = trimmed.slice(separator + 1).trim();
  const statusIcons = {
    在线: '🟢', 已恢复: '🟢', 成功: '🟢',
    离线: '🔴', 故障: '🔴', 失败: '🔴',
    中断: '🟠', 告警: '🟠', 警告: '🟠', 采集异常: '🟠',
  };
  return {
    label,
    value: label === '状态' && statusIcons[rawValue]
      ? `${statusIcons[rawValue]} ${rawValue}`
      : rawValue,
    code: ['地址', '链接', '云端链接', 'URL', 'Host'].includes(label),
  };
};
