export const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

export const unwrap = (result) => result?.data ?? result ?? {};

export const parseOciConfig = (text) => text
  .split(/\r?\n/)
  .reduce((values, rawLine) => {
    const line = rawLine.trim();
    if (!line || line.startsWith('[') || line.startsWith('#') || line.startsWith(';')) return values;
    const separator = line.indexOf('=');
    if (separator < 0) return values;
    const key = line.slice(0, separator).trim().toLowerCase();
    const value = line.slice(separator + 1).replace(/\s+#.*$/, '').trim();
    if (key) values[key] = value;
    return values;
  }, {});

export function downloadJson(filename, payload) {
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

export function parseImportedAccounts(text) {
  const parsed = JSON.parse(text);
  if (Array.isArray(parsed)) return parsed;
  if (Array.isArray(parsed?.accounts)) return parsed.accounts;
  throw new Error('导入内容必须是账号数组或包含 accounts 的对象');
}

export function getOciStatusTone(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (['RUNNING', 'AVAILABLE', 'ACTIVE', 'SUCCESS', 'VALID'].includes(normalized)) return 'success';
  if (['PROVISIONING', 'STARTING', 'STOPPING', 'UPDATING', 'PENDING', 'CREATING'].includes(normalized)) return 'info';
  if (['STOPPED', 'INACTIVE', 'TERMINATED', 'UNKNOWN', 'UNVERIFIED'].includes(normalized)) return 'neutral';
  if (['FAILED', 'FAILURE', 'ERROR', 'INVALID'].includes(normalized)) return 'danger';
  return 'neutral';
}

export function getVerifyStatusLabel(status) {
  const normalized = String(status || '').trim().toUpperCase();
  if (['SUCCESS', 'VALID'].includes(normalized)) return '已验证';
  if (['FAILED', 'FAILURE', 'ERROR', 'INVALID'].includes(normalized)) return '失败';
  if (['UNKNOWN', 'UNVERIFIED', ''].includes(normalized)) return '未验证';
  return status || '未验证';
}

export function parseNumberInput(value) {
  const text = String(value || '').trim();
  if (!text) return null;
  const numeric = Number(text);
  return Number.isFinite(numeric) ? numeric : Number.NaN;
}

export function clampResizeValue(value, min, max, fallback) {
  if (!Number.isFinite(value)) return fallback;
  if (Number.isFinite(min) && value < min) return min;
  if (Number.isFinite(max) && value > max) return max;
  return value;
}

export function formatShapeSummary(shape) {
  if (!shape) return '-';
  if (shape.isFlexible) {
    const ocpu = shape.ocpuOptions?.min && shape.ocpuOptions?.max
      ? `${formatInstanceMetric(shape.ocpuOptions.min)}-${formatInstanceMetric(shape.ocpuOptions.max)} OCPU`
      : `${formatInstanceMetric(shape.ocpuCount)} OCPU`;
    const memory = shape.memoryOptions?.min && shape.memoryOptions?.max
      ? `${formatInstanceMetric(shape.memoryOptions.min)}-${formatInstanceMetric(shape.memoryOptions.max)} GB`
      : `${formatInstanceMetric(shape.memoryGb)} GB`;
    return `${ocpu} / ${memory}`;
  }
  return `${formatInstanceMetric(shape.ocpuCount)} OCPU / ${formatInstanceMetric(shape.memoryGb)} GB`;
}

export function formatBaselineLabel(value) {
  return {
    BASELINE_1_8: '1/8 OCPU 基线',
    BASELINE_1_2: '1/2 OCPU 基线',
    BASELINE_1_1: '1 OCPU 基线',
  }[value] || value;
}

export function resourceColumnSpecs(columns, renderActions) {
  const flexibleColumn = columns.find((column) => ['displayName', 'connectionString', 'volumeId'].includes(column)) || columns[0];
  const specs = columns.map((column) => {
    if (column === flexibleColumn) {
      if (column === 'connectionString') return { id: column, role: 'content' };
      if (column === 'volumeId') return { id: column, role: 'identifier' };
      return { id: column, role: 'primary' };
    }
    if (column === 'state') return { id: column, role: 'status' };
    if (column === 'volumeType') return { id: column, role: 'type' };
    if (column === 'timeCreated') return { id: column, role: 'datetime' };
    if (['privateIp', 'publicIp', 'fingerprint'].includes(column)) {
      return { id: column, role: 'identifier' };
    }
    if (['subnetId', 'attachmentId'].includes(column)) {
      return { id: column, role: 'identifier', minWidth: 200 };
    }
    return { id: column, role: 'meta', grow: 1, minWidth: 160 };
  });
  if (renderActions) specs.push({ id: 'actions', role: 'actions-md' });
  return specs;
}

export function columnLabel(column) {
  return {
    displayName: '名称',
    state: '状态',
    privateIp: '私网 IP',
    publicIp: '公网 IP',
    subnetId: '子网',
    volumeType: '类型',
    device: '设备',
    volumeId: '卷 ID',
    attachmentId: '附加 ID',
    connectionString: '连接串',
    fingerprint: '指纹',
    timeCreated: '创建时间',
  }[column] || column;
}

export function formatInstanceMetric(value) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric) || numeric <= 0) return '-';
  return Number.isInteger(numeric) ? String(numeric) : numeric.toFixed(1).replace(/\.0$/, '');
}

export function formatOciDate(value) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

export function formatCurrency(value) {
  const numeric = Number(value);
  if (!Number.isFinite(numeric)) return '-';
  return numeric.toLocaleString('zh-CN', {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}
