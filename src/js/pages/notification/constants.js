export const getChannelTypeName = (type) => {
  const names = {
    email: 'Email 邮箱',
    telegram: 'Telegram Bot',
  };
  return names[type] || type;
};

export const getSourceModuleName = (module) => {
  const names = {
    uptime: '可用性监测',
    server: '主机实例',
    github: 'GitHub',
    openai: 'OpenAI 接口',
    system: '系统设置',
    filebox: '文件柜',
    totp: '双因子认证',
    antigravity: 'Antigravity',
    cron: '定时任务',
  };
  return names[module] || module;
};

export const getEventTypeName = (type) => {
  const names = {
    down: '服务宕机 (Down)',
    up: '服务恢复 (Up)',
    offline: '主机离线',
    online: '主机上线',
    interrupted: '连接中断',
    degraded: '采集异常',
    cpu_high: 'CPU高负载',
    cpu_normal: 'CPU恢复正常',
    memory_high: '内存不足',
    memory_normal: '内存恢复',
    disk_high: '磁盘空间不足',
    disk_normal: '磁盘恢复正常',
    traffic_high: '流量超额',
    traffic_normal: '流量恢复',
    balance_low: '余额不足',
    log_too_large: '日志体积过大',
    pending: '状态待确认',
    ssl_expiry: 'SSL 证书即将到期',
    'resource.created': '资源已创建',
    'resource.updated': '资源已更新',
    'resource.deleted': '资源已删除',
    'security.revealed': '密钥已查看',
    'backup.imported': '备份已导入',
    'backup.exported': '备份已导出',
    cleanup: '清理任务',
    'database.backup': '数据库备份',
    'database.import': '数据库导入',
    'log.cleanup': '日志清理',
    'migration.failed': '迁移失败',
    login: '登录',
    logout: '登出',
    'playback.error': '播放错误',
    'proxy.blocked': '代理拦截',
    action_failed: 'Actions 执行失败',
    action_recovered: 'Actions 恢复正常',
    release_published: '发布新版本',
    star_spike: 'Star 激增',
    issue_opened: '新增 Issue',
    pull_request_opened: '新增拉取请求',
    repository_unreachable: '仓库无法访问',
    token_invalid: 'Token 已失效',
    rate_limit_low: 'API 限额偏低',
    webhook_delivery_failed: 'Webhook 投递失败',
    webhook_ping: 'Webhook 连通成功',
    quota_window_refreshed: '配额窗口已刷新',
    'task.completed': '定时任务执行完成',
    'task.failed': '定时任务执行失败',
    'workflow.completed': '工作流执行完成',
    'workflow.failed': '工作流执行失败',
    created: '已创建',
    updated: '已更新',
    deleted: '已删除',
    revealed: '已查看',
    imported: '已导入',
    exported: '已导出',
  };
  return names[type] || type;
};

export const FALLBACK_EVENT_CATALOG = [
  { module: 'uptime', events: ['down', 'up', 'pending', 'resource.created', 'resource.deleted', 'ssl_expiry'], dynamic_events: ['down', 'up'] },
  { module: 'server', events: ['offline', 'online', 'interrupted', 'degraded', 'cpu_high', 'cpu_normal', 'memory_high', 'memory_normal', 'disk_high', 'disk_normal', 'traffic_high', 'traffic_normal'], dynamic_events: ['offline', 'online', 'interrupted', 'degraded', 'cpu_high', 'cpu_normal', 'memory_high', 'memory_normal', 'disk_high', 'disk_normal', 'traffic_high', 'traffic_normal'] },
  { module: 'github', events: ['action_failed', 'action_recovered', 'release_published', 'star_spike', 'issue_opened', 'pull_request_opened', 'repository_unreachable', 'token_invalid', 'rate_limit_low', 'webhook_delivery_failed', 'webhook_ping'], dynamic_events: ['action_failed', 'action_recovered'] },
  { module: 'system', events: ['database.backup', 'database.import', 'log.cleanup', 'migration.failed', 'cpu_high', 'cpu_normal', 'memory_high', 'memory_normal', 'disk_high', 'disk_normal'], dynamic_events: ['cpu_high', 'cpu_normal', 'memory_high', 'memory_normal', 'disk_high', 'disk_normal'] },
  { module: 'filebox', events: ['resource.created', 'resource.deleted', 'cleanup'] },
  { module: 'totp', events: ['resource.created', 'resource.updated', 'resource.deleted', 'security.revealed', 'backup.imported', 'backup.exported'] },
  { module: 'antigravity', events: ['quota_window_refreshed'] },
  { module: 'cron', events: ['task.completed', 'task.failed', 'workflow.completed', 'workflow.failed'] },
];
