import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Tabs } from '@cloudflare/kumo';
import { toast } from '../modules/toast.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import {
  AppCard,
  EmptyState,
  InlineStatusPill,
  PageStack,
  PageToolbar,
  SectionHeader,
  cx,
} from '../components/ui/AppPrimitives.jsx';
import {
  Activity,
  Bot,
  Copy,
  Download,
  Edit,
  Eye,
  EyeOff,
  FileText,
  Key,
  Plug,
  Plus,
  RefreshCw,
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

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
  'x-admin-password': localStorage.getItem('admin_password') || '',
});

const AI_ACCESS_BASE = '/api/ai-access';
const OPENAPI_ROUTE = '/api/openapi.json';
const API_SEGMENT = 'api';
const routePrefixLiteral = (...segments) => `/${segments.join('/')}`;

const apiDocsShellClass = 'h-[calc(100dvh-6.25rem-1px)] max-h-[calc(100dvh-6.25rem-1px)] min-h-0 gap-3 overflow-visible';
const fixedPanelClass = 'h-full min-h-0';

const defaultMCPForm = {
  name: '',
  transport: 'stdio',
  command: '',
  url: '',
  description: '',
  enabled: true,
  envJson: '',
};

const defaultSkillForm = {
  name: '',
  description: '',
  entrypoint: '',
  version: '1.0.0',
  enabled: true,
  permissionsText: 'read',
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

const methodClassName = (method) => {
  const normalized = method.toUpperCase();
  if (normalized === 'GET') return 'border-kumo-info/20 bg-kumo-info/10 text-kumo-info';
  if (normalized === 'POST') return 'border-kumo-success/20 bg-kumo-success/10 text-kumo-success';
  if (normalized === 'PUT' || normalized === 'PATCH') return 'border-kumo-warning/20 bg-kumo-warning/10 text-kumo-warning';
  if (normalized === 'DELETE') return 'border-kumo-danger/20 bg-kumo-danger/10 text-kumo-danger';
  return 'border-kumo-line bg-kumo-recessed text-kumo-subtle';
};

const getRouteKey = (route) => `${route.prefix}:${route.module}:${route.auth}`;

const sortRoutes = (routes) => [...routes].sort((a, b) => {
  const groupSort = String(a.group).localeCompare(String(b.group), 'zh-CN');
  if (groupSort !== 0) return groupSort;
  return String(a.prefix).localeCompare(String(b.prefix), 'en');
});

const routeGroup = (route) => {
  const prefix = route.prefix || '';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'auth'))) return '认证';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings'))) return '系统设置';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'system')) || prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'logs')) || prefix.startsWith(routePrefixLiteral('ws', 'logs'))) return '系统';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cloudflare'))) return 'Cloudflare';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server')) || prefix.startsWith(routePrefixLiteral('ws', 'ssh')) || prefix.startsWith(routePrefixLiteral('socket.io'))) return '主机实例';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'openai')) || prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'ai')) || prefix.startsWith(routePrefixLiteral('v1')) || prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'chat'))) return 'AI 接入';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'aliyun'))) return '阿里云';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'tencent'))) return '腾讯云';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'koyeb')) || prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'flyio'))) return 'PaaS';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'totp'))) return '双因子认证';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'filebox'))) return '文件柜';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'uptime'))) return '可用性监测';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'notification'))) return '通知';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'scheduler')) || prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cron'))) return '定时任务';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'backup'))) return '备份';
  return '基础';
};

