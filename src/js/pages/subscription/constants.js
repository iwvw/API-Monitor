export const API = '/api/subscription';
export const INTERNAL_API = '/api/server/agent/proxy/nodes';
export const RUNTIME_API = '/api/server/agent/proxy/runtimes';
export const TUNNEL_API = '/api/server/agent/proxy/tunnels';
export const PREFERRED_API = '/api/server/agent/proxy/preferred-addresses';
export const SERVER_INVENTORY_API = '/api/server/s';
export const DEFAULT_EXTERNAL_POOL_ID = 'sub_default_nodes';
export const LOAD_TIMEOUT_MS = 8000;
export const INITIAL_SKELETON_MS = 900;

export const SUBSCRIPTION_LOG_COLUMNS = [
  { id: 'createdAt', role: 'datetime' },
  { id: 'subscription', role: 'primary', minWidth: 176 },
  { id: 'client', role: 'content', minWidth: 200, verticalAlign: 'middle' },
  { id: 'format', role: 'type' },
  { id: 'result', role: 'status' },
  { id: 'nodes', role: 'count' },
  { id: 'traffic', role: 'meta', grow: 1, minWidth: 176, align: 'right' },
];

export const TUNNEL_STATUS_META = {
  running: { variant: 'success', label: '已连接' },
  disconnected: { variant: 'warning', label: '已断开' },
  failed: { variant: 'error', label: '部署失败' },
  cleanup_failed: { variant: 'error', label: '清理失败' },
  removing: { variant: 'neutral', label: '卸载中' },
};

export const SUBSCRIPTION_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'subscription', role: 'primary', minWidth: 176, maxWidth: 220, grow: 0 },
  { id: 'status', role: 'status' },
  { id: 'traffic', role: 'content', grow: 1, minWidth: 200, align: 'left', verticalAlign: 'middle' },
  { id: 'access', role: 'meta', grow: 1, minWidth: 200, align: 'center' },
  { id: 'actions', role: 'actions-lg', width: 208, maxWidth: 220 },
];

export const PLAN_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'plan', role: 'primary', minWidth: 176, maxWidth: 220, grow: 0 },
  { id: 'status', role: 'status' },
  { id: 'quota', role: 'number', grow: 1, minWidth: 176, align: 'center' },
  { id: 'reset', role: 'date', grow: 1, minWidth: 176, align: 'center' },
  { id: 'nodes', role: 'meta', grow: 1, minWidth: 176, align: 'center' },
  { id: 'subscriptions', role: 'count', grow: 1, minWidth: 176, align: 'center' },
  { id: 'actions', role: 'actions-md' },
];

export const NODE_COLUMNS = [
  { id: 'enabled', role: 'control' },
  { id: 'name', role: 'primary', minWidth: 176, maxWidth: 200, grow: 0 },
  { id: 'type', role: 'type', grow: 1, minWidth: 160 },
  { id: 'connection', role: 'content', grow: 1, minWidth: 216, align: 'left', verticalAlign: 'middle' },
  { id: 'host', role: 'meta', grow: 1, minWidth: 200, align: 'left' },
  { id: 'actions', role: 'actions-lg', width: 160, maxWidth: 200 },
];

export const RUNTIME_HOST_COLUMNS = [
  { id: 'check', role: 'check' },
  { id: 'name', role: 'primary', minWidth: 120, width: 120, grow: 0 },
  { id: 'status', role: 'status', grow: 1, minWidth: 150, align: 'center' },
  { id: 'location', role: 'meta', grow: 1, minWidth: 150, align: 'center' },
  { id: 'online', role: 'count', grow: 1, minWidth: 150, align: 'center' },
  { id: 'agentVersion', role: 'meta', grow: 1, minWidth: 150, align: 'center' },
  { id: 'proxy', role: 'content', grow: 1, minWidth: 150, align: 'center', verticalAlign: 'middle' },
  { id: 'nodeType', role: 'type', grow: 1, minWidth: 150 },
  { id: 'actions', role: 'actions-xl', width: 320 },
];

export const emptyInternalNodeForm = { server_id: '', name: '', protocol: 'vless-reality', access_mode: 'direct', preferred_address_id: '', public_host: '', server_name: 'www.cloudflare.com', certificate_pem: '', private_key_pem: '', enabled: true, stable: false };

export const emptySubscriptionForm = {
  plan_id: '',
  name: '',
  remark: '',
  enabled: true,
  template_id: 'builtin_mihomo_default',
};

export const emptyPlanForm = {
  name: '', remark: '', enabled: true, total_bytes: 0, cycle_type: 'monthly', cycle_day: 1,
  rate_limit_enabled: true, rate_limit_per_minute: 30, node_ids: [], selection_mode: 'explicit', include_internal_nodes: true, include_external_nodes: false,
};

export const emptyTemplateForm = {
  name: '',
  format: 'clash',
  content: '',
  description: '',
};

export const emptyNodeForm = {
  name: '',
  type: '',
  server: '',
  port: 0,
  country_code: '',
  location: '',
  tags: '',
  traffic_server_id: '',
  ownership: 'external',
  management: 'unmanaged',
  traffic_reporting: 'unavailable',
  enabled: true,
  stable: false,
  sort_order: 0,
  raw: '',
  config_json: '',
};

export const TRAFFIC_UNITS = [
  { value: 'GB', label: 'GB', bytes: 1024 ** 3 },
  { value: 'TB', label: 'TB', bytes: 1024 ** 4 },
];
