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
});

export const parseTags = text => String(text || '')
  .split(/[,，]/)
  .map(item => item.trim())
  .filter(Boolean);

export const parseWarnDays = text => String(text || '')
  .split(/[,，\s]+/)
  .map(item => Number(item.trim()))
  .filter(day => Number.isFinite(day) && day > 0);

export const formToPayload = form => ({
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
});

export const validateForm = form => {
  if (!form.name.trim()) return '请填写资产名称';
  if (!form.category) return '请选择资产分类';
  if (!form.asset_type) return '请选择资产类型';
  if (form.cost_amount !== '' && !Number.isFinite(Number(form.cost_amount))) {
    return '成本金额必须是数字';
  }
  return '';
};
