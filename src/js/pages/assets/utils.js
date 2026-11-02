import { formatDateTime } from '../../modules/utils.js';
import { COST_CYCLE_LABEL, STATUS_META, TYPE_BY_CATEGORY } from './constants.js';

export const statusMeta = status => STATUS_META[status] || STATUS_META.unknown;

export const formatExpireAt = value => {
  if (!value) return '--';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return String(value);
  return formatDateTime(date, { year: 'numeric', month: '2-digit', day: '2-digit' });
};

export const formatDaysLeft = asset => {
  if (asset.days_left === null || asset.days_left === undefined) return '--';
  const days = Number(asset.days_left);
  if (!Number.isFinite(days)) return '--';
  if (days < 0) return `已过期 ${Math.abs(days)} 天`;
  if (days === 0) return '今天到期';
  return `剩 ${days} 天`;
};

export const daysTone = asset => {
  const status = asset.derived_status;
  if (status === 'expired') return 'danger';
  if (status === 'expiring') {
    const days = Number(asset.days_left);
    if (Number.isFinite(days) && days <= 7) return 'danger';
    return 'warning';
  }
  if (status === 'active') return 'info';
  return 'neutral';
};

export const formatCost = asset => {
  const amount = Number(asset.cost_amount);
  if (!Number.isFinite(amount) || amount <= 0) return '--';
  const currency = asset.cost_currency || '';
  const cycle = COST_CYCLE_LABEL[asset.cost_cycle] || '';
  const value = `${currency} ${amount.toLocaleString()}`.trim();
  return cycle ? `${value} / ${cycle}` : value;
};

export const formatMoney = (amount, currency) => {
  const value = Number(amount);
  if (!Number.isFinite(value)) return '0';
  return `${currency || ''} ${value.toLocaleString()}`.trim();
};

// parseUtcTimestamp 解析后端 CURRENT_TIMESTAMP 产生的无时区字符串
// （YYYY-MM-DD HH:MM:SS，UTC）。直接 new Date(...) 会按浏览器本地时区解释，
// 导致非 UTC 站点显示偏移数小时/跨天。
const parseUtcTimestamp = value => {
  if (!value) return null;
  const text = String(value);
  if (/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/.test(text)) {
    const date = new Date(text.replace(' ', 'T') + 'Z');
    return Number.isNaN(date.getTime()) ? null : date;
  }
  const date = new Date(text);
  return Number.isNaN(date.getTime()) ? null : date;
};

export const formatSyncTime = value => {
  if (!value) return '未同步';
  const date = parseUtcTimestamp(value);
  if (!date) return String(value);
  return formatDateTime(date);
};

export const formatEventTime = value => {
  if (!value) return '';
  const date = parseUtcTimestamp(value);
  if (!date) return String(value);
  return formatDateTime(date);
};

export const toExpireInputValue = value => {
  if (!value) return '';
  const text = String(value);
  return text.length >= 10 ? text.slice(0, 10) : text;
};

export const emptyForm = (category = 'physical') => ({
  category,
  asset_type: (TYPE_BY_CATEGORY[category] || TYPE_BY_CATEGORY.physical)[0].value,
  name: '',
  provider: '',
  owner: '',
  location: '',
  serial_no: '',
  model: '',
  status: 'active',
  acquire_date: '',
  expire_at: '',
  warn_days_text: '',
  auto_renew: false,
  cost_amount: '',
  cost_currency: '',
  cost_cycle: '',
  tags_text: '',
  remark: '',
  url: '',
});

export const assetToForm = asset => ({
  category: asset.category || 'physical',
  asset_type: asset.asset_type || 'server',
  name: asset.name || '',
  provider: asset.provider || '',
  owner: asset.owner || '',
  location: asset.location || '',
  serial_no: asset.serial_no || '',
  model: asset.model || '',
  status: asset.status || 'active',
  acquire_date: asset.acquire_date || '',
  expire_at: toExpireInputValue(asset.expire_at),
  warn_days_text: Array.isArray(asset.warn_days) ? asset.warn_days.join(', ') : '',
  auto_renew: Boolean(asset.auto_renew),
  cost_amount: asset.cost_amount ? String(asset.cost_amount) : '',
  cost_currency: asset.cost_currency || '',
  cost_cycle: asset.cost_cycle || '',
  tags_text: Array.isArray(asset.tags) ? asset.tags.join(', ') : '',
  remark: asset.remark || '',
  url: assetLinkUrl(asset) || '',
});

export const parseTags = text => String(text || '')
  .split(/[,，]/)
  .map(item => item.trim())
  .filter(Boolean);

export const parseWarnDays = text => String(text || '')
  .split(/[,，\s]+/)
  .map(item => Number(item.trim()))
  .filter(day => Number.isFinite(day) && day > 0);

