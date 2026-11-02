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
  const transports = [];
  if (Array.isArray(server.terminal_transports)) {
    server.terminal_transports
      .filter(transport => TERMINAL_TRANSPORTS.has(transport))
      .forEach(transport => {
        if (!transports.includes(transport)) transports.push(transport);
      });
  }

  if (isAgentOnline(server) && !transports.includes('agent')) transports.push('agent');
  if (
    (server.ssh_configured === true || server.supports_ssh === true || hasSshEndpoint(server)) &&
    !transports.includes('ssh')
  ) {
    transports.push('ssh');
  }
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

export function resolveTerminalSocketTransport(server = {}, requested = '') {
  const transports = getTerminalTransports(server);
  // Let the backend choose from its live Agent registry when both transports
  // are configured. This avoids retrying a stale Agent route after the Agent
  // disconnects while the page still shows its last online snapshot.
  if (transports.includes('agent') && transports.includes('ssh')) return 'auto';
  if (requested === 'ssh' && transports.includes('ssh')) return 'ssh';
  if (requested === 'agent' && transports.includes('agent')) return 'agent';
  return resolveTerminalProtocol(server) || 'auto';
}

export function canOpenTerminal(server = {}) {
  return Boolean(resolveTerminalProtocol(server));
}

// 终端保活与重连参数。
// 心跳 25s：必须小于后端 terminalPongWait（120s），也要小于常见反向代理的
// 空闲超时（Nginx proxy_read_timeout 默认 60s），否则连接会被中间设备静默切断。
export const TERMINAL_HEARTBEAT_MS = 25000;
export const TERMINAL_RECONNECT_BASE_MS = 1000;
export const TERMINAL_RECONNECT_MAX_MS = 30000;

/**
 * 计算下一次重连等待时长：指数退避 1s → 2s → 4s …… 上限 30s。
 * attempt 从 0 开始（首次重连）。
 */
export function terminalReconnectDelay(attempt = 0) {
  const safeAttempt = Math.max(0, Math.min(Number(attempt) || 0, 5));
  return Math.min(TERMINAL_RECONNECT_BASE_MS * 2 ** safeAttempt, TERMINAL_RECONNECT_MAX_MS);
}

/**
 * WebSocket 是否已经不可用（需要主动重连）。
 * 注意 CONNECTING 不算 dead：正常握手途中不该打断。
 */
export function isTerminalSocketDead(ws) {
  if (!ws) return true;
  return ws.readyState === 3 /* CLOSED */ || ws.readyState === 2 /* CLOSING */;
}

/**
 * 判断 attach 回原 PTY 是否应该放弃。
 *
 * 重连时优先 attach 原 PTY 以保留 shell 上下文（工作目录、环境变量、正在跑的
 * 命令）。但 PTY 可能已经随 shell 退出被回收，此时会一直 attach 失败、无限
 * 重连。约定：连续失败到第 2 次（attempt >= 1）就放弃 attach，退化为新开 shell。
 */
export function shouldDropPtyAttach(attempt, ptyId) {
  return Boolean(ptyId) && Number(attempt) >= 1;
}
