const DANGEROUS_PATTERNS = [
  { pattern: /\brm\s+-[^\n;|&]*r[^\n;|&]*f\b/i, reason: '递归强制删除文件' },
  { pattern: /\bdd\s+if=.*\bof=/i, reason: '直接写入磁盘或块设备' },
  { pattern: /\bmkfs(?:\.[a-z0-9]+)?\b/i, reason: '格式化文件系统' },
  { pattern: /\bshutdown\b|\breboot\b|\bpoweroff\b|\bhalt\b/i, reason: '重启或关闭主机' },
  { pattern: /\bdocker\s+(?:system\s+prune|rm|rmi|volume\s+rm)\b/i, reason: '删除 Docker 资源' },
  { pattern: /\bkubectl\s+delete\b/i, reason: '删除 Kubernetes 资源' },
  { pattern: /\bDROP\s+(?:DATABASE|TABLE)\b/i, reason: '删除数据库对象' },
  { pattern: /\bRemove-Item\b[^\n;|]*\s-(?:Recurse|r)\b/i, reason: 'PowerShell 递归删除' },
  { pattern: /\bStop-Computer\b|\bRestart-Computer\b/i, reason: '重启或关闭 Windows 主机' },
];

const DEFAULT_VARIABLES = {
  date: () => new Date().toISOString().slice(0, 10),
  datetime: () => new Date().toISOString(),
};

function normalizeList(value) {
  if (Array.isArray(value)) return value.map(item => String(item).trim()).filter(Boolean);
  if (typeof value !== 'string') return [];
  const trimmed = value.trim();
  if (!trimmed) return [];
  try {
    const parsed = JSON.parse(trimmed);
    if (Array.isArray(parsed)) return normalizeList(parsed);
  } catch {
    // fall through to comma parsing
  }
  return trimmed.split(',').map(item => item.trim()).filter(Boolean);
}

function serializeList(value) {
  return JSON.stringify(normalizeList(value));
}

function detectDangerousCommand(command) {
  const text = String(command || '');
  const matches = DANGEROUS_PATTERNS
    .filter(item => item.pattern.test(text))
    .map(item => item.reason);

  return {
    dangerous: matches.length > 0,
    reasons: Array.from(new Set(matches)),
  };
}

function buildCommandVariables(server = {}, extra = {}) {
  const variables = {
    host: server.host || '',
    name: server.name || '',
    port: server.port || 22,
    username: server.username || '',
    cwd: extra.cwd || '',
  };

  for (const [key, getValue] of Object.entries(DEFAULT_VARIABLES)) {
    variables[key] = getValue();
  }

  for (const [key, value] of Object.entries(extra || {})) {
    if (value !== undefined && value !== null) variables[key] = value;
  }

  return variables;
}

function renderCommandTemplate(command, variables = {}) {
  return String(command || '').replace(/\{([a-zA-Z0-9_]+)\}/g, (match, key) => {
    if (!Object.prototype.hasOwnProperty.call(variables, key)) return match;
    return String(variables[key] ?? '');
  });
}

function normalizeSnippet(row = {}) {
  const detection = detectDangerousCommand(row.content || row.command || '');
  return {
    ...row,
    tags: normalizeList(row.tags),
    favorite: Boolean(row.favorite),
    is_builtin: Boolean(row.is_builtin),
    run_count: Number(row.run_count || 0),
    dangerous: detection.dangerous,
    dangerous_reasons: detection.reasons,
  };
}

module.exports = {
  buildCommandVariables,
  detectDangerousCommand,
  normalizeList,
  normalizeSnippet,
  renderCommandTemplate,
  serializeList,
};
