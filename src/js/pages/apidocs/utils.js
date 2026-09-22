import { formatDateTime } from '../../modules/utils.js';
import {
  API_KEY_EXPIRY_PRESETS,
  API_SEGMENT,
  FALLBACK_SECTION,
  GROUP_NAME_TO_MODULE_ID,
  GROUP_ORDER,
  MODULE_LABELS,
  OPENAPI_ROUTE,
  SECTION_OF_GROUP,
  SECTION_ORDER,
} from './constants.js';

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

const routePrefixLiteral = (...segments) => `/${segments.join('/')}`;

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
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'gcp'))) return 'Google Cloud';
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'huawei'))) return '华为云';
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
  if (prefix.startsWith(routePrefixLiteral(API_SEGMENT, 'assets'))) return '资产管理';
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

const groupOrderIndex = group => {
  const index = GROUP_ORDER.indexOf(String(group || ''));
  return index === -1 ? GROUP_ORDER.length : index;
};

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

export {
  moduleLabel,
  getAuthHeaders,
  routePrefixLiteral,
  createDefaultAPIKeyForm,
  formatKeyTime,
  toLocalDateTimeInput,
  parseLocalDateTime,
  matchExpiryPreset,
  apiKeyStatus,
  normalizeSummary,
  methodClassName,
  getRouteKey,
  sortRoutes,
  routeGroup,
  groupOrderIndex,
  sectionOfGroup,
  sectionOrderIndex,
  routeDescription,
  routeStatus,
  routeMethods,
  countBy,
  normalizeRoutes,
  normalizeDocsPayload,
  fetchJsonEnvelope,
  apiRequest,
  formatJSON,
  formatAuditDetails,
  buildCurlExample,
  formatDateTime,
};
