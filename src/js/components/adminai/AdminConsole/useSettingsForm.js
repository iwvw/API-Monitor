import { useEffect, useRef, useState } from 'react';
import { toast } from '../../../modules/toast.js';
import { SETTING_FIELDS } from './constants.jsx';

/* 设置表单状态（吸底保存栏与设置卡共用，确保保存动作始终可达） */
export function useSettingsForm() {
  const [values, setValues] = useState(null);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedAt, setSavedAt] = useState(0);
  const [modelOptions, setModelOptions] = useState([]);

  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/admin-ai/settings');
        const data = await res.json();
        const body = data.data || data;
        setValues(body.settings || {});
      } catch {
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  // 模型下拉选项：只列对外 /v1 暴露的模型（/api/openai/models 与 /v1/models 同源，
  // 已过滤禁用模型并应用映射去前缀；经 /v1 调用自动多端点负载均衡）。
  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/openai/models');
        const data = await res.json();
        const list = Array.isArray(data) ? data : (data.data || []);
        const options = (list || [])
          .filter((m) => m && m.id)
          .map((m) => ({ value: m.id, label: m.id }));
        options.sort((a, b) => a.label.localeCompare(b.label));
        setModelOptions(options);
      } catch {
        setModelOptions([]);
      }
    })();
  }, []);

  // 配置多选模型仅在模型网关可用列表内收敛：端点列表删除模型后，
  // /api/openai/models 不再返回该 id，这里自动剔除对应选中项并静默持久化，
  // 避免已删模型仍被后端 summaryModel 引用；网关无可用模型时不清空配置。
  const prunedRef = useRef(false);
  useEffect(() => {
    if (!values || loading) return undefined;
    if (!modelOptions.length || prunedRef.current) return undefined;
    const available = new Set(modelOptions.map((o) => o.value));
    const next = { ...values };
    let changed = false;
    for (const field of SETTING_FIELDS) {
      if (field.kind !== 'multi_select') continue;
      const raw = String(next[field.key] || '').trim();
      const kept = raw
        .split(',')
        .map((s) => s.trim())
        .filter((m) => m && available.has(m))
        .join(',');
      if (kept !== raw) {
        next[field.key] = kept;
        changed = true;
      }
    }
    if (!changed) return undefined;
    prunedRef.current = true;
    setValues(next);
    // 只写发生变化的键，避免静默覆盖用户已改但未保存的表单值；
    // 后端按 key 逐键 INSERT OR REPLACE，缺失的键保持不变。
    const prunedKeys = SETTING_FIELDS.filter(
      (f) => f.kind === 'multi_select' && next[f.key] !== values[f.key]
    );
    if (prunedKeys.length) {
      const body = Object.fromEntries(prunedKeys.map((f) => [f.key, next[f.key]]));
      fetch('/api/admin-ai/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      }).catch(() => {});
    }
    return undefined;
  }, [loading, modelOptions, values]);

  useEffect(() => {
    if (!savedAt) return undefined;
    const timer = window.setTimeout(() => setSavedAt(0), 2500);
    return () => window.clearTimeout(timer);
  }, [savedAt]);

  const setField = (key, value) => setValues((prev) => ({ ...prev, [key]: value }));

  const save = async () => {
    setSaving(true);
    try {
      const res = await fetch('/api/admin-ai/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(values || {}),
      });
      const data = await res.json();
      if ((data.data || data).ok) {
        setSavedAt(Date.now());
        toast.success('设置已保存');
      } else {
        toast.error('保存失败');
      }
    } catch {
      toast.error('保存失败');
    } finally {
      setSaving(false);
    }
  };

  return { values, loading, saving, savedAt, modelOptions, setField, save };
}
