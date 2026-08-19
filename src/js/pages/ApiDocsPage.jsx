import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { CalendarDotsIcon } from '@phosphor-icons/react';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { ClipboardText, DatePicker, Label, LayerCard, Loader, Pagination, Popover, Switch, Table, Tabs } from '@cloudflare/kumo';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { formatDateTime } from '../modules/utils.js';
import { MODULE_GROUPS } from '../store.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import {
  AppCard,
  EmptyState,
  StatusBadge,
  PageStack,
  SectionCard,
  TabBarOverflowActions,
  cx,
  stickyTabsBaseClass,
} from '../components/ui/AppPrimitives.jsx';
import {
  Activity,
  Bot,
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Copy,
  Download,
  Edit,
  Eye,
  EyeOff,
  FileText,
  Folder,
  FolderOpen,
  History,
  Key,
  Plug,
  Plus,
  RefreshCw,
  RotateCw,
  Search,
  Shield,
  Settings,
  Trash,
  X,
} from '../components/Icons.jsx';

const AUTH_LABEL = {
  public: '公开',
  session: '登录',
  api_key: 'API Key',
  agent_key: 'Agent Key',
};

const AUTH_TONE = {
  public: 'success',
  session: 'brand',
  api_key: 'warning',
  agent_key: 'info',
};

const STATUS_LABEL = {
  active: '可用',
  retired: '停用',
  unknown: '未知',
};

const STATUS_TONE = {
  active: 'success',
  retired: 'danger',
  unknown: 'neutral',
};

const RESPONSE_LABEL = {
  json: 'JSON',
  static: '静态',
  stream: '流式',
  websocket: 'WebSocket',
  proxy: '代理',
};

const MODULE_LABELS = {
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

const moduleLabel = moduleName => {
  if (!moduleName) return '其他';
  if (MODULE_LABELS[moduleName]) return MODULE_LABELS[moduleName];
  return moduleName
    .split('-')
    .filter(Boolean)
    .map(part => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
};

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

const AI_ACCESS_BASE = '/api/ai-access';
const API_KEYS_BASE = '/api/api-keys';
const OPENAPI_ROUTE = '/api/openapi.json';
const API_SEGMENT = 'api';
const routePrefixLiteral = (...segments) => `/${segments.join('/')}`;

const apiDocsShellClass =
  'api-docs-workspace flex min-h-full w-full min-w-0 flex-col gap-3';
const fixedPanelClass = 'min-h-0';

const createDefaultAPIKeyForm = () => {
  const expires = new Date(Date.now() + 90 * 24 * 60 * 60 * 1000);
  expires.setMinutes(expires.getMinutes() - expires.getTimezoneOffset());
  return {
    name: '',
    kind: 'plugin',
    scopes: [],
    expiresAt: expires.toISOString().slice(0, 16),
    enabled: true,
  };
};

const API_KEY_KINDS = [
  { value: 'plugin', label: '浏览器插件', prefix: 'akp_', scope: '读取 TOTP 验证码' },
  { value: 'ai', label: 'AI Agent', prefix: 'aka_', scope: '调用 AI / MCP 工具' },
  { value: 'openai', label: 'OpenAI 网关', prefix: 'ako_', scope: '调用 OpenAI 兼容网关' },
  { value: 'api', label: '通用 API', prefix: 'ak_', scope: '按所选权限访问后台 API' },
];

const API_SCOPE_LABELS = {
  'totp:read': 'TOTP 只读',
  'ai:mcp': 'AI / MCP',
  'openai:gateway': 'OpenAI 网关',
  'api:read': 'API 读取',
  'api:write': 'API 修改',
};

const API_KEY_EXPIRY_PRESETS = [
  { value: '7d', label: '7 天', days: 7 },
  { value: '30d', label: '30 天', days: 30 },
  { value: '90d', label: '90 天', days: 90 },
  { value: '180d', label: '180 天', days: 180 },
  { value: '365d', label: '1 年', days: 365 },
  { value: 'never', label: '长期有效', days: 0 },
];

const API_KEY_EXPIRY_HOURS = Array.from({ length: 24 }, (_, hour) => ({
  value: String(hour).padStart(2, '0'),
  label: String(hour).padStart(2, '0'),
}));

const API_KEY_EXPIRY_MINUTES = Array.from({ length: 60 }, (_, minute) => ({
  value: String(minute).padStart(2, '0'),
  label: String(minute).padStart(2, '0'),
}));

const formatKeyTime = value => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString('zh-CN', { hour12: false });
};

const toLocalDateTimeInput = value => {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  date.setMinutes(date.getMinutes() - date.getTimezoneOffset());
  return date.toISOString().slice(0, 16);
};

const parseLocalDateTime = value => {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
};

const isSameLocalDate = (left, right) =>
  left.getFullYear() === right.getFullYear() &&
  left.getMonth() === right.getMonth() &&
  left.getDate() === right.getDate();

const matchExpiryPreset = expiresAt => {
  if (!expiresAt) return 'never';
  const expiry = parseLocalDateTime(expiresAt);
  if (!expiry) return '';
  const matchedPreset = API_KEY_EXPIRY_PRESETS.find(preset => {
    if (preset.days === 0) return false;
    const target = new Date();
    target.setDate(target.getDate() + preset.days);
    return isSameLocalDate(expiry, target);
  });
  return matchedPreset?.value || '';
};

const apiKeyStatus = key => {
  if (key.revokedAt) return { label: '已撤销', tone: 'danger' };
  if (!key.enabled) return { label: '已停用', tone: 'neutral' };
  if (key.expiresAt && new Date(key.expiresAt).getTime() <= Date.now()) {
    return { label: '已过期', tone: 'warning' };
  }
  return { label: '使用中', tone: 'success' };
};

const normalizeSummary = (summary = {}) => ({
  total: Number(summary.total) || 0,
  byOwner: summary.byOwner || {},
  byAuth: summary.byAuth || {},
  byGroup: summary.byGroup || {},
  byStatus: summary.byStatus || {},
  byResponse: summary.byResponse || {},
  openapiRoute: summary.openapiRoute || OPENAPI_ROUTE,
});

const methodClassName = method => {
  const normalized = method.toUpperCase();
  if (normalized === 'GET') return 'border-kumo-info/20 bg-kumo-info/10 text-kumo-info';
  if (normalized === 'POST') return 'border-kumo-success/20 bg-kumo-success/10 text-kumo-success';
  if (normalized === 'PUT' || normalized === 'PATCH')
    return 'border-kumo-warning/20 bg-kumo-warning/10 text-kumo-warning';
  if (normalized === 'DELETE') return 'border-kumo-danger/20 bg-kumo-danger/10 text-kumo-danger';
  return 'border-kumo-line bg-kumo-recessed text-kumo-subtle';
};

const getRouteKey = route => `${route.prefix}:${route.module}:${route.auth}`;

const sortRoutes = routes =>
  [...routes].sort((a, b) => {
    const sectionOrder = sectionOrderIndex(sectionOfGroup(a.group)) - sectionOrderIndex(sectionOfGroup(b.group));
    if (sectionOrder !== 0) return sectionOrder;
    const groupOrder = groupOrderIndex(a.group) - groupOrderIndex(b.group);
    if (groupOrder !== 0) return groupOrder;
    const groupSort = String(a.group).localeCompare(String(b.group), 'zh-CN');
    if (groupSort !== 0) return groupSort;
    return String(a.prefix).localeCompare(String(b.prefix), 'en');
  });

const routeGroup = route => {
  const prefix = route.prefix || '';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'openai')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'chat')) ||
    prefix.startsWith(routePrefixLiteral('v1'))
  )
    return '模型网关';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'subscription')) ||
    prefix.startsWith(routePrefixLiteral('sub'))
  )
    return '订阅分发';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cloudflare'))) return 'Cloudflare';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'aliyun'))) return '阿里云';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'tencent'))) return '腾讯云';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'oracle'))) return '甲骨文云';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'm365'))) return 'Microsoft 365';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'github'))) return 'GitHub';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server')) ||
    prefix.startsWith(routePrefixLiteral('ws', 'ssh')) ||
    prefix.startsWith(routePrefixLiteral('ws', 'agent-terminal')) ||
    prefix.startsWith(routePrefixLiteral('socket.io'))
  )
    return '主机实例';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'koyeb')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'flyio'))
  )
    return 'PaaS';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'scheduler')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cron'))
  )
    return '定时任务';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'uptime'))) return '可用性监测';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'filebox'))) return '文件柜';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'drawio'))) return '图编辑器';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'prompts'))) return '提示词库';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'totp'))) return '双因子认证';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'notification'))) return '通知中心';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'auth'))) return '认证';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'system', 'logs')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'logs')) ||
    prefix.startsWith(routePrefixLiteral('ws', 'logs')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'sys-logs')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'app-log-file')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'log-settings')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'enforce-log-limits')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'clear-logs')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'clear-app-logs'))
  )
    return '系统日志';
  if (
    prefix === routePrefixLiteral(API_SEGMENT, 'system', 'api-docs') ||
    prefix === routePrefixLiteral(API_SEGMENT, 'system', 'openapi.json') ||
    prefix === routePrefixLiteral(API_SEGMENT, 'openapi.json') ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'api-keys')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'system', 'api-keys')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'ai-access')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'system', 'ai-access')) ||
    prefix === routePrefixLiteral(API_SEGMENT, 'ai', 'manifest') ||
    prefix === routePrefixLiteral(API_SEGMENT, 'ai', 'mcp')
  )
    return 'API 接口';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'backup'))
  )
    return '系统设置';
  if (
    prefix === '/health' ||
    prefix === routePrefixLiteral(API_SEGMENT, 'migration', 'status') ||
    prefix === routePrefixLiteral(API_SEGMENT, 'system', 'host-metrics') ||
    prefix === routePrefixLiteral(API_SEGMENT, 'system', 'api-stats')
  )
    return '仪表盘';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'system'))) return '系统';
  return '基础';
};