const routeDescription = (route) => {
  const prefix = route.prefix || '';
  if (prefix === '/health') return '服务健康检查与版本状态';
  if (prefix === '/api/migration/status') return '读取迁移状态、路由归属和废弃模块信息';
  if (prefix === '/api/system/api-docs') return '读取系统自动生成的 API 文档清单';
  if (prefix === '/api/system/openapi.json') return '导出 OpenAPI 3.1 接口文档';
  if (prefix === '/api/openapi.json') return '导出 OpenAPI 3.1 接口文档';
  if (prefix === '/api/ai-access') return '读取 AI 接入、Agent Key、MCP、Skill 和审计概览';
  if (prefix === '/api/ai-access/key/rotate') return '轮换 AI Agent Key';
  if (prefix.startsWith('/api/ai-access/mcp-servers')) return '管理 AI 接入的 MCP 服务配置';
  if (prefix.startsWith('/api/ai-access/skills')) return '管理 AI 接入的 Skill 配置';
  if (prefix === '/api/ai-access/audit/clear') return '清空 AI 接入调用审计';
  if (prefix === '/api/system/ai-access') return '读取 AI 接入、Agent Key、MCP、Skill 和审计概览';
  if (prefix === '/api/system/ai-access/key/rotate') return '轮换 AI Agent Key';
  if (prefix.startsWith('/api/system/ai-access/mcp-servers')) return '管理 AI 接入的 MCP 服务配置';
  if (prefix.startsWith('/api/system/ai-access/skills')) return '管理 AI 接入的 Skill 配置';
  if (prefix === '/api/system/ai-access/audit/clear') return '清空 AI 接入调用审计';
  if (prefix === '/api/ai/manifest') return '供外部 AI 客户端读取系统接入能力清单';
  if (prefix === '/api/ai/mcp') return '供外部 AI 客户端通过 MCP 调用系统工具';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'auth'))) return '登录认证、会话校验和退出登录';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'database'))) return '数据库统计、分析、导入导出和维护操作';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'log')) || prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'sys-logs')) || prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings', 'app-log-file'))) return '系统日志读取、清理和保留策略';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'settings'))) return '读取和保存系统运行配置';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'system'))) return '系统运行状态、日志、统计和管理能力';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'logs')) || prefix.startsWith(routePrefixLiteral('ws', 'logs'))) return '读取系统日志和实时日志流';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cloudflare', 'accounts'))) return '管理 Cloudflare 账号、令牌、Pages、Workers、R2、Tunnel 和 Zone 资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cloudflare'))) return '管理 Cloudflare DNS、边缘资源和账号资产';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'agent'))) return '管理服务器 Agent 安装、密钥、状态和心跳';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'metrics'))) return '读取服务器指标历史、最新指标和清理记录';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'network-quality'))) return '管理服务器网络质量目标和采集结果';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'sftp'))) return '通过 SFTP 浏览、读写、上传和下载文件';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server', 'tasks'))) return '管理服务器任务、任务日志和执行流';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'server'))) return '管理主机实例、凭据、Docker、终端和监控能力';
  if (prefix.startsWith(routePrefixLiteral('ws', 'ssh')) || prefix.startsWith(routePrefixLiteral('socket.io'))) return '主机终端和 Agent 实时连接';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'openai')) || prefix.startsWith(routePrefixLiteral('v1')) || prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'chat'))) return 'OpenAI 兼容模型代理、聊天和流式响应';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'aliyun'))) return '管理阿里云 DNS、计算和云资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'tencent'))) return '管理腾讯云 DNS、计算和云资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'koyeb'))) return '管理 Koyeb 账号、服务和部署资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'flyio'))) return '管理 Fly.io 账号、应用和机器资源';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'totp'))) return '管理双因子认证账户、分组和动态验证码';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'filebox'))) return '管理文件柜上传、分享、历史记录和下载';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'uptime'))) return '管理可用性监测、公开状态、推送和徽章';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'notification'))) return '管理通知渠道、规则、事件目录和发送历史';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'scheduler'))) return '管理工作流调度、DAG、运行记录和分布式节点';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'cron'))) return '管理定时任务、调度器和执行日志';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'backup'))) return '管理本地备份配置、备份记录和执行器';
  if (route.owner === 'retired') return '历史模块已停用，暂未迁移到当前后端';
  return route.description || '系统接口';
};

const routeStatus = (route) => (route.owner === 'retired' ? 'retired' : 'active');

const routeMethods = (route) => {
  if (route.responseMode === 'websocket') return ['GET'];
  if (route.responseMode === 'stream') return route.prefix?.startsWith('/v1') ? ['GET', 'POST'] : ['GET'];
  if (route.owner === 'retired') return ['GET'];
  if (route.matchMode === 'pattern') return ['GET', 'POST', 'PUT', 'DELETE'];
  if (route.auth === 'public' && (route.prefix === '/health' || String(route.description || '').includes('status'))) {
    return ['GET'];
  }
  return ['GET', 'POST', 'PUT', 'DELETE'];
};

const countBy = (routes, keyFn) => routes.reduce((acc, route) => {
  const key = keyFn(route);
  acc[key] = (acc[key] || 0) + 1;
  return acc;
}, {});

const normalizeRoutes = (routes = []) => sortRoutes(routes.map((route) => ({
  prefix: route.prefix || '',
  module: route.module || '',
  group: route.group || routeGroup(route),
  owner: route.owner || 'go',
  auth: route.auth || 'session',
  responseMode: route.responseMode || 'json',
  description: routeDescription(route),
  matchMode: route.matchMode || 'prefix',
  methods: Array.isArray(route.methods) && route.methods.length > 0 ? route.methods : routeMethods(route),
  status: route.status || routeStatus(route),
})));

const normalizeDocsPayload = (payload = {}) => {
  const routes = normalizeRoutes(Array.isArray(payload.routes) ? payload.routes : []);
  const summary = normalizeSummary({
    total: routes.length,
    byOwner: countBy(routes, (route) => route.owner),
    byAuth: countBy(routes, (route) => route.auth),
    byGroup: countBy(routes, (route) => route.group),
    byStatus: countBy(routes, (route) => route.status),
    byResponse: countBy(routes, (route) => route.responseMode),
    ...(payload.summary || {}),
  });

  return {
    ...payload,
    routes,
    summary,
    aiAccess: payload.aiAccess || {
      plannedModules: [
        { id: 'providers', name: '模型端点', description: '统一管理 OpenAI 兼容端点、模型发现、健康检测与负载均衡' },
        { id: 'mcp', name: 'MCP 服务', description: '管理 MCP 服务、工具发现、资源、提示词与调用权限' },
        { id: 'skills', name: 'Skill 管理', description: '管理本地 Skill、版本、入口、依赖与启用状态' },
        { id: 'permissions', name: '工具权限', description: '统一约束模型、MCP、Skill 和内部系统动作的调用边界' },
        { id: 'audit', name: '调用审计', description: '记录模型请求、工具调用、Skill 执行、耗时和失败原因' },
      ],
    },
  };
};

