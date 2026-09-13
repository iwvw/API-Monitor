export const emptyAccountForm = {
  name: '',
  tenancyOcid: '',
  userOcid: '',
  fingerprint: '',
  region: 'ap-tokyo-1',
  privateKeyPem: '',
  passphrase: '',
  defaultCompartmentId: '',
  description: '',
};

export const emptyResizeForm = {
  shape: '',
  ocpuCount: '',
  memoryGb: '',
  baselineOcpuUtilization: '',
  avoidDowntime: false,
};

export const stateOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'RUNNING', label: '运行中' },
  { value: 'STOPPED', label: '已停止' },
  { value: 'PROVISIONING', label: '创建中' },
  { value: 'TERMINATED', label: '已终止' },
];

export const INSTANCE_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'status', role: 'status' },
  { id: 'publicIp', role: 'identifier' },
  { id: 'shape', role: 'meta', grow: 1, minWidth: 160 },
  { id: 'ocpu', role: 'number', width: 88 },
  { id: 'memory', role: 'number', width: 88 },
  { id: 'createdAt', role: 'datetime' },
];

export const ACCOUNT_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'region', role: 'meta', grow: 1, minWidth: 160 },
  { id: 'compartment', role: 'identifier' },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-lg', width: 160, maxWidth: 200 },
];

export const CACHE_TTL_MS = {
  accounts: 30_000,
  compartments: 5 * 60_000,
  instances: 45_000,
  detail: 45_000,
  shapes: 5 * 60_000,
  cost: 10 * 60_000,
};

export const ADVANCED_INSTANCE_ACTIONS = [
  { value: 'SOFTSTOP', label: '软停止' },
  { value: 'RESET', label: '强制重启' },
  { value: 'SOFTRESET', label: '软重启' },
  { value: 'REBOOTMIGRATE', label: '迁移重启' },
];