// 资产的外部链接存在 metadata.url（后端 assets 表本就有 metadata_json 列，
// 读写链路已通，无需改库表）。这里统一做三件事：读、规范化、写回时与既有
// metadata 合并。
//
// 合并而非覆盖：编辑表单只回传 url 一个键，若直接覆盖 metadata_json，
// 纳管资产（origin=linked）由来源同步写入的其他元数据会被一并抹掉。
export const ASSET_URL_METADATA_KEY = 'url';

export const assetLinkUrl = asset => {
  const raw = asset?.metadata?.[ASSET_URL_METADATA_KEY];
  return typeof raw === 'string' ? raw.trim() : '';
};

// normalizeAssetUrl 把用户输入补全成可跳转的绝对地址：
// 面板里常见只填 "example.com" 或 "www.example.com"，直接用作 href 会变成
// 相对路径跳到面板自身。这里无协议时补 https://。
//
// 只接受 http/https：其它协议一律判非法返回 null（调用方据此报错）。
// 注意不能只看 "scheme://" 形式——mailto:a@b.com、javascript:alert(1) 这类
// 不带 // 的 scheme 若被当成裸主机名补上 https://，会得到
// "https://mailto:a@b.com" 这种看似合法实则无意义的地址。
// 因此这里先识别 "scheme:" 前缀：不是 http/https 就直接拒绝。
//
// 但 scheme 语法本身与「主机名:端口」同形（RFC 3986 下 "nas.local:8080"
// 也是合法 URI），只按正则判会把 nas.local:8080、server:3000/admin 这类
// 带端口的地址误判成未知协议，提示还写着「需要是 http/https 地址」，
// 用户完全无从修正。区分办法看冒号后面：scheme 后接的是非数字串
// （mailto:a、data:text），端口则是纯数字开头。带 // 的一律按 scheme 处理
// （ftp://a.com/x 没有歧义）。
// 返回空串表示「无链接」，返回 null 表示输入无法识别。
export const normalizeAssetUrl = value => {
  const text = String(value ?? '').trim();
  if (!text) return '';
  const schemeMatch = /^([a-z][a-z0-9+.-]*):(.*)$/is.exec(text);
  const hasScheme = Boolean(
    schemeMatch && (schemeMatch[2].startsWith('//') || !/^\d/.test(schemeMatch[2]))
  );
  if (hasScheme && !/^https?$/i.test(schemeMatch[1])) return null;
  const withScheme = hasScheme ? text : `https://${text}`;
  try {
    const parsed = new URL(withScheme);
    if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') return null;
    if (!parsed.hostname) return null;
    return withScheme;
  } catch {
    return null;
  }
};

export const formToPayload = (form, existingAsset) => {
  const mergedMetadata = { ...(existingAsset?.metadata || {}) };
  const url = normalizeAssetUrl(form.url);
  if (url) mergedMetadata[ASSET_URL_METADATA_KEY] = url;
  // 只在用户主动清空时删键。输入非法（normalizeAssetUrl 返回 null）时保留原值，
  // 免得将来新增调用方漏跑 validateForm 就把已有链接静默抹掉。
  else if (!String(form.url ?? '').trim()) delete mergedMetadata[ASSET_URL_METADATA_KEY];

  return {
    category: form.category,
    asset_type: form.asset_type,
    name: form.name.trim(),
    provider: form.provider.trim(),
    owner: form.owner.trim(),
    location: form.location.trim(),
    serial_no: form.serial_no.trim(),
    model: form.model.trim(),
    status: form.status,
    acquire_date: form.acquire_date,
    // 送裸日期（YYYY-MM-DD），由后端按站点时区解释为当日零点。
    // 前端硬拼 Z 会按 UTC 解释，负时区展示会退回前一天。
    expire_at: form.expire_at || '',
    warn_days: parseWarnDays(form.warn_days_text),
    auto_renew: Boolean(form.auto_renew),
    cost_amount: form.cost_amount === '' ? 0 : Number(form.cost_amount),
    cost_currency: form.cost_currency.trim().toUpperCase(),
    cost_cycle: form.cost_cycle,
    tags: parseTags(form.tags_text),
    remark: form.remark.trim(),
    metadata: mergedMetadata,
  };
};

export const validateForm = form => {
  if (!form.name.trim()) return '请填写资产名称';
  if (!form.category) return '请选择资产分类';
  if (!form.asset_type) return '请选择资产类型';
  if (form.cost_amount !== '' && !Number.isFinite(Number(form.cost_amount))) {
    return '成本金额必须是数字';
  }
  if (String(form.url || '').trim() && normalizeAssetUrl(form.url) === null) {
    return '链接需要是 http/https 地址，例如 https://example.com';
  }
  return '';
};