const fetchJsonEnvelope = async (url) => {
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

const formatJSON = (value) => JSON.stringify(value || {}, null, 2);

function StatCard({ icon: Icon, label, value, tone = 'brand' }) {
  const toneClass = {
    brand: 'text-kumo-brand bg-kumo-brand/10 border-kumo-brand/20',
    success: 'text-kumo-success bg-kumo-success/10 border-kumo-success/20',
    warning: 'text-kumo-warning bg-kumo-warning/10 border-kumo-warning/20',
    info: 'text-kumo-info bg-kumo-info/10 border-kumo-info/20',
  }[tone] || 'text-kumo-brand bg-kumo-brand/10 border-kumo-brand/20';

  return (
    <AppCard padding="md" className="min-w-0">
      <div className="flex items-center gap-3">
        <div className={cx('flex h-9 w-9 shrink-0 items-center justify-center rounded-md border', toneClass)}>
          <Icon className="h-4 w-4" />
        </div>
        <div className="min-w-0">
          <div className="text-[11px] font-semibold text-kumo-subtle">{label}</div>
          <div className="mt-0.5 truncate font-mono text-xl font-bold text-kumo-strong">{value}</div>
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
      className="w-full min-w-34 text-xs text-kumo-strong sm:w-36"
    />
  );
}

function RouteMethodPills({ methods = [] }) {
  return (
    <div className="flex flex-wrap gap-1">
      {methods.map((method) => (
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

function RouteList({ routes, selectedRoute, onSelect }) {
  if (routes.length === 0) {
    return (
      <EmptyState
        icon={Search}
        title="没有匹配的接口"
        description="调整搜索词或筛选条件后再查看。"
      />
    );
  }

  return (
    <AppCard padding="none" className={cx(fixedPanelClass, 'min-h-0 overflow-hidden')}>
      <div className="h-full overflow-y-auto divide-y divide-kumo-line/80">
        {routes.map((route) => {
          const active = selectedRoute && getRouteKey(selectedRoute) === getRouteKey(route);
          return (
            <button
              key={getRouteKey(route)}
              type="button"
              onClick={() => onSelect(route)}
              className={cx(
                'flex w-full min-w-0 flex-col gap-2 px-3 py-3 text-left transition-colors hover:bg-kumo-recessed/60',
                active && 'bg-kumo-brand/10'
              )}
            >
              <div className="flex min-w-0 items-center justify-between gap-2">
                <div className="min-w-0 truncate font-mono text-xs font-bold text-kumo-strong">
                  {route.prefix}
                </div>
                <InlineStatusPill tone={STATUS_TONE[route.status]}>
                  {STATUS_LABEL[route.status] || route.status}
                </InlineStatusPill>
              </div>
              <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                <RouteMethodPills methods={route.methods} />
                <InlineStatusPill tone={AUTH_TONE[route.auth]}>
                  {AUTH_LABEL[route.auth] || route.auth}
                </InlineStatusPill>
                <span className="truncate text-[11px] font-semibold text-kumo-subtle">
                  {route.group}
                </span>
              </div>
              <div className="line-clamp-2 text-xs leading-relaxed text-kumo-subtle">
                {route.description}
              </div>
            </button>
          );
        })}
      </div>
    </AppCard>
  );
}

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
      <div className={fixedPanelClass}>
        <EmptyState
          icon={FileText}
          title="选择一个接口"
          description="左侧接口列表会自动跟随后端路由清单更新。"
          className="h-full"
        />
      </div>
    );
  }

  const curl = `curl -X ${route.methods?.[0] || 'GET'} "${window.location.origin}${route.prefix}"`;

  return (
    <AppCard padding="lg" className={cx(fixedPanelClass, 'min-h-0 overflow-y-auto')}>
      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-kumo-line pb-4">
        <div className="min-w-0">
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <InlineStatusPill tone={STATUS_TONE[route.status]}>
              {STATUS_LABEL[route.status] || route.status}
            </InlineStatusPill>
            <InlineStatusPill tone={AUTH_TONE[route.auth]}>
              {AUTH_LABEL[route.auth] || route.auth}
            </InlineStatusPill>
            <InlineStatusPill tone="neutral">
              {RESPONSE_LABEL[route.responseMode] || route.responseMode}
            </InlineStatusPill>
          </div>
          <h2 className="break-all font-mono text-base font-bold text-kumo-strong">
            {route.prefix}
          </h2>
          <p className="mt-2 text-sm leading-relaxed text-kumo-subtle">{route.description}</p>
        </div>
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
      </div>

      <div className="grid gap-3 py-4 sm:grid-cols-2">
        <InfoRow label="模块" value={route.module} />
        <InfoRow label="分组" value={route.group} />
        <InfoRow label="归属" value={route.owner} />
        <InfoRow label="匹配模式" value={route.matchMode} />
        <InfoRow label="认证方式" value={AUTH_LABEL[route.auth] || route.auth} />
        <InfoRow label="响应类型" value={RESPONSE_LABEL[route.responseMode] || route.responseMode} />
      </div>

      <div className="space-y-3 border-t border-kumo-line pt-4">
        <div>
          <div className="mb-2 text-xs font-semibold text-kumo-subtle">请求方法</div>
          <RouteMethodPills methods={route.methods} />
        </div>
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
    </AppCard>
  );
}

function InfoRow({ label, value }) {
  return (
    <div className="min-w-0 rounded-md border border-kumo-line/80 bg-kumo-recessed/30 px-3 py-2">
      <div className="text-[11px] font-semibold text-kumo-subtle">{label}</div>
      <div className="mt-1 truncate font-mono text-xs font-bold text-kumo-strong">{value || '-'}</div>
    </div>
  );
}

function SnippetBox({ label, value, onCopy }) {
  return (
    <div className="min-w-0 rounded-md border border-kumo-line bg-kumo-recessed/35">
      <div className="flex items-center justify-between gap-2 border-b border-kumo-line px-3 py-2">
        <div className="truncate text-xs font-bold text-kumo-strong">{label}</div>
        <Button size="sm" variant="ghost" onClick={() => onCopy(value, `${label} 已复制`)} className="gap-1.5">
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

function AIAccessConsole({
  aiAccess,
  loading,
  error,
  keyVisible,
  setKeyVisible,
  mcpForm,
  setMcpForm,
  mcpEditingId,
  skillForm,
  setSkillForm,
  skillEditingId,
  onRefresh,
  onRotateKey,
  onSaveMCP,
  onEditMCP,
  onCancelMCP,
  onDeleteMCP,
  onSaveSkill,
  onEditSkill,
  onCancelSkill,
  onDeleteSkill,
  onClearAudit,
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
        action={<Button size="sm" variant="secondary" onClick={onRefresh}>重试</Button>}
      />
    );
  }

  const agentKey = aiAccess?.agentKey || {};
  const endpoints = aiAccess?.endpoints || {};
  const configs = aiAccess?.configs || {};
  const tools = aiAccess?.tools || [];
  const policy = aiAccess?.policy || {};
  const mcpServers = aiAccess?.mcpServers || [];
  const skills = aiAccess?.skills || [];
  const audit = aiAccess?.audit || [];

  return (
    <div className="grid h-full min-h-0 min-w-0 gap-4 pt-3 xl:grid-cols-[minmax(360px,0.82fr)_minmax(0,1.18fr)]">
      <div className={cx(fixedPanelClass, 'min-h-0 space-y-4 overflow-y-auto px-px pb-2 pr-1')}>
        <div className="grid gap-3 sm:grid-cols-2">
          <StatCard icon={Settings} label="可用工具" value={tools.length} />
          <StatCard icon={Plug} label="MCP 服务" value={mcpServers.length} tone="info" />
          <StatCard icon={Bot} label="Skill" value={skills.length} tone="success" />
          <StatCard icon={Activity} label="审计记录" value={audit.length} tone="warning" />
        </div>

        <AppCard padding="md">
          <SectionHeader title="Agent Key" />
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <div className="min-w-0 flex-1 truncate rounded-md border border-kumo-line bg-kumo-recessed/40 px-3 py-2 font-mono text-xs font-bold text-kumo-strong">
              {keyVisible ? agentKey.value : agentKey.masked}
            </div>
            <Button size="sm" variant="secondary" onClick={() => setKeyVisible(!keyVisible)}>
              {keyVisible ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
            </Button>
            <Button size="sm" variant="secondary" onClick={() => onCopy(agentKey.value, 'Agent Key 已复制')}>
              <Copy className="h-3.5 w-3.5" />
            </Button>
            <Button size="sm" variant="danger" onClick={onRotateKey} className="gap-1.5">
              <Key className="h-3.5 w-3.5" />
              <span>轮换</span>
            </Button>
          </div>
        </AppCard>

        <AppCard padding="md">
          <SectionHeader title="接入地址" />
          <div className="space-y-2">
            {Object.entries(endpoints).map(([key, value]) => (
              <button
                key={key}
                type="button"
                onClick={() => onCopy(value, '地址已复制')}
                className="flex w-full min-w-0 items-center justify-between gap-2 rounded-md border border-kumo-line bg-kumo-recessed/30 px-3 py-2 text-left hover:border-kumo-brand/60"
              >
                <span className="text-xs font-bold text-kumo-subtle">{key}</span>
                <span className="min-w-0 truncate font-mono text-xs text-kumo-strong">{value}</span>
              </button>
            ))}
          </div>
        </AppCard>

        <AppCard padding="md">
          <SectionHeader title="调用策略" />
          <div className="grid gap-2 text-xs text-kumo-subtle">
            <div className="flex items-center justify-between gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2">
              <span>允许方法</span>
              <span className="font-mono text-kumo-strong">{(policy.allowedMethods || []).join(' / ') || '-'}</span>
            </div>
            <div className="flex items-center justify-between gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2">
              <span>请求体限制</span>
              <span className="font-mono text-kumo-strong">{policy.bodyLimitBytes ? `${Math.round(policy.bodyLimitBytes / 1024)} KB` : '-'}</span>
            </div>
            <div className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 px-3 py-2 leading-relaxed">
              {policy.auth || 'Agent Key 调用会写入审计记录。'}
            </div>
          </div>
        </AppCard>

        <AppCard padding="md">
          <SectionHeader title="MCP 服务" />
          <div className="grid gap-2">
            <Input size="sm" value={mcpForm.name} onChange={(event) => setMcpForm({ ...mcpForm, name: event.target.value })} placeholder="服务名称" className="text-xs" />
            <div className="grid gap-2 sm:grid-cols-2">
              <Select
                size="sm"
                aria-label="传输方式"
                value={mcpForm.transport}
                onValueChange={(value) => setMcpForm({ ...mcpForm, transport: value })}
                items={[{ value: 'stdio', label: 'stdio' }, { value: 'http', label: 'HTTP' }, { value: 'sse', label: 'SSE' }]}
                className="text-xs"
              />
              <Select
                size="sm"
                aria-label="启用状态"
                value={mcpForm.enabled ? 'true' : 'false'}
                onValueChange={(value) => setMcpForm({ ...mcpForm, enabled: value === 'true' })}
                items={[{ value: 'true', label: '启用' }, { value: 'false', label: '停用' }]}
                className="text-xs"
              />
            </div>
            <Input size="sm" value={mcpForm.command} onChange={(event) => setMcpForm({ ...mcpForm, command: event.target.value })} placeholder="启动命令" className="text-xs" />
            <Input size="sm" value={mcpForm.url} onChange={(event) => setMcpForm({ ...mcpForm, url: event.target.value })} placeholder="远程地址" className="text-xs" />
            <Textarea value={mcpForm.description} onChange={(event) => setMcpForm({ ...mcpForm, description: event.target.value })} placeholder="说明" className="min-h-20 text-xs" />
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="primary" onClick={onSaveMCP} className="gap-1.5">
                {mcpEditingId ? <Edit className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
                <span>{mcpEditingId ? '保存 MCP' : '添加 MCP'}</span>
              </Button>
              {mcpEditingId && (
                <Button size="sm" variant="secondary" onClick={onCancelMCP} className="gap-1.5">
                  <X className="h-3.5 w-3.5" />
                  <span>取消</span>
                </Button>
              )}
            </div>
          </div>
          <div className="mt-3 space-y-2">
            {mcpServers.length === 0 && <div className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 p-3 text-xs text-kumo-subtle">暂无 MCP 服务</div>}
            {mcpServers.map((item) => (
              <div key={item.id} className={cx('rounded-md border bg-kumo-recessed/25 p-3', item.id === mcpEditingId ? 'border-kumo-brand/70' : 'border-kumo-line/80')}>
                <div className="flex items-center justify-between gap-2">
                  <div className="min-w-0">
                    <div className="truncate text-xs font-bold text-kumo-strong">{item.name}</div>
                    <div className="mt-0.5 truncate font-mono text-[11px] text-kumo-subtle">{item.command || item.url || '-'}</div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <InlineStatusPill tone={item.enabled ? 'success' : 'neutral'}>{item.enabled ? '启用' : '停用'}</InlineStatusPill>
                    <Button size="sm" variant="ghost" onClick={() => onEditMCP(item)}>
                      <Edit className="h-3.5 w-3.5" />
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => onDeleteMCP(item.id)}>
                      <Trash className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </AppCard>
      </div>

      <div className={cx(fixedPanelClass, 'min-h-0 space-y-4 overflow-y-auto px-px pb-2 pr-1')}>
        <AppCard padding="md">
          <SectionHeader title="连接 AI" action={<InlineStatusPill tone="success">MCP 已就绪</InlineStatusPill>} />
          <div className="grid gap-2 md:grid-cols-3">
            {[
              { step: '1', title: '复制配置', text: '复制 Codex MCP 或 Claude Desktop 配置，配置内已包含 Agent Key。' },
              { step: '2', title: '粘贴启用', text: '粘贴到 AI 客户端的 MCP 配置区，保存后重启或刷新客户端连接。' },
              { step: '3', title: '调用接口', text: '连接后可使用 list_apis、get_openapi、call_api 等工具访问系统接口。' },
            ].map((item) => (
              <div key={item.step} className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 p-3">
                <div className="mb-2 flex items-center gap-2">
                  <span className="flex h-5 w-5 items-center justify-center rounded border border-kumo-brand/30 bg-kumo-brand/10 font-mono text-[10px] font-bold text-kumo-brand">
                    {item.step}
                  </span>
                  <div className="text-xs font-bold text-kumo-strong">{item.title}</div>
                </div>
                <p className="text-xs leading-relaxed text-kumo-subtle">{item.text}</p>
              </div>
            ))}
          </div>
        </AppCard>

        <AppCard padding="md">
          <SectionHeader title="一键配置" action={<Button size="sm" variant="secondary" onClick={onRefresh}>刷新</Button>} />
          <div className="grid gap-3">
            <SnippetBox label="Codex MCP" value={formatJSON(configs.codex)} onCopy={onCopy} />
            <SnippetBox label="Claude Desktop" value={formatJSON(configs.claudeDesktop)} onCopy={onCopy} />
            <SnippetBox label="cURL" value={configs.curl || ''} onCopy={onCopy} />
          </div>
        </AppCard>

        <AppCard padding="md">
          <SectionHeader title="Skill 管理" />
          <div className="grid gap-2">
            <div className="grid gap-2 sm:grid-cols-2">
              <Input size="sm" value={skillForm.name} onChange={(event) => setSkillForm({ ...skillForm, name: event.target.value })} placeholder="Skill 名称" className="text-xs" />
              <Input size="sm" value={skillForm.version} onChange={(event) => setSkillForm({ ...skillForm, version: event.target.value })} placeholder="版本" className="text-xs" />
            </div>
            <Select
              size="sm"
              aria-label="Skill 启用状态"
              value={skillForm.enabled ? 'true' : 'false'}
              onValueChange={(value) => setSkillForm({ ...skillForm, enabled: value === 'true' })}
              items={[{ value: 'true', label: '启用' }, { value: 'false', label: '停用' }]}
              className="text-xs"
            />
            <Input size="sm" value={skillForm.entrypoint} onChange={(event) => setSkillForm({ ...skillForm, entrypoint: event.target.value })} placeholder="入口路径或命令" className="text-xs" />
            <Input size="sm" value={skillForm.permissionsText} onChange={(event) => setSkillForm({ ...skillForm, permissionsText: event.target.value })} placeholder="权限，逗号分隔" className="text-xs" />
            <Textarea value={skillForm.description} onChange={(event) => setSkillForm({ ...skillForm, description: event.target.value })} placeholder="说明" className="min-h-20 text-xs" />
            <div className="flex flex-wrap gap-2">
              <Button size="sm" variant="primary" onClick={onSaveSkill} className="gap-1.5">
                {skillEditingId ? <Edit className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
                <span>{skillEditingId ? '保存 Skill' : '添加 Skill'}</span>
              </Button>
              {skillEditingId && (
                <Button size="sm" variant="secondary" onClick={onCancelSkill} className="gap-1.5">
                  <X className="h-3.5 w-3.5" />
                  <span>取消</span>
                </Button>
              )}
            </div>
          </div>
          <div className="mt-3 grid gap-2">
            {skills.length === 0 && <div className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 p-3 text-xs text-kumo-subtle">暂无 Skill</div>}
            {skills.map((item) => (
              <div key={item.id} className={cx('rounded-md border bg-kumo-recessed/25 p-3', item.id === skillEditingId ? 'border-kumo-brand/70' : 'border-kumo-line/80')}>
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="flex min-w-0 items-center gap-2">
                      <div className="truncate text-xs font-bold text-kumo-strong">{item.name}</div>
                      <InlineStatusPill tone="info">{item.version || '1.0.0'}</InlineStatusPill>
                      <InlineStatusPill tone={item.enabled ? 'success' : 'neutral'}>{item.enabled ? '启用' : '停用'}</InlineStatusPill>
                    </div>
                    <div className="mt-1 line-clamp-2 text-xs text-kumo-subtle">{item.description || item.entrypoint || '-'}</div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Button size="sm" variant="ghost" onClick={() => onEditSkill(item)}>
                      <Edit className="h-3.5 w-3.5" />
                    </Button>
                    <Button size="sm" variant="ghost" onClick={() => onDeleteSkill(item.id)}>
                      <Trash className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </AppCard>

        <AppCard padding="md">
          <SectionHeader title="AI 工具" action={<InlineStatusPill tone="info">Agent 可调用</InlineStatusPill>} />
          <div className="grid gap-2">
            {tools.map((tool) => (
              <div key={tool.name} className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 p-3">
                <div className="flex min-w-0 items-center justify-between gap-2">
                  <div className="truncate font-mono text-xs font-bold text-kumo-strong">{tool.name}</div>
                  <InlineStatusPill tone={tool.name === 'call_api' ? 'success' : 'neutral'}>
                    {tool.name === 'call_api' ? '内部调用' : '读取'}
                  </InlineStatusPill>
                </div>
                <div className="mt-1 line-clamp-2 text-xs leading-relaxed text-kumo-subtle">{tool.description}</div>
              </div>
            ))}
          </div>
        </AppCard>

        <AppCard padding="md">
          <SectionHeader title="调用审计" action={<Button size="sm" variant="secondary" onClick={onClearAudit}>清空</Button>} />
          <div className="space-y-2">
            {audit.length === 0 && <div className="rounded-md border border-kumo-line/80 bg-kumo-recessed/25 p-3 text-xs text-kumo-subtle">暂无审计记录</div>}
            {audit.map((item) => (
              <div key={item.id} className="grid gap-2 rounded-md border border-kumo-line/80 bg-kumo-recessed/25 p-3 md:grid-cols-[1fr_auto]">
                <div className="min-w-0">
                  <div className="truncate text-xs font-bold text-kumo-strong">{item.action}</div>
                  <div className="mt-1 truncate font-mono text-[11px] text-kumo-subtle">{item.target || item.details}</div>
                </div>
                <div className="flex items-center gap-2 md:justify-end">
                  <InlineStatusPill tone={item.status === 'success' ? 'success' : 'danger'}>{item.status}</InlineStatusPill>
                  <span className="font-mono text-[11px] text-kumo-subtle">{item.latencyMs}ms</span>
                </div>
              </div>
            ))}
          </div>
        </AppCard>
      </div>
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
  const [mcpForm, setMcpForm] = useState(defaultMCPForm);
  const [mcpEditingId, setMcpEditingId] = useState('');
  const [skillForm, setSkillForm] = useState(defaultSkillForm);
  const [skillEditingId, setSkillEditingId] = useState('');

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
      setSelectedKey((current) => {
        if (current && normalizedDocs.routes.some((route) => getRouteKey(route) === current)) {
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

  const routes = docs?.routes || [];
  const summary = normalizeSummary(docs?.summary);

  const groupItems = useMemo(() => {
    const groups = [...new Set(routes.map((route) => route.group).filter(Boolean))].sort((a, b) => a.localeCompare(b, 'zh-CN'));
    return [{ value: 'all', label: '全部分组' }, ...groups.map((item) => ({ value: item, label: item }))];
  }, [routes]);

  const filteredRoutes = useMemo(() => {
    const text = query.trim().toLowerCase();
    return routes.filter((route) => {
      if (group !== 'all' && route.group !== group) return false;
      if (auth !== 'all' && route.auth !== auth) return false;
      if (status !== 'all' && route.status !== status) return false;
      if (!text) return true;
      return [
        route.prefix,
        route.module,
        route.group,
        route.description,
        route.auth,
        route.responseMode,
      ].some((value) => String(value || '').toLowerCase().includes(text));
    });
  }, [auth, group, query, routes, status]);

  const selectedRoute = useMemo(() => {
    const visibleSelected = filteredRoutes.find((route) => getRouteKey(route) === selectedKey);
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

  const saveMCP = async () => {
    try {
      const url = mcpEditingId ? `${AI_ACCESS_BASE}/mcp-servers/${mcpEditingId}` : `${AI_ACCESS_BASE}/mcp-servers`;
      const payload = await apiRequest(url, {
        method: mcpEditingId ? 'PUT' : 'POST',
        body: JSON.stringify(mcpForm),
      });
      setAiAccess(payload);
      setMcpForm(defaultMCPForm);
      setMcpEditingId('');
      toast.success('MCP 服务已保存');
    } catch (error) {
      toast.error(error.message || '保存失败');
    }
  };

  const editMCP = (item) => {
    setMcpEditingId(item.id);
    setMcpForm({
      name: item.name || '',
      transport: item.transport || 'stdio',
      command: item.command || '',
      url: item.url || '',
      description: item.description || '',
      enabled: item.enabled !== false,
      envJson: item.envJson || '',
    });
  };

  const cancelMCP = () => {
    setMcpEditingId('');
    setMcpForm(defaultMCPForm);
  };

  const deleteMCP = async (id) => {
    try {
      const payload = await apiRequest(`${AI_ACCESS_BASE}/mcp-servers/${id}`, { method: 'DELETE' });
      setAiAccess(payload);
      if (mcpEditingId === id) cancelMCP();
      toast.success('MCP 服务已删除');
    } catch (error) {
      toast.error(error.message || '删除失败');
    }
  };

  const saveSkill = async () => {
    try {
      const permissions = skillForm.permissionsText
        .split(',')
        .map((item) => item.trim())
        .filter(Boolean);
      const url = skillEditingId ? `${AI_ACCESS_BASE}/skills/${skillEditingId}` : `${AI_ACCESS_BASE}/skills`;
      const payload = await apiRequest(url, {
        method: skillEditingId ? 'PUT' : 'POST',
        body: JSON.stringify({ ...skillForm, permissions }),
      });
      setAiAccess(payload);
      setSkillForm(defaultSkillForm);
      setSkillEditingId('');
      toast.success('Skill 已保存');
    } catch (error) {
      toast.error(error.message || '保存失败');
    }
  };

  const editSkill = (item) => {
    setSkillEditingId(item.id);
    setSkillForm({
      name: item.name || '',
      description: item.description || '',
      entrypoint: item.entrypoint || '',
      version: item.version || '1.0.0',
      enabled: item.enabled !== false,
      permissionsText: (item.permissions || []).join(', '),
    });
  };

  const cancelSkill = () => {
    setSkillEditingId('');
    setSkillForm(defaultSkillForm);
  };

  const deleteSkill = async (id) => {
    try {
      const payload = await apiRequest(`${AI_ACCESS_BASE}/skills/${id}`, { method: 'DELETE' });
      setAiAccess(payload);
      if (skillEditingId === id) cancelSkill();
      toast.success('Skill 已删除');
    } catch (error) {
      toast.error(error.message || '删除失败');
    }
  };

  const clearAIAudit = async () => {
    try {
      const payload = await apiRequest(`${AI_ACCESS_BASE}/audit/clear`, { method: 'POST' });
      setAiAccess(payload);
      toast.success('审计记录已清空');
    } catch (error) {
      toast.error(error.message || '清空失败');
    }
  };

  if (loading) {
    return (
      <PageStack className={apiDocsShellClass}>
        <div className="grid gap-3 md:grid-cols-4">
          {Array.from({ length: 4 }).map((_, index) => (
            <AppCard key={index} padding="md">
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
    <PageStack className={apiDocsShellClass}>
      <PageToolbar>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeView}
          onValueChange={setActiveView}
          tabs={[
            { value: 'routes', label: '接口' },
            { value: 'ai', label: 'AI 接入' },
          ]}
        />
        <div className="flex flex-wrap items-center gap-2">
          <Button
            size="sm"
            variant="secondary"
            disabled={refreshing}
            onClick={() => loadDocs(true)}
            className="gap-1.5"
          >
            <RefreshCw className={cx('h-3.5 w-3.5', refreshing && 'animate-spin')} />
            <span>刷新</span>
          </Button>
          <Button
            size="sm"
            variant="primary"
            disabled={!summary.openapiRoute}
            onClick={exportOpenAPI}
            className="gap-1.5"
          >
            <Download className="h-3.5 w-3.5" />
            <span>OpenAPI</span>
          </Button>
        </div>
      </PageToolbar>

      {activeView === 'routes' && (
        <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-visible p-1">
          <div className="grid gap-3 pt-3 sm:grid-cols-2 xl:grid-cols-4">
            <StatCard icon={FileText} label="接口总数" value={summary.total} />
            <StatCard icon={Activity} label="可用接口" value={summary.byStatus.active || 0} tone="success" />
            <StatCard icon={Shield} label="登录保护" value={summary.byAuth.session || 0} tone="warning" />
            <StatCard icon={Bot} label="AI 接口" value={summary.byGroup['AI 接入'] || 0} tone="info" />
          </div>

          <AppCard padding="md">
            <div className="grid gap-3 lg:grid-cols-[minmax(220px,1fr)_auto_auto_auto]">
              <Input
                size="sm"
                aria-label="搜索接口"
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder="搜索路径、模块或描述"
                className="w-full text-xs text-kumo-strong"
              />
              <FilterSelect label="接口分组" value={group} onValueChange={setGroup} items={groupItems} />
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

          <div className="grid min-h-0 min-w-0 flex-1 gap-4 overflow-hidden p-px xl:grid-cols-[minmax(360px,1fr)_minmax(0,1fr)]">
            <RouteList
              routes={filteredRoutes}
              selectedRoute={selectedRoute}
              onSelect={(route) => setSelectedKey(getRouteKey(route))}
            />
            <RouteDetail route={selectedRoute} openapiRoute={summary.openapiRoute} />
          </div>
        </div>
      )}

      {activeView === 'ai' && (
        <div className="min-h-0 flex-1 overflow-visible p-1">
          <AIAccessConsole
            aiAccess={aiAccess}
            loading={aiLoading}
            error={aiError}
            keyVisible={keyVisible}
            setKeyVisible={setKeyVisible}
            mcpForm={mcpForm}
            setMcpForm={setMcpForm}
            mcpEditingId={mcpEditingId}
            skillForm={skillForm}
            setSkillForm={setSkillForm}
            skillEditingId={skillEditingId}
            onRefresh={refreshAIAccess}
            onRotateKey={rotateAIKey}
            onSaveMCP={saveMCP}
            onEditMCP={editMCP}
            onCancelMCP={cancelMCP}
            onDeleteMCP={deleteMCP}
            onSaveSkill={saveSkill}
            onEditSkill={editSkill}
            onCancelSkill={cancelSkill}
            onDeleteSkill={deleteSkill}
            onClearAudit={clearAIAudit}
            onCopy={copyText}
          />
        </div>
      )}
    </PageStack>
  );
}

export default ApiDocsPage;
