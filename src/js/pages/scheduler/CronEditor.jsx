import React from 'react';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { PERIOD_ITEMS, WEEKDAY_ITEMS } from './constants.js';
import { formatTimestamp, getCronExpressionFromSimple } from './utils.js';

export function CronEditor({ form, setForm, preview, previewError }) {
  const currentSchedule = getCronExpressionFromSimple(form);
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3 rounded-md border border-kumo-line p-3">
        <div>
          <div className="text-sm font-medium text-kumo-strong">可视化 Cron 编辑器</div>
          <div className="text-xs text-kumo-subtle">按周期自动生成表达式。</div>
        </div>
        <Switch
          checked={form.useCustom}
          onCheckedChange={(checked) => setForm((prev) => ({ ...prev, useCustom: Boolean(checked) }))}
        />
      </div>

      {form.useCustom ? (
        <Input
          size="sm"
          label="Cron 表达式"
          value={form.schedule}
          onChange={(event) => setForm((prev) => ({ ...prev, schedule: event.target.value }))}
        />
      ) : (
        <div className="space-y-3">
          <Select alignItemWithTrigger size="sm" label="周期" className="w-full" value={form.periodType} onValueChange={(value) => setForm((prev) => ({ ...prev, periodType: value }))} items={PERIOD_ITEMS} />
          {form.periodType === 'week' && (
            <Select alignItemWithTrigger size="sm" label="星期" className="w-full" value={form.weekday} onValueChange={(value) => setForm((prev) => ({ ...prev, weekday: value }))} items={WEEKDAY_ITEMS} />
          )}
          {form.periodType === 'month' && (
            <Input size="sm" type="number" label="日期" min="1" max="31" value={form.dayOfMonth} onChange={(event) => setForm((prev) => ({ ...prev, dayOfMonth: Number(event.target.value) }))} />
          )}
          {['day', 'week', 'month'].includes(form.periodType) && (
            <div className="grid grid-cols-2 gap-3">
              <Input size="sm" type="number" label="小时" min="0" max="23" value={form.hour} onChange={(event) => setForm((prev) => ({ ...prev, hour: Number(event.target.value) }))} />
              <Input size="sm" type="number" label="分钟" min="0" max="59" value={form.minute} onChange={(event) => setForm((prev) => ({ ...prev, minute: Number(event.target.value) }))} />
            </div>
          )}
          {form.periodType === 'hour' && (
            <Input size="sm" type="number" label="分钟" min="0" max="59" value={form.minute} onChange={(event) => setForm((prev) => ({ ...prev, minute: Number(event.target.value) }))} />
          )}
        </div>
      )}

      <div className="grid gap-3 cq-sm:grid-cols-[minmax(0,1fr)_minmax(0,1.3fr)]">
        <div className="flex items-center px-3 py-2 rounded-md border border-kumo-line bg-kumo-recessed font-mono text-xs text-kumo-default">
          {currentSchedule || '手动触发'}
        </div>
        <div className="rounded-md border border-kumo-line px-3 py-2 text-xs">
          {previewError ? (
            <span className="text-kumo-danger">{previewError}</span>
          ) : preview?.summary ? (
            <div className="space-y-1">
              <div className="font-medium text-kumo-strong">{preview.summary}</div>
              <div className="text-kumo-subtle">未来执行：{(preview.next || []).map(formatTimestamp).join('、')}</div>
            </div>
          ) : (
            <span className="text-kumo-subtle">填写周期后会预览未来 5 次运行时间。</span>
          )}
        </div>
      </div>
    </div>
  );
}
