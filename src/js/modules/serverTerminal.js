const AGENT_PLACEHOLDER_HOSTS = new Set(['0.0.0.0']);
const TERMINAL_TRANSPORTS = new Set(['agent', 'ssh']);

const normalizeString = (value) => (typeof value === 'string' ? value.trim() : '');

export function hasSshEndpoint(server = {}) {
  const host = normalizeString(server.host);
  const username = normalizeString(server.username);
  const port = Number(server.port || 22);

  return Boolean(
    host &&
    username &&
    !AGENT_PLACEHOLDER_HOSTS.has(host) &&
    Number.isFinite(port) &&
    port > 0 &&
    port <= 65535
  );
}

export function isAgentServer(server = {}) {
  const host = normalizeString(server.host);
  return Boolean(
    server.agent_online === true ||
    AGENT_PLACEHOLDER_HOSTS.has(host) ||
    server.tags?.includes?.('Agent')
  );
}

export function isAgentOnline(server = {}) {
  if (typeof server.agent_online === 'boolean') return server.agent_online;
  if (typeof server.agentOnline === 'boolean') return server.agentOnline;
  if (typeof server.capabilities?.agentOnline === 'boolean') {
    return server.capabilities.agentOnline;
  }

  return Boolean(server.status === 'online' && isAgentServer(server));
}

export function getTerminalTransports(server = {}) {
  if (Array.isArray(server.terminal_transports)) {
    return server.terminal_transports.filter(transport => TERMINAL_TRANSPORTS.has(transport));
  }

  const transports = [];
  if (isAgentOnline(server)) transports.push('agent');
  if (server.ssh_configured === true || hasSshEndpoint(server)) transports.push('ssh');
  return transports;
}

export function resolveTerminalProtocol(server = {}) {
  const transports = getTerminalTransports(server);
  const preferred = server.preferred_terminal_transport;

  if (TERMINAL_TRANSPORTS.has(preferred) && transports.includes(preferred)) {
    return preferred;
  }

  if (isAgentOnline(server) && transports.includes('agent')) return 'agent';
  if (transports.includes('ssh')) return 'ssh';
  return null;
}

export function canOpenTerminal(server = {}) {
  return Boolean(resolveTerminalProtocol(server));
}
