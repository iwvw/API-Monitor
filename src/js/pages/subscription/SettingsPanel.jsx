import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SectionCard } from '../../components/ui/AppPrimitives.jsx';
import { Save } from '../../components/Icons.jsx';

export default function SettingsPanel({ settings, setSettings, templateItems, onSaveSettings }) {
  return settings && (
    <SectionCard title="默认策略" className="max-w-3xl">
        <div className="grid gap-4 cq-sm:grid-cols-2">
          <Select alignItemWithTrigger size="sm" label="默认模板" value={settings.default_template_id} onValueChange={(value) => setSettings((prev) => ({ ...prev, default_template_id: String(value) }))} items={templateItems} />
          <Input size="sm" label="默认上游刷新间隔（小时）" type="number" value={settings.default_refresh_hours || 24} onChange={(e) => setSettings((prev) => ({ ...prev, default_refresh_hours: Number(e.target.value) || 24 }))} />
          <Input size="sm" label="默认限流阈值（次/分钟）" type="number" value={settings.default_rate_limit_per_minute || 30} onChange={(e) => setSettings((prev) => ({ ...prev, default_rate_limit_per_minute: Number(e.target.value) || 30 }))} />
          <Switch
            size="sm"
            label="默认启用限流"
            controlFirst={false}
            checked={!!settings.default_rate_limit_enabled}
            onCheckedChange={(checked) => setSettings((prev) => ({ ...prev, default_rate_limit_enabled: checked }))}
          />
        </div>
        <div className="mt-4 flex justify-end">
          <Button size="sm" variant="primary" onClick={onSaveSettings}><Save className="h-3.5 w-3.5" />保存设置</Button>
        </div>
    </SectionCard>
  );
}
