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
  return unique.length > 0 ? unique.join(', ') : EMPTY_PORTS_TEXT;
}
