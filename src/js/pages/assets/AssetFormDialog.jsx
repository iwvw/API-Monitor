import React, { useEffect, useMemo, useState } from 'react';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { LayerDialog } from '@cloudflare/kumo/components/layer-dialog';
import { Text } from '@cloudflare/kumo';
import { DateField } from '../../components/ui/DateField.jsx';
import { CURRENCY_OPTIONS, CATEGORIES, COST_CYCLES, PERSISTED_STATUSES, TYPE_BY_CATEGORY } from './constants.js';
import { assetToForm, emptyForm, formToPayload, validateForm } from './utils.js';

export default function AssetFormDialog({ open, mode, asset, saving, onClose, onSubmit }) {
  const [form, setForm] = useState(emptyForm());
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    setError('');
    if (mode === 'edit' && asset) {
      setForm(assetToForm(asset));
    } else {
      setForm(emptyForm());
    }
  }, [open, mode, asset]);

  const typeOptions = useMemo(() => TYPE_BY_CATEGORY[form.category] || [], [form.category]);
  const isPhysical = form.category === 'physical';

  const update = patch => setForm(prev => ({ ...prev, ...patch }));

  const changeCategory = category => {
    const types = TYPE_BY_CATEGORY[category] || [];
    update({ category, asset_type: types[0]?.value || '' });
  };

  const submit = () => {
    const message = validateForm(form);
    if (message) {
      setError(message);
      return;
    }
    setError('');
    void onSubmit(formToPayload(form));
  };

  return (
    <LayerDialog.Root open={open} onOpenChange={next => { if (!next) onClose(); }}>
      <LayerDialog.Content size="xl">
        <LayerDialog.Title>{mode === 'edit' ? '编辑资产' : '登记资产'}</LayerDialog.Title>
        <LayerDialog.Description>
          登记面板管不到的实体资产，或为已有对象建立资产台账。到期时刻按站点时区展示。
        </LayerDialog.Description>
        <LayerDialog.Body>
          <form
            id="asset-form"
            className="space-y-4"
            onSubmit={event => { event.preventDefault(); submit(); }}
          >
            <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-2">
              <Input
                size="sm"
                label="资产名称"
                value={form.name}
                onChange={event => update({ name: event.target.value })}
                placeholder="如 生产环境数据库服务器"
                autoFocus
              />
              <div className="flex min-w-0 flex-col gap-1.5">
                <span className="text-xs font-medium text-kumo-strong">资产分类</span>
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="资产分类"
                  value={form.category}
                  onValueChange={changeCategory}
                  items={CATEGORIES.map(item => ({ value: item.value, label: item.label }))}
                />
              </div>
              <div className="flex min-w-0 flex-col gap-1.5">
                <span className="text-xs font-medium text-kumo-strong">资产类型</span>
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="资产类型"
                  value={form.asset_type}
                  onValueChange={value => update({ asset_type: value })}
                  items={typeOptions}
                />
              </div>
              <div className="flex min-w-0 flex-col gap-1.5">
                <span className="text-xs font-medium text-kumo-strong">持久状态</span>
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="持久状态"
                  value={form.status}
                  onValueChange={value => update({ status: value })}
                  items={PERSISTED_STATUSES}
                />
              </div>
              <Input
                size="sm"
                label="提供方"
                value={form.provider}
                onChange={event => update({ provider: event.target.value })}
                placeholder="如 阿里云 / 自建机房"
              />
              <Input
                size="sm"
                label="负责人"
                value={form.owner}
                onChange={event => update({ owner: event.target.value })}
                placeholder="如 张三"
              />
              {isPhysical && (
                <>
                  <Input
                    size="sm"
                    label="位置"
                    value={form.location}
                    onChange={event => update({ location: event.target.value })}
                    placeholder="如 机房 A 区 3 号柜"
                  />
                  <Input
                    size="sm"
                    label="序列号"
                    value={form.serial_no}
                    onChange={event => update({ serial_no: event.target.value })}
                  />
                  <Input
                    size="sm"
                    label="型号"
                    value={form.model}
                    onChange={event => update({ model: event.target.value })}
                  />
                </>
              )}
            </div>

            <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-2">
              <div className="flex min-w-0 flex-col gap-1.5">
                <span className="text-xs font-medium text-kumo-strong">购置日期</span>
                <DateField
                  value={form.acquire_date}
                  onChange={value => update({ acquire_date: value })}
                  placeholder="选择购置日期"
                />
              </div>
              <div className="flex min-w-0 flex-col gap-1.5">
                <span className="text-xs font-medium text-kumo-strong">到期日期</span>
                <DateField
                  value={form.expire_at}
                  onChange={value => update({ expire_at: value })}
                  placeholder="选择到期日期"
                />
              </div>
              <Input
                size="sm"
                label="告警阈值（天，逗号分隔）"
                value={form.warn_days_text}
                onChange={event => update({ warn_days_text: event.target.value })}
                placeholder="留空使用全局默认，如 30, 14, 7"
              />
              <div className="flex min-w-0 items-center justify-between gap-2 rounded-md border border-kumo-line px-3 py-2">
                <div className="min-w-0">
                  <div className="text-xs font-medium text-kumo-strong">自动续费</div>
                  <div className="mt-0.5 text-[11px] text-kumo-subtle">开启后不再产生到期告警</div>
                </div>
                <Switch
                  size="sm"
                  aria-label="自动续费"
                  checked={form.auto_renew}
                  onCheckedChange={checked => update({ auto_renew: checked })}
                />
              </div>
            </div>

            <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-3">
              <Input
                size="sm"
                label="成本金额"
                value={form.cost_amount}
                onChange={event => update({ cost_amount: event.target.value })}
                placeholder="如 1200"
              />
              <div className="flex min-w-0 flex-col gap-1.5">
                <span className="text-xs font-medium text-kumo-strong">币种</span>
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="币种"
                  value={form.cost_currency}
                  onValueChange={value => update({ cost_currency: value })}
                  items={[{ value: '', label: '未指定' }, ...CURRENCY_OPTIONS]}
                />
              </div>
              <div className="flex min-w-0 flex-col gap-1.5">
                <span className="text-xs font-medium text-kumo-strong">计费周期</span>
                <Select
                  alignItemWithTrigger
                  size="sm"
                  aria-label="计费周期"
                  value={form.cost_cycle}
                  onValueChange={value => update({ cost_cycle: value })}
                  items={[{ value: '', label: '未指定' }, ...COST_CYCLES]}
                />
              </div>
            </div>

            <Input
              size="sm"
              label="标签（逗号分隔）"
              value={form.tags_text}
              onChange={event => update({ tags_text: event.target.value })}
              placeholder="如 生产, 核心, 华东"
            />
            <Input
              size="sm"
              label="备注"
              value={form.remark}
              onChange={event => update({ remark: event.target.value })}
              placeholder="补充说明"
            />

            {error && (
              <Text variant="secondary" size="xs" className="text-kumo-danger">{error}</Text>
            )}
          </form>
        </LayerDialog.Body>
        <LayerDialog.Actions dismissLabel="取消">
          <LayerDialog.Actions.Primary type="submit" form="asset-form" loading={saving}>
            {mode === 'edit' ? '保存' : '登记'}
          </LayerDialog.Actions.Primary>
        </LayerDialog.Actions>
      </LayerDialog.Content>
    </LayerDialog.Root>
  );
}
