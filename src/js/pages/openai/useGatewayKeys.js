import { useCallback, useMemo, useState } from 'react';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { getAuthHeaders, toLocalDateTimeValue, parseLocalDateTime } from './utils.js';

export function useGatewayKeys() {
  const { confirmPress } = useConfirmPress();
const [gatewayKeys, setGatewayKeys] = useState([]);
const [gatewayKeysLoading, setGatewayKeysLoading] = useState(false);
const [gatewayKeyToggleLoading, setGatewayKeyToggleLoading] = useState({});
const [gatewayKeyDialogOpen, setGatewayKeyDialogOpen] = useState(false);
const [editingGatewayKey, setEditingGatewayKey] = useState(null);
const [gatewayKeyForm, setGatewayKeyForm] = useState({
  name: '',
  expiresAt: '',
  allowedModels: [],
  allowedEndpoints: [],
  maxTokensQuota: '',
});
const [gatewayKeyAdvancedOpen, setGatewayKeyAdvancedOpen] = useState(false);
const [gatewayKeyFormError, setGatewayKeyFormError] = useState('');
const [gatewayKeySaving, setGatewayKeySaving] = useState(false);
const [newGatewayKey, setNewGatewayKey] = useState(null);

const loadGatewayKeys = useCallback(async () => {
  setGatewayKeysLoading(true);
  try {
    const response = await fetch('/api/openai/keys', { headers: getAuthHeaders() });
    const data = await response.json().catch(() => []);
    if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
    setGatewayKeys(Array.isArray(data) ? data : []);
  } catch (error) {
    toast.error('加载网关密钥失败: ' + error.message);
  } finally {
    setGatewayKeysLoading(false);
  }
}, [getAuthHeaders]);
const defaultGatewayKey = useMemo(
  () => gatewayKeys.find(key => key.isDefault) || gatewayKeys[0] || null,
  [gatewayKeys]
);
const openAddGatewayKeyModal = () => {
  setEditingGatewayKey(null);
  setGatewayKeyForm({
    name: '',
    expiresAt: '',
    allowedModels: [],
    allowedEndpoints: [],
    maxTokensQuota: '',
  });
  setGatewayKeyFormError('');
  setGatewayKeyDialogOpen(true);
};

const openEditGatewayKeyModal = key => {
  setEditingGatewayKey(key);
  setGatewayKeyForm({
    name: key.name || '',
    expiresAt: key.expiresAt ? toLocalDateTimeValue(new Date(key.expiresAt)) : '',
    allowedModels: Array.isArray(key.allowedModels) ? key.allowedModels : [],
    allowedEndpoints: Array.isArray(key.allowedEndpoints) ? key.allowedEndpoints : [],
    maxTokensQuota: key.maxTokensQuota ? String(key.maxTokensQuota) : '',
  });
  setGatewayKeyFormError('');
  setGatewayKeyDialogOpen(true);
};

const normalizeGatewayKeyForm = () => ({
  name: gatewayKeyForm.name.trim(),
  expiresAt: gatewayKeyForm.expiresAt ? new Date(gatewayKeyForm.expiresAt).toISOString() : '',
  allowedModels: Array.isArray(gatewayKeyForm.allowedModels)
    ? gatewayKeyForm.allowedModels
    : [],
  allowedEndpoints: Array.isArray(gatewayKeyForm.allowedEndpoints)
    ? gatewayKeyForm.allowedEndpoints
    : [],
  maxTokensQuota: gatewayKeyForm.maxTokensQuota
    ? Number(gatewayKeyForm.maxTokensQuota)
    : 0,
});

// 白名单列表项勾选/取消（模型与端点共用，下拉多选）。
const toggleGatewayKeyListItem = (field, value, checked) => {
  setGatewayKeyForm(current => {
    const list = Array.isArray(current[field]) ? current[field] : [];
    const next = checked ? (list.includes(value) ? list : [...list, value]) : list.filter(item => item !== value);
    return { ...current, [field]: next };
  });
};

const removeGatewayKeyListItem = (field, value) => {
  setGatewayKeyForm(current => ({
    ...current,
    [field]: (Array.isArray(current[field]) ? current[field] : []).filter(item => item !== value),
  }));
};

// 过期时间预设：相对当前时间 +N 天，保留当天剩余时刻（23:59 或当前时分）。
const applyGatewayKeyExpiryPreset = days => {
  setGatewayKeyForm(current => {
    if (!days) {
      return { ...current, expiresAt: '' };
    }
    const existing = parseLocalDateTime(current.expiresAt);
    const next = new Date(Date.now() + days * 24 * 60 * 60 * 1000);
    if (existing) {
      next.setHours(existing.getHours(), existing.getMinutes(), 0, 0);
    } else {
      next.setHours(23, 59, 0, 0);
    }
    return { ...current, expiresAt: toLocalDateTimeValue(next) };
  });
};

const updateGatewayKeyExpiryDate = date => {
  if (!date) return;
  setGatewayKeyForm(current => {
    const existing = parseLocalDateTime(current.expiresAt);
    const next = new Date(date);
    next.setHours(existing?.getHours() ?? 23, existing?.getMinutes() ?? 59, 0, 0);
    return { ...current, expiresAt: toLocalDateTimeValue(next) };
  });
};

const updateGatewayKeyExpiryTime = (part, value) => {
  setGatewayKeyForm(current => {
    const next = parseLocalDateTime(current.expiresAt);
    if (!next) return current;
    if (part === 'hour') next.setHours(Number(value));
    if (part === 'minute') next.setMinutes(Number(value));
    return { ...current, expiresAt: toLocalDateTimeValue(next) };
  });
};

const saveGatewayKey = async () => {
  const payload = normalizeGatewayKeyForm();
  if (!payload.name) {
    setGatewayKeyFormError('请填写密钥名称');
    return;
  }
  setGatewayKeySaving(true);
  setGatewayKeyFormError('');
  try {
    const response = await fetch(
      editingGatewayKey ? `/api/openai/keys/${editingGatewayKey.id}` : '/api/openai/keys',
      {
        method: editingGatewayKey ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      }
    );
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || '保存失败');
    setGatewayKeyDialogOpen(false);
    if (data.apiKey) {
      setNewGatewayKey({ name: payload.name, apiKey: data.apiKey });
    }
    toast.success(editingGatewayKey ? '密钥已更新' : '密钥已创建');
    await loadGatewayKeys();
  } catch (error) {
    setGatewayKeyFormError(error.message);
  } finally {
    setGatewayKeySaving(false);
  }
};

