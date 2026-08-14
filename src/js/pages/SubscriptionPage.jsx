import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Button, RefreshButton } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input } from '@cloudflare/kumo/components/input';
import { Label } from '@cloudflare/kumo/components/label';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Badge, ClipboardText, Code, DropdownMenu, LayerCard, Meter, Tabs } from '@cloudflare/kumo';
import { AppTable, DataTableFrame, PageStack, SectionCard, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { getOSIconClass, getServerPlatformLabel } from '../modules/osPlatform.js';
import useStore from '../store.js';
import { dialog } from '../modules/dialog.js';
import { toast } from '../modules/toast.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import CountryFlag from '../components/CountryFlag.jsx';
import { Box, Copy, Download, Edit, FileText, Globe, Plug, Plus, RefreshCw, Save, Server, Star, Trash, X } from '../components/Icons.jsx';

const API = '/api/subscription';
const INTERNAL_API = '/api/server/agent/proxy/nodes';
const RUNTIME_API = '/api/server/agent/proxy/runtimes';
const TUNNEL_API = '/api/server/agent/proxy/tunnels';
const PREFERRED_API = '/api/server/agent/proxy/preferred-addresses';
const SERVER_INVENTORY_API = '/api/server/s';
const DEFAULT_EXTERNAL_POOL_ID = 'sub_default_nodes';
const LOAD_TIMEOUT_MS = 8000;
const INITIAL_SKELETON_MS = 900;

const SUBSCRIPTION_LOG_COLUMNS = [
  { id: 'createdAt', role: 'datetime' },
  { id: 'subscription', role: 'primary', minWidth: 176 },
  { id: 'client', role: 'content', minWidth: 200, verticalAlign: 'middle' },
  { id: 'format', role: 'type' },
  { id: 'result', role: 'status' },
  { id: 'nodes', role: 'count' },
  { id: 'traffic', role: 'meta', grow: 1, minWidth: 176, align: 'right' },
];

const TUNNEL_STATUS_META = {
  running: { variant: 'success', label: '已连接' },
  disconnected: { variant: 'warning', label: '已断开' },
  failed: { variant: 'error', label: '部署失败' },
  cleanup_failed: { variant: 'error', label: '清理失败' },
  removing: { variant: 'neutral', label: '卸载中' },
};
const tunnelStatusMeta = (applyStatus) => TUNNEL_STATUS_META[applyStatus] || { variant: 'warning', label: '部署中' };

const SUBSCRIPTION_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'subscription', role: 'primary', minWidth: 176, maxWidth: 220, grow: 0 },
  { id: 'status', role: 'status' },
  { id: 'traffic', role: 'content', minWidth: 220, verticalAlign: 'middle' },
  { id: 'access', role: 'meta', grow: 1, minWidth: 176 },
  { id: 'actions', role: 'actions-lg', width: 208, maxWidth: 220 },
];

const PLAN_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'plan', role: 'primary' },
  { id: 'status', role: 'status' },
  { id: 'quota', role: 'number', grow: 1, minWidth: 176 },
  { id: 'reset', role: 'date', grow: 1, minWidth: 176 },
  { id: 'nodes', role: 'meta', grow: 1, minWidth: 176 },
  { id: 'subscriptions', role: 'count' },
  { id: 'actions', role: 'actions-md' },
];

const NODE_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'name', role: 'primary', minWidth: 176, maxWidth: 200, grow: 0 },
  { id: 'type', role: 'type' },
  { id: 'connection', role: 'content', minWidth: 240 },
  { id: 'host', role: 'meta', grow: 1, minWidth: 176 },
  { id: 'actions', role: 'actions-lg', width: 160, maxWidth: 200 },
];

const RUNTIME_HOST_COLUMNS = [
  { id: 'check', role: 'check' },
  { id: 'status', role: 'status' },
  { id: 'name', role: 'primary', minWidth: 120, width: 120, grow: 0 },
  { id: 'location', role: 'meta', grow: 1, minWidth: 104, align: 'center' },
  { id: 'online', role: 'count', grow: 1, minWidth: 104 },
  { id: 'agentVersion', role: 'meta', grow: 1, minWidth: 104, align: 'center' },
  { id: 'proxy', role: 'content', grow: 0, width: 180, align: 'center', verticalAlign: 'middle' },
  { id: 'nodeType', role: 'type', grow: 1, minWidth: 120 },
  { id: 'actions', role: 'actions-xl', width: 320 },
];

const emptyInternalNodeForm = { server_id: '', name: '', protocol: 'vless-reality', access_mode: 'direct', preferred_address_id: '', public_host: '', server_name: 'www.cloudflare.com', certificate_pem: '', private_key_pem: '', enabled: true, stable: false };

const getInstanceCountryCode = (server) => {
  const direct = String(server?.country_code || server?.countryCode || server?.resolved_country || '').trim();
  if (/^[a-z]{2}$/i.test(direct)) return direct.toUpperCase();
  const location = String(server?.location || '').trim();
  if (/^[a-z]{2}$/i.test(location)) return location.toUpperCase();
  const known = { singapore: 'SG', japan: 'JP', germany: 'DE', france: 'FR', 'hong kong': 'HK', london: 'GB' };
  return known[location.toLowerCase()] || '';
};

const getInstanceLocationLabel = (server) => getInstanceCountryCode(server) || String(server?.location || '—');

const countryFlagEmoji = (countryCode) => {
  const code = String(countryCode || '').trim().toUpperCase();
  if (!/^[A-Z]{2}$/.test(code)) return '';
  return String.fromCodePoint(...code.split('').map((letter) => 127397 + letter.charCodeAt(0)));
};

const formatInstanceUptime = (value) => {
  const text = String(value || '').trim();
  if (!text) return '-';
  const dayMatch = text.match(/(\d+)\s*(?:d|天)/i);
  if (dayMatch) return `${dayMatch[1]}天`;
  return /(?:h|m|s|时|分|秒)/i.test(text) ? '0天' : text;
};

const emptySubscriptionForm = {
  plan_id: '',
  name: '',
  remark: '',
  enabled: true,
  template_id: 'builtin_mihomo_default',
};

const emptyPlanForm = {
  name: '', remark: '', enabled: true, total_bytes: 0, cycle_type: 'monthly', cycle_day: 1,
  rate_limit_enabled: true, rate_limit_per_minute: 30, node_ids: [], selection_mode: 'explicit', include_internal_nodes: true, include_external_nodes: false,
};

const emptyTemplateForm = {
  name: '',
  format: 'clash',
  content: '',
  description: '',
};

const emptyNodeForm = {
  name: '',
  type: '',
  server: '',
  port: 0,
  country_code: '',
  location: '',
  tags: '',
  traffic_server_id: '',
  ownership: 'external',
  management: 'unmanaged',
  traffic_reporting: 'unavailable',
  enabled: true,
  stable: false,
  sort_order: 0,
  raw: '',
  config_json: '',
};

const safeBtoa = (str) => {
  try {
    return btoa(unescape(encodeURIComponent(str)));
  } catch (e) {
    return btoa(str);
  }
};

const safeAtob = (str) => {
  try {
    return decodeURIComponent(escape(atob(str)));
  } catch (e) {
    return atob(str);
  }
};

