import React, { useEffect, useState } from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Loader } from '@cloudflare/kumo';
import { SectionCard, FieldRow } from '../../ui/AppPrimitives.jsx';
import { toast } from '../../../modules/toast.js';
import { MessageSquare } from '../../Icons.jsx';
import { BRIEFING_TEMPLATE_OPTIONS } from './constants.jsx';

/* ==================== 模板页（站点简报格式模板） ==================== */

export function TemplatesCard() {
  const [cfg, setCfg] = useState({ type: 'standard', custom: '' });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [savedAt, setSavedAt] = useState(0);

  useEffect(() => {
    (async () => {
      try {
        const res = await fetch('/api/admin-ai/settings');
        const data = await res.json();
        const body = data.data || data;
        const raw = (body.settings || {})['admin_ai_briefing_template'];
        if (raw) {
          try {
            setCfg((prev) => ({ ...prev, ...JSON.parse(raw) }));
          } catch {
          }
        }
      } catch {
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  useEffect(() => {
    if (!savedAt) return undefined;
    const timer = window.setTimeout(() => setSavedAt(0), 2500);
    return () => window.clearTimeout(timer);
  }, [savedAt]);

  const option = BRIEFING_TEMPLATE_OPTIONS.find((o) => o.value === cfg.type) || BRIEFING_TEMPLATE_OPTIONS[0];

  const handleSave = async () => {
    setSaving(true);
    try {
      const res = await fetch('/api/admin-ai/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ admin_ai_briefing_template: JSON.stringify(cfg) }),
      });
      const data = await res.json();
      if ((data.data || data).ok) {
        setSavedAt(Date.now());
        toast.success('模板已保存');
      } else {
        toast.error('保存失败');
      }
    } catch {
      toast.error('保存失败');
    } finally {
      setSaving(false);
    }
  };

  if (loading) {
    return <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>;
  }

  return (
    <div className="space-y-4 pb-4">
      <SectionCard
        icon={<MessageSquare className="h-4 w-4 text-brand" />}
        title="站点简报模板"
        description="/briefing"
        bodyPadding="none"
      >
        <FieldRow title="模板类型" description={option.description}>
          <Select alignItemWithTrigger
            size="sm"
            className="w-full"
            value={cfg.type}
            onValueChange={(v) => setCfg((prev) => ({ ...prev, type: v }))}
            items={BRIEFING_TEMPLATE_OPTIONS.map((o) => ({ value: o.value, label: o.label }))}
          />
        </FieldRow>
        {cfg.type === 'custom' && (
          <div className="border-b border-kumo-line px-4 py-3">
            <Textarea
              rows={3}
              value={cfg.custom}
              onChange={(e) => setCfg((prev) => ({ ...prev, custom: e.target.value }))}
              placeholder="编写简报格式要求，如：使用表格呈现所有指标，先异常后正常；结尾附明日关注事项…"
              className="w-full"
            />
          </div>
        )}
        <div className="flex items-center gap-3 px-4 py-3">
          <Button size="sm" variant="primary" onClick={handleSave} disabled={saving}>
            {saving ? '保存中...' : '保存'}
          </Button>
        </div>
      </SectionCard>
    </div>
  );
}

export default TemplatesCard;
