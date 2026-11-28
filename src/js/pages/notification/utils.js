import { formatDateTime } from '../../modules/utils.js';

export const formatHistoryDate = (raw) => {
  if (!raw) return '';
  return formatDateTime(raw);
};

export const buildSampleEventData = (rule = {}) => {
  if (rule.source_module === 'cron') {
    const isWorkflow = (rule.event_type || '').startsWith('workflow');
    return {
      severity: rule.severity || 'info',
      eventType: rule.event_type || 'task.completed',
      sourceModule: 'cron',
      taskId: 12,
      taskName: '每日 Token 用量统计',
      workflowId: 8,
      workflowName: '每日Token用量分析',
      status: 'success',
      summary: '成功 3，失败 0，跳过 0',
      output: '读取 GET /api/modelhub/analytics/summary 完成，总量 1,283,990 tokens',
      duration: 11,
      triggerType: 'cron',
      time: new Date().toLocaleString('zh-CN'),
      ...(isWorkflow ? { workflowId: 8, workflowName: '每日Token用量分析' } : { taskId: 12, taskName: '每日 Token 用量统计' }),
    };
  }
  return {
    severity: rule.severity || 'warning',
    eventType: rule.event_type || 'down',
    sourceModule: rule.source_module || 'uptime',
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
    downDuration: '5 分钟 32 秒',
    downDurationMs: 332000,
    lastActive: new Date(Date.now() - 332000).toISOString(),
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

export const isPrivateHostname = (hostname) => {
  const host = String(hostname || '').trim().toLowerCase().replace(/^\[|\]$/g, '');
  if (!host) return false;
  if (host === 'localhost' || host.endsWith('.localhost') || host.endsWith('.local') || host.endsWith('.internal')) return true;
  if (host.includes(':')) {
    return host === '::1' || host.startsWith('fe80:') || host.startsWith('fc') || host.startsWith('fd');
  }
  const ipv4 = host.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
  if (ipv4) {
    const a = Number(ipv4[1]);
    const b = Number(ipv4[2]);
    if (a === 0 || a === 10 || a === 127) return true;
    if (a === 172 && b >= 16 && b <= 31) return true;
    if (a === 192 && b === 168) return true;
    if (a === 169 && b === 254) return true;
    if (a === 100 && b >= 64 && b <= 127) return true;
  }
  return false;
};

export const getNotificationTargetWarning = (type, config = {}) => {
  const targets = [];
  if (type === 'email' && config.host) targets.push({ label: 'SMTP 主机', host: config.host });
  if (type === 'telegram' && config.proxy_url) {
    try {
      const proxy = new URL(config.proxy_url);
      targets.push({ label: 'Telegram 代理', host: proxy.hostname });
    } catch {
      return null;
    }
  }
  const risky = targets.filter(item => isPrivateHostname(item.host));
  if (risky.length === 0) return null;
  return `${risky.map(item => `「${item.label}」${item.host}`).join('、')} 指向内网或回环地址,测试投递会让服务器向该目标发起连接。`;
};
