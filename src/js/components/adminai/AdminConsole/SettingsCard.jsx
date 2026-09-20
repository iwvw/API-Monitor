import React from 'react';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Select } from '@cloudflare/kumo/components/select';
import { Loader } from '@cloudflare/kumo';
import { SectionCard, FieldRow, cx } from '../../ui/AppPrimitives.jsx';
import { ShieldCheck } from '../../Icons.jsx';
import { SETTING_FIELDS, SETTING_SECTIONS, REASONING_EFFORT_OPTIONS } from './constants.jsx';

export function SettingsCard({ form }) {
  const { values, loading, modelOptions, setField } = form;

  if (loading) {
    return <div className="flex justify-center py-10"><Loader size={20} className="text-kumo-subtle" /></div>;
  }

  const renderField = (field) => {
    const value = (values && values[field.key]) || (field.kind === 'switch' ? 'false' : '');
    if (field.kind === 'switch') {
      if (field.group === 'security') {
        // 安全与审批：开关渲染为卡片风格（与 AI 接入权限卡片同款：图标+文本，
        // 选中态=开关打开）；颜色统一：选中 border/brand + text/brand，
        // 未选中 border/kumo-line + text/kumo-strong
        const checked = value === 'true';
        return (
          <button
            key={field.key}
            type="button"
            aria-pressed={checked}
            onClick={() => setField(field.key, checked ? 'false' : 'true')}
className={cx(
              'flex flex-col items-center gap-1.5 rounded-lg border px-3 py-3',
              checked
                ? 'border-(--text-color-brand) bg-kumo-tint text-brand'
                : 'border-kumo-line bg-kumo-recessed/25 text-kumo-strong hover:bg-kumo-recessed/50'
            )}
          >
            <ShieldCheck className={cx('h-4 w-4', checked ? 'text-brand' : 'text-kumo-strong')} />
            <span className="text-xs font-medium">{field.label}</span>
          </button>
        );
      }
      return (
        <FieldRow key={field.key} title={field.label} description={field.description}>
          <Switch
            checked={value === 'true'}
            onCheckedChange={(checked) => setField(field.key, checked ? 'true' : 'false')}
          />
        </FieldRow>
      );
    }
    let control;
    if (field.kind === 'multi_select') {
      // 多选摘要模型：值存逗号串（后端按候选顺序逐个失败回退）；
      // 只把仍存在于可用候选中的 id 传给 Select，已删/停用的模型 id 不参与显示。
      const available = new Set(modelOptions.map((o) => o.value));
      const current = String(value || '')
        .split(',')
        .map((s) => s.trim())
        .filter((s) => s && available.has(s));
      control = (
        <Select
          multiple
          placeholder={modelOptions.length ? '选择摘要模型' : '模型网关无可用模型'}
          items={modelOptions}
          value={current}
          onValueChange={(vals) => setField(field.key, (vals || []).join(','))}
          renderValue={(vals) => (vals && vals.length ? `已选 ${vals.length} 个` : null)}
          size="sm"
          className="w-full"
        />
      );
    } else if (field.kind === 'effort_select') {
      control = (
        <Select
          alignItemWithTrigger
          placeholder="不指定"
          value={value || undefined}
          onValueChange={(v) => setField(field.key, v ? String(v) : '')}
          items={REASONING_EFFORT_OPTIONS}
          size="sm"
          className="w-full"
        />
      );
    } else if (field.kind === 'select') {
      control = (
        <Select alignItemWithTrigger
          placeholder={modelOptions.length ? '选择模型' : '模型网关无可用模型'}
          value={value || undefined}
          onValueChange={(v) => setField(field.key, String(v))}
          items={modelOptions}
          size="sm"
          className="w-full"
        />
      );
    } else {
      control = (
        <Input
          size="sm"
          className={field.kind === 'number' ? 'w-24' : 'w-full'}
          type={field.kind === 'number' ? 'number' : 'text'}
          placeholder={field.placeholder}
          aria-label={field.label}
          value={value}
          onChange={(e) => setField(field.key, e.target.value)}
        />
      );
    }
    return (
      <FieldRow key={field.key} title={field.label} description={field.description}>
        {control}
      </FieldRow>
    );
  };

  return (
    <div className="space-y-4">
      {SETTING_SECTIONS.map((section) => (
        <SectionCard key={section.key} icon={section.icon} title={section.title} description={section.description} bodyPadding="none">
          {section.key === 'security' ? (
            <div className="grid gap-3 p-4 grid-cols-2">
              {SETTING_FIELDS.filter((field) => field.group === section.key).map(renderField)}
            </div>
          ) : (
            SETTING_FIELDS.filter((field) => field.group === section.key).map(renderField)
          )}
        </SectionCard>
      ))}
    </div>
  );
}

export default SettingsCard;
