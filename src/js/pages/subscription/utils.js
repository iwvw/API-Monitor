import { toast } from '../../modules/toast.js';
import { TRAFFIC_UNITS, TUNNEL_STATUS_META } from './constants.js';

export const tunnelStatusMeta = (applyStatus) => TUNNEL_STATUS_META[applyStatus] || { variant: 'warning', label: '部署中' };

export const getInstanceCountryCode = (server) => {
  const direct = String(server?.country_code || server?.countryCode || server?.resolved_country || '').trim();
  if (/^[a-z]{2}$/i.test(direct)) return direct.toUpperCase();
  const location = String(server?.location || '').trim();
  if (/^[a-z]{2}$/i.test(location)) return location.toUpperCase();
  const known = { singapore: 'SG', japan: 'JP', germany: 'DE', france: 'FR', 'hong kong': 'HK', london: 'GB' };
  return known[location.toLowerCase()] || '';
};

export const getInstanceLocationLabel = (server) => getInstanceCountryCode(server) || String(server?.location || '—');

export const countryFlagEmoji = (countryCode) => {
  const code = String(countryCode || '').trim().toUpperCase();
  if (!/^[A-Z]{2}$/.test(code)) return '';
  return String.fromCodePoint(...code.split('').map((letter) => 127397 + letter.charCodeAt(0)));
};

export const formatInstanceUptime = (value) => {
  const text = String(value || '').trim();
  if (!text) return '-';
  const dayMatch = text.match(/(\d+)\s*(?:d|天)/i);
  if (dayMatch) return `${dayMatch[1]}天`;
  return /(?:h|m|s|时|分|秒)/i.test(text) ? '0天' : text;
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

export const parseNodeUrlToConfig = (urlStr) => {
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

export const buildNodeUrl = (config) => {
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

export const syncNodeForm = (prev, changedField, value) => {
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

export const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

export const formatBytes = (bytes = 0) => {
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

export const trafficUnitBytes = (unit) => TRAFFIC_UNITS.find((item) => item.value === unit)?.bytes || TRAFFIC_UNITS[0].bytes;

export const preferredTrafficUnit = (bytes) => {
  const value = Number(bytes) || 0;
  const tbBytes = trafficUnitBytes('TB');
  if (value >= tbBytes) return 'TB';
  return 'GB';
};

export const trafficDisplayValue = (bytes, unit) => {
  const value = Number(bytes) || 0;
  if (value <= 0) return '0';
  const converted = value / trafficUnitBytes(unit);
  return Number.isInteger(converted) ? String(converted) : String(Number(converted.toFixed(3)));
};

export const formatTime = (value) => {
  if (!value) return '-';
  const date = new Date(String(value).replace(' ', 'T'));
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
};

export const statusLabel = (sub) => {
  if (!sub.enabled) return ['停用', 'neutral'];
  if (sub.plan_enabled === false) return ['套餐停用', 'error'];
  if (sub.traffic?.status === 'expired') return ['已过期', 'error'];
  if (sub.traffic?.status === 'exhausted') return ['流量用尽', 'warning'];
  return ['运行中', 'success'];
};

export const meteringLabel = (status) => {
  if (status === 'available') return '内部节点精准计量';
  if (status === 'unavailable') return '外部节点不可计量';
  return '等待节点计量同步';
};

export const managedNodeState = (node) => {
	if (node.publishable) return ['可发布', 'success'];
	if (node.apply_status === 'runtime_running_unreachable') return ['公网不可达', 'error'];
	if (node.apply_status === 'drifted') return ['状态漂移', 'error'];
	if (node.apply_status === 'remove_failed') return ['卸载失败', 'error'];
	if (node.apply_status === 'failed') return ['部署失败', 'error'];
	if (node.apply_status === 'stopped') return ['已停用', 'neutral'];
	return ['同步中', 'warning'];
};

export const normalizeManagedServer = (server) => {
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

export const isLinuxManagedServer = (server) => {
  const platform = `${server?.platform || ''} ${server?.platform_version || ''}`.trim().toLowerCase();
  if (!platform) return true;
  if (['windows', 'darwin', 'macos', 'freebsd'].some((marker) => platform.includes(marker))) return false;
  return ['linux', 'ubuntu', 'debian', 'centos', 'rhel', 'red hat', 'fedora', 'rocky', 'alma', 'alpine', 'arch', 'opensuse', 'sles', 'oracle linux', 'amzn', 'amazon linux']
    .some((marker) => platform.includes(marker));
};

export const parseNodeConfig = (node) => {
  if (!node?.config_json) return {};
  try {
    return JSON.parse(node.config_json);
  } catch {
    return {};
  }
};

export const nodeEndpoint = (node) => {
  const host = node.server || '-';
  return `${host}:${node.port || '-'}`;
};

export const nodeNetworkTags = (node) => {
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

export const nodeNetworkTagClass = (tag) => {
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

export const nodeCountryCode = (node) => {
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

export const latencyChipClass = (latency) => {
  const value = Number(latency) || 0;
  if (value <= 0) return 'border-kumo-line bg-kumo-recessed/50 text-kumo-subtle';
  if (value <= 120) return 'border-kumo-success/25 bg-kumo-success/10 text-kumo-success';
  if (value <= 260) return 'border-kumo-warning/30 bg-kumo-warning/10 text-kumo-warning';
  return 'border-kumo-danger/25 bg-kumo-danger/10 text-kumo-danger';
};

export const nodeTypeBadgeVariant = (type) => {
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

export const subscriptionURL = (base, sub, format = '') => {
  if (!sub?.public_token) return '';
  const suffix = format ? `?format=${format}` : '';
  return `${base}/sub/${sub.public_token}${suffix}`;
};

export const normalizePublicBase = (configured, fallback = '') => {
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

export const copyText = async (text, message = '已复制') => {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      const textarea = document.createElement('textarea');
      textarea.value = text;
      textarea.setAttribute('readonly', '');
      textarea.style.position = 'fixed';
      textarea.style.opacity = '0';
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand('copy');
      document.body.removeChild(textarea);
    }
    toast.success(message);
  } catch {
    toast.error('复制失败');
  }
};

export const templateLanguage = (format) => (format === 'clash' ? 'yaml' : 'bash');
