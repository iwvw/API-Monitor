/**
 * Capability helpers for server transports.
 *
 * Monitoring, terminal and file management do not always use the same channel.
 * Agent can be offline while SSH is still usable, so callers should use these
 * capability fields instead of deriving actions from `status`.
 */

const AGENT_PLACEHOLDER_HOSTS = new Set(['0.0.0.0']);

function normalizeString(value) {
  return typeof value === 'string' ? value.trim() : '';
}

function normalizePort(value) {
  const port = Number(value || 22);
  return Number.isFinite(port) && port > 0 && port <= 65535 ? port : null;
}

function hasSshEndpoint(server = {}) {
  const host = normalizeString(server.host);
  return Boolean(host && !AGENT_PLACEHOLDER_HOSTS.has(host) && normalizePort(server.port));
}

function hasSshCredential(server = {}) {
  const authType = server.auth_type || 'password';
  if (authType === 'key') return Boolean(normalizeString(server.private_key));
  return Boolean(normalizeString(server.password));
}

function hasSshConfig(server = {}) {
  return Boolean(
    hasSshEndpoint(server) &&
    normalizeString(server.username) &&
    normalizeString(server.auth_type || 'password') &&
    hasSshCredential(server)
  );
}

function getServerCapabilities(server = {}, options = {}) {
  const agentOnline = Boolean(options.agentOnline);
  const sshConfigured = hasSshConfig(server);
  const terminalTransports = [];
  const fileTransports = [];

  if (agentOnline) {
    terminalTransports.push('agent');
    fileTransports.push('agent');
  }

  if (sshConfigured) {
    terminalTransports.push('ssh');
    fileTransports.push('sftp');
  }

  return {
    agent_online: agentOnline,
    ssh_configured: sshConfigured,
    terminal_transports: terminalTransports,
    preferred_terminal_transport: terminalTransports[0] || null,
    file_transports: fileTransports,
  };
}

module.exports = {
  hasSshEndpoint,
  hasSshCredential,
  hasSshConfig,
  getServerCapabilities,
};
