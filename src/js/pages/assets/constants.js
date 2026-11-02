export const ASSETS_API = '/api/assets';

export const CATEGORIES = [
  {
    value: 'physical',
    label: '实体资产',
    types: [
      { value: 'server', label: '服务器 / 主机' },
      { value: 'network', label: '网络设备' },
      { value: 'storage', label: '存储' },
      { value: 'terminal', label: '终端' },
      { value: 'other_hw', label: '其他硬件' },
    ],
  },
  {
    value: 'virtual',
    label: '虚拟资产',
    types: [
      { value: 'cloud_instance', label: '云资源实例' },
      { value: 'domain', label: '域名' },
      { value: 'ssl_cert', label: 'SSL 证书' },
      { value: 'subscription', label: '订阅 / 套餐' },
      { value: 'license', label: '许可证' },
      { value: 'api_key', label: 'API Key / 凭据' },
      { value: 'proxy_node', label: '代理节点' },
      { value: 'saas', label: 'SaaS' },
    ],
  },
];

export const CATEGORY_LABEL = {
  physical: '实体',
  virtual: '虚拟',
};

export const TYPE_LABEL = CATEGORIES.reduce((acc, category) => {
  category.types.forEach(type => {
    acc[type.value] = type.label;
  });
  return acc;
}, {});

export const TYPE_BY_CATEGORY = CATEGORIES.reduce((acc, category) => {
  acc[category.value] = category.types;
  return acc;
}, {});

export const STATUS_META = {
  active: { label: '正常', tone: 'info' },
  expiring: { label: '即将到期', tone: 'warning' },
  expired: { label: '已过期', tone: 'danger' },
  retired: { label: '已退役', tone: 'neutral' },
  orphan: { label: '来源失效', tone: 'neutral' },
  unknown: { label: '无到期信息', tone: 'neutral' },
};

export const PERSISTED_STATUSES = [
  { value: 'active', label: '正常' },
  { value: 'retired', label: '已退役' },
  { value: 'orphan', label: '来源失效' },
  { value: 'unknown', label: '未知' },
];

export const COST_CYCLES = [
  { value: 'monthly', label: '每月' },
  { value: 'quarterly', label: '每季度' },
  { value: 'yearly', label: '每年' },
  { value: 'one_time', label: '一次性' },
  { value: 'usage', label: '按用量' },
];

export const COST_CYCLE_LABEL = COST_CYCLES.reduce((acc, item) => {
  acc[item.value] = item.label;
  return acc;
}, {});

export const CURRENCY_OPTIONS = [
  { value: 'CNY', label: 'CNY 人民币' },
  { value: 'USD', label: 'USD 美元' },
  { value: 'EUR', label: 'EUR 欧元' },
  { value: 'JPY', label: 'JPY 日元' },
  { value: 'HKD', label: 'HKD 港币' },
  { value: 'GBP', label: 'GBP 英镑' },
];

export const EXPIRING_PRESETS = [
  { value: '7', label: '7 天内' },
  { value: '30', label: '30 天内' },
  { value: '90', label: '90 天内' },
];

export const BUCKET_LABEL = {
  expired: '已过期',
  within_7: '7 天内',
  within_30: '30 天内',
  normal: '正常',
  no_renew: '不续费',
};

// 纳管来源模块的中文名。与后端 sources.go 的 sourceGroup.label 保持一致，
// 避免在列表里直接暴露 server_accounts 这类表名。
export const SOURCE_MODULE_LABEL = {
  server_accounts: '主机实例',
  subscription_subscriptions: '订阅套餐',
  uptime_monitor_states: 'SSL 证书',
  openai_gateway_keys: '模型网关 Key',
  api_access_keys: '集中访问密钥',
  aiagent_tokens: 'Agent 令牌',
  managed_proxy_nodes: '托管代理节点',
};

export const sourceModuleLabel = module => SOURCE_MODULE_LABEL[module] || module || '来源';

export const BUCKET_TONE = {
  expired: 'danger',
  within_7: 'danger',
  within_30: 'warning',
  normal: 'info',
  no_renew: 'neutral',
};

// 成本列不用 number 角色：number 的列宽上限 144px（ideal 112 / max 144），而成本
// 渲染的是「币种 + 金额 + 周期」整串（如 "USD 12,345.67 / 每年"）。实测该串在
// 13px 字体下约 130px，加 Kumo 单元格左右各 12px 内边距共需 ~154px；金额上到
// 百万级（"CNY 1,234,567.89 / 每季度"）约 161px，需 ~185px。用 number 角色会
// 截断成 "CNY 27.19 /…"（截图即为此现象）。
// 这里改为弹性列：minWidth 168 保证常见金额完整显示，grow 1 让它在宽屏下随容器
// 继续变宽（表格总宽不足时仍靠外层横向滚动兜底），不写死上限以免大额被截。
export const ASSET_COLUMNS = [
  { id: 'name', role: 'primary', minWidth: 200, maxWidth: 320, grow: 3 },
  { id: 'provider', role: 'meta', minWidth: 140, maxWidth: 220, grow: 2 },
  { id: 'status', role: 'status' },
  { id: 'expire', role: 'date' },
  { id: 'days', role: 'status' },
  { id: 'cost', role: 'content', minWidth: 168, maxWidth: null, grow: 1, align: 'right' },
  { id: 'tags', role: 'content', minWidth: 160, maxWidth: 260, grow: 2 },
  // 操作列用 actions-lg（144）而非 actions-md（120）：纳管且填了链接的资产会有
  // 4 个按钮（打开/刷新来源/编辑/删除），实测需要 106px 内容宽 + 左右各 12px
  // 内边距 = 130px，120 的列宽只有 96px 可用，按钮会被裁掉最后一个。
  { id: 'actions', role: 'actions-lg' },
];

export const TABLE_SUMMARY_COLUMNS = [
  { id: 'name', role: 'primary', minWidth: 160, maxWidth: 280, grow: 3 },
  { id: 'status', role: 'status' },
  { id: 'expire', role: 'date' },
  { id: 'days', role: 'status' },
  // 同 ASSET_COLUMNS 的成本列：maxWidth 220 对「币种+金额+周期」整串仍偏窄
  // （百万级金额约需 185px），去掉上限让它随容器增长。
  { id: 'cost', role: 'content', minWidth: 168, maxWidth: null, grow: 2 },
];

export const PAGE_SIZE = 100;