const parseNodeUrlToConfig = (urlStr) => {
  try {
    const raw = String(urlStr).trim();
    if (!raw) return null;

    if (raw.toLowerCase().startsWith('vmess://')) {
      const b64Part = raw.substring(8).trim();
      try {
        const decoded = safeAtob(b64Part);
        const obj = JSON.parse(decoded);
        const name = obj.ps || 'vmess-node';
        const server = obj.add || '';
        const port = Number(obj.port) || 0;
        const type = 'vmess';
        
        const config = {
          name,
          type,
          server,
          port,
          uuid: obj.id,
          alterId: Number(obj.aid) || 0,
          cipher: obj.scy || 'auto',
        };
        if (obj.net) config.network = obj.net;
        if (obj.tls === 'tls') {
          config.tls = true;
          if (obj.sni) {
            config.sni = obj.sni;
            config.servername = obj.sni;
          }
        }
        if (obj.net === 'ws') {
          config['ws-opts'] = {
            path: obj.path || '/',
          };
          if (obj.host) {
            config['ws-opts'].headers = { Host: obj.host };
          }
        }
        return { name, type, server, port, config };
      } catch (e) {}
    }

    const match = raw.match(/^([^:]+):\/\/([^@]+@)?([^:\/?#]+)(?::(\d+))?([^#]*)(?:#(.*))?$/);
    if (!match) return null;

    let type = match[1].toLowerCase();
    if (type === 'hy2') type = 'hysteria2';

    const userInfo = match[2] ? match[2].slice(0, -1) : '';
    const server = match[3];
    const port = match[4] ? Number(match[4]) : 0;
    const rest = match[5] || '';
    const hash = match[6] ? decodeURIComponent(match[6]) : '';
    const name = hash || `${type}-node`;

    const config = {
      name,
      type,
      server,
      port,
    };

    const query = {};
    if (rest.startsWith('?')) {
      const parts = rest.substring(1).split('&');
      for (const part of parts) {
        const [k, v] = part.split('=');
        if (k) {
          query[decodeURIComponent(k)] = decodeURIComponent(v || '');
        }
      }
    }

    if (type === 'vless') {
      config.uuid = userInfo;
      if (query.encryption && query.encryption !== 'none') {
        config.encryption = query.encryption;
      }
      const network = query.type || query.network;
      if (network && network !== 'tcp') {
        config.network = network;
      }
      if (query.security === 'tls') {
        config.tls = true;
      }
      const sni = query.sni || query.servername;
      if (sni) {
        config.servername = sni;
        config.sni = sni;
      }
      if (query.fp) {
        config['client-fingerprint'] = query.fp;
      }
      if (query.allowInsecure === '1' || query.allowInsecure === 'true' || query.insecure === '1' || query.insecure === 'true' || query['skip-cert-verify'] === 'true') {
        config['skip-cert-verify'] = true;
      }
      if (network === 'ws') {
        config['ws-opts'] = {};
        if (query.path) config['ws-opts'].path = query.path;
        const host = query.host || query.Host || sni;
        if (host) {
          config['ws-opts'].headers = { Host: host };
        }
        if (Object.keys(config['ws-opts']).length === 0) delete config['ws-opts'];
      }
    } else if (type === 'trojan') {
      config.password = userInfo;
      config.tls = true;
      const sni = query.sni || query.peer || query.servername;
      if (sni) {
        config.sni = sni;
      }
      if (query.allowInsecure === '1' || query.allowInsecure === 'true' || query.insecure === '1' || query.insecure === 'true' || query['skip-cert-verify'] === 'true') {
        config['skip-cert-verify'] = true;
      }
      if (query.alpn) {
        config.alpn = query.alpn.split(',');
      }
      const network = query.type || query.network;
      if (network) {
        config.network = network;
      }
      if (query.fp) {
        config['client-fingerprint'] = query.fp;
      }
    } else if (type === 'hysteria2') {
      config.password = userInfo;
      const sni = query.sni || query.peer || query.servername;
      if (sni) {
        config.sni = sni;
      }
      if (query.insecure === '1' || query.insecure === 'true' || query.allowInsecure === '1' || query.allowInsecure === 'true' || query['skip-cert-verify'] === 'true') {
        config['skip-cert-verify'] = true;
      }
      if (query.alpn) {
        config.alpn = query.alpn.split(',');
      }
    } else if (type === 'ss') {
      if (userInfo) {
        try {
          const decoded = safeAtob(userInfo);
          const parts = decoded.split(':');
          if (parts.length === 2) {
            config.cipher = parts[0];
            config.password = parts[1];
          }
        } catch (e) {
          const parts = userInfo.split(':');
          if (parts.length === 2) {
            config.cipher = parts[0];
            config.password = parts[1];
          }
        }
      }
    }

    return { name, type, server, port, config };
  } catch (err) {
    return null;
  }
};

const buildNodeUrl = (config) => {
  try {
    if (!config || !config.type || !config.server || !config.port) return '';
    const type = config.type.toLowerCase();
    const server = config.server;
    const port = config.port;
    const name = config.name || '';

    if (type === 'vmess') {
      const obj = {
        v: '2',
        ps: name,
        add: server,
        port: String(port),
        id: config.uuid || '',
        aid: String(config.alterId || 0),
        net: config.network || 'tcp',
        type: 'none',
        host: config['ws-opts']?.headers?.Host || '',
        path: config['ws-opts']?.path || '',
        tls: config.tls ? 'tls' : '',
        sni: config.sni || config.servername || '',
      };
      return 'vmess://' + safeBtoa(JSON.stringify(obj));
    }

    let userInfo = '';
    const query = [];

    if (type === 'vless') {
      userInfo = config.uuid || '';
      if (config.encryption) query.push(`encryption=${encodeURIComponent(config.encryption)}`);
      if (config.network) query.push(`type=${encodeURIComponent(config.network)}`);
      if (config.tls) query.push(`security=tls`);
      const sni = config.sni || config.servername;
      if (sni) query.push(`sni=${encodeURIComponent(sni)}`);
      if (config['client-fingerprint']) query.push(`fp=${encodeURIComponent(config['client-fingerprint'])}`);
      if (config['skip-cert-verify']) query.push(`skip-cert-verify=true`);
      if (config.network === 'ws' && config['ws-opts']?.path) {
        query.push(`path=${encodeURIComponent(config['ws-opts'].path)}`);
      }
    } else if (type === 'trojan') {
      userInfo = config.password || '';
      const sni = config.sni;
      if (sni) query.push(`sni=${encodeURIComponent(sni)}`);
      if (config['skip-cert-verify']) query.push(`skip-cert-verify=true`);
      if (config.alpn) query.push(`alpn=${encodeURIComponent(config.alpn.join(','))}`);
      if (config.network) query.push(`type=${encodeURIComponent(config.network)}`);
      if (config['client-fingerprint']) query.push(`fp=${encodeURIComponent(config['client-fingerprint'])}`);
    } else if (type === 'hysteria2') {
      userInfo = config.password || '';
      const sni = config.sni;
      if (sni) query.push(`sni=${encodeURIComponent(sni)}`);
      if (config['skip-cert-verify']) query.push(`skip-cert-verify=true`);
      if (config.alpn) query.push(`alpn=${encodeURIComponent(config.alpn.join(','))}`);
    } else if (type === 'ss') {
      if (config.cipher && config.password) {
        userInfo = safeBtoa(`${config.cipher}:${config.password}`);
      }
    }

    const userPart = userInfo ? `${userInfo}@` : '';
    const queryPart = query.length > 0 ? `?${query.join('&')}` : '';
    const hashPart = name ? `#${name}` : '';

    return `${type}://${userPart}${server}:${port}${queryPart}${hashPart}`;
  } catch (e) {
    return '';
  }
};

const syncNodeForm = (prev, changedField, value) => {
  const next = { ...prev, [changedField]: value };

  if (['name', 'type', 'server', 'port'].includes(changedField)) {
    if (changedField === 'type') {
      next.type = value.toLowerCase();
    }

    let parsedConfig = null;
    try {
      parsedConfig = JSON.parse(prev.config_json || '{}');
    } catch (e) {}

    if (!parsedConfig || typeof parsedConfig !== 'object') {
      parsedConfig = {};
    }

    parsedConfig.name = next.name;
    parsedConfig.type = next.type;
    parsedConfig.server = next.server;
    parsedConfig.port = next.port ? Number(next.port) : 0;

    next.config_json = JSON.stringify(parsedConfig);

    if (next.raw) {
      try {
        const match = next.raw.match(/^([^:]+):\/\/([^@]+@)?([^:\/?#]+)(?::(\d+))?([^#]*)(?:#(.*))?$/);
        if (match) {
          const proto = changedField === 'type' ? value.toLowerCase() : match[1];
          const userInfo = match[2] || '';
          const host = changedField === 'server' ? value : match[3];
          const port = changedField === 'port' ? (value ? `:${value}` : '') : (match[4] ? `:${match[4]}` : '');
          const rest = match[5] || '';
          const hash = changedField === 'name' ? `#${value}` : (match[6] ? `#${match[6]}` : '');
          next.raw = `${proto}://${userInfo}${host}${port}${rest}${hash}`;
        }
      } catch (e) {}
    } else {
      next.raw = buildNodeUrl(parsedConfig);
    }
  }

  if (changedField === 'config_json') {
    try {
      const parsedConfig = JSON.parse(value);
      if (parsedConfig && typeof parsedConfig === 'object') {
        if (parsedConfig.name !== undefined) next.name = String(parsedConfig.name);
        if (parsedConfig.type !== undefined) next.type = String(parsedConfig.type).toLowerCase();
        if (parsedConfig.server !== undefined) next.server = String(parsedConfig.server);
        if (parsedConfig.port !== undefined) next.port = Number(parsedConfig.port) || 0;

        next.raw = buildNodeUrl(parsedConfig);
      }
    } catch (e) {}
  }

  if (changedField === 'raw') {
    const parsed = parseNodeUrlToConfig(value);
    if (parsed) {
      next.name = parsed.name;
      next.type = parsed.type;
      next.server = parsed.server;
      next.port = parsed.port;
      next.config_json = JSON.stringify(parsed.config);
    }
  }

  return next;
};

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

const formatBytes = (bytes = 0) => {
  const value = Math.max(0, Number(bytes) || 0);
  if (value < 1024) return `${value} B`;
  const units = ['KB', 'MB', 'GB', 'TB', 'PB'];
  let current = value / 1024;
  let index = 0;
  while (current >= 1024 && index < units.length - 1) {
    current /= 1024;
    index += 1;
  }
  return `${current.toFixed(current >= 100 ? 0 : 1)} ${units[index]}`;
};

const TRAFFIC_UNITS = [
  { value: 'GB', label: 'GB', bytes: 1024 ** 3 },
  { value: 'TB', label: 'TB', bytes: 1024 ** 4 },
];

const trafficUnitBytes = (unit) => TRAFFIC_UNITS.find((item) => item.value === unit)?.bytes || TRAFFIC_UNITS[0].bytes;

const preferredTrafficUnit = (bytes) => {
  const value = Number(bytes) || 0;
  const tbBytes = trafficUnitBytes('TB');
  if (value >= tbBytes) return 'TB';
  return 'GB';
};

const trafficDisplayValue = (bytes, unit) => {
  const value = Number(bytes) || 0;
  if (value <= 0) return '0';
  const converted = value / trafficUnitBytes(unit);
  return Number.isInteger(converted) ? String(converted) : String(Number(converted.toFixed(3)));
};

const formatTime = (value) => {
  if (!value) return '-';
  const date = new Date(String(value).replace(' ', 'T'));
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
};

const statusLabel = (sub) => {
  if (!sub.enabled) return ['停用', 'neutral'];
  if (sub.plan_enabled === false) return ['套餐停用', 'error'];
  if (sub.traffic?.status === 'expired') return ['已过期', 'error'];
  if (sub.traffic?.status === 'exhausted') return ['流量用尽', 'warning'];
  return ['运行中', 'success'];
};

const meteringLabel = (status) => {
  if (status === 'available') return '内部节点精准计量';
  if (status === 'unavailable') return '外部节点不可计量';
  return '等待节点计量同步';
};

const managedNodeState = (node) => {
	if (node.publishable) return ['可发布', 'success'];
	if (node.apply_status === 'runtime_running_unreachable') return ['公网不可达', 'error'];
	if (node.apply_status === 'drifted') return ['状态漂移', 'error'];
	if (node.apply_status === 'remove_failed') return ['卸载失败', 'error'];
	if (node.apply_status === 'failed') return ['部署失败', 'error'];
	if (node.apply_status === 'stopped') return ['已停用', 'neutral'];
	return ['同步中', 'warning'];
};

const normalizeManagedServer = (server) => {
  const info = server?.info && typeof server.info === 'object' ? server.info : {};
  return {
    ...server,
    status: server?.is_online ? 'online' : (server?.status || 'offline'),
    platform: server?.platform || info.platform || '',
    platform_version: server?.platform_version || info.platformVersion || info.platform_version || '',
    agent_version: server?.agent_version || info.agentVersion || info.agent_version || '',
    uptime: server?.uptime || info.uptime || '',
    country_code: server?.country_code || server?.resolved_country || server?.country || '',
  };
};

const isLinuxManagedServer = (server) => {
  const platform = `${server?.platform || ''} ${server?.platform_version || ''}`.trim().toLowerCase();
  if (!platform) return true;
  if (['windows', 'darwin', 'macos', 'freebsd'].some((marker) => platform.includes(marker))) return false;
  return ['linux', 'ubuntu', 'debian', 'centos', 'rhel', 'red hat', 'fedora', 'rocky', 'alma', 'alpine', 'arch', 'opensuse', 'sles', 'oracle linux', 'amzn', 'amazon linux']
    .some((marker) => platform.includes(marker));
};

const parseNodeConfig = (node) => {
  if (!node?.config_json) return {};
  try {
    return JSON.parse(node.config_json);
  } catch {
    return {};
  }
};

const nodeEndpoint = (node) => {
  const host = node.server || '-';
  return `${host}:${node.port || '-'}`;
};

const nodeNetworkTags = (node) => {
  const cfg = parseNodeConfig(node);
  const wsPath = cfg['ws-opts']?.path;
  const sni = cfg.sni || cfg.servername;
  return [
    cfg.network ? { key: 'network', label: cfg.network, tone: String(cfg.network).toLowerCase() } : null,
    cfg.tls ? { key: 'tls', label: 'tls' } : null,
    sni ? { key: 'sni', label: `sni ${sni}` } : null,
    wsPath ? { key: 'path', label: `path ${wsPath}` } : null,
    cfg['client-fingerprint'] ? { key: 'fingerprint', label: `fp ${cfg['client-fingerprint']}` } : null,
    cfg['skip-cert-verify'] ? { key: 'insecure', label: 'insecure' } : null,
    Array.isArray(cfg.alpn) && cfg.alpn.length > 0 ? { key: 'alpn', label: `alpn ${cfg.alpn.join(',')}` } : null,
  ].filter(Boolean);
};

const nodeNetworkTagClass = (tag) => {
  if (tag.key === 'tls') return 'border-kumo-success/20 bg-kumo-success/10 text-kumo-success';
  if (tag.key === 'sni') return 'border-kumo-warning/25 bg-kumo-warning/10 text-kumo-warning';
  if (tag.key === 'path') return 'border-kumo-info/20 bg-kumo-info/10 text-kumo-info';
  if (tag.key === 'fingerprint') return 'border-kumo-badge-purple/20 bg-kumo-badge-purple/10 text-kumo-badge-purple';
  if (tag.key === 'alpn') return 'border-kumo-badge-purple/20 bg-kumo-badge-purple/10 text-kumo-badge-purple';
  if (tag.key === 'insecure') return 'border-kumo-danger/20 bg-kumo-danger/10 text-kumo-danger';
  if (tag.tone === 'ws') return 'border-kumo-info/20 bg-kumo-info/10 text-kumo-info';
  if (tag.tone === 'grpc') return 'border-kumo-badge-purple/20 bg-kumo-badge-purple/10 text-kumo-badge-purple';
  if (tag.tone === 'h2' || tag.tone === 'http') return 'border-kumo-badge-purple/20 bg-kumo-badge-purple/10 text-kumo-badge-purple';
  if (tag.tone === 'tcp') return 'border-kumo-badge-orange/20 bg-kumo-badge-orange/10 text-kumo-badge-orange';
  return 'border-kumo-line bg-kumo-recessed/35 text-kumo-subtle';
};

const nodeCountryCode = (node) => {
  const direct = String(node?.country_code || '').trim();
  if (/^[a-z]{2}$/i.test(direct)) return direct.toUpperCase();
  const name = String(node?.name || '').trim();
  const runes = Array.from(name);
  if (runes.length >= 2) {
    const first = runes[0].codePointAt(0);
    const second = runes[1].codePointAt(0);
    if (first >= 0x1F1E6 && first <= 0x1F1FF && second >= 0x1F1E6 && second <= 0x1F1FF) {
      return String.fromCharCode(65 + first - 0x1F1E6, 65 + second - 0x1F1E6);
    }
  }
  const namePrefix = name.match(/^([A-Za-z]{2})(?=$|[\s_-]|[\u4e00-\u9fa5])/);
  if (namePrefix) return namePrefix[1].toUpperCase();
  const location = String(node?.location || '').trim();
  const locationPrefix = location.match(/^([A-Za-z]{2})(?=$|[\s_-]|[\u4e00-\u9fa5])/);
  return locationPrefix ? locationPrefix[1].toUpperCase() : '';
};

function NodeFlag({ node }) {
  const code = nodeCountryCode(node);
  if (!code) return null;
  return <CountryFlag preferSvg countryCode={code} className="h-3.5 w-5 shrink-0 !rounded-[2px] text-sm" />;
}

const latencyChipClass = (latency) => {
  const value = Number(latency) || 0;
  if (value <= 0) return 'border-kumo-line bg-kumo-recessed/50 text-kumo-subtle';
  if (value <= 120) return 'border-kumo-success/25 bg-kumo-success/10 text-kumo-success';
  if (value <= 260) return 'border-kumo-warning/30 bg-kumo-warning/10 text-kumo-warning';
  return 'border-kumo-danger/25 bg-kumo-danger/10 text-kumo-danger';
};

function NodeHostQuality({ node, serverNameById }) {
  const hostName = node.traffic_server_id ? serverNameById.get(String(node.traffic_server_id)) || node.traffic_server_id : '';
  const orderMap = { '移动': 1, '联通': 2, '电信': 3 };
  const samples = Array.isArray(node?.quality)
    ? [...node.quality].sort((a, b) => {
        const orderA = orderMap[a.name] ?? 99;
        const orderB = orderMap[b.name] ?? 99;
        return orderA - orderB;
      }).slice(0, 3)
    : [];
  return (
    <div className="flex min-w-0 flex-col items-start gap-1 text-left">
      <span
        className={`inline-flex max-w-full items-center rounded-[3px] border px-1.5 py-0.5 text-[10px] font-semibold leading-4 ${hostName ? 'border-kumo-info/25 bg-kumo-info/10 text-kumo-info' : 'border-kumo-line bg-kumo-recessed/45 text-kumo-subtle'}`}
        title={hostName || '未绑定主机'}
      >
        <span className="truncate">{hostName || '未绑定'}</span>
      </span>
      <div className="flex max-w-full flex-wrap justify-start gap-1">
        {samples.length > 0 ? samples.map((item) => {
          const latency = Math.round(Number(item.avg_latency_ms ?? item.latency_ms) || 0);
          return (
            <span
              key={`${item.name}-${item.sampled_at || latency}`}
              className={`inline-flex items-center gap-1 rounded-[3px] border px-1.5 py-0.5 text-[10px] font-semibold leading-4 tabular-nums ${latencyChipClass(latency)}`}
              title={`${item.name || '线路'} 24h 平均 ${latency > 0 ? `${latency}ms` : '暂无延迟'}`}
            >
              <span className="max-w-8 truncate">{item.name || '-'}</span>
              <span>{latency > 0 ? `${latency}ms` : '-'}</span>
            </span>
          );
        }) : (
          <span className="inline-flex rounded-[3px] border border-kumo-line bg-kumo-recessed/45 px-1.5 py-0.5 text-[10px] font-semibold leading-4 text-kumo-subtle">
            暂无延迟
          </span>
        )}
      </div>
    </div>
  );
}

const nodeTypeBadgeVariant = (type) => {
  switch (String(type || '').toLowerCase()) {
    case 'vless':
		return 'purple';
    case 'vmess':
		return 'blue';
    case 'trojan':
		return 'red';
    case 'ss':
    case 'shadowsocks':
		return 'green';
    case 'hysteria2':
    case 'hy2':
    case 'hysteria':
		return 'teal';
    case 'tuic':
		return 'orange';
    case 'socks':
    case 'socks5':
      return 'neutral';
    case 'http':
		return 'secondary';
    default:
		return 'neutral';
  }
};

const subscriptionURL = (base, sub, format = '') => {
  if (!sub?.public_token) return '';
  const suffix = format ? `?format=${format}` : '';
  return `${base}/sub/${sub.public_token}${suffix}`;
};

const normalizePublicBase = (configured, fallback = '') => {
  const value = String(configured || '').trim().replace(/\/+$/g, '');
  if (value) return value.replace(/\/api$/i, '');
  if (!fallback) return '';
  try {
    const url = new URL(fallback);
    if (/^517\d$/.test(url.port)) {
      url.port = '3000';
      return url.origin;
    }
    return url.origin;
  } catch {
    return String(fallback || '').replace(/\/+$/g, '');
  }
};

const copyText = async (text, message = '已复制') => {
  try {
    await navigator.clipboard.writeText(text);
    toast.success(message);
  } catch {
    toast.error('复制失败');
  }
};

const templateLanguage = (format) => (format === 'clash' ? 'yaml' : 'bash');

function TemplateCodeEditor({ label, value, format, onChange }) {
  return (
    <CodeEditor
      value={value}
      onChange={onChange}
      language={templateLanguage(format)}
      label={label}
      minHeight="20rem"
    />
  );
}

function LinkCopyButton({ label, text, onCopy, variant = 'secondary' }) {
  return (
    <Button
      size="sm"
      variant={variant}
      disabled={!text}
      onClick={() => onCopy(text, `${label} 链接已复制`)}
      className="gap-1.5"
    >
      <Copy className="h-3.5 w-3.5" />
      <span>{label}</span>
    </Button>
  );
}

function TrafficSizeInput({ label, value, onChange }) {
  const [unit, setUnit] = useState(() => preferredTrafficUnit(value));

  return (
    <div className="min-w-0 space-y-1.5">
      <Label className="text-xs font-semibold text-kumo-subtle">{label}</Label>
      <div className="grid min-w-0 grid-cols-[minmax(0,1fr)_4.75rem] gap-2">
        <Input
          size="sm"
          aria-label={label}
          type="number"
          min="0"
          step="0.001"
          value={trafficDisplayValue(value, unit)}
          onChange={(event) => onChange(Math.round((Number(event.target.value) || 0) * trafficUnitBytes(unit)))}
          className="w-full min-w-0"
        />
        <Select
          size="sm"
          aria-label={`${label}单位`}
          value={unit}
          onValueChange={(nextUnit) => setUnit(String(nextUnit))}
          items={TRAFFIC_UNITS.map(({ value: itemValue, label: itemLabel }) => ({ value: itemValue, label: itemLabel }))}
          className="w-full min-w-0"
        />
      </div>
    </div>
  );
}

function MasonryGrid({ children, className = '' }) {
  const containerRef = useRef(null);
  const childArray = React.Children.toArray(children);
  const childKeys = childArray.map((child, index) => child.key || index).join('|');
  const [rowSpans, setRowSpans] = useState([]);

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return undefined;

    let frameId = null;
    const items = Array.from(container.children);
    const updateSpans = () => {
      frameId = null;
      const styles = getComputedStyle(container);
      const rowHeight = Number.parseFloat(styles.gridAutoRows) || 1;
      const rowGap = Number.parseFloat(styles.rowGap) || 0;
      const nextSpans = items.map((item) => {
        const content = item.firstElementChild || item;
        const height = content.getBoundingClientRect().height || item.scrollHeight;
        return Math.max(1, Math.ceil((height + rowGap) / (rowHeight + rowGap)));
      });
      setRowSpans((previous) => previous.length === nextSpans.length && previous.every((value, index) => value === nextSpans[index]) ? previous : nextSpans);
    };
    const scheduleUpdate = () => {
      if (frameId === null) frameId = requestAnimationFrame(updateSpans);
    };

    updateSpans();
    const resizeObserver = typeof ResizeObserver === 'function' ? new ResizeObserver(scheduleUpdate) : null;
    items.forEach((item) => resizeObserver?.observe(item.firstElementChild || item));
    resizeObserver?.observe(container);

    return () => {
      if (frameId !== null) cancelAnimationFrame(frameId);
      resizeObserver?.disconnect();
    };
  }, [childKeys]);

  return (
    <div ref={containerRef} className={`grid grid-flow-row-dense grid-cols-1 items-start gap-3 cq-lg:grid-cols-2 ${className}`} style={{ gridAutoRows: '1px' }}>
      {childArray.map((child, index) => (
        <div key={child.key || index} className="min-w-0 self-start" style={rowSpans[index] ? { gridRowEnd: `span ${rowSpans[index]}` } : undefined}>
          {child}
        </div>
      ))}
    </div>
  );
}

function SubscriptionPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const publicApiUrl = useStore((state) => state.publicApiUrl);
  const [activeTab, setActiveTab] = useState('instances');
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [profiles, setProfiles] = useState([]);
  const [plans, setPlans] = useState([]);
  const [subscriptions, setSubscriptions] = useState([]);
  const [nodes, setNodes] = useState([]);
  const [internalNodes, setInternalNodes] = useState([]);
	const [managedTunnels, setManagedTunnels] = useState([]);
	const [managedRuntimes, setManagedRuntimes] = useState([]);
	const [preferredAddresses, setPreferredAddresses] = useState([]);
	const [tunnelForm, setTunnelForm] = useState({ account_id: '', zone_id: '', hostname: '' });
	const [tunnelModalOpen, setTunnelModalOpen] = useState(false);
	const [tunnelTargetServer, setTunnelTargetServer] = useState(null);
	const [preferredModalOpen, setPreferredModalOpen] = useState(false);
	const [preferredForm, setPreferredForm] = useState({ name: '', address: '', port: 443, enabled: true, is_default: false });
	const [cloudflareAccounts, setCloudflareAccounts] = useState([]);
	const [cloudflareZones, setCloudflareZones] = useState([]);
	const [tunnelTasks, setTunnelTasks] = useState({});
	const [nodeTasks, setNodeTasks] = useState({});
	const [selectedInternalHosts, setSelectedInternalHosts] = useState(new Set());
	const [selectedRuntimeHosts, setSelectedRuntimeHosts] = useState(new Set());
	const [internalNodeModalOpen, setInternalNodeModalOpen] = useState(false);
	const [internalNodeForm, setInternalNodeForm] = useState(emptyInternalNodeForm);
	const [editingInternalNodeId, setEditingInternalNodeId] = useState(null);
  const [internalNodeActions, setInternalNodeActions] = useState({});
  const [externalNodeActions, setExternalNodeActions] = useState({});
  const [uninstallingServerId, setUninstallingServerId] = useState('');
  const [templates, setTemplates] = useState([]);
  const [logs, setLogs] = useState([]);
  const [servers, setServers] = useState([]);
  const [settings, setSettings] = useState(null);

  const [subscriptionModalOpen, setSubscriptionModalOpen] = useState(false);
  const [subscriptionForm, setSubscriptionForm] = useState(emptySubscriptionForm);
  const [editingSubscriptionId, setEditingSubscriptionId] = useState(null);
  const [planModalOpen, setPlanModalOpen] = useState(false);
  const [planForm, setPlanForm] = useState(emptyPlanForm);
  const [editingPlanId, setEditingPlanId] = useState(null);
  const [planNodeTypeFilter, setPlanNodeTypeFilter] = useState('all');
  const [planNodeSourceFilter, setPlanNodeSourceFilter] = useState('all');

  const [importModalOpen, setImportModalOpen] = useState(false);
  const [importText, setImportText] = useState('');
  const [importSourceURL, setImportSourceURL] = useState('');
  const [importPreview, setImportPreview] = useState([]);
  const [nodeModalOpen, setNodeModalOpen] = useState(false);
  const [nodeForm, setNodeForm] = useState(emptyNodeForm);
  const [editingNodeId, setEditingNodeId] = useState(null);
  const [protocolFilter, setProtocolFilter] = useState('all');
  const [tagFilter, setTagFilter] = useState('all');

  const [templateModalOpen, setTemplateModalOpen] = useState(false);
  const [templateForm, setTemplateForm] = useState(emptyTemplateForm);
  const [editingTemplateId, setEditingTemplateId] = useState(null);
  const [templateSubscriptionId, setTemplateSubscriptionId] = useState('');
  const [templateBindingId, setTemplateBindingId] = useState('');
  const loadGenerationRef = useRef(0);
	const terminalTaskIDsRef = useRef(new Set());

  const publicBase = useMemo(
    () => normalizePublicBase(publicApiUrl, typeof window === 'undefined' ? '' : window.location.origin),
    [publicApiUrl]
  );

  const loadAll = async () => {
    const generation = ++loadGenerationRef.current;
    setLoading(true);
    const loadJSON = async (url) => {
      const controller = new AbortController();
      const timeout = window.setTimeout(() => controller.abort(), LOAD_TIMEOUT_MS);
      try {
        const response = await fetch(url, { headers: getAuthHeaders(), signal: controller.signal, cache: 'no-store' });
        if (!response.ok) throw new Error(`${url}: HTTP ${response.status}`);
        return await response.json();
      } finally {
        window.clearTimeout(timeout);
      }
    };

    const resources = [
      [`${API}/profiles`, setProfiles, []],
      [`${API}/plans`, setPlans, []],
      [`${API}/subscriptions`, setSubscriptions, []],
      [`${API}/nodes`, setNodes, []],
      [INTERNAL_API, setInternalNodes, []],
	  [TUNNEL_API, setManagedTunnels, []],
	  [PREFERRED_API, setPreferredAddresses, []],
      [`${API}/templates`, setTemplates, []],
      [`${API}/logs?limit=200`, setLogs, []],
      [SERVER_INVENTORY_API, (items) => setServers((Array.isArray(items) ? items : []).map(normalizeManagedServer).filter(isLinuxManagedServer)), []],
      [`${API}/settings`, setSettings, {}],
	  [RUNTIME_API, setManagedRuntimes, []],
    ];
    const requests = resources.map(([url, setter, fallback]) => loadJSON(url)
      .then((payload) => {
        if (loadGenerationRef.current === generation) setter(payload.data ?? fallback);
        return true;
      })
      .catch((error) => {
        console.warn(`Subscription resource unavailable: ${url}`, error);
		if (loadGenerationRef.current === generation) setter(fallback);
        return false;
      }));

    // A slow optional resource must never own the whole page's loading state.
    await Promise.race([
      Promise.allSettled([requests[4], requests[7]]),
      new Promise((resolve) => window.setTimeout(resolve, INITIAL_SKELETON_MS)),
    ]);
    if (loadGenerationRef.current === generation) setLoading(false);

    const results = await Promise.all(requests);
    if (loadGenerationRef.current === generation && results.some((success) => !success)) {
      toast.warning(`${results.filter((success) => !success).length} 项数据暂时无法载入，其余内容已显示`);
    }
  };

  useEffect(() => {
    loadAll();
  }, []);

	const runtimeLifecycleServers = useMemo(() => servers.filter((server) => server.status === 'online' && server.agent_capabilities?.proxy_runtime_lifecycle_v2 === true), [servers]);
	useEffect(() => {
		const valid = new Set(runtimeLifecycleServers.map((server) => String(server.id)));
		setSelectedRuntimeHosts((current) => {
			const next = new Set([...current].filter((id) => valid.has(String(id))));
			return next.size === current.size ? current : next;
		});
	}, [runtimeLifecycleServers]);

	useEffect(() => {
	    const entries = [
	      ...Object.entries(tunnelTasks).map(([taskID, value]) => ['tunnel', taskID, value]),
	      ...Object.entries(nodeTasks).map(([taskID, value]) => ['node', taskID, value]),
	    ];
	    if (entries.length === 0) return undefined;
	    const removeTask = (kind, taskID) => {
	      const setter = kind === 'tunnel' ? setTunnelTasks : setNodeTasks;
	      setter((current) => { const next = { ...current }; delete next[taskID]; return next; });
	    };
	    const handleEvent = (kind, taskID, payload, source) => {
	      const data = payload?.data || {};
	      const terminal = payload?.status === 'completed' || payload?.status === 'failed' || payload?.type === 'completed' || payload?.type === 'failed';
	      const setter = kind === 'tunnel' ? setTunnelTasks : setNodeTasks;
	      setter((current) => ({ ...current, [taskID]: payload }));
	      if (payload?.type === 'progress' && !terminal && Number(payload.progress || 0) < 100 && data?.message) toast.info(`${data.message}（${payload.progress || 0}%）`, { isManual: true, timeout: 2500 });
	      if (terminal && !terminalTaskIDsRef.current.has(taskID)) {
	        terminalTaskIDsRef.current.add(taskID);
	        if (payload.status === 'completed' || payload.type === 'completed') {
	          toast.success(typeof data === 'string' ? data : (data?.message || (kind === 'node' ? '节点部署已完成' : 'Tunnel 任务已完成')));
	          loadAll();
	        } else {
	          toast.error(payload.error || '任务失败');
	        }
	        source?.close();
	        removeTask(kind, taskID);
	      }
	    };
    const pollers = [];
    const sources = entries.map(([kind, taskID]) => {
      const source = new EventSource(`/api/server/tasks/${taskID}/stream`);
      source.onmessage = (event) => {
        try { handleEvent(kind, taskID, JSON.parse(event.data), source); } catch { /* wait for the status fallback */ }
      };
      source.onerror = () => {
        source.close();
        const poll = window.setInterval(async () => {
          try {
            const response = await fetch(`/api/server/tasks/${taskID}`, { headers: getAuthHeaders(), cache: 'no-store' });
            if (!response.ok) return;
            const body = await response.json();
            handleEvent(kind, taskID, body.data || body, source);
            if ((body.data || body).status === 'completed' || (body.data || body).status === 'failed') window.clearInterval(poll);
          } catch { /* keep retrying while the task is running */ }
        }, 750);
        pollers.push(poll);
      };
      return source;
    });
    return () => { sources.forEach((source) => source.close()); pollers.forEach((poll) => window.clearInterval(poll)); };
  }, [Object.keys(tunnelTasks).join(','), Object.keys(nodeTasks).join(',')]);

	useEffect(() => {
		if (!tunnelModalOpen) return undefined;
		fetch('/api/cloudflare/accounts', { headers: getAuthHeaders(), cache: 'no-store' }).then((response) => response.json()).then((payload) => {
			const accounts = Array.isArray(payload) ? payload : (payload.data || payload.accounts || []);
			setCloudflareAccounts(accounts);
			setTunnelForm((current) => ({ ...current, account_id: current.account_id || accounts[0]?.id || '' }));
		}).catch(() => setCloudflareAccounts([]));
		return undefined;
	}, [tunnelModalOpen]);

	useEffect(() => {
		if (!tunnelForm.account_id) { setCloudflareZones([]); return undefined; }
		fetch(`/api/cloudflare/accounts/${encodeURIComponent(tunnelForm.account_id)}/zones`, { headers: getAuthHeaders(), cache: 'no-store' }).then((response) => response.json()).then((payload) => {
			const zones = Array.isArray(payload) ? payload : (payload.data || payload.zones || []);
			setCloudflareZones(zones);
			setTunnelForm((current) => ({ ...current, zone_id: current.zone_id || zones[0]?.id || '' }));
		}).catch(() => setCloudflareZones([]));
		return undefined;
	}, [tunnelForm.account_id]);

	useEffect(() => {
		if (!tunnelTargetServer || !tunnelForm.zone_id) return;
		const zone = cloudflareZones.find((item) => String(item.id) === String(tunnelForm.zone_id));
		const zoneName = String(zone?.name || '').trim().toLowerCase().replace(/\.$/, '');
		if (!zoneName) return;
		const generated = `cf-${String(tunnelTargetServer.id).replace(/[^a-z0-9]/gi, '').slice(0, 12) || 'node'}.${zoneName}`;
		setTunnelForm((current) => ({ ...current, hostname: generated }));
	}, [tunnelTargetServer, tunnelForm.zone_id, cloudflareZones]);

  const openTunnelDeployment = (server) => {
		setTunnelTargetServer(server);
		setTunnelModalOpen(true);
	};

	const deployProxyRuntime = async (serverIDs) => {
		const targets = Array.isArray(serverIDs) ? serverIDs : [serverIDs];
		if (targets.length === 0) return;
		setSaving(true);
		try {
			const results = await Promise.allSettled(targets.map(async (serverID) => {
				const response = await fetch(`${RUNTIME_API}/${serverID}/install`, { method: 'POST', headers: getAuthHeaders() });
				const payload = await response.json();
				if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '代理程序部署失败');
				const taskID = payload.data?.task_id;
				if (taskID) setNodeTasks((current) => ({ ...current, [taskID]: { progress: 0, status: 'pending', data: { message: '代理程序部署任务已提交' } } }));
				return payload;
			}));
			const failed = results.filter((item) => item.status === 'rejected');
			if (failed.length) toast.error(`${failed.length} 台提交失败：${failed[0].reason?.message || '未知错误'}`);
			if (results.length > failed.length) toast.info(`已提交 ${results.length - failed.length} 台代理程序部署`, { isManual: true });
			setSelectedRuntimeHosts(new Set());
			await loadAll();
		} finally { setSaving(false); }
	};

	const uninstallProxyRuntime = async (server) => {
		if (!confirmPress(`runtime-uninstall:${server.id}`, `卸载 ${server.name} 的 sing-box`)) return;
		try {
			const response = await fetch(`${RUNTIME_API}/${server.id}/uninstall`, { method: 'POST', headers: getAuthHeaders() });
			const payload = await response.json();
			if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '代理程序卸载失败');
			const taskID = payload.data?.task_id;
			if (taskID) setNodeTasks((current) => ({ ...current, [taskID]: { progress: 0, status: 'pending', data: { message: `${server.name} 卸载任务已提交` } } }));
			toast.info('代理程序卸载任务已提交', { isManual: true });
		} catch (error) { toast.error(error.message || '代理程序卸载失败'); }
	};

  const deployTunnel = async (server = tunnelTargetServer) => {
    if (!tunnelForm.account_id || !tunnelForm.zone_id || !tunnelForm.hostname) {
      toast.warning('请填写 Cloudflare 账号、Zone ID 和 Tunnel 域名');
      return;
    }
    try {
      const response = await fetch(`${TUNNEL_API}/${server.id}/deploy`, { method: 'POST', headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify(tunnelForm) });
      const payload = await response.json();
      if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || 'Tunnel 部署失败');
      const taskID = payload.data?.task_id;
      if (taskID) setTunnelTasks((current) => ({ ...current, [taskID]: { progress: 0, data: { message: '任务已提交' } } }));
      toast.info(`${server.name} 的 Tunnel 部署已提交`, { isManual: true });
		setTunnelModalOpen(false);
    } catch (error) { toast.error(error.message || 'Tunnel 部署失败'); }
  };

	const savePreferredAddress = async () => {
		if (!preferredForm.name.trim() || !preferredForm.address.trim()) return toast.warning('请填写优选地址名称和域名/IP');
		try {
			const response = await fetch(PREFERRED_API, { method: 'POST', headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify(preferredForm) });
			const payload = await response.json();
			if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '保存优选地址失败');
			setPreferredForm({ name: '', address: '', port: 443, enabled: true, is_default: false });
			await loadAll();
			toast.success('优选地址已保存');
		} catch (error) { toast.error(error.message || '保存优选地址失败'); }
	};

	const deletePreferredAddress = async (item) => {
		if (!confirmPress(`preferred-address:${item.id}`, `删除优选地址 ${item.name}`)) return;
		try {
			const response = await fetch(`${PREFERRED_API}/${item.id}`, { method: 'DELETE', headers: getAuthHeaders() });
			const payload = await response.json();
			if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '删除优选地址失败');
			await loadAll();
			toast.success('优选地址已删除');
		} catch (error) { toast.error(error.message || '删除优选地址失败'); }
	};

	const setPreferredDefault = async (item) => {
		try {
			const response = await fetch(`${PREFERRED_API}/${item.id}`, { method: 'PUT', headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify({ name: item.name, address: item.address, port: item.port, enabled: item.enabled !== false, is_default: true, sort_order: item.sort_order || 0 }) });
			const payload = await response.json();
			if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '设为默认失败');
			await loadAll();
			toast.success(`已将 ${item.name} 设为默认`);
		} catch (error) { toast.error(error.message || '设为默认失败'); }
	};

  const uninstallTunnel = async (server) => {
    const entry = managedTunnels.find((item) => item.server_id === server.id);
    if (!entry) return toast.warning('该主机没有 Managed Tunnel');
		const affectedNodes = Number(entry.node_count ?? internalNodes.filter((node) => node.server_id === server.id && node.access_mode === 'cloudflare_tunnel').length);
    if (!confirmPress(`managed-tunnel-delete:${server.id}`, `卸载 ${server.name} 的 Tunnel、DNS、cloudflared 以及 ${affectedNodes} 个关联节点`)) return;
    try {
      const response = await fetch(`${TUNNEL_API}/${server.id}?cascade=1`, { method: 'DELETE', headers: getAuthHeaders() });
      const payload = await response.json();
      if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || 'Tunnel 卸载失败');
      const taskID = payload.data?.task_id;
      if (taskID) setTunnelTasks((current) => ({ ...current, [taskID]: { progress: 0, data: { message: '卸载任务已提交' } } }));
      toast.info('Tunnel 卸载任务已提交', { isManual: true });
    } catch (error) { toast.error(error.message || 'Tunnel 卸载失败'); }
  };

	const waitForTaskTerminal = async (taskID, timeoutMs = 240000) => {
		const deadline = Date.now() + timeoutMs;
		while (Date.now() < deadline) {
			const response = await fetch(`/api/server/tasks/${taskID}`, { headers: getAuthHeaders(), cache: 'no-store' });
			const payload = await response.json();
			if (!response.ok) throw new Error(payload.error || '无法读取任务状态');
			const task = payload.data || payload;
			if (task.status === 'completed') return task;
			if (task.status === 'failed' || task.status === 'cancelled') throw new Error(task.error || '任务执行失败');
			await new Promise((resolve) => window.setTimeout(resolve, 750));
		}
		throw new Error('任务等待超时，请到任务状态查看结果');
	};

  const createInternalNode = async () => {
    const selectedIDs = [...selectedInternalHosts];
    const targetServerIDs = selectedIDs.length > 0 ? selectedIDs : [internalNodeForm.server_id].filter(Boolean);
    if (targetServerIDs.length === 0) {
      toast.warning('请选择目标实例');
      return;
    }
    setSaving(true);
    try {
      const results = await Promise.allSettled(targetServerIDs.map(async (serverID) => {
        const server = servers.find((item) => item.id === serverID);
        const customName = internalNodeForm.name.trim();
        const flag = countryFlagEmoji(getInstanceCountryCode(server));
        const generatedName = `${flag ? `${flag} ` : ''}${server?.name || serverID}`;
        const namedNode = customName
          ? `${flag && !customName.startsWith(flag) ? `${flag} ` : ''}${targetServerIDs.length > 1 ? `${customName}-${server?.name || serverID}` : customName}`
          : generatedName;
        const existingNode = internalNodes.find((node) => (
          node.server_id === serverID
          && node.protocol === internalNodeForm.protocol
          && node.name === namedNode
        ));
        if (existingNode) {
          const res = await fetch(`${INTERNAL_API}/${existingNode.id}/reconcile`, { method: 'POST', headers: getAuthHeaders() });
          const data = await res.json();
          if (!res.ok || data.success === false) throw new Error(`${server?.name || serverID}: ${data.error || data.message || '重新部署失败'}`);
          if (data.data?.task_id) setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${server?.name || serverID} 部署任务已提交` } } }));
          return { ...data, reused: true };
        }
        const payload = {
          ...internalNodeForm,
          server_id: serverID,
          name: namedNode,
          public_host: server?.host || '',
        };
        const res = await fetch(INTERNAL_API, { method: 'POST', headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
        const data = await res.json();
        if (!res.ok || data.success === false) throw new Error(`${servers.find((server) => server.id === serverID)?.name || serverID}: ${data.error || data.message || '部署失败'}`);
        if (data.data?.task_id) setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${server?.name || serverID} 部署任务已提交` } } }));
        return data;
      }));
      const succeeded = results.filter((result) => result.status === 'fulfilled');
      const failed = results.filter((result) => result.status === 'rejected');
      if (succeeded.length > 0) toast.info(`已提交 ${succeeded.length} 个节点生成任务`, { isManual: true });
      if (failed.length > 0) toast.error(`${failed.length} 台部署失败：${failed[0].reason?.message || '未知错误'}`);
      if (succeeded.length > 0) {
        setInternalNodeModalOpen(false);
        setInternalNodeForm(emptyInternalNodeForm);
        setSelectedInternalHosts(new Set());
      }
      await loadAll();
    } catch (error) { toast.error(error.message); } finally { setSaving(false); }
  };

  const withInternalNodeAction = async (nodeID, action, callback) => {
    const actionKey = `${nodeID}:${action}`;
    if (internalNodeActions[actionKey]) return;
    setInternalNodeActions((previous) => ({ ...previous, [actionKey]: true }));
    try {
      await callback();
    } finally {
      setInternalNodeActions((previous) => {
        const next = { ...previous };
        delete next[actionKey];
        return next;
      });
    }
  };

  const reconcileInternalNode = async (node) => {
    await withInternalNodeAction(node.id, 'reconcile', async () => {
      try {
        const res = await fetch(`${INTERNAL_API}/${node.id}/reconcile`, { method: 'POST', headers: getAuthHeaders() });
        const data = await res.json();
        if (!res.ok || data.success === false) throw new Error(data.error || data.message || '重新部署失败');
        if (data.data?.task_id) setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${node.name} 部署任务已提交` } } }));
        else toast.success(`${node.name} 已重新部署`);
        await loadAll();
      } catch (error) {
        toast.error(error.message || '重新部署失败');
      }
    });
  };

  const deleteInternalNode = async (node) => {
    if (!confirmPress(`internal-node-delete:${node.id}`, `卸载节点 ${node.name}`)) return;
    await withInternalNodeAction(node.id, 'delete', async () => {
      try {
		const requestDelete = async (force = false) => {
			const suffix = force ? '?force=1' : '';
			const response = await fetch(`${INTERNAL_API}/${node.id}${suffix}`, { method: 'DELETE', headers: getAuthHeaders() });
			const payload = await response.json().catch(() => ({}));
			return { response, payload };
		};
		let { response, payload } = await requestDelete();
		if ((!response.ok || payload.success === false) && payload.data?.can_force_detach) {
			const confirmed = await dialog.confirm({
				title: '仅从面板移除节点',
				message: `${payload.error}。继续会移除面板记录和套餐关联，但主机恢复连接后仍可能存在残留服务与防火墙规则。`,
				confirmText: '从面板移除',
				cancelText: '保留节点',
				variant: 'destructive',
			});
			if (!confirmed) return;
			({ response, payload } = await requestDelete(true));
		}
		if (!response.ok || payload.success === false) throw new Error(payload.error || payload.message || '卸载失败');
				const taskID = payload.data?.task_id;
				if (taskID) {
					setNodeTasks((current) => ({ ...current, [taskID]: { progress: 0, status: 'pending', data: { message: `${node.name} 卸载任务已提交` } } }));
					toast.info(`${node.name} 卸载任务已提交`, { isManual: true });
				} else toast.success(`${node.name} 已卸载`);
        await loadAll();
      } catch (error) {
        toast.error(error.message || '卸载失败');
      }
    });
  };

  const reconcileInternalNodes = async (managed) => {
    setSaving(true);
    try {
		let completed = 0;
		for (const node of managed) {
        const res = await fetch(`${INTERNAL_API}/${node.id}/reconcile`, { method: 'POST', headers: getAuthHeaders() });
        const data = await res.json();
        if (!res.ok || data.success === false) throw new Error(data.error || data.message || `${node.name} 重新部署失败`);
			if (data.data?.task_id) {
				setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${node.name} 部署任务已提交` } } }));
				await waitForTaskTerminal(data.data.task_id);
			}
			completed += 1;
		}
		toast.success(`已重新部署 ${completed} 个节点`);
      await loadAll();
    } catch (error) {
      toast.error(error.message || '重新部署失败');
    } finally {
      setSaving(false);
    }
  };

  const uninstallInternalNodes = async (server, managed) => {
    if (!confirmPress(`instance-proxy-uninstall:${server.id}`, `卸载 ${server.name} 的 ${managed.length} 个节点`)) return;
    setUninstallingServerId(server.id);
    setSaving(true);
    try {
		let completed = 0;
		for (const node of managed) {
        const res = await fetch(`${INTERNAL_API}/${node.id}`, { method: 'DELETE', headers: getAuthHeaders() });
        const data = await res.json();
        if (!res.ok || data.success === false) throw new Error(data.error || data.message || `${node.name} 卸载失败`);
			const taskID = data.data?.task_id;
			if (taskID) {
				setNodeTasks((current) => ({ ...current, [taskID]: { progress: 0, status: 'pending', data: { message: `${node.name} 卸载任务已提交` } } }));
				await waitForTaskTerminal(taskID);
			}
			completed += 1;
		}
		toast.success(`已卸载 ${completed} 个节点`);
      await loadAll();
    } catch (error) {
      toast.error(error.message || '卸载失败');
    } finally {
      setUninstallingServerId('');
      setSaving(false);
    }
  };

  const toggleInternalNodeEnabled = async (node, enabled) => {
    try {
      const res = await fetch(`${INTERNAL_API}/${node.id}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || data.message || '更新失败');
      if (data.data?.task_id) {
        setNodeTasks((current) => ({ ...current, [data.data.task_id]: { progress: 0, status: 'pending', data: { message: `${node.name} 状态更新任务已提交` } } }));
        toast.info(enabled ? `正在启用 ${node.name}` : `正在停用 ${node.name}`, { isManual: true });
      } else toast.success(enabled ? '内部节点已启用' : '内部节点已停用');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '更新失败');
    }
  };

  const toggleInternalHost = (serverID, checked) => setSelectedInternalHosts((previous) => {
    const next = new Set(previous); if (checked) next.add(serverID); else next.delete(serverID); return next;
  });

  const startInternalDeployment = (serverID = '') => {
    setEditingInternalNodeId(null);
    const nextSelection = serverID ? new Set([serverID]) : new Set(selectedInternalHosts);
    if (serverID) setSelectedInternalHosts(nextSelection);
    const selected = serverID || [...nextSelection][0] || '';
    setInternalNodeForm((prev) => ({ ...emptyInternalNodeForm, server_id: selected, protocol: prev.protocol || 'vless-reality', public_host: servers.find((server) => server.id === selected)?.host || '' }));
    setInternalNodeModalOpen(true);
  };

  const openEditInternalNode = (node) => {
    setEditingInternalNodeId(node.id);
    setSelectedInternalHosts(new Set([node.server_id]));
    setInternalNodeForm({ ...emptyInternalNodeForm, ...node, server_name: node.server_name || 'www.cloudflare.com' });
    setInternalNodeModalOpen(true);
  };

  const saveInternalNode = async () => {
    if (!internalNodeForm.name.trim()) return toast.warning('请输入节点名称');
    setSaving(true);
    try {
      const res = await fetch(`${INTERNAL_API}/${editingInternalNodeId}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ name: internalNodeForm.name.trim(), stable: !!internalNodeForm.stable, preferred_address_id: internalNodeForm.preferred_address_id || '', connect_address: '', connect_port: 0 }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || data.message || '保存失败');
      setInternalNodeModalOpen(false);
      setEditingInternalNodeId(null);
      toast.success('节点配置已更新');
      await loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const templateItems = useMemo(() => templates.map((item) => ({
    value: item.id,
    label: item.valid === false ? `${item.name}（配置错误）` : item.name,
    disabled: item.valid === false,
  })), [templates]);
  const runtimeByServer = useMemo(() => new Map(managedRuntimes.map((item) => [String(item.server_id), item])), [managedRuntimes]);
  const runtimeReadyServers = useMemo(() => servers.filter((server) => runtimeByServer.get(String(server.id))?.apply_status === 'running'), [servers, runtimeByServer]);
	useEffect(() => {
		const valid = new Set(runtimeReadyServers.map((server) => String(server.id)));
		setSelectedInternalHosts((current) => {
			const next = new Set([...current].filter((id) => valid.has(String(id))));
			return next.size === current.size ? current : next;
		});
	}, [runtimeReadyServers]);
  const serverNameById = useMemo(() => {
    const map = new Map();
    servers.forEach((item) => map.set(String(item.id), item.name || item.host || item.id));
    return map;
  }, [servers]);
  const externalNodePool = useMemo(
    () => profiles.find((item) => item.id === DEFAULT_EXTERNAL_POOL_ID) || null,
    [profiles]
  );
  const exportSubscriptions = subscriptions;
  const subscriptionItems = useMemo(() => exportSubscriptions.map((item) => ({ value: item.id, label: item.name })), [exportSubscriptions]);
  const planItems = useMemo(() => plans.map((item) => ({ value: item.id, label: item.enabled ? item.name : `${item.name}（已停用）`, disabled: !item.enabled })), [plans]);
  const firstEnabledPlanID = useMemo(() => plans.find((item) => item.enabled)?.id || '', [plans]);
  const planCandidateNodes = useMemo(() => [
    ...internalNodes.map((node) => ({ ...node, source_group: 'internal', display_type: node.protocol === 'vless-reality' ? 'vless' : node.protocol })),
    ...nodes.map((node) => ({ ...node, source_group: 'external', display_type: String(node.type || 'unknown').toLowerCase() })),
  ], [internalNodes, nodes]);
  const planNodeTypeItems = useMemo(() => [{ value: 'all', label: '全部类型' }, ...Array.from(new Set(planCandidateNodes.map((node) => node.display_type))).sort().map((value) => ({ value, label: value.toUpperCase() }))], [planCandidateNodes]);
  const visiblePlanNodes = useMemo(() => planCandidateNodes.filter((node) => (
    (planNodeTypeFilter === 'all' || node.display_type === planNodeTypeFilter)
    && (planNodeSourceFilter === 'all' || node.source_group === planNodeSourceFilter)
  )), [planCandidateNodes, planNodeSourceFilter, planNodeTypeFilter]);
  const visiblePlanNodeIDs = useMemo(() => visiblePlanNodes.map((node) => node.id), [visiblePlanNodes]);
  const allVisiblePlanNodesSelected = visiblePlanNodeIDs.length > 0 && visiblePlanNodeIDs.every((id) => planForm.node_ids.includes(id));
  const selectedTemplateSubscription = useMemo(
    () => exportSubscriptions.find((item) => item.id === templateSubscriptionId) || null,
    [exportSubscriptions, templateSubscriptionId]
  );
  useEffect(() => {
    if (exportSubscriptions.length > 0 && !exportSubscriptions.some((item) => item.id === templateSubscriptionId)) {
      setTemplateSubscriptionId(exportSubscriptions[0].id);
    }
  }, [exportSubscriptions, templateSubscriptionId]);
  useEffect(() => {
    setTemplateBindingId(
      selectedTemplateSubscription?.template_id
      || settings?.default_template_id
      || 'builtin_mihomo_default'
    );
  }, [selectedTemplateSubscription, settings?.default_template_id]);
  const visibleNodes = useMemo(
    () => nodes,
    [nodes]
  );
  const protocolItems = useMemo(() => {
    const counts = new Map();
    visibleNodes.forEach((node) => {
      const key = String(node.type || 'unknown').toLowerCase();
      counts.set(key, (counts.get(key) || 0) + 1);
    });
    return [
      { value: 'all', label: `全部 (${visibleNodes.length})` },
      ...Array.from(counts.entries())
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([value, count]) => ({ value, label: `${value.toUpperCase()} (${count})` })),
    ];
  }, [visibleNodes]);
  const tagItems = useMemo(() => {
    const counts = new Map();
    visibleNodes.forEach((node) => {
      const tags = String(node.tags || '').split(',').map((item) => item.trim()).filter(Boolean);
      if (tags.length === 0) return;
      tags.forEach((tag) => counts.set(tag, (counts.get(tag) || 0) + 1));
    });
    return [
      { value: 'all', label: `全部 (${visibleNodes.length})` },
      ...Array.from(counts.entries())
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([value, count]) => ({ value, label: `${value} (${count})` })),
    ];
  }, [visibleNodes]);
  const filteredNodes = useMemo(() => (
    visibleNodes.filter((node) => {
      const protocolOK = protocolFilter === 'all' || String(node.type || 'unknown').toLowerCase() === protocolFilter;
      const tagOK = tagFilter === 'all' || String(node.tags || '').split(',').map((item) => item.trim()).includes(tagFilter);
      return protocolOK && tagOK;
    })
  ), [protocolFilter, tagFilter, visibleNodes]);
  useEffect(() => {
    if (!protocolItems.some((item) => item.value === protocolFilter)) {
      setProtocolFilter('all');
    }
  }, [protocolFilter, protocolItems]);

  useEffect(() => {
    if (!tagItems.some((item) => item.value === tagFilter)) {
      setTagFilter('all');
    }
  }, [tagFilter, tagItems]);

  useEffect(() => {
    setTemplateBindingId(selectedTemplateSubscription?.template_id || settings?.default_template_id || 'builtin_mihomo_default');
  }, [selectedTemplateSubscription, settings]);

  const openCreateSubscription = () => {
    const linkIndex = exportSubscriptions.length + 1;
    setEditingSubscriptionId(null);
    setSubscriptionForm({
      ...emptySubscriptionForm,
      plan_id: firstEnabledPlanID,
      name: `订阅 ${linkIndex}`,
      template_id: settings?.default_template_id || 'builtin_mihomo_default',
    });
    setSubscriptionModalOpen(true);
  };

  const openCreatePlan = () => {
    setEditingPlanId(null);
    setPlanForm({ ...emptyPlanForm, node_ids: [] });
    setPlanNodeTypeFilter('all'); setPlanNodeSourceFilter('all');
    setPlanModalOpen(true);
  };

  const openEditPlan = (plan) => {
    setEditingPlanId(plan.id);
    setPlanForm({ ...emptyPlanForm, ...plan, node_ids: Array.isArray(plan.node_ids) ? plan.node_ids : [] });
    setPlanNodeTypeFilter('all'); setPlanNodeSourceFilter('all');
    setPlanModalOpen(true);
  };

  const savePlan = async () => {
    if (!planForm.name.trim()) return toast.warning('请输入套餐名称');
    setSaving(true);
    try {
      const res = await fetch(editingPlanId ? `${API}/plans/${editingPlanId}` : `${API}/plans`, { method: editingPlanId ? 'PUT' : 'POST', headers: getAuthHeaders(), body: JSON.stringify(planForm) });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      setPlanModalOpen(false); await loadAll(); toast.success('套餐已保存');
    } catch (error) { toast.error(error.message); } finally { setSaving(false); }
  };

  const deletePlan = async (plan) => {
    if (!confirmPress(`plan-delete:${plan.id}`, `删除套餐「${plan.name}」`)) return;
    const res = await fetch(`${API}/plans/${plan.id}`, { method: 'DELETE', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) return toast.error(data.error || '删除失败');
    await loadAll(); toast.success('套餐已删除');
  };

  const togglePlanEnabled = async (plan, enabled) => {
    try {
      const res = await fetch(`${API}/plans/${plan.id}`, { method: 'PATCH', headers: getAuthHeaders(), body: JSON.stringify({ enabled }) });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || data.success === false) throw new Error(data.error || '套餐状态更新失败');
      await loadAll();
      toast.success(enabled ? '套餐已启用，对应订阅已恢复' : '套餐已停用，对应订阅已失效');
    } catch (error) { toast.error(error.message || '套餐状态更新失败'); }
  };

  const openEditSubscription = (sub) => {
    setEditingSubscriptionId(sub.id);
    setSubscriptionForm({
      ...emptySubscriptionForm,
      plan_id: sub.plan_id || '',
      name: sub.name || '',
      remark: sub.remark || '',
      enabled: sub.enabled !== false,
      template_id: sub.template_id || settings?.default_template_id || 'builtin_mihomo_default',
    });
    setSubscriptionModalOpen(true);
  };

  const saveSubscription = async () => {
    if (!subscriptionForm.name.trim()) {
      toast.warning('请输入名称');
      return;
    }
    setSaving(true);
    try {
      const res = await fetch(editingSubscriptionId ? `${API}/subscriptions/${editingSubscriptionId}` : `${API}/subscriptions`, {
        method: editingSubscriptionId ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(subscriptionForm),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      if (!editingSubscriptionId && data.data?.public_token) {
        await copyText(subscriptionURL(publicBase, data.data), '订阅链接已创建并复制');
      } else {
        toast.success(editingSubscriptionId ? '订阅链接已更新' : '订阅链接已创建');
      }
      setSubscriptionModalOpen(false);
      loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const deleteSubscription = async (sub) => {
    if (!confirmPress(`subscription-delete:${sub.id}`, `删除订阅链接「${sub.name}」`)) return;
    const res = await fetch(`${API}/subscriptions/${sub.id}`, { method: 'DELETE', headers: getAuthHeaders() });
    const data = await res.json().catch(() => ({}));
    if (!res.ok || data.success === false) {
      toast.error(data.error || '删除失败');
      return;
    }
    toast.success('订阅链接已删除');
    loadAll();
  };

  const toggleSubscriptionEnabled = async (sub, enabled) => {
    try {
      const res = await fetch(`${API}/subscriptions/${sub.id}`, { method: 'PATCH', headers: getAuthHeaders(), body: JSON.stringify({ enabled }) });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || data.success === false) throw new Error(data.error || '订阅状态更新失败');
      await loadAll();
      toast.success(enabled ? '订阅已启用' : '订阅已停用');
    } catch (error) { toast.error(error.message || '订阅状态更新失败'); }
  };

  const resetToken = async (sub) => {
    const confirmed = await dialog.confirm({
      title: '重置连接凭据',
      message: `确定要重置「${sub.name}」的连接凭据吗？旧链接、VLESS UUID 和 HY2 密码都会失效，已下载配置会在 Agent 同步后断开。`,
      confirmText: '重置并同步',
      confirmClass: 'text-kumo-warning',
    });
    if (!confirmed) return;
    const res = await fetch(`${API}/subscriptions/${sub.id}/reset-token`, { method: 'POST', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '重置失败');
      return;
    }
    const queued = Number(data.data?.nodes_queued || 0);
    toast.success(queued > 0 ? `连接凭据已重置，正在同步 ${queued} 个节点` : '连接凭据已重置');
    loadAll();
  };

  const rotateAddress = async (sub) => {
    const confirmed = await dialog.confirm({
      title: '更换订阅地址',
      message: `确定要更换「${sub.name}」的订阅链接吗？旧链接立即失效，VLESS UUID 和 HY2 密码保持不变，已配置的客户端不会断开。`,
      confirmText: '更换订阅地址',
    });
    if (!confirmed) return;
    const res = await fetch(`${API}/subscriptions/${sub.id}/rotate-address`, { method: 'POST', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '更换失败');
      return;
    }
    toast.success('订阅地址已更换');
    loadAll();
  };

  const refreshProfileUpstream = async (profile) => {
    const res = await fetch(`${API}/profiles/${profile.id}/refresh-upstream`, { method: 'POST', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '刷新失败');
      return;
    }
    toast.success('节点来源已刷新');
    loadAll();
  };

  const openImportModal = () => {
    setImportText('');
    setImportSourceURL('');
    setImportPreview([]);
    setImportModalOpen(true);
  };

  const previewImport = async () => {
    if (!importSourceURL.trim() && !importText.trim()) {
      toast.warning('请填写原订阅 URL 或粘贴订阅内容');
      return;
    }
    const res = await fetch(`${API}/import/preview`, {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({ text: importText, source_url: importSourceURL }),
    });
    const data = await res.json();
    setImportPreview(data.data || []);
  };

  const commitImport = async (replace = false) => {
    if (!importSourceURL.trim() && !importText.trim()) {
      toast.warning('请填写原订阅 URL 或粘贴订阅内容');
      return;
    }
    const res = await fetch(`${API}/import/commit`, {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({ subscription_id: DEFAULT_EXTERNAL_POOL_ID, text: importText, source_url: importSourceURL, replace }),
    });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '导入失败');
      return;
    }
    toast.success(`已接管 ${data.data?.imported || 0} 个节点`);
    setImportModalOpen(false);
    loadAll();
  };

  const openEditNode = (node) => {
    setEditingNodeId(node.id);
    setNodeForm({
      ...emptyNodeForm,
      ...node,
      port: node.port || 0,
      sort_order: node.sort_order || 0,
    });
    setNodeModalOpen(true);
  };

  const saveNode = async () => {
    if (!editingNodeId) return;
    if (!nodeForm.name.trim()) {
      toast.warning('请输入节点名称');
      return;
    }
    setSaving(true);
    try {
      const res = await fetch(`${API}/nodes/${editingNodeId}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          ...nodeForm,
          port: Number(nodeForm.port) || 0,
          sort_order: Number(nodeForm.sort_order) || 0,
        }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      toast.success('节点已更新');
      setNodeModalOpen(false);
      loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const toggleNodeEnabled = async (node, enabled) => {
    try {
      const res = await fetch(`${API}/nodes/${node.id}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          ...node,
          enabled,
          port: Number(node.port) || 0,
          sort_order: Number(node.sort_order) || 0,
        }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '更新失败');
      toast.success(enabled ? '节点已启用' : '节点已停用');
      loadAll();
    } catch (error) {
      toast.error(error.message || '更新失败');
    }
  };

  const deleteNode = async (node) => {
    if (!confirmPress(`external-node-delete:${node.id}`, `删除节点 ${node.name}`)) return;
    const actionKey = `${node.id}:delete`;
    if (externalNodeActions[actionKey]) return;
    setExternalNodeActions((current) => ({ ...current, [actionKey]: true }));
    try {
      const res = await fetch(`${API}/nodes/${node.id}`, { method: 'DELETE', headers: getAuthHeaders() });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || data.success === false) throw new Error(data.error || '删除失败');
      toast.success(`${node.name} 已删除`);
      await loadAll();
    } catch (error) {
      toast.error(error.message || '删除失败');
    } finally {
      setExternalNodeActions((current) => {
        const next = { ...current };
        delete next[actionKey];
        return next;
      });
    }
  };

  const openCreateTemplate = () => {
    setEditingTemplateId(null);
    setTemplateForm(emptyTemplateForm);
    setTemplateModalOpen(true);
  };

  const openEditTemplate = (tpl) => {
    setEditingTemplateId(tpl.id);
    setTemplateForm({ name: tpl.name, format: tpl.format, content: tpl.content, description: tpl.description || '' });
    setTemplateModalOpen(true);
  };

  const openCloneTemplate = (tpl) => {
    setEditingTemplateId(null);
    setTemplateForm({
      name: `${tpl.name}（自定义）`,
      format: tpl.format,
      content: tpl.content,
      description: `基于 ${tpl.name} 的自定义模板`,
    });
    setTemplateModalOpen(true);
  };

  const saveTemplate = async () => {
    setSaving(true);
    try {
      const res = await fetch(editingTemplateId ? `${API}/templates/${editingTemplateId}` : `${API}/templates`, {
        method: editingTemplateId ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(templateForm),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      toast.success('模板已保存');
      setTemplateModalOpen(false);
      loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const setDefaultTemplate = async (tpl) => {
    const res = await fetch(`${API}/templates/${tpl.id}/default`, { method: 'POST', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '设置失败');
      return;
    }
    toast.success('默认模板已更新');
    loadAll();
  };

  const deleteTemplate = async (tpl) => {
    if (tpl.builtin) {
      toast.warning('内置模板不能删除');
      return;
    }
    if (!confirmPress(`template-delete:${tpl.id}`, `删除模板「${tpl.name}」`)) return;
    const res = await fetch(`${API}/templates/${tpl.id}`, { method: 'DELETE', headers: getAuthHeaders() });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '删除失败');
      return;
    }
    toast.success('模板已删除');
    loadAll();
  };

  const saveTemplateBinding = async () => {
    if (!selectedTemplateSubscription) {
      toast.warning('请选择对外订阅');
      return;
    }
    setSaving(true);
    try {
      const res = await fetch(`${API}/subscriptions/${selectedTemplateSubscription.id}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          ...selectedTemplateSubscription,
          template_id: templateBindingId || settings?.default_template_id || 'builtin_mihomo_default',
        }),
      });
      const data = await res.json();
      if (!res.ok || data.success === false) throw new Error(data.error || '保存失败');
      toast.success('转换模板已更新');
      loadAll();
    } catch (error) {
      toast.error(error.message || '保存失败');
    } finally {
      setSaving(false);
    }
  };

  const saveSettings = async () => {
    const res = await fetch(`${API}/settings`, {
      method: 'PUT',
      headers: getAuthHeaders(),
      body: JSON.stringify(settings),
    });
    const data = await res.json();
    if (!res.ok || data.success === false) {
      toast.error(data.error || '保存失败');
      return;
    }
    toast.success('设置已保存');
    loadAll();
  };

  const exportBackup = () => {
    fetch(`${API}/export`, { headers: getAuthHeaders() })
      .then((res) => res.json())
      .then((payload) => {
        if (!payload.success) throw new Error(payload.error || '导出失败');
        const blob = new Blob([JSON.stringify(payload.data, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `subscriptions_export_${new Date().toISOString().slice(0, 10)}.json`;
        link.click();
        URL.revokeObjectURL(url);
      })
      .catch((error) => toast.error(error.message || '导出失败'));
  };

  const renderSubscriptions = () => {
    const currentSubscriptions = exportSubscriptions;
    return (
      <SectionCard
        title={`订阅管理 (${currentSubscriptions.length})`}
        bodyPadding="none"
        actions={(
          <div className="flex min-w-0 flex-wrap items-center justify-end gap-2">
            <span className="hidden rounded border border-kumo-info/20 bg-kumo-info/10 px-1.5 py-0.5 text-[11px] font-semibold text-kumo-info cq-sm:inline-flex">{visibleNodes.length} 个节点</span>
            <span className="hidden rounded border border-kumo-badge-purple/20 bg-kumo-badge-purple/10 px-1.5 py-0.5 text-[11px] font-semibold text-kumo-badge-purple cq-sm:inline-flex">{currentSubscriptions.length} 个订阅</span>
            <Button size="sm" variant="primary" onClick={() => openCreateSubscription()} disabled={!firstEnabledPlanID}><Plus className="h-3.5 w-3.5" />生成订阅</Button>
          </div>
        )}
      >
        <DataTableFrame variant="embedded">
          <AppTable tableId="subscriptions" columns={SUBSCRIPTION_COLUMNS}>
            <Table.Header sticky variant="compact">
              <Table.Row>
                <Table.Head className="text-center">启用</Table.Head>
                <Table.Head>订阅链接</Table.Head>
                <Table.Head className="text-center">状态</Table.Head>
                <Table.Head>流量</Table.Head>
                <Table.Head>访问</Table.Head>
                <Table.Head className="app-table-action">操作</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {currentSubscriptions.map((sub) => {
                const [label, variant] = statusLabel(sub);
                const used = (sub.traffic?.upload || 0) + (sub.traffic?.download || 0);
                const meteringAvailable = sub.traffic?.metering_status === 'available';
				const meteringStatus = sub.traffic?.metering_status || 'pending';
                const link = subscriptionURL(publicBase, sub);
                return (
                  <Table.Row key={sub.id} onDoubleClick={() => openEditSubscription(sub)} className="cursor-pointer">
                    <Table.Cell className="text-center">
                      <Switch size="sm" aria-label={sub.enabled ? `停用订阅 ${sub.name}` : `启用订阅 ${sub.name}`} checked={!!sub.enabled} onCheckedChange={(checked) => toggleSubscriptionEnabled(sub, checked)} />
                    </Table.Cell>
                    <Table.Cell>
                      <div className="truncate text-sm font-semibold text-kumo-strong">{sub.name}</div>
                      <div className="mt-1 flex min-w-0 flex-wrap items-center gap-1">
                        <span className="rounded border border-kumo-info/20 bg-kumo-info/10 px-1.5 py-0.5 text-[10px] font-semibold text-kumo-info">{sub.node_count || 0} 个节点</span>
						<Badge variant="neutral">{plans.find((plan) => plan.id === sub.plan_id)?.name || '未绑定套餐'}</Badge>
                      </div>
                    </Table.Cell>
                    <Table.Cell className="text-center">
                      <Badge variant={variant} appearance="dot">{label}</Badge>
                    </Table.Cell>
                    <Table.Cell>
                      <Meter
                        label="已用流量"
                        value={meteringAvailable ? Math.min(100, sub.traffic?.percent || 0) : 0}
						customValue={meteringAvailable ? `${formatBytes(used)} / ${sub.traffic?.total ? formatBytes(sub.traffic.total) : '无限制'}` : meteringLabel(meteringStatus)}
                      />
                      <div className="mt-1 text-[10px] text-kumo-subtle">
                        {sub.traffic?.cycle_end ? `下次重置 ${formatTime(sub.traffic.cycle_end)}` : '不自动重置'}
                      </div>
                    </Table.Cell>
                    <Table.Cell>
                      <div className="text-xs font-semibold text-kumo-strong">{sub.access_count_today || 0} 次</div>
                      <div className="mt-1 text-[11px] text-kumo-subtle">{formatTime(sub.last_access_at)}</div>
                    </Table.Cell>
                    <Table.Cell className="text-center">
                      <div className="inline-flex items-center justify-center gap-2">
                        <DropdownMenu>
                          <DropdownMenu.Trigger
                            render={<Button size="sm" shape="square" variant="secondary" aria-label="复制订阅链接" title="复制订阅链接" icon={<Copy className="h-3.5 w-3.5" />} />}
                          />
                          <DropdownMenu.Content side="bottom" align="end" sideOffset={6} className="min-w-52">
                            <DropdownMenu.Item onClick={() => copyText(link, '自适应订阅链接已复制')}>
                              自适应订阅（按客户端自动识别）
                            </DropdownMenu.Item>
                            <DropdownMenu.Item onClick={() => copyText(subscriptionURL(publicBase, sub, 'clash'), 'Mihomo / Clash 订阅链接已复制')}>
                              Mihomo / Clash（YAML）
                            </DropdownMenu.Item>
                            <DropdownMenu.Item onClick={() => copyText(subscriptionURL(publicBase, sub, 'base64'), 'Base64 订阅链接已复制')}>
                              sing-box 官方 / v2rayN（Base64）
                            </DropdownMenu.Item>
                            <DropdownMenu.Item onClick={() => copyText(subscriptionURL(publicBase, sub, 'raw'), 'Raw 订阅链接已复制')}>
                              通用节点链接（Raw）
                            </DropdownMenu.Item>
                            <DropdownMenu.Item onClick={() => copyText(subscriptionURL(publicBase, sub, 'info'), '订阅信息页链接已复制')}>
                              订阅信息页（浏览器打开）
                            </DropdownMenu.Item>
                          </DropdownMenu.Content>
                        </DropdownMenu>
                        <Button size="sm" shape="square" variant="secondary" aria-label="编辑订阅链接" title="编辑订阅链接" onClick={() => openEditSubscription(sub)} icon={<Edit className="h-3.5 w-3.5" />} />
                        <DropdownMenu>
                          <DropdownMenu.Trigger
                            render={<Button size="sm" shape="square" variant="secondary" aria-label="订阅安全操作" title="订阅安全操作" icon={<RefreshCw className="h-3.5 w-3.5" />} />}
                          />
                          <DropdownMenu.Content side="bottom" align="end" sideOffset={6} className="min-w-56">
                            <DropdownMenu.Item onClick={() => rotateAddress(sub)}>
                              更换订阅地址（凭据不变）
                            </DropdownMenu.Item>
                            <DropdownMenu.Item onClick={() => resetToken(sub)} variant="danger">
                              重置连接凭据（UUID / 密码）
                            </DropdownMenu.Item>
                          </DropdownMenu.Content>
                        </DropdownMenu>
                        <Button size="sm" shape="square" variant={isArmed(`subscription-delete:${sub.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除订阅链接" title="删除订阅链接" onClick={() => deleteSubscription(sub)} icon={<Trash className="h-3.5 w-3.5" />} />
                      </div>
                    </Table.Cell>
                  </Table.Row>
                );
              })}
              {currentSubscriptions.length === 0 && (
                <Table.Row><Table.Cell colSpan={6} className="p-8 text-center text-kumo-subtle">暂无订阅</Table.Cell></Table.Row>
              )}
            </Table.Body>
          </AppTable>
        </DataTableFrame>
      </SectionCard>
    );
  };

  const renderPlans = () => (
    <SectionCard title={`套餐管理 (${plans.length})`} bodyPadding="none" actions={<Button size="sm" variant="primary" onClick={openCreatePlan}><Plus className="h-3.5 w-3.5" />新建套餐</Button>}>
      <DataTableFrame variant="embedded">
        <AppTable tableId="subscription-plans" columns={PLAN_COLUMNS}>
          <Table.Header sticky variant="compact"><Table.Row><Table.Head className="text-center">启用</Table.Head><Table.Head>套餐</Table.Head><Table.Head className="text-center">状态</Table.Head><Table.Head>单订阅额度</Table.Head><Table.Head className="text-center">重置</Table.Head><Table.Head>节点范围</Table.Head><Table.Head className="text-center">订阅</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
          <Table.Body>
            {plans.map((plan) => {
			  const externalCount = plan.selection_mode === 'all' ? (plan.include_external_nodes ? nodes.length : 0) : (plan.node_ids || []).filter((id) => nodes.some((node) => node.id === id)).length;
			  const internalCount = plan.selection_mode === 'all' ? (plan.include_internal_nodes ? internalNodes.length : 0) : (plan.node_ids || []).filter((id) => internalNodes.some((node) => node.id === id)).length;
			  return <Table.Row key={plan.id} onDoubleClick={() => openEditPlan(plan)} className="cursor-pointer">
              <Table.Cell className="text-center"><Switch size="sm" aria-label={plan.enabled ? `停用套餐 ${plan.name}` : `启用套餐 ${plan.name}`} checked={!!plan.enabled} onCheckedChange={(checked) => togglePlanEnabled(plan, checked)} /></Table.Cell>
              <Table.Cell><div className="font-semibold text-kumo-strong">{plan.name}</div>{plan.remark ? <div className="mt-1 truncate text-[11px] text-kumo-subtle">{plan.remark}</div> : null}</Table.Cell>
              <Table.Cell className="text-center"><Badge variant={plan.enabled ? 'success' : 'neutral'} appearance="dot">{plan.enabled ? '启用' : '停用'}</Badge></Table.Cell>
			  <Table.Cell><div>{plan.total_bytes > 0 ? formatBytes(plan.total_bytes) : '不限'}</div></Table.Cell>
              <Table.Cell className="text-center">{plan.cycle_type === 'monthly' ? `每月 ${plan.cycle_day} 日` : plan.cycle_type === 'custom' ? '自定义' : '不重置'}</Table.Cell>
				<Table.Cell><div className="text-xs font-semibold text-kumo-strong">内部 {internalCount} · 外部 {externalCount}</div></Table.Cell>
              <Table.Cell className="text-center">{plan.subscription_count || 0}</Table.Cell>
              <Table.Cell className="text-center"><div className="inline-flex justify-center gap-2"><Button size="sm" shape="square" variant="secondary" onClick={() => openEditPlan(plan)} icon={<Edit className="h-3.5 w-3.5" />} aria-label="编辑套餐" /><Button size="sm" shape="square" variant={isArmed(`plan-delete:${plan.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deletePlan(plan)} icon={<Trash className="h-3.5 w-3.5" />} aria-label="删除套餐" /></div></Table.Cell>
			</Table.Row>;})}
            {plans.length === 0 && <Table.Row><Table.Cell colSpan={8} className="p-8 text-center text-kumo-subtle">暂无套餐。套餐统一定义节点范围、额度和重置规则。</Table.Cell></Table.Row>}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
  );

  const renderNodes = () => (
    <div className="grid gap-3">
    <SectionCard
      title={`本机节点 (${internalNodes.length})`}
      className="min-h-0"
      bodyPadding="none"
      bodyClassName="min-h-0"
      actions={<Button size="sm" variant="primary" disabled={runtimeReadyServers.length === 0} onClick={() => startInternalDeployment()}><Plus className="h-3.5 w-3.5" />生成节点</Button>}
    >
      <DataTableFrame variant="embedded">
        <AppTable tableId="internal-proxy-nodes" columns={NODE_COLUMNS}>
          <Table.Header sticky variant="compact"><Table.Row><Table.Head className="text-center">状态</Table.Head><Table.Head>节点名称</Table.Head><Table.Head className="text-center">类型</Table.Head><Table.Head>连接</Table.Head><Table.Head>主机 / 延迟</Table.Head><Table.Head className="app-table-action">操作</Table.Head></Table.Row></Table.Header>
          <Table.Body>
            {internalNodes.map((node) => {
              const server = servers.find((item) => item.id === node.server_id);
              const protocol = node.protocol === 'vless-reality' ? 'vless' : node.protocol;
              const isTunnelNode = node.access_mode === 'cloudflare_tunnel';
              const preferredAddress = preferredAddresses.find((item) => item.id === node.preferred_address_id && item.enabled !== false)
                || preferredAddresses.find((item) => item.is_default && item.enabled !== false);
              const displayHost = isTunnelNode
                ? (node.connect_address || preferredAddress?.address || node.tunnel_hostname || node.public_host || '-')
                : (node.public_host || '-');
              const displayPort = isTunnelNode
                ? (node.connect_port || preferredAddress?.port || 443)
                : (node.assigned_port || '-');
              const connectionTags = isTunnelNode
                ? ['ws', 'tls', 'tunnel', node.runtime].filter(Boolean)
                : [node.transport, node.protocol === 'vless-reality' ? 'reality' : (node.protocol === 'hysteria2' ? 'tls' : null), node.runtime].filter(Boolean);
              const reconciling = !!internalNodeActions[`${node.id}:reconcile`];
              const deleting = !!internalNodeActions[`${node.id}:delete`];
              const deleteConfirmKey = `internal-node-delete:${node.id}`;
              const confirmingDelete = isArmed(deleteConfirmKey);
              return <Table.Row key={node.id} onDoubleClick={() => openEditInternalNode(node)} className="cursor-pointer">
                <Table.Cell className="text-center"><Switch size="sm" aria-label={node.enabled ? '停用内部节点' : '启用内部节点'} checked={!!node.enabled} onCheckedChange={(checked) => toggleInternalNodeEnabled(node, checked)} /></Table.Cell>
                <Table.Cell><div className="flex min-w-0 items-center gap-1.5 truncate text-sm font-bold text-kumo-strong">{node.stable && <Star className="h-3.5 w-3.5 shrink-0 text-kumo-warning" />}{node.name}{node.stable && <Badge variant="success">稳定</Badge>}</div>{!node.publishable && (() => { const [stateLabel, stateVariant] = managedNodeState(node); return <div className="mt-1"><Badge variant={stateVariant}>{stateLabel}</Badge></div>; })()}</Table.Cell>
                <Table.Cell className="text-center"><Badge variant={nodeTypeBadgeVariant(protocol)} className="uppercase">{node.protocol === 'vless-reality' ? 'VLESS' : node.protocol === 'hysteria2' ? 'HYSTERIA2' : node.protocol === 'socks' ? 'SOCKS5' : node.protocol === 'http' ? 'HTTP' : String(node.protocol || 'UNKNOWN').toUpperCase()}</Badge></Table.Cell>
                <Table.Cell><div className="truncate font-mono text-xs text-kumo-strong">{displayHost}:{displayPort}</div><div className="mt-1 flex min-w-0 flex-nowrap items-center gap-1 overflow-x-auto scrollbar-none">{connectionTags.map((tag) => <span key={tag} className={`${tag === node.runtime ? 'hidden cq-sm:inline-flex' : 'inline-flex'} shrink-0 rounded-[3px] border px-1.5 py-0.5 font-mono text-[10px] leading-4 ${nodeNetworkTagClass({ key: tag === 'tls' ? 'tls' : 'network', tone: tag })}`}>{tag}</span>)}</div></Table.Cell>
                <Table.Cell>{server?.status === 'online' ? <NodeHostQuality node={{ ...node, traffic_server_id: node.server_id }} serverNameById={serverNameById} /> : <div className="flex min-w-0 flex-col items-start gap-1"><span className="inline-flex max-w-full rounded-[3px] border border-kumo-info/25 bg-kumo-info/10 px-1.5 py-0.5 text-[10px] font-semibold leading-4 text-kumo-info"><span className="truncate">{server?.name || node.server_name || node.server_id}</span></span><span className={`inline-flex rounded-[3px] border px-1.5 py-0.5 text-[10px] font-semibold leading-4 ${latencyChipClass(0)}`}>主机离线</span></div>}</Table.Cell>
                <Table.Cell className="text-center"><div className="inline-flex items-center justify-center gap-2"><Button size="sm" shape="square" variant="secondary" aria-label={`编辑 ${node.name}`} title={`编辑 ${node.name}`} disabled={reconciling || deleting} onClick={(event) => { event.stopPropagation(); openEditInternalNode(node); }} icon={<Edit className="h-3.5 w-3.5" />} /><RefreshButton size="sm" variant="secondary" loading={reconciling} aria-label={`重新部署 ${node.name}`} title={`重新部署 ${node.name}`} disabled={reconciling || deleting} onClick={(event) => { event.stopPropagation(); reconcileInternalNode(node); }} /><Button size="sm" shape="square" variant={confirmingDelete ? 'destructive' : 'secondary-destructive'} aria-label={confirmingDelete ? `再次确认卸载 ${node.name}` : `卸载 ${node.name}`} title={confirmingDelete ? '再次点击确认卸载' : `卸载 ${node.name}`} disabled={reconciling} loading={deleting} onClick={(event) => { event.stopPropagation(); deleteInternalNode(node); }} icon={<Trash className="h-3.5 w-3.5" />} /></div></Table.Cell>
              </Table.Row>;
            })}
            {internalNodes.length === 0 && <Table.Row><Table.Cell colSpan={6} className="p-6 text-center text-kumo-subtle">暂无本机节点</Table.Cell></Table.Row>}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
    <SectionCard
      title={(
        <div className="flex flex-wrap items-center gap-2 cq-sm:gap-3">
          <span>节点列表 ({filteredNodes.length})</span>
          <Tabs
            {...TOOL_TABS_PROPS}
            value={protocolFilter}
            onValueChange={(value) => setProtocolFilter(String(value))}
            tabs={protocolItems}
            className="min-w-max shrink-0"
            listClassName="max-w-full overflow-x-auto whitespace-nowrap scrollbar-thin"
          />
          {tagItems.length > 1 && (
            <Select
              size="sm"
              aria-label="标签筛选"
              value={tagFilter}
              onValueChange={(value) => setTagFilter(String(value))}
              items={tagItems}
              className="w-36 shrink-0"
            />
          )}
        </div>
      )}
      className="min-h-0"
      bodyPadding="none"
      bodyClassName="min-h-0"
      actions={<Button size="sm" variant="primary" onClick={() => openImportModal()} aria-label="导入外部节点" title="导入外部节点"><Download className="h-3.5 w-3.5" />导入外部节点</Button>}
    >
      <DataTableFrame variant="embedded">
        <AppTable tableId="external-proxy-nodes" columns={NODE_COLUMNS}>
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.Head className="text-center">状态</Table.Head>
              <Table.Head>节点名称</Table.Head>
              <Table.Head className="text-center">类型</Table.Head>
              <Table.Head>连接</Table.Head>
              <Table.Head>主机 / 延迟</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {filteredNodes.map((node) => {
              const networkTags = nodeNetworkTags(node);
              const deleting = !!externalNodeActions[`${node.id}:delete`];
              const deleteConfirmKey = `external-node-delete:${node.id}`;
              const confirmingDelete = isArmed(deleteConfirmKey);
              return (
                <Table.Row key={node.id} onDoubleClick={() => openEditNode(node)} className="cursor-pointer">
                  <Table.Cell className="text-center">
                    <Switch
                      size="sm"
                      aria-label={node.enabled ? '停用节点' : '启用节点'}
                      checked={!!node.enabled}
                      onCheckedChange={(checked) => toggleNodeEnabled(node, checked)}
                    />
                  </Table.Cell>
                  <Table.Cell>
                    <div className="flex min-w-0 items-center gap-1.5 truncate text-sm font-bold text-kumo-strong">
                      <NodeFlag node={node} />
                      {node.stable && <Star className="h-3.5 w-3.5 shrink-0 text-kumo-warning" />}
                      <span className="truncate">{node.name}</span>
                      {node.stable && <Badge variant="success">稳定</Badge>}
                    </div>
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <Badge variant={nodeTypeBadgeVariant(node.type)} className="uppercase">{node.type || '-'}</Badge>
                  </Table.Cell>
                  <Table.Cell>
                    <div className="truncate font-mono text-xs text-kumo-strong">{nodeEndpoint(node)}</div>
                    <div className="mt-1 flex min-w-0 flex-nowrap items-center gap-1 overflow-x-auto scrollbar-none">
                      {networkTags.map((tag) => (
                        <span
                          key={tag.key}
                          className={`${['fingerprint', 'path', 'alpn'].includes(tag.key) ? 'hidden cq-sm:inline-flex' : 'inline-flex'} shrink-0 min-w-0 max-w-full truncate rounded-[3px] border px-1.5 py-0.5 font-mono text-[10px] leading-4 ${nodeNetworkTagClass(tag)}`}
                          title={tag.label}
                        >
                          {tag.label}
                        </span>
                      ))}
                      {networkTags.length === 0 && <span className="font-mono text-[11px] text-kumo-subtle">-</span>}
                    </div>
                  </Table.Cell>
                  <Table.Cell>
                    <NodeHostQuality node={node} serverNameById={serverNameById} />
                  </Table.Cell>
                  <Table.Cell className="text-center">
                    <div className="inline-flex items-center justify-center gap-2">
                      <Button size="sm" shape="square" variant="secondary" aria-label="编辑节点" title="编辑节点" onClick={() => openEditNode(node)} icon={<Edit className="h-3.5 w-3.5" />} />
                      <Button
                        size="sm"
                        shape="square"
                        variant={confirmingDelete ? 'destructive' : 'secondary-destructive'}
                        aria-label={confirmingDelete ? `再次确认删除 ${node.name}` : `删除 ${node.name}`}
                        title={confirmingDelete ? '再次点击确认删除' : `删除 ${node.name}`}
                        loading={deleting}
                        onClick={(event) => {
                          event.stopPropagation();
                          deleteNode(node);
                        }}
                        icon={<Trash className="h-3.5 w-3.5" />}
                      />
                    </div>
                  </Table.Cell>
                </Table.Row>
              );
            })}
            {filteredNodes.length === 0 && (
              <Table.Row><Table.Cell colSpan={6} className="p-8 text-center text-kumo-subtle">{visibleNodes.length === 0 ? '暂无节点。' : '没有符合筛选条件的节点。'}</Table.Cell></Table.Row>
            )}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard></div>
  );

  const renderInstanceManagement = () => (
    <SectionCard
      title={`Linux 主机 (${servers.length})`}
      bodyPadding="none"
      actions={<Button size="sm" variant="primary" loading={saving} disabled={selectedRuntimeHosts.size === 0} onClick={() => deployProxyRuntime([...selectedRuntimeHosts])}><Plus className="h-3.5 w-3.5" />批量部署程序 ({selectedRuntimeHosts.size})</Button>}
    >
      <DataTableFrame variant="embedded">
        <AppTable tableId="runtime-hosts" columns={RUNTIME_HOST_COLUMNS} className="text-xs [&_td]:border-kumo-interact/45 [&_th]:border-kumo-interact/50">
          <Table.Header sticky variant="compact">
            <Table.Row>
              <Table.CheckHead checked={runtimeLifecycleServers.length > 0 && selectedRuntimeHosts.size === runtimeLifecycleServers.length} indeterminate={selectedRuntimeHosts.size > 0 && selectedRuntimeHosts.size < runtimeLifecycleServers.length} onCheckedChange={(checked) => setSelectedRuntimeHosts(checked ? new Set(runtimeLifecycleServers.map((server) => server.id)) : new Set())} />
              <Table.Head className="text-center">状态</Table.Head>
              <Table.Head className="text-left">名称</Table.Head>
              <Table.Head className="text-center">位置</Table.Head>
              <Table.Head className="text-center">在线</Table.Head>
              <Table.Head className="text-center">Agent 版本</Table.Head>
              <Table.Head className="text-center">代理服务</Table.Head>
              <Table.Head className="text-center">节点类型</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {servers.map((server) => {
              const managed = internalNodes.filter((node) => node.server_id === server.id);
              const runtime = runtimeByServer.get(String(server.id));
              const tunnel = managedTunnels.find((item) => item.server_id === server.id);
              const countryCode = getInstanceCountryCode(server);
              const locationLabel = getInstanceLocationLabel(server);
              const supportsRuntimeLifecycle = server.agent_capabilities?.proxy_runtime_lifecycle_v2 === true;
              return (
                <Table.Row key={server.id}>
                  <Table.CheckCell disabled={!supportsRuntimeLifecycle || server.status !== 'online'} checked={selectedRuntimeHosts.has(server.id)} onCheckedChange={(checked) => setSelectedRuntimeHosts((previous) => { const next = new Set(previous); if (checked) next.add(server.id); else next.delete(server.id); return next; })} />
					<Table.Cell className="text-center"><Badge variant={server.status === 'online' ? 'success' : server.status === 'offline' ? 'error' : 'neutral'} appearance="dot">{server.status === 'online' ? '在线' : server.status === 'offline' ? '离线' : '未知'}</Badge></Table.Cell>
                  <Table.Cell><div className="flex min-w-0 items-center gap-2"><i className={getOSIconClass(getServerPlatformLabel(server), { offline: server.status !== 'online' })} title={getServerPlatformLabel(server) || 'Linux'} /><span className={`truncate font-bold ${server.status === 'online' ? 'text-kumo-strong' : 'text-kumo-subtle'}`} title={server.name}>{server.name}</span></div></Table.Cell>
                  <Table.Cell className="text-center"><div className="mx-auto flex w-[64px] items-center justify-center gap-1.5">{countryCode && <CountryFlag preferSvg countryCode={countryCode} className="h-3.5 w-5 shrink-0 !rounded-[2px] text-sm" />}<span className="truncate font-semibold uppercase text-kumo-strong" title={server.location || locationLabel}>{locationLabel}</span></div></Table.Cell>
                  <Table.Cell className="text-center"><span className="font-semibold tabular-nums text-kumo-strong">{formatInstanceUptime(server.uptime)}</span></Table.Cell>
                  <Table.Cell className="text-center"><span className="font-mono text-xs">{server.agent_version && server.agent_version !== '<nil>' ? server.agent_version : '未报告'}</span></Table.Cell>
					<Table.Cell className="text-center"><div className="flex min-w-0 flex-nowrap items-center justify-center gap-2 px-2">{runtime ? <Badge className="shrink-0" variant={runtime.apply_status === 'running' ? 'success' : ['failed', 'drifted'].includes(runtime.apply_status) ? 'error' : 'warning'}>{runtime.apply_status === 'running' ? `sing-box${runtime.version ? ` ${runtime.version}` : ''}` : runtime.apply_status === 'failed' ? '部署失败' : runtime.apply_status === 'drifted' ? '状态漂移' : '部署中'}</Badge> : <Badge variant="neutral" className="shrink-0">未安装</Badge>}{server.status === 'online' && !supportsRuntimeLifecycle && <Badge variant="warning" className="shrink-0">需升级 Agent</Badge>}{tunnel && <Badge className="shrink-0" variant={tunnel.apply_status === 'running' ? 'success' : tunnel.apply_status === 'failed' ? 'error' : 'warning'} title={tunnel.apply_status === 'disconnected' && tunnel.last_error ? tunnel.last_error : undefined}>Tunnel {tunnel.apply_status === 'running' ? '已连接' : tunnel.apply_status === 'disconnected' ? '已断开' : tunnel.apply_status}</Badge>}</div></Table.Cell>
					<Table.Cell className="text-center"><div className="flex flex-nowrap justify-center gap-1">{managed.map((node) => <Badge key={node.id} variant={nodeTypeBadgeVariant(node.protocol)}>{node.protocol === 'hysteria2' ? 'HY2' : node.protocol === 'socks' ? 'SOCKS5' : node.protocol === 'http' ? 'HTTP' : 'VLESS'}</Badge>)}{managed.length === 0 && <span className="text-xs text-kumo-subtle">—</span>}</div></Table.Cell>
                  <Table.Cell className="text-center"><div className="flex w-full flex-nowrap items-center justify-center gap-1">{runtime?.apply_status === 'running' ? <><Button size="sm" variant="secondary" onClick={() => deployProxyRuntime(server.id)} disabled={!supportsRuntimeLifecycle || saving} title={!supportsRuntimeLifecycle ? '请先升级 Agent' : undefined}>升级 / 重装</Button><Button size="sm" variant={isArmed(`runtime-uninstall:${server.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => uninstallProxyRuntime(server)} disabled={saving || managed.length > 0 || !supportsRuntimeLifecycle} title={managed.length > 0 ? '请先在节点管理中卸载该实例的全部节点' : !supportsRuntimeLifecycle ? '请先升级 Agent' : '卸载 sing-box'}>{isArmed(`runtime-uninstall:${server.id}`) ? '再次确认' : '卸载程序'}</Button></> : <Button size="sm" variant="secondary" onClick={() => deployProxyRuntime(server.id)} disabled={!supportsRuntimeLifecycle || saving} title={!supportsRuntimeLifecycle ? '请先升级 Agent' : undefined}>安装代理</Button>}{tunnel ? <Button size="sm" variant={isArmed(`managed-tunnel-delete:${server.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => uninstallTunnel(server)}>{isArmed(`managed-tunnel-delete:${server.id}`) ? '再次确认' : '卸载 Tunnel'}</Button> : <Button size="sm" variant="secondary" onClick={() => openTunnelDeployment(server)} disabled={server.status !== 'online'}>部署 Tunnel</Button>}</div></Table.Cell>
                </Table.Row>
              );
            })}
            {servers.length === 0 && <Table.Row><Table.Cell colSpan={9} className="p-6 text-center text-kumo-subtle">暂无 Linux 主机</Table.Cell></Table.Row>}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </SectionCard>
  );

  const renderTunnelControls = () => (
    <LayerCard className="mb-3 overflow-hidden rounded-lg border border-kumo-line bg-kumo-base p-0 shadow-none ring-0">
      <LayerCard.Secondary className="flex min-h-12 items-center justify-between gap-3 px-3 cq-sm:px-4">
        <div className="text-sm font-semibold text-kumo-strong">Tunnel 与优选地址</div>
        <div className="flex shrink-0 items-center gap-2">
          <Button className="cq-sm:hidden" size="sm" variant="secondary" onClick={() => setPreferredModalOpen(true)}><Plus className="h-3.5 w-3.5" />管理</Button>
          <Button className="hidden cq-sm:inline-flex" size="sm" variant="secondary" onClick={() => setPreferredModalOpen(true)}><Plus className="h-3.5 w-3.5" />优选地址</Button>
        </div>
      </LayerCard.Secondary>
      <LayerCard.Primary className="px-3 py-2.5 cq-sm:px-4">
        {managedTunnels.length > 0 || preferredAddresses.length > 0 ? (
          <div className="flex flex-col gap-2">
            {managedTunnels.length > 0 && (
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
                <span className="text-xs font-semibold text-kumo-subtle">Tunnel</span>
                {managedTunnels.map((item) => {
                  const meta = tunnelStatusMeta(item.apply_status);
                  return (
                    <span
                      key={item.server_id}
                      className="cursor-pointer"
                      title={item.last_error ? `${item.last_error}；${item.hostname}（点击复制）` : `${item.hostname}（点击复制）`}
                      onClick={() => copyText(item.hostname, `已复制 ${item.server_name} 的 Tunnel 地址`)}
                    >
                      <Badge variant="secondary" className="gap-1.5 !text-xs">
                        <Badge variant={meta.variant} appearance="dot">{meta.label}</Badge>
                        <span className="font-semibold">{item.server_name}</span>
                        <Copy className="h-3 w-3 shrink-0 text-kumo-subtle" />
                      </Badge>
                    </span>
                  );
                })}
              </div>
            )}
            {preferredAddresses.length > 0 && (
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1.5">
                <span className="text-xs font-semibold text-kumo-subtle">优选地址</span>
                {preferredAddresses.map((item) => (
                  <span
                    key={item.id}
                    className={`cursor-pointer ${item.enabled === false ? 'opacity-50' : ''}`}
                    title={item.last_error ? `${item.last_error}；${item.address}:${item.port}（点击复制）` : `${item.address}:${item.port}（点击复制）`}
                    onClick={() => copyText(`${item.address}:${item.port}`, `已复制 ${item.name} 的地址`)}
                  >
                    <Badge variant="secondary" className="gap-1.5 !text-xs">
                      <span className={`font-semibold ${item.enabled === false ? 'text-kumo-subtle' : ''}`}>{item.name}</span>
                      <Copy className="h-3 w-3 shrink-0 text-kumo-subtle" />
                      {item.last_status === 'healthy' && <Badge variant="success" appearance="dot">{item.last_latency_ms > 0 ? `${item.last_latency_ms}ms` : '正常'}</Badge>}
                      {item.last_status === 'failed' && <Badge variant="error" appearance="dot">不可达</Badge>}
                    </Badge>
                  </span>
                ))}
              </div>
            )}
          </div>
        ) : (
          <span className="text-xs text-kumo-subtle">暂无 Tunnel 与优选地址</span>
        )}
      </LayerCard.Primary>
    </LayerCard>
  );

  const renderNodesSkeleton = () => (
    <div className="grid gap-3" aria-busy="true" aria-label="正在加载节点">
      <LayerCard className="flex flex-col overflow-hidden p-0 shadow-none">
        <LayerCard.Secondary className="flex min-h-[56px] items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed/20 px-4 py-3.5">
          <div className="flex min-w-0 flex-1 items-center gap-3">
            <SkeletonLine className="h-4 w-20" />
            <SkeletonLine className="h-3 w-72 max-w-[42vw]" />
          </div>
          <div className="hidden shrink-0 items-center gap-2 cq-sm:flex">
            <SkeletonLine className="h-8 w-24" />
            <SkeletonLine className="h-8 w-24" />
            <SkeletonLine className="h-8 w-28" />
          </div>
        </LayerCard.Secondary>
        <LayerCard.Primary className="p-4">
          <div className="grid gap-4 cq-lg:grid-cols-4">
            <div className="space-y-2">
              <SkeletonLine className="h-3 w-12" />
              <SkeletonLine className="h-8 w-full" />
            </div>
            <div className="space-y-2 cq-lg:col-span-2">
              <SkeletonLine className="h-3 w-20" />
              <SkeletonLine className="h-8 w-full" />
            </div>
            <div className="space-y-2">
              <SkeletonLine className="h-3 w-24" />
              <SkeletonLine className="h-8 w-full" />
            </div>
            <SkeletonLine className="h-8 w-32" />
          </div>
        </LayerCard.Primary>
      </LayerCard>

      <DataTableFrame>
        <AppTable tableId="subscription-nodes-skeleton" columns={NODE_COLUMNS}>
          <Table.Header>
            <Table.Row>
              <Table.Head className="text-center">状态</Table.Head>
              <Table.Head>节点名称</Table.Head>
              <Table.Head className="text-center">类型</Table.Head>
              <Table.Head>连接</Table.Head>
              <Table.Head>主机 / 延迟</Table.Head>
              <Table.Head className="app-table-action">操作</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {Array.from({ length: 6 }).map((_, index) => (
              <Table.Row key={index}>
                <Table.Cell><SkeletonLine className="mx-auto h-5 w-9" /></Table.Cell>
                <Table.Cell>
                  <SkeletonLine className="h-4 w-32" />
                </Table.Cell>
                <Table.Cell><SkeletonLine className="h-5 w-16" /></Table.Cell>
                <Table.Cell><SkeletonLine className="h-3 w-44" /></Table.Cell>
                <Table.Cell><SkeletonLine className="h-3 w-16" /></Table.Cell>
                <Table.Cell>
                  <div className="flex gap-1">
                    <SkeletonLine className="h-8 w-8" />
                    <SkeletonLine className="h-8 w-8" />
                  </div>
                </Table.Cell>
              </Table.Row>
            ))}
          </Table.Body>
        </AppTable>
      </DataTableFrame>
    </div>
  );

  const renderTemplates = () => (
    <MasonryGrid>
      <SectionCard
        title="模板转换"
        actions={(
          <div className="flex flex-wrap justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={openCreateTemplate}><Plus className="h-3.5 w-3.5" />新建模板</Button>
            <Button size="sm" variant="primary" onClick={saveTemplateBinding} loading={saving} disabled={!selectedTemplateSubscription}><Save className="h-3.5 w-3.5" />保存转换</Button>
          </div>
        )}
      >
        <div className="grid grid-cols-1 gap-4 cq-sm:grid-cols-2">
          <Select size="sm" label="对外订阅" value={templateSubscriptionId} onValueChange={(value) => setTemplateSubscriptionId(String(value))} items={subscriptionItems} className="w-full" />
          <Select size="sm" label="输出模板" value={templateBindingId} onValueChange={(value) => setTemplateBindingId(String(value))} items={templateItems} disabled={!selectedTemplateSubscription} className="w-full" />
        </div>
        {selectedTemplateSubscription && (
          <div className="mt-4 grid gap-2 border-t border-kumo-line pt-4 cq-sm:grid-cols-2">
            <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription)} tooltip={{ text: '复制自适应订阅链接（按客户端自动识别）', copiedText: '自适应订阅链接已复制' }} labels={{ copyAction: '复制自适应订阅' }} />
            <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription, 'clash')} tooltip={{ text: '复制 Mihomo / Clash 链接', copiedText: 'Mihomo / Clash 链接已复制' }} labels={{ copyAction: '复制 Clash（YAML）' }} />
            <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription, 'base64')} tooltip={{ text: '复制 Base64 链接（sing-box 官方 / v2rayN）', copiedText: 'Base64 链接已复制' }} labels={{ copyAction: '复制 Base64' }} />
            <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription, 'raw')} tooltip={{ text: '复制 Raw 链接', copiedText: 'Raw 链接已复制' }} labels={{ copyAction: '复制 Raw' }} />
            <ClipboardText size="sm" text={subscriptionURL(publicBase, selectedTemplateSubscription, 'info')} tooltip={{ text: '复制订阅信息页链接（浏览器打开）', copiedText: '订阅信息页链接已复制' }} labels={{ copyAction: '复制信息页' }} />
          </div>
        )}
      </SectionCard>

      {templates.map((tpl) => (
          <LayerCard key={tpl.id} className="overflow-hidden">
            <LayerCard.Secondary className="flex items-center justify-between gap-3">
              <div className="min-w-0">
                <div className="flex min-w-0 items-center gap-2">
                  <span className="truncate text-sm font-bold text-kumo-strong">{tpl.name}</span>
                  {tpl.is_default && <Badge variant="success">默认</Badge>}
					{tpl.builtin && <Badge variant="neutral">内置</Badge>}
					{tpl.valid === false && <Badge variant="error">配置错误</Badge>}
                </div>
              </div>
              <div className="flex shrink-0 gap-1">
                <Button size="sm" variant="secondary" onClick={() => setDefaultTemplate(tpl)} disabled={tpl.valid === false}>默认</Button>
                <Button size="sm" shape="square" variant="secondary" onClick={() => openCloneTemplate(tpl)} aria-label="复制模板" title="复制模板" icon={<Copy className="h-3.5 w-3.5" />} />
                <Button size="sm" shape="square" variant="secondary" onClick={() => openEditTemplate(tpl)} aria-label="编辑模板" title="编辑模板" icon={<Edit className="h-3.5 w-3.5" />} />
                <Button size="sm" variant={isArmed(`template-delete:${tpl.id}`) ? 'destructive' : 'secondary-destructive'} onClick={() => deleteTemplate(tpl)} disabled={tpl.builtin}><Trash className="h-3.5 w-3.5" /></Button>
              </div>
            </LayerCard.Secondary>
            <LayerCard.Primary>
              <div className={`mb-3 text-xs ${tpl.valid === false ? 'text-kumo-danger' : 'text-kumo-subtle'}`}>{tpl.validation_error || tpl.description || tpl.format}</div>
              <div className="max-h-44 overflow-auto">
                <Code lang={tpl.format === 'clash' ? 'yaml' : 'text'} code={tpl.content} />
              </div>
            </LayerCard.Primary>
          </LayerCard>
      ))}
    </MasonryGrid>
  );

  const renderLogs = () => (
    <DataTableFrame>
      <AppTable tableId="subscription-access-logs" columns={SUBSCRIPTION_LOG_COLUMNS}>
          <Table.Header>
            <Table.Row>
              <Table.Head>时间</Table.Head>
              <Table.Head>对外订阅</Table.Head>
              <Table.Head>客户端</Table.Head>
              <Table.Head>格式</Table.Head>
              <Table.Head>结果</Table.Head>
              <Table.Head>节点</Table.Head>
              <Table.Head>流量快照</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {logs.map((log) => (
              <Table.Row key={log.id}>
                <Table.Cell className="text-xs">{formatTime(log.created_at)}</Table.Cell>
                <Table.Cell className="text-xs">{subscriptions.find((item) => item.id === log.subscription_id)?.name || log.subscription_id || '-'}</Table.Cell>
                <Table.Cell><div className="truncate font-mono text-[11px]">{log.ip_address}</div><div className="truncate text-[10px] text-kumo-subtle">{log.user_agent}</div></Table.Cell>
                <Table.Cell className="text-xs">{log.format || '-'}</Table.Cell>
                <Table.Cell><Badge variant={log.success ? 'success' : 'error'} appearance="dot">{log.success ? '成功' : log.error_message || log.status_code}</Badge></Table.Cell>
                <Table.Cell className="text-xs">{log.node_count}</Table.Cell>
                <Table.Cell className="text-xs">{formatBytes((log.upload_bytes || 0) + (log.download_bytes || 0))} / {formatBytes(log.total_bytes || 0)}</Table.Cell>
              </Table.Row>
            ))}
            {logs.length === 0 && (
              <Table.Row><Table.Cell colSpan={7} className="p-8 text-center text-kumo-subtle">暂无访问日志。</Table.Cell></Table.Row>
            )}
          </Table.Body>
      </AppTable>
    </DataTableFrame>
  );

  const renderSettings = () => settings && (
    <SectionCard title="默认策略" className="max-w-3xl">
        <div className="grid gap-4 cq-sm:grid-cols-2">
          <Select size="sm" label="默认模板" value={settings.default_template_id} onValueChange={(value) => setSettings((prev) => ({ ...prev, default_template_id: String(value) }))} items={templateItems} />
          <Input size="sm" label="默认上游刷新间隔（小时）" type="number" value={settings.default_refresh_hours || 24} onChange={(e) => setSettings((prev) => ({ ...prev, default_refresh_hours: Number(e.target.value) || 24 }))} />
          <Input size="sm" label="默认限流阈值（次/分钟）" type="number" value={settings.default_rate_limit_per_minute || 30} onChange={(e) => setSettings((prev) => ({ ...prev, default_rate_limit_per_minute: Number(e.target.value) || 30 }))} />
          <Switch
            size="sm"
            label="默认启用限流"
            controlFirst={false}
            checked={!!settings.default_rate_limit_enabled}
            onCheckedChange={(checked) => setSettings((prev) => ({ ...prev, default_rate_limit_enabled: checked }))}
          />
        </div>
        <div className="mt-4 flex justify-end">
          <Button size="sm" variant="primary" onClick={saveSettings}><Save className="h-3.5 w-3.5" />保存设置</Button>
        </div>
    </SectionCard>
  );

  return (
    <PageStack>
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={(value) => setActiveTab(String(value))}
          tabs={[
            { value: 'instances', label: <span className="inline-flex items-center gap-1.5"><Server className="h-3.5 w-3.5" />实例管理</span> },
            { value: 'nodes', label: <span className="inline-flex items-center gap-1.5"><Globe className="h-3.5 w-3.5" />节点管理</span> },
            { value: 'plans', label: <span className="inline-flex items-center gap-1.5"><Box className="h-3.5 w-3.5" />套餐管理</span> },
            { value: 'subscriptions', label: <span className="inline-flex items-center gap-1.5"><Plug className="h-3.5 w-3.5" />订阅管理</span> },
            { value: 'templates', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-3.5 w-3.5" />模板管理</span> },
          ]}
        />
      </div>

      <div className="min-w-0">
        {loading && servers.length === 0 && nodes.length === 0 && plans.length === 0 && subscriptions.length === 0 ? renderNodesSkeleton() : (
          <div className="min-w-0">
            {activeTab === 'nodes' && renderNodes()}
            {activeTab === 'instances' && <>{renderTunnelControls()}{renderInstanceManagement()}</>}
            {activeTab === 'plans' && renderPlans()}
            {activeTab === 'subscriptions' && renderSubscriptions()}
            {activeTab === 'templates' && renderTemplates()}
          </div>
        )}
      </div>

      <Dialog.Root open={planModalOpen} onOpenChange={setPlanModalOpen}>
        <Dialog size="xl" className="@container flex max-h-[min(calc(100dvh-2rem),48rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:!w-[min(72rem,calc(100vw-3rem))] cq-sm:!max-w-[min(72rem,calc(100vw-3rem))]">
          <div className="border-b border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Dialog.Title>{editingPlanId ? '编辑套餐' : '新建套餐'}</Dialog.Title></div>
          <div className="min-h-0 flex-1 space-y-4 overflow-y-auto p-3 scrollbar-thin cq-sm:p-5">
            <div className="grid gap-3 cq-sm:grid-cols-2"><Input size="sm" label="套餐名称" value={planForm.name} onChange={(e) => setPlanForm((prev) => ({ ...prev, name: e.target.value }))} /><Input size="sm" label="备注" value={planForm.remark} onChange={(e) => setPlanForm((prev) => ({ ...prev, remark: e.target.value }))} /></div>
            <div className="grid items-end gap-3 cq-md:grid-cols-[minmax(16rem,1.2fr)_minmax(12rem,.8fr)_minmax(10rem,.7fr)]"><TrafficSizeInput label="订阅额度（仅托管节点，0 不限）" value={planForm.total_bytes} onChange={(value) => setPlanForm((prev) => ({ ...prev, total_bytes: value }))} /><Select size="sm" label="重置周期" value={planForm.cycle_type} onValueChange={(value) => setPlanForm((prev) => ({ ...prev, cycle_type: String(value) }))} items={[{ value: 'monthly', label: '每月重置' }, { value: 'none', label: '不重置' }]} /><Input size="sm" label="每月重置日" type="number" min="1" max="31" value={planForm.cycle_day} disabled={planForm.cycle_type !== 'monthly'} onChange={(e) => setPlanForm((prev) => ({ ...prev, cycle_day: Number(e.target.value) || 1 }))} /></div>
			{planForm.total_bytes > 0 && ((planForm.selection_mode === 'all' && planForm.include_external_nodes) || (planForm.selection_mode === 'explicit' && planForm.node_ids.some((id) => nodes.some((node) => node.id === id)))) && <div className="rounded-md border border-kumo-warning/30 bg-kumo-warning/10 px-3 py-2 text-xs text-kumo-warning">外部节点不受 Agent 管理，额度仅约束内部节点。</div>}
            <div className="grid items-end gap-3 cq-md:grid-cols-[minmax(18rem,1fr)_auto]"><Input size="sm" label="订阅请求限制（次/分钟）" type="number" min="1" value={planForm.rate_limit_per_minute} onChange={(e) => setPlanForm((prev) => ({ ...prev, rate_limit_per_minute: Number(e.target.value) || 30 }))} /><div className="flex min-h-8 items-center"><Switch size="sm" label="启用请求限制" checked={planForm.rate_limit_enabled} onCheckedChange={(checked) => setPlanForm((prev) => ({ ...prev, rate_limit_enabled: checked }))} /></div></div>
            <div className="border-t border-kumo-line pt-4">
              <div className="mb-3 grid items-end gap-3 cq-sm:grid-cols-[14rem_1fr]">
                <Select size="sm" label="节点范围" value={planForm.selection_mode} onValueChange={(value) => setPlanForm((prev) => ({ ...prev, selection_mode: String(value), node_ids: String(value) === 'all' ? [] : prev.node_ids }))} items={[{ value: 'explicit', label: '指定节点' }, { value: 'all', label: '全部当前及未来节点' }]} />
                {planForm.selection_mode === 'all' && <div className="flex min-h-8 flex-wrap items-center gap-x-6 gap-y-2"><Switch size="sm" label="包含内部节点" checked={planForm.include_internal_nodes} onCheckedChange={(checked) => setPlanForm((prev) => ({ ...prev, include_internal_nodes: checked }))} /><Switch size="sm" label="包含外部节点" checked={planForm.include_external_nodes} onCheckedChange={(checked) => setPlanForm((prev) => ({ ...prev, include_external_nodes: checked }))} /></div>}
              </div>
              {planForm.selection_mode === 'explicit' && <>
				<div className="mb-2 flex flex-wrap items-end justify-between gap-2"><div className="flex flex-wrap items-end gap-2"><Label className="text-xs font-semibold text-kumo-subtle">套餐节点</Label><Select size="sm" aria-label="节点类型筛选" value={planNodeTypeFilter} onValueChange={(value) => setPlanNodeTypeFilter(String(value))} items={planNodeTypeItems} className="w-36" /><Select size="sm" aria-label="节点来源筛选" value={planNodeSourceFilter} onValueChange={(value) => setPlanNodeSourceFilter(String(value))} items={[{ value: 'all', label: '全部来源' }, { value: 'internal', label: 'Agent 节点' }, { value: 'external', label: '外部节点' }]} className="w-36" /></div><div className="flex items-center gap-2"><Badge variant="neutral">已选 {planForm.node_ids.length}</Badge><Button size="sm" variant="secondary" disabled={visiblePlanNodeIDs.length === 0} onClick={() => setPlanForm((prev) => ({ ...prev, node_ids: allVisiblePlanNodesSelected ? prev.node_ids.filter((id) => !visiblePlanNodeIDs.includes(id)) : [...new Set([...prev.node_ids, ...visiblePlanNodeIDs])] }))}>{allVisiblePlanNodesSelected ? '取消当前全部' : '全选当前结果'}</Button></div></div>
				<div className="max-h-72 overflow-auto rounded-md border border-kumo-line p-2 scrollbar-thin"><div className="grid gap-1 cq-sm:grid-cols-2 cq-lg:grid-cols-3">{visiblePlanNodes.map((node) => <label key={`${node.source_group}-${node.id}`} className="flex min-w-0 items-center gap-2 rounded px-2 py-1.5 hover:bg-kumo-recessed"><Checkbox aria-label={`选择套餐节点 ${node.name}`} checked={planForm.node_ids.includes(node.id)} onCheckedChange={(checked) => setPlanForm((prev) => ({ ...prev, node_ids: checked ? [...new Set([...prev.node_ids, node.id])] : prev.node_ids.filter((id) => id !== node.id) }))} /><span className="min-w-0 flex-1 truncate text-xs font-semibold">{node.name}</span><Badge variant="neutral">{node.source_group === 'internal' ? 'Agent' : '外部'}</Badge><Badge variant={nodeTypeBadgeVariant(node.display_type)}>{node.display_type || '-'}</Badge></label>)}{visiblePlanNodes.length === 0 && <div className="p-5 text-center text-xs text-kumo-subtle cq-sm:col-span-2 cq-lg:col-span-3">没有符合类型与来源条件的节点</div>}</div></div>
              </>}
            </div>
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5"><Dialog.Close render={(props) => <Button size="sm" variant="secondary" {...props}>取消</Button>} /><Button size="sm" variant="primary" loading={saving} onClick={savePlan}><Save className="h-3.5 w-3.5" />保存套餐</Button></div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={internalNodeModalOpen} onOpenChange={setInternalNodeModalOpen}>
        <Dialog size="xl" className="@container !w-[min(58rem,calc(100vw-1rem))] !max-w-[min(58rem,calc(100vw-1rem))] overflow-hidden p-0">
          <div className="border-b border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Dialog.Title>{editingInternalNodeId ? '编辑内部节点' : '生成内部节点'}</Dialog.Title></div>
          <div className="grid gap-3 p-3 cq-sm:grid-cols-2 cq-sm:p-5">
            {!editingInternalNodeId && <div className="cq-sm:col-span-2">
				<div className="flex items-center justify-between gap-2"><Label className="text-xs font-semibold text-kumo-subtle">已安装代理程序的实例</Label><Badge variant="neutral">已选 {selectedInternalHosts.size} / {runtimeReadyServers.length}</Badge></div>
              <div className="mt-1.5 max-h-44 overflow-auto rounded-md border border-kumo-line bg-kumo-recessed/20 p-1.5 scrollbar-thin">
                <div className="grid gap-1 cq-sm:grid-cols-2">
                  {runtimeReadyServers.map((server) => {
                    const checked = selectedInternalHosts.has(server.id);
					return <label key={server.id} className="flex min-w-0 items-center gap-2 rounded px-2 py-1.5 hover:bg-kumo-base/60"><Checkbox checked={checked} disabled={server.status !== 'online'} onCheckedChange={(value) => { const next = new Set(selectedInternalHosts); if (value) next.add(server.id); else next.delete(server.id); setSelectedInternalHosts(next); const first = [...next][0] || ''; setInternalNodeForm((prev) => ({ ...prev, server_id: first, public_host: servers.find((item) => item.id === first)?.host || '' })); }} aria-label={`选择 ${server.name}`} /><span className="min-w-0 flex-1 truncate text-xs font-semibold text-kumo-strong">{server.name}</span><Badge variant={server.status === 'online' ? 'success' : 'neutral'} appearance="dot">{server.status === 'online' ? '在线' : '离线'}</Badge></label>;
                  })}
                  {runtimeReadyServers.length === 0 && <div className="p-3 text-center text-xs text-kumo-subtle cq-sm:col-span-2">暂无已安装 sing-box 的实例</div>}
                </div>
              </div>
            </div>}
            {!editingInternalNodeId && <Select size="sm" label="节点协议" value={internalNodeForm.protocol} onValueChange={(value) => setInternalNodeForm((prev) => ({ ...prev, protocol: String(value), access_mode: String(value) === 'socks' || String(value) === 'http' ? 'direct' : prev.access_mode }))} items={[{ value: 'vless-reality', label: 'VLESS REALITY' }, { value: 'hysteria2', label: 'Hysteria2' }, { value: 'socks', label: 'SOCKS5' }, { value: 'http', label: 'HTTP' }]} />}
            <Input size="sm" label={editingInternalNodeId ? '节点名称' : selectedInternalHosts.size > 1 ? '节点名称前缀（可选）' : '节点名称（可选）'} placeholder="留空按实例名生成" value={internalNodeForm.name} onChange={(event) => setInternalNodeForm((prev) => ({ ...prev, name: event.target.value }))} />
            {!editingInternalNodeId && internalNodeForm.protocol === 'vless-reality' && <Input size="sm" label="REALITY 握手站点" placeholder="默认 www.cloudflare.com" value={internalNodeForm.server_name} onChange={(event) => setInternalNodeForm((prev) => ({ ...prev, server_name: event.target.value }))} />}
            {!editingInternalNodeId && internalNodeForm.protocol === 'hysteria2' && <div className="flex min-h-8 items-center rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 text-xs text-kumo-subtle">TLS 信息自动生成。</div>}
            {!editingInternalNodeId && (internalNodeForm.protocol === 'socks' || internalNodeForm.protocol === 'http') && <div className="flex min-h-8 items-center rounded-md border border-kumo-info/25 bg-kumo-info/10 px-3 text-xs text-kumo-subtle">明文字段：SOCKS/HTTP 仅直连，无 TLS 加密。</div>}
            {!editingInternalNodeId && <Select size="sm" label="接入方式" value={internalNodeForm.access_mode || 'direct'} disabled={internalNodeForm.protocol === 'socks' || internalNodeForm.protocol === 'http'} onValueChange={(value) => setInternalNodeForm((prev) => ({ ...prev, access_mode: String(value) }))} items={[{ value: 'direct', label: '直连节点' }, { value: 'cloudflare_tunnel', label: 'Cloudflare Tunnel（VLESS WS）' }]} />}
            {internalNodeForm.access_mode === 'cloudflare_tunnel' && <Select size="sm" label="优选地址" value={internalNodeForm.preferred_address_id || ''} onValueChange={(value) => setInternalNodeForm((prev) => ({ ...prev, preferred_address_id: String(value) }))} items={[{ value: '', label: '继承默认地址' }, ...preferredAddresses.map((item) => ({ value: item.id, label: `${item.name} · ${item.address}` }))]} />}
            <div className="flex min-h-8 items-center rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2"><Switch size="sm" label="稳定节点" controlFirst={false} checked={!!internalNodeForm.stable} onCheckedChange={(checked) => setInternalNodeForm((prev) => ({ ...prev, stable: checked }))} /></div>
          </div>
          <div className="flex justify-end gap-2 border-t border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Button size="sm" variant="secondary" onClick={() => setInternalNodeModalOpen(false)}>取消</Button><Button size="sm" variant="primary" loading={saving} onClick={editingInternalNodeId ? saveInternalNode : createInternalNode}>{editingInternalNodeId ? '保存' : '生成节点'}</Button></div>
        </Dialog>
      </Dialog.Root>

		<Dialog.Root open={tunnelModalOpen} onOpenChange={setTunnelModalOpen}>
			<Dialog size="lg" className="@container w-[calc(100vw-1rem)] max-w-2xl p-0">
				<div className="border-b border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Dialog.Title>部署 Cloudflare Named Tunnel</Dialog.Title></div>
				<div className="grid gap-3 p-3 cq-sm:grid-cols-2 cq-sm:p-5"><Select size="sm" label="Cloudflare 账号" value={tunnelForm.account_id} onValueChange={(value) => setTunnelForm((prev) => ({ ...prev, account_id: String(value), zone_id: '', hostname: '' }))} items={cloudflareAccounts.map((item) => ({ value: item.id, label: item.name || item.email || item.id }))} /><Select size="sm" label="DNS Zone" value={tunnelForm.zone_id} onValueChange={(value) => setTunnelForm((prev) => ({ ...prev, zone_id: String(value) }))} items={cloudflareZones.map((item) => ({ value: item.id, label: item.name || item.id }))} /><Input size="sm" className="cq-sm:col-span-2" label="自动生成的 Tunnel 域名" value={tunnelForm.hostname || '选择 DNS Zone 后自动生成'} readOnly /></div>
				<div className="flex justify-end gap-2 border-t border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4"><Button size="sm" variant="secondary" onClick={() => setTunnelModalOpen(false)}>取消</Button><Button size="sm" variant="primary" onClick={() => deployTunnel()} disabled={!tunnelTargetServer || !tunnelForm.hostname}>开始部署</Button></div>
			</Dialog>
		</Dialog.Root>

		<Dialog.Root open={preferredModalOpen} onOpenChange={setPreferredModalOpen}>
			<Dialog size="lg" className="@container !w-[min(56rem,calc(100vw-1rem))] !max-w-[min(56rem,calc(100vw-1rem))] overflow-hidden p-0">
				<div className="flex min-h-12 items-center justify-between gap-3 border-b border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
					<Dialog.Title>优选地址</Dialog.Title>
					<div className="flex shrink-0 items-center gap-2">
						<Badge variant="neutral">{preferredAddresses.length} 个地址</Badge>
						<Dialog.Close
							aria-label="关闭"
							render={(props) => (
								<Button
									{...props}
									type="button"
									variant="secondary"
									shape="square"
									size="sm"
									icon={<X className="h-3.5 w-3.5" />}
									aria-label="关闭"
								/>
							)}
						/>
					</div>
				</div>
				<div className="grid min-h-0 min-w-0 cq-lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
					<div className="flex min-w-0 flex-col gap-3 border-b border-kumo-line p-3 cq-sm:p-4 cq-lg:border-b-0 cq-lg:border-r">
						<div className="text-xs font-semibold text-kumo-strong">添加新地址</div>
						<Input size="sm" label="名称" value={preferredForm.name} onChange={(event) => setPreferredForm((prev) => ({ ...prev, name: event.target.value }))} />
						<Input size="sm" label="域名或 IP" placeholder="saas.sin.fan" value={preferredForm.address} onChange={(event) => setPreferredForm((prev) => ({ ...prev, address: event.target.value }))} />
						<Input size="sm" label="端口" type="number" value={preferredForm.port} onChange={(event) => setPreferredForm((prev) => ({ ...prev, port: Number(event.target.value) || 443 }))} />
						<div className="flex min-w-0 items-center justify-between gap-3">
							<Label className="min-w-0 truncate text-xs text-kumo-subtle">保存后设为全局默认</Label>
							<Switch size="sm" aria-label="保存后设为全局默认" checked={!!preferredForm.is_default} onCheckedChange={(checked) => setPreferredForm((prev) => ({ ...prev, is_default: checked }))} />
						</div>
						<Button size="sm" variant="primary" className="self-end" onClick={savePreferredAddress}><Save className="h-3.5 w-3.5" />添加地址</Button>
					</div>
					<div className="flex min-h-0 min-w-0 flex-col">
						<div className="px-3 pt-3 cq-sm:px-4"><div className="text-xs font-semibold text-kumo-strong">地址列表</div></div>
						<div className="max-h-64 min-h-0 overflow-y-auto p-2 scrollbar-thin">
							{preferredAddresses.length === 0 ? (
								<div className="p-6 text-center text-xs text-kumo-subtle">暂无优选地址，请先添加</div>
							) : [...preferredAddresses].sort((a, b) => (a.sort_order || 0) - (b.sort_order || 0) || String(a.created_at || '').localeCompare(String(b.created_at || ''))).map((item) => (
<div key={item.id} className={`flex min-w-0 items-center gap-2 rounded-md px-2 py-1.5 ${item.is_default ? 'bg-kumo-recessed/60' : 'hover:bg-kumo-recessed/40'}`}>
									<div className="min-w-0 flex-1">
										<div className="flex min-w-0 items-center gap-1.5">
											<span className={`truncate text-xs font-semibold ${item.enabled === false ? 'text-kumo-subtle' : 'text-kumo-strong'}`}>{item.name}</span>
										</div>
										<div className="truncate font-mono text-[11px] text-kumo-subtle">{item.address}:{item.port}</div>
									</div>
									<div className="flex shrink-0 items-center gap-1">
										{!item.is_default && <Button size="sm" variant="secondary" onClick={() => setPreferredDefault(item)} icon={<Star className="h-3.5 w-3.5" />}>默认</Button>}
										<Button size="sm" shape="square" variant="secondary-destructive" onClick={() => deletePreferredAddress(item)} icon={<Trash className="h-3.5 w-3.5" />} aria-label={`删除 ${item.name}`} />
									</div>
								</div>
							))}
						</div>
					</div>
				</div>
				<div className="flex justify-end gap-2 border-t border-kumo-line px-3 py-3 cq-sm:px-5 cq-sm:py-4">
					<Button size="sm" variant="secondary" onClick={() => setPreferredModalOpen(false)}>关闭</Button>
				</div>
			</Dialog>
		</Dialog.Root>

      <Dialog.Root open={subscriptionModalOpen} onOpenChange={setSubscriptionModalOpen}>
        <Dialog size="lg" className="@container flex max-h-[min(calc(100dvh-2rem),42rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:w-[min(calc(100vw-3rem),64rem)]">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b border-kumo-line bg-kumo-recessed/20 px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
              <div className="min-w-0">
                <Dialog.Title className="min-w-0 truncate text-base font-semibold text-kumo-strong">
                  {editingSubscriptionId ? '编辑对外订阅' : '创建对外订阅'}
                </Dialog.Title>
              </div>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square"
                    size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden px-3 py-3 text-xs scrollbar-thin cq-sm:px-5 cq-sm:py-4">
              <div className="space-y-4">
                <section className="space-y-3">
                  <div className="text-[11px] font-bold uppercase tracking-wide text-kumo-subtle">基础信息</div>
                  <div className="grid min-w-0 gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(16rem,100%),1fr))]">
                    <div className="min-w-0">
                      <Input size="sm" label="名称" value={subscriptionForm.name} onChange={(e) => setSubscriptionForm((prev) => ({ ...prev, name: e.target.value }))} className="w-full min-w-0" />
                    </div>
                    <div className="min-w-0"><Select size="sm" label="套餐" value={subscriptionForm.plan_id || ''} onValueChange={(value) => setSubscriptionForm((prev) => ({ ...prev, plan_id: String(value) }))} items={planItems} className="w-full min-w-0" /></div>
                  </div>
                </section>

              </div>
            </div>

            <div className="flex shrink-0 items-center justify-between gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5 cq-sm:justify-end">
              <Dialog.Close render={(props) => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
              <Button size="sm" variant="primary" loading={saving} onClick={saveSubscription}><Save className="h-3.5 w-3.5" />保存</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={nodeModalOpen} onOpenChange={setNodeModalOpen}>
        <Dialog size="lg" className="@container flex max-h-[min(calc(100dvh-2rem),44rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:w-[min(calc(100vw-3rem),72rem)]">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b border-kumo-line bg-kumo-recessed/20 px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
              <div className="min-w-0">
                <Dialog.Title className="min-w-0 truncate text-base font-semibold text-kumo-strong">编辑节点</Dialog.Title>
              </div>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square"
                    size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden px-3 py-3 scrollbar-thin cq-sm:px-5 cq-sm:py-4">
              <div className="space-y-4">
                <section className="min-w-0 space-y-3">
                  <div className="text-[11px] font-bold uppercase tracking-wide text-kumo-subtle">连接信息</div>
                  <div className="grid min-w-0 gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(14rem,100%),1fr))]">
                    <Input size="sm" label="节点名称" value={nodeForm.name} onChange={(e) => setNodeForm((prev) => syncNodeForm(prev, 'name', e.target.value))} className="w-full min-w-0" />
                    <Input size="sm" label="协议类型" value={nodeForm.type} onChange={(e) => setNodeForm((prev) => syncNodeForm(prev, 'type', e.target.value))} className="w-full min-w-0" />
                    <Input size="sm" label="服务器地址" value={nodeForm.server} onChange={(e) => setNodeForm((prev) => syncNodeForm(prev, 'server', e.target.value))} className="w-full min-w-0" />
                    <Input size="sm" label="端口" type="number" value={nodeForm.port || 0} onChange={(e) => setNodeForm((prev) => syncNodeForm(prev, 'port', Number(e.target.value) || 0))} className="w-full min-w-0" />
                    <Input size="sm" label="国家 / 地区代码" value={nodeForm.country_code || ''} onChange={(e) => setNodeForm((prev) => ({ ...prev, country_code: e.target.value }))} className="w-full min-w-0" />
                    <Input size="sm" label="位置" value={nodeForm.location || ''} onChange={(e) => setNodeForm((prev) => ({ ...prev, location: e.target.value }))} className="w-full min-w-0" />
                    <Input size="sm" label="标签" value={nodeForm.tags || ''} onChange={(e) => setNodeForm((prev) => ({ ...prev, tags: e.target.value }))} className="w-full min-w-0" />
                  </div>
                </section>

                <section className="min-w-0 space-y-3 border-t border-kumo-line pt-4">
                  <div className="text-[11px] font-bold uppercase tracking-wide text-kumo-subtle">外部节点属性</div>
                  <div className="grid min-w-0 items-end gap-3 [grid-template-columns:repeat(auto-fit,minmax(min(14rem,100%),1fr))]">
                    <Input size="sm" label="排序" type="number" value={nodeForm.sort_order || 0} onChange={(e) => setNodeForm((prev) => ({ ...prev, sort_order: Number(e.target.value) || 0 }))} className="w-full min-w-0" />
                    <div className="grid min-h-8 min-w-0 gap-3 rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2 cq-sm:grid-cols-2">
                      <Switch size="sm" label="启用节点" controlFirst={false} checked={!!nodeForm.enabled} onCheckedChange={(checked) => setNodeForm((prev) => ({ ...prev, enabled: checked }))} />
                      <Switch size="sm" label="稳定节点" controlFirst={false} checked={!!nodeForm.stable} onCheckedChange={(checked) => setNodeForm((prev) => ({ ...prev, stable: checked }))} />
                    </div>
                  </div>
                </section>

                <section className="min-w-0 space-y-3 border-t border-kumo-line pt-4">
                  <div className="text-[11px] font-bold uppercase tracking-wide text-kumo-subtle">原始配置</div>
                  <div className="grid min-w-0 gap-3">
                    <CodeEditor label="原始节点链接" language="text" minHeight="9rem" value={nodeForm.raw || ''} onChange={(raw) => setNodeForm((prev) => syncNodeForm(prev, 'raw', raw))} />
                    <CodeEditor label="节点配置 JSON" language="json" minHeight="9rem" value={nodeForm.config_json || ''} onChange={(config_json) => setNodeForm((prev) => syncNodeForm(prev, 'config_json', config_json))} />
                  </div>
                </section>
              </div>
            </div>

            <div className="flex shrink-0 items-center justify-between gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5 cq-sm:justify-end">
              <Dialog.Close render={(props) => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
              <Button size="sm" variant="primary" loading={saving} onClick={saveNode}><Save className="h-3.5 w-3.5" />保存节点</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={importModalOpen} onOpenChange={setImportModalOpen}>
        <Dialog size="xl" className="@container flex !h-auto max-h-[min(calc(100dvh-1rem),42rem)] !w-[min(72rem,calc(100vw-1rem))] !max-w-[min(72rem,calc(100vw-1rem))] flex-col overflow-hidden p-0">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b border-kumo-line bg-kumo-recessed/20 px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
              <div className="min-w-0">
                <Dialog.Title className="min-w-0 truncate text-base font-semibold text-kumo-strong">导入节点</Dialog.Title>
              </div>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square"
                    size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden px-3 py-3 scrollbar-thin cq-sm:px-5 cq-sm:py-4">
              <div className="grid min-w-0 items-start gap-4 cq-lg:grid-cols-[minmax(0,1.35fr)_minmax(18rem,0.65fr)]">
                <div className="min-w-0 space-y-3">
                  <Input size="sm" label="订阅 URL" placeholder="https://example.com/sub.yaml" value={importSourceURL} onChange={(e) => setImportSourceURL(e.target.value)} />
                  <CodeEditor className="h-[18rem] min-w-0" label="节点链接 / YAML / Base64 内容" language="yaml" minHeight="18rem" placeholder="可粘贴节点链接、Base64 订阅，或 Clash/Mihomo YAML 的 proxies。" value={importText} onChange={setImportText} />
                </div>
                <LayerCard className="flex h-[18rem] min-h-0 min-w-0 flex-col overflow-hidden border border-kumo-line bg-kumo-elevated p-0 shadow-none cq-lg:mt-[3.5rem]">
                  <LayerCard.Secondary className="flex min-h-11 items-center justify-between gap-3 border-b border-kumo-line bg-kumo-recessed/20 px-4 py-2.5">
                    <div className="min-w-0 truncate text-sm font-bold text-kumo-strong">解析预览</div>
					<Badge variant="neutral">{importPreview.length} 个节点</Badge>
                  </LayerCard.Secondary>
                  <LayerCard.Primary className="min-h-0 flex-1 overflow-y-auto p-0 scrollbar-thin">
                    <div className="flex min-h-full flex-col divide-y divide-kumo-line">
                      {importPreview.map((node, index) => (
                        <div key={`${node.name}-${index}`} className="grid min-h-14 min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-4 py-2.5 text-xs">
                          <div className="min-w-0">
                            <div className="flex min-w-0 items-center gap-2">
                              <NodeFlag node={node} />
                              <span className="truncate font-semibold text-kumo-strong">{node.name || '未命名节点'}</span>
                            </div>
                            <div className="mt-1 truncate font-mono text-[11px] text-kumo-subtle">{node.server || '-'}:{node.port || '-'}</div>
                          </div>
                          <Badge variant={nodeTypeBadgeVariant(node.type)} className="shrink-0 uppercase">{node.type || '-'}</Badge>
                        </div>
                      ))}
                      {importPreview.length === 0 && (
                        <div className="flex flex-1 items-center justify-center px-6 py-10 text-center text-xs text-kumo-subtle">预览后显示解析出的节点。</div>
                      )}
                    </div>
                  </LayerCard.Primary>
                </LayerCard>
              </div>
            </div>

            <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5 cq-sm:justify-end">
              <Button size="sm" variant="secondary" onClick={previewImport}>预览</Button>
              <div className="flex items-center gap-2">
                <Button size="sm" variant="secondary" onClick={() => commitImport(true)}>
                  <Download className="h-3.5 w-3.5" />
                  覆盖导入
                </Button>
                <Button size="sm" variant="primary" onClick={() => commitImport(false)}>
                  <Download className="h-3.5 w-3.5" />
                  追加导入
                </Button>
              </div>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={templateModalOpen} onOpenChange={setTemplateModalOpen}>
        <Dialog size="lg" className="@container flex max-h-[min(calc(100dvh-2rem),42rem)] w-[calc(100vw-1rem)] max-w-[calc(100vw-1rem)] flex-col overflow-hidden p-0 cq-sm:w-[min(calc(100vw-3rem),64rem)]">
          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex min-h-14 shrink-0 items-center justify-between gap-4 border-b border-kumo-line bg-kumo-recessed/20 px-3 py-3 cq-sm:px-5 cq-sm:py-3.5">
              <div className="min-w-0">
                <Dialog.Title className="min-w-0 truncate text-base font-semibold text-kumo-strong">
                  {editingTemplateId ? '编辑模板' : '创建模板'}
                </Dialog.Title>
              </div>
              <Dialog.Close
                aria-label="关闭"
                render={(props) => (
                  <Button
                    {...props}
                    type="button"
                    variant="secondary"
                    shape="square"
                    size="sm"
                    icon={<X className="h-3.5 w-3.5" />}
                    aria-label="关闭"
                    className="shrink-0"
                  />
                )}
              />
            </div>

            <div className="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden px-3 py-3 scrollbar-thin cq-sm:px-5 cq-sm:py-4">
              <div className="grid gap-4">
                <div className="grid gap-3 cq-sm:grid-cols-2">
                  <Input size="sm" label="名称" value={templateForm.name} onChange={(e) => setTemplateForm((prev) => ({ ...prev, name: e.target.value }))} />
                  <Select size="sm" label="格式" value={templateForm.format} onValueChange={(value) => setTemplateForm((prev) => ({ ...prev, format: String(value) }))} items={[{ value: 'clash', label: 'Mihomo/Clash YAML' }, { value: 'raw', label: 'Raw URI List' }, { value: 'base64', label: 'Base64 URI List' }]} />
                </div>
                <TemplateCodeEditor label="模板内容" value={templateForm.content} format={templateForm.format} onChange={(content) => setTemplateForm((prev) => ({ ...prev, content }))} />
                <Input size="sm" label="描述" value={templateForm.description} onChange={(e) => setTemplateForm((prev) => ({ ...prev, description: e.target.value }))} />
              </div>
            </div>

            <div className="flex shrink-0 items-center justify-between gap-3 border-t border-kumo-line bg-kumo-recessed/25 px-3 py-3 cq-sm:px-5 cq-sm:justify-end">
              <Dialog.Close render={(props) => <Button size="sm" variant="secondary" {...props}>取消</Button>} />
              <Button size="sm" variant="primary" loading={saving} onClick={saveTemplate}><Save className="h-3.5 w-3.5" />保存模板</Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </PageStack>
  );
}

export default SubscriptionPage;