// 与侧边栏模块顺序一致，用于接口目录的分组排序
const GROUP_ORDER = [
  '仪表盘',
  'Cloudflare',
  '阿里云',
  '腾讯云',
  '甲骨文云',
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
const groupOrderIndex = group => {
  const index = GROUP_ORDER.indexOf(String(group || ''));
  return index === -1 ? GROUP_ORDER.length : index;
};

// 顶层分区：按侧边栏 MODULE_GROUPS 推导（仪表盘 / 云服务 / 工具箱 / API 服务 / 系统）
const SECTION_OF_GROUP = {};
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

// 后端 route.group 用的是模块展示名，先映射回模块 id 再查分区
const GROUP_NAME_TO_MODULE_ID = {
  仪表盘: 'dashboard',
  模型网关: 'openai',
  订阅分发: 'subscription',
  Cloudflare: 'dns',
  阿里云: 'aliyun',
  腾讯云: 'tencent',
  甲骨文云: 'oracle',
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

const FALLBACK_SECTION = {
  认证: '认证',
  系统: '系统基础',
  基础: '基础',
};

const SECTION_ORDER = ['仪表盘', '云服务', '工具箱', 'API 服务', '系统', '认证', '系统基础', '基础', '其他'];
const sectionOfGroup = group => {
  const moduleId = GROUP_NAME_TO_MODULE_ID[group];
  if (moduleId && SECTION_OF_GROUP[moduleId]) return SECTION_OF_GROUP[moduleId];
  return FALLBACK_SECTION[group] || '其他';
};
const sectionOrderIndex = section => {
  const index = SECTION_ORDER.indexOf(section);
  return index === -1 ? SECTION_ORDER.length : index;
};

const routeDescription = route => {
  const prefix = route.prefix || '';
  if (prefix === '/health') return '服务健康检查与版本状态';
  if (prefix === '/api/migration/status') return '读取迁移状态、路由归属和废弃模块信息';
  if (prefix === '/api/system/api-docs') return '读取系统自动生成的 API 文档清单';
  if (prefix === '/api/system/openapi.json') return '导出 OpenAPI 3.1 接口文档';
  if (prefix === '/api/openapi.json') return '导出 OpenAPI 3.1 接口文档';
  if (prefix === '/api/ai-access') return '读取 AI 接入、Agent Key 和审计概览';
  if (prefix === '/api/ai-access/key/rotate') return '轮换 AI Agent Key';
  if (prefix.startsWith('/api/ai-access/mcp-servers')) return '管理 AI 接入的 MCP 服务配置';
  if (prefix.startsWith('/api/ai-access/skills')) return '管理 AI 接入的 Skill 配置';
  if (prefix === '/api/ai-access/audit') return '分页查询 AI 接入调用审计';
  if (prefix === '/api/ai-access/audit/clear') return '清空 AI 接入调用审计';
  if (prefix === '/api/system/ai-access') return '读取 AI 接入、Agent Key 和审计概览';
  if (prefix === '/api/system/ai-access/key/rotate') return '轮换 AI Agent Key';
  if (prefix.startsWith('/api/system/ai-access/mcp-servers')) return '管理 AI 接入的 MCP 服务配置';
  if (prefix.startsWith('/api/system/ai-access/skills')) return '管理 AI 接入的 Skill 配置';
  if (prefix === '/api/system/ai-access/audit') return '分页查询 AI 接入调用审计';
  if (prefix === '/api/system/ai-access/audit/clear') return '清空 AI 接入调用审计';
  if (prefix === '/api/ai/manifest') return '供外部 AI 客户端读取系统接入能力清单';
  if (prefix === '/api/ai/mcp') return '供外部 AI 客户端通过 MCP 调用系统工具';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'auth')))
    return '登录认证、会话校验和退出登录';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'database')))
    return '数据库统计、分析、导入导出和维护操作';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'log')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'sys-logs')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'app-log-file'))
  )
    return '系统日志读取、清理和保留策略';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings')))
    return '读取和保存系统运行配置';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'system')))
    return '系统运行状态、日志、统计和管理能力';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'logs')) ||
    prefix.startsWith(routePrefixLiteral('ws', 'logs'))
  )
    return '读取系统日志和实时日志流';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cloudflare', 'accounts')))
    return '管理 Cloudflare 账号、令牌、Pages、Workers、R2、Tunnel 和 Zone 资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cloudflare')))
    return '管理 Cloudflare DNS、边缘资源和账号资产';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'agent')))
    return '管理服务器 Agent 安装、密钥、状态和心跳';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'metrics')))
    return '读取服务器指标历史、最新指标和清理记录';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'network-quality')))
    return '管理服务器网络质量目标和采集结果';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'sftp')))
    return '通过 SFTP 浏览、读写、上传和下载文件';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'tasks')))
    return '管理服务器任务、任务日志和执行流';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server')))
    return '管理主机实例、凭据、Docker、终端和监控能力';
  if (
    prefix.startsWith(routePrefixLiteral('ws', 'ssh')) ||
    prefix.startsWith(routePrefixLiteral('ws', 'agent-terminal')) ||
    prefix.startsWith(routePrefixLiteral('socket.io'))
  )
    return '主机终端和 Agent 实时连接';
  if (
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'openai')) ||
    prefix.startsWith(routePrefixLiteral('v1')) ||
    prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'chat'))
  )
    return 'OpenAI 兼容模型代理、聊天和流式响应';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'aliyun')))
    return '管理阿里云 DNS、计算和云资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'tencent')))
    return '管理腾讯云 DNS、计算和云资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'koyeb')))
    return '管理 Koyeb 账号、服务和部署资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'flyio')))
    return '管理 Fly.io 账号、应用和机器资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'totp')))
    return '管理双因子认证账户、分组和动态验证码';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'filebox')))
    return '管理文件柜上传、分享、历史记录和下载';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'uptime')))
    return '管理可用性监测、公开状态、推送和徽章';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'notification')))
    return '管理通知渠道、规则、事件目录和发送历史';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'scheduler')))
    return '管理工作流调度、DAG、运行记录和分布式节点';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cron')))
    return '管理定时任务、调度器和执行日志';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'backup')))
    return '管理本地备份配置、备份记录和执行器';
  if (route.owner === 'retired') return '历史模块已停用，暂未迁移到当前后端';
  return route.description || '系统接口';
};

const routeStatus = route => (route.owner === 'retired' ? 'retired' : 'active');

const routeMethods = route => {
  if (route.responseMode === 'websocket') return ['GET'];
  if (route.responseMode === 'stream')
    return route.prefix?.startsWith('/v1') ? ['GET', 'POST'] : ['GET'];
  if (route.owner === 'retired') return ['GET'];
  if (route.matchMode === 'pattern') return ['GET', 'POST', 'PUT', 'DELETE'];
  if (
    route.auth === 'public' &&
    (route.prefix === '/health' || String(route.description || '').includes('status'))
  ) {
    return ['GET'];
  }
  return ['GET', 'POST', 'PUT', 'DELETE'];
};

const countBy = (routes, keyFn) =>
  routes.reduce((acc, route) => {
    const key = keyFn(route);
    acc[key] = (acc[key] || 0) + 1;
    return acc;
  }, {});

const normalizeRoutes = (routes = []) =>
  sortRoutes(
    routes.map(route => ({
      prefix: route.prefix || '',
      module: route.module || '',
      group: route.group || routeGroup(route),
      owner: route.owner || 'go',
      auth: route.auth || 'session',
      responseMode: route.responseMode || 'json',
      description: route.description || routeDescription(route),
      detail: route.detail || route.description || routeDescription(route),
      matchMode: route.matchMode || 'prefix',
      methods:
        Array.isArray(route.methods) && route.methods.length > 0
          ? route.methods
          : routeMethods(route),
      status: route.status || routeStatus(route),
      pathParams: Array.isArray(route.pathParams) ? route.pathParams : [],
      queryParams: Array.isArray(route.queryParams) ? route.queryParams : [],
      headers: Array.isArray(route.headers) ? route.headers : [],
      requestContentType: route.requestContentType || '',
      requestExample: route.requestExample ?? null,
      responseExample: route.responseExample ?? null,
      notes: Array.isArray(route.notes) ? route.notes : [],
    }))
  );

const normalizeDocsPayload = (payload = {}) => {
  const routes = normalizeRoutes(Array.isArray(payload.routes) ? payload.routes : []);
  const summary = normalizeSummary({
    total: routes.length,
    byOwner: countBy(routes, route => route.owner),
    byAuth: countBy(routes, route => route.auth),
    byGroup: countBy(routes, route => route.group),
    byStatus: countBy(routes, route => route.status),
    byResponse: countBy(routes, route => route.responseMode),
    ...(payload.summary || {}),
  });

  return {
    ...payload,
    routes,
    summary,
    aiAccess: payload.aiAccess || {
      plannedModules: [
        {
          id: 'providers',
          name: '模型端点',
          description: 'OpenAI 兼容端点与模型',
        },
        {
          id: 'permissions',
          name: '工具权限',
          description: '调用权限',
        },
        {
          id: 'audit',
          name: '调用审计',
          description: '调用记录',
        },
      ],
    },
  };
};

const fetchJsonEnvelope = async url => {
  const response = await fetch(url, { headers: getAuthHeaders() });
  const result = await response.json().catch(() => ({}));
  if (!response.ok || result.success === false) {
    throw new Error(result.error || `${url} 加载失败`);
  }
  return result.data || result;
};

const apiRequest = async (url, options = {}) => {
  const response = await fetch(url, {
    ...options,
    headers: {
      ...getAuthHeaders(),
      ...(options.headers || {}),
    },
  });
  const result = await response.json().catch(() => ({}));
  if (!response.ok || result.success === false) {
    throw new Error(result.error || `${url} 请求失败`);
  }
  return result.data || result;
};

const formatJSON = value => {
  if (value == null) return '';
  if (typeof value === 'string') return value;
  return JSON.stringify(value, null, 2);
};

const formatAuditDetails = value => {
  if (value == null || value === '') return '—';
  if (typeof value !== 'string') return formatJSON(value);
  try {
    return JSON.stringify(JSON.parse(value), null, 2);
  } catch {
    return value;
  }
};

function StatCard({ icon: Icon, label, value, tone = 'brand' }) {
  const toneClass =
    {
      brand: 'bg-kumo-info/6 text-kumo-info',
      success: 'bg-kumo-success/6 text-kumo-success',
      warning: 'bg-kumo-warning/8 text-kumo-warning',
      info: 'bg-brand/7 text-brand',
    }[tone] || 'bg-kumo-info/6 text-kumo-info';

  return (
    <AppCard padding="none" className={cx('min-w-0 p-2 cq-sm:p-3', toneClass)}>
      <div className="flex items-center justify-between gap-2 text-[11px] text-kumo-subtle cq-sm:gap-3 cq-sm:text-xs">
        <span className="truncate">{label}</span>
        <span className="shrink-0">
          <Icon className="h-5 w-5" />
        </span>
      </div>
      <div className="mt-1">
        <div className="truncate font-mono text-base font-bold text-kumo-strong cq-sm:text-lg">
          {value}
        </div>
      </div>
    </AppCard>
  );
}

function FilterSelect({ label, value, onValueChange, items }) {
  return (
    <Select
      size="sm"
      aria-label={label}
      value={value}
      onValueChange={onValueChange}
      items={items}
      className="w-full min-w-0 text-xs text-kumo-strong"
    />
  );
}

function RouteMethodPills({ methods = [] }) {
  return (
    <div className="flex flex-wrap gap-1">
      {methods.map(method => (
        <span
          key={method}
          className={cx(
            'inline-flex items-center rounded border px-1.5 py-0.5 font-mono text-[10px] font-bold',
            methodClassName(method)
          )}
        >
          {method}
        </span>
      ))}
    </div>
  );
}

