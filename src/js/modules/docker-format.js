const EMPTY_PORTS_TEXT = '-';

const hasPortValue = (value) => value !== undefined && value !== null && String(value).trim() !== '';

const splitPortText = (value) => (
  String(value || '')
    .split(',')
    .map(item => item.trim())
    .filter(Boolean)
);

const getPortKey = (value) => (
  String(value || '')
    .trim()
    .toLowerCase()
    .replace(/\s+/g, ' ')
    .replace(/\/tcp$/, '')
);

const uniquePortTexts = (items) => {
  const seen = new Set();
  const result = [];
  for (const item of items) {
    const key = getPortKey(item);
    if (!key || seen.has(key)) continue;
    seen.add(key);
    result.push(item);
  }
  return result;
};

const formatPortObject = (port) => {
  const privatePort = port.PrivatePort ?? port.privatePort ?? port.containerPort;
  const publicPort = port.PublicPort ?? port.publicPort ?? port.hostPort;
  const type = port.Type ?? port.type ?? 'tcp';
  if (hasPortValue(publicPort) && hasPortValue(privatePort)) return `${publicPort}:${privatePort}/${type}`;
  if (hasPortValue(privatePort)) return `${privatePort}/${type}`;
  return '';
};

const formatInspectPortsObject = (ports) => {
  const result = [];
  for (const [containerPortSpec, bindings] of Object.entries(ports || {})) {
    const [containerPort, type = 'tcp'] = String(containerPortSpec).split('/');
    if (Array.isArray(bindings) && bindings.length > 0) {
      bindings.forEach(binding => {
        const hostPort = binding?.HostPort ?? binding?.hostPort;
        if (hasPortValue(hostPort) && hasPortValue(containerPort)) {
          result.push(`${hostPort}:${containerPort}/${type}`);
        }
      });
      continue;
    }
    if (hasPortValue(containerPort)) {
      result.push(`${containerPort}/${type}`);
    }
  }
  return result;
};

export function formatDockerContainerPorts(container = {}) {
  const ports = container?.ports ?? container?.Ports ?? container?.portMappings;
  let formatted = [];

  if (typeof ports === 'string') {
    formatted = splitPortText(ports);
  } else if (Array.isArray(ports)) {
    formatted = ports.flatMap(port => (
      typeof port === 'string' ? splitPortText(port) : [formatPortObject(port)]
    )).filter(Boolean);
  } else if (ports && typeof ports === 'object') {
    formatted = formatInspectPortsObject(ports);
  }

  const unique = uniquePortTexts(formatted);
  return unique.join(', ') || EMPTY_PORTS_TEXT;
}

// 按与 formatDockerContainerPorts 完全相同的顺序/去重规则产出可点击端口项，
// 保证展示文本（label）与 formatDockerContainerPorts 的结果逐项一致。
const getDockerContainerPortItems = (container = {}) => {
  if (typeof container === 'string') {
    return splitPortText(container).map(label => ({ label, hostPort: getHostPortFromText(label) }));
  }

  const ports = container?.ports ?? container?.Ports ?? container?.portMappings;
  let formatted = [];

  if (typeof ports === 'string') {
    formatted = splitPortText(ports);
  } else if (Array.isArray(ports)) {
    formatted = ports.flatMap(port => (
      typeof port === 'string' ? splitPortText(port) : [formatPortObject(port)]
    )).filter(Boolean);
  } else if (ports && typeof ports === 'object') {
    formatted = formatInspectPortsObject(ports);
  }

  return uniquePortTexts(formatted).map(label => ({ label, hostPort: getHostPortFromText(label) }));
};

// 只认 "公网:容器" 形式，冒号前即为可访问的公网端口（Agent 上报格式见
// agent-rust/src/docker.rs 的 ports_vec，无映射时只有容器端口）。
const getHostPortFromText = label => {
  const match = /^(\d+):\d+(\/\w+)?$/.exec(String(label || '').trim());
  return match ? match[1] : '';
};

const stripHostBrackets = host => {
  const text = String(host || '').trim();
  return text.startsWith('[') && text.endsWith(']') ? text.slice(1, -1) : text;
};

const isIPv6Literal = text => text.includes(':');
const hasTrailingPort = text => (text.match(/:/g) || []).length === 1 && /:\d+$/.test(text);

const isLinkableHost = host => {
  const text = stripHostBrackets(host);
  if (!text || text === '-' || text === '0.0.0.0' || text === '::') return false;
  if (/[\s/?#]/.test(text)) return false;
  // 主机地址可能带端口（如 1.2.3.4:22），拼出来的容器端口链接必然歧义，直接不给链接。
  return !hasTrailingPort(text);
};

export function buildDockerContainerPortUrl(host, portLabel, protocol = 'http') {
  if (!isLinkableHost(host)) return '';
  const hostPort = getHostPortFromText(portLabel);
  if (!hostPort) return '';
  const rawHost = stripHostBrackets(host);
  const hostForUrl = isIPv6Literal(rawHost) ? `[${rawHost}]` : rawHost;
  return `${protocol}://${hostForUrl}:${hostPort}`;
}

export function getDockerContainerPortLinks(container = {}, host = '') {
  return getDockerContainerPortItems(container).map(item => ({
    ...item,
    url: buildDockerContainerPortUrl(host, item.label),
  }));
}
