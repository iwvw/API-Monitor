import { MODULE_GROUPS } from '../../store.js';

export const AUTH_LABEL = {
  public: '公开',
  session: '登录',
  api_key: 'API Key',
  agent_key: 'Agent Key',
};

export const AUTH_TONE = {
  public: 'success',
  session: 'brand',
  api_key: 'warning',
  agent_key: 'info',
};

export const STATUS_LABEL = {
  active: '可用',
  retired: '停用',
  unknown: '未知',
};

export const STATUS_TONE = {
  active: 'success',
  retired: 'danger',
  unknown: 'neutral',
};

export const RESPONSE_LABEL = {
  json: 'JSON',
  static: '静态',
  stream: '流式',
  websocket: 'WebSocket',
  proxy: '代理',
};

export const MODULE_LABELS = {
  health: '健康检查',
  migration: '迁移状态',
  'auth-2fa-status': '2FA 状态',
  'auth-login-options': '登录选项',
  'auth-plugin-pairing': '插件配对',
  'auth-github-config': 'GitHub 登录配置',
  'auth-github-login': 'GitHub 登录',
  'auth-2fa-management': '2FA 管理',
  'auth-webauthn-management': 'WebAuthn 管理',
  'auth-webauthn-login': 'WebAuthn 登录',
  auth: '认证',
  'settings-logs': '系统日志',
  'settings-site-brand': '站点品牌',
  'settings-database': '数据库维护',
  settings: '系统设置',
  'system-host-metrics': '主机指标',
  'system-api-stats': 'API 统计',
  'system-api-docs': 'API 文档',
  'system-api-keys': 'API 密钥',
  'system-ai-access': 'AI 接入',
  'ai-access': 'AI 接入',
  'system-logs': '系统日志',
  totp: 'TOTP 动态码',
  filebox: '文件柜',
  uptime: '可用性监测',
  notification: '通知',
  scheduler: '工作流调度',
  cron: '定时任务',
  backup: '备份中心',
  'cloudflare-accounts': '账号',
  'cloudflare-templates': 'DNS 模板',
  'cloudflare-pages': 'Pages',
  'cloudflare-workers': 'Workers',
  'cloudflare-r2': 'R2 存储',
  'cloudflare-tunnels': 'Tunnels',
  'cloudflare-dns': 'DNS',
  'cloudflare-zone-resources': 'Zone 资源',
  aliyun: '阿里云',
  tencent: '腾讯云',
  oracle: 'Oracle OCI',
  gcp: 'Google Cloud',
  huawei: '华为云',
  'm365-public-register': 'M365 公开注册',
  m365: 'Microsoft 365',
  koyeb: 'Koyeb',
  flyio: 'Fly.io',
  'github-webhook': 'Webhook',
  'github-events': '实时事件',
  'github-public-pages': '公开页面',
  github: 'GitHub',
  'drawio-versions': '版本管理',
  'drawio-thumbnails': '缩略图',
  'drawio-export': '导出',
  'drawio-drafts': '草稿',
  'drawio-documents': '文档',
  'drawio-import': '导入',
  'drawio-render': '渲染任务',
  'drawio-settings': '设置',
  drawio: '绘图',
  'prompts-public': '公开访问',
  'prompts-versions': '版本管理',
  'prompts-entries': '条目',
  'prompts-drafts': '草稿',
  'prompts-collections': '集合',
  'prompts-settings': '设置',
  prompts: '提示词库',
  openai: 'OpenAI 网关',
  subscription: '订阅分发',
  'subscription-public': '公开订阅',
  'openai-compatible': 'OpenAI 兼容',
  'server-operations': '主机操作',
  'server-agent': 'Agent',
  'server-agent-proxy-legacy': '托管代理(旧)',
  'server-agent-proxy': '托管代理',
  'server-agent-tunnels': '托管隧道',
  'server-agent-proxy-runtime': '代理运行时',
  'server-remote-desktop': '远程桌面',
  'server-monitor': '监控',
  'server-docker': 'Docker',
  'server-docker-v2': 'Docker v2',
  'server-sftp': 'SFTP',
  'server-tasks-v2': '任务 v2',
  'server-tasks': '任务',
  'server-accounts': '主机账号',
  'server-api': '主机接口',
  'server-status-pages': '状态页',
  'server-credentials': '凭据',
  'server-metrics': '指标',
  'server-terminal': 'SSH 终端',
  'server-terminal-agent': 'Agent 终端',
  'server-websocket': 'WebSocket',
  'server-snippets': '命令片段',
};