const toggleGatewayKey = async key => {
  if (gatewayKeyToggleLoading[key.id]) return;
  const nextEnabled = !key.enabled;
  setGatewayKeyToggleLoading(prev => ({ ...prev, [key.id]: true }));
  try {
    const response = await fetch(`/api/openai/keys/${key.id}/toggle`, {
      method: 'POST',
      headers: getAuthHeaders(),
      body: JSON.stringify({ enabled: nextEnabled }),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
    const confirmedEnabled = Boolean(data.enabled);
    setGatewayKeys(prev =>
      prev.map(item => (item.id === key.id ? { ...item, enabled: confirmedEnabled } : item))
    );
    toast.success(confirmedEnabled ? `${key.name} 已启用` : `${key.name} 已停用`);
  } catch (error) {
    toast.error('更新密钥状态失败: ' + error.message);
  } finally {
    setGatewayKeyToggleLoading(prev => ({ ...prev, [key.id]: false }));
  }
};

const setDefaultGatewayKey = async key => {
  try {
    const response = await fetch(`/api/openai/keys/${key.id}/default`, {
      method: 'PUT',
      headers: getAuthHeaders(),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || '设置默认密钥失败');
    toast.success(`已将 "${key.name}" 设为默认密钥`);
    await loadGatewayKeys();
  } catch (error) {
    toast.error('设置默认密钥失败: ' + error.message);
  }
};

const rotateGatewayKey = async key => {
  if (!(await dialog.confirm(`确认轮换 "${key.name}"？旧密钥会立即失效。`))) return;
  try {
    const response = await fetch(`/api/openai/keys/${key.id}/rotate`, {
      method: 'POST',
      headers: getAuthHeaders(),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || '轮换失败');
    setNewGatewayKey({ name: key.name, apiKey: data.apiKey });
    toast.success('密钥已轮换');
    await loadGatewayKeys();
  } catch (error) {
    toast.error('轮换密钥失败: ' + error.message);
  }
};

const deleteGatewayKey = async key => {
  if (!confirmPress(`gateway-key-${key.id}`, `删除网关密钥「${key.name}」`)) return;
  try {
    const response = await fetch(`/api/openai/keys/${key.id}`, {
      method: 'DELETE',
      headers: getAuthHeaders(),
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || !data.success) throw new Error(data.error || '删除失败');
    toast.success('密钥已删除');
    await loadGatewayKeys();
  } catch (error) {
    toast.error('删除密钥失败: ' + error.message);
  }
};
  return {
    gatewayKeys,
    gatewayKeysLoading,
    gatewayKeyToggleLoading,
    gatewayKeyDialogOpen, setGatewayKeyDialogOpen,
    editingGatewayKey, setEditingGatewayKey,
    gatewayKeyForm, setGatewayKeyForm,
    gatewayKeyAdvancedOpen, setGatewayKeyAdvancedOpen,
    gatewayKeyFormError, setGatewayKeyFormError,
    gatewayKeySaving,
    newGatewayKey, setNewGatewayKey,
    loadGatewayKeys,
    defaultGatewayKey,
    openAddGatewayKeyModal,
    openEditGatewayKeyModal,
    applyGatewayKeyExpiryPreset,
    updateGatewayKeyExpiryDate,
    updateGatewayKeyExpiryTime,
    toggleGatewayKeyListItem,
    removeGatewayKeyListItem,
    saveGatewayKey,
    toggleGatewayKey,
    setDefaultGatewayKey,
    rotateGatewayKey,
    deleteGatewayKey,
  };
}
