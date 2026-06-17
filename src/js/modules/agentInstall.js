export function normalizeAgentInstallOs(osType = 'linux') {
  const value = String(osType || '').trim().toLowerCase();
  if (value === 'win' || value === 'windows' || value === 'powershell') {
    return 'win';
  }
  return 'linux';
}

export function isWindowsAgentInstallOs(osType = 'linux') {
  return normalizeAgentInstallOs(osType) === 'win';
}

export function buildAgentInstallEndpoint({
  baseUrl = '',
  serverId = '',
  agentKey = '',
  protocol = 'https',
  osType = 'linux',
} = {}) {
  const normalizedBaseUrl = String(baseUrl || '').trim().replace(/\/+$/, '');
  if (!normalizedBaseUrl || !serverId || !agentKey) return '';

  const normalizedProtocol = encodeURIComponent(protocol || 'https');
  const normalizedOs = normalizeAgentInstallOs(osType);

  return `${normalizedBaseUrl}/api/server/agent/install/${normalizedOs}/${serverId}/${agentKey}?protocol=${normalizedProtocol}`;
}

export function buildAgentInstallCommand(options = {}) {
  const installUrl = buildAgentInstallEndpoint(options);
  if (!installUrl) return '';

  if (isWindowsAgentInstallOs(options.osType)) {
    return `powershell -c "irm ${installUrl} | iex"`;
  }

  return `curl -fsSL ${installUrl} | bash`;
}

export function getAgentInstallExecutionHint(osType = 'linux') {
  if (isWindowsAgentInstallOs(osType)) {
    return '请在 Windows PowerShell 中执行，不要在 CMD、bash 或 zsh 中运行。';
  }

  return '请在 Linux / macOS 终端中执行，不要在 PowerShell 中运行。';
}