function RouteTree({ routes, selectedRoute, onSelect, revealAll }) {
  const selectedKey = selectedRoute ? getRouteKey(selectedRoute) : '';

  const tree = useMemo(() => {
    const sectionMap = new Map();
    routes.forEach(route => {
      const groupName = route.group || '基础';
      const sectionName = sectionOfGroup(groupName);
      if (!sectionMap.has(sectionName)) sectionMap.set(sectionName, new Map());
      const groupMap = sectionMap.get(sectionName);
      if (!groupMap.has(groupName)) groupMap.set(groupName, new Map());
      const moduleMap = groupMap.get(groupName);
      const moduleName = route.module || '';
      if (!moduleMap.has(moduleName)) moduleMap.set(moduleName, []);
      moduleMap.get(moduleName).push(route);
    });
    return [...sectionMap.entries()].map(([section, groupMap]) => ({
      section,
      count: [...groupMap.values()].reduce(
        (acc, moduleMap) => acc + [...moduleMap.values()].reduce((sum, list) => sum + list.length, 0),
        0
      ),
      groups: [...groupMap.entries()].map(([group, moduleMap]) => ({
        group,
        count: [...moduleMap.values()].reduce((acc, list) => acc + list.length, 0),
        modules: [...moduleMap.entries()].map(([module, list]) => ({
          module,
          label: moduleLabel(module),
          count: list.length,
          routes: list,
        })),
      })),
    }));
  }, [routes]);

  const [collapsedSections, setCollapsedSections] = useState(() => new Set());
  const [collapsedGroups, setCollapsedGroups] = useState(() => new Set());
  const [collapsedModules, setCollapsedModules] = useState(() => new Set());

  // 数据异步到达前 tree 为空，此时 collapsed 集为空会误判为「全部展开」。
  // 数据到达（或搜索态切换）时重置三份折叠集为「全折叠」，之后由用户交互接管。
  useEffect(() => {
    if (tree.length === 0) return;
    setCollapsedSections(new Set(tree.map(item => item.section)));
    setCollapsedGroups(new Set(tree.flatMap(item => item.groups.map(group => group.group))));
    setCollapsedModules(
      new Set(tree.flatMap(item => item.groups.flatMap(group => group.modules.map(mod => `${group.group}\u0000${mod.module}`))))
    );
  }, [tree, revealAll]);

  // 用户点击选中某路由后，自动展开其所在层级；初始未选择时保持全部折叠。
  useEffect(() => {
    if (!selectedRoute || !selectedKey) return;
    const sectionName = sectionOfGroup(selectedRoute.group);
    const groupName = selectedRoute.group || '基础';
    const moduleName = selectedRoute.module || '';
    setCollapsedSections(current => {
      if (!current.has(sectionName)) return current;
      const next = new Set(current);
      next.delete(sectionName);
      return next;
    });
    setCollapsedGroups(current => {
      if (!current.has(groupName)) return current;
      const next = new Set(current);
      next.delete(groupName);
      return next;
    });
    setCollapsedModules(current => {
      const key = `${groupName}\u0000${moduleName}`;
      if (!current.has(key)) return current;
      const next = new Set(current);
      next.delete(key);
      return next;
    });
  }, [selectedRoute, selectedKey]);

  const toggleSection = sectionName => {
    setCollapsedSections(current => {
      const next = new Set(current);
      if (next.has(sectionName)) next.delete(sectionName);
      else next.add(sectionName);
      return next;
    });
  };

  const toggleGroup = groupName => {
    setCollapsedGroups(current => {
      const next = new Set(current);
      if (next.has(groupName)) next.delete(groupName);
      else next.add(groupName);
      return next;
    });
  };

  const toggleModule = (groupName, moduleName) => {
    const key = `${groupName}\u0000${moduleName}`;
    setCollapsedModules(current => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  const expandAll = () => {
    setCollapsedSections(new Set());
    setCollapsedGroups(new Set());
    setCollapsedModules(new Set());
  };

  const collapseAll = () => {
    setCollapsedSections(new Set(tree.map(item => item.section)));
    setCollapsedGroups(new Set(tree.flatMap(item => item.groups.map(group => group.group))));
    setCollapsedModules(
      new Set(tree.flatMap(item => item.groups.map(group => `${group.group}\u0000${group.module}`)))
    );
  };

  const sectionCollapsed = name => !revealAll && collapsedSections.has(name);
  const groupCollapsed = name => !revealAll && collapsedGroups.has(name);
  const moduleCollapsed = (groupName, moduleName) =>
    !revealAll && collapsedModules.has(`${groupName}\u0000${moduleName}`);

  return (
    <SectionCard
      title={`接口目录 (${routes.length})`}
      icon={<Search className="h-4 w-4 text-brand" />}
      action={
        <div className="flex items-center gap-1">
          <Button size="sm" variant="ghost" onClick={expandAll} className="gap-1">
            <ChevronDown className="h-3.5 w-3.5" />
            <span className="hidden cq-sm:inline">展开</span>
          </Button>
          <Button size="sm" variant="ghost" onClick={collapseAll} className="gap-1">
            <ChevronUp className="h-3.5 w-3.5" />
            <span className="hidden cq-sm:inline">折叠</span>
          </Button>
        </div>
      }
      className="min-h-0 self-start"
      bodyPadding="none"
      bodyClassName="flex min-w-0 flex-col"
    >
      {routes.length === 0 ? (
        <AppCard padding="none" className="flex min-h-0 items-center justify-center">
          <EmptyState
            icon={Search}
            title="没有匹配的接口"
            description="换个筛选条件试试"
            card={false}
            className="min-h-0"
          />
        </AppCard>
      ) : (
        <div className="divide-y divide-kumo-line/80">
          {tree.map(sectionItem => (
            <div key={sectionItem.section}>
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => toggleSection(sectionItem.section)}
                className="h-auto w-full min-w-0 !justify-between gap-1.5 rounded-none px-3 py-2 text-left"
              >
                <span className="flex min-w-0 items-center gap-2">
                  {sectionCollapsed(sectionItem.section) ? (
                    <ChevronRight className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
                  ) : (
                    <ChevronDown className="h-3.5 w-3.5 shrink-0 text-kumo-subtle" />
                  )}
                  {sectionCollapsed(sectionItem.section) ? (
                    <Folder className="h-4 w-4 shrink-0 text-brand/80" />
                  ) : (
                    <FolderOpen className="h-4 w-4 shrink-0 text-brand" />
                  )}
                  <span className="truncate text-xs font-bold text-kumo-strong">
                    {sectionItem.section}
                  </span>
                </span>
                <StatusBadge tone="neutral">{sectionItem.count}</StatusBadge>
              </Button>
              {!sectionCollapsed(sectionItem.section) && (
                <div className="border-t border-kumo-line/50">
                  {sectionItem.groups.map(groupItem => (
                    <div key={groupItem.group}>
                      <Button
                        type="button"
                        size="sm"
                        variant="ghost"
                        title={groupItem.group}
                        onClick={() => toggleGroup(groupItem.group)}
                        className="h-auto w-full min-w-0 !justify-between gap-1.5 rounded-none py-1.5 pl-7 pr-3 text-left"
                      >
                        <span className="flex min-w-0 items-center gap-1.5">
                          {groupCollapsed(groupItem.group) ? (
                            <ChevronRight className="h-3 w-3 shrink-0 text-kumo-subtle" />
                          ) : (
                            <ChevronDown className="h-3 w-3 shrink-0 text-kumo-subtle" />
                          )}
                          <span className="truncate text-[11px] font-semibold text-kumo-subtle">
                            {groupItem.group}
                          </span>
                        </span>
                        <StatusBadge tone="neutral">{groupItem.count}</StatusBadge>
                      </Button>
                      {!groupCollapsed(groupItem.group) && (
                        <div className="border-t border-kumo-line/40">
                          {groupItem.modules.map(moduleItem => (
                            <div key={moduleItem.module}>
                              <Button
                                type="button"
                                size="sm"
                                variant="ghost"
                                title={moduleItem.module}
                                onClick={() => toggleModule(groupItem.group, moduleItem.module)}
                                className="h-auto w-full min-w-0 !justify-between gap-1.5 rounded-none py-1.5 pl-11 pr-3 text-left"
                              >
                                <span className="flex min-w-0 items-center gap-1.5">
                                  {moduleCollapsed(groupItem.group, moduleItem.module) ? (
                                    <ChevronRight className="h-3 w-3 shrink-0 text-kumo-subtle" />
                                  ) : (
                                    <ChevronDown className="h-3 w-3 shrink-0 text-kumo-subtle" />
                                  )}
                                  <span className="truncate text-[11px] font-semibold text-kumo-subtle">
                                    {moduleItem.label}
                                  </span>
                                </span>
                                <StatusBadge tone="neutral">{moduleItem.count}</StatusBadge>
                              </Button>
                              {!moduleCollapsed(groupItem.group, moduleItem.module) && (
                                <div className="border-t border-kumo-line/40">
                                  {moduleItem.routes.map(route => {
                                    const active = selectedKey === getRouteKey(route);
                                    return (
                                      <Button
                                        key={getRouteKey(route)}
                                        type="button"
                                        size="sm"
                                        variant="ghost"
                                        onClick={() => onSelect(route)}
                                        className={cx(
                                          'h-auto w-full min-w-0 flex-col items-stretch gap-1 rounded-none py-2 pl-14 pr-3 text-left',
                                          active && 'bg-brand/10'
                                        )}
                                      >
<div className="flex min-w-0 items-center justify-between gap-2">
                                          <div className="min-w-0 truncate font-mono text-xs font-bold text-kumo-strong">
                                            {route.prefix}
                                          </div>
                                          <StatusBadge tone={STATUS_TONE[route.status]}>
                                            {STATUS_LABEL[route.status] || route.status}
                                          </StatusBadge>
                                        </div>
                                        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                                          <RouteMethodPills methods={route.methods} />
                                          <StatusBadge tone={AUTH_TONE[route.auth]}>
                                            {AUTH_LABEL[route.auth] || route.auth}
                                          </StatusBadge>
                                        </div>
                                        <div className="line-clamp-1 text-xs leading-relaxed text-kumo-subtle">
                                          {route.description}
                                        </div>
                                      </Button>
                                    );
                                  })}
                                </div>
                              )}
                            </div>
                          ))}
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </SectionCard>
  );
}

function ParamTable({ title, items }) {
  if (!items?.length) return null;
  return (
    <div>
      <div className="mb-2 text-xs font-semibold text-kumo-subtle">{title}</div>
      <div className="space-y-2">
        {items.map(item => (
          <div
            key={`${title}:${item.in}:${item.name}`}
            className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2"
          >
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-mono text-xs font-bold text-kumo-strong">{item.name}</span>
              <StatusBadge tone="neutral">{item.in}</StatusBadge>
              <StatusBadge tone={item.required ? 'warning' : 'neutral'}>
                {item.required ? '必填' : '可选'}
              </StatusBadge>
            </div>
            <div className="mt-1 text-xs leading-relaxed text-kumo-subtle">
              {item.description || '-'}
            </div>
            {item.example ? (
              <div className="mt-1 font-mono text-[11px] text-kumo-subtle">
                例如: {item.example}
              </div>
            ) : null}
          </div>
        ))}
      </div>
    </div>
  );
}

const buildCurlExample = route => {
  const method = route.methods?.[0] || 'GET';
  const lines = [`curl -X ${method} "${window.location.origin}${route.prefix}"`];

  if (route.auth === 'session') {
    // session 认证通过 Cookie 自动携带，无需额外请求头。
  } else if (route.auth === 'api_key') {
    lines.push('  -H "Authorization: Bearer sk-xxx"');
  } else if (route.auth === 'agent_key') {
    lines.push('  -H "Authorization: Bearer am-xxx"');
  }

  if (route.requestExample) {
    const contentType = route.requestContentType || 'application/json';
    lines.push(`  -H "Content-Type: ${contentType}"`);
    if (contentType === 'application/json') {
      lines.push(`  -d '${formatJSON(route.requestExample)}'`);
    }
  }

  return lines.join(' \\\n');
};

function RouteDetail({ route, openapiRoute }) {
  const copyText = async (text, message) => {
    try {
      await navigator.clipboard.writeText(text);
      toast.success(message);
    } catch (error) {
      console.error('copy failed:', error);
      toast.error('复制失败');
    }
  };

  if (!route) {
    return (
      <div>
        <AppCard padding="none" className="flex min-h-0 items-center justify-center">
          <EmptyState
            icon={FileText}
            title="选择一个接口"
            description="从左侧选择接口"
            card={false}
            className="min-h-0"
          />
        </AppCard>
      </div>
    );
  }

  const curl = buildCurlExample(route);

  return (
    <SectionCard
      title={<span className="break-all font-mono text-base">{route.prefix}</span>}
      icon={<FileText className="h-4 w-4 text-brand" />}
      meta={
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge tone={STATUS_TONE[route.status]}>
            {STATUS_LABEL[route.status] || route.status}
          </StatusBadge>
          <StatusBadge tone={AUTH_TONE[route.auth]}>
            {AUTH_LABEL[route.auth] || route.auth}
          </StatusBadge>
          <StatusBadge tone="neutral">
            {RESPONSE_LABEL[route.responseMode] || route.responseMode}
          </StatusBadge>
        </div>
      }
      actions={
        <div className="flex shrink-0 gap-2">
          <Button
            size="sm"
            variant="secondary"
            onClick={() => copyText(route.prefix, '接口路径已复制')}
            className="gap-1.5"
          >
            <Copy className="h-3.5 w-3.5" />
            <span>路径</span>
          </Button>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => copyText(curl, 'cURL 已复制')}
            className="gap-1.5"
          >
            <Copy className="h-3.5 w-3.5" />
            <span>cURL</span>
          </Button>
        </div>
      }
      className="min-h-0"
      bodyPadding="lg"
      bodyClassName="flex min-w-0 flex-col"
    >
      <div className="grid gap-3 py-4 cq-sm:grid-cols-2">
        <InfoRow label="模块" value={route.module} />
        <InfoRow label="分组" value={route.group} />
        <InfoRow label="归属" value={route.owner} />
        <InfoRow label="匹配模式" value={route.matchMode} />
        <InfoRow label="认证方式" value={AUTH_LABEL[route.auth] || route.auth} />
        <InfoRow
          label="响应类型"
          value={RESPONSE_LABEL[route.responseMode] || route.responseMode}
        />
      </div>

      <div className="flex-1 space-y-3 border-t border-kumo-line pt-4">
        <div>
          <div className="mb-2 text-xs font-semibold text-kumo-subtle">接口说明</div>
          <div className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2 text-xs leading-relaxed text-kumo-subtle">
            {route.detail || route.description}
          </div>
        </div>
        <div>
          <div className="mb-2 text-xs font-semibold text-kumo-subtle">请求方法</div>
          <RouteMethodPills methods={route.methods} />
        </div>
        <ParamTable title="路径参数" items={route.pathParams} />
        <ParamTable title="查询参数" items={route.queryParams} />
        <ParamTable title="认证与请求头" items={route.headers} />
        {route.notes?.length ? (
          <div>
            <div className="mb-2 text-xs font-semibold text-kumo-subtle">调用提示</div>
            <div className="space-y-2">
              {route.notes.map(note => (
                <div
                  key={note}
                  className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2 text-xs leading-relaxed text-kumo-subtle"
                >
                  {note}
                </div>
              ))}
            </div>
          </div>
        ) : null}
        <SnippetBox label="cURL 示例" value={curl} onCopy={copyText} />
        {route.requestExample ? (
          <SnippetBox label="请求示例" value={formatJSON(route.requestExample)} onCopy={copyText} />
        ) : null}
        {route.responseExample ? (
          <SnippetBox
            label="响应示例"
            value={formatJSON(route.responseExample)}
            onCopy={copyText}
          />
        ) : null}
        {openapiRoute && (
          <div>
            <div className="mb-2 text-xs font-semibold text-kumo-subtle">OpenAPI 文档</div>
            <div className="flex min-w-0 flex-wrap items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/40 px-3 py-2">
              <span className="min-w-0 flex-1 truncate font-mono text-xs text-kumo-strong">
                {openapiRoute}
              </span>
              <Button
                size="sm"
                variant="ghost"
                onClick={() => copyText(openapiRoute, 'OpenAPI 地址已复制')}
                className="gap-1.5"
              >
                <Copy className="h-3.5 w-3.5" />
                <span>复制</span>
              </Button>
            </div>
          </div>
        )}
      </div>
    </SectionCard>
  );
}

function InfoRow({ label, value }) {
  return (
    <div className="min-w-0 rounded-md border border-kumo-line/80 bg-kumo-recessed/30 px-3 py-2">
      <div className="text-[11px] font-semibold text-kumo-subtle">{label}</div>
      <div className="mt-1 truncate font-mono text-xs font-bold text-kumo-strong">
        {value || '-'}
      </div>
    </div>
  );
}

function SnippetBox({ label, value, onCopy }) {
  return (
    <div className="min-w-0 rounded-md border border-kumo-line bg-kumo-recessed/35">
      <div className="flex items-center justify-between gap-2 border-b border-kumo-line px-3 py-2">
        <div className="truncate text-xs font-bold text-kumo-strong">{label}</div>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => onCopy(value, `${label} 已复制`)}
          className="gap-1.5"
        >
          <Copy className="h-3.5 w-3.5" />
          <span>复制</span>
        </Button>
      </div>
      <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all p-3 font-mono text-[11px] leading-relaxed text-kumo-subtle">
        {value}
      </pre>
    </div>
  );
}

const POLICY_CARDS = [
  { value: 'minimal', title: '只读', Icon: Eye },
  { value: 'standard', title: '标准', Icon: Shield },
  { value: 'full', title: '全部权限', Icon: Key },
];

function AIAccessConsole({
  aiAccess,
  loading,
  error,
  keyVisible,
  setKeyVisible,
  onRefresh,
  onRotateKey,
  onToggleWrite,
  onSetPolicy,
  onCopy,
}) {
  if (loading) {
    return (
      <AppCard padding="lg">
        <SkeletonLine className="h-5 w-36" />
        <SkeletonLine className="mt-4 h-80 w-full" />
      </AppCard>
    );
  }

  if (error) {
    return (
      <EmptyState
        icon={Bot}
        title="AI 接入暂不可用"
        description={error}
        action={
          <Button size="sm" variant="secondary" onClick={onRefresh}>
            重试
          </Button>
        }
      />
    );
  }

  const agentKey = aiAccess?.agentKey || {};
  const endpoints = aiAccess?.endpoints || {};
  const guide = aiAccess?.guide || '';
  const policy = aiAccess?.policy || {};

  return (
    <div className="grid h-full min-h-0 min-w-0 gap-4 cq-xl:grid-cols-[minmax(360px,0.82fr)_minmax(0,1.18fr)]">
      <div
        className={cx(fixedPanelClass, 'min-h-0 space-y-4 overflow-y-auto px-px pb-2 pr-1 pt-px')}
      >
        <SectionCard title="Agent Key" icon={<Key className="h-4 w-4 text-brand" />}>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <div className="min-w-0 flex-1 truncate rounded-md border border-kumo-line bg-kumo-recessed/40 px-3 py-2 font-mono text-xs font-bold text-kumo-strong">
              {keyVisible ? agentKey.value : agentKey.masked}
            </div>
            <Button size="sm" variant="secondary" onClick={() => setKeyVisible(!keyVisible)}>
              {keyVisible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => onCopy(agentKey.value, 'Agent Key 已复制')}
            >
              <Copy className="h-3.5 w-3.5" />
            </Button>
            <Button size="sm" variant="destructive" onClick={onRotateKey} className="gap-1.5">
              <Key className="h-3.5 w-3.5" />
              <span>轮换</span>
            </Button>
          </div>
        </SectionCard>

        <SectionCard title="接入地址" icon={<Plug className="h-4 w-4 text-brand" />}>
          <div className="space-y-2">
            {Object.entries(endpoints).map(([key, value]) => (
              <div key={key} className="grid min-w-0 gap-1">
                <span className="text-xs font-bold text-kumo-subtle">{key}</span>
                <ClipboardText
                  size="sm"
                  text={value}
                  className="min-w-0 w-full"
                  tooltip={{ text: '复制地址', copiedText: '地址已复制' }}
                />
              </div>
            ))}
          </div>
        </SectionCard>

        <SectionCard title="调用策略" icon={<Shield className="h-4 w-4 text-brand" />}>
          <div className="grid gap-2 text-xs text-kumo-subtle">
            <div className="flex items-center justify-between gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2">
              <span>允许方法</span>
              <span className="font-mono text-kumo-strong">
                {(policy.allowedMethods || []).join(' / ') || '-'}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2">
              <span>请求体限制</span>
              <span className="font-mono text-kumo-strong">
                {policy.bodyLimitBytes ? `${Math.round(policy.bodyLimitBytes / 1024)} KB` : '-'}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2">
              <div className="flex items-center gap-2">
                <span>允许写入</span>
                <span className="hidden text-[10px] text-kumo-subtle cq-sm:inline">
                  开启后 Agent 才能执行 POST/PUT/PATCH/DELETE，全部写入都会审计
                </span>
              </div>
              <Switch
                checked={policy.writeEnabled === true}
                onCheckedChange={checked => onToggleWrite(Boolean(checked))}
                aria-label="允许 AI Agent 写入操作"
              />
            </div>
            <div className="grid gap-2 cq-md:grid-cols-3">
              {POLICY_CARDS.map(({ value, title, Icon }) => {
                const active = (policy.accessPolicy || 'standard') === value;
                return (
                  <button
                    key={value}
                    type="button"
                    onClick={() => onSetPolicy(value)}
                    aria-pressed={active}
                    aria-label={`切换到 ${title} 权限模式`}
className={cx(
                      'flex flex-col items-center gap-1.5 rounded-lg border px-3 py-3 transition-colors',
                      active
                        ? 'border-(--text-color-brand) bg-kumo-tint text-brand'
                        : 'border-kumo-line bg-kumo-recessed/25 text-kumo-strong hover:bg-kumo-recessed/50'
                    )}
                  >
                    <Icon className={cx('h-4 w-4', active ? 'text-brand' : 'text-kumo-strong')} />
                    <span className="text-xs font-medium">{title}</span>
                  </button>
                );
              })}
            </div>
          </div>
        </SectionCard>
      </div>

      <div
        className={cx(fixedPanelClass, 'min-h-0 space-y-4 overflow-y-auto px-px pb-2 pr-1 pt-px')}
      >
        <SectionCard
          title="AI 接入指南"
          icon={<Bot className="h-4 w-4 text-brand" />}
          action={
            <Button size="sm" variant="secondary" onClick={onRefresh}>
              刷新
            </Button>
          }
          bodyClassName="grid gap-3"
        >
<div className="grid gap-2 cq-md:grid-cols-3">
            {[
              {
                step: '1',
                title: '复制密钥并注册',
                text: '复制有效 Agent Key，在 AI 客户端按指南注册 manifest / MCP 地址并以 Bearer 鉴权连接。',
              },
              {
                step: '2',
                title: '扫描目录',
                text: '用 list_apis 按模块/分组过滤查看可用接口，不拉全量、省 token。',
              },
              {
                step: '3',
                title: '按契约调用',
                text: '先用 get_route 取接口请求体 schema 与示例，再 call_api 调用，减少试错；写入需开启「允许写入」。',
              },
            ].map(item => (
              <div
                key={item.step}
                className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 p-3"
              >
                <div className="mb-2 flex items-center gap-2">
                  <span className="flex h-5 w-5 items-center justify-center rounded border border-brand/30 bg-brand/10 font-mono text-[10px] font-bold text-brand">
                    {item.step}
                  </span>
                  <div className="text-xs font-bold text-kumo-strong">{item.title}</div>
                </div>
                <p className="text-xs leading-relaxed text-kumo-subtle">{item.text}</p>
              </div>
            ))}
          </div>
          <SnippetBox label="复制指南" value={guide} onCopy={onCopy} />
        </SectionCard>
      </div>
    </div>
  );
}

function AIAuditConsole({
  records,
  total,
  page,
  pageSize,
  loading,
  error,
  actionFilter,
  searchText,
  onActionFilterChange,
  onSearchTextChange,
  onClearFilters,
  onPageChange,
  onPageSizeChange,
  onRefresh,
}) {
  const [selected, setSelected] = useState(null);

  if (loading && records.length === 0) {
    return (
      <AppCard padding="lg">
        <SkeletonLine className="h-5 w-36" />
        <SkeletonLine className="mt-4 h-80 w-full" />
      </AppCard>
    );
  }

  if (error) {
    return (
      <EmptyState
        icon={Activity}
        title="调用审计暂不可用"
        description={error}
        action={
          <Button size="sm" variant="secondary" onClick={onRefresh}>
            重试
          </Button>
        }
      />
    );
  }

  return (
    <>
    <LayerCard className="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden p-0 shadow-none">
      <div className="flex items-center gap-2 border-b border-kumo-line px-3 py-2">
        <Select
          value={actionFilter}
          onValueChange={onActionFilterChange}
          className="w-[140px]"
          size="sm"
          aria-label="操作类型"
          items={[
            { value: '', label: '全部操作' },
            { value: 'mcp.describe', label: 'mcp.describe' },
            { value: 'tools/call', label: 'tools/call' },
            { value: 'manifest', label: 'manifest' },
            { value: 'notifications/cancelled', label: 'notifications/cancelled' },
          ]}
        />
        <div className="relative flex-1 max-w-xs">
          <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-kumo-subtle" />
          <Input
            value={searchText}
            onChange={e => onSearchTextChange(e.target.value)}
            placeholder="搜索时间、动作、目标、IP..."
            className="w-full pl-7"
            size="sm"
            aria-label="搜索调用日志"
          />
        </div>
        {searchText || actionFilter ? (
          <Button size="sm" variant="ghost" onClick={onClearFilters}>
            清除
          </Button>
        ) : null}
      </div>
      <div className="min-h-0 min-w-0 flex-1 overflow-auto scrollbar-thin">
        <Table layout="fixed" className="min-w-[1080px] [&_td]:!px-2 [&_td]:!py-2 [&_th]:!px-2 [&_th]:!py-2">
          <colgroup>
            <col style={{ width: 150 }} />
            <col style={{ width: 110 }} />
            <col style={{ width: 150 }} />
            <col style={{ width: 190 }} />
            <col style={{ width: 84 }} />
            <col style={{ width: 92 }} />
            <col style={{ width: 140 }} />
            <col style={{ width: 240 }} />
          </colgroup>
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.Head className="text-center">时间</Table.Head>
              <Table.Head className="text-center">Agent</Table.Head>
              <Table.Head className="text-center">动作</Table.Head>
              <Table.Head className="text-center">目标</Table.Head>
              <Table.Head className="text-center">状态</Table.Head>
              <Table.Head className="text-center">耗时</Table.Head>
              <Table.Head className="text-center">IP</Table.Head>
              <Table.Head className="text-center">详情</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {loading ? (
              <Table.Row>
                <Table.Cell colSpan={8} className="py-8 text-center">
                  <Loader size={20} className="mx-auto text-kumo-subtle" />
                </Table.Cell>
              </Table.Row>
            ) : records.length === 0 ? (
              <Table.Row>
                <Table.Cell colSpan={8} className="py-8 text-center text-sm text-kumo-subtle">
                  暂无审计记录
                </Table.Cell>
              </Table.Row>
            ) : (
              records.map(item => (
                <Table.Row key={item.id} className="text-sm">
                  <Table.Cell className="truncate text-center font-mono text-kumo-subtle">
                    {formatDateTime(item.createdAt)}
                  </Table.Cell>
                  <Table.Cell
                    className="truncate text-center font-mono text-kumo-subtle"
                    title={item.agentName}
                  >
                    {item.agentName || '-'}
                  </Table.Cell>
                  <Table.Cell
                    className="truncate text-center font-mono font-medium text-kumo-strong"
                    title={item.action}
                  >
                    {item.action}
                  </Table.Cell>
                  <Table.Cell
                    className="truncate text-center font-mono text-kumo-subtle"
                    title={item.target || item.details}
                  >
                    {item.target || item.details || '-'}
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <StatusBadge tone={item.status === 'success' ? 'success' : 'danger'}>
                      {item.status}
                    </StatusBadge>
                  </Table.Cell>
                  <Table.Cell className="text-center font-mono text-kumo-subtle">
                    {item.latencyMs != null ? `${item.latencyMs}ms` : '-'}
                  </Table.Cell>
                  <Table.Cell
                    className="truncate text-center font-mono text-kumo-subtle"
                    title={item.ipAddress}
                  >
                    {item.ipAddress || '—'}
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <Button
                      type="button"
                      size="sm"
                      variant="ghost"
                      onClick={() => setSelected(item)}
                      title="查看完整详情"
                      className="h-auto max-w-full gap-1 px-1.5 py-0.5 text-kumo-subtle hover:text-kumo-strong"
                    >
                      <span className="truncate">{item.details || '查看'}</span>
                      <Eye className="h-3.5 w-3.5 shrink-0" />
                    </Button>
                  </Table.Cell>
                </Table.Row>
              ))
            )}
          </Table.Body>
        </Table>
      </div>

      {total > 0 && (
        <Pagination
          page={page}
          setPage={onPageChange}
          perPage={pageSize}
          totalCount={total}
          labels={{
            navigation: '调用审计分页',
            firstPage: '第一页',
            previousPage: '上一页',
            nextPage: '下一页',
            lastPage: '最后一页',
            pageNumber: '页码',
            pageSize: '每页数量',
          }}
          className="shrink-0 flex-wrap gap-x-3 gap-y-1 border-x-0 border-b-0 border-t border-kumo-line bg-kumo-base px-3 py-2 text-sm shadow-none [&_[data-slot=pagination-controls]]:ml-auto [&_[data-slot=pagination-info]]:min-w-0 max-sm:[&_[data-slot=pagination-info]]:hidden max-sm:[&_[data-slot=pagination-page-size]]:hidden max-sm:[&_[data-slot=pagination-separator]]:hidden max-sm:[&_[data-slot=pagination-controls]]:m-auto"
        >
          <Pagination.Info>
            {({ pageShowingRange, totalCount }) => (
              <span className="text-kumo-subtle">
                显示 {pageShowingRange}，共 {totalCount} 条
              </span>
            )}
          </Pagination.Info>
          <Pagination.Separator />
          <Pagination.PageSize
            value={pageSize}
            onChange={size => onPageSizeChange(size)}
            options={[10, 20, 50, 100]}
            label="每页"
          />
          <Pagination.Controls />
        </Pagination>
      )}

      <Dialog.Root open={!!selected} onOpenChange={open => !open && setSelected(null)}>
        <Dialog className="!w-[min(44rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 select-none text-base font-bold text-kumo-strong">
            审计详情
          </Dialog.Title>
          <Dialog.Description className="mb-4 select-none text-xs text-kumo-subtle">
            {selected ? `#${selected.id} · ${formatDateTime(selected.createdAt)}` : ''}
          </Dialog.Description>
          <div className="grid grid-cols-2 gap-x-4 gap-y-3 text-xs">
            {[
              { label: 'Agent', value: selected?.agentName || '-' },
              { label: '动作', value: selected?.action || '-' },
              { label: '目标', value: selected?.target || '-' },
              {
                label: '状态',
                value: selected?.status || '-',
                pill: selected?.status === 'success' ? 'success' : 'danger',
              },
              { label: '耗时', value: selected?.latencyMs != null ? `${selected.latencyMs}ms` : '-' },
              { label: 'IP', value: selected?.ipAddress || '—' },
            ].map(field => (
              <div key={field.label}>
                <div className="mb-0.5 text-kumo-subtle">{field.label}</div>
                <div className="break-all font-mono text-kumo-strong">
                  {field.pill ? (
                    <StatusBadge tone={field.pill}>{field.value}</StatusBadge>
                  ) : (
                    field.value
                  )}
                </div>
              </div>
            ))}
            <div className="col-span-2">
              <div className="mb-0.5 text-kumo-subtle">User-Agent</div>
              <div className="break-all rounded-md border border-kumo-line bg-kumo-recessed/40 px-3 py-2 font-mono text-kumo-strong">
                {selected?.userAgent || '—'}
              </div>
            </div>
            <div className="col-span-2">
              <div className="mb-0.5 text-kumo-subtle">详情</div>
              <pre className="max-h-60 overflow-auto whitespace-pre-wrap break-all rounded-md border border-kumo-line bg-kumo-recessed/40 p-3 font-mono leading-relaxed text-kumo-strong">
                {selected ? formatAuditDetails(selected.details) : ''}
              </pre>
            </div>
          </div>
          <div className="mt-5 flex justify-end">
            <Dialog.Close asChild>
              <Button size="sm" variant="secondary" onClick={() => setSelected(null)}>
                关闭
              </Button>
            </Dialog.Close>
          </div>
        </Dialog>
      </Dialog.Root>
    </LayerCard>
    </>
  );
}
function APIKeyConsole({
  overview,
  loading,
  error,
  form,
  setForm,
  editingId,
  submitting,
  issuedSecret,
  onDismissSecret,
  onSave,
  onEdit,
  onCancelEdit,
  onToggle,
  onRotate,
  onRevoke,
  onRefresh,
  onCopy,
}) {
  if (loading && !overview) {
    return (
      <AppCard padding="lg">
        <SkeletonLine className="h-5 w-36" />
        <SkeletonLine className="mt-4 h-80 w-full" />
      </AppCard>
    );
  }

  if (error && !overview) {
    return (
      <EmptyState
        icon={Key}
        title="密钥管理暂不可用"
        description={error}
        action={
          <Button size="sm" variant="secondary" onClick={onRefresh}>
            重试
          </Button>
        }
      />
    );
  }

  const keys = overview?.keys || [];
  const summary = overview?.summary || {};
  const selectedKind = API_KEY_KINDS.find(item => item.value === form.kind) || API_KEY_KINDS[0];
  const selectedExpiryPreset = matchExpiryPreset(form.expiresAt);

  const toggleScope = scope => {
    setForm(current => ({
      ...current,
      scopes: current.scopes.includes(scope)
        ? current.scopes.filter(item => item !== scope)
        : [...current.scopes, scope],
    }));
  };

  const updateExpiryDate = date => {
    if (!date) return;
    setForm(current => {
      const existing = parseLocalDateTime(current.expiresAt);
      const next = new Date(date);
      next.setHours(existing?.getHours() ?? 23, existing?.getMinutes() ?? 59, 0, 0);
      return { ...current, expiresAt: toLocalDateTimeInput(next) };
    });
  };

  const updateExpiryTime = (part, value) => {
    setForm(current => {
      const next = parseLocalDateTime(current.expiresAt);
      if (!next) return current;
      if (part === 'hour') next.setHours(Number(value));
      if (part === 'minute') next.setMinutes(Number(value));
      return { ...current, expiresAt: toLocalDateTimeInput(next) };
    });
  };

  const applyExpiryPreset = preset => {
    setForm(current => {
      if (preset.days === 0) {
        return { ...current, expiresAt: '' };
      }
      const base = parseLocalDateTime(current.expiresAt);
      const next = new Date();
      next.setDate(next.getDate() + preset.days);
      next.setSeconds(0, 0);
      next.setHours(base?.getHours() ?? 23, base?.getMinutes() ?? 59, 0, 0);
      return { ...current, expiresAt: toLocalDateTimeInput(next) };
    });
  };

  return (
    <div className="grid h-full min-h-0 min-w-0 gap-4 cq-xl:grid-cols-[minmax(320px,0.72fr)_minmax(0,1.28fr)]">
      <div className="min-h-0 space-y-4 overflow-y-auto px-px pb-2 pr-1 pt-px">
        <div className="grid grid-cols-2 gap-3">
          <StatCard icon={Key} label="密钥总数" value={summary.total || 0} />
          <StatCard icon={Shield} label="使用中" value={summary.active || 0} tone="success" />
          <StatCard icon={Activity} label="已过期" value={summary.expired || 0} tone="warning" />
          <StatCard
            icon={X}
            label="停用 / 撤销"
            value={summary.revoked || 0}
            tone="info"
          />
        </div>

        {issuedSecret && (
          <SectionCard
            title="新密钥仅显示一次"
            icon={<Key className="h-4 w-4 text-kumo-warning" />}
            action={
              <Button size="sm" variant="ghost" onClick={onDismissSecret} aria-label="关闭">
                <X className="h-3.5 w-3.5" />
              </Button>
            }
          >
            <div className="space-y-2">
              <div className="break-all rounded-md border border-kumo-warning/30 bg-kumo-warning/8 px-3 py-2 font-mono text-xs font-bold text-kumo-strong">
                {issuedSecret}
              </div>
              <Button
                size="sm"
                variant="primary"
                onClick={() => onCopy(issuedSecret, 'API Key 已复制')}
                className="gap-1.5"
              >
                <Copy className="h-3.5 w-3.5" />
                <span>复制密钥</span>
              </Button>
            </div>
          </SectionCard>
        )}

        <SectionCard
          title={editingId ? '编辑密钥' : '生成密钥'}
          icon={<Plus className="h-4 w-4 text-brand" />}
          bodyClassName="space-y-3"
        >
          <Input
            size="sm"
            value={form.name}
            onChange={event => setForm(current => ({ ...current, name: event.target.value }))}
            placeholder="密钥名称，例如 Chrome 插件"
            aria-label="密钥名称"
            className="text-xs"
          />
          <Select
            size="sm"
            aria-label="密钥类型"
            value={form.kind}
            disabled={Boolean(editingId)}
            onValueChange={kind => setForm(current => ({ ...current, kind, scopes: [] }))}
            items={API_KEY_KINDS.map(({ value, label }) => ({ value, label }))}
            className="text-xs"
          />
          <div className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2 text-xs text-kumo-subtle">
            <div className="flex items-center justify-between gap-2">
              <span>{selectedKind.scope}</span>
              <span className="font-mono text-kumo-strong">{selectedKind.prefix}</span>
            </div>
          </div>
          {form.kind === 'api' && (
            <div className="grid gap-2 cq-sm:grid-cols-2">
              {[
                { value: 'api:read', label: '读取后台 API' },
                { value: 'api:write', label: '修改后台 API' },
              ].map(scope => (
                <label
                  key={scope.value}
                  className="flex cursor-pointer items-center gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2 text-xs text-kumo-strong"
                >
                  <Checkbox
                    checked={form.scopes.includes(scope.value)}
                    onCheckedChange={() => toggleScope(scope.value)}
                    aria-label={scope.label}
                  />
                  <span>{scope.label}</span>
                </label>
              ))}
            </div>
          )}
          <div className="space-y-2">
            <Label>
              过期时间
              <span className="font-normal text-kumo-subtle">（可选）</span>
            </Label>
            <div className="flex flex-wrap gap-2">
              {API_KEY_EXPIRY_PRESETS.map(preset => {
                const active = selectedExpiryPreset === preset.value;
                return (
                  <Button
                    key={preset.value}
                    size="sm"
                    variant={active ? 'primary' : 'secondary'}
                    onClick={() => applyExpiryPreset(preset)}
                    className={cx(
                      'min-w-[4.5rem] transition-colors',
                      active && 'shadow-[0_0_0_1px_rgba(255,255,255,0.12)]'
                    )}
                  >
                    {preset.label}
                  </Button>
                );
              })}
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Popover>
                <Popover.Trigger
                  render={
                    <Button
                      size="sm"
                      variant="outline"
                      icon={CalendarDotsIcon}
                      className="min-w-[12.5rem] justify-start font-normal cq-sm:min-w-[13.5rem]"
                    />
                  }
                >
                  <span className="truncate">
                    {form.expiresAt ? formatKeyTime(form.expiresAt) : '长期有效'}
                  </span>
                </Popover.Trigger>
                <Popover.Content className="p-3">
                  <DatePicker
                    size="sm"
                    mode="single"
                    selected={parseLocalDateTime(form.expiresAt)}
                    onChange={updateExpiryDate}
                  />
                  {form.expiresAt && (
                    <div className="mt-2 flex justify-end border-t border-kumo-line pt-2">
                      <Button
                        size="xs"
                        variant="ghost"
                        onClick={() => setForm(current => ({ ...current, expiresAt: '' }))}
                      >
                        清除
                      </Button>
                    </div>
                  )}
                </Popover.Content>
              </Popover>
              <div className="flex items-center gap-1.5">
                <Select
                  size="sm"
                  aria-label="过期小时"
                  disabled={!form.expiresAt}
                  value={form.expiresAt.slice(11, 13)}
                  onValueChange={value => updateExpiryTime('hour', value)}
                  items={API_KEY_EXPIRY_HOURS}
                />
                <span className="text-center text-sm text-kumo-subtle">:</span>
                <Select
                  size="sm"
                  aria-label="过期分钟"
                  disabled={!form.expiresAt}
                  value={form.expiresAt.slice(14, 16)}
                  onValueChange={value => updateExpiryTime('minute', value)}
                  items={API_KEY_EXPIRY_MINUTES}
                />
              </div>
            </div>
            <p className="text-[11px] text-kumo-subtle">留空表示长期有效，建议使用 90 天并定期轮换。</p>
          </div>
          {editingId && (
            <Select
              size="sm"
              aria-label="启用状态"
              value={form.enabled ? 'true' : 'false'}
              onValueChange={value =>
                setForm(current => ({ ...current, enabled: value === 'true' }))
              }
              items={[
                { value: 'true', label: '启用' },
                { value: 'false', label: '停用' },
              ]}
              className="text-xs"
            />
          )}
          <div className="flex flex-wrap gap-2">
            <Button
              size="sm"
              variant="primary"
              disabled={submitting}
              onClick={onSave}
              className="gap-1.5"
            >
              {editingId ? <Edit className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
              <span>{editingId ? '保存修改' : '生成密钥'}</span>
            </Button>
            {editingId && (
              <Button size="sm" variant="secondary" onClick={onCancelEdit} className="gap-1.5">
                <X className="h-3.5 w-3.5" />
                <span>取消</span>
              </Button>
            )}
          </div>
        </SectionCard>
      </div>

      <SectionCard
        title="密钥与使用监控"
        icon={<Activity className="h-4 w-4 text-brand" />}
        action={
          <Button size="sm" variant="secondary" onClick={onRefresh} loading={loading}>
            <RefreshCw className="h-3.5 w-3.5" />
          </Button>
        }
        className="min-h-0"
        bodyClassName="h-full min-h-0 overflow-y-auto"
      >
        {keys.length === 0 ? (
          <div className="flex h-full min-h-48 items-center justify-center text-xs text-kumo-subtle">
            尚未生成密钥
          </div>
        ) : (
          <div className="space-y-2">
            {keys.map(key => {
              const status = apiKeyStatus(key);
              const kind = API_KEY_KINDS.find(item => item.value === key.kind);
              return (
                <div
                  key={key.id}
                  className={cx(
                    'rounded-md border bg-kumo-recessed/20 p-3',
                    editingId === key.id ? 'border-brand/70' : 'border-kumo-line/80'
                  )}
                >
                  <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 flex-1">
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <span className="truncate text-xs font-bold text-kumo-strong">{key.name}</span>
                        <StatusBadge tone={status.tone}>{status.label}</StatusBadge>
                        <span className="rounded border border-kumo-line px-1.5 py-0.5 text-[10px] text-kumo-subtle">
                          {kind?.label || key.kind}
                        </span>
                      </div>
                      <div className="mt-2 break-all font-mono text-[11px] font-semibold text-kumo-strong">
                        {key.maskedKey}
                      </div>
                      <div className="mt-2 flex flex-wrap gap-1">
                        {(key.scopes || []).map(scope => (
                          <span
                            key={scope}
                            className="rounded border border-brand/20 bg-brand/7 px-1.5 py-0.5 text-[10px] text-brand"
                          >
                            {API_SCOPE_LABELS[scope] || scope}
                          </span>
                        ))}
                      </div>
                    </div>
                    <div className="flex shrink-0 flex-wrap items-center gap-1">
                      <Button size="sm" shape="square" variant="secondary" onClick={() => onEdit(key)} aria-label="编辑密钥" title="编辑">
                        <Edit className="h-3.5 w-3.5" />
                      </Button>
                      {!key.revokedAt && (
                        <Button
                          size="sm"
                          shape="square"
                          variant="secondary"
                          onClick={() => onToggle(key)}
                          aria-label={key.enabled ? '停用密钥' : '启用密钥'}
                          title={key.enabled ? '停用' : '启用'}
                        >
                          {key.enabled ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
                        </Button>
                      )}
                      <Button size="sm" shape="square" variant="secondary" onClick={() => onRotate(key)} aria-label="轮换密钥" title="轮换">
                        <RefreshCw className="h-3.5 w-3.5" />
                      </Button>
                      {!key.revokedAt && (
                        <Button size="sm" shape="square" variant="secondary-destructive" onClick={() => onRevoke(key)} aria-label="撤销密钥" title="撤销">
                          <Trash className="h-3.5 w-3.5" />
                        </Button>
                      )}
                    </div>
                  </div>
                  <div className="mt-3 grid gap-2 border-t border-kumo-line/70 pt-3 text-[11px] text-kumo-subtle cq-sm:grid-cols-2 cq-2xl:grid-cols-4">
                    <div><span className="block">请求次数</span><strong className="font-mono text-kumo-strong">{Number(key.requestCount || 0).toLocaleString('en-US', { useGrouping: false })}</strong></div>
                    <div><span className="block">过期时间</span><strong className="font-normal text-kumo-strong">{formatKeyTime(key.expiresAt)}</strong></div>
                    <div><span className="block">最后使用</span><strong className="font-normal text-kumo-strong">{formatKeyTime(key.lastUsedAt)}</strong></div>
                    <div className="min-w-0"><span className="block">最后 IP</span><strong className="block truncate font-mono font-normal text-kumo-strong" title={key.lastIpAddress || ''}>{key.lastIpAddress || '-'}</strong></div>
                  </div>
                  {key.lastUserAgent && (
                    <div className="mt-2 truncate text-[10px] text-kumo-subtle" title={key.lastUserAgent}>
                      {key.lastUserAgent}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </SectionCard>
    </div>
  );
}

function ApiDocsPage() {
  const [docs, setDocs] = useState(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [activeView, setActiveView] = useState('routes');
  const [query, setQuery] = useState('');
  const [group, setGroup] = useState('all');
  const [auth, setAuth] = useState('all');
  const [status, setStatus] = useState('all');
  const [selectedKey, setSelectedKey] = useState('');
  const [aiAccess, setAiAccess] = useState(null);
  const [aiLoading, setAiLoading] = useState(false);
  const [aiError, setAiError] = useState('');
  const [keyVisible, setKeyVisible] = useState(false);
  const [auditRecords, setAuditRecords] = useState([]);
  const [auditTotal, setAuditTotal] = useState(0);
  const [auditPage, setAuditPage] = useState(1);
  const [auditPageSize, setAuditPageSize] = useState(20);
  const [auditDays, setAuditDays] = useState(7);
  const [auditLoading, setAuditLoading] = useState(false);
  const [auditError, setAuditError] = useState('');
  const [auditAction, setAuditAction] = useState('');
  const [auditSearch, setAuditSearch] = useState('');
  const [apiKeyOverview, setApiKeyOverview] = useState(null);
  const [apiKeysLoading, setApiKeysLoading] = useState(false);
  const [apiKeysError, setApiKeysError] = useState('');
  const [apiKeyForm, setApiKeyForm] = useState(createDefaultAPIKeyForm);
  const [apiKeyEditingId, setApiKeyEditingId] = useState('');
  const [apiKeySubmitting, setApiKeySubmitting] = useState(false);
  const [issuedAPIKey, setIssuedAPIKey] = useState('');

  const loadDocs = useCallback(async (silent = false) => {
    if (silent) {
      setRefreshing(true);
    } else {
      setLoading(true);
    }
    try {
      let nextDocs;
      try {
        nextDocs = await fetchJsonEnvelope('/api/system/api-docs');
      } catch (primaryError) {
        console.info('api docs route unavailable, falling back to migration status:', primaryError);
        const migration = await fetchJsonEnvelope('/api/migration/status');
        nextDocs = {
          version: migration.version,
          summary: {
            byOwner: migration.routeSummary,
          },
          routes: migration.routes,
        };
      }
      const normalizedDocs = normalizeDocsPayload(nextDocs);
      setDocs(normalizedDocs);
      setSelectedKey(current => {
        if (current && normalizedDocs.routes.some(route => getRouteKey(route) === current)) {
          return current;
        }
        return normalizedDocs.routes[0] ? getRouteKey(normalizedDocs.routes[0]) : '';
      });
    } catch (error) {
      console.error('load api docs failed:', error);
      toast.error(error.message || '接口文档加载失败');
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    loadDocs();
  }, [loadDocs]);

  const loadAIAccess = useCallback(async (silent = false) => {
    if (!silent) setAiLoading(true);
    setAiError('');
    try {
      const payload = await fetchJsonEnvelope(AI_ACCESS_BASE);
      setAiAccess(payload);
    } catch (error) {
      console.error('load ai access failed:', error);
      setAiError(error.message || 'AI 接入数据加载失败');
    } finally {
      setAiLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activeView === 'ai' && !aiAccess && !aiLoading && !aiError) {
      loadAIAccess();
    }
  }, [activeView, aiAccess, aiError, aiLoading, loadAIAccess]);

  const loadAIAudit = useCallback(async (silent = false) => {
    if (!silent) setAuditLoading(true);
    setAuditError('');
    try {
      const params = new URLSearchParams({ days: auditDays, page: auditPage, pageSize: auditPageSize });
      if (auditAction) params.set('action', auditAction);
      if (auditSearch) params.set('search', auditSearch);
      const payload = await fetchJsonEnvelope(
        `${AI_ACCESS_BASE}/audit?${params}`
      );
      setAuditRecords(payload.records || []);
      setAuditTotal(payload.total || 0);
    } catch (error) {
      console.error('load ai audit failed:', error);
      setAuditError(error.message || '调用审计加载失败');
    } finally {
      setAuditLoading(false);
    }
  }, [auditDays, auditPage, auditPageSize, auditAction, auditSearch]);

  const handleAuditActionChange = useCallback(value => {
    setAuditAction(value);
    setAuditPage(1);
  }, []);

  const handleAuditSearchChange = useCallback(value => {
    setAuditSearch(value);
    setAuditPage(1);
  }, []);

  const clearAuditFilters = useCallback(() => {
    setAuditAction('');
    setAuditSearch('');
    setAuditPage(1);
  }, []);

  useEffect(() => {
    if (activeView !== 'audit') return undefined;
    // 搜索输入防抖：避免每敲一个字符触发一次请求
    const timer = window.setTimeout(() => {
      loadAIAudit();
    }, 300);
    return () => window.clearTimeout(timer);
  }, [activeView, loadAIAudit]);

  const loadAPIKeys = useCallback(async (silent = false) => {
    if (!silent) setApiKeysLoading(true);
    setApiKeysError('');
    try {
      setApiKeyOverview(await fetchJsonEnvelope(API_KEYS_BASE));
    } catch (error) {
      console.error('load api keys failed:', error);
      setApiKeysError(error.message || '密钥数据加载失败');
    } finally {
      setApiKeysLoading(false);
    }
  }, []);

  useEffect(() => {
    if (activeView === 'keys' && !apiKeyOverview && !apiKeysLoading && !apiKeysError) {
      loadAPIKeys();
    }
  }, [activeView, apiKeyOverview, apiKeysError, apiKeysLoading, loadAPIKeys]);

  const routes = docs?.routes || [];
  const summary = normalizeSummary(docs?.summary);
  const aiRouteCount = routes.filter(
    route =>
      route.group === '模型网关' ||
      route.prefix === '/api/ai/manifest' ||
      route.prefix === '/api/ai/mcp' ||
      route.prefix.startsWith('/api/ai-access')
  ).length;

  const groupItems = useMemo(() => {
    const groups = [...new Set(routes.map(route => route.group).filter(Boolean))].sort((a, b) => {
      const order = groupOrderIndex(a) - groupOrderIndex(b);
      return order !== 0 ? order : a.localeCompare(b, 'zh-CN');
    });
    return [
      { value: 'all', label: '全部分组' },
      ...groups.map(item => ({ value: item, label: item })),
    ];
  }, [routes]);

  const filteredRoutes = useMemo(() => {
    const text = query.trim().toLowerCase();
    return routes.filter(route => {
      if (group !== 'all' && route.group !== group) return false;
      if (auth !== 'all' && route.auth !== auth) return false;
      if (status !== 'all' && route.status !== status) return false;
      if (!text) return true;
      return [
        route.prefix,
        route.module,
        route.group,
        route.description,
        route.detail,
        route.auth,
        route.responseMode,
        ...(route.notes || []),
      ].some(value =>
        String(value || '')
          .toLowerCase()
          .includes(text)
      );
    });
  }, [auth, group, query, routes, status]);

  const selectedRoute = useMemo(() => {
    const visibleSelected = filteredRoutes.find(route => getRouteKey(route) === selectedKey);
    if (visibleSelected) return visibleSelected;
    return filteredRoutes[0] || null;
  }, [filteredRoutes, selectedKey]);

  const exportOpenAPI = () => {
    if (!summary.openapiRoute) return;
    window.open(summary.openapiRoute, '_blank', 'noopener,noreferrer');
  };

  const copyText = async (text, message) => {
    try {
      await navigator.clipboard.writeText(String(text || ''));
      toast.success(message);
    } catch (error) {
      console.error('copy failed:', error);
      toast.error('复制失败');
    }
  };

  const refreshAIAccess = () => loadAIAccess(true);

  const rotateAIKey = async () => {
    try {
      const payload = await apiRequest(`${AI_ACCESS_BASE}/key/rotate`, { method: 'POST' });
      setAiAccess(payload);
      setKeyVisible(true);
      toast.success('Agent Key 已轮换');
    } catch (error) {
      toast.error(error.message || '轮换失败');
    }
  };

  const toggleAIWrite = async enabled => {
    try {
      const payload = await apiRequest(`${AI_ACCESS_BASE}/write`, {
        method: 'PUT',
        body: JSON.stringify({ writeEnabled: enabled }),
      });
      setAiAccess(payload);
      toast.success(enabled ? '已开启 AI 写入，写操作将受到审计' : '已关闭 AI 写入，Agent 仅可读');
    } catch (error) {
      toast.error(error.message || '切换失败');
    }
  };

  const setAIAccessPolicy = async policy => {
    try {
      const payload = await apiRequest(`${AI_ACCESS_BASE}/policy`, {
        method: 'PUT',
        body: JSON.stringify({ policy }),
      });
      setAiAccess(payload);
      const label = { minimal: '只读（minimal）', standard: '标准（standard）', full: '全部权限（full）' }[policy] || policy;
      toast.success(`AI 接入权限模式已切换为 ${label}`);
    } catch (error) {
      toast.error(error.message || '切换失败');
    }
  };

  const clearAIAudit = async () => {
    try {
      const confirmed = await dialog.confirm('确认清空全部调用审计记录？此操作不可恢复。');
      if (!confirmed) return;
      await apiRequest(`${AI_ACCESS_BASE}/audit/clear`, { method: 'POST' });
      setAuditRecords([]);
      setAuditTotal(0);
      setAuditPage(1);
      toast.success('审计记录已清空');
    } catch (error) {
      toast.error(error.message || '清空失败');
    }
  };

  const resetAPIKeyForm = () => {
    setApiKeyEditingId('');
    setApiKeyForm(createDefaultAPIKeyForm());
  };

  const saveAPIKey = async () => {
    if (!apiKeyForm.name.trim()) {
      toast.error('请输入密钥名称');
      return;
    }
    if (apiKeyForm.kind === 'api' && apiKeyForm.scopes.length === 0) {
      toast.error('通用 API Key 至少需要一个权限');
      return;
    }
    let expiresAt = '';
    if (apiKeyForm.expiresAt) {
      const expiry = new Date(apiKeyForm.expiresAt);
      if (Number.isNaN(expiry.getTime())) {
        toast.error('过期时间无效');
        return;
      }
      expiresAt = expiry.toISOString();
    }
    setApiKeySubmitting(true);
    try {
      const payload = await apiRequest(
        apiKeyEditingId ? `${API_KEYS_BASE}/${apiKeyEditingId}` : API_KEYS_BASE,
        {
          method: apiKeyEditingId ? 'PUT' : 'POST',
          body: JSON.stringify({ ...apiKeyForm, name: apiKeyForm.name.trim(), expiresAt }),
        }
      );
      if (payload.apiKey) setIssuedAPIKey(payload.apiKey);
      toast.success(apiKeyEditingId ? '密钥设置已更新' : 'API Key 已生成');
      resetAPIKeyForm();
      await loadAPIKeys(true);
    } catch (error) {
      toast.error(error.message || '密钥保存失败');
    } finally {
      setApiKeySubmitting(false);
    }
  };

  const editAPIKey = key => {
    setApiKeyEditingId(key.id);
    setApiKeyForm({
      name: key.name || '',
      kind: key.kind || 'api',
      scopes: key.scopes || [],
      expiresAt: toLocalDateTimeInput(key.expiresAt),
      enabled: key.enabled !== false,
    });
  };

  const updateAPIKey = async (key, changes, successMessage) => {
    try {
      await apiRequest(`${API_KEYS_BASE}/${key.id}`, {
        method: 'PUT',
        body: JSON.stringify({
          name: key.name,
          kind: key.kind,
          scopes: key.scopes || [],
          expiresAt: key.expiresAt || '',
          enabled: key.enabled !== false,
          ...changes,
        }),
      });
      toast.success(successMessage);
      await loadAPIKeys(true);
    } catch (error) {
      toast.error(error.message || '密钥更新失败');
    }
  };

  const toggleAPIKey = key =>
    updateAPIKey(key, { enabled: !key.enabled }, key.enabled ? '密钥已停用' : '密钥已启用');

  const rotateAPIKey = async key => {
    const confirmed = await dialog.confirm({
      title: '确认轮换密钥',
      message: `轮换“${key.name}”后，旧密钥会立即失效。确定要继续吗？`,
      confirmText: '确认轮换',
      confirmClass: '!bg-kumo-danger !text-white',
    });
    if (!confirmed) return;
    try {
      const payload = await apiRequest(`${API_KEYS_BASE}/${key.id}/rotate`, { method: 'POST' });
      setIssuedAPIKey(payload.apiKey || '');
      toast.success('API Key 已轮换');
      await loadAPIKeys(true);
    } catch (error) {
      toast.error(error.message || '密钥轮换失败');
    }
  };

  const revokeAPIKey = async key => {
    const confirmed = await dialog.confirm({
      title: '确认撤销密钥',
      message: `撤销“${key.name}”后，只有轮换才能重新启用。确定要继续吗？`,
      confirmText: '确认撤销',
      confirmClass: '!bg-kumo-danger !text-white',
    });
    if (!confirmed) return;
    try {
      await apiRequest(`${API_KEYS_BASE}/${key.id}/revoke`, { method: 'POST' });
      if (apiKeyEditingId === key.id) resetAPIKeyForm();
      toast.success('API Key 已撤销');
      await loadAPIKeys(true);
    } catch (error) {
      toast.error(error.message || '密钥撤销失败');
    }
  };

  if (loading) {
    return (
      <PageStack viewport className={apiDocsShellClass}>
        <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
          <Tabs
            {...MODULE_TABS_PROPS}
            value={activeView}
            onValueChange={setActiveView}
            tabs={[
              { value: 'routes', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-3.5 w-3.5" />接口</span> },
              { value: 'ai', label: <span className="inline-flex items-center gap-1.5"><Bot className="h-3.5 w-3.5" />AI 接入</span> },
              { value: 'audit', label: <span className="inline-flex items-center gap-1.5"><History className="h-3.5 w-3.5" />调用审计</span> },
              { value: 'keys', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />密钥管理</span> },
            ]}
          />
        </div>
        <div className="grid grid-cols-4 gap-2 cq-sm:gap-3">
          {Array.from({ length: 4 }).map((_, index) => (
            <AppCard key={index} padding="none" className="min-w-0 p-2 cq-sm:p-3">
              <SkeletonLine className="h-4 w-20" />
              <SkeletonLine className="mt-3 h-6 w-14" />
            </AppCard>
          ))}
        </div>
        <AppCard padding="lg">
          <SkeletonLine className="h-5 w-36" />
          <SkeletonLine className="mt-4 h-80 w-full" />
        </AppCard>
      </PageStack>
    );
  }

  return (
    <PageStack viewport className={apiDocsShellClass}>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeView}
          onValueChange={setActiveView}
          tabs={[
            { value: 'routes', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-3.5 w-3.5" />接口</span> },
            { value: 'ai', label: <span className="inline-flex items-center gap-1.5"><Bot className="h-3.5 w-3.5" />AI 接入</span> },
            { value: 'audit', label: <span className="inline-flex items-center gap-1.5"><History className="h-3.5 w-3.5" />调用审计</span> },
            { value: 'keys', label: <span className="inline-flex items-center gap-1.5"><Key className="h-3.5 w-3.5" />密钥管理</span> },
          ]}
        />
        {activeView === 'routes' && (
        <TabBarOverflowActions
          items={[
            {
              key: 'refresh',
              label: '刷新',
              icon: <RefreshCw className="h-3.5 w-3.5" />,
              onClick: () => loadDocs(true),
              disabled: refreshing,
              loading: refreshing,
            },
            {
              key: 'export',
              label: 'OpenAPI',
              icon: <Download className="h-3.5 w-3.5" />,
              onClick: exportOpenAPI,
              disabled: !summary.openapiRoute,
              variant: 'primary',
            },
          ]}
        />
        )}
        {activeView === 'audit' && (
        <TabBarOverflowActions
          items={[
            {
              type: 'select',
              key: 'days',
              label: '时间范围',
              value: String(auditDays),
              onValueChange: value => setAuditDays(Number(value)),
              options: [
                { value: '7', label: '近 7 天' },
                { value: '30', label: '近 30 天' },
                { value: '90', label: '近 90 天' },
              ],
            },
            {
              key: 'refresh',
              label: '刷新',
              icon: <RefreshCw className="h-3.5 w-3.5" />,
              onClick: () => loadAIAudit(true),
              disabled: auditLoading,
              loading: auditLoading,
            },
            {
              key: 'clear',
              label: '清空',
              icon: <Trash className="h-3.5 w-3.5" />,
              onClick: clearAIAudit,
              variant: 'destructive',
            },
          ]}
        />
        )}
      </div>

      {activeView === 'routes' && (
        <div className="flex min-w-0 flex-col gap-2">
          <div className="space-y-2">
            <div className="grid grid-cols-4 gap-2 cq-sm:gap-3">
              <StatCard icon={FileText} label="接口总数" value={summary.total} />
              <StatCard
                icon={Activity}
                label="可用接口"
                value={summary.byStatus.active || 0}
                tone="success"
              />
              <StatCard
                icon={Shield}
                label="登录保护"
                value={summary.byAuth.session || 0}
                tone="warning"
              />
              <StatCard
                icon={Bot}
                label="AI 接口"
                value={aiRouteCount}
                tone="info"
              />
            </div>

            <AppCard padding="md" className="shrink-0">
              <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_repeat(3,minmax(0,0.67fr))] items-center gap-2 cq-sm:grid-cols-[minmax(240px,1.35fr)_repeat(3,minmax(0,0.82fr))]">
                <Input
                  size="sm"
                  aria-label="搜索接口"
                  value={query}
                  onChange={event => setQuery(event.target.value)}
                  placeholder="搜索路径、模块或描述"
                  className="min-w-0 w-full text-xs text-kumo-strong"
                />
                <FilterSelect
                  label="接口分组"
                  value={group}
                  onValueChange={setGroup}
                  items={groupItems}
                />
                <FilterSelect
                  label="认证方式"
                  value={auth}
                  onValueChange={setAuth}
                  items={[
                    { value: 'all', label: '全部认证' },
                    { value: 'public', label: '公开' },
                    { value: 'session', label: '登录' },
                    { value: 'api_key', label: 'API Key' },
                    { value: 'agent_key', label: 'Agent Key' },
                  ]}
                />
                <FilterSelect
                  label="接口状态"
                  value={status}
                  onValueChange={setStatus}
                  items={[
                    { value: 'all', label: '全部状态' },
                    { value: 'active', label: '可用' },
                    { value: 'retired', label: '停用' },
                    { value: 'unknown', label: '未知' },
                  ]}
                />
              </div>
            </AppCard>
          </div>

          <div className="grid min-w-0 gap-3 cq-xl:grid-cols-[minmax(340px,0.9fr)_minmax(0,1.1fr)] cq-2xl:grid-cols-[minmax(360px,0.84fr)_minmax(0,1.16fr)]">
            <RouteTree
              routes={filteredRoutes}
              selectedRoute={selectedRoute}
              onSelect={route => setSelectedKey(getRouteKey(route))}
              revealAll={query.trim().length > 0}
            />
            <div className="min-w-0 cq-xl:sticky cq-xl:top-[70px] cq-xl:max-h-[calc(100vh-82px)] cq-xl:overflow-y-auto cq-xl:overscroll-contain cq-xl:self-start">
              <RouteDetail route={selectedRoute} openapiRoute={summary.openapiRoute} />
            </div>
          </div>
        </div>
      )}

      {activeView === 'ai' && (
        <div className="min-h-0 flex-1">
          <AIAccessConsole
            aiAccess={aiAccess}
            loading={aiLoading}
            error={aiError}
            keyVisible={keyVisible}
            setKeyVisible={setKeyVisible}
            onRefresh={refreshAIAccess}
            onRotateKey={rotateAIKey}
            onToggleWrite={toggleAIWrite}
            onSetPolicy={setAIAccessPolicy}
            onCopy={copyText}
          />
        </div>
      )}

      {activeView === 'audit' && (
        <div className="min-h-0 flex-1">
          <AIAuditConsole
            records={auditRecords}
            total={auditTotal}
            page={auditPage}
            pageSize={auditPageSize}
            loading={auditLoading}
            error={auditError}
            actionFilter={auditAction}
            searchText={auditSearch}
            onActionFilterChange={handleAuditActionChange}
            onSearchTextChange={handleAuditSearchChange}
            onClearFilters={clearAuditFilters}
            onPageChange={setAuditPage}
            onPageSizeChange={setAuditPageSize}
            onRefresh={() => loadAIAudit(true)}
          />
        </div>
      )}

      {activeView === 'keys' && (
        <div className="min-h-0 flex-1">
          <APIKeyConsole
            overview={apiKeyOverview}
            loading={apiKeysLoading}
            error={apiKeysError}
            form={apiKeyForm}
            setForm={setApiKeyForm}
            editingId={apiKeyEditingId}
            submitting={apiKeySubmitting}
            issuedSecret={issuedAPIKey}
            onDismissSecret={() => setIssuedAPIKey('')}
            onSave={saveAPIKey}
            onEdit={editAPIKey}
            onCancelEdit={resetAPIKeyForm}
            onToggle={toggleAPIKey}
            onRotate={rotateAPIKey}
            onRevoke={revokeAPIKey}
            onRefresh={() => loadAPIKeys(true)}
            onCopy={copyText}
          />
        </div>
      )}

    </PageStack>
  );
}

export default ApiDocsPage;
