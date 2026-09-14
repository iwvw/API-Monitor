export const emptyAccountForm = {
  name: '',
  serviceAccountJson: '',
  defaultProjectId: '',
  description: '',
};

export const emptyCreateForm = {
  name: '',
  zone: '',
  machineType: '',
  image: '',
  bootDiskSizeGb: '',
  network: '',
  subnetwork: '',
};

export const emptyFirewallForm = {
  name: '',
  description: '',
  direction: 'INGRESS',
  priority: 1000,
  action: 'allow',
  sourceRanges: '',
  destinationRanges: '',
  protocol: 'tcp',
  ports: '',
  network: '',
  disabled: false,
};

export const emptyBucketForm = { name: '', location: '', storageClass: 'STANDARD' };

export const stateOptions = [
  { value: 'all', label: '全部状态' },
  { value: 'RUNNING', label: '运行中' },
  { value: 'TERMINATED', label: '已停止' },
  { value: 'STOPPING', label: '停止中' },
  { value: 'STARTING', label: '启动中' },
  { value: 'PROVISIONING', label: '创建中' },
  { value: 'STAGING', label: '部署中' },
];

export const INSTANCE_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 200, minWidth: 160 },
  { id: 'status', role: 'status' },
  { id: 'publicIp', role: 'identifier', width: 180, minWidth: 150 },
  { id: 'spec', role: 'meta', width: 180 },
  { id: 'zone', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'createdAt', role: 'datetime' },
  { id: 'actions', role: 'actions-lg', width: 150 },
];
export const DISK_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 200, minWidth: 160 },
  { id: 'zone', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'type', role: 'meta', width: 120 },
  { id: 'sizeGb', role: 'number', width: 88 },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-lg', width: 150 },
];
export const ACCOUNT_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'clientEmail', role: 'identifier' },
  { id: 'defaultProjectId', role: 'meta', grow: 1, minWidth: 160 },
  { id: 'status', role: 'status' },
  { id: 'actions', role: 'actions-lg', width: 160, maxWidth: 200 },
];
export const FIREWALL_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', minWidth: 180 },
  { id: 'direction', role: 'meta', width: 100 },
  { id: 'action', role: 'status' },
  { id: 'priority', role: 'number', width: 90 },
  { id: 'network', role: 'meta', width: 140 },
  { id: 'actions', role: 'actions-lg', width: 130 },
];
export const ADDRESS_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'address', role: 'identifier' },
  { id: 'region', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'status', role: 'status' },
];
export const BUCKET_TABLE_COLUMNS = [
  { id: 'name', role: 'primary' },
  { id: 'location', role: 'meta', width: 140 },
  { id: 'storageClass', role: 'meta', width: 120 },
  { id: 'timeCreated', role: 'datetime' },
];
export const OBJECT_TABLE_COLUMNS = [
  { id: 'select', role: 'meta', width: 40 },
  { id: 'name', role: 'primary' },
  { id: 'size', role: 'number', width: 100 },
  { id: 'type', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'updated', role: 'datetime' },
  { id: 'actions', role: 'actions-sm', width: 120 },
];
export const BILLING_ACCOUNT_TABLE_COLUMNS = [
  { id: 'name', role: 'meta', width: 260 },
  { id: 'displayName', role: 'primary', grow: 1, minWidth: 160 },
  { id: 'open', role: 'status' },
];
export const BUDGET_TABLE_COLUMNS = [
  { id: 'displayName', role: 'primary', minWidth: 160 },
  { id: 'amount', role: 'meta', width: 130 },
  { id: 'thresholds', role: 'meta', width: 200 },
];
export const MODEL_USAGE_TABLE_COLUMNS = [
  { id: 'model', role: 'primary' },
  { id: 'count', role: 'number', width: 140 },
];

export const CACHE_TTL_MS = {
  accounts: 30_000,
  projects: 5 * 60_000,
  instances: 45_000,
  disks: 60_000,
  firewalls: 5 * 60_000,
  addresses: 5 * 60_000,
  buckets: 60_000,
  objects: 30_000,
  billing: 10 * 60_000,
};

export const MODEL_USAGE_TTL_MS = 5 * 60_000;