// 与侧边栏模块顺序一致，用于接口目录的分组排序
export const GROUP_ORDER = [
  '仪表盘',
  'Cloudflare',
  '阿里云',
  '腾讯云',
  '甲骨文云',
  'Google Cloud',
  '华为云',
  'Microsoft 365',
  'GitHub',
  '主机实例',
  'PaaS',
  '定时任务',
  '可用性监测',
  '文件柜',
  '图编辑器',
  '提示词库',
  '双因子认证',
  '模型网关',
  '订阅分发',
  '通知中心',
  'API 接口',
  '系统日志',
  '系统设置',
  '认证',
  '系统',
  '基础',
];

// 后端 route.group 用的是模块展示名，先映射回模块 id 再查分区
export const GROUP_NAME_TO_MODULE_ID = {
  仪表盘: 'dashboard',
  模型网关: 'openai',
  订阅分发: 'subscription',
  Cloudflare: 'dns',
  阿里云: 'aliyun',
  腾讯云: 'tencent',
  甲骨文云: 'oracle',
  'Google Cloud': 'gcp',
  华为云: 'huawei',
  'Microsoft 365': 'm365',
  GitHub: 'github',
  主机实例: 'server',
  PaaS: 'paas',
  定时任务: 'scheduler',
  可用性监测: 'uptime',
  文件柜: 'filebox',
  图编辑器: 'drawio',
  提示词库: 'prompts',
  双因子认证: 'totp',
  通知中心: 'notification',
  'API 接口': 'apidocs',
  系统日志: 'systemlogs',
  系统设置: 'settings',
};

// 顶层分区：按侧边栏 MODULE_GROUPS 推导（仪表盘 / 云服务 / 工具箱 / API 服务 / 系统）
export const SECTION_OF_GROUP = {};
MODULE_GROUPS.forEach(section => {
  (section.modules || []).forEach(moduleId => {
    SECTION_OF_GROUP[moduleId] = section.name;
  });
  (section.subgroups || []).forEach(subgroup => {
    (subgroup.modules || []).forEach(moduleId => {
      SECTION_OF_GROUP[moduleId] = section.name;
    });
  });
  (section.trailingModules || []).forEach(moduleId => {
    SECTION_OF_GROUP[moduleId] = section.name;
  });
});

export const FALLBACK_SECTION = {
  认证: '认证',
  系统: '系统基础',
  基础: '基础',
};

export const SECTION_ORDER = ['仪表盘', '云服务', '工具箱', 'API 服务', '系统', '认证', '系统基础', '基础', '其他'];

export const AI_ACCESS_BASE = '/api/ai-access';
export const API_KEYS_BASE = '/api/api-keys';
export const OPENAPI_ROUTE = '/api/openapi.json';
export const API_SEGMENT = 'api';

export const apiDocsShellClass =
  'api-docs-workspace flex min-h-full w-full min-w-0 flex-col gap-3';
export const fixedPanelClass = 'min-h-0';

export const API_KEY_KINDS = [
  { value: 'plugin', label: '浏览器插件', prefix: 'akp_', scope: '读取 TOTP 验证码' },
  { value: 'ai', label: 'AI Agent', prefix: 'aka_', scope: '调用 AI / MCP 工具' },
  { value: 'openai', label: 'OpenAI 网关', prefix: 'ako_', scope: '调用 OpenAI 兼容网关' },
  { value: 'api', label: '通用 API', prefix: 'ak_', scope: '按所选权限访问后台 API' },
];

export const API_SCOPE_LABELS = {
  'totp:read': 'TOTP 只读',
  'ai:mcp': 'AI / MCP',
  'openai:gateway': 'OpenAI 网关',
  'api:read': 'API 读取',
  'api:write': 'API 修改',
};

export const API_KEY_EXPIRY_PRESETS = [
  { value: '7d', label: '7 天', days: 7 },
  { value: '30d', label: '30 天', days: 30 },
  { value: '90d', label: '90 天', days: 90 },
  { value: '180d', label: '180 天', days: 180 },
  { value: '365d', label: '1 年', days: 365 },
  { value: 'never', label: '长期有效', days: 0 },
];

export const API_KEY_EXPIRY_HOURS = Array.from({ length: 24 }, (_, hour) => ({
  value: String(hour).padStart(2, '0'),
  label: String(hour).padStart(2, '0'),
}));

export const API_KEY_EXPIRY_MINUTES = Array.from({ length: 60 }, (_, minute) => ({
  value: String(minute).padStart(2, '0'),
  label: String(minute).padStart(2, '0'),
}));
